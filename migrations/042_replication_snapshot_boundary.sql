ALTER TABLE tgstrm.topic_replications ADD COLUMN initial_upper_message_id bigint CHECK(initial_upper_message_id>=0);
ALTER TABLE tgstrm.collection_rename_jobs ADD COLUMN request_key text;
ALTER TABLE tgstrm.collection_rename_jobs ADD COLUMN lease_token uuid;
ALTER TABLE tgstrm.collection_rename_jobs ADD COLUMN lease_expires_at timestamptz;
ALTER TABLE tgstrm.collection_rename_jobs ADD CONSTRAINT collection_rename_request_unique UNIQUE(collection_id,request_key);
