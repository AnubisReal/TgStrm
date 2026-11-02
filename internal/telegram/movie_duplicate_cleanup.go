package telegram

import (
	"context"
	"errors"
	"fmt"
	"github.com/gotd/td/tg"
	"strings"
)

// Delete only a planned redundant pair, after verifying its surviving document.
// Empty redundant messages are accepted so uncertain network replies can retry.
func (s *Service) DeleteDuplicateMoviePair(ctx context.Context, delivery string) error {
	var channel, hash, file, tmdb int64
	var keeper, video, poster int
	err := s.DB.QueryRowContext(ctx, `SELECT c.telegram_peer_id,COALESCE(c.access_hash,0),j.file_id,d.tmdb_id,k.destination_telegram_message_id,j.video_id,j.poster_id
 FROM tgstrm.movie_duplicate_cleanup j JOIN tgstrm.movie_monitor_deliveries d ON d.id=j.delivery_id
 JOIN tgstrm.movie_monitor_deliveries k ON k.id=j.keeper_id
 JOIN tgstrm.movie_monitors mon ON mon.id=d.monitor_id JOIN tgstrm.chats c ON c.id=mon.destination_chat_id
 JOIN tgstrm.movie_delivery_files f ON f.delivery_id=k.id AND f.telegram_file_id=j.file_id AND f.destination_chat_id=mon.destination_chat_id AND f.destination_topic_id=mon.destination_topic_id
 JOIN tgstrm.messages m ON m.id=k.destination_message_id AND m.is_active
 JOIN tgstrm.message_media duplicate_media ON duplicate_media.message_id=d.destination_message_id AND duplicate_media.telegram_file_id=j.file_id
 WHERE j.delivery_id::text=$1 AND j.status='pending' AND d.destination_telegram_message_id=j.video_id AND d.presentation_telegram_message_id=j.poster_id
 AND c.telegram_peer_type='channel' AND NOT EXISTS(SELECT 1 FROM tgstrm.movie_monitor_deliveries other JOIN tgstrm.movie_monitors other_mon ON other_mon.id=other.monitor_id WHERE other.id<>d.id AND (other.presentation_telegram_message_id=j.poster_id OR other.destination_telegram_message_id=j.video_id) AND other_mon.destination_chat_id=mon.destination_chat_id)`, delivery).Scan(&channel, &hash, &file, &tmdb, &keeper, &video, &poster)
	if err != nil {
		return err
	}
	if keeper == video || keeper == poster {
		return ErrTopicIdentityConflict
	}
	s.apiMu.RLock()
	api := s.runtimeAPI
	s.apiMu.RUnlock()
	if api == nil {
		return errors.New("Telegram se está reconectando")
	}
	found, err := api.ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{Channel: &tg.InputChannel{ChannelID: channel, AccessHash: hash}, ID: []tg.InputMessageClass{&tg.InputMessageID{ID: keeper}, &tg.InputMessageID{ID: video}, &tg.InputMessageID{ID: poster}}})
	if err != nil {
		return err
	}
	actual := map[int]*tg.Message{}
	if data, ok := found.AsModified(); ok {
		for _, item := range data.GetMessages() {
			if m, ok := item.(*tg.Message); ok {
				actual[m.ID] = m
			}
		}
	}
	if len(actual) == 0 {
		return ErrTopicIdentityConflict
	}
	ids, err := validateDuplicateMoviePair(actual, keeper, video, poster, file, tmdb)
	if err != nil {
		return err
	}
	if len(ids) > 0 {
		_, err = api.ChannelsDeleteMessages(ctx, &tg.ChannelsDeleteMessagesRequest{Channel: &tg.InputChannel{ChannelID: channel, AccessHash: hash}, ID: ids})
		if err != nil {
			return err
		}
	}
	return s.removeDeletedVideos(ctx, channel, []int{video, poster})
}
func validateDuplicateMoviePair(actual map[int]*tg.Message, keeper, video, poster int, file, tmdb int64) ([]int, error) {
	if keeper == video || keeper == poster || video == poster {
		return nil, ErrTopicIdentityConflict
	}
	validVideo := func(m *tg.Message) bool {
		if m == nil || !m.Out {
			return false
		}
		v, ok := videoMetadata(m.Media)
		return ok && v.documentID == file
	}
	if !validVideo(actual[keeper]) {
		return nil, ErrTopicIdentityConflict
	}
	var ids []int
	if m := actual[video]; m != nil {
		if !validVideo(m) {
			return nil, ErrTopicIdentityConflict
		}
		ids = append(ids, video)
	}
	if m := actual[poster]; m != nil {
		_, photo := m.Media.(*tg.MessageMediaPhoto)
		expected := fmt.Sprintf("https://www.themoviedb.org/movie/%d", tmdb)
		match := false
		for _, word := range strings.Fields(m.Message) {
			if strings.TrimRight(word, "/") == expected {
				match = true
			}
		}
		if !m.Out || !photo || !match {
			return nil, ErrTopicIdentityConflict
		}
		ids = append(ids, poster)
	}
	return ids, nil
}
