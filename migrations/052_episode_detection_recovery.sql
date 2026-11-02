-- Independent of processed_at: Telegram reconciliation also updates that field.
CREATE TABLE tgstrm.episode_detection_rechecks (
 message_id bigint PRIMARY KEY REFERENCES tgstrm.messages(id) ON DELETE CASCADE,
 source_signature bytea,
 detector_version text NOT NULL,
 next_attempt_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL DEFAULT now()
);
