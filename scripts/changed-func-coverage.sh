#!/usr/bin/env bash
# changed-func-coverage.sh — which functions did this diff touch that no test
# exercises?
#
# A REPORT, NOT A GATE. It always exits 0 when it can answer the question, and
# non-zero only when it cannot answer it at all. Nothing is blocked by what it
# finds. Making it a gate would get it switched off, and the number it prints
# is a prompt to look, not a threshold to clear.
#
# WHY IT EXISTS. Package coverage hides exactly the thing worth seeing.
# internal/keeper reads 79.2% while the function written to fix a data-loss bug
# reads 0.0%. The package figure moves by a fraction of a point when a new
# untested function lands, so nothing draws attention to it, and the function
# most likely to be wrong is the one nobody looks at. This asks the narrow
# question instead: of the functions in THIS diff, which are at 0.0%?
#
#   scripts/changed-func-coverage.sh [base-ref]
#
# base-ref defaults to HEAD~1, which is what `make fast` already means by
# "changed" — the golangci-lint step in gate-static uses --new-from-rev=HEAD~1.
# One name for one thing. Pass a merge-base to report on a whole lane.
#
# THE -coverpkg CAVEAT — read this before changing the go test invocation.
#
# `go test ./somepkg` instruments only somepkg and runs only somepkg's tests.
# A function exercised solely from another package's tests reads 0.0% under
# that invocation, and reporting it as untested is a false alarm. False alarms
# are how a report earns its way out of the loop, so this script passes
# -coverpkg with the full set of changed packages and runs the tests of those
# packages AND of every package that imports one of them. Coverage credit then
# follows the call, not the directory.
#
# What that costs, stated plainly, because the next person to widen it should
# know: -coverpkg instruments every named package for every test binary in the
# run, so the build is not shared with an ordinary `go test` and this is a
# second compile of that set. The run also grows with the importer set — a
# change to a widely imported package pulls in a lot of test binaries. It is
# proportional to the diff, not to the repo, which is why it is affordable at
# all. MEASURED, so nobody has to guess: a 14-file diff touching internal/daemon
# and internal/runloop took 10m17s wall. That is why this is a target you run,
# not a step in `make fast`.
#
# What it still does not catch: a function reached only from a package that
# does not import the changed one — through an interface satisfied elsewhere,
# or from a test tagged out of this run (the scenario tier is not in it). Such
# a function is reported at 0.0% and the reading is wrong. That is
# the residual false-alarm rate of the method and it is the reason this stays a
# report. Set CHANGED_FUNC_COVERAGE_WIDE=1 to run the whole module's tests
# instead, which removes the residual and costs a full instrumented suite.
#
# Bash 3.2 clean — no mapfile, no associative arrays. Stock macOS ships 3.2 and
# hk-038d1 is already one script in this repo that cannot run there.

set -uo pipefail

BASE="${1:-HEAD~1}"

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null)"
if [[ -z "${REPO_ROOT}" ]]; then
    echo "changed-func-coverage: not inside a git repository" >&2
    exit 2
fi
cd "${REPO_ROOT}" || exit 2

if ! git rev-parse --verify --quiet "${BASE}^{commit}" >/dev/null; then
    echo "changed-func-coverage: cannot resolve base ref '${BASE}'" >&2
    exit 2
fi

MODULE="$(go list -m 2>/dev/null | head -1)"
if [[ -z "${MODULE}" ]]; then
    echo "changed-func-coverage: no Go module here" >&2
    exit 2
fi

WORK="$(mktemp -d)"
trap 'rm -rf "${WORK}"' EXIT

echo "changed-function coverage — base ${BASE}"
echo ""

# ---------------------------------------------------------------------------
# 1. The changed production files, and the lines the diff touched in each.
#
# -U0 so a hunk header names only the lines that actually changed. Test files
# are excluded as SUBJECTS: editing a test does not make the functions around
# it newly untested. They still participate as coverage sources.
# ---------------------------------------------------------------------------
git diff --name-only --diff-filter=d "${BASE}...HEAD" -- '*.go' \
    | grep -v '_test\.go$' \
    | sort -u > "${WORK}/files"

if [[ ! -s "${WORK}/files" ]]; then
    echo "No changed Go files between ${BASE} and HEAD. Nothing to report."
    exit 0
fi

