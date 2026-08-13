#!/usr/bin/env bash
# pipefail-grepq-gate.sh — a shell script that turns on pipefail must not send a
# producer's output through a consumer that leaves early.
#
# WHY THIS EXISTS. `grep -q` exits the instant it matches. The producer upstream
# is then writing to a closed pipe, so it dies — SIGPIPE for most programs, an
# abort trap (exit 134) for a Go binary. `pipefail` takes the worst status in the
# pipeline, so it reports the producer's death as the status of the whole thing.
# A MATCH comes back as a FAILURE.
#
# It only bites when the payload is big enough that the producer is still writing
# when the consumer leaves. Below the pipe buffer the producer finishes first and
# everything looks fine. So this passes on small inputs and on a quiet developer
# box and fails on a full ledger. It is a matter of position and speed, not of
# platform, and no amount of re-running it on a small input will show it.
#
# The two directions, and the second is worse:
#   POSITIVE  `if producer | (grep, quiet) X; then found` — says "not found" when X IS there.
#   NEGATIVE  `if producer | (grep, quiet) BANNED; then fail` — says "clean" whatever
#             the input holds. It is a gate that has never been able to block anything.
#
# THE FIX is a here-string, which has no writer process to kill:
#   ok=$(producer); grep -q X <<<"$ok"
# or drop -q, send grep's output to /dev/null, and read grep's own status alone.
#
# WHY A GATE AND NOT A COMMENT. One script in this repo carried a comment saying
# "never pipe into a quiet grep here" and then did it eight times below that
# comment. A comment does not hold this shut.
#
# WHAT IT COVERS. Every `.sh` file git knows about that turns on pipefail. It also
# catches a pipe into `head` when the producer is `br`, `kerf` or `harmonik` —
# those are slow streaming Go binaries with large output, and that exact shape had
# both boot digests reporting an abort trap instead of the backlog.
#
# WHAT IT DOES NOT COVER, on purpose:
#   - Makefile recipes. This Makefile sets neither SHELL nor .SHELLFLAGS, so its
#     recipes run without pipefail and the shape is harmless there. The scope
#     guard below fails if that stops being true.
#   - GitHub workflow `run:` blocks. None of them turn on pipefail today, and
#     GitHub's default shell on Linux is `bash -e`, which does not either. A step
#     that adds `shell: bash` DOES get pipefail, and this gate will not see it.
#   - A script that inherits pipefail from a caller that sources it.
#
# WHERE THE BAR SITS, AND WHY IT IS LOWER THAN IT LOOKS. It is tempting to say the
# pipe buffer is 64 KB, so anything under 64 KB is safe. That is wrong. What decides it
# is whether the producer still needs one more write() after grep leaves, and that
# depends on the producer's own block size. Measured on this box, match on line 2,
# three runs each:
#
#     payload    cat        awk          grep upstream
#      16 KB     0 0 0      0 0 0        0   0   0
#      24 KB     0 0 0      141 141 141  0   141 141
#      32 KB     0 0 0      141 141 141  141 0   141
#     128 KB     141 ...    141 ...      141 141 141
#
# Read the third column again. Same command, same input, THREE DIFFERENT ANSWERS. A
# line-buffered producer makes this a race, not a threshold, so "it passed when I ran
# it" is not evidence about anything but that run.
#
# A shell BUILTIN producer is not exempt either: bash forks a subshell for printf and
# echo inside a pipeline, and that subshell takes the SIGPIPE. Builtins write in one
# large block, so they hold out longer — to about 64 KB here — which only means they
# fail later and more surprisingly.
#
# One real exemption: grep must reach a line terminator before it can conclude, so a
# producer that emits ONE unbroken line never orphans itself. 900 KB on a single line
# exits 0; the same 900 KB split into ten thousand lines exits 141. Treat that as an
# implementation detail of this grep, not as a licence.
#
# IN A MULTI-STAGE PIPELINE, judge the stage DIRECTLY upstream of the early-exit
# consumer, not the original input. A filtering middle stage collapses the payload, so
# the pipeline cannot bite however large the input grows. A pass-through middle stage
# — grep -v, sed, tr — scales with the input and bites normally. Two sites four lines
# apart in the same file in this repo split exactly that way.
#
# So a site is harmless only when the producer's WHOLE output is reliably and
# permanently a few KB at most — a version string, one jq field, a status word, a
# percentage. Anything that grows with the project, a log, or a captured command's
# combined output is not, and 40 KB is not "comfortably under the buffer". Sites judged
# harmless are listed in scripts/pipefail-grepq-gate.allow, each with its reason.
#
# ONE TRAP WHEN YOU APPLY THE CURE. A here-string of an EMPTY capture is one empty
# line, not nothing. `grep -q PATTERN` is unaffected, but `grep -vq PATTERN` matches
# that empty line, so an empty producer flips from "nothing found" to "found". Guard
# an inverted match with a test that the capture is non-empty.
# An allow entry that matches nothing is itself a failure, so the list cannot rot
# into a set of stale excuses.
#
# NOTE TO ANYONE EDITING THIS FILE. This gate scans itself. The patterns below are
# written so that their own source text does not match them, which is why they are
# spelled with bracket classes instead of a literal pipe followed by grep. Keep it
# that way, and never write the banned shape in here.
#
# Exit 0 = clean; exit 1 = a site that can bite, or a stale allow entry.

