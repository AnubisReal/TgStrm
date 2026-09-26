package telegram

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const DefaultPersistentCacheLimit int64 = 512 * 1024 * 1024

type PersistentCacheStats struct {
	UsedBytes  int64 `json:"used_bytes"`
	LimitBytes int64 `json:"limit_bytes"`
	Entries    int   `json:"entries"`
}

type persistentCacheEntry struct {
	path   string
	size   int64
	usedAt time.Time
}

// PersistentStreamCache stores only derived Telegram blocks. Its directory can
// be deleted safely at any time; Telegram and PostgreSQL remain authoritative.
type PersistentStreamCache struct {
	mu      sync.Mutex
	dir     string
	limit   int64
	used    int64
	entries map[string]persistentCacheEntry
}

func NewPersistentStreamCache(dir string, limit int64) (*PersistentStreamCache, error) {
	if limit <= 0 {
		limit = DefaultPersistentCacheLimit
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	c := &PersistentStreamCache{dir: dir, limit: limit, entries: make(map[string]persistentCacheEntry)}
	items, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item.IsDir() || !strings.HasSuffix(item.Name(), ".block") {
			continue
		}
		info, statErr := item.Info()
		if statErr != nil || info.Size() <= 0 || info.Size() > StreamCacheBlockSize {
			continue
		}
		path := filepath.Join(dir, item.Name())
		c.entries[item.Name()] = persistentCacheEntry{path: path, size: info.Size(), usedAt: info.ModTime()}
		c.used += info.Size()
	}
	c.evictLocked()
	return c, nil
}

func persistentCacheName(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:]) + ".block"
}

func (c *PersistentStreamCache) Get(key string, expectedSize int64) ([]byte, bool) {
	if c == nil {
		return nil, false
	}
	name := persistentCacheName(key)
	c.mu.Lock()
	entry, ok := c.entries[name]
	if !ok || entry.size != expectedSize {
		if ok {
			c.removeLocked(name, entry)
		}
		c.mu.Unlock()
		return nil, false
	}
	data, err := os.ReadFile(entry.path)
	if err != nil || int64(len(data)) != expectedSize {
		c.removeLocked(name, entry)
		c.mu.Unlock()
		return nil, false
	}
	now := time.Now()
	if now.Sub(entry.usedAt) >= time.Minute {
		entry.usedAt = now
		c.entries[name] = entry
		_ = os.Chtimes(entry.path, now, now)
	}
	c.mu.Unlock()
	return data, true
}

func (c *PersistentStreamCache) Put(key string, data []byte) error {
	if c == nil || len(data) == 0 || int64(len(data)) > StreamCacheBlockSize {
		return nil
	}
	name := persistentCacheName(key)
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.entries[name]; ok && entry.size == int64(len(data)) {
		entry.usedAt = time.Now()
		c.entries[name] = entry
		return nil
	}
	temp, err := os.CreateTemp(c.dir, ".block-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err = temp.Chmod(0600); err == nil {
		_, err = temp.Write(data)
	}
	if err == nil {
		err = temp.Sync()
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	path := filepath.Join(c.dir, name)
	if previous, ok := c.entries[name]; ok {
		c.used -= previous.size
	}
	if err = os.Rename(tempName, path); err != nil {
		return err
	}
	if directory, openErr := os.Open(c.dir); openErr == nil {
		if syncErr := directory.Sync(); syncErr != nil {
			_ = directory.Close()
			return syncErr
		}
		_ = directory.Close()
	}
	now := time.Now()
	c.entries[name] = persistentCacheEntry{path: path, size: int64(len(data)), usedAt: now}
	c.used += int64(len(data))
	c.evictLocked()
	return nil
}

func (c *PersistentStreamCache) SetLimit(limit int64) {
	if c == nil || limit <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.limit = limit
	c.evictLocked()
}

func (c *PersistentStreamCache) Stats() PersistentCacheStats {
	if c == nil {
		return PersistentCacheStats{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return PersistentCacheStats{UsedBytes: c.used, LimitBytes: c.limit, Entries: len(c.entries)}
}

func (c *PersistentStreamCache) Clear() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var result error
	for name, entry := range c.entries {
		if err := os.Remove(entry.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			result = errors.Join(result, err)
			continue
		}
		delete(c.entries, name)
	}
	c.used = 0
	return result
}

func (c *PersistentStreamCache) removeLocked(name string, entry persistentCacheEntry) {
	_ = os.Remove(entry.path)
	delete(c.entries, name)
	c.used -= entry.size
	if c.used < 0 {
		c.used = 0
	}
}

func (c *PersistentStreamCache) evictLocked() {
	if c.used <= c.limit {
		return
	}
	type candidate struct {
		name  string
		entry persistentCacheEntry
	}
	items := make([]candidate, 0, len(c.entries))
	for name, entry := range c.entries {
		items = append(items, candidate{name, entry})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].entry.usedAt.Before(items[j].entry.usedAt) })
	for _, item := range items {
		if c.used <= c.limit {
			break
		}
		c.removeLocked(item.name, item.entry)
	}
}

func StreamCacheKey(location VideoFileLocation, blockStart int64) string {
	return fmt.Sprintf("v1:%d:%d:%d", location.DocumentID, location.ByteSize, blockStart)
}
