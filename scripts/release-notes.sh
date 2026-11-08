#!/usr/bin/env bash
# Prints the CHANGELOG.md section for a tag (e.g. v0.1.0 -> the "## 0.1.0 ..." section) so GitHub release notes
# are the same text as the changelog. Fails if the section is missing or empty.
set -euo pipefail
tag="${1:?usage: release-notes.sh vX.Y.Z}"
ver="${tag#v}"
notes="$(awk -v ver="$ver" '
  /^## / { if (on) exit; on = ($2 == ver) ; next }
  on { print }
' "$(dirname "$0")/../CHANGELOG.md")"
if [ -z "$(printf '%s' "$notes" | tr -d '[:space:]')" ]; then
  echo "no CHANGELOG.md section for $ver" >&2
  exit 1
fi
printf '%s\n' "$notes"
