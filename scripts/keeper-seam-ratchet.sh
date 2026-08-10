#!/usr/bin/env bash
set -euo pipefail

repo_dir="$(cd "$(dirname "$0")/.." && pwd)"
allow="$repo_dir/internal/keeper/legacy_seams.txt"
actual="$(mktemp)"
trap 'rm -f "$actual"' EXIT

awk '
  /^type CyclerConfig struct/ { inside=1; next }
  inside && /^}/ { inside=0 }
  inside && /func[({]/ { print $1 }
' "$repo_dir/internal/keeper/cycle.go" > "$actual"

awk '
  /^type fn(Pane|Gauge|Handoff|Respawn)( |$)/ { print $2 }
' "$repo_dir/internal/keeper/ports.go" >> "$actual"

LC_ALL=C sort -u "$actual" -o "$actual"
if ! diff -u "$allow" "$actual"; then
  echo "keeper-seam-ratchet: legacy seam set changed" >&2
  echo "Remove entries only as their callers migrate. Do not add new seams." >&2
  exit 1
fi
echo "keeper-seam-ratchet: PASS"
