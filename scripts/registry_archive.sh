#!/usr/bin/env bash
# Package the official registry directory as a reproducible tar.gz plus a
# SHA-256 file, in the layout `jz registry update` expects.
set -euo pipefail

out="${1:-dist}"
root="$(cd "$(dirname "$0")/.." && pwd)"
mkdir -p "$out"
archive="$out/jsonize-registry.tar.gz"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

mkdir -p "$tmp/jsonize-registry"
cp "$root/registry/registry.yaml" "$tmp/jsonize-registry/"
cp -R "$root/registry/parsers" "$tmp/jsonize-registry/"

# Deterministic archive: fixed mtime, sorted members, no owner info.
tar --sort=name --mtime='2000-01-01 00:00:00Z' --owner=0 --group=0 --numeric-owner \
  -C "$tmp" -cf - jsonize-registry | gzip -n > "$archive"
(cd "$out" && sha256sum jsonize-registry.tar.gz > jsonize-registry.tar.gz.sha256)
echo "wrote $archive and $archive.sha256"
