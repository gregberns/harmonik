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
#
# The second half of this file covers the two committed scopes, --head-only and
# --range. They exist because every gate in this repo runs after the commit is
# made, so a scan of the index alone could not be wired to one without being a
# no-op. The wiring itself is asserted in scripts/gate-fails-closed-test.sh: no
# test in THIS file can tell whether anything calls this script, and for the
# whole life of the scan nothing did.

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

# The committed-scope cases below each need their OWN value. Two files that
# share a line are similar enough for git to call the second one a RENAME of the
# first, and in a rename the shared line is context, not an added line — so a
# case that reused one value was measuring rename detection rather than the
# scan.
KEY_TIP="ANTHROPIC_API""_KEY=sk1111zyxwvutsrqponmlkjihgfedcba9876543210"
KEY_LANE="OPENAI_API""_KEY=sk-lane8877665544332211aabbccddeeff0099"
KEY_RENAME="GITHUB""_TOKEN=ghp_renamedfilecarriesakey0123456789"

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

# 4. The match must be reported as a MATCH, not merely as a block. The word
#    BLOCKED alone is too weak an assertion to be worth anything here: the scan
#    prints it for a find AND for "the scan could not run", so a pipe put back
#    into the extraction leaves this green while every real find is reported as
#    a breakage. Measured — the product still failed closed, and this suite
#    could not tell the two apart. Assert on the sentence that only a real match
#    produces.
if grep -q "matches pattern" "$tmp/out"; then
  PASS=$(( PASS + 1 ))
else
  FAIL=$(( FAIL + 1 ))
  echo "FAIL: the large-diff block did not report a pattern match (a broken scan reports BLOCKED too)"
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

# ---------------------------------------------------------------------------
# THE COMMITTED SCOPES — --head-only and --range.
#
# WHY THESE EXIST. The scan read the INDEX and nothing else, and every gate in
# this repo runs AFTER the commit is made, when the ordinary flow leaves nothing
# staged. So the scan could not be wired anywhere without installing a step that
# reads whatever happened to be left in the index rather than the change under
# test — not a GUARANTEED no-op, since a staged key does still block, but a step
# that is silent whenever it answers nothing. The first case below is the one
# that pins that: the secret is committed, the index is verified EMPTY, and the
# scan must still refuse. Under the old script that case exits 0.
# ---------------------------------------------------------------------------

run_scan_args() {
  ( cd "$repo" && bash "$SCAN" "$@" >"$tmp/out" 2>&1 )
}

# The scratch repo has its own history, so it gets its own baseline. Without
# this the range mode would look for harmonik's baseline commit, not find it,
# and fall back to the tip — which is a real behaviour worth testing, and it is
# tested on purpose further down rather than by accident here.
first_commit=$(git -C "$repo" rev-list --max-parents=0 HEAD | tail -1)

# 20. A key in the commit just made is refused, with an EMPTY index.
#
#     THE FILLER AFTER THE KEY IS LOAD-BEARING, and it is here for case 24 below
#     rather than for this one. Case 24 asks whether the finding was reported as
#     a MATCH or as "the scan could not run", which is the assertion that tells a
#     working scan from one whose reader was put back behind a pipe. With an
#     84-byte fixture the writer finishes before the reader can leave, no SIGPIPE
#     happens, and that mutant stays green. Measured: at 84 bytes the pipe mutant
#     survives case 24; with the filler below it dies.
reset_stage
{
  printf '%s\n' "$FAKE_KEY"
  for i in $(seq 1 8000); do
    printf 'line %d: ordinary source text that carries no credential at all\n' "$i"
  done
} >"$repo/committed.txt"
git -C "$repo" add committed.txt
git -C "$repo" commit -q -m 'commit a secret'
staged_now=$(git -C "$repo" diff --cached --name-only)
if [ -n "$staged_now" ]; then
  FAIL=$(( FAIL + 1 ))
  echo "FAIL: the committed-scope case did not start from an empty index"
fi
run_scan_args --head-only
check "--head-only refuses a key in the commit just made (index empty)" 1 "$?"

# 21. And the default scope agrees the index is clean at that same moment. This
#     is the proof that wiring the OLD script into a gate would have been a
#     no-op: same repository, same second, same secret, and it passes.
run_scan
check "the index scope passes at that same moment — which is why --head-only had to exist" 0 "$?"

# 22. A clean commit is not refused. Without this, a --head-only that blocked
#     everything would satisfy case 20 and be useless.
printf 'ordinary source line\n' >"$repo/plainfile.txt"
git -C "$repo" add plainfile.txt
git -C "$repo" commit -q -m 'commit something ordinary'
run_scan_args --head-only
check "--head-only passes a clean commit" 0 "$?"

# 23. The range reaches back past the tip. The secret is in the tree but NOT in
#     the commit just made, so only a range scan can see it.
SECRET_SCAN_BASELINE="$first_commit" run_scan_args --range
check "--range finds a key that an earlier commit in the range added" 1 "$?"

# 24. And it REFUSES rather than reporting, for the RIGHT reason. The
#     commit-message gate's range is advisory because a bad message on a merged
#     commit has no legal repair. A leaked credential has one — rotate the key —
#     so this range fails. Same tightening as case 4: "BLOCKED" alone is also
#     what a scan that could not run prints.
if grep -q "matches pattern" "$tmp/out"; then
  PASS=$(( PASS + 1 ))
else
  FAIL=$(( FAIL + 1 ))
  echo "FAIL: the range finding was not reported as a pattern match"
  sed 's/^/    /' "$tmp/out"
fi

# 25. A baseline this repository does not have must not read as clean. It falls
#     back to the tip, which is a weaker claim, and it still refuses.
reset_stage
printf '%s\n' "$KEY_TIP" >"$repo/tipsecret.txt"
git -C "$repo" add tipsecret.txt
git -C "$repo" commit -q -m 'another committed secret'
SECRET_SCAN_BASELINE=0000000000000000000000000000000000000000 run_scan_args --range
check "--range with an unknown baseline falls back to the tip and still refuses" 1 "$?"

# 26. An empty range is the no-op this whole change is about. HEAD IS the
#     baseline here, so BASELINE..HEAD holds nothing; the scan must read the tip
#     rather than read nothing and call it clean.
head_now=$(git -C "$repo" rev-parse HEAD)
SECRET_SCAN_BASELINE="$head_now" run_scan_args --range
check "--range with an empty range reads the tip instead of passing on nothing" 1 "$?"

