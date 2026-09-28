package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
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

func TestLocalPostgresProbeIsDisabledInDockerMode(t *testing.T) {
	store, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a, err := NewWithOptions(store, Options{DatabaseSetupMode: "container"})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	r := httptest.NewRequest("POST", "http://example.com/api/v1/setup/database/probe-local", nil)
	r.Header.Set("Origin", "http://example.com")
	r.Header.Set("X-Setup-Token", store.Token)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("container probe status = %d: %s", w.Code, w.Body.String())
	}
}

func TestPostgresAppOwnerCanRunMigrationsWithoutClusterPrivileges(t *testing.T) {
	password := os.Getenv("TGSTRM_TEST_PG_PASSWORD")
	if password == "" {
		t.Skip("isolated PostgreSQL fixture required")
	}
	port, _ := strconv.Atoi(os.Getenv("TGSTRM_TEST_PG_PORT"))
	adminCfg := config.Database{Host: "127.0.0.1", Port: uint16(port), Name: "postgres", User: "tgstrm_test", Password: password, SSLMode: "disable"}
	adminDB, err := postgres.Connect(context.Background(), adminCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer adminDB.Close()
	roleName := fmt.Sprintf("tgstrm_app_test_%d", os.Getpid())
	databaseName := roleName
	appPassword := password + "-limited-role"
	var createRole string
	if err := adminDB.QueryRow("SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', $1::text, $2::text)", roleName, appPassword).Scan(&createRole); err != nil {
		t.Fatal(err)
	}
	if _, err := adminDB.Exec(createRole); err != nil {
		t.Fatal(err)
	}
	if _, err := adminDB.Exec("CREATE DATABASE " + pgx.Identifier{databaseName}.Sanitize() + " OWNER " + pgx.Identifier{roleName}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	appCfg := config.Database{Host: "127.0.0.1", Port: uint16(port), Name: databaseName, User: roleName, Password: appPassword, SSLMode: "disable"}
	appDB, err := postgres.Connect(context.Background(), appCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer appDB.Close()
	if err := postgres.Migrate(context.Background(), appDB); err != nil {
		t.Fatalf("least-privilege database owner migration: %v", err)
	}
	var isSuper, canCreateDB bool
	if err := adminDB.QueryRow("SELECT rolsuper, rolcreatedb FROM pg_roles WHERE rolname=$1", roleName).Scan(&isSuper, &canCreateDB); err != nil {
		t.Fatal(err)
	}
	if isSuper || canCreateDB {
		t.Fatal("application database role received cluster-level privileges")
	}
}

func TestPostgresBootstrapRestartAndMigrationIntegrity(t *testing.T) {
	password := os.Getenv("TGSTRM_TEST_PG_PASSWORD")
	if password == "" {
		t.Skip("isolated PostgreSQL fixture required")
	}
	port, _ := strconv.Atoi(os.Getenv("TGSTRM_TEST_PG_PORT"))
	cfg := config.Database{Host: "127.0.0.1", Port: uint16(port), Name: "tgstrm_bootstrap_test", User: "tgstrm_test", Password: password, SSLMode: "disable"}
	store, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(store)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(struct {
		config.Database
		CreateDatabase bool `json:"create_database"`
	}{Database: cfg, CreateDatabase: true})
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
	var databaseExists bool
	if err := func() error {
		probe, e := postgres.Connect(context.Background(), config.Database{Host: "127.0.0.1", Port: uint16(port), Name: "postgres", User: "tgstrm_test", Password: password, SSLMode: "disable"})
		if e != nil {
			return e
		}
		defer probe.Close()
		return probe.QueryRow("SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname=$1)", cfg.Name).Scan(&databaseExists)
	}(); err != nil {
		t.Fatal(err)
	}
	if databaseExists {
		t.Fatal("test connection created the database")
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
	var sessionCookie *http.Cookie
	authCall := func(path, payload string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://example.com"+path, strings.NewReader(payload))
		r.Header.Set("Origin", "http://example.com")
		r.Header.Set("Content-Type", "application/json")
		if sessionCookie != nil {
			r.AddCookie(sessionCookie)
		}
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		return w
	}
	login := authCall("/api/v1/admin/login", `{"username":"admin","password":"admin"}`)
	if login.Code != http.StatusOK {
		t.Fatalf("initial admin login: %d %s", login.Code, login.Body.String())
	}
	for _, cookie := range login.Result().Cookies() {
		if cookie.Name == adminCookieLocal {
			sessionCookie = cookie
		}
	}
	if sessionCookie == nil {
		t.Fatal("initial admin session cookie missing")
	}
	if skipped := authCall("/api/v1/admin/password/skip", "{}"); skipped.Code != http.StatusOK {
		t.Fatalf("initial password setup skip: %d %s", skipped.Code, skipped.Body.String())
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
