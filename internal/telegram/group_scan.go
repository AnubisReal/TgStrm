package telegram

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gotd/td/tgerr"
)

type GroupVideoScanStatus struct {
	Status          string `json:"status"`
	TotalTopics     int    `json:"total_topics"`
	CompletedTopics int    `json:"completed_topics"`
	VideoCount      int    `json:"video_count"`
	TotalBytes      int64  `json:"total_bytes"`
	FailedTopics    int    `json:"failed_topics"`
	LastError       string `json:"last_error,omitempty"`
}

func (s *Service) GroupVideoScanStatus(ctx context.Context, groupID string) (GroupVideoScanStatus, error) {
	var status GroupVideoScanStatus
	var cursor []byte
	var lastError sql.NullString
	err := s.DB.QueryRowContext(ctx, `SELECT status,cursor,last_error FROM tgstrm.sync_states
		WHERE account_id=$1 AND scope='group_videos' AND resource_id=$2`, accountID, groupID).
		Scan(&status.Status, &cursor, &lastError)
	if err == sql.ErrNoRows {
		status.Status = "idle"
		return status, nil
	}
	if err != nil {
		return status, err
	}
	_ = json.Unmarshal(cursor, &status)
	if status.Status == "" {
		status.Status = "ready"
	}
	status.LastError = lastError.String
	_ = s.DB.QueryRowContext(ctx, `SELECT count(*),COALESCE(sum(mm.byte_size),0)
		FROM tgstrm.messages m JOIN tgstrm.topics t ON t.id=m.topic_id
		JOIN tgstrm.message_media mm ON mm.message_id=m.id
		WHERE t.chat_id=$1 AND t.is_active AND m.is_active AND m.media_type='video'`, groupID).
		Scan(&status.VideoCount, &status.TotalBytes)
	return status, nil
}

func (s *Service) ScanGroupVideos(ctx context.Context, groupID string) error {
	var knownTopics int
	_ = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM tgstrm.topics WHERE chat_id=$1 AND is_active`, groupID).Scan(&knownTopics)
	status := GroupVideoScanStatus{Status: "running", TotalTopics: knownTopics}
	if err := s.saveGroupVideoScanStatus(ctx, groupID, status, ""); err != nil {
		return err
	}
	refreshCtx, cancelRefresh := context.WithTimeout(ctx, 30*time.Second)
	refreshErr := s.RefreshTopics(refreshCtx, groupID)
	cancelRefresh()
	lastErr := ""
	if refreshErr != nil {
		lastErr = refreshErr.Error()
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT t.id::text FROM tgstrm.topics t
		LEFT JOIN tgstrm.messages m ON m.topic_id=t.id AND m.is_active AND m.media_type='video'
		WHERE t.chat_id=$1 AND t.is_active GROUP BY t.id,t.telegram_topic_id
		ORDER BY count(m.id),t.telegram_topic_id`, groupID)
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
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}

	status.TotalTopics = len(topicIDs)
	if err = s.saveGroupVideoScanStatus(ctx, groupID, status, lastErr); err != nil {
		return err
	}
	for _, topicID := range topicIDs {
		topicFailed := false
		for _, refresh := range []func(context.Context, string, string, int) (TopicRemotePage, error){s.RefreshTopicVideos, s.RefreshTopicVideoDocuments} {
			if scanErr := scanTopicPages(ctx, groupID, topicID, refresh); scanErr != nil {
				topicFailed = true
				lastErr = scanErr.Error()
			}
		}
		if topicFailed {
			status.FailedTopics++
		}
		status.CompletedTopics++
		_ = s.DB.QueryRowContext(ctx, `SELECT count(*),COALESCE(sum(mm.byte_size),0) FROM tgstrm.messages m
			JOIN tgstrm.topics t ON t.id=m.topic_id JOIN tgstrm.message_media mm ON mm.message_id=m.id
			WHERE t.chat_id=$1 AND t.is_active AND m.is_active AND m.media_type='video'`, groupID).
			Scan(&status.VideoCount, &status.TotalBytes)
		_ = s.saveGroupVideoScanStatus(ctx, groupID, status, lastErr)
		_, _ = s.DB.ExecContext(ctx, `INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload)
			VALUES('topics.snapshot','group',$1::bigint,jsonb_build_object('group_id',$1::bigint::text,'scan',true))`, groupID)
	}
	status.Status = "ready"
	if status.FailedTopics > 0 {
		status.Status = "stale"
	}
	return s.saveGroupVideoScanStatus(ctx, groupID, status, lastErr)
}

func scanTopicPages(ctx context.Context, groupID, topicID string, refresh func(context.Context, string, string, int) (TopicRemotePage, error)) error {
	before := 0
	seen := map[int]bool{}
	for {
		var page TopicRemotePage
		var pageErr error
		for attempt := 0; attempt < 4; attempt++ {
			pageCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
			page, pageErr = refresh(pageCtx, groupID, topicID, before)
			cancel()
			wait, flood := tgerr.AsFloodWait(pageErr)
			if !flood {
				break
			}
			if wait < time.Second {
				wait = time.Second
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait + time.Second):
			}
		}
		if pageErr != nil {
			return pageErr
		}
		if !page.HasMore || page.NextOffset <= 0 || seen[page.NextOffset] {
			return nil
		}
		seen[page.NextOffset] = true
		before = page.NextOffset
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
	}
}

func (s *Service) saveGroupVideoScanStatus(ctx context.Context, groupID string, status GroupVideoScanStatus, lastError string) error {
	raw, err := json.Marshal(status)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO tgstrm.sync_states(account_id,scope,resource_id,status,cursor,last_error,started_at,completed_at)
		VALUES($1,'group_videos',$2,$3,$4,NULLIF($5,''),CASE WHEN $3='running' THEN now() END,CASE WHEN $3 IN ('ready','stale') THEN now() END)
		ON CONFLICT(account_id,scope,resource_id) DO UPDATE SET status=EXCLUDED.status,cursor=EXCLUDED.cursor,last_error=EXCLUDED.last_error,
		started_at=CASE WHEN EXCLUDED.status='running' AND tgstrm.sync_states.status<>'running' THEN now() ELSE tgstrm.sync_states.started_at END,
		completed_at=CASE WHEN EXCLUDED.status IN ('ready','stale') THEN now() ELSE tgstrm.sync_states.completed_at END,updated_at=now()`,
		accountID, groupID, status.Status, raw, fmt.Sprintf("%.500s", lastError))
	return err
}
