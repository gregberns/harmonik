#!/usr/bin/env bash
#
# scenario-pkgs-test.sh — assertions for scripts/scenario-pkgs.sh.
#
# scenario-pkgs.sh decides which packages `make test-scenario` runs, and
# test-scenario is a step of `make full`, the merge decision. Two ways it can go
# wrong are both silent:
#
#   IT PRINTS TOO MUCH. The first version grepped the filesystem. This repo keeps
#   agent worktrees at `.claude/worktrees/<name>/`, each a full second copy of
#   the tree, so on the main checkout it emitted 280 file paths inside those
#   copies. `go test` rejects a path outside the main module, so `make full` died
#   on the operator's own checkout while passing in CI and inside a worktree.
#   Case 2 below is that regression.
#
#   IT PRINTS TOO LITTLE. An empty list makes the scenario tier pass by running
#   nothing, which is the exact false-green this project keeps finding. Case 3
#   holds the fail-closed behaviour.
#
# Refs hk-od9d4.
set -uo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="$repo_root/scripts/scenario-pkgs.sh"

assertions=0
failures=0

pass() {
	assertions=$((assertions + 1))
	echo "scenario-pkgs-test: ok: $1"
}

fail() {
	assertions=$((assertions + 1))
	failures=$((failures + 1))
	echo "scenario-pkgs-test: FAIL: $1"
}

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# ---------------------------------------------------------------------------
# Case 1 — on this checkout it prints a non-empty list, and every entry is a
# real package of THIS module. `go list` is the judge, because that is what
# `go test` will do with the same argument.
# ---------------------------------------------------------------------------
out="$work/real.out"
if "$script" >"$out" 2>"$work/real.err"; then
	if [ -s "$out" ]; then
		pass "this checkout: prints a non-empty package list ($(wc -l <"$out" | tr -d ' ') packages)"
	else
		fail "this checkout: exited 0 but printed nothing"
	fi

	# NO `-e` HERE, and that is the whole point of the check. `go list -e` reports
	# a broken or absent package instead of failing, so it exits 0 for any string
	# at all — including the nested-worktree path this assertion exists to catch.
	# With -e the check could never fail. Without it, `go list` is a real judge:
	# it exits non-zero on a path that is not a package of this module, which is
	# exactly what `go test` will do with the same argument.
	bad=""
	while read -r pkg; do
		[ -n "$pkg" ] || continue
		if ! (cd "$repo_root" && go list -tags scenario "$pkg" >/dev/null 2>&1); then
			bad="$bad $pkg"
		fi
	done <"$out"
	if [ -z "$bad" ]; then
		pass "this checkout: every printed path is a package go list accepts"
	else
		fail "this checkout: go list rejects:$bad"
	fi

	if grep -q '\.claude/worktrees' "$out"; then
		fail "this checkout: printed a path inside .claude/worktrees"
	else
		pass "this checkout: printed no path inside .claude/worktrees"
	fi
else
	fail "this checkout: script exited non-zero ($(cat "$work/real.err"))"
fi

# ---------------------------------------------------------------------------
# Case 2 — THE REGRESSION. A module with a second, complete checkout nested
# inside it. The nested copy carries a scenario file. The script must report the
# outer module's package and MUST NOT report the nested one.
# ---------------------------------------------------------------------------
fixture="$work/outer"
mkdir -p "$fixture/scripts" "$fixture/pkga" "$fixture/.claude/worktrees/wt-a/pkga"

cat >"$fixture/go.mod" <<'EOF'
module example.com/outer

go 1.21
EOF

cat >"$fixture/pkga/a.go" <<'EOF'
package pkga

func A() int { return 1 }
EOF

cat >"$fixture/pkga/scn_test.go" <<'EOF'
//go:build scenario

package pkga

import "testing"

func TestScenarioOuter(t *testing.T) {}
EOF

# The nested checkout is a full second module, exactly like a git worktree here.
cat >"$fixture/.claude/worktrees/wt-a/go.mod" <<'EOF'
module example.com/outer

go 1.21
EOF
cp "$fixture/pkga/a.go" "$fixture/.claude/worktrees/wt-a/pkga/a.go"
cp "$fixture/pkga/scn_test.go" "$fixture/.claude/worktrees/wt-a/pkga/scn_test.go"

cp "$script" "$fixture/scripts/scenario-pkgs.sh"
chmod +x "$fixture/scripts/scenario-pkgs.sh"

nested_out="$work/nested.out"
if "$fixture/scripts/scenario-pkgs.sh" >"$nested_out" 2>"$work/nested.err"; then
	if grep -q 'worktrees' "$nested_out"; then
		fail "nested checkout: leaked a package path from inside .claude/worktrees — $(tr '\n' ' ' <"$nested_out")"
	else
		pass "nested checkout: no package path from inside .claude/worktrees"
	fi
	if grep -qx './pkga' "$nested_out"; then
		pass "nested checkout: still found the outer module's own scenario package"
	else
		fail "nested checkout: lost the outer module's scenario package (got '$(tr '\n' ' ' <"$nested_out")')"
	fi
