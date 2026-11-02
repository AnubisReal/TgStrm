package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
)

type mediaSettingsPayload struct {
	AmbientEnabled     bool   `json:"ambient_enabled"`
	AmbientIdleMinutes int    `json:"ambient_idle_minutes"`
	AmbientWindow      string `json:"ambient_window"`
	PublicBaseURL      string `json:"public_base_url"`
	MoviesPath         string `json:"movies_path"`
	ShowsPath          string `json:"shows_path"`
	SeriesNaming       string `json:"series_naming"`
	IncludeTMDBID      bool   `json:"include_tmdb_id"`
	TMDBToken          string `json:"tmdb_token,omitempty"`
	TMDBConfigured     bool   `json:"tmdb_configured"`
	TMDBVerified       bool   `json:"tmdb_verified"`
	CacheLimitMB       int64  `json:"cache_limit_mb"`
	CacheUsedBytes     int64  `json:"cache_used_bytes"`
	CacheEntries       int    `json:"cache_entries"`
	NotificationLevel  string `json:"notification_level"`
}

func (a *API) mediaSettingsDefaults() mediaSettingsPayload {
	return mediaSettingsPayload{
		MoviesPath:         filepath.Join(a.store.Dir(), "library", "movies"),
		ShowsPath:          filepath.Join(a.store.Dir(), "library", "shows"),
		SeriesNaming:       "universal",
		AmbientIdleMinutes: 5, AmbientWindow: "week",
		CacheLimitMB:      512,
		NotificationLevel: "balanced",
	}
}

