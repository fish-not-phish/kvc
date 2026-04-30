// Package dotenv parses simple KEY=VALUE .env files for kvc.
//
// Scope is deliberately minimal: enough to find placeholders, substitute
// them, and pass values to `docker compose` via subprocess env. We do NOT
// implement docker compose's full .env semantics — in particular, no
// recursive ${VAR} expansion within values. If you need cross-referenced
// .env values, use compose.yml's `environment:` with ${VAR} interpolation,
// where docker handles it natively.
package dotenv

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"
)

type Entry struct {
	Key   string
	Value string
	Line  int // 1-based source line; useful for error reporting
}

func Parse(b []byte) ([]Entry, error) {
	var out []Entry
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	line := 0
	for sc.Scan() {
		line++
		raw := sc.Text()
		s := strings.TrimSpace(raw)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		s = strings.TrimPrefix(s, "export ")
		eq := strings.Index(s, "=")
		if eq < 0 {
			return nil, fmt.Errorf("line %d: missing '=' in %q", line, raw)
		}
		key := strings.TrimSpace(s[:eq])
		if !validKey(key) {
			return nil, fmt.Errorf("line %d: invalid key %q", line, key)
		}
		val := strings.TrimSpace(s[eq+1:])
		if len(val) >= 2 {
			f, l := val[0], val[len(val)-1]
			if (f == '"' && l == '"') || (f == '\'' && l == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		out = append(out, Entry{Key: key, Value: val, Line: line})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func validKey(k string) bool {
	if k == "" {
		return false
	}
	for i, r := range k {
		switch {
		case r == '_':
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case i > 0 && r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}
