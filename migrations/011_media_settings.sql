CREATE TABLE tgstrm.integration_secrets (
 key text PRIMARY KEY,
 ciphertext bytea NOT NULL,
 updated_at timestamptz NOT NULL DEFAULT now()
);
