CREATE TABLE tgstrm.stream_assets (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 message_id bigint NOT NULL UNIQUE REFERENCES tgstrm.messages(id) ON DELETE RESTRICT,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO tgstrm.stream_assets(message_id)
SELECT m.id FROM tgstrm.messages m
WHERE m.media_type='video'
ON CONFLICT(message_id) DO NOTHING;

CREATE TABLE tgstrm.library_targets (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 name text NOT NULL,
 media_type text NOT NULL CHECK (media_type IN ('movies','shows')),
 root_path text NOT NULL,
 public_base_url text NOT NULL,
 auto_sync boolean NOT NULL DEFAULT false,
 is_enabled boolean NOT NULL DEFAULT true,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(root_path,media_type)
);

CREATE TABLE tgstrm.topic_library_rules (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 library_id uuid NOT NULL REFERENCES tgstrm.library_targets(id) ON DELETE CASCADE,
 topic_id bigint NOT NULL REFERENCES tgstrm.topics(id) ON DELETE CASCADE,
 tmdb_media_type text CHECK (tmdb_media_type IN ('movie','tv')),
 tmdb_id bigint,
 auto_sync boolean NOT NULL DEFAULT false,
 is_enabled boolean NOT NULL DEFAULT true,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(library_id,topic_id),
 CHECK ((tmdb_id IS NULL) = (tmdb_media_type IS NULL))
);

CREATE TABLE tgstrm.stream_exports (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 library_id uuid NOT NULL REFERENCES tgstrm.library_targets(id) ON DELETE CASCADE,
 asset_id uuid NOT NULL REFERENCES tgstrm.stream_assets(id) ON DELETE RESTRICT,
 relative_path text NOT NULL,
 content_hash bytea NOT NULL,
 status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','written','missing','error')),
 last_error text,
 written_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(library_id,asset_id),
 UNIQUE(library_id,relative_path)
);

CREATE INDEX topic_library_rules_topic ON tgstrm.topic_library_rules(topic_id) WHERE is_enabled;
CREATE INDEX stream_exports_status ON tgstrm.stream_exports(status,updated_at);
