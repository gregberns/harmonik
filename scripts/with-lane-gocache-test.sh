#!/usr/bin/env bash
# Tests for scripts/with-lane-gocache.sh. Each case names the claim it defends.
#
# The claims worth defending are all about the KEY, because every one of them is
# silent when it breaks. Two checkouts that collide on a key share a cache and
# reintroduce the corruption the script exists to prevent, with no error. Two
# recipe lines in one checkout that disagree on a key each build cold, and the
# only symptom is that check-short got slower.

set -uo pipefail

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
subject="$script_dir/with-lane-gocache.sh"

failures=0

fail() {
    echo "FAIL: $1" >&2
    failures=$((failures + 1))
}

# key_from prints the GOCACHE the subject exports when run from directory $1.
# The subshell keeps the cd from leaking — a cd persists across calls here.
key_from() {
    # shellcheck disable=SC2016 # $GOCACHE is expanded by the child shell, which is the point.
    (cd "$1" 2>/dev/null && HARMONIK_LANE_GOCACHE_ROOT="$tmp/cache" "$subject" sh -c 'printf %s "$GOCACHE"')
}

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

repo_root=$(cd "$script_dir/.." && pwd)

# Every invocation inside one checkout must agree, or the recipe's separate
# wrapped lines stop sharing a cache and each one builds cold.
root_key=$(key_from "$repo_root")
if [ -z "$root_key" ]; then
    fail "no GOCACHE exported from the repo root"
fi
for sub in scripts internal/daemon; do
    if [ -d "$repo_root/$sub" ]; then
        sub_key=$(key_from "$repo_root/$sub")
        if [ "$sub_key" != "$root_key" ]; then
            fail "$sub resolves to '$sub_key', repo root to '$root_key' — they must match"
        fi
    fi
done

# Two different checkouts must never collide, INCLUDING when their directory
# names are identical. The name alone was the first design and it fails here
# silently, which is why the key carries a hash of the full path.
mkdir -p "$tmp/a/harmonik" "$tmp/b/harmonik"
for d in "$tmp/a/harmonik" "$tmp/b/harmonik"; do
    git -C "$d" init -q .
    git -C "$d" config user.email test@example.com
    git -C "$d" config user.name test
done
key_a=$(key_from "$tmp/a/harmonik")
key_b=$(key_from "$tmp/b/harmonik")
if [ -z "$key_a" ] || [ -z "$key_b" ]; then
    fail "same-named checkouts: one produced no key (a='$key_a' b='$key_b')"
elif [ "$key_a" = "$key_b" ]; then
    fail "two checkouts both named 'harmonik' collide on '$key_a' — they would share a cache"
fi

# The key stays readable in a disk sweep. A bare hash would defeat the whole
# reason docs/disk-reclaim.md lists this path.
#
# Compare against the ACTUAL checkout name, never a literal. This file ships to
# every lane, so a hardcoded `harmonik-` would pass from the main checkout and
# from CI — which also checks out into a directory named `harmonik` — and fail in
# every worktree lane. That is a red gate the other lane can neither see nor fix,
# handed to it by a gate whose whole purpose is to let lanes run at once.
want=$(basename "$repo_root")
case "$(basename "$root_key")" in
"$want"-????????) ;;
*) fail "key '$(basename "$root_key")' is not '$want' plus an 8-character hash suffix" ;;
esac

# The cache directory is created, and it is NOT removed on exit. Persistence is
# the entire difference from with-isolated-gocache.sh.
if [ ! -d "$root_key" ]; then
    fail "cache directory '$root_key' was not created"
fi
marker="$root_key/persistence-probe"
: > "$marker"
key_from "$repo_root" > /dev/null
if [ ! -f "$marker" ]; then
    fail "cache directory was cleaned between runs — it must persist"
fi

# Outside a git worktree it refuses rather than guessing a key.
mkdir -p "$tmp/notrepo"
out=$( (cd "$tmp/notrepo" && HARMONIK_LANE_GOCACHE_ROOT="$tmp/cache" "$subject" true 2>&1) )
status=$?
if [ "$status" -ne 2 ]; then
    fail "outside a worktree: exit $status, want 2 (output: $out)"
fi

# No command is a usage error, not a silent success.
out=$(HARMONIK_LANE_GOCACHE_ROOT="$tmp/cache" "$subject" 2>&1)
status=$?
if [ "$status" -ne 2 ]; then
    fail "no arguments: exit $status, want 2 (output: $out)"
fi

# The command's exit status passes through, so the wrapper is transparent in a
# recipe and a failing gate step still fails.
HARMONIK_LANE_GOCACHE_ROOT="$tmp/cache" "$subject" sh -c 'exit 7'
if [ $? -ne 7 ]; then
    fail "exit-status passthrough: want 7"
fi

if [ "$failures" -ne 0 ]; then
    echo "with-lane-gocache-test: $failures failure(s)" >&2
    exit 1
fi

echo "with-lane-gocache-test: PASS"
