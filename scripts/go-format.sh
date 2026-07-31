#!/usr/bin/env bash
# Format or check exactly the Go files that belong to the current Git working
# tree: tracked files plus non-ignored untracked files. Ignored worktrees and
# scratch artifacts are excluded by Git's own ignore rules, while a newly
# created (not-yet-tracked) production file is still checked.

set -euo pipefail

usage() {
    echo "usage: scripts/go-format.sh check|write" >&2
    exit 2
}

[[ $# -eq 1 ]] || usage
mode=$1
[[ "$mode" == "check" || "$mode" == "write" ]] || usage

repo_root=$(git rev-parse --show-toplevel)
cd "$repo_root"

# The tools live in the MAIN working tree, not this one. `.tools` holds built
# binaries and is gitignored, so a git worktree receives none of it and this
# script died with a bare "No such file or directory" on the tool path.
# --git-common-dir gives the shared .git from a worktree as well as from the
# main checkout, so its parent is the main working tree either way.
tools_home=$(git rev-parse --path-format=absolute --git-common-dir 2>/dev/null | sed 's|/\.git/*$||')
[[ -n "$tools_home" ]] || tools_home=$repo_root

gofumpt=${GOFUMPT:-"$tools_home/.tools/gofumpt"}
gci=${GCI:-"$tools_home/.tools/gci"}
module=$(go list -m -f '{{.Path}}')

files=()
while IFS= read -r -d '' file; do
    # A tracked path can be deleted in the working tree; formatters should not
    # receive a path that no longer exists.
    [[ -f "$file" ]] && files+=("$file")
done < <(git ls-files -z --cached --others --exclude-standard -- '*.go')

[[ ${#files[@]} -gt 0 ]] || exit 0

if [[ "$mode" == "write" ]]; then
    "$gci" write -s standard -s default -s "prefix($module)" -- "${files[@]}"
    "$gofumpt" -w -- "${files[@]}"
    exit 0
fi

status=0
unformatted=$("$gofumpt" -l -- "${files[@]}")
if [[ -n "$unformatted" ]]; then
    echo "gofumpt: unformatted files (run 'make fmt' to fix):"
    echo "$unformatted"
    "$gofumpt" -d -- "${files[@]}"
    status=1
fi

# Stdin must be a character device here. gci's diff (and print) subcommands
# unconditionally prepend stdin to the file list whenever stdin is NOT a
# character device -- see gci's pkg/io StdInGenerator, which tests
# os.Stdin.Stat() against os.ModeCharDevice. An interactive shell hands gci a
# terminal, so this is invisible locally; a CI build step hands it a pipe, so
# gci parses zero bytes as Go source and dies with
# "StdIn:1:1: expected 'package', found 'EOF'" before ever looking at the
# arguments. /dev/null is a character device, so this pins the check to the
# explicit file list in every environment.
gci_diff=$("$gci" diff -s standard -s default -s "prefix($module)" -- "${files[@]}" </dev/null)
if [[ -n "$gci_diff" ]]; then
    echo "gci: import order drift detected (run 'make fmt' to fix):"
    echo "$gci_diff"
    status=1
fi

exit "$status"
