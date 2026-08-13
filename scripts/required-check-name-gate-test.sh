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

# expect <label> <expected-exit> <target> [reason-substring]
# Runs the gate against <target> through the environment seam, then asserts the
# exit code and, when a reason is given, that the gate said why.
expect() {
  local label="$1" want="$2" target="$3" reason="${4:-}"
  local out ec
  N=$(( N + 1 ))
  out="$tmp/out.$N"

  if [ "$target" = "@default" ]; then
    bash "$GATE" >"$out" 2>&1
  else
    REQUIRED_CHECK_WORKFLOW="$target" bash "$GATE" >"$out" 2>&1
  fi
  ec=$?

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
  "runs no step that invokes 'make full'"

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

# One unreadable file in the directory fails the whole scan. The gate
#    cannot know the broken file was not the one meant to carry the context.
mkdir -p "$tmp/dir-broken"
good >"$tmp/dir-broken/ci.yml"
cp "$tmp/broken.yml" "$tmp/dir-broken/other.yml"
expect "one unreadable workflow fails the whole directory scan" 1 "$tmp/dir-broken" \
  "cannot be parsed"

echo "required-check-name-gate-test: $(( PASS + FAIL )) assertions, ${FAIL} failed"
[ "$FAIL" -eq 0 ] || exit 1
