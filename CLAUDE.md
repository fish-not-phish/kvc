# kvc — Vault-backed secret injection for Docker Compose

## Project Overview

`kvc` is a small Go CLI that injects secrets from a HashiCorp Vault or OpenBao server into a Docker Compose file at deploy time, then runs `docker compose` against it. `kvc` itself never writes plaintext to disk — compose YAML goes to docker via stdin, `.env` values via subprocess env. **Docker, however, persists the resolved env in `/var/lib/docker/containers/<id>/config.v2.json`** (this is where `docker inspect` reads from). The README threat-model section spells this out; do not let that "no temp files" framing confuse the broader picture.

**Target audience:** homelabbers running plain Docker Compose / Docker stacks (not Swarm).

**Why this design exists.** An earlier plan (preserved only in git history) called for a Django + Next.js + Postgres + Celery + Redis web service with its own AES-encrypted secret store and a mounted `/var/run/docker.sock`. For a single-user, single-host home lab that's an order of magnitude more attack surface than the problem warrants — a long-running daemon with full docker access, plus an in-app encryption key sitting next to its ciphertext (encryption theater against any local attacker). A CLI that delegates secret storage to a real vault and only decrypts in-process during `up` inverts the threat model in the user's favor.

## Core Flow

```
1. User runs `kvc up` in a directory with a compose file. If `-f` is not given, kvc searches the cwd in docker-compose's canonical order: `compose.yaml`, `compose.yml`, `docker-compose.yaml`, `docker-compose.yml`.
2. CLI checks the kernel keyring for a cached, unexpired Vault token.
   - If found: use it.
   - If not: prompt for the Vault userpass password, exchange it for a token,
     and (in cached mode) store the token in the keyring with TTL = lease.
3. Read compose.yml. If a sibling `.env` exists (or --env-file is set), read it too.
4. Parse both, find all @@<mount>/<path>#<key>@@ placeholders. Dedupe.
5. For each placeholder, GET <mount>/data/<path> from Vault and pluck the named key (default "value").
6. Substitute compose YAML values in memory. Substitute .env values in memory.
7. Spawn `docker compose --env-file /dev/null -f - up -d`:
   - Compose YAML goes via stdin.
   - Resolved .env values go via subprocess env (overriding any on-disk .env auto-load).
8. Process exits. Plaintext is gone — no temp files, ever.
```

## Tech Stack

- **Language:** Go 1.22+
- **CLI framework:** `cobra`
- **Vault client:** `github.com/hashicorp/vault/api` (works against OpenBao — same API surface)
- **Keyring:** Linux kernel keyring via `keyctl` syscalls (`golang.org/x/sys/unix`). macOS / Windows out of v1 scope.
- **Password prompt:** `golang.org/x/term` (TTY read, no echo, no argv exposure)
- **TOML:** `github.com/BurntSushi/toml`
- **Build artifact:** single static binary (`CGO_ENABLED=0`)

## Authentication

### Userpass

- Username is set once via `kvc init` and saved to `~/.config/kvc/config.toml`.
- Password is prompted on every uncached invocation. Read via TTY, never via argv or env.
- Auth call: `POST /v1/auth/userpass/login/<username>` with `{"password": "<pw>"}` → returns a client token + lease duration.

### Config override flags

`--vault-addr`, `--username`, and `--password-stdin` can be passed to `kvc up` and `kvc check` to override (or replace) values from `~/.config/kvc/config.toml`. Resolution order: CLI flag > config file > prompt.

- `--vault-addr <URL>` — override `vault_addr` from config.
- `--username <name>` — override `username` from config.
- `--password-stdin` — read the password from stdin instead of prompting. Implies `--no-cache` (never writes to the keyring). Intended for CI/CD pipelines that supply the password via a platform secret.

Together these three flags make `kvc` fully operable without a config file on disk, which is the normal state in an ephemeral CI runner:

```sh
echo "$VAULT_PASSWORD" | kvc up --vault-addr "$VAULT_ADDR" --username "$VAULT_USER" --password-stdin --no-cache
```

## Token Caching — Two Modes

Both modes are first-class. The user picks per-invocation or sets a default in config.

### Strict mode (`--no-cache`, or `cache_tokens = false`)

- Re-prompts for the password on every invocation.
- Never reads from or writes to the keyring.
- Most secure, most friction. Treats every deploy as a discrete authenticated event.

### Cached mode (default, `cache_tokens = true`)

- After successful auth, store the Vault token in the **Linux kernel keyring** (`@s` session keyring) with TTL = `min(token_lease, keyring_max_ttl)`.
- On subsequent `up`, look up the keyring entry first; if present and unexpired, skip the password prompt entirely.
- Keyring lives in kernel memory only — never on disk, dies on logout.
- `kvc logout` revokes the token (`POST /v1/auth/token/revoke-self`) and clears the keyring entry.

