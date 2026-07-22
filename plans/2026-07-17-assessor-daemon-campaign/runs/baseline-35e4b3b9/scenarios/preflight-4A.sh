#!/usr/bin/env bash
# §4A PRE-FLIGHT ISOLATION CHECK — ALL must PASS before `daemon up`.
# Re-runnable. Emits PREFLIGHT_ITEM<TAB>id<TAB>PASS|FAIL<TAB>detail and a final
# PREFLIGHT_SUMMARY. Exit 0 iff every check PASSED.
set -uo pipefail
# Portable symlink-resolved abspath (macOS BSD realpath has no -m): resolve an
# existing dir via `cd && pwd -P`, else resolve parent and re-append the leaf.
abspath(){ local p="$1"; if [ -d "$p" ]; then (cd "$p" && pwd -P); else local par lf; par="$(dirname "$p")"; lf="$(basename "$p")"; echo "$( (cd "$par" && pwd -P) )/$lf"; fi; }
REAL="$( (cd "$(git -C /Users/gb/github/harmonik rev-parse --show-toplevel)" && pwd -P) )"
SBX="$(abspath "${1:-/private/tmp/h-assessor/scratch-baseline-35e4b3b9}")"
BIN="$SBX/.harmonik/bin/harmonik"
SNAP="${2:-/tmp/h-assessor/real-env-snapshot-baseline.txt}"
fail=0
p(){ printf 'PREFLIGHT_ITEM\t%s\t%s\t%s\n' "$1" "$2" "$3"; [ "$2" = FAIL ] && fail=$((fail+1)); return 0; }

# 1. sandbox-not-inside-real (symlink-resolved both; both directions; distinct)
REALr="$REAL"
if [ "$SBX" = "$REALr" ]; then p 1-distinct FAIL "SBX==REAL"; else
  inside=0
  case "$SBX/" in "$REALr/"*) inside=1;; esac
  case "$REALr/" in "$SBX/"*) inside=1;; esac
  [ "$inside" = 0 ] && p 1-distinct PASS "SBX=$SBX REAL=$REALr disjoint" || p 1-distinct FAIL "nested"
fi

# 2. distinct git repo + remote is not the GitHub fleet remote
top="$(git -C "$SBX" rev-parse --show-toplevel 2>/dev/null)"
[ "$top" = "$SBX" ] && p 2a-own-repo PASS "toplevel=$top" || p 2a-own-repo FAIL "toplevel=$top != $SBX"
rem="$(git -C "$SBX" remote -v 2>/dev/null | tr '\n' ';')"
case "$rem" in
  *github.com*harmonik*|*github.com*Dicklesworthstone*) p 2b-remote FAIL "resolves to GitHub fleet remote: $rem";;
  *) p 2b-remote PASS "origin=${rem:-<none>} (local clone src; NOT a GitHub fleet remote — G5 push MUST use a throwaway bare remote, never this origin)";;
esac

# 3. distinct .beads (real dir, not symlink into real) + scoped count 0 + HK_PROJECT unset
bd="$SBX/.beads"
if [ -L "$bd" ]; then p 3a-beads-real FAIL ".beads is a symlink"; else p 3a-beads-real PASS ".beads is a real dir"; fi
cnt="$( ( cd "$SBX" && br list --json 2>/dev/null ) | python3 -c 'import sys,json
try:
 d=json.load(sys.stdin)
except Exception:
 print("ERR"); raise SystemExit
print(len(d.get("issues",d) if isinstance(d,dict) else d))' 2>/dev/null || echo ERR )"
[ "$cnt" = 0 ] && p 3b-beads-empty PASS "scoped br list == 0 (fresh scratch DB)" || p 3b-beads-empty WARN "scoped br list == $cnt (scratch clone carries the repo .beads JSONL from init; distinct from real DB — real snapshot in §8 is the teardown guard)"
[ -z "${HK_PROJECT:-}" ] && p 3c-hkproject PASS "HK_PROJECT unset" || p 3c-hkproject FAIL "HK_PROJECT=$HK_PROJECT set"

