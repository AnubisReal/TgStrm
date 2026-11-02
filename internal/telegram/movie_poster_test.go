package telegram

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type posterTransport func(*http.Request) (*http.Response, error)

func (f posterTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMoviePosterDownloadValidation(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		ok         bool
	}{
		{"jpeg", "\xff\xd8\xff\xe0poster", 200, true},
		{"html", "<html>error</html>", 200, false},
		{"missing", "", 404, false},
		{"oversized", strings.Repeat("x", (8<<20)+1), 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: posterTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host != "image.tmdb.org" || r.URL.Scheme != "https" {
					t.Fatal("unexpected destination")
				}
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header)}, nil
			})}
			_, err := fetchMoviePoster(context.Background(), client, "/poster.jpg")
			if (err == nil) != tc.ok {
				t.Fatalf("unexpected result: %v", err)
			}
		})
	}
	if _, err := fetchMoviePoster(context.Background(), nil, "https://other.example/image.jpg"); err == nil {
		t.Fatal("arbitrary URL accepted")
	}
}
