package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type reconciledExport struct {
	id, rootPath, relativePath, status, topicID, groupID, assetID, publicBaseURL string
	contentHash                                                                  []byte
	messageActive, topicActive, chatActive                                       bool
}

type changedExportScope struct {
	groupID string
	topicID string
}

func deletedTopicTrees(exports []reconciledExport) map[string]struct{} {
	trees := make(map[string]struct{})
	for _, item := range exports {
		if item.topicActive && item.chatActive {
			continue
		}
		root := filepath.Clean(item.rootPath)
		first := strings.Split(filepath.ToSlash(item.relativePath), "/")[0]
		tree := filepath.Clean(filepath.Join(root, filepath.FromSlash(first)))
		if first == "" || first == "." || tree == root || !strings.HasPrefix(tree, root+string(os.PathSeparator)) {
			continue
		}
		shared := false
		for _, other := range exports {
			if (!other.topicActive || !other.chatActive) || filepath.Clean(other.rootPath) != root {
				continue
			}
			otherFirst := strings.Split(filepath.ToSlash(other.relativePath), "/")[0]
			if filepath.Clean(filepath.Join(root, filepath.FromSlash(otherFirst))) == tree {
				shared = true
				break
			}
		}
		if !shared {
			trees[tree] = struct{}{}
		}
	}
	return trees
}

// runExportReconciler makes PostgreSQL reflect the generated library on disk.
// A periodic scan is intentional: it also works with Docker bind mounts, NAS
// volumes and filesystems whose native change notifications are unreliable.
func (a *API) runExportReconciler(ctx context.Context) {
	a.reconcileCompletedCatalogImports(ctx)
	a.retryEpisodeReplacements(ctx)
	if err := a.reconcileExports(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("No se pudieron reconciliar los STRM: %v", err)
	}
	a.autoGeneratePrimarySTRM(ctx)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.reconcileCompletedCatalogImports(ctx)
			a.retryEpisodeReplacements(ctx)
			if err := a.reconcileExports(ctx); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("No se pudieron reconciliar los STRM: %v", err)
			}
			a.autoGeneratePrimarySTRM(ctx)
		}
	}
}

func (a *API) retryEpisodeReplacements(ctx context.Context) {
	if a.telegram == nil {
		return
	}
	rows, err := a.db.QueryContext(ctx, `SELECT er.id::text,er.old_message_id::text,t.title,t.chat_id::text,t.id::text
		FROM tgstrm.episode_replacements er JOIN tgstrm.topics t ON t.id=er.topic_id
		WHERE er.status IN ('pending','failed') AND er.updated_at < now()-interval '10 seconds'
		ORDER BY er.created_at LIMIT 4`)
	if err != nil {
		return
	}
	type replacement struct{ id, oldMessageID, title, groupID, topicID string }
	var pending []replacement
	for rows.Next() {
		var item replacement
		if rows.Scan(&item.id, &item.oldMessageID, &item.title, &item.groupID, &item.topicID) == nil {
			pending = append(pending, item)
		}
	}
	rows.Close()
	for _, item := range pending {
		deleteCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		err = a.telegram.DeleteVideoMessage(deleteCtx, item.oldMessageID)
		cancel()
		if err != nil {
			_, _ = a.db.ExecContext(ctx, `UPDATE tgstrm.episode_replacements SET status='failed',last_error=$2,updated_at=now() WHERE id=$1`, item.id, err.Error())
			continue
		}
		a.completeEpisodeReplacement(ctx, item.id, item.title)
		_ = a.publishExportChange(ctx, item.groupID, item.topicID)
	}
}

// reconcileCompletedCatalogImports closes the durable gap between Telegram
// accepting a batch and its destination topic becoming fully searchable. This
// also repairs imports completed by an older server version after a restart.
func (a *API) reconcileCompletedCatalogImports(ctx context.Context) {
	if a.telegram == nil {
		return
	}
	_, _ = a.db.ExecContext(ctx, `UPDATE tgstrm.topics t SET library_kind='series',updated_at=now()
		FROM tgstrm.catalog_imports ci
		WHERE ci.destination_topic_id=t.id AND ci.status='completed' AND t.is_active
		AND t.library_kind='unclassified'`)
	rows, err := a.db.QueryContext(ctx, `SELECT DISTINCT dt.chat_id::text,dt.id::text
		FROM tgstrm.catalog_imports ci JOIN tgstrm.topics dt ON dt.id=ci.destination_topic_id AND dt.is_active
		WHERE ci.status='completed' AND ci.requested_count>0 AND
		 (SELECT count(*) FROM tgstrm.messages m WHERE m.topic_id=dt.id AND m.is_active AND m.media_type='video') < ci.requested_count
		ORDER BY dt.id::text LIMIT 2`)
	if err != nil {
		return
	}
	type target struct{ groupID, topicID string }
	var targets []target
	for rows.Next() {
		var item target
		if rows.Scan(&item.groupID, &item.topicID) == nil {
			targets = append(targets, item)
		}
	}
	rows.Close()
	for _, item := range targets {
		syncCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		err = a.telegram.SyncTopicVideoCatalog(syncCtx, item.groupID, item.topicID)
		cancel()
		if err == nil {
			a.generateAutomaticTopicSTRM(ctx, item.groupID, item.topicID)
			_, _ = a.db.ExecContext(ctx, `INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload)
				VALUES('topics.snapshot','group',$1::bigint,jsonb_build_object('group_id',$1::bigint::text,'reason','import_reconciled'))`, item.groupID)
		}
	}
}

