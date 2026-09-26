ALTER TABLE tgstrm.tmdb_matches ADD COLUMN backdrop_path text;

CREATE TABLE tgstrm.tmdb_topic_scan_states (
 topic_id bigint PRIMARY KEY REFERENCES tgstrm.topics(id) ON DELETE CASCADE,
 source_text text NOT NULL,
 status text NOT NULL CHECK (status IN ('matched','ambiguous','not_found','failed')),
 checked_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
