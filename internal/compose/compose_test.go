package compose

import (
	"reflect"
	"strings"
	"testing"
)

func TestFindPlaceholders(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "valid quoted",
			in:   `KEY: "@@kv/myapp/db#password@@"`,
			want: []string{"kv/myapp/db#password"},
		},
		{
			name: "valid single-quoted",
			in:   `KEY: '@@kv/myapp/db#password@@'`,
			want: []string{"kv/myapp/db#password"},
		},
		{
			name: "valid unquoted (still matches; check warns)",
			in:   `KEY: @@kv/myapp/db#password@@`,
			want: []string{"kv/myapp/db#password"},
		},
		{
			name: "missing #key is rejected",
			in:   `KEY: "@@kv/myapp/db@@"`,
			want: nil,
		},
		{
			name: "missing path segment is rejected",
			in:   `KEY: "@@kv#password@@"`,
			want: nil,
		},
		{
			name: "template angle-brackets do not match",
			in:   `KEY: "@@<mount>/<path>#<key>@@"`,
			want: nil,
		},
		{
			name: "multi-segment path",
			in:   `KEY: "@@kv/team/svc/db#password@@"`,
			want: []string{"kv/team/svc/db#password"},
		},
		{
			name: "dedupes repeats",
			in:   "A: \"@@kv/x/y#z@@\"\nB: \"@@kv/x/y#z@@\"\n",
			want: []string{"kv/x/y#z"},
		},
		{
			name: "multiple distinct",
			in:   "A: \"@@kv/x/y#z@@\"\nB: \"@@kv/a/b#c@@\"\n",
			want: []string{"kv/x/y#z", "kv/a/b#c"},
		},
		{
			name: "no placeholders",
			in:   "KEY: hello\nOTHER: world\n",
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := FindPlaceholders([]byte(tc.in))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("FindPlaceholders(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestFindUnquoted(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "double-quoted is fine",
			in:   `KEY: "@@kv/x/y#z@@"`,
			want: nil,
		},
		{
			name: "single-quoted is fine",
			in:   `KEY: '@@kv/x/y#z@@'`,
			want: nil,
		},
		{
			name: "bare placeholder is flagged",
			in:   `KEY: @@kv/x/y#z@@`,
			want: []string{"kv/x/y#z"},
		},
		{
			name: "mix: only bare flagged, dedupes",
			in:   "A: @@kv/x/y#z@@\nB: \"@@kv/x/y#z@@\"\nC: @@kv/a/b#c@@\n",
			want: []string{"kv/x/y#z", "kv/a/b#c"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := FindUnquoted([]byte(tc.in))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("FindUnquoted(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestSubstitute(t *testing.T) {
	secrets := map[string]string{
		"kv/x/y#z":   "the-secret",
		"kv/a/b#c":   `quotes " and \ slash`,
		"kv/m/n#nl":  "with\nnewline",
	}

	t.Run("replaces and YAML-quotes", func(t *testing.T) {
		in := []byte(`KEY: "@@kv/x/y#z@@"` + "\n")
		out, missing := Substitute(in, secrets)
		want := `KEY: "the-secret"` + "\n"
		if string(out) != want {
			t.Errorf("got %q, want %q", string(out), want)
		}
		if len(missing) != 0 {
			t.Errorf("missing = %v, want empty", missing)
		}
	})

	t.Run("escapes special chars in YAML output", func(t *testing.T) {
		in := []byte(`KEY: "@@kv/a/b#c@@"`)
		out, _ := Substitute(in, secrets)
		// Must escape both " and \ so the resulting YAML is parseable.
		got := string(out)
		if !strings.Contains(got, `\"`) || !strings.Contains(got, `\\`) {
			t.Errorf("expected escaped quote and backslash in %q", got)
		}
	})

	t.Run("escapes newline in YAML output", func(t *testing.T) {
		in := []byte(`KEY: "@@kv/m/n#nl@@"`)
		out, _ := Substitute(in, secrets)
		// Newline must become \n inside the rendered scalar — a literal
		// newline would terminate the YAML value.
		if strings.Contains(string(out), "\nnewline") {
			t.Errorf("literal newline leaked into YAML: %q", string(out))
		}
		if !strings.Contains(string(out), `\n`) {
			t.Errorf("expected escaped \\n in %q", string(out))
		}
	})

	t.Run("missing placeholders are reported and left in place", func(t *testing.T) {
		in := []byte(`KEY: "@@kv/missing/x#k@@"`)
		out, missing := Substitute(in, secrets)
		if string(out) != string(in) {
			t.Errorf("expected input unchanged when secret missing, got %q", string(out))
		}
		if len(missing) != 1 || missing[0] != "kv/missing/x#k" {
			t.Errorf("missing = %v, want [kv/missing/x#k]", missing)
		}
	})
}

func TestSubstituteValue(t *testing.T) {
	secrets := map[string]string{
		"kv/x/y#z": "raw value with $pecial chars",
	}

	t.Run("raw substitution, no YAML quoting", func(t *testing.T) {
		got, missing := SubstituteValue(`@@kv/x/y#z@@`, secrets)
		if got != "raw value with $pecial chars" {
			t.Errorf("got %q, want raw value", got)
		}
		if len(missing) != 0 {
			t.Errorf("missing = %v, want empty", missing)
		}
	})

	t.Run("substring placeholder also substitutes", func(t *testing.T) {
		got, _ := SubstituteValue(`prefix-@@kv/x/y#z@@-suffix`, secrets)
		want := `prefix-raw value with $pecial chars-suffix`
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("missing reported", func(t *testing.T) {
		got, missing := SubstituteValue(`@@kv/missing#k@@`, secrets)
		if got != `@@kv/missing#k@@` {
			t.Errorf("expected unchanged, got %q", got)
		}
		if len(missing) != 1 || missing[0] != "kv/missing#k" {
			t.Errorf("missing = %v, want [kv/missing#k]", missing)
		}
	})
}
