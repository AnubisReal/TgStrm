-- Additive collection foundation. Does not switch active_chat_id or enable jobs.
-- Composite references prevent associations across Telegram accounts.
ALTER TABLE tgstrm.chats ADD CONSTRAINT chats_id_account_unique UNIQUE(id,account_id);

CREATE TABLE tgstrm.library_collections (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 account_id uuid NOT NULL REFERENCES tgstrm.telegram_accounts(id) ON DELETE RESTRICT,
 name text NOT NULL CHECK(char_length(btrim(name)) BETWEEN 1 AND 128),
 telegram_filter_id integer CHECK(telegram_filter_id > 0),
 state text NOT NULL DEFAULT 'draft' CHECK(state IN ('draft','ready','attention','archived')),
 series_name_format text NOT NULL DEFAULT 'Series {letra}' CHECK(char_length(btrim(series_name_format)) BETWEEN 1 AND 128),
 movies_name_format text NOT NULL DEFAULT 'Películas' CHECK(char_length(btrim(movies_name_format)) BETWEEN 1 AND 128),
 revision bigint NOT NULL DEFAULT 1 CHECK(revision > 0),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(id,account_id),
 UNIQUE(account_id,telegram_filter_id)
);

CREATE TABLE tgstrm.collection_groups (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 collection_id uuid NOT NULL,
 account_id uuid NOT NULL,
 destination_chat_id bigint NOT NULL,
 bucket text NOT NULL DEFAULT '' CHECK(bucket='' OR bucket IN ('0-9','OTHER') OR (char_length(bucket)=1 AND position(bucket IN 'ABCDEFGHIJKLMNÑOPQRSTUVWXYZ')>0)),
 media_kind text NOT NULL CHECK(media_kind IN ('series','movies')),
 part integer NOT NULL DEFAULT 1 CHECK(part>0),
 naming_mode text NOT NULL DEFAULT 'automatic' CHECK(naming_mode IN ('automatic','manual')),
 applied_naming_revision bigint CHECK(applied_naming_revision>0),
 last_applied_title text,
 state text NOT NULL DEFAULT 'ready' CHECK(state IN ('ready','paused','attention','archived')),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(collection_id,account_id) REFERENCES tgstrm.library_collections(id,account_id) ON DELETE RESTRICT,
 FOREIGN KEY(destination_chat_id,account_id) REFERENCES tgstrm.chats(id,account_id) ON DELETE RESTRICT,
 CHECK(media_kind<>'series' OR bucket<>''),
 UNIQUE(collection_id,destination_chat_id),
 UNIQUE(collection_id,media_kind,bucket,part)
);
CREATE INDEX collection_groups_destination ON tgstrm.collection_groups(destination_chat_id,account_id);
CREATE INDEX collection_groups_account ON tgstrm.collection_groups(account_id);

CREATE TABLE tgstrm.catalog_targets (
 account_id uuid PRIMARY KEY REFERENCES tgstrm.telegram_accounts(id) ON DELETE RESTRICT,
 mode text NOT NULL CHECK(mode IN ('group','collection')),
 group_chat_id bigint,
 collection_id uuid,
 revision bigint NOT NULL DEFAULT 1 CHECK(revision>0),
 updated_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(group_chat_id,account_id) REFERENCES tgstrm.chats(id,account_id) ON DELETE RESTRICT,
 FOREIGN KEY(collection_id,account_id) REFERENCES tgstrm.library_collections(id,account_id) ON DELETE RESTRICT,
 CHECK((mode='group' AND group_chat_id IS NOT NULL AND collection_id IS NULL)
    OR (mode='collection' AND collection_id IS NOT NULL AND group_chat_id IS NULL))
);
CREATE INDEX catalog_targets_group ON tgstrm.catalog_targets(group_chat_id,account_id);
CREATE INDEX catalog_targets_collection ON tgstrm.catalog_targets(collection_id,account_id);

-- No backfill or activation yet: legacy group configuration remains authoritative
-- until every existing worker is connected to the common scope resolver.
