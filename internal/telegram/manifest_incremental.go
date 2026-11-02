package telegram

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strconv"
	"time"

	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"tgstrm/internal/library"
)

// Match confirmation locks the plan first. Event transactions use the same
// order, making evidence invalidation and plan transition one atomic operation.
func lockManifestPlans(ctx context.Context, tx *sql.Tx, channel int64, thread int) error {
	rows, err := tx.QueryContext(ctx, `SELECT p.id FROM tgstrm.collection_replication_plans p WHERE EXISTS(
 SELECT 1 FROM tgstrm.topic_replications tr JOIN tgstrm.group_replications gr ON gr.id=tr.group_replication_id
 JOIN tgstrm.topics t ON t.id=tr.source_topic_id JOIN tgstrm.chats c ON c.id=t.chat_id
 WHERE gr.plan_id=p.id AND c.account_id=$1 AND c.telegram_peer_type='channel' AND c.telegram_peer_id=$2
 AND ($3=0 OR t.telegram_topic_id=$3)) ORDER BY p.id FOR UPDATE OF p`, accountID, channel, thread)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
	}
	return rows.Err()
}
func invalidateManifestScope(ctx context.Context, tx *sql.Tx, channel int64, thread int) error {
	_, err := tx.ExecContext(ctx, `UPDATE tgstrm.topic_replication_manifests mf SET status='pending',invalidated_at=now(),revision=mf.revision+1
 FROM tgstrm.topics t JOIN tgstrm.chats c ON c.id=t.chat_id WHERE mf.source_topic_id=t.id AND c.account_id=$1
 AND c.telegram_peer_type='channel' AND c.telegram_peer_id=$2 AND ($3=0 OR t.telegram_topic_id=$3)`, accountID, channel, thread)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE tgstrm.collection_replication_plans p SET status='preparing',revision=p.revision+1,next_attempt_at=now(),updated_at=now()
 WHERE (p.status='review' OR (p.confirmed_at IS NOT NULL AND p.status='completed')) AND EXISTS(
 SELECT 1 FROM tgstrm.topic_replications tr JOIN tgstrm.group_replications gr ON gr.id=tr.group_replication_id
 JOIN tgstrm.topic_replication_manifests mf ON mf.id=tr.manifest_id JOIN tgstrm.chats c ON c.id=gr.source_chat_id
 JOIN tgstrm.topics t ON t.id=tr.source_topic_id WHERE gr.plan_id=p.id AND tr.import_id IS NULL AND NOT tr.excluded
 AND mf.invalidated_at IS NOT NULL AND c.account_id=$1 AND c.telegram_peer_id=$2 AND ($3=0 OR t.telegram_topic_id=$3))`, accountID, channel, thread)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload)
 SELECT 'library.replication.progress','library',0,jsonb_build_object('plan_id',p.id::text,'reason','topic_content_changed')
 FROM tgstrm.collection_replication_plans p WHERE EXISTS(SELECT 1 FROM tgstrm.group_replications gr JOIN tgstrm.chats c ON c.id=gr.source_chat_id
 WHERE gr.plan_id=p.id AND c.account_id=$1 AND c.telegram_peer_id=$2)`, accountID, channel)
	return err
}

