package httpapi

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type catalogSearchResult struct {
	Kind           string  `json:"kind"`
	ID             string  `json:"id"`
	GroupID        string  `json:"group_id"`
	TopicID        string  `json:"topic_id,omitempty"`
	VideoID        string  `json:"video_id,omitempty"`
	Title          string  `json:"title"`
	Detail         string  `json:"detail,omitempty"`
	GroupTitle     string  `json:"group_title"`
	TopicTitle     string  `json:"topic_title,omitempty"`
	MediaType      string  `json:"media_type,omitempty"`
	OfficialTitle  string  `json:"official_title,omitempty"`
	OriginalTitle  string  `json:"original_title,omitempty"`
	Year           string  `json:"year,omitempty"`
	PosterPath     string  `json:"poster_path,omitempty"`
	Matched        bool    `json:"matched"`
	Exported       bool    `json:"exported"`
	ByteSize       int64   `json:"byte_size,omitempty"`
	Score          float64 `json:"score"`
	Remote         bool    `json:"remote"`
	Importable     bool    `json:"importable,omitempty"`
	RemotePeerType string  `json:"remote_peer_type,omitempty"`
	RemotePeerID   int64   `json:"remote_peer_id,omitempty"`
	RemoteMessage  int     `json:"remote_message_id,omitempty"`
	RemoteTopicID  int     `json:"remote_topic_id,omitempty"`
}

