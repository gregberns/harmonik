#!/usr/bin/env bash
# secret-scan-test.sh — the assertions for scripts/secret-scan.sh.
#
# This scan had no test at all, and it failed open on the one input it exists
# to catch. It piped the whole staged diff into `grep -q`. `grep -q` leaves at
# the first match, the writer takes SIGPIPE, and `pipefail` reports the writer's
# death as the pipeline's status — so a match read as no-match. A tiny staged
# diff was blocked and an ordinary-sized one carrying the same key went through.
#
# So the case that matters here is the LARGE one. A test that stages forty
# bytes passes against the defect it was written for and measures nothing.

set -uo pipefail

SCAN="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)/secret-scan.sh"
PASS=0
FAIL=0

check() {
  local label="$1" expected="$2" actual="$3"
  if [ "$expected" = "$actual" ]; then
    PASS=$(( PASS + 1 ))
  else
    FAIL=$(( FAIL + 1 ))
    echo "FAIL: ${label} — expected exit ${expected}, got ${actual}"
  fi
}

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

repo="$tmp/repo"
mkdir -p "$repo"
git -C "$repo" init -q
git -C "$repo" config user.email 'test@example.invalid'
git -C "$repo" config user.name 'secret-scan-test'
git -C "$repo" config commit.gpgsign false

# A credential shape the scan claims to catch. Split so this file does not
# itself trip the scan when it is staged.
FAKE_KEY="ANTHROPIC_API""_KEY=sk0000abcdefghijklmnopqrstuvwxyz0123456789"

