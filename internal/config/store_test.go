package config

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestEncryptedConfigSurvivesRestartAndRejectsTampering(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := Database{Host: "localhost", Port: 5432, Name: "tgstrm", User: "test", Password: "test-secret", SSLMode: "disable"}
	if err = s.SaveDatabase(want); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "database.enc"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(want.Password)) {
		t.Fatal("plaintext secret on disk")
	}
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.LoadDatabase()
	if err != nil || got == nil || *got != want {
		t.Fatalf("restart lost config: %v", err)
	}
	if reopened.Token != s.Token {
		t.Fatal("setup key changed on restart")
	}
	raw[len(raw)-1] ^= 1
	os.WriteFile(filepath.Join(dir, "database.enc"), raw, 0600)
	if _, err = reopened.LoadDatabase(); err == nil {
		t.Fatal("tampered ciphertext accepted")
	}
	os.Remove(filepath.Join(dir, "master.key"))
	if _, err = Open(dir); err == nil {
		t.Fatal("replaced missing master key")
	}
}

func TestDataLayoutUsesPersistentRoot(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.EnsureDataLayout(); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{"library/movies", "library/shows", "temp", "backups"} {
		if info, statErr := os.Stat(filepath.Join(dir, filepath.FromSlash(relative))); statErr != nil || !info.IsDir() {
			t.Fatalf("missing data directory %s: %v", relative, statErr)
		}
	}
}
