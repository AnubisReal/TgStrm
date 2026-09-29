package httpapi

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"tgstrm/internal/config"
	"tgstrm/internal/storage/postgres"
	telegramauth "tgstrm/internal/telegram"
	"tgstrm/internal/webui"
)

type API struct {
	mu                sync.Mutex
	store             *config.Store
	db                *sql.DB
	configured        bool
	recovery          bool
	telegram          *telegramauth.Service
	databaseSetupMode string
	client            *http.Client
	tmdbRateMu        sync.Mutex
	tmdbNextRequest   time.Time
	tmdbBlockedUntil  time.Time
	tmdbScanMu        sync.Mutex
	tmdbScans         map[string]bool
	groupScanMu       sync.Mutex
	groupScans        map[string]bool
	exportLocks       sync.Map
	backupMu          sync.Mutex
	restoreMu         sync.Mutex
	maintenance       bool
	backgroundCancel  context.CancelFunc
	backgroundWG      sync.WaitGroup
	trustedProxies    []*net.IPNet
}

// Options contains the narrow deployment settings that must be known before
// PostgreSQL is available. Everything else stays in the encrypted database
// configuration managed by the panel.
type Options struct {
	TrustedProxyCIDRs []string
	DatabaseSetupMode string
}

// A bounded 206 response prevents an abandoned seek from monopolizing the
// Telegram transfer queue when cancellation is delayed by a reverse proxy.
// RFC 9110 permits a server to return a subset of a requested range.
const streamHTTPRangeWindow int64 = 8 * 1024 * 1024

func New(store *config.Store) (*API, error) {
	return NewWithOptions(store, Options{})
}

