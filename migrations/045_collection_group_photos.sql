-- Durable decorative work: independent of topic imports and mirror delivery.
ALTER TABLE tgstrm.collection_groups
 ADD COLUMN photo_status text NOT NULL DEFAULT 'pending' CHECK (photo_status IN ('pending','applied','attention')),
 ADD COLUMN telegram_photo_id bigint,
 ADD COLUMN photo_last_error text,
 ADD COLUMN photo_next_attempt_at timestamptz NOT NULL DEFAULT now();
CREATE INDEX collection_group_photo_pending_idx ON tgstrm.collection_groups(photo_next_attempt_at)
 WHERE photo_status<>'applied' AND state='ready';
