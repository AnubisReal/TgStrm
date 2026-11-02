package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	telegramauth "tgstrm/internal/telegram"
	"time"
)

// reconciliationReport is deliberately read-only. The automatic reconcilers
// already correct known transient states; this endpoint gives the operator a
// precise, safe view before any manual repair is ever offered.
type reconciliationReport struct {
	CheckedAt          time.Time `json:"checked_at"`
	ExportsChecked     int       `json:"exports_checked"`
	MissingFiles       int       `json:"missing_files"`
	ErroredExports     int       `json:"errored_exports"`
	PendingExports     int       `json:"pending_exports"`
	VideosWithoutAsset int       `json:"videos_without_asset"`
	PendingEvents      int       `json:"pending_events"`
	MirrorPending      int       `json:"mirror_pending"`
	MirrorFailed       int       `json:"mirror_failed"`
}

type missingExport struct {
	ID, RootPath, RelativePath, AssetID, GroupID, TopicID, PublicBaseURL string
}

func (a *API) reconciliation(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		fail(w, http.StatusServiceUnavailable, "database_unavailable", "PostgreSQL no está disponible.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	report := reconciliationReport{CheckedAt: time.Now()}
	groupID, topicID := r.PathValue("groupID"), r.PathValue("topicID")
	rows, err := a.db.QueryContext(ctx, `SELECT lt.root_path,e.relative_path,e.status
		FROM tgstrm.stream_exports e JOIN tgstrm.library_targets lt ON lt.id=e.library_id
		JOIN tgstrm.stream_assets sa ON sa.id=e.asset_id
		JOIN tgstrm.messages m ON m.id=sa.message_id AND m.is_active
		JOIN tgstrm.topics t ON t.id=m.topic_id AND t.is_active
		JOIN tgstrm.chats c ON c.id=t.chat_id AND c.is_active
        WHERE ($1='' OR c.id::text=$1) AND ($2='' OR t.id::text=$2)`, groupID, topicID)
	if err != nil {
		fail(w, http.StatusInternalServerError, "reconciliation_failed", "No se pudo revisar los STRM.")
		return
	}
	for rows.Next() {
		var root, relative, status string
		if err := rows.Scan(&root, &relative, &status); err != nil {
			rows.Close()
			fail(w, http.StatusInternalServerError, "reconciliation_failed", "No se pudo revisar los STRM.")
			return
		}
		report.ExportsChecked++
		if status == "error" {
			report.ErroredExports++
		}
		cleanRoot := filepath.Clean(root)
		path := filepath.Clean(filepath.Join(cleanRoot, filepath.FromSlash(relative)))
		if path == cleanRoot || !strings.HasPrefix(path, cleanRoot+string(os.PathSeparator)) {
			report.ErroredExports++
			continue
		}
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				report.MissingFiles++
			} else {
				report.ErroredExports++
			}
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		fail(w, http.StatusInternalServerError, "reconciliation_failed", "No se pudo terminar la revisión.")
		return
	}
	rows.Close()
	// Pending is a live workflow state. Count it after the filesystem pass so a
	// short atomic-write transition cannot make a completed export look stuck in
	// the audit result.
	if err := a.db.QueryRowContext(ctx, `SELECT count(*) FROM tgstrm.stream_exports e
		JOIN tgstrm.stream_assets sa ON sa.id=e.asset_id
		JOIN tgstrm.messages m ON m.id=sa.message_id AND m.is_active
		JOIN tgstrm.topics t ON t.id=m.topic_id AND t.is_active
		JOIN tgstrm.chats c ON c.id=t.chat_id AND c.is_active
		WHERE e.status='pending' AND e.updated_at < now()-interval '10 seconds' AND ($1='' OR c.id::text=$1) AND ($2='' OR t.id::text=$2)`, groupID, topicID).Scan(&report.PendingExports); err != nil {
		fail(w, 500, "reconciliation_failed", "No se pudo completar la comprobación de estado.")
		return
	}
	if err := a.db.QueryRowContext(ctx, `SELECT count(*) FROM tgstrm.messages m
		JOIN tgstrm.topics t ON t.id=m.topic_id AND t.is_active
		JOIN tgstrm.chats c ON c.id=t.chat_id AND c.is_active
		LEFT JOIN tgstrm.stream_assets sa ON sa.message_id=m.id
		WHERE m.is_active AND m.media_type='video' AND sa.id IS NULL AND ($1='' OR c.id::text=$1) AND ($2='' OR t.id::text=$2)`, groupID, topicID).Scan(&report.VideosWithoutAsset); err != nil {
		fail(w, 500, "reconciliation_failed", "No se pudo completar la comprobación de estado.")
		return
	}
	if topicID == "" {
		if err := a.db.QueryRowContext(ctx, `SELECT count(*) FROM tgstrm.outbox_events WHERE delivered_at IS NULL`).Scan(&report.PendingEvents); err != nil {
			fail(w, 500, "reconciliation_failed", "No se pudo completar la comprobación de estado.")
			return
		}
		if err := a.db.QueryRowContext(ctx, `SELECT count(*) FILTER (WHERE status IN ('pending','sending')),
		count(*) FILTER (WHERE status='failed' OR edit_status='failed') FROM tgstrm.mirror_deliveries`).Scan(&report.MirrorPending, &report.MirrorFailed); err != nil {
			fail(w, 500, "reconciliation_failed", "No se pudo completar la comprobación de estado.")
			return
		}
	}
	if topicID != "" && r.Method == http.MethodPost {
		status := "completed"
		if report.MissingFiles+report.ErroredExports > 0 {
			status = "error"
		}
		detail := fmt.Sprintf("%d STRM revisados · %d ausentes · %d con incidencias", report.ExportsChecked, report.MissingFiles, report.ErroredExports)
		tx, err := a.db.BeginTx(ctx, nil)
		if err == nil {
			_, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.activity_events(kind,status,title,detail,group_id,topic_id,item_count)
            SELECT 'reconciliation',$1,'Comprobación de '||title,$2,chat_id,id,$3 FROM tgstrm.topics WHERE id::text=$4 AND chat_id::text=$5`, status, detail, report.ExportsChecked, topicID, groupID)
			if err == nil {
				_, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload) VALUES('activity.snapshot','topic',$1::bigint,'{}'::jsonb)`, topicID)
			}
			if err == nil {
				err = tx.Commit()
			} else {
				_ = tx.Rollback()
			}
		}
		if err != nil {
			log.Printf("record topic check group=%s topic=%s: %v", groupID, topicID, err)
		}
	}
	writeJSON(w, http.StatusOK, report)
}

