#!/usr/bin/env bash
# Run the atago end-to-end suite against a freshly built jz.
#
# The binary is placed first on PATH inside a throw-away sandbox whose
# HOME, XDG and cache directories point into the sandbox, so the suite can
# never read or write the developer's real registries. Every spec under
# e2e/atago runs on every operating system; a scenario that belongs to
# one of them says so with only/skip, which is how a new spec is on
# Windows from the day it is added.
#
# The report is TAP, one line per scenario, so the log says which
# scenarios were skipped and why. A skip is allowed for one of two
# reasons: the operating system, or a suite that is optional by name
# (its name ends in "(optional)": the real-hardware scenarios). Any other
# skip, such as a missing command, fails the run: a required scenario
# that was not run has verified nothing. Set E2E_REPORT=console for
# atago's own progress output while working on a scenario; the skip
# check is then not applied.
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

exe="$(go env GOEXE)"
if [ -n "${COVER:-}" ]; then
  mkdir -p "$COVER"
  go build -cover -covermode=atomic -coverpkg=./... -o "$sandbox/bin/jz$exe" ./cmd/jz
  export GOCOVERDIR="$COVER"
else
  go build -o "$sandbox/bin/jz$exe" ./cmd/jz
fi

# The producer, reader, server and stand-in the suites run. It is built
# here rather than in four suite setups so that one build serves them
# all, and so that the file exists, and has been through whatever the
# system does to a new executable, before any suite starts: on Windows a
# binary run in the same breath as the build that wrote it has twice
# been reported as not found. Running it once proves it can be.
go build -o "$sandbox/bin/e2ehelper$exe" ./e2e/atago/e2ehelper
"$sandbox/bin/e2ehelper$exe" relay < /dev/null > /dev/null

export PATH="$sandbox/bin:$PATH"
export HOME="$sandbox/home"
export USERPROFILE="$sandbox/home"
export APPDATA="$sandbox/home/AppData/Roaming"
export LOCALAPPDATA="$sandbox/home/AppData/Local"
export XDG_CONFIG_HOME="$sandbox/home/.config"
export XDG_CACHE_HOME="$sandbox/home/.cache"
unset JSONIZE_REGISTRY_PATH

specs=(e2e/atago/*.atago.yaml)
report="${E2E_REPORT:-tap}"
if [ "$report" != tap ]; then
  exec atago run --ci --retry-failed 0 --report "$report" "$@" "${specs[@]}"
fi

tap="$sandbox/report.tap"
status=0
atago run --ci --retry-failed 0 --report tap "$@" "${specs[@]}" | tee "$tap" || status=$?

# Every skip, with its reason, and the ones that are not allowed.
skipped="$(grep -E '^ok [0-9]+ - .* # SKIP ' "$tap" || true)"
if [ -n "$skipped" ]; then
  echo
  echo "skipped:"
  echo "$skipped" | sed -E 's/^ok [0-9]+ - /  /; s/ # SKIP / -- /'
fi
unexpected="$(echo "$skipped" | grep -vE '# SKIP (only on os=|skip on os=)' | grep -vE '^ok [0-9]+ - [^/]*\(optional\) / ' || true)"
if [ -n "$unexpected" ]; then
  echo
  echo "required scenarios were skipped; install what they need or gate them on the operating system:" >&2
  echo "$unexpected" | sed -E 's/^ok [0-9]+ - /  /' >&2
  status=1
fi
exit "$status"
