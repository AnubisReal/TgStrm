package httpapi

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const backupFormatVersion = 1

var backupNamePattern = regexp.MustCompile(`^tgstrm-[0-9]{8}-[0-9]{6}\.tgz$`)

type backupManifest struct {
	FormatVersion int               `json:"format_version"`
	CreatedAt     time.Time         `json:"created_at"`
	InstanceID    string            `json:"instance_id"`
	Database      string            `json:"database"`
	PostgreSQL    string            `json:"postgresql_version"`
	Files         map[string]string `json:"sha256"`
}

type backupItem struct {
	Name       string    `json:"name"`
	CreatedAt  time.Time `json:"created_at"`
	Size       int64     `json:"size"`
	Version    int       `json:"version"`
	Valid      bool      `json:"valid"`
	Restorable bool      `json:"restorable"`
	instanceID string
}

type backupPolicy struct {
	Enabled       bool   `json:"enabled"`
	Frequency     string `json:"frequency"`
	Hour          string `json:"hour"`
	Weekday       int    `json:"weekday"`
	IntervalHours int    `json:"interval_hours"`
	Retention     int    `json:"retention"`
}

type backupState struct {
	LastRunAt  *time.Time `json:"last_run_at,omitempty"`
	LastStatus string     `json:"last_status,omitempty"`
	LastError  string     `json:"last_error,omitempty"`
	NextRunAt  *time.Time `json:"next_run_at,omitempty"`
}

type backupSettingsPayload struct {
	backupPolicy
	backupState
}

func defaultBackupPolicy() backupPolicy {
	return backupPolicy{Frequency: "daily", Hour: "03:00", Weekday: 1, IntervalHours: 24, Retention: 7}
}

