package dotenv

import (
	"reflect"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    []Entry
		wantErr string // substring of expected error; "" = no error
	}{
		{
			name: "simple KEY=VALUE",
			in:   "FOO=bar\n",
			want: []Entry{{Key: "FOO", Value: "bar", Line: 1}},
		},
		{
			name: "blank lines and comments skipped",
			in:   "\n# this is a comment\n   # indented comment\nFOO=bar\n\nBAZ=qux\n",
			want: []Entry{
				{Key: "FOO", Value: "bar", Line: 4},
				{Key: "BAZ", Value: "qux", Line: 6},
			},
		},
		{
			name: "double-quoted strips quotes",
			in:   `FOO="hello world"` + "\n",
			want: []Entry{{Key: "FOO", Value: "hello world", Line: 1}},
		},
		{
			name: "single-quoted strips quotes",
			in:   `FOO='hello world'` + "\n",
			want: []Entry{{Key: "FOO", Value: "hello world", Line: 1}},
		},
		{
			name: "mismatched quotes left intact",
			in:   `FOO="not closed` + "\n",
			want: []Entry{{Key: "FOO", Value: `"not closed`, Line: 1}},
		},
		{
			name: "export prefix tolerated",
			in:   "export FOO=bar\n",
			want: []Entry{{Key: "FOO", Value: "bar", Line: 1}},
		},
		{
			name: "value containing # is preserved verbatim",
			in:   "FOO=hello#world\n",
			want: []Entry{{Key: "FOO", Value: "hello#world", Line: 1}},
		},
		{
			name: "value containing = is preserved",
			in:   "DSN=postgres://user:pass=word@host/db\n",
			want: []Entry{{Key: "DSN", Value: "postgres://user:pass=word@host/db", Line: 1}},
		},
		{
			name: "placeholder value passes through unchanged",
			in:   "PASSWORD=@@kv/myapp/db#password@@\n",
			want: []Entry{{Key: "PASSWORD", Value: "@@kv/myapp/db#password@@", Line: 1}},
		},
		{
			name: "trailing whitespace trimmed on unquoted",
			in:   "FOO=bar   \n",
			want: []Entry{{Key: "FOO", Value: "bar", Line: 1}},
		},
		{
			name: "empty value allowed",
			in:   "FOO=\n",
			want: []Entry{{Key: "FOO", Value: "", Line: 1}},
		},
		{
			name:    "missing equals errors",
			in:      "FOO\n",
			wantErr: "missing '='",
		},
		{
			name:    "invalid key with leading digit errors",
			in:      "1FOO=bar\n",
			wantErr: "invalid key",
		},
		{
			name:    "invalid key with dash errors",
			in:      "FOO-BAR=baz\n",
			wantErr: "invalid key",
		},
		{
			name:    "invalid empty key errors",
			in:      "=bar\n",
			wantErr: "invalid key",
		},
		{
			name: "key with underscores and digits ok",
			in:   "FOO_BAR_2=ok\n",
			want: []Entry{{Key: "FOO_BAR_2", Value: "ok", Line: 1}},
		},
		{
			name: "preserves declaration order",
			in:   "C=3\nA=1\nB=2\n",
			want: []Entry{
				{Key: "C", Value: "3", Line: 1},
				{Key: "A", Value: "1", Line: 2},
				{Key: "B", Value: "2", Line: 3},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse([]byte(tc.in))
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
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Parse(%q)\n  got  %+v\n  want %+v", tc.in, got, tc.want)
			}
		})
	}
}
