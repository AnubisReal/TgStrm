package telegram

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"unicode/utf8"
)

var ErrTopicIdentityConflict = errors.New("la identidad del topic requiere revisión")

// Correct only a replicated destination. Never rename the external source.
func (s *Service) CorrectReplicatedTopicIdentity(ctx context.Context, topicID, previous, desired, poster string, tmdbID int64) error {
	if !utf8.ValidString(desired) || len(desired) > 128 {
		return fmt.Errorf("%w: el título corregido excede el límite de Telegram (128 bytes)", ErrTopicIdentityConflict)
	}
	var channelID, hash int64
	var threadID int
	var groupID string
	if err := s.DB.QueryRowContext(ctx, `SELECT c.telegram_peer_id,COALESCE(c.access_hash,0),t.telegram_topic_id,c.id::text FROM tgstrm.topics t JOIN tgstrm.managed_catalog_chats c ON c.id=t.chat_id WHERE t.id::text=$1 AND t.is_active AND c.is_active AND EXISTS(SELECT 1 FROM tgstrm.topic_replications tr WHERE tr.destination_topic_id=t.id)`, topicID).Scan(&channelID, &hash, &threadID, &groupID); err != nil {
		return err
	}
	s.apiMu.RLock()
	api := s.runtimeAPI
	s.apiMu.RUnlock()
	if api == nil {
		return errors.New("Telegram se está reconectando")
	}
	peer := &tg.InputPeerChannel{ChannelID: channelID, AccessHash: hash}
	topics, err := api.MessagesGetForumTopicsByID(ctx, &tg.MessagesGetForumTopicsByIDRequest{Peer: peer, Topics: []int{threadID}})
	if err != nil {
		return err
	}
	var current string
	for _, item := range topics.Topics {
		if topic, ok := item.(*tg.ForumTopic); ok && topic.ID == threadID {
			current = topic.Title
		}
	}
	if current == "" || (current != previous && current != desired) {
		return fmt.Errorf("%w: el título cambió manualmente en Telegram; revisa la coincidencia", ErrTopicIdentityConflict)
	}
	if current != desired {
		request := &tg.MessagesEditForumTopicRequest{Peer: peer, TopicID: threadID}
		request.SetTitle(desired)
		if _, err = api.MessagesEditForumTopic(ctx, request); err != nil && !tgerr.Is(err, "TOPIC_NOT_MODIFIED") {
			return err
		}
	}
	if _, err = s.DB.ExecContext(ctx, `UPDATE tgstrm.topics SET title=$2,updated_at=now() WHERE id::text=$1`, topicID, desired); err != nil {
		return err
	}
	if _, err = s.DB.ExecContext(ctx, `INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload) VALUES('topics.snapshot','group',$1::bigint,jsonb_build_object('reason','identity_title_corrected'))`, groupID); err != nil {
		return err
	}
	// Existing presentation is edited in place, keeping its position and message ID.
	var messageID int
	var oldID int64
	var oldType string
	err = s.DB.QueryRowContext(ctx, `SELECT COALESCE(telegram_message_id,0),tmdb_id,media_type FROM tgstrm.topic_presentations WHERE topic_id::text=$1`, topicID).Scan(&messageID, &oldID, &oldType)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if errors.Is(err, sql.ErrNoRows) || messageID == 0 {
		return fmt.Errorf("%w: la presentación no tiene un mensaje confirmado", ErrTopicIdentityConflict)
	}
	if oldID != tmdbID && poster == "" {
		return fmt.Errorf("%w: la nueva coincidencia no tiene póster; no se conservará como correcta una imagen de otra serie", ErrTopicIdentityConflict)
	}
	if err == nil && messageID > 0 {
		caption := fmt.Sprintf("https://www.themoviedb.org/tv/%d", tmdbID)
		oldCaption := fmt.Sprintf("https://www.themoviedb.org/%s/%d", oldType, oldID)
		found, readErr := api.ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{Channel: &tg.InputChannel{ChannelID: channelID, AccessHash: hash}, ID: []tg.InputMessageClass{&tg.InputMessageID{ID: messageID}}})
		if readErr != nil {
			return readErr
		}
		verified := false
		if modified, ok := found.AsModified(); ok {
			for _, item := range modified.GetMessages() {
				if msg, ok := item.(*tg.Message); ok && msg.Out && (msg.Message == oldCaption || msg.Message == caption) {
					verified = true
				}
			}
		}
		if !verified {
			return fmt.Errorf("%w: la presentación cambió o se borró; no se sobrescribirá", ErrTopicIdentityConflict)
		}
		request := &tg.MessagesEditMessageRequest{Peer: peer, ID: messageID}
		request.SetMessage(caption)
		if poster != "" {
			if !tmdbPosterPath.MatchString(poster) {
				return fmt.Errorf("%w: póster no válido", ErrTopicIdentityConflict)
			}
			request.SetMedia(&tg.InputMediaPhotoExternal{URL: "https://image.tmdb.org/t/p/w780" + poster})
		}
		if _, err = api.MessagesEditMessage(ctx, request); err != nil && !tgerr.Is(err, "MESSAGE_NOT_MODIFIED") {
			return err
		}
		if _, err = s.DB.ExecContext(ctx, `UPDATE tgstrm.topic_presentations SET tmdb_id=$2,poster_path=$3,media_type='tv',last_error=NULL,updated_at=now() WHERE topic_id::text=$1`, topicID, tmdbID, poster); err != nil {
			return err
		}
	}
	return nil
}
