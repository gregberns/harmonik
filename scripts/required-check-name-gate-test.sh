#!/usr/bin/env bash
# required-check-name-gate-test.sh — self-test for required-check-name-gate.sh.
#
# The gate's whole job is to go red when the merge-gating CI job stops being
# able to report the status context branch protection requires. So the case
# that matters here is the FAILING one: a gate that has only ever been seen to
# pass proves nothing.
#
# Every refusal is checked twice — the exit code AND the reason the gate gave.
# A check that refuses the right file for the wrong reason reads as evidence
# for a rule that never ran, and this suite had exactly that hole: deleting the
# gate's file-exists guard reddened nothing, because a different branch of the
# gate happened to refuse the same fixture.
#
# Every refusal rule also has a passing twin that differs only in the thing
# under test, so deleting a rule from the gate turns a fixture green and this
# suite goes red.
#
# Exit codes are read from the variable the gate's own process set, never from
# a pipeline. Nothing here writes `producer | grep -q PATTERN`: grep -q exits
# at the first match, the writer takes SIGPIPE, and pipefail reports the
# writer's death as the pipeline's status, so a match reads as a failure.
# scripts/pipefail-grepq-gate.sh owns that rule for the whole tree and scans
# both these files, so this suite does not re-assert it.

set -uo pipefail

GATE='scripts/required-check-name-gate.sh'
PASS=0
FAIL=0
N=0

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# Every gate run below is watchdogged, because the gate can be made to hang and
# it runs inside `make full`. A merge decision that never finishes is worse than
# one that says no, and a suite that hangs with it reports nothing at all. A run
# that outlives the watchdog is killed — python child first, then the shell —
# and reported as exit 124, which no assertion expects.
#
# NOT `timeout`: macOS ships no such binary, so reaching for it exits 127. That
# reads as a refusal, and a hang would be filed as a fix that worked.
GATE_TIMEOUT=20

# expect <label> <expected-exit> <target> [reason-substring]
# Runs the gate against <target> through the environment seam, then asserts the
# exit code and, when a reason is given, that the gate said why.
expect() {
  local label="$1" want="$2" target="$3" reason="${4:-}"
  local out ec pid wd
  N=$(( N + 1 ))
  out="$tmp/out.$N"

  if [ "$target" = "@default" ]; then
    bash "$GATE" >"$out" 2>&1 &
  else
    REQUIRED_CHECK_WORKFLOW="$target" bash "$GATE" >"$out" 2>&1 &
  fi
  pid=$!
  # The watchdog holds no descriptor of ours: an orphaned `sleep` that inherits
  # this suite's stdout keeps the pipe open long after the suite has finished,
  # so anything reading the output to EOF looks hung for GATE_TIMEOUT seconds.
  # Its `sleep` child is killed with it for the same reason.
  ( sleep "$GATE_TIMEOUT"; pkill -9 -P "$pid" 2>/dev/null; kill -9 "$pid" 2>/dev/null ) >/dev/null 2>&1 &
  wd=$!
  wait "$pid"
  ec=$?
  pkill -P "$wd" 2>/dev/null
  kill "$wd" 2>/dev/null
  wait "$wd" 2>/dev/null
  if [ "$ec" -ge 128 ]; then
    echo "*** the gate did not finish within ${GATE_TIMEOUT}s and was killed ***" >>"$out"
    ec=124
  fi

  if [ "$ec" = "$want" ]; then
    PASS=$(( PASS + 1 ))
  else
    FAIL=$(( FAIL + 1 ))
    echo "FAIL: ${label} — expected exit ${want}, got ${ec}"
    sed 's/^/    /' "$out"
  fi

  [ -n "$reason" ] || return 0
  if grep -Fq -- "$reason" "$out"; then
    PASS=$(( PASS + 1 ))
  else
    FAIL=$(( FAIL + 1 ))
    echo "FAIL: ${label} — refused, but never said '${reason}'"
    sed 's/^/    /' "$out"
  fi
}

# A workflow that satisfies every rule. Fixtures below are this file with one
# thing changed, so each assertion isolates the rule it names.
good() {
  cat <<'YML'
name: CI
on:
  push:
    branches: ["**"]
  pull_request:
jobs:
  check:
    name: check (Tier 2)
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: make full
        run: make full
YML
}

# ---------------------------------------------------------------- the tree
# The repo's own workflows, scanned as a directory. This is the assertion
#    that keeps main mergeable, and it is the one that must never be relaxed.
expect "the repo's own workflows declare a working required context" 0 "@default"

# The same tree addressed as a single file, which is the other half of the
#    environment seam the fixtures below rely on.
expect "the seam accepts a single workflow file" 0 ".github/workflows/ci.yml"

# ------------------------------------------------------- the name itself
# The job renamed to something descriptive but wrong — the exact change that
#    landed on this branch and would have wedged main.
good | sed 's/name: check (Tier 2)/name: make full/' >"$tmp/renamed.yml"
expect "a renamed job is refused" 1 "$tmp/renamed.yml" \
  "no job in any scanned workflow is named"