The `--no-cache` flag overrides `cache_tokens = true`. There is no flag to force caching on when config disables it — that would defeat the point.

**Only the token is ever cached.** Never the password. The token is bounded (TTL, renewable up to max-TTL, revocable from Vault); the password is unbounded.

## Secret Resolution

### Placeholder syntax

In any value position in compose YAML: `"@@<mount>/<path>#<key>@@"` (always double-quoted; single quotes also accepted). The full spec is matched by `[a-zA-Z0-9_-]+(/[a-zA-Z0-9_-]+)+#[a-zA-Z0-9_-]+`.

**Why quotes are canonical.** `@` is a reserved YAML indicator, so a bare unquoted `@@…@@` makes the file invalid YAML at rest. `kvc up` works regardless because it pre-substitutes before piping to docker, but anything else that reads the raw file — `docker compose down -f file`, `docker compose config`, `yamllint`, an IDE — will reject it. The substitution regex still matches both bare and quoted forms, but `kvc check` warns when it finds unquoted placeholders.

- The first slash-segment is the KV v2 mount.
- Everything between that first slash and the `#` is the path under the mount.
- `<key>` (after `#`) is required — every placeholder must name which field of the secret to read.

`"@@kv/db#password@@"` resolves to a GET against `<vault_addr>/v1/kv/data/db`, reading `data.data.password` from the KV v2 response.

There is no `kv_mount` config setting — every placeholder names its own mount. This makes a single compose file capable of pulling from multiple mounts (e.g. a per-stack mount plus a shared-infra mount), and pushes ACL design entirely onto the user, where it belongs.

**Why the explicit `#<key>` requirement.** An earlier draft made `#<key>` optional and defaulted to `value`, supporting two layouts (path-per-secret with `{value: ...}` blobs vs. multi-key paths). The optionality was removed after operational use revealed it caused more confusion than it saved keystrokes — readers couldn't tell at a glance whether `@@kv/foo/bar@@` meant "secret at path `foo/bar` reading default `value` key" or "I forgot to name a key." Forcing every placeholder to spell out the key removes that ambiguity. Operators who want one secret per credential just store one key per path; they still have to name it.

**Why no per-stack `path_prefix`.** An earlier draft used a `kvc.toml` per stack with a `path_prefix` to keep placeholders short. This was dropped because real stacks compose secrets from multiple Vault locations — a stack-specific directory plus shared directories. A single prefix forces either duplication across stack directories (rotation hazard) or a flat layout that loses the per-directory policy boundary. Inline full paths let Vault's layout reflect actual secret scope and let each placeholder name exactly the path it needs.

