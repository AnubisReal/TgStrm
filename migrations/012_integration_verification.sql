ALTER TABLE tgstrm.integration_secrets
  ADD COLUMN last_verified_at timestamptz,
  ADD COLUMN last_error text;