# A job with no name at all reports under its KEY, and the gate has to say
#    so — that is what tells a reader which string GitHub would have used.
cat >"$tmp/unnamed.yml" <<'YML'
name: CI
on:
  pull_request:
jobs:
  merge-gate:
    runs-on: ubuntu-latest
    steps:
      - run: make full
YML
expect "a job with no name reports under its key and is refused" 1 "$tmp/unnamed.yml" \
  "[merge-gate] merge-gate"

# A STEP named the same thing must not satisfy the gate. Steps report
#    nothing; only a job's name becomes a status context.
cat >"$tmp/stepnamed.yml" <<'YML'
name: CI
on:
  pull_request:
jobs:
  check:
    name: make full
    runs-on: ubuntu-latest
    steps:
      - name: check (Tier 2)
        run: make full
YML
expect "a STEP named like the context does not satisfy the gate" 1 "$tmp/stepnamed.yml" \
  "no job in any scanned workflow is named"

# A name that merely CONTAINS the context is refused. GitHub matches the
#    whole string.
good | sed 's/name: check (Tier 2)/name: check (Tier 2) — make full/' >"$tmp/nearmiss.yml"
expect "a name that merely CONTAINS the context is refused" 1 "$tmp/nearmiss.yml" \
  "no job in any scanned workflow is named"

# The comparison is a string comparison, not a pattern match. `check (Tier 2)`
#    read as a regex matches the text `check Tier 2`, because the parentheses
#    become a group. Any regression to a pattern match — dropping `grep -F`,
#    reaching for `re.match` — turns this fixture green.
good | sed 's/name: check (Tier 2)/name: check Tier 2/' >"$tmp/regexbait.yml"
expect "the required context is compared as text, not as a pattern" 1 "$tmp/regexbait.yml" \
  "no job in any scanned workflow is named"

# ------------------------------------------- spellings YAML calls the same
# Every one of these is the same string to a YAML reader, so every one
#      has to pass. The gate used to refuse all of them.
good | sed 's/name: check (Tier 2)/name: "check (Tier 2)"/' >"$tmp/dquoted.yml"
expect "a double-quoted name is the same string and passes" 0 "$tmp/dquoted.yml"

good | sed "s/name: check (Tier 2)/name: 'check (Tier 2)'/" >"$tmp/squoted.yml"
expect "a single-quoted name is the same string and passes" 0 "$tmp/squoted.yml"

good | sed 's/name: check (Tier 2)/name:    check (Tier 2)/' >"$tmp/spaced.yml"
expect "extra spaces after the colon are not part of the name" 0 "$tmp/spaced.yml"

good | sed 's/name: check (Tier 2)/name: check (Tier 2)   # load-bearing/' >"$tmp/commented.yml"
expect "a trailing comment is not part of the name" 0 "$tmp/commented.yml"

cat >"$tmp/folded.yml" <<'YML'
name: CI
on:
  pull_request:
jobs:
  check:
    name: >-
      check (Tier 2)
    runs-on: ubuntu-latest
    steps:
      - run: make full
YML
expect "a folded block scalar name passes" 0 "$tmp/folded.yml"

cat >"$tmp/literal.yml" <<'YML'
name: CI
on:
  pull_request:
jobs:
  check:
    name: |
      check (Tier 2)
    runs-on: ubuntu-latest
    steps:
      - run: make full
YML
expect "a literal block scalar name passes, trailing newline and all" 0 "$tmp/literal.yml"

# ------------------------------------------------ the name is not enough
# A build matrix. The name is right and the context still never
#       arrives, because every leg reports `check (Tier 2) (1.22)` instead.
cat >"$tmp/matrix.yml" <<'YML'
name: CI
on:
  pull_request:
jobs:
  check:
    name: check (Tier 2)
    strategy:
      matrix:
        go: ["1.22", "1.23"]
    runs-on: ubuntu-latest
    steps:
      - run: make full
YML
expect "a build matrix is refused; the bare context is never reported" 1 "$tmp/matrix.yml" \
  "it uses a build matrix"

cat >"$tmp/nomatrix.yml" <<'YML'
name: CI
on:
  pull_request:
jobs:
  check:
    name: check (Tier 2)
    strategy:
      fail-fast: true
    runs-on: ubuntu-latest
    steps:
      - run: make full
YML
expect "a strategy without a matrix still passes" 0 "$tmp/nomatrix.yml"

# The trigger. A workflow that never runs on a pull request reports
#       nothing, and a required context that never arrives reads as pending.
good | sed '/^  pull_request:$/d' >"$tmp/nopr.yml"
expect "losing the pull_request trigger is refused" 1 "$tmp/nopr.yml" \
  "has no pull_request trigger"

cat >"$tmp/onflow.yml" <<'YML'
name: CI
on: [push, pull_request]
jobs:
  check:
    name: check (Tier 2)
    runs-on: ubuntu-latest
    steps:
      - run: make full
YML
expect "the flow-sequence trigger form is understood and passes" 0 "$tmp/onflow.yml"