# 27. A secret that arrives by MERGE is in scope for the tip check. The merge is
#     the moment this branch takes the content on, and it is the last moment the
#     merge can be redone.
git -C "$repo" rm -q tipsecret.txt
git -C "$repo" commit -q -m 'remove the committed secrets'
git -C "$repo" checkout -q -b sidelane
printf '%s\n' "$KEY_LANE" >"$repo/fromlane.txt"
git -C "$repo" add fromlane.txt
git -C "$repo" commit -q -m 'a lane adds a secret'
git -C "$repo" checkout -q -
git -C "$repo" merge -q --no-ff -m 'merge the lane' sidelane
run_scan_args --head-only
check "--head-only refuses a secret that arrived by merge" 1 "$?"

# 28. A clean range passes. The same argument as case 22, for the other scope.
git -C "$repo" rm -q fromlane.txt
git -C "$repo" commit -q -m 'remove the merged secret'
clean_base=$(git -C "$repo" rev-parse HEAD)
printf 'ordinary source line two\n' >"$repo/plainfile2.txt"
git -C "$repo" add plainfile2.txt
git -C "$repo" commit -q -m 'ordinary work on top'
SECRET_SCAN_BASELINE="$clean_base" run_scan_args --range
check "--range passes a range with no credential in it" 0 "$?"

# 29. An argument the scan does not understand is refused rather than silently
#     read as the default scope. A typo'd flag that scanned the empty index
#     would be the no-op again, wearing a flag.
run_scan_args --scan-everything
check "an unknown argument is refused" 1 "$?"

# 30. A key added inside a RENAMED file. The scan asked git for
#     `--diff-filter=ACM`, which drops a rename (R) from the diff altogether, so
#     this commit produced an EMPTY diff and read as clean while the key sat in
#     the tree. Found by this suite, in this scratch repository, while the
#     committed scopes above were being written.
for i in $(seq 1 60); do
  printf 'line %d: ordinary source text that carries no credential at all\n' "$i"
done >"$repo/tobemoved.txt"
git -C "$repo" add tobemoved.txt
git -C "$repo" commit -q -m 'a file that is about to move'
git -C "$repo" mv tobemoved.txt renamed.txt
printf '%s\n' "$KEY_RENAME" >>"$repo/renamed.txt"
git -C "$repo" add renamed.txt
git -C "$repo" commit -q -m 'rename a file and add a key to it'
if [ -z "$(git -C "$repo" diff --diff-filter=ACM --name-only HEAD^ HEAD)" ]; then
  PASS=$(( PASS + 1 ))
else
  FAIL=$(( FAIL + 1 ))
  echo "FAIL: the rename case no longer produces a rename, so it measures nothing"
fi
run_scan_args --head-only
check "a key added inside a RENAMED file is caught" 1 "$?"

# 31. A .env under a directory whose name is not plain ASCII. With git's default
#     core.quotepath, `--name-only` renders that path as a C quoted string —
#     "caf\303\251 dir/.env" — and the closing quote defeats a pattern that
#     anchors .env at the END of the line, so the file passed. Measured against
#     the old form: the quoted name matched nothing. The fixture guard below is
#     the load-bearing half; without it this case would go green on a git that
#     stopped quoting, while measuring nothing.
mkdir -p "$repo/café dir"
printf 'HARMLESS=1\n' >"$repo/café dir/.env"
git -C "$repo" add -f "café dir/.env"
git -C "$repo" commit -q -m 'a .env under a directory with an accent in its name'
quoted_name=$(git -C "$repo" diff --name-only HEAD^ HEAD)
case "$quoted_name" in
  '"'*) PASS=$(( PASS + 1 )) ;;
  *)    FAIL=$(( FAIL + 1 ))
        echo "FAIL: git no longer quotes the non-ASCII path (${quoted_name}), so this case measures nothing" ;;
esac
run_scan_args --head-only
check "a .env under a non-ASCII directory name is blocked" 1 "$?"

# 32. A .env that arrives as a TYPECHANGE — a symlink where a regular file was,
#     or the reverse. `--diff-filter=ACMR` drops T, so the name list came back
#     EMPTY for such a commit and it read as clean. The guard asserts ACMR
#     really would have missed it, so the case cannot pass for the wrong reason.
printf 'HARMLESS=1\n' >"$repo/.env"
git -C "$repo" add -f .env
git -C "$repo" commit -q -m 'a regular .env, about to change type'
rm -f "$repo/.env"
ln -s plainfile.txt "$repo/.env"
git -C "$repo" add -f .env
git -C "$repo" commit -q -m 'the .env becomes a symlink'
if [ -z "$(git -C "$repo" diff --name-only --diff-filter=ACMR HEAD^ HEAD)" ] \
   && [ -n "$(git -C "$repo" diff --name-only --diff-filter=T HEAD^ HEAD)" ]; then
  PASS=$(( PASS + 1 ))
else
  FAIL=$(( FAIL + 1 ))
  echo "FAIL: the typechange case no longer produces a T that ACMR would drop, so it measures nothing"
fi
run_scan_args --head-only
check "a .env that arrives as a TYPECHANGE is blocked" 1 "$?"
git -C "$repo" rm -q .env "café dir/.env"
git -C "$repo" commit -q -m 'remove the env files again'

# 33. THE FINDING MUST NOT PUBLISH THE CREDENTIAL. The output of this scan goes
#     to a CI log, and a CI log has a wider audience than the tree the value sits
#     in, so a report that prints the matching line leaks the key further than
#     the commit did. It must still be actionable: the path and the line number
#     have to be there, or nobody can act on the refusal.
#
#     The filler after the key is here for the same reason as case 20's: the
#     "where is it" assertion below only fires on the real-match path, so it is
#     the assertion that kills a reader put back behind a pipe — and it can only
#     do that if the writer is still writing when the reader leaves. At three
#     lines the mutant survived this case.
KEY_REDACT="ANTHROPIC_API""_KEY=sk2222qqqqwwwweeeerrrrttttyyyyuuuuiiiioooo"
KEY_TAIL="sk2222qqqqwwwweeeerrrrttttyyyyuuuuiiiioooo"
mkdir -p "$repo/pkg"
{
  printf 'first line\nsecond line\n%s\n' "$KEY_REDACT"
  for i in $(seq 1 8000); do
    printf 'line %d: ordinary source text that carries no credential at all\n' "$i"
  done
} >"$repo/pkg/config.go"
git -C "$repo" add pkg/config.go
git -C "$repo" commit -q -m 'commit a key three lines into a file'
run_scan_args --head-only
check "--head-only blocks the redaction fixture" 1 "$?"
if grep -qF -- "$KEY_TAIL" "$tmp/out"; then
  FAIL=$(( FAIL + 1 ))
  echo "FAIL: the refusal printed the credential itself into the log"
