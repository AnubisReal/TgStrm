-- Preserve deleted cabinet history without reserving its alphabetical slot.
ALTER TABLE tgstrm.collection_groups DROP CONSTRAINT collection_groups_collection_id_media_kind_bucket_part_key;
UPDATE tgstrm.collection_groups cg SET state='archived',updated_at=now()
FROM tgstrm.chats c WHERE c.id=cg.destination_chat_id AND NOT c.is_active;
CREATE UNIQUE INDEX collection_groups_live_part_key
ON tgstrm.collection_groups(collection_id,media_kind,bucket,part) WHERE state<>'archived';
