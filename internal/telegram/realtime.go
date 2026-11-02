package telegram

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gotd/td/tg"
)

type RealtimeEvent struct {
	ID          int64           `json:"id"`
	Type        string          `json:"type"`
	AggregateID string          `json:"aggregate_id"`
	Payload     json.RawMessage `json:"payload"`
}

type videoRefresh struct {
	channelID       int64
	topicTelegramID int
}

type watchedTopic struct {
	groupID string
	topicID string
}

type watchedTopicState struct {
	seenAt    time.Time
	checkedAt time.Time
	beforeID  int
}

// WatchTopic marks a topic as actively viewed by a web client. Telegram's
// update documentation recommends periodically recovering the difference for
// channels that are currently open.
func (s *Service) WatchTopic(groupID, topicID string) {
	now := time.Now()
	key := watchedTopic{groupID: groupID, topicID: topicID}
	s.watchedTopicsMu.Lock()
	if s.watchedTopics == nil {
		s.watchedTopics = make(map[watchedTopic]watchedTopicState)
	}
	state := s.watchedTopics[key]
	state.seenAt = now
	s.watchedTopics[key] = state
	s.watchedTopicsMu.Unlock()
}

func (s *Service) reconcileOneWatchedTopic(ctx context.Context) error {
	now := time.Now()
	var selected watchedTopic
	found := false
	s.watchedTopicsMu.Lock()
	for key, state := range s.watchedTopics {
		if now.Sub(state.seenAt) > 15*time.Minute {
			delete(s.watchedTopics, key)
			continue
		}
		if now.Sub(state.checkedAt) < 8*time.Second {
			continue
		}
		if !found || state.checkedAt.Before(s.watchedTopics[selected].checkedAt) {
			selected, found = key, true
		}
	}
	if found {
		state := s.watchedTopics[selected]
		state.checkedAt = now
		s.watchedTopics[selected] = state
	}
	s.watchedTopicsMu.Unlock()
	if !found {
		return nil
	}
	if _, err := s.RefreshTopicMessages(ctx, selected.groupID, selected.topicID, 0); err != nil {
		if errors.Is(err, ErrTopicNotFound) {
			s.watchedTopicsMu.Lock()
			delete(s.watchedTopics, selected)
			s.watchedTopicsMu.Unlock()
			return nil
		}
		return fmt.Errorf("refresh watched messages: %w", err)
	}
	if err := s.reconcileWatchedTopicPresence(ctx, selected); err != nil {
		return fmt.Errorf("verify watched message IDs: %w", err)
	}
	if err := s.queueVideoEvent(ctx, selected.topicID, "videos.snapshot"); err != nil {
		return fmt.Errorf("publish watched snapshot: %w", err)
	}
	return nil
}

// reconcileWatchedTopicPresence verifies IDs already stored locally. Telegram
// returns messageEmpty for deleted channel messages, which makes this check
// authoritative even when updateDeleteChannelMessages was missed.
func (s *Service) reconcileWatchedTopicPresence(ctx context.Context, watched watchedTopic) error {
	var channelID, accessHash int64
	if err := s.DB.QueryRowContext(ctx, `SELECT c.telegram_peer_id,COALESCE(c.access_hash,0)
		FROM tgstrm.topics t JOIN tgstrm.chats c ON c.id=t.chat_id
		WHERE c.id::text=$1 AND t.id::text=$2 AND c.is_active AND t.is_active`, watched.groupID, watched.topicID).
		Scan(&channelID, &accessHash); err != nil {
		return err
	}
	s.watchedTopicsMu.Lock()
	state := s.watchedTopics[watched]
	beforeID := state.beforeID
	s.watchedTopicsMu.Unlock()
	rows, err := s.DB.QueryContext(ctx, `SELECT telegram_message_id FROM tgstrm.messages
		WHERE topic_id=$1::bigint AND is_active AND media_type='video'
		  AND ($2::integer=0 OR telegram_message_id<$2::integer)
		ORDER BY telegram_message_id DESC LIMIT 100`, watched.topicID, beforeID)
	if err != nil {
		return fmt.Errorf("list local video IDs: %w", err)
	}
	var ids []int
	for rows.Next() {
		var id int
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err = rows.Close(); err != nil {
		return err
	}
	s.watchedTopicsMu.Lock()
	state = s.watchedTopics[watched]
	if len(ids) == 100 {
		state.beforeID = ids[len(ids)-1]
	} else {
		state.beforeID = 0
	}
	s.watchedTopics[watched] = state
	s.watchedTopicsMu.Unlock()
	if len(ids) == 0 {
		return nil
	}
	input := make([]tg.InputMessageClass, 0, len(ids))
	for _, id := range ids {
		input = append(input, &tg.InputMessageID{ID: id})
	}
	s.apiMu.RLock()
	api := s.runtimeAPI
	s.apiMu.RUnlock()
	if api == nil {
		return errors.New("telegram is reconnecting")
	}
	result, err := api.ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{
		Channel: &tg.InputChannel{ChannelID: channelID, AccessHash: accessHash}, ID: input,
	})
	if err != nil {
		return fmt.Errorf("channels.getMessages: %w", err)
	}
	alive := make(map[int]struct{}, len(ids))
	if modified, ok := result.AsModified(); ok {
		for _, item := range modified.GetMessages() {
			if message, ok := item.(*tg.Message); ok {
				alive[message.ID] = struct{}{}
			}
		}
	}
	deleted := make([]int, 0)
	for _, id := range ids {
		if _, ok := alive[id]; !ok {
			deleted = append(deleted, id)
		}
	}
	if len(deleted) == 0 {
		return nil
	}
	if err := s.removeDeletedVideos(ctx, channelID, deleted); err != nil {
		return fmt.Errorf("deactivate deleted videos: %w", err)
	}
	return nil
}