**Layout.** `kvc` reads KV v2 mounts and addresses each value as `<mount>/<path>#<key>`. The recommended pattern is to group related credentials at one path (e.g. an app's DB user + password + DSN under one secret) so they share a rotation cadence and a single ACL boundary. Operators who prefer one credential per path can do that too — just store one key per path; the placeholder still names the key explicitly.

### .env file resolution

Many compose stacks externalise values into a `.env` file alongside the compose file, and reference them via `${VAR}` interpolation inside compose.yml. `kvc` supports placeholders in both files:

- **Auto-detection**: if a `.env` file exists in the same directory as the compose file, it's read. `--env-file PATH` overrides; `--no-env-file` disables.
- **Resolution path**: `.env` is parsed in-process (`internal/dotenv`), placeholder values are substituted in memory, and the resulting `KEY=VALUE` pairs are placed on the `docker compose` subprocess via `cmd.Env = append(os.Environ(), resolved...)`. Compose's `${VAR}` interpolation reads from process env first, so this overrides any on-disk `.env` auto-load — and we additionally pass `--env-file /dev/null` to the subprocess for explicitness.
- **No round-trip through .env format.** We don't write a substituted `.env` back to disk and we don't try to emit dotenv-quoted values. The values flow as opaque strings into subprocess env. This sidesteps escaping issues (`$`, quotes, newlines, etc.) entirely.

**Caveats** (documented in README, not patched):

- No recursive `${VAR}` expansion within `.env` values. `kvc` substitutes `@@…@@` only; cross-references between `.env` keys (`FOO=${BAR}_x`) pass through verbatim. Move that logic to `compose.yml`'s `environment:` block where docker handles it.
- `env_file:` directive in `compose.yml` is unsupported when the referenced file contains placeholders. Docker reads `env_file:` paths directly from disk into the container's environment, bypassing our subprocess-env interception. Use `${VAR}` interpolation instead.

## Commands (v1 scope)

```
kvc init
  Prompts for vault URL and username. Writes ~/.config/kvc/config.toml.

kvc up [-f compose.yml] [--env-file PATH] [--no-env-file] [--no-cache]
       [--vault-addr URL] [--username NAME] [--password-stdin]
  Auth → fetch secrets → substitute compose + .env → pipe YAML to
  `docker compose --env-file /dev/null -f - up -d` with resolved env on the subprocess.

kvc down [-f compose.yml]
  Runs `docker compose down`. No secret fetching needed.

kvc check [-f compose.yml] [--env-file PATH] [--no-env-file]
          [--vault-addr URL] [--username NAME] [--password-stdin]
  Scans compose.yml AND the resolved .env (if present) for every
  @@<mount>/<path>#<key>@@ placeholder, GETs each from Vault, reports
  which are missing or unreadable. No deploy.

kvc logout
  Revokes the cached token (server-side) and clears the keyring entry.
```

**Out of v1 scope** (revisit only if real usage demands it): arbitrary `kvc run`, secret rotation helpers, multi-stack orchestration, non-Vault backends (Bitwarden/Vaultwarden, etc.), macOS / Windows keyring support.

## What we deliberately do NOT do

- **No temp file written by `kvc`.** `docker compose -f - up -d` reads from stdin; `.env` values flow via subprocess env. `kvc` does not write plaintext anywhere on disk. Note: this is narrower than "no plaintext on disk anywhere" — Docker persists the container's env array in `/var/lib/docker/containers/<id>/config.v2.json` independently of how it was passed in.
- **No password caching.** Ever. Only the bounded, revocable Vault token is cached.
- **No password in argv or env.** TTY read or `--password-stdin` only. Never via argv flags or environment variables.
- **No long-running daemon.** No socket exposure, no API surface, no RBAC layer to maintain.

## Known residual risks (must be in README)

- **`docker inspect` exposes env.** Once secrets land in a container's environment, anyone in the host's `docker` group can read them via `docker inspect`. Fixing this requires Swarm-mode `docker secret`s (mounted as files in `/run/secrets/`), which the target audience doesn't run. Users who need this should use Swarm.
- **Bootstrap problem.** If Vault/OpenBao is itself deployed via Docker Compose on the same host, `kvc` cannot deploy *it* (chicken/egg). Vault's own compose stack must use a plain `.env` file. Document loudly.
- **Application-level leaks.** If a containerized app prints its env at startup or in error messages, secrets end up in `docker logs`. `kvc` cannot fix this.
- **Process memory.** Plaintext exists in the `kvc` process and (briefly) in the `docker compose` process during stack-up. We zero the password buffer, the secrets map values, and the rendered YAML buffer after use, but a root-on-host attacker with `/proc` access can still read it during the deploy window. (No `mlock` today — the Go runtime moves objects, so locking pages is a partial measure at best; revisit only if a concrete threat warrants it.)

## Configuration files

Single global file. No per-stack config — all stack-specific information lives in `compose.yml` placeholders.

### Global: `~/.config/kvc/config.toml`

```toml
vault_addr = "https://vault.example.com:8200"
username = "<your username>"
cache_tokens = true       # default. set false for strict-by-default mode.
keyring_max_ttl = "8h"    # cap on cached token TTL regardless of vault lease.
```

The KV mount is not set here — it's named per-placeholder in the compose file, so one host can deploy stacks that read from any mix of mounts.

## File Structure (target)

```
/
├── CLAUDE.md
├── README.md
├── go.mod
├── go.sum
├── main.go
├── cmd/
│   ├── init.go
│   ├── up.go
│   ├── down.go
│   ├── check.go
│   └── logout.go
└── internal/
    ├── config/        # global TOML loading (~/.config/kvc/config.toml)
    ├── vault/         # client wrapper, userpass auth, KV v2 reads, revoke
    ├── keyring/       # linux keyctl wrapper
    ├── compose/       # placeholder regex + YAML substitution + .env value substitution
    ├── dotenv/        # minimal KEY=VALUE .env parser (no ${} expansion)
    └── tty/           # password prompts via x/term
```

A `test/` directory holds a smoketest `compose.yml` that exercises the placeholder pipeline end-to-end without depending on any production stack.

## Implementation Order

1. `internal/config` — load global config (`~/.config/kvc/config.toml`).
2. `internal/vault` — userpass auth, KV v2 read, token self-revoke.
3. `internal/compose` — placeholder regex + substitution against a `map[string]string` keyed by full placeholder spec (`<mount>/<path>[#<key>]`).
4. `cmd/check` — fastest end-to-end loop without involving docker; great smoke test.
5. `cmd/up` — wire the stdin pipe to `docker compose -f -`.
6. `cmd/init`, `cmd/down`.
7. `internal/keyring` — kernel keyring cache (linux keyctl).
8. `cmd/logout` — token revoke + keyring clear.
9. README with explicit threat model and bootstrap caveats.
