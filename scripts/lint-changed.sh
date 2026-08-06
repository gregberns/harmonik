#!/usr/bin/env bash
# lint-changed.sh — the changed-line lint step of gate-static.
#
# It runs one command. It exists for what it does with ONE exit code.
#
# THE PROBLEM. golangci-lint analyses the whole tree even when --new-from-rev
# limits what it REPORTS. With a cold analysis cache that work can exceed the
# cap in .golangci.yml, and the linter then exits 4 with no file named and no
# finding printed. Measured on this tree 2026-08-05 (hk-velgn): the changed-line
# run reported "0 issues.", then timed out, then took the whole gate down. What
# the agent saw was a red `make fast` with nothing failing in it, which is
# indistinguishable from a code failure. docs/disk-reclaim.md section 0 tells
# operators to clear that very cache, so the project routinely creates the
# state that produces this.
#
# WHAT THIS DOES ABOUT IT. A timeout is not a finding. It is a run that reached
# no answer, and it is reported in those words. The step STILL FAILS: a gate
# that passed a step which measured nothing would be the fail-open shape
# scripts/gate-fails-closed-test.sh exists to refuse, and a reader who is told
# "inconclusive" and given a green has been told nothing.
#
# WHY NOT JUST RAISE THE CAP. A bigger number is one machine's number and it
# goes wrong again on the next machine, on the next cold cache, or when the tree
# grows. Raising it also does not help the reader who hits it. The honest output
# is the part that lasts; the cap can still be tuned in .golangci.yml on top of
# it, and neither change depends on the other.
#
# WHY THE FLAGS STAY IN THE MAKEFILE. This wrapper runs whatever argv it is
# given and translates ONE exit code. It does not own the lint invocation. Two
# reasons. The step stays readable in `make -n fast`, which is where
# scripts/lint-allow-test.sh looks to prove that `make fast` still lints changed
# lines — moving --new-from-rev in here made that assertion go red, correctly,
# because the property it guards became invisible. And a translator that runs
# argv can wrap any lint step, while one that builds its own command line has
# to grow a flag for every caller.
#
# The cap in .golangci.yml is deliberately not repeated here. One name for one
# thing: read it there.
#
# scripts/lint-changed-test.sh holds this to both halves — a timeout must fail
# AND read as inconclusive, and a real finding must fail WITHOUT the
# inconclusive wording, so the excuse cannot spread to every failure.

set -uo pipefail

if [ $# -lt 1 ]; then
    echo "usage: scripts/lint-changed.sh path/to/golangci-lint run [args ...]" >&2
    exit 2
fi

out=$(mktemp "${TMPDIR:-/tmp}/lint-changed-XXXXXX") || {
    echo "lint-changed: could not create a temp file, so no run can be judged" >&2
    exit 2
}
trap 'rm -f "$out"' EXIT

status=0
scripts/with-lane-gocache.sh "$@" >"$out" 2>&1 || status=$?

cat "$out"

# golangci-lint exits 4 for its own timeout. The message is matched as well as
# the code, because a code alone is a single point of failure across linter
# versions and being generous here costs nothing: every branch below still
# fails.
if [ "$status" -eq 4 ] || grep -qi 'timeout exceeded' "$out"; then
    cat >&2 <<'EOF'

lint-changed: the linter timed out. THIS IS NOT A VERDICT ON YOUR CODE.
  No file was named and no finding was reported, so nothing here says your
  change is wrong. The step reached no answer at all.
  THE USUAL CAUSE is a cold golangci-lint cache. A changed-line run still
  analyses the whole tree, and building that analysis from nothing can exceed
  the cap in .golangci.yml. Warm, the same step finishes well inside it.
  docs/disk-reclaim.md section 0 tells you to clear that cache, so the project
  can put you here on purpose.
  WHAT TO DO. Run the step again. The second run reuses the analysis the first
  one built. If a warm run times out too, the cap is wrong or a linter hangs,
  and that is a real defect worth a bead.
  THIS STEP STILL FAILS, because a step that measured nothing must never
  report a pass.
EOF
    exit "$status"
fi

exit "$status"
