package telegram

import (
	"context"
	"errors"
	"fmt"
	"github.com/gotd/td/tg"
)

func (s *Service) fileTransferPeer(ctx context.Context, group string) (tg.InputPeerClass, error) {
	var kind string
	var id, hash int64
	if err := s.DB.QueryRowContext(ctx, `SELECT telegram_peer_type,telegram_peer_id,COALESCE(access_hash,0) FROM tgstrm.chats WHERE id::text=$1 AND account_id=$2 AND is_active`, group, accountID).Scan(&kind, &id, &hash); err != nil {
		return nil, err
	}
	switch kind {
	case "channel":
		if hash != 0 {
			return &tg.InputPeerChannel{ChannelID: id, AccessHash: hash}, nil
		}
	case "chat":
		return &tg.InputPeerChat{ChatID: id}, nil
	}
	return nil, errors.New("el grupo no está disponible; actualiza tus grupos")
}

// Copies preserve captions and media. The persisted job and source message
// identify each Telegram random_id across retries; no download/upload occurs.
func (s *Service) ForwardFileBatch(ctx context.Context, job, sourceGroup, destinationGroup, destinationTopic string, ids []int) error {
	if len(ids) == 0 || len(ids) > 100 {
		return errors.New("lote de archivos no válido")
	}
	from, err := s.fileTransferPeer(ctx, sourceGroup)
	if err != nil {
		return err
	}
	to, err := s.fileTransferPeer(ctx, destinationGroup)
	if err != nil {
		return err
	}
	request := &tg.MessagesForwardMessagesRequest{FromPeer: from, ToPeer: to, ID: ids, DropAuthor: true}
	for _, id := range ids {
		request.RandomID = append(request.RandomID, stableRandomID("manual-file-transfer", job, fmt.Sprint(id)))
	}
	if destinationTopic != "" {
		var thread int
		if err = s.DB.QueryRowContext(ctx, `SELECT telegram_topic_id FROM tgstrm.topics WHERE id::text=$1 AND chat_id::text=$2 AND is_active`, destinationTopic, destinationGroup).Scan(&thread); err != nil {
			return err
		}
		if thread > 0 {
			request.SetTopMsgID(thread)
		}
	}
	s.apiMu.RLock()
	api := s.runtimeAPI
	s.apiMu.RUnlock()
	if api == nil {
		return errors.New("Telegram se está reconectando")
	}
	updates, err := api.MessagesForwardMessages(ctx, request)
	if err != nil {
		return err
	}
	return confirmForwardedFileBatch(updates, request.RandomID)
}

func confirmForwardedFileBatch(updates tg.UpdatesClass, randomIDs []int64) error {
	var list []tg.UpdateClass
	switch u := updates.(type) {
	case *tg.Updates:
		list = u.Updates
	case *tg.UpdatesCombined:
		list = u.Updates
	case *tg.UpdateShort:
		list = []tg.UpdateClass{u.Update}
	}
	confirmed := make(map[int64]bool)
	for _, u := range list {
		if m, ok := u.(*tg.UpdateMessageID); ok && m.ID > 0 {
			confirmed[m.RandomID] = true
		}
	}
	for _, id := range randomIDs {
		if !confirmed[id] {
			return errors.New("Telegram no confirmó todos los archivos; revisa el destino antes de repetir el envío")
		}
	}
	return nil
}
