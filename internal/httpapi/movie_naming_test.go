package httpapi

import (
	"path/filepath"
	"testing"
)

func TestMovieSTRMPathUsesCanonicalTitleAndYear(t *testing.T) {
	got, ok := movieSTRMPath("Troya", "2004")
	if !ok {
		t.Fatal("expected a valid movie path")
	}
	want := filepath.Join("Troya (2004)", "Troya (2004).strm")
	if got != want {
		t.Fatalf("movieSTRMPath() = %q, want %q", got, want)
	}
}

func TestMovieSTRMPathRequiresConfirmedTitleAndYear(t *testing.T) {
	for _, input := range [][2]string{{"", "2004"}, {"Troya", ""}} {
		if got, ok := movieSTRMPath(input[0], input[1]); ok || got != "" {
			t.Fatalf("movieSTRMPath(%q, %q) = %q, %v; want invalid", input[0], input[1], got, ok)
		}
	}
}