else
  PASS=$(( PASS + 1 ))
fi
if grep -qF -- 'pkg/config.go:3' "$tmp/out"; then
  PASS=$(( PASS + 1 ))
else
  FAIL=$(( FAIL + 1 ))
  echo "FAIL: the refusal did not say where the match is, so it is not actionable"
  sed 's/^/    /' "$tmp/out"
fi

# 34. EVERY ARGUMENT POSITION IS READ. The scan looked at "$1" alone, so
#     `--head-only --range` ran the head scope and `--head-only --garbage` ran it
#     too — a caller that asked for the wide scope, or that made a typo, got a
#     narrow answer and exit 0 with no word about it. The tip is CLEAN at this
#     point on purpose: that is what makes exit 0 the old answer and exit 1 a
#     real change.
printf 'ordinary source line three\n' >"$repo/plainfile3.txt"
git -C "$repo" add plainfile3.txt
git -C "$repo" commit -q -m 'an ordinary clean commit'
run_scan_args --head-only
check "the tip really is clean, so a wrong-argument pass would read as exit 0" 0 "$?"
run_scan_args --head-only --range
check "two scopes at once are refused" 1 "$?"
run_scan_args --head-only --garbage
check "an unknown argument AFTER a good one is refused" 1 "$?"
run_scan_args --garbage --head-only
check "an unknown argument BEFORE a good one is refused" 1 "$?"

# 35. A grep that cannot do the .env search blocks. That search carried
#     `|| true`, which folded grep's exit 2 — a real error — into "no .env file
#     in scope", so the one negative assertion in this script could never fail.
#     The stub breaks ONLY the .env search and lets the content greps work, which
#     is the case the earlier all-greps-broken fixture cannot reach.
real_grep=$(command -v grep)
envbrokendir="$tmp/envbrokenbin"
mkdir -p "$envbrokendir"
{
  printf '#!/bin/sh\n'
  printf 'for a in "$@"; do\n'
  printf '  case "$a" in *.env*) exit 2 ;; esac\n'
  printf 'done\n'
  printf 'exec %s "$@"\n' "$real_grep"
} >"$envbrokendir/grep"
chmod +x "$envbrokendir/grep"
( cd "$repo" && PATH="$envbrokendir:$PATH" bash "$SCAN" --head-only >"$tmp/out" 2>&1 )
check "a grep that cannot search the file list blocks rather than passing" 1 "$?"
if grep -q "could not run" "$tmp/out"; then
  PASS=$(( PASS + 1 ))
else
  FAIL=$(( FAIL + 1 ))
  echo "FAIL: the broken .env search did not say the scan could not run"
  sed 's/^/    /' "$tmp/out"
fi

# ---------------------------------------------------------------------------
# THE DIFF IS PARSED, NOT PATTERN-MATCHED LINE BY LINE.
#
# The scan used to decide what a diff line was from its first characters. It read
# every line starting with '+', then threw away every line starting with '+++' on
# the grounds that those are 'b/path' file headers. A SOURCE line that itself
# begins with two plus signs renders in a diff as '+++…' — the diff's own marker,
# then the line's own two — so it was thrown away with the headers and no
# credential pattern ever saw it. A key on such a line read CLEAN in every scope,
# exit 0, with the key sitting at HEAD.
#
# It is not a hypothetical shape. Eleven tracked files in harmonik carry '++'
# lines today, seven of them committed .patch and .diff dumps under
# .harmonik/context/, .kerf/works/ and plans/ — agent-produced patch dumps are
# exactly where a
# key lands by accident.
#
# The same guess corrupted the finding's LOCATION: the index took such a line for
# a file header, set the path to the line's own text and skipped its line
# counter, so a real finding was reported at a path that does not exist, one line
# off. That is not a fail-open by itself, and it makes a refusal unactionable.
#
# The suite could see NONE of this. It stood at 44 assertions and 0 failed, at
# the time, while the index used a different rule from the added-lines file.
# That number is a record of that moment and not of this file, which now runs
# 102.
# ---------------------------------------------------------------------------

# 36. A key on a line that begins with two plus signs. Every scope must refuse.
KEY_PLUSPLUS="ANTHROPIC_API""_KEY=sk4444plusplusaaaabbbbccccdddd0123456789"
KEY_PLUSPLUS_STAGED="OPENAI_API""_KEY=sk-plusplusstaged55554444333322221111"
plusplus_base=$(git -C "$repo" rev-parse HEAD)
{
  printf '++%s\n' "$KEY_PLUSPLUS"
  for i in $(seq 1 8000); do
    printf 'line %d: ordinary source text that carries no credential at all\n' "$i"
  done
} >"$repo/plusplus.txt"
git -C "$repo" add plusplus.txt
git -C "$repo" commit -q -m 'a key on a line that begins with two plus signs'
#     The fixture guard, and it is the load-bearing half. Without it this case
#     would go green on a git that rendered the line some other way, while
#     measuring nothing.
git -C "$repo" diff HEAD^ HEAD >"$tmp/pp-diff"
if grep -qE '^\+\+\+[^ ]' "$tmp/pp-diff"; then
  PASS=$(( PASS + 1 ))
else
  FAIL=$(( FAIL + 1 ))
  echo "FAIL: the fixture no longer renders as a '+++'-prefixed content line, so this case measures nothing"
fi
run_scan_args --head-only
check "--head-only refuses a key on a line beginning with two plus signs" 1 "$?"
if grep -q "matches pattern" "$tmp/out"; then
  PASS=$(( PASS + 1 ))
else
  FAIL=$(( FAIL + 1 ))
  echo "FAIL: the two-plus-signs finding was not reported as a pattern match"
  sed 's/^/    /' "$tmp/out"
fi
if grep -qF -- 'plusplus.txt:1' "$tmp/out"; then
  PASS=$(( PASS + 1 ))
