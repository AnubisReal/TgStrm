package httpapi

import (
	"context"
	"testing"
)

func TestEpisodeRecoveryBackoffAndVersion(t *testing.T) {
	db := correctionsAuditDB(t)
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var id string
	if err = tx.QueryRowContext(ctx, `SELECT message_id::text FROM tgstrm.stream_assets LIMIT 1`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM tgstrm.episode_detection_rechecks WHERE message_id::text=$1`, id); err != nil {
		t.Fatal(err)
	}
	due := func(version string) bool {
		var ok bool
		err := tx.QueryRowContext(ctx, `SELECT `+episodeRecheckDueSQL+` FROM tgstrm.stream_assets sa LEFT JOIN tgstrm.episode_detection_rechecks re ON re.message_id=sa.message_id WHERE sa.message_id::text=$2`, version, id).Scan(&ok)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if !due(episodeDetectorVersion) {
		t.Fatal("never examined source wasn't selected")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.episode_detection_rechecks(message_id,source_signature,detector_version,next_attempt_at) SELECT message_id,source_signature,$2,now()+interval '1 day' FROM tgstrm.stream_assets WHERE message_id::text=$1`, id, episodeDetectorVersion); err != nil {
		t.Fatal(err)
	}
	if due(episodeDetectorVersion) {
		t.Fatal("repeated same-source recheck bypassed backoff")
	}
	if !due("new-version") {
		t.Fatal("new detector did not invalidate prior result")
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tgstrm.episode_detection_rechecks SET next_attempt_at=now()-interval '1 second' WHERE message_id::text=$1`, id); err != nil {
		t.Fatal(err)
	}
	if !due(episodeDetectorVersion) {
		t.Fatal("expired backoff not selected")
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tgstrm.episode_detection_rechecks SET next_attempt_at=now()+interval '1 day',source_signature=decode('00','hex') WHERE message_id::text=$1`, id); err != nil {
		t.Fatal(err)
	}
	if !due(episodeDetectorVersion) {
		t.Fatal("changed source not selected")
	}
}

func TestManualEpisodeCaptionAssignmentSQL(t *testing.T) {
	db := correctionsAuditDB(t)
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var topic, message, caption string
	var series int64
	if err = tx.QueryRowContext(ctx, `SELECT j.topic_id::text,m.id::text,COALESCE(m.body,''),j.tmdb_id FROM tgstrm.topic_identity_jobs j JOIN tgstrm.messages m ON m.topic_id=j.topic_id AND m.is_active AND m.media_type='video' LIMIT 1`).Scan(&topic, &message, &caption, &series); err != nil {
		t.Fatal(err)
	}
	// Exercise the previously broken case: a confirmed topic without a job.
	if _, err = tx.ExecContext(ctx, `DELETE FROM tgstrm.topic_caption_corrections WHERE topic_id::text=$1`, topic); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM tgstrm.topic_identity_jobs WHERE topic_id::text=$1`, topic); err != nil {
		t.Fatal(err)
	}
	desired := "Asignación comprobada S01E01"
	if _, err = tx.ExecContext(ctx, manualEpisodeCaptionSQL, topic, message, caption, desired, series); err != nil {
		t.Fatal(err)
	}
	var actual, status string
	if err = tx.QueryRowContext(ctx, `SELECT desired_caption,status FROM tgstrm.topic_caption_corrections WHERE topic_id::text=$1 AND message_id::text=$2`, topic, message).Scan(&actual, &status); err != nil {
		t.Fatal(err)
	}
	if actual != desired || status != "pending" {
		t.Fatalf("%s %s", actual, status)
	}
}

func TestRecoveryCopiesDoNotBlockTopicCompletion(t *testing.T) {
	db := correctionsAuditDB(t)
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var topic, library, asset string
	if err = tx.QueryRowContext(ctx, `SELECT m.topic_id::text,e.library_id::text,e.asset_id::text FROM tgstrm.stream_exports e JOIN tgstrm.stream_assets sa ON sa.id=e.asset_id JOIN tgstrm.messages m ON m.id=sa.message_id WHERE m.topic_id IS NOT NULL LIMIT 1`).Scan(&topic, &library, &asset); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM tgstrm.retired_export_paths rp USING tgstrm.stream_assets sa,tgstrm.messages m WHERE rp.asset_id=sa.id AND m.id=sa.message_id AND m.topic_id::text=$1`, topic); err != nil {
		t.Fatal(err)
	}
	check := func(want bool) {
		var pending bool
		if err := tx.QueryRowContext(ctx, pendingTopicExportRetirementSQL, topic).Scan(&pending); err != nil {
			t.Fatal(err)
		}
		if pending != want {
			t.Fatalf("pending=%v want=%v", pending, want)
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.retired_export_paths(library_id,asset_id,relative_path,content_hash) VALUES($1::uuid,$2::uuid,'.tgstrm-recovery/episode-corrections/audit.strm',decode('00','hex'))`, library, asset); err != nil {
		t.Fatal(err)
	}
	check(false)
	if _, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.retired_export_paths(library_id,asset_id,relative_path,content_hash) VALUES($1::uuid,$2::uuid,'Season 01/obsolete-audit.strm',decode('00','hex'))`, library, asset); err != nil {
		t.Fatal(err)
	}
	check(true)
}

func TestUnavailableContextIsNotEpisodeEvidence(t *testing.T) {
	db := correctionsAuditDB(t)
	a := &API{db: db}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ep, handled, identified := a.detectCumulativeEpisode(ctx, 1032, "Aida 6x85 - Fuera de caja.mp4")
	if !handled || identified || ep.Source != "context_unavailable" {
		t.Fatalf("outage treated as usable evidence: %+v handled=%v identified=%v", ep, handled, identified)
	}
}

func TestManualEpisodeConflictBelongsToCurrentSeries(t *testing.T) {
	db := correctionsAuditDB(t)
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var topic, asset, other string
	var series int64
	err = tx.QueryRowContext(ctx, `SELECT m.topic_id::text,sa.id::text,other.id::text,tm.tmdb_id FROM tgstrm.messages m JOIN tgstrm.stream_assets sa ON sa.message_id=m.id JOIN tgstrm.tmdb_matches tm ON tm.topic_id=m.topic_id AND tm.message_id IS NULL AND tm.media_type='tv' JOIN LATERAL(SELECT sa2.id FROM tgstrm.messages m2 JOIN tgstrm.stream_assets sa2 ON sa2.message_id=m2.id WHERE m2.topic_id=m.topic_id AND m2.id<>m.id AND m2.is_active LIMIT 1) other ON true WHERE m.is_active LIMIT 1`).Scan(&topic, &asset, &other, &series)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.stream_episode_overrides(asset_id,series_tmdb_id,season_number,episode_number,episode_title) VALUES($1::uuid,$2,0,999,'Audit') ON CONFLICT(asset_id) DO UPDATE SET series_tmdb_id=EXCLUDED.series_tmdb_id,season_number=0,episode_number=999`, other, series); err != nil {
		t.Fatal(err)
	}
	check := func(id string, tmdb int64, want bool) {
		var conflict bool
		if err = tx.QueryRowContext(ctx, episodeAssignmentConflictSQL, topic, id, 0, 999, tmdb).Scan(&conflict); err != nil {
			t.Fatal(err)
		}
		if conflict != want {
			t.Fatalf("conflict=%v want=%v", conflict, want)
		}
	}
	check(asset, series, true)
	check(other, series, false)
	check(asset, series+1, false)
}
