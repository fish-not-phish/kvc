# Security Policy

`kvc` is a security tool. We take vulnerability reports seriously and
welcome responsible disclosure.

## Reporting a vulnerability

**Do not open a public GitHub issue for security vulnerabilities.**

Instead, email the maintainer directly. Include:

- A clear description of the issue and its impact.
- Steps to reproduce (or a proof-of-concept if you have one).
- The version of `kvc` you tested against (`kvc --version`).
- The OS, Vault/OpenBao version, and Docker Compose version in use.

You should expect an acknowledgement within **7 days**. If the issue is
confirmed, we'll work with you on a coordinated disclosure timeline —
usually 30–90 days depending on severity.

## Scope

In scope:

- The `kvc` binary itself (CLI, secret resolution, keyring handling,
  subprocess invocation of `docker compose`).
- The shipped configuration loader and TOML parsing.

Out of scope (these are documented residual risks in the README's threat
model — see "What Docker does on its own"):

- `docker inspect` exposing container env (Docker behavior, not `kvc`).
- Plaintext in `/var/lib/docker/containers/<id>/config.v2.json` (same).
- Application-level leaks (a containerized app printing its env).
- Vault/OpenBao server bugs — report those upstream.

## What this tool does and doesn't promise

`kvc` itself never writes plaintext to disk: compose YAML goes to
`docker compose` via stdin, `.env` values via subprocess environment, and
the Vault token lives only in the Linux kernel keyring. Once values reach
Docker, plaintext **does** persist in Docker's container config — this is
the documented limitation, not a vulnerability. See the README's threat
model for the full picture.

## Stability

`kvc` is currently at **v1.0** — the API and config schema are
considered stable, but `kvc` is a small project with limited resources.
Treat findings against `kvc` itself as in-scope; findings about Docker,
Vault, or OpenBao should be reported to those projects.
