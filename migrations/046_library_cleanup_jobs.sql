-- A confirmed deletion is durable and resumable after restart. No catalog,
-- Telegram identity, TMDB match or stream asset is removed.
CREATE TABLE tgstrm.library_cleanup_jobs (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 account_id text NOT NULL,
 scope jsonb NOT NULL,
 plan jsonb NOT NULL,
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','running','completed','attention')),
 completed_items integer NOT NULL DEFAULT 0,
 total_items integer NOT NULL,
 last_error text,
 lease_until timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX library_cleanup_one_active ON tgstrm.library_cleanup_jobs(account_id)
 WHERE status IN ('pending','running');
