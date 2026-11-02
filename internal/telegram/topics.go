package telegram

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gotd/td/tg"
)

var ErrGroupNotFound = errors.New("group not found")

type Topic struct {
	ID                  string     `json:"id"`
	TelegramID          int        `json:"telegram_id"`
	Title               string     `json:"title"`
	IconColor           int        `json:"icon_color,omitempty"`
	IconEmojiID         string     `json:"icon_emoji_id,omitempty"`
	Closed              bool       `json:"closed"`
	Hidden              bool       `json:"hidden"`
	Pinned              bool       `json:"pinned"`
	UnreadCount         int        `json:"unread_count"`
	UnreadMentionsCount int        `json:"unread_mentions_count"`
	TopMessageID        int        `json:"top_message_id"`
	CreatedAt           *time.Time `json:"created_at,omitempty"`
	LastMessageAt       *time.Time `json:"last_message_at,omitempty"`
	VideoCount          int        `json:"video_count"`
	TotalBytes          int64      `json:"total_bytes"`
	ExportedCount       int        `json:"exported_count"`
	UnresolvedCount     int        `json:"unresolved_count"`
	LibraryKind         string     `json:"library_kind"`
}

type GroupDetail struct {
	ID         string `json:"id"`
	TelegramID string `json:"telegram_id"`
	Title      string `json:"title"`
	Username   string `json:"username,omitempty"`
	Type       string `json:"type"`
	Forum      bool   `json:"forum"`
}

type remoteTopic struct {
	id, iconColor, topMessage, unread, mentions int
	iconEmojiID                                 int64
	title                                       string
	closed, hidden, pinned                      bool
	createdAt, lastMessageAt                    time.Time
}

func (s *Service) Group(ctx context.Context, id string) (GroupDetail, error) {
	var group GroupDetail
	err := s.DB.QueryRowContext(ctx, `SELECT id::text,telegram_peer_id::text,title,COALESCE(username,''),telegram_peer_type,is_forum
	 FROM tgstrm.chats WHERE id::text=$1 AND account_id=$2 AND is_active`, id, accountID).
		Scan(&group.ID, &group.TelegramID, &group.Title, &group.Username, &group.Type, &group.Forum)
	if errors.Is(err, sql.ErrNoRows) {
		return GroupDetail{}, ErrGroupNotFound
	}
	return group, err
}