func (s *Service) Subscribe(ctx context.Context) <-chan RealtimeEvent {
	s.subMu.Lock()
	if s.subscribers == nil {
		s.subscribers = make(map[uint64]chan RealtimeEvent)
	}
	s.nextSubID++
	id := s.nextSubID
	// A bounded per-client queue isolates slow tabs. Catalog snapshots are small
	// hints that make the client reread PostgreSQL, so keeping a few bursts here
	// is cheaper and safer than dropping a state transition for one subscriber.
	ch := make(chan RealtimeEvent, 256)
	s.subscribers[id] = ch
	s.subMu.Unlock()
	go func() {
		<-ctx.Done()
		s.subMu.Lock()
		if current, ok := s.subscribers[id]; ok {
			delete(s.subscribers, id)
			close(current)
		}
		s.subMu.Unlock()
	}()
	return ch
}

func (s *Service) deliverOutbox(ctx context.Context) error {
	// Snapshot events only tell an already connected browser to reread the
	// canonical PostgreSQL state. A newly opened browser always performs that
	// initial read itself, so retaining snapshots while no browser exists would
	// create an unbounded, stale queue without improving delivery guarantees.
	// Activity and operation events keep their normal outbox behavior.
	s.subMu.Lock()
	hasSubscribers := len(s.subscribers) > 0
	s.subMu.Unlock()
	if !hasSubscribers {
		_, err := s.DB.ExecContext(ctx, `UPDATE tgstrm.outbox_events SET delivered_at=now()
			WHERE delivered_at IS NULL AND event_type IN ('topics.snapshot','videos.snapshot','groups.snapshot')`)
		return err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id,event_type,aggregate_id::text,payload FROM tgstrm.outbox_events
	 WHERE delivered_at IS NULL ORDER BY id LIMIT 100`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var delivered []int64
	for rows.Next() {
		var event RealtimeEvent
		if err = rows.Scan(&event.ID, &event.Type, &event.AggregateID, &event.Payload); err != nil {
			return err
		}
		s.subMu.Lock()
		allSent := true
		for _, subscriber := range s.subscribers {
			select {
			case subscriber <- event:
			default:
				allSent = false
			}
		}
		s.subMu.Unlock()
		// The outbox row remains pending if any active tab could not accept it.
		// A later 300 ms pass retries it after that tab drains its queue.
		if allSent {
			delivered = append(delivered, event.ID)
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for _, id := range delivered {
		if _, err = s.DB.ExecContext(ctx, `UPDATE tgstrm.outbox_events SET delivered_at=now() WHERE id=$1 AND delivered_at IS NULL`, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) queueTopicRefresh(channelID int64) {
	s.runtimeMu.Lock()
	ch := s.refreshTopics
	s.runtimeMu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- channelID:
	default:
	}
}

func (s *Service) queueVideoRefresh(channelID int64, topicTelegramID int) {
	if channelID == 0 {
		return
	}
	refresh := videoRefresh{channelID: channelID, topicTelegramID: topicTelegramID}
	s.videoQueueMu.Lock()
	if s.queuedVideos == nil {
		s.queuedVideos = make(map[videoRefresh]struct{})
	}
	if _, exists := s.queuedVideos[refresh]; exists {
		s.videoQueueMu.Unlock()
		return
	}
	s.queuedVideos[refresh] = struct{}{}
	s.videoQueueMu.Unlock()
	s.runtimeMu.Lock()
	ch := s.refreshVideos
	s.runtimeMu.Unlock()
	if ch == nil {
		s.videoQueueMu.Lock()
		delete(s.queuedVideos, refresh)
		s.videoQueueMu.Unlock()
		return
	}
	select {
	case ch <- refresh:
	default:
		s.videoQueueMu.Lock()
		delete(s.queuedVideos, refresh)
		s.videoQueueMu.Unlock()
	}
}

func (s *Service) queueChannelRecovery(channelID int64) {
	if channelID == 0 {
		return
	}
	s.queueTopicRefresh(channelID)
	s.queueVideoRefresh(channelID, 0)
}

func (s *Service) queueCatalogRecovery() {
	s.runtimeMu.Lock()
	ch := s.recoverCatalog
	s.runtimeMu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
	}
}

func (s *Service) queueInaccessibleChannel(channelID int64) {
	s.runtimeMu.Lock()
	ch := s.inaccessibleChannels
	s.runtimeMu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- channelID:
	default:
	}
}

func topicFromMessage(message *tg.Message) (int, bool) {
	reply, ok := message.GetReplyTo()
	if !ok {
		return 0, false
	}
	header, ok := reply.(*tg.MessageReplyHeader)
	if !ok {
		return 0, false
	}
	if topicID, ok := header.GetReplyToTopID(); ok && topicID != 0 {
		return topicID, true
	}
	topicID, ok := header.GetReplyToMsgID()
	return topicID, ok && topicID != 0
}

func (s *Service) queueVideoEvent(ctx context.Context, topicID string, eventType string) error {
	payload, err := json.Marshal(map[string]any{"topic_id": topicID})
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload)
	 VALUES($1,'topic',$2::bigint,$3)`, eventType, topicID, payload)
	return err
}

