#!/usr/bin/env bash
# changed-func-coverage-test.sh — self-test for scripts/changed-func-coverage.sh.
#
# The report under test answers one question: of the functions this diff
# touched, which ones does no test exercise? Package granularity cannot answer
# it. internal/keeper reads 79.2% while the function written to fix a data-loss
# bug reads 0.0%, and the package number hides that completely.
#
# Every assertion below is paired with a mutation that a wrong implementation
# would pass. The mutations are listed in the header of each section. A report
# that lists every zero-coverage function in the package would pass "the
# changed function is listed" and fail "the unchanged one is not". A report
# built on naive per-package `go test` would pass both and fail the
# cross-package section, because coverage credit for a function exercised only
# from another package's tests needs -coverpkg to be visible at all.
#
# Runs in a throwaway git repo with a throwaway Go module. Nothing here touches
# the harmonik repo.

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCRIPT="${REPO_ROOT}/scripts/changed-func-coverage.sh"

PASS=0
FAIL=0

pass() { printf '  ok   %s\n' "$1"; PASS=$((PASS + 1)); }
fail() { printf '  FAIL %s\n' "$1"; FAIL=$((FAIL + 1)); }

# A missing or crashed script must not produce green assertions. Every
# "is not reported" check below passes trivially against an empty report, and
# the two bash-version greps pass trivially against a file that is not there.
# That is the same fail-open shape as a review gate that approves because it
# could not run. Refuse to score the run at all unless the report is real.
die() { printf '\nchanged-func-coverage-test: %s\n' "$1" >&2; exit 1; }

[[ -f "${SCRIPT}" ]] || die "scripts/changed-func-coverage.sh does not exist"
[[ -x "${SCRIPT}" ]] || die "scripts/changed-func-coverage.sh is not executable"

# run_report <outfile> <base-ref> — run the report and set REPORT_EXIT.
# Anything that is not a report ends the run rather than scoring it.
#
# The watchdog is not decoration. The first version of the report walked a
# function's span line by line, and the last function in a file has no next
# start to bound it, so it counted toward a sentinel. Without a deadline that
# reads as a slow test rather than a defect.
run_report() {
    "${SCRIPT}" "$2" > "$1" 2>&1 &
    local pid=$!
    local waited=0
    while kill -0 "${pid}" 2>/dev/null; do
        if [[ ${waited} -ge 120 ]]; then
            kill -9 "${pid}" 2>/dev/null
            die "the report did not finish within 120s on a three-file module — it is hung"
        fi
        sleep 1
        waited=$((waited + 1))
    done
    wait "${pid}"
    REPORT_EXIT=$?
    if [[ ${REPORT_EXIT} -eq 127 ]]; then
        die "the report could not be executed at all (exit 127)"
    fi
    if ! grep -q '^changed-function coverage' "$1"; then
        printf '  report was:\n' >&2
        sed 's/^/  | /' "$1" >&2
        die "output is not a report — it has no 'changed-function coverage' header"
    fi
}

# assert_reported <label> <report-file> <function-name>
# The function must appear in the zero-coverage section of the report.
#
# The section is captured and matched with a here-string rather than piped into
# `grep -q`. Under pipefail a piped `grep -q` leaves on the first match, the awk
# feeding it dies of SIGPIPE, and pipefail returns that death as the pipeline's
# status — so a MATCH arrives here as a miss. It flips the sense of both of these
# helpers, and for assert_not_reported that means an assertion that always passes.
assert_reported() {
    local section
    section="$(zero_section "$2")"
    if grep -q "[^A-Za-z0-9_]$3\$" <<<"$section"; then
        pass "$1"
    else
        fail "$1 — expected $3 in the zero-coverage section"
        printf '       report was:\n'
        sed 's/^/       | /' "$2"
    fi
}

# assert_not_reported <label> <report-file> <function-name>
assert_not_reported() {
    local section
    section="$(zero_section "$2")"
    if grep -q "[^A-Za-z0-9_]$3\$" <<<"$section"; then
        fail "$1 — $3 must NOT be in the zero-coverage section"
        printf '       report was:\n'
        sed 's/^/       | /' "$2"
    else
        pass "$1"
    fi
}

# zero_section <report-file> — the lines listing zero-coverage functions.
# Each such line ends in the function name, which is what the assertions match.
zero_section() {
    awk '/^ZERO-COVERAGE FUNCTIONS/ {inside = 1; next} inside' "$1"
}

