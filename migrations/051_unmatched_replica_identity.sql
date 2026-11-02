-- A replicated topic can lose its match independently of its Telegram identity.
-- Confirming a fresh match must use the same durable correction workflow.
CREATE TRIGGER tmdb_topic_identity_first_confirmation AFTER INSERT ON tgstrm.tmdb_matches
 FOR EACH ROW EXECUTE FUNCTION tgstrm.queue_topic_identity_correction();
