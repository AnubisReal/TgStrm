CREATE TABLE tgstrm.mirror_links (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 source_topic_id bigint NOT NULL REFERENCES tgstrm.topics(id) ON DELETE CASCADE,
 destination_topic_id bigint NOT NULL REFERENCES tgstrm.topics(id) ON DELETE CASCADE,
 status text NOT NULL DEFAULT 'active' CHECK(status IN ('active','paused','blocked')),
 start_after_message_id integer NOT NULL DEFAULT 0,
 last_error text,
 last_mirrored_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(source_topic_id,destination_topic_id),
 CHECK(source_topic_id<>destination_topic_id)
);

CREATE UNIQUE INDEX mirror_links_one_active_destination
 ON tgstrm.mirror_links(destination_topic_id) WHERE status='active';

CREATE TABLE tgstrm.mirror_deliveries (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 link_id uuid NOT NULL REFERENCES tgstrm.mirror_links(id) ON DELETE CASCADE,
 source_message_id bigint NOT NULL REFERENCES tgstrm.messages(id) ON DELETE CASCADE,
 destination_message_id bigint REFERENCES tgstrm.messages(id) ON DELETE SET NULL,
 random_id bigint NOT NULL,
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','sending','completed','failed')),
 attempts integer NOT NULL DEFAULT 0,
 next_attempt_at timestamptz NOT NULL DEFAULT now(),
 last_error text,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 completed_at timestamptz,
 UNIQUE(link_id,source_message_id),
 UNIQUE(random_id)
);

CREATE INDEX mirror_deliveries_pending
 ON tgstrm.mirror_deliveries(next_attempt_at,created_at)
 WHERE status IN ('pending','failed');
