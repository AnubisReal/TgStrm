ALTER TABLE tgstrm.topic_identity_jobs
 ADD COLUMN identity_applied_revision bigint NOT NULL DEFAULT 0,
 ADD COLUMN captions_prepared_revision bigint NOT NULL DEFAULT 0;
CREATE TABLE tgstrm.topic_caption_corrections (
 topic_id bigint NOT NULL REFERENCES tgstrm.topics(id) ON DELETE RESTRICT,
 message_id bigint NOT NULL REFERENCES tgstrm.messages(id) ON DELETE RESTRICT,
 revision bigint NOT NULL,
 previous_caption text NOT NULL,
 desired_caption text NOT NULL,
 source_caption_snapshot text,
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','completed','attention')),
 last_error text,
 next_attempt_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(topic_id,message_id)
);

-- Manual episode assignments belong to the series that was confirmed.
CREATE FUNCTION tgstrm.discard_stale_series_episode_overrides() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.message_id IS NULL AND NEW.media_type='tv' AND OLD.tmdb_id IS DISTINCT FROM NEW.tmdb_id THEN
  DELETE FROM tgstrm.stream_episode_overrides eo USING tgstrm.stream_assets sa,tgstrm.messages m
  WHERE eo.asset_id=sa.id AND sa.message_id=m.id AND m.topic_id=NEW.topic_id AND eo.series_tmdb_id<>NEW.tmdb_id;
 END IF;
 RETURN NULL;
END $$;
CREATE TRIGGER tmdb_discard_stale_episode_overrides AFTER UPDATE ON tgstrm.tmdb_matches
 FOR EACH ROW EXECUTE FUNCTION tgstrm.discard_stale_series_episode_overrides();