// NewWithOptions creates the API with an explicit list of reverse-proxy
// networks. Forwarded HTTPS headers are trusted only from these networks.
func NewWithOptions(store *config.Store, options Options) (*API, error) {
	trustedProxies, err := parseTrustedProxyCIDRs(options.TrustedProxyCIDRs)
	if err != nil {
		return nil, err
	}
	a := &API{store: store, client: &http.Client{Timeout: 12 * time.Second}, trustedProxies: trustedProxies}
	if options.DatabaseSetupMode == "container" || options.DatabaseSetupMode == "external" {
		a.databaseSetupMode = options.DatabaseSetupMode
	} else {
		a.databaseSetupMode = "native"
	}
	cfg, err := store.LoadDatabase()
	if err != nil {
		return nil, err
	}
	if cfg != nil {
		a.configured = true
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		db, err := postgres.Connect(ctx, *cfg)
		if err == nil {
			err = postgres.Migrate(ctx, db)
		}
		if err != nil {
			if db != nil {
				db.Close()
			}
			a.recovery = true
		} else {
			a.db = db
			if err = a.ensureDefaultAdmin(ctx, db); err != nil {
				db.Close()
				return nil, fmt.Errorf("prepare administrator: %w", err)
			}
			_, _ = db.ExecContext(ctx, `UPDATE tgstrm.sync_states SET status='stale',last_error='El proceso fue interrumpido por un reinicio',updated_at=now() WHERE status='running'`)
			cacheLimit := telegramauth.DefaultPersistentCacheLimit
			var raw []byte
			if queryErr := db.QueryRowContext(ctx, `SELECT value FROM tgstrm.settings WHERE key='media'`).Scan(&raw); queryErr == nil {
				var saved mediaSettingsPayload
				if json.Unmarshal(raw, &saved) == nil && saved.CacheLimitMB >= 64 && saved.CacheLimitMB <= 8192 {
					cacheLimit = saved.CacheLimitMB * 1024 * 1024
				}
			}
			persistentCache, cacheErr := telegramauth.NewPersistentStreamCache(filepath.Join(store.Dir(), "cache", "telegram-blocks"), cacheLimit)
			if cacheErr != nil {
				log.Printf("La caché persistente no está disponible: %v", cacheErr)
			}
			a.telegram = &telegramauth.Service{DB: db, Vault: store, PersistentCache: persistentCache}
			a.telegram.Start(context.Background())
			backgroundCtx, backgroundCancel := context.WithCancel(context.Background())
			a.backgroundCancel = backgroundCancel
			for _, run := range []func(context.Context){a.runCatalogImportQueue, a.runExportReconciler, a.runMovieMatchReconciler, a.runTopicArtworkReconciler, a.runBackupScheduler} {
				a.backgroundWG.Add(1)
				go func(run func(context.Context)) { defer a.backgroundWG.Done(); run(backgroundCtx) }(run)
			}
		}
	}
	return a, nil
}
func (a *API) Close() {
	a.mu.Lock()
	cancel, telegram, db := a.backgroundCancel, a.telegram, a.db
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	a.backgroundWG.Wait()
	if db != nil {
		if telegram != nil {
			telegram.Stop()
		}
		db.Close()
	}
}
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
func fail(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"code": code, "message": message})
}
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health/live", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /api/v1/setup/status", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		state := "unconfigured"
		if a.configured {
			state = "ready"
		}
		if a.db != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			a.recovery = a.db.PingContext(ctx) != nil
			cancel()
		}
		if a.recovery {
			state = "recovery"
		}
		telegramState := "disconnected"
		if a.telegram != nil {
			if profile, err := a.telegram.Profile(r.Context()); err == nil {
				if profile.Connected {
					telegramState = "connected"
				} else {
					telegramState = "authorized"
				}
			}
		}
		writeJSON(w, 200, map[string]string{"database": state, "telegram": telegramState, "database_setup_mode": a.databaseSetupMode})
	})
	mux.HandleFunc("POST /api/v1/setup/database/test", a.protect(a.testDatabase))
	mux.HandleFunc("POST /api/v1/setup/database/probe-local", a.protect(a.probeLocalDatabase))
	mux.HandleFunc("POST /api/v1/setup/database", a.protect(a.saveDatabase))
	mux.HandleFunc("GET /api/v1/admin/session", a.sameOrigin(a.adminSessionStatus))
	mux.HandleFunc("POST /api/v1/admin/login", a.sameOrigin(a.adminLogin))
	mux.HandleFunc("POST /api/v1/admin/password/initial", a.sameOrigin(a.adminInitialPassword))
	mux.HandleFunc("POST /api/v1/admin/password/skip", a.sameOrigin(a.adminSkipInitialPassword))
	mux.HandleFunc("POST /api/v1/admin/logout", a.sameOrigin(a.adminLogout))
	mux.HandleFunc("POST /api/v1/admin/recovery/request", a.sameOrigin(a.adminRecoveryRequest))
	mux.HandleFunc("POST /api/v1/admin/recovery/complete", a.sameOrigin(a.adminRecoveryComplete))
	mux.HandleFunc("GET /api/v1/admin/account", a.sameOrigin(a.adminAccount))
	mux.HandleFunc("PUT /api/v1/admin/account", a.sameOrigin(a.adminAccount))
	mux.HandleFunc("GET /api/v1/admin/sessions", a.sameOrigin(a.adminSessions))
	mux.HandleFunc("DELETE /api/v1/admin/sessions", a.sameOrigin(a.adminSessions))
	mux.HandleFunc("GET /api/v1/telegram/auth", a.sameOrigin(a.telegramAuth("status")))
	mux.HandleFunc("POST /api/v1/telegram/auth/configure", a.sameOrigin(a.telegramAuth("configure")))
	mux.HandleFunc("POST /api/v1/telegram/auth/send-code", a.sameOrigin(a.telegramAuth("send-code")))
	mux.HandleFunc("POST /api/v1/telegram/auth/verify-code", a.sameOrigin(a.telegramAuth("verify-code")))
	mux.HandleFunc("POST /api/v1/telegram/auth/verify-password", a.sameOrigin(a.telegramAuth("verify-password")))
	mux.HandleFunc("POST /api/v1/telegram/logout", a.sameOrigin(a.telegramLogout))
	mux.HandleFunc("GET /api/v1/profile", a.sameOrigin(a.profile))
	mux.HandleFunc("GET /api/v1/profile/avatar", a.sameOrigin(a.avatar))
	mux.HandleFunc("GET /api/v1/events", a.sameOrigin(a.events))
	mux.HandleFunc("GET /api/v1/activity", a.sameOrigin(a.activity))
	mux.HandleFunc("GET /api/v1/jobs", a.sameOrigin(a.jobs))
	mux.HandleFunc("POST /api/v1/jobs/{kind}/{id}/retry", a.sameOrigin(a.retryJob))
	mux.HandleFunc("POST /api/v1/activity/clear", a.sameOrigin(a.clearActivity))
	mux.HandleFunc("GET /api/v1/groups", a.sameOrigin(a.groups))
	mux.HandleFunc("GET /api/v1/groups/{groupID}", a.sameOrigin(a.group))
	mux.HandleFunc("GET /api/v1/groups/{groupID}/topics", a.sameOrigin(a.groupTopics))
	mux.HandleFunc("GET /api/v1/groups/{groupID}/video-scan", a.sameOrigin(a.groupVideoScan))
	mux.HandleFunc("POST /api/v1/groups/{groupID}/video-scan", a.sameOrigin(a.groupVideoScan))
	mux.HandleFunc("GET /api/v1/groups/{groupID}/topics/artwork", a.sameOrigin(a.topicArtwork))
	mux.HandleFunc("POST /api/v1/groups/{groupID}/topics/artwork", a.sameOrigin(a.startTopicArtworkScan))
	mux.HandleFunc("GET /api/v1/groups/{groupID}/topics/{topicID}", a.sameOrigin(a.topicMessages))
	mux.HandleFunc("DELETE /api/v1/groups/{groupID}/topics/{topicID}", a.sameOrigin(a.deleteTopic))
	mux.HandleFunc("GET /api/v1/groups/{groupID}/topics/{topicID}/episode-gaps", a.sameOrigin(a.topicEpisodeGaps))
	mux.HandleFunc("POST /api/v1/groups/{groupID}/topics/{topicID}/watch", a.sameOrigin(a.watchTopic))
	mux.HandleFunc("GET /api/v1/groups/{groupID}/topics/{topicID}/videos/{videoID}/episode-resolution", a.sameOrigin(a.episodeResolution))
	mux.HandleFunc("GET /api/v1/groups/{groupID}/topics/{topicID}/videos/{videoID}/episodes", a.sameOrigin(a.seasonEpisodes))
	mux.HandleFunc("PUT /api/v1/groups/{groupID}/topics/{topicID}/videos/{videoID}/episode-resolution", a.sameOrigin(a.saveEpisodeResolution))
	mux.HandleFunc("GET /api/v1/videos/{videoID}", a.sameOrigin(a.video))
	mux.HandleFunc("GET /api/v1/videos/{videoID}/stream", a.sameOrigin(a.videoStream))
	mux.HandleFunc("HEAD /api/v1/videos/{videoID}/stream", a.sameOrigin(a.videoStream))
	mux.HandleFunc("GET /stream/{assetID}", a.publicVideoStream)
	mux.HandleFunc("HEAD /stream/{assetID}", a.publicVideoStream)
	mux.HandleFunc("GET /api/v1/settings/media", a.sameOrigin(a.mediaSettings))
	mux.HandleFunc("GET /api/v1/health/system", a.sameOrigin(a.systemHealth))
	mux.HandleFunc("GET /api/v1/health/reconciliation", a.sameOrigin(a.reconciliation))
	mux.HandleFunc("POST /api/v1/health/reconciliation/repair", a.sameOrigin(a.repairMissingExports))
	mux.HandleFunc("GET /api/v1/backups", a.sameOrigin(a.backups))
	mux.HandleFunc("POST /api/v1/backups", a.sameOrigin(a.backups))
	mux.HandleFunc("GET /api/v1/settings/backups", a.sameOrigin(a.backupSettings))
	mux.HandleFunc("PUT /api/v1/settings/backups", a.sameOrigin(a.backupSettings))
	mux.HandleFunc("POST /api/v1/backups/{name}/restore", a.sameOrigin(a.restoreBackup))
	mux.HandleFunc("PUT /api/v1/settings/media", a.sameOrigin(a.mediaSettings))
	mux.HandleFunc("POST /api/v1/settings/media/test-tmdb", a.sameOrigin(a.testTMDB))
	mux.HandleFunc("DELETE /api/v1/settings/media/cache", a.sameOrigin(a.clearMediaCache))
	mux.HandleFunc("GET /api/v1/tmdb/search", a.sameOrigin(a.searchTMDB))
	mux.HandleFunc("GET /api/v1/search", a.sameOrigin(a.catalogSearch))
	mux.HandleFunc("POST /api/v1/search/remote/index", a.sameOrigin(a.indexRemoteSearchResult))
	mux.HandleFunc("GET /api/v1/groups/{groupID}/topics/{topicID}/tmdb-match", a.sameOrigin(a.listTMDBMatches))
	mux.HandleFunc("PUT /api/v1/groups/{groupID}/topics/{topicID}/tmdb-match", a.sameOrigin(a.confirmTMDBMatch))
	mux.HandleFunc("POST /api/v1/groups/{groupID}/topics/{topicID}/tmdb-scan", a.sameOrigin(a.startTMDBScan))
	mux.HandleFunc("GET /api/v1/groups/{groupID}/topics/{topicID}/tmdb-scan", a.sameOrigin(a.tmdbScanStatus))
	mux.HandleFunc("POST /api/v1/groups/{groupID}/topics/{topicID}/exports", a.sameOrigin(a.generateTopicSTRM))
	mux.HandleFunc("POST /api/v1/groups/{groupID}/topics/{topicID}/import", a.sameOrigin(a.importTopicToCatalog))
	mux.HandleFunc("GET /api/v1/groups/{groupID}/topics/{topicID}/import", a.sameOrigin(a.catalogImportStatus))
	mux.HandleFunc("POST /api/v1/groups/{groupID}/imports/preview", a.sameOrigin(a.previewCatalogImports))
	mux.HandleFunc("POST /api/v1/groups/active", a.sameOrigin(a.selectGroup))
	mux.HandleFunc("GET /api/v1/mirrors", a.sameOrigin(a.mirrorLinks))
	mux.HandleFunc("POST /api/v1/mirrors", a.sameOrigin(a.mirrorLinks))
	mux.HandleFunc("PATCH /api/v1/mirrors/{linkID}", a.sameOrigin(a.mirrorLink))
	mux.HandleFunc("DELETE /api/v1/mirrors/{linkID}", a.sameOrigin(a.mirrorLink))
	// Security headers are the outermost layer so they cover both JSON/stream
	// endpoints and the compiled browser application served by webui.
	return securityHeaders(webui.Handler(a.adminGate(mux)))
}