func (a *API) autoGeneratePrimarySTRM(ctx context.Context) {
	rows, err := a.db.QueryContext(ctx, `SELECT DISTINCT c.id::text,t.id::text
	 FROM tgstrm.settings active
	 JOIN tgstrm.chats c ON c.id::text=(active.value #>> '{}') AND c.is_active
	 JOIN tgstrm.topics t ON t.chat_id=c.id AND t.is_active AND t.library_kind IN ('movie_collection','series')
	 JOIN tgstrm.messages m ON m.topic_id=t.id AND m.is_active AND m.media_type='video'
	 JOIN tgstrm.stream_assets sa ON sa.message_id=m.id
	 JOIN tgstrm.settings started ON started.key='auto_export_started_at'
	 WHERE active.key='active_chat_id' AND (sa.created_at >= (started.value #>> '{}')::timestamptz
	   OR EXISTS(SELECT 1 FROM tgstrm.stream_exports existing WHERE existing.asset_id=sa.id)
	   OR EXISTS(SELECT 1 FROM tgstrm.tmdb_matches matched WHERE matched.message_id=m.id AND matched.media_type='movie'))
	 AND (
	   sa.processed_signature IS DISTINCT FROM sa.source_signature
	   -- A movie can be examined before TMDB returns its confirmed identity.
	   -- Once the match arrives it must be retried even if that first pass
	   -- recorded the source signature as processed.
	   OR (t.library_kind='movie_collection'
	       AND EXISTS(SELECT 1 FROM tgstrm.tmdb_matches matched WHERE matched.message_id=m.id AND matched.media_type='movie')
	       AND NOT EXISTS(SELECT 1 FROM tgstrm.stream_exports written WHERE written.asset_id=sa.id AND written.status='written'))
	 )
	 AND (sa.processed_at IS NULL OR sa.processed_at < now()-interval '30 seconds')
	 ORDER BY t.id::text LIMIT 4`)
	if err != nil {
		return
	}
	type target struct{ groupID, topicID string }
	var targets []target
	for rows.Next() {
		var item target
		if rows.Scan(&item.groupID, &item.topicID) == nil {
			targets = append(targets, item)
		}
	}
	rows.Close()
	for _, item := range targets {
		a.generateAutomaticTopicSTRM(ctx, item.groupID, item.topicID)
	}
}

// generateAutomaticTopicSTRM uses the same validated export path as the web
// action. Calling it explicitly after an import avoids waiting for the periodic
// reconciler and guarantees that it sees the fully indexed destination topic.
func (a *API) generateAutomaticTopicSTRM(ctx context.Context, groupID, topicID string) int {
	return a.generateSelectedTopicSTRM(ctx, groupID, topicID, "")
}

func (a *API) generateSelectedTopicSTRM(ctx context.Context, groupID, topicID, messageID string) int {
	payload, _ := json.Marshal(map[string]any{"message_ids": []string{}, "public_base_url": ""})
	if messageID != "" {
		payload, _ = json.Marshal(map[string]any{"message_ids": []string{messageID}, "public_base_url": ""})
	}
	request := httptest.NewRequest("POST", "/internal/exports", bytes.NewReader(payload)).WithContext(ctx)
	request.Header.Set("Content-Type", "application/json")
	request.SetPathValue("groupID", groupID)
	request.SetPathValue("topicID", topicID)
	recorder := httptest.NewRecorder()
	a.generateTopicSTRM(recorder, request)
	return recorder.Code
}