WORK="$(mktemp -d)"
trap 'rm -rf "${WORK}"' EXIT

# ---------------------------------------------------------------------------
# Fixture. Two packages so the cross-package case is real.
#
#   p.Covered    — exercised by p's own test
#   p.Untouched  — exercised by nothing
#   p.Later      — exercised by nothing, and the diff does not touch it
#   q.FromP      — exercised ONLY by p's test, never by q's
#   q.Nobody     — exercised by nothing
# ---------------------------------------------------------------------------
mkdir -p "${WORK}/p" "${WORK}/q"
cd "${WORK}" || exit 1

cat > go.mod <<'EOF'
module example.com/x

go 1.21
EOF

cat > q/q.go <<'EOF'
package q

func FromP(n int) int {
	return n + 1
}

func Nobody(n int) int {
	return n - 1
}
EOF

cat > p/p.go <<'EOF'
package p

import "example.com/x/q"

func Covered(n int) int {
	return q.FromP(n)
}

func Untouched(n int) int {
	doubled := n * 2
	return doubled
}

func Later(n int) int {
	return n * 3
}
EOF

cat > p/p_test.go <<'EOF'
package p

import "testing"

func TestCovered(t *testing.T) {
	if got := Covered(1); got != 2 {
		t.Fatalf("got %d", got)
	}
}
EOF

cat > q/q_test.go <<'EOF'
package q

import "testing"

func TestNothingHere(t *testing.T) {
	_ = t
}
EOF

git init -q .
git config user.email test@example.com
git config user.name test
git add -A
git commit -qm base

# The diff. Untouched changes on its LAST body line, which is the case a
# start-line-only mapping gets wrong: an implementation that attributes a line
# to the next function down would blame Later instead.
cat > p/p.go <<'EOF'
package p

import "example.com/x/q"

func Covered(n int) int {
	return q.FromP(n) + 0
}

func Untouched(n int) int {
	doubled := n * 2
	return doubled + 0
}

func Later(n int) int {
	return n * 3
}
EOF

git add -A
git commit -qm change

# ---------------------------------------------------------------------------
# Section 1 — the report names the changed function no test reaches.
#
# Mutations this rejects:
#   - listing every zero-coverage function in the package (Later appears)
#   - listing every changed function regardless of coverage (Covered appears)
#   - attributing a changed line to the following function (Later, not
#     Untouched, is named)
# ---------------------------------------------------------------------------
printf 'changed-func-coverage: the report\n'

run_report "${WORK}/report.txt" HEAD~1
report_exit=${REPORT_EXIT}

assert_reported     "a changed function with no test is reported" "${WORK}/report.txt" Untouched
assert_not_reported "a zero-coverage function the diff did not touch is not reported" "${WORK}/report.txt" Later
assert_not_reported "a changed function that is covered is not reported" "${WORK}/report.txt" Covered

# ---------------------------------------------------------------------------
# Section 2 — it is a report, not a gate.
#
# Mutation this rejects: exiting non-zero on findings, which would make it a
# gate and get it switched off.
# ---------------------------------------------------------------------------
if [[ ${report_exit} -eq 0 ]]; then
    pass "exits 0 with findings — a report, not a gate"
else
    fail "exits ${report_exit} with findings; a report must exit 0"
fi

# ---------------------------------------------------------------------------
# Section 3 — the package number is shown beside the function numbers.
#
# The whole reason this tool exists is the gap between the two. A report that
# prints only function lines makes the reader go and find the package figure,
# and the contrast is the finding.
#
# Mutation this rejects: dropping the package coverage line.
# ---------------------------------------------------------------------------
# The figure has to be under its heading, not merely somewhere in the output.
# A version that printed the number with nothing naming it survived an earlier
# form of this check.
pkg_section() {
    awk '/^PACKAGE COVERAGE/ {inside = 1; next} /^$/ {inside = 0} inside' "${WORK}/report.txt"
}
pkg_lines="$(pkg_section)"
if grep -q '^PACKAGE COVERAGE' "${WORK}/report.txt" \
   && grep -qE '^example\.com/x/p +[0-9]+\.[0-9]+%' <<<"${pkg_lines}"; then
    pass "package coverage is printed beside the function findings"
