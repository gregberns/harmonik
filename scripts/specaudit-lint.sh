#!/usr/bin/env bash
set -euo pipefail

# specaudit-lint.sh — Harmonik spec-drift lint gate (M1-1)
#
# The internal/specaudit suite is 132 test files that walk specs/*.md and assert
# structural invariants. 129 of them execute ZERO product code — they are a
# spec-drift LINT, not unit tests, so they are relocated behind the `specaudit`
# build tag and skipped by the default `go test ./...`. This script runs them.
#
# The 3 product-importing carve-outs (ar025, hqwn57, sh_inv005) are NOT tagged
# and continue to run under the default test build. See
# internal/specaudit/RELOCATED-ALLOWLIST.md for the full tagged-file list.
#
# Exit 0 = no spec drift. Non-zero = a spec-prose invariant failed.

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

echo "specaudit-lint: go test -tags specaudit ./internal/specaudit/..."
exec go test -tags specaudit ./internal/specaudit/... -count=1