func (a *API) catalogSearch(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(query)) < 2 {
		writeJSON(w, http.StatusOK, map[string]any{"query": query, "items": []catalogSearchResult{}, "has_more": false})
		return
	}
	if len([]rune(query)) > 120 {
		fail(w, http.StatusUnprocessableEntity, "search_too_long", "La búsqueda no puede superar 120 caracteres.")
		return
	}
	limit := 24
	refreshRequested := r.URL.Query().Get("refresh") == "1"
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed >= 1 && parsed <= 40 {
			limit = parsed
		}
	}
	rows, err := a.db.QueryContext(r.Context(), `
WITH file_results AS (
 SELECT 'file'::text kind,m.id::text id,c.id::text group_id,t.id::text topic_id,m.id::text video_id,
  COALESCE(NULLIF(mm.file_name,''),NULLIF(m.body,''),'Archivo de vídeo') title,
  CASE WHEN NULLIF(m.body,'') IS DISTINCT FROM NULLIF(mm.file_name,'') THEN COALESCE(NULLIF(m.body,''),'') ELSE '' END detail,
  c.title group_title,t.title topic_title,COALESCE(tm.media_type,'') media_type,
  COALESCE(tm.title,'') official_title,COALESCE(tm.original_title,'') original_title,
  COALESCE(EXTRACT(YEAR FROM tm.release_date)::int::text,'') release_year,COALESCE(tm.poster_path,'') poster_path,
  (tm.id IS NOT NULL) matched,EXISTS(SELECT 1 FROM tgstrm.stream_assets sa JOIN tgstrm.stream_exports se ON se.asset_id=sa.id AND se.status='written' WHERE sa.message_id=m.id) exported,
  COALESCE(mm.byte_size,0) byte_size,
  GREATEST(similarity(lower(COALESCE(mm.file_name,'')),lower($1)),similarity(lower(m.body),lower($1)),similarity(lower(t.title),lower($1)),similarity(lower(COALESCE(tm.title,'')),lower($1)),similarity(lower(COALESCE(tm.original_title,'')),lower($1)))
  + CASE WHEN lower(COALESCE(mm.file_name,''))=lower($1) THEN 2 WHEN lower(COALESCE(mm.file_name,'')) LIKE lower($1)||'%' THEN 1 ELSE 0 END score
 FROM tgstrm.messages m JOIN tgstrm.message_media mm ON mm.message_id=m.id JOIN tgstrm.topics t ON t.id=m.topic_id JOIN tgstrm.chats c ON c.id=m.chat_id
 LEFT JOIN tgstrm.tmdb_matches tm ON tm.message_id=m.id
 WHERE m.is_active AND t.is_active AND c.is_active AND (lower(COALESCE(mm.file_name,'')) LIKE '%'||lower($1)||'%' OR lower(m.body) LIKE '%'||lower($1)||'%' OR lower(t.title) LIKE '%'||lower($1)||'%' OR lower(COALESCE(tm.title,'')) LIKE '%'||lower($1)||'%' OR lower(COALESCE(tm.original_title,'')) LIKE '%'||lower($1)||'%' OR similarity(lower(COALESCE(mm.file_name,'')),lower($1)) >= 0.45 OR similarity(lower(m.body),lower($1)) >= 0.45)
), topic_results AS (
 SELECT 'topic',t.id::text,c.id::text,t.id::text,''::text,t.title,''::text,c.title,t.title,COALESCE(tm.media_type,''),COALESCE(tm.title,''),COALESCE(tm.original_title,''),COALESCE(EXTRACT(YEAR FROM tm.release_date)::int::text,''),COALESCE(tm.poster_path,''),(tm.id IS NOT NULL),
  EXISTS(SELECT 1 FROM tgstrm.messages m JOIN tgstrm.stream_assets sa ON sa.message_id=m.id JOIN tgstrm.stream_exports se ON se.asset_id=sa.id AND se.status='written' WHERE m.topic_id=t.id AND m.is_active),0::bigint,
  GREATEST(similarity(lower(t.title),lower($1)),similarity(lower(COALESCE(tm.title,'')),lower($1)),similarity(lower(COALESCE(tm.original_title,'')),lower($1))) + CASE WHEN lower(t.title)=lower($1) THEN 2 WHEN lower(t.title) LIKE lower($1)||'%' THEN 1 ELSE 0 END
 FROM tgstrm.topics t JOIN tgstrm.chats c ON c.id=t.chat_id LEFT JOIN tgstrm.tmdb_matches tm ON tm.topic_id=t.id AND tm.message_id IS NULL
 WHERE t.is_active AND c.is_active AND (lower(t.title) LIKE '%'||lower($1)||'%' OR lower(COALESCE(tm.title,'')) LIKE '%'||lower($1)||'%' OR lower(COALESCE(tm.original_title,'')) LIKE '%'||lower($1)||'%' OR lower(t.title) % lower($1))
), group_results AS (
 SELECT 'group',c.id::text,c.id::text,''::text,''::text,c.title,''::text,c.title,''::text,''::text,''::text,''::text,''::text,''::text,false,false,0::bigint,
  similarity(lower(c.title),lower($1)) + CASE WHEN lower(c.title)=lower($1) THEN 2 WHEN lower(c.title) LIKE lower($1)||'%' THEN 1 ELSE 0 END
 FROM tgstrm.chats c WHERE c.is_active AND (lower(c.title) LIKE '%'||lower($1)||'%' OR lower(c.title) % lower($1))
), combined AS (SELECT * FROM file_results UNION ALL SELECT * FROM topic_results UNION ALL SELECT * FROM group_results)
SELECT kind,id,group_id,topic_id,video_id,title,detail,group_title,topic_title,media_type,official_title,original_title,release_year,poster_path,matched,exported,byte_size,score
FROM combined ORDER BY score DESC, matched DESC, title LIMIT $2`, query, limit+1)
	if err != nil {
		log.Printf("catalog search failed: %v", err)
		fail(w, http.StatusInternalServerError, "search_failed", "No se pudo buscar en el catálogo.")
		return
	}
	defer rows.Close()
	items := make([]catalogSearchResult, 0, limit)
	managedGroups := map[string]bool{}
	managedRows, managedErr := a.db.QueryContext(r.Context(), `SELECT id::text FROM tgstrm.managed_catalog_chats WHERE is_active`)
	if managedErr != nil {
		fail(w, 503, "library_unavailable", "No se pudo comprobar el destino de biblioteca.")
		return
	}
	for managedRows.Next() {
		var id string
		if managedErr = managedRows.Scan(&id); managedErr != nil {
			break
		}
		managedGroups[id] = true
	}
	if managedErr == nil {
		managedErr = managedRows.Err()
	}
	managedRows.Close()
	if managedErr != nil {
		fail(w, 503, "library_unavailable", "No se pudo comprobar el destino de biblioteca.")
		return
	}
	for rows.Next() {
		var item catalogSearchResult
		if err = rows.Scan(&item.Kind, &item.ID, &item.GroupID, &item.TopicID, &item.VideoID, &item.Title, &item.Detail, &item.GroupTitle, &item.TopicTitle, &item.MediaType, &item.OfficialTitle, &item.OriginalTitle, &item.Year, &item.PosterPath, &item.Matched, &item.Exported, &item.ByteSize, &item.Score); err != nil {
			fail(w, 500, "search_failed", "No se pudo leer el resultado.")
			return
		}
		item.Importable = item.Kind == "file" && item.TopicID != "" && item.GroupID != "" && !managedGroups[item.GroupID]
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		fail(w, http.StatusInternalServerError, "search_failed", "No se pudo leer el resultado.")
		return
	}
	_ = rows.Close()
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	hasFileResult := false
	for _, item := range items {
		if item.Kind == "file" {
			hasFileResult = true
			break
		}
	}
	// A previously discovered topic may have metadata but no message rows yet.
	// In that case the topic itself is a useful local result, but the search is
	// still incomplete until its media/captions have been hydrated.
	topicMatch := false
	if !refreshRequested && !hasFileResult && len([]rune(query)) >= 4 {
		_ = a.db.QueryRowContext(r.Context(), `SELECT EXISTS(
			SELECT 1 FROM tgstrm.topics t JOIN tgstrm.chats c ON c.id=t.chat_id
			WHERE t.is_active AND c.is_active AND lower(t.title) LIKE '%'||lower($1)||'%'
		)`, query).Scan(&topicMatch)
	}
	if !refreshRequested && !hasFileResult && len([]rune(query)) >= 4 && (len(items) == 0 || topicMatch) {
		if service := a.telegramService(); service != nil {
			// External forum topics are intentionally lazy in the normal UI. A
			// search is the explicit request to look beyond the already opened
			// catalogue, so refresh topic metadata and then hydrate only topics
			// whose group/topic title can explain this query. This also indexes
			// captions (Telegram's "pie de archivo") and document videos.
			refreshCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
			if len(items) == 0 {
				_ = service.RefreshSearchTopics(refreshCtx)
			}
			_ = service.RefreshSearchMedia(refreshCtx, query, 8)
			cancel()
			clone := r.Clone(r.Context())
			values := clone.URL.Query()
			values.Set("refresh", "1")
			clone.URL.RawQuery = values.Encode()
			a.catalogSearch(w, clone)
			return
		}
	}
	// Telegram's account-wide index is a fallback for a term that is not yet
	// present in PostgreSQL. It is intentionally read-only; synchronization is
	// a separate user action so a search cannot silently alter the catalogue.
	if len(items) == 0 {
		if service := a.telegramService(); service != nil {
			if remote, remoteErr := service.GlobalSearch(r.Context(), query, min(limit, 25)); remoteErr == nil {
				for _, item := range remote {
					title := item.FileName
					if title == "" {
						title = item.Caption
					}
					if title == "" {
						title = "Archivo de Telegram"
					}
					items = append(items, catalogSearchResult{Kind: "file", ID: "remote:" + item.PeerType + ":" + strconv.FormatInt(item.PeerID, 10) + ":" + strconv.Itoa(item.MessageID), GroupID: "", Title: title, Detail: item.Caption, GroupTitle: item.PeerTitle, ByteSize: item.ByteSize, Score: 1, Remote: true, RemotePeerType: item.PeerType, RemotePeerID: item.PeerID, RemoteMessage: item.MessageID, RemoteTopicID: item.TopicTelegramID})
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"query": query, "items": items, "has_more": hasMore})
}
