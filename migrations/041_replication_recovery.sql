ALTER TABLE tgstrm.collection_replication_plans ADD COLUMN confirmed_at timestamptz;
ALTER TABLE tgstrm.catalog_import_messages ADD COLUMN destination_telegram_message_id integer;
ALTER TABLE tgstrm.catalog_import_messages ADD COLUMN destination_deleted_at timestamptz;
ALTER TABLE tgstrm.mirror_deliveries ADD COLUMN destination_deleted_at timestamptz;
ALTER TABLE tgstrm.mirror_deliveries ADD COLUMN last_applied_caption text;
ALTER TABLE tgstrm.mirror_deliveries ADD COLUMN edit_conflict boolean NOT NULL DEFAULT false;
-- Keep provenance when an origin disappears; runtime deletions are soft.
ALTER TABLE tgstrm.mirror_links DROP CONSTRAINT mirror_links_source_topic_id_fkey;
ALTER TABLE tgstrm.mirror_links ADD CONSTRAINT mirror_links_source_topic_id_fkey FOREIGN KEY(source_topic_id) REFERENCES tgstrm.topics(id) ON DELETE RESTRICT;
ALTER TABLE tgstrm.mirror_deliveries DROP CONSTRAINT mirror_deliveries_source_message_id_fkey;
ALTER TABLE tgstrm.mirror_deliveries ADD CONSTRAINT mirror_deliveries_source_message_id_fkey FOREIGN KEY(source_message_id) REFERENCES tgstrm.messages(id) ON DELETE RESTRICT;

CREATE FUNCTION tgstrm.mark_replication_destination_deleted() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.is_active AND NOT NEW.is_active THEN
  UPDATE tgstrm.mirror_deliveries SET destination_deleted_at=now(),updated_at=now()
  WHERE destination_message_id=NEW.id;
  UPDATE tgstrm.catalog_import_messages SET destination_deleted_at=now(),updated_at=now()
  WHERE destination_message_id=NEW.id;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER mark_replication_destination_deleted AFTER UPDATE OF is_active ON tgstrm.messages FOR EACH ROW EXECUTE FUNCTION tgstrm.mark_replication_destination_deleted();

CREATE OR REPLACE VIEW tgstrm.managed_catalog_chats AS
SELECT c.* FROM tgstrm.chats c WHERE
 EXISTS(SELECT 1 FROM tgstrm.collection_groups cg JOIN tgstrm.library_collections lc ON lc.id=cg.collection_id
 WHERE cg.destination_chat_id=c.id AND cg.account_id=c.account_id AND cg.state='ready' AND lc.state='ready')
 OR EXISTS(SELECT 1 FROM tgstrm.catalog_targets ct WHERE ct.account_id=c.account_id AND ct.mode='group' AND ct.group_chat_id=c.id)
 OR EXISTS(SELECT 1 FROM tgstrm.settings s WHERE s.key='active_chat_id' AND s.value #>> '{}' = c.id::text)
 OR EXISTS(SELECT 1 FROM tgstrm.catalog_imports ci WHERE ci.destination_chat_id=c.id AND ci.status IN ('pending','running','failed'))
 OR EXISTS(SELECT 1 FROM tgstrm.mirror_links ml JOIN tgstrm.topics t ON t.id=ml.destination_topic_id WHERE t.chat_id=c.id AND ml.status='active');
