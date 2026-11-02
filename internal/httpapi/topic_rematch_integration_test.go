package httpapi

import (
	"context"
	"testing"
)

func TestSeriesRematchInvalidatesPreviousEpisodeOverrides(t *testing.T) {
	db := correctionsAuditDB(t)
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var topic, asset string
	var oldID int64
	err = tx.QueryRowContext(ctx, `SELECT tm.topic_id::text,sa.id::text,tm.tmdb_id
 FROM tgstrm.tmdb_matches tm JOIN tgstrm.topic_replications tr ON tr.destination_topic_id=tm.topic_id
 JOIN tgstrm.messages m ON m.topic_id=tm.topic_id AND m.is_active
 JOIN tgstrm.stream_assets sa ON sa.message_id=m.id WHERE tm.message_id IS NULL AND tm.media_type='tv' LIMIT 1`).Scan(&topic, &asset, &oldID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.stream_episode_overrides(asset_id,series_tmdb_id,season_number,episode_number,episode_title) VALUES($1::uuid,$2,1,1,'Título anterior') ON CONFLICT(asset_id) DO UPDATE SET series_tmdb_id=EXCLUDED.series_tmdb_id,episode_title=EXCLUDED.episode_title`, asset, oldID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE tgstrm.tmdb_matches SET tmdb_id=tmdb_id+10000000 WHERE topic_id::text=$1 AND message_id IS NULL`, topic)
	if err != nil {
		t.Fatal(err)
	}
	var remaining int
	var status string
	var identity, prepared int64
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM tgstrm.stream_episode_overrides WHERE asset_id::text=$1`, asset).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatal("episode assignment from the wrong series survived")
	}
	if err = tx.QueryRowContext(ctx, `SELECT status,identity_applied_revision,captions_prepared_revision FROM tgstrm.topic_identity_jobs WHERE topic_id::text=$1`, topic).Scan(&status, &identity, &prepared); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || identity != 0 || prepared != 0 {
		t.Fatalf("incorrect initial phases: %s %d %d", status, identity, prepared)
	}
	var invalidated bool
	if err = tx.QueryRowContext(ctx, `SELECT processed_signature IS NULL FROM tgstrm.stream_assets WHERE id::text=$1`, asset).Scan(&invalidated); err != nil {
		t.Fatal(err)
	}
	if !invalidated {
		t.Fatal("STRM regeneration was not enqueued")
	}
}

func TestFirstConfirmationOfReplicatedTopicQueuesCorrection(t *testing.T) {
	db := correctionsAuditDB(t)
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var topic string
	if err = tx.QueryRowContext(ctx, `SELECT tm.topic_id::text FROM tgstrm.tmdb_matches tm JOIN tgstrm.topic_replications tr ON tr.destination_topic_id=tm.topic_id WHERE tm.message_id IS NULL AND tm.media_type='tv' LIMIT 1`).Scan(&topic); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `CREATE TEMP TABLE rematch_saved ON COMMIT DROP AS SELECT * FROM tgstrm.tmdb_matches WHERE topic_id::text=$1 AND message_id IS NULL`, topic); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM tgstrm.tmdb_matches WHERE topic_id::text=$1 AND message_id IS NULL`, topic); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.tmdb_matches SELECT * FROM rematch_saved`); err != nil {
		t.Fatal(err)
	}
	var status string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM tgstrm.topic_identity_jobs WHERE topic_id::text=$1`, topic).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "pending" {
		t.Fatalf("first confirmation: %s", status)
	}
}
