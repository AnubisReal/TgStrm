package httpapi

import (
	"net/http"
	"tgstrm/internal/library"
	telegramauth "tgstrm/internal/telegram"
)

func (a *API) reconnectMirror(w http.ResponseWriter, r *http.Request) {
	if !a.libraryReady(w) {
		return
	}
	var in struct {
		SourceTopicID string `json:"source_topic_id"`
		Confirm       bool   `json:"confirm"`
	}
	if !decodeLibraryRequest(w, r, &in) {
		return
	}
	var oldSource, destination, oldTitle, newTitle, sourceGroup string
	err := a.db.QueryRowContext(r.Context(), `SELECT ml.source_topic_id::text,ml.destination_topic_id::text,old.title,new.title,new.chat_id::text FROM tgstrm.mirror_links ml JOIN tgstrm.topics old ON old.id=ml.source_topic_id JOIN tgstrm.topics dest ON dest.id=ml.destination_topic_id JOIN tgstrm.chats dc ON dc.id=dest.chat_id JOIN tgstrm.topics new ON new.id::text=$2 JOIN tgstrm.chats sc ON sc.id=new.chat_id WHERE ml.id::text=$1 AND ml.retired_at IS NULL AND ml.status IN ('blocked','paused') AND new.is_active AND sc.is_active AND sc.account_id=$3 AND dest.is_active AND dc.is_active AND dc.account_id=$3 AND new.chat_id<>dest.chat_id AND new.id<>old.id AND NOT EXISTS(SELECT 1 FROM tgstrm.managed_catalog_chats mc WHERE mc.id=new.chat_id)`, r.PathValue("linkID"), in.SourceTopicID, telegramauth.CatalogAccountID).Scan(&oldSource, &destination, &oldTitle, &newTitle, &sourceGroup)
	if err != nil {
		fail(w, 422, "invalid_reconnection", "Elige un origen externo accesible para un vínculo pausado o bloqueado.")
		return
	}
	if !in.Confirm {
		writeJSON(w, 200, map[string]any{"previous_source": oldTitle, "new_source": newTitle, "destination_topic_id": destination, "copies_preserved": true, "history_mode": "include_existing"})
		return
	}
	if a.telegram == nil {
		fail(w, 503, "telegram_unavailable", "Conecta Telegram para verificar el origen.")
		return
	}
	manifest, _, _, err := a.telegram.ScanPersistedTopicEligibility(r.Context(), sourceGroup, in.SourceTopicID)
	if err != nil || library.TopicEligibility(manifest.Evidence) != "eligible" {
		fail(w, 422, "source_ineligible", "El origen nuevo debe tener su historial completo y solo archivos de vídeo válidos.")
		return
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 500, "reconnection_failed", "No se pudo guardar la reconexión.")
		return
	}
	defer tx.Rollback()
	var previous string
	if err = tx.QueryRowContext(r.Context(), `SELECT source_topic_id::text FROM tgstrm.mirror_links WHERE id::text=$1 AND status IN ('blocked','paused') AND retired_at IS NULL FOR UPDATE`, r.PathValue("linkID")).Scan(&previous); err != nil || previous != oldSource {
		fail(w, 409, "link_changed", "El vínculo cambió. Repite la revisión.")
		return
	}
	var id string
	err = tx.QueryRowContext(r.Context(), `INSERT INTO tgstrm.mirror_links(source_topic_id,destination_topic_id,history_mode,history_status,start_after_message_id,replaces_link_id) VALUES($1::bigint,$2::bigint,'include_existing','pending',0,$3::uuid) RETURNING id::text`, in.SourceTopicID, destination, r.PathValue("linkID")).Scan(&id)
	if err != nil {
		fail(w, 409, "mirror_conflict", "El destino o ese origen ya tienen un vínculo. No se ha cambiado el anterior.")
		return
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE tgstrm.mirror_links SET status='paused',retired_at=now(),updated_at=now() WHERE id::text=$1`, r.PathValue("linkID")); err != nil || tx.Commit() != nil {
		fail(w, 500, "reconnection_failed", "No se pudo confirmar la reconexión.")
		return
	}
	a.telegram.WakeMirrorWorker()
	writeJSON(w, 201, map[string]any{"id": id, "copies_preserved": true})
}
