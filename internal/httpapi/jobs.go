package httpapi

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"time"
)

// jobItem is a read model over the durable workflows that already exist in
// PostgreSQL. It deliberately does not introduce a second queue or duplicate
// state: imports, mirror deliveries, exports and scans remain their own
// idempotent processes.
type jobItem struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Status    string    `json:"status"`
	Title     string    `json:"title"`
	Detail    string    `json:"detail"`
	UpdatedAt time.Time `json:"updated_at"`
	Href      string    `json:"href,omitempty"`
}

func (a *API) jobs(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		fail(w, http.StatusServiceUnavailable, "database_unavailable", "PostgreSQL no está disponible.")
		return
	}
	rows, err := a.db.QueryContext(r.Context(), `
		SELECT id,kind,status,title,detail,updated_at,href FROM (
			SELECT ci.id::text,'import',ci.status,COALESCE(dt.title,ci.destination_title,st.title),
				concat(ci.completed_count,' de ',ci.requested_count,' archivos',CASE WHEN ci.last_error IS NULL THEN '' ELSE ' · '||ci.last_error END),
				ci.updated_at,concat('/groups/',sc.id,'/topics/',st.id,'/manage')
			FROM tgstrm.catalog_imports ci JOIN tgstrm.topics st ON st.id=ci.source_topic_id AND st.is_active
			JOIN tgstrm.chats sc ON sc.id=st.chat_id AND sc.is_active
			LEFT JOIN tgstrm.topics dt ON dt.id=ci.destination_topic_id AND dt.is_active
			WHERE ci.status IN ('pending','review','running','failed')
			UNION ALL
			SELECT ml.id::text,'mirror',
				CASE WHEN ml.history_status='failed' OR count(md.id) FILTER (WHERE md.status='failed' OR md.edit_status='failed')>0 THEN 'failed' WHEN ml.history_status='pending' THEN 'pending' ELSE 'running' END,
				concat(st.title,' → ',dt.title),
				CASE WHEN ml.history_mode='include_existing' AND ml.history_status IN ('pending','indexing','copying') THEN concat(ml.history_completed,' de ',ml.history_total,' archivos históricos')
				ELSE concat(count(md.id) FILTER (WHERE md.status='completed'),' sincronizados · ',count(md.id) FILTER (WHERE md.status IN ('pending','sending')),' pendientes',CASE WHEN count(md.id) FILTER (WHERE md.status='failed' OR md.edit_status='failed')>0 THEN ' · requiere atención' ELSE '' END) END,
				greatest(ml.updated_at,COALESCE(max(md.updated_at),ml.updated_at)),concat('/mirror/manage')
			FROM tgstrm.mirror_links ml JOIN tgstrm.topics st ON st.id=ml.source_topic_id JOIN tgstrm.topics dt ON dt.id=ml.destination_topic_id
			LEFT JOIN tgstrm.mirror_deliveries md ON md.link_id=ml.id
			WHERE ml.status='active'
			GROUP BY ml.id,st.title,dt.title
			HAVING ml.history_status IN ('pending','indexing','copying','failed') OR count(md.id) FILTER (WHERE md.status='failed' OR md.edit_status='failed')>0
				OR min(md.updated_at) FILTER (WHERE md.status IN ('pending','sending') OR md.edit_status IN ('pending','editing')) < now()-interval '5 seconds'
			UNION ALL
			SELECT concat('strm-',t.id),'strm',
				CASE WHEN count(e.id) FILTER (WHERE e.status IN ('error','missing'))>0 THEN 'failed' ELSE 'running' END,
				t.title,
				concat(count(e.id) FILTER (WHERE e.status='written'),' disponibles · ',count(e.id) FILTER (WHERE e.status='pending'),' pendientes',CASE WHEN count(e.id) FILTER (WHERE e.status IN ('error','missing'))>0 THEN ' · '||count(e.id) FILTER (WHERE e.status IN ('error','missing'))||' con incidencia' ELSE '' END),
				max(e.updated_at),concat('/groups/',c.id,'/topics/',t.id,'/manage')
			FROM tgstrm.stream_exports e JOIN tgstrm.stream_assets sa ON sa.id=e.asset_id JOIN tgstrm.messages m ON m.id=sa.message_id
			JOIN tgstrm.topics t ON t.id=m.topic_id JOIN tgstrm.chats c ON c.id=t.chat_id
			WHERE e.status IN ('pending','error','missing') AND m.is_active AND t.is_active AND c.is_active
			GROUP BY t.id,t.title,c.id
			HAVING count(e.id) FILTER (WHERE e.status IN ('error','missing'))>0
				OR min(e.updated_at) FILTER (WHERE e.status='pending') < now()-interval '5 seconds'
			UNION ALL
			SELECT concat('sync-',ss.scope,'-',ss.resource_id),'scan',ss.status,
				COALESCE(c.title,'Sincronización de Telegram'),
				COALESCE(NULLIF(ss.last_error,''),CASE WHEN ss.status='running' THEN 'Leyendo cambios de Telegram.' ELSE 'La sincronización necesita revisión.' END),
				ss.updated_at,CASE WHEN c.id IS NULL THEN '/home' ELSE concat('/groups/',c.id,'/manage') END
			FROM tgstrm.sync_states ss LEFT JOIN tgstrm.chats c ON c.id=ss.resource_id
			WHERE ss.status='failed' OR (ss.status='running' AND ss.updated_at < now()-interval '5 seconds')
		) jobs(id,kind,status,title,detail,updated_at,href)
		ORDER BY CASE WHEN status='failed' THEN 0 ELSE 1 END,updated_at DESC LIMIT 40`)
	if err != nil {
		fail(w, http.StatusInternalServerError, "jobs_failed", "No se pudieron cargar los trabajos.")
		return
	}
	defer rows.Close()
	items := []jobItem{}
	for rows.Next() {
		var item jobItem
		if err := rows.Scan(&item.ID, &item.Kind, &item.Status, &item.Title, &item.Detail, &item.UpdatedAt, &item.Href); err != nil {
			fail(w, http.StatusInternalServerError, "jobs_failed", "No se pudieron leer los trabajos.")
			return
		}
		items = append(items, item)
	}
	recentRows, err := a.db.QueryContext(r.Context(), `SELECT id,kind,status,title,detail,updated_at,href FROM (
		SELECT ci.id::text id,'import' kind,'completed' status,dt.title,
		concat(ci.completed_count,' de ',ci.requested_count,' archivos enviados'),ci.updated_at,
		concat('/groups/',dc.id,'/topics/',dt.id,'/manage')
		FROM tgstrm.catalog_imports ci
		JOIN tgstrm.topics dt ON dt.id=ci.destination_topic_id AND dt.is_active
		JOIN tgstrm.chats dc ON dc.id=dt.chat_id AND dc.is_active
		WHERE ci.status='completed'
		UNION ALL
		SELECT ml.id::text,'mirror','completed',concat(st.title,' → ',dt.title),
		concat(ml.history_completed,' archivos históricos sincronizados'),ml.updated_at,'/mirror/manage'
		FROM tgstrm.mirror_links ml JOIN tgstrm.topics st ON st.id=ml.source_topic_id JOIN tgstrm.topics dt ON dt.id=ml.destination_topic_id
		WHERE ml.history_mode='include_existing' AND ml.history_status='completed'
	) recent(id,kind,status,title,detail,updated_at,href) ORDER BY updated_at DESC LIMIT 4`)
	if err != nil {
		log.Printf("load recent jobs: %v", err)
		fail(w, http.StatusInternalServerError, "jobs_failed", "No se pudieron cargar los trabajos recientes.")
		return
	}
	defer recentRows.Close()
	recent := []jobItem{}
	for recentRows.Next() {
		var item jobItem
		if err := recentRows.Scan(&item.ID, &item.Kind, &item.Status, &item.Title, &item.Detail, &item.UpdatedAt, &item.Href); err != nil {
			fail(w, http.StatusInternalServerError, "jobs_failed", "No se pudieron leer los trabajos recientes.")
			return
		}
		recent = append(recent, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "recent_completed": recent})
}

