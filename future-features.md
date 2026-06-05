# Future Features

Roughly priority-ordered. v1.1 cluster is items 1–3.

---

## High value / low effort

### ~~1. Shell completions~~ ✓ done
Cobra has `completion` built in. Add `cmd/completion.go` and `kvc completion bash|zsh|fish` just works.

### ~~2. Extra args passthrough to `docker compose up`~~ ✓ done
`kvc up` always runs `up -d`. Users can't pass `--build`, `--force-recreate`, specific service names, etc. Support a `--` separator to forward remaining args to the compose subprocess.

### 3. `kvc run`
Generalize the secret-injection pipeline for arbitrary compose subcommands — `kvc run -- logs -f myservice`, `kvc run -- exec myservice bash`, etc. CLAUDE.md defers this to "if real usage demands it."

### ~~4. Token auth method~~ ✓ done
Add a `--token` flag (or `VAULT_TOKEN` env fallback) to skip the full userpass flow. Useful for testing and scripting without the `--password-stdin` ceremony.

---

## Medium value / moderate effort

### ~~5. `kvc status`~~ ✓ done
Show cached keyring state (token expiry, vault addr it came from), loaded config, and whether Vault is reachable. Users currently have no visibility into cached state without running `keyctl` manually.

### 6. macOS Keychain support
The `internal/keyring` package is the only hard Linux dependency. Swapping in a cross-platform keyring library behind an interface (e.g. `go-keyring`) opens the door for Mac users without changing any surface area.

### ~~7. AppRole auth method~~ ✓ done
Userpass works for humans; AppRole (Role ID + Secret ID) is the right fit for machines and cleaner for CI/CD than echoing a password into stdin.

### 8. KV v2 secret versioning
Vault KV v2 supports pinning to a specific version. A `@@kv/db#password@v3@@` syntax (or a `--pin-versions` flag) would enable controlled rollouts and rollbacks.

---

## Longer term

### 9. Integration test suite
The `test/` directory has a smoketest compose file but no automation. A harness that spins up a dev Vault instance (`vault server -dev`) and runs `kvc check` and `kvc up` end-to-end would catch regressions the unit tests can't reach.

### 10. Bitwarden / Vaultwarden backend
The most-requested alternative among homelabbers who don't want to operate Vault. The placeholder syntax is already backend-agnostic; it's a new `internal/vault` implementation behind an interface.

### 11. Token renewal
A cached token silently becomes unusable if Vault's max TTL expires between deployments. Add `kvc renew` or auto-renewal inside `kvc up` before the deploy to remove this surprise failure mode.
