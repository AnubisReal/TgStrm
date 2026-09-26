package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
)

type mirrorLinkView struct {
	ID                 string       `json:"id"`
	SourceGroupID      string       `json:"source_group_id"`
	SourceGroup        string       `json:"source_group"`
	SourceTopicID      string       `json:"source_topic_id"`
	SourceTopic        string       `json:"source_topic"`
	DestinationGroupID string       `json:"destination_group_id"`
	DestinationGroup   string       `json:"destination_group"`
	DestinationTopicID string       `json:"destination_topic_id"`
	DestinationTopic   string       `json:"destination_topic"`
	Status             string       `json:"status"`
	Synchronized       int          `json:"synchronized"`
	Pending            int          `json:"pending"`
	Failed             int          `json:"failed"`
	LastError          string       `json:"last_error,omitempty"`
	LastMirroredAt     sql.NullTime `json:"-"`
	LastMirrored       string       `json:"last_mirrored_at,omitempty"`
	HistoryMode        string       `json:"history_mode"`
	HistoryStatus      string       `json:"history_status"`
	HistoryTotal       int          `json:"history_total"`
	HistoryCompleted   int          `json:"history_completed"`
	HistoryError       string       `json:"history_error,omitempty"`
}

func (a *API) mirrorLinks(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		a.createMirrorLink(w, r)
		return
	}
	rows, err := a.db.QueryContext(r.Context(), `SELECT ml.id::text,sc.id::text,sc.title,st.id::text,st.title,
		dc.id::text,dc.title,dt.id::text,dt.title,ml.status,
		count(md.id) FILTER(WHERE md.status='completed' AND source.is_active AND EXISTS(
			SELECT 1 FROM tgstrm.messages target
			WHERE target.topic_id=ml.destination_topic_id AND target.is_active
			AND (target.id=md.destination_message_id OR
				(md.destination_telegram_message_id IS NOT NULL AND target.telegram_message_id=md.destination_telegram_message_id))
		)),count(md.id) FILTER(WHERE md.status IN ('pending','sending')),
		count(md.id) FILTER(WHERE md.status='failed'),COALESCE(ml.last_error,''),ml.last_mirrored_at,
		ml.history_mode,ml.history_status,ml.history_total,ml.history_completed,COALESCE(ml.history_last_error,'')
		FROM tgstrm.mirror_links ml
		JOIN tgstrm.topics st ON st.id=ml.source_topic_id JOIN tgstrm.chats sc ON sc.id=st.chat_id
		JOIN tgstrm.topics dt ON dt.id=ml.destination_topic_id JOIN tgstrm.chats dc ON dc.id=dt.chat_id
		LEFT JOIN tgstrm.mirror_deliveries md ON md.link_id=ml.id
		LEFT JOIN tgstrm.messages source ON source.id=md.source_message_id
		GROUP BY ml.id,sc.id,st.id,dc.id,dt.id ORDER BY ml.created_at DESC`)
	if err != nil {
		fail(w, 500, "mirror_list_failed", "No se pudieron cargar los vínculos.")
		return
	}
	defer rows.Close()
	items := []mirrorLinkView{}
	for rows.Next() {
		var v mirrorLinkView
		if err = rows.Scan(&v.ID, &v.SourceGroupID, &v.SourceGroup, &v.SourceTopicID, &v.SourceTopic, &v.DestinationGroupID, &v.DestinationGroup, &v.DestinationTopicID, &v.DestinationTopic, &v.Status, &v.Synchronized, &v.Pending, &v.Failed, &v.LastError, &v.LastMirroredAt, &v.HistoryMode, &v.HistoryStatus, &v.HistoryTotal, &v.HistoryCompleted, &v.HistoryError); err != nil {
			fail(w, 500, "mirror_list_failed", "No se pudieron cargar los vínculos.")
			return
		}
		if v.LastMirroredAt.Valid {
			v.LastMirrored = v.LastMirroredAt.Time.Format("2006-01-02T15:04:05Z07:00")
		}
		items = append(items, v)
	}
	writeJSON(w, 200, map[string]any{"links": items})
}

