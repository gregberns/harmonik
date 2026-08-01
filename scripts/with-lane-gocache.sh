#!/usr/bin/env bash
# Run one command with a Go build cache private to THIS checkout.
#
# Why this exists, and why it is not with-isolated-gocache.sh. Two lanes work
# this repo at once, from separate worktrees, and they share one GOCACHE. The
# Makefile already records what that costs: measured 2026-07-22, a concurrent
# process invalidating cache facts mid-run makes a build fail with
# "could not import ... no such file or directory", and a gate that fails closed
# reports that as a hard failure. So the second lane waits, and a lane that waits
# on a 2-to-3-minute recipe is the bottleneck the split exists to remove.
#
# with-isolated-gocache.sh solves the same collision by handing the command a
# `mktemp -d` cache and deleting it on the way out. That is exactly right for
# cmd-coverage-gate.sh, which runs once at the end of a tier. It is the wrong
# shape for a recipe one lane runs over and over: every run starts COLD, so the
# fix costs more than the collision it prevents.
#
# The cache here is keyed on the checkout ROOT and it PERSISTS. Two lanes never
# touch each other's cache, and each lane's own cache stays as warm as the shared
# one is today. That is the whole difference: isolation between lanes, warmth
# within a lane.
#
# SAY WHAT THIS DOES NOT DO. It narrows the corruption window, it does not close
# it. Two runs in the SAME checkout — an agent and a human, or two agents — still
# share one cache and are exactly as exposed as before. And a persistent cache is
# reachable by Go's own daily trim, which evicts entries unused for five days; a
# `mktemp -d` cache never lives long enough to trim, so this shape gives that
# vector back. The practical risk is low, because a run in progress is not using
# five-day-stale entries, but "concurrent runs cannot corrupt each other" is only
# true ACROSS checkouts.
#
# It also isolates GOCACHE alone. golangci-lint keeps its own cache under
# ~/Library/Caches/golangci-lint and that stays shared — `--allow-parallel-runners`
# is what covers the lint side, and it is already on those recipe lines.
#
# Measured on this box 2026-08-01, `go build ./...` back to back. Under a lane
# cache: 9.15s to fill it, then 1.33s. Under with-isolated-gocache.sh: 8.78s then
# 8.67s — cold every time, because the cache it just filled is deleted on exit.
# Same isolation, and the second run is about 6.5x faster. `go build` is the
# CHEAP case; check-short is `go test -short -race ./...` at `-p=1`, where the
# compile work being thrown away is far larger. That is why check-short is
# wrapped in this script rather than in that one.
#
# DISK, and this one has a sharp edge. Each checkout keeps its own cache, so N
# checkouts cost N caches, and **a cache outlives the worktree that made it**.
# Agent worktrees are created and thrown away constantly here, and nothing reaps
# what they leave behind. `go build ./...` alone fills 157 MiB and a check-short
# cache carrying -race test objects is larger again. The Go caches under
# ~/Library/Caches were the single largest item the 2026-07-28 disk sweep found,
# at 10.5 GiB, so this is not free.
#
# `go clean -cache` does NOT reach these. It clears whatever GOCACHE resolves to,
# which by default is ~/Library/Caches/go-build. Sweep the whole tree with
# `rm -rf ~/Library/Caches/harmonik-lane-gocache`. Deleting any one directory is
# always safe — it costs that checkout one cold build and touches no other.
# docs/disk-reclaim.md carries this in its cache-location table.

set -euo pipefail

if [ $# -eq 0 ]; then
    echo "usage: scripts/with-lane-gocache.sh command [args ...]" >&2
    exit 2
fi

# The key is derived from the checkout root, not passed in: every invocation in a
# given tree computes the same path with nothing to remember or export, and that
# is what lets separate recipe lines share one cache. --show-toplevel gives the
# WORKTREE root rather than the shared .git, so two worktrees of one repository
# resolve differently. That is the property this depends on.
#
# The key is the directory name PLUS a hash of the full path. The name alone
# reads well in a disk sweep, but it is not unique — a second clone named
# `harmonik` anywhere on the box would silently share this cache and reintroduce
# exactly the corruption being defended against, with no error to notice. The
# hash makes the key unique while the name keeps it readable.
root=$(git rev-parse --show-toplevel 2>/dev/null) || {
    echo "with-lane-gocache: not inside a git worktree" >&2
    exit 2
}
lane="$(basename "$root")-$(printf '%s' "$root" | shasum | cut -c1-8)"

cache_root="${HARMONIK_LANE_GOCACHE_ROOT:-$HOME/Library/Caches/harmonik-lane-gocache}"
cache_dir="$cache_root/$lane"
mkdir -p "$cache_dir"

# No trap and no cleanup, deliberately. The cache outliving the command is the
# point. That also means no temp directory can be removed out from under a
# command that outlives this wrapper, so the signal-forwarding dance that
# with-isolated-gocache.sh needs has nothing to protect here.
export GOCACHE="$cache_dir"
exec "$@"
