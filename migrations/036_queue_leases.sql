-- Durable leases allow another worker to recover queue rows after a process
-- exits unexpectedly. Tokens prevent an old worker from extending a newer claim.
ALTER TABLE tgstrm.catalog_imports
 ADD COLUMN lease_token uuid,
 ADD COLUMN lease_expires_at timestamptz;

CREATE INDEX catalog_imports_expired_leases
 ON tgstrm.catalog_imports(lease_expires_at,id)
 WHERE status='running';

ALTER TABLE tgstrm.mirror_deliveries
 ADD COLUMN delivery_lease_token uuid,
 ADD COLUMN delivery_lease_expires_at timestamptz,
 ADD COLUMN edit_lease_token uuid,
 ADD COLUMN edit_lease_expires_at timestamptz;

-- Rows left in an in-progress state by pre-lease versions are safe to reclaim:
-- delivery random_id is stable, and edits are idempotent replacements.
UPDATE tgstrm.mirror_deliveries SET delivery_lease_expires_at=now()
 WHERE status='sending' AND delivery_lease_expires_at IS NULL;
UPDATE tgstrm.mirror_deliveries SET edit_lease_expires_at=now()
 WHERE edit_status='editing' AND edit_lease_expires_at IS NULL;

CREATE INDEX mirror_deliveries_expired_delivery_leases
 ON tgstrm.mirror_deliveries(delivery_lease_expires_at,id)
 WHERE status='sending';

CREATE INDEX mirror_deliveries_expired_edit_leases
 ON tgstrm.mirror_deliveries(edit_lease_expires_at,id)
 WHERE edit_status='editing';

ALTER TABLE tgstrm.mirror_links
 ADD COLUMN history_lease_token uuid,
 ADD COLUMN history_lease_expires_at timestamptz;

UPDATE tgstrm.mirror_links SET history_lease_expires_at=now()
 WHERE history_status='indexing' AND history_lease_expires_at IS NULL;

CREATE INDEX mirror_links_expired_history_leases
 ON tgstrm.mirror_links(history_lease_expires_at,id)
 WHERE history_status='indexing';
