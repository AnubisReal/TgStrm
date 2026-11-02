package telegram

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

type ImportResult struct {
	ImportID           string `json:"import_id"`
	DestinationGroupID string `json:"destination_group_id"`
	DestinationTopicID string `json:"destination_topic_id"`
	Copied             int    `json:"copied"`
	Requested          int    `json:"requested"`
}

type importDocument struct {
	messageID     string
	telegramID    int
	documentID    int64
	accessHash    int64
	fileReference []byte
	dcID          int
	fileName      string
	caption       string
}

var normalizedImportEpisodePattern = regexp.MustCompile(`(?i)(?:^|[^[:alnum:]])S(\d{1,2})E(\d{1,3})(?:[^[:digit:]]|$)`)

func importEpisodeOrder(value string) (season, episode int, ok bool) {
	match := normalizedImportEpisodePattern.FindStringSubmatch(value)
	if len(match) != 3 {
		return 0, 0, false
	}
	season, seasonErr := strconv.Atoi(match[1])
	episode, episodeErr := strconv.Atoi(match[2])
	return season, episode, seasonErr == nil && episodeErr == nil && season >= 0 && episode > 0
}

var filenameImportEpisodePattern = regexp.MustCompile(`(?i)(?:^|[^[:alnum:]])(?:T|S)(\d{1,2})[ ._-]+(?:E)?(\d{1,3})(?:[^[:digit:]]|$)`)

func documentEpisodeOrder(d importDocument) (int, int, bool) {
	if season, episode, ok := importEpisodeOrder(d.caption); ok {
		return season, episode, true
	}
	if season, episode, ok := importEpisodeOrder(d.fileName); ok {
		return season, episode, true
	}
	match := filenameImportEpisodePattern.FindStringSubmatch(d.fileName)
	if len(match) != 3 {
		return 0, 0, false
	}
	season, e1 := strconv.Atoi(match[1])
	episode, e2 := strconv.Atoi(match[2])
	return season, episode, e1 == nil && e2 == nil && episode > 0
}

func sortImportDocuments(docs []importDocument) {
	sort.SliceStable(docs, func(i, j int) bool {
		leftSeason, leftEpisode, leftOK := documentEpisodeOrder(docs[i])
		rightSeason, rightEpisode, rightOK := documentEpisodeOrder(docs[j])
		if leftOK != rightOK {
			return leftOK
		}
		if leftOK {
			if leftSeason != rightSeason {
				return leftSeason < rightSeason
			}
			if leftEpisode != rightEpisode {
				return leftEpisode < rightEpisode
			}
		}
		return docs[i].telegramID < docs[j].telegramID
	})
}

