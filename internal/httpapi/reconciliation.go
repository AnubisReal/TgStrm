package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
	rows, err := a.db.QueryContext(ctx, `SELECT lt.root_path,e.relative_path,e.status
		FROM tgstrm.stream_exports e JOIN tgstrm.library_targets lt ON lt.id=e.library_id
		JOIN tgstrm.stream_assets sa ON sa.id=e.asset_id
		JOIN tgstrm.messages m ON m.id=sa.message_id AND m.is_active
		JOIN tgstrm.topics t ON t.id=m.topic_id AND t.is_active
		JOIN tgstrm.chats c ON c.id=t.chat_id AND c.is_active`)
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
	_ = a.db.QueryRowContext(ctx, `SELECT count(*) FROM tgstrm.stream_exports e
		JOIN tgstrm.stream_assets sa ON sa.id=e.asset_id
		JOIN tgstrm.messages m ON m.id=sa.message_id AND m.is_active
		JOIN tgstrm.topics t ON t.id=m.topic_id AND t.is_active
		JOIN tgstrm.chats c ON c.id=t.chat_id AND c.is_active
		WHERE e.status='pending' AND e.updated_at < now()-interval '10 seconds'`).Scan(&report.PendingExports)
	_ = a.db.QueryRowContext(ctx, `SELECT count(*) FROM tgstrm.messages m
		JOIN tgstrm.topics t ON t.id=m.topic_id AND t.is_active
		JOIN tgstrm.chats c ON c.id=t.chat_id AND c.is_active
		LEFT JOIN tgstrm.stream_assets sa ON sa.message_id=m.id
		WHERE m.is_active AND m.media_type='video' AND sa.id IS NULL`).Scan(&report.VideosWithoutAsset)
	_ = a.db.QueryRowContext(ctx, `SELECT count(*) FROM tgstrm.outbox_events WHERE delivered_at IS NULL`).Scan(&report.PendingEvents)
	_ = a.db.QueryRowContext(ctx, `SELECT count(*) FILTER (WHERE status IN ('pending','sending')),
		count(*) FILTER (WHERE status='failed' OR edit_status='failed') FROM tgstrm.mirror_deliveries`).Scan(&report.MirrorPending, &report.MirrorFailed)
	writeJSON(w, http.StatusOK, report)
}

// repairMissingExports only recreates an existing export record when its
// active source still exists and the file is absent or differs from the
// canonical content stored by PostgreSQL. It never deletes, renames or
// reclassifies a library entry.
func (a *API) repairMissingExports(w http.ResponseWriter, r *http.Request) {
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
