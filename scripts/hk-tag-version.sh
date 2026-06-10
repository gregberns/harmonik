#!/usr/bin/env bash
# hk-tag-version.sh — stamp a local "daemon-YYYYMMDD.NN" tag on HEAD.
# Called from the daemon restart procedure (hk-keeper.sh / SHUTDOWN.md §2).
# LOCAL ONLY — never pushed. Safe to call repeatedly; mints the next seq.
#
# Usage:  ./scripts/hk-tag-version.sh [optional note]
# Output: prints the tag name on stdout (only the tag, so callers can capture it).
set -euo pipefail

REPO="${HK_PROJECT:-$(git rev-parse --show-toplevel)}"
DATE="$(date '+%Y%m%d')"
PREFIX="daemon-${DATE}"

# Highest existing sequence for today, default 00.
LAST="$(git -C "$REPO" tag --list "${PREFIX}.*" \
        | sed -n "s/^${PREFIX}\.//p" | sort -n | tail -1)"
NEXT="$(printf '%02d' "$((10#${LAST:-0} + 1))")"
TAG="${PREFIX}.${NEXT}"

SHORT="$(git -C "$REPO" rev-parse --short HEAD)"
NOTE="${1:-}"
MSG="daemon restart @ $(date '+%F %T') | HEAD ${SHORT}${NOTE:+ | ${NOTE}}"

git -C "$REPO" tag -a "$TAG" -m "$MSG"
echo "$TAG"