set -uo pipefail

ROOT=""
ALLOW_FILE=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --root)  ROOT="$2"; shift 2 ;;
    --allow) ALLOW_FILE="$2"; shift 2 ;;
    -h|--help)
      sed -n '2,50p' "$0" | sed 's/^# \{0,1\}//'
      exit 0 ;;
    *) echo "pipefail-grepq-gate: unknown argument: $1" >&2; exit 2 ;;
  esac
done

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null)" || REPO_ROOT=""
if [[ -z "$REPO_ROOT" ]]; then
  REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
fi

# Scanning a root other than the repo is the self-test's seam. In that mode the
# built-in allow list is keyed to repo paths and cannot match, so the stale-entry
# check is switched off and only the allow file passed with --allow applies.
FIXTURE_MODE=0
if [[ -n "$ROOT" ]]; then
  FIXTURE_MODE=1
else
  ROOT="$REPO_ROOT"
fi
if [[ -z "$ALLOW_FILE" && $FIXTURE_MODE -eq 0 ]]; then
  ALLOW_FILE="$REPO_ROOT/scripts/pipefail-grepq-gate.allow"
fi

# ── Patterns ──────────────────────────────────────────────────────────────────
# A pipe, then grep, then a flag cluster ending in q — so -q, -qE, -Fxq, -iq and
# `-q --` all match, and so does --quiet. Written with bracket classes so this
# file does not flag its own source.
# The leading (^|[^|]) is load-bearing: without it `cmd || grep -qi PAT FILE` reads
# as a pipe into a quiet grep, and that logical-OR form has no pipe and no hazard.
# It was a false positive on the first run of this gate.
BANNED_GREPQ='(^|[^|])[|][[:space:]]*(LC_ALL=[A-Za-z0-9_.-]+[[:space:]]+)?e?grep[[:space:]]+(-[A-Za-z-]*[[:space:]]+)*(-[A-Za-z]*q|--quiet)'
# A slow streaming Go binary piped into head. `tail` is safe — it reads to the end.
BANNED_HEAD='(^|[[:space:]([{;&])(br|kerf|harmonik)[[:space:]][^|]*[^|][|][[:space:]]*head([[:space:]]|$)'
# Any form of `set` that turns pipefail on: -o pipefail, -euo pipefail, -eo pipefail.
PIPEFAIL_SET='set[[:space:]]+-[a-zA-Z]*o[[:space:]]+pipefail'

fail_count=0
report() {
  printf '  %s:%s\n      %s\n      -> %s\n' "$1" "$2" "$3" "$4"
}

# ── Collect the files to scan ─────────────────────────────────────────────────
files=()
if [[ $FIXTURE_MODE -eq 1 ]]; then
  while IFS= read -r f; do files+=("$f"); done < <(find "$ROOT" -type f -name '*.sh' | sort)
else
  # git ls-files, so ignored paths — including the throwaway worktrees under
  # .claude/worktrees — stay out, and a brand-new untracked script stays in.
  while IFS= read -r f; do
    files+=("$ROOT/$f")
  done < <(git -C "$ROOT" ls-files --cached --others --exclude-standard -- '*.sh' | sort)
fi

