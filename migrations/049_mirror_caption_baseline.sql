-- A source caption and its normalized destination are intentionally different.
-- Compare source content, not the destination or replayed edit timestamps.
ALTER TABLE tgstrm.mirror_deliveries ADD COLUMN source_caption_snapshot text;
UPDATE tgstrm.mirror_deliveries md
SET source_caption_snapshot=COALESCE(source.body,'')
FROM tgstrm.messages source
WHERE source.id=md.source_message_id AND md.status='completed';
