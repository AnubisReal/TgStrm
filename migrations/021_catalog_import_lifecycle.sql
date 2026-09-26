DROP INDEX IF EXISTS tgstrm.catalog_imports_source_active;

-- Completed and failed attempts are immutable history. Only an import that is
-- currently being prepared or sent blocks a second concurrent import for the
-- same source topic.
CREATE UNIQUE INDEX catalog_imports_source_active
ON tgstrm.catalog_imports(source_topic_id)
WHERE status IN ('pending','review','running');
