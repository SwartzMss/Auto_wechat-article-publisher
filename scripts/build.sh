#!/usr/bin/env bash
set -euo pipefail

# Build script for Auto WeChat Article Publisher.
# - Checks for Go toolchain.
# - Runs fast compile-time checks via `go test`.
# - Builds frontend (Vite) unless SKIP_WEB=1.
# - Produces a release binary under ./bin (path can be overridden with OUTPUT).

ROOT="$(cd -- "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

log() { printf '[build] %s\n' "$*"; }
fail() { printf '[build] ERROR: %s\n' "$*" >&2; exit 1; }

command -v go >/dev/null 2>&1 || fail "Go is not installed or not in PATH."

# Allow overriding output path/name: OUTPUT=./out/myapp ./scripts/build.sh
OUTPUT="${OUTPUT:-$ROOT/bin/auto-wechat-article-publisher}"
mkdir -p "$(dirname "$OUTPUT")"

log "Using go version: $(go version)"
log "Running compile checks (go test ./...)"
GOFLAGS=${GOFLAGS:-}
go test $GOFLAGS ./...

# Frontend build (optional)
WEB_DIR="$ROOT/server/web"
if [ "${SKIP_WEB:-0}" != "1" ] && [ -f "$WEB_DIR/package.json" ]; then
  command -v npm >/dev/null 2>&1 || fail "npm not found; install Node.js or set SKIP_WEB=1 to skip frontend build."
  log "Installing frontend deps (if needed)"
  (cd "$WEB_DIR" && npm install --no-fund --no-audit)
  log "Building frontend (npm run build)"
  (cd "$WEB_DIR" && npm run build)
else
  log "Skipping frontend build (SKIP_WEB=1 or no package.json)"
fi

log "Building binary -> $OUTPUT"
go build $GOFLAGS -o "$OUTPUT" .

log "Done."