func (s *Service) refreshVideosForTopic(ctx context.Context, refresh videoRefresh) error {
	if refresh.topicTelegramID == 0 {
		return s.refreshAllVideosForChannel(ctx, refresh.channelID)
	}
	var groupID, topicID string
	err := s.DB.QueryRowContext(ctx, `SELECT c.id::text,t.id::text FROM tgstrm.chats c
	 JOIN tgstrm.topics t ON t.chat_id=c.id AND t.telegram_topic_id=$3 AND t.is_active
	 JOIN tgstrm.managed_catalog_chats mc ON mc.id=c.id
	 WHERE c.account_id=$1 AND c.telegram_peer_type='channel' AND c.telegram_peer_id=$2 AND c.is_active`,
		accountID, refresh.channelID, refresh.topicTelegramID).Scan(&groupID, &topicID)
	if errors.Is(err, sql.ErrNoRows) {
		if err = s.refreshTopicsForChannel(ctx, refresh.channelID); err != nil {
			return err
		}
		err = s.DB.QueryRowContext(ctx, `SELECT c.id::text,t.id::text FROM tgstrm.chats c
		 JOIN tgstrm.topics t ON t.chat_id=c.id AND t.telegram_topic_id=$3 AND t.is_active
		 WHERE c.account_id=$1 AND c.telegram_peer_type='channel' AND c.telegram_peer_id=$2 AND c.is_active`,
			accountID, refresh.channelID, refresh.topicTelegramID).Scan(&groupID, &topicID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
	}
	if err != nil {
		return err
	}
	if _, err = s.RefreshTopicMessages(ctx, groupID, topicID, 0); err != nil {
		return err
	}
	return s.queueVideoEvent(ctx, topicID, "videos.snapshot")
}

// reconcileOneUnresolvedPrimaryTopic repairs the narrow case where Telegram's
// edit update was missed while the client was reconnecting. MTProto updates are
// still the fast path; this only rereads one unresolved topic at a time.
func (s *Service) reconcileOneUnresolvedPrimaryTopic(ctx context.Context) error {
	var groupID, topicID string
	err := s.DB.QueryRowContext(ctx, `SELECT c.id::text,t.id::text
		FROM tgstrm.stream_assets sa
		JOIN tgstrm.messages m ON m.id=sa.message_id AND m.is_active AND m.media_type='video'
		JOIN tgstrm.topics t ON t.id=m.topic_id AND t.is_active
		JOIN tgstrm.chats c ON c.id=t.chat_id AND c.is_active
		JOIN tgstrm.managed_catalog_chats mc ON mc.id=c.id
		WHERE sa.last_export_error IS NOT NULL
		  AND (sa.processed_at IS NULL OR sa.processed_at < now()-interval '2 minutes')
		ORDER BY sa.processed_at NULLS FIRST LIMIT 1`).Scan(&groupID, &topicID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err = s.RefreshTopicMessages(ctx, groupID, topicID, 0); err != nil {
		return err
	}
	// Back off genuinely unresolved files. If the caption changed, its source
	// signature differs and the export reconciler will pick it up immediately.
	_, _ = s.DB.ExecContext(ctx, `UPDATE tgstrm.stream_assets sa SET processed_at=now()
		FROM tgstrm.messages m WHERE m.id=sa.message_id AND m.topic_id=$1
		AND sa.last_export_error IS NOT NULL
		AND sa.processed_signature IS NOT DISTINCT FROM sa.source_signature`, topicID)
	return s.queueVideoEvent(ctx, topicID, "videos.snapshot")
}

func (s *Service) refreshAllVideosForChannel(ctx context.Context, channelID int64) error {
	var groupID string
	err := s.DB.QueryRowContext(ctx, `SELECT id::text FROM tgstrm.chats WHERE account_id=$1 AND telegram_peer_type='channel'
	 AND telegram_peer_id=$2 AND is_forum AND is_active`, accountID, channelID).Scan(&groupID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = s.RefreshTopics(ctx, groupID); err != nil {
		return err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id::text FROM tgstrm.topics WHERE chat_id=$1 AND is_active ORDER BY id`, groupID)
	if err != nil {
		return err
	}
	var topicIDs []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		topicIDs = append(topicIDs, id)
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, topicID := range topicIDs {
		if _, err = s.RefreshTopicMessages(ctx, groupID, topicID, 0); err != nil {
			return err
		}
		if err = s.queueVideoEvent(ctx, topicID, "videos.snapshot"); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) recoverCatalogState(ctx context.Context, api *tg.Client) error {
	if err := s.syncDialogs(ctx, api); err != nil {
		return err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT telegram_peer_id FROM tgstrm.chats WHERE account_id=$1
	 AND telegram_peer_type='channel' AND is_forum AND is_active ORDER BY id`, accountID)
	if err != nil {
		return err
	}
	var channels []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		channels = append(channels, id)
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, channelID := range channels {
		if err = s.refreshAllVideosForChannel(ctx, channelID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) markChannelInaccessible(ctx context.Context, channelID int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var groupID int64
	err = tx.QueryRowContext(ctx, `UPDATE tgstrm.chats SET is_active=false,updated_at=now() WHERE account_id=$1
	 AND telegram_peer_type='channel' AND telegram_peer_id=$2 AND is_active RETURNING id`, accountID, channelID).Scan(&groupID)
	if errors.Is(err, sql.ErrNoRows) {
		return tx.Commit()
	}
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload)
	 VALUES('groups.snapshot','group',$1::bigint,jsonb_build_object('group_id',$1::bigint::text,'accessible',false))`, groupID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) removeDeletedVideos(ctx context.Context, channelID int64, messageIDs []int) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `UPDATE tgstrm.messages m SET is_active=false,updated_at=now()
	 FROM tgstrm.chats c WHERE m.chat_id=c.id AND c.account_id=$1 AND c.telegram_peer_type='channel'
	 AND c.telegram_peer_id=$2 AND m.telegram_message_id=ANY($3) AND m.is_active RETURNING m.topic_id::text`,
		accountID, channelID, messageIDs)
	if err != nil {
		return err
	}
	topics := map[string]struct{}{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return err
		}
		topics[id] = struct{}{}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for id := range topics {
		_, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload)
		 VALUES('videos.snapshot','topic',$1::bigint,jsonb_build_object('topic_id',$1::bigint::text))`, id)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// removeDeletedTopics recognizes the topic-create service message in the
// ordinary channel deletion update. Telegram defines the topic ID as that root
// message ID, so no additional RPC is needed for the realtime path.
func (s *Service) removeDeletedTopics(ctx context.Context, channelID int64, messageIDs []int) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `UPDATE tgstrm.topics t SET is_active=false,updated_at=now()
		FROM tgstrm.chats c WHERE t.chat_id=c.id AND c.account_id=$1
		AND c.telegram_peer_type='channel' AND c.telegram_peer_id=$2
		AND t.telegram_topic_id=ANY($3) AND t.is_active RETURNING t.id::text,t.chat_id::text,t.title`, accountID, channelID, messageIDs)
	if err != nil {
		return err
	}
	groups := map[string]struct{}{}
	type deletedTopic struct{ id, groupID, title string }
	var deleted []deletedTopic
	for rows.Next() {
		var topicID, groupID, title string
		if err = rows.Scan(&topicID, &groupID, &title); err != nil {
			rows.Close()
			return err
		}
		groups[groupID] = struct{}{}
		deleted = append(deleted, deletedTopic{id: topicID, groupID: groupID, title: title})
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, topic := range deleted {
		var exportCount int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM tgstrm.stream_exports e
			JOIN tgstrm.stream_assets sa ON sa.id=e.asset_id JOIN tgstrm.messages m ON m.id=sa.message_id WHERE m.topic_id=$1`, topic.id).Scan(&exportCount); err != nil {
			return err
		}
		detail := "Topic eliminado de Telegram"
		if exportCount > 0 {
			detail = fmt.Sprintf("Topic eliminado de Telegram · %d STRM asociados se retirarán de la biblioteca", exportCount)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.activity_events(kind,status,title,detail,group_id,topic_id,item_count)
			VALUES('topic_deleted','completed',$1,$2,$3::bigint,$4::bigint,$5)`, topic.title, detail, topic.groupID, topic.id, exportCount); err != nil {
			return err
		}
	}
	for groupID := range groups {
		if _, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload)
			VALUES('topics.snapshot','group',$1::bigint,jsonb_build_object('group_id',$1::bigint::text,'reason','topic_deleted'))`, groupID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload)
			VALUES('activity.changed','group',$1::bigint,jsonb_build_object('group_id',$1::bigint::text,'reason','topic_deleted'))`, groupID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// applyRealtimeVideo persists a complete Telegram update without another RPC.
// It returns false only when the local catalog cannot yet resolve its topic.
func (s *Service) applyRealtimeVideo(ctx context.Context, channelID int64, topicTelegramID int, message *tg.Message, edited bool) (bool, error) {
	var groupID, topicID string
	err := s.DB.QueryRowContext(ctx, `SELECT c.id::text,t.id::text FROM tgstrm.chats c
	 JOIN tgstrm.topics t ON t.chat_id=c.id AND t.telegram_topic_id=$3 AND t.is_active
	 WHERE c.account_id=$1 AND c.telegram_peer_type='channel' AND c.telegram_peer_id=$2 AND c.is_active`,
		accountID, channelID, topicTelegramID).Scan(&groupID, &topicID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	media, hasMedia := message.GetMedia()
	if !hasMedia || !isVideoMedia(media) {
		if !edited {
			return true, tx.Commit()
		}
		result, err := tx.ExecContext(ctx, `UPDATE tgstrm.messages SET is_active=false,updated_at=now()
		 WHERE chat_id=$1 AND telegram_message_id=$2 AND is_active`, groupID, message.ID)
		if err != nil {
			return false, err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return false, err
		}
		if changed == 0 {
			return true, tx.Commit()
		}
	} else {
		if _, err = storeVideoMessage(ctx, tx, groupID, topicID, message); err != nil {
			return false, err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload)
	 VALUES('videos.snapshot','topic',$1::bigint,jsonb_build_object('topic_id',$1::bigint::text))`, topicID)
	if err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func channelFromPeer(peer tg.PeerClass) (int64, bool) {
	value, ok := peer.(*tg.PeerChannel)
	if !ok {
		return 0, false
	}
	return value.ChannelID, true
}

func (s *Service) updateDispatcher() tg.UpdateDispatcher {
	d := tg.NewUpdateDispatcher()
	queueMessage := func(ctx context.Context, message tg.MessageClass, edited bool) error {
		service, ok := message.(*tg.MessageService)
		if ok {
			if channelID, ok := channelFromPeer(service.PeerID); ok {
				s.queueTopicRefresh(channelID)
			}
			return nil
		}
		value, ok := message.(*tg.Message)
		if !ok {
			return nil
		}
		channelID, ok := channelFromPeer(value.PeerID)
		if !ok {
			return nil
		}
		topicID, ok := topicFromMessage(value)
		// Plain groups have one local scope. Replies to ordinary messages must
		// not be mistaken for forum topics. Only explicitly catalogued groups
		// are considered; unrelated Telegram conversations remain untouched.
		var wholeGroup bool
		if err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tgstrm.chats c JOIN tgstrm.topics t ON t.chat_id=c.id AND t.telegram_topic_id=0 AND t.is_active WHERE c.account_id=$1 AND c.telegram_peer_id=$2 AND c.telegram_peer_type='channel' AND c.is_active AND NOT c.is_forum)`, accountID, channelID).Scan(&wholeGroup); err != nil {
			return err
		}
		if wholeGroup {
			topicID, ok = 0, true
		}
		if !ok {
			return nil
		}
		if err := s.invalidateManifestMessage(ctx, channelID, topicID, value, edited); err != nil {
			return err
		}
		media, hasMedia := value.GetMedia()
		isVideo := hasMedia && isVideoMedia(media)
		handled, err := s.applyRealtimeVideo(ctx, channelID, topicID, value, edited)
		if err != nil {
			return err
		}
		if !handled && (isVideo || edited) {
			s.queueTopicRefresh(channelID)
			s.queueVideoRefresh(channelID, topicID)
		}
		if handled && isVideo && !edited {
			if err := s.QueueMirrorDelivery(ctx, channelID, topicID, value.ID); err != nil {
				return err
			}
		}
		if handled && isVideo && edited {
			if err := s.QueueMirrorEdit(ctx, channelID, topicID, value.ID, value.Message); err != nil {
				return err
			}
		}
		return nil
	}
	d.OnNewChannelMessage(func(ctx context.Context, _ tg.Entities, u *tg.UpdateNewChannelMessage) error {
		return queueMessage(ctx, u.Message, false)
	})
	d.OnEditChannelMessage(func(ctx context.Context, _ tg.Entities, u *tg.UpdateEditChannelMessage) error {
		return queueMessage(ctx, u.Message, true)
	})
	d.OnDeleteChannelMessages(func(ctx context.Context, _ tg.Entities, u *tg.UpdateDeleteChannelMessages) error {
		if err := s.invalidateDeletedMessageManifests(ctx, u.ChannelID, u.Messages); err != nil {
			return err
		}
		if err := s.removeDeletedTopics(ctx, u.ChannelID, u.Messages); err != nil {
			return err
		}
		if err := s.removeDeletedVideos(ctx, u.ChannelID, u.Messages); err != nil {
			return err
		}
		s.queueTopicRefresh(u.ChannelID)
		return nil
	})
	d.OnPinnedForumTopic(func(_ context.Context, _ tg.Entities, u *tg.UpdatePinnedForumTopic) error {
		if id, ok := channelFromPeer(u.Peer); ok {
			s.queueTopicRefresh(id)
		}
		return nil
	})
	d.OnPinnedForumTopics(func(_ context.Context, _ tg.Entities, u *tg.UpdatePinnedForumTopics) error {
		if id, ok := channelFromPeer(u.Peer); ok {
			s.queueTopicRefresh(id)
		}
		return nil
	})
	return d
}

func (s *Service) refreshTopicsForChannel(ctx context.Context, channelID int64) error {
	var groupID string
	err := s.DB.QueryRowContext(ctx, `SELECT c.id::text FROM tgstrm.chats c
	 WHERE (EXISTS(SELECT 1 FROM tgstrm.managed_catalog_chats mc WHERE mc.id=c.id) OR EXISTS(SELECT 1 FROM tgstrm.group_replications gr WHERE gr.source_chat_id=c.id AND gr.discover_new_topics AND gr.status IN ('running','completed'))) AND c.account_id=$1 AND c.telegram_peer_type='channel' AND c.telegram_peer_id=$2 AND c.is_forum AND c.is_active`, accountID, channelID).Scan(&groupID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = s.RefreshTopics(ctx, groupID); err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `UPDATE tgstrm.group_replications SET last_discovered_at=NULL WHERE source_chat_id::text=$1 AND discover_new_topics AND status IN ('running','completed')`, groupID)
	return err
}