func parseSingleRange(value string, size int64) (int64, int64, error) {
	if size <= 0 {
		return 0, 0, errors.New("empty resource")
	}
	if value == "" {
		return 0, min(size-1, telegramauth.StreamChunkSize-1), nil
	}
	if !strings.HasPrefix(value, "bytes=") || strings.Contains(value, ",") {
		return 0, 0, errors.New("unsupported range")
	}
	parts := strings.SplitN(strings.TrimPrefix(value, "bytes="), "-", 2)
	if len(parts) != 2 {
		return 0, 0, errors.New("invalid range")
	}
	if parts[0] == "" {
		suffix, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || suffix <= 0 {
			return 0, 0, errors.New("invalid suffix range")
		}
		if suffix > size {
			suffix = size
		}
		return size - suffix, size - 1, nil
	}
	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || start < 0 || start >= size {
		return 0, 0, errors.New("invalid range start")
	}
	end := size - 1
	if parts[1] != "" {
		end, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil || end < start {
			return 0, 0, errors.New("invalid range end")
		}
		if end >= size {
			end = size - 1
		}
	}
	return start, end, nil
}

func (a *API) videoStream(w http.ResponseWriter, r *http.Request) {
	service := a.telegramService()
	if service == nil {
		fail(w, 503, "telegram_unavailable", "Telegram no está disponible.")
		return
	}
	location, err := service.VideoLocation(r.Context(), r.PathValue("videoID"))
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "video_not_found", "El vídeo no está disponible.")
		return
	}
	if err != nil {
		fail(w, 409, "video_location_unavailable", "La ubicación del vídeo todavía no está disponible.")
		return
	}
	a.serveVideoLocation(w, r, service, location)
}

