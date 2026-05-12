#!/usr/bin/env bash
# reset_test.sh — idempotency test for reset.sh
#
# Demonstrates that running reset.sh twice with a synthetic fixture-corpus
# directory containing a single placeholder.txt produces identical final state.
#
# Usage: bash test/exploratory/reset_test.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RESET_SH="$SCRIPT_DIR/reset.sh"

TMPDIR_BASE="$(mktemp -d)"
trap 'rm -rf "$TMPDIR_BASE"' EXIT

PROJECT="$TMPDIR_BASE/project"
FIXTURE_CORPUS="$TMPDIR_BASE/fixture-corpus"

mkdir -p "$PROJECT"
mkdir -p "$FIXTURE_CORPUS"

# Synthetic fixture corpus: one placeholder file
echo "fixture content" > "$FIXTURE_CORPUS/placeholder.txt"

# Seed an existing .beads/ and .harmonik/ so we verify they are removed on each run
mkdir -p "$PROJECT/.harmonik"
echo "stale" > "$PROJECT/.harmonik/stale.txt"

echo "--- First reset ---"
bash "$RESET_SH" --project "$PROJECT" --fixture-corpus "$FIXTURE_CORPUS"

snapshot_1="$(find "$PROJECT/.beads" -type f | sort)"
echo "State after first reset:"
echo "$snapshot_1"

echo ""
echo "--- Second reset (idempotency check) ---"
bash "$RESET_SH" --project "$PROJECT" --fixture-corpus "$FIXTURE_CORPUS"

snapshot_2="$(find "$PROJECT/.beads" -type f | sort)"
echo "State after second reset:"
echo "$snapshot_2"

echo ""
if [[ "$snapshot_1" == "$snapshot_2" ]]; then
    echo "PASS: both resets produced identical .beads/ contents"
else
    echo "FAIL: resets produced different .beads/ contents" >&2
    echo "First:  $snapshot_1" >&2
    echo "Second: $snapshot_2" >&2
    exit 1
fi

# Verify .harmonik/ is absent after the second reset
if [[ -d "$PROJECT/.harmonik" ]]; then
    echo "FAIL: .harmonik/ still present after reset" >&2
    exit 1
fi
echo "PASS: .harmonik/ absent after reset"

# Verify fixture file exists and has correct content
EXPECTED="fixture content"
ACTUAL="$(cat "$PROJECT/.beads/placeholder.txt")"
if [[ "$ACTUAL" != "$EXPECTED" ]]; then
    echo "FAIL: placeholder.txt content mismatch: got '$ACTUAL'" >&2
    exit 1
fi
echo "PASS: placeholder.txt content matches fixture"

# Verify missing-flag error paths
if bash "$RESET_SH" 2>/dev/null; then
    echo "FAIL: reset.sh should exit non-zero when flags are missing" >&2
    exit 1
fi
echo "PASS: missing flags cause non-zero exit"

echo ""
echo "All tests passed."
