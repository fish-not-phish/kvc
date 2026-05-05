# Contributing to kvc

Thanks for your interest. `kvc` is small on purpose. Every feature
expands the attack surface of a security tool, so the bar for additions
is high. This document describes what to expect.

## Before you start

- **Open an issue first** for anything beyond a small bug fix or doc tweak.
  Discussing the design before code lands saves you and the maintainer time
  if the change conflicts with the project's threat model or scope.
- **Read [`CLAUDE.md`](./CLAUDE.md)** for the project's design intent,
  threat model, and the things we deliberately do NOT do. Several common
  feature requests (a long-running daemon, password caching, mounting the
  docker socket, automatic secret rotation) are out of scope by design.
- **Read the README's threat model** so you understand what `kvc`
  protects against and what it doesn't. PRs that broaden the security
  claims without expanding the actual protection won't be merged.

## What's in scope

- Bug fixes, especially in placeholder parsing, Vault auth, keyring
  handling, or subprocess invocation.
- Documentation improvements: clearer examples, fixed typos, better
  threat-model framing.
- Security hardening that doesn't add operational complexity (e.g.
  zeroing more buffers, tightening regex, refusing footgun configurations).
- Tests. The project is light on tests; well-scoped unit tests for
  `internal/compose`, `internal/dotenv`, and `internal/vault.ParseSpec`
  are especially welcome.

## What's out of scope

- Multi-platform support (macOS / Windows). Linux kernel keyring is
  load-bearing; cross-platform alternatives exist but bring their own
  threat-model questions.
- New authentication backends beyond `userpass` and raw token. AppRole,
  OIDC, etc. can be added if there's demand, but each adds surface area.
- A daemon or server mode of any kind.
- A web UI.
- Non-Vault backends (Bitwarden, sealed-secrets, etc.). `kvc` is a
  Vault/OpenBao tool by design.
- Anything that requires mounting `/var/run/docker.sock`.

## Development setup

```sh
git clone https://github.com/fish-not-phish/kvc.git kvc && cd kvc
go build ./...
go vet ./...
go test ./...
```

The smoketest fixture in `test/` expects a Vault/OpenBao instance with
KV v2 paths matching the placeholders. For local development against a
real Vault, copy `test/docker-compose.yml` and `test/.env.example` somewhere else
and substitute the template placeholders (`<mount>/<path>#<key>`) with
real Vault paths you have read access to.

## Style

- Match the surrounding code. The project favors small, focused functions
  over abstractions.
- No new dependencies without a clear reason. Each dep is more code on
  the trust path.
- Comments explain *why*, not *what*. Avoid restating the code in prose.

## Submitting a PR

- Keep changes focused. One concern per PR.
- Update relevant docs (README, CLAUDE.md, SECURITY.md) if your change
  affects user-facing behavior or the threat model.
- Don't bump the version yourself. Releases are tagged by the maintainer.

## Reporting security issues

**Don't open a public PR or issue for security vulnerabilities.** See
[`SECURITY.md`](./SECURITY.md) for the disclosure process.