func (a *API) mediaSettings(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		fail(w, http.StatusServiceUnavailable, "database_unavailable", "La base de datos no está disponible.")
		return
	}
	if r.Method == http.MethodGet {
		payload := a.mediaSettingsDefaults()
		var raw []byte
		err := a.db.QueryRowContext(r.Context(), `SELECT value FROM tgstrm.settings WHERE key='media'`).Scan(&raw)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			fail(w, 500, "settings_failed", "No se pudo leer la configuración.")
			return
		}
		if len(raw) > 0 && json.Unmarshal(raw, &payload) != nil {
			fail(w, 500, "settings_invalid", "La configuración guardada no es válida.")
			return
		}
		if payload.CacheLimitMB <= 0 {
			payload.CacheLimitMB = 512
		}
		if payload.SeriesNaming == "" {
			payload.SeriesNaming = "universal"
		}
		if payload.NotificationLevel == "" {
			payload.NotificationLevel = "balanced"
		}
		if service := a.telegramService(); service != nil {
			stats := service.PersistentCacheStats()
			payload.CacheUsedBytes, payload.CacheEntries = stats.UsedBytes, stats.Entries
			if stats.LimitBytes > 0 {
				payload.CacheLimitMB = stats.LimitBytes / (1024 * 1024)
			}
		}
		_ = a.db.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM tgstrm.integration_secrets WHERE key='tmdb_read_token'),
		 EXISTS(SELECT 1 FROM tgstrm.integration_secrets WHERE key='tmdb_read_token' AND last_verified_at IS NOT NULL AND last_error IS NULL)`).Scan(&payload.TMDBConfigured, &payload.TMDBVerified)
		writeJSON(w, 200, payload)
		return
	}
	var input mediaSettingsPayload
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&input); err != nil {
		fail(w, 400, "invalid_json", "Revisa los datos enviados.")
		return
	}
	if input.AmbientIdleMinutes == 0 {
		input.AmbientIdleMinutes = 5
	}
	if input.AmbientWindow == "" {
		input.AmbientWindow = "week"
	}
	if input.AmbientIdleMinutes < 1 || input.AmbientIdleMinutes > 120 || (input.AmbientWindow != "day" && input.AmbientWindow != "week") {
		fail(w, 422, "invalid_ambient_settings", "Elige entre 1 y 120 minutos y tendencias del día o semana.")
		return
	}
	input.PublicBaseURL = strings.TrimRight(strings.TrimSpace(input.PublicBaseURL), "/")
	input.MoviesPath, input.ShowsPath = strings.TrimSpace(input.MoviesPath), strings.TrimSpace(input.ShowsPath)
	parsed, err := url.Parse(input.PublicBaseURL)
	if input.PublicBaseURL != "" && (err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.RawQuery != "" || parsed.Fragment != "") {
		fail(w, 422, "invalid_public_url", "Introduce una URL pública HTTP o HTTPS sin parámetros.")
		return
	}
	defaults := a.mediaSettingsDefaults()
	if input.MoviesPath == "" {
		input.MoviesPath = defaults.MoviesPath
	}
	if input.ShowsPath == "" {
		input.ShowsPath = defaults.ShowsPath
	}
	if input.CacheLimitMB == 0 {
		input.CacheLimitMB = 512
	}
	if input.CacheLimitMB < 64 || input.CacheLimitMB > 8192 {
		fail(w, 422, "invalid_cache_limit", "El límite de caché debe estar entre 64 MB y 8192 MB.")
		return
	}
	if input.SeriesNaming == "" {
		input.SeriesNaming = "universal"
	}
	if input.SeriesNaming != "universal" && input.SeriesNaming != "jellyfin_tmdb" && input.SeriesNaming != "compact" {
		fail(w, 422, "invalid_series_naming", "Selecciona una estructura válida para las series.")
		return
	}
	if input.NotificationLevel == "" {
		input.NotificationLevel = "balanced"
	}
	if input.NotificationLevel != "strict" && input.NotificationLevel != "balanced" && input.NotificationLevel != "essential" {
		fail(w, 422, "invalid_notification_level", "Selecciona un nivel de notificaciones válido.")
		return
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 500, "settings_failed", "No se pudo guardar la configuración.")
		return
	}
	defer tx.Rollback()
	stored := mediaSettingsPayload{AmbientEnabled: input.AmbientEnabled, AmbientIdleMinutes: input.AmbientIdleMinutes, AmbientWindow: input.AmbientWindow, PublicBaseURL: input.PublicBaseURL, MoviesPath: input.MoviesPath, ShowsPath: input.ShowsPath, SeriesNaming: input.SeriesNaming, IncludeTMDBID: input.IncludeTMDBID, CacheLimitMB: input.CacheLimitMB, NotificationLevel: input.NotificationLevel}
	raw, _ := json.Marshal(stored)
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO tgstrm.settings(key,value) VALUES('media',$1) ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value,revision=tgstrm.settings.revision+1,updated_at=now()`, raw); err != nil {
		fail(w, 500, "settings_failed", "No se pudo guardar la configuración.")
		return
	}
	if token := strings.TrimSpace(input.TMDBToken); token != "" {
		sealed, sealErr := a.store.Seal("integrations/tmdb/read-token", []byte(token))
		if sealErr != nil {
			fail(w, 500, "settings_failed", "No se pudo cifrar el token de TMDB.")
			return
		}
		if _, err = tx.ExecContext(r.Context(), `INSERT INTO tgstrm.integration_secrets(key,ciphertext) VALUES('tmdb_read_token',$1) ON CONFLICT(key) DO UPDATE SET ciphertext=EXCLUDED.ciphertext,updated_at=now(),last_verified_at=NULL,last_error=NULL`, sealed); err != nil {
			fail(w, 500, "settings_failed", "No se pudo guardar el token de TMDB.")
			return
		}
	}
	if err = tx.Commit(); err != nil {
		fail(w, 500, "settings_failed", "No se pudo confirmar la configuración.")
		return
	}
	if service := a.telegramService(); service != nil {
		service.SetPersistentCacheLimit(input.CacheLimitMB * 1024 * 1024)
		stats := service.PersistentCacheStats()
		stored.CacheUsedBytes, stored.CacheEntries = stats.UsedBytes, stats.Entries
	}
	stored.TMDBConfigured = input.TMDBToken != ""
	if !stored.TMDBConfigured {
		_ = a.db.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM tgstrm.integration_secrets WHERE key='tmdb_read_token'),
		 EXISTS(SELECT 1 FROM tgstrm.integration_secrets WHERE key='tmdb_read_token' AND last_verified_at IS NOT NULL AND last_error IS NULL)`).Scan(&stored.TMDBConfigured, &stored.TMDBVerified)
	}
	writeJSON(w, 200, stored)
}

func (a *API) clearMediaCache(w http.ResponseWriter, r *http.Request) {
	service := a.telegramService()
	if service == nil {
		fail(w, http.StatusServiceUnavailable, "telegram_unavailable", "Telegram no está disponible.")
		return
	}
	if err := service.ClearPersistentCache(); err != nil {
		fail(w, 500, "cache_clear_failed", "No se pudo vaciar completamente la caché.")
		return
	}
	writeJSON(w, 200, service.PersistentCacheStats())
}

func (a *API) testTMDB(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		fail(w, http.StatusServiceUnavailable, "database_unavailable", "La base de datos no está disponible.")
		return
	}
	var configuration json.RawMessage
	err := a.tmdbRequest(r, "configuration", nil, &configuration)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 422, "tmdb_not_configured", "Guarda primero una API Key o un Read Access Token de TMDB.")
		return
	}
	if err != nil {
		_, _ = a.db.ExecContext(r.Context(), `UPDATE tgstrm.integration_secrets SET last_error=$1 WHERE key='tmdb_read_token'`, "connection failed")
		var responseErr tmdbResponseError
		if errors.As(err, &responseErr) && (responseErr.status == http.StatusUnauthorized || responseErr.status == http.StatusForbidden) {
			_, _ = a.db.ExecContext(r.Context(), `UPDATE tgstrm.integration_secrets SET last_verified_at=NULL,last_error=$1 WHERE key='tmdb_read_token'`, "credentials rejected")
			fail(w, 422, "tmdb_rejected", "TMDB rechazó la credencial configurada.")
		} else {
			fail(w, 502, "tmdb_unreachable", "No se pudo conectar con TMDB.")
		}
		return
	}
	_, _ = a.db.ExecContext(r.Context(), `UPDATE tgstrm.integration_secrets SET last_verified_at=now(),last_error=NULL WHERE key='tmdb_read_token'`)
	writeJSON(w, 200, map[string]any{"status": "verified", "tmdb_verified": true})
}