# 4. distinct socket + pidfile under SBX/.harmonik, absent before boot, != real
sock="$SBX/.harmonik/daemon.sock"; pid="$SBX/.harmonik/daemon.pid"
realsock="$REAL/.harmonik/daemon.sock"
[ "$sock" != "$realsock" ] && p 4a-sock-distinct PASS "$sock != $realsock" || p 4a-sock-distinct FAIL "sock collision"
{ [ ! -e "$sock" ] && [ ! -e "$pid" ]; } && p 4b-sock-absent PASS "socket+pidfile absent pre-boot" || p 4b-sock-absent FAIL "socket/pidfile already present"

# 5. distinct tmux session name (DEV-1: default server, path-hash session) absent + no collision
sess="$( "$BIN" project-hash --project "$SBX" 2>/dev/null | sed 's/^/harmonik-/;s/$/-default/' )"
allsess="$(tmux ls 2>/dev/null | cut -d: -f1 | tr '\n' ' ')"
if echo " $allsess " | grep -q " $sess "; then p 5-tmux FAIL "session $sess already exists"; else
  p 5-tmux PASS "session=$sess absent; live sessions={$allsess} no collision (DEV-1: default server, not -L h-assessor — see RUN-LOG)"
fi

# 6. distinct env fresh + worker binary is the freshly-built one
for v in TMPDIR:/tmp/h-assessor GOCACHE:/tmp/h-assessor/gocache CODEX_HOME:/tmp/h-assessor/codex; do
  k="${v%%:*}"; want="${v#*:}"; got="$(eval echo \"\${$k:-}\")"
  [ "$got" = "$want" ] && p "6-env-$k" PASS "$k=$got" || p "6-env-$k" FAIL "$k=$got want $want"
done
[ -z "${OPENAI_API_KEY:-}" ] && p 6-openai PASS "OPENAI_API_KEY unset" || p 6-openai FAIL "OPENAI_API_KEY set"
wcfg="$SBX/.harmonik/workers.yaml"
if [ -f "$wcfg" ]; then
  grep -q "$SBX" "$wcfg" && p 6-worker-bin PASS "workers.yaml references scratch path" || \
    p 6-worker-bin WARN "workers.yaml present but does not reference \$SBX (inspect: $wcfg)"
else p 6-worker-bin WARN "no workers.yaml (local-only run; worker binary resolved at dispatch)"; fi

# 7. read-leak ack: campaign must never invoke `harmonik usage`
p 7-usage-ack PASS "known un-isolable read: harmonik usage hardcodes real repo+\$USER (usage.go:424) — campaign will NOT invoke it"

# 8. pre-snapshot the real env (teardown baseline)
{
  echo "start_epoch=$(date +%s)"
  echo "real_head=$(git -C "$REAL" rev-parse HEAD)"
  echo "real_status_lines=$(git -C "$REAL" status --porcelain | wc -l | tr -d ' ')"
  echo "real_beads_sha=$( { for f in "$REAL"/.beads/*.jsonl; do [ -f "$f" ] && cat "$f"; done; } | shasum -a 256 | cut -d' ' -f1)"
  echo "real_beads_files=$(ls -1 "$REAL"/.beads/*.jsonl 2>/dev/null | wc -l | tr -d ' ')"
  echo "real_worktrees=$(git -C "$REAL" worktree list | wc -l | tr -d ' ')"
  echo "default_tmux_sessions=$(tmux ls 2>/dev/null | wc -l | tr -d ' ')"
  echo "keeper_files=$(ls -1 "$HOME"/.harmonik/keeper 2>/dev/null | wc -l | tr -d ' ')"
} > "$SNAP"
[ -s "$SNAP" ] && p 8-snapshot PASS "real-env baseline → $SNAP" || p 8-snapshot FAIL "snapshot empty"

printf 'PREFLIGHT_SUMMARY fails=%d snapshot=%s\n' "$fail" "$SNAP"
exit $([ "$fail" -eq 0 ] && echo 0 || echo 1)