func (a *API) retryJob(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("kind") != "import" {
		fail(w, http.StatusNotFound, "job_not_found", "Ese trabajo no admite reintento manual.")
		return
	}
	service := a.telegramService()
	if a.db == nil || service == nil {
		fail(w, http.StatusServiceUnavailable, "telegram_unavailable", "Telegram no está disponible.")
		return
	}
	var sourceTopicID string
	var sourceActive, destinationActive bool
	err := a.db.QueryRowContext(r.Context(), `SELECT ci.source_topic_id::text,st.is_active AND sc.is_active,COALESCE(dt.is_active,false)
		FROM tgstrm.catalog_imports ci
		JOIN tgstrm.topics st ON st.id=ci.source_topic_id JOIN tgstrm.chats sc ON sc.id=st.chat_id
		LEFT JOIN tgstrm.topics dt ON dt.id=ci.destination_topic_id
		WHERE ci.id=$1::uuid AND ci.status='failed'`, r.PathValue("id")).Scan(&sourceTopicID, &sourceActive, &destinationActive)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, http.StatusNotFound, "job_not_retryable", "La importación ya no está disponible para reintentar.")
		return
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, "job_retry_failed", "No se pudo preparar el reintento.")
		return
	}
	if !sourceActive || !destinationActive {
		_, _ = a.db.ExecContext(r.Context(), `UPDATE tgstrm.catalog_imports SET last_error='El topic de origen o el destino anterior ya no está disponible en Telegram.',updated_at=now() WHERE id=$1::uuid`, r.PathValue("id"))
		fail(w, http.StatusConflict, "job_source_unavailable", "No se puede reintentar: el topic de origen o el destino anterior ya no existe en Telegram.")
		return
	}
	if err = service.RetryCatalogImport(r.Context(), r.PathValue("id")); err != nil {
		fail(w, http.StatusConflict, "job_retry_failed", "No se pudo encolar el reintento de forma segura.")
		return
	}
	_, _ = a.db.ExecContext(r.Context(), `INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload)
		VALUES('catalog.import.progress','topic',$1::bigint,jsonb_build_object('import_id',$2,'reason','manual_retry'))`, sourceTopicID, r.PathValue("id"))
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "pending"})
}
