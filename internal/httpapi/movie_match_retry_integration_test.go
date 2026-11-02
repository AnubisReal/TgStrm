package httpapi

import (
	"context"
	"testing"
	"tgstrm/migrations"
)

func TestMovieMatchRetryPreservesCompletedAndConfirmed(t *testing.T) {
	db := correctionsAuditDB(t)
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var before, after int
	const query = `SELECT count(*) FROM tgstrm.movie_monitor_deliveries WHERE status IN ('completed','excluded') OR confirmed_at IS NOT NULL OR presentation_telegram_message_id IS NOT NULL OR destination_telegram_message_id IS NOT NULL`
	if err = tx.QueryRow(query).Scan(&before); err != nil {
		t.Fatal(err)
	}
	body, err := migrations.Files.ReadFile("059_movie_match_formats_retry.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(string(body)); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(query).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("protected deliveries changed: %d -> %d", before, after)
	}
	var remaining int
	if err = tx.QueryRow(`SELECT count(*) FROM tgstrm.movie_monitor_deliveries d JOIN tgstrm.messages m ON m.id=d.source_message_id JOIN tgstrm.movie_monitors mon ON mon.id=d.monitor_id WHERE d.status='review' AND d.tmdb_id IS NULL AND d.confirmed_at IS NULL AND d.presentation_telegram_message_id IS NULL AND d.destination_telegram_message_id IS NULL AND d.last_error='No hay una coincidencia única de título y año.' AND m.is_active AND mon.status='active' AND (d.lease_expires_at IS NULL OR d.lease_expires_at<now())`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("eligible leftovers: %d", remaining)
	}
}
