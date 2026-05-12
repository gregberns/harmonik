#!/usr/bin/env bash
set -euo pipefail

usage() {
    echo "Usage: $0 --project DIR --fixture-corpus DIR" >&2
    echo "" >&2
    echo "  --project DIR         Project directory to reset" >&2
    echo "  --fixture-corpus DIR  Directory containing .beads/ snapshot to seed from" >&2
    exit 2
}

PROJECT=""
FIXTURE_CORPUS=""

while [[ $# -gt 0 ]]; do
    case "$1" in
        --project)
            [[ $# -ge 2 ]] || usage
            PROJECT="$2"
            shift 2
            ;;
        --fixture-corpus)
            [[ $# -ge 2 ]] || usage
            FIXTURE_CORPUS="$2"
            shift 2
            ;;
        *)
            echo "Unknown argument: $1" >&2
            usage
            ;;
    esac
done

if [[ -z "$PROJECT" ]]; then
    echo "Error: --project is required" >&2
    usage
fi

if [[ -z "$FIXTURE_CORPUS" ]]; then
    echo "Error: --fixture-corpus is required" >&2
    usage
fi

if [[ ! -d "$PROJECT" ]]; then
    echo "Error: --project directory does not exist: $PROJECT" >&2
    exit 2
fi

if [[ ! -d "$FIXTURE_CORPUS" ]]; then
    echo "Error: --fixture-corpus directory does not exist: $FIXTURE_CORPUS" >&2
    exit 2
fi

# 1. Remove .harmonik/ if present
if [[ -d "$PROJECT/.harmonik" ]]; then
    rm -rf "$PROJECT/.harmonik"
fi

# 2. Force-remove any harmonik-created git worktrees under .claude/worktrees/agent-*
# This handles both the registered-worktree case and any stale directories.
if [[ -d "$PROJECT/.claude/worktrees" ]]; then
    for wt_dir in "$PROJECT"/.claude/worktrees/agent-*; do
        [[ -d "$wt_dir" ]] || continue
        # Try to remove via git worktree first (removes registration + directory).
        # Run git from the project root so it knows its own worktree list.
        if git -C "$PROJECT" worktree remove --force --force "$wt_dir" 2>/dev/null; then
            :
        else
            # Fallback: not registered (or already detached) — remove the directory directly.
            rm -rf "$wt_dir"
        fi
    done
fi

# 3. Remove .beads/ if present
if [[ -d "$PROJECT/.beads" ]]; then
    rm -rf "$PROJECT/.beads"
fi

# 4. Seed fresh .beads/ from the fixture corpus
mkdir -p "$PROJECT/.beads"
cp -R "$FIXTURE_CORPUS/." "$PROJECT/.beads/"

echo "Reset complete: $PROJECT"
