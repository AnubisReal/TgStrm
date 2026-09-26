ALTER TABLE tgstrm.mirror_deliveries
 ADD COLUMN destination_telegram_message_id integer,
 ADD COLUMN desired_caption text,
 ADD COLUMN edit_status text NOT NULL DEFAULT 'idle' CHECK(edit_status IN ('idle','pending','editing','failed')),
 ADD COLUMN source_edited_at timestamptz,
 ADD COLUMN edit_attempts integer NOT NULL DEFAULT 0,
 ADD COLUMN edit_next_attempt_at timestamptz NOT NULL DEFAULT now(),
 ADD COLUMN edit_last_error text;

CREATE INDEX mirror_deliveries_edit_pending
 ON tgstrm.mirror_deliveries(edit_next_attempt_at,updated_at)
 WHERE edit_status IN ('pending','editing','failed');
