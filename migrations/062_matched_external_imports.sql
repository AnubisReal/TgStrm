-- Persist scope with the job. Explicit external destinations do not join the
-- catalogue merely because a matching/copy job exists for them.
ALTER TABLE tgstrm.catalog_imports ADD COLUMN catalogue_enabled boolean NOT NULL DEFAULT true;
CREATE OR REPLACE VIEW tgstrm.managed_catalog_chats AS
SELECT c.* FROM tgstrm.chats c WHERE
 EXISTS(SELECT 1 FROM tgstrm.collection_groups cg JOIN tgstrm.library_collections lc ON lc.id=cg.collection_id
 WHERE cg.destination_chat_id=c.id AND cg.account_id=c.account_id AND cg.state='ready' AND lc.state='ready')
 OR EXISTS(SELECT 1 FROM tgstrm.catalog_targets ct WHERE ct.account_id=c.account_id AND ct.mode='group' AND ct.group_chat_id=c.id)
 OR EXISTS(SELECT 1 FROM tgstrm.settings s WHERE s.key='active_chat_id' AND s.value #>> '{}' = c.id::text)
 OR EXISTS(SELECT 1 FROM tgstrm.catalog_imports ci WHERE ci.destination_chat_id=c.id AND ci.catalogue_enabled AND ci.status IN ('pending','running','failed'))
 OR EXISTS(SELECT 1 FROM tgstrm.mirror_links ml JOIN tgstrm.topics t ON t.id=ml.destination_topic_id WHERE t.chat_id=c.id AND ml.status='active');
