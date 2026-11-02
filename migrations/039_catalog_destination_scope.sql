-- One shared membership contract for realtime, enrichment and STRM workers.
-- Managed destinations survive changing the active collection. Legacy group
-- settings are preserved; collection selection does not redirect queued work.
CREATE VIEW tgstrm.managed_catalog_chats AS
SELECT c.* FROM tgstrm.chats c WHERE
 EXISTS(SELECT 1 FROM tgstrm.collection_groups cg JOIN tgstrm.library_collections lc ON lc.id=cg.collection_id
        WHERE cg.destination_chat_id=c.id AND cg.account_id=c.account_id AND cg.state='ready' AND lc.state='ready')
 OR EXISTS(SELECT 1 FROM tgstrm.catalog_targets ct WHERE ct.account_id=c.account_id AND ct.mode='group' AND ct.group_chat_id=c.id)
 OR EXISTS(SELECT 1 FROM tgstrm.settings s WHERE s.key='active_chat_id' AND s.value #>> '{}' = c.id::text);

CREATE VIEW tgstrm.active_catalog_chats AS
SELECT c.* FROM tgstrm.chats c WHERE
 EXISTS(SELECT 1 FROM tgstrm.catalog_targets ct WHERE ct.account_id=c.account_id AND (
 (ct.mode='group' AND ct.group_chat_id=c.id) OR
 (ct.mode='collection' AND EXISTS(SELECT 1 FROM tgstrm.collection_groups cg
 JOIN tgstrm.library_collections lc ON lc.id=cg.collection_id WHERE cg.collection_id=ct.collection_id
 AND cg.destination_chat_id=c.id AND cg.account_id=c.account_id AND cg.state='ready' AND lc.state='ready'))))
 OR (NOT EXISTS(SELECT 1 FROM tgstrm.catalog_targets ct WHERE ct.account_id=c.account_id)
 AND EXISTS(SELECT 1 FROM tgstrm.settings s WHERE s.key='active_chat_id' AND s.value #>> '{}' = c.id::text));
