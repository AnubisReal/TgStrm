CREATE TABLE tgstrm.admin_users (
 id uuid PRIMARY KEY,
 username text NOT NULL,
 password_hash text NOT NULL,
 must_change_password boolean NOT NULL DEFAULT true,
 failed_attempts integer NOT NULL DEFAULT 0 CHECK (failed_attempts >= 0),
 locked_until timestamptz,
 last_login_at timestamptz,
 password_changed_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 CONSTRAINT admin_username_format CHECK (username ~ '^[A-Za-z0-9._-]{3,32}$')
);
CREATE UNIQUE INDEX admin_users_username_ci ON tgstrm.admin_users(lower(username));
ALTER TABLE tgstrm.web_sessions
 ADD COLUMN id uuid,
 ADD COLUMN admin_user_id uuid REFERENCES tgstrm.admin_users(id) ON DELETE CASCADE,
 ADD COLUMN scope text NOT NULL DEFAULT 'full' CHECK (scope IN ('full','password_change')),
 ADD COLUMN last_seen_at timestamptz NOT NULL DEFAULT now(),
 ADD COLUMN user_agent_hash bytea;
CREATE UNIQUE INDEX web_sessions_id ON tgstrm.web_sessions(id);
CREATE INDEX web_sessions_admin_active ON tgstrm.web_sessions(admin_user_id, expires_at) WHERE revoked_at IS NULL;
CREATE TABLE tgstrm.admin_recovery_challenges (
 id uuid PRIMARY KEY,
 admin_user_id uuid NOT NULL REFERENCES tgstrm.admin_users(id) ON DELETE CASCADE,
 code_hash bytea NOT NULL,
 expires_at timestamptz NOT NULL,
 attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
 consumed_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 CHECK (expires_at > created_at)
);
CREATE INDEX admin_recovery_active ON tgstrm.admin_recovery_challenges(admin_user_id, expires_at) WHERE consumed_at IS NULL;