func manifestMessageSignature(message *tg.Message) string {
	// Transport credentials/file references and view counters do not alter
	// eligibility. Stable media identity, attributes, text and links do.
	var document any
	if media, ok := message.Media.(*tg.MessageMediaDocument); ok {
		document = struct {
			ID         int64
			Mime       string
			Size       int64
			Attributes []tg.DocumentAttributeClass
		}{}
		if raw, ok := media.GetDocument(); ok {
			if d, ok := raw.AsNotEmpty(); ok {
				document = struct {
					ID         int64
					Mime       string
					Size       int64
					Attributes []tg.DocumentAttributeClass
				}{d.ID, d.MimeType, d.Size, d.Attributes}
			}
		}
	}
	var references TopicManifest
	collectTopicReferences(&references, message)
	text := message.Message
	entities := message.Entities
	if document == nil {
		text = ""
		entities = nil
	}
	data, _ := json.Marshal(struct {
		Text       string
		Entities   []tg.MessageEntityClass
		Document   any
		References []library.TopicReference
	}{text, entities, document, references.Evidence.References})
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// Rechecks deliberately compare outside the scan generation: unknown deletes
// do not destroy valid evidence. A generation/recheck fence rejects stale reads.
func equivalentManifestEvidence(oldEvidence, oldVideos []byte, manifest TopicManifest) bool {
	var old library.TopicEvidence
	if json.Unmarshal(oldEvidence, &old) != nil {
		return false
	}
	old.ChangedSinceScan = false
	if !reflect.DeepEqual(old, manifest.Evidence) {
		return false
	}
	var videos []int
	if json.Unmarshal(oldVideos, &videos) != nil {
		return false
	}
	sort.Ints(videos)
	current := append([]int(nil), manifest.VideoIDs...)
	sort.Ints(current)
	if len(videos) != len(current) {
		return false
	}
	for i := range videos {
		if videos[i] != current[i] {
			return false
		}
	}
	var previous struct {
		Signatures map[string]string `json:"message_signatures"`
	}
	_ = json.Unmarshal(oldEvidence, &previous)
	if previous.Signatures != nil {
		return reflect.DeepEqual(previous.Signatures, manifest.MessageSignatures)
	}
	return true
}

func (s *Service) ReconcileManifestDeletion(ctx context.Context) error {
	var id, group, topic string
	var channel int64
	var thread int
	var generation, checkRevision int64
	var evidence, videos []byte
	err := s.DB.QueryRowContext(ctx, claimManifestRecheckSQL).Scan(&id, &group, &topic, &channel, &thread, &generation, &checkRevision, &evidence, &videos)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	manifest, err := s.ScanTopicEligibility(ctx, group, topic)
	if err != nil {
		delay := 30 * time.Second
		if wait, ok := tgerr.AsFloodWait(err); ok {
			delay = wait
		}
		_, saveErr := s.DB.ExecContext(ctx, `UPDATE tgstrm.manifest_rechecks SET next_attempt_at=now()+($3::double precision*interval '1 second'),last_error=$4 WHERE manifest_id::text=$1 AND revision=$2`, id, checkRevision, delay.Seconds(), err.Error())
		if saveErr != nil {
			return saveErr
		}
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = lockManifestPlans(ctx, tx, channel, thread); err != nil {
		return err
	}
	var current int64
	if err = tx.QueryRowContext(ctx, `SELECT revision FROM tgstrm.manifest_rechecks WHERE manifest_id::text=$1 FOR UPDATE`, id).Scan(&current); errors.Is(err, sql.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	if current != checkRevision {
		return nil
	}
	if err = tx.QueryRowContext(ctx, `SELECT revision FROM tgstrm.topic_replication_manifests WHERE id::text=$1 FOR UPDATE`, id).Scan(&current); err != nil {
		return err
	}
	if current != generation {
		_, err = tx.ExecContext(ctx, `UPDATE tgstrm.manifest_rechecks SET next_attempt_at=now()+interval '5 seconds' WHERE manifest_id::text=$1`, id)
		if err != nil {
			return err
		}
		return tx.Commit()
	}
	if !equivalentManifestEvidence(evidence, videos, manifest) {
		if err = invalidateManifestScope(ctx, tx, channel, thread); err != nil {
			return err
		}
	} else {
		data, err := marshalManifestEvidence(manifest)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE tgstrm.topic_replication_manifests SET evidence=$2::jsonb,upper_message_id=$3 WHERE id::text=$1`, id, string(data), manifest.UpperMessageID)
		if err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM tgstrm.manifest_rechecks WHERE manifest_id::text=$1 AND revision=$2`, id, checkRevision); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE tgstrm.collection_replication_plans p SET revision=p.revision+1,updated_at=now() WHERE EXISTS(SELECT 1 FROM tgstrm.topic_replications tr JOIN tgstrm.group_replications gr ON gr.id=tr.group_replication_id WHERE gr.plan_id=p.id AND tr.manifest_id::text=$1)`, id)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload)
 SELECT 'library.replication.progress','library',0,jsonb_build_object('plan_id',p.id::text,'reason','content_check_completed') FROM tgstrm.collection_replication_plans p
 WHERE EXISTS(SELECT 1 FROM tgstrm.topic_replications tr JOIN tgstrm.group_replications gr ON gr.id=tr.group_replication_id WHERE gr.plan_id=p.id AND tr.manifest_id::text=$1)`, id)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) invalidateManifestMessage(ctx context.Context, channel int64, thread int, message *tg.Message, edited bool) error {
	var ref TopicManifest
	collectTopicReferences(&ref, message)
	_, document := message.Media.(*tg.MessageMediaDocument)
	if !edited && !document && len(ref.Evidence.References) == 0 {
		return nil
	}
	signature := manifestMessageSignature(message)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = lockManifestPlans(ctx, tx, channel, thread); err != nil {
		return err
	}
	var changed bool
	err = tx.QueryRowContext(ctx, manifestMessageUpdateSQL, accountID, channel, thread, strconv.Itoa(message.ID), message.ID, signature).Scan(&changed)
	if err != nil {
		return err
	}
	if changed {
		if err = invalidateManifestScope(ctx, tx, channel, thread); err != nil {
			return err
		}
	}
	return tx.Commit()
}

const manifestMessageUpdateSQL = `WITH prior AS (SELECT c.id AS chat_id,CASE WHEN u.updated_at>mf.scanned_at THEN u.signature ELSE COALESCE(mf.evidence->'message_signatures'->>$4,u.signature,'') END AS signature
 FROM tgstrm.chats c LEFT JOIN tgstrm.manifest_message_updates u ON u.chat_id=c.id AND u.telegram_message_id=$5
 LEFT JOIN tgstrm.topics t ON t.chat_id=c.id AND t.telegram_topic_id=$3
 LEFT JOIN tgstrm.topic_replication_manifests mf ON mf.source_topic_id=t.id
 WHERE c.account_id=$1 AND c.telegram_peer_type='channel' AND c.telegram_peer_id=$2 AND mf.id IS NOT NULL), saved AS (
 INSERT INTO tgstrm.manifest_message_updates(chat_id,telegram_message_id,signature) SELECT chat_id,$5,$6 FROM prior
 ON CONFLICT(chat_id,telegram_message_id) DO UPDATE SET signature=EXCLUDED.signature,updated_at=now() RETURNING chat_id)
 SELECT EXISTS(SELECT 1 FROM prior WHERE signature<>$6)`

const claimManifestRecheckSQL = `WITH due AS (
 SELECT q.manifest_id FROM tgstrm.manifest_rechecks q JOIN tgstrm.topic_replication_manifests mf ON mf.id=q.manifest_id
 JOIN tgstrm.chats c ON c.id=mf.source_chat_id JOIN tgstrm.topics t ON t.id=mf.source_topic_id
 WHERE q.next_attempt_at<=now() AND c.is_active AND t.is_active
 ORDER BY EXISTS(SELECT 1 FROM tgstrm.topic_replications tr JOIN tgstrm.group_replications gr ON gr.id=tr.group_replication_id JOIN tgstrm.collection_replication_plans p ON p.id=gr.plan_id
 WHERE tr.manifest_id=mf.id AND tr.import_id IS NULL AND NOT tr.excluded AND p.status IN ('review','preparing')) DESC,q.next_attempt_at,q.manifest_id
 FOR UPDATE OF q SKIP LOCKED LIMIT 1
), claimed AS (UPDATE tgstrm.manifest_rechecks q SET next_attempt_at=now()+interval '2 minutes' FROM due
 WHERE q.manifest_id=due.manifest_id RETURNING q.manifest_id,q.revision)
 SELECT mf.id::text,c.id::text,t.id::text,c.telegram_peer_id,t.telegram_topic_id,mf.revision,q.revision,mf.evidence,mf.video_ids
 FROM claimed q JOIN tgstrm.topic_replication_manifests mf ON mf.id=q.manifest_id
 JOIN tgstrm.chats c ON c.id=mf.source_chat_id JOIN tgstrm.topics t ON t.id=mf.source_topic_id`
