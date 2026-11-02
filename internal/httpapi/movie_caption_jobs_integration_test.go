package httpapi

import (
	"context"
	"strings"
	"testing"
	"tgstrm/migrations"
)

// Transaction-only fixtures; no Telegram calls, filesystem exports or lasting edits.
func TestDirectMovieCaptionIdentityTransition(t *testing.T) {
	db := correctionsAuditDB(t)
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var installed bool
	if err = tx.QueryRowContext(ctx, `SELECT to_regclass('tgstrm.movie_caption_jobs') IS NOT NULL`).Scan(&installed); err != nil {
		t.Fatal(err)
	}
	if !installed {
		body, e := migrations.Files.ReadFile("056_direct_movie_captions.sql")
		if e != nil {
			t.Fatal(e)
		}
		if _, err = tx.ExecContext(ctx, string(body)); err != nil {
			t.Fatal(err)
		}
	}
	var id string
	err = tx.QueryRowContext(ctx, `INSERT INTO tgstrm.messages(chat_id,topic_id,telegram_message_id,body,media_type,sent_at,is_outgoing)
 SELECT t.chat_id,t.id,2147483000,'Willow.1988.1080p','video',now(),true FROM tgstrm.topics t JOIN tgstrm.managed_catalog_chats c ON c.id=t.chat_id
 WHERE t.is_active AND c.is_active AND t.library_kind='movie_collection' AND c.telegram_peer_type='channel'
 AND NOT EXISTS(SELECT 1 FROM tgstrm.messages m WHERE m.chat_id=t.chat_id AND m.telegram_message_id=2147483000)
 LIMIT 1 RETURNING id::text`).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := tx.ExecContext(ctx, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`INSERT INTO tgstrm.message_media(message_id,media_type,file_name) VALUES($1::bigint,'video','Willow.mkv')`, id)
	exec(`INSERT INTO tgstrm.stream_assets(message_id,source_signature) VALUES($1::bigint,sha256(convert_to('Willow.1988.1080p','UTF8')||decode('00','hex')||convert_to('Willow.mkv','UTF8')))`, id)
	exec(`INSERT INTO tgstrm.tmdb_matches(topic_id,message_id,media_type,tmdb_id,title,original_title,release_date) SELECT topic_id,id,'movie',847,'Willow','Willow','1988-05-20'::date FROM tgstrm.messages WHERE id::text=$1`, id)
	exec(`INSERT INTO tgstrm.tmdb_scan_states(message_id,source_text,status) VALUES($1::bigint,'Willow.1988.1080p','matched')`, id)
	var rowsCount int
	rows, e := tx.QueryContext(ctx, strings.Replace(directMovieCaptionsSQL, "ORDER BY m.id LIMIT 50", "AND m.id::text=$1 ORDER BY m.id LIMIT 50", 1), id)
	if e != nil {
		t.Fatal(e)
	}
	for rows.Next() {
		var mid, prev, title, year string
		var tmdb int64
		if e = rows.Scan(&mid, &tmdb, &prev, &title, &year); e != nil {
			t.Fatal(e)
		}
		if mid == id {
			rowsCount++
		}
	}
	rows.Close()
	if rowsCount != 1 {
		t.Fatal("direct upload missing from candidates")
	}
	// Incoming files and unmatched uploads must not be normalized.
	exec(`UPDATE tgstrm.messages SET is_outgoing=false WHERE id::text=$1`, id)
	var eligible bool
	filtered := strings.Replace(directMovieCaptionsSQL, "ORDER BY m.id LIMIT 50", "AND m.id::text=$1 ORDER BY m.id LIMIT 50", 1)
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS (`+filtered+`)`, id).Scan(&eligible); err != nil || eligible {
		t.Fatalf("incoming video eligible: %v %v", eligible, err)
	}
	exec(`UPDATE tgstrm.messages SET is_outgoing=true WHERE id::text=$1`, id)
	exec(`INSERT INTO tgstrm.movie_caption_jobs(message_id,tmdb_id,previous_caption,desired_caption) VALUES($1::bigint,847,'Willow.1988.1080p','Willow (1988)')`, id)
	exec(`UPDATE tgstrm.messages SET body='Willow (1988)' WHERE id::text=$1`, id)
	exec(`UPDATE tgstrm.stream_assets SET source_signature=sha256(convert_to('Willow (1988)','UTF8')||decode('00','hex')||convert_to('Willow.mkv','UTF8')) WHERE message_id::text=$1`, id)
	var matches int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM tgstrm.tmdb_matches WHERE message_id::text=$1`, id).Scan(&matches); err != nil || matches != 1 {
		t.Fatalf("own normalization lost identity: %d %v", matches, err)
	}
	exec(`UPDATE tgstrm.movie_caption_jobs SET status='completed' WHERE message_id::text=$1`, id)
	// A genuine filename edit must still discard an inferred identity.
	exec(`UPDATE tgstrm.message_media SET file_name='Another-film.mkv' WHERE message_id::text=$1`, id)
	exec(`UPDATE tgstrm.stream_assets SET source_signature=sha256(convert_to('Willow (1988)','UTF8')||decode('00','hex')||convert_to('Another-film.mkv','UTF8')) WHERE message_id::text=$1`, id)
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM tgstrm.tmdb_matches WHERE message_id::text=$1`, id).Scan(&matches); err != nil || matches != 0 {
		t.Fatalf("external filename edit retained stale identity: %d %v", matches, err)
	}
}
