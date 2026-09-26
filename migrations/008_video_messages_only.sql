-- The catalog is intentionally video-only. Existing rows were populated before
-- media classification was strict and can be fetched again safely from Telegram.
DELETE FROM tgstrm.message_media;
DELETE FROM tgstrm.messages;

ALTER TABLE tgstrm.messages
 ADD CONSTRAINT messages_media_type_video_only CHECK (media_type = 'video');
