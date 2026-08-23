#!/usr/bin/env bash
# comment-only-commit-gate-test.sh — self-test for the comment-volume ratchet.
#
# The classifier is the load-bearing part of that gate, and its expensive
# failure is a FALSE POSITIVE: refusing real work. Three of the cases below
# (build tag, directive, spec sync) are regressions the first draft actually
# had, caught in review against commit 327918085.

set -uo pipefail
export LC_ALL=C

gate=$(cd "$(dirname "$0")" && pwd)/comment-only-commit-gate.sh
[ -x "$gate" ] || { echo "comment-only-commit-gate-test: no gate at $gate" >&2; exit 2; }

work=$(mktemp -d) || { echo "comment-only-commit-gate-test: no temp dir" >&2; exit 2; }
trap 'rm -rf "$work"' EXIT

pass=0
fail=0

# check <name> <want-exit> — the repo is already staged; run the gate in it.
check() {
    local name="$1" want="$2" got
    ( cd "$work/repo" && COMMENT_ONLY_GATE_BASELINE="$(git rev-list --max-parents=0 HEAD)" "$gate" ) >/dev/null 2>&1
    got=$?
    if [ "$got" = "$want" ]; then
        pass=$((pass + 1))
        printf '  ok    %s (exit %s)\n' "$name" "$got"
    else
        fail=$((fail + 1))
        printf '  FAIL  %s: want exit %s, got %s\n' "$name" "$want" "$got"
    fi
}

# fresh <body...> — a scratch repo whose HEAD holds a baseline Go file.
fresh() {
    rm -rf "$work/repo"
    mkdir -p "$work/repo"
    cd "$work/repo" || exit 2
    git init -q .
    git config user.email t@t; git config user.name t
    mkdir -p specs
    cat > a.go <<'EOF'
package a

// Doc is a thing.
func Doc() int {
	x := 1
	return x
}
EOF
    echo "spec" > specs/s.md
    git add -A && git commit -qm base
}

echo "comment-only-commit-gate-test:"

# 1. A comment-only edit is refused. This is the whole point.
fresh
sed -i.bak 's|// Doc is a thing.|// Doc is a thing that does something.|' a.go && rm -f a.go.bak
check "comment-only edit is refused" 1

# 2. A comment edit that comes with a code edit passes.
fresh
sed -i.bak -e 's|// Doc is a thing.|// Doc is a better thing.|' -e 's|x := 1|x := 2|' a.go && rm -f a.go.bak
check "comment plus code passes" 0

# 3. A net deletion passes, so a commentcut sweep stays legal.
fresh
sed -i.bak '/\/\/ Doc is a thing./d' a.go && rm -f a.go.bak
check "net comment deletion passes" 0

# 4. A build tag is CODE, not comment. Regression guard: commit 327918085
#    added 129 //go:build lines and no other Go line.
fresh
printf '//go:build specaudit\n\n%s' "$(cat a.go)" > a.go.new && mv a.go.new a.go
check "build-tag-only change passes" 0

# 5. //nolint is a directive, so it is code too.
fresh
sed -i.bak 's|	return x|	//nolint:gosec // deliberate\n	return x|' a.go && rm -f a.go.bak
check "nolint directive passes" 0

# 6. A spec amendment that syncs the Go comment citing it is one piece of work.
fresh
sed -i.bak 's|// Doc is a thing.|// Doc is a thing (specs/s.md).|' a.go && rm -f a.go.bak
echo "spec amended" >> specs/s.md
check "spec change plus comment sync passes" 0

# 7. A re-indent is a code change, not a comment change.
fresh
sed -i.bak 's|	x := 1|		x := 1|' a.go && rm -f a.go.bak
check "re-indent passes" 0

# 8. A clean tree says nothing.
fresh
check "clean tree passes" 0

printf 'comment-only-commit-gate-test: %d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ] || exit 1
