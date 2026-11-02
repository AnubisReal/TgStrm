package telegram

import (
	"context"
	"database/sql"
	"errors"

	"github.com/gotd/td/tg"
)

// A locally unavailable queued destination is checked remotely once. Never
// substitute another group, create a replacement, or suppress Telegram errors.
func (s *Service) refreshQueuedImportDestination(ctx context.Context, importID string) (string, int64, int64, error) {
	var group string
	var peer, hash int64
	err := s.DB.QueryRowContext(ctx, `SELECT c.id::text,c.telegram_peer_id,COALESCE(c.access_hash,0) FROM tgstrm.catalog_imports ci JOIN tgstrm.chats c ON c.id=ci.destination_chat_id WHERE ci.id::text=$1 AND c.account_id=$2 AND c.telegram_peer_type='channel'`, importID, accountID).Scan(&group, &peer, &hash)
	if err != nil {
		return "", 0, 0, err
	}
	s.apiMu.RLock()
	api := s.runtimeAPI
	s.apiMu.RUnlock()
	if api == nil {
		return "", 0, 0, errors.New("Telegram se está reconectando")
	}
	response, err := api.ChannelsGetChannels(ctx, []tg.InputChannelClass{&tg.InputChannel{ChannelID: peer, AccessHash: hash}})
	if err != nil {
		return "", 0, 0, err
	}
	for _, chat := range response.GetChats() {
		channel, ok := confirmedImportForum(chat, peer)
		if !ok {
			continue
		}
		if _, err = s.DB.ExecContext(ctx, `UPDATE tgstrm.chats SET is_active=true,is_forum=true,access_hash=$3,title=$4,last_seen_at=now(),updated_at=now() WHERE id::text=$1 AND account_id=$2`, group, accountID, channel.AccessHash, channel.Title); err != nil {
			return "", 0, 0, err
		}
		return group, peer, channel.AccessHash, nil
	}
	return "", 0, 0, sql.ErrNoRows
}

func confirmedImportForum(chat tg.ChatClass, peer int64) (*tg.Channel, bool) {
	channel, ok := chat.(*tg.Channel)
	return channel, ok && channel.ID == peer && !channel.Left && channel.Forum && !channel.Min
}
