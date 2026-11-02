ALTER TABLE tgstrm.movie_monitors ADD COLUMN last_scanned_at timestamptz;
ALTER TABLE tgstrm.movie_monitors ADD COLUMN last_topics_refreshed_at timestamptz;
CREATE TABLE tgstrm.movie_monitor_scopes (
 monitor_id uuid NOT NULL REFERENCES tgstrm.movie_monitors(id) ON DELETE RESTRICT,
 topic_id bigint NOT NULL REFERENCES tgstrm.topics(id) ON DELETE RESTRICT,
 initial_scan_complete boolean NOT NULL DEFAULT false,
 cursor_id integer NOT NULL DEFAULT 0,
 upper_id integer NOT NULL DEFAULT 0,
 boundary_id integer NOT NULL DEFAULT 0,
 next_scan_at timestamptz NOT NULL DEFAULT now(),
 lease_token uuid,
 lease_expires_at timestamptz,
 last_error text,
 PRIMARY KEY(monitor_id,topic_id)
);
CREATE TABLE tgstrm.movie_destination_jobs (
 collection_id uuid PRIMARY KEY REFERENCES tgstrm.library_collections(id) ON DELETE RESTRICT,
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','creating','ready','attention')),
 destination_chat_id bigint REFERENCES tgstrm.chats(id) ON DELETE RESTRICT,
 last_error text,
 updated_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE tgstrm.movie_monitor_deliveries ADD COLUMN approved_signature text;
ALTER TABLE tgstrm.movie_monitor_deliveries ADD COLUMN last_stage text NOT NULL DEFAULT 'matching';
