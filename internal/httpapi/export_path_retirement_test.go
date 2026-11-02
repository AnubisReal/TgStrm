package httpapi

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"
)

func TestRetiredSTRMRemovalIsBoundedAndVerified(t *testing.T) {
	root := t.TempDir()
	content := []byte("https://example.invalid/stream/asset\n")
	hash := sha256.Sum256(content)
	path := filepath.Join(root, "Old", "Season 01", "Episode.strm")
	if err := atomicWrite(path, content); err != nil {
		t.Fatal(err)
	}
	if err := removeRetiredSTRM(root, "../outside.strm", hash[:]); err == nil {
		t.Fatal("accepted escape")
	}
	if err := removeRetiredSTRM(root, "Old/Season 01/Episode.strm", []byte("wrong")); err == nil {
		t.Fatal("removed changed content")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("old file disappeared before verification")
	}
	if err := removeRetiredSTRM(root, "Old/Season 01/Episode.strm", hash[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "Old")); !os.IsNotExist(err) {
		t.Fatal("empty old tree remains", err)
	}
	if err := removeRetiredSTRM(root, "Old/Season 01/Episode.strm", hash[:]); err != nil {
		t.Fatal("retry not idempotent", err)
	}
	outside := t.TempDir()
	victim := filepath.Join(outside, "keep.strm")
	if err := os.WriteFile(victim, content, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := removeRetiredSTRM(root, "linked/keep.strm", hash[:]); err == nil {
		t.Fatal("accepted external symlink")
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatal("external file modified")
	}
}
