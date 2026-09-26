package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"
)

type remoteSearchIndexInput struct {
	PeerType        string `json:"peer_type"`
	PeerID          int64  `json:"peer_id"`
	TopicTelegramID int    `json:"topic_id"`
	MessageID       int    `json:"message_id,omitempty"`
}

// indexRemoteSearchResult turns a Telegram-only search hit into the existing
// local read model. It is deliberately POST-only and explicit: searching must
// remain read-only, while indexing may hydrate the selected forum topic.
func (a *API) indexRemoteSearchResult(w http.ResponseWriter, r *http.Request) {
	service := a.telegramService()
	if a.db == nil || service == nil {
		fail(w, http.StatusServiceUnavailable, "telegram_unavailable", "Telegram no está disponible.")
		return
	}
	var input remoteSearchIndexInput
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || input.PeerType != "channel" || input.PeerID <= 0 || input.TopicTelegramID <= 0 {
		fail(w, http.StatusUnprocessableEntity, "remote_result_not_indexable", "Este resultado no pertenece a un topic de Telegram indexable.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	groupID, topicID, err := service.IndexRemoteTopic(ctx, strings.ToLower(input.PeerType), input.PeerID, input.TopicTelegramID)
	if err != nil {
		log.Printf("remote search index failed peer=%s/%d topic=%d: %v", input.PeerType, input.PeerID, input.TopicTelegramID, err)
		status := http.StatusBadGateway
		code := "remote_index_failed"
		if errors.Is(err, context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
			code = "remote_index_timeout"
		}
		fail(w, status, code, "No se pudo indexar el topic remoto. Comprueba que siga disponible y vuelve a intentarlo.")
		return
	}
	localMessageID := ""
	if input.MessageID > 0 {
		_ = a.db.QueryRowContext(ctx, `SELECT m.id::text FROM tgstrm.messages m WHERE m.topic_id::text=$1 AND m.telegram_message_id=$2 AND m.is_active`, topicID, input.MessageID).Scan(&localMessageID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"group_id": groupID, "topic_id": topicID, "message_id": localMessageID, "message": "Topic indexado correctamente."})
}
