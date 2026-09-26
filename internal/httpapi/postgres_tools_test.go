package httpapi

import "testing"

func TestParsePostgresMajor(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    int
		wantErr bool
	}{
		{name: "server version", version: "17.4 (Debian 17.4-1.pgdg12+1)", want: 17},
		{name: "client version", version: "pg_dump (PostgreSQL) 16.9", want: 16},
		{name: "next major beta", version: "18beta1", want: 18},
		{name: "pre major release", version: "9.6.24", wantErr: true},
		{name: "empty", version: "", wantErr: true},
		{name: "invalid", version: "PostgreSQL unknown", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parsePostgresMajor(test.version)
			if (err != nil) != test.wantErr {
				t.Fatalf("parsePostgresMajor(%q) error = %v, wantErr %v", test.version, err, test.wantErr)
			}
			if err == nil && got != test.want {
				t.Fatalf("parsePostgresMajor(%q) = %d, want %d", test.version, got, test.want)
			}
		})
	}
}
