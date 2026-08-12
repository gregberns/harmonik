#!/bin/zsh
# run-full.sh — run `make full`, the merge decision, and refuse to run it when
# the box cannot give an honest answer.
#
# `make full` is what CI runs and what decides whether work can land. On a
# developer box it has three ways to report a failure that is not in the code,
# and all three are silent. This script refuses to start on the first two and
# records the third for the reader.
#
#   1. Another heavy run sharing the box. `make full` starts 110 test binaries
#      at once. A second one turns every wall-clock assertion in the suite into
#      a measurement of machine load. A leaked compiled test binary counts, and
#      its name matches neither "make" nor "go test", so leakcheck.sh is asked
#      as well as pgrep.
#   2. A process holding the sidecar lock on the operator's Claude config. A
#      held lock and a starved box produce the same log. One leaked test binary
#      took a run from 0 failures to 22 with no code change between them.
#   3. Free disk below the floor. Under it the suite manufactures failures that
#      have nothing to do with the tree. Sampled every 30 seconds, so a dip in
#      the middle of a run is visible afterwards rather than invisible.
#
# It prints FULL_RC and writes the same to the log. Read THAT number. Reading an
# exit code out of a pipeline is how a red gate gets recorded as green here: in
# zsh the array is $pipestatus, and the bash $PIPESTATUS spelling evaluates to
# empty, which reads as success.
#
# It also prints a WARNING for each way the run can be green and still mean
# nothing: the branch moved under it, or the sidecar lock was taken while it
# ran. A green result under either is not evidence.
#
# Logs go to $HARMONIK_GATE_LOGDIR, or a directory under $TMPDIR. They are large
# and they are not repository content, so a log directory inside the work tree
# is refused rather than allowed to dirty the tree the suite is testing.
#
# Exit codes
#   0     make full passed
#   1..69 make full ran and failed — this is make's own code, read the log
#   70    REFUSED: another heavy run is on this box
#   71    REFUSED: something holds the Claude config sidecar lock
#   72    REFUSED: free disk is below the floor
#   73    REFUSED: bad setup — not a work tree, or a log directory inside it

set -u

FLOOR_MiB=${HARMONIK_GATE_FLOOR_MiB:-10240}

REPO=$(git rev-parse --show-toplevel) || exit 73
cd "$REPO" || exit 73