else
	fail "nested checkout: script exited non-zero ($(cat "$work/nested.err"))"
fi

# ---------------------------------------------------------------------------
# Case 2b — TAG SHAPES A TEXT MATCH GETS WRONG. Both of these are scenario files
# and neither equals the string "//go:build scenario" in the first few lines:
# a compound constraint, and a constraint below a long header comment. A grep for
# the literal tag line silently drops the package, which is the same false green
# this script exists to prevent. Asking the compiler gets both right.
# ---------------------------------------------------------------------------
shape_case() {
	label="$1"
	dir="$work/shape-$2"
	mkdir -p "$dir/scripts" "$dir/pkga"
	printf 'module example.com/shape%s\n\ngo 1.21\n' "$2" >"$dir/go.mod"
	printf 'package pkga\n\nfunc A() int { return 1 }\n' >"$dir/pkga/a.go"
	cat >"$dir/pkga/scn_test.go"
	cp "$script" "$dir/scripts/scenario-pkgs.sh"
	chmod +x "$dir/scripts/scenario-pkgs.sh"

	shape_out="$work/shape-$2.out"
	if "$dir/scripts/scenario-pkgs.sh" >"$shape_out" 2>/dev/null && grep -qx './pkga' "$shape_out"; then
		pass "$label: found the scenario package"
	else
		fail "$label: did not find the scenario package (got '$(tr '\n' ' ' <"$shape_out")')"
	fi
}

shape_case "compound tag" "compound" <<'EOF'
//go:build scenario && !windows

package pkga

import "testing"

func TestCompound(t *testing.T) {}
EOF

shape_case "tag below a header comment" "header" <<'EOF'
// header line 1
// header line 2
// header line 3
// header line 4
// header line 5
// header line 6

//go:build scenario

package pkga

import "testing"

func TestHeader(t *testing.T) {}
EOF

# ---------------------------------------------------------------------------
# Case 2c — A NEGATION IS NOT A SCENARIO PACKAGE. `//go:build !scenario` turns
# files OFF under the tag, so the package has nothing for this tier to run and
# must not be listed. Guarded because the fix for the two cases above is to look
# for a file-set CHANGE, and a change is also what a negation produces.
# ---------------------------------------------------------------------------
negate="$work/negate"
mkdir -p "$negate/scripts" "$negate/pkga"
printf 'module example.com/negate\n\ngo 1.21\n' >"$negate/go.mod"
printf 'package pkga\n\nfunc A() int { return 1 }\n' >"$negate/pkga/a.go"
cat >"$negate/pkga/scn_test.go" <<'EOF'
//go:build !scenario

package pkga

import "testing"

func TestNegated(t *testing.T) {}
EOF
cp "$script" "$negate/scripts/scenario-pkgs.sh"
chmod +x "$negate/scripts/scenario-pkgs.sh"

negate_out="$work/negate.out"
"$negate/scripts/scenario-pkgs.sh" >"$negate_out" 2>/dev/null
if [ -s "$negate_out" ]; then
	fail "negated tag: listed a package the scenario tag turns nothing on in — $(tr '\n' ' ' <"$negate_out")"
else
	pass "negated tag: listed no package"
fi

# ---------------------------------------------------------------------------
# Case 3 — FAIL CLOSED. A module with no scenario file anywhere must make the
# script exit non-zero AND print nothing on stdout. A zero exit with an empty
# list would let `make test-scenario` run no packages and report success.
# ---------------------------------------------------------------------------
empty="$work/empty"
mkdir -p "$empty/scripts" "$empty/pkgb"

cat >"$empty/go.mod" <<'EOF'
module example.com/empty

go 1.21
EOF

cat >"$empty/pkgb/b.go" <<'EOF'
package pkgb

func B() int { return 2 }
EOF

cp "$script" "$empty/scripts/scenario-pkgs.sh"
chmod +x "$empty/scripts/scenario-pkgs.sh"

empty_out="$work/empty.out"
empty_err="$work/empty.err"
"$empty/scripts/scenario-pkgs.sh" >"$empty_out" 2>"$empty_err"
empty_status=$?

if [ "$empty_status" -ne 0 ]; then
	pass "no scenario file: exits non-zero (got $empty_status)"
else
	fail "no scenario file: exited 0, so an empty tier would read as a pass"
fi

if [ -s "$empty_out" ]; then
	fail "no scenario file: printed a package list on stdout — $(tr '\n' ' ' <"$empty_out")"
else
	pass "no scenario file: printed nothing on stdout"
fi

if [ -s "$empty_err" ]; then
	pass "no scenario file: said why on stderr"
else
	fail "no scenario file: failed silently, with no diagnostic"
fi

# ---------------------------------------------------------------------------
echo "scenario-pkgs-test: $assertions assertions, $failures failed"
[ "$failures" -eq 0 ] || exit 1
