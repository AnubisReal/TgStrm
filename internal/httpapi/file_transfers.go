package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gotd/td/tgerr"
	"net/http"
	telegramauth "tgstrm/internal/telegram"
	"time"
)

func (a *API) fileTransferTargets(w http.ResponseWriter, r *http.Request) {
	service := a.telegramService()
	if service == nil {
		fail(w, 503, "telegram_unavailable", "Telegram no está disponible.")
		return
	}
	groups, err := service.Groups(r.Context())
	if err != nil {
		fail(w, 500, "targets_failed", "No se pudieron cargar los grupos.")
		return
	}
	writeJSON(w, 200, map[string]any{"groups": groups})
}
func (a *API) createFileTransfer(w http.ResponseWriter, r *http.Request) {
	service := a.telegramService()
	if service == nil || a.db == nil {
		fail(w, 503, "telegram_unavailable", "Telegram no está disponible.")
		return
	}
	var in struct {
		DestinationGroup string   `json:"destination_group_id"`
		DestinationTopic string   `json:"destination_topic_id"`
		MessageIDs       []string `json:"message_ids"`
		All              bool     `json:"all"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&in) != nil || in.DestinationGroup == "" || (!in.All && len(in.MessageIDs) == 0) || (in.All && len(in.MessageIDs) > 0) {
		fail(w, 400, "invalid_selection", "Selecciona archivos o el envío completo y un destino.")
		return
	}
	sourceGroup, sourceTopic := r.PathValue("groupID"), r.PathValue("topicID")
	var valid, forum bool
	err := a.db.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM tgstrm.topics t JOIN tgstrm.chats c ON c.id=t.chat_id WHERE t.id::text=$1 AND c.id::text=$2 AND c.account_id=$3 AND t.is_active AND c.is_active)`, sourceTopic, sourceGroup, telegramauth.CatalogAccountID).Scan(&valid)
	if err != nil || !valid {
		fail(w, 404, "source_missing", "El origen no está disponible.")
		return
	}
	err = a.db.QueryRowContext(r.Context(), `SELECT is_forum FROM tgstrm.chats WHERE id::text=$1 AND account_id=$2 AND is_active`, in.DestinationGroup, telegramauth.CatalogAccountID).Scan(&forum)
	if err != nil {
		fail(w, 422, "invalid_destination", "El grupo destino no está disponible.")
		return
	}
	if forum {
		if in.DestinationTopic == "" {
			fail(w, 422, "topic_required", "Selecciona un topic del grupo destino.")
			return
		}
		err = a.db.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM tgstrm.topics WHERE id::text=$1 AND chat_id::text=$2 AND is_active AND telegram_topic_id>0)`, in.DestinationTopic, in.DestinationGroup).Scan(&valid)
		if err != nil || !valid {
			fail(w, 422, "invalid_destination", "El topic destino no está disponible.")
			return
		}
	} else if in.DestinationTopic != "" {
		fail(w, 422, "invalid_destination", "Este grupo recibe los archivos directamente, sin topic.")
		return
	}
	if sourceTopic == in.DestinationTopic || sourceGroup == in.DestinationGroup && !forum {
		fail(w, 422, "same_destination", "Elige un destino distinto del origen.")
		return
	}
	if in.All {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
		err = service.SyncTopicVideoCatalog(ctx, sourceGroup, sourceTopic)
		cancel()
		if err != nil {
			fail(w, 502, "scan_failed", fmt.Sprintf("No se pudo completar el historial: %.250s", err))
			return
		}
	}
	rows, err := a.db.QueryContext(r.Context(), `SELECT id::text FROM tgstrm.messages WHERE topic_id::text=$1 AND is_active AND media_type='video' AND ($2 OR id::text=ANY($3::text[])) ORDER BY telegram_message_id,id`, sourceTopic, in.All, in.MessageIDs)
	if err != nil {
		fail(w, 500, "selection_failed", "No se pudieron leer los archivos.")
		return
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids = append(ids, id)
	}
	if err != nil || rows.Err() != nil {
		fail(w, 500, "selection_failed", "No se pudo leer la selección completa.")
		return
	}
	if len(ids) == 0 || !in.All && len(ids) != len(in.MessageIDs) {
		fail(w, 422, "invalid_selection", "La selección contiene archivos ausentes o que no son vídeos.")
		return
	}
	// Serialize identical source/destination submissions, making a double click
	// reuse its saved pending job instead of creating a second delivery.
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 500, "queue_failed", "No se pudo guardar el envío.")
		return
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(r.Context(), `SELECT pg_advisory_xact_lock(hashtext($1))`, sourceTopic+":"+in.DestinationGroup+":"+in.DestinationTopic)
	if err != nil {
		fail(w, 500, "queue_failed", "No se pudo guardar el envío.")
		return
	}
	var id string
	err = tx.QueryRowContext(r.Context(), `INSERT INTO tgstrm.file_transfers(source_topic_id,destination_chat_id,destination_topic_id,message_ids)
 SELECT $1::bigint,$2::bigint,NULLIF($3,'')::bigint,$4::bigint[] WHERE NOT EXISTS(SELECT 1 FROM tgstrm.file_transfers WHERE source_topic_id::text=$1 AND destination_chat_id::text=$2 AND COALESCE(destination_topic_id::text,'')=$3 AND status IN ('pending','running','attention')) RETURNING id::text`, sourceTopic, in.DestinationGroup, in.DestinationTopic, ids).Scan(&id)
	if err != nil {
		fail(w, 409, "transfer_exists", "Este origen ya tiene un envío pendiente a ese destino. Revisa su progreso antes de repetirlo.")
		return
	}
	if tx.Commit() != nil {
		fail(w, 500, "queue_failed", "No se pudo guardar el envío.")
		return
	}
	writeJSON(w, 202, map[string]any{"id": id, "requested": len(ids), "status": "pending"})
}

// Only unsent/uncertain jobs can be stopped. A running batch is never
// acknowledged as cancelled while Telegram may still accept its messages.
func (a *API) cancelFileTransfer(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		fail(w, 503, "database_unavailable", "PostgreSQL no está disponible.")
		return
	}
	result, err := a.db.ExecContext(r.Context(), `UPDATE tgstrm.file_transfers f SET status='cancelled',updated_at=now()
 WHERE f.id::text=$1 AND f.status IN ('pending','attention') AND EXISTS(SELECT 1 FROM tgstrm.topics t JOIN tgstrm.chats c ON c.id=t.chat_id WHERE t.id=f.source_topic_id AND c.account_id=$2)`, r.PathValue("transferID"), telegramauth.CatalogAccountID)
	if err != nil {
		fail(w, 500, "cancel_failed", "No se pudo detener el envío.")
		return
	}
	if n, _ := result.RowsAffected(); n != 1 {
		fail(w, 409, "transfer_running", "Espera a que termine el lote en curso antes de detenerlo.")
		return
	}
	writeJSON(w, 200, map[string]any{"status": "cancelled"})
}
func (a *API) fileTransferStatus(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		fail(w, 503, "database_unavailable", "PostgreSQL no está disponible.")
		return
	}
	var status, lastError string
	var completed, total int
	var next time.Time
	err := a.db.QueryRowContext(r.Context(), `SELECT f.status,COALESCE(f.last_error,''),f.completed_count,cardinality(f.message_ids),f.next_attempt_at FROM tgstrm.file_transfers f JOIN tgstrm.topics t ON t.id=f.source_topic_id JOIN tgstrm.chats c ON c.id=t.chat_id WHERE f.id::text=$1 AND c.account_id=$2`, r.PathValue("transferID"), telegramauth.CatalogAccountID).Scan(&status, &lastError, &completed, &total, &next)
	if err != nil {
		fail(w, 404, "transfer_missing", "El envío no está disponible.")
		return
	}
	writeJSON(w, 200, map[string]any{"status": status, "completed": completed, "requested": total, "last_error": lastError, "next_attempt_at": next})
}
func (a *API) runFileTransfers(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if a.telegram == nil || !a.telegram.Connected() {
				continue
			}
			work, cancel := context.WithTimeout(ctx, 90*time.Second)
			a.processFileTransfer(work)
			cancel()
		}
	}
}
func (a *API) processFileTransfer(ctx context.Context) {
	// An interrupted uncertain send requires inspection, never an automatic replay.
	_, _ = a.db.ExecContext(ctx, `UPDATE tgstrm.file_transfers SET status='attention',last_error='El envío se interrumpió sin confirmación. Comprueba el destino antes de repetirlo.',updated_at=now() WHERE status='running' AND lease_expires_at<now()`)
	var id, sourceGroup, sourceTopic, targetGroup, targetTopic string
	var completed int
	err := a.db.QueryRowContext(ctx, fileTransferClaimSQL).Scan(&id, &sourceGroup, &sourceTopic, &targetGroup, &targetTopic, &completed)
	if err != nil {
		return
	}
	rows, err := a.db.QueryContext(ctx, `SELECT m.telegram_message_id FROM tgstrm.file_transfers f CROSS JOIN LATERAL unnest(f.message_ids) WITH ORDINALITY x(id,ord) JOIN tgstrm.messages m ON m.id=x.id WHERE f.id::text=$1 AND x.ord>$2 ORDER BY x.ord LIMIT 100`, id, completed)
	ids := []int{}
	if err == nil {
		for rows.Next() {
			var n int
			if err = rows.Scan(&n); err != nil {
				break
			}
			ids = append(ids, n)
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
	}
	if err == nil && len(ids) == 0 {
		err = errors.New("el manifiesto guardado no contiene los archivos pendientes")
	}
	if err == nil {
		err = a.telegram.ForwardFileBatch(ctx, id, sourceGroup, targetGroup, targetTopic, ids)
	}
	if err != nil {
		if wait, ok := tgerr.AsFloodWait(err); ok {
			_, _ = a.db.ExecContext(context.Background(), `UPDATE tgstrm.file_transfers SET status='pending',last_error=$2,next_attempt_at=now()+($3::double precision*interval '1 second'),lease_expires_at=NULL,updated_at=now() WHERE id::text=$1`, id, err.Error(), wait.Seconds())
		} else {
			_, _ = a.db.ExecContext(context.Background(), `UPDATE tgstrm.file_transfers SET status='attention',last_error=$2,lease_expires_at=NULL,updated_at=now() WHERE id::text=$1`, id, fmt.Sprintf("%.500s", err))
		}
		return
	}
	_, _ = a.db.ExecContext(ctx, `UPDATE tgstrm.file_transfers SET completed_count=completed_count+$2,status=CASE WHEN completed_count+$2=cardinality(message_ids) THEN 'completed' ELSE 'pending' END,last_error=NULL,lease_expires_at=NULL,updated_at=now() WHERE id::text=$1`, id, len(ids))
}

const fileTransferClaimSQL = `UPDATE tgstrm.file_transfers f SET status='running',lease_expires_at=now()+interval '2 minutes',updated_at=now()
 WHERE f.id=(SELECT id FROM tgstrm.file_transfers WHERE status='pending' AND next_attempt_at<=now() ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1) AND f.status='pending'
 RETURNING f.id::text,(SELECT chat_id::text FROM tgstrm.topics WHERE id=f.source_topic_id),f.source_topic_id::text,f.destination_chat_id::text,COALESCE(f.destination_topic_id::text,''),f.completed_count`
