package compose

import (
	"fmt"
	"regexp"
	"strings"
)

// Match @@<mount>/<path>[#<key>]@@, optionally already wrapped in single or
// double quotes. We replace the entire match (including any quotes) with a
// freshly emitted YAML double-quoted scalar so the output is always valid YAML
// in any value position.
//
// The captured spec is opaque to this package; vault.Get parses it into
// (mount, path, key) at fetch time.
var specPattern = `[a-zA-Z0-9_-]+(?:/[a-zA-Z0-9_-]+)+#[a-zA-Z0-9_-]+`
var placeholderRE = regexp.MustCompile(`(?:"@@(` + specPattern + `)@@"|'@@(` + specPattern + `)@@'|@@(` + specPattern + `)@@)`)

func FindPlaceholders(yaml []byte) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, m := range placeholderRE.FindAllSubmatch(yaml, -1) {
		p := pickPath(m)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

// FindUnquoted returns the deduped specs of placeholders that appear without
// surrounding YAML quotes. Unquoted `@@…@@` is invalid YAML on its own (`@` is
// a reserved indicator), so any tool that reads the file directly — `docker
// compose config`, `yamllint`, an IDE — will reject it. `kvc up` works
// only because it pre-substitutes before handing the YAML to docker.
func FindUnquoted(yaml []byte) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, m := range placeholderRE.FindAllSubmatch(yaml, -1) {
		// submatch index 3 is the bare (unquoted) alternative in the regex.
		if len(m[3]) == 0 {
			continue
		}
		p := string(m[3])
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

// Substitute replaces every placeholder match with a YAML-quoted secret.
// Placeholders whose paths are missing from `secrets` are left in place and
// returned in `missing` (deduplicated, in first-seen order).
func Substitute(yaml []byte, secrets map[string]string) (out []byte, missing []string) {
	seenMissing := map[string]struct{}{}
	out = placeholderRE.ReplaceAllFunc(yaml, func(match []byte) []byte {
		sub := placeholderRE.FindSubmatch(match)
		p := pickPath(sub)
		v, ok := secrets[p]
		if !ok {
			if _, dup := seenMissing[p]; !dup {
				seenMissing[p] = struct{}{}
				missing = append(missing, p)
			}
			return match
		}
		return []byte(yamlDoubleQuote(v))
	})
	return
}

func pickPath(submatches [][]byte) string {
	for i := 1; i < len(submatches); i++ {
		if len(submatches[i]) > 0 {
			return string(submatches[i])
		}
	}
	return ""
}

// SubstituteValue replaces @@<spec>@@ placeholders in a single string with
// the corresponding secret value, raw — no YAML quoting. Used for .env
// values that go directly into subprocess env (where the value is just an
// opaque string, not YAML).
func SubstituteValue(s string, secrets map[string]string) (out string, missing []string) {
	seenMissing := map[string]struct{}{}
	out = placeholderRE.ReplaceAllStringFunc(s, func(match string) string {
		sub := placeholderRE.FindStringSubmatch(match)
		p := pickPathStr(sub)
		v, ok := secrets[p]
		if !ok {
			if _, dup := seenMissing[p]; !dup {
				seenMissing[p] = struct{}{}
				missing = append(missing, p)
			}
			return match
		}
		return v
	})
	return
}

func pickPathStr(submatches []string) string {
	for i := 1; i < len(submatches); i++ {
		if submatches[i] != "" {
			return submatches[i]
		}
	}
	return ""
}

func yamlDoubleQuote(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\x%02x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
