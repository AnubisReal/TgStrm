package telegram

import (
	"context"
	"errors"
)

func (s *Service) ActivateReplicationMirror(ctx context.Context, sourceTopicID, destinationTopicID, importID string) error {
	if sourceTopicID == destinationTopicID || destinationTopicID == "" {
		return errors.New("destino de espejo no válido")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// A positive acknowledgement is reconciled to a real destination identity
	// before seeding the mirror. Never infer success from a prepared request.
	_, err = tx.ExecContext(ctx, `UPDATE tgstrm.catalog_import_messages cim SET destination_message_id=m.id FROM tgstrm.messages m WHERE cim.import_id::text=$1 AND m.topic_id::text=$2 AND m.telegram_message_id=cim.destination_telegram_message_id AND cim.destination_message_id IS NULL`, importID, destinationTopicID)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE tgstrm.catalog_import_messages cim SET destination_message_id=matched.id,destination_telegram_message_id=matched.telegram_message_id FROM tgstrm.messages source JOIN tgstrm.message_media sm ON sm.message_id=source.id JOIN LATERAL (
 SELECT min(target.id) id,min(target.telegram_message_id) telegram_message_id FROM tgstrm.messages target JOIN tgstrm.message_media tm ON tm.message_id=target.id WHERE target.topic_id::text=$2 AND target.is_active AND tm.telegram_file_id=sm.telegram_file_id AND target.body IS NOT DISTINCT FROM (SELECT caption.normalized_caption FROM tgstrm.catalog_import_messages caption WHERE caption.import_id::text=$1 AND caption.source_message_id=source.id) HAVING count(*)=1
 ) matched ON true WHERE cim.import_id::text=$1 AND cim.source_message_id=source.id AND cim.status='completed' AND cim.destination_telegram_message_id IS NULL AND cim.destination_deleted_at IS NULL`, importID, destinationTopicID)
	if err != nil {
		return err
	}
	var unverified bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tgstrm.catalog_import_messages WHERE import_id::text=$1 AND status='completed' AND destination_telegram_message_id IS NULL AND destination_deleted_at IS NULL)`, importID).Scan(&unverified); err != nil {
		return err
	}
	if unverified {
		return errors.New("hay entregas sin identidad destino verificada; revisa el envío antes de activar el espejo")
	}
	var linkID string
	err = tx.QueryRowContext(ctx, `INSERT INTO tgstrm.mirror_links(source_topic_id,destination_topic_id,start_after_message_id,history_mode,history_status)
 SELECT tr.source_topic_id,dt.id,COALESCE(tr.initial_upper_message_id,mf.upper_message_id),'new_only','not_requested'
 FROM tgstrm.topic_replications tr JOIN tgstrm.topic_replication_manifests mf ON mf.id=tr.manifest_id
 JOIN tgstrm.topics dt ON dt.id::text=$2 AND dt.is_active JOIN tgstrm.managed_catalog_chats c ON c.id=dt.chat_id AND c.is_active
 WHERE tr.source_topic_id::text=$1 AND tr.import_id::text=$3
 ON CONFLICT(source_topic_id,destination_topic_id) DO UPDATE SET updated_at=now() RETURNING id::text`, sourceTopicID, destinationTopicID, importID).Scan(&linkID)
	if err != nil {
		return err
	}
	// Import and mirror use a shared delivery record from this point on. Deleted
	// destination messages retain a tombstone and are never queued a second time.
	_, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.mirror_deliveries(link_id,source_message_id,random_id,status,destination_message_id,destination_telegram_message_id,last_applied_caption,source_edited_at,source_caption_snapshot,destination_deleted_at,completed_at)
 SELECT $1::uuid,cim.source_message_id,mod(abs(hashtextextended($1::text || ':' || cim.source_message_id::text,0)),9223372036854775806)+1,'completed',cim.destination_message_id,cim.destination_telegram_message_id,cim.normalized_caption,COALESCE(source.edited_at,source.sent_at),COALESCE(source.body,''),cim.destination_deleted_at,now()
 FROM tgstrm.catalog_import_messages cim JOIN tgstrm.messages source ON source.id=cim.source_message_id WHERE cim.import_id::text=$2 AND cim.status='completed'
 ON CONFLICT(link_id,source_message_id) DO NOTHING`, linkID, importID)
	if err != nil {
		return err
	}
	// Messages observed after the initial snapshot are enqueued only after the
	// imported ledger exists, so overlap cannot deliver the same source twice.
	_, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.mirror_deliveries(link_id,source_message_id,random_id)
 SELECT ml.id,m.id,mod(abs(hashtextextended(ml.id::text || ':' || m.id::text,0)),9223372036854775806)+1 FROM tgstrm.mirror_links ml JOIN tgstrm.messages m ON m.topic_id=ml.source_topic_id
 WHERE ml.id::text=$1 AND m.is_active AND m.media_type='video' AND m.telegram_message_id>ml.start_after_message_id ON CONFLICT(link_id,source_message_id) DO NOTHING`, linkID)
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	s.WakeMirrorWorker()
	return nil
}
