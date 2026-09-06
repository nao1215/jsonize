#!/usr/bin/env bash
# Merge unit-test coverage with coverage collected from the atago E2E run
# (binary built with -cover) into a single cover.out.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
unit="$(mktemp -d)"
e2e="$(mktemp -d)"
trap 'rm -rf "$unit" "$e2e"' EXIT
go test -cover -coverpkg=./... ./... -args -test.gocoverdir="$unit"
COVER="$e2e" bash ./scripts/run_e2e.sh
go tool covdata textfmt -i="$unit,$e2e" -o cover.out
go tool cover -func=cover.out | tail -1
