#!/usr/bin/env sh
# install.sh — build and install kvc from source.
#
# Usage:
#   ./install.sh                       # build from current source tree
#   PREFIX=$HOME/.local ./install.sh   # install somewhere other than /usr/local
#
# Or, with Go already installed and a published module path:
#   go install github.com/fish-not-phish/kvc@latest
set -eu

PREFIX="${PREFIX:-/usr/local}"
BIN="${PREFIX}/bin"
SRC_DIR="$(cd "$(dirname "$0")" && pwd)"

if ! command -v go >/dev/null 2>&1; then
    cat >&2 <<'EOF'
kvc install requires Go 1.22+ to build from source.

Install Go from https://go.dev/dl/ and re-run, or grab a pre-built binary
from the project's releases page.
EOF
    exit 1
fi

cd "$SRC_DIR"

# Resolve deps the first time. Idempotent on subsequent runs.
go mod tidy

# Stamp the version. Prefer the current git tag (so tagged releases show as
# v1.0.0 etc.); fall back to the default baked into main.go.
VERSION="$(git describe --tags --always --dirty 2>/dev/null || true)"
if [ -n "$VERSION" ]; then
    LDFLAGS="-s -w -X main.version=$VERSION"
else
    LDFLAGS="-s -w"
fi

CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o kvc .

mkdir -p "$BIN" 2>/dev/null || true
if [ -w "$BIN" ]; then
    install -m 0755 kvc "$BIN/kvc"
else
    echo "elevating with sudo to install into $BIN..."
    sudo install -m 0755 kvc "$BIN/kvc"
fi

echo "installed: $BIN/kvc"
"$BIN/kvc" --version 2>/dev/null || true
