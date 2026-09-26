package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"tgstrm/internal/config"
	"tgstrm/internal/storage/postgres"
)

func TestBootstrapRejectsUnauthenticatedAndForeignOrigins(t *testing.T) {
	store, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(store)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	for _, tc := range []struct {
		origin, token string
		status        int
	}{{"http://evil.test", store.Token, 403}, {"http://example.com", "bad", 401}, {"", store.Token, 403}} {
		r := httptest.NewRequest("POST", "http://example.com/api/v1/setup/database", strings.NewReader(`{}`))
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("X-Setup-Token", tc.token)
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("got %d want %d", w.Code, tc.status)
		}
	}
}

func TestTelegramStatusGETAllowsMissingOriginButRejectsCrossSite(t *testing.T) {
	store, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(store)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	r := httptest.NewRequest(http.MethodGet, "http://example.com/api/v1/telegram/auth", nil)
	w := httptest.NewRecorder()
	a.sameOrigin(a.telegramAuth("status"))(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("same-origin GET blocked: %d", w.Code)
	}
	r = httptest.NewRequest(http.MethodGet, "http://example.com/api/v1/telegram/auth", nil)
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w = httptest.NewRecorder()
	a.sameOrigin(a.telegramAuth("status"))(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-site GET accepted: %d", w.Code)
	}
}
func TestPostgresBootstrapRestartAndMigrationIntegrity(t *testing.T) {
	password := os.Getenv("TGSTRM_TEST_PG_PASSWORD")
	if password == "" {
		t.Skip("isolated PostgreSQL fixture required")
	}
	port, _ := strconv.Atoi(os.Getenv("TGSTRM_TEST_PG_PORT"))
	cfg := config.Database{Host: "127.0.0.1", Port: uint16(port), Name: "postgres", User: "tgstrm_test", Password: password, SSLMode: "disable"}
	store, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(store)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(cfg)
	call := func(a *API, path string, payload []byte) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://example.com"+path, bytes.NewReader(payload))
		r.Header.Set("Origin", "http://example.com")
		r.Header.Set("X-Setup-Token", store.Token)
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		return w
	}
	if w := call(a, "/api/v1/setup/database/test", body); w.Code != 200 {
		t.Fatalf("test: %d %s", w.Code, w.Body.String())
	}
	if c, _ := store.LoadDatabase(); c != nil {
		t.Fatal("test connection persisted config")
	}
	if w := call(a, "/api/v1/setup/database", body); w.Code != 201 {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	var instance string
	if err = a.db.QueryRow("SELECT instance_id FROM tgstrm.installation").Scan(&instance); err != nil {
		t.Fatal(err)
	}
	if _, err = a.db.Exec(`INSERT INTO tgstrm.settings(key,value) VALUES ('test','{"kept":true}')`); err != nil {
		t.Fatal(err)
	}
	if w := call(a, "/api/v1/setup/database", body); w.Code != 409 {
		t.Fatal("configuration replace accepted")
	}
	authCall := func(path, payload string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://example.com"+path, strings.NewReader(payload))
		r.Header.Set("Origin", "http://example.com")
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		return w
	}
	if w := authCall("/api/v1/telegram/auth/configure", `{"api_id":12,"api_hash":"bad"}`); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid telegram credentials accepted: %d %s", w.Code, w.Body.String())
	}
	apiHash := "0123456789abcdef0123456789abcdef"
	if w := authCall("/api/v1/telegram/auth/configure", `{"api_id":12345,"api_hash":"`+apiHash+`"}`); w.Code != http.StatusOK {
		t.Fatalf("telegram credentials: %d %s", w.Code, w.Body.String())
	}
	var credentials []byte
	if err := a.db.QueryRow("SELECT credentials_ciphertext FROM tgstrm.telegram_accounts").Scan(&credentials); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(credentials, []byte(apiHash)) {
		t.Fatal("telegram API hash stored in plaintext")
	}
	a.Close()
	a, err = New(store)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if a.db == nil || a.recovery {
		t.Fatal("restart not ready")
	}
	var restored string
	if err = a.db.QueryRow("SELECT instance_id FROM tgstrm.installation").Scan(&restored); err != nil || restored != instance {
		t.Fatal("installation identity changed")
	}
	var count int
	a.db.QueryRow("SELECT count(*) FROM tgstrm.settings WHERE key='test'").Scan(&count)
	if count != 1 {
		t.Fatal("settings lost")
	}
	if err = postgres.Migrate(context.Background(), a.db); err != nil {
		t.Fatal(err)
	}
	if _, err = a.db.Exec("UPDATE tgstrm.schema_migrations SET checksum='tampered'"); err != nil {
		t.Fatal(err)
	}
	if err = postgres.Migrate(context.Background(), a.db); err == nil {
		t.Fatal("modified migration accepted")
	}
	a.Close()
	b, err := New(store)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if !b.configured || !b.recovery {
		t.Fatal("incompatible schema reset to first install")
	}
}