func (a *API) publicVideoStream(w http.ResponseWriter, r *http.Request) {
	service := a.telegramService()
	if service == nil {
		fail(w, 503, "telegram_unavailable", "Telegram no está disponible.")
		return
	}
	location, err := service.PublicVideoLocation(r.Context(), r.PathValue("assetID"))
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "stream_not_found", "El stream no está disponible.")
		return
	}
	if err != nil {
		fail(w, 409, "stream_location_unavailable", "La ubicación del stream no está disponible.")
		return
	}
	a.serveVideoLocation(w, r, service, location)
}

func (a *API) serveVideoLocation(w http.ResponseWriter, r *http.Request, service *telegramauth.Service, location telegramauth.VideoFileLocation) {
	service.PrepareVideoIndex(location)
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Type", location.MIMEType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": location.FileName}))
	w.Header().Set("Cache-Control", "private, no-store")
	// Nginx Proxy Manager must forward each Telegram fragment immediately;
	// buffering the upstream response causes Safari to underrun intermittently.
	w.Header().Set("X-Accel-Buffering", "no")
	etag := fmt.Sprintf(`"tgstrm-%d-%d"`, location.DocumentID, location.ByteSize)
	w.Header().Set("ETag", etag)
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Length", strconv.FormatInt(location.ByteSize, 10))
		w.WriteHeader(http.StatusOK)
		return
	}
	rangeHeader := r.Header.Get("Range")
	if ifRange := r.Header.Get("If-Range"); ifRange != "" && ifRange != etag {
		rangeHeader = ""
	}
	if rangeHeader == "" {
		a.videoFullStream(w, r, service, location)
		return
	}
	start, end, err := parseSingleRange(rangeHeader, location.ByteSize)
	if err != nil {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", location.ByteSize))
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	end = min(end, start+streamHTTPRangeWindow-1)
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, location.ByteSize))
	w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	a.writeVideoBytes(w, r, service, location, start, end, http.StatusPartialContent)
}

