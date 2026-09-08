#!/usr/bin/env bash
#
# scenario-pkgs.sh — print every Go package in THIS MODULE whose test set grows
# when the `scenario` build tag is on, one `./path` per line.
#
# WHY THIS IS DERIVED AND NOT A LIST. `make test-scenario` used to name two
# packages by hand — ./test/scenario/... and ./internal/daemon/... — under a
# comment claiming that covered every package with a `//go:build scenario` file.
# The claim was false. Scenario files had spread to four more packages, and 11
# test functions in them were compiled by no target and run by no target. `make
# vet-tagged` typechecks them, so they still built. Typechecking runs nothing, so
# they were free to rot, and one had been red for weeks with nobody able to see
# it. A hand-written list of "everywhere X lives" goes stale the first time
# somebody adds an X. A derived one cannot. Refs hk-od9d4.
#
# WHY IT ASKS THE COMPILER RATHER THAN GREPPING FOR THE TAG. Two greps were
# tried and both were wrong:
#
#   `grep -r` over the filesystem knows nothing about module boundaries. This
#   repo keeps agent worktrees at `.claude/worktrees/<name>/`, each a full second
#   copy of the tree, so from the main checkout it returned 280 paths inside
#   those copies. `go test` rejects a path outside the main module, so
#   `make test-scenario` — and with it `make full`, the merge decision — died on
#   the operator's own checkout while passing in CI and inside a worktree.
#
#   Matching the tag line by text is wrong in a quieter way. `//go:build
#   scenario && !windows` is a scenario file and does not equal the string
#   "//go:build scenario". A constraint below a long header comment is a scenario
#   file and is not in the first few lines. Every such miss silently drops a
#   package from the tier, which is the same false green this script exists to
#   prevent.
#
# So it asks `go list` twice, once with the tag and once without, and reports the
# packages whose file set GROWS with it on. That is the compiler's own answer to
# "does the scenario tag turn anything on here", so it is right for compound
# constraints, for negations, and wherever in the file the constraint sits.
#
# NOT `./...` for the test run: that would run every ordinary test in the tree a
# second time under -race, which `make full` already ran under -short.
#
# Fails loudly and prints nothing when no package qualifies, so a caller cannot
# run an empty package list and read success out of it.
#
# scripts/scenario-pkgs-test.sh holds this script's assertions and runs inside
# `make script-tests`.
set -uo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root" || exit 1

# The repo carries a go.work (the platform re-grounding segment modules). This
# script reports on THIS module — the root module — only; the scenario tier
# lives here and in none of the segment modules. In workspace mode `go list -m`
# returns EVERY member, which the single-module logic below cannot read (the awk
# then gets a multiline module path and errors). Turn the workspace off so every
# go query in this script sees the root module alone.
export GOWORK=off

# One line per package: import path, then the count of files in each of the
# three test-bearing sets. -e keeps a package with a build error from killing
# the listing.
fmt='{{.ImportPath}} {{len .GoFiles}} {{len .TestGoFiles}} {{len .XTestGoFiles}}'

with_tag="$(go list -e -tags scenario -f "$fmt" ./... 2>/dev/null)"
without_tag="$(go list -e -f "$fmt" ./... 2>/dev/null)"

if [ -z "$with_tag" ] || [ -z "$without_tag" ]; then
	echo "scenario-pkgs.sh: 'go list' returned nothing for $repo_root." >&2
	echo "  The module does not resolve. Refusing to print a package list, because an" >&2
	echo "  empty one would make the scenario tier pass by running nothing." >&2
	exit 1
fi

module_path="$(go list -m 2>/dev/null)"
if [ -z "$module_path" ]; then
	echo "scenario-pkgs.sh: 'go list -m' gave no module path for $repo_root." >&2
	exit 1
fi

# Index the untagged counts by import path, then report any package whose total
# file count is higher with the tag on.
pkgs="$(
	awk -v module="$module_path" '
		NR == FNR {
			base[$1] = $2 + $3 + $4
			next
		}
		{
			total = $2 + $3 + $4
			if (!($1 in base) || total > base[$1]) {
				path = $1
				if (path == module) {
					print "."
				} else {
					sub("^" module "/", "", path)
					print "./" path
				}
			}
		}
	' <(printf '%s\n' "$without_tag") <(printf '%s\n' "$with_tag") | sort -u
)"

if [ -z "$pkgs" ]; then
	echo "scenario-pkgs.sh: the scenario build tag turns on no files in module $module_path." >&2
	echo "  Either the scenario tier was deleted, or the tag was renamed. Refusing to print" >&2
	echo "  an empty package list, because that would make the scenario tier pass by" >&2
	echo "  running nothing." >&2
	exit 1
fi

echo "$pkgs"