else
  FAIL=$(( FAIL + 1 ))
  echo "FAIL: the two-plus-signs finding was not located at plusplus.txt:1"
  sed 's/^/    /' "$tmp/out"
fi
SECRET_SCAN_BASELINE="$plusplus_base" run_scan_args --range
check "--range refuses a key on a line beginning with two plus signs" 1 "$?"
if grep -q "matches pattern" "$tmp/out"; then
  PASS=$(( PASS + 1 ))
else
  FAIL=$(( FAIL + 1 ))
  echo "FAIL: the two-plus-signs range finding was not reported as a pattern match"
  sed 's/^/    /' "$tmp/out"
fi
#     And the index scope, which is the scope the reviewer reproduced it in. The
#     empty-index run first is what makes the second one mean something: exit 0
#     then exit 1 is a change this fixture caused, rather than a repository that
#     was already dirty.
git -C "$repo" reset -q
run_scan
check "the index is clean before the two-plus-signs file is staged" 0 "$?"
printf '++%s\n' "$KEY_PLUSPLUS_STAGED" >"$repo/plusplus-staged.txt"
git -C "$repo" add plusplus-staged.txt
run_scan
check "the index scope refuses a key on a line beginning with two plus signs" 1 "$?"
git -C "$repo" reset -q
rm -f "$repo/plusplus-staged.txt"

# 37. THE LOCATION SURVIVES A '++' LINE. The path and the line number have to be
#     right, or the refusal cannot be acted on. Against the old index this
#     reported the line's own text as the path, one line early.
KEY_FIRSTLINE="ANTHROPIC_API""_KEY=sk5555firstlineaaaabbbbccccdddd01234567"
mkdir -p "$repo/pkg37"
printf '++ two plus signs and a space\nan ordinary line\n%s\n' "$KEY_FIRSTLINE" >"$repo/pkg37/config.go"
git -C "$repo" add pkg37/config.go
git -C "$repo" commit -q -m 'a file whose first line begins with two plus signs'
run_scan_args --head-only
check "--head-only refuses a key under a leading two-plus-signs line" 1 "$?"
if grep -qF -- 'pkg37/config.go:3' "$tmp/out"; then
  PASS=$(( PASS + 1 ))
else
  FAIL=$(( FAIL + 1 ))
  echo "FAIL: the finding was not located at pkg37/config.go:3 (a leading '++' line moved it)"
  sed 's/^/    /' "$tmp/out"
fi

# 38. CONTENT THAT LOOKS LIKE A DIFF. A committed .patch or .diff file holds
#     'diff --git', '--- a/…', '+++ b/…', '@@ -1 +1 @@' and lines that start with
#     '+' and '-' as its own text. A parser that reads the first characters of a
#     line to decide what the line IS gets every one of them wrong. This project
#     commits such files, which is why the carrier is real.
KEY_PATCHDUMP="ANTHROPIC_API""_KEY=sk6666patchdumpaaaabbbbccccdddd01234567"
mkdir -p "$repo/pkg38"
{
  printf 'diff --git a/fake b/fake\n'
  printf -- '--- a/fake\n'
  printf '+++ b/fake\n'
  printf '@@ -1 +1 @@\n'
  printf -- '-an old line\n'
  printf '+a new line\n'
  printf '++ two plus signs and a space\n'
  printf '++%s\n' "$KEY_PATCHDUMP"
} >"$repo/pkg38/patchdump.txt"
git -C "$repo" add pkg38/patchdump.txt
git -C "$repo" commit -q -m 'commit a patch dump that carries a key on its last line'
run_scan_args --head-only
check "--head-only refuses a key inside a file whose content looks like a diff" 1 "$?"
if grep -qF -- 'pkg38/patchdump.txt:8' "$tmp/out"; then
  PASS=$(( PASS + 1 ))
else
  FAIL=$(( FAIL + 1 ))
  echo "FAIL: the finding was not located at pkg38/patchdump.txt:8 (the diff-shaped content moved it)"
  sed 's/^/    /' "$tmp/out"
fi
#     And the same content with no key in it passes. Without this, a parser that
#     refused every diff-shaped file would satisfy the case above and be useless.
{
  printf 'diff --git a/fake b/fake\n'
  printf -- '--- a/fake\n'
  printf '+++ b/fake\n'
  printf '@@ -1 +1 @@\n'
  printf -- '-an old line\n'
  printf '+a new line\n'
  printf '++ two plus signs and a space\n'
  printf '++an ordinary closing line\n'
} >"$repo/pkg38/cleandump.txt"
git -C "$repo" add pkg38/cleandump.txt
git -C "$repo" commit -q -m 'commit a patch dump with no key in it'
run_scan_args --head-only
check "--head-only passes a diff-shaped file with no credential in it" 0 "$?"

# 39. MANY FILES, MANY HUNKS, EACH FINDING AT ITS OWN PLACE. One hunk in one new
#     file is the easy case, and it is the only case the suite had. Here the
#     first file is MODIFIED in two places far enough apart to make two hunks,
#     the second file is new, and both carry a '++' line ahead of their key —
#     which is what a patch dump pasted into a source tree looks like.
KEY_MULTI_A="ANTHROPIC_API""_KEY=sk7777multiaaaabbbbccccddddeeee01234567"
KEY_MULTI_B="ANTHROPIC_API""_KEY=sk8888multibbbbccccddddeeeeffff01234567"
KEY_MULTI_C="ANTHROPIC_API""_KEY=sk9999multiccccddddeeeeffffgggg01234567"
mkdir -p "$repo/multi"
for i in $(seq 1 200); do
  printf 'line %d: ordinary source text that carries no credential at all\n' "$i"