if [[ ${#files[@]} -eq 0 ]]; then
  echo "pipefail-grepq-gate: FAIL — found no .sh files under $ROOT; the scan would pass by finding nothing" >&2
  exit 1
fi

# ── Load the allow list ───────────────────────────────────────────────────────
# Format, one per line:   <repo-relative path>::<fixed text from the offending line>
# Blank lines and lines starting with # are comments and carry the reason.
allow_paths=()
allow_anchors=()
allow_hits=()
if [[ -n "$ALLOW_FILE" ]]; then
  if [[ ! -f "$ALLOW_FILE" ]]; then
    echo "pipefail-grepq-gate: FAIL — allow list $ALLOW_FILE is missing" >&2
    exit 1
  fi
  while IFS= read -r line; do
    [[ -z "$line" || "$line" == \#* ]] && continue
    allow_paths+=("${line%%::*}")
    allow_anchors+=("${line#*::}")
    allow_hits+=(0)
  done < "$ALLOW_FILE"
fi

is_allowed() {  # $1 = repo-relative path, $2 = the offending line
  local i
  for i in "${!allow_paths[@]}"; do
    if [[ "${allow_paths[$i]}" == "$1" ]] && [[ "$2" == *"${allow_anchors[$i]}"* ]]; then
      allow_hits[$i]=1
      return 0
    fi
  done
  return 1
}

# ── Scan ──────────────────────────────────────────────────────────────────────
# Two grep invocations over the whole set, not two per file. Per-file invocations
# cost about two seconds on this tree, and freeze-gates is the inner loop.
#
# Every grep here reads files by name. Nothing is sent through a pipe to answer
# the question, because that is the defect this gate exists to catch.
findings=""

pipefail_files=()
while IFS= read -r f; do
  [[ -n "$f" ]] && pipefail_files+=("$f")
done < <(grep -lE "$PIPEFAIL_SET" "${files[@]}" 2>/dev/null || true)

if [[ ${#pipefail_files[@]} -gt 0 ]]; then
  # -H so a single-file set still carries the filename, the way a multi-file set does.
  hits_grepq="$(grep -HnE "$BANNED_GREPQ" "${pipefail_files[@]}" 2>/dev/null || true)"
  hits_head="$(grep -HnE "$BANNED_HEAD" "${pipefail_files[@]}" 2>/dev/null || true)"
  all_hits="$(sort -u <<<"$hits_grepq"$'\n'"$hits_head" | sed '/^$/d')"

  while IFS= read -r hit; do
    [[ -z "$hit" ]] && continue
    path="${hit%%:*}"
    rest="${hit#*:}"
    lineno="${rest%%:*}"
    text="${rest#*:}"
    rel="${path#$ROOT/}"
    # A commented-out line does not execute, so it cannot bite. Several scripts
    # here describe the hazard in a comment, and flagging those would punish the
    # only files that document it.
    trimmed="${text#"${text%%[![:space:]]*}"}"
    [[ "$trimmed" == \#* ]] && continue
    if is_allowed "$rel" "$text"; then
      continue
    fi
    findings+="$(report "$rel" "$lineno" "$trimmed" "capture the producer first, then match the capture with a here-string")"$'\n'
    fail_count=$((fail_count + 1))
  done <<<"$all_hits"
fi

# ── Scope guard: the Makefile must keep running without pipefail ──────────────
if [[ $FIXTURE_MODE -eq 0 && -f "$REPO_ROOT/Makefile" ]]; then
  if grep -Eq '^[[:space:]]*(SHELL|\.SHELLFLAGS)[[:space:]]*[:+?]?=.*pipefail' "$REPO_ROOT/Makefile" 2>/dev/null; then
    echo "pipefail-grepq-gate: FAIL — the Makefile now turns pipefail on for its recipes." >&2
    echo "  This gate only scans .sh files. Extend it to cover recipe bodies, then" >&2
    echo "  update the WHAT IT DOES NOT COVER note in its header." >&2
    fail_count=$((fail_count + 1))
  fi
fi

# ── Stale allow entries ───────────────────────────────────────────────────────
if [[ $FIXTURE_MODE -eq 0 ]]; then
  for i in "${!allow_paths[@]}"; do
    if [[ "${allow_hits[$i]}" -eq 0 ]]; then
      echo "pipefail-grepq-gate: FAIL — stale allow entry, it matches nothing:" >&2
      echo "    ${allow_paths[$i]}::${allow_anchors[$i]}" >&2
      echo "  The site was fixed or moved. Delete the entry from $ALLOW_FILE." >&2
      fail_count=$((fail_count + 1))
    fi
  done
fi

# ── Verdict ───────────────────────────────────────────────────────────────────
if [[ $fail_count -gt 0 ]]; then
  if [[ -n "$findings" ]]; then
    echo "pipefail-grepq-gate: FAIL — a shell script with pipefail on sends a producer into a consumer that leaves early." >&2
    printf '%s' "$findings" >&2
    echo "  Each of these reports a match as a failure once the payload outgrows the pipe" >&2
    echo "  buffer. A negative assertion written this way can never fail at all." >&2
    echo "  Fix:  out=\$(producer); grep -q PATTERN <<<\"\$out\"" >&2
    echo "  If a site genuinely cannot bite — the producer's whole output is reliably" >&2
    echo "  under the 64 KB pipe buffer — add it to" >&2
    echo "  ${ALLOW_FILE:-the allow list} with the reason." >&2
  fi
  exit 1
fi

echo "pipefail-grepq-gate: ok — scanned ${#files[@]} shell files under $ROOT, ${#allow_paths[@]} allowed sites, no unguarded early-exit pipe under pipefail"
exit 0
