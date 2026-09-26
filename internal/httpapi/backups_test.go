package httpapi

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNextBackupRun(t *testing.T) {
	location := time.FixedZone("test", 2*60*60)
	now := time.Date(2026, 9, 19, 16, 0, 0, 0, location) // Saturday.
	tests := []struct {
		name   string
		policy backupPolicy
		want   time.Time
	}{
		{"daily", backupPolicy{Frequency: "daily", Hour: "03:00"}, time.Date(2026, 9, 20, 3, 0, 0, 0, location)},
		{"weekly", backupPolicy{Frequency: "weekly", Hour: "04:30", Weekday: 1}, time.Date(2026, 9, 21, 4, 30, 0, 0, location)},
		{"interval", backupPolicy{Frequency: "interval", IntervalHours: 6}, now.Add(6 * time.Hour)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := nextBackupRun(now, test.policy); !got.Equal(test.want) {
				t.Fatalf("next run = %s, want %s", got, test.want)
			}
		})
	}
}

func TestTMDBRateLimitSpacesConcurrentCallers(t *testing.T) {
	a := &API{}
	if err := a.waitTMDBRequestSlot(context.Background()); err != nil {
		t.Fatal(err)
	}
	a.tmdbRateMu.Lock()
	firstReady := a.tmdbNextRequest
	a.tmdbRateMu.Unlock()
	if err := a.waitTMDBRequestSlot(context.Background()); err != nil {
		t.Fatal(err)
	}
	a.tmdbRateMu.Lock()
	secondReady := a.tmdbNextRequest
	a.tmdbRateMu.Unlock()
	if secondReady.Sub(firstReady) != tmdbRequestInterval {
		t.Fatalf("request cadence = %s, want %s", secondReady.Sub(firstReady), tmdbRequestInterval)
	}
}

func TestTMDBRateLimitHonorsRetryAfterAndCancellation(t *testing.T) {
	a := &API{}
	a.deferTMDBRequests("3")
	a.tmdbRateMu.Lock()
	blockedFor := time.Until(a.tmdbBlockedUntil)
	a.tmdbRateMu.Unlock()
	if blockedFor < 2900*time.Millisecond || blockedFor > 3*time.Second {
		t.Fatalf("429 delay = %s, want approximately 3s", blockedFor)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := a.waitTMDBRequestSlot(ctx); err == nil {
		t.Fatal("canceled TMDB wait succeeded")
	}
}

func TestTMDBRateLimitDoesNotHoldRequestForLongCooldown(t *testing.T) {
	a := &API{}
	a.deferTMDBRequests("60")
	err := a.waitTMDBRequestSlot(context.Background())
	var responseErr tmdbResponseError
	if !errors.As(err, &responseErr) || responseErr.status != http.StatusTooManyRequests {
		t.Fatalf("long cooldown error = %v, want HTTP 429", err)
	}
}

func TestRestoreArchiveVerifiesEveryFile(t *testing.T) {
	source := t.TempDir()
	files := map[string]string{}
	for _, name := range []string{"database.dump", "bootstrap/master.key", "bootstrap/setup.key", "bootstrap/database.enc"} {
		path := filepath.Join(source, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("content:"+name), 0600); err != nil {
			t.Fatal(err)
		}
		files[name] = path
	}
	manifest := backupManifest{FormatVersion: backupFormatVersion, CreatedAt: time.Now(), InstanceID: "test-instance", Database: "test", Files: map[string]string{}}
	for name, path := range files {
		sum, err := fileSHA256(path)
		if err != nil {
			t.Fatal(err)
		}
		manifest.Files[name] = sum
	}
	archivePath := filepath.Join(t.TempDir(), "backup.tgz")
	archive, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if err = writeBackupArchive(archive, manifest, files); err != nil {
		t.Fatal(err)
	}
	if err = archive.Close(); err != nil {
		t.Fatal(err)
	}
	var extractedManifest backupManifest
	if err = extractRestoreArchive(archivePath, t.TempDir(), &extractedManifest); err != nil {
		t.Fatalf("valid archive rejected: %v", err)
	}
	if extractedManifest.InstanceID != manifest.InstanceID {
		t.Fatal("installation identity was not preserved")
	}
}

func TestRestoreArchiveRejectsChecksumMismatch(t *testing.T) {
	source := t.TempDir()
	files := map[string]string{}
	for _, name := range []string{"database.dump", "bootstrap/master.key", "bootstrap/setup.key", "bootstrap/database.enc"} {
		path := filepath.Join(source, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("content:"+name), 0600); err != nil {
			t.Fatal(err)
		}
		files[name] = path
	}
	manifest := backupManifest{FormatVersion: backupFormatVersion, CreatedAt: time.Now(), InstanceID: "test-instance", Database: "test", Files: map[string]string{}}
	for name, path := range files {
		sum, err := fileSHA256(path)
		if err != nil {
			t.Fatal(err)
		}
		manifest.Files[name] = sum
	}
	manifest.Files["database.dump"] = "not-the-real-checksum"
	archivePath := filepath.Join(t.TempDir(), "backup.tgz")
	archive, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if err = writeBackupArchive(archive, manifest, files); err != nil {
		t.Fatal(err)
	}
	if err = archive.Close(); err != nil {
		t.Fatal(err)
	}
	var extracted backupManifest
	if err = extractRestoreArchive(archivePath, t.TempDir(), &extracted); err == nil {
		t.Fatal("archive with a checksum mismatch was accepted")
	}
}

func TestReplaceBootstrapFilesDoesNotChangeFilesWhenStagingFails(t *testing.T) {
	root, extracted := t.TempDir(), t.TempDir()
	for _, name := range []string{"master.key", "setup.key", "database.enc"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("old:"+name), 0600); err != nil {
			t.Fatal(err)
		}
		if name != "database.enc" {
			if err := os.MkdirAll(filepath.Join(extracted, "bootstrap"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(extracted, "bootstrap", name), []byte("new:"+name), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	// A missing staged file must fail before the existing bootstrap is moved.
	if err := replaceBootstrapFiles(root, extracted); err == nil {
		t.Fatal("replacement unexpectedly succeeded without database.enc")
	}
	for _, name := range []string{"master.key", "setup.key", "database.enc"} {
		got, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || string(got) != "old:"+name {
			t.Fatalf("%s was not restored after rollback: %q, %v", name, got, err)
		}
	}
}

func TestReplaceBootstrapFilesStagesAndInstallsCompleteSet(t *testing.T) {
	root, extracted := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(extracted, "bootstrap"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"master.key", "setup.key", "database.enc"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("old:"+name), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(extracted, "bootstrap", name), []byte("new:"+name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := replaceBootstrapFiles(root, extracted); err != nil {
		t.Fatalf("replaceBootstrapFiles() error = %v", err)
	}
	for _, name := range []string{"master.key", "setup.key", "database.enc"} {
		got, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || string(got) != "new:"+name {
			t.Fatalf("%s = %q, %v; want new bootstrap content", name, got, err)
		}
		if _, err = os.Stat(filepath.Join(root, name) + ".restore-old"); !os.IsNotExist(err) {
			t.Fatalf("stale rollback file for %s remains: %v", name, err)
		}
	}
}