func (a *API) backups(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		fail(w, http.StatusServiceUnavailable, "database_unavailable", "La base de datos no está disponible.")
		return
	}
	if r.Method == http.MethodGet {
		items, err := listBackups(filepath.Join(a.store.Dir(), "backups"))
		if err != nil {
			fail(w, 500, "backup_list_failed", "No se pudieron leer las copias de seguridad.")
			return
		}
		var instanceID string
		_ = a.db.QueryRowContext(r.Context(), `SELECT instance_id::text FROM tgstrm.installation WHERE id`).Scan(&instanceID)
		for index := range items {
			items[index].Restorable = items[index].Valid && items[index].instanceID != "" && items[index].instanceID == instanceID
		}
		writeJSON(w, 200, map[string]any{"backups": items, "directory": filepath.Join(a.store.Dir(), "backups")})
		return
	}
	item, err := a.createBackup(r.Context())
	if err != nil {
		fail(w, 500, "backup_failed", fmt.Sprintf("No se pudo crear la copia: %.300s", err.Error()))
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (a *API) createBackup(ctx context.Context) (backupItem, error) {
	a.backupMu.Lock()
	defer a.backupMu.Unlock()
	backupDir := filepath.Join(a.store.Dir(), "backups")
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		return backupItem{}, err
	}
	cfg, err := a.store.LoadDatabase()
	if err != nil || cfg == nil {
		return backupItem{}, errors.New("no se pudo leer la conexión cifrada de PostgreSQL")
	}
	var pgVersion string
	if err = a.db.QueryRowContext(ctx, `SHOW server_version`).Scan(&pgVersion); err != nil {
		return backupItem{}, fmt.Errorf("no se pudo consultar la versión de PostgreSQL: %w", err)
	}
	serverMajor, err := parsePostgresMajor(pgVersion)
	if err != nil {
		return backupItem{}, err
	}
	pgDumpPath, err := postgresToolPath("pg_dump", serverMajor)
	if err != nil {
		return backupItem{}, err
	}
	pgRestorePath, err := postgresToolPath("pg_restore", serverMajor)
	if err != nil {
		return backupItem{}, err
	}
	stamp := time.Now()
	name := "tgstrm-" + stamp.Format("20060102-150405") + ".tgz"
	dump, err := os.CreateTemp(backupDir, ".database-*.dump")
	if err != nil {
		return backupItem{}, err
	}
	dumpPath := dump.Name()
	dump.Close()
	defer os.Remove(dumpPath)

	backupCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	args := []string{"--format=custom", "--no-owner", "--no-acl", "--schema=tgstrm", "--file", dumpPath, "--host", cfg.Host, "--port", strconv.Itoa(int(cfg.Port)), "--username", cfg.User, "--dbname", cfg.Name}
	cmd := exec.CommandContext(backupCtx, pgDumpPath, args...)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+cfg.Password, "PGSSLMODE="+cfg.SSLMode)
	if cfg.RootCert != "" {
		cmd.Env = append(cmd.Env, "PGSSLROOTCERT="+cfg.RootCert)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err = cmd.Run(); err != nil {
		return backupItem{}, fmt.Errorf("pg_dump: %s", strings.TrimSpace(stderr.String()))
	}
	// Validate the custom-format archive before advertising it as restorable.
	// Restore performs this check again after extraction; doing it here catches
	// a truncated or otherwise unreadable dump at backup creation time.
	listCmd := exec.CommandContext(backupCtx, pgRestorePath, "--list", dumpPath)
	if output, listErr := listCmd.CombinedOutput(); listErr != nil {
		return backupItem{}, fmt.Errorf("pg_dump produced an unreadable archive: %s", strings.TrimSpace(string(output)))
	}

	files := map[string]string{"database.dump": dumpPath}
	for _, source := range []string{"master.key", "setup.key", "database.enc"} {
		path := filepath.Join(a.store.Dir(), source)
		if _, statErr := os.Stat(path); statErr != nil {
			return backupItem{}, fmt.Errorf("falta %s en la raíz persistente", source)
		}
		files["bootstrap/"+source] = path
	}
	var instanceID string
	if err = a.db.QueryRowContext(backupCtx, `SELECT instance_id::text FROM tgstrm.installation WHERE id`).Scan(&instanceID); err != nil {
		return backupItem{}, errors.New("no se pudo leer la identidad de la instalación")
	}
	manifest := backupManifest{FormatVersion: backupFormatVersion, CreatedAt: stamp, InstanceID: instanceID, Database: cfg.Name, PostgreSQL: pgVersion, Files: make(map[string]string, len(files))}
	for archiveName, source := range files {
		sum, sumErr := fileSHA256(source)
		if sumErr != nil {
			return backupItem{}, sumErr
		}
		manifest.Files[archiveName] = sum
	}

	temp, err := os.CreateTemp(backupDir, ".tgstrm-backup-*.tmp")
	if err != nil {
		return backupItem{}, err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err = writeBackupArchive(temp, manifest, files); err != nil {
		temp.Close()
		return backupItem{}, err
	}
	if err = temp.Sync(); err != nil {
		temp.Close()
		return backupItem{}, err
	}
	if err = temp.Close(); err != nil {
		return backupItem{}, err
	}
	finalPath := filepath.Join(backupDir, name)
	if err = os.Rename(tempPath, finalPath); err != nil {
		return backupItem{}, err
	}
	info, err := os.Stat(finalPath)
	if err != nil {
		return backupItem{}, err
	}
	policy, _, _ := a.loadBackupSettings(ctx)
	if policy.Retention > 0 {
		_ = pruneBackups(backupDir, policy.Retention)
	}
	return backupItem{Name: name, CreatedAt: stamp, Size: info.Size(), Version: backupFormatVersion, Valid: true, Restorable: true, instanceID: instanceID}, nil
}

func (a *API) backupSettings(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		fail(w, http.StatusServiceUnavailable, "database_unavailable", "La base de datos no está disponible.")
		return
	}
	if r.Method == http.MethodGet {
		policy, state, err := a.loadBackupSettings(r.Context())
		if err != nil {
			fail(w, 500, "backup_settings_failed", "No se pudo leer la programación.")
			return
		}
		writeJSON(w, 200, backupSettingsPayload{backupPolicy: policy, backupState: state})
		return
	}
	var policy backupPolicy
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024)).Decode(&policy) != nil {
		fail(w, 400, "invalid_backup_policy", "La programación no es válida.")
		return
	}
	if err := validateBackupPolicy(policy); err != nil {
		fail(w, 422, "invalid_backup_policy", err.Error())
		return
	}
	_, currentState, _ := a.loadBackupSettings(r.Context())
	if policy.Enabled {
		next := nextBackupRun(time.Now(), policy)
		currentState.NextRunAt = &next
	} else {
		currentState.NextRunAt = nil
	}
	if err := a.saveBackupSettings(r.Context(), policy, currentState); err != nil {
		fail(w, 500, "backup_settings_failed", "No se pudo guardar la programación.")
		return
	}
	writeJSON(w, 200, backupSettingsPayload{backupPolicy: policy, backupState: currentState})
}

