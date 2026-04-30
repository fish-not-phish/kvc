package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fish-not-phish/kvc/internal/compose"
	"github.com/fish-not-phish/kvc/internal/dotenv"
)

// composeCandidates is the search order docker compose itself uses when
// no -f flag is given. Listed canonical-first.
var composeCandidates = []string{
	"compose.yaml",
	"compose.yml",
	"docker-compose.yaml",
	"docker-compose.yml",
}

// resolveComposeFile returns the path to use for the compose file. If the
// user passed an explicit path, it's used as-is (and `os.ReadFile` will
// surface a missing-file error later, which is the right error message).
// Otherwise we walk composeCandidates in cwd and pick the first that exists.
func resolveComposeFile(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	for _, name := range composeCandidates {
		if _, err := os.Stat(name); err == nil {
			return name, nil
		}
	}
	return "", fmt.Errorf("no compose file found in cwd (looked for %s) — pass -f <path>", strings.Join(composeCandidates, ", "))
}

// resolveEnvFile picks the .env path to use. Returns "" if .env handling is
// disabled or auto-detect found nothing. If the user set --env-file
// explicitly and the file is missing, that's an error.
func resolveEnvFile(composeFile, explicit string, disabled bool) (string, error) {
	if disabled {
		return "", nil
	}
	if explicit != "" {
		if _, err := os.Stat(explicit); err != nil {
			return "", fmt.Errorf("--env-file %s: %w", explicit, err)
		}
		return explicit, nil
	}
	candidate := filepath.Join(filepath.Dir(composeFile), ".env")
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}
	return "", nil
}

// loadEnvEntries reads and parses a .env file. Returns nil entries if path
// is empty.
func loadEnvEntries(path string) ([]dotenv.Entry, error) {
	if path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	entries, err := dotenv.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return entries, nil
}

// allPlaceholderSpecs returns the deduped union of placeholder specs across
// the compose YAML and any .env entry values, preserving first-seen order.
func allPlaceholderSpecs(composeYAML []byte, envEntries []dotenv.Entry) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(specs []string) {
		for _, s := range specs {
			if _, dup := seen[s]; dup {
				continue
			}
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	add(compose.FindPlaceholders(composeYAML))
	for _, e := range envEntries {
		add(compose.FindPlaceholders([]byte(e.Value)))
	}
	return out
}

// resolveEnvEntries substitutes placeholders in .env values and returns
// `KEY=VALUE` strings ready to merge into a subprocess env. Any unresolved
// placeholders are reported.
func resolveEnvEntries(entries []dotenv.Entry, secrets map[string]string) (envVars []string, missing []string) {
	seenMissing := map[string]struct{}{}
	for _, e := range entries {
		v, miss := compose.SubstituteValue(e.Value, secrets)
		for _, m := range miss {
			if _, dup := seenMissing[m]; dup {
				continue
			}
			seenMissing[m] = struct{}{}
			missing = append(missing, m)
		}
		envVars = append(envVars, e.Key+"="+v)
	}
	return
}