done >"$repo/multi/a.txt"
git -C "$repo" add multi/a.txt
git -C "$repo" commit -q -m 'a file that is about to grow two hunks'
{
  for i in $(seq 1 4); do
    printf 'line %d: ordinary source text that carries no credential at all\n' "$i"
  done
  printf '++ a fragment from a patch dump\n'
  printf '%s\n' "$KEY_MULTI_A"
  for i in $(seq 5 150); do
    printf 'line %d: ordinary source text that carries no credential at all\n' "$i"
  done
  printf '%s\n' "$KEY_MULTI_B"
  for i in $(seq 151 200); do
    printf 'line %d: ordinary source text that carries no credential at all\n' "$i"
  done
} >"$repo/multi/a.txt"
{
  printf 'a harmless first line\n'
  printf '++ another fragment from a patch dump\n'
  printf 'a harmless third line\n'
  printf '%s\n' "$KEY_MULTI_C"
} >"$repo/multi/b.txt"
git -C "$repo" add multi/a.txt multi/b.txt
git -C "$repo" commit -q -m 'two keys in two hunks of one file and one in another'
#     The fixture guard: two hunks in a.txt, or this case measures one hunk twice.
git -C "$repo" diff HEAD^ HEAD -- multi/a.txt >"$tmp/multi-diff"
if [ "$(grep -c '^@@' "$tmp/multi-diff")" -eq 2 ]; then
  PASS=$(( PASS + 1 ))
else
  FAIL=$(( FAIL + 1 ))
  echo "FAIL: multi/a.txt no longer produces two hunks ($(grep -c '^@@' "$tmp/multi-diff")), so this case measures nothing"
fi
run_scan_args --head-only
check "--head-only refuses a multi-file multi-hunk commit carrying three keys" 1 "$?"
for want in 'multi/a.txt:6' 'multi/a.txt:153' 'multi/b.txt:4'; do
  if grep -qF -- "$want" "$tmp/out"; then
    PASS=$(( PASS + 1 ))
  else
    FAIL=$(( FAIL + 1 ))
    echo "FAIL: the multi-file finding was not reported at ${want}"
    sed 's/^/    /' "$tmp/out"
  fi
done

# 40. A KEY IN A FILE MARKED `-diff`. One line in .gitattributes —
#     `nodiff.txt -diff` — makes git call that file binary. `git diff` then
#     prints "Binary files … differ" and no '+' line at all, so the scan read
#     zero added lines from it, printed clean and exited 0 with the key at HEAD.
#     The attribute is committed by whoever commits the key, so the file that
#     turns the scan off travels with the leak. All three scopes are asserted
#     because all three call `git diff` and each one had to be fixed on its own.
KEY_NODIFF="ANTHROPIC_API""_KEY=skaaaanodiffaaaabbbbccccdddd012345678901"
KEY_NODIFF_STAGED="GITHUB""_TOKEN=ghp_nodiffstaged0123456789abcdefghij"
nodiff_base=$(git -C "$repo" rev-parse HEAD)
printf 'nodiff.txt -diff\nnodiff-staged.txt -diff\n' >"$repo/.gitattributes"
printf '%s\n' "$KEY_NODIFF" >"$repo/nodiff.txt"
git -C "$repo" add .gitattributes nodiff.txt
git -C "$repo" commit -q -m 'a key in a file marked -diff'
#     The fixture guard, and it is the load-bearing half. Without it this case
#     goes green on a git that stopped honouring the attribute, while measuring
#     nothing.
git -C "$repo" diff HEAD^ HEAD -- nodiff.txt >"$tmp/nodiff-diff"
if grep -q '^Binary files' "$tmp/nodiff-diff"; then
  PASS=$(( PASS + 1 ))
else
  FAIL=$(( FAIL + 1 ))
  echo "FAIL: the -diff attribute no longer renders nodiff.txt as binary, so this case measures nothing"
fi
run_scan_args --head-only
check "--head-only refuses a key in a file marked -diff" 1 "$?"
if grep -q "matches pattern" "$tmp/out"; then
  PASS=$(( PASS + 1 ))
else
  FAIL=$(( FAIL + 1 ))
  echo "FAIL: the -diff finding was not reported as a pattern match"
  sed 's/^/    /' "$tmp/out"
fi
if grep -qF -- 'nodiff.txt:1' "$tmp/out"; then
  PASS=$(( PASS + 1 ))
else
  FAIL=$(( FAIL + 1 ))
  echo "FAIL: the -diff finding was not located at nodiff.txt:1"
  sed 's/^/    /' "$tmp/out"
fi
SECRET_SCAN_BASELINE="$nodiff_base" run_scan_args --range
check "--range refuses a key in a file marked -diff" 1 "$?"
#     And the index scope. The empty-index run first is what makes the second
#     one mean something: exit 0 then exit 1 is a change this fixture caused,
#     rather than a repository that was already dirty.
git -C "$repo" reset -q
run_scan
check "the index is clean before the -diff file is staged" 0 "$?"
printf '%s\n' "$KEY_NODIFF_STAGED" >"$repo/nodiff-staged.txt"
git -C "$repo" add nodiff-staged.txt
run_scan
check "the index scope refuses a key in a file marked -diff" 1 "$?"
git -C "$repo" reset -q
rm -f "$repo/nodiff-staged.txt"


# 41. A `diff.external` COMMAND IN .git/config. git hands the whole content diff
#     to that program and prints whatever it prints. A program that prints
#     nothing turns every content diff into zero bytes, so the scan read no
#     added lines and exited 0 with the key at HEAD. Unlike the `-diff`
#     attribute above, this one does NOT travel in a commit — it is local config
#     — so it is the accident case rather than the attack case: a developer with
#     a difftool wired in globally runs the gate and it silently measures
#     nothing. Case 42 covers the same switch reached through the environment.
KEY_EXTDIFF="ANTHROPIC_API""_KEY=skbbbbextdiffaaaabbbbccccdddd01234567890"
printf '#!/bin/sh\nexit 0\n' >"$tmp/silent-diff.sh"
chmod +x "$tmp/silent-diff.sh"
printf '%s\n' "$KEY_EXTDIFF" >"$repo/extdiff.txt"
git -C "$repo" add extdiff.txt
git -C "$repo" commit -q -m 'a key in a file about to be hidden by diff.external'
git -C "$repo" config diff.external "$tmp/silent-diff.sh"
#     The fixture guard, in two halves, because either half alone can go green
#     while measuring nothing. First: with the config live and --no-ext-diff
#     withheld, the key must be GONE from the diff. Second: --no-ext-diff must
#     bring it back. Together they prove the config is being honoured AND that
#     the flag is the thing that defeats it.
git -C "$repo" diff --text HEAD^ HEAD -- extdiff.txt >"$tmp/ext-off"
git -C "$repo" diff --text --no-ext-diff HEAD^ HEAD -- extdiff.txt >"$tmp/ext-on"
if grep -qF -- "$KEY_EXTDIFF" "$tmp/ext-off"; then
  FAIL=$(( FAIL + 1 ))
  echo "FAIL: diff.external no longer suppresses the diff, so this case measures nothing"
