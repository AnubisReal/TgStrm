package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
)

// enqueueCatalogImport commits the canonical import row and its complete
// source manifest before the API acknowledges the request. PostgreSQL remains
// the only queue and source of truth.
func (a *API) enqueueCatalogImport(ctx context.Context, sourceTopicID, destinationTopicID, destinationTitle string, messageIDs []string, match tmdbResult, matched bool, mediaType string) (string, bool, error) {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, sourceTopicID); err != nil {
		return "", false, err
	}
	var existingID, existingStatus string
	err = tx.QueryRowContext(ctx, `SELECT id::text,status FROM tgstrm.catalog_imports
		WHERE source_topic_id=$1::bigint AND status IN ('pending','review','running')
		ORDER BY created_at DESC LIMIT 1`, sourceTopicID).Scan(&existingID, &existingStatus)
	if err == nil && existingStatus == "running" {
		return existingID, true, tx.Commit()
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", false, err
	}

	var destinationTopic sql.NullString
	if strings.TrimSpace(destinationTopicID) != "" {
		destinationTopic = sql.NullString{String: destinationTopicID, Valid: true}
	} else {
		_ = tx.QueryRowContext(ctx, `SELECT t.id::text FROM tgstrm.settings s
	JOIN tgstrm.chats c ON c.id::text=(s.value #>> '{}') AND c.is_active
	JOIN tgstrm.topics t ON t.chat_id=c.id AND t.is_active AND lower(t.title)=lower($1)
		WHERE s.key='active_chat_id' ORDER BY t.id DESC LIMIT 1`, destinationTitle).Scan(&destinationTopic)
	}
	payload := []byte(`null`)
	queuedMediaType, tmdbID := "", int64(0)
	if matched {
		payload, err = json.Marshal(match)
		if err != nil {
			return "", false, err
		}
		if mediaType != "movie" {
			queuedMediaType = "tv"
		} else {
			queuedMediaType = "movie"
		}
		tmdbID = match.ID
	}

	var importID string
	if err == nil && existingID != "" {
		importID = existingID
		_, err = tx.ExecContext(ctx, `UPDATE tgstrm.catalog_imports SET destination_topic_id=NULLIF($2,'')::bigint,destination_title=$3,status='pending',lease_token=NULL,lease_expires_at=NULL,tmdb_media_type=NULLIF($4,''),tmdb_id=NULLIF($5,0),tmdb_payload=$6,requested_count=$7,completed_count=0,last_error=NULL,queued_at=now(),next_attempt_at=now(),started_at=NULL,completed_at=NULL,updated_at=now() WHERE id=$1::uuid`, importID, destinationTopic.String, destinationTitle, queuedMediaType, tmdbID, payload, len(messageIDs))
	} else if destinationTopic.Valid {
		err = tx.QueryRowContext(ctx, `INSERT INTO tgstrm.catalog_imports
			(source_topic_id,destination_topic_id,destination_title,status,tmdb_media_type,tmdb_id,tmdb_payload,requested_count,completed_count,last_error,queued_at,next_attempt_at,started_at,completed_at)
			VALUES($1,$2::bigint,$3,'pending',NULLIF($4,''),NULLIF($5,0),$6,$7,0,NULL,now(),now(),NULL,NULL)
			ON CONFLICT(source_topic_id,destination_topic_id) DO UPDATE SET destination_title=EXCLUDED.destination_title,
			status='pending',lease_token=NULL,lease_expires_at=NULL,tmdb_media_type=EXCLUDED.tmdb_media_type,tmdb_id=EXCLUDED.tmdb_id,tmdb_payload=EXCLUDED.tmdb_payload,
			requested_count=EXCLUDED.requested_count,completed_count=0,last_error=NULL,queued_at=now(),next_attempt_at=now(),started_at=NULL,completed_at=NULL,updated_at=now()
			RETURNING id::text`, sourceTopicID, destinationTopic.String, destinationTitle, queuedMediaType, tmdbID, payload, len(messageIDs)).Scan(&importID)
	} else {
		err = tx.QueryRowContext(ctx, `INSERT INTO tgstrm.catalog_imports
			(source_topic_id,destination_title,status,tmdb_media_type,tmdb_id,tmdb_payload,requested_count,queued_at,next_attempt_at)
			VALUES($1,$2,'pending',NULLIF($3,''),NULLIF($4,0),$5,$6,now(),now()) RETURNING id::text`,
			sourceTopicID, destinationTitle, queuedMediaType, tmdbID, payload, len(messageIDs)).Scan(&importID)
	}
	if err != nil {
		return "", false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tgstrm.catalog_import_messages SET selected_for_run=false,updated_at=now()
		WHERE import_id=$1::uuid`, importID); err != nil {
		return "", false, err
	}
	for _, messageID := range messageIDs {
		if _, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.catalog_import_messages(import_id,source_message_id,status,selected_for_run)
			VALUES($1::uuid,$2::bigint,'pending',true) ON CONFLICT(import_id,source_message_id)
			DO UPDATE SET status='pending',
				selected_for_run=true,last_error=NULL,updated_at=now()`, importID, messageID); err != nil {
			return "", false, err
		}
	}
	if err = tx.Commit(); err != nil {
		return "", false, err
	}
	a.publishCatalogImportEvent(context.Background(), importID, sourceTopicID, "queued")
	return importID, false, nil
}