# changed-lines: "<file> <line>" per touched line.
: > "${WORK}/changed-lines"
while IFS= read -r f; do
    [[ -n "${f}" ]] || continue
    git diff -U0 "${BASE}...HEAD" -- "${f}" \
        | awk -v file="${f}" '
            /^@@/ {
                # @@ -old,cnt +new,cnt @@
                plus = $3
                sub(/^\+/, "", plus)
                split(plus, a, ",")
                start = a[1] + 0
                count = (a[2] == "" ? 1 : a[2] + 0)
                for (i = 0; i < count; i++) print file, start + i
            }'
done < "${WORK}/files" >> "${WORK}/changed-lines"

# ---------------------------------------------------------------------------
# 2. The packages those files live in, and the test set that exercises them.
# ---------------------------------------------------------------------------
# A file at the module root has no slash to strip, and its directory is ".".
sed -e 's|^[^/]*\.go$|.|' -e 's|/[^/]*\.go$||' "${WORK}/files" | sort -u > "${WORK}/dirs"

: > "${WORK}/pkgs"
while IFS= read -r d; do
    [[ -n "${d}" ]] || continue
    p="$(go list "./${d}" 2>/dev/null)"
    [[ -n "${p}" ]] && echo "${p}"
done < "${WORK}/dirs" | sort -u > "${WORK}/pkgs"

if [[ ! -s "${WORK}/pkgs" ]]; then
    echo "No buildable Go package holds the changed files. Nothing to report."
    exit 0
fi

COVERPKG="$(tr '\n' ',' < "${WORK}/pkgs" | sed 's/,$//')"

if [[ "${CHANGED_FUNC_COVERAGE_WIDE:-0}" == "1" ]]; then
    echo "./..." > "${WORK}/testpkgs"
else
    # The changed packages plus every package that imports one of them. See the
    # -coverpkg caveat in the header for what this buys and what it misses.
    cp "${WORK}/pkgs" "${WORK}/testpkgs"
    go list -e -f '{{.ImportPath}} {{join .Imports " "}} {{join .TestImports " "}} {{join .XTestImports " "}}' ./... 2>/dev/null \
        | awk -v changed="$(tr '\n' ' ' < "${WORK}/pkgs")" '
            BEGIN { n = split(changed, c, " ") }
            {
                for (i = 2; i <= NF; i++)
                    for (j = 1; j <= n; j++)
                        if (c[j] != "" && $i == c[j]) { print $1; next }
            }' >> "${WORK}/testpkgs"
    sort -u "${WORK}/testpkgs" -o "${WORK}/testpkgs"
fi

# ---------------------------------------------------------------------------
# 3. Measure.
# ---------------------------------------------------------------------------
PROFILE="${WORK}/cover.out"
test_args=""
while IFS= read -r p; do
    [[ -n "${p}" ]] && test_args="${test_args} ${p}"
done < "${WORK}/testpkgs"

: > "${WORK}/failedpkgs"

# shellcheck disable=SC2086
if ! go test -count=1 -covermode=set -coverpkg="${COVERPKG}" \
        -coverprofile="${PROFILE}" ${test_args} > "${WORK}/gotest.log" 2>&1; then
    # A failing test still writes a profile for the packages that passed. Carry
    # on rather than refuse: a red suite is exactly when someone wants this.
    #
    # But NAME the packages. In a package whose tests failed, a 0.0% may mean
    # only that the test never got to run, and a reader who has to work that
    # out for themselves will either distrust the whole report or trust the
    # wrong line of it. This was not hypothetical — the first real run of this
    # script reported two functions in internal/daemon, both of which do have
    # tests, in a run where internal/daemon was red.
    awk '/^FAIL[ \t]+[^ \t]+\// {print $2}' "${WORK}/gotest.log" \
        | sort -u > "${WORK}/failedpkgs"

    echo "NOTE: the test run was not clean, so a 0.0% below may mean only that"
    echo "      the test never ran. Tests failed in:"
    if [[ -s "${WORK}/failedpkgs" ]]; then
        sed 's/^/        /' "${WORK}/failedpkgs"
    else
        echo "        (go test failed without naming a package — see the build error)"
        sed 's/^/        | /' "${WORK}/gotest.log" | tail -5
    fi
    echo ""
fi

if [[ ! -s "${PROFILE}" ]]; then
    echo "changed-func-coverage: no coverage profile was produced" >&2
    sed 's/^/  | /' "${WORK}/gotest.log" >&2
    exit 2
fi

