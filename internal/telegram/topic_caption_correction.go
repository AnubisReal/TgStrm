package telegram

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

// Change only an owned video in a replicated destination, preserving its ID.
func (s *Service) CorrectReplicatedVideoCaption(ctx context.Context, topicID, messageID, previous, desired string) error {
	var channelID, hash int64
	var remoteID int
	err := s.DB.QueryRowContext(ctx, `SELECT c.telegram_peer_id,COALESCE(c.access_hash,0),m.telegram_message_id
 FROM tgstrm.messages m JOIN tgstrm.topics t ON t.id=m.topic_id
 JOIN tgstrm.managed_catalog_chats c ON c.id=t.chat_id
 WHERE t.id::text=$1 AND m.id::text=$2 AND m.is_active AND m.media_type='video' AND t.is_active AND c.is_active
 AND EXISTS(SELECT 1 FROM tgstrm.topic_replications tr WHERE tr.destination_topic_id=t.id)`, topicID, messageID).Scan(&channelID, &hash, &remoteID)
	if err != nil {
		return err
	}
	s.apiMu.RLock()
	api := s.runtimeAPI
	s.apiMu.RUnlock()
	if api == nil {
		return errors.New("Telegram se está reconectando")
	}
	found, err := api.ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{Channel: &tg.InputChannel{ChannelID: channelID, AccessHash: hash}, ID: []tg.InputMessageClass{&tg.InputMessageID{ID: remoteID}}})
	if err != nil {
		return err
	}
	var applied sql.NullString
	_ = s.DB.QueryRowContext(ctx, `SELECT last_applied_caption FROM tgstrm.mirror_deliveries WHERE destination_message_id::text=$1 AND status='completed' ORDER BY updated_at DESC LIMIT 1`, messageID).Scan(&applied)
	var actual *tg.Message
	if modified, ok := found.AsModified(); ok {
		for _, item := range modified.GetMessages() {
			if m, ok := item.(*tg.Message); ok && m.ID == remoteID {
				actual = m
			}
		}
	}
	if actual == nil || !actual.Out || (actual.Message != previous && actual.Message != desired && (!applied.Valid || actual.Message != applied.String)) {
		return fmt.Errorf("%w: el archivo se borró o su pie cambió manualmente", ErrTopicIdentityConflict)
	}
	if actual.Message != desired {
		req := &tg.MessagesEditMessageRequest{Peer: &tg.InputPeerChannel{ChannelID: channelID, AccessHash: hash}, ID: remoteID}
		req.SetMessage(desired)
		if _, err = api.MessagesEditMessage(ctx, req); err != nil && !tgerr.Is(err, "MESSAGE_NOT_MODIFIED") {
			return err
		}
	}
	return nil
}