// videoFullStream keeps responses without a Range header compliant with HTTP:
// Safari may issue this request while probing an MP4 and requires a 200 response
// whose Content-Length describes the complete resource. Data is still fetched
// progressively from Telegram and stops as soon as the browser disconnects.
func (a *API) videoFullStream(w http.ResponseWriter, r *http.Request, service *telegramauth.Service, location telegramauth.VideoFileLocation) {
	w.Header().Set("Content-Length", strconv.FormatInt(location.ByteSize, 10))
	a.writeVideoBytes(w, r, service, location, 0, location.ByteSize-1, http.StatusOK)
}

// writeVideoBytes translates one HTTP byte range into the aligned, bounded
// chunks required by Telegram while preserving the exact range requested by
// strict clients such as Safari.
func (a *API) writeVideoBytes(w http.ResponseWriter, r *http.Request, service *telegramauth.Service, location telegramauth.VideoFileLocation, start, end int64, status int) {
	firstBlockEnd := ((start/telegramauth.StreamCacheBlockSize)+1)*telegramauth.StreamCacheBlockSize - 1
	firstEnd := min(end, start+telegramauth.InitialStreamChunkSize-1, firstBlockEnd)
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	first, err := service.DownloadVideoRange(ctx, location, start, firstEnd)
	cancel()
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		log.Printf("download video start video=%s start=%d: %v", location.VideoID, start, err)
		fail(w, 502, "telegram_download_failed", "Telegram no pudo entregar este fragmento.")
		return
	}
	if len(first) == 0 {
		fail(w, 502, "empty_video_range", "Telegram devolvió un fragmento vacío.")
		return
	}
	w.WriteHeader(status)
	if _, err = w.Write(first); err != nil {
		return
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	type segment struct{ start, end int64 }
	type result struct {
		content []byte
		err     error
	}
	segments := make([]segment, 0)
	for offset := start + int64(len(first)); offset <= end; {
		// Telegram requires every request to remain inside one aligned 1 MiB block.
		blockEnd := ((offset/telegramauth.StreamCacheBlockSize)+1)*telegramauth.StreamCacheBlockSize - 1
		segmentEnd := min(end, blockEnd)
		segments = append(segments, segment{start: offset, end: segmentEnd})
		offset = segmentEnd + 1
	}
	if len(segments) == 0 {
		return
	}

	const prefetchWindow = 3
	results := make([]chan result, len(segments))
	launch := func(index int) {
		results[index] = make(chan result, 1)
		part := segments[index]
		go func() {
			downloadCtx, downloadCancel := context.WithTimeout(r.Context(), 45*time.Second)
			defer downloadCancel()
			content, downloadErr := service.DownloadVideoRange(downloadCtx, location, part.start, part.end)
			results[index] <- result{content: content, err: downloadErr}
		}()
	}
	for index := 0; index < min(prefetchWindow, len(segments)); index++ {
		launch(index)
	}
	for index, part := range segments {
		item := <-results[index]
		if next := index + prefetchWindow; next < len(segments) {
			launch(next)
		}
		if item.err != nil {
			if !errors.Is(item.err, context.Canceled) {
				log.Printf("download video video=%s start=%d: %v", location.VideoID, part.start, item.err)
			}
			return
		}
		if len(item.content) == 0 {
			return
		}
		if _, err = w.Write(item.content); err != nil {
			return
		}
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}
}

func (a *API) video(w http.ResponseWriter, r *http.Request) {
	service := a.telegramService()
	if service == nil {
		fail(w, 503, "telegram_unavailable", "Telegram no está disponible.")
		return
	}
	video, err := service.Video(r.Context(), r.PathValue("videoID"))
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "video_not_found", "El vídeo no está disponible.")
		return
	}
	if err != nil {
		fail(w, 500, "video_failed", "No se pudo cargar el vídeo.")
		return
	}
	writeJSON(w, 200, video)
}

