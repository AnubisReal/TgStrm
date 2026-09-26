// Package config owns the minimal encrypted configuration required before PostgreSQL is available.
package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type Database struct {
	Host     string `json:"host"`
	Port     uint16 `json:"port"`
	Name     string `json:"name"`
	User     string `json:"user"`
	Password string `json:"password"`
	SSLMode  string `json:"ssl_mode"`
	RootCert string `json:"root_cert,omitempty"`
}

type Store struct {
	dir   string
	key   []byte
	Token string
}

// Dir is the persistent application root. In containers this is normally /data.
func (s *Store) Dir() string { return s.dir }

func (s *Store) EnsureDataLayout() error {
	for _, relative := range []string{
		filepath.Join("library", "movies"),
		filepath.Join("library", "shows"),
		filepath.Join("cache", "telegram-blocks"),
		"temp",
		"backups",
	} {
		if err := os.MkdirAll(filepath.Join(s.dir, relative), 0755); err != nil {
			return err
		}
	}
	return nil
}

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(dir, "database.enc")); err == nil {
		if _, err := os.Stat(filepath.Join(dir, "master.key")); err != nil {
			return nil, errors.New("missing master key for existing database configuration; restore the original key")
		}
	}
	key, err := readOrCreate(filepath.Join(dir, "master.key"), 32)
	if err != nil {
		return nil, err
	}
	token, err := readOrCreate(filepath.Join(dir, "setup.key"), 32)
	if err != nil {
		return nil, err
	}
	return &Store{dir: dir, key: key, Token: base64.RawURLEncoding.EncodeToString(token)}, nil
}
func readOrCreate(path string, n int) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err == nil {
		if len(b) != n {
			return nil, fmt.Errorf("invalid key file: %s", filepath.Base(path))
		}
		return b, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	b = make([]byte, n)
	if _, err = rand.Read(b); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		return readOrCreate(path, n)
	}
	if err != nil {
		return nil, err
	}
	if _, err = f.Write(b); err != nil {
		f.Close()
		return nil, err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return nil, err
	}
	return b, f.Close()
}
func (s *Store) aead() (cipher.AEAD, error) {
	b, e := aes.NewCipher(s.key)
	if e != nil {
		return nil, e
	}
	return cipher.NewGCM(b)
}
func (s *Store) LoadDatabase() (*Database, error) {
	b, err := os.ReadFile(filepath.Join(s.dir, "database.enc"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a, err := s.aead()
	if err != nil {
		return nil, err
	}
	if len(b) < a.NonceSize() {
		return nil, errors.New("invalid encrypted configuration")
	}
	plain, err := a.Open(nil, b[:a.NonceSize()], b[a.NonceSize():], []byte("tgstrm/database/v1"))
	if err != nil {
		return nil, errors.New("cannot decrypt database configuration")
	}
	var cfg Database
	if err = json.Unmarshal(plain, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
func (s *Store) SaveDatabase(cfg Database) error {
	plain, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	a, err := s.aead()
	if err != nil {
		return err
	}
	nonce := make([]byte, a.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return err
	}
	encrypted := a.Seal(nonce, nonce, plain, []byte("tgstrm/database/v1"))
	f, err := os.CreateTemp(s.dir, ".database-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(encrypted); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(s.dir, "database.enc"))
}

// Seal binds encrypted records to their purpose to prevent swapping ciphertexts.
func (s *Store) Seal(scope string, plain []byte) ([]byte, error) {
	a, err := s.aead()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, a.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return a.Seal(nonce, nonce, plain, []byte(scope)), nil
}
func (s *Store) Unseal(scope string, encrypted []byte) ([]byte, error) {
	a, err := s.aead()
	if err != nil {
		return nil, err
	}
	if len(encrypted) < a.NonceSize() {
		return nil, errors.New("invalid ciphertext")
	}
	return a.Open(nil, encrypted[:a.NonceSize()], encrypted[a.NonceSize():], []byte(scope))
}