func (a *API) runCatalogImportQueue(ctx context.Context) {
	// Requeue abandoned claims after their lease expires. The per-message manifest
	// and stable Telegram random_id make recovery safe after an uncertain send.
	_, _ = a.db.ExecContext(ctx, `UPDATE tgstrm.catalog_imports SET status='pending',lease_token=NULL,lease_expires_at=NULL,
		next_attempt_at=now(),updated_at=now() WHERE status='running'
		AND (lease_expires_at IS NULL OR lease_expires_at<=now())`)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := a.processNextCatalogImport(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("Cola de importación: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (a *API) processNextCatalogImport(ctx context.Context) error {
	if a.telegram == nil || !a.telegram.Connected() {
		return nil
	}
	var importID, leaseToken string
	err := a.db.QueryRowContext(ctx, `WITH next AS (
		SELECT id FROM tgstrm.catalog_imports
		WHERE (status='pending' AND next_attempt_at<=now())
		   OR (status='running' AND (lease_expires_at IS NULL OR lease_expires_at<=now()))
		ORDER BY queued_at,id FOR UPDATE SKIP LOCKED LIMIT 1
	) UPDATE tgstrm.catalog_imports ci SET status='running',started_at=now(),attempt_count=attempt_count+1,
		last_error=NULL,lease_token=gen_random_uuid(),lease_expires_at=now()+interval '2 minutes',updated_at=now()
	FROM next WHERE ci.id=next.id RETURNING ci.id::text,ci.lease_token::text`).Scan(&importID, &leaseToken)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	leaseCtx, stopLease := a.keepCatalogImportLease(ctx, importID, leaseToken)
	defer stopLease()

	var groupID, topicID, destinationTopicID, destinationTitle, mediaType string
	var payload []byte
	err = a.db.QueryRowContext(leaseCtx, `SELECT sc.id::text,st.id::text,COALESCE(ci.destination_topic_id::text,''),COALESCE(ci.destination_title,st.title),COALESCE(ci.tmdb_media_type,'tv'),COALESCE(ci.tmdb_payload,'null'::jsonb)
		FROM tgstrm.catalog_imports ci JOIN tgstrm.topics st ON st.id=ci.source_topic_id AND st.is_active
		JOIN tgstrm.chats sc ON sc.id=st.chat_id AND sc.is_active WHERE ci.id=$1::uuid`, importID).
		Scan(&groupID, &topicID, &destinationTopicID, &destinationTitle, &mediaType, &payload)
	if err != nil {
		a.failCatalogImport(importID, topicID, leaseToken, err)
		return nil
	}
	rows, err := a.db.QueryContext(leaseCtx, `SELECT source_message_id::text FROM tgstrm.catalog_import_messages
		WHERE import_id=$1::uuid AND selected_for_run ORDER BY source_message_id`, importID)
	if err != nil {
		a.failCatalogImport(importID, topicID, leaseToken, err)
		return nil
	}
	var selected []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			selected = append(selected, id)
		}
	}
	rows.Close()
	if len(selected) == 0 {
		a.failCatalogImport(importID, topicID, leaseToken, errors.New("la importación no conserva archivos de origen"))
		return nil
	}
	var match tmdbResult
	matched := json.Unmarshal(payload, &match) == nil && match.ID > 0
	a.publishCatalogImportEvent(leaseCtx, importID, topicID, "running")
	a.runCatalogImport(leaseCtx, importID, leaseToken, groupID, topicID, destinationTopicID, destinationTitle, mediaType, selected, match, matched)
	stopLease()
	_, _ = a.db.ExecContext(ctx, `UPDATE tgstrm.catalog_imports SET
		status=CASE WHEN status='running' THEN 'pending' ELSE status END,
		next_attempt_at=CASE WHEN status='running' THEN now() ELSE next_attempt_at END,
		lease_token=NULL,lease_expires_at=NULL
		WHERE id=$1::uuid AND lease_token=$2::uuid`, importID, leaseToken)
	return nil
}

func (a *API) keepCatalogImportLease(parent context.Context, importID, leaseToken string) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				result, err := a.db.ExecContext(ctx, `UPDATE tgstrm.catalog_imports SET lease_expires_at=now()+interval '2 minutes'
					WHERE id=$1::uuid AND lease_token=$2::uuid`, importID, leaseToken)
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

func (a *API) failCatalogImport(importID, topicID, leaseToken string, cause error) {
	if cause == nil {
		return
	}
	if errors.Is(cause, context.Canceled) {
		return
	}
	message := fmt.Sprintf("%.500s", cause.Error())
	if strings.Contains(strings.ToLower(message), "reconectando") {
		result, err := a.db.Exec(`UPDATE tgstrm.catalog_imports SET status='pending',lease_token=NULL,lease_expires_at=NULL,next_attempt_at=now()+interval '5 seconds',last_error=$3,updated_at=now() WHERE id=$1::uuid AND lease_token=$2::uuid`, importID, leaseToken, message)
		if err != nil {
			return
		}
		if changed, _ := result.RowsAffected(); changed != 1 {
			return
		}
		a.publishCatalogImportEvent(context.Background(), importID, topicID, "queued")
		return
	}
	result, err := a.db.Exec(`UPDATE tgstrm.catalog_imports SET status='failed',lease_token=NULL,lease_expires_at=NULL,last_error=$3,updated_at=now() WHERE id=$1::uuid AND lease_token=$2::uuid`, importID, leaseToken, message)
	if err != nil {
		return
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return
	}
	a.publishCatalogImportEvent(context.Background(), importID, topicID, "failed")
}

func (a *API) publishCatalogImportEvent(ctx context.Context, importID, topicID, reason string) {
	if strings.TrimSpace(topicID) == "" {
		return
	}
	_, _ = a.db.ExecContext(ctx, `INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload)
		VALUES('catalog.import.progress','topic',$1::bigint,jsonb_build_object('import_id',$2,'reason',$3))`, topicID, importID, reason)
}
