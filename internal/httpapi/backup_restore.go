package httpapi

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var restoreFiles = map[string]bool{
	"database.dump":          true,
	"bootstrap/master.key":   true,
	"bootstrap/setup.key":    true,
	"bootstrap/database.enc": true,
}

func (a *API) restoreBackup(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		fail(w, http.StatusServiceUnavailable, "database_unavailable", "La base de datos no está disponible.")
		return
	}
	var input struct {
		Confirmation string `json:"confirmation"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input) != nil || input.Confirmation != "RESTAURAR" {
		fail(w, 422, "confirmation_required", "Escribe RESTAURAR para confirmar la operación.")
		return
	}
	name := r.PathValue("name")
	if !backupNamePattern.MatchString(name) {
		fail(w, 404, "backup_not_found", "La copia no existe.")
		return
	}
	a.restoreMu.Lock()
	defer a.restoreMu.Unlock()
	a.mu.Lock()
	maintenance := a.maintenance
	a.mu.Unlock()
	if maintenance {
		fail(w, 409, "maintenance_active", "TgStrm ya está en modo mantenimiento.")
		return
	}

	restoreCtx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()
	prepared, err := a.prepareRestore(restoreCtx, filepath.Join(a.store.Dir(), "backups", name))
	if err != nil {
		fail(w, 422, "backup_invalid", fmt.Sprintf("La copia no se puede restaurar: %.400s", err.Error()))
		return
	}
	defer os.RemoveAll(prepared.dir)

	safety, err := a.createBackup(restoreCtx)
	if err != nil {
		fail(w, 500, "safety_backup_failed", "No se pudo crear la copia preventiva. No se modificó la instalación.")
		return
	}
	safetyPath := filepath.Join(a.store.Dir(), "backups", safety.Name)
	// Validate and stage the safety archive while the current database and
	// bootstrap files still belong to the same installation. This avoids a
	// rollback preflight depending on a partially restored installation.
	safetyPrepared, err := a.prepareRestore(restoreCtx, safetyPath)
	if err != nil {
		fail(w, 500, "safety_backup_invalid", fmt.Sprintf("La copia preventiva no pasó la validación (%s). No se modificó la instalación.", err.Error()))
		return
	}
	defer os.RemoveAll(safetyPrepared.dir)
	a.mu.Lock()
	a.maintenance = true
	if a.backgroundCancel != nil {
		a.backgroundCancel()
	}
	a.mu.Unlock()
	if a.telegram != nil {
		a.telegram.Stop()
	}
	a.backgroundWG.Wait()

	if err = a.runPGRestore(restoreCtx, prepared); err != nil {
		fail(w, 500, "restore_failed", fmt.Sprintf("PostgreSQL rechazó la restauración. La transacción se revirtió; reinicia TgStrm. Detalle: %.300s", err.Error()))
		return
	}
	if err = replaceBootstrapFiles(a.store.Dir(), prepared.dir); err != nil {
		// The database restore has already committed. If bootstrap replacement
		// fails, restore the database from the safety snapshot so it remains
		// paired with the original encrypted connection settings and keys.
		_ = os.RemoveAll(prepared.dir)
		rollbackCtx, rollbackCancel := context.WithTimeout(context.WithoutCancel(r.Context()), 30*time.Minute)
		defer rollbackCancel()
		safetyErr := a.runPGRestore(rollbackCtx, safetyPrepared)
		var bootstrapErr error
		if safetyErr == nil {
			bootstrapErr = replaceBootstrapFiles(a.store.Dir(), safetyPrepared.dir)
		}
		if safetyErr != nil {
			fail(w, 500, "restore_rollback_failed", fmt.Sprintf("Falló la sustitución de la configuración (%s) y también la restauración de seguridad (%s). La copia preventiva es %s. No reinicies hasta revisar el estado de PostgreSQL.", err.Error(), safetyErr.Error(), safety.Name))
			return
		}
		if bootstrapErr != nil {
			fail(w, 500, "restore_rollback_incomplete", fmt.Sprintf("PostgreSQL volvió a la copia preventiva %s, pero no se pudieron reponer sus archivos de configuración (%s). No reinicies hasta revisar la instalación.", safety.Name, bootstrapErr.Error()))
			return
		}
		fail(w, 500, "restore_bootstrap_rolled_back", fmt.Sprintf("No se pudo reemplazar la configuración cifrada (%s). PostgreSQL volvió a la copia preventiva %s; reinicia TgStrm para reconectar los servicios.", err.Error(), safety.Name))
		return
	}
	writeJSON(w, 200, map[string]any{"status": "restored", "restart_required": true, "backup": name, "safety_backup": safety.Name})
}

type preparedRestore struct {
	dir           string
	manifest      backupManifest
	pgRestorePath string
	cfg           struct {
		host, name, user, password, sslMode, rootCert string
		port                                          uint16
	}
}

func (a *API) prepareRestore(ctx context.Context, archivePath string) (preparedRestore, error) {
	var prepared preparedRestore
	info, err := os.Stat(archivePath)
	if err != nil || !info.Mode().IsRegular() {
		return prepared, errors.New("archivo no encontrado")
	}
	prepared.dir, err = os.MkdirTemp(filepath.Join(a.store.Dir(), "temp"), "restore-*")
	if err != nil {
		return prepared, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			os.RemoveAll(prepared.dir)
		}
	}()
	if err = extractRestoreArchive(archivePath, prepared.dir, &prepared.manifest); err != nil {
		return prepared, err
	}
	if prepared.manifest.FormatVersion != backupFormatVersion {
		return prepared, errors.New("versión de copia incompatible")
	}
	var currentInstance string
	if err = a.db.QueryRowContext(ctx, `SELECT instance_id::text FROM tgstrm.installation WHERE id`).Scan(&currentInstance); err != nil || prepared.manifest.InstanceID == "" || prepared.manifest.InstanceID != currentInstance {
		return prepared, errors.New("la copia pertenece a otra instalación")
	}
	cfg, err := a.store.LoadDatabase()
	if err != nil || cfg == nil || prepared.manifest.Database != cfg.Name {
		return prepared, errors.New("la base de datos de la copia no coincide con esta instalación")
	}
	prepared.cfg.host, prepared.cfg.port, prepared.cfg.name = cfg.Host, cfg.Port, cfg.Name
	prepared.cfg.user, prepared.cfg.password, prepared.cfg.sslMode, prepared.cfg.rootCert = cfg.User, cfg.Password, cfg.SSLMode, cfg.RootCert
	var serverVersion string
	if err = a.db.QueryRowContext(ctx, `SHOW server_version`).Scan(&serverVersion); err != nil {
		return prepared, fmt.Errorf("no se pudo consultar la versión del servidor PostgreSQL: %w", err)
	}
	serverMajor, err := parsePostgresMajor(serverVersion)
	if err != nil {
		return prepared, err
	}
	backupMajor, err := parsePostgresMajor(prepared.manifest.PostgreSQL)
	if err != nil {
		return prepared, fmt.Errorf("versión PostgreSQL del manifiesto inválida: %w", err)
	}
	if backupMajor > serverMajor {
		return prepared, fmt.Errorf("la copia requiere PostgreSQL %d o posterior; esta instalación usa PostgreSQL %d", backupMajor, serverMajor)
	}
	prepared.pgRestorePath, err = postgresToolPath("pg_restore", serverMajor)
	if err != nil {
		return prepared, err
	}
	cmd := exec.CommandContext(ctx, prepared.pgRestorePath, "--list", filepath.Join(prepared.dir, "database.dump"))
	if output, listErr := cmd.CombinedOutput(); listErr != nil {
		return prepared, fmt.Errorf("volcado PostgreSQL inválido: %s", strings.TrimSpace(string(output)))
	}
	cleanup = false
	return prepared, nil
}

func extractRestoreArchive(archivePath, destination string, manifest *backupManifest) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	found := map[string]string{}
	for {
		header, nextErr := tarReader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return nextErr
		}
		if header.Typeflag != tar.TypeReg || header.Size < 0 || header.Size > 20*1024*1024*1024 {
			return errors.New("la copia contiene una entrada no permitida")
		}
		if header.Name == "manifest.json" {
			if header.Size > 1024*1024 || json.NewDecoder(io.LimitReader(tarReader, header.Size)).Decode(manifest) != nil {
				return errors.New("manifiesto inválido")
			}
			continue
		}
		if !restoreFiles[header.Name] {
			return fmt.Errorf("entrada inesperada: %s", header.Name)
		}
		target := filepath.Join(destination, filepath.FromSlash(header.Name))
		if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		output, openErr := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if openErr != nil {
			return openErr
		}
		hash := sha256.New()
		_, copyErr := io.Copy(io.MultiWriter(output, hash), io.LimitReader(tarReader, header.Size))
		closeErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		found[header.Name] = hex.EncodeToString(hash.Sum(nil))
	}
	if manifest.FormatVersion == 0 || len(found) != len(restoreFiles) {
		return errors.New("la copia está incompleta")
	}
	for name := range restoreFiles {
		if manifest.Files[name] == "" || found[name] != manifest.Files[name] {
			return fmt.Errorf("la suma de %s no coincide", name)
		}
	}
	return nil
}

func (a *API) runPGRestore(ctx context.Context, prepared preparedRestore) error {
	args := []string{"--clean", "--if-exists", "--single-transaction", "--no-owner", "--no-acl", "--schema=tgstrm", "--host", prepared.cfg.host, "--port", strconv.Itoa(int(prepared.cfg.port)), "--username", prepared.cfg.user, "--dbname", prepared.cfg.name, filepath.Join(prepared.dir, "database.dump")}
	cmd := exec.CommandContext(ctx, prepared.pgRestorePath, args...)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+prepared.cfg.password, "PGSSLMODE="+prepared.cfg.sslMode)
	if prepared.cfg.rootCert != "" {
		cmd.Env = append(cmd.Env, "PGSSLROOTCERT="+prepared.cfg.rootCert)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pg_restore: %s", strings.TrimSpace(stderr.String()))
	}
	return nil
}

func replaceBootstrapFiles(root, extracted string) error {
	names := []string{"master.key", "setup.key", "database.enc"}
	staged := make(map[string]string, len(names))
	defer func() {
		for _, path := range staged {
			_ = os.Remove(path)
		}
	}()
	for _, name := range names {
		source := filepath.Join(extracted, "bootstrap", name)
		data, err := os.ReadFile(source)
		if err != nil {
			return err
		}
		file, err := os.CreateTemp(root, ".restore-"+name+"-*")
		if err != nil {
			return err
		}
		if err = file.Chmod(0600); err == nil {
			_, err = file.Write(data)
		}
		if err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			os.Remove(file.Name())
			return err
		}
		staged[name] = file.Name()
	}
	movedOld := []string{}
	for _, name := range names {
		target := filepath.Join(root, name)
		old := target + ".restore-old"
		_ = os.Remove(old)
		if err := os.Rename(target, old); err != nil {
			rollbackErrs := []error{}
			for _, previous := range movedOld {
				if rollbackErr := os.Rename(filepath.Join(root, previous)+".restore-old", filepath.Join(root, previous)); rollbackErr != nil {
					rollbackErrs = append(rollbackErrs, rollbackErr)
				}
			}
			return errors.Join(append([]error{err}, rollbackErrs...)...)
		}
		movedOld = append(movedOld, name)
	}
	installed := []string{}
	for _, name := range names {
		target := filepath.Join(root, name)
		if err := os.Rename(staged[name], target); err != nil {
			rollbackErrs := []error{}
			for _, current := range installed {
				if rollbackErr := os.Remove(filepath.Join(root, current)); rollbackErr != nil {
					rollbackErrs = append(rollbackErrs, rollbackErr)
				}
			}
			for _, previous := range movedOld {
				if rollbackErr := os.Rename(filepath.Join(root, previous)+".restore-old", filepath.Join(root, previous)); rollbackErr != nil {
					rollbackErrs = append(rollbackErrs, rollbackErr)
				}
			}
			return errors.Join(append([]error{err}, rollbackErrs...)...)
		}
		delete(staged, name)
		installed = append(installed, name)
	}
	for _, name := range names {
		_ = os.Remove(filepath.Join(root, name) + ".restore-old")
	}
	return nil
}
