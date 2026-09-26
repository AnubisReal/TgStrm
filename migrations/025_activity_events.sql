CREATE TABLE tgstrm.activity_events (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 kind text NOT NULL,
 status text NOT NULL CHECK (status IN ('pending','completed','failed','error')),
 title text NOT NULL,
 detail text NOT NULL,
 group_id bigint REFERENCES tgstrm.chats(id) ON DELETE SET NULL,
 topic_id bigint REFERENCES tgstrm.topics(id) ON DELETE SET NULL,
 item_count integer NOT NULL DEFAULT 0 CHECK (item_count >= 0),
 occurred_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX activity_events_occurred ON tgstrm.activity_events(occurred_at DESC);
