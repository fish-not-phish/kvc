# kvc

Vault-backed secret injection for Docker Compose. `kvc` itself never
writes plaintext to disk. It pipes the resolved compose YAML to
`docker compose` via stdin and passes `.env` values through subprocess
environment. Once values reach Docker, plaintext **does** land on disk
in `/var/lib/docker/containers/<id>/config.v2.json` (the same place
`docker inspect` reads from); see the [threat model](#threat-model) for the full picture.

## Why kvc?

The main threat kvc defends against is **plaintext secrets sitting on
disk at rest**.

In a typical Docker Compose setup, credentials live in plaintext in
compose YAML and (more commonly) a sibling `.env` file. Those files
get backed up, synced to cloud drives, accidentally committed to git,
included in disk images, and (most acutely) scraped by info-stealing
malware that specifically hunts for files named `.env`. Once
exfiltrated, every credential in the file is compromised, and rotating
across many stacks is painful and incomplete.

With kvc, those files contain only placeholders like
`"@@kv/myapp/db#password@@"`. Plaintext exists only in Vault (encrypted
at rest, audited, ACL'd), briefly in the `kvc` and `docker compose`
processes during `up`, and (once the stack is running) in Docker's
own container state files (see [threat model](#threat-model)).

Concretely, this defeats:

- **Info-stealer malware exfiltrating `.env` files.** RedLine, Lumma,
  Stealc and similar families specifically grep for `.env` files and
  ship them off. With kvc, what they ship is a list of `@@…@@`
  strings, useless without your Vault credentials.
- **Accidental git commits.** A `.env` or compose file pushed to a
  public repo (one of the leading sources of credential leaks on
  GitHub) leaks placeholders, not credentials.
- **Backups, cloud sync, and disk forensics.** `.env` files routinely
  end up in iCloud / Dropbox / Google Drive folders, nightly backup
  tarballs, rsync mirrors, and the disks of stolen or discarded
  laptops. None of that contains plaintext anymore.
- **Misconfigured file permissions.** A `.env` that's mode 644, or
  sitting in a world-readable directory, exposes only placeholders to
  other users on the host.
- **Secret rotation blast radius.** When a credential is compromised,
  you rotate once in Vault and every stack that references it picks up
  the new value on the next `kvc up`. No `find . -name .env | xargs
  sed` across your homelab.
- **Centralized audit trail.** Vault logs every secret read, so you
  have a record of what got pulled, when, and by whom (a Vault feature
  kvc routes through).

**What kvc is not.** Once a stack is running, `docker inspect` shows
the resolved env in plaintext to anyone with docker access. An
attacker already running code as you on this host can use the cached
Vault token, or wait for you to type your password, to resolve
placeholders themselves. kvc raises the bar against *passive disk*
attacks (file copies, backups, malware that scrapes files); it doesn't
replace endpoint security. Read the full [threat model](#threat-model)
before relying on it.

## How it works

```
kvc up
  └── locates the compose file (compose.yaml/.yml or docker-compose.yaml/.yml in cwd; or -f <path>)
  └── reads it (and ./.env next to it, if present)
  └── finds @@<mount>/<path>#<key>@@ placeholders in both
  └── auths to Vault (userpass; token cached in the kernel keyring)
  └── GETs each secret from <mount>/data/<path> and reads the named <key>
  └── substitutes docker-compose.yml values in memory
  └── resolves .env values in memory and exposes them as subprocess env
  └── pipes the substituted YAML to `docker compose --env-file /dev/null -f - up -d`
  └── exits. Plaintext is gone, no temp files.
```

## Install

### From source (requires Go 1.22+)

```sh
git clone https://github.com/fish-not-phish/kvc.git kvc && cd kvc
chmod +x install.sh
./install.sh                          # installs to /usr/local/bin
PREFIX=$HOME/.local ./install.sh      # or somewhere user-writable
```

Or via the Makefile:

```sh
make build       # produces ./kvc
sudo make install
```

### Via `go install`

```sh
go install github.com/fish-not-phish/kvc@latest
```

## First-time setup

```sh
kvc init
# Vault address (e.g. https://vault.example.com:8200): <your vault url>
# Vault userpass username: <your username>
```

This writes `~/.config/kvc/config.toml`:

```toml
vault_addr = "https://vault.example.com:8200"
username = "<your username>"
cache_tokens = true
keyring_max_ttl = "8h"
```

## Writing placeholders

In any value position in your compose file, write
`"@@<mount>/<path>#<key>@@"`:

- `<mount>`: KV v2 mount (the first slash-segment).
- `<path>`: path under that mount (everything between the first slash and `#`).
- `<key>`: which field of the secret to read.

```yaml
services:
  app:
    image: example/app
    environment:
      DB_USER: "@@<mount>/<path>#<key>@@"
      DB_PASSWORD: "@@<mount>/<path>#<key>@@"
      API_TOKEN: "@@<mount>/<path>#<key>@@"
```

The mount is inline, so a single compose file can pull from multiple Vault
mounts. ACL design is your problem; write a policy per mount that grants
the deploying user read on the paths it needs.

**Always quote placeholders.** `@` is a reserved YAML indicator, so a bare
`KEY: @@foo@@` is technically invalid YAML. `kvc up` survives because it
substitutes before handing the file to `docker compose`, but anything else
that reads the raw file (`docker compose down -f file`, `docker compose
config`, `yamllint`, an IDE's YAML check) will reject it. Wrap every
placeholder in double quotes (`"@@foo@@"`); single quotes work too. `kvc
check` warns when it finds unquoted placeholders.

**Placeholders must occupy a whole YAML value.** `password: "@@foo@@"` is
fine. Embedded forms like `args: ["--pwd=@@foo@@"]` are not. Substitution
emits a quoted YAML scalar, so the placeholder needs to be a complete value,
not a substring.

## .env file support

Many compose stacks pull values from a `.env` file via `${VAR}`
interpolation rather than inlining `environment:` in the compose file.
`kvc` handles this:

- By default, if a `.env` file exists in the same directory as the compose
  file, `kvc` reads it.
- Override with `--env-file PATH`. Disable entirely with `--no-env-file`.

### At a glance

| Pattern                                             | Works? |
|-----------------------------------------------------|--------|
| Inline `"@@…@@"` in compose.yml `environment:`      | ✓      |
| Compose `${VAR}` interpolation reading from `.env`  | ✓      |
| Placeholders in `.env` values                       | ✓      |
| Plain (non-secret) values in `.env` passed through  | ✓      |
| `env_file: .env` directive in compose.yml           | ✗      |
| Recursive `${VAR}` expansion within `.env` values   | ✗      |

The `✗` cases are not bugs. They're places where docker compose owns the
parsing path and `kvc` can't intercept it without rewriting the YAML.
Workarounds for each are in the **.env caveats** subsection below.

Placeholders in `.env` values are resolved in memory just like the compose
file, then exposed to `docker compose` as **subprocess environment** so
`${VAR}` interpolation in the YAML picks them up. The on-disk `.env` is
never rewritten; placeholders stay as `@@…@@` at rest.

```
# .env
DB_USER=app
DB_PASSWORD=@@<mount>/<path>#<key>@@
```

```yaml
# docker-compose.yml
services:
  app:
    image: example/app
    environment:
      DB_USER: ${DB_USER}
      DB_PASSWORD: ${DB_PASSWORD}
```

`kvc up` resolves the placeholder, sets `DB_PASSWORD=<secret>` on the
docker subprocess, and compose's `${DB_PASSWORD}` interpolation picks it up.

`kvc check` validates placeholders across both files and reports any
that won't resolve.

### .env caveats

- **No recursive `${VAR}` expansion within `.env` values.** `kvc` parses
  `.env` line-by-line and substitutes `@@…@@` placeholders only. If you
  reference one `.env` key from another (`FOO=${BAR}_x`), that won't expand;
  `kvc` passes the literal string to docker. Move such logic to
  compose.yml's `environment:` instead, where docker handles it.
- **`env_file:` directive in compose.yml is unsupported with placeholders.**
  When compose has `env_file: .env`, docker reads the file directly into
  the container's environment. The subprocess-env trick can't intercept
  it, and the container would receive literal `@@…@@` strings. Use
  `${VAR}` interpolation instead.
- **`kvc` always passes `--env-file /dev/null` to docker compose** so
  docker doesn't auto-load the on-disk `.env` (which still contains
  placeholders). All env vars come from the subprocess env that `kvc`
  controls.

## Commands

```
kvc init                        Set up ~/.config/kvc/config.toml.
kvc up [-f FILE]                Auth, fetch secrets, pipe to `docker compose up -d`.
       [--env-file PATH]              Override .env auto-detection.
       [--no-env-file]                Skip .env auto-detection entirely.
       [--no-cache]                   Skip the kernel-keyring token cache.
       [--token-stdin]                Read a Vault token from stdin instead of prompting.
kvc down [-f FILE]              `docker compose down` (no secrets needed).
kvc check [-f FILE]             Verify every placeholder resolves in Vault. No deploy.
       [--env-file PATH]              Same .env semantics as `up`.
       [--no-env-file]
kvc logout                      Revoke the cached token and clear the keyring entry.
```

If `-f` is omitted, kvc searches the cwd in this order and uses the
first that exists: `compose.yaml`, `compose.yml`, `docker-compose.yaml`,
`docker-compose.yml`. (Same precedence docker compose itself uses.)

## Token caching

By default, after a successful `userpass` login `kvc` stores the Vault
token in the **Linux kernel keyring** (`@s` session keyring) with TTL =
`min(token_lease, keyring_max_ttl)`. Subsequent invocations skip the password
prompt until the entry expires or the session ends.

Set `cache_tokens = false` in the config (or pass `--no-cache`) to re-prompt
on every invocation. Only the bounded, revocable token is ever cached.
**Never the password.**

`kvc logout` revokes the token server-side and removes it from the keyring.

## Threat model

**What `kvc` itself protects:**

- No plaintext written by `kvc`. The resolved compose YAML goes to
  `docker compose -f -` via stdin; resolved `.env` values go via subprocess
  environment. `kvc` itself never writes a temp file.
- No password in argv, env, or shell history. TTY read only.
- Vault token lives in the kernel keyring only (`@s` session keyring),
  never on disk, dies on session end or `kvc logout`.
- No long-running daemon, no docker socket exposure, no API surface.

**What Docker does on its own (and `kvc` can't prevent):**

- **`/var/lib/docker/containers/<id>/config.v2.json` stores the full env
  array in plaintext.** This is where `docker inspect` reads from. Anyone
  with root, anyone in the host's `docker` group, or anyone who can read
  `/var/lib/docker/` sees the secrets. This is plain Docker behavior; the
  only way to keep secrets out of this file is Swarm-mode `docker secret`s
  mounted at `/run/secrets/<name>` (a tmpfs), which the target audience
  doesn't run. If you need this, run Swarm.
- **Container logs (`/var/lib/docker/containers/<id>/<id>-json.log`)**
  capture stdout/stderr. If a containerized app prints its env at startup
  or in error messages, secrets end up there. `kvc` can't fix this.
- **`/proc/<pid>/environ` exposes a running container's env to root on the
  host** for the lifetime of the process.

**Other residual risks:**

- **Bootstrap.** If Vault/OpenBao is itself deployed via Docker Compose on
  this host, `kvc` cannot deploy *it* (chicken/egg). Vault's own compose
  stack must use a plain `.env` file.
- **Process memory.** Plaintext exists briefly in the `kvc` and
  `docker compose` processes during stack-up. We zero the password buffer,
  the secrets map, and the rendered YAML buffer after use, but a
  root-on-host attacker with `/proc` access can read it during the deploy
  window. We don't `mlock`, so under memory pressure secrets could page
  out to swap.
- **Core dumps.** If `kvc`, `docker compose`, or `dockerd` crashes and
  the system is configured to dump core, plaintext can land in
  `/var/lib/systemd/coredump/` (or wherever your system writes cores).
  Disable core dumps for the relevant binaries if this is in your threat
  model.

## Vault layout

`kvc` reads from KV v2 mounts and expects each placeholder to name a
specific key inside a secret:

```
<mount>/<path>           <key1> = "...", <key2> = "...", ...
```

Group related credentials at one path (e.g. an app's DB user + password +
DSN under the same secret) so they rotate together and share one ACL
boundary. If you'd rather have one secret per credential, just store one
key per path. `kvc` doesn't care, but every placeholder still has
to name its key explicitly with `#<key>`.

## Requirements

- Linux (kernel keyring). macOS / Windows are not supported in v1.
- Docker with the Compose v2 plugin (`docker compose`, not `docker-compose`).
- HashiCorp Vault or OpenBao with userpass auth and a KV v2 mount.
