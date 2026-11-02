package telegram

import (
	"context"
	"database/sql"
	"errors"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

// Normalize only a direct, owned movie in a managed destination. The remote
// caption is checked first so delayed jobs cannot overwrite intervening edits.
func (s *Service) CorrectManagedMovieCaption(ctx context.Context, messageID string, tmdbID int64, previous, desired string) error {
	var channel, hash int64
	var remote int
	err := s.DB.QueryRowContext(ctx, `SELECT c.telegram_peer_id,COALESCE(c.access_hash,0),m.telegram_message_id
 FROM tgstrm.messages m JOIN tgstrm.topics t ON t.id=m.topic_id
 JOIN tgstrm.managed_catalog_chats c ON c.id=m.chat_id
 JOIN tgstrm.tmdb_matches tm ON tm.message_id=m.id AND tm.media_type='movie'
 WHERE m.id::text=$1 AND tm.tmdb_id=$2 AND trim(tm.title)||' ('||to_char(tm.release_date,'YYYY')||')'=$3
 AND m.body IN ($4,$3) AND m.is_active AND m.is_outgoing AND m.media_type='video'
 AND t.is_active AND t.library_kind='movie_collection' AND c.is_active AND c.telegram_peer_type='channel'
 AND NOT EXISTS(SELECT 1 FROM tgstrm.movie_monitor_deliveries d WHERE d.destination_message_id=m.id)
 AND NOT EXISTS(SELECT 1 FROM tgstrm.catalog_import_messages d WHERE d.destination_message_id=m.id)
 AND NOT EXISTS(SELECT 1 FROM tgstrm.mirror_deliveries d WHERE d.destination_message_id=m.id)`, messageID, tmdbID, desired, previous).Scan(&channel, &hash, &remote)
	if err != nil {
		return err
	}
	s.apiMu.RLock()
	api := s.runtimeAPI
	s.apiMu.RUnlock()
	if api == nil {
		return errors.New("Telegram se está reconectando")
	}
	found, err := api.ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{Channel: &tg.InputChannel{ChannelID: channel, AccessHash: hash}, ID: []tg.InputMessageClass{&tg.InputMessageID{ID: remote}}})
	if err != nil {
		return err
	}
	var actual *tg.Message
	if data, ok := found.AsModified(); ok {
		for _, item := range data.GetMessages() {
			if m, ok := item.(*tg.Message); ok && m.ID == remote {
				actual = m
			}
		}
	}
	if actual == nil {
		return sql.ErrNoRows
	}
	if !actual.Out || (actual.Message != previous && actual.Message != desired) {
		return ErrTopicIdentityConflict
	}
	if actual.Message == desired {
		return nil
	}
	req := &tg.MessagesEditMessageRequest{Peer: &tg.InputPeerChannel{ChannelID: channel, AccessHash: hash}, ID: remote, NoWebpage: true}
	req.SetMessage(desired)
	_, err = api.MessagesEditMessage(ctx, req)
	if tgerr.Is(err, "MESSAGE_NOT_MODIFIED") {
		return nil
	}
	return err
}
