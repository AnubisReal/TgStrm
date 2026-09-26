package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"
)

type activityChild struct {
	ID       string    `json:"id"`
	Status   string    `json:"status"`
	Detail   string    `json:"detail"`
	Occurred time.Time `json:"occurred_at"`
}

type activityItem struct {
	ID       string          `json:"id"`
	Kind     string          `json:"kind"`
	Status   string          `json:"status"`
	Title    string          `json:"title"`
	Detail   string          `json:"detail"`
	Occurred time.Time       `json:"occurred_at"`
	Href     string          `json:"href,omitempty"`
	Children []activityChild `json:"children,omitempty"`
}

func (a *API) activity(w http.ResponseWriter, r *http.Request) {
	scope := "activity"
	if r.URL.Query().Get("scope") == "notifications" {
		scope = "notifications"
	}
	clearedAt := time.Unix(0, 0)
	var raw []byte
	err := a.db.QueryRowContext(r.Context(), `SELECT value FROM tgstrm.settings WHERE key=$1`, scope+"_cleared_at").Scan(&raw)
	if err == nil {
		var stored string
		if json.Unmarshal(raw, &stored) == nil {
			if parsed, parseErr := time.Parse(time.RFC3339Nano, stored); parseErr == nil {
				clearedAt = parsed
			}
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		fail(w, http.StatusInternalServerError, "activity_failed", "No se pudo cargar la actividad.")
		return
	}
	rows, err := a.db.QueryContext(r.Context(), `
			SELECT id,kind,status,title,detail,occurred_at,href,children FROM (
			 SELECT ae.id::text,ae.kind,ae.status,ae.title,ae.detail,ae.occurred_at,
			   CASE WHEN ae.group_id IS NOT NULL THEN concat('/groups/',ae.group_id) ELSE '/activity' END AS href,'[]'::jsonb AS children
			 FROM tgstrm.activity_events ae
			 UNION ALL
			 SELECT ci.id::text AS id,'import' AS kind,ci.status,st.title AS title,
			   concat(ci.completed_count,' de ',ci.requested_count,' archivos',CASE WHEN ci.last_error IS NOT NULL THEN ' · '||ci.last_error ELSE '' END) AS detail,
			   ci.updated_at AS occurred_at,
			   concat('/groups/',sc.id,'/topics/',st.id,'/manage') AS href,'[]'::jsonb AS children
			 FROM tgstrm.catalog_imports ci JOIN tgstrm.topics st ON st.id=ci.source_topic_id
			 JOIN tgstrm.chats sc ON sc.id=st.chat_id
			 UNION ALL
			 SELECT ml.id::text,'mirror',
			   CASE WHEN ml.history_status='failed' OR EXISTS(SELECT 1 FROM tgstrm.mirror_deliveries failed WHERE failed.link_id=ml.id AND (failed.status='failed' OR failed.edit_status='failed')) THEN 'error'
			     WHEN ml.history_status IN ('pending','indexing','copying') OR EXISTS(SELECT 1 FROM tgstrm.mirror_deliveries pending WHERE pending.link_id=ml.id AND (pending.status IN ('pending','sending') OR pending.edit_status IN ('pending','editing'))) THEN 'pending'
			     ELSE 'completed' END,
			   concat(st.title,' → ',dt.title),
			   CASE WHEN ml.history_mode='include_existing' THEN
			     CASE WHEN ml.history_status='completed' THEN concat('Historial sincronizado · ',ml.history_completed,' archivos · seguimiento en tiempo real')
			       WHEN ml.history_status='indexing' THEN 'Leyendo el historial completo desde Telegram'
			       WHEN ml.history_status='failed' THEN concat('No se pudo completar el historial · ',COALESCE(ml.history_last_error,ml.last_error,'requiere revisión'))
			       ELSE concat(ml.history_completed,' de ',ml.history_total,' archivos históricos') END
			     ELSE concat('Espejo en tiempo real · ',(SELECT count(*) FROM tgstrm.mirror_deliveries delivered WHERE delivered.link_id=ml.id AND delivered.status='completed'),' archivos sincronizados') END,
			   GREATEST(ml.updated_at,COALESCE((SELECT max(changed.updated_at) FROM tgstrm.mirror_deliveries changed WHERE changed.link_id=ml.id),ml.updated_at)),
			   '/mirror/manage',
			   COALESCE((SELECT jsonb_agg(jsonb_build_object('id',details.id,'status',details.child_status,'detail',details.detail,'occurred_at',details.updated_at)
			     ORDER BY (details.child_status='error') DESC,details.updated_at DESC) FROM (
			       SELECT md.id::text id,CASE WHEN md.status='failed' OR md.edit_status='failed' THEN 'error' WHEN md.status='completed' AND md.edit_status='idle' THEN 'completed' ELSE 'pending' END child_status,
			       concat(COALESCE(mm.file_name,'Archivo de Telegram'),CASE WHEN COALESCE(md.edit_last_error,md.last_error) IS NULL THEN '' ELSE ' · '||COALESCE(md.edit_last_error,md.last_error) END) detail,md.updated_at
			       FROM tgstrm.mirror_deliveries md JOIN tgstrm.messages source ON source.id=md.source_message_id
			       LEFT JOIN tgstrm.message_media mm ON mm.message_id=source.id WHERE md.link_id=ml.id
			       ORDER BY (md.status='failed' OR md.edit_status='failed') DESC,source.telegram_message_id LIMIT 250
			     ) details),'[]'::jsonb)
			 FROM tgstrm.mirror_links ml JOIN tgstrm.topics st ON st.id=ml.source_topic_id JOIN tgstrm.topics dt ON dt.id=ml.destination_topic_id
			 UNION ALL
			 SELECT concat('topic-',t.id),'strm',
			   CASE WHEN bool_or(e.status IN ('error','failed','missing')) THEN 'error' WHEN bool_or(e.status='pending') THEN 'pending' ELSE 'written' END,
			   t.title,
			   CASE
			     WHEN count(*) FILTER (WHERE e.status IN ('error','failed','missing')) > 0 THEN
			       concat('Sincronización incompleta · ',count(*) FILTER (WHERE e.status='written'),' STRM disponibles · ',count(*) FILTER (WHERE e.status IN ('error','failed','missing')),' con error')
			     WHEN count(*) FILTER (WHERE e.status='pending') > 0 THEN
			       concat('Sincronización en curso · ',count(*) FILTER (WHERE e.status='written'),' STRM disponibles · ',count(*) FILTER (WHERE e.status='pending'),' pendientes')
			     ELSE concat('Biblioteca sincronizada · ',count(*),' archivos STRM disponibles')
			   END,
			   max(e.updated_at),
			   concat('/groups/',c.id,'/topics/',t.id,'/manage'),
			   jsonb_agg(jsonb_build_object('id',e.id::text,'status',e.status,'detail',e.relative_path || CASE WHEN e.last_error IS NOT NULL THEN ' · '||e.last_error ELSE '' END,'occurred_at',e.updated_at) ORDER BY (e.status IN ('error','failed','missing')) DESC,e.updated_at DESC)
			 FROM tgstrm.stream_exports e JOIN tgstrm.stream_assets sa ON sa.id=e.asset_id
			 JOIN tgstrm.messages m ON m.id=sa.message_id JOIN tgstrm.topics t ON t.id=m.topic_id JOIN tgstrm.chats c ON c.id=t.chat_id
			 WHERE m.is_active AND t.is_active AND c.is_active
			 GROUP BY t.id,t.title,c.id
			 UNION ALL
			 SELECT concat('review-',t.id),'review','pending',t.title,concat(count(*),' archivos necesitan revisión'),max(sa.updated_at),
			   concat('/groups/',c.id,'/topics/',t.id,'/manage'),
			   jsonb_agg(jsonb_build_object('id',sa.id::text,'status','pending','detail',sa.last_export_error,'occurred_at',sa.updated_at) ORDER BY sa.updated_at DESC)
			 FROM tgstrm.stream_assets sa JOIN tgstrm.messages m ON m.id=sa.message_id
			 JOIN tgstrm.topics t ON t.id=m.topic_id JOIN tgstrm.chats c ON c.id=t.chat_id
			 WHERE sa.last_export_error IS NOT NULL AND m.is_active AND t.is_active AND c.is_active
			 GROUP BY t.id,t.title,c.id
			) activity WHERE occurred_at > $1 ORDER BY occurred_at DESC LIMIT 100`, clearedAt)
	if err != nil {
		log.Printf("No se pudo consultar la actividad: %v", err)
		fail(w, http.StatusInternalServerError, "activity_failed", "No se pudo cargar la actividad.")
		return
	}
	defer rows.Close()
	items := []activityItem{}
	for rows.Next() {
		var item activityItem
		var children []byte
		if rows.Scan(&item.ID, &item.Kind, &item.Status, &item.Title, &item.Detail, &item.Occurred, &item.Href, &children) == nil {
			_ = json.Unmarshal(children, &item.Children)
			items = append(items, item)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *API) clearActivity(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Scope string `json:"scope"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&input) != nil || (input.Scope != "activity" && input.Scope != "notifications") {
		fail(w, http.StatusBadRequest, "invalid_scope", "Selecciona qué historial quieres limpiar.")
		return
	}
	clearedAt := time.Now().UTC()
	raw, _ := json.Marshal(clearedAt.Format(time.RFC3339Nano))
	if _, err := a.db.ExecContext(r.Context(), `INSERT INTO tgstrm.settings(key,value) VALUES($1,$2)
		ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value,revision=tgstrm.settings.revision+1,updated_at=now()`, input.Scope+"_cleared_at", raw); err != nil {
		fail(w, http.StatusInternalServerError, "activity_clear_failed", "No se pudo limpiar el historial.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "cleared", "scope": input.Scope, "cleared_at": clearedAt})
}
