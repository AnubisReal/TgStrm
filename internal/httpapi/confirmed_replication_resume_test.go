package httpapi

import (
	"context"
	"testing"
)

func TestConfirmedReplicationResumeDatabase(t *testing.T) {
	db := correctionsAuditDB(t)
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var plan, topic string
	if err = tx.QueryRowContext(ctx, `SELECT gr.plan_id::text,tr.id::text FROM tgstrm.topic_replications tr JOIN tgstrm.group_replications gr ON gr.id=tr.group_replication_id LIMIT 1`).Scan(&plan, &topic); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := tx.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	reset := func() {
		exec(`UPDATE tgstrm.collection_replication_plans SET status='preparing',confirmed_at=now() WHERE id::text=$1`, plan)
		exec(`UPDATE tgstrm.group_replications SET status='review' WHERE plan_id::text=$1`, plan)
		exec(`UPDATE tgstrm.topic_replications tr SET status='eligible',excluded=false FROM tgstrm.group_replications gr WHERE gr.id=tr.group_replication_id AND gr.plan_id::text=$1`, plan)
		exec(`UPDATE tgstrm.topic_replications SET import_id=NULL WHERE id::text=$1`, topic)
	}
	for _, tc := range []struct {
		name, q string
		ready   bool
	}{
		{"confirmed", "", true},
		{"unconfirmed", `UPDATE tgstrm.collection_replication_plans SET confirmed_at=NULL WHERE id::text=$1`, false},
		{"undiscovered", `UPDATE tgstrm.group_replications SET status='pending' WHERE plan_id::text=$1`, false},
		{"needs_review", `UPDATE tgstrm.topic_replications SET status='review' WHERE id::text=$1`, false},
		{"excluded_review", `UPDATE tgstrm.topic_replications SET status='review',excluded=true WHERE id::text=$1`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reset()
			if tc.q != "" {
				arg := plan
				if tc.name == "needs_review" || tc.name == "excluded_review" {
					arg = topic
				}
				exec(tc.q, arg)
			}
			result, err := tx.ExecContext(ctx, resumeConfirmedReplicationSQL, plan)
			if err != nil {
				t.Fatal(err)
			}
			n, _ := result.RowsAffected()
			if (n == 1) != tc.ready {
				t.Fatalf("unexpected transition %d", n)
			}
		})
	}
}
