package telegram

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"tgstrm/internal/library"
)

var ErrTopicChangedDuringScan = errors.New("el topic cambió durante la comprobación; se revisará nuevamente")

// Review plans may share the same topic. Reuse only recent, complete evidence
// with no invalidation or outstanding deletion check. Execution still scans.
const recentReviewManifestSQL = `SELECT mf.id::text,mf.revision,mf.evidence,mf.video_ids,mf.upper_message_id
 FROM tgstrm.topic_replication_manifests mf JOIN tgstrm.topics t ON t.id=mf.source_topic_id JOIN tgstrm.chats c ON c.id=t.chat_id
 WHERE c.id::text=$1 AND t.id::text=$2 AND c.account_id=$3 AND c.is_active AND t.is_active
 AND mf.invalidated_at IS NULL AND mf.status IN ('eligible','excluded_nonvideo','empty','review')
 AND COALESCE((mf.evidence->>'complete')::boolean,false) AND mf.scanned_at>now()-interval '5 minutes'
 AND NOT EXISTS(SELECT 1 FROM tgstrm.manifest_rechecks q WHERE q.manifest_id=mf.id)`

func (s *Service) ReviewTopicEligibility(ctx context.Context, groupID, topicID string) (TopicManifest, string, int64, error) {
	var manifest TopicManifest
	var id string
	var revision int64
	var evidence, videos []byte
	err := s.DB.QueryRowContext(ctx, recentReviewManifestSQL, groupID, topicID, accountID).Scan(&id, &revision, &evidence, &videos, &manifest.UpperMessageID)
	if err == nil {
		var stored struct {
			library.TopicEvidence
			MessageIDs []int             `json:"message_ids"`
			Signatures map[string]string `json:"message_signatures"`
		}
		if json.Unmarshal(evidence, &stored) == nil && json.Unmarshal(videos, &manifest.VideoIDs) == nil {
			manifest.Evidence = stored.TopicEvidence
			manifest.MessageIDs = stored.MessageIDs
			manifest.MessageSignatures = stored.Signatures
			return manifest, id, revision, nil
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return manifest, "", 0, err
	}
	return s.ScanPersistedTopicEligibility(ctx, groupID, topicID)
}

// SaveTopicManifest persists review evidence only. Confirmation must revalidate
// its revision and source changes; this method never queues an import.
func (s *Service) SaveTopicManifest(ctx context.Context, groupID, topicID string, manifest TopicManifest) (string, int64, error) {
	evidence, err := marshalManifestEvidence(manifest)
	if err != nil {
		return "", 0, err
	}
	if manifest.VideoIDs == nil {
		manifest.VideoIDs = []int{}
	}
	videos, err := json.Marshal(manifest.VideoIDs)
	if err != nil {
		return "", 0, err
	}
	var id string
	var revision int64
	err = s.DB.QueryRowContext(ctx, `INSERT INTO tgstrm.topic_replication_manifests
 (account_id,source_chat_id,source_topic_id,status,evidence,video_ids,upper_message_id)
 SELECT $1,c.id,t.id,$4,$5::jsonb,$6::jsonb,$7 FROM tgstrm.chats c JOIN tgstrm.topics t ON t.chat_id=c.id
 WHERE c.id::text=$2 AND t.id::text=$3 AND c.account_id=$1 AND c.is_active AND t.is_active
 ON CONFLICT(account_id,source_topic_id) DO UPDATE SET status=EXCLUDED.status,evidence=EXCLUDED.evidence,
 video_ids=EXCLUDED.video_ids,upper_message_id=EXCLUDED.upper_message_id,
 revision=tgstrm.topic_replication_manifests.revision+1,scanned_at=now(),invalidated_at=NULL
 RETURNING id::text,revision`, accountID, groupID, topicID, library.TopicEligibility(manifest.Evidence), string(evidence), string(videos), manifest.UpperMessageID).Scan(&id, &revision)
	return id, revision, err
}

// ScanPersistedTopicEligibility uses a generation fence. An update received
// during the remote scan increments that generation, so a late scan cannot
// overwrite the invalidation and authorize a stale copy.
func (s *Service) ScanPersistedTopicEligibility(ctx context.Context, groupID, topicID string) (TopicManifest, string, int64, error) {
	// A known wait is not a new scan: keep the generation and evidence intact.
	if _, err := s.historyReadGate.read(ctx, nil); err != nil {
		return TopicManifest{}, "", 0, err
	}
	id, revision, err := s.beginManifestScan(ctx, groupID, topicID)
	if err != nil {
		return TopicManifest{}, "", 0, err
	}
	manifest, scanErr := s.ScanTopicEligibility(ctx, groupID, topicID)
	if scanErr != nil {
		manifest.Evidence.ScanFailed = true
	}
	evidence, err := marshalManifestEvidence(manifest)
	if err != nil {
		return manifest, id, revision, err
	}
	if manifest.VideoIDs == nil {
		manifest.VideoIDs = []int{}
	}
	videos, err := json.Marshal(manifest.VideoIDs)
	if err != nil {
		return manifest, id, revision, err
	}
	result, err := s.DB.ExecContext(ctx, `UPDATE tgstrm.topic_replication_manifests SET status=$3,evidence=$4::jsonb,video_ids=$5::jsonb,upper_message_id=$6,scanned_at=now() WHERE id::text=$1 AND revision=$2 AND invalidated_at IS NULL`, id, revision, library.TopicEligibility(manifest.Evidence), string(evidence), string(videos), manifest.UpperMessageID)
	if err != nil {
		return manifest, id, revision, err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		manifest.Evidence.ChangedSinceScan = true
		return manifest, id, revision, ErrTopicChangedDuringScan
	}
	return manifest, id, revision, scanErr
}

func (s *Service) invalidateTopicManifests(ctx context.Context, channelID int64, threadID int) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = lockManifestPlans(ctx, tx, channelID, threadID); err != nil {
		return err
	}
	if err = invalidateManifestScope(ctx, tx, channelID, threadID); err != nil {
		return err
	}
	return tx.Commit()
}