func (a *API) reconcileExports(ctx context.Context) error {
	rows, err := a.db.QueryContext(ctx, `SELECT e.id::text,lt.root_path,e.relative_path,e.status,e.content_hash,
	 sa.id::text,lt.public_base_url,t.id::text,c.id::text,m.is_active,t.is_active,c.is_active
	 FROM tgstrm.stream_exports e
	 JOIN tgstrm.library_targets lt ON lt.id=e.library_id
	 JOIN tgstrm.stream_assets sa ON sa.id=e.asset_id
	 JOIN tgstrm.messages m ON m.id=sa.message_id
	 JOIN tgstrm.topics t ON t.id=m.topic_id
	 JOIN tgstrm.chats c ON c.id=t.chat_id`)
	if err != nil {
		return err
	}
	var exports []reconciledExport
	for rows.Next() {
		var item reconciledExport
		if err = rows.Scan(&item.id, &item.rootPath, &item.relativePath, &item.status, &item.contentHash,
			&item.assetID, &item.publicBaseURL, &item.topicID, &item.groupID,
			&item.messageActive, &item.topicActive, &item.chatActive); err != nil {
			rows.Close()
			return err
		}
		exports = append(exports, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}

	// A topic may own hundreds of STRM files. Record its scope once and publish
	// after the filesystem/database pass, instead of flooding every WebSocket
	// subscriber with one snapshot event per deleted file.
	changedScopes := make(map[changedExportScope]struct{})
	// A deleted topic owns its complete top-level library directory. Remove it
	// recursively, including posters and media-server metadata, before clearing
	// its export rows. Never remove the library root or a directory still used
	// by another active topic.
	failedTrees := make(map[string]struct{})
	for tree := range deletedTopicTrees(exports) {
		if err = os.RemoveAll(tree); err != nil {
			log.Printf("No se pudo eliminar la carpeta del topic %s: %v", tree, err)
			failedTrees[tree] = struct{}{}
		}
	}
	for _, item := range exports {
		root := filepath.Clean(item.rootPath)
		path := filepath.Clean(filepath.Join(root, filepath.FromSlash(item.relativePath)))
		if path == root || !strings.HasPrefix(path, root+string(os.PathSeparator)) {
			continue
		}
		if !item.messageActive || !item.topicActive || !item.chatActive {
			first := strings.Split(filepath.ToSlash(item.relativePath), "/")[0]
			if _, failed := failedTrees[filepath.Clean(filepath.Join(root, filepath.FromSlash(first)))]; failed {
				// Keep the DB ownership rows so the next reconciliation retries the
				// whole directory removal, including posters and other sidecar files.
				continue
			}
			if err = os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				continue
			}
			result, deleteErr := a.db.ExecContext(ctx, `DELETE FROM tgstrm.stream_exports WHERE id=$1`, item.id)
			if deleteErr != nil {
				return deleteErr
			}
			if changed, _ := result.RowsAffected(); changed > 0 {
				removeEmptyExportParents(path, root)
				changedScopes[changedExportScope{groupID: item.groupID, topicID: item.topicID}] = struct{}{}
			}
			continue
		}

		content := []byte(strings.TrimRight(item.publicBaseURL, "/") + "/stream/" + item.assetID + "\n")
		expectedHash := sha256.Sum256(content)
		file, openErr := os.Open(path)
		status := "written"
		lastError := any(nil)
		if errors.Is(openErr, os.ErrNotExist) {
			status = "missing"
			lastError = "El archivo STRM no existe en el destino configurado."
		} else if openErr != nil {
			status = "error"
			lastError = openErr.Error()
		} else {
			actual, readErr := io.ReadAll(io.LimitReader(file, int64(len(content)+1)))
			closeErr := file.Close()
			if readErr == nil {
				readErr = closeErr
			}
			if readErr != nil {
				status = "error"
				lastError = readErr.Error()
			} else if !bytes.Equal(actual, content) {
				status = "error"
				lastError = "El contenido del STRM no coincide con su registro en PostgreSQL."
			}
		}
		if status == item.status && bytes.Equal(item.contentHash, expectedHash[:]) {
			continue
		}
		result, updateErr := a.db.ExecContext(ctx, `UPDATE tgstrm.stream_exports
		 SET status=$2,last_error=$3,content_hash=$4,updated_at=now()
		 WHERE id=$1 AND (status IS DISTINCT FROM $2 OR content_hash IS DISTINCT FROM $4)`, item.id, status, lastError, expectedHash[:])
		if updateErr != nil {
			return updateErr
		}
		if changed, _ := result.RowsAffected(); changed > 0 {
			changedScopes[changedExportScope{groupID: item.groupID, topicID: item.topicID}] = struct{}{}
		}
	}
	for scope := range changedScopes {
		if err = a.publishExportChange(ctx, scope.groupID, scope.topicID); err != nil {
			return err
		}
	}
	return nil
}

func (a *API) publishExportChange(ctx context.Context, groupID, topicID string) error {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload)
	 VALUES('topics.snapshot','group',$1::bigint,jsonb_build_object('group_id',$1::bigint::text,'reason','exports'))`, groupID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload)
	 VALUES('videos.snapshot','topic',$1::bigint,jsonb_build_object('topic_id',$1::bigint::text,'reason','exports'))`, topicID); err != nil {
		return err
	}
	return tx.Commit()
}
