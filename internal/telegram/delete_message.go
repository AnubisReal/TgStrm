package telegram

import (
	"context"
	"database/sql"
	"errors"

	"github.com/gotd/td/tg"
)

// DeleteVideoMessage permanently removes one known channel message and mirrors
// the successful Telegram mutation locally without waiting for its update to
// make a round trip through the dispatcher.
func (s *Service) DeleteVideoMessage(ctx context.Context, messageID string) error {
	var channelID, accessHash int64
	var telegramMessageID int
	err := s.DB.QueryRowContext(ctx, `SELECT c.telegram_peer_id,COALESCE(c.access_hash,0),m.telegram_message_id
		FROM tgstrm.messages m JOIN tgstrm.chats c ON c.id=m.chat_id
		WHERE m.id::text=$1 AND m.is_active AND m.media_type='video' AND c.telegram_peer_type='channel'`, messageID).
		Scan(&channelID, &accessHash, &telegramMessageID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	s.apiMu.RLock()
	api := s.runtimeAPI
	s.apiMu.RUnlock()
	if api == nil {
		return errors.New("telegram no está conectado")
	}
	if _, err = api.ChannelsDeleteMessages(ctx, &tg.ChannelsDeleteMessagesRequest{
		Channel: &tg.InputChannel{ChannelID: channelID, AccessHash: accessHash},
		ID:      []int{telegramMessageID},
	}); err != nil {
		return err
	}
	return s.removeDeletedVideos(ctx, channelID, []int{telegramMessageID})
}
