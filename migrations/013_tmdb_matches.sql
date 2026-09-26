CREATE TABLE tgstrm.tmdb_matches (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 topic_id bigint NOT NULL REFERENCES tgstrm.topics(id) ON DELETE CASCADE,
 message_id bigint REFERENCES tgstrm.messages(id) ON DELETE CASCADE,
 media_type text NOT NULL CHECK (media_type IN ('movie','tv')),
 tmdb_id bigint NOT NULL CHECK (tmdb_id > 0),
 title text NOT NULL,
 original_title text NOT NULL DEFAULT '',
 release_date date,
 poster_path text,
 overview text NOT NULL DEFAULT '',
 metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
 confirmed_at timestamptz NOT NULL DEFAULT now(),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX tmdb_matches_topic_show
 ON tgstrm.tmdb_matches(topic_id) WHERE message_id IS NULL;
CREATE UNIQUE INDEX tmdb_matches_message_movie
 ON tgstrm.tmdb_matches(message_id) WHERE message_id IS NOT NULL;
CREATE INDEX tmdb_matches_identity ON tgstrm.tmdb_matches(media_type,tmdb_id);
