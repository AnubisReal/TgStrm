package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gotd/td/tgerr"
	"tgstrm/internal/library"
	telegramauth "tgstrm/internal/telegram"
)

func (a *API) replicationPlans(w http.ResponseWriter, r *http.Request) {
	if !a.libraryReady(w) {
		return
	}
	var in struct {
		CollectionID string   `json:"collection_id"`
		GroupIDs     []string `json:"group_ids"`
	}
	if !decodeLibraryRequest(w, r, &in) {
		return
	}
	if in.CollectionID == "" || len(in.GroupIDs) == 0 || len(in.GroupIDs) > 200 {
		fail(w, 422, "invalid_selection", "Elige una carpeta y entre 1 y 200 grupos.")
		return
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 500, "plan_failed", "No se pudo preparar la selección.")
		return
	}
	defer tx.Rollback()
	var planID string
	err = tx.QueryRowContext(r.Context(), `INSERT INTO tgstrm.collection_replication_plans(account_id,collection_id) SELECT $1,id FROM tgstrm.library_collections WHERE id::text=$2 AND account_id=$1 AND state IN ('draft','ready') RETURNING id::text`, telegramauth.CatalogAccountID, in.CollectionID).Scan(&planID)
	if err != nil {
		fail(w, 422, "invalid_collection", "La carpeta no está disponible.")
		return
	}
	seen := map[string]bool{}
	for _, id := range in.GroupIDs {
		numeric, parseErr := strconv.ParseInt(id, 10, 64)
		if parseErr != nil || numeric <= 0 || seen[id] {
			fail(w, 422, "invalid_selection", "La selección contiene grupos inválidos o repetidos.")
			return
		}
		seen[id] = true
		var title string
		err = tx.QueryRowContext(r.Context(), `SELECT title FROM tgstrm.chats c WHERE c.id::text=$1 AND c.account_id=$2 AND c.is_active AND c.is_forum AND NOT EXISTS(SELECT 1 FROM tgstrm.managed_catalog_chats mc WHERE mc.id=c.id)`, id, telegramauth.CatalogAccountID).Scan(&title)
		if err != nil {
			fail(w, 422, "invalid_source", "Selecciona grupos externos accesibles que tengan topics.")
			return
		}
		bucket := "OTHER"
		words := strings.Fields(strings.ToUpper(title))
		for i := len(words) - 1; i >= 0; i-- {
			if b, e := library.NormalizeBucket(words[i]); e == nil {
				bucket = b
				break
			}
		}
		var part int
		err = tx.QueryRowContext(r.Context(), `SELECT COALESCE((SELECT max(part) FROM tgstrm.collection_groups WHERE collection_id::text=$1 AND media_kind='series' AND bucket=$2 AND state<>'archived' AND EXISTS(SELECT 1 FROM tgstrm.chats c WHERE c.id=destination_chat_id AND c.is_active)),0)+1+(SELECT count(*) FROM tgstrm.group_replications WHERE plan_id::text=$3 AND bucket=$2)`, in.CollectionID, bucket, planID).Scan(&part)
		if err != nil {
			fail(w, 500, "plan_failed", "No se pudo asignar el cajón.")
			return
		}
		var pattern string
		if err = tx.QueryRowContext(r.Context(), `SELECT series_name_format FROM tgstrm.library_collections WHERE id::text=$1`, in.CollectionID).Scan(&pattern); err != nil {
			fail(w, 500, "plan_failed", "No se pudo leer el formato.")
			return
		}
		desired, err := library.FormatDestinationName(pattern, bucket, part)
		if err != nil {
			fail(w, 422, "invalid_format", "Revisa el formato de nombres de la carpeta.")
			return
		}
		if _, err = tx.ExecContext(r.Context(), `INSERT INTO tgstrm.group_replications(plan_id,account_id,source_chat_id,bucket,part,destination_title) VALUES($1::uuid,$2,$3::bigint,$4,$5,$6)`, planID, telegramauth.CatalogAccountID, id, bucket, part, desired); err != nil {
			fail(w, 500, "plan_failed", "No se pudo guardar un grupo seleccionado.")
			return
		}
	}
	if err = tx.Commit(); err != nil {
		fail(w, 500, "plan_failed", "No se pudo guardar el borrador.")
		return
	}
	writeJSON(w, 202, map[string]any{"id": planID, "status": "preparing", "review_url": "/imports/replications/" + planID + "/review"})
}

type replicationGroupView struct {
	ID               string `json:"id"`
	SourceID         string `json:"source_group_id"`
	SourceTitle      string `json:"source_title"`
	DestinationID    string `json:"destination_group_id"`
	DestinationTitle string `json:"destination_title"`
	Bucket           string `json:"bucket"`
	Kind             string `json:"media_kind"`
	Part             int    `json:"part"`
	Status           string `json:"status"`
	Error            string `json:"last_error"`
	Topics           int    `json:"topics"`
	Eligible         int    `json:"eligible"`
	Completed        int    `json:"completed"`
}
type replicationTopicView struct {
	ID                  string      `json:"id"`
	GroupID             string      `json:"group_replication_id"`
	SourceTopicID       string      `json:"source_topic_id"`
	Title               string      `json:"title"`
	Status              string      `json:"status"`
	Excluded            bool        `json:"excluded"`
	Videos              int         `json:"videos"`
	Match               *tmdbResult `json:"match"`
	Error               string      `json:"last_error"`
	ImportID            string      `json:"import_id"`
	Copied              int         `json:"copied"`
	PresentationPending bool        `json:"presentation_pending"`
}

