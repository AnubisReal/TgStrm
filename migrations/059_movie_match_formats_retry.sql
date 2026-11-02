-- Revisit only unassociated, unsent automatic-review items with the expanded
-- parser. Confirmed identities, sent videos, captions and library files stay put.
UPDATE tgstrm.movie_monitor_deliveries d
 SET status='matching',last_stage='matching',last_error=NULL,next_attempt_at=now(),
 lease_token=NULL,lease_expires_at=NULL,updated_at=now()
 FROM tgstrm.messages m,tgstrm.movie_monitors mon
 WHERE d.source_message_id=m.id AND d.monitor_id=mon.id AND m.is_active
 AND mon.status='active' AND d.status='review' AND d.tmdb_id IS NULL
 AND d.confirmed_at IS NULL AND d.presentation_telegram_message_id IS NULL
 AND d.destination_telegram_message_id IS NULL
 AND d.last_error='No hay una coincidencia única de título y año.'
 AND (d.lease_expires_at IS NULL OR d.lease_expires_at<now());
