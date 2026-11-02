-- Separate filtered cursors avoid skipping videos when one media category has
-- more messages than the other. Existing history cursors remain a safe boundary.
ALTER TABLE tgstrm.movie_monitor_scopes ADD COLUMN scan_pass smallint NOT NULL DEFAULT 0 CHECK(scan_pass IN (0,1));
