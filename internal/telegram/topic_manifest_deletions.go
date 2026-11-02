package telegram

import (
	"context"
	"database/sql"
	"sort"
)

// Resolve before removing catalogue rows. The channel ID scopes all lookups:
// Telegram message IDs are not globally unique. Retained inactive rows also
// resolve repeated deletion events without guessing another topic.
const deletedManifestTopicsSQL = `WITH source AS (
 SELECT id FROM tgstrm.chats WHERE account_id=$1 AND telegram_peer_type='channel' AND telegram_peer_id=$2
), deleted AS (SELECT DISTINCT unnest($3::integer[]) AS id)
SELECT d.id,t.telegram_topic_id FROM deleted d
JOIN tgstrm.messages m ON m.telegram_message_id=d.id
JOIN source c ON c.id=m.chat_id JOIN tgstrm.topics t ON t.id=m.topic_id
UNION
SELECT d.id,t.telegram_topic_id FROM deleted d JOIN tgstrm.topics t ON t.telegram_topic_id=d.id JOIN source c ON c.id=t.chat_id
UNION
SELECT d.id,t.telegram_topic_id FROM source c JOIN tgstrm.topic_replication_manifests mf ON mf.source_chat_id=c.id
JOIN tgstrm.topics t ON t.id=mf.source_topic_id
JOIN deleted d ON mf.video_ids @> jsonb_build_array(d.id) OR COALESCE(mf.evidence->'message_ids','[]'::jsonb) @> jsonb_build_array(d.id)`

type deletedMessageTopic struct{ message, topic int }

func deletedManifestScope(ids []int, resolved []deletedMessageTopic) ([]int, bool) {
	known := make(map[int]bool)
	topics := make(map[int]bool)
	for _, v := range resolved {
		known[v.message] = true
		topics[v.topic] = true
	}
	for _, id := range ids {
		if id > 0 && !known[id] {
			return nil, true
		}
	}
	result := make([]int, 0, len(topics))
	for topic := range topics {
		result = append(result, topic)
	}
	sort.Ints(result)
	return result, false
}

func (s *Service) invalidateDeletedMessageManifests(ctx context.Context, channelID int64, ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = invalidateDeletedMessageManifestsTx(ctx, tx, channelID, ids); err != nil {
		return err
	}
	return tx.Commit()
}
func invalidateDeletedMessageManifestsTx(ctx context.Context, tx *sql.Tx, channelID int64, ids []int) error {
	var err error
	if err = lockManifestPlans(ctx, tx, channelID, 0); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `INSERT INTO tgstrm.manifest_deleted_messages(chat_id,telegram_message_id)
 SELECT c.id,d.id FROM tgstrm.chats c CROSS JOIN (SELECT DISTINCT unnest($3::integer[]) AS id) d
 WHERE c.account_id=$1 AND c.telegram_peer_type='channel' AND c.telegram_peer_id=$2 AND d.id>0
 AND EXISTS(SELECT 1 FROM tgstrm.topic_replication_manifests mf WHERE mf.source_chat_id=c.id)
 ORDER BY c.id,d.id ON CONFLICT DO NOTHING RETURNING telegram_message_id`, accountID, channelID, ids)
	if err != nil {
		return err
	}
	var fresh []int
	for rows.Next() {
		var id int
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		fresh = append(fresh, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(fresh) == 0 {
		return nil
	}
	rows, err = tx.QueryContext(ctx, deletedManifestTopicsSQL, accountID, channelID, fresh)
	if err != nil {
		return err
	}
	known := map[int]bool{}
	topics := map[int]bool{}
	for rows.Next() {
		var v deletedMessageTopic
		if err = rows.Scan(&v.message, &v.topic); err != nil {
			rows.Close()
			return err
		}
		known[v.message] = true
		topics[v.topic] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	orderedTopics := make([]int, 0, len(topics))
	for topic := range topics {
		orderedTopics = append(orderedTopics, topic)
	}
	sort.Ints(orderedTopics)
	for _, topic := range orderedTopics {
		if err = invalidateManifestScope(ctx, tx, channelID, topic); err != nil {
			return err
		}
	}
	var unknown []int
	for _, id := range fresh {
		if !known[id] {
			unknown = append(unknown, id)
		}
	}
	if len(unknown) > 0 {
		// Complete inventories prove that older absent IDs never belonged to that
		// topic. Legacy history or newer IDs require verification, not invalidation.
		_, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.manifest_rechecks(manifest_id,next_attempt_at)
 SELECT mf.id,now()+interval '5 seconds' FROM tgstrm.topic_replication_manifests mf JOIN tgstrm.chats c ON c.id=mf.source_chat_id
 WHERE c.account_id=$1 AND c.telegram_peer_type='channel' AND c.telegram_peer_id=$2
 AND EXISTS(SELECT 1 FROM unnest($3::integer[]) d(id) WHERE NOT COALESCE((mf.evidence->>'complete')::boolean,false) OR NOT (mf.evidence ? 'message_ids') OR d.id>mf.upper_message_id)
 ON CONFLICT(manifest_id) DO UPDATE SET revision=tgstrm.manifest_rechecks.revision+1,next_attempt_at=now()+interval '5 seconds',updated_at=now(),last_error=NULL`, accountID, channelID, unknown)
		if err != nil {
			return err
		}
	}
	if len(unknown) > 0 {
		_, err = tx.ExecContext(ctx, `WITH changed AS (UPDATE tgstrm.collection_replication_plans p SET revision=p.revision+1,updated_at=now()
 WHERE EXISTS(SELECT 1 FROM tgstrm.topic_replications tr JOIN tgstrm.group_replications gr ON gr.id=tr.group_replication_id JOIN tgstrm.manifest_rechecks q ON q.manifest_id=tr.manifest_id JOIN tgstrm.chats c ON c.id=gr.source_chat_id
 WHERE gr.plan_id=p.id AND c.account_id=$1 AND c.telegram_peer_id=$2 AND NOT tr.excluded AND tr.import_id IS NULL) RETURNING id)
 INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload)
 SELECT 'library.replication.progress','library',0,jsonb_build_object('plan_id',id::text,'reason','checking_unknown_deletion') FROM changed`, accountID, channelID)
		if err != nil {
			return err
		}
	}
	return nil
}
