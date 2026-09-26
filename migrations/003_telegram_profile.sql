ALTER TABLE tgstrm.telegram_accounts
 ADD COLUMN profile_ciphertext bytea,
 ADD COLUMN avatar bytea,
 ADD COLUMN avatar_content_type text,
 ADD COLUMN connected_at timestamptz,
 ADD COLUMN last_seen_at timestamptz;
