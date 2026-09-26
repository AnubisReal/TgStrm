package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type healthComponent struct {
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type systemHealthPayload struct {
	CheckedAt     time.Time       `json:"checked_at"`
	Database      healthComponent `json:"database"`
	Telegram      healthComponent `json:"telegram"`
	Realtime      healthComponent `json:"realtime"`
	Mirror        healthComponent `json:"mirror"`
	Storage       healthComponent `json:"storage"`
	LastEventAt   *time.Time      `json:"last_event_at"`
	LastSyncAt    *time.Time      `json:"last_sync_at"`
	MirrorActive  int             `json:"mirror_active"`
	MirrorPending int             `json:"mirror_pending"`
	MirrorFailed  int             `json:"mirror_failed"`
	PendingEvents int             `json:"pending_events"`
	MoviesPath    string          `json:"movies_path"`
	ShowsPath     string          `json:"shows_path"`
}

func (a *API) systemHealth(w http.ResponseWriter, r *http.Request) {
	result := systemHealthPayload{CheckedAt: time.Now(), Database: healthComponent{Status: "error", Detail: "PostgreSQL no está disponible."}}
	if a.db == nil {
		writeJSON(w, http.StatusOK, result)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	started := time.Now()
	if err := a.db.PingContext(ctx); err != nil {
		writeJSON(w, http.StatusOK, result)
		return
	}
	result.Database = healthComponent{Status: "ok", Detail: "PostgreSQL responde en " + time.Since(started).Round(time.Millisecond).String() + "."}

	if a.telegram != nil {
		if profile, err := a.telegram.Profile(ctx); err == nil && profile.Connected {
			result.Telegram = healthComponent{Status: "ok", Detail: "Sesión MTProto conectada."}
			result.Realtime = healthComponent{Status: "ok", Detail: "El receptor de actualizaciones está activo."}
		} else {
			result.Telegram = healthComponent{Status: "error", Detail: "La sesión de Telegram está desconectada."}
			result.Realtime = healthComponent{Status: "warning", Detail: "Las actualizaciones se recuperarán al reconectar."}
		}
	} else {
		result.Telegram = healthComponent{Status: "error", Detail: "Telegram no está configurado."}
		result.Realtime = healthComponent{Status: "warning", Detail: "El receptor de actualizaciones no está iniciado."}
	}

	_ = a.db.QueryRowContext(ctx, `SELECT
		count(DISTINCT ml.id) FILTER (WHERE ml.status='active'),
		count(md.id) FILTER (WHERE md.status IN ('pending','sending')),
		count(md.id) FILTER (WHERE md.status='failed' OR md.edit_status='failed')
	 FROM tgstrm.mirror_links ml
	 LEFT JOIN tgstrm.mirror_deliveries md ON md.link_id=ml.id`).Scan(&result.MirrorActive, &result.MirrorPending, &result.MirrorFailed)
	if result.MirrorFailed > 0 {
		result.Mirror = healthComponent{Status: "error", Detail: "Hay entregas del Espejo que necesitan atención."}
	} else if result.MirrorPending > 0 {
		result.Mirror = healthComponent{Status: "warning", Detail: "El Espejo está procesando entregas pendientes."}
	} else {
		result.Mirror = healthComponent{Status: "ok", Detail: "La cola del Espejo está al día."}
	}

	_ = a.db.QueryRowContext(ctx, `SELECT count(*) FROM tgstrm.outbox_events WHERE delivered_at IS NULL`).Scan(&result.PendingEvents)
	var lastEvent, lastSync sql.NullTime
	_ = a.db.QueryRowContext(ctx, `SELECT GREATEST(
		(SELECT max(updated_at) FROM tgstrm.telegram_update_states),
		(SELECT max(updated_at) FROM tgstrm.telegram_channel_states))`).Scan(&lastEvent)
	_ = a.db.QueryRowContext(ctx, `SELECT max(completed_at) FROM tgstrm.sync_states WHERE status='ready'`).Scan(&lastSync)
	if lastEvent.Valid {
		value := lastEvent.Time
		result.LastEventAt = &value
	}
	if lastSync.Valid {
		value := lastSync.Time
		result.LastSyncAt = &value
	}

	settings := a.mediaSettingsDefaults()
	var raw []byte
	if err := a.db.QueryRowContext(ctx, `SELECT value FROM tgstrm.settings WHERE key='media'`).Scan(&raw); err == nil {
		_ = json.Unmarshal(raw, &settings)
	}
	result.MoviesPath, result.ShowsPath = settings.MoviesPath, settings.ShowsPath
	result.Storage = storageHealth(settings.MoviesPath, settings.ShowsPath)
	writeJSON(w, http.StatusOK, result)
}

func storageHealth(paths ...string) healthComponent {
	for _, path := range paths {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return healthComponent{Status: "error", Detail: "Una ruta multimedia no es válida."}
		}
		info, err := os.Stat(absolute)
		if err != nil {
			if os.IsNotExist(err) {
				continue // The exporter creates configured destinations on first use.
			}
			return healthComponent{Status: "error", Detail: "No se puede acceder a una ruta multimedia."}
		}
		if !info.IsDir() {
			return healthComponent{Status: "error", Detail: "Una ruta multimedia no es una carpeta."}
		}
	}
	return healthComponent{Status: "ok", Detail: "Las rutas multimedia están disponibles."}
}