// Additive JSON evidence retains existing manifests and records every examined
// message, including archives and unknown documents absent from the video catalog.
func marshalManifestEvidence(manifest TopicManifest) ([]byte, error) {
	return json.Marshal(struct {
		library.TopicEvidence
		MessageIDs []int             `json:"message_ids"`
		Signatures map[string]string `json:"message_signatures,omitempty"`
	}{manifest.Evidence, manifest.MessageIDs, manifest.MessageSignatures})
}

const beginManifestScanSQL = `INSERT INTO tgstrm.topic_replication_manifests(account_id,source_chat_id,source_topic_id,status,evidence,video_ids)
 SELECT $1,c.id,t.id,'pending','{}'::jsonb,'[]'::jsonb FROM tgstrm.chats c JOIN tgstrm.topics t ON t.chat_id=c.id
 WHERE c.id::text=$2 AND t.id::text=$3 AND c.account_id=$1 AND c.is_active AND t.is_active
 ON CONFLICT(account_id,source_topic_id) DO UPDATE SET status='pending',revision=tgstrm.topic_replication_manifests.revision+1,invalidated_at=NULL
 RETURNING id::text,revision`

func (s *Service) beginManifestScan(ctx context.Context, groupID, topicID string) (string, int64, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", 0, err
	}
	defer tx.Rollback()
	var channel int64
	var thread int
	if err = tx.QueryRowContext(ctx, `SELECT c.telegram_peer_id,t.telegram_topic_id FROM tgstrm.chats c JOIN tgstrm.topics t ON t.chat_id=c.id WHERE c.id::text=$1 AND t.id::text=$2 AND c.account_id=$3`, groupID, topicID, accountID).Scan(&channel, &thread); err != nil {
		return "", 0, err
	}
	if err = lockManifestPlans(ctx, tx, channel, thread); err != nil {
		return "", 0, err
	}
	var id string
	var revision int64
	if err = tx.QueryRowContext(ctx, beginManifestScanSQL, accountID, groupID, topicID).Scan(&id, &revision); err != nil {
		return "", 0, err
	}
	return id, revision, tx.Commit()
}
