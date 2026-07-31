#!/usr/bin/env bash
# Tests for scripts/changed-go-packages.sh. Each case builds a throwaway git
# repository in the state the gate would meet it in, and names the claim it
# defends.

set -euo pipefail

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
subject="$script_dir/changed-go-packages.sh"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

failures=0

# new_repo prints the path of a fresh repository holding one commit.
new_repo() {
    local name=$1
    local repo="$tmp/$name"
    mkdir -p "$repo"
    git -C "$repo" init -q .
    # Isolate from the developer's global config: a global commit.gpgsign or
    # core.hooksPath would otherwise make this gate fail on their machine only.
    git -C "$repo" config user.email test@example.com
    git -C "$repo" config user.name test
    git -C "$repo" config commit.gpgsign false
    git -C "$repo" config core.hooksPath /dev/null
    printf 'module example.test/changed\n\ngo 1.23\n' > "$repo/go.mod"
    printf 'ignored/\n' > "$repo/.gitignore"
    mkdir -p "$repo/internal/base"
    printf 'package base\n' > "$repo/internal/base/base.go"
    git -C "$repo" add -A
    git -C "$repo" commit -qm base
    printf '%s' "$repo"
}

# expect runs the subject in a repository and compares its whole output.
expect() {
    local claim=$1 repo=$2 want=$3
    local got
    got=$(cd "$repo" && "$subject")
    if [[ "$got" != "$want" ]]; then
        printf 'changed-go-packages-test: FAIL - %s\n  want: %s\n  got:  %s\n' \
            "$claim" "${want:-<nothing>}" "${got:-<nothing>}" >&2
        failures=$((failures + 1))
    fi
}

# expect_fails runs the subject and requires it to refuse loudly: a non-zero
# exit AND a message. Printing nothing and exiting 0 is the failure this whole
# script exists to remove, so it must never be how a broken environment looks.
expect_fails() {
    local claim=$1 repo=$2 env_assignment=$3 stdout stderr status
    stderr=$(mktemp "$tmp/stderr.XXXXXX")
    set +e
    stdout=$(cd "$repo" && env "$env_assignment" "$subject" 2>"$stderr")
    status=$?
    set -e
    if [[ $status -eq 0 || -n "$stdout" || ! -s "$stderr" ]]; then
        printf 'changed-go-packages-test: FAIL - %s\n  exit: %d  stdout: %s  stderr: %s\n' \
            "$claim" "$status" "${stdout:-<nothing>}" "$(head -1 "$stderr" 2>/dev/null || echo '<nothing>')" >&2
        failures=$((failures + 1))
    fi
    rm -f "$stderr"
}

# The regression this script exists for. Committing is the documented flow, and
# it leaves the tree clean.
repo=$(new_repo committed)
mkdir -p "$repo/internal/fresh"
printf 'package fresh\n' > "$repo/internal/fresh/fresh.go"
printf 'package fresh\n' > "$repo/internal/fresh/fresh_test.go"
git -C "$repo" add -A
git -C "$repo" commit -qm "add fresh"
expect "a package added by the last commit is tested on a clean tree" \
    "$repo" "./internal/fresh"

# The same silence before a commit: git diff never lists an untracked file.
repo=$(new_repo untracked)
mkdir -p "$repo/internal/new"
printf 'package new\n' > "$repo/internal/new/new.go"
expect "an untracked new package is tested before anyone stages it" \
    "$repo" "./internal/new"

repo=$(new_repo modified)
printf 'package base\n\nvar Changed = 1\n' > "$repo/internal/base/base.go"
expect "an uncommitted change to a tracked file is tested" \
    "$repo" "./internal/base"

repo=$(new_repo staged)
mkdir -p "$repo/internal/staged"
printf 'package staged\n' > "$repo/internal/staged/staged.go"
git -C "$repo" add -A
expect "a staged but uncommitted package is tested" \
    "$repo" "./internal/staged"

# A commit's diff still names a path it deleted. go test must not be handed a
# directory that is no longer there.
repo=$(new_repo deleted)
mkdir -p "$repo/internal/doomed"
printf 'package doomed\n' > "$repo/internal/doomed/doomed.go"
git -C "$repo" add -A
git -C "$repo" commit -qm "add doomed"
git -C "$repo" rm -q -r internal/doomed
git -C "$repo" commit -qm "remove doomed"
expect "a package the last commit deleted is not offered to go test" \
    "$repo" ""

repo=$(new_repo nongo)
printf '# notes\n' > "$repo/README.md"
git -C "$repo" add -A
git -C "$repo" commit -qm "docs only"
expect "a commit that touches no Go file selects nothing" \
    "$repo" ""

repo=$(new_repo ignoredfile)
mkdir -p "$repo/ignored/pkg"
printf 'package ignored\n' > "$repo/ignored/pkg/ignored.go"
expect "an ignored Go file is not tested" \
    "$repo" ""

# Two files in one package must not run that package twice.
repo=$(new_repo dedupe)
printf 'package base\n\nvar A = 1\n' > "$repo/internal/base/base.go"
printf 'package base\n\nvar B = 2\n' > "$repo/internal/base/more.go"
expect "two changed files in one package select that package once" \
    "$repo" "./internal/base"

repo=$(new_repo rootfile)
printf 'package main\n\nfunc main() {}\n' > "$repo/main.go"
expect "a Go file at the repository root selects the root package" \
    "$repo" "."