cat >"$tmp/onscalar.yml" <<'YML'
name: CI
on: pull_request
jobs:
  check:
    name: check (Tier 2)
    runs-on: ubuntu-latest
    steps:
      - run: make full
YML
expect "the bare-scalar trigger form is understood and passes" 0 "$tmp/onscalar.yml"

# Path filters. A workflow skipped by a path filter leaves the required
#       context pending forever, which is the wedge itself.
cat >"$tmp/paths.yml" <<'YML'
name: CI
on:
  pull_request:
    paths:
      - "docs/**"
jobs:
  check:
    name: check (Tier 2)
    runs-on: ubuntu-latest
    steps:
      - run: make full
YML
expect "a path filter on the required workflow is refused" 1 "$tmp/paths.yml" \
  "carries path filters"

cat >"$tmp/pathsignore.yml" <<'YML'
name: CI
on:
  pull_request:
    paths-ignore:
      - "docs/**"
jobs:
  check:
    name: check (Tier 2)
    runs-on: ubuntu-latest
    steps:
      - run: make full
YML
expect "a paths-ignore filter is refused for the same reason" 1 "$tmp/pathsignore.yml" \
  "carries path filters"

# Branch filters. On a pull_request trigger these filter the BASE
#       branch, so one that excludes main excludes every protected merge.
cat >"$tmp/otherbranch.yml" <<'YML'
name: CI
on:
  pull_request:
    branches: [develop]
jobs:
  check:
    name: check (Tier 2)
    runs-on: ubuntu-latest
    steps:
      - run: make full
YML
expect "a base-branch filter that excludes main is refused" 1 "$tmp/otherbranch.yml" \
  "does not run for pull requests targeting 'main'"

cat >"$tmp/mainbranch.yml" <<'YML'
name: CI
on:
  pull_request:
    branches: [main, "release/**"]
jobs:
  check:
    name: check (Tier 2)
    runs-on: ubuntu-latest
    steps:
      - run: make full
YML
expect "a base-branch filter that includes main passes" 0 "$tmp/mainbranch.yml"

cat >"$tmp/ignoremain.yml" <<'YML'
name: CI
on:
  pull_request:
    branches-ignore: [main]
jobs:
  check:
    name: check (Tier 2)
    runs-on: ubuntu-latest
    steps:
      - run: make full
YML
expect "branches-ignore on main is refused" 1 "$tmp/ignoremain.yml" \
  "does not run for pull requests targeting 'main'"

# Event types. Drop `synchronize` and the check never re-runs when the
#       pull request is pushed to; drop `opened` and it never runs at all.
cat >"$tmp/types.yml" <<'YML'
name: CI
on:
  pull_request:
    types: [closed]
jobs:
  check:
    name: check (Tier 2)
    runs-on: ubuntu-latest
    steps:
      - run: make full
YML
expect "a types list without opened and synchronize is refused" 1 "$tmp/types.yml" \
  "types list omits opened, synchronize"

cat >"$tmp/goodtypes.yml" <<'YML'
name: CI
on:
  pull_request:
    types: [opened, synchronize, reopened, ready_for_review]
jobs:
  check:
    name: check (Tier 2)
    runs-on: ubuntu-latest
    steps:
      - run: make full
YML
expect "a types list that keeps opened and synchronize passes" 0 "$tmp/goodtypes.yml"

# A conditional job. The gate cannot evaluate an Actions expression, so
#       it refuses any job-level if: on the required job rather than guess.
cat >"$tmp/iffalse.yml" <<'YML'
name: CI
on:
  pull_request:
jobs:
  check:
    name: check (Tier 2)
    if: false
    runs-on: ubuntu-latest
    steps:
      - run: make full
YML
expect "if: false on the required job is refused" 1 "$tmp/iffalse.yml" \
  "carries a job-level if:"

cat >"$tmp/ifexpr.yml" <<'YML'
name: CI
on:
  pull_request:
jobs:
  check:
    name: check (Tier 2)
    if: github.actor != 'dependabot[bot]'
    runs-on: ubuntu-latest
    steps:
      - run: make full
YML
expect "any job-level if: on the required job is refused" 1 "$tmp/ifexpr.yml" \
  "carries a job-level if:"

# continue-on-error. The context arrives and reports success after a
#       real failure, on every REST surface. ci.yml warns about it in prose
#       and nothing measured it until now.
cat >"$tmp/coe.yml" <<'YML'
name: CI
on:
  pull_request:
jobs:
  check:
    name: check (Tier 2)
    continue-on-error: true
    runs-on: ubuntu-latest
    steps:
      - run: make full
YML
expect "continue-on-error on the required job is refused" 1 "$tmp/coe.yml" \
  "carries continue-on-error"

cat >"$tmp/coefalse.yml" <<'YML'
name: CI
on:
  pull_request:
jobs:
  check:
    name: check (Tier 2)
    continue-on-error: false
    runs-on: ubuntu-latest
    steps:
      - run: make full
YML
expect "continue-on-error: false is the default and passes" 0 "$tmp/coefalse.yml"

# The decoy. A job can carry the required name and do nothing, while the
#       work moves to a job under another name. The context arrives green and
#       means nothing.
cat >"$tmp/decoy.yml" <<'YML'
name: CI
on:
  pull_request:
jobs:
  decoy:
    name: check (Tier 2)
    runs-on: ubuntu-latest
    steps:
      - run: "true"
  real:
    name: make full
    runs-on: ubuntu-latest
    steps:
      - run: make full
YML
expect "a decoy job that carries the name but runs nothing is refused" 1 "$tmp/decoy.yml" \
  "runs no step that invokes 'make full', so it carries"

cat >"$tmp/stepif.yml" <<'YML'
name: CI
on:
  pull_request:
jobs:
  check:
    name: check (Tier 2)
    runs-on: ubuntu-latest
    steps:
      - if: false
        run: make full
YML
expect "a step-level if: on the make full step is refused too" 1 "$tmp/stepif.yml" \
  "every step that invokes 'make full' carries a step-level if:"

cat >"$tmp/stepif-elsewhere.yml" <<'YML'
name: CI
on:
  pull_request:
jobs:
  check:
    name: check (Tier 2)
    runs-on: ubuntu-latest
    steps:
      - if: github.event_name == 'push'
        run: echo optional
      - run: make full
YML
expect "a step-level if: on some OTHER step is fine" 0 "$tmp/stepif-elsewhere.yml"

cat >"$tmp/reusable.yml" <<'YML'
name: CI
on:
  pull_request:
jobs:
  check:
    name: check (Tier 2)
    uses: ./.github/workflows/other.yml
YML
expect "a required job that delegates to a reusable workflow is refused" 1 "$tmp/reusable.yml" \
  "delegates to a reusable workflow"

cat >"$tmp/twojobs.yml" <<'YML'
name: CI
on:
  pull_request:
jobs:
  broken:
    name: check (Tier 2)
    if: false
    runs-on: ubuntu-latest
    steps:
      - run: make full
  working:
    name: check (Tier 2)
    runs-on: ubuntu-latest
    steps:
      - run: make full
YML
expect "one working job is enough even beside a broken namesake" 0 "$tmp/twojobs.yml"

# --------------------------------------------- the reader fails closed
# A workflow this gate cannot read is a workflow it cannot vouch for.
#       The old gate passed a file that was not valid YAML at all, because a
#       grep for a name line does not care whether the file parses.
cat >"$tmp/broken.yml" <<'YML'
name: CI
on:
  pull_request:
jobs:
  check:
    name: check (Tier 2)
    runs-on: ubuntu-latest
    steps:
      - run: make full
     stray: {{{ unclosed
YML
expect "a file that is not valid YAML is refused" 1 "$tmp/broken.yml" \
  "cannot be parsed"

cat >"$tmp/anchors.yml" <<'YML'
name: CI
on:
  pull_request:
defaults: &shared
  run:
    shell: bash
jobs:
  check:
    name: check (Tier 2)
    runs-on: ubuntu-latest
    defaults: *shared
    steps:
      - run: make full
YML
expect "anchors and aliases are refused rather than guessed at" 1 "$tmp/anchors.yml" \
  "anchors, aliases and tags are not understood"

printf 'name: CI\non:\n  pull_request:\njobs:\n  check:\n\tname: check (Tier 2)\n' >"$tmp/tabs.yml"
expect "a tab used as indentation is refused" 1 "$tmp/tabs.yml" \
  "a tab is used for indentation"

cat >"$tmp/multidoc.yml" <<'YML'
name: CI
on:
  pull_request:
jobs:
  check:
    name: check (Tier 2)
    runs-on: ubuntu-latest
    steps:
      - run: make full
---
name: Second
YML
expect "a second YAML document in one file is refused" 1 "$tmp/multidoc.yml" \
  "more than one YAML document"

cat >"$tmp/unclosed.yml" <<'YML'
name: CI
on:
  pull_request:
jobs:
  check:
    name: "check (Tier 2)
    runs-on: ubuntu-latest
    steps:
      - run: make full
YML
expect "a quoted scalar left open is refused" 1 "$tmp/unclosed.yml" \
  "not closed on its line"

# ------------------------------------------------- addressing the target
# The file-exists guard, pinned by the reason it gives. Deleting it used
#       to redden nothing here, because another branch of the gate refused the
#       same fixture and the suite only read the exit code.
expect "a missing target is refused, and says it is missing" 1 "$tmp/does-not-exist.yml" \
  "is missing"

mkdir -p "$tmp/emptydir"
expect "a directory with no workflows in it is refused by name" 1 "$tmp/emptydir" \
  "holds no .yml or .yaml workflow file"

mkdir -p "$tmp/dir-second"
cat >"$tmp/dir-second/lint.yml" <<'YML'
name: Lint
on:
  pull_request:
jobs:
  lint:
    name: lint
    runs-on: ubuntu-latest
    steps:
      - run: make lint
YML
good >"$tmp/dir-second/merge.yaml"
expect "a second workflow may legitimately carry the context, .yaml included" 0 "$tmp/dir-second"

# An unreadable file that NAMES the required context fails the scan even
#    beside a healthy workflow: it may declare a second job under that name,
#    and the gate cannot say which of two same-named check runs is graded.
mkdir -p "$tmp/dir-broken"
good >"$tmp/dir-broken/ci.yml"
cp "$tmp/broken.yml" "$tmp/dir-broken/other.yml"
expect "an unreadable workflow that names the context fails the scan" 1 "$tmp/dir-broken" \
  "cannot be parsed"
expect "and it says the unreadable file named the context" 1 "$tmp/dir-broken" \
  "mentions 'check (Tier 2)'"

# ------------------------------------------ the reader always finishes
# The flow scanner used to return with its index unchanged on a character it
#    could not consume, and the caller looped on it forever. Measured at 10s and
#    killed, three times over. This gate runs inside `make full`, which IS the
#    merge decision, and ci.yml declares no timeout-minutes, so a hang here
#    stops every developer and sits in CI until GitHub's six-hour default. One
#    hung file hangs the whole scan, healthy workflows included.
#    Each input below is also a syntax error to a real YAML parser (checked
#    against ruby's Psych), so refusing them is the right answer as well as a
#    terminating one. Every assertion here is watchdogged: a hang comes back as
#    exit 124 and reads as a failure instead of wedging this suite.
cat >"$tmp/flow-colon.yml" <<'YML'
name: CI
on:
  pull_request:
jobs:
  check:
    name: check (Tier 2)
    runs-on: ubuntu-latest
    env: [a:b]
    steps:
      - run: make full
YML
expect "a colon inside a flow SEQUENCE is refused, not spun on" 1 "$tmp/flow-colon.yml" \
  "where this reader expects a value, a comma or a closing bracket"

cat >"$tmp/flow-mismatch.yml" <<'YML'
name: CI
on:
  pull_request:
jobs:
  check:
    name: check (Tier 2)
    runs-on: ubuntu-latest
    env: [a}
    steps:
      - run: make full
YML
expect "a mismatched flow bracket is refused, not spun on" 1 "$tmp/flow-mismatch.yml" \
  "cannot get past"

# Ordinary shell in a run: step. `[[` opens a flow sequence to a YAML reader,
#    which is why this is reachable by writing perfectly normal CI.
cat >"$tmp/flow-run.yml" <<'YML'
name: CI
on:
  pull_request:
jobs:
  check:
    name: check (Tier 2)
    runs-on: ubuntu-latest
    steps:
      - run: [[ -n "${X:-}" ]] && make full
YML
expect "a run: step that opens with [[ is refused, not spun on" 1 "$tmp/flow-run.yml" \
  "where this reader expects a value, a comma or a closing bracket"

# This one terminated before the fix, and mis-parsed in silence. A reader that
#    invents a key named after the empty scalar is not reading the file.
cat >"$tmp/flow-map-colon.yml" <<'YML'
name: CI
on:
  pull_request:
jobs:
  check:
    name: check (Tier 2)
    runs-on: ubuntu-latest
    env: {a: b: c}
    steps:
      - run: make full
YML
expect "a second colon inside a flow MAPPING is refused, not mis-read" 1 "$tmp/flow-map-colon.yml" \
  "where this reader expects a value, a comma or a closing bracket"

# Finishing is about time, not only about loops. The plain-key pattern this
#    reader used to carry let two of its parts match the same run of whitespace,
#    so a line with a long run of it and no `key:` cost time quadratic in its
#    length: 20 KB ran for 0.9s, 40 KB for 3.7s, and 300 KB did not finish
#    inside ten seconds. The gate is inside `make full`. This fixture is that
#    line, and against the pattern it comes back as the watchdog's 124.
{ printf 'name: CI\nx'; head -c 307200 </dev/zero | tr '\000' ' '; printf '\n'; } >"$tmp/slowline.yml"
expect "a 300 KB run of whitespace is read in one pass, not backtracked over" 1 "$tmp/slowline.yml" \
  "this line is not a mapping key"

# Depth is bounded by the reader's own stack, and running out of it is still a
#    refusal — so it says so, like every other refusal here. A traceback fails
#    closed and names no reason, which leaves a reader guessing whether the gate
#    judged the file or fell over.
{ printf 'name: CI\nx: '; head -c 20000 </dev/zero | tr '\000' '{'; printf '\n'; } >"$tmp/deepnest.yml"
expect "a file nested past the reader's stack is refused with a reason" 1 "$tmp/deepnest.yml" \
  "it nests deeper than this reader follows"

# The passing twins. Refusing every flow collection would satisfy the four
#    assertions above and read no workflow at all.
cat >"$tmp/flow-ok.yml" <<'YML'
name: CI
on:
  pull_request:
jobs:
  check:
    name: check (Tier 2)
    runs-on: ubuntu-latest
    env: {FOO: bar, BAZ: qux}
    steps:
      - run: make full
YML
expect "a well-formed flow mapping still reads and passes" 0 "$tmp/flow-ok.yml"

cat >"$tmp/flow-job.yml" <<'YML'
name: CI
on: {pull_request: null}
jobs:
  check: {name: check (Tier 2), runs-on: ubuntu-latest, steps: [{run: make full}]}
YML
expect "a whole job written in flow style still reads and passes" 0 "$tmp/flow-job.yml"

# ------------------------------------- continue-on-error, on the right line
# Rule 8 used to read the job and nothing else. Every occurrence of this flag
#    in this repo's history was written on the STEP: on ci.yml's own step, and
#    on the scenario.yml step that reported 20 straight real failures as green.
#    A job-level-only rule passes all of them.
cat >"$tmp/stepcoe.yml" <<'YML'
name: CI
on:
  pull_request:
jobs:
  check:
    name: check (Tier 2)
    runs-on: ubuntu-latest
    steps:
      - name: make full
        continue-on-error: true
        run: make full
YML
expect "continue-on-error on the make full STEP is refused too" 1 "$tmp/stepcoe.yml" \
  "every step that invokes 'make full' carries continue-on-error"

cat >"$tmp/stepcoe-false.yml" <<'YML'
name: CI
on:
  pull_request:
jobs:
  check:
    name: check (Tier 2)
    runs-on: ubuntu-latest
    steps:
      - name: make full
        continue-on-error: false
        run: make full
YML
expect "continue-on-error: false on that step is the default and passes" 0 "$tmp/stepcoe-false.yml"

# The flag on some OTHER step masks that step and nothing else. Refusing it
#    would be a false fail, and false fails teach people to route around a gate.
cat >"$tmp/stepcoe-elsewhere.yml" <<'YML'
name: CI
on:
  pull_request:
jobs:
  check:
    name: check (Tier 2)
    runs-on: ubuntu-latest
    steps:
      - name: upload logs
        continue-on-error: true
        run: echo optional
      - run: make full
YML
expect "continue-on-error on some OTHER step is fine" 0 "$tmp/stepcoe-elsewhere.yml"

# An expression is not `false`, and the gate cannot evaluate it. Pinned so the
#    step-level rule above cannot be written in a way that regresses it.
cat >"$tmp/coeexpr.yml" <<'YML'
name: CI
on:
  pull_request:
jobs:
  check:
    name: check (Tier 2)
    continue-on-error: ${{ github.event_name == 'push' }}
    runs-on: ubuntu-latest
    steps:
      - run: make full
YML
expect "continue-on-error written as an expression is still refused" 1 "$tmp/coeexpr.yml" \
  "carries continue-on-error"

# ---------------------------------------------------- duplicate keys
# GitHub rejects a duplicate key outright — "'jobs' is already defined" — so
#    the run never starts and the required context is pending forever. This is
#    what a badly resolved merge conflict in a workflow file looks like. Read
#    as last-wins the gate's answer depends on which copy came second, which is
#    its own tell: both orders are asserted here for that reason.
cat >"$tmp/dupjobs-good-last.yml" <<'YML'
name: CI
on:
  pull_request:
jobs:
  other:
    runs-on: ubuntu-latest
    steps:
      - run: true
jobs:
  check:
    name: check (Tier 2)
    runs-on: ubuntu-latest
    steps:
      - run: make full
YML
expect "two jobs: blocks are refused, healthy copy last" 1 "$tmp/dupjobs-good-last.yml" \
  "the key 'jobs' is defined twice"

cat >"$tmp/dupjobs-good-first.yml" <<'YML'
name: CI
on:
  pull_request:
jobs:
  check:
    name: check (Tier 2)
    runs-on: ubuntu-latest
    steps:
      - run: make full
jobs:
  other:
    runs-on: ubuntu-latest
    steps:
      - run: true
YML
expect "two jobs: blocks are refused, healthy copy first" 1 "$tmp/dupjobs-good-first.yml" \
  "the key 'jobs' is defined twice"

cat >"$tmp/dupon.yml" <<'YML'
name: CI
on:
  push:
jobs:
  check:
    name: check (Tier 2)
    runs-on: ubuntu-latest
    steps:
      - run: make full
on:
  pull_request:
YML
expect "two on: blocks are refused for the same reason" 1 "$tmp/dupon.yml" \
  "the key 'on' is defined twice"

# ------------------------------------------- a workflow GitHub would refuse
# Nine rules describe ways a VALID workflow loses the context. A workflow
#    GitHub refuses loses it just as completely: no run, no check run, pending
#    forever. runs-on is the minimal check that says so.
cat >"$tmp/norunson.yml" <<'YML'
name: CI
on:
  pull_request:
jobs:
  check:
    name: check (Tier 2)
    steps:
      - run: make full
YML
expect "a required job with no runs-on: is refused" 1 "$tmp/norunson.yml" \
  "it declares no runs-on:"

# One bad job kills the whole file, so a SIBLING job counts too.
cat >"$tmp/sibling-norunson.yml" <<'YML'
name: CI
on:
  pull_request:
jobs:
  check:
    name: check (Tier 2)
    runs-on: ubuntu-latest
    steps:
      - run: make full
  lint:
    steps:
      - run: make lint
YML
expect "a SIBLING job with no runs-on: is refused; GitHub refuses the file" 1 "$tmp/sibling-norunson.yml" \
  "job 'lint' in the same workflow declares no runs-on:"

# The twin for the sibling rule. A sibling that delegates with `uses:` names
#    no runner and GitHub accepts it, so reading `runs-on:` alone — without the
#    `uses:` short-circuit — would redden a perfectly good workflow.
cat >"$tmp/sibling-uses.yml" <<'YML'
name: CI
on:
  pull_request:
jobs:
  check:
    name: check (Tier 2)
    runs-on: ubuntu-latest
    steps:
      - run: make full
  release:
    uses: ./.github/workflows/release.yml
YML
expect "a SIBLING job that delegates with uses: needs no runs-on: and passes" 0 "$tmp/sibling-uses.yml"

cat >"$tmp/runson-list.yml" <<'YML'
name: CI
on:
  pull_request:
jobs:
  check:
    name: check (Tier 2)
    runs-on: [self-hosted, linux]
    steps:
      - run: make full
YML
expect "runs-on written as a list is a runner and passes" 0 "$tmp/runson-list.yml"

# ------------------------------------------------- rule 7, one hop away
# A skipped dependency skips this job, GitHub reports the required check as
#    SKIPPED, and branch protection reads a skipped required check as
#    SATISFIED. That is a merge nobody decided on — the same outcome rule 7
#    already refuses for a job-level if:, reached through needs:.
cat >"$tmp/needs-if.yml" <<'YML'
name: CI
on:
  pull_request:
jobs:
  gate:
    runs-on: ubuntu-latest
    if: github.actor == 'nobody'
    steps:
      - run: true
  check:
    name: check (Tier 2)
    needs: [gate]
    runs-on: ubuntu-latest
    steps:
      - run: make full
YML
expect "needs: a conditional job is refused" 1 "$tmp/needs-if.yml" \
  "it needs job 'gate'"

# Two hops. A rule that only reads the direct dependency is one job away from
#    the same hole.
cat >"$tmp/needs-if-2hop.yml" <<'YML'
name: CI
on:
  pull_request:
jobs:
  gate:
    runs-on: ubuntu-latest
    if: github.actor == 'nobody'
    steps:
      - run: true
  build:
    runs-on: ubuntu-latest
    needs: [gate]
    steps:
      - run: true
  check:
    name: check (Tier 2)
    needs: [build]
    runs-on: ubuntu-latest
    steps:
      - run: make full
YML
expect "needs: a job that itself needs a conditional job is refused" 1 "$tmp/needs-if-2hop.yml" \
  "it needs job 'gate'"

# `needs:` takes a bare string as readily as a list, and GitHub reads the two
#    the same way. A rule that only understands the list form leaves the whole
#    of rule 11 off for anyone who wrote the short one.
cat >"$tmp/needs-if-scalar.yml" <<'YML'
name: CI
on:
  pull_request:
jobs:
  gate:
    runs-on: ubuntu-latest
    if: github.actor == 'nobody'
    steps:
      - run: true
  check:
    name: check (Tier 2)
    needs: gate
    runs-on: ubuntu-latest
    steps:
      - run: make full
YML
expect "needs: written as a bare string is read too" 1 "$tmp/needs-if-scalar.yml" \
  "it needs job 'gate'"

cat >"$tmp/needs-missing.yml" <<'YML'
name: CI
on:
  pull_request:
jobs:
  check:
    name: check (Tier 2)
    needs: [nosuchjob]
    runs-on: ubuntu-latest
    steps:
      - run: make full
YML
expect "needs: a job that does not exist is refused" 1 "$tmp/needs-missing.yml" \
  "which this workflow does not declare"

# The twin. An ordinary needs: is how CI is written and must keep passing.
cat >"$tmp/needs-ok.yml" <<'YML'
name: CI
on:
  pull_request:
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: make build
  check:
    name: check (Tier 2)
    needs: [build]
    runs-on: ubuntu-latest
    steps:
      - run: make full
YML
expect "needs: an unconditional job passes" 0 "$tmp/needs-ok.yml"

# ------------------------------------- what one unreadable file may redden
# A refusal the gate cannot scope is one people learn to ignore. An anchor in
#    an unrelated workflow used to redden a gate whose subject is one job in
#    ci.yml. It stays closed two ways, asserted below: fatal when nothing else
#    carries the context, and fatal beside a healthy workflow when the file
#    NAMES the context (asserted above with dir-broken).
mkdir -p "$tmp/dir-unrelated"
good >"$tmp/dir-unrelated/ci.yml"
cat >"$tmp/dir-unrelated/dependabot.yml" <<'YML'
name: Dependabot
on:
  pull_request:
x-common: &common
  runs-on: ubuntu-latest
jobs:
  auto:
    <<: *common
    steps:
      - run: echo hi
YML
expect "an unreadable UNRELATED workflow does not redden a healthy gate" 0 "$tmp/dir-unrelated" \
  "It is set aside"

# ...and the same file is fatal the moment nothing readable carries the
#    context, because it might have been the file meant to.
mkdir -p "$tmp/dir-nocarrier"
cp "$tmp/dir-unrelated/dependabot.yml" "$tmp/dir-nocarrier/dependabot.yml"
cat >"$tmp/dir-nocarrier/lint.yml" <<'YML'
name: Lint
on:
  pull_request:
jobs:
  lint:
    name: lint
    runs-on: ubuntu-latest
    steps:
      - run: make lint
YML
expect "an unreadable workflow is fatal when nothing else carries the context" 1 "$tmp/dir-nocarrier" \
  "cannot be parsed"

# ------------------------------------------- tabs: structure versus content
# YAML forbids a tab in structural indentation. A tab INSIDE a block scalar is
#    content, and it is how anyone writes a Makefile recipe or a heredoc body
#    in a run: step. Refusing it reddened the gate over legal, ordinary CI.
printf 'name: CI\non:\n  pull_request:\njobs:\n  check:\n    name: check (Tier 2)\n    runs-on: ubuntu-latest\n    steps:\n      - run: |\n          cat > Makefile <<'"'"'EOF'"'"'\n          all:\n          \tmake full\n          EOF\n          make full\n' >"$tmp/tabcontent.yml"
expect "a tab inside a block scalar is content, not indentation, and passes" 0 "$tmp/tabcontent.yml"

# ...but the tab above is on the SECOND content line, and that is the only
#    reason it is content. The FIRST content line is what FIXES the column the
#    content starts in, and YAML measures that column in SPACES. A tab there
#    sets nothing, so it is structural: libyaml says "found a tab character
#    where an indentation space is expected", GitHub refuses the whole
#    workflow, no check run is created, and the required context is pending
#    forever. Ruby's Psych refuses this exact file and accepts the one above —
#    the two fixtures differ by one line. A reader that measures both the same
#    way says ok to a file GitHub will not run, which is the wedge direction.
printf 'name: CI\non:\n  pull_request:\njobs:\n  check:\n    name: check (Tier 2)\n    runs-on: ubuntu-latest\n    steps:\n      - run: |\n          \tmake full\n' >"$tmp/tabfirst.yml"
expect "a tab on the FIRST content line of a block scalar is refused" 1 "$tmp/tabfirst.yml" \
  "a tab appears in a block scalar's indentation"

# The same tab, with an explicit indentation indicator. `|2` fixes the column
#    in the HEADER, before any content line is read, so the first content line
#    no longer decides anything and a tab at that column is content. Psych
#    accepts this file. Without this twin, "refuse every tab on a first content
#    line" would satisfy the assertion above and be wrong.
printf 'name: CI\non:\n  pull_request:\njobs:\n  check:\n    name: check (Tier 2)\n    runs-on: ubuntu-latest\n    steps:\n      - run: |2\n          \tmake full\n' >"$tmp/tabfirst-explicit.yml"
expect "an explicit indent indicator makes that same tab content, and it passes" 0 "$tmp/tabfirst-explicit.yml"

# A line of nothing but a tab is blank to a reader that trims it, and is a
#    structural error to libyaml, which reads its indentation like any other
#    line's. Psych refuses this file.
printf 'name: CI\non:\n  pull_request:\njobs:\n  check:\n    name: check (Tier 2)\n    runs-on: ubuntu-latest\n    steps:\n      - run: |\n          make full\n\t\n          echo done\n' >"$tmp/tabblankline.yml"
expect "a whitespace line that is a lone tab is refused, not trimmed away" 1 "$tmp/tabblankline.yml" \
  "a tab appears in a block scalar's indentation"

# A tab SHALLOWER than the block content ends the scalar and lands on the
#    structural reader, which still refuses it. Ruby's Psych refuses the same
#    file: "found a tab character where an indentation space is expected".
printf 'name: CI\non:\n  pull_request:\njobs:\n  check:\n    name: check (Tier 2)\n    runs-on: ubuntu-latest\n    steps:\n      - run: |\n          all:\n\tmake full\n' >"$tmp/tabshallow.yml"
expect "a tab left of the block content is still refused" 1 "$tmp/tabshallow.yml" \
  "a tab is used for indentation"

# ------------------------------------------------ say only what can be seen
# A job that calls a local composite action may well run the command inside
#    it. "It runs no step that invokes make full" is a stronger claim than a
#    gate that does not read composite actions can make.
cat >"$tmp/composite.yml" <<'YML'
name: CI
on:
  pull_request:
jobs:
  check:
    name: check (Tier 2)
    runs-on: ubuntu-latest
    steps:
      - uses: ./.github/actions/make-full
YML
expect "a composite action is refused as unconfirmable, not as empty" 1 "$tmp/composite.yml" \
  "it runs no step that invokes 'make full' directly, and it calls './.github/actions/make-full'"

# The reusable-workflow refusal is not a parser limitation. A calling job
#    composes its child contexts, so the bare name is never reported at all.
expect "the reusable-workflow refusal names the composed context" 1 "$tmp/reusable.yml" \
  "<job name> / <child job name>"

echo "required-check-name-gate-test: $(( PASS + FAIL )) assertions, ${FAIL} failed"
[ "$FAIL" -eq 0 ] || exit 1
