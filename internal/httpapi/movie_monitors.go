package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type movieMonitorInput struct {
	SourceGroupID      string `json:"source_group_id"`
	SourceTopicID      string `json:"source_topic_id"`
	DestinationGroupID string `json:"destination_group_id"`
	DestinationTopicID string `json:"destination_topic_id"`
	HistoryMode        string `json:"history_mode"`
}

func (a *API) createMovieMonitor(ctx context.Context, in movieMonitorInput) (string, error) {
	if in.HistoryMode == "" {
		in.HistoryMode = "new_only"
	}
	if in.HistoryMode != "new_only" && in.HistoryMode != "include_existing" {
		return "", errors.New("selecciona un modo de historial válido")
	}
	if _, err := strconv.ParseInt(in.SourceGroupID, 10, 64); err != nil {
		return "", errors.New("selecciona un grupo origen")
	}
	var sourceForum bool
	if err := a.db.QueryRowContext(ctx, `SELECT is_forum FROM tgstrm.chats WHERE id::text=$1 AND is_active AND telegram_peer_type='channel'`, in.SourceGroupID).Scan(&sourceForum); err != nil {
		return "", errors.New("el grupo origen no está disponible")
	}
	service := a.telegramService()
	if service == nil {
		return "", errors.New("Telegram no está disponible")
	}
	if _, err := service.Group(ctx, in.SourceGroupID); err != nil {
		return "", errors.New("el origen no pertenece a la sesión de Telegram activa")
	}
	if in.SourceTopicID != "" {
		var valid bool
		if err := a.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tgstrm.topics WHERE id::text=$1 AND chat_id::text=$2 AND is_active)`, in.SourceTopicID, in.SourceGroupID).Scan(&valid); err != nil || !valid {
			return "", errors.New("el topic no pertenece al grupo origen")
		}
	}
	if !sourceForum {
		var err error
		in.SourceTopicID, err = service.EnsureWholeGroupTopic(ctx, in.SourceGroupID, false)
		if err != nil {
			return "", err
		}
	}
	if in.DestinationGroupID == "" {
		var mode, group, collection string
		if err := a.db.QueryRowContext(ctx, `SELECT mode,COALESCE(group_chat_id::text,''),COALESCE(collection_id::text,'') FROM tgstrm.catalog_targets LIMIT 1`).Scan(&mode, &group, &collection); err != nil {
			return "", errors.New("configura la biblioteca principal")
		}
		if mode == "collection" {
			var err error
			in.DestinationGroupID, err = service.EnsureMovieCollectionGroup(ctx, collection)
			if err != nil {
				return "", err
			}
			if err = service.EnsureCollectionFolder(ctx, collection); err != nil {
				return "", err
			}
		} else {
			in.DestinationGroupID = group
		}
	}
	if in.SourceGroupID == in.DestinationGroupID {
		return "", errors.New("el origen y el destino deben ser grupos distintos")
	}
	var destForum bool
	if err := a.db.QueryRowContext(ctx, `SELECT is_forum FROM tgstrm.managed_catalog_chats WHERE id::text=$1 AND is_active`, in.DestinationGroupID).Scan(&destForum); err != nil {
		return "", errors.New("el destino debe pertenecer a tu biblioteca")
	}
	if !destForum {
		var err error
		in.DestinationTopicID, err = service.EnsureWholeGroupTopic(ctx, in.DestinationGroupID, true)
		if err != nil {
			return "", err
		}
	} else if in.DestinationTopicID == "" {
		if err := a.db.QueryRowContext(ctx, `SELECT min(id)::text FROM tgstrm.topics WHERE chat_id::text=$1 AND is_active AND library_kind='movie_collection' HAVING count(*)=1`, in.DestinationGroupID).Scan(&in.DestinationTopicID); err != nil {
			return "", errors.New("selecciona el topic de películas del destino")
		}
	}
	var valid bool
	if err := a.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tgstrm.topics WHERE id::text=$1 AND chat_id::text=$2 AND is_active AND library_kind='movie_collection')`, in.DestinationTopicID, in.DestinationGroupID).Scan(&valid); err != nil || !valid {
		return "", errors.New("marca el topic destino como películas")
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	// Lock the monitor graph across creation to reject cycles and overlapping source scopes.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('movie-monitor-routes'))`); err != nil {
		return "", err
	}
	if err = tx.QueryRowContext(ctx, `WITH RECURSIVE routes AS (SELECT destination_chat_id FROM tgstrm.movie_monitors WHERE source_chat_id=$1::bigint UNION SELECT m.destination_chat_id FROM tgstrm.movie_monitors m JOIN routes r ON r.destination_chat_id=m.source_chat_id) SELECT EXISTS(SELECT 1 FROM routes WHERE destination_chat_id=$2::bigint)`, in.DestinationGroupID, in.SourceGroupID).Scan(&valid); err != nil {
		return "", err
	}
	if valid {
		return "", errors.New("este vínculo crearía un ciclo de reenvíos")
	}
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tgstrm.movie_monitors WHERE source_chat_id=$1::bigint AND (source_topic_id IS NULL OR NULLIF($2,'')::bigint IS NULL OR source_topic_id=NULLIF($2,'')::bigint))`, in.SourceGroupID, in.SourceTopicID).Scan(&valid); err != nil {
		return "", err
	}
	if valid {
		return "", errors.New("este origen ya está monitorizado; utiliza su vínculo existente")
	}
	boundary := 0
	if in.HistoryMode == "new_only" {
		// Establish an authoritative boundary without importing old messages.
		if in.SourceTopicID == "" {
			if err = service.RefreshTopics(ctx, in.SourceGroupID); err != nil {
				return "", err
			}
		} else {
			_, high, readErr := service.ReadMovieScopePage(ctx, in.SourceGroupID, in.SourceTopicID, 0)
			if readErr != nil {
				return "", readErr
			}
			boundary = high
		}
	}
	var id string
	err = tx.QueryRowContext(ctx, `INSERT INTO tgstrm.movie_monitors(source_chat_id,source_topic_id,destination_chat_id,destination_topic_id,status,history_mode,start_after_message_id) VALUES($1::bigint,NULLIF($2,'')::bigint,$3::bigint,$4::bigint,'active',$5,$6) RETURNING id::text`, in.SourceGroupID, in.SourceTopicID, in.DestinationGroupID, in.DestinationTopicID, in.HistoryMode, boundary).Scan(&id)
	if err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload) VALUES('mirror.progress','group',$1::bigint,jsonb_build_object('movie_monitor_id',$2::text))`, in.SourceGroupID, id); err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload) VALUES('library.collection.changed','group',$1::bigint,jsonb_build_object('group_id',$1::text))`, in.DestinationGroupID); err != nil {
		return "", err
	}
	return id, tx.Commit()
}

func (a *API) movieMonitors(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		var in movieMonitorInput
		if !decodeLibraryRequest(w, r, &in) {
			return
		}
		id, err := a.createMovieMonitor(r.Context(), in)
		if err != nil {
			fail(w, 422, "movie_monitor_invalid", err.Error())
			return
		}
		writeJSON(w, 201, map[string]any{"id": id})
		return
	}
	rows, err := a.db.QueryContext(r.Context(), `SELECT mm.id::text,sc.id::text,sc.title,COALESCE(st.id::text,''),CASE WHEN st.telegram_topic_id=0 OR st.id IS NULL THEN 'Todo el grupo' ELSE st.title END,dc.id::text,dc.title,dt.id::text,dt.title,mm.status,mm.history_mode,COALESCE(mm.last_error,(SELECT failed.last_error FROM tgstrm.movie_monitor_deliveries failed WHERE failed.monitor_id=mm.id AND failed.status='failed' ORDER BY failed.updated_at DESC LIMIT 1),''),count(d.id),count(d.id) FILTER(WHERE d.status='completed' AND EXISTS(SELECT 1 FROM tgstrm.messages m WHERE m.id=d.destination_message_id AND m.is_active AND EXISTS(SELECT 1 FROM tgstrm.stream_assets sa JOIN tgstrm.stream_exports e ON e.asset_id=sa.id WHERE sa.message_id=m.id AND e.status='written'))),count(d.id) FILTER(WHERE d.status='review'),count(d.id) FILTER(WHERE d.status NOT IN ('review','completed','excluded')),GREATEST(COALESCE(min(d.next_attempt_at) FILTER(WHERE d.status='failed'),now()),COALESCE((SELECT max(s.next_scan_at) FROM tgstrm.movie_monitor_scopes s WHERE s.monitor_id=mm.id AND s.last_error IS NOT NULL),now())),EXISTS(SELECT 1 FROM tgstrm.movie_monitor_scopes s WHERE s.monitor_id=mm.id AND NOT s.initial_scan_complete) FROM tgstrm.movie_monitors mm JOIN tgstrm.chats sc ON sc.id=mm.source_chat_id LEFT JOIN tgstrm.topics st ON st.id=mm.source_topic_id JOIN tgstrm.chats dc ON dc.id=mm.destination_chat_id JOIN tgstrm.topics dt ON dt.id=mm.destination_topic_id LEFT JOIN tgstrm.movie_monitor_deliveries d ON d.monitor_id=mm.id GROUP BY mm.id,sc.id,st.id,dc.id,dt.id ORDER BY mm.created_at DESC`)
	if err != nil {
		fail(w, 500, "movie_monitor_list_failed", "No se pudieron cargar las películas monitorizadas.")
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, sg, source, st, scope, dg, dest, dt, title, status, history, last string
		var total, completed, review, pending int
		var next time.Time
		var scanning bool
		if err = rows.Scan(&id, &sg, &source, &st, &scope, &dg, &dest, &dt, &title, &status, &history, &last, &total, &completed, &review, &pending, &next, &scanning); err != nil {
			fail(w, 500, "movie_monitor_list_failed", err.Error())
			return
		}
		items = append(items, map[string]any{"id": id, "source_group_id": sg, "source_group": source, "source_topic_id": st, "source_topic": scope, "destination_group_id": dg, "destination_group": dest, "destination_topic_id": dt, "status": status, "history_mode": history, "last_error": last, "total": total, "completed": completed, "review": review, "pending": pending, "next_attempt_at": next, "scanning": scanning})
	}
	if err = rows.Err(); err != nil {
		fail(w, 500, "movie_monitor_list_failed", "No se pudieron completar los vínculos.")
		return
	}
	writeJSON(w, 200, map[string]any{"monitors": items})
}
func (a *API) movieMonitor(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Status string `json:"status"`
	}
	if !decodeLibraryRequest(w, r, &in) {
		return
	}
	if in.Status != "active" && in.Status != "paused" {
		fail(w, 422, "invalid_status", "Elige activar o pausar.")
		return
	}
	result, err := a.db.ExecContext(r.Context(), `UPDATE tgstrm.movie_monitors SET status=$2,updated_at=now() WHERE id::text=$1`, r.PathValue("monitorID"), in.Status)
	if err != nil {
		fail(w, 500, "update_failed", "No se pudo cambiar el vínculo.")
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		fail(w, 404, "not_found", "Vínculo no encontrado.")
		return
	}
	writeJSON(w, 200, map[string]any{"status": in.Status})
}

func (a *API) movieMonitorReview(w http.ResponseWriter, r *http.Request) {
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 || offset > 1000000 {
		fail(w, 422, "invalid_offset", "Página no válida.")
		return
	}
	rows, err := a.db.QueryContext(r.Context(), `SELECT d.id::text,m.id::text,COALESCE(mm.file_name,''),COALESCE(m.body,''),COALESCE(d.last_error,''),d.status FROM tgstrm.movie_monitor_deliveries d JOIN tgstrm.messages m ON m.id=d.source_message_id JOIN tgstrm.message_media mm ON mm.message_id=m.id WHERE d.monitor_id::text=$1 AND d.status IN ('review','failed') ORDER BY m.telegram_message_id,d.id LIMIT 51 OFFSET $2`, r.PathValue("monitorID"), offset)
	if err != nil {
		fail(w, 500, "review_failed", "No se pudo cargar la revisión.")
		return
	}
	defer rows.Close()
	items := []map[string]string{}
	for rows.Next() {
		var id, msg, file, body, last, status string
		if err = rows.Scan(&id, &msg, &file, &body, &last, &status); err != nil {
			fail(w, 500, "review_failed", err.Error())
			return
		}
		items = append(items, map[string]string{"id": id, "message_id": msg, "file_name": file, "caption": body, "last_error": last, "status": status})
	}
	if err = rows.Err(); err != nil {
		fail(w, 500, "review_failed", "No se pudo completar la revisión.")
		return
	}
	hasMore := len(items) > 50
	if hasMore {
		items = items[:50]
	}
	writeJSON(w, 200, map[string]any{"items": items, "has_more": hasMore, "next_offset": offset + len(items)})
}
func (a *API) confirmMovieMonitorMatch(w http.ResponseWriter, r *http.Request) {
	var in struct {
		TMDBID int64 `json:"tmdb_id"`
	}
	if !decodeLibraryRequest(w, r, &in) {
		return
	}
	if in.TMDBID <= 0 {
		fail(w, 422, "invalid_match", "Selecciona una película.")
		return
	}
	match, err := a.tmdbMovieDetails(r.Context(), in.TMDBID)
	if err != nil {
		fail(w, 502, "tmdb_failed", "TMDB no está disponible; no se ha cambiado la coincidencia.")
		return
	}
	if err = validateMonitoredMovieMatch(match); err != nil {
		fail(w, 422, "invalid_match", err.Error())
		return
	}
	payload, _ := json.Marshal(match)
	result, err := a.db.ExecContext(r.Context(), `UPDATE tgstrm.movie_monitor_deliveries SET status='ready',last_stage='ready',tmdb_id=$3,match_payload=$4,confirmed_at=now(),approved_signature=source_signature,last_error=NULL,next_attempt_at=now(),updated_at=now() WHERE id::text=$2 AND monitor_id::text=$1 AND status='review' AND lease_token IS NULL AND source_signature=(SELECT encode(source_signature,'hex') FROM tgstrm.stream_assets WHERE message_id=tgstrm.movie_monitor_deliveries.source_message_id)`, r.PathValue("monitorID"), r.PathValue("deliveryID"), in.TMDBID, payload)
	if err != nil {
		fail(w, 500, "match_failed", "No se pudo guardar la coincidencia.")
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		fail(w, 409, "review_changed", "El archivo ya no está pendiente; actualiza la revisión.")
		return
	}
	a.publishMovieMonitor(r.Context(), r.PathValue("monitorID"))
	writeJSON(w, 200, map[string]any{"status": "ready"})
}
func validateMonitoredMovieMatch(m tmdbResult) error {
	if m.ID <= 0 || strings.TrimSpace(m.Title) == "" || len(m.ReleaseDate) != 10 || m.PosterPath == "" {
		return errors.New("La película requiere título, año y póster; revisa su ficha TMDB.")
	}
	if _, err := time.Parse("2006-01-02", m.ReleaseDate); err != nil {
		return errors.New("La película requiere una fecha válida en TMDB.")
	}
	return nil
}
func movieCaption(m tmdbResult) string {
	return strings.TrimSpace(m.Title) + " (" + m.ReleaseDate[:4] + ")"
}
func (a *API) publishMovieMonitor(ctx context.Context, id string) {
	_, _ = a.db.ExecContext(ctx, `INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload) SELECT 'mirror.progress','group',source_chat_id,jsonb_build_object('movie_monitor_id',id::text) FROM tgstrm.movie_monitors WHERE id::text=$1`, id)
}

func (a *API) tmdbMovieDetails(ctx context.Context, id int64) (tmdbResult, error) {
	var result tmdbResult
	err := a.tmdbRequestContext(ctx, "movie/"+strconv.FormatInt(id, 10), url.Values{"language": {"es-ES"}}, &result)
	return result, err
}
