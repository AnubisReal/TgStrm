package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	telegramauth "tgstrm/internal/telegram"
)

type deleteTopicInput struct {
	ConfirmTitle string `json:"confirm_title"`
}

func (a *API) deleteTopic(w http.ResponseWriter, r *http.Request) {
	service := a.telegramService()
	if a.db == nil || service == nil {
		fail(w, http.StatusServiceUnavailable, "telegram_unavailable", "Telegram no está disponible.")
		return
	}
	var input deleteTopicInput
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil {
		fail(w, http.StatusBadRequest, "invalid_confirmation", "Confirma el título exacto del topic.")
		return
	}
	var title string
	err := a.db.QueryRowContext(r.Context(), `SELECT t.title FROM tgstrm.topics t JOIN tgstrm.chats c ON c.id=t.chat_id
		WHERE c.id::text=$1 AND t.id::text=$2 AND c.is_active AND t.is_active`, r.PathValue("groupID"), r.PathValue("topicID")).Scan(&title)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, http.StatusNotFound, "topic_not_found", "El topic ya no está disponible.")
		return
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, "topic_failed", "No se pudo comprobar el topic.")
		return
	}
	if strings.TrimSpace(input.ConfirmTitle) != title {
		fail(w, http.StatusConflict, "title_mismatch", "El título de confirmación no coincide.")
		return
	}
	if err = service.DeleteForumTopic(r.Context(), r.PathValue("groupID"), r.PathValue("topicID")); errors.Is(err, telegramauth.ErrTopicNotFound) {
		fail(w, http.StatusNotFound, "topic_not_found", "El topic ya no está disponible.")
		return
	} else if err != nil {
		fail(w, http.StatusBadGateway, "topic_delete_failed", "Telegram no pudo eliminar el topic.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
