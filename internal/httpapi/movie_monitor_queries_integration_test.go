package httpapi

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"tgstrm/migrations"
)

// Parse/plan every new query against PostgreSQL without executing a delivery,
// creating a Telegram group, or committing any fixture/migration.
func TestMovieMonitorQueriesPrepare(t *testing.T) {
	db := correctionsAuditDB(t)
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, entry := range []struct{ table, file string }{{"movie_monitors", "053_movie_monitors.sql"}, {"movie_monitor_scopes", "054_movie_monitor_runtime.sql"}} {
		var installed bool
		if err = tx.QueryRowContext(ctx, `SELECT to_regclass('tgstrm.'||$1) IS NOT NULL`, entry.table).Scan(&installed); err != nil {
			t.Fatal(err)
		}
		if !installed {
			body, readErr := migrations.Files.ReadFile(entry.file)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if _, err = tx.ExecContext(ctx, string(body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	var filtered bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='tgstrm' AND table_name='movie_monitor_scopes' AND column_name='scan_pass')`).Scan(&filtered); err != nil {
		t.Fatal(err)
	}
	if !filtered {
		body, _ := migrations.Files.ReadFile("055_movie_monitor_filtered_scan.sql")
		if _, err = tx.ExecContext(ctx, string(body)); err != nil {
			t.Fatal(err)
		}
	}
	literals := regexp.MustCompile("(?s)`([^`]+)`")
	count := 0
	for _, file := range []string{"movie_monitors.go", "movie_monitor_worker.go", "../telegram/movie_monitors.go", "jobs.go", "activity.go"} {
		data, readErr := os.ReadFile(file)
		if readErr != nil {
			t.Fatal(readErr)
		}
		for _, match := range literals.FindAllSubmatch(data, -1) {
			query := strings.TrimSpace(string(match[1]))
			upper := strings.ToUpper(query)
			if !(strings.HasPrefix(upper, "SELECT ") || strings.HasPrefix(upper, "WITH ") || strings.HasPrefix(upper, "INSERT ") || strings.HasPrefix(upper, "UPDATE ")) {
				continue
			}
			name := fmt.Sprintf("movie_query_audit_%d", count)
			if _, err = tx.ExecContext(ctx, "PREPARE "+name+" AS "+query); err != nil {
				t.Fatalf("%s query %d: %v", file, count, err)
			}
			if _, err = tx.ExecContext(ctx, "DEALLOCATE "+name); err != nil {
				t.Fatal(err)
			}
			count++
		}
	}
	if count < 40 {
		t.Fatalf("audit coverage unexpectedly small: %d", count)
	}
}
