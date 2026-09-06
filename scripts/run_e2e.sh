#!/usr/bin/env bash
# Run the atago end-to-end suite against a freshly built jz.
#
# The binary is placed first on PATH inside a throw-away sandbox whose
# HOME, XDG and cache directories point into the sandbox, so the suite can
# never read or write the developer's real registries.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

if ! command -v atago >/dev/null 2>&1; then
  echo "atago is not installed: go install github.com/nao1215/atago@latest" >&2
  exit 127
fi

sandbox="$(mktemp -d)"
trap 'rm -rf "$sandbox"' EXIT
mkdir -p "$sandbox/bin" "$sandbox/home"

if [ -n "${COVER:-}" ]; then
  mkdir -p "$COVER"
  go build -cover -covermode=atomic -coverpkg=./... -o "$sandbox/bin/jz" ./cmd/jz
  export GOCOVERDIR="$COVER"
else
  go build -o "$sandbox/bin/jz" ./cmd/jz
fi

export PATH="$sandbox/bin:$PATH"
export HOME="$sandbox/home"
export USERPROFILE="$sandbox/home"
export XDG_CONFIG_HOME="$sandbox/home/.config"
export XDG_CACHE_HOME="$sandbox/home/.cache"
unset JSONIZE_REGISTRY_PATH

specs=(e2e/atago/*.atago.yaml)
exec atago run --ci --retry-failed 0 "$@" "${specs[@]}"