# ---------------------------------------------------------------------------
# 4. Package totals, for the contrast that is the whole point.
# ---------------------------------------------------------------------------
echo "PACKAGE COVERAGE"
while IFS= read -r p; do
    [[ -n "${p}" ]] || continue
    {
        head -1 "${PROFILE}"
        grep "^${p}/[^/]*\.go:" "${PROFILE}" || true
    } > "${WORK}/filtered"
    if [[ "$(wc -l < "${WORK}/filtered")" -le 1 ]]; then
        printf '%-60s %s\n' "${p}" "no statements"
        continue
    fi
    pct="$(go tool cover -func="${WORK}/filtered" 2>/dev/null | awk '/^total:/ {print $3}')"
    printf '%-60s %s\n' "${p}" "${pct:-unknown}"
done < "${WORK}/pkgs"
echo ""

# ---------------------------------------------------------------------------
# 5. Map each changed line to the function whose body holds it.
#
# `go tool cover -func` gives one line per function: <file>:<line>: <name> <pct>
# It reports the START line only. Within a file, functions come out in source
# order, so a function's span runs to the line before the next function starts,
# and the last one runs to end of file. A changed line inside that span belongs
# to that function. Getting this wrong by one function is why the self-test
# changes a function on its LAST body line.
# ---------------------------------------------------------------------------
go tool cover -func="${PROFILE}" 2>/dev/null | grep -v '^total:' > "${WORK}/funcs"

# funcs lines look like:
#   /abs/or/module/path/pkg/file.go:12:  FuncName   0.0%
# Normalize the file to a repo-relative path so it joins with the diff.
awk -v module="${MODULE}/" '
    {
        loc = $1
        sub(/:$/, "", $3)
        split(loc, part, ":")
        file = part[1]
        line = part[2] + 0
        sub("^" module, "", file)
        print file, line, $2, $3
    }' "${WORK}/funcs" \
    | sort -k1,1 -k2,2n > "${WORK}/funcs.norm"

# The sort above is load-bearing: the span of a function is defined by where
# the NEXT one starts, so the starts have to arrive in ascending order per file.
#
# Walk the changed lines of a file against each function's span rather than
# walking the span itself. The last function in a file has no next start and so
# no upper bound, and iterating that span counts to its sentinel — which is how
# the first version of this hung.
awk '
    NR == FNR { n[$1] = n[$1] + 1; L[$1, n[$1]] = $2 + 0; next }
    {
        file = $1
        idx[file] = idx[file] + 1
        F[file, idx[file]] = $2 + 0
        N[file, idx[file]] = $3
        P[file, idx[file]] = $4
    }
    END {
        for (file in idx) {
            count = idx[file]
            touched = n[file] + 0
            if (touched == 0) continue
            for (i = 1; i <= count; i++) {
                if (P[file, i] != "0.0%") continue
                start   = F[file, i]
                bounded = (i < count)
                end     = (bounded ? F[file, i + 1] - 1 : 0)
                for (k = 1; k <= touched; k++) {
                    line = L[file, k]
                    if (line < start) continue
                    if (bounded && line > end) continue
                    printf "%s:%d %s %s\n", file, start, P[file, i], N[file, i]
                    break
                }
            }
        }
    }' "${WORK}/changed-lines" "${WORK}/funcs.norm" | sort > "${WORK}/zeros"

echo "ZERO-COVERAGE FUNCTIONS THIS DIFF TOUCHED"
if [[ ! -s "${WORK}/zeros" ]]; then
    echo "None. Every function this diff touched is exercised by some test."
    exit 0
fi

# Repo-relative directories whose tests failed, so each finding can carry its
# own caveat instead of the reader holding the NOTE in their head.
sed "s|^${MODULE}/||" "${WORK}/failedpkgs" > "${WORK}/faileddirs" 2>/dev/null || : > "${WORK}/faileddirs"

suspect=0
while IFS=' ' read -r loc pct name; do
    dir="$(echo "${loc}" | sed -e 's|:[0-9]*$||' -e 's|/[^/]*$||')"
    mark="  "
    if [[ -s "${WORK}/faileddirs" ]] && grep -qx "${dir}" "${WORK}/faileddirs"; then
        mark="? "
        suspect=$((suspect + 1))
    fi
    printf '%s%-50s %-7s %s\n' "${mark}" "${loc}" "${pct}" "${name}"
done < "${WORK}/zeros"

if [[ ${suspect} -gt 0 ]]; then
    echo ""
    echo "?  its package's tests failed in this run, so the 0.0% may mean the test"
    echo "   never ran rather than that the function has none. Check before acting."
fi

echo ""
count="$(wc -l < "${WORK}/zeros" | tr -d ' ')"
if [[ "${count}" == "1" ]]; then
    echo "1 function this diff touched is exercised by no test in this run."
else
    echo "${count} functions this diff touched are exercised by no test in this run."
fi
echo "Read the -coverpkg caveat at the top of this script before acting on that."

exit 0
