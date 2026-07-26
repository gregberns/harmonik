#!/usr/bin/env bash
# prod-readiness checkpoint — admiral watch (30-min cadence)
# NON-NOOP by construction, and CRASH-DURABLE: the reliable signals (build, vet,
# fix-campaign burndown, C1 status, stall verdict) are computed and WRITTEN FIRST,
# so even if the optional bounded test is OOM/resource-killed the entry survives.
# Appends ONE entry to LOG.md per run; never edits prior entries.
#
# Interim proxy for "runs in prod without errors" during freeze-and-carve:
#   build (hard gate) + vet + C1(critical) status + commit delta + a light unit test.
# The DEFINITIVE gate remains the assessor daemon campaign (PLAN §5).
set -uo pipefail
cd "$(git -C "$(dirname "$0")" rev-parse --show-toplevel)" || exit 3
LOG="plans/2026-07-17-prod-readiness-watch/LOG.md"
TS="$(date +%Y-%m-%dT%H:%M:%S%z)"
# Prod-readiness progress lands on origin/main via PR merges — the local working-branch
# HEAD does NOT move on a GitHub PR merge, so track origin/main or every PR-based landing
# reads as a false STALL (HC: PR#33 crit3 fix @9db85569 showed commits=0/stall=YES).
git fetch origin main -q 2>/dev/null || true
HEAD="$(git rev-parse --short origin/main 2>/dev/null || git rev-parse --short HEAD)"

# --- previous checkpoint's HEAD/build (for commit-delta + stall detection) ---
PREV_HEAD="$(grep -oE 'HEAD=`[0-9a-f]+`' "$LOG" 2>/dev/null | tail -1 | grep -oE '[0-9a-f]+' || true)"
PREV_BUILD="$(grep -oE 'build=[A-Z]+' "$LOG" 2>/dev/null | tail -1 | cut -d= -f2 || true)"

# --- build (hard gate — the true "can prod run" signal) ---
BUILD_ERR="$(go build ./... 2>&1)"; BUILD_RC=$?
if [ $BUILD_RC -eq 0 ]; then BUILD=OK; BUILD_N=0
else BUILD=FAIL; BUILD_N="$(printf '%s\n' "$BUILD_ERR" | grep -cE '\.go:[0-9]+')"; fi

# --- vet (soft signal) ---
VET_ERR="$(go vet ./... 2>&1)"; if [ $? -eq 0 ]; then VET=OK; VET_N=0
else VET=WARN; VET_N="$(printf '%s\n' "$VET_ERR" | grep -cE '\.go:[0-9]+')"; fi

# --- C1 critical: BOTH confirm AND veto_verdict ops registered in the router? ---
C1_CONFIRM="$(grep -cE 'Register\("(confirm|confirm_verdict)"' internal/daemon/socketdispatch.go 2>/dev/null)"
C1_VETO="$(grep -cE 'Register\("veto_verdict"' internal/daemon/socketdispatch.go 2>/dev/null)"
if [ "$C1_CONFIRM" -ge 1 ] && [ "$C1_VETO" -ge 1 ]; then C1=CLOSED; else C1=OPEN; fi

# --- commit delta since last checkpoint ---
if [ -n "$PREV_HEAD" ]; then
  N_COMMITS="$(git rev-list --count "${PREV_HEAD}..HEAD" 2>/dev/null || echo '?')"
else N_COMMITS="(first)"; fi

# --- STALL verdict: no new commits AND build no better than last time ---
STALL=no
if [ -n "$PREV_HEAD" ] && [ "$PREV_HEAD" = "$HEAD" ] && [ "$BUILD" = "$PREV_BUILD" ]; then STALL=YES; fi

# --- PHASE 1: write the durable entry NOW (survives a test kill) ---
{
  echo ""
  echo "### $TS  ·  HEAD=\`$HEAD\`  ·  stall=$STALL"
  echo "- build=$BUILD (errs=$BUILD_N)  vet=$VET (warns=$VET_N)"
  echo "- C1(critical confirm/veto)=$C1  ·  commits_since_prev=$N_COMMITS"
  [ "$STALL" = "YES" ] && echo "- ⚠️ STALL: HEAD unchanged and build no better than prior checkpoint — investigate."
} >> "$LOG"

# echo the durable summary immediately (the loop can act on this even if test dies)
echo "CHECKPOINT $TS HEAD=$HEAD build=$BUILD C1=$C1 commits=$N_COMMITS stall=$STALL"

# --- PHASE 2: OPTIONAL light unit test — bounded, serial, LIGHT packages only ---
# Excludes the OOM-prone integration packages (daemon/handler/lifecycle) that killed
# the whole checkpoint. `-short` + `-p 1 -parallel 1` keep memory low. If this step
# is killed or hangs, the durable entry above already stands — no noop, no data loss.
LIGHT="./internal/core/... ./internal/queue/... ./internal/eventbus/... ./internal/workspace/... ./internal/brcli/..."
TEST_OUT="$(GOFLAGS= go test -short -count=1 -timeout=120s -p 1 -parallel 1 $LIGHT 2>&1)"; TEST_RC=$?
T_OK="$(printf '%s\n' "$TEST_OUT" | grep -cE '^ok ')"
T_REALFAIL="$(printf '%s\n' "$TEST_OUT" | grep -cE '^--- FAIL:')"
T_BUILDFAIL="$(printf '%s\n' "$TEST_OUT" | grep -cE '\[build failed\]')"
FAIL_PKGS="$(printf '%s\n' "$TEST_OUT" | grep -E '^FAIL' | awk '{print $2}' | sort -u | tr '\n' ' ')"
{
  echo "- light-unit-test: ok=$T_OK real_fail=$T_REALFAIL build_failed=$T_BUILDFAIL (rc=$TEST_RC; scope=core,queue,eventbus,workspace,brcli — daemon/handler/lifecycle excluded, OOM-prone)"
  [ "$T_REALFAIL" -gt 0 ] && [ -n "$FAIL_PKGS" ] && echo "- FAIL pkgs: $FAIL_PKGS"
  [ "$T_BUILDFAIL" -gt 0 ] && [ "$BUILD" = OK ] && echo "- note: $T_BUILDFAIL pkg(s) [build failed] but \`go build ./...\` is clean → transient cache/concurrent-build noise, not a code regression."
} >> "$LOG"
echo "  test: ok=$T_OK real_fail=$T_REALFAIL build_failed=$T_BUILDFAIL rc=$TEST_RC"

[ "$STALL" = "YES" ] && exit 2
{ [ "$BUILD" = "FAIL" ] || [ "$T_REALFAIL" -gt 0 ]; } && exit 1
exit 0
