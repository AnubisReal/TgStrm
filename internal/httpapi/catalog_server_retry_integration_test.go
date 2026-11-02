package httpapi

import (
	"context"
	"testing"
)

func TestCatalogServerRetryPreservesDelivery(t *testing.T) {
	db := correctionsAuditDB(t)
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var id, token string
	if err = tx.QueryRowContext(ctx, `UPDATE tgstrm.catalog_imports SET status='running',lease_token=gen_random_uuid(),attempt_count=100 WHERE id=(SELECT id FROM tgstrm.catalog_imports LIMIT 1) RETURNING id::text,lease_token::text`).Scan(&id, &token); err != nil {
		t.Fatal(err)
	}
	var before int
	if err = tx.QueryRowContext(ctx, `SELECT completed_count FROM tgstrm.catalog_imports WHERE id::text=$1`, id).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, catalogServerRetrySQL, id, token, "rpc error code 500: POSTPONED_TIMEOUT"); err != nil {
		t.Fatal(err)
	}
	var state string
	var count int
	var delay float64
	var released bool
	if err = tx.QueryRowContext(ctx, `SELECT status,completed_count,extract(epoch from next_attempt_at-now()),lease_token IS NULL AND lease_expires_at IS NULL FROM tgstrm.catalog_imports WHERE id::text=$1`, id).Scan(&state, &count, &delay, &released); err != nil {
		t.Fatal(err)
	}
	if state != "pending" || count != before || delay != 300 || !released {
		t.Fatalf("%s %d %f %v", state, count, delay, released)
	}
	result, err := tx.ExecContext(ctx, catalogServerRetrySQL, id, token, "stale worker")
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := result.RowsAffected(); n != 0 {
		t.Fatal("stale lease changed retry")
	}
}
