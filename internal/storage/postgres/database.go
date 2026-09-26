package postgres

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"tgstrm/internal/config"
	"tgstrm/migrations"
)

const legacyTelegramProfileChecksum = "38e7a8b3ababeecb34bd003c8088e34d8f1b82d46aef99e5bdaaeb03771f7c15"

func Validate(c config.Database) error {
	if strings.TrimSpace(c.Host) == "" || c.Port == 0 || strings.TrimSpace(c.Name) == "" || strings.TrimSpace(c.User) == "" {
		return errors.New("Completa servidor, puerto, base de datos y usuario.")
	}
	if strings.ContainsAny(c.Host, "/\\?#@ ") {
		return errors.New("Introduce un nombre de servidor o una IP, no una URL.")
	}
	if c.SSLMode != "disable" && c.SSLMode != "require" && c.SSLMode != "verify-full" {
		return errors.New("Selecciona un modo TLS válido.")
	}
	return nil
}
func Connect(ctx context.Context, c config.Database) (*sql.DB, error) {
	if err := Validate(c); err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "postgres", Host: net.JoinHostPort(c.Host, strconv.Itoa(int(c.Port))), Path: "/" + c.Name, User: url.UserPassword(c.User, c.Password)}
	q := url.Values{"sslmode": {c.SSLMode}, "connect_timeout": {"5"}, "application_name": {"tgstrm"}}
	if c.RootCert != "" {
		q.Set("sslrootcert", c.RootCert)
	}
	u.RawQuery = q.Encode()
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// Migrate locks one transaction across checks and DDL, and refuses modified/newer schemas.
func Migrate(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(812314,1)"); err != nil {
		return err
	}
	var exists bool
	if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname='tgstrm')").Scan(&exists); err != nil {
		return err
	}
	if !exists {
		if _, err = tx.ExecContext(ctx, `CREATE SCHEMA tgstrm; CREATE TABLE tgstrm.schema_migrations(version text PRIMARY KEY, checksum text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
			return err
		}
	}
	rows, err := tx.QueryContext(ctx, "SELECT version,checksum FROM tgstrm.schema_migrations")
	if err != nil {
		return err
	}
	applied := map[string]string{}
	for rows.Next() {
		var v, c string
		if err = rows.Scan(&v, &c); err != nil {
			rows.Close()
			return err
		}
		applied[v] = c
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	files, err := migrations.Files.ReadDir(".")
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, f := range files {
		known[f.Name()] = true
	}
	for v := range applied {
		if !known[v] {
			return fmt.Errorf("database migration %s is newer than this application", v)
		}
	}
	for _, f := range files {
		body, err := migrations.Files.ReadFile(f.Name())
		if err != nil {
			return err
		}
		sum := sha256.Sum256(body)
		checksum := hex.EncodeToString(sum[:])
		if previous, ok := applied[f.Name()]; ok {
			if previous != checksum {
				reconciled, reconcileErr := reconcileLegacyMigrationChecksum(ctx, tx, f.Name(), previous, checksum)
				if reconcileErr != nil {
					return reconcileErr
				}
				if !reconciled {
					return fmt.Errorf("migration checksum mismatch: %s", f.Name())
				}
			}
			continue
		}
		if _, err = tx.ExecContext(ctx, string(body)); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO tgstrm.schema_migrations(version,checksum) VALUES($1,$2)", f.Name(), checksum); err != nil {
			return err
		}
	}
	id := make([]byte, 16)
	if _, err = rand.Read(id); err != nil {
		return err
	}
	id[6] = (id[6] & 15) | 64
	id[8] = (id[8] & 63) | 128
	if _, err = tx.ExecContext(ctx, "INSERT INTO tgstrm.installation(id,instance_id) VALUES(true,$1) ON CONFLICT(id) DO NOTHING", hex.EncodeToString(id)); err != nil {
		return err
	}
	return tx.Commit()
}

// reconcileLegacyMigrationChecksum upgrades the ledger for the one migration
// that changed during pre-release development. The exact legacy digest is
// accepted only after confirming that every column produced by the current
// migration already exists with the expected PostgreSQL type. Unknown digest
// changes continue to fail closed.
func reconcileLegacyMigrationChecksum(ctx context.Context, tx *sql.Tx, name, previous, current string) (bool, error) {
	if name != "003_telegram_profile.sql" || previous != legacyTelegramProfileChecksum {
		return false, nil
	}
	var matchingColumns int
	err := tx.QueryRowContext(ctx, `
		SELECT count(*)
		FROM (VALUES
			('profile_ciphertext', 'bytea'),
			('avatar', 'bytea'),
			('avatar_content_type', 'text'),
			('connected_at', 'timestamptz'),
			('last_seen_at', 'timestamptz')
		) AS expected(column_name, udt_name)
		JOIN information_schema.columns actual
		  ON actual.table_schema = 'tgstrm'
		 AND actual.table_name = 'telegram_accounts'
		 AND actual.column_name = expected.column_name
		 AND actual.udt_name = expected.udt_name
		 AND actual.is_nullable = 'YES'`).Scan(&matchingColumns)
	if err != nil {
		return false, fmt.Errorf("verify legacy migration %s: %w", name, err)
	}
	if matchingColumns != 5 {
		return false, nil
	}
	result, err := tx.ExecContext(ctx, `UPDATE tgstrm.schema_migrations SET checksum=$1 WHERE version=$2 AND checksum=$3`, current, name, previous)
	if err != nil {
		return false, fmt.Errorf("reconcile legacy migration %s: %w", name, err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("verify reconciled migration %s: %w", name, err)
	}
	if updated != 1 {
		return false, fmt.Errorf("reconcile legacy migration %s: expected one ledger row, updated %d", name, updated)
	}
	return true, nil
}