func (a *API) watchTopic(w http.ResponseWriter, r *http.Request) {
	service := a.telegramService()
	if service == nil {
		fail(w, http.StatusServiceUnavailable, "telegram_unavailable", "Telegram no está disponible.")
		return
	}
	service.WatchTopic(r.PathValue("groupID"), r.PathValue("topicID"))
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) topicMessages(w http.ResponseWriter, r *http.Request) {
	service := a.telegramService()
	if service == nil {
		fail(w, 503, "telegram_unavailable", "Telegram no está disponible.")
		return
	}
	groupID, topicID := r.PathValue("groupID"), r.PathValue("topicID")
	service.WatchTopic(groupID, topicID)
	topic, err := service.Topic(r.Context(), groupID, topicID)
	if errors.Is(err, telegramauth.ErrTopicNotFound) {
		fail(w, 404, "topic_not_found", "El topic no está disponible.")
		return
	}
	if err != nil {
		fail(w, 500, "topic_failed", "No se pudo cargar el topic.")
		return
	}
	before := 0
	if raw := r.URL.Query().Get("before"); raw != "" {
		if _, err = fmt.Sscanf(raw, "%d", &before); err != nil || before < 0 {
			fail(w, 400, "invalid_cursor", "El cursor no es válido.")
			return
		}
	}
	syncState := "ready"
	messages, hasMore, err := service.TopicMessages(r.Context(), topicID, before, 50)
	if err != nil {
		fail(w, 500, "messages_failed", "No se pudieron cargar los mensajes.")
		return
	}
	enrichTopicEpisodeNumbers(messages)
	remotePage := telegramauth.TopicRemotePage{}
	allowRemoteRefresh := r.URL.Query().Get("refresh") != "0"
	// The newest page is refreshed once. Older pages are served from PostgreSQL
	// until the persisted range is exhausted, avoiding duplicate MTProto calls.
	if allowRemoteRefresh && (before == 0 || !hasMore) {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		remotePage, err = service.RefreshTopicMessages(ctx, groupID, topicID, before)
		cancel()
		if err != nil {
			log.Printf("refresh topic messages group=%s topic=%s: %v", groupID, topicID, err)
			syncState = "stale"
		} else {
			// Refresh the topic summary after Telegram's messages and media have
			// been persisted, so the response includes their current counts/bytes.
			topic, err = service.Topic(r.Context(), groupID, topicID)
			if errors.Is(err, telegramauth.ErrTopicNotFound) {
				fail(w, 404, "topic_not_found", "El topic no está disponible.")
				return
			}
			if err != nil {
				fail(w, 500, "topic_failed", "No se pudieron cargar las estadísticas del topic.")
				return
			}
			messages, hasMore, err = service.TopicMessages(r.Context(), topicID, before, 50)
			if err != nil {
				fail(w, 500, "messages_failed", "No se pudieron cargar los mensajes.")
				return
			}
			enrichTopicEpisodeNumbers(messages)
		}
	}
	nextCursor := 0
	if remotePage.HasMore && remotePage.NextOffset > 0 {
		hasMore = true
		nextCursor = remotePage.NextOffset
	} else if hasMore && len(messages) > 0 {
		nextCursor = messages[len(messages)-1].TelegramID
	}
	writeJSON(w, 200, map[string]any{"topic": topic, "messages": messages, "has_more": hasMore, "next_cursor": nextCursor, "sync": syncState})
}

// Episode identity is useful to the catalog before a STRM exists. Prefer the
// saved manual resolution or written-path identity supplied by the service;
// otherwise infer S/E from the Telegram filename or caption so cards and gap
// placeholders can be interleaved in their real season/episode order.
func enrichTopicEpisodeNumbers(messages []telegramauth.TopicMessage) {
	for i := range messages {
		if messages[i].EpisodeNumber > 0 {
			continue
		}
		if episode, ok := detectEpisode(messages[i].FileName, messages[i].Body); ok {
			messages[i].SeasonNumber = episode.Season
			messages[i].EpisodeNumber = episode.Episode
		}
	}
}

func (a *API) telegramService() *telegramauth.Service {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.telegram
}

func (a *API) group(w http.ResponseWriter, r *http.Request) {
	service := a.telegramService()
	if service == nil {
		fail(w, 503, "telegram_unavailable", "Telegram no está disponible.")
		return
	}
	group, err := service.Group(r.Context(), r.PathValue("groupID"))
	if errors.Is(err, telegramauth.ErrGroupNotFound) {
		fail(w, 404, "group_not_found", "El grupo no está disponible.")
		return
	}
	if err != nil {
		fail(w, 500, "group_failed", "No se pudo cargar el grupo.")
		return
	}
	writeJSON(w, 200, group)
}

func (a *API) groupTopics(w http.ResponseWriter, r *http.Request) {
	service := a.telegramService()
	if service == nil {
		fail(w, 503, "telegram_unavailable", "Telegram no está disponible.")
		return
	}
	groupID := r.PathValue("groupID")
	group, err := service.Group(r.Context(), groupID)
	if errors.Is(err, telegramauth.ErrGroupNotFound) {
		fail(w, 404, "group_not_found", "El grupo no está disponible.")
		return
	}
	if err != nil {
		fail(w, 500, "group_failed", "No se pudo cargar el grupo.")
		return
	}
	syncState := "ready"
	if group.Forum && r.URL.Query().Get("refresh") != "0" {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		err = service.RefreshTopics(ctx, groupID)
		cancel()
		if err != nil {
			syncState = "stale"
		}
	}
	topics, err := service.Topics(r.Context(), groupID)
	if err != nil {
		fail(w, 500, "topics_failed", "No se pudieron cargar los topics.")
		return
	}
	writeJSON(w, 200, map[string]any{"group": group, "topics": topics, "sync": syncState})
}