// repairMissingExports only recreates an existing export record when its
// active source still exists and the file is absent or differs from the
// canonical content stored by PostgreSQL. It never deletes, renames or
// reclassifies a library entry.
func (a *API) repairMissingExports(w http.ResponseWriter, r *http.Request) {
	ctx, unlock := a.lockExportLibrary(r.Context())
	defer unlock()
	r = r.WithContext(ctx)
	if a.db == nil {
		fail(w, http.StatusServiceUnavailable, "database_unavailable", "PostgreSQL no está disponible.")
		return
	}
	settings := a.mediaSettingsDefaults()
	var raw []byte
	if err := a.db.QueryRowContext(r.Context(), `SELECT value FROM tgstrm.settings WHERE key='media'`).Scan(&raw); err == nil {
		_ = json.Unmarshal(raw, &settings)
	}
	baseURL := strings.TrimRight(strings.TrimSpace(settings.PublicBaseURL), "/")
	if baseURL == "" {
		fail(w, http.StatusUnprocessableEntity, "public_url_required", "Configura la dirección pública antes de reparar STRM.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	rows, err := a.db.QueryContext(ctx, `SELECT e.id::text,lt.root_path,e.relative_path,sa.id::text,c.id::text,t.id::text,
		lt.public_base_url
		FROM tgstrm.stream_exports e
		JOIN tgstrm.library_targets lt ON lt.id=e.library_id
		JOIN tgstrm.stream_assets sa ON sa.id=e.asset_id
		JOIN tgstrm.messages m ON m.id=sa.message_id AND m.is_active
		JOIN tgstrm.topics t ON t.id=m.topic_id AND t.is_active
		JOIN tgstrm.chats c ON c.id=t.chat_id AND c.is_active`)
	if err != nil {
		fail(w, http.StatusInternalServerError, "repair_failed", "No se pudieron preparar los STRM faltantes.")
		return
	}
	var candidates []missingExport
	for rows.Next() {
		var item missingExport
		if err := rows.Scan(&item.ID, &item.RootPath, &item.RelativePath, &item.AssetID, &item.GroupID, &item.TopicID,
			&item.PublicBaseURL); err != nil {
			rows.Close()
			fail(w, http.StatusInternalServerError, "repair_failed", "No se pudieron preparar los STRM faltantes.")
			return
		}
		root := filepath.Clean(item.RootPath)
		path := filepath.Clean(filepath.Join(root, filepath.FromSlash(item.RelativePath)))
		if root == "." || path == root || !strings.HasPrefix(path, root+string(os.PathSeparator)) {
			continue
		}
		content := []byte(strings.TrimRight(item.PublicBaseURL, "/") + "/stream/" + item.AssetID + "\n")
		file, openErr := os.Open(path)
		if openErr != nil {
			candidates = append(candidates, item)
		} else {
			actual, readErr := io.ReadAll(io.LimitReader(file, int64(len(content)+1)))
			closeErr := file.Close()
			if readErr == nil {
				readErr = closeErr
			}
			if readErr != nil || !bytes.Equal(actual, content) {
				candidates = append(candidates, item)
			}
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		fail(w, http.StatusInternalServerError, "repair_failed", "No se pudo terminar la preparación.")
		return
	}
	rows.Close()

	changed := make(map[changedExportScope]struct{})
	written, failed := 0, 0
	for _, item := range candidates {
		root := filepath.Clean(item.RootPath)
		path := filepath.Clean(filepath.Join(root, filepath.FromSlash(item.RelativePath)))
		content := []byte(strings.TrimRight(item.PublicBaseURL, "/") + "/stream/" + item.AssetID + "\n")
		expectedHash := sha256.Sum256(content)
		if err := atomicWrite(path, content); err != nil {
			failed++
			_, _ = a.db.ExecContext(ctx, `UPDATE tgstrm.stream_exports SET status='error',last_error=$2,updated_at=now() WHERE id=$1`, item.ID, fmt.Sprintf("No se pudo reparar el STRM: %.450s", err.Error()))
			continue
		}
		if _, err := a.db.ExecContext(ctx, `UPDATE tgstrm.stream_exports SET status='written',written_at=now(),content_hash=$2,last_error=NULL,updated_at=now() WHERE id=$1`, item.ID, expectedHash[:]); err != nil {
			failed++
			continue
		}
		_, _ = a.db.ExecContext(ctx, `UPDATE tgstrm.stream_assets SET processed_signature=source_signature,processed_at=now(),last_export_error=NULL,updated_at=now() WHERE id=$1`, item.AssetID)
		changed[changedExportScope{groupID: item.GroupID, topicID: item.TopicID}] = struct{}{}
		written++
	}
	for scope := range changed {
		_ = a.publishExportChange(ctx, scope.groupID, scope.topicID)
	}
	if written > 0 || failed > 0 {
		status, detail := "completed", fmt.Sprintf("%d STRM reconstruidos", written)
		if failed > 0 {
			status, detail = "failed", fmt.Sprintf("%d STRM reconstruidos · %d requieren revisión", written, failed)
		}
		_, _ = a.db.ExecContext(ctx, `INSERT INTO tgstrm.activity_events(kind,status,title,detail,item_count)
			VALUES('reconciliation',$1,'Reparación de biblioteca',$2,$3)`, status, detail, written)
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "completed", "found": len(candidates), "written": written, "failed": failed})
}

// checkTopic refreshes only the chosen thread, then audits its known exports.
// It invokes no direct export or repair and does not scan other topics.
// Refreshed catalog records still follow the existing automatic reconcilers.
func (a *API) checkTopic(w http.ResponseWriter, r *http.Request) {
	service := a.telegramService()
	if service == nil {
		fail(w, 503, "telegram_unavailable", "Telegram no está disponible.")
		return
	}
	lockKey := "check:" + r.PathValue("groupID") + ":" + r.PathValue("topicID")
	value, _ := a.exportLocks.LoadOrStore(lockKey, &sync.Mutex{})
	lock := value.(*sync.Mutex)
	if !lock.TryLock() {
		fail(w, 409, "topic_check_running", "Este topic ya se está comprobando.")
		return
	}
	defer lock.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	groupID, topicID := r.PathValue("groupID"), r.PathValue("topicID")
	if _, err := service.Topic(ctx, groupID, topicID); err != nil {
		if errors.Is(err, telegramauth.ErrTopicNotFound) {
			fail(w, 404, "topic_not_found", "El topic no está disponible.")
		} else {
			fail(w, 500, "topic_failed", "No se pudo consultar el topic.")
		}
		return
	}
	if err := service.SyncTopicVideoCatalog(ctx, groupID, topicID); err != nil {
		fail(w, 502, "topic_check_failed", "No se pudo completar la comprobación con Telegram. Puedes volver a intentarlo.")
		return
	}
	a.reconciliation(w, r)
}

func (a *API) topicExportStates(w http.ResponseWriter, r *http.Request) {
	service := a.telegramService()
	if service == nil {
		fail(w, 503, "telegram_unavailable", "El catálogo no está disponible.")
		return
	}
	states, err := service.TopicExportStates(r.Context(), r.PathValue("groupID"), r.PathValue("topicID"))
	if err != nil {
		fail(w, 500, "export_states_failed", "No se pudieron consultar los estados STRM.")
		return
	}
	writeJSON(w, 200, map[string]any{"states": states})
}
