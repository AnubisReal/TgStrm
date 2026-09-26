package telegram

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

var mirrorWorkerMu sync.Mutex

// RecoverMirrorSources closes the only gap that realtime updates cannot cover:
// new messages written while this process was offline. It reads forward only
// to the last locally known delivery and deliberately never treats an absent
// source message as a deletion, because mirrors preserve destination copies.
func (s *Service) RecoverMirrorSources(ctx context.Context) error {
	rows, err := s.DB.QueryContext(ctx, `SELECT sc.id::text,st.id::text,
		GREATEST(ml.start_after_message_id,COALESCE(max(source.telegram_message_id),0))
		FROM tgstrm.mirror_links ml
		JOIN tgstrm.topics st ON st.id=ml.source_topic_id AND st.is_active
		JOIN tgstrm.chats sc ON sc.id=st.chat_id AND sc.is_active
		LEFT JOIN tgstrm.mirror_deliveries md ON md.link_id=ml.id
		LEFT JOIN tgstrm.messages source ON source.id=md.source_message_id
		WHERE ml.status='active' GROUP BY ml.id,sc.id,st.id ORDER BY ml.created_at`)
	if err != nil {
		return err
	}
	type source struct {
		groupID, topicID string
		boundary         int
	}
	var sources []source
	for rows.Next() {
		var item source
		if err = rows.Scan(&item.groupID, &item.topicID, &item.boundary); err != nil {
			rows.Close()
			return err
		}
		sources = append(sources, item)
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, item := range sources {
		before := 0
		seen := make(map[int]struct{})
		for {
			page, pageErr := s.RefreshTopicMessages(ctx, item.groupID, item.topicID, before)
			if pageErr != nil {
				return pageErr
			}
			if !page.HasMore || page.NextOffset <= 0 || page.NextOffset <= item.boundary {
				break
			}
			if _, duplicate := seen[page.NextOffset]; duplicate {
				break
			}
			seen[page.NextOffset] = struct{}{}
			before = page.NextOffset
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(150 * time.Millisecond):
			}
		}
	}
	return nil
}

