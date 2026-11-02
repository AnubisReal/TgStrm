package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	telegramauth "tgstrm/internal/telegram"
)

// Resolve an old deep link by stored topic identity, not the current target.
// Changing the selected collection must not redirect a saved video to its root.
func (a *API) libraryTopicLocation(w http.ResponseWriter, r *http.Request) {
	if !a.libraryReady(w) {
		return
	}
	var groupID, collectionID string
	err := a.db.QueryRowContext(r.Context(), `SELECT t.chat_id::text,COALESCE((SELECT cg.collection_id::text FROM tgstrm.collection_groups cg JOIN tgstrm.library_collections lc ON lc.id=cg.collection_id WHERE cg.destination_chat_id=t.chat_id AND cg.account_id=c.account_id AND cg.state<>'archived' AND lc.state<>'archived' ORDER BY cg.created_at LIMIT 1),'') FROM tgstrm.topics t JOIN tgstrm.managed_catalog_chats c ON c.id=t.chat_id WHERE t.id::text=$1 AND c.account_id=$2 AND t.is_active AND c.is_active`, r.PathValue("topicID"), telegramauth.CatalogAccountID).Scan(&groupID, &collectionID)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			fail(w, 503, "catalog_unavailable", "No se pudo consultar la biblioteca. Vuelve a intentarlo.")
			return
		}
		fail(w, 404, "topic_not_found", "El topic ya no está disponible en tu biblioteca.")
		return
	}
	writeJSON(w, 200, map[string]string{"group_id": groupID, "collection_id": collectionID})
}