else
    fail "no PACKAGE COVERAGE section naming example.com/x/p and its percentage"
    sed 's/^/       | /' "${WORK}/report.txt"
fi

# ---------------------------------------------------------------------------
# Section 4 — cross-package coverage credit. THE discriminator.
#
# q.FromP is exercised only by p's test. Under `go test ./q` with no -coverpkg,
# q is instrumented but only q's own tests run against it, so FromP reads 0.0%
# and a naive implementation reports it as untested. It is tested. The report
# must widen instrumentation across the changed set, and q.Nobody must still be
# reported so the widening is not just a blanket amnesty.
#
# Mutations this rejects:
#   - per-package `go test` with no -coverpkg (FromP falsely reported)
#   - treating every cross-package function as covered (Nobody disappears)
# ---------------------------------------------------------------------------
printf 'changed-func-coverage: cross-package credit\n'

cat > q/q.go <<'EOF'
package q

func FromP(n int) int {
	return n + 1 + 0
}

func Nobody(n int) int {
	return n - 1 + 0
}
EOF

git add -A
git commit -qm cross

run_report "${WORK}/cross.txt" HEAD~1

assert_not_reported "a function exercised only from another package's test is not reported" "${WORK}/cross.txt" FromP
assert_reported     "a function no test reaches is still reported when instrumentation widens" "${WORK}/cross.txt" Nobody

# ---------------------------------------------------------------------------
# Section 5 — nothing to say, said clearly.
#
# Mutation this rejects: a crash, or silence, when the diff has no Go in it. A
# report that dies on a docs-only commit gets removed from the loop.
# ---------------------------------------------------------------------------
printf 'changed-func-coverage: empty and test-only diffs\n'

printf 'notes\n' > README.md
git add -A
git commit -qm docs

run_report "${WORK}/docs.txt" HEAD~1
docs_exit=${REPORT_EXIT}

if [[ ${docs_exit} -eq 0 ]] && grep -qi 'no changed go' "${WORK}/docs.txt"; then
    pass "a diff with no Go files says so and exits 0"
else
    fail "a docs-only diff must exit 0 and say there is no Go to report on (exit ${docs_exit})"
    sed 's/^/       | /' "${WORK}/docs.txt"
fi

# A test-only change touches no production function. The report must not blame
# the package's untested functions for it.
cat > p/p_test.go <<'EOF'
package p

import "testing"

func TestCovered(t *testing.T) {
	if got := Covered(1); got != 2 {
		t.Fatalf("got %d", got)
	}
	if got := Covered(2); got != 3 {
		t.Fatalf("got %d", got)
	}
}
EOF

git add -A
git commit -qm testonly

run_report "${WORK}/testonly.txt" HEAD~1
testonly_exit=${REPORT_EXIT}

assert_not_reported "a test-only diff does not blame production functions" "${WORK}/testonly.txt" Untouched
if [[ ${testonly_exit} -eq 0 ]]; then
    pass "a test-only diff exits 0"
else
    fail "a test-only diff exited ${testonly_exit}"
fi

# ---------------------------------------------------------------------------
# Section 6 — a red package's findings carry their own caveat.
#
# In a package whose tests failed, 0.0% can mean the test never ran. The first
# real run of this report named two functions in a red internal/daemon, and
# both of them do have tests. A reader who has to work that out unaided will
# either distrust the whole report or trust the wrong line of it.
#
# Mutations this rejects:
#   - reporting a red package's zeros with no distinction from a green one's
#   - refusing to report at all when the suite is red, which is exactly when
#     someone wants the report
# ---------------------------------------------------------------------------
printf 'changed-func-coverage: findings from a red package are marked\n'

cat > q/q_test.go <<'EOF'
package q

import "testing"

func TestDeliberatelyRed(t *testing.T) {
	t.Fatal("this test fails on purpose")
}
EOF

cat > q/q.go <<'EOF'
package q

func FromP(n int) int {
	return n + 1 + 0
}

func Nobody(n int) int {
	return n - 1 + 0 + 0
}
EOF

git add -A
git commit -qm redpkg

run_report "${WORK}/red.txt" HEAD~1

assert_reported "a red package's zero-coverage function is still reported" "${WORK}/red.txt" Nobody

if grep -q 'example.com/x/q' "${WORK}/red.txt" && grep -qi 'tests failed' "${WORK}/red.txt"; then
    pass "the report names the package whose tests failed"