else
  PASS=$(( PASS + 1 ))
fi
if grep -qF -- "$KEY_EXTDIFF" "$tmp/ext-on"; then
  PASS=$(( PASS + 1 ))
else
  FAIL=$(( FAIL + 1 ))
  echo "FAIL: --no-ext-diff did not restore the content diff, so this case measures nothing"
fi
run_scan_args --head-only
check "--head-only refuses a key hidden by a diff.external command" 1 "$?"
if grep -qF -- 'extdiff.txt:1' "$tmp/out"; then
  PASS=$(( PASS + 1 ))
else
  FAIL=$(( FAIL + 1 ))
  echo "FAIL: the diff.external finding was not located at extdiff.txt:1"
  sed 's/^/    /' "$tmp/out"
fi
extdiff_base=$(git -C "$repo" rev-parse HEAD^)
SECRET_SCAN_BASELINE="$extdiff_base" run_scan_args --range
check "--range refuses a key hidden by a diff.external command" 1 "$?"
git -C "$repo" config --unset diff.external

# 42. THE SAME SWITCH, REACHED THROUGH THE ENVIRONMENT. GIT_EXTERNAL_DIFF needs
#     no repository config at all, so a scan that only defended against the
#     config key would still read zero bytes for any agent or CI job that
#     exported it. --no-ext-diff covers both; this case is what proves the
#     coverage rather than assuming it from the one above.
run_scan_args_env() {
  ( cd "$repo" && env "$1" bash "$SCAN" "${@:2}" >"$tmp/out" 2>&1 )
}
git -C "$repo" diff --text HEAD^ HEAD -- extdiff.txt >"$tmp/ext-env-off"
if grep -qF -- "$KEY_EXTDIFF" "$tmp/ext-env-off"; then
  PASS=$(( PASS + 1 ))
else
  FAIL=$(( FAIL + 1 ))
  echo "FAIL: diff.external was not unset, so the environment case measures nothing"
fi
GIT_EXTERNAL_DIFF="$tmp/silent-diff.sh" git -C "$repo" diff --text HEAD^ HEAD -- extdiff.txt >"$tmp/ext-env-on"
if grep -qF -- "$KEY_EXTDIFF" "$tmp/ext-env-on"; then
  FAIL=$(( FAIL + 1 ))
  echo "FAIL: GIT_EXTERNAL_DIFF no longer suppresses the diff, so this case measures nothing"
else
  PASS=$(( PASS + 1 ))
fi
run_scan_args_env "GIT_EXTERNAL_DIFF=$tmp/silent-diff.sh" --head-only
check "--head-only refuses a key hidden by GIT_EXTERNAL_DIFF" 1 "$?"

# 43. A textconv FILTER BEHIND A `diff=<driver>` ATTRIBUTE. This one is the
#     nastier of the two halves of the pair, because the attribute travels in
#     the commit exactly like `-diff` does. The filter rewrites the file's
#     content before git diffs it, so a filter that redacts anything
#     key-shaped — which is a REASONABLE thing for someone to install, and that
#     is the point — makes the diff clean while the blob at HEAD still holds
#     the key. --no-textconv reads the blob instead of the filter's output.
KEY_TEXTCONV="ANTHROPIC_API""_KEY=skccccctextconvaaaabbbbccccdddd0123456"
printf '#!/bin/sh\nsed "s/_KEY=.*/_KEY=[redacted]/" "$1"\n' >"$tmp/redact.sh"
chmod +x "$tmp/redact.sh"
git -C "$repo" config diff.redact.textconv "$tmp/redact.sh"
printf 'textconv.txt diff=redact\n' >>"$repo/.gitattributes"
printf '%s\n' "$KEY_TEXTCONV" >"$repo/textconv.txt"
git -C "$repo" add .gitattributes textconv.txt
git -C "$repo" commit -q -m 'a key in a file behind a textconv filter'
#     The fixture guard, both halves again: the filter must really redact, and
#     --no-textconv must really defeat it.
git -C "$repo" diff --text --no-ext-diff HEAD^ HEAD -- textconv.txt >"$tmp/tc-off"
git -C "$repo" diff --text --no-ext-diff --no-textconv HEAD^ HEAD -- textconv.txt >"$tmp/tc-on"
if grep -qF -- "$KEY_TEXTCONV" "$tmp/tc-off"; then
  FAIL=$(( FAIL + 1 ))
  echo "FAIL: the textconv filter no longer redacts, so this case measures nothing"
else
  PASS=$(( PASS + 1 ))
fi
if grep -qF -- "$KEY_TEXTCONV" "$tmp/tc-on"; then
  PASS=$(( PASS + 1 ))
else
  FAIL=$(( FAIL + 1 ))
  echo "FAIL: --no-textconv did not read past the filter, so this case measures nothing"
fi
run_scan_args --head-only
check "--head-only refuses a key hidden behind a textconv filter" 1 "$?"
if grep -qF -- 'textconv.txt:1' "$tmp/out"; then
  PASS=$(( PASS + 1 ))
else
  FAIL=$(( FAIL + 1 ))
  echo "FAIL: the textconv finding was not located at textconv.txt:1"
  sed 's/^/    /' "$tmp/out"
fi
textconv_base=$(git -C "$repo" rev-parse HEAD^)
SECRET_SCAN_BASELINE="$textconv_base" run_scan_args --range
check "--range refuses a key hidden behind a textconv filter" 1 "$?"
git -C "$repo" config --unset diff.redact.textconv

