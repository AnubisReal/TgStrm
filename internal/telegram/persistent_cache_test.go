package telegram

import (
	"os"
	"testing"
	"time"
)

func TestPersistentStreamCacheSurvivesRestartAndEvictsLRU(t *testing.T) {
	dir := t.TempDir()
	cache, err := NewPersistentStreamCache(dir, 8)
	if err != nil {
		t.Fatal(err)
	}
	if err = cache.Put("old", []byte("1234")); err != nil {
		t.Fatal(err)
	}
	if err = cache.Put("new", []byte("5678")); err != nil {
		t.Fatal(err)
	}
	oldPath := cache.entries[persistentCacheName("old")].path
	oldTime := time.Now().Add(-time.Hour)
	if err = os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if data, ok := cache.Get("new", 4); !ok || string(data) != "5678" {
		t.Fatalf("unexpected cache hit: %q %v", data, ok)
	}

	reopened, err := NewPersistentStreamCache(dir, 4)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reopened.Get("old", 4); ok {
		t.Fatal("oldest block was not evicted")
	}
	if data, ok := reopened.Get("new", 4); !ok || string(data) != "5678" {
		t.Fatalf("newest block did not survive restart: %q %v", data, ok)
	}
}

func TestPersistentStreamCacheRejectsCorruptBlock(t *testing.T) {
	dir := t.TempDir()
	cache, err := NewPersistentStreamCache(dir, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if err = cache.Put("video", []byte("valid")); err != nil {
		t.Fatal(err)
	}
	name := persistentCacheName("video")
	if err = os.WriteFile(cache.entries[name].path, []byte("bad"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, ok := cache.Get("video", 5); ok {
		t.Fatal("corrupt block was served")
	}
	if cache.Stats().Entries != 0 {
		t.Fatal("corrupt block was not removed")
	}
}

func TestPersistentStreamPolicy(t *testing.T) {
	if !shouldPersistStreamBlock(0, false) || !shouldPersistStreamBlock(PersistentStreamHeadSize-StreamCacheBlockSize, false) {
		t.Fatal("initial playback window must be persistent")
	}
	if shouldPersistStreamBlock(PersistentStreamHeadSize, false) {
		t.Fatal("ordinary playback after the initial window must remain ephemeral")
	}
	if !shouldPersistStreamBlock(PersistentStreamHeadSize*100, true) {
		t.Fatal("an explicitly detected MP4 index must be persistent")
	}
}
