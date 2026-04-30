package vault

import (
	"strings"
	"testing"
)

func TestParseSpec(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		wantMount string
		wantPath  string
		wantKey   string
		wantErr   string // substring; "" = no error
	}{
		{
			name:      "single-segment path",
			in:        "kv/db#password",
			wantMount: "kv",
			wantPath:  "db",
			wantKey:   "password",
		},
		{
			name:      "multi-segment path",
			in:        "kv/team/svc/db#password",
			wantMount: "kv",
			wantPath:  "team/svc/db",
			wantKey:   "password",
		},
		{
			name:      "key with underscores and digits",
			in:        "kv/myapp/db#PG_PASSWORD_2",
			wantMount: "kv",
			wantPath:  "myapp/db",
			wantKey:   "PG_PASSWORD_2",
		},
		{
			name:    "missing #key is rejected",
			in:      "kv/myapp/db",
			wantErr: "expected <mount>/<path>#<key>",
		},
		{
			name:    "empty key after # is rejected",
			in:      "kv/myapp/db#",
			wantErr: "empty key after #",
		},
		{
			name:    "missing slash is rejected",
			in:      "kv#password",
			wantErr: "expected <mount>/<path>#<key>",
		},
		{
			name:    "trailing slash before # is rejected",
			in:      "kv/#password",
			wantErr: "expected <mount>/<path>#<key>",
		},
		{
			name:    "leading slash is rejected",
			in:      "/db#password",
			wantErr: "expected <mount>/<path>#<key>",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mount, path, key, err := ParseSpec(tc.in)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("error = %v, want to contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if mount != tc.wantMount || path != tc.wantPath || key != tc.wantKey {
				t.Errorf("ParseSpec(%q) = (%q, %q, %q), want (%q, %q, %q)",
					tc.in, mount, path, key, tc.wantMount, tc.wantPath, tc.wantKey)
			}
		})
	}
}