# 44. A KEY BURIED BEHIND A NUL BYTE. --text makes git print a binary blob's
#     raw bytes instead of "Binary files … differ", which is what case 40 needs.
#     It also hands those raw bytes to awk, and THE AWKS DISAGREE about what a
#     NUL does to a record. Measured on 2026-08-12 on the record
#     `+HEADER<NUL><key>`: one-true-awk 20200816, which is /usr/bin/awk on
#     macOS, reports length 7 and drops everything after the NUL — so the key is
#     invisible and the scan exits 0. GNU Awk 5.4.1 and mawk 1.3.4 both report
#     18 and keep it. So on macOS this is a live hole and on a GNU box it never
#     was, which is the worst shape a gate can have: it passes locally and the
#     two machines disagree about what the gate means.
#
#     The fix is one `tr '\0' ' '` ahead of the parser, and it is a SPACE rather
#     than a deletion on purpose: deleting the NUL joins the bytes on either
#     side, which can manufacture a match out of two harmless fragments, and a
#     newline would desynchronise the hunk line counter and misreport the line.
#
#     THE CASE IS WEAKER ON A GNU HOST, BUT IT IS NOT VACUOUS THERE, and an
#     earlier version of this comment said it was — "already green under gawk
#     and mawk". Measured by reverting the transform and running this whole
#     suite under each awk: four assertions redden under one-true-awk and ONE
#     reddens under gawk and mawk. The one is the location assertion below.
#     Without the transform a NUL survives into the file the redaction loop
#     greps, grep answers "Binary file … matches" with no line number, and the
#     refusal degrades to "(location unknown)" — so on a GNU host the scan still
#     refuses but can no longer say where. That is what keeps this case honest
#     on ubuntu-latest, and it is why the exit-code assertions are not the
#     load-bearing half there.
KEY_NUL="ANTHROPIC_API""_KEY=skddddnulburiedaaaabbbbccccdddd0123456"
printf 'HEADER\0%s\n' "$KEY_NUL" >"$repo/nul.txt"
git -C "$repo" add nul.txt
git -C "$repo" commit -q -m 'a key buried behind a NUL byte'
#     Fixture guard: the diff the scan actually reads must really carry a NUL.
#     If a future git stops emitting the raw bytes, this case would go green
#     while measuring nothing at all.
git -C "$repo" diff --text --no-ext-diff --no-textconv HEAD^ HEAD -- nul.txt >"$tmp/nul-diff"
nul_bytes=$(LC_ALL=C tr -dc '\0' <"$tmp/nul-diff" | wc -c | tr -d ' ')
if [ "$nul_bytes" -gt 0 ]; then
  PASS=$(( PASS + 1 ))
else
  FAIL=$(( FAIL + 1 ))
  echo "FAIL: the --text diff of nul.txt carries no NUL byte, so this case measures nothing"
fi
#     Not an assertion — a note, because the answer is a property of the host's
#     awk and not of this repository. It tells whoever reads a failure here
#     which of the two worlds they are in.
awk_nul_len=$(printf 'HEADER\0TAIL\n' | awk '{print length($0)}' | head -1)
if [ "$awk_nul_len" -lt 11 ]; then
  echo "secret-scan-test: note — this host's awk truncates records at NUL (length ${awk_nul_len} of 11); case 44 is a live regression test here"
else
  echo "secret-scan-test: note — this host's awk keeps bytes past NUL (length ${awk_nul_len} of 11); case 44 asserts behaviour that was already correct here"
fi
run_scan_args --head-only
check "--head-only refuses a key buried behind a NUL byte" 1 "$?"
if grep -qF -- 'nul.txt:1' "$tmp/out"; then
  PASS=$(( PASS + 1 ))
else
  FAIL=$(( FAIL + 1 ))
  echo "FAIL: the NUL-buried finding was not located at nul.txt:1"
  sed 's/^/    /' "$tmp/out"
fi
nul_base=$(git -C "$repo" rev-parse HEAD^)
SECRET_SCAN_BASELINE="$nul_base" run_scan_args --range
check "--range refuses a key buried behind a NUL byte" 1 "$?"
#     And the index scope, which reads the same bytes through a different git
#     invocation and so had to be fixed on its own.
KEY_NUL_STAGED="GITHUB""_TOKEN=ghp_nulstaged0123456789abcdefghijkl"
git -C "$repo" reset -q
rm -f "$repo/nul.txt"
run_scan
check "the index is clean before the NUL-bearing file is staged" 0 "$?"
printf 'HEADER\0%s\n' "$KEY_NUL_STAGED" >"$repo/nul-staged.txt"
git -C "$repo" add nul-staged.txt
run_scan
check "the index scope refuses a key buried behind a NUL byte" 1 "$?"
git -C "$repo" reset -q
rm -f "$repo/nul-staged.txt"
# 45. A COMMIT THAT ADDS AN ORDINARY BINARY FILE. This is the LOCALE case, and
#     it is a regression test for a defect this change introduced rather than
#     for one it inherited. --text above puts a binary blob's raw bytes into the
#     file the transform reads, and BSD tr — /usr/bin/tr on macOS — decodes its
#     input as characters of the current locale. Under LANG=en_US.UTF-8, the
#     default here, it refuses any byte sequence that is not valid UTF-8: rc 1
#     and "tr: Illegal byte sequence". Measured 2026-08-12: 4096 bytes of
#     /dev/urandom give rc 1 in the default locale and rc 0 under LC_ALL=C, and
#     end to end a scratch commit adding 8 KB of random bytes made --head-only
#     print "BLOCKED — the scan could not run — the diff could not be read" and
#     exit 1.
#
#     It fails CLOSED, so this case does not guard a leak. It guards the shape
#     this file keeps naming: GNU tr does not refuse those bytes, so without
#     LC_ALL=C the first commit adding a binary file turns `make fast` and
#     `make full` red on macOS while the Linux runner stays green, and the two
#     machines disagree about what the gate means.
#
#     The bytes are FIXED, not random, so the case reproduces. 0xFF cannot
#     appear anywhere in valid UTF-8, which is what makes the guard below exact
#     and free of an iconv dependency.
binary_base=$(git -C "$repo" rev-parse HEAD)
: >"$repo/blob.bin"
for i in $(seq 1 200); do
  printf '\377\376\200\201\202\203\204\205\211\222' >>"$repo/blob.bin"
done
git -C "$repo" add blob.bin
git -C "$repo" commit -q -m 'add an ordinary binary file'
#     The fixture guard. Without it this case goes green on a git that stopped
#     printing the raw bytes, while measuring nothing.
git -C "$repo" diff --text --no-color --no-ext-diff --no-textconv HEAD^ HEAD -- blob.bin >"$tmp/bin-diff"
bin_ff=$(LC_ALL=C tr -dc '\377' <"$tmp/bin-diff" | wc -c | tr -d ' ')
if [ "$bin_ff" -gt 0 ]; then
  PASS=$(( PASS + 1 ))
else
  FAIL=$(( FAIL + 1 ))
  echo "FAIL: the --text diff of blob.bin carries no 0xFF byte, so the locale case measures nothing"
