#!/usr/bin/env bash
# Print the CHANGELOG.md section of one version, for the release notes.
#
#   scripts/release_notes.sh v0.1.0
#
# The section runs from "## [0.1.0]" to the next "## " heading. A tag
# with no section, or with an empty one, fails, so a release cannot go
# out with notes nobody wrote.
set -euo pipefail

if [ "$#" -ne 1 ]; then
  echo "usage: $0 vX.Y.Z" >&2
  exit 2
fi
version="${1#v}"
root="$(cd "$(dirname "$0")/.." && pwd)"

notes="$(awk -v heading="## [${version}]" '
  index($0, heading) == 1 { found = 1; next }
  found && /^## / { exit }
  found { print }
' "$root/CHANGELOG.md" | sed -e '/./,$!d')"

if [ -z "$(printf '%s' "$notes" | tr -d '[:space:]')" ]; then
  echo "CHANGELOG.md has no notes under ## [${version}]" >&2
  exit 1
fi
printf '%s\n' "$notes"
