package httpapi

import "testing"

func TestParseSingleRange(t *testing.T) {
	tests := []struct {
		name, header     string
		size, start, end int64
		invalid          bool
	}{
		{"first", "bytes=0-99", 1000, 0, 99, false},
		{"open", "bytes=100-", 2_000_000, 100, 1_999_999, false},
		{"suffix", "bytes=-100", 1000, 900, 999, false},
		{"clamped", "bytes=900-2000", 1000, 900, 999, false},
		{"multiple", "bytes=0-1,3-4", 1000, 0, 0, true},
		{"past end", "bytes=1000-", 1000, 0, 0, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			start, end, err := parseSingleRange(test.header, test.size)
			if (err != nil) != test.invalid || (!test.invalid && (start != test.start || end != test.end)) {
				t.Fatalf("got %d-%d err=%v", start, end, err)
			}
		})
	}
}

func TestStreamRangeWindow(t *testing.T) {
	start, end, err := parseSingleRange("bytes=1000-", 100*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	end = min(end, start+streamHTTPRangeWindow-1)
	if got := end - start + 1; got != streamHTTPRangeWindow {
		t.Fatalf("unexpected bounded range size: %d", got)
	}

	start, end, err = parseSingleRange("bytes=1000-1999", 100*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	end = min(end, start+streamHTTPRangeWindow-1)
	if start != 1000 || end != 1999 {
		t.Fatalf("small explicit range changed: %d-%d", start, end)
	}
}
