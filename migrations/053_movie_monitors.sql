-- Movie sources are explicit, independent from series mirrors. Empty registries
-- never enqueue or send anything. Existing import and mirror tables are intact.
CREATE TABLE tgstrm.movie_monitors (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 source_chat_id bigint NOT NULL REFERENCES tgstrm.chats(id) ON DELETE RESTRICT,
 source_topic_id bigint REFERENCES tgstrm.topics(id) ON DELETE RESTRICT,
 destination_chat_id bigint NOT NULL REFERENCES tgstrm.chats(id) ON DELETE RESTRICT,
 destination_topic_id bigint REFERENCES tgstrm.topics(id) ON DELETE RESTRICT,
 status text NOT NULL DEFAULT 'paused' CHECK(status IN ('active','paused','blocked')),
 history_mode text NOT NULL DEFAULT 'new_only' CHECK(history_mode IN ('new_only','include_existing')),
 start_after_message_id integer NOT NULL DEFAULT 0 CHECK(start_after_message_id>=0),
 last_error text,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK(source_chat_id<>destination_chat_id)
);
CREATE UNIQUE INDEX movie_monitor_route ON tgstrm.movie_monitors
 (source_chat_id,COALESCE(source_topic_id,0),destination_chat_id,COALESCE(destination_topic_id,0));

CREATE TABLE tgstrm.movie_monitor_deliveries (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 monitor_id uuid NOT NULL REFERENCES tgstrm.movie_monitors(id) ON DELETE RESTRICT,
 source_message_id bigint NOT NULL REFERENCES tgstrm.messages(id) ON DELETE RESTRICT,
 source_signature text NOT NULL,
 status text NOT NULL DEFAULT 'matching' CHECK(status IN
 ('matching','review','ready','presenting','sending','indexing','exporting','completed','failed','excluded')),
 tmdb_id bigint CHECK(tmdb_id>0),
 match_payload jsonb,
 confirmed_at timestamptz,
 presentation_random_id bigint NOT NULL UNIQUE CHECK(presentation_random_id>0),
 video_random_id bigint NOT NULL UNIQUE CHECK(video_random_id>0),
 presentation_telegram_message_id integer CHECK(presentation_telegram_message_id>0),
 destination_telegram_message_id integer CHECK(destination_telegram_message_id>0),
 destination_message_id bigint REFERENCES tgstrm.messages(id) ON DELETE SET NULL,
 lease_token uuid,
 lease_expires_at timestamptz,
 attempts integer NOT NULL DEFAULT 0,
 next_attempt_at timestamptz NOT NULL DEFAULT now(),
 last_error text,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(monitor_id,source_message_id),
 CHECK(presentation_random_id<>video_random_id),
 CHECK(status NOT IN ('ready','presenting','sending','indexing','exporting','completed')
       OR (tmdb_id IS NOT NULL AND match_payload IS NOT NULL AND confirmed_at IS NOT NULL)),
 CHECK(status NOT IN ('sending','indexing','exporting','completed')
       OR presentation_telegram_message_id IS NOT NULL),
 CHECK(status NOT IN ('indexing','exporting','completed')
       OR destination_telegram_message_id IS NOT NULL)
);
CREATE INDEX movie_monitor_deliveries_pending ON tgstrm.movie_monitor_deliveries(next_attempt_at,created_at)
 WHERE status NOT IN ('review','completed','excluded');
CREATE INDEX movie_monitor_deliveries_review ON tgstrm.movie_monitor_deliveries(monitor_id,created_at)
 WHERE status='review';