else
    fail "the report does not name example.com/x/q as having failed"
    sed 's/^/       | /' "${WORK}/red.txt"
fi

red_zero="$(zero_section "${WORK}/red.txt")"
if grep -qE '^\? .*Nobody$' <<<"${red_zero}"; then
    pass "the finding itself is marked as coming from a red package"
else
    fail "the Nobody finding is not marked, so it reads as a confirmed zero"
    sed 's/^/       | /' "${WORK}/red.txt"
fi

# Restore a green q for anything downstream.
cat > q/q_test.go <<'EOF'
package q

import "testing"

func TestNothingHere(t *testing.T) {
	_ = t
}
EOF
git add -A
git commit -qm greenagain

# ---------------------------------------------------------------------------
# Section 7 — the -coverpkg caveat is written down.
#
# Section 4 pins the behaviour. This pins the explanation, because the next
# person to widen or narrow the instrumentation needs to know what the choice
# costs. Required by the work item.
#
# Mutation this rejects: implementing the widening and documenting nothing.
# ---------------------------------------------------------------------------
printf 'changed-func-coverage: the caveat is documented\n'

if grep -q 'coverpkg' "${SCRIPT}"; then
    pass "the script mentions -coverpkg"
else
    fail "the script never mentions -coverpkg"
fi

caveat="$(awk '/^#/ {print} !/^#/ {exit}' "${SCRIPT}" | grep -c 'coverpkg')"
if [[ "${caveat}" -ge 2 ]]; then
    pass "the -coverpkg caveat is explained in the header, not just used"
else
    fail "the header comment does not explain the -coverpkg trade-off (${caveat} mention(s))"
fi

# ---------------------------------------------------------------------------
# Section 8 — bash 3.2. Stock macOS ships it and mapfile / declare -A are
# bash 4. hk-038d1 is the same defect in make script-tests; do not add another.
#
# Mutation this rejects: reaching for mapfile or an associative array because
# the developer's own bash is new enough not to notice.
# ---------------------------------------------------------------------------
printf 'changed-func-coverage: runs on the bash macOS ships\n'

# Comment lines are stripped first. The header explains why these constructs
# are avoided, and naming a thing must not count as using it — otherwise the
# cheapest way to go green is to delete the explanation.
code_only() { grep -v '^[[:space:]]*#' "${SCRIPT}"; }

# Capture once, then match the capture. These two are a MERGE GATE, and piped
# into `grep -q` they are a gate that cannot block: grep leaves the instant it
# sees a `mapfile`, the `grep -v` upstream dies of SIGPIPE still writing, and
# pipefail reports that death — which reads here as "the construct is not
# present". Measured on this box against a copy of the subject script with
# `mapfile` inserted at the top: at 6.6 KB of non-comment code the piped form
# still detects it, at 65 KB it reports the script clean (exit 141). The subject
# is 6.6 KB today, so the gate works today and fails open as soon as it grows.
code="$(code_only)"

if grep -qE '(^|[^[:alnum:]_])(mapfile|readarray)([^[:alnum:]_]|$)' <<<"${code}"; then
    fail "uses mapfile/readarray — bash 4 only, and stock macOS bash is 3.2"
else
    pass "no mapfile/readarray"
fi

if grep -qE 'declare +-[A-Za-z]*A|local +-[A-Za-z]*A' <<<"${code}"; then
    fail "uses an associative array — bash 4 only"
else
    pass "no associative arrays"
fi

# And the check itself has to be able to fail. A grep that never matches is
# indistinguishable from a clean script. Prove it matches a script that does
# use the construct.
probe="${WORK}/bash4-probe.sh"
printf '#!/usr/bin/env bash\nmapfile -t x < /dev/null\ndeclare -A y\n' > "${probe}"
if grep -qE '(^|[^[:alnum:]_])(mapfile|readarray)([^[:alnum:]_]|$)' "${probe}" \
   && grep -qE 'declare +-[A-Za-z]*A|local +-[A-Za-z]*A' "${probe}"; then
    pass "the bash-4 checks detect the constructs they look for"
else
    fail "the bash-4 checks pass a script that plainly uses mapfile and declare -A"
fi

# ---------------------------------------------------------------------------
printf '\nchanged-func-coverage-test: %d passed, %d failed\n' "${PASS}" "${FAIL}"
[[ ${FAIL} -eq 0 ]] || exit 1
exit 0
