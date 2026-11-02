package httpapi

import (
	"context"
	"errors"
	"strings"
)

// Manual sends may target any active forum in the same Telegram account.
// The queue persists whether this is a catalogue or external destination.
func (a *API) resolveManualImportDestination(ctx context.Context, sourceGroup, sourceTopic, group, topic, mediaType string, tmdbID int64) (string, error) {
	group = strings.TrimSpace(group)
	if topic != "" && topic == sourceTopic {
		return "", errors.New("El topic de destino no puede ser el mismo que el origen.")
	}
	if topic != "" {
		var actualGroup, kind string
		var existingMatch int64
		err := a.db.QueryRowContext(ctx, `SELECT c.id::text,t.library_kind,COALESCE(tm.tmdb_id,0)
   FROM tgstrm.chats c JOIN tgstrm.topics t ON t.chat_id=c.id
   LEFT JOIN tgstrm.tmdb_matches tm ON tm.topic_id=t.id AND tm.message_id IS NULL
   WHERE t.id::text=$1 AND t.is_active AND c.is_active AND c.is_forum
   AND c.account_id=(SELECT account_id FROM tgstrm.chats WHERE id::text=$2)`, topic, sourceGroup).Scan(&actualGroup, &kind, &existingMatch)
		if err != nil {
			return "", errors.New("El topic destino no está disponible.")
		}
		if group != "" && group != actualGroup {
			return "", errors.New("El topic no pertenece al grupo seleccionado.")
		}
		if (mediaType == "tv" && (kind == "movie_collection" || existingMatch != 0 && existingMatch != tmdbID)) || (mediaType == "movie" && (kind == "series" || existingMatch != 0)) {
			return "", errors.New("El topic destino pertenece a otro contenido. Selecciona la serie correcta o crea un topic nuevo.")
		}
		group = actualGroup
	}
	if group == "" {
		if err := a.db.QueryRowContext(ctx, `SELECT min(id)::text FROM tgstrm.active_catalog_chats WHERE is_active AND is_forum
    AND account_id=(SELECT account_id FROM tgstrm.chats WHERE id::text=$1) HAVING count(*)=1`, sourceGroup).Scan(&group); err != nil {
			return "", errors.New("Selecciona el grupo de tu biblioteca donde quieres enviar los archivos.")
		}
	}
	var valid bool
	err := a.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tgstrm.chats WHERE id::text=$1 AND is_active AND is_forum
  AND account_id=(SELECT account_id FROM tgstrm.chats WHERE id::text=$2))`, group, sourceGroup).Scan(&valid)
	if err != nil || !valid {
		return "", errors.New("El grupo destino no está disponible o no admite topics.")
	}
	return group, nil
}

func (a *API) importDestinationGroups(ctx context.Context, sourceGroup string) []map[string]any {
	items := make([]map[string]any, 0)
	rows, err := a.db.QueryContext(ctx, `SELECT c.id::text,c.title,COALESCE((SELECT min(cg.bucket) FROM tgstrm.collection_groups cg WHERE cg.destination_chat_id=c.id AND cg.state='ready' AND cg.media_kind='series'),''),EXISTS(SELECT 1 FROM tgstrm.active_catalog_chats ac WHERE ac.id=c.id) FROM tgstrm.chats c
 WHERE c.is_active AND c.is_forum
 AND c.account_id=(SELECT account_id FROM tgstrm.chats WHERE id::text=$1) ORDER BY lower(c.title),c.id`, sourceGroup)
	if err != nil {
		return items
	}
	defer rows.Close()
	for rows.Next() {
		var id, title, bucket string
		var catalogue bool
		if rows.Scan(&id, &title, &bucket, &catalogue) == nil {
			items = append(items, map[string]any{"id": id, "title": title, "bucket": bucket, "catalogue": catalogue})
		}
	}
	return items
}
