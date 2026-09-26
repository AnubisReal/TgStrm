package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"tgstrm/internal/config"
	"tgstrm/internal/storage/postgres"
)

func TestCatalogSearchRequiresTwoCharacters(t *testing.T) {
	a := &API{}
	r := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=x", nil)
	w := httptest.NewRecorder()
	a.catalogSearch(w, r)
	if w.Code != http.StatusOK || w.Body.String() != "{\"has_more\":false,\"items\":[],\"query\":\"x\"}\n" {
		t.Fatalf("unexpected response: %d %s", w.Code, w.Body.String())
	}
}

func TestCatalogSearchLive(t *testing.T) {
	if os.Getenv("TGSTRM_LIVE_SEARCH") != "1" {
		t.Skip("live catalog fixture not requested")
	}
	store, err := config.Open("../../data")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := store.LoadDatabase()
	if err != nil || cfg == nil {
		t.Fatalf("database config: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	db, err := postgres.Connect(ctx, *cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a := &API{db: db}
	for _, query := range []string{"alf", "Screeners", "El Desguace 202504"} {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/search?q="+url.QueryEscape(query)+"&limit=5", nil)
		w := httptest.NewRecorder()
		a.catalogSearch(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("search %s: %d %s", query, w.Code, w.Body.String())
		}
		t.Logf("search %s: %s", query, w.Body.String())
	}
	var topics int
	if err := db.QueryRow(`SELECT count(*) FROM tgstrm.topics t JOIN tgstrm.chats c ON c.id=t.chat_id WHERE lower(t.title) LIKE '%screeners%' OR lower(c.title) LIKE '%desguace%'`).Scan(&topics); err != nil {
		t.Fatal(err)
	}
	t.Logf("screeners/desguace topics: %d", topics)
}

func TestTelegramGlobalSearchLive(t *testing.T) {
	if os.Getenv("TGSTRM_LIVE_TELEGRAM_SEARCH") != "1" {
		t.Skip("live Telegram fixture not requested")
	}
	store, err := config.Open("../../data")
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(store)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if a.telegram == nil {
		t.Fatal("telegram service unavailable")
	}
	deadline := time.Now().Add(15 * time.Second)
	for !a.telegram.Connected() && time.Now().Before(deadline) {
		time.Sleep(250 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	refreshCtx, refreshCancel := context.WithTimeout(context.Background(), 20*time.Second)
	_ = a.telegram.RefreshSearchTopics(refreshCtx)
	refreshCancel()
	for _, query := range []string{"Screeners", "ALF", "El Desguace"} {
		items, err := a.telegram.GlobalSearch(ctx, query, 10)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("Telegram %s results: %d", query, len(items))
	}
}

func TestCatalogSearchWithTelegramLive(t *testing.T) {
	if os.Getenv("TGSTRM_LIVE_CATALOG_SEARCH") != "1" {
		t.Skip("live catalog+Telegram fixture not requested")
	}
	store, err := config.Open("../../data")
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(store)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	service := a.telegramService()
	if service == nil {
		t.Fatal("telegram service unavailable")
	}
	deadline := time.Now().Add(20 * time.Second)
	for !service.Connected() && time.Now().Before(deadline) {
		time.Sleep(250 * time.Millisecond)
	}
	for _, query := range []string{"Screeners", "El Desguace 202504"} {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/search?q="+url.QueryEscape(query)+"&limit=10", nil)
		w := httptest.NewRecorder()
		a.catalogSearch(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("search %s: %d %s", query, w.Code, w.Body.String())
		}
		t.Logf("catalog search %s: %s", query, w.Body.String())
	}
}
