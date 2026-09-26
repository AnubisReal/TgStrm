ALTER TABLE tgstrm.message_media ADD COLUMN telegram_dc_id integer;
CREATE INDEX message_media_document_lookup ON tgstrm.message_media(telegram_file_id) WHERE telegram_file_id IS NOT NULL;
