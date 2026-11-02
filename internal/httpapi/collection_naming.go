package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/gotd/td/tgerr"
	"tgstrm/internal/library"
	telegramauth "tgstrm/internal/telegram"
)

type renamePreviewItem struct {
	ID        string `json:"id"`
	GroupID   string `json:"group_id"`
	Previous  string `json:"previous_title"`
	Desired   string `json:"desired_title"`
	Manual    bool   `json:"manual"`
	Conflict  bool   `json:"conflict"`
	Available bool   `json:"available"`
}

func (a *API) namingPreview(ctx context.Context, collectionID string) (int64, []renamePreviewItem, error) {
	var series, movies string
	var revision int64
	if err := a.db.QueryRowContext(ctx, `SELECT series_name_format,movies_name_format,revision FROM tgstrm.library_collections WHERE id::text=$1 AND account_id=$2 AND state<>'archived'`, collectionID, telegramauth.CatalogAccountID).Scan(&series, &movies, &revision); err != nil {
		return 0, nil, err
	}
	rows, err := a.db.QueryContext(ctx, `SELECT cg.id::text,c.id::text,c.title,cg.bucket,cg.media_kind,cg.part,cg.naming_mode='manual',COALESCE(cg.last_applied_title,''),c.is_active FROM tgstrm.collection_groups cg JOIN tgstrm.chats c ON c.id=cg.destination_chat_id WHERE cg.collection_id::text=$1 AND cg.state<>'archived' ORDER BY cg.media_kind,cg.bucket,cg.part`, collectionID)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	items := []renamePreviewItem{}
	for rows.Next() {
		var item renamePreviewItem
		var bucket, kind, applied string
		var part int
		if err = rows.Scan(&item.ID, &item.GroupID, &item.Previous, &bucket, &kind, &part, &item.Manual, &applied, &item.Available); err != nil {
			return 0, nil, err
		}
		pattern := series
		if kind == "movies" {
			pattern = movies
		}
		item.Desired, err = library.FormatDestinationName(pattern, bucket, part)
		if err != nil {
			return 0, nil, err
		}
		item.Conflict = applied != "" && applied != item.Previous && item.Previous != item.Desired
		items = append(items, item)
	}
	return revision, items, rows.Err()
}
func (a *API) previewCollectionNaming(w http.ResponseWriter, r *http.Request) {
	if !a.libraryReady(w) {
		return
	}
	revision, items, err := a.namingPreview(r.Context(), r.PathValue("collectionID"))
	if err != nil {
		fail(w, 422, "preview_failed", "No se pudo calcular la vista previa.")
		return
	}
	writeJSON(w, 200, map[string]any{"revision": revision, "items": items})
}
func (a *API) applyCollectionNaming(w http.ResponseWriter, r *http.Request) {
	if !a.libraryReady(w) {
		return
	}
	var in struct {
		Revision      int64    `json:"revision"`
		RequestKey    string   `json:"request_key"`
		GroupIDs      []string `json:"group_ids"`
		IncludeManual bool     `json:"include_manual"`
	}
	if !decodeLibraryRequest(w, r, &in) {
		return
	}
	if len(in.RequestKey) < 16 || len(in.RequestKey) > 100 || len(in.GroupIDs) == 0 || len(in.GroupIDs) > 200 {
		fail(w, 422, "invalid_selection", "Selecciona los cajones que deseas renombrar.")
		return
	}
	var existing string
	if a.db.QueryRowContext(r.Context(), `SELECT id::text FROM tgstrm.collection_rename_jobs WHERE collection_id::text=$1 AND request_key=$2`, r.PathValue("collectionID"), in.RequestKey).Scan(&existing) == nil {
		writeJSON(w, 202, map[string]any{"id": existing})
		return
	}
	revision, items, err := a.namingPreview(r.Context(), r.PathValue("collectionID"))
	if err != nil || revision != in.Revision {
		fail(w, 409, "revision_conflict", "La carpeta cambió. Repite la vista previa.")
		return
	}
	selected := map[string]bool{}
	for _, id := range in.GroupIDs {
		if selected[id] {
			fail(w, 422, "invalid_selection", "No repitas cajones.")
			return
		}
		selected[id] = true
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 500, "rename_failed", "No se pudo guardar el trabajo.")
		return
	}
	defer tx.Rollback()
	var jobID string
	err = tx.QueryRowContext(r.Context(), `INSERT INTO tgstrm.collection_rename_jobs(collection_id,naming_revision,request_key) SELECT id,revision,$3 FROM tgstrm.library_collections WHERE id::text=$1 AND account_id=$2 AND revision=$4 RETURNING id::text`, r.PathValue("collectionID"), telegramauth.CatalogAccountID, in.RequestKey, revision).Scan(&jobID)
	if err != nil {
		fail(w, 409, "revision_conflict", "La carpeta cambió.")
		return
	}
	count := 0
	for _, item := range items {
		if !selected[item.ID] {
			continue
		}
		if !item.Available || item.Conflict || (item.Manual && !in.IncludeManual) || item.Previous == item.Desired {
			fail(w, 422, "rename_conflict", "Algún cajón seleccionado tiene un cambio externo, es manual o ya coincide.")
			return
		}
		if _, err = tx.ExecContext(r.Context(), `INSERT INTO tgstrm.collection_rename_items(job_id,group_id,previous_title,desired_title) VALUES($1::uuid,$2::uuid,$3,$4)`, jobID, item.ID, item.Previous, item.Desired); err != nil {
			fail(w, 500, "rename_failed", "No se pudo guardar un cajón.")
			return
		}
		count++
	}
	if count != len(selected) {
		fail(w, 422, "invalid_selection", "La selección contiene cajones ajenos a la carpeta.")
		return
	}
	if err = tx.Commit(); err != nil {
		fail(w, 500, "rename_failed", "No se pudo guardar el trabajo.")
		return
	}
	writeJSON(w, 202, map[string]any{"id": jobID, "status": "pending"})
}
func (a *API) processCollectionRename(ctx context.Context) error {
	var jobID, token string
	err := a.db.QueryRowContext(ctx, `WITH next AS (SELECT id FROM tgstrm.collection_rename_jobs WHERE status IN ('pending','running') AND next_attempt_at<=now() AND (lease_expires_at IS NULL OR lease_expires_at<=now()) ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1) UPDATE tgstrm.collection_rename_jobs j SET status='running',lease_token=gen_random_uuid(),lease_expires_at=now()+interval '2 minutes' FROM next WHERE j.id=next.id RETURNING j.id::text,j.lease_token::text`).Scan(&jobID, &token)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	defer a.db.ExecContext(ctx, `UPDATE tgstrm.collection_rename_jobs SET lease_token=NULL,lease_expires_at=NULL WHERE id::text=$1 AND lease_token::text=$2`, jobID, token)
	var id, groupID, previous, desired string
	err = a.db.QueryRowContext(ctx, `SELECT ri.group_id::text,cg.destination_chat_id::text,ri.previous_title,ri.desired_title FROM tgstrm.collection_rename_items ri JOIN tgstrm.collection_groups cg ON cg.id=ri.group_id WHERE ri.job_id::text=$1 AND ri.status='pending' ORDER BY ri.group_id LIMIT 1`, jobID).Scan(&id, &groupID, &previous, &desired)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = a.db.ExecContext(ctx, `UPDATE tgstrm.collection_rename_jobs SET status=CASE WHEN EXISTS(SELECT 1 FROM tgstrm.collection_rename_items WHERE job_id=tgstrm.collection_rename_jobs.id AND status='attention') THEN 'attention' ELSE 'completed' END WHERE id::text=$1`, jobID)
		return err
	}
	if err != nil {
		return err
	}
	err = a.telegram.RenameCollectionGroup(ctx, groupID, previous, desired)
	if err != nil {
		if delay, ok := tgerr.AsFloodWait(err); ok {
			_, _ = a.db.ExecContext(ctx, `UPDATE tgstrm.collection_rename_jobs SET status='pending',next_attempt_at=now()+($2::double precision*interval '1 second') WHERE id::text=$1`, jobID, delay.Seconds())
			return err
		}
		_, _ = a.db.ExecContext(ctx, `UPDATE tgstrm.collection_rename_items SET status='attention',last_error=$3 WHERE job_id::text=$1 AND group_id::text=$2`, jobID, id, err.Error())
		return err
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE tgstrm.collection_rename_items SET status='completed',last_error=NULL WHERE job_id::text=$1 AND group_id::text=$2`, jobID, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tgstrm.collection_groups SET last_applied_title=$2,applied_naming_revision=(SELECT naming_revision FROM tgstrm.collection_rename_jobs WHERE id::text=$3) WHERE id::text=$1`, id, desired, jobID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload) VALUES('groups.snapshot','group',0,jsonb_build_object('reason','collection_rename')),('library.collection.updated','library',0,jsonb_build_object('reason','collection_rename'))`)
	if err != nil {
		return err
	}
	return tx.Commit()
}
