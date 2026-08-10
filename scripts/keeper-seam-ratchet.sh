#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
allowlist="$repo_root/internal/keeper/legacy_seams.txt"
cycle="$repo_root/internal/keeper/cycle.go"
ports="$repo_root/internal/keeper/ports.go"

if [[ ! -f "$allowlist" ]]; then
  echo "keeper-seam-ratchet: missing $allowlist" >&2
  exit 1
fi

found="$(mktemp)"
expected="$(mktemp)"
trap 'rm -f "$found" "$expected"' EXIT

sed -n '/^type CyclerConfig struct {$/,/^}$/p' "$cycle" \
  | sed -nE 's/^[[:space:]]*([A-Z][A-Za-z0-9]*)[[:space:]]+func\(.*/function:\1/p' \
  >"$found"
sed -nE 's/^type ((fn|legacy)[A-Z][A-Za-z0-9]*) struct.*/adapter:\1/p' "$ports" \
  >>"$found"

LC_ALL=C sort -u "$found" -o "$found"
LC_ALL=C sort -u "$allowlist" >"$expected"

if ! diff -u "$expected" "$found"; then
  echo "keeper-seam-ratchet: compatibility seams changed without updating the tracked list" >&2
  exit 1
fi

echo "keeper-seam-ratchet: OK"
