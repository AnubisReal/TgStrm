package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"tgstrm/internal/library"
	telegramauth "tgstrm/internal/telegram"
)

type collectionView struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	State            string `json:"state"`
	TelegramFilterID int    `json:"telegram_filter_id"`
	Revision         int64  `json:"revision"`
	SeriesNameFormat string `json:"series_name_format"`
	MoviesNameFormat string `json:"movies_name_format"`
	GroupCount       int    `json:"group_count"`
}

func decodeLibraryRequest(w http.ResponseWriter, r *http.Request, out any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(out) != nil {
		fail(w, 400, "invalid_request", "Revisa los datos enviados.")
		return false
	}
	if decoder.Decode(new(any)) != io.EOF {
		fail(w, 400, "invalid_request", "Envía un único objeto JSON.")
		return false
	}
	return true
}
func (a *API) libraryReady(w http.ResponseWriter) bool {
	if a.db == nil {
		fail(w, 503, "database_unavailable", "La base de datos no está disponible.")
		return false
	}
	return true
}
func (a *API) libraryCollections(w http.ResponseWriter, r *http.Request) {
	if !a.libraryReady(w) {
		return
	}
	if r.Method == http.MethodPost {
		var in struct {
			Name             string `json:"name"`
			TelegramFilterID int    `json:"telegram_filter_id"`
		}
		if !decodeLibraryRequest(w, r, &in) {
			return
		}
		in.Name = strings.TrimSpace(in.Name)
		if in.Name == "" || utf8.RuneCountInString(in.Name) > 12 || strings.ContainsAny(in.Name, "\n\r\t") || in.TelegramFilterID < 0 {
			fail(w, 422, "invalid_folder", "El nombre admite entre 1 y 12 caracteres.")
			return
		}
		state := "draft"
		if in.TelegramFilterID > 0 {
			service := a.telegramService()
			if service == nil {
				fail(w, 503, "telegram_unavailable", "Conecta Telegram antes de elegir su carpeta.")
				return
			}
			folders, err := service.LibraryFolders(r.Context())
			if err != nil {
				fail(w, 502, "folders_unavailable", "No se pudieron comprobar las carpetas Telegram.")
				return
			}
			found := false
			for _, folder := range folders {
				if folder.ID == in.TelegramFilterID && !folder.Shared {
					found = true
					in.Name = folder.Title
				}
			}
			if !found {
				fail(w, 422, "invalid_folder", "Elige una carpeta propia que exista en Telegram.")
				return
			}
			state = "ready"
		}
		var id string
		err := a.db.QueryRowContext(r.Context(), `INSERT INTO tgstrm.library_collections(account_id,name,telegram_filter_id,state) VALUES($1,$2,NULLIF($3,0),$4) RETURNING id::text`, telegramauth.CatalogAccountID, in.Name, in.TelegramFilterID, state).Scan(&id)
		if err != nil {
			fail(w, 409, "collection_conflict", "No se pudo guardar la carpeta. Comprueba que no esté asociada ya.")
			return
		}
		writeJSON(w, 201, map[string]any{"id": id, "state": state})
		return
	}
	rows, err := a.db.QueryContext(r.Context(), `SELECT lc.id::text,lc.name,lc.state,COALESCE(lc.telegram_filter_id,0),lc.revision,lc.series_name_format,lc.movies_name_format,count(cg.id) FILTER(WHERE cg.state<>'archived') FROM tgstrm.library_collections lc LEFT JOIN tgstrm.collection_groups cg ON cg.collection_id=lc.id WHERE lc.account_id=$1 AND lc.state<>'archived' GROUP BY lc.id ORDER BY lc.created_at`, telegramauth.CatalogAccountID)
	if err != nil {
		fail(w, 500, "collections_unavailable", "No se pudieron cargar las carpetas.")
		return
	}
	defer rows.Close()
	items := []collectionView{}
	for rows.Next() {
		var item collectionView
		if err = rows.Scan(&item.ID, &item.Name, &item.State, &item.TelegramFilterID, &item.Revision, &item.SeriesNameFormat, &item.MoviesNameFormat, &item.GroupCount); err != nil {
			fail(w, 500, "collections_unavailable", "No se pudieron leer las carpetas.")
			return
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		fail(w, 500, "collections_unavailable", "No se pudieron leer las carpetas.")
		return
	}
	writeJSON(w, 200, map[string]any{"collections": items})
}
func (a *API) libraryFolders(w http.ResponseWriter, r *http.Request) {
	service := a.telegramService()
	if service == nil {
		fail(w, 503, "telegram_unavailable", "Conecta Telegram.")
		return
	}
	folders, err := service.LibraryFolders(r.Context())
	if err != nil {
		fail(w, 502, "folders_unavailable", "No se pudieron comprobar las carpetas Telegram.")
		return
	}
	writeJSON(w, 200, map[string]any{"folders": folders})
}
func (a *API) libraryTarget(w http.ResponseWriter, r *http.Request) {
	if !a.libraryReady(w) {
		return
	}
	resolver := library.Resolver{DB: a.db}
	if r.Method == http.MethodGet {
		scope, err := resolver.Active(r.Context(), telegramauth.CatalogAccountID)
		if err != nil {
			fail(w, 500, "target_unavailable", "No se pudo cargar el destino.")
			return
		}
		writeJSON(w, 200, scope)
		return
	}
	var in library.Scope
	if !decodeLibraryRequest(w, r, &in) {
		return
	}
	current, err := resolver.Active(r.Context(), telegramauth.CatalogAccountID)
	if err != nil {
		fail(w, 500, "target_unavailable", "No se pudo comprobar el destino.")
		return
	}
	if in.Revision != current.Revision {
		fail(w, 409, "revision_conflict", "La configuración cambió. Vuelve a cargarla.")
		return
	}
	if in.Mode == "group" && in.GroupID != "" && in.CollectionID == "" {
		service := a.telegramService()
		if service == nil {
			fail(w, 503, "telegram_unavailable", "Conecta Telegram.")
			return
		}
		if err = service.SelectGroup(r.Context(), in.GroupID, in.Revision); err != nil {
			fail(w, 422, "invalid_target", "El grupo no está disponible.")
			return
		}
	} else if in.Mode == "collection" && in.CollectionID != "" && in.GroupID == "" {
		tx, err := a.db.BeginTx(r.Context(), nil)
		if err != nil {
			fail(w, 500, "target_unavailable", "No se pudo abrir la configuración.")
			return
		}
		defer tx.Rollback()
		if _, err = tx.ExecContext(r.Context(), `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "catalog-target:"+telegramauth.CatalogAccountID); err != nil {
			fail(w, 500, "target_unavailable", "No se pudo bloquear la configuración.")
			return
		}
		var revision int64
		if err = tx.QueryRowContext(r.Context(), `SELECT COALESCE((SELECT revision FROM tgstrm.catalog_targets WHERE account_id=$1),(SELECT revision FROM tgstrm.settings WHERE key='active_chat_id'),0)`, telegramauth.CatalogAccountID).Scan(&revision); err != nil {
			fail(w, 500, "target_unavailable", "No se pudo comprobar la revisión.")
			return
		}
		if revision != in.Revision {
			fail(w, 409, "revision_conflict", "La configuración cambió. Vuelve a cargarla.")
			return
		}
		result, err := tx.ExecContext(r.Context(), `INSERT INTO tgstrm.catalog_targets(account_id,mode,collection_id)
 SELECT $1,'collection',id FROM tgstrm.library_collections WHERE id::text=$2 AND account_id=$1 AND state IN ('draft','ready')
 ON CONFLICT(account_id) DO UPDATE SET mode='collection',group_chat_id=NULL,collection_id=EXCLUDED.collection_id,revision=tgstrm.catalog_targets.revision+1,updated_at=now()
 WHERE tgstrm.catalog_targets.revision=$3`, telegramauth.CatalogAccountID, in.CollectionID, in.Revision)
		if err != nil {
			fail(w, 422, "invalid_target", "La carpeta no está disponible.")
			return
		}
		if n, _ := result.RowsAffected(); n != 1 {
			fail(w, 409, "revision_conflict", "La carpeta o la configuración cambió.")
			return
		}
		if err = tx.Commit(); err != nil {
			fail(w, 500, "target_unavailable", "No se pudo guardar el destino.")
			return
		}
	} else {
		fail(w, 422, "invalid_target", "Elige un grupo o una carpeta de biblioteca.")
		return
	}
	scope, err := resolver.Active(r.Context(), telegramauth.CatalogAccountID)
	if err != nil {
		fail(w, 500, "target_unavailable", "No se pudo leer el destino guardado.")
		return
	}
	writeJSON(w, 200, scope)
}
func (a *API) libraryCollection(w http.ResponseWriter, r *http.Request) {
	if !a.libraryReady(w) {
		return
	}
	var item collectionView
	err := a.db.QueryRowContext(r.Context(), `SELECT id::text,name,state,COALESCE(telegram_filter_id,0),revision,series_name_format,movies_name_format FROM tgstrm.library_collections WHERE id::text=$1 AND account_id=$2 AND state<>'archived'`, r.PathValue("collectionID"), telegramauth.CatalogAccountID).Scan(&item.ID, &item.Name, &item.State, &item.TelegramFilterID, &item.Revision, &item.SeriesNameFormat, &item.MoviesNameFormat)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "collection_not_found", "La carpeta no está disponible.")
		return
	}
	if err != nil {
		fail(w, 500, "collection_unavailable", "No se pudo cargar la carpeta.")
		return
	}
	type member struct {
		ID      string `json:"id"`
		GroupID string `json:"group_id"`
		Title   string `json:"title"`
		Bucket  string `json:"bucket"`
		Kind    string `json:"media_kind"`
		Part    int    `json:"part"`
		State   string `json:"state"`
		Manual  bool   `json:"manual"`
	}
	rows, err := a.db.QueryContext(r.Context(), `SELECT cg.id::text,c.id::text,c.title,cg.bucket,cg.media_kind,cg.part,CASE WHEN c.is_active THEN cg.state ELSE 'destination_missing' END,cg.naming_mode='manual' FROM tgstrm.collection_groups cg JOIN tgstrm.chats c ON c.id=cg.destination_chat_id WHERE cg.collection_id::text=$1 AND cg.account_id=$2 AND cg.state<>'archived' ORDER BY cg.media_kind,cg.bucket,cg.part`, item.ID, telegramauth.CatalogAccountID)
	if err != nil {
		fail(w, 500, "collection_unavailable", "No se pudieron cargar sus cajones.")
		return
	}
	defer rows.Close()
	members := []member{}
	buckets := []string{}
	for rows.Next() {
		var m member
		if rows.Scan(&m.ID, &m.GroupID, &m.Title, &m.Bucket, &m.Kind, &m.Part, &m.State, &m.Manual) != nil {
			fail(w, 500, "collection_unavailable", "No se pudo leer un cajón.")
			return
		}
		members = append(members, m)
		if m.Kind == "series" && m.Bucket != "" && m.State != "destination_missing" {
			buckets = append(buckets, m.Bucket)
		}
	}
	if rows.Err() != nil {
		fail(w, 500, "collection_unavailable", "No se pudieron leer sus cajones.")
		return
	}
	var summary struct {
		VideoCount       int64 `json:"video_count"`
		TotalBytes       int64 `json:"total_bytes"`
		UnknownSizeCount int64 `json:"unknown_size_count"`
	}
	err = a.db.QueryRowContext(r.Context(), `SELECT count(*),COALESCE(sum(mm.byte_size),0),count(*) FILTER (WHERE mm.byte_size IS NULL)
	 FROM tgstrm.messages m JOIN tgstrm.topics t ON t.id=m.topic_id
	 JOIN tgstrm.chats c ON c.id=m.chat_id
	 LEFT JOIN tgstrm.message_media mm ON mm.message_id=m.id
	 WHERE m.is_active AND t.is_active AND c.is_active AND m.media_type='video'
	 AND EXISTS (SELECT 1 FROM tgstrm.collection_groups cg WHERE cg.destination_chat_id=m.chat_id
	 AND cg.collection_id::text=$1 AND cg.account_id=$2 AND cg.state NOT IN ('archived','destination_missing'))`, item.ID, telegramauth.CatalogAccountID).Scan(&summary.VideoCount, &summary.TotalBytes, &summary.UnknownSizeCount)
	if err != nil {
		fail(w, 500, "collection_unavailable", "No se pudo cargar el resumen de la biblioteca.")
		return
	}
	missing, _ := library.MissingBuckets(buckets)
	item.GroupCount = len(members)
	writeJSON(w, 200, map[string]any{"collection": item, "groups": members, "missing_buckets": missing, "summary": summary})
}
func (a *API) libraryNaming(w http.ResponseWriter, r *http.Request) {
	if !a.libraryReady(w) {
		return
	}
	var in struct {
		Series   string `json:"series_name_format"`
		Movies   string `json:"movies_name_format"`
		Revision int64  `json:"revision"`
	}
	if !decodeLibraryRequest(w, r, &in) {
		return
	}
	example, err := library.FormatDestinationName(in.Series, "N", 1)
	if err != nil {
		fail(w, 422, "invalid_format", "Revisa el formato de series y sus variables.")
		return
	}
	movieExample, err := library.FormatDestinationName(in.Movies, "N", 1)
	if err != nil {
		fail(w, 422, "invalid_format", "Revisa el formato de películas y sus variables.")
		return
	}
	result, err := a.db.ExecContext(r.Context(), `UPDATE tgstrm.library_collections SET series_name_format=$3,movies_name_format=$4,revision=revision+1,updated_at=now() WHERE id::text=$1 AND account_id=$2 AND revision=$5`, r.PathValue("collectionID"), telegramauth.CatalogAccountID, strings.TrimSpace(in.Series), strings.TrimSpace(in.Movies), in.Revision)
	if err != nil {
		fail(w, 500, "save_failed", "No se pudieron guardar los formatos.")
		return
	}
	if n, _ := result.RowsAffected(); n != 1 {
		fail(w, 409, "revision_conflict", "La carpeta cambió. Vuelve a cargarla.")
		return
	}
	writeJSON(w, 200, map[string]any{"series_example": example, "movies_example": movieExample, "revision": in.Revision + 1})
}
