#!/usr/bin/env bash
# Print the Go packages a delta-scoped gate must test, one per line, as
# ./-prefixed paths ready to hand to `go test`.
#
# Three sources, unioned, because the gate runs on both sides of a commit:
#
#   1. tracked files that differ from HEAD   - work that is not committed yet
#   2. untracked, non-ignored files          - a new package that is not added yet
#   3. files changed by HEAD itself          - the commit the gate was told to judge
#
# Source 3 is why this script exists. The documented flow is commit first, then
# run the gate. After a commit the working tree is clean, so sources 1 and 2 are
# empty. A gate that consulted only those tested NOTHING and reported that there
# was nothing to do, which reads like success. Source 2 is the same silence
# before a commit: `git diff` never lists an untracked file, so a new package
# was invisible until someone staged it.
#
# The union is deliberate. The gate must be useful whether the caller committed
# first or not, and testing one package twice costs a second.
#
# Every failure in here must be loud. This script exists because a gate went
# quiet, so "printed nothing and exited 0" is the one outcome it must never
# reach by accident.

set -euo pipefail

repo_root=$(git rev-parse --show-toplevel)
cd "$repo_root"

# A missing HEAD~1 has two causes and they need opposite answers.
#
# A repository with one genuine commit has nothing earlier to consult, and that
# is fine - source 3 is empty because there is no history, and the working tree
# sources still cover everything.
#
# A shallow clone has one commit because the history was TRUNCATED. Source 3
# would contribute nothing, and a commit that added a whole package would be
# reported as "no Go file changed". That is this script's own defect through a
# different door, so refuse rather than go quiet. `--depth 1` is the shape that
# bites; a deeper shallow clone still resolves HEAD~1 and is fine.
if ! git rev-parse --verify -q HEAD~1 >/dev/null 2>&1 &&
    [[ "$(git rev-parse --is-shallow-repository 2>/dev/null)" == "true" ]]; then
    printf 'changed-go-packages: this is a shallow clone with no HEAD~1, so the commit under\n' >&2
    printf 'test cannot be read. Any answer here would omit it. Re-clone with full history\n' >&2
    printf '(actions/checkout with fetch-depth: 0), or run a tier that tests every package.\n' >&2
    exit 1
fi

# --no-renames on both diffs. Git detects a cross-package move as one rename and
# then names only the destination, so the SOURCE package - which just lost a
# file and may no longer build - would never be tested. That is the same false
# green this script exists to remove.
candidates=$(
    {
        git diff --no-renames --name-only HEAD -- '*.go'
        git ls-files --others --exclude-standard -- '*.go'
        # A repository with one commit has no earlier revision. Not an error.
        if git rev-parse --verify -q HEAD~1 >/dev/null; then
            git diff --no-renames --name-only HEAD~1 HEAD -- '*.go'
        fi
    } | while IFS= read -r file; do
        dir=$(dirname "$file")
        [[ "$dir" == "." ]] || dir="./$dir"
        printf '%s\n' "$dir"
    done | sort -u
)

if [[ -z "$candidates" ]]; then
    exit 0
fi

# Nothing is printed until every candidate has been classified. A run that
# listed two packages and then met a broken third would otherwise emit two lines
# AND exit 1, and a caller that ignored the status would act on a partial set as
# if it were the whole one. Silence on failure has to be structural here, not a
# side effect of which candidate happened to fail first.
selected=()

while IFS= read -r dir; do
    # A commit's diff still names a path that the same commit deleted. Answer
    # that here rather than from a `go list` message, so the check does not rest
    # on the wording of an error string.
    if [[ "$dir" != "." && ! -d "$dir" ]]; then
        continue
    fi
    # For a directory that IS there, `go list` is the predicate rather than
    # "the directory exists". A directory can survive the deletion of its last
    # Go file, and a directory can hold only build-excluded files -
    # testdata/depguard is exactly that today. Both pass a -d test and then fail
    # `go test` with "no Go files" or "build constraints exclude all Go files",
    # which turns the gate red for no defect at all.
    if listing=$(go list "$dir" 2>&1); then
        selected+=("$dir")
        continue
    fi
    # Exactly two `go list` failures mean "this directory is not a package to
    # test". Every other failure means the module environment is broken - a bad
    # go.mod, or GOFLAGS=-mod=vendor against a stale vendor tree. Treating THAT
    # as "skip this directory" would drop every candidate and print nothing,
    # so a broken toolchain would read as "nothing to test". That is this
    # script's own defect wearing a new costume. Refuse loudly instead.
    case "$listing" in
    *"no Go files in"* | *"build constraints exclude all Go files"*)
        continue
        ;;
    esac
    printf 'changed-go-packages: cannot resolve %s as a package, and not because it holds none.\n' "$dir" >&2
    printf 'The changed-package set cannot be trusted, so no set is offered. Fix the cause below.\n' >&2
    printf '%s\n' "$listing" >&2
    exit 1
done <<<"$candidates"

if [[ ${#selected[@]} -gt 0 ]]; then
    printf '%s\n' "${selected[@]}"
fi
