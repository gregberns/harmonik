#!/usr/bin/env bash
# release-preflight.sh — the VET GATE for cutting a harmonik release.
#
# Runs the full set of guards that must pass BEFORE tagging a release:
#   - we are on a clean `main` that is in sync with origin/main,
#   - `make build` and `make check` (the full race/lint/coverage tier) pass,
#   - goreleaser config validates AND a 4-platform snapshot cross-compiles,
#     producing binaries for all of linux/darwin × amd64/arm64.
#
# On success it prints the validated SHA and `PREFLIGHT GREEN — safe to tag`,
# then ECHOES (does not run) the suggested tag + push commands. The operator
# copies those commands deliberately — this script never tags or pushes.
#
# Usage:  scripts/release-preflight.sh
set -euo pipefail

# Resolve repo root from the script's location so the gate works regardless of
# the caller's CWD, then operate from there.
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

# ---------------------------------------------------------------------------
# Gate 1 — branch must be `main`.
# ---------------------------------------------------------------------------
CURRENT_BRANCH="$(git rev-parse --abbrev-ref HEAD)"
if [[ "$CURRENT_BRANCH" != "main" ]]; then
  echo "ABORT: releases are cut from 'main', but you are on '$CURRENT_BRANCH'." >&2
  echo "       git checkout main, then re-run." >&2
  exit 1
fi

# ---------------------------------------------------------------------------
# Gate 2 — working tree must be clean (no uncommitted changes, no untracked).
# ---------------------------------------------------------------------------
if [[ -n "$(git status --porcelain)" ]]; then
  echo "ABORT: working tree is not clean. Commit or stash before releasing:" >&2
  git status --short >&2
  exit 1
fi

# ---------------------------------------------------------------------------
# Gate 3 — local main must equal origin/main (fetch first so the compare is
# against the true remote tip, not a stale tracking ref).
# ---------------------------------------------------------------------------
echo "==> Fetching origin to compare main with origin/main..."
git fetch origin main
LOCAL_SHA="$(git rev-parse main)"
REMOTE_SHA="$(git rev-parse origin/main)"
if [[ "$LOCAL_SHA" != "$REMOTE_SHA" ]]; then
  echo "ABORT: local main ($LOCAL_SHA) != origin/main ($REMOTE_SHA)." >&2
  echo "       Pull/push so they match, then re-run." >&2
  exit 1
fi

# ---------------------------------------------------------------------------
# Gate 4 — build + full check tier must pass.
# ---------------------------------------------------------------------------
echo "==> make build"
make build
echo "==> make check (full race/lint/coverage tier)"
make check

# ---------------------------------------------------------------------------
# Gate 5 — locate goreleaser. Prefer the GOPATH-installed binary; fall back to
# whatever is on PATH.
# ---------------------------------------------------------------------------
GORELEASER="$(go env GOPATH)/bin/goreleaser"
if [[ ! -x "$GORELEASER" ]]; then
  if command -v goreleaser >/dev/null 2>&1; then
    GORELEASER="$(command -v goreleaser)"
  else
    echo "ABORT: goreleaser not found at \$(go env GOPATH)/bin/goreleaser or on PATH." >&2
    echo "       Install it: go install github.com/goreleaser/goreleaser/v2@latest" >&2
    exit 1
  fi
fi
echo "==> Using goreleaser: $GORELEASER"

# ---------------------------------------------------------------------------
# Gate 6 — goreleaser config must validate, then a snapshot must cross-compile
# all four target platforms.
# ---------------------------------------------------------------------------
echo "==> goreleaser check"
"$GORELEASER" check

echo "==> goreleaser release --snapshot --clean (4-platform cross-compile)"
"$GORELEASER" release --snapshot --clean

# ---------------------------------------------------------------------------
# Gate 7 — assert dist/ contains a binary for every os/arch combo.
# goreleaser lays binaries out as dist/<build-id>_<os>_<arch[_version]>/harmonik.
# ---------------------------------------------------------------------------
echo "==> Verifying dist/ contains all 4 os/arch binaries..."
MISSING=0
for combo in linux_amd64 linux_arm64 darwin_amd64 darwin_arm64; do
  os="${combo%_*}"
  arch="${combo#*_}"
  # Match the binary under any build-output dir for this os/arch (the arch dir
  # may carry a version suffix, e.g. *_amd64_v1, so glob the arch prefix).
  if ! find dist -type f -name harmonik -path "*_${os}_${arch}*" 2>/dev/null | grep -q .; then
    echo "  MISSING: no harmonik binary for $os/$arch" >&2
    MISSING=1
  else
    echo "  OK: $os/$arch"
  fi
done
if [[ "$MISSING" -ne 0 ]]; then
  echo "ABORT: snapshot did not produce all 4 target binaries." >&2
  exit 1
fi

# ---------------------------------------------------------------------------
# All gates passed — report the validated SHA and the (manual) next steps.
# ---------------------------------------------------------------------------
VALIDATED_SHA="$(git rev-parse HEAD)"
echo ""
echo "Validated SHA: $VALIDATED_SHA"
echo "PREFLIGHT GREEN — safe to tag $VALIDATED_SHA"
echo ""
echo "Suggested next steps (NOT run by this script — copy them deliberately):"
echo "  git tag -a vX.Y.Z -m 'harmonik vX.Y.Z' $VALIDATED_SHA"
echo "  git push origin vX.Y.Z"