fi
run_scan_args --head-only
check "--head-only reads a commit that adds a binary file" 0 "$?"
SECRET_SCAN_BASELINE="$binary_base" run_scan_args --range
check "--range reads across a commit that adds a binary file" 0 "$?"
#     The index scope reads the same bytes through a different git invocation.
git -C "$repo" reset -q
printf '\377\376\200\201\202\203\204\205\211\222' >"$repo/blob-staged.bin"
git -C "$repo" add blob-staged.bin
run_scan
check "the index scope reads a staged binary file" 0 "$?"
git -C "$repo" reset -q
rm -f "$repo/blob-staged.bin"
#     And the scan must still FIND a key in a commit that also carries those
#     bytes. Without this, a transform that silently dropped the whole diff
#     would satisfy the three cases above and be useless.
KEY_BINARY="ANTHROPIC_API""_KEY=skeeeebinaryaaaabbbbccccddddeeee01234567"
printf '\377\376\200\201\202\203\204\205\211\222' >"$repo/blob2.bin"
printf '%s\n' "$KEY_BINARY" >"$repo/alongside.txt"
git -C "$repo" add blob2.bin alongside.txt
git -C "$repo" commit -q -m 'a key in a commit that also adds binary bytes'
run_scan_args --head-only
check "--head-only still finds a key in a commit that also adds binary bytes" 1 "$?"
if grep -q "matches pattern" "$tmp/out"; then
  PASS=$(( PASS + 1 ))
else
  FAIL=$(( FAIL + 1 ))
  echo "FAIL: the binary-alongside finding was not reported as a pattern match"
  sed 's/^/    /' "$tmp/out"
fi

# 46. GIT COLOUR FORCED ON. `color.ui = always` or `color.diff = always` in
#     .git/config makes git decorate the diff with ANSI escapes even when the
#     output is a pipe. The damage is not a pattern missing a decorated key: the
#     PARSER never opens a hunk at all, because a hunk header arrives as
#     "<ESC>[36m@@ -0,0 +1 @@" and the test for one reads the first three bytes.
#     Measured 2026-08-12 with git 2.50.1, one committed key and color.ui set to
#     always: all three scopes printed "clean — 0 added line(s) read" and exited
#     0 with the key at HEAD. --no-color on every content diff closes it.
#
#     Same family as cases 40 to 43 — whoever controls the repository or the
#     environment controls what `git diff` prints — and like diff.external it is
#     local config rather than something a commit carries, so it is the accident
#     case: a developer or a job that forces colour turns the gate into a no-op
#     that still prints a reassuring line.
KEY_COLOR="ANTHROPIC_API""_KEY=skffffcoloraaaabbbbccccddddeeeeff01234567"
KEY_COLOR_STAGED="OPENAI_API""_KEY=sk-colorstaged99998888777766665555"
color_base=$(git -C "$repo" rev-parse HEAD)
printf '%s\n' "$KEY_COLOR" >"$repo/color.txt"
git -C "$repo" add color.txt
git -C "$repo" commit -q -m 'a key in a commit read with colour forced on'
git -C "$repo" config color.ui always
#     The fixture guard, in two halves like case 41, because either half alone
#     can go green while measuring nothing. First: with colour forced and
#     --no-color withheld, the diff must really carry escapes. Second:
#     --no-color must really take them away.
git -C "$repo" diff --text --no-ext-diff --no-textconv HEAD^ HEAD -- color.txt >"$tmp/color-off"
git -C "$repo" diff --text --no-color --no-ext-diff --no-textconv HEAD^ HEAD -- color.txt >"$tmp/color-on"
if [ "$(LC_ALL=C tr -dc '\033' <"$tmp/color-off" | wc -c | tr -d ' ')" -gt 0 ]; then
  PASS=$(( PASS + 1 ))
else
  FAIL=$(( FAIL + 1 ))
  echo "FAIL: color.ui=always no longer colours the diff, so the colour case measures nothing"
fi
if [ "$(LC_ALL=C tr -dc '\033' <"$tmp/color-on" | wc -c | tr -d ' ')" -eq 0 ]; then
  PASS=$(( PASS + 1 ))
else
  FAIL=$(( FAIL + 1 ))
  echo "FAIL: --no-color did not remove the escapes, so the colour case measures nothing"
fi
run_scan_args --head-only
check "--head-only refuses a key with colour forced on" 1 "$?"
if grep -q "matches pattern" "$tmp/out"; then
  PASS=$(( PASS + 1 ))
else
  FAIL=$(( FAIL + 1 ))
  echo "FAIL: the colour finding was not reported as a pattern match"
  sed 's/^/    /' "$tmp/out"
fi
#     The location has to survive the escapes too, or the refusal is not
#     actionable.
if grep -qF -- 'color.txt:1' "$tmp/out"; then
  PASS=$(( PASS + 1 ))
else
  FAIL=$(( FAIL + 1 ))
  echo "FAIL: the colour finding was not located at color.txt:1"
  sed 's/^/    /' "$tmp/out"
fi
SECRET_SCAN_BASELINE="$color_base" run_scan_args --range
check "--range refuses a key with colour forced on" 1 "$?"
#     And the index scope, which calls git diff separately and so had to be
#     fixed on its own. The empty-index run first is what makes the second one
#     mean something: exit 0 then exit 1 is a change this fixture caused.
git -C "$repo" reset -q
run_scan
check "the index is clean before the colour fixture is staged" 0 "$?"
printf '%s\n' "$KEY_COLOR_STAGED" >"$repo/color-staged.txt"
git -C "$repo" add color-staged.txt
run_scan
check "the index scope refuses a key with colour forced on" 1 "$?"
git -C "$repo" reset -q
rm -f "$repo/color-staged.txt"
git -C "$repo" config --unset color.ui

echo "secret-scan-test: $(( PASS + FAIL )) assertions, ${FAIL} failed"
# A floor, for the same reason scripts/gate-fails-closed-test.sh carries one: a
# run that stopped early reports "0 failed" and exits 0, which reads exactly
# like a clean pass.
if [ $(( PASS + FAIL )) -lt 102 ]; then
  echo "secret-scan-test: only $(( PASS + FAIL )) assertions ran; this file expects 102" >&2
  exit 1
fi
[ "$FAIL" -eq 0 ] || exit 1
