INSERT INTO tgstrm.settings(key,value)
VALUES('auto_export_started_at',to_jsonb(now()))
ON CONFLICT(key) DO NOTHING;

CREATE TABLE tgstrm.catalog_imports (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 source_topic_id bigint NOT NULL REFERENCES tgstrm.topics(id) ON DELETE RESTRICT,
 destination_topic_id bigint REFERENCES tgstrm.topics(id) ON DELETE SET NULL,
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','review','running','completed','failed')),
 tmdb_media_type text CHECK(tmdb_media_type IN ('movie','tv')),
 tmdb_id bigint,
 requested_count integer NOT NULL DEFAULT 0,
 completed_count integer NOT NULL DEFAULT 0,
 last_error text,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 completed_at timestamptz,
 UNIQUE(source_topic_id,destination_topic_id)
);

CREATE TABLE tgstrm.catalog_import_messages (
 import_id uuid NOT NULL REFERENCES tgstrm.catalog_imports(id) ON DELETE CASCADE,
 source_message_id bigint NOT NULL REFERENCES tgstrm.messages(id) ON DELETE RESTRICT,
 destination_message_id bigint REFERENCES tgstrm.messages(id) ON DELETE SET NULL,
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','copying','completed','failed')),
 normalized_caption text,
 last_error text,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(import_id,source_message_id)
);