// QueueMirrorDelivery records work before it is attempted. The unique key on
// link and source message makes duplicate Telegram updates harmless.
func (s *Service) QueueMirrorDelivery(ctx context.Context, channelID int64, topicTelegramID, messageTelegramID int) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO tgstrm.mirror_deliveries(link_id,source_message_id,random_id)
		SELECT ml.id,m.id,mod(abs(hashtextextended(ml.id::text || ':' || m.id::text,0)),9223372036854775806)+1
		FROM tgstrm.mirror_links ml JOIN tgstrm.topics t ON t.id=ml.source_topic_id
		JOIN tgstrm.chats c ON c.id=t.chat_id JOIN tgstrm.messages m ON m.topic_id=t.id
		WHERE ml.status='active' AND c.telegram_peer_id=$1 AND t.telegram_topic_id=$2
		AND m.telegram_message_id=$3 AND m.media_type='video' AND m.is_active
		AND m.telegram_message_id>ml.start_after_message_id
		ON CONFLICT(link_id,source_message_id) DO NOTHING`, channelID, topicTelegramID, messageTelegramID)
	if err == nil {
		s.wakeMirrorWorker()
	}
	return err
}

// QueueMirrorEdit keeps only the newest requested caption for a delivery. If
// the delivery predates destination ID tracking, the shared Telegram document
// ID provides a conservative recovery path to the copied message.
func (s *Service) QueueMirrorEdit(ctx context.Context, channelID int64, topicTelegramID, messageTelegramID int, caption string) error {
	// An edit can overtake the delivery worker (or even be the first update we
	// observe after reconnecting). Ensure the durable delivery row exists before
	// attaching the desired caption so the edit can never be dropped.
	_, err := s.DB.ExecContext(ctx, `INSERT INTO tgstrm.mirror_deliveries(link_id,source_message_id,random_id)
		SELECT ml.id,m.id,mod(abs(hashtextextended(ml.id::text || ':' || m.id::text,0)),9223372036854775806)+1
		FROM tgstrm.mirror_links ml JOIN tgstrm.topics t ON t.id=ml.source_topic_id
		JOIN tgstrm.chats c ON c.id=t.chat_id JOIN tgstrm.messages m ON m.topic_id=t.id
		WHERE ml.status='active' AND c.telegram_peer_id=$1 AND t.telegram_topic_id=$2
		AND m.telegram_message_id=$3 AND m.media_type='video' AND m.is_active
		AND m.telegram_message_id>ml.start_after_message_id
		ON CONFLICT(link_id,source_message_id) DO NOTHING`, channelID, topicTelegramID, messageTelegramID)
	if err != nil {
		return err
	}
	_, _ = s.DB.ExecContext(ctx, `UPDATE tgstrm.mirror_deliveries md SET
		destination_message_id=COALESCE(md.destination_message_id,candidate.id),
		destination_telegram_message_id=COALESCE(md.destination_telegram_message_id,candidate.telegram_message_id),updated_at=now()
		FROM tgstrm.mirror_links ml
		JOIN tgstrm.topics source_topic ON source_topic.id=ml.source_topic_id
		JOIN tgstrm.chats source_chat ON source_chat.id=source_topic.chat_id
		JOIN tgstrm.messages source ON source.topic_id=source_topic.id
		JOIN tgstrm.message_media sm ON sm.message_id=source.id
		JOIN LATERAL (SELECT target.id,target.telegram_message_id FROM tgstrm.messages target
			JOIN tgstrm.message_media tm ON tm.message_id=target.id
			WHERE target.topic_id=ml.destination_topic_id AND target.is_active AND tm.telegram_file_id=sm.telegram_file_id
			ORDER BY target.telegram_message_id DESC LIMIT 1) candidate ON true
		WHERE md.link_id=ml.id AND source_chat.telegram_peer_id=$1 AND source_topic.telegram_topic_id=$2
		AND source.id=md.source_message_id AND source.telegram_message_id=$3
		AND md.destination_telegram_message_id IS NULL`, channelID, topicTelegramID, messageTelegramID)
	result, err := s.DB.ExecContext(ctx, `UPDATE tgstrm.mirror_deliveries md SET desired_caption=$4,edit_status='pending',
		source_edited_at=now(),edit_next_attempt_at=now(),edit_last_error=NULL,updated_at=now()
		FROM tgstrm.mirror_links ml JOIN tgstrm.topics t ON t.id=ml.source_topic_id
		JOIN tgstrm.chats c ON c.id=t.chat_id JOIN tgstrm.messages m ON m.id=md.source_message_id
		WHERE md.link_id=ml.id AND ml.status='active' AND c.telegram_peer_id=$1 AND t.telegram_topic_id=$2
		AND m.telegram_message_id=$3`, channelID, topicTelegramID, messageTelegramID, caption)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed > 0 {
		s.wakeMirrorWorker()
	}
	return nil
}

func (s *Service) wakeMirrorWorker() {
	s.runtimeMu.Lock()
	ch := s.mirrorWake
	s.runtimeMu.Unlock()
	if ch != nil {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// WakeMirrorWorker exposes a non-blocking wake-up for HTTP mutations that
// commit mirror work. The work itself always remains in PostgreSQL.
func (s *Service) WakeMirrorWorker() { s.wakeMirrorWorker() }

// ProcessMirrorDeliveries is restart-safe. A failed item remains in PostgreSQL
// and is retried with the same Telegram random_id.
func (s *Service) ProcessMirrorDeliveries(ctx context.Context) {
	if !mirrorWorkerMu.TryLock() {
		return
	}
	defer mirrorWorkerMu.Unlock()
	defer s.processMirrorEdits(ctx)
	s.prepareMirrorHistory(ctx)
	// Updates are the fast path. This set-based recovery also covers messages
	// received while the process was offline or while a client was reconnecting.
	_, _ = s.DB.ExecContext(ctx, `INSERT INTO tgstrm.mirror_deliveries(link_id,source_message_id,random_id)
		SELECT ml.id,m.id,mod(abs(hashtextextended(ml.id::text || ':' || m.id::text,0)),9223372036854775806)+1
		FROM tgstrm.mirror_links ml JOIN tgstrm.messages m ON m.topic_id=ml.source_topic_id
		WHERE ml.status='active' AND m.is_active AND m.media_type='video' AND m.telegram_message_id>ml.start_after_message_id
		ON CONFLICT(link_id,source_message_id) DO NOTHING`)
	// A topic may already contain the same Telegram documents (for example, it
	// was imported before the mirror was created). Adopt those messages as
	// completed deliveries instead of sending duplicates.
	s.reconcileMirrorExistingDestinations(ctx)
	s.reconcileMirrorEditState(ctx)
	for ctx.Err() == nil {
		var deliveryID, linkID, sourceMessageID, sourceGroupID, sourceTopicID, destinationTopicID, fileName, caption string
		var deliveryLease string
		var sourceTelegramID, sourceChannelID, sourceTopicTelegramID, destinationChannelID, destinationTopicTelegramID, dcID int
		var sourceHash, destinationHash, documentID, documentHash, randomID int64
		var fileReference []byte
		err := s.DB.QueryRowContext(ctx, `WITH candidate AS (
			SELECT md.id FROM tgstrm.mirror_deliveries md
			JOIN tgstrm.mirror_links ml ON ml.id=md.link_id
			JOIN tgstrm.messages source ON source.id=md.source_message_id
			WHERE ml.status='active' AND md.next_attempt_at<=now()
			AND (md.status IN ('pending','failed') OR (md.status='sending' AND md.delivery_lease_expires_at<=now()))
			-- Keep every link's Telegram history ordered from oldest to newest,
			-- including when two server instances claim work concurrently.
			AND NOT EXISTS (
				SELECT 1 FROM tgstrm.mirror_deliveries earlier
				JOIN tgstrm.messages earlier_source ON earlier_source.id=earlier.source_message_id
				WHERE earlier.link_id=md.link_id AND earlier.status<>'completed'
				AND earlier_source.telegram_message_id<source.telegram_message_id
			)
			ORDER BY md.link_id,source.telegram_message_id,md.id FOR UPDATE OF md SKIP LOCKED LIMIT 1
		) UPDATE tgstrm.mirror_deliveries md SET status='sending',attempts=attempts+1,
			delivery_lease_token=gen_random_uuid(),delivery_lease_expires_at=now()+interval '15 minutes',updated_at=now()
		FROM candidate WHERE md.id=candidate.id RETURNING md.id::text,md.delivery_lease_token::text`).Scan(&deliveryID, &deliveryLease)
		if errors.Is(err, sql.ErrNoRows) {
			s.refreshMirrorHistoryState(ctx)
			return
		}
		if err != nil {
			return
		}
		err = s.DB.QueryRowContext(ctx, `SELECT ml.id::text,m.id::text,sc.id::text,st.id::text,
			m.telegram_message_id,sc.telegram_peer_id,COALESCE(sc.access_hash,0),st.telegram_topic_id,
			dc.telegram_peer_id,COALESCE(dc.access_hash,0),dt.telegram_topic_id,dt.id::text,
			mm.telegram_file_id,mm.access_hash,mm.file_reference,COALESCE(mm.telegram_dc_id,0),COALESCE(mm.file_name,''),COALESCE(m.body,''),md.random_id
			FROM tgstrm.mirror_deliveries md JOIN tgstrm.mirror_links ml ON ml.id=md.link_id
			JOIN tgstrm.messages m ON m.id=md.source_message_id JOIN tgstrm.message_media mm ON mm.message_id=m.id
			JOIN tgstrm.topics st ON st.id=ml.source_topic_id JOIN tgstrm.chats sc ON sc.id=st.chat_id
			JOIN tgstrm.topics dt ON dt.id=ml.destination_topic_id JOIN tgstrm.chats dc ON dc.id=dt.chat_id
			WHERE md.id=$1::uuid AND ml.status='active'`, deliveryID).Scan(&linkID, &sourceMessageID, &sourceGroupID, &sourceTopicID, &sourceTelegramID, &sourceChannelID, &sourceHash, &sourceTopicTelegramID, &destinationChannelID, &destinationHash, &destinationTopicTelegramID, &destinationTopicID, &documentID, &documentHash, &fileReference, &dcID, &fileName, &caption, &randomID)
		if err != nil {
			return
		}
		s.apiMu.RLock()
		api := s.runtimeAPI
		s.apiMu.RUnlock()
		if api == nil {
			s.failMirror(ctx, deliveryID, linkID, deliveryLease, errors.New("Telegram se está reconectando"))
			return
		}
		send := func(docID, docHash int64, ref []byte) (int, error) {
			reply := &tg.InputReplyToMessage{ReplyToMsgID: destinationTopicTelegramID}
			reply.SetTopMsgID(destinationTopicTelegramID)
			updates, err := api.MessagesSendMedia(ctx, &tg.MessagesSendMediaRequest{Peer: &tg.InputPeerChannel{ChannelID: int64(destinationChannelID), AccessHash: destinationHash}, ReplyTo: reply, Media: &tg.InputMediaDocument{ID: &tg.InputDocument{ID: docID, AccessHash: docHash, FileReference: ref}}, Message: caption, RandomID: randomID})
			if err != nil {
				return 0, err
			}
			return messageIDFromUpdates(updates, randomID), nil
		}
		destinationTelegramID, sendErr := send(documentID, documentHash, fileReference)
		if tgerr.Is(sendErr, "FILE_REFERENCE_EXPIRED", "FILE_REFERENCE_INVALID") {
			fresh, refreshErr := s.refreshExpiredVideoLocation(ctx, VideoFileLocation{VideoID: sourceMessageID, FileName: fileName, DocumentID: documentID, AccessHash: documentHash, FileReference: fileReference, DCID: dcID, GroupID: sourceGroupID, TopicID: sourceTopicID, MessageID: sourceTelegramID, ChannelID: int64(sourceChannelID), ChannelHash: sourceHash})
			if refreshErr == nil {
				destinationTelegramID, sendErr = send(fresh.DocumentID, fresh.AccessHash, fresh.FileReference)
			} else {
				sendErr = refreshErr
			}
		}
		if sendErr != nil {
			s.failMirror(ctx, deliveryID, linkID, deliveryLease, sendErr)
			continue
		}
		result, updateErr := s.DB.ExecContext(ctx, `UPDATE tgstrm.mirror_deliveries md SET status='completed',
			destination_telegram_message_id=NULLIF($2,0),last_error=NULL,completed_at=now(),
			source_edited_at=(SELECT COALESCE(m.edited_at,m.sent_at) FROM tgstrm.messages m WHERE m.id=md.source_message_id),
			delivery_lease_token=NULL,delivery_lease_expires_at=NULL,updated_at=now()
			WHERE md.id=$1 AND md.delivery_lease_token=$3::uuid`, deliveryID, destinationTelegramID, deliveryLease)
		if updateErr != nil {
			return
		}
		if changed, _ := result.RowsAffected(); changed != 1 {
			continue
		}
		_, _ = s.DB.ExecContext(ctx, `UPDATE tgstrm.mirror_links SET last_error=NULL,last_mirrored_at=now(),updated_at=now() WHERE id=$1`, linkID)
		s.refreshMirrorHistoryState(ctx)
		_, _ = s.DB.ExecContext(ctx, `INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload)
			VALUES('mirror.progress','topic',$1::bigint,jsonb_build_object('link_id',$2::text,'status','completed'))`, destinationTopicID, linkID)
		s.queueVideoRefresh(int64(destinationChannelID), destinationTopicTelegramID)
	}
}

func (s *Service) reconcileMirrorExistingDestinations(ctx context.Context) {
	_, _ = s.DB.ExecContext(ctx, `UPDATE tgstrm.mirror_deliveries md SET
		status='completed',destination_message_id=target.id,destination_telegram_message_id=target.telegram_message_id,
		last_error=NULL,completed_at=COALESCE(md.completed_at,now()),
		source_edited_at=COALESCE(source.edited_at,source.sent_at),desired_caption=NULL,edit_status='idle',edit_last_error=NULL,
		delivery_lease_token=NULL,delivery_lease_expires_at=NULL,updated_at=now()
	FROM tgstrm.mirror_links ml
	JOIN tgstrm.messages source ON source.topic_id=ml.source_topic_id AND source.is_active
	JOIN tgstrm.message_media source_media ON source_media.message_id=source.id
	JOIN LATERAL (
		SELECT destination.id,destination.telegram_message_id
		FROM tgstrm.messages destination
		JOIN tgstrm.message_media destination_media ON destination_media.message_id=destination.id
		WHERE destination.topic_id=ml.destination_topic_id AND destination.is_active
		AND destination_media.telegram_file_id=source_media.telegram_file_id
		ORDER BY destination.telegram_message_id DESC LIMIT 1
	) target ON true
	WHERE md.link_id=ml.id AND md.source_message_id=source.id AND ml.status='active'
	AND (md.status IN ('pending','failed') OR (md.status='sending' AND md.delivery_lease_expires_at<=now()))`)
}

// prepareMirrorHistory obtains the authoritative topic history before it
// creates deliveries. Realtime updates are still the fast path for new files;
// this explicit scan is only used when the user requests existing files.
func (s *Service) prepareMirrorHistory(ctx context.Context) {
	var linkID, historyLease, groupID, topicID string
	err := s.DB.QueryRowContext(ctx, `UPDATE tgstrm.mirror_links ml SET history_status='indexing',history_attempts=history_attempts+1,
		history_last_error=NULL,history_lease_token=gen_random_uuid(),history_lease_expires_at=now()+interval '30 minutes',updated_at=now() FROM (
			SELECT candidate.id,sc.id AS group_id,st.id AS topic_id FROM tgstrm.mirror_links candidate
			JOIN tgstrm.topics st ON st.id=candidate.source_topic_id AND st.is_active
			JOIN tgstrm.chats sc ON sc.id=st.chat_id AND sc.is_active
			WHERE candidate.status='active' AND candidate.history_mode='include_existing'
			AND candidate.history_next_attempt_at<=now()
			AND (candidate.history_status IN ('pending','failed') OR (candidate.history_status='indexing' AND candidate.history_lease_expires_at<=now()))
			ORDER BY candidate.created_at FOR UPDATE OF candidate SKIP LOCKED LIMIT 1
		) selected WHERE ml.id=selected.id
		RETURNING ml.id::text,ml.history_lease_token::text,selected.group_id::text,selected.topic_id::text`).Scan(&linkID, &historyLease, &groupID, &topicID)
	if errors.Is(err, sql.ErrNoRows) || err != nil {
		return
	}
	leaseCtx, stopLease := s.keepMirrorHistoryLease(ctx, linkID, historyLease)
	defer stopLease()
	if err = s.SyncTopicVideoCatalog(leaseCtx, groupID, topicID); err != nil {
		detail := fmt.Sprintf("%.500s", err.Error())
		_, _ = s.DB.ExecContext(ctx, `UPDATE tgstrm.mirror_links SET history_status='failed',history_last_error=$3,history_lease_token=NULL,history_lease_expires_at=NULL,
			history_next_attempt_at=now()+make_interval(secs=>LEAST(300,power(2,LEAST(history_attempts,8))::integer)),updated_at=now() WHERE id=$1::uuid AND history_lease_token=$2::uuid`, linkID, historyLease, detail)
		return
	}
	tx, err := s.DB.BeginTx(leaseCtx, nil)
	if err != nil {
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(leaseCtx, `INSERT INTO tgstrm.mirror_deliveries(link_id,source_message_id,random_id)
		SELECT ml.id,m.id,mod(abs(hashtextextended(ml.id::text || ':' || m.id::text,0)),9223372036854775806)+1
		FROM tgstrm.mirror_links ml JOIN tgstrm.messages m ON m.topic_id=ml.source_topic_id
		WHERE ml.id=$1::uuid AND m.is_active AND m.media_type='video'
		ON CONFLICT(link_id,source_message_id) DO NOTHING`, linkID); err != nil {
		return
	}
	if _, err = tx.ExecContext(leaseCtx, `UPDATE tgstrm.mirror_deliveries md SET
		status='completed',destination_message_id=target.id,destination_telegram_message_id=target.telegram_message_id,
		last_error=NULL,completed_at=COALESCE(md.completed_at,now()),
		source_edited_at=COALESCE(source.edited_at,source.sent_at),desired_caption=NULL,edit_status='idle',edit_last_error=NULL,
		delivery_lease_token=NULL,delivery_lease_expires_at=NULL,updated_at=now()
	FROM tgstrm.mirror_links ml
	JOIN tgstrm.messages source ON source.topic_id=ml.source_topic_id AND source.is_active
	JOIN tgstrm.message_media source_media ON source_media.message_id=source.id
	JOIN LATERAL (
		SELECT destination.id,destination.telegram_message_id FROM tgstrm.messages destination
		JOIN tgstrm.message_media destination_media ON destination_media.message_id=destination.id
		WHERE destination.topic_id=ml.destination_topic_id AND destination.is_active
		AND destination_media.telegram_file_id=source_media.telegram_file_id
		ORDER BY destination.telegram_message_id DESC LIMIT 1
	) target ON true
	WHERE md.link_id=ml.id AND md.source_message_id=source.id AND ml.id=$1::uuid
		AND (md.status IN ('pending','failed') OR (md.status='sending' AND md.delivery_lease_expires_at<=now()))`, linkID); err != nil {
		return
	}
	result, err := tx.ExecContext(leaseCtx, `UPDATE tgstrm.mirror_links ml SET history_status='copying',
		history_total=(SELECT count(*) FROM tgstrm.mirror_deliveries WHERE link_id=ml.id),
		history_completed=(SELECT count(*) FROM tgstrm.mirror_deliveries WHERE link_id=ml.id AND status='completed'),
		history_last_error=NULL,history_lease_token=NULL,history_lease_expires_at=NULL,updated_at=now()
		WHERE ml.id=$1::uuid AND ml.history_lease_token=$2::uuid`, linkID, historyLease)
	if err != nil {
		return
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return
	}
	if err = tx.Commit(); err == nil {
		stopLease()
		s.publishMirrorHistoryProgress(ctx, linkID, topicID)
	}
}

func (s *Service) keepMirrorHistoryLease(parent context.Context, linkID, leaseToken string) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				result, err := s.DB.ExecContext(ctx, `UPDATE tgstrm.mirror_links SET history_lease_expires_at=now()+interval '30 minutes'
					WHERE id=$1::uuid AND history_lease_token=$2::uuid`, linkID, leaseToken)
				if err != nil {
					cancel()
					return
				}
				if changed, _ := result.RowsAffected(); changed != 1 {
					cancel()
					return
				}
			}
		}
	}()
	return ctx, cancel
}

func (s *Service) refreshMirrorHistoryState(ctx context.Context) {
	rows, err := s.DB.QueryContext(ctx, `UPDATE tgstrm.mirror_links ml SET
		history_total=counts.total,history_completed=counts.completed,
		history_status=CASE WHEN counts.total=counts.completed THEN 'completed' ELSE 'copying' END,
		history_last_error=CASE WHEN counts.failed=0 THEN NULL ELSE history_last_error END,updated_at=now()
	FROM (SELECT link_id,count(*) total,count(*) FILTER(WHERE status='completed') completed,count(*) FILTER(WHERE status='failed') failed
		FROM tgstrm.mirror_deliveries GROUP BY link_id) counts
	WHERE ml.id=counts.link_id AND ml.history_mode='include_existing' AND ml.history_status IN ('copying','failed')
	AND (ml.history_total IS DISTINCT FROM counts.total OR ml.history_completed IS DISTINCT FROM counts.completed
		OR ml.history_status IS DISTINCT FROM CASE WHEN counts.total=counts.completed THEN 'completed' ELSE 'copying' END
		OR (counts.failed=0 AND ml.history_last_error IS NOT NULL))
	RETURNING ml.id::text,ml.source_topic_id::text`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var linkID, topicID string
		if rows.Scan(&linkID, &topicID) == nil {
			s.publishMirrorHistoryProgress(ctx, linkID, topicID)
		}
	}
}

func (s *Service) publishMirrorHistoryProgress(ctx context.Context, linkID, topicID string) {
	_, _ = s.DB.ExecContext(ctx, `INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload)
		VALUES('mirror.progress','topic',$1::bigint,jsonb_build_object('link_id',$2::text,'status','history'))`, topicID, linkID)
}

// reconcileMirrorEditState repairs the association for deliveries created by
// older versions and closes edit gaps caused by downtime. The source caption
// is authoritative for an active mirror.
func (s *Service) reconcileMirrorEditState(ctx context.Context) {
	// Prefer the exact Telegram message ID returned by messages.sendMedia. This
	// remains unambiguous when the same document was mirrored more than once.
	_, _ = s.DB.ExecContext(ctx, `UPDATE tgstrm.mirror_deliveries md SET
		destination_message_id=target.id,updated_at=now()
		FROM tgstrm.mirror_links ml,tgstrm.messages target
		WHERE md.link_id=ml.id AND ml.status='active' AND md.status='completed'
		AND md.destination_message_id IS NULL AND md.destination_telegram_message_id IS NOT NULL
		AND target.topic_id=ml.destination_topic_id AND target.is_active
		AND target.telegram_message_id=md.destination_telegram_message_id`)
	_, _ = s.DB.ExecContext(ctx, `UPDATE tgstrm.mirror_deliveries md SET
		destination_message_id=candidate.id,destination_telegram_message_id=candidate.telegram_message_id,updated_at=now()
		FROM tgstrm.mirror_links ml JOIN tgstrm.messages source ON source.topic_id=ml.source_topic_id
		JOIN tgstrm.message_media sm ON sm.message_id=source.id
		JOIN LATERAL (SELECT target.id,target.telegram_message_id FROM tgstrm.messages target
			JOIN tgstrm.message_media tm ON tm.message_id=target.id
			WHERE target.topic_id=ml.destination_topic_id AND target.is_active AND tm.telegram_file_id=sm.telegram_file_id
			ORDER BY target.telegram_message_id DESC LIMIT 1) candidate ON true
		WHERE md.link_id=ml.id AND source.id=md.source_message_id AND ml.status='active' AND md.status='completed'
		AND md.destination_telegram_message_id IS NULL`)
	_, _ = s.DB.ExecContext(ctx, `UPDATE tgstrm.mirror_deliveries md SET desired_caption=source.body,
		edit_status='pending',source_edited_at=COALESCE(source.edited_at,source.sent_at),
		edit_next_attempt_at=now(),edit_last_error=NULL,updated_at=now()
		FROM tgstrm.mirror_links ml,tgstrm.messages source,tgstrm.messages target
		WHERE md.link_id=ml.id AND source.id=md.source_message_id AND target.id=md.destination_message_id
		AND ml.status='active' AND md.status='completed' AND md.edit_status='idle'
		AND source.body IS DISTINCT FROM target.body
		AND (md.source_edited_at IS NULL OR COALESCE(source.edited_at,source.sent_at)>md.source_edited_at)`)
}

func messageIDFromUpdates(value tg.UpdatesClass, randomID int64) int {
	var updates []tg.UpdateClass
	switch item := value.(type) {
	case *tg.Updates:
		updates = item.Updates
	case *tg.UpdatesCombined:
		updates = item.Updates
	case *tg.UpdateShort:
		updates = []tg.UpdateClass{item.Update}
	case *tg.UpdateShortSentMessage:
		return item.ID
	}
	for _, update := range updates {
		if mapped, ok := update.(*tg.UpdateMessageID); ok && mapped.RandomID == randomID {
			return mapped.ID
		}
	}
	for _, update := range updates {
		if created, ok := update.(*tg.UpdateNewChannelMessage); ok {
			if message, ok := created.Message.(*tg.Message); ok {
				return message.ID
			}
		}
	}
	return 0
}

func (s *Service) processMirrorEdits(ctx context.Context) {
	for ctx.Err() == nil {
		var deliveryID, editLease, linkID, caption, destinationTopicID string
		var channelID, accessHash int64
		var topicTelegramID, messageTelegramID int
		err := s.DB.QueryRowContext(ctx, `WITH candidate AS (
			SELECT md.id FROM tgstrm.mirror_deliveries md JOIN tgstrm.mirror_links ml ON ml.id=md.link_id
			WHERE ml.status='active' AND md.status='completed'
			AND md.destination_telegram_message_id IS NOT NULL AND md.edit_next_attempt_at<=now()
			AND (md.edit_lease_token IS NULL OR md.edit_lease_expires_at<=now())
			AND (md.edit_status IN ('pending','failed') OR (md.edit_status='editing' AND md.edit_lease_expires_at<=now()))
			ORDER BY md.source_edited_at,md.updated_at FOR UPDATE OF md SKIP LOCKED LIMIT 1
		) UPDATE tgstrm.mirror_deliveries md SET edit_status='editing',edit_attempts=edit_attempts+1,
			edit_lease_token=gen_random_uuid(),edit_lease_expires_at=now()+interval '15 minutes',updated_at=now()
		FROM candidate WHERE md.id=candidate.id RETURNING md.id::text,md.edit_lease_token::text`).Scan(&deliveryID, &editLease)
		if errors.Is(err, sql.ErrNoRows) {
			return
		}
		if err != nil {
			return
		}
		err = s.DB.QueryRowContext(ctx, `SELECT ml.id::text,COALESCE(md.desired_caption,''),dt.id::text,
			dc.telegram_peer_id,COALESCE(dc.access_hash,0),dt.telegram_topic_id,md.destination_telegram_message_id
			FROM tgstrm.mirror_deliveries md JOIN tgstrm.mirror_links ml ON ml.id=md.link_id
			JOIN tgstrm.topics dt ON dt.id=ml.destination_topic_id JOIN tgstrm.chats dc ON dc.id=dt.chat_id
			WHERE md.id=$1::uuid AND ml.status='active'`, deliveryID).Scan(&linkID, &caption, &destinationTopicID, &channelID, &accessHash, &topicTelegramID, &messageTelegramID)
		if err != nil {
			return
		}
		s.apiMu.RLock()
		api := s.runtimeAPI
		s.apiMu.RUnlock()
		if api == nil {
			s.failMirrorEdit(ctx, deliveryID, linkID, editLease, caption, errors.New("Telegram se está reconectando"))
			return
		}
		request := &tg.MessagesEditMessageRequest{Peer: &tg.InputPeerChannel{ChannelID: channelID, AccessHash: accessHash}, ID: messageTelegramID}
		request.SetMessage(caption)
		_, editErr := api.MessagesEditMessage(ctx, request)
		if editErr != nil && !tgerr.Is(editErr, "MESSAGE_NOT_MODIFIED") {
			s.failMirrorEdit(ctx, deliveryID, linkID, editLease, caption, editErr)
			continue
		}
		result, updateErr := s.DB.ExecContext(ctx, `UPDATE tgstrm.mirror_deliveries SET
			edit_status=CASE WHEN desired_caption IS DISTINCT FROM $3 THEN 'pending' ELSE 'idle' END,
			edit_last_error=NULL,desired_caption=CASE WHEN desired_caption IS DISTINCT FROM $3 THEN desired_caption ELSE NULL END,
			edit_lease_token=NULL,edit_lease_expires_at=NULL,updated_at=now()
			WHERE id=$1::uuid AND edit_lease_token=$2::uuid`, deliveryID, editLease, caption)
		if updateErr != nil {
			return
		}
		if changed, _ := result.RowsAffected(); changed != 1 {
			continue
		}
		_, _ = s.DB.ExecContext(ctx, `UPDATE tgstrm.mirror_links SET last_error=NULL,last_mirrored_at=now(),updated_at=now() WHERE id=$1`, linkID)
		_, _ = s.DB.ExecContext(ctx, `INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload) VALUES('mirror.progress','topic',$1::bigint,jsonb_build_object('link_id',$2::text,'status','edited'))`, destinationTopicID, linkID)
		s.queueVideoRefresh(channelID, topicTelegramID)
	}
}

func (s *Service) failMirrorEdit(ctx context.Context, deliveryID, linkID, editLease, caption string, cause error) {
	detail := fmt.Sprintf("%.500s", cause.Error())
	var status string
	err := s.DB.QueryRowContext(ctx, `UPDATE tgstrm.mirror_deliveries SET
		edit_status=CASE WHEN desired_caption IS DISTINCT FROM $3 THEN 'pending' ELSE 'failed' END,
		edit_last_error=CASE WHEN desired_caption IS DISTINCT FROM $3 THEN NULL ELSE $4 END,
		edit_next_attempt_at=CASE WHEN desired_caption IS DISTINCT FROM $3 THEN now()
			ELSE now()+make_interval(secs=>LEAST(300,power(2,LEAST(edit_attempts,8))::integer)) END,
		edit_lease_token=NULL,edit_lease_expires_at=NULL,updated_at=now()
		WHERE id=$1::uuid AND edit_lease_token=$2::uuid RETURNING edit_status`, deliveryID, editLease, caption, detail).Scan(&status)
	if err != nil || status != "failed" {
		return
	}
	_, _ = s.DB.ExecContext(ctx, `UPDATE tgstrm.mirror_links SET last_error=$2,updated_at=now() WHERE id=$1`, linkID, detail)
}

func (s *Service) failMirror(ctx context.Context, deliveryID, linkID, deliveryLease string, cause error) {
	detail := fmt.Sprintf("%.500s", cause.Error())
	result, err := s.DB.ExecContext(ctx, `UPDATE tgstrm.mirror_deliveries SET status='failed',last_error=$3,delivery_lease_token=NULL,delivery_lease_expires_at=NULL,
		next_attempt_at=now()+make_interval(secs=>LEAST(300,power(2,LEAST(attempts,8))::integer)),updated_at=now()
		WHERE id=$1::uuid AND delivery_lease_token=$2::uuid`, deliveryID, deliveryLease, detail)
	if err != nil {
		return
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return
	}
	_, _ = s.DB.ExecContext(ctx, `UPDATE tgstrm.mirror_links SET last_error=$2,updated_at=now() WHERE id=$1`, linkID, detail)
}
