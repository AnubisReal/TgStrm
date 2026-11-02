-- Durable maintenance retries; transient Telegram failures must not require a
-- new browser visit, nor trigger one remote scan every worker tick.
ALTER TABLE tgstrm.topic_replication_manifests
 ADD COLUMN next_attempt_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE tgstrm.group_replications
 ADD COLUMN next_discovery_at timestamptz NOT NULL DEFAULT now();
CREATE INDEX topic_manifest_retry_idx ON tgstrm.topic_replication_manifests(next_attempt_at)
 WHERE invalidated_at IS NOT NULL OR status IN ('pending','scan_failed');
