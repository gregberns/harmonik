#!/usr/bin/env bash
# hk-version-report.sh — for each local daemon-* version tag, show the tagged
# commit's date and the count of OPEN beads labeled found-in:<tag>.
# "GOOD" = zero open issues found against that version.
set -euo pipefail
REPO="${HK_PROJECT:-$(git rev-parse --show-toplevel)}"
git -C "$REPO" tag --list 'daemon-*' | sort | while read -r TAG; do
  DATE="$(git -C "$REPO" log -1 --format=%ci "$TAG" | cut -d' ' -f1)"
  N="$(br list --label "found-in:$TAG" --status open --json | grep -o '"total":[0-9]*' | head -1 | cut -d: -f2)"
  N="${N:-0}"
  [ "$N" -eq 0 ] && STATE=GOOD || STATE="$N open"
  printf '%-22s %s  %s\n' "$TAG" "$DATE" "$STATE"
done
