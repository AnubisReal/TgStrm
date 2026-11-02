package httpapi

import (
	"context"
	"testing"
	"tgstrm/migrations"
)

// Fixtures are rolled back. No Telegram RPC, real deletion, or STRM write.
func TestMovieDeliveryFileGuard(t *testing.T) {
	db := correctionsAuditDB(t)
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var installed bool
	if err = tx.QueryRowContext(ctx, `SELECT to_regclass('tgstrm.movie_delivery_files') IS NOT NULL`).Scan(&installed); err != nil {
		t.Fatal(err)
	}
	if !installed {
		b, e := migrations.Files.ReadFile("057_movie_delivery_file_guard.sql")
		if e != nil {
			t.Fatal(e)
		}
		if _, err = tx.ExecContext(ctx, string(b)); err != nil {
			t.Fatal(err)
		}
	}
	var monitor, sourceChat, sourceTopic, group, topic string
	err = tx.QueryRowContext(ctx, `SELECT mm.id::text,mm.source_chat_id::text,t.id::text,mm.destination_chat_id::text,mm.destination_topic_id::text
 FROM tgstrm.movie_monitors mm JOIN tgstrm.topics t ON t.chat_id=mm.source_chat_id
 WHERE mm.destination_topic_id IS NOT NULL LIMIT 1`).Scan(&monitor, &sourceChat, &sourceTopic, &group, &topic)
	if err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := tx.ExecContext(ctx, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	source := func(remote int, file int64) string {
		t.Helper()
		var id string
		e := tx.QueryRowContext(ctx, `INSERT INTO tgstrm.messages(chat_id,topic_id,telegram_message_id,body,media_type,sent_at)
 VALUES($1::bigint,$2::bigint,$3,'Guard audit','video',now()) RETURNING id::text`, sourceChat, sourceTopic, remote).Scan(&id)
		if e != nil {
			t.Fatal(e)
		}
		exec(`INSERT INTO tgstrm.message_media(message_id,media_type,telegram_file_id,file_name) VALUES($1::bigint,'video',$2,'Same title (2020).mkv')`, id, file)
		return id
	}
	delivery := func(src string, random int64) movieDelivery {
		t.Helper()
		var id string
		e := tx.QueryRowContext(ctx, `INSERT INTO tgstrm.movie_monitor_deliveries(monitor_id,source_message_id,source_signature,presentation_random_id,video_random_id)
 VALUES($1::uuid,$2::bigint,'audit',$3,$4) RETURNING id::text`, monitor, src, random, random+1).Scan(&id)
		if e != nil {
			t.Fatal(e)
		}
		return movieDelivery{id: id, source: src, group: group, topic: topic, stage: "ready"}
	}
	first := delivery(source(2147482800, 9223372000000100), 9223372000000200)
	second := delivery(source(2147482801, 9223372000000100), 9223372000000202)
	version := delivery(source(2147482802, 9223372000000101), 9223372000000204)
	check := func(d movieDelivery, want bool) {
		t.Helper()
		got, e := reserveMovieDeliveryFile(ctx, tx, d)
		if e != nil || got != want {
			t.Fatalf("stage=%s want %v got %v: %v", d.stage, want, got, e)
		}
	}
	check(first, true)
	if _, err = reserveMovieDeliveryFile(ctx, tx, second); err != errMovieFileReserved {
		t.Fatalf("pending owner must defer its duplicate, not lose it: %v", err)
	}
	check(first, true)   // same owner survives restart
	check(version, true) // same title, different document is a valid version
	first.stage = "sending"
	check(first, true) // uncertain acceptance retains the owner
	// Changing the source document after reservation must not send different media.
	exec(`UPDATE tgstrm.message_media SET telegram_file_id=9223372000000102 WHERE message_id::text=$1`, first.source)
	if _, err = reserveMovieDeliveryFile(ctx, tx, first); err != errMovieFileIdentityChanged {
		t.Fatalf("source mutation accepted: %v", err)
	}
	exec(`UPDATE tgstrm.message_media SET telegram_file_id=9223372000000100 WHERE message_id::text=$1`, first.source)
	var dest string
	err = tx.QueryRowContext(ctx, `INSERT INTO tgstrm.messages(chat_id,topic_id,telegram_message_id,body,media_type,sent_at,is_outgoing)
 VALUES($1::bigint,$2::bigint,2147482803,'Same title (2020)','video',now(),true) RETURNING id::text`, group, topic).Scan(&dest)
	if err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO tgstrm.message_media(message_id,media_type,telegram_file_id) VALUES($1::bigint,'video',9223372000000100)`, dest)
	first.stage = "ready"
	check(first, false) // even an owner cannot resend a ready file already present
	first.stage = "sending"
	check(first, true) // its in-flight acceptance remains resumable
	// Unreserved file already uploaded directly to the target is also blocked.
	exec(`UPDATE tgstrm.message_media SET telegram_file_id=9223372000000103 WHERE message_id::text=$1`, dest)
	direct := delivery(source(2147482804, 9223372000000103), 9223372000000206)
	check(direct, false)
	// A known completed copy that was later deleted can acquire a new owner.
	exec(`UPDATE tgstrm.message_media SET telegram_file_id=9223372000000100 WHERE message_id::text=$1`, dest)
	exec(`UPDATE tgstrm.messages SET is_active=false WHERE id::text=$1`, dest)
	exec(`UPDATE tgstrm.movie_monitor_deliveries SET status='completed',last_stage='completed',tmdb_id=123,match_payload='{}',confirmed_at=now(),presentation_telegram_message_id=2147482805,destination_telegram_message_id=2147482803,destination_message_id=$2::bigint WHERE id::text=$1`, first.id, dest)
	check(second, true)
	// Same media in another topic is an independent destination scope.
	var other string
	err = tx.QueryRowContext(ctx, `SELECT id::text FROM tgstrm.topics WHERE chat_id::text=$1 AND id::text<>$2 LIMIT 1`, group, topic).Scan(&other)
	if err == nil {
		version.topic = other
		if _, err = reserveMovieDeliveryFile(ctx, tx, version); err != errMovieFileIdentityChanged {
			t.Fatal("a reserved delivery changed destination")
		}
	}
	// Confirm the migration seeded one owner for every existing active media key.
	var missing int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM tgstrm.movie_monitor_deliveries d JOIN tgstrm.movie_monitors mm ON mm.id=d.monitor_id
 JOIN tgstrm.messages m ON m.id=d.destination_message_id AND m.is_active
 JOIN tgstrm.message_media media ON media.message_id=m.id
 WHERE mm.destination_topic_id IS NOT NULL AND media.telegram_file_id>0
 AND NOT EXISTS(SELECT 1 FROM tgstrm.movie_delivery_files f WHERE f.destination_chat_id=mm.destination_chat_id AND f.destination_topic_id=mm.destination_topic_id AND f.telegram_file_id=media.telegram_file_id)`).Scan(&missing); err != nil || missing != 0 {
		t.Fatalf("missing seeded owners: %d %v", missing, err)
	}
}