S=${HARMONIK_GATE_LOGDIR:-${TMPDIR:-/tmp}/harmonik-gate}
mkdir -p "$S" || exit 73
# A log inside the work tree dirties the thing under test. The suite has a
# detector for an unexpectedly dirty tree, so this would surface as a failure
# with no relation to the code.
S_ABS=$(cd "$S" && pwd -P) || exit 73
REPO_ABS=$(cd "$REPO" && pwd -P) || exit 73
if [[ "$S_ABS" == "$REPO_ABS" || "$S_ABS" == "$REPO_ABS"/* ]]; then
  echo "REFUSED: the log directory $S_ABS is inside the work tree; the suite would test a dirty tree."
  exit 73
fi
LOG=$S/full.log
DISK=$S/full-disk.log

# --- pre-flight: refuse rather than produce a result nobody can read ---------
BUSY=$(pgrep -fl "make |go test|golangci" | grep -v "run-full.sh" | grep -v "pgrep")
if [[ -n "$BUSY" ]]; then
  echo "REFUSED: another heavy run is on this box; its timing numbers would poison this one."
  echo "$BUSY"
  exit 70
fi
# pgrep cannot see this one: a leaked test binary is called daemon.test, which
# matches none of the patterns above. That leak is what took a run from 0
# failures to 22, so it is worth the extra call.
#
# Only compute-competing leaks are refused. leakcheck.sh reports every orphan it
# finds, including an idle shell that has been parked for days consuming
# nothing. Refusing on those would block every run for something that poisons no
# measurement, and a check that refuses when nothing is wrong gets switched off.
#
# The test is CPU, not the process name. A busy-looping orphaned shell and an
# idle parked one are both called /bin/zsh, so no name list can tell them apart
# — and the busy one is the whole reason leakcheck.sh exists. A SPAWN line is a
# live parent with a runaway child pool and always counts.
if [[ -x "$REPO/scripts/leakcheck.sh" ]]; then
  LEAKS=$("$REPO/scripts/leakcheck.sh" 2>/dev/null | awk '
    /^(LEAK|BUSY)/ {
      for (i = 1; i <= NF; i++)
        if ($i ~ /^cpu=/ && substr($i, 5) + 0 >= 5.0) { print; break }
      next
    }
    /^SPAWN/ && /pid=/ { print }')
  if [[ -n "$LEAKS" ]]; then
    echo "REFUSED: leaked build or test processes are still on this box."
    echo "$LEAKS"
    echo "Clear them with scripts/leakcheck.sh --kill, then run this again."
    exit 70
  fi
fi
LOCKHOLDER=$(lsof "$HOME/.claude.json.lock" 2>/dev/null | tail -n +2)
if [[ -n "$LOCKHOLDER" ]]; then
  echo "REFUSED: something holds ~/.claude.json.lock. Kill it before believing any gate result."
  echo "$LOCKHOLDER"
  exit 71
fi
FREE_MiB=$(df -m "$REPO" | awk 'NR==2{print $4}')
if [[ -z "$FREE_MiB" ]] || (( FREE_MiB < FLOOR_MiB )); then
  echo "REFUSED: ${FREE_MiB:-unknown} MiB free is below the $FLOOR_MiB MiB floor; the gate fails for the disk, not the code."
  exit 72
fi

HEAD_BEFORE=$(git rev-parse HEAD)
# Tracked files only. `make full` leaves artifacts behind, and counting those
# would fire the warning on every run until nobody reads it.
TREE_BEFORE=$(git status --porcelain --untracked-files=no | shasum | awk '{print $1}')
{
  echo "HEAD_BEFORE=$HEAD_BEFORE"
  echo "START=$(date '+%F %T')"
  echo "PREFLIGHT: no heavy run, no leaked test binary, no lock holder, ${FREE_MiB} MiB free"
  df -m "$REPO" | tail -1
} > "$LOG"

: > "$DISK"
# Sampler: available disk and anything holding the sidecar lock, every 30s.
( while :; do
    printf '%s %s MiB lock=%s\n' \
      "$(date '+%T')" \
      "$(df -m "$REPO" | awk 'NR==2{print $4}')" \
      "$(lsof -t "$HOME/.claude.json.lock" 2>/dev/null | tr '\n' ',')" >> "$DISK"
    sleep 30
  done ) &
SAMPLER=$!
# Without this the sampler outlives a killed run and loops forever. That leak
# has happened here at scale: dozens of orphaned subshells, some for a day, and
# they are the thing that poisons the NEXT run.
trap 'kill $SAMPLER 2>/dev/null' EXIT INT TERM

make full >> "$LOG" 2>&1
RC=$?

kill $SAMPLER 2>/dev/null
HEAD_AFTER=$(git rev-parse HEAD)
TREE_AFTER=$(git status --porcelain --untracked-files=no | shasum | awk '{print $1}')
DISK_MIN=$(awk '{print $2}' "$DISK" | sort -n | head -1)
LOCK_HELD=$(grep -c 'lock=[0-9]' "$DISK")
{
  echo "FULL_RC=$RC"
  echo "END=$(date '+%F %T')"
  echo "HEAD_AFTER=$HEAD_AFTER"
  echo "DISK_MIN_MiB=$DISK_MIN"
  echo "LOCK_SEEN_HELD=$LOCK_HELD"
  df -m "$REPO" | tail -1
} >> "$LOG"

# Each of these makes a green result mean nothing. Say so on stdout, where the
# reader is, rather than only in a log they have to go and open.
if [[ "$HEAD_BEFORE" != "$HEAD_AFTER" ]]; then
  echo "WARNING: the branch moved during this run ($HEAD_BEFORE -> $HEAD_AFTER). This verdict is about neither commit."
fi
if [[ "$TREE_BEFORE" != "$TREE_AFTER" ]]; then
  echo "WARNING: the working tree changed during this run. The suite tests the tree, not the commit."
fi
if (( LOCK_HELD > 0 )); then
  echo "WARNING: something held ~/.claude.json.lock during this run. A green result here is not evidence."
fi
if [[ -n "$DISK_MIN" ]] && (( DISK_MIN < FLOOR_MiB )); then
  echo "WARNING: free disk dipped to ${DISK_MIN} MiB, below the ${FLOOR_MiB} MiB floor. Failures may be the disk."
fi

echo "FULL_RC=$RC"
echo "log: $LOG"
exit $RC
