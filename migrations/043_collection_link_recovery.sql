-- Keep prior mirror provenance when a user explicitly reconnects an origin.
ALTER TABLE tgstrm.mirror_links ADD COLUMN retired_at timestamptz;
ALTER TABLE tgstrm.mirror_links ADD COLUMN replaces_link_id uuid REFERENCES tgstrm.mirror_links(id) ON DELETE SET NULL;
CREATE INDEX mirror_links_current_idx ON tgstrm.mirror_links(created_at DESC) WHERE retired_at IS NULL;
