package httpapi

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"tgstrm/internal/config"
)

type movieAuditTransport func(*http.Request) (*http.Response, error)

func (f movieAuditTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestMovieProviderEvidence(t *testing.T) {
	db := correctionsAuditDB(t)
	store, err := config.Open(os.Getenv("TGSTRM_TEST_DATA_DIR"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name                   string
		search, aliases, dates string
		ok                     bool
		missing                bool
	}{
		{"official alias", `{"results":[{"id":1,"title":"Otro título","original_title":"Other title","release_date":"2001-01-01"}]}`, `{"titles":[{"title":"Nombre alternativo"}]}`, `{}`, true, false},
		{"alias budget exhausted", `{"results":[{"id":1,"title":"Other","release_date":"2001-01-01"},{"id":2,"title":"Other","release_date":"2001-01-01"},{"id":3,"title":"Other","release_date":"2001-01-01"},{"id":4,"title":"Other","release_date":"2001-01-01"},{"id":5,"title":"Other","release_date":"2001-01-01"},{"id":6,"title":"Other","release_date":"2001-01-01"},{"id":7,"title":"Other","release_date":"2001-01-01"}]}`, `{"titles":[{"title":"Nombre alternativo"}]}`, `{}`, false, false},
		{"ambiguous aliases", `{"results":[{"id":1,"title":"Otro título","release_date":"2001-01-01"},{"id":2,"title":"Otra película","release_date":"2001-01-01"}]}`, `{"titles":[{"title":"Nombre alternativo"}]}`, `{}`, false, false},
		{"regional release verified", `{"results":[{"id":1,"title":"Nombre alternativo","release_date":"2000-01-01"}]}`, `{}`, `{"results":[{"iso_3166_1":"ES","release_dates":[{"release_date":"2001-01-01"}]}]}`, true, false},
		{"wrong regional year", `{"results":[{"id":1,"title":"Nombre alternativo","release_date":"2000-01-01"}]}`, `{}`, `{"results":[{"iso_3166_1":"ES","release_dates":[{"release_date":"2002-01-01"}]}]}`, false, false},
		{"another country not Spain", `{"results":[{"id":1,"title":"Nombre alternativo","release_date":"2000-01-01"}]}`, `{}`, `{"results":[{"iso_3166_1":"US","release_dates":[{"release_date":"2001-01-01"}]}]}`, false, false},
		{"stale candidate before valid alias", `{"results":[{"id":2,"title":"Stale","release_date":"2001-01-01"},{"id":1,"title":"Other","release_date":"2001-01-01"}]}`, `{"titles":[{"title":"Nombre alternativo"}]}`, `{}`, true, true},
		{"only stale candidate", `{"results":[{"id":2,"title":"Stale","release_date":"2001-01-01"}]}`, `{}`, `{}`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &API{db: db, store: store, client: &http.Client{Transport: movieAuditTransport(func(r *http.Request) (*http.Response, error) {
				if tc.missing && strings.Contains(r.URL.Path, "/movie/2/") {
					return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
				}
				body := tc.search
				if strings.HasSuffix(r.URL.Path, "alternative_titles") {
					body = tc.aliases
				}
				if strings.HasSuffix(r.URL.Path, "release_dates") {
					body = tc.dates
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}}
			_, ok, err := a.identifyMonitoredMovie(context.Background(), "", "Nombre alternativo (2001).mkv")
			if err != nil || ok != tc.ok {
				t.Fatalf("ok=%v err=%v", ok, err)
			}
		})
	}
}
