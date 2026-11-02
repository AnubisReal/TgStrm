package httpapi

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"tgstrm/internal/library"
	telegramauth "tgstrm/internal/telegram"
)

type cleanupAutomaticKey struct{}
type exportLibraryReadKey struct{}

func (a *API) lockExportLibrary(ctx context.Context) (context.Context, func()) {
	if ctx.Value(exportLibraryReadKey{}) != nil {
		return ctx, func() {}
	}
	a.libraryFilesystem.RLock()
	return context.WithValue(ctx, exportLibraryReadKey{}, true), a.libraryFilesystem.RUnlock
}

type cleanupTree struct {
	Topics  []string `json:"topics"`
	Root    string   `json:"root"`
	Name    string   `json:"name"`
	Exports []string `json:"exports"`
}
type cleanupPreview struct {
	TargetName  string        `json:"target_name"`
	ActiveJobID string        `json:"active_job_id,omitempty"`
	Scope       library.Scope `json:"scope"`
	Groups      []string      `json:"groups"`
	Trees       []cleanupTree `json:"trees"`
	Exports     int           `json:"exports"`
	Fingerprint string        `json:"fingerprint"`
	Paused      bool          `json:"paused"`
}

func cleanupTreeName(relative string) (string, error) {
	if !filepath.IsLocal(filepath.FromSlash(relative)) {
		return "", errors.New("ruta STRM fuera de su biblioteca")
	}
	first := strings.Split(filepath.ToSlash(filepath.Clean(relative)), "/")[0]
	if first == "." || first == "" || strings.HasPrefix(first, ".tgstrm-") {
		return "", errors.New("ruta de biblioteca no válida")
	}
	return first, nil
}
func (a *API) cleanupPreview(ctx context.Context) (cleanupPreview, error) {
	var result cleanupPreview
	result.Groups = []string{}
	result.Trees = []cleanupTree{}
	scope, err := (library.Resolver{DB: a.db}).Active(ctx, telegramauth.CatalogAccountID)
	if err != nil {
		return result, err
	}
	result.Scope = scope
	rows, err := a.db.QueryContext(ctx, `SELECT c.id::text,c.title FROM tgstrm.chats c WHERE c.account_id=$1 AND (($2='group' AND c.id::text=$3) OR ($2='collection' AND EXISTS(SELECT 1 FROM tgstrm.collection_groups cg WHERE cg.destination_chat_id=c.id AND cg.collection_id::text=$4 AND cg.state<>'archived'))) ORDER BY c.id`, telegramauth.CatalogAccountID, scope.Mode, scope.GroupID, scope.CollectionID)
	if err != nil {
		return result, err
	}
	chosen := map[string]bool{}
	for rows.Next() {
		var id, title string
		if err = rows.Scan(&id, &title); err != nil {
			rows.Close()
			return result, err
		}
		if scope.Mode == "group" {
			result.TargetName = title
		}
		chosen[id] = true
		result.Groups = append(result.Groups, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	if scope.Mode == "collection" {
		if err = a.db.QueryRowContext(ctx, `SELECT name FROM tgstrm.library_collections WHERE id::text=$1 AND account_id=$2`, scope.CollectionID, telegramauth.CatalogAccountID).Scan(&result.TargetName); err != nil {
			return result, err
		}
	}
	rows, err = a.db.QueryContext(ctx, `SELECT e.id::text,lt.root_path,e.relative_path,t.chat_id::text,t.id::text FROM tgstrm.stream_exports e JOIN tgstrm.library_targets lt ON lt.id=e.library_id JOIN tgstrm.stream_assets sa ON sa.id=e.asset_id JOIN tgstrm.messages m ON m.id=sa.message_id JOIN tgstrm.topics t ON t.id=m.topic_id ORDER BY e.id`)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	type owner struct {
		tree    cleanupTree
		foreign bool
		wanted  bool
	}
	trees := map[string]*owner{}
	for rows.Next() {
		var id, root, relative, group, topic string
		if err = rows.Scan(&id, &root, &relative, &group, &topic); err != nil {
			return result, err
		}
		name, e := cleanupTreeName(relative)
		if e != nil {
			if chosen[group] {
				return result, e
			}
			continue
		}
		root, err = filepath.Abs(filepath.Clean(root))
		if err == nil {
			if resolved, e := filepath.EvalSymlinks(root); e == nil {
				root = resolved
			} else if !os.IsNotExist(e) && chosen[group] {
				return result, e
			}
		}
		if err != nil || root == string(os.PathSeparator) {
			if chosen[group] {
				return result, errors.New("ubicación de biblioteca no segura")
			}
			continue
		}
		key := root + "\x00" + name
		item := trees[key]
		if item == nil {
			item = &owner{tree: cleanupTree{Root: root, Name: name}}
			trees[key] = item
		}
		if chosen[group] {
			item.wanted = true
			item.tree.Exports = append(item.tree.Exports, id)
			if !containsCleanupID(item.tree.Topics, topic) {
				item.tree.Topics = append(item.tree.Topics, topic)
			}
			result.Exports++
		} else {
			item.foreign = true
		}
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	keys := []string{}
	for key := range trees {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		item := trees[key]
		if !item.wanted {
			continue
		}
		if item.foreign {
			return result, errors.New("una carpeta comparte archivos con otra biblioteca; no se borrará")
		}
		result.Trees = append(result.Trees, item.tree)
	}
	for _, group := range result.Groups {
		var paused bool
		err = a.db.QueryRowContext(ctx, `SELECT COALESCE((SELECT (value->>$1)::boolean FROM tgstrm.settings WHERE key='strm_cleanup_paused_groups'),false)`, group).Scan(&paused)
		if err != nil {
			return result, err
		}
		result.Paused = result.Paused || paused
	}
	// Fingerprint binds confirmation to identities, saved target and physical paths.
	raw, _ := json.Marshal(struct {
		Scope  library.Scope
		Groups []string
		Trees  []cleanupTree
	}{result.Scope, result.Groups, result.Trees})
	sum := sha256.Sum256(raw)
	result.Fingerprint = hex.EncodeToString(sum[:])
	err = a.db.QueryRowContext(ctx, `SELECT id::text FROM tgstrm.library_cleanup_jobs WHERE account_id=$1 AND status IN ('pending','running') ORDER BY created_at LIMIT 1`, telegramauth.CatalogAccountID).Scan(&result.ActiveJobID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return result, err
	}
	return result, nil
}
func (a *API) libraryCleanup(w http.ResponseWriter, r *http.Request) {
	if !a.libraryReady(w) {
		return
	}
	a.libraryFilesystem.Lock()
	defer a.libraryFilesystem.Unlock()
	preview, err := a.cleanupPreview(r.Context())
	if err != nil {
		fail(w, 409, "cleanup_unavailable", err.Error())
		return
	}
	if r.Method == http.MethodGet {
		writeJSON(w, 200, preview)
		return
	}
	var in struct {
		Fingerprint  string `json:"fingerprint"`
		Confirmation string `json:"confirmation"`
		Action       string `json:"action"`
	}
	if !decodeLibraryRequest(w, r, &in) {
		return
	}
	if in.Fingerprint != preview.Fingerprint {
		fail(w, 409, "cleanup_changed", "La biblioteca cambió. Revisa de nuevo la limpieza.")
		return
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 500, "cleanup_failed", "No se pudo preparar la limpieza.")
		return
	}
	defer tx.Rollback()
	var running bool
	err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM tgstrm.library_cleanup_jobs WHERE account_id=$1 AND status IN ('pending','running'))`, telegramauth.CatalogAccountID).Scan(&running)
	if err != nil || running {
		fail(w, 409, "cleanup_busy", "Hay una limpieza en curso.")
		return
	}
	pause := map[string]bool{}
	for _, group := range preview.Groups {
		pause[group] = in.Action != "resume"
	}
	data, _ := json.Marshal(pause)
	if in.Action != "resume" && (in.Action != "clean" || in.Confirmation != "LIMPIAR") {
		fail(w, 422, "confirmation_required", "Escribe LIMPIAR para confirmar.")
		return
	}
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO tgstrm.settings(key,value) VALUES('strm_cleanup_paused_groups',$1::jsonb) ON CONFLICT(key) DO UPDATE SET value=tgstrm.settings.value||EXCLUDED.value`, string(data)); err != nil {
		fail(w, 500, "cleanup_failed", "No se pudo pausar la generación.")
		return
	}
	var id string
	if in.Action == "clean" {
		plan, _ := json.Marshal(preview.Trees)
		scope, _ := json.Marshal(map[string]any{"target": preview.Scope, "groups": preview.Groups})
		err = tx.QueryRowContext(r.Context(), `INSERT INTO tgstrm.library_cleanup_jobs(account_id,scope,plan,total_items) VALUES($1,$2::jsonb,$3::jsonb,$4) RETURNING id::text`, telegramauth.CatalogAccountID, string(scope), string(plan), len(preview.Trees)).Scan(&id)
		if err != nil {
			fail(w, 500, "cleanup_failed", "No se pudo guardar la limpieza.")
			return
		}
	}
	if err = tx.Commit(); err != nil {
		fail(w, 500, "cleanup_failed", "No se pudo confirmar la limpieza.")
		return
	}
	writeJSON(w, 202, map[string]any{"id": id, "status": "accepted"})
}
func (a *API) libraryCleanupJob(w http.ResponseWriter, r *http.Request) {
	if !a.libraryReady(w) {
		return
	}
	var status, lastError string
	var done, total int
	err := a.db.QueryRowContext(r.Context(), `SELECT status,completed_items,total_items,COALESCE(last_error,'') FROM tgstrm.library_cleanup_jobs WHERE id::text=$1 AND account_id=$2`, r.PathValue("jobID"), telegramauth.CatalogAccountID).Scan(&status, &done, &total, &lastError)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		fail(w, 500, "cleanup_unavailable", "No se pudo comprobar la limpieza.")
		return
	}
	writeJSON(w, 200, map[string]any{"status": status, "completed": done, "total": total, "last_error": lastError})
}
func deleteCleanupTree(tree cleanupTree) error {
	name, err := cleanupTreeName(tree.Name)
	if err != nil || name != tree.Name {
		return errors.New("ruta de limpieza no válida")
	}
	if !filepath.IsAbs(tree.Root) || filepath.Clean(tree.Root) == string(os.PathSeparator) {
		return errors.New("raíz de limpieza no válida")
	}
	root, err := os.OpenRoot(tree.Root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	// One top-level child: RemoveAll cannot follow a linked intermediate folder.
	return root.RemoveAll(name)
}
func (a *API) processLibraryCleanup(ctx context.Context) error {
	a.libraryFilesystem.Lock()
	defer a.libraryFilesystem.Unlock()
	var id string
	var raw []byte
	var done int
	err := a.db.QueryRowContext(ctx, `WITH candidate AS (SELECT id FROM tgstrm.library_cleanup_jobs WHERE status IN ('pending','running') AND (lease_until IS NULL OR lease_until<=now()) ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1) UPDATE tgstrm.library_cleanup_jobs j SET status='running',lease_until=now()+interval '2 minutes',updated_at=now() FROM candidate WHERE j.id=candidate.id RETURNING j.id::text,j.plan,j.completed_items`).Scan(&id, &raw, &done)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var trees []cleanupTree
	if err = json.Unmarshal(raw, &trees); err != nil {
		return a.failLibraryCleanup(ctx, id, err)
	}
	for index := done; index < len(trees); index++ {
		if err = ctx.Err(); err != nil {
			return err
		}
		item := trees[index]
		if err = deleteCleanupTree(item); err != nil {
			return a.failLibraryCleanup(ctx, id, err)
		}
		tx, err := a.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		for _, export := range item.Exports {
			if _, err = tx.ExecContext(ctx, `UPDATE tgstrm.stream_assets sa SET processed_signature=NULL,processed_at=NULL,last_export_error=NULL,updated_at=now() FROM tgstrm.stream_exports e WHERE e.asset_id=sa.id AND e.id::text=$1`, export); err != nil {
				break
			}
			if _, err = tx.ExecContext(ctx, `DELETE FROM tgstrm.stream_exports WHERE id::text=$1`, export); err != nil {
				break
			}
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, `UPDATE tgstrm.library_cleanup_jobs SET completed_items=$2,lease_until=now()+interval '2 minutes',updated_at=now() WHERE id::text=$1`, id, index+1)
		}
		if err == nil {
			err = tx.Commit()
		} else {
			tx.Rollback()
		}
		if err != nil {
			return err
		}
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE tgstrm.library_cleanup_jobs SET status='completed',lease_until=NULL,last_error=NULL,updated_at=now() WHERE id::text=$1`, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload) VALUES('library.replication.progress','library',0,jsonb_build_object('reason','library_cleanup'))`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload) SELECT 'topics.snapshot','group',(g)::bigint,jsonb_build_object('reason','library_cleanup','group_id',g) FROM tgstrm.library_cleanup_jobs j CROSS JOIN LATERAL jsonb_array_elements_text(COALESCE(j.scope->'groups','[]'::jsonb)) g WHERE j.id::text=$1`, id); err != nil {
		return err
	}
	for _, tree := range trees {
		for _, topic := range tree.Topics {
			if _, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload) VALUES('videos.snapshot','topic',$1::bigint,jsonb_build_object('topic_id',$1::bigint::text,'reason','library_cleanup'))`, topic); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
func (a *API) failLibraryCleanup(ctx context.Context, id string, cause error) error {
	_, err := a.db.ExecContext(ctx, `UPDATE tgstrm.library_cleanup_jobs SET status='attention',last_error=$2,lease_until=NULL,updated_at=now() WHERE id::text=$1`, id, fmt.Sprintf("No se pudo completar: %.400s", cause.Error()))
	if err != nil {
		return err
	}
	return cause
}

func containsCleanupID(ids []string, id string) bool {
	for _, current := range ids {
		if current == id {
			return true
		}
	}
	return false
}