# A repository with one commit has no HEAD~1, and asking for it must not fail.
repo=$(new_repo firstcommit)
expect "a repository with a single commit does not error" \
    "$repo" ""

# Both sides at once: the caller committed one package and is part way through
# another. The gate must cover both.
repo=$(new_repo bothsides)
mkdir -p "$repo/internal/done"
printf 'package done\n' > "$repo/internal/done/done.go"
git -C "$repo" add -A
git -C "$repo" commit -qm "add done"
printf 'package base\n\nvar InFlight = 1\n' > "$repo/internal/base/base.go"
expect "committed work and uncommitted work are both tested" \
    "$repo" "./internal/base
./internal/done"

# Git detects a cross-package move as one rename and names only the destination.
# The source package just lost a file and may no longer build.
repo=$(new_repo renamed)
mkdir -p "$repo/internal/from" "$repo/internal/to"
printf 'package from\n\nvar Moved = 1\n' > "$repo/internal/from/moved.go"
printf 'package from\n' > "$repo/internal/from/stay.go"
printf 'package to\n' > "$repo/internal/to/keep.go"
git -C "$repo" add -A
git -C "$repo" commit -qm "add both"
git -C "$repo" mv internal/from/moved.go internal/to/moved.go
sed 's/package from/package to/' "$repo/internal/to/moved.go" > "$repo/internal/to/moved.go.new"
mv "$repo/internal/to/moved.go.new" "$repo/internal/to/moved.go"
git -C "$repo" add -A
git -C "$repo" commit -qm "move it"
expect "a cross-package rename tests the package the file left, not only the one it joined" \
    "$repo" "./internal/from
./internal/to"

# A directory can survive the deletion of its last Go file.
repo=$(new_repo emptied)
mkdir -p "$repo/internal/emptied"
printf 'package emptied\n' > "$repo/internal/emptied/only.go"
printf 'notes\n' > "$repo/internal/emptied/README.md"
git -C "$repo" add -A
git -C "$repo" commit -qm "add emptied"
git -C "$repo" rm -q internal/emptied/only.go
git -C "$repo" commit -qm "remove its last go file"
expect "a directory that outlives its last Go file is not offered to go test" \
    "$repo" ""

# A directory can hold only build-excluded files. testdata/depguard in the real
# repository is exactly this shape.
repo=$(new_repo buildexcluded)
mkdir -p "$repo/testdata/fixture"
printf '//go:build ignore\n\npackage fixture\n' > "$repo/testdata/fixture/fixture.go"
git -C "$repo" add -A
git -C "$repo" commit -qm "add build-excluded fixture"
expect "a directory whose only Go files are build-excluded is not offered to go test" \
    "$repo" ""

# A broken module environment must not look like "nothing to test". GOFLAGS
# -mod=vendor against a repository with no vendor tree makes every `go list`
# fail for a reason that is NOT "this directory holds no package".
repo=$(new_repo brokenmodule)
# A module with a requirement and no vendor tree. -mod=vendor then fails for a
# reason that is NOT "this directory holds no package". A module with no
# requirements at all would vendor cleanly and prove nothing.
printf 'module example.test/changed\n\ngo 1.23\n\nrequire example.test/absent v1.0.0\n' > "$repo/go.mod"
mkdir -p "$repo/internal/real"
printf 'package real\n' > "$repo/internal/real/real.go"
git -C "$repo" add -A
git -C "$repo" commit -qm "add real"
expect_fails "a broken module environment refuses loudly instead of selecting nothing" \
    "$repo" "GOFLAGS=-mod=vendor"

# A refusal must offer no partial set. `internal/base` classifies cleanly and is
# ordered before `internal/broken`, so a script that printed as it went would
# emit one line AND exit non-zero. A caller that ignored the status would then
# act on half a set believing it was whole.
repo=$(new_repo partialset)
printf 'module example.test/changed\n\ngo 1.23\n\nrequire example.test/absent v1.0.0\n' > "$repo/go.mod"
printf 'package base\n\nvar Touched = 1\n' > "$repo/internal/base/base.go"
mkdir -p "$repo/internal/broken"
printf 'package broken\n' > "$repo/internal/broken/broken.go"
git -C "$repo" add -A
git -C "$repo" commit -qm "touch base, add broken"
expect_fails "a refusal offers no partial set, even when an earlier package resolved cleanly" \
    "$repo" "GOFLAGS=-mod=vendor"

# A shallow clone has one commit because the history was truncated, not because
# the project is new. Source 3 is silently empty, so a commit that added a whole
# package would be reported as "nothing changed" - this script's own defect
# through a different door. The genuine-first-commit case above must stay quiet,
# so these two tests are a pair: only the shallow flag tells them apart.
repo=$(new_repo shallowsource)
mkdir -p "$repo/internal/added"
printf 'package added\n' > "$repo/internal/added/added.go"
git -C "$repo" add -A
git -C "$repo" commit -qm "add a package"
shallow="$tmp/shallowclone"
git clone -q --depth 1 "file://$repo" "$shallow"
expect_fails "a shallow clone refuses instead of reporting that nothing changed" \
    "$shallow" "GOFLAGS="

if [[ $failures -ne 0 ]]; then
    printf 'changed-go-packages-test: %d FAILED\n' "$failures" >&2
    exit 1
fi
printf 'changed-go-packages-test: PASS\n'