reset_stage() {
  rm -f "$repo"/*.txt "$repo"/.env 2>/dev/null || true
  git -C "$repo" rm -rq --cached . 2>/dev/null || true
}

run_scan() {
  ( cd "$repo" && bash "$SCAN" >"$tmp/out" 2>&1 )
}

# 1. Nothing staged is not a finding.
reset_stage
run_scan
check "an empty stage passes" 0 "$?"

# 2. A small staged diff carrying a key is blocked. This is the case that
#    passed even while the scan was broken.
reset_stage
printf '%s\n' "$FAKE_KEY" >"$repo/small.txt"
git -C "$repo" add small.txt
run_scan
check "a key in a small staged diff is blocked" 1 "$?"

# 3. THE ONE THAT MATTERS. The same key inside an ordinary-sized commit. Under
#    the old piped form this exited 0 and admitted the secret.
reset_stage
{
  printf '%s\n' "$FAKE_KEY"
  # ~400 KB of ordinary content after the match, so the writer is still writing
  # when a matching reader would leave.
  for i in $(seq 1 8000); do
    printf 'line %d: ordinary source text that carries no credential at all\n' "$i"
  done
} >"$repo/large.txt"
git -C "$repo" add large.txt
size=$(wc -c <"$repo/large.txt")
run_scan
check "a key inside a ${size}-byte staged diff is blocked" 1 "$?"

# 4. The match must be reported, not merely counted. A scan that exits 1 with
#    no reason is not actionable.
if grep -q "BLOCKED" "$tmp/out"; then
  PASS=$(( PASS + 1 ))
else
  FAIL=$(( FAIL + 1 ))
  echo "FAIL: the large-diff block printed no reason"
  sed 's/^/    /' "$tmp/out"
fi

# 5. A large staged diff with no credential in it passes. Without this, a scan
#    that blocked everything would satisfy case 3 and be useless.
reset_stage
for i in $(seq 1 8000); do
  printf 'line %d: ordinary source text that carries no credential at all\n' "$i"
done >"$repo/clean.txt"
git -C "$repo" add clean.txt
run_scan
check "a large staged diff with no credential passes" 0 "$?"

# 6. A staged .env file is blocked whatever it holds.
reset_stage
printf 'HARMLESS=1\n' >"$repo/.env"
git -C "$repo" add -f .env
run_scan
check "a staged .env file is blocked" 1 "$?"

# ---------------------------------------------------------------------------
# EVERY PATTERN, not just the first one. Deleting any of the other six from the
# list left this suite green, so six sevenths of the scan was unmeasured — and
# the private-key one had never matched anything in its life.
#
# Each fixture is split across two string literals so that staging THIS file
# cannot trip the scan on itself.
# ---------------------------------------------------------------------------
declare -a PATTERN_FIXTURES=(
  "anthropic-env|ANTHROPIC_API""_KEY=sk0000abcdefghijklmnopqrstuvwxyz0123456789"
  "anthropic-key|sk-ant-""api1-abcdefghij0123456789"
  "anthropic-bare|sk-ant-""abcdefghijklmnopqrstuvwxyz0123456789abcdef"
  "aws-secret|AWS_SECRET_ACCESS""_KEY=abcdefghijklmnopqrstuvwxyz0123456789"
  "github-token|GITHUB""_TOKEN=ghp_abcdefghijklmnopqrstuvwxyz0123"
  "openai-key|OPENAI_API""_KEY=sk-abcdefghijklmnopqrstuvwxyz0123456789"
  "private-key|-----BEGIN RSA PRIVATE ""KEY-----"
)

for fixture in "${PATTERN_FIXTURES[@]}"; do
  label="${fixture%%|*}"
  secret="${fixture#*|}"
  reset_stage
  printf '%s\n' "$secret" >"$repo/${label}.txt"
  git -C "$repo" add "${label}.txt"
  run_scan
  check "the ${label} pattern is caught" 1 "$?"
done

# The key at the END of a large diff. Every case above puts it at the start,
# which is right for reproducing the SIGPIPE race and wrong for proving the
# scan reads the whole staged diff. Truncating the extraction to the first
# twenty lines left this suite green before this case existed.
reset_stage
{
  for i in $(seq 1 8000); do
    printf 'line %d: ordinary source text that carries no credential at all\n' "$i"
  done
  printf '%s\n' "$FAKE_KEY"
} >"$repo/tail.txt"
git -C "$repo" add tail.txt
run_scan
check "a key at the END of a large staged diff is caught" 1 "$?"

# A secret added to a file that already exists. Every case above stages a NEW
# file, so narrowing the diff filter to added-files-only left the suite green —
# and a secret pasted into an existing file is the ordinary way this happens.
reset_stage
printf 'nothing secret here\n' >"$repo/existing.txt"
git -C "$repo" add existing.txt
git -C "$repo" commit -q -m 'seed an existing file'
printf '%s\n' "$FAKE_KEY" >>"$repo/existing.txt"
git -C "$repo" add existing.txt
run_scan
check "a key added to an EXISTING file is caught" 1 "$?"
git -C "$repo" rm -q --cached existing.txt >/dev/null 2>&1 || true

# The .env family, not only the bare name. Narrowing the match to a literal
# ".env" left the suite green while ".env.local" and ".env-prod" are the names
# people actually use.
for envname in .env.local .env-prod; do
  reset_stage
  rm -f "$repo/.env.local" "$repo/.env-prod"
  printf 'HARMLESS=1\n' >"$repo/${envname}"
  git -C "$repo" add -f "$envname"
  run_scan
  check "a staged ${envname} is blocked" 1 "$?"
  rm -f "$repo/${envname}"
done

# A scan that cannot run is not a clean result. grep says 2 or more when it
# could not do the job, and reading that as "no secret found" is how the
# private-key pattern stayed broken for the life of the script.
reset_stage
printf 'nothing secret here\n' >"$repo/plain.txt"
git -C "$repo" add plain.txt
brokendir="$tmp/brokenbin"
mkdir -p "$brokendir"
printf '#!/bin/sh\nexit 2\n' >"$brokendir/grep"
chmod +x "$brokendir/grep"
( cd "$repo" && PATH="$brokendir:$PATH" bash "$SCAN" >"$tmp/out" 2>&1 )
check "a grep that cannot run blocks rather than passing" 1 "$?"
if grep -q "could not run" "$tmp/out"; then
  PASS=$(( PASS + 1 ))
else
  FAIL=$(( FAIL + 1 ))
  echo "FAIL: the unrunnable-scan refusal did not say the scan could not run"
  sed 's/^/    /' "$tmp/out"
fi

echo "secret-scan-test: $(( PASS + FAIL )) assertions, ${FAIL} failed"
[ "$FAIL" -eq 0 ] || exit 1
