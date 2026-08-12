#!/usr/bin/env bash
# Reap the shared Go build cache down to a size limit, least-recently-used first.
#
# Why this exists. Go bounds its build cache by TIME, not by SIZE. cmd/go
# removes entries that are unused for 5 days (trimLimit in
# cmd/go/internal/cache/cache.go) and it has no size limit at all. That default
# is tuned for one developer on one machine. This box makes 13 to 17 GiB of new
# cache entries each day across about 15 lane branches and five build
# configurations (-cover, -race, -tags=scenario, subprocess, integration), so
# the steady state is near 70 GiB. The disk fills days before Go removes
# anything. Below the daemon 10 GiB watermark dispatch stops silently and the
# fleet reports fake failures. See docs/disk-reclaim.md and bead hk-qqg5c.
#
# Why this is safe to run at any time, including during a build. Cache entries
# are content addressed. A deleted entry is a cache miss, and the build makes
# it again. A delete costs rebuild time, never correctness. On Unix a delete of
# an open file lets the reader keep its handle, so a running build is not hurt.
#
# Why mtime and not creation time. Go touches an entry's mtime each time the
# entry is USED, at most once per hour. So mtime means "last used", not "made
# at". Oldest mtime first is therefore least-recently-used. Creation time would
# delete the packages you rebuild against every day.
#
# Usage:
#   scripts/go-cache-reap.sh            reap to the default limit
#   scripts/go-cache-reap.sh 10         reap to 10 GiB
#   DRY_RUN=1 scripts/go-cache-reap.sh  report only, delete nothing

set -uo pipefail

LIMIT_GIB="${1:-15}"
DRY_RUN="${DRY_RUN:-0}"

case "$LIMIT_GIB" in
  ''|*[!0-9]*) echo "limit must be a whole number of GiB, got '$LIMIT_GIB'" >&2; exit 2 ;;
esac

cache="$(go env GOCACHE 2>/dev/null)"
if [ -z "$cache" ] || [ "$cache" = "off" ] || [ ! -d "$cache" ]; then
  echo "GOCACHE is not a usable directory: '${cache}'" >&2
  exit 1
fi
# Refuse to delete inside anything that is not a Go build cache.
if [ ! -f "$cache/trim.txt" ] && [ ! -d "$cache/00" ]; then
  echo "'$cache' has no trim.txt and no 00/ shard, so it is not a Go build cache" >&2
  exit 1
fi

used_kib() { du -sk "$cache" 2>/dev/null | awk '{print $1}'; }
gib() { awk -v k="$1" 'BEGIN{printf "%.1f", k/1048576}'; }

before_kib="$(used_kib)"
limit_kib=$(( LIMIT_GIB * 1024 * 1024 ))

if [ "$before_kib" -le "$limit_kib" ]; then
  echo "go build cache is $(gib "$before_kib") GiB, within the ${LIMIT_GIB} GiB limit. Nothing to do."
  exit 0
fi
excess_bytes=$(( (before_kib - limit_kib) * 1024 ))

tmp="$(mktemp -t go-cache-reap)" || exit 1
trap 'rm -f "$tmp" "$tmp.del"' EXIT

# Only the -d objects hold real bytes. The -a files are one line of metadata
# each. List oldest-used first.
find "$cache" -type f -name '*-d' -print0 2>/dev/null \
  | xargs -0 stat -f '%m %z %N' 2>/dev/null \
  | sort -n > "$tmp"

# Take from the oldest end until the excess is covered.
awk -v need="$excess_bytes" '
  { total += $2; path = $0; sub(/^[0-9]+ [0-9]+ /, "", path); print path }
  total >= need { exit }
' "$tmp" > "$tmp.del"

count="$(wc -l < "$tmp.del" | tr -d ' ')"
if [ "$count" -eq 0 ]; then
  echo "found no cache objects to remove, but the cache is over the limit" >&2
  exit 1
fi

if [ "$DRY_RUN" = "1" ]; then
  echo "DRY RUN: would remove ${count} objects to bring $(gib "$before_kib") GiB down to ${LIMIT_GIB} GiB"
  echo "oldest object last used: $(date -r "$(head -1 "$tmp" | awk '{print $1}')")"
  exit 0
fi

# Remove each object with its -a metadata sibling.
while IFS= read -r f; do
  printf '%s\0%s\0' "$f" "${f%-d}-a"
done < "$tmp.del" | xargs -0 rm -f 2>/dev/null

after_kib="$(used_kib)"
reclaimed_mib=$(( (before_kib - after_kib) / 1024 ))
echo "removed ${count} objects: $(gib "$before_kib") GiB -> $(gib "$after_kib") GiB (reclaimed ${reclaimed_mib} MiB)"

# A reap that frees nothing did not work. Do not report it as done.
if [ "$reclaimed_mib" -le 0 ]; then
  echo "WARNING: reclaimed 0 MiB. The reap did not remove anything." >&2
  exit 1
fi
