package telegram

import (
	"context"
	"errors"
	"github.com/gotd/td/tg"
)

// RecoverReplicationGroup adopts only a forum owned by this account with the
// operation marker created before the uncertain request. Titles are not identity.
func (s *Service) RecoverReplicationGroup(ctx context.Context, replicationID, groupID string) error {
	s.importMu.Lock()
	defer s.importMu.Unlock()
	var peerID, hash int64
	if err := s.DB.QueryRowContext(ctx, `SELECT telegram_peer_id,COALESCE(access_hash,0) FROM tgstrm.chats WHERE id::text=$1 AND account_id=$2 AND is_active AND is_forum AND telegram_peer_type='channel'`, groupID, accountID).Scan(&peerID, &hash); err != nil {
		return err
	}
	s.apiMu.RLock()
	api := s.runtimeAPI
	s.apiMu.RUnlock()
	if api == nil {
		return errors.New("Telegram se está reconectando")
	}
	full, err := api.ChannelsGetFullChannel(ctx, &tg.InputChannel{ChannelID: peerID, AccessHash: hash})
	if err != nil {
		return err
	}
	detail, ok := full.FullChat.(*tg.ChannelFull)
	if !ok || detail.About != "Biblioteca TgStrm · "+replicationID {
		return errors.New("el grupo no pertenece a esta operación de creación")
	}
	owned := false
	for _, item := range full.Chats {
		if channel, ok := item.(*tg.Channel); ok && channel.ID == peerID && channel.Creator && channel.Forum {
			owned = true
		}
	}
	if !owned {
		return errors.New("el destino debe ser un foro propio")
	}
	result, err := s.DB.ExecContext(ctx, `UPDATE tgstrm.group_replications SET destination_chat_id=$2::bigint,status='running',last_error=NULL,updated_at=now() WHERE id::text=$1 AND account_id=$3 AND status='creating' AND destination_chat_id IS NULL`, replicationID, groupID, accountID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return errors.New("la creación ya cambió; vuelve a revisar")
	}
	return nil
}
