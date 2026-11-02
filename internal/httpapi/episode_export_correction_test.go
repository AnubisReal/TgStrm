package httpapi

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"
)

func TestEpisodeArchiveResumesAfterFilesystemMove(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	old := "Episode S01E01.strm"
	backup := ".tgstrm-recovery/episode-corrections/asset/Episode S01E01.strm"
	content := []byte("https://example.invalid/stream/asset\n")
	hash := sha256.Sum256(content)
	if err = root.WriteFile(old, content, 0600); err != nil {
		t.Fatal(err)
	}
	moved, err := archiveOwnedSTRM(root, old, backup, hash[:])
	if err != nil || !moved {
		t.Fatal(moved, err)
	}
	// The app can die here, before the database stores the new path.
	moved, err = archiveOwnedSTRM(root, old, backup, hash[:])
	if err != nil || moved {
		t.Fatal("recovery retry failed", moved, err)
	}
	got, err := root.ReadFile(backup)
	if err != nil || string(got) != string(content) {
		t.Fatal("recovery content changed", err)
	}
	if err = root.WriteFile(backup, []byte("manual change"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = archiveOwnedSTRM(root, old, backup, hash[:]); err == nil {
		t.Fatal("changed recovery accepted")
	}
	if _, err = archiveOwnedSTRM(root, "../outside.strm", backup, hash[:]); err == nil {
		t.Fatal("escape accepted")
	}
	outside := t.TempDir()
	if err = os.WriteFile(filepath.Join(outside, "video.strm"), content, 0600); err != nil {
		t.Fatal(err)
	}
	if err = root.Symlink(outside, "linked"); err != nil {
		t.Fatal(err)
	}
	if _, err = archiveOwnedSTRM(root, "linked/video.strm", backup, hash[:]); err == nil {
		t.Fatal("external link accepted")
	}
}
