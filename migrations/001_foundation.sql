CREATE TABLE tgstrm.installation (
 id boolean PRIMARY KEY DEFAULT true CHECK (id),
 instance_id uuid NOT NULL UNIQUE,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE tgstrm.settings (
 key text PRIMARY KEY,
 value jsonb NOT NULL,
 revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE tgstrm.telegram_accounts (
 id uuid PRIMARY KEY,
 telegram_user_id bigint UNIQUE,
 status text NOT NULL DEFAULT 'disconnected' CHECK (status IN ('disconnected','connecting','authorized','revoked')),
 credentials_ciphertext bytea,
 session_ciphertext bytea,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE tgstrm.web_sessions (
 token_hash bytea PRIMARY KEY,
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL,
 revoked_at timestamptz,
 CHECK (expires_at > created_at)
);
CREATE INDEX web_sessions_expiry ON tgstrm.web_sessions(expires_at);
