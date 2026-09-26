CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX chats_title_trgm ON tgstrm.chats USING gin (lower(title) gin_trgm_ops);
CREATE INDEX topics_title_trgm ON tgstrm.topics USING gin (lower(title) gin_trgm_ops) WHERE is_active;
CREATE INDEX messages_body_trgm ON tgstrm.messages USING gin (lower(body) gin_trgm_ops) WHERE is_active;
CREATE INDEX message_media_filename_trgm ON tgstrm.message_media USING gin (lower(file_name) gin_trgm_ops) WHERE file_name IS NOT NULL;
CREATE INDEX tmdb_matches_title_trgm ON tgstrm.tmdb_matches USING gin (lower(title) gin_trgm_ops);
CREATE INDEX tmdb_matches_original_title_trgm ON tgstrm.tmdb_matches USING gin (lower(original_title) gin_trgm_ops) WHERE original_title <> '';
