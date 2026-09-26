package telegram

import (
	"context"
	"database/sql"
	"errors"

	"github.com/gotd/td/tg"
)

// DeleteForumTopic removes one known topic from Telegram and applies the same
// local tombstone immediately. The update dispatcher remains idempotent if
// Telegram delivers the deletion event afterwards.
func (s *Service) DeleteForumTopic(ctx context.Context, groupID, topicID string) error {
	var channelID, accessHash int64
	var telegramTopicID int
	err := s.DB.QueryRowContext(ctx, `SELECT c.telegram_peer_id,COALESCE(c.access_hash,0),t.telegram_topic_id
		FROM tgstrm.topics t JOIN tgstrm.chats c ON c.id=t.chat_id
		WHERE c.id::text=$1 AND t.id::text=$2 AND c.account_id=$3 AND c.is_active AND t.is_active
		AND c.telegram_peer_type='channel'`, groupID, topicID, accountID).
		Scan(&channelID, &accessHash, &telegramTopicID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrTopicNotFound
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
	if _, err = api.MessagesDeleteTopicHistory(ctx, &tg.MessagesDeleteTopicHistoryRequest{
		Peer:     &tg.InputPeerChannel{ChannelID: channelID, AccessHash: accessHash},
		TopMsgID: telegramTopicID,
	}); err != nil {
		return err
	}
	return s.removeDeletedTopics(ctx, channelID, []int{telegramTopicID})
}