func (a *API) createMirrorLink(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SourceTopicID      string `json:"source_topic_id"`
		DestinationTopicID string `json:"destination_topic_id"`
		HistoryMode        string `json:"history_mode"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2048)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(&in) != nil || in.SourceTopicID == "" || in.DestinationTopicID == "" {
		fail(w, 400, "invalid_request", "Selecciona los dos topics.")
		return
	}
	if in.HistoryMode == "" {
		in.HistoryMode = "new_only"
	}
	if in.HistoryMode != "new_only" && in.HistoryMode != "include_existing" {
		fail(w, 422, "invalid_history_mode", "Selecciona si quieres copiar el historial existente.")
		return
	}
	var sourceChat, destinationChat, activeChat string
	err := a.db.QueryRowContext(r.Context(), `SELECT st.chat_id::text,dt.chat_id::text,COALESCE((SELECT value #>> '{}' FROM tgstrm.settings WHERE key='active_chat_id'),'')
		FROM tgstrm.topics st,tgstrm.topics dt WHERE st.id::text=$1 AND dt.id::text=$2 AND st.is_active AND dt.is_active`, in.SourceTopicID, in.DestinationTopicID).Scan(&sourceChat, &destinationChat, &activeChat)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "topic_not_found", "Alguno de los topics ya no está disponible.")
		return
	}
	if err != nil {
		fail(w, 500, "mirror_create_failed", "No se pudo validar el vínculo.")
		return
	}
	if sourceChat == destinationChat || destinationChat != activeChat {
		fail(w, 422, "invalid_mirror", "El origen debe ser externo y el destino debe pertenecer al grupo principal.")
		return
	}
	var id string
	err = a.db.QueryRowContext(r.Context(), `INSERT INTO tgstrm.mirror_links(source_topic_id,destination_topic_id,start_after_message_id,history_mode,history_status)
		SELECT $1::bigint,$2::bigint,CASE WHEN $3='include_existing' THEN 0 ELSE GREATEST(COALESCE(t.top_message_id,0),COALESCE(max(m.telegram_message_id),0)) END,
		$3,CASE WHEN $3='include_existing' THEN 'pending' ELSE 'not_requested' END
		FROM tgstrm.topics t LEFT JOIN tgstrm.messages m ON m.topic_id=t.id WHERE t.id=$1::bigint GROUP BY t.id
		ON CONFLICT(source_topic_id,destination_topic_id) DO UPDATE SET status='active',last_error=NULL,
		history_mode=EXCLUDED.history_mode,history_status=CASE WHEN EXCLUDED.history_mode='include_existing' AND tgstrm.mirror_links.history_status<>'completed' THEN 'pending' ELSE tgstrm.mirror_links.history_status END,
		start_after_message_id=CASE WHEN EXCLUDED.history_mode='include_existing' THEN 0 ELSE tgstrm.mirror_links.start_after_message_id END,
		history_last_error=NULL,history_next_attempt_at=now(),updated_at=now() RETURNING id::text`, in.SourceTopicID, in.DestinationTopicID, in.HistoryMode).Scan(&id)
	if err != nil {
		fail(w, 409, "mirror_conflict", "Ese destino ya está vinculado o el vínculo ya existe.")
		return
	}
	if service := a.telegramService(); service != nil {
		service.WakeMirrorWorker()
	}
	writeJSON(w, 201, map[string]string{"id": id, "status": "active"})
}

func (a *API) mirrorLink(w http.ResponseWriter, r *http.Request) {
	var status string
	switch r.Method {
	case http.MethodPatch:
		var in struct {
			Status string `json:"status"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 512)
		if json.NewDecoder(r.Body).Decode(&in) != nil || (in.Status != "active" && in.Status != "paused") {
			fail(w, 400, "invalid_status", "Estado de espejo no válido.")
			return
		}
		err := a.db.QueryRowContext(r.Context(), `UPDATE tgstrm.mirror_links SET status=$2,last_error=NULL,updated_at=now() WHERE id::text=$1 RETURNING status`, r.PathValue("linkID"), in.Status).Scan(&status)
		if errors.Is(err, sql.ErrNoRows) {
			fail(w, 404, "mirror_not_found", "El vínculo no existe.")
			return
		}
		if err != nil {
			fail(w, 409, "mirror_conflict", "No se pudo activar ese vínculo.")
			return
		}
		writeJSON(w, 200, map[string]string{"status": status})
	case http.MethodDelete:
		result, err := a.db.ExecContext(r.Context(), `DELETE FROM tgstrm.mirror_links WHERE id::text=$1`, r.PathValue("linkID"))
		if err != nil {
			fail(w, 500, "mirror_delete_failed", "No se pudo eliminar el vínculo.")
			return
		}
		n, _ := result.RowsAffected()
		if n == 0 {
			fail(w, 404, "mirror_not_found", "El vínculo no existe.")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