func (a *API) events(w http.ResponseWriter, r *http.Request) {
	service := a.telegramService()
	if service == nil {
		fail(w, 503, "telegram_unavailable", "Telegram no está disponible.")
		return
	}
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	events := service.Subscribe(r.Context())
	for event := range events {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		err = wsjson.Write(ctx, conn, event)
		cancel()
		if err != nil {
			return
		}
	}
}

func (a *API) groups(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	service := a.telegram
	a.mu.Unlock()
	if service == nil {
		fail(w, 503, "telegram_unavailable", "Telegram no está disponible.")
		return
	}
	groups, err := service.Groups(r.Context())
	if err != nil {
		fail(w, 500, "groups_failed", "No se pudieron cargar los grupos.")
		return
	}
	writeJSON(w, 200, map[string]any{"groups": groups})
}

func (a *API) selectGroup(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	service := a.telegram
	a.mu.Unlock()
	if service == nil {
		fail(w, 503, "telegram_unavailable", "Telegram no está disponible.")
		return
	}
	var input struct {
		GroupID string `json:"group_id"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(&input) != nil || input.GroupID == "" {
		fail(w, 400, "invalid_request", "Selecciona un grupo válido.")
		return
	}
	if err := service.SelectGroup(r.Context(), input.GroupID); err != nil {
		fail(w, 422, "group_not_found", "El grupo seleccionado no está disponible.")
		return
	}
	writeJSON(w, 200, map[string]string{"status": "selected"})
}

func (a *API) profile(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	service := a.telegram
	a.mu.Unlock()
	if service == nil {
		fail(w, 503, "database_required", "La base de datos no está disponible.")
		return
	}
	profile, err := service.Profile(r.Context())
	if err != nil {
		fail(w, 401, "telegram_required", "Conecta Telegram para abrir el panel.")
		return
	}
	writeJSON(w, 200, profile)
}

func (a *API) avatar(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	service := a.telegram
	a.mu.Unlock()
	if service == nil {
		http.NotFound(w, r)
		return
	}
	content, contentType, err := service.Avatar(r.Context())
	if err != nil || len(content) == 0 {
		http.NotFound(w, r)
		return
	}
	if contentType == "" {
		contentType = "image/jpeg"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Write(content)
}

func (a *API) sameOrigin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		originHeader := r.Header.Get("Origin")
		// Browsers commonly omit Origin on same-origin GET. GET is read-only;
		// reject an explicit foreign Origin and cross-site fetch metadata.
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) && originHeader == "" && r.Header.Get("Sec-Fetch-Site") != "cross-site" {
			next(w, r)
			return
		}
		origin, err := url.Parse(originHeader)
		if err != nil || origin.Host != r.Host || (origin.Scheme != "http" && origin.Scheme != "https") {
			fail(w, http.StatusForbidden, "origin_rejected", "Origen de la solicitud no permitido.")
			return
		}
		next(w, r)
	}
}

func (a *API) telegramAuth(operation string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		service := a.telegram
		recovery := a.recovery
		a.mu.Unlock()
		if recovery || service == nil {
			fail(w, http.StatusServiceUnavailable, "database_required", "Configura PostgreSQL antes de conectar Telegram.")
			return
		}

		var input telegramauth.Input
		if r.Method != http.MethodGet {
			r.Body = http.MaxBytesReader(w, r.Body, 4096)
			decoder := json.NewDecoder(r.Body)
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&input); err != nil {
				fail(w, http.StatusBadRequest, "invalid_request", "Revisa los datos enviados.")
				return
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()
		state, err := service.Execute(ctx, operation, input)
		if err != nil {
			fail(w, http.StatusUnprocessableEntity, "telegram_auth_failed", telegramauth.PublicError(err))
			return
		}
		writeJSON(w, http.StatusOK, state)
	}
}

func (a *API) telegramLogout(w http.ResponseWriter, r *http.Request) {
	service := a.telegramService()
	if service == nil {
		fail(w, http.StatusServiceUnavailable, "telegram_unavailable", "Telegram no está disponible.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if err := service.Logout(ctx); err != nil {
		fail(w, http.StatusBadGateway, "telegram_logout_failed", "Telegram no pudo cerrar esta sesión. Inténtalo de nuevo.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "disconnected", "next": "/login"})
}

// Bootstrap routes require a possession key and a same-origin browser request.
// Deployment beyond loopback is intentionally deferred until panel authentication exists.
func (a *API) protect(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		origin, err := url.Parse(r.Header.Get("Origin"))
		if err != nil || origin.Host != r.Host || (origin.Scheme != "http" && origin.Scheme != "https") {
			fail(w, 403, "origin_rejected", "Origen de la solicitud no permitido.")
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Setup-Token")), []byte(a.store.Token)) != 1 {
			fail(w, 401, "setup_token_required", "El código de instalación no es válido.")
			return
		}
		next(w, r)
	}
}

type databaseSetupRequest struct {
	config.Database
	CreateDatabase bool `json:"create_database"`
}

func decodeDatabase(w http.ResponseWriter, r *http.Request) (config.Database, bool, bool) {
	var input databaseSetupRequest
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(&input); err != nil {
		fail(w, 400, "invalid_request", "Revisa los datos de conexión.")
		return config.Database{}, false, false
	}
	if d.Decode(&struct{}{}) != io.EOF {
		fail(w, 400, "invalid_request", "Solicitud no válida.")
		return config.Database{}, false, false
	}
	if err := postgres.Validate(input.Database); err != nil {
		fail(w, 400, "invalid_configuration", err.Error())
		return config.Database{}, false, false
	}
	return input.Database, input.CreateDatabase, true
}
func (a *API) testDatabase(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.configured {
		fail(w, 409, "already_configured", "La conexión ya está configurada. Su edición requerirá el panel autenticado.")
		return
	}
	cfg, createDatabase, ok := decodeDatabase(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	if err := postgres.CheckSetup(ctx, cfg, createDatabase); err != nil {
		fail(w, 422, "database_unreachable", publicDatabaseSetupError(err))
		return
	}
	message := "Conexión correcta. La estructura se preparará al guardar."
	if createDatabase {
		message = "Conexión correcta. Al guardar se creará la base si todavía no existe y luego su esquema TgStrm."
	}
	writeJSON(w, 200, map[string]string{"status": "connected", "message": message})
}
func (a *API) saveDatabase(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.configured {
		fail(w, 409, "already_configured", "Esta instalación ya tiene una base de datos. No se puede sustituir desde el asistente.")
		return
	}
	cfg, createDatabase, ok := decodeDatabase(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	db, err := postgres.OpenForSetup(ctx, cfg, createDatabase)
	if err != nil {
		fail(w, 422, "database_unreachable", publicDatabaseSetupError(err))
		return
	}
	if err = postgres.Migrate(ctx, db); err != nil {
		db.Close()
		fail(w, 422, "migration_failed", "No se pudo preparar el esquema. Comprueba permisos y compatibilidad; no se ha guardado la conexión.")
		return
	}
	if err = a.ensureDefaultAdmin(ctx, db); err != nil {
		db.Close()
		fail(w, 500, "admin_setup_failed", "No se pudo preparar la cuenta administradora.")
		return
	}
	if err = a.store.SaveDatabase(cfg); err != nil {
		db.Close()
		fail(w, 500, "configuration_write_failed", "No se pudo guardar la configuración cifrada.")
		return
	}
	a.db = db
	a.telegram = &telegramauth.Service{DB: db, Vault: a.store}
	a.telegram.Start(context.Background())
	a.configured = true
	a.recovery = false
	writeJSON(w, 201, map[string]string{"status": "ready"})
}

func publicDatabaseSetupError(err error) string {
	if strings.Contains(err.Error(), "CREATEDB") {
		return err.Error()
	}
	return "No se pudo conectar con PostgreSQL. Comprueba que el servicio esté iniciado, el servidor, el puerto, usuario, contraseña y TLS."
}

func (a *API) probeLocalDatabase(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	configured := a.configured
	a.mu.Unlock()
	if configured {
		fail(w, http.StatusConflict, "already_configured", "La conexión ya está configurada.")
		return
	}
	if a.databaseSetupMode != "native" {
		fail(w, http.StatusConflict, "container_runtime", "Esta instalación se ejecuta en un contenedor. Usa el servicio PostgreSQL de la misma red de Docker.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", "5432"))
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "not_found", "message": "No hay un servicio escuchando en 127.0.0.1:5432. Instala PostgreSQL con el instalador oficial de tu sistema y vuelve a comprobar."})
		return
	}
	_ = conn.Close()
	writeJSON(w, http.StatusOK, map[string]string{"status": "available", "message": "Hay un servicio escuchando en 127.0.0.1:5432. Comprueba usuario y contraseña con Probar conexión."})
}
