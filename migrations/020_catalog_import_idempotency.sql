CREATE UNIQUE INDEX catalog_imports_source_active
ON tgstrm.catalog_imports(source_topic_id)
WHERE status IN ('pending','review','running','completed');
