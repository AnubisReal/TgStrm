CREATE TABLE tgstrm.tmdb_scan_states (
 message_id bigint PRIMARY KEY REFERENCES tgstrm.messages(id) ON DELETE CASCADE,
 source_text text NOT NULL,
 status text NOT NULL CHECK (status IN ('matched','ambiguous','not_found','failed')),
 checked_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX tmdb_scan_states_status ON tgstrm.tmdb_scan_states(status,checked_at);