func validateBackupPolicy(policy backupPolicy) error {
	if policy.Frequency != "daily" && policy.Frequency != "weekly" && policy.Frequency != "interval" {
		return errors.New("Selecciona una frecuencia válida.")
	}
	if _, err := time.Parse("15:04", policy.Hour); err != nil {
		return errors.New("Selecciona una hora válida.")
	}
	if policy.Weekday < 0 || policy.Weekday > 6 || policy.IntervalHours < 1 || policy.IntervalHours > 168 {
		return errors.New("El intervalo de la copia no es válido.")
	}
	if policy.Retention < 1 || policy.Retention > 100 {
		return errors.New("Conserva entre 1 y 100 copias.")
	}
	return nil
}

func (a *API) loadBackupSettings(ctx context.Context) (backupPolicy, backupState, error) {
	policy := defaultBackupPolicy()
	state := backupState{}
	var raw []byte
	err := a.db.QueryRowContext(ctx, `SELECT value FROM tgstrm.settings WHERE key='backup_policy'`).Scan(&raw)
	if err == nil {
		if err = json.Unmarshal(raw, &policy); err != nil {
			return policy, state, err
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return policy, state, err
	}
	err = a.db.QueryRowContext(ctx, `SELECT value FROM tgstrm.settings WHERE key='backup_state'`).Scan(&raw)
	if err == nil {
		if err = json.Unmarshal(raw, &state); err != nil {
			return policy, state, err
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return policy, state, err
	}
	return policy, state, nil
}

func (a *API) saveBackupSettings(ctx context.Context, policy backupPolicy, state backupState) error {
	policyRaw, _ := json.Marshal(policy)
	stateRaw, _ := json.Marshal(state)
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for key, raw := range map[string][]byte{"backup_policy": policyRaw, "backup_state": stateRaw} {
		if _, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.settings(key,value) VALUES($1,$2) ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value,revision=tgstrm.settings.revision+1,updated_at=now()`, key, raw); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func nextBackupRun(now time.Time, policy backupPolicy) time.Time {
	if policy.Frequency == "interval" {
		return now.Add(time.Duration(policy.IntervalHours) * time.Hour)
	}
	clock, _ := time.Parse("15:04", policy.Hour)
	next := time.Date(now.Year(), now.Month(), now.Day(), clock.Hour(), clock.Minute(), 0, 0, now.Location())
	if policy.Frequency == "daily" {
		if !next.After(now) {
			next = next.AddDate(0, 0, 1)
		}
		return next
	}
	days := (policy.Weekday - int(next.Weekday()) + 7) % 7
	next = next.AddDate(0, 0, days)
	if !next.After(now) {
		next = next.AddDate(0, 0, 7)
	}
	return next
}

func (a *API) runBackupScheduler(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	a.runScheduledBackup(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.runScheduledBackup(ctx)
		}
	}
}

func (a *API) runScheduledBackup(ctx context.Context) {
	policy, state, err := a.loadBackupSettings(ctx)
	if err != nil || !policy.Enabled {
		return
	}
	if state.NextRunAt == nil {
		next := nextBackupRun(time.Now(), policy)
		state.NextRunAt = &next
		_ = a.saveBackupSettings(ctx, policy, state)
		return
	}
	if time.Now().Before(*state.NextRunAt) {
		return
	}
	runAt := time.Now()
	item, runErr := a.createBackup(ctx)
	state.LastRunAt = &runAt
	next := nextBackupRun(time.Now(), policy)
	state.NextRunAt = &next
	if runErr != nil {
		state.LastStatus, state.LastError = "failed", fmt.Sprintf("%.500s", runErr.Error())
	} else {
		state.LastStatus, state.LastError = "completed", ""
	}
	_ = a.saveBackupSettings(ctx, policy, state)
	status, detail := "completed", "Copia automática creada: "+item.Name
	if runErr != nil {
		status, detail = "failed", "La copia automática falló: "+state.LastError
	}
	_, _ = a.db.ExecContext(ctx, `INSERT INTO tgstrm.activity_events(kind,status,title,detail) VALUES('backup',$1,'Copia de seguridad',$2)`, status, detail)
	_, _ = a.db.ExecContext(ctx, `INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload) VALUES('backup.completed','system',0,jsonb_build_object('status',$1,'detail',$2))`, status, detail)
}

func pruneBackups(directory string, keep int) error {
	items, err := listBackups(directory)
	if err != nil {
		return err
	}
	if len(items) <= keep {
		return nil
	}
	for _, item := range items[keep:] {
		if err = os.Remove(filepath.Join(directory, item.Name)); err != nil {
			return err
		}
	}
	return nil
}

func writeBackupArchive(destination io.Writer, manifest backupManifest, files map[string]string) error {
	gzipWriter := gzip.NewWriter(destination)
	tarWriter := tar.NewWriter(gzipWriter)
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err == nil {
		err = tarWriter.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0600, Size: int64(len(manifestBytes)), ModTime: manifest.CreatedAt})
	}
	if err == nil {
		_, err = tarWriter.Write(manifestBytes)
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err != nil {
			break
		}
		file, openErr := os.Open(files[name])
		if openErr != nil {
			err = openErr
			break
		}
		info, statErr := file.Stat()
		if statErr == nil {
			err = tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: info.Size(), ModTime: info.ModTime()})
		}
		if err == nil {
			_, err = io.Copy(tarWriter, file)
		}
		file.Close()
	}
	if closeErr := tarWriter.Close(); err == nil {
		err = closeErr
	}
	if closeErr := gzipWriter.Close(); err == nil {
		err = closeErr
	}
	return err
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err = io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func listBackups(directory string) ([]backupItem, error) {
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return []backupItem{}, nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]backupItem, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !backupNamePattern.MatchString(entry.Name()) {
			continue
		}
		info, statErr := entry.Info()
		if statErr != nil {
			continue
		}
		manifest, manifestErr := readBackupManifest(filepath.Join(directory, entry.Name()))
		item := backupItem{Name: entry.Name(), CreatedAt: info.ModTime(), Size: info.Size(), Valid: manifestErr == nil}
		if manifestErr == nil {
			item.CreatedAt, item.Version, item.instanceID = manifest.CreatedAt, manifest.FormatVersion, manifest.InstanceID
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items, nil
}

func readBackupManifest(path string) (backupManifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return backupManifest{}, err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return backupManifest{}, err
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	header, err := tarReader.Next()
	if err != nil || header.Name != "manifest.json" || header.Size > 1024*1024 {
		return backupManifest{}, errors.New("manifiesto inválido")
	}
	var manifest backupManifest
	err = json.NewDecoder(io.LimitReader(tarReader, header.Size)).Decode(&manifest)
	return manifest, err
}