func stableRandomID(parts ...string) int64 {
	h := fnv.New64a()
	for _, part := range parts {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	return int64(h.Sum64() & 0x7fffffffffffffff)
}

func (s *Service) publishImportProgress(ctx context.Context, importID, sourceTopicID string) {
	var status string
	var requested, completed, failed int
	if s.DB.QueryRowContext(ctx, `SELECT ci.status,ci.requested_count,ci.completed_count,
		count(cim.source_message_id) FILTER (WHERE cim.status='failed')
		FROM tgstrm.catalog_imports ci LEFT JOIN tgstrm.catalog_import_messages cim ON cim.import_id=ci.id
		WHERE ci.id=$1 GROUP BY ci.id`, importID).Scan(&status, &requested, &completed, &failed) != nil {
		return
	}
	payload, _ := json.Marshal(map[string]any{"import_id": importID, "status": status, "requested": requested, "completed": completed, "failed": failed})
	_, _ = s.DB.ExecContext(ctx, `INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload)
		VALUES('catalog.import.progress','topic',$1,$2)`, sourceTopicID, payload)
}

// RetryCatalogImport puts the persisted manifest back on the canonical queue.
// The queue worker owns claims and restart recovery; the HTTP handler never
// starts a second delivery path.
func (s *Service) RetryCatalogImport(ctx context.Context, importID string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var manifestCount int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM tgstrm.catalog_import_messages
		WHERE import_id=$1::uuid AND selected_for_run`, importID).Scan(&manifestCount); err != nil {
		return err
	}
	if manifestCount == 0 {
		return errors.New("la importación no conserva archivos para reintentar")
	}
	var sourceTopicID string
	err = tx.QueryRowContext(ctx, `UPDATE tgstrm.catalog_imports SET status='pending',last_error=NULL,
		lease_token=NULL,lease_expires_at=NULL,next_attempt_at=now(),updated_at=now()
		WHERE id=$1::uuid AND status='failed' RETURNING source_topic_id::text`, importID).Scan(&sourceTopicID)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("la importación ya está siendo procesada")
	}
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tgstrm.catalog_import_messages SET status='pending',last_error=NULL,updated_at=now()
		WHERE import_id=$1::uuid AND selected_for_run AND status='failed'`, importID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	s.publishImportProgress(ctx, importID, sourceTopicID)
	return nil
}

// ImportTopic re-sends Telegram's existing documents without downloading
// them. Stable random IDs and persisted item state make retries idempotent.
func (s *Service) ImportTopic(ctx context.Context, sourceGroupID, sourceTopicID, destinationTitle string, selected []string, captions map[string]string) (ImportResult, error) {
	return s.ImportTopicPrepared(ctx, sourceGroupID, sourceTopicID, destinationTitle, selected, captions, nil)
}

// ImportTopicPrepared exposes the durable destination as soon as it exists.
// The callback runs before the first media send, allowing catalog metadata to
// become visible independently from a potentially long Telegram batch.
func (s *Service) ImportTopicPrepared(ctx context.Context, sourceGroupID, sourceTopicID, destinationTitle string, selected []string, captions map[string]string, destinationReady func(ImportResult) error) (ImportResult, error) {
	return s.importTopicPrepared(ctx, "", sourceGroupID, sourceTopicID, "", destinationTitle, selected, captions, destinationReady)
}

// ImportQueuedTopicPrepared executes a catalog import that was durably queued
// before the HTTP request returned. Reusing that row keeps one canonical job
// identity across queueing, sending, restart recovery and post-processing.
func (s *Service) ImportQueuedTopicPrepared(ctx context.Context, importID, sourceGroupID, sourceTopicID, destinationTitle string, selected []string, captions map[string]string, destinationReady func(ImportResult) error) (ImportResult, error) {
	return s.ImportQueuedTopicPreparedToTopic(ctx, importID, sourceGroupID, sourceTopicID, "", destinationTitle, selected, captions, destinationReady)
}

// ImportQueuedTopicPreparedToTopic is the explicit-destination variant used by
// reviewed imports. The destination must be a topic in the configured primary
// group; an empty ID retains the legacy title-based create-or-reuse behavior.
func (s *Service) ImportQueuedTopicPreparedToTopic(ctx context.Context, importID, sourceGroupID, sourceTopicID, destinationTopicID, destinationTitle string, selected []string, captions map[string]string, destinationReady func(ImportResult) error) (ImportResult, error) {
	if strings.TrimSpace(importID) == "" {
		return ImportResult{}, errors.New("falta la identidad persistente de la importación")
	}
	return s.importTopicPrepared(ctx, importID, sourceGroupID, sourceTopicID, destinationTopicID, destinationTitle, selected, captions, destinationReady)
}

func (s *Service) importTopicPrepared(ctx context.Context, queuedImportID, sourceGroupID, sourceTopicID, requestedDestinationTopicID, destinationTitle string, selected []string, captions map[string]string, destinationReady func(ImportResult) error) (ImportResult, error) {
	s.importMu.Lock()
	defer s.importMu.Unlock()
	var sourceTitle string
	var sourcePeerID, sourcePeerHash int64
	err := s.DB.QueryRowContext(ctx, `SELECT t.title,c.telegram_peer_id,COALESCE(c.access_hash,0)
		FROM tgstrm.topics t JOIN tgstrm.chats c ON c.id=t.chat_id
		WHERE t.id::text=$1 AND t.chat_id::text=$2 AND t.is_active AND c.is_active AND c.account_id=$3`, sourceTopicID, sourceGroupID, accountID).
		Scan(&sourceTitle, &sourcePeerID, &sourcePeerHash)
	if err != nil {
		return ImportResult{}, err
	}
	if strings.TrimSpace(destinationTitle) == "" {
		destinationTitle = sourceTitle
	}
	if runes := []rune(destinationTitle); len(runes) > 128 {
		destinationTitle = string(runes[:128])
	}

	var destinationGroupID string
	var peerID, accessHash int64
	if queuedImportID != "" {
		err = s.DB.QueryRowContext(ctx, `SELECT c.id::text,c.telegram_peer_id,COALESCE(c.access_hash,0)
 FROM tgstrm.catalog_imports ci JOIN tgstrm.chats c ON c.id=ci.destination_chat_id
 WHERE ci.id::text=$1 AND c.account_id=$2 AND c.is_active AND c.is_forum`, queuedImportID, accountID).Scan(&destinationGroupID, &peerID, &accessHash)
	} else if requestedDestinationTopicID != "" {
		err = s.DB.QueryRowContext(ctx, `SELECT c.id::text,c.telegram_peer_id,COALESCE(c.access_hash,0)
 FROM tgstrm.topics t JOIN tgstrm.managed_catalog_chats c ON c.id=t.chat_id
 WHERE t.id::text=$1 AND t.is_active AND c.account_id=$2 AND c.is_active AND c.is_forum`, requestedDestinationTopicID, accountID).Scan(&destinationGroupID, &peerID, &accessHash)
	} else {
		err = s.DB.QueryRowContext(ctx, `SELECT c.id::text,c.telegram_peer_id,COALESCE(c.access_hash,0)
 FROM tgstrm.active_catalog_chats c WHERE c.account_id=$1 AND c.is_active AND c.is_forum
 AND (SELECT count(*) FROM tgstrm.active_catalog_chats WHERE account_id=$1 AND is_active AND is_forum)=1`, accountID).Scan(&destinationGroupID, &peerID, &accessHash)
	}
	if errors.Is(err, sql.ErrNoRows) && queuedImportID != "" {
		destinationGroupID, peerID, accessHash, err = s.refreshQueuedImportDestination(ctx, queuedImportID)
	}
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ImportResult{}, errors.New("el destino guardado no está disponible; elige un cajón de tu biblioteca")
		}
		return ImportResult{}, fmt.Errorf("comprobar destino guardado: %w", err)
	}

	s.apiMu.RLock()
	api := s.runtimeAPI
	s.apiMu.RUnlock()
	if api == nil {
		return ImportResult{}, errors.New("Telegram se está reconectando")
	}
	peer := &tg.InputPeerChannel{ChannelID: peerID, AccessHash: accessHash}

	_ = s.RefreshTopics(ctx, destinationGroupID)
	destinationTopicID := strings.TrimSpace(requestedDestinationTopicID)
	var destinationTelegramID int
	if destinationTopicID != "" {
		err = s.DB.QueryRowContext(ctx, `SELECT t.id::text,t.telegram_topic_id FROM tgstrm.topics t
			WHERE t.id::text=$1 AND t.chat_id::text=$2 AND t.is_active`, destinationTopicID, destinationGroupID).
			Scan(&destinationTopicID, &destinationTelegramID)
	} else {
		err = s.DB.QueryRowContext(ctx, `SELECT id::text,telegram_topic_id FROM tgstrm.topics
			WHERE chat_id::text=$1 AND is_active AND lower(title)=lower($2) ORDER BY id LIMIT 1`, destinationGroupID, destinationTitle).
			Scan(&destinationTopicID, &destinationTelegramID)
	}
	if errors.Is(err, sql.ErrNoRows) && strings.TrimSpace(requestedDestinationTopicID) == "" {
		// One creation identity per durable job: retries reuse it, but a new
		// import after the user deletes a topic must not replay its old creation.
		creationIdentity := sourceTopicID
		if queuedImportID != "" {
			creationIdentity = queuedImportID
		}
		_, err = api.MessagesCreateForumTopic(ctx, &tg.MessagesCreateForumTopicRequest{
			Peer: peer, Title: destinationTitle,
			RandomID: stableRandomID(creationIdentity, destinationGroupID, "topic"), IconColor: 0x6FB9F0,
		})
		if err != nil {
			return ImportResult{}, fmt.Errorf("crear topic: %w", err)
		}
		if err = s.RefreshTopics(ctx, destinationGroupID); err != nil {
			return ImportResult{}, err
		}
		err = s.DB.QueryRowContext(ctx, `SELECT id::text,telegram_topic_id FROM tgstrm.topics
			WHERE chat_id::text=$1 AND is_active AND lower(title)=lower($2) ORDER BY id DESC LIMIT 1`, destinationGroupID, destinationTitle).
			Scan(&destinationTopicID, &destinationTelegramID)
	}
	if err != nil {
		return ImportResult{}, err
	}

	if destinationTopicID == sourceTopicID {
		return ImportResult{}, errors.New("el topic de destino no puede ser el mismo que el origen")
	}

	importID := queuedImportID
	if importID == "" {
		err = s.DB.QueryRowContext(ctx, `INSERT INTO tgstrm.catalog_imports(source_topic_id,destination_topic_id,destination_title,status,started_at,attempt_count)
			VALUES($1,$2,$3,'running',now(),1) ON CONFLICT(source_topic_id,destination_topic_id)
			DO UPDATE SET destination_topic_id=EXCLUDED.destination_topic_id,destination_title=EXCLUDED.destination_title,
			status='running',started_at=now(),attempt_count=tgstrm.catalog_imports.attempt_count+1,last_error=NULL,updated_at=now()
			RETURNING id::text`, sourceTopicID, destinationTopicID, destinationTitle).Scan(&importID)
	} else {
		err = s.DB.QueryRowContext(ctx, `UPDATE tgstrm.catalog_imports
			SET destination_topic_id=$2,destination_title=$3,status='running',started_at=COALESCE(started_at,now()),last_error=NULL,updated_at=now()
			WHERE id=$1::uuid AND source_topic_id=$4::bigint AND status='running' RETURNING id::text`,
			queuedImportID, destinationTopicID, destinationTitle, sourceTopicID).Scan(&importID)
	}
	if err != nil {
		return ImportResult{}, err
	}
	prepared := ImportResult{ImportID: importID, DestinationGroupID: destinationGroupID, DestinationTopicID: destinationTopicID}
	if destinationReady != nil {
		if err = destinationReady(prepared); err != nil {
			return ImportResult{}, fmt.Errorf("preparar destino: %w", err)
		}
	}

	selectedSet := make(map[string]bool, len(selected))
	for _, id := range selected {
		selectedSet[id] = true
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT m.id::text,m.telegram_message_id,mm.telegram_file_id,mm.access_hash,
		mm.file_reference,COALESCE(mm.telegram_dc_id,0),COALESCE(mm.file_name,''),COALESCE(m.body,'')
		FROM tgstrm.messages m JOIN tgstrm.message_media mm ON mm.message_id=m.id
		WHERE m.topic_id::text=$1 AND m.is_active AND m.media_type='video' ORDER BY m.telegram_message_id`, sourceTopicID)
	if err != nil {
		return ImportResult{}, err
	}
	defer rows.Close()
	var docs []importDocument
	for rows.Next() {
		var d importDocument
		if err = rows.Scan(&d.messageID, &d.telegramID, &d.documentID, &d.accessHash, &d.fileReference, &d.dcID, &d.fileName, &d.caption); err != nil {
			return ImportResult{}, err
		}
		if len(selectedSet) == 0 || selectedSet[d.messageID] {
			if normalized := strings.TrimSpace(captions[d.messageID]); normalized != "" {
				d.caption = normalized
			}
			docs = append(docs, d)
		}
	}
	if err = rows.Err(); err != nil {
		return ImportResult{}, err
	}
	if len(docs) == 0 {
		return ImportResult{}, errors.New("no hay vídeos para enviar")
	}
	// Telegram orders newly sent messages by acceptance time. Send recognized
	// episodes in library order so S01E01 remains the oldest item in the topic.
	sortImportDocuments(docs)
	// Persist the complete manifest before sending the first document. A process
	// can stop between any two Telegram requests; startup recovery must know
	// about the documents that had not started yet, not only those already sent.
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return ImportResult{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tgstrm.catalog_imports SET requested_count=$2,updated_at=now() WHERE id=$1`, importID, len(docs)); err != nil {
		tx.Rollback()
		return ImportResult{}, err
	}
	for _, d := range docs {
		if _, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.catalog_import_messages(import_id,source_message_id,status,normalized_caption)
			VALUES($1,$2,'pending',$3) ON CONFLICT(import_id,source_message_id)
			DO UPDATE SET normalized_caption=EXCLUDED.normalized_caption,updated_at=now()`, importID, d.messageID, d.caption); err != nil {
			tx.Rollback()
			return ImportResult{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return ImportResult{}, err
	}
	s.publishImportProgress(ctx, importID, sourceTopicID)

	copied := 0
	for _, d := range docs {
		var blocked bool
		if err = s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tgstrm.topic_replications tr JOIN tgstrm.group_replications gr ON gr.id=tr.group_replication_id JOIN tgstrm.collection_replication_plans p ON p.id=gr.plan_id LEFT JOIN tgstrm.topic_replication_manifests mf ON mf.id=tr.manifest_id WHERE tr.import_id::text=$1 AND (p.status<>'running' OR mf.status<>'eligible' OR mf.invalidated_at IS NOT NULL OR mf.revision IS DISTINCT FROM tr.manifest_revision))`, importID).Scan(&blocked); err != nil {
			return ImportResult{}, err
		}
		if blocked {
			return ImportResult{}, errors.New("la réplica se pausó o el origen cambió; revisa su manifiesto antes de continuar")
		}
		var alreadyComplete bool
		_ = s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tgstrm.catalog_import_messages
			WHERE import_id=$1 AND source_message_id=$2 AND status='completed')`, importID, d.messageID).Scan(&alreadyComplete)
		if alreadyComplete {
			copied++
			continue
		}
		_, _ = s.DB.ExecContext(ctx, `INSERT INTO tgstrm.catalog_import_messages(import_id,source_message_id,status,normalized_caption)
			VALUES($1,$2,'copying',$3) ON CONFLICT(import_id,source_message_id)
			DO UPDATE SET status='copying',normalized_caption=EXCLUDED.normalized_caption,last_error=NULL,updated_at=now()`, importID, d.messageID, d.caption)

		send := func(documentID, documentHash int64, fileReference []byte) error {
			reply := &tg.InputReplyToMessage{ReplyToMsgID: destinationTelegramID}
			reply.SetTopMsgID(destinationTelegramID)
			updates, sendErr := api.MessagesSendMedia(ctx, &tg.MessagesSendMediaRequest{
				Peer: peer, ReplyTo: reply,
				Media:   &tg.InputMediaDocument{ID: &tg.InputDocument{ID: documentID, AccessHash: documentHash, FileReference: fileReference}},
				Message: d.caption, RandomID: stableRandomID(importID, d.messageID),
			})
			if sendErr == nil {
				var list []tg.UpdateClass
				switch result := updates.(type) {
				case *tg.Updates:
					list = result.Updates
				case *tg.UpdatesCombined:
					list = result.Updates
				}
				for _, item := range list {
					if update, ok := item.(*tg.UpdateMessageID); ok && update.RandomID == stableRandomID(importID, d.messageID) {
						if _, err := s.DB.ExecContext(ctx, `UPDATE tgstrm.catalog_import_messages SET destination_telegram_message_id=$3 WHERE import_id=$1 AND source_message_id=$2`, importID, d.messageID, update.ID); err != nil {
							return err
						}
					}
					if update, ok := item.(*tg.UpdateNewChannelMessage); ok {
						if message, ok := update.Message.(*tg.Message); ok {
							tx, storeErr := s.DB.BeginTx(ctx, nil)
							if storeErr != nil {
								return storeErr
							}
							_, storeErr = storeVideoMessage(ctx, tx, destinationGroupID, destinationTopicID, message)
							if storeErr == nil {
								_, storeErr = tx.ExecContext(ctx, `UPDATE tgstrm.catalog_import_messages SET destination_telegram_message_id=$3,destination_message_id=(SELECT id FROM tgstrm.messages WHERE chat_id::text=$4 AND telegram_message_id=$3) WHERE import_id=$1 AND source_message_id=$2`, importID, d.messageID, message.ID, destinationGroupID)
							}
							if storeErr != nil {
								tx.Rollback()
								return storeErr
							}
							if storeErr = tx.Commit(); storeErr != nil {
								return storeErr
							}
						}
					}
				}
			}
			return sendErr
		}
		documentID, documentHash, fileReference := d.documentID, d.accessHash, d.fileReference
		fileReferenceRefreshed := false
		var sendErr error
		for {
			sendErr = send(documentID, documentHash, fileReference)
			if tgerr.Is(sendErr, "FILE_REFERENCE_EXPIRED", "FILE_REFERENCE_INVALID") && !fileReferenceRefreshed {
				fresh, refreshErr := s.refreshExpiredVideoLocation(ctx, VideoFileLocation{
					VideoID: d.messageID, FileName: d.fileName, DocumentID: d.documentID, AccessHash: d.accessHash,
					FileReference: d.fileReference, DCID: d.dcID, GroupID: sourceGroupID, TopicID: sourceTopicID,
					MessageID: d.telegramID, ChannelID: sourcePeerID, ChannelHash: sourcePeerHash,
				})
				if refreshErr != nil {
					sendErr = refreshErr
					break
				}
				documentID, documentHash, fileReference = fresh.DocumentID, fresh.AccessHash, fresh.FileReference
				fileReferenceRefreshed = true
				continue
			}
			if _, limited := tgerr.AsFloodWait(sendErr); limited {
				var replication bool
				if err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tgstrm.topic_replications WHERE import_id::text=$1)`, importID).Scan(&replication); err != nil {
					return ImportResult{}, err
				}
				if replication {
					return ImportResult{}, sendErr
				}
			}
			flood, waitErr := tgerr.FloodWait(ctx, sendErr)
			if flood {
				// Telegram accepted the request rate only after this server-supplied
				// delay. Retry this exact item so ordering and progress stay intact.
				if ctx.Err() != nil {
					return ImportResult{}, ctx.Err()
				}
				continue
			}
			sendErr = waitErr
			break
		}
		if sendErr != nil {
			_, _ = s.DB.ExecContext(ctx, `UPDATE tgstrm.catalog_import_messages SET status='failed',last_error=$3,updated_at=now()
				WHERE import_id=$1 AND source_message_id=$2`, importID, d.messageID, fmt.Sprintf("%.500s", sendErr.Error()))
			s.publishImportProgress(ctx, importID, sourceTopicID)
			continue
		}
		copied++
		_, _ = s.DB.ExecContext(ctx, `UPDATE tgstrm.catalog_import_messages SET status='completed',updated_at=now()
			WHERE import_id=$1 AND source_message_id=$2`, importID, d.messageID)
		_, _ = s.DB.ExecContext(ctx, `UPDATE tgstrm.catalog_imports SET completed_count=$2,updated_at=now() WHERE id=$1`, importID, copied)
		s.publishImportProgress(ctx, importID, sourceTopicID)
	}
	status := "completed"
	if copied < len(docs) {
		status = "failed"
	}
	_, _ = s.DB.ExecContext(ctx, `UPDATE tgstrm.catalog_imports SET status=$2,completed_count=$3,
		completed_at=CASE WHEN $2='completed' THEN now() END,updated_at=now() WHERE id=$1`, importID, status, copied)
	s.publishImportProgress(ctx, importID, sourceTopicID)
	s.queueTopicRefresh(peerID)
	s.queueVideoRefresh(peerID, destinationTelegramID)
	return ImportResult{ImportID: importID, DestinationGroupID: destinationGroupID, DestinationTopicID: destinationTopicID, Copied: copied, Requested: len(docs)}, nil
}