func (s *Service) RefreshTopics(ctx context.Context, groupID string) error {
	var peerType string
	var peerID, accessHash int64
	var forum bool
	err := s.DB.QueryRowContext(ctx, `SELECT telegram_peer_type,telegram_peer_id,COALESCE(access_hash,0),is_forum
	 FROM tgstrm.chats WHERE id::text=$1 AND account_id=$2 AND is_active`, groupID, accountID).
		Scan(&peerType, &peerID, &accessHash, &forum)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrGroupNotFound
	}
	if err != nil || !forum {
		return err
	}
	if peerType != "channel" || accessHash == 0 {
		return errors.New("forum peer unavailable")
	}

	s.apiMu.RLock()
	api := s.runtimeAPI
	s.apiMu.RUnlock()
	if api == nil {
		return errors.New("telegram is reconnecting")
	}

	_, _ = s.DB.ExecContext(ctx, `INSERT INTO tgstrm.sync_states(account_id,scope,resource_id,status,started_at,last_error)
	 VALUES($1,'topics',$2,'running',now(),NULL) ON CONFLICT(account_id,scope,resource_id) DO UPDATE SET
	 status='running',started_at=now(),last_error=NULL,updated_at=now()`, accountID, groupID)

	peer := &tg.InputPeerChannel{ChannelID: peerID, AccessHash: accessHash}
	request := &tg.MessagesGetForumTopicsRequest{Peer: peer, Limit: 100}
	collected := make(map[int]remoteTopic)
	for page := 0; page < 100; page++ {
		result, callErr := api.MessagesGetForumTopics(ctx, request)
		if callErr != nil {
			s.recordTopicSyncFailure(ctx, groupID, callErr)
			return callErr
		}
		messageDates := make(map[int]time.Time, len(result.Messages))
		for _, message := range result.Messages {
			if value, ok := message.AsNotEmpty(); ok {
				messageDates[value.GetID()] = time.Unix(int64(value.GetDate()), 0).UTC()
			}
		}
		if len(result.Topics) == 0 {
			break
		}
		lastID := 0
		for _, item := range result.Topics {
			lastID = item.GetID()
			topic, ok := item.(*tg.ForumTopic)
			if !ok {
				continue
			}
			value := remoteTopic{id: topic.ID, title: topic.Title, iconColor: topic.IconColor,
				topMessage: topic.TopMessage, unread: topic.UnreadCount, mentions: topic.UnreadMentionsCount,
				closed: topic.Closed, hidden: topic.Hidden, pinned: topic.Pinned,
				createdAt: time.Unix(int64(topic.Date), 0).UTC(), lastMessageAt: messageDates[topic.TopMessage]}
			if emoji, ok := topic.GetIconEmojiID(); ok {
				value.iconEmojiID = emoji
			}
			collected[topic.ID] = value
		}
		if len(collected) >= result.Count || lastID == 0 {
			break
		}
		last, ok := collected[lastID]
		if !ok {
			break
		}
		request.OffsetTopic = last.id
		request.OffsetID = last.topMessage
		if !last.lastMessageAt.IsZero() {
			request.OffsetDate = int(last.lastMessageAt.Unix())
		} else {
			request.OffsetDate = int(last.createdAt.Unix())
		}
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "UPDATE tgstrm.topics SET is_active=false,updated_at=now() WHERE chat_id=$1", groupID); err != nil {
		return err
	}
	for _, topic := range collected {
		_, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.topics(chat_id,telegram_topic_id,title,icon_color,icon_emoji_id,
		 is_closed,is_hidden,is_pinned,unread_count,unread_mentions_count,top_message_id,telegram_created_at,last_message_at,is_active,
		 library_kind,classification_source,classified_at)
		 VALUES($1::bigint,$2,$3,$4,NULLIF($5::bigint,0),$6,$7,$8,$9,$10,$11,$12,NULLIF($13,'0001-01-01 00:00:00+00'::timestamptz),true,
		 CASE WHEN EXISTS(SELECT 1 FROM tgstrm.managed_catalog_chats mc WHERE mc.id=$1::bigint)
		   THEN CASE WHEN lower(trim($3)) IN ('peliculas','películas','movies','films') THEN 'movie_collection'
		             WHEN lower(trim($3))='general' THEN 'ignored' ELSE 'series' END
		   ELSE 'unclassified' END,
		 'automatic',CASE WHEN EXISTS(SELECT 1 FROM tgstrm.managed_catalog_chats mc WHERE mc.id=$1::bigint) THEN now() END)
		 ON CONFLICT(chat_id,telegram_topic_id) DO UPDATE SET title=EXCLUDED.title,icon_color=EXCLUDED.icon_color,
		 icon_emoji_id=EXCLUDED.icon_emoji_id,is_closed=EXCLUDED.is_closed,is_hidden=EXCLUDED.is_hidden,
		 is_pinned=EXCLUDED.is_pinned,unread_count=EXCLUDED.unread_count,unread_mentions_count=EXCLUDED.unread_mentions_count,
		 top_message_id=EXCLUDED.top_message_id,telegram_created_at=EXCLUDED.telegram_created_at,
		 last_message_at=EXCLUDED.last_message_at,is_active=true,last_seen_at=now(),updated_at=now(),
		 library_kind=CASE WHEN tgstrm.topics.classification_source='manual' THEN tgstrm.topics.library_kind ELSE EXCLUDED.library_kind END,
		 classified_at=CASE WHEN tgstrm.topics.classification_source='manual' THEN tgstrm.topics.classified_at ELSE EXCLUDED.classified_at END`,
			groupID, topic.id, topic.title, topic.iconColor, topic.iconEmojiID, topic.closed, topic.hidden,
			topic.pinned, topic.unread, topic.mentions, topic.topMessage, topic.createdAt, topic.lastMessageAt)
		if err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.sync_states(account_id,scope,resource_id,status,completed_at,last_error)
	 VALUES($1,'topics',$2,'ready',now(),NULL) ON CONFLICT(account_id,scope,resource_id) DO UPDATE SET
	 status='ready',completed_at=now(),last_error=NULL,updated_at=now()`, accountID, groupID)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]any{"group_id": groupID, "topic_count": len(collected)})
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload)
	 VALUES('topics.snapshot','group',$1,$2)`, groupID, payload); err != nil {
		return err
	}
	return tx.Commit()
}

// RefreshSearchTopics refreshes forum topic metadata before a global search
// when a topic has not yet been opened in the web UI. It does not download
// messages or media; message content remains lazy and is loaded only after a
// result is selected.
func (s *Service) RefreshSearchTopics(ctx context.Context) error {
	rows, err := s.DB.QueryContext(ctx, `SELECT id::text FROM tgstrm.chats WHERE account_id=$1 AND is_active AND is_forum ORDER BY id`, accountID)
	if err != nil {
		return err
	}
	var groups []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			groups = append(groups, id)
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	for _, groupID := range groups {
		groupID := groupID
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			if err := s.RefreshTopics(ctx, groupID); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return firstErr
}

// RefreshSearchMedia hydrates the video/document metadata for the small set of
// forum topics that can explain a search term. Topic metadata is refreshed
// separately by RefreshSearchTopics; once it is present, matching a topic or
// group title gives us a safe, bounded scope in which to read captions and
// filenames. This keeps the global search responsive without scanning every
// message in every group for each keystroke.
func (s *Service) RefreshSearchMedia(ctx context.Context, query string, maxTopics int) error {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	if maxTopics < 1 {
		maxTopics = 8
	}
	if maxTopics > 24 {
		maxTopics = 24
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT t.id::text,c.id::text
		FROM tgstrm.topics t JOIN tgstrm.chats c ON c.id=t.chat_id
		WHERE t.is_active AND c.is_active AND c.account_id=$1
		  AND lower(t.title) LIKE '%'||lower($2)||'%'
		ORDER BY CASE WHEN lower(t.title)=lower($2) THEN 0 ELSE 1 END,t.id
		LIMIT $3`, accountID, query, maxTopics)
	if err != nil {
		return err
	}
	type target struct{ topicID, groupID string }
	var targets []target
	for rows.Next() {
		var item target
		if err := rows.Scan(&item.topicID, &item.groupID); err != nil {
			rows.Close()
			return err
		}
		targets = append(targets, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if len(targets) == 0 {
		return nil
	}
	sem := make(chan struct{}, 2)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	for _, item := range targets {
		item := item
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			if err := s.SyncTopicVideoCatalog(ctx, item.groupID, item.topicID); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return firstErr
}

func (s *Service) recordTopicSyncFailure(ctx context.Context, groupID string, syncErr error) {
	_, _ = s.DB.ExecContext(ctx, `INSERT INTO tgstrm.sync_states(account_id,scope,resource_id,status,last_error)
	 VALUES($1,'topics',$2,'failed',$3) ON CONFLICT(account_id,scope,resource_id) DO UPDATE SET
	 status='failed',last_error=EXCLUDED.last_error,updated_at=now()`, accountID, groupID, fmt.Sprintf("%.500s", syncErr.Error()))
}

func (s *Service) Topics(ctx context.Context, groupID string) ([]Topic, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT t.id::text,t.telegram_topic_id,t.title,COALESCE(t.icon_color,0),COALESCE(t.icon_emoji_id,0)::text,
	 t.is_closed,t.is_hidden,t.is_pinned,t.unread_count,t.unread_mentions_count,COALESCE(t.top_message_id,0),t.telegram_created_at,t.last_message_at,
	 COALESCE(stats.video_count,0),COALESCE(stats.total_bytes,0),COALESCE(exports.exported_count,0),COALESCE(stats.unresolved_count,0),t.library_kind
	 FROM tgstrm.topics t
	 LEFT JOIN (
	   SELECT m.topic_id,
	     count(DISTINCT m.id) FILTER (WHERE m.is_active AND m.media_type='video') AS video_count,
	     COALESCE(sum(mm.byte_size) FILTER (WHERE m.is_active AND m.media_type='video'),0) AS total_bytes,
	     count(DISTINCT sa.id) FILTER (WHERE m.is_active AND m.media_type='video' AND sa.last_export_error IS NOT NULL) AS unresolved_count
	   FROM tgstrm.messages m
	   LEFT JOIN tgstrm.message_media mm ON mm.message_id=m.id
	   LEFT JOIN tgstrm.stream_assets sa ON sa.message_id=m.id
	   GROUP BY m.topic_id
	 ) stats ON stats.topic_id=t.id
	 LEFT JOIN (
	   SELECT m.topic_id,count(DISTINCT sa.id) AS exported_count
	   FROM tgstrm.messages m JOIN tgstrm.stream_assets sa ON sa.message_id=m.id
	   JOIN tgstrm.stream_exports e ON e.asset_id=sa.id AND e.status='written'
	   WHERE m.is_active GROUP BY m.topic_id
	 ) exports ON exports.topic_id=t.id
	 WHERE t.chat_id=$1 AND t.is_active ORDER BY t.is_pinned DESC,t.top_message_id DESC,t.telegram_topic_id DESC`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Topic{}
	for rows.Next() {
		var topic Topic
		if err = rows.Scan(&topic.ID, &topic.TelegramID, &topic.Title, &topic.IconColor, &topic.IconEmojiID,
			&topic.Closed, &topic.Hidden, &topic.Pinned, &topic.UnreadCount, &topic.UnreadMentionsCount,
			&topic.TopMessageID, &topic.CreatedAt, &topic.LastMessageAt, &topic.VideoCount, &topic.TotalBytes, &topic.ExportedCount, &topic.UnresolvedCount, &topic.LibraryKind); err != nil {
			return nil, err
		}
		result = append(result, topic)
	}
	return result, rows.Err()
}
