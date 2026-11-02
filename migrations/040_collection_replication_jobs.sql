ALTER TABLE tgstrm.catalog_imports ADD COLUMN destination_chat_id bigint REFERENCES tgstrm.chats(id) ON DELETE RESTRICT;
UPDATE tgstrm.catalog_imports ci SET destination_chat_id=t.chat_id FROM tgstrm.topics t WHERE ci.destination_topic_id=t.id;
-- Freeze the old target for queued jobs that have not created their topic yet.
UPDATE tgstrm.catalog_imports ci SET destination_chat_id=c.id FROM tgstrm.chats c,tgstrm.settings s
WHERE ci.destination_chat_id IS NULL AND s.key='active_chat_id' AND s.value #>> '{}' = c.id::text;
CREATE INDEX catalog_imports_destination_chat_idx ON tgstrm.catalog_imports(destination_chat_id);

CREATE TABLE tgstrm.collection_replication_plans (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 account_id uuid NOT NULL REFERENCES tgstrm.telegram_accounts(id) ON DELETE RESTRICT,
 collection_id uuid NOT NULL,
 revision bigint NOT NULL DEFAULT 1 CHECK(revision>0),
 status text NOT NULL DEFAULT 'preparing' CHECK(status IN ('preparing','review','running','paused','completed','attention')),
 last_error text,
 next_attempt_at timestamptz NOT NULL DEFAULT now(),
 lease_token uuid, lease_expires_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(collection_id,account_id) REFERENCES tgstrm.library_collections(id,account_id) ON DELETE RESTRICT,
 UNIQUE(id,account_id)
);
CREATE INDEX replication_plan_collection_idx ON tgstrm.collection_replication_plans(collection_id,account_id);
CREATE INDEX replication_plan_queue_idx ON tgstrm.collection_replication_plans(next_attempt_at,created_at) WHERE status IN ('preparing','running');
CREATE TABLE tgstrm.group_replications (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 plan_id uuid NOT NULL REFERENCES tgstrm.collection_replication_plans(id) ON DELETE RESTRICT,
 account_id uuid NOT NULL,
 source_chat_id bigint NOT NULL,
 destination_chat_id bigint,
 bucket text NOT NULL DEFAULT 'OTHER', media_kind text NOT NULL DEFAULT 'series' CHECK(media_kind IN ('series','movies')),
 part integer NOT NULL DEFAULT 1 CHECK(part>0), destination_title text NOT NULL DEFAULT '',
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','review','creating','running','completed','paused','attention','source_missing','access_lost','destination_missing')),
 discover_new_topics boolean NOT NULL DEFAULT true,
 last_error text, last_discovered_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(plan_id,account_id) REFERENCES tgstrm.collection_replication_plans(id,account_id) ON DELETE RESTRICT,
 FOREIGN KEY(source_chat_id,account_id) REFERENCES tgstrm.chats(id,account_id) ON DELETE RESTRICT,
 FOREIGN KEY(destination_chat_id,account_id) REFERENCES tgstrm.chats(id,account_id) ON DELETE RESTRICT,
 CHECK(source_chat_id IS DISTINCT FROM destination_chat_id),
 UNIQUE(plan_id,source_chat_id)
);
CREATE INDEX group_replications_source_idx ON tgstrm.group_replications(source_chat_id,account_id);
CREATE INDEX group_replications_destination_idx ON tgstrm.group_replications(destination_chat_id,account_id);
CREATE TABLE tgstrm.topic_replications (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 group_replication_id uuid NOT NULL REFERENCES tgstrm.group_replications(id) ON DELETE RESTRICT,
 source_topic_id bigint NOT NULL REFERENCES tgstrm.topics(id) ON DELETE RESTRICT,
 source_telegram_topic_id bigint NOT NULL,
 manifest_id uuid REFERENCES tgstrm.topic_replication_manifests(id) ON DELETE RESTRICT,
 manifest_revision bigint,
 destination_topic_id bigint REFERENCES tgstrm.topics(id) ON DELETE RESTRICT,
 import_id uuid REFERENCES tgstrm.catalog_imports(id) ON DELETE RESTRICT,
 match_payload jsonb, tmdb_media_type text NOT NULL DEFAULT 'tv' CHECK(tmdb_media_type IN ('tv','movie')),
 excluded boolean NOT NULL DEFAULT false,
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','eligible','excluded_nonvideo','empty','review','scan_failed','queued','completed','attention','destination_missing')),
 last_error text,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(group_replication_id,source_topic_id)
);
CREATE INDEX topic_replications_source_idx ON tgstrm.topic_replications(source_topic_id);
CREATE INDEX topic_replications_manifest_idx ON tgstrm.topic_replications(manifest_id);
CREATE INDEX topic_replications_destination_idx ON tgstrm.topic_replications(destination_topic_id);
CREATE INDEX topic_replications_import_idx ON tgstrm.topic_replications(import_id);
CREATE TABLE tgstrm.topic_presentations (
 topic_id bigint PRIMARY KEY REFERENCES tgstrm.topics(id) ON DELETE RESTRICT,
 random_id bigint NOT NULL UNIQUE,
 tmdb_id bigint NOT NULL CHECK(tmdb_id>0), media_type text NOT NULL CHECK(media_type IN ('tv','movie')),
 poster_path text NOT NULL,
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','sent','attention','skipped')),
 telegram_message_id bigint, last_error text,
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE tgstrm.collection_rename_jobs (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 collection_id uuid NOT NULL REFERENCES tgstrm.library_collections(id) ON DELETE RESTRICT,
 naming_revision bigint NOT NULL,
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','running','completed','attention','paused')),
 next_attempt_at timestamptz NOT NULL DEFAULT now(),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX collection_rename_jobs_collection_idx ON tgstrm.collection_rename_jobs(collection_id);
CREATE TABLE tgstrm.collection_rename_items (
 job_id uuid NOT NULL REFERENCES tgstrm.collection_rename_jobs(id) ON DELETE RESTRICT,
 group_id uuid NOT NULL REFERENCES tgstrm.collection_groups(id) ON DELETE RESTRICT,
 previous_title text NOT NULL, desired_title text NOT NULL,
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','completed','attention')),
 last_error text,
 PRIMARY KEY(job_id,group_id)
);
CREATE INDEX collection_rename_items_group_idx ON tgstrm.collection_rename_items(group_id);
