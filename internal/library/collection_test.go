package library

import "testing"

func TestCollectionCoverage(t *testing.T) {
	missing, err := MissingBuckets([]string{"a", " n ", "Ñ", "N"})
	if err != nil || len(missing) != 24 {
		t.Fatalf("coverage: %v %v", missing, err)
	}
	for _, bucket := range missing {
		if bucket == "A" || bucket == "N" || bucket == "Ñ" {
			t.Fatalf("linked section reported missing: %s", bucket)
		}
	}
	if _, err := MissingBuckets([]string{"NN"}); err == nil {
		t.Fatal("invalid section accepted")
	}
}

func TestDestinationNames(t *testing.T) {
	for _, tc := range []struct {
		kind, bucket string
		part         int
		want         string
	}{
		{"series", "n", 1, "Series N"},
		{"series", "N", 2, "Series N 2"},
		{"movies", "0-9", 1, "Películas 0-9"},
		{"movies", "OTHER", 1, "Películas Otros"},
	} {
		got, err := DestinationName(tc.kind, tc.bucket, tc.part)
		if err != nil || got != tc.want {
			t.Fatalf("%+v: %q %v", tc, got, err)
		}
	}
	if _, err := DestinationName("unknown", "N", 1); err == nil {
		t.Fatal("invalid kind accepted")
	}
	if _, err := DestinationName("series", "N", 0); err == nil {
		t.Fatal("invalid part accepted")
	}
}

func TestCustomNameFormats(t *testing.T) {
	for _, tc := range []struct {
		pattern, bucket string
		part            int
		want            string
	}{
		{"Series {letra} {parte}", "Ñ", 1, "Series Ñ"},
		{"Series {letra} {parte}", "Ñ", 2, "Series Ñ 2"},
		{"Películas", "", 1, "Películas"},
		{"Películas", "", 2, "Películas 2"},
	} {
		got, err := FormatDestinationName(tc.pattern, tc.bucket, tc.part)
		if err != nil || got != tc.want {
			t.Fatalf("%+v: %q %v", tc, got, err)
		}
	}
	for _, pattern := range []string{"", "{unknown}", "Series {letra", "Name\n{letra}"} {
		if _, err := FormatDestinationName(pattern, "N", 1); err == nil {
			t.Fatalf("accepted %q", pattern)
		}
	}
	if _, err := FormatDestinationName("Series {letra}", "", 1); err == nil {
		t.Fatal("missing bucket accepted")
	}
}

func TestLinkStatesPreserveCopies(t *testing.T) {
	for _, tc := range []struct {
		removed, accessible, destinationRemoved bool
		want                                    string
	}{
		{false, true, false, "linked"},
		{false, false, false, "access_lost"},
		{true, false, false, "source_missing"},
		{false, true, true, "destination_missing"},
	} {
		if got := LinkState(tc.removed, tc.accessible, tc.destinationRemoved); got != tc.want {
			t.Fatalf("%+v: %s", tc, got)
		}
	}
}