func (a *API) replicationPlan(w http.ResponseWriter, r *http.Request) {
	if !a.libraryReady(w) {
		return
	}
	id := r.PathValue("planID")
	var status, collectionID, lastError string
	var revision int64
	var nextAttempt time.Time
	if err := a.db.QueryRowContext(r.Context(), `SELECT status,collection_id::text,revision,COALESCE(last_error,''),next_attempt_at FROM tgstrm.collection_replication_plans WHERE id::text=$1 AND account_id=$2`, id, telegramauth.CatalogAccountID).Scan(&status, &collectionID, &revision, &lastError, &nextAttempt); err != nil {
		fail(w, 404, "plan_not_found", "El borrador no está disponible.")
		return
	}
	if r.Method == http.MethodPatch {
		if status != "review" && status != "preparing" {
			fail(w, 409, "plan_busy", "Espera a que termine la comprobación o pausa la copia.")
			return
		}
		var in struct {
			Revision           int64    `json:"revision"`
			TopicID            string   `json:"topic_id"`
			TopicIDs           []string `json:"topic_ids"`
			TMDBID             int64    `json:"tmdb_id"`
			Excluded           *bool    `json:"excluded"`
			GroupID            string   `json:"group_replication_id"`
			Bucket             string   `json:"bucket"`
			Kind               string   `json:"media_kind"`
			Part               int      `json:"part"`
			DestinationGroupID string   `json:"destination_group_id"`
		}
		if !decodeLibraryRequest(w, r, &in) {
			return
		}
		if in.Revision != revision {
			fail(w, 409, "revision_conflict", "El borrador cambió. Vuelve a cargarlo.")
			return
		}
		var payload []byte
		if in.TMDBID > 0 {
			match, err := a.localizedTVDetails(r.Context(), in.TMDBID)
			if err != nil {
				fail(w, 502, "match_failed", "TMDB no pudo confirmar esta serie.")
				return
			}
			payload, _ = json.Marshal(match)
		}
		tx, err := a.db.BeginTx(r.Context(), nil)
		if err != nil {
			fail(w, 500, "plan_failed", "No se pudo actualizar el borrador.")
			return
		}
		defer tx.Rollback()
		result, err := tx.ExecContext(r.Context(), `UPDATE tgstrm.collection_replication_plans SET revision=revision+1,updated_at=now() WHERE id::text=$1 AND account_id=$2 AND revision=$3 AND status IN ('review','preparing')`, id, telegramauth.CatalogAccountID, in.Revision)
		if err != nil {
			fail(w, 500, "plan_failed", "No se pudo actualizar el borrador.")
			return
		}
		if n, _ := result.RowsAffected(); n != 1 {
			fail(w, 409, "revision_conflict", "El borrador cambió.")
			return
		}
		if len(in.TopicIDs) > 0 {
			if in.Excluded == nil || in.TopicID != "" || in.GroupID != "" || in.TMDBID != 0 || len(in.TopicIDs) > 10000 {
				fail(w, 422, "invalid_choice", "Revisa la selección de topics.")
				return
			}
			ids, _ := json.Marshal(in.TopicIDs)
			result, err = tx.ExecContext(r.Context(), `UPDATE tgstrm.topic_replications tr SET excluded=$3,updated_at=now() FROM tgstrm.group_replications gr WHERE gr.id=tr.group_replication_id AND gr.plan_id::text=$1 AND tr.id::text IN (SELECT jsonb_array_elements_text($2::jsonb)) AND tr.status NOT IN ('empty','excluded_nonvideo')`, id, string(ids), *in.Excluded)
		} else if in.TopicID != "" {
			if len(payload) == 0 && in.Excluded == nil {
				fail(w, 422, "invalid_choice", "Elige una coincidencia o cambia su inclusión.")
				return
			}
			var exclude any
			if in.Excluded != nil {
				exclude = *in.Excluded
			}
			result, err = tx.ExecContext(r.Context(), `UPDATE tgstrm.topic_replications tr SET match_payload=COALESCE($3::jsonb,match_payload),excluded=COALESCE($4::boolean,excluded),updated_at=now() FROM tgstrm.group_replications gr WHERE tr.id::text=$1 AND gr.id=tr.group_replication_id AND gr.plan_id::text=$2`, in.TopicID, id, nullableJSON(payload), exclude)
		} else if in.GroupID != "" {
			if status != "review" {
				fail(w, 409, "plan_busy", "El destino se podrá cambiar cuando termine la comprobación.")
				return
			}
			bucket, bucketErr := library.NormalizeBucket(in.Bucket)
			if bucketErr != nil || (in.Kind != "series" && in.Kind != "movies") || in.Part < 1 {
				fail(w, 422, "invalid_cabinet", "Revisa la letra, el tipo y la parte del cajón.")
				return
			}
			var series, movies string
			if err = tx.QueryRowContext(r.Context(), `SELECT series_name_format,movies_name_format FROM tgstrm.library_collections WHERE id::text=$1`, collectionID).Scan(&series, &movies); err != nil {
				fail(w, 500, "plan_failed", "No se pudo leer el formato.")
				return
			}
			pattern := series
			if in.Kind == "movies" {
				pattern = movies
			}
			title, formatErr := library.FormatDestinationName(pattern, bucket, in.Part)
			if formatErr != nil {
				fail(w, 422, "invalid_format", "Revisa el formato del cajón.")
				return
			}
			if in.DestinationGroupID != "" {
				var valid bool
				err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM tgstrm.collection_groups WHERE collection_id::text=$1 AND destination_chat_id::text=$2 AND state='ready')`, collectionID, in.DestinationGroupID).Scan(&valid)
				if err != nil || !valid {
					fail(w, 422, "invalid_destination", "El cajón elegido no pertenece a esta carpeta.")
					return
				}
			}
			result, err = tx.ExecContext(r.Context(), `UPDATE tgstrm.group_replications SET bucket=$3,media_kind=$4,part=$5,destination_title=$6,destination_chat_id=NULLIF($7,'')::bigint,updated_at=now() WHERE id::text=$1 AND plan_id::text=$2`, in.GroupID, id, bucket, in.Kind, in.Part, title, in.DestinationGroupID)
			if err == nil {
				_, err = tx.ExecContext(r.Context(), `UPDATE tgstrm.topic_replications SET tmdb_media_type=$2,match_payload=CASE WHEN $2='movie' THEN NULL ELSE match_payload END WHERE group_replication_id::text=$1`, in.GroupID, map[string]string{"series": "tv", "movies": "movie"}[in.Kind])
			}
		} else {
			fail(w, 422, "invalid_choice", "Selecciona el cajón o el topic que quieres revisar.")
			return
		}
		if err != nil {
			fail(w, 409, "plan_conflict", "No se pudo guardar esta elección.")
			return
		}
		if n, _ := result.RowsAffected(); n == 0 || (len(in.TopicIDs) == 0 && n != 1) {
			fail(w, 404, "item_not_found", "El elemento no pertenece al borrador.")
			return
		}
		if err = tx.Commit(); err != nil {
			fail(w, 500, "plan_failed", "No se pudo guardar la revisión.")
			return
		}
		writeJSON(w, 200, map[string]any{"revision": in.Revision + 1})
		return
	}
	rows, err := a.db.QueryContext(r.Context(), `SELECT gr.id::text,sc.id::text,sc.title,COALESCE(gr.destination_chat_id::text,''),gr.destination_title,gr.bucket,gr.media_kind,gr.part,gr.status,COALESCE(gr.last_error,''),count(tr.id),count(tr.id) FILTER(WHERE tr.status='eligible' AND NOT tr.excluded),count(tr.id) FILTER(WHERE tr.status='completed') FROM tgstrm.group_replications gr JOIN tgstrm.chats sc ON sc.id=gr.source_chat_id LEFT JOIN tgstrm.topic_replications tr ON tr.group_replication_id=gr.id WHERE gr.plan_id::text=$1 GROUP BY gr.id,sc.id ORDER BY gr.created_at,gr.id`, id)
	if err != nil {
		fail(w, 500, "plan_failed", "No se pudieron leer los cajones.")
		return
	}
	groups := []replicationGroupView{}
	for rows.Next() {
		var item replicationGroupView
		if err = rows.Scan(&item.ID, &item.SourceID, &item.SourceTitle, &item.DestinationID, &item.DestinationTitle, &item.Bucket, &item.Kind, &item.Part, &item.Status, &item.Error, &item.Topics, &item.Eligible, &item.Completed); err != nil {
			rows.Close()
			fail(w, 500, "plan_failed", "No se pudo leer un cajón.")
			return
		}
		groups = append(groups, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		fail(w, 500, "plan_failed", "No se pudieron leer los cajones.")
		return
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	rows, err = a.db.QueryContext(r.Context(), `SELECT tr.id::text,tr.group_replication_id::text,st.id::text,st.title,CASE WHEN tr.import_id IS NULL AND (mf.invalidated_at IS NOT NULL OR mf.revision IS DISTINCT FROM tr.manifest_revision) THEN 'pending' WHEN tr.import_id IS NULL AND EXISTS(SELECT 1 FROM tgstrm.manifest_rechecks q WHERE q.manifest_id=mf.id) THEN 'checking' WHEN tr.status='queued' AND (mf.id IS NULL OR mf.status<>'eligible' OR mf.invalidated_at IS NOT NULL OR mf.revision IS DISTINCT FROM tr.manifest_revision) THEN 'checking' ELSE tr.status END,tr.excluded,COALESCE((mf.evidence->>'videos')::integer,0),tr.match_payload,COALESCE(NULLIF(tr.last_error,''),(SELECT q.last_error FROM tgstrm.manifest_rechecks q WHERE q.manifest_id=mf.id),''),COALESCE(tr.import_id::text,''),COALESCE(ci.completed_count,0),EXISTS(SELECT 1 FROM tgstrm.topic_presentations tp WHERE tp.topic_id=ci.destination_topic_id AND tp.status IN ('pending','attention') AND ci.status='failed' AND ci.completed_count=0) FROM tgstrm.topic_replications tr JOIN tgstrm.group_replications gr ON gr.id=tr.group_replication_id JOIN tgstrm.topics st ON st.id=tr.source_topic_id LEFT JOIN tgstrm.topic_replication_manifests mf ON mf.id=tr.manifest_id LEFT JOIN tgstrm.catalog_imports ci ON ci.id=tr.import_id WHERE gr.plan_id::text=$1 ORDER BY gr.created_at,gr.id,lower(st.title),tr.id LIMIT 101 OFFSET $2`, id, offset)
	if err != nil {
		fail(w, 500, "plan_failed", "No se pudieron leer los topics.")
		return
	}
	defer rows.Close()
	topics := []replicationTopicView{}
	for rows.Next() {
		var item replicationTopicView
		var payload []byte
		if err = rows.Scan(&item.ID, &item.GroupID, &item.SourceTopicID, &item.Title, &item.Status, &item.Excluded, &item.Videos, &payload, &item.Error, &item.ImportID, &item.Copied, &item.PresentationPending); err != nil {
			fail(w, 500, "plan_failed", "No se pudo leer un topic.")
			return
		}
		if len(payload) > 0 {
			var match tmdbResult
			if json.Unmarshal(payload, &match) == nil && match.ID > 0 {
				item.Match = &match
			}
		}
		topics = append(topics, item)
	}
	if rows.Err() != nil {
		fail(w, 500, "plan_failed", "No se pudieron leer los topics.")
		return
	}
	more := len(topics) > 100
	if more {
		topics = topics[:100]
	}
	var indexedVideos, writtenSTRM, exportErrors, identityPending int
	err = a.db.QueryRowContext(r.Context(), `SELECT count(DISTINCT m.id),count(DISTINCT sa.id) FILTER(WHERE EXISTS(SELECT 1 FROM tgstrm.stream_exports e WHERE e.asset_id=sa.id AND e.status='written')),count(DISTINCT sa.id) FILTER(WHERE sa.last_export_error IS NOT NULL AND sa.last_export_error<>''),count(DISTINCT dt.id) FILTER(WHERE EXISTS(SELECT 1 FROM tgstrm.topic_identity_jobs j WHERE j.topic_id=dt.id AND j.status<>'completed'))
 FROM tgstrm.topic_replications tr JOIN tgstrm.group_replications gr ON gr.id=tr.group_replication_id
 JOIN tgstrm.topics dt ON dt.id=tr.destination_topic_id AND dt.is_active
 LEFT JOIN tgstrm.messages m ON m.topic_id=dt.id AND m.is_active AND m.media_type='video'
 LEFT JOIN tgstrm.stream_assets sa ON sa.message_id=m.id WHERE gr.plan_id::text=$1`, id).Scan(&indexedVideos, &writtenSTRM, &exportErrors, &identityPending)
	if err != nil {
		fail(w, 500, "plan_failed", "No se pudo comprobar el estado de la biblioteca.")
		return
	}

	var waitingImports int
	var importResume sql.NullTime
	if err = a.db.QueryRowContext(r.Context(), `SELECT count(*),min(ci.next_attempt_at) FROM tgstrm.catalog_imports ci JOIN tgstrm.topic_replications tr ON tr.import_id=ci.id JOIN tgstrm.group_replications gr ON gr.id=tr.group_replication_id WHERE gr.plan_id::text=$1 AND ci.status='pending' AND ci.next_attempt_at>now() AND (ci.last_error LIKE '%FLOOD_WAIT%' OR ci.last_error LIKE '%rpc error code 5%')`, id).Scan(&waitingImports, &importResume); err != nil {
		fail(w, 500, "plan_failed", "No se pudo consultar la espera de Telegram.")
		return
	}
	var resumeAt any
	if importResume.Valid {
		resumeAt = importResume.Time
	}
	writeJSON(w, 200, map[string]any{"id": id, "collection_id": collectionID, "revision": revision, "status": status, "last_error": lastError, "groups": groups, "topics": topics, "has_more": more, "next_offset": offset + len(topics), "next_attempt_at": nextAttempt, "telegram_wait": map[string]any{"jobs": waitingImports, "resume_at": resumeAt}, "library": map[string]int{"videos": indexedVideos, "written": writtenSTRM, "errors": exportErrors, "identity_pending": identityPending}})
}
func nullableJSON(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return string(value)
}

func (a *API) confirmReplicationPlan(w http.ResponseWriter, r *http.Request) {
	if !a.libraryReady(w) {
		return
	}
	var in struct {
		Revision int64 `json:"revision"`
	}
	if !decodeLibraryRequest(w, r, &in) {
		return
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 500, "plan_failed", "No se pudo confirmar.")
		return
	}
	defer tx.Rollback()
	var status string
	var revision int64
	if err = tx.QueryRowContext(r.Context(), `SELECT status,revision FROM tgstrm.collection_replication_plans WHERE id::text=$1 AND account_id=$2 FOR UPDATE`, r.PathValue("planID"), telegramauth.CatalogAccountID).Scan(&status, &revision); err != nil {
		fail(w, 404, "plan_not_found", "El borrador no está disponible.")
		return
	}
	if status == "running" || status == "completed" {
		writeJSON(w, 202, map[string]any{"id": r.PathValue("planID"), "status": status})
		return
	}
	if status != "review" || revision != in.Revision {
		fail(w, 409, "revision_conflict", "La revisión cambió o todavía no ha terminado.")
		return
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE tgstrm.collection_groups cg SET state='archived',updated_at=now() FROM tgstrm.chats c WHERE c.id=cg.destination_chat_id AND NOT c.is_active AND cg.state<>'archived'`); err != nil {
		fail(w, 500, "plan_failed", "No se pudo comprobar el destino.")
		return
	}
	var collisions bool
	err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM tgstrm.group_replications gr WHERE gr.plan_id::text=$1 AND gr.destination_chat_id IS NULL AND (
 EXISTS(SELECT 1 FROM tgstrm.collection_groups cg WHERE cg.collection_id=(SELECT collection_id FROM tgstrm.collection_replication_plans WHERE id=gr.plan_id) AND cg.media_kind=gr.media_kind AND cg.bucket=gr.bucket AND cg.part=gr.part AND cg.state<>'archived' AND EXISTS(SELECT 1 FROM tgstrm.chats c WHERE c.id=cg.destination_chat_id AND c.is_active))
 OR EXISTS(SELECT 1 FROM tgstrm.group_replications other JOIN tgstrm.collection_replication_plans op ON op.id=other.plan_id JOIN tgstrm.collection_replication_plans own ON own.id=gr.plan_id WHERE other.id<>gr.id AND op.collection_id=own.collection_id AND other.media_kind=gr.media_kind AND other.bucket=gr.bucket AND other.part=gr.part AND (other.plan_id=gr.plan_id OR (op.confirmed_at IS NOT NULL AND op.status IN ('preparing','review','running','paused','attention') AND (other.destination_chat_id IS NULL OR EXISTS(SELECT 1 FROM tgstrm.chats dc WHERE dc.id=other.destination_chat_id AND dc.is_active)))))))`, r.PathValue("planID")).Scan(&collisions)
	if err != nil || collisions {
		fail(w, 409, "cabinet_collision", "La letra y parte del cajón ya están asignadas. Revisa la parte antes de confirmar.")
		return
	}
	var duplicateMatches bool
	err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM tgstrm.topic_replications tr JOIN tgstrm.group_replications gr ON gr.id=tr.group_replication_id WHERE gr.plan_id::text=$1 AND gr.media_kind='series' AND tr.status='eligible' AND NOT tr.excluded AND COALESCE((tr.match_payload->>'id')::bigint,0)>0 GROUP BY gr.id,tr.match_payload->>'id' HAVING count(*)>1)`, r.PathValue("planID")).Scan(&duplicateMatches)
	if err != nil || duplicateMatches {
		fail(w, 409, "duplicate_series", "Hay topics seleccionados asociados a la misma serie en un cajón. Corrige su coincidencia o deja incluida una sola fuente antes de confirmar.")
		return
	}
	var invalid, total int
	err = tx.QueryRowContext(r.Context(), `SELECT count(*) FILTER(WHERE NOT tr.excluded AND tr.status NOT IN ('excluded_nonvideo','empty') AND (tr.status<>'eligible' OR mf.id IS NULL OR mf.status<>'eligible' OR mf.invalidated_at IS NOT NULL OR mf.revision IS DISTINCT FROM tr.manifest_revision OR EXISTS(SELECT 1 FROM tgstrm.manifest_rechecks q WHERE q.manifest_id=mf.id) OR (gr.media_kind='series' AND COALESCE((tr.match_payload->>'id')::bigint,0)=0))),count(*) FILTER(WHERE tr.status='eligible' AND NOT tr.excluded) FROM tgstrm.topic_replications tr JOIN tgstrm.group_replications gr ON gr.id=tr.group_replication_id LEFT JOIN tgstrm.topic_replication_manifests mf ON mf.id=tr.manifest_id WHERE gr.plan_id::text=$1`, r.PathValue("planID")).Scan(&invalid, &total)
	if err != nil || invalid > 0 || total == 0 {
		fail(w, 422, "review_required", "Revisa las coincidencias, exclusiones o cambios pendientes antes de enviar.")
		return
	}
	_, err = tx.ExecContext(r.Context(), `UPDATE tgstrm.collection_replication_plans SET status='running',confirmed_at=COALESCE(confirmed_at,now()),revision=revision+1,next_attempt_at=now(),last_error=NULL,updated_at=now() WHERE id::text=$1`, r.PathValue("planID"))
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `UPDATE tgstrm.group_replications SET status='running' WHERE plan_id::text=$1`, r.PathValue("planID"))
	}
	if err != nil || tx.Commit() != nil {
		fail(w, 500, "plan_failed", "No se pudo guardar la confirmación.")
		return
	}
	writeJSON(w, 202, map[string]any{"id": r.PathValue("planID"), "status": "running"})
}
func (a *API) controlReplicationPlan(w http.ResponseWriter, r *http.Request) {
	if !a.libraryReady(w) {
		return
	}
	if r.PathValue("action") == "skip-presentation" {
		var in struct {
			TopicID string `json:"topic_replication_id"`
		}
		if !decodeLibraryRequest(w, r, &in) {
			return
		}
		result, err := a.db.ExecContext(r.Context(), `UPDATE tgstrm.topic_presentations tp SET status='skipped',last_error=NULL,updated_at=now() FROM tgstrm.topic_replications tr,tgstrm.group_replications gr,tgstrm.collection_replication_plans p,tgstrm.catalog_imports ci WHERE tr.id::text=$1 AND tr.group_replication_id=gr.id AND gr.plan_id=p.id AND p.id::text=$2 AND p.account_id=$3 AND tr.import_id=ci.id AND ci.status='failed' AND ci.completed_count=0 AND tp.topic_id=ci.destination_topic_id AND tp.status IN ('pending','attention')`, in.TopicID, r.PathValue("planID"), telegramauth.CatalogAccountID)
		if err != nil {
			fail(w, 500, "presentation_failed", "No se pudo guardar la decisión.")
			return
		}
		if n, _ := result.RowsAffected(); n != 1 {
			fail(w, 409, "presentation_conflict", "La presentación no está pendiente o el envío ya avanzó.")
			return
		}
		writeJSON(w, 200, map[string]any{"status": "skipped"})
		return
	}
	if r.PathValue("action") == "recover-group" {
		if a.telegram == nil {
			fail(w, 503, "telegram_unavailable", "Conecta Telegram para verificar el grupo.")
			return
		}
		var in struct {
			GroupID       string `json:"group_replication_id"`
			DestinationID string `json:"destination_group_id"`
		}
		if !decodeLibraryRequest(w, r, &in) {
			return
		}
		var belongs bool
		if err := a.db.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM tgstrm.group_replications gr JOIN tgstrm.collection_replication_plans p ON p.id=gr.plan_id WHERE gr.id::text=$1 AND p.id::text=$2 AND p.account_id=$3 AND p.status='attention' AND gr.status='creating')`, in.GroupID, r.PathValue("planID"), telegramauth.CatalogAccountID).Scan(&belongs); err != nil || !belongs {
			fail(w, 409, "recovery_conflict", "El trabajo no tiene una creación pendiente de recuperar.")
			return
		}
		if err := a.telegram.RecoverReplicationGroup(r.Context(), in.GroupID, in.DestinationID); err != nil {
			fail(w, 422, "recovery_failed", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"status": "recovered"})
		return
	}
	action := r.PathValue("action")
	var query string
	switch action {
	case "rematch":
		query = `UPDATE tgstrm.collection_replication_plans p SET status='preparing',revision=revision+1,next_attempt_at=now(),last_error=NULL,updated_at=now() WHERE id::text=$1 AND account_id=$2 AND status='review' AND confirmed_at IS NULL AND EXISTS(SELECT 1 FROM tgstrm.topic_replications tr JOIN tgstrm.group_replications gr ON gr.id=tr.group_replication_id WHERE gr.plan_id=p.id AND gr.media_kind='series' AND tr.status='eligible' AND NOT tr.excluded AND COALESCE((tr.match_payload->>'id')::bigint,0)=0)`
	case "pause":
		query = `UPDATE tgstrm.collection_replication_plans SET status='paused',revision=revision+1,updated_at=now() WHERE id::text=$1 AND account_id=$2 AND status IN ('preparing','running')`
	case "resume":
		query = `UPDATE tgstrm.collection_replication_plans SET status=CASE WHEN EXISTS(SELECT 1 FROM tgstrm.topic_replications tr JOIN tgstrm.group_replications gr ON gr.id=tr.group_replication_id WHERE gr.plan_id=tgstrm.collection_replication_plans.id AND tr.import_id IS NOT NULL) THEN 'running' ELSE 'preparing' END,revision=revision+1,next_attempt_at=now(),last_error=NULL,updated_at=now() WHERE id::text=$1 AND account_id=$2 AND status='paused'`
	case "retry":
		query = `UPDATE tgstrm.collection_replication_plans SET status=CASE WHEN confirmed_at IS NOT NULL THEN 'running' ELSE 'preparing' END,revision=revision+1,next_attempt_at=now(),last_error=NULL,updated_at=now() WHERE id::text=$1 AND account_id=$2 AND status='attention' AND NOT EXISTS(SELECT 1 FROM tgstrm.group_replications WHERE plan_id=tgstrm.collection_replication_plans.id AND status='creating')`
	default:
		fail(w, 404, "action_not_found", "Acción no disponible.")
		return
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 500, "plan_failed", "No se pudo abrir el trabajo.")
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), query, r.PathValue("planID"), telegramauth.CatalogAccountID)
	if err != nil {
		fail(w, 500, "plan_failed", "No se pudo cambiar el trabajo.")
		return
	}
	if n, _ := result.RowsAffected(); n != 1 {
		fail(w, 409, "plan_conflict", "El trabajo no admite esta acción en su estado actual.")
		return
	}
	if action == "rematch" {
		_, err = tx.ExecContext(r.Context(), `UPDATE tgstrm.topic_replications tr SET status='pending',last_error=NULL,updated_at=now() FROM tgstrm.group_replications gr WHERE gr.id=tr.group_replication_id AND gr.plan_id::text=$1 AND gr.media_kind='series' AND tr.status='eligible' AND NOT tr.excluded AND COALESCE((tr.match_payload->>'id')::bigint,0)=0`, r.PathValue("planID"))
		if err != nil {
			fail(w, 500, "rematch_failed", "No se pudo preparar la revisión de coincidencias.")
			return
		}
	}
	if action == "retry" {
		_, err = tx.ExecContext(r.Context(), `UPDATE tgstrm.catalog_imports ci SET status='pending',last_error=NULL,lease_token=NULL,lease_expires_at=NULL,next_attempt_at=now(),updated_at=now() WHERE ci.status='failed' AND EXISTS(SELECT 1 FROM tgstrm.topic_replications tr JOIN tgstrm.group_replications gr ON gr.id=tr.group_replication_id WHERE tr.import_id=ci.id AND gr.plan_id::text=$1)`, r.PathValue("planID"))
		if err != nil {
			fail(w, 500, "retry_failed", "No se pudo reactivar la importación guardada.")
			return
		}
	}
	if err = tx.Commit(); err != nil {
		fail(w, 500, "plan_failed", "No se pudo guardar el trabajo.")
		return
	}
	writeJSON(w, 200, map[string]any{"status": "accepted"})
}

func (a *API) runCollectionWorker(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if a.telegram != nil && a.telegram.Connected() {
			workCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
			if err := a.processCollectionPlan(workCtx); err != nil && !errors.Is(err, context.Canceled) { /* persisted by the worker */
			}
			cancel()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Discovery and presentation maintenance must not delay each preparation step.
// Each queue keeps its existing leases and retry deadlines.
func (a *API) runCollectionMaintenanceWorker(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if a.telegram != nil && a.telegram.Connected() {
			for _, work := range []func(context.Context) error{a.maintainCollections, a.processCollectionRename, a.processCollectionGroupPhoto} {
				if ctx.Err() != nil {
					return
				}
				workCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
				_ = work(workCtx)
				cancel()
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (a *API) processCollectionPlan(ctx context.Context) error {
	var planID, token, status, collectionID string
	err := a.db.QueryRowContext(ctx, `WITH next AS (SELECT id FROM tgstrm.collection_replication_plans WHERE status IN ('preparing','running') AND next_attempt_at<=now() AND (lease_expires_at IS NULL OR lease_expires_at<=now()) ORDER BY updated_at,id FOR UPDATE SKIP LOCKED LIMIT 1) UPDATE tgstrm.collection_replication_plans p SET lease_token=gen_random_uuid(),lease_expires_at=now()+interval '2 minutes' FROM next WHERE p.id=next.id RETURNING p.id::text,p.lease_token::text,p.status,p.collection_id::text`).Scan(&planID, &token, &status, &collectionID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if status == "preparing" {
		err = a.prepareReplicationStep(ctx, planID)
	} else {
		err = a.executeReplicationStep(ctx, planID, collectionID)
	}
	if err != nil {
		delay := 5 * time.Second
		state := "attention"
		if wait, ok := tgerr.AsFloodWait(err); ok {
			delay = wait
			state = status
		} else if errors.Is(err, telegramauth.ErrTopicChangedDuringScan) {
			state = "preparing"
		}
		_, _ = a.db.ExecContext(ctx, `UPDATE tgstrm.collection_replication_plans SET status=$3,last_error=$4,next_attempt_at=now()+($5::double precision*interval '1 second'),lease_token=NULL,lease_expires_at=NULL,revision=revision+1,updated_at=now() WHERE id::text=$1 AND lease_token::text=$2 AND status IN ('preparing','running')`, planID, token, state, fmt.Sprintf("%.300s", err.Error()), delay.Seconds())
	} else {
		_, _ = a.db.ExecContext(ctx, `UPDATE tgstrm.collection_replication_plans SET lease_token=NULL,lease_expires_at=NULL,next_attempt_at=now(),last_error=NULL,revision=revision+1,updated_at=now() WHERE id::text=$1 AND lease_token::text=$2`, planID, token)
	}
	_, _ = a.db.ExecContext(ctx, `INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload) VALUES('library.replication.progress','library',0,jsonb_build_object('plan_id',$1::text))`, planID)
	return err
}
func (a *API) prepareReplicationStep(ctx context.Context, planID string) error {
	// Confirmed copies already validate each next source before creating a
	// destination. Do not restart their entire review after recovery.
	result, err := a.db.ExecContext(ctx, resumeConfirmedReplicationSQL, planID)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed == 1 {
		return nil
	}
	var groupID, sourceID string
	err = a.db.QueryRowContext(ctx, `SELECT id::text,source_chat_id::text FROM tgstrm.group_replications WHERE plan_id::text=$1 AND status='pending' ORDER BY created_at,id LIMIT 1`, planID).Scan(&groupID, &sourceID)
	if err == nil {
		if err = a.telegram.RefreshTopics(ctx, sourceID); err != nil {
			return err
		}
		tx, err := a.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		_, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.topic_replications(group_replication_id,source_topic_id,source_telegram_topic_id) SELECT $1::uuid,id,telegram_topic_id FROM tgstrm.topics WHERE chat_id::text=$2 AND is_active AND NOT is_hidden AND telegram_topic_id<>1 ON CONFLICT(group_replication_id,source_topic_id) DO NOTHING`, groupID, sourceID)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE tgstrm.group_replications SET status='review',last_discovered_at=now() WHERE id::text=$1`, groupID)
		if err != nil {
			return err
		}
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var topicID, title, kind string
	err = a.db.QueryRowContext(ctx, `SELECT tr.id::text,gr.source_chat_id::text,tr.source_topic_id::text,t.title,gr.media_kind FROM tgstrm.topic_replications tr JOIN tgstrm.group_replications gr ON gr.id=tr.group_replication_id JOIN tgstrm.topics t ON t.id=tr.source_topic_id LEFT JOIN tgstrm.topic_replication_manifests mf ON mf.id=tr.manifest_id WHERE gr.plan_id::text=$1 AND (tr.status IN ('pending','scan_failed') OR mf.invalidated_at IS NOT NULL OR mf.revision IS DISTINCT FROM tr.manifest_revision) AND tr.import_id IS NULL AND NOT tr.excluded ORDER BY gr.created_at,tr.created_at,tr.id LIMIT 1`, planID).Scan(&topicID, &sourceID, &groupID, &title, &kind)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = a.db.ExecContext(ctx, `UPDATE tgstrm.collection_replication_plans p SET status=CASE WHEN p.confirmed_at IS NOT NULL AND NOT EXISTS(SELECT 1 FROM tgstrm.topic_replications tr JOIN tgstrm.group_replications gr ON gr.id=tr.group_replication_id WHERE gr.plan_id=p.id AND NOT tr.excluded AND tr.status NOT IN ('completed','queued','excluded_nonvideo','empty') AND (tr.status<>'eligible' OR EXISTS(SELECT 1 FROM tgstrm.manifest_rechecks q WHERE q.manifest_id=tr.manifest_id) OR (gr.media_kind='series' AND COALESCE((tr.match_payload->>'id')::bigint,0)=0))) THEN 'running' ELSE 'review' END,last_error=NULL WHERE id::text=$1`, planID)
		return err
	}
	if err != nil {
		return err
	}
	manifest, manifestID, revision, err := a.telegram.ReviewTopicEligibility(ctx, sourceID, groupID)
	if err != nil {
		return err
	}
	eligibility := library.TopicEligibility(manifest.Evidence)
	var match tmdbResult
	matched := false
	referenceConflict := false
	var existingPayload []byte
	if err = a.db.QueryRowContext(ctx, `SELECT match_payload FROM tgstrm.topic_replications WHERE id::text=$1`, topicID).Scan(&existingPayload); err != nil {
		return err
	}
	hasExistingMatch := json.Unmarshal(existingPayload, &match) == nil && match.ID > 0
	if eligibility == "eligible" && kind == "series" && !hasExistingMatch {
		if len(manifest.Evidence.References) > 0 {
			match, matched, referenceConflict = a.matchReplicationReferences(ctx, manifest.Evidence.References)
		}
		if !matched && !referenceConflict {
			results, searchErr := a.searchExternalSeries(ctx, title)
			if searchErr == nil {
				match, matched = uniqueReplicationMatch(title, results)
				if matched {
					match, searchErr = a.localizedTVDetails(ctx, match.ID)
					matched = searchErr == nil
				}
			}
		}
	}
	var payload []byte
	if matched {
		payload, _ = json.Marshal(match)
	}
	referenceError := ""
	if referenceConflict {
		referenceError = "Las referencias del topic apuntan a series distintas. Revisa la coincidencia."
	}
	_, err = a.db.ExecContext(ctx, `UPDATE tgstrm.topic_replications SET manifest_id=$2::uuid,manifest_revision=$3,status=$4,match_payload=COALESCE(match_payload,$5::jsonb),last_error=NULLIF($6,''),updated_at=now() WHERE id::text=$1`, topicID, manifestID, revision, eligibility, nullableJSON(payload), referenceError)
	return err
}

func (a *API) executeReplicationStep(ctx context.Context, planID, collectionID string) error {
	// Completed child imports are reconciled into mirrors only after the copied
	// files have destination identities, using the existing delivery ledger.
	var childID, importID, sourceTopic, destinationTopic string
	err := a.db.QueryRowContext(ctx, `SELECT tr.id::text,ci.id::text,tr.source_topic_id::text,COALESCE(ci.destination_topic_id::text,'') FROM tgstrm.topic_replications tr JOIN tgstrm.group_replications gr ON gr.id=tr.group_replication_id JOIN tgstrm.catalog_imports ci ON ci.id=tr.import_id WHERE gr.plan_id::text=$1 AND tr.status='queued' AND ci.status IN ('completed','failed') ORDER BY tr.updated_at LIMIT 1`, planID).Scan(&childID, &importID, &sourceTopic, &destinationTopic)
	if err == nil {
		var childStatus string
		if err = a.db.QueryRowContext(ctx, `SELECT status FROM tgstrm.catalog_imports WHERE id::text=$1`, importID).Scan(&childStatus); err != nil {
			return err
		}
		if childStatus != "completed" {
			var title, reason string
			if err = a.db.QueryRowContext(ctx, `SELECT t.title,COALESCE(ci.last_error,'Error sin detalle') FROM tgstrm.catalog_imports ci JOIN tgstrm.topics t ON t.id=ci.source_topic_id WHERE ci.id::text=$1`, importID).Scan(&title, &reason); err != nil {
				return err
			}
			return fmt.Errorf("%s: %s. Pulsa «Revisar de nuevo» para reintentar el trabajo conservando lo ya enviado", title, reason)
		}
		var destinationGroupID string
		if err = a.db.QueryRowContext(ctx, `SELECT chat_id::text FROM tgstrm.topics WHERE id::text=$1 AND is_active`, destinationTopic).Scan(&destinationGroupID); err != nil {
			return err
		}
		if err = a.telegram.SyncTopicVideoCatalog(ctx, destinationGroupID, destinationTopic); err != nil {
			return err
		}
		if err = a.telegram.ActivateReplicationMirror(ctx, sourceTopic, destinationTopic, importID); err != nil {
			return err
		}
		_, err = a.db.ExecContext(ctx, `UPDATE tgstrm.topic_replications SET status='completed',destination_topic_id=$2::bigint,updated_at=now() WHERE id::text=$1`, childID, destinationTopic)
		return err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var groupID, sourceGroup, title, destinationGroup, bucket, kind string
	var part int
	var payload []byte
	err = a.db.QueryRowContext(ctx, `SELECT tr.id::text,gr.id::text,gr.source_chat_id::text,tr.source_topic_id::text,st.title,COALESCE(gr.destination_chat_id::text,''),gr.bucket,gr.media_kind,gr.part,tr.match_payload FROM tgstrm.topic_replications tr JOIN tgstrm.group_replications gr ON gr.id=tr.group_replication_id JOIN tgstrm.topics st ON st.id=tr.source_topic_id WHERE gr.plan_id::text=$1 AND tr.status='eligible' AND NOT tr.excluded AND tr.import_id IS NULL ORDER BY gr.created_at,tr.created_at,tr.id LIMIT 1`, planID).Scan(&childID, &groupID, &sourceGroup, &sourceTopic, &title, &destinationGroup, &bucket, &kind, &part, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		var outstanding int
		if err = a.db.QueryRowContext(ctx, `SELECT count(*) FROM tgstrm.topic_replications tr JOIN tgstrm.group_replications gr ON gr.id=tr.group_replication_id WHERE gr.plan_id::text=$1 AND tr.status='queued'`, planID).Scan(&outstanding); err != nil {
			return err
		}
		if outstanding == 0 {
			_, err = a.db.ExecContext(ctx, `UPDATE tgstrm.group_replications SET status='completed' WHERE plan_id::text=$1 AND status='running'`, planID)
			if err != nil {
				return err
			}
			_, err = a.db.ExecContext(ctx, `UPDATE tgstrm.collection_replication_plans SET status='completed',last_error=NULL WHERE id::text=$1`, planID)
		}
		return err
	}
	if err != nil {
		return err
	}
	// A final authoritative full scan precedes creating any destination. Never
	// substitute the video-only index for the mixed-document eligibility gate.
	manifest, manifestID, revision, err := a.telegram.ScanPersistedTopicEligibility(ctx, sourceGroup, sourceTopic)
	if err != nil {
		return err
	}
	if library.TopicEligibility(manifest.Evidence) != "eligible" {
		_, _ = a.db.ExecContext(ctx, `UPDATE tgstrm.topic_replications SET status=$2,manifest_id=$3::uuid,manifest_revision=$4 WHERE id::text=$1`, childID, library.TopicEligibility(manifest.Evidence), manifestID, revision)
		return errors.New("el topic cambió y ya no es elegible; vuelve a revisar la selección")
	}
	if destinationGroup == "" {
		destinationGroup, err = a.telegram.CreateReplicationGroup(ctx, groupID)
		if err != nil {
			return err
		}
	}
	_, err = a.db.ExecContext(ctx, `INSERT INTO tgstrm.collection_groups(collection_id,account_id,destination_chat_id,bucket,media_kind,part,last_applied_title,applied_naming_revision)
 SELECT lc.id,lc.account_id,$2::bigint,$3,$4,$5,c.title,lc.revision FROM tgstrm.library_collections lc JOIN tgstrm.chats c ON c.id::text=$2 WHERE lc.id::text=$1 ON CONFLICT(collection_id,destination_chat_id) DO NOTHING`, collectionID, destinationGroup, bucket, kind, part)
	if err != nil {
		return err
	}
	if err = a.telegram.EnsureCollectionFolder(ctx, collectionID); err != nil {
		return err
	}
	if err = a.telegram.RefreshTopics(ctx, destinationGroup); err != nil {
		return err
	}
	var match tmdbResult
	matched := json.Unmarshal(payload, &match) == nil && match.ID > 0
	mediaType := "tv"
	if kind == "movies" {
		mediaType = "movie"
		matched = false
	} else {
		if !matched {
			return errors.New("la serie necesita una coincidencia TMDB confirmada")
		}
		title = match.Title
		if title == "" {
			title = match.OriginalTitle
		}
		if len(match.ReleaseDate) >= 4 {
			title += " (" + match.ReleaseDate[:4] + ")"
		}
		title += fmt.Sprintf(" [tmdbid-%d]", match.ID)
	}
	var collision bool
	if err = a.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tgstrm.topics WHERE chat_id::text=$1 AND is_active AND lower(title)=lower($2))`, destinationGroup, title).Scan(&collision); err != nil {
		return err
	}
	if collision {
		return errors.New("el cajón ya contiene un topic con este título; evita fusionarlo sin una decisión revisada")
	}
	videos, _ := json.Marshal(manifest.VideoIDs)
	rows, err := a.db.QueryContext(ctx, `SELECT m.id::text FROM tgstrm.messages m WHERE m.topic_id::text=$1 AND m.is_active AND m.media_type='video' AND m.telegram_message_id IN (SELECT value::integer FROM jsonb_array_elements_text($2::jsonb)) ORDER BY m.telegram_message_id`, sourceTopic, string(videos))
	if err != nil {
		return err
	}
	selected := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		selected = append(selected, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(selected) != manifest.Evidence.Videos {
		return errors.New("el catálogo local no coincide con el manifiesto Telegram; se necesita volver a comprobarlo")
	}
	if _, err = a.db.ExecContext(ctx, `UPDATE tgstrm.topic_replications SET manifest_id=$2::uuid,manifest_revision=$3 WHERE id::text=$1`, childID, manifestID, revision); err != nil {
		return err
	}
	_, _, err = a.enqueueCatalogImportToGroup(ctx, sourceTopic, destinationGroup, "", title, selected, match, matched, mediaType, childID)
	return err
}

const resumeConfirmedReplicationSQL = `UPDATE tgstrm.collection_replication_plans p SET status='running',last_error=NULL WHERE id::text=$1 AND status='preparing' AND confirmed_at IS NOT NULL AND NOT EXISTS(SELECT 1 FROM tgstrm.group_replications gr WHERE gr.plan_id=p.id AND gr.status='pending') AND NOT EXISTS(SELECT 1 FROM tgstrm.topic_replications tr JOIN tgstrm.group_replications gr ON gr.id=tr.group_replication_id WHERE gr.plan_id=p.id AND NOT tr.excluded AND tr.import_id IS NULL AND tr.status NOT IN ('eligible','empty','excluded_nonvideo'))`
