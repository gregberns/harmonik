#!/usr/bin/env bash
# scratch-daemon-smoke.sh — END-TO-END smoke for the scratch-daemon harness (hk-6eqv9).
#
# Proves the DURABLE capability of scripts/scratch-daemon.sh works as a whole loop,
# not just piece-by-piece: a scratch batch run produces a structured pass/fail
# summary, a FAIL turns into a deduped feedback bead on a "fleet" repo, and a re-run
# (or a DIFFERENT run of the same logical failure) updates that bead instead of
# minting a duplicate. The cross-run dedup is the load-bearing assertion.
#
# HERMETICITY (non-negotiable): this smoke NEVER writes to the live fleet beads DB
# and NEVER touches the live fleet daemon.
#   - The batch step runs OFFLINE via `scratch-daemon.sh batch --from-events <ndjson>`:
#     it folds a synthetic captured event stream into the REAL results artifact +
#     BATCH_SUMMARY using the REAL fold + fail_signature-normalization code, with no
#     daemon, no claude, no network. So the structured-summary + signature-stability
#     assertions run by default, deterministically.
#   - The feedback step targets a THROWAWAY git+beads repo via the
#     SCRATCH_FEEDBACK_FLEET_ROOT test seam (added in scratch-daemon.sh for exactly
#     this), so the create/dedup loop is proven without the live fleet DB.
#   - Every throwaway dir is under a mktemp root and rm'd on exit (trap cleanup).
#
# WHY OFFLINE for the batch step: a LIVE 1-bead batch requires the scratch daemon to
# spawn a real claude agent on a real bead (~minutes, needs a claude binary + network,
# non-deterministic). That is gated behind --full (see below). The default smoke
# exercises the SAME structured-summary + signature-derivation code paths offline, so
# it is fast, hermetic, and deterministic while still being a true end-to-end proof of
# the batch->feedback->dedup capability.
#
# DEFAULT phases (fast, hermetic, always run):
#   A  fail_signature STABILITY — fold TWO synthetic streams for the SAME logical
#      failure but with DIFFERENT volatile tokens (run-id, timestamp, tmpdir path) and
#      assert the derived fail_signature is byte-identical (the dedup precondition).
#   B  trivial 1-bead PASS batch — assert the JSON artifact + BATCH_SUMMARY line are
#      produced with verdict=pass and the command exits 0.
#   C  feedback CREATE — feed a real (offline-derived) FAIL artifact to `feedback`
#      against a throwaway fleet; assert exactly one bead is created.
#   D  feedback IDEMPOTENCY — re-run `feedback` on the SAME artifact AND on a SECOND
#      artifact from a DIFFERENT run of the same logical failure; assert BOTH update
#      the existing bead (created=0, updated=1) and the throwaway fleet ends with
#      exactly ONE feedback bead. This is the cross-run dedup proof.
#
# GATED phases (--full or SMOKE_SCENARIO=1 — heavy: clone + go build + go test):
#   E  REAL daemon lifecycle — init a throwaway clone, build, up, assert status RUNNING,
#      down. Proves the daemon stands up isolated (own socket/tmux/binary).
#   F  remote-substrate scenario — the REAL entry for the remote-substrate path is the
#      Go scenario test TestScenario_RemoteSubstrate_Localhost_E2E (NOT a queue submit;
#      `batch --file` is the real entry for live bead PROCESSING). Passing that e2e
#      needs BOTH a real claude binary (it spawns an agent) AND passwordless ssh
#      localhost, so Phase F by default only COMPILE-CHECKS the scenario path (builds
#      under -tags=scenario + asserts the test symbol exists) and runs the full e2e
#      only when SMOKE_SCENARIO_RUN=1 (reported as honest PASS/SKIP/FAIL).
#
# Usage:
#   ./scripts/scratch-daemon-smoke.sh            # default fast hermetic phases A-D
#   ./scripts/scratch-daemon-smoke.sh --full     # also run gated phases E-F
#   SMOKE_SCENARIO=1 ./scripts/scratch-daemon-smoke.sh   # same as --full
#   KEEP_DIR=1 ./scripts/scratch-daemon-smoke.sh # leave the temp dirs for inspection
#
# Exit: 0 if every assertion passed; 1 if any failed.
#
# Refs: hk-6eqv9 (end-to-end smoke), hk-1gkc8 (feedback), hk-6vr02 (batch), hk-4tdlw.

set -uo pipefail   # NOT -e: assertions handle their own failures and keep going.

SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
SD="$SELF_DIR/scratch-daemon.sh"
[ -x "$SD" ] || SD="bash $SELF_DIR/scratch-daemon.sh"

FULL=0
case "${1:-}" in --full) FULL=1;; "") :;; *) echo "unknown arg '$1' (use --full)" >&2; exit 2;; esac
[ "${SMOKE_SCENARIO:-0}" = "1" ] && FULL=1
KEEP_DIR="${KEEP_DIR:-0}"

command -v jq >/dev/null 2>&1 || { echo "FATAL: jq required" >&2; exit 2; }
command -v br >/dev/null 2>&1 || { echo "FATAL: br required" >&2; exit 2; }

# --- temp workspace + cleanup ----------------------------------------------
# Use a SHORT /tmp base (not $TMPDIR): the Phase E scratch daemon binds a unix socket
# at <root>/clone/.harmonik/daemon.sock, and macOS caps a unix socket path at 104 bytes
# (sun_path). The default macOS $TMPDIR (/var/folders/<long-hash>/T) plus the clone +
# .harmonik/daemon.sock suffix blows past 104, so the bind silently fails and the daemon
# never gets a socket. A short /tmp prefix keeps the full socket path well under the cap.
# (smoke-scratch.sh uses /tmp for the same reason.)
ROOT="$(mktemp -d "/tmp/hk-sm.XXXXXX")"
CLONE=""   # set in Phase E; torn down in cleanup so a flaky bring-up never leaks a daemon.
cleanup() {
    local ec=$?
    # If Phase E started a scratch daemon, tear it down (kills ONLY that scratch clone's
    # daemon + tmux session — scratch-daemon.sh down is fleet-safe by construction).
    if [ -n "$CLONE" ] && [ -d "$CLONE/.harmonik" ]; then
        $SD down "$CLONE" >/dev/null 2>&1 || true
    fi
    if [ "$KEEP_DIR" = "1" ]; then
        echo "[smoke] KEEP_DIR=1 — leaving $ROOT"
    else
        rm -rf "$ROOT" 2>/dev/null || true
    fi
    return $ec
}
trap cleanup EXIT INT TERM

PASS=0; FAIL=0
ok()   { echo "[smoke] PASS: $*"; PASS=$((PASS+1)); }
bad()  { echo "[smoke] FAIL: $*"; FAIL=$((FAIL+1)); }
# assert_eq <expected> <actual> <msg>
assert_eq() { if [ "$1" = "$2" ]; then ok "$3 (= $1)"; else bad "$3 (expected '$1', got '$2')"; fi; }

echo "[smoke] scratch-daemon end-to-end smoke (hk-6eqv9); full=$FULL; workspace=$ROOT"

# A scratch "project" dir for the offline batch artifacts. guard_path requires a real,
# non-fleet path with an existing parent; .harmonik must exist for the artifact write.
SCRATCH="$ROOT/scratch"; mkdir -p "$SCRATCH/.harmonik"

# ===========================================================================
# Synthetic event streams (match the real subscribe envelope:
#   {"type":..,"run_id":..,"payload":{run_id,bead_id,success,summary}} ).
# ===========================================================================
mk_pass_stream() { # <file> <bead> <runid>
    cat > "$1" <<EOF
{"type":"run_started","run_id":"$3","payload":{"run_id":"$3","bead_id":"$2"}}
{"type":"run_completed","run_id":"$3","payload":{"run_id":"$3","bead_id":"$2","success":true,"summary":"auto-close: exit=0"}}
EOF
}
# A FAIL summary that deliberately embeds volatile tokens (run-id, tmpdir path, ISO
# timestamp) — exactly the shape a real worktree_create_failed summary has. Normalized
# fail_signature MUST be identical across runs despite these differing.
mk_fail_stream() { # <file> <bead> <runid> <tmphash> <ts>
    cat > "$1" <<EOF
{"type":"run_started","run_id":"$3","payload":{"run_id":"$3","bead_id":"$2"}}
{"type":"run_failed","run_id":"$3","payload":{"run_id":"$3","bead_id":"$2","success":false,"summary":"worktree_create_failed: fatal: '/private/tmp/hk.$4/worktree-agent-$3' already exists ($5)"}}
EOF
}
# sig_of <tag> <summary-text> — fold a one-item FAIL run whose summary is exactly the
# given text through the REAL batch `--from-events` path and echo the derived
# fail_signature. Uses jq for safe JSON encoding of arbitrary summary text. This is how
# the BUG-3 stability assertions re-derive signatures from synthetic failed runs rather
# than hand-fixed strings.
sig_of() { # <tag> <summary>
    local f="$ROOT/sig.$1.ndjson" r="run-$1"
    jq -nc --arg r "$r" '{type:"run_started",run_id:$r,payload:{run_id:$r,bead_id:"hk-sig"}}'  >"$f"
    jq -nc --arg r "$r" --arg s "$2" '{type:"run_failed",run_id:$r,payload:{run_id:$r,bead_id:"hk-sig",success:false,summary:$s}}' >>"$f"
    $SD batch "$SCRATCH" "sig$1" --from-events "$f" >/dev/null 2>&1 || true
    jq -r '.[0].fail_signature' "$SCRATCH/.harmonik/batch-sig$1-events.json" 2>/dev/null
}

# ===========================================================================
# Phase A — fail_signature STABILITY across two runs of the same logical failure.
# ===========================================================================
echo "[smoke] --- Phase A: fail_signature stability ---"
mk_fail_stream "$ROOT/fail1.ndjson" hk-smoke-fail run-aaa111aaa "AbC123" "2026-06-25T14:03:11Z"
mk_fail_stream "$ROOT/fail2.ndjson" hk-smoke-fail run-bbb222bbb "ZZ99xx" "2026-06-26T09:59:01Z"

$SD batch "$SCRATCH" smokefail --from-events "$ROOT/fail1.ndjson" >"$ROOT/batch1.out" 2>&1; b1=$?
cp "$SCRATCH/.harmonik/batch-smokefail-events.json" "$ROOT/art1.json" 2>/dev/null
$SD batch "$SCRATCH" smokefail --from-events "$ROOT/fail2.ndjson" >"$ROOT/batch2.out" 2>&1 || true
cp "$SCRATCH/.harmonik/batch-smokefail-events.json" "$ROOT/art2.json" 2>/dev/null

assert_eq 1 "$b1" "fail batch exits non-zero"
sig1="$(jq -r '.[0].fail_signature' "$ROOT/art1.json" 2>/dev/null)"
sig2="$(jq -r '.[0].fail_signature' "$ROOT/art2.json" 2>/dev/null)"
if [ -n "$sig1" ] && [ "$sig1" = "$sig2" ]; then
    ok "fail_signature stable across runs: '$sig1'"
else
    bad "fail_signature UNSTABLE across runs (sig1='$sig1' sig2='$sig2') — dedup would break"
fi
# Confirm normalization actually fired (no raw run-id / timestamp / abs path leaked).
if printf '%s' "$sig1" | grep -qE 'run-aaa|2026-06-25|/private/tmp'; then
    bad "fail_signature still contains volatile tokens: '$sig1'"
else
    ok "fail_signature has no volatile tokens (run-id/timestamp/path redacted)"
fi

# --- BUG-3 guards: the signature must collapse volatile-only differences AND keep
# semantically-different failures DISTINCT. Both directions, by default (pure fold). ---
echo "[smoke] --- Phase A2: normalization positive (collapse) + negative (no false-merge) ---"

# POSITIVE 1 — Go panic stacks differing only in goroutine id + hex addresses collapse.
pa="$(sig_of pa 'panic: runtime error: invalid memory address goroutine 4823 [running]: main.f(0x14000a2f00)')"
pb="$(sig_of pb 'panic: runtime error: invalid memory address goroutine 51 [running]: main.f(0x1400ffee20)')"
if [ -n "$pa" ] && [ "$pa" = "$pb" ]; then ok "panic stacks collapse (goroutine/addr redacted): '$pa'"
else bad "panic stacks did NOT collapse — idempotency broken (pa='$pa' pb='$pb')"; fi

# POSITIVE 2 — merge-failed differing only in git SHAs collapse.
ma="$(sig_of ma 'merge-failed (dot): cannot rebase abc1234 onto def5678')"
mb="$(sig_of mb 'merge-failed (dot): cannot rebase 9f8e7d6 onto 1a2b3c4')"
if [ -n "$ma" ] && [ "$ma" = "$mb" ]; then ok "merge-failed collapses (git SHAs redacted): '$ma'"
else bad "merge-failed did NOT collapse — idempotency broken (ma='$ma' mb='$mb')"; fi

# POSITIVE 3 — same logical failure under different tmpdir roots collapses, tail kept.
ta="$(sig_of ta 'build error in /var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/scratch-x.AbC12Z/clone/internal/daemon/foo.go')"
tb="$(sig_of tb 'build error in /var/folders/aa/zz9zzz_bb222c_dd_3e4f5g0000gp/T/scratch-y.ZZ99yx/clone/internal/daemon/foo.go')"
if [ -n "$ta" ] && [ "$ta" = "$tb" ]; then ok "tmpdir-rooted path collapses, tail kept: '$ta'"
else bad "tmpdir path did NOT collapse (ta='$ta' tb='$tb')"; fi

# NEGATIVE — two DIFFERENT vet failures in DIFFERENT files must NOT merge (distinct
# signatures → distinct prov hashes → two beads; the false-merge bug guard).
na="$(sig_of na 'go vet failure: internal/daemon/foo.go missing return')"
nb="$(sig_of nb 'go vet failure: internal/queue/bar.go missing return')"
if [ -n "$na" ] && [ "$na" != "$nb" ]; then ok "distinct vet failures stay DISTINCT (no false-merge): '$na' vs '$nb'"
else bad "distinct failures FALSE-MERGED into one signature — second bug would be lost (na='$na' nb='$nb')"; fi

# ===========================================================================
# Phase B — trivial 1-bead PASS batch produces the structured summary.
# ===========================================================================
echo "[smoke] --- Phase B: trivial 1-bead pass batch summary ---"
mk_pass_stream "$ROOT/pass.ndjson" hk-smoke-pass run-pass123
$SD batch "$SCRATCH" smokepass --from-events "$ROOT/pass.ndjson" >"$ROOT/passbatch.out" 2>&1; bp=$?
assert_eq 0 "$bp" "pass batch exits 0"
PASS_ART="$SCRATCH/.harmonik/batch-smokepass-events.json"
if [ -f "$PASS_ART" ]; then ok "JSON artifact produced: batch-smokepass-events.json"; else bad "JSON artifact missing"; fi
assert_eq pass "$(jq -r '.[0].verdict' "$PASS_ART" 2>/dev/null)" "artifact verdict is pass"
if grep -qE '^BATCH_SUMMARY name=smokepass total=1 pass=1 fail=0 incomplete=0 ' "$ROOT/passbatch.out"; then
    ok "BATCH_SUMMARY line correct (total=1 pass=1)"
else
    bad "BATCH_SUMMARY line missing/wrong: $(grep BATCH_SUMMARY "$ROOT/passbatch.out" || echo '(none)')"
fi

# ===========================================================================
# Phase C — feedback CREATE against a THROWAWAY fleet (hermetic).
# ===========================================================================
echo "[smoke] --- Phase C: feedback creates a deduped fleet bead ---"
FLEET="$ROOT/fleet"; mkdir -p "$FLEET"
git -C "$FLEET" init -q
git -C "$FLEET" config user.email smoke@harmonik.local
git -C "$FLEET" config user.name "Smoke Runner"
( cd "$FLEET" && br init --prefix smk >/dev/null 2>&1 ) \
    || { bad "could not br-init throwaway fleet — aborting feedback phases"; }

if [ -d "$FLEET/.beads" ]; then
    ok "throwaway fleet .beads initialized"
    export SCRATCH_FEEDBACK_FLEET_ROOT="$FLEET"

    $SD feedback "$ROOT/art1.json" --batch smokefail >"$ROOT/fb1.out" 2>&1; f1=$?
    assert_eq 0 "$f1" "feedback (create) exits 0"
    if grep -qE '^FEEDBACK_SUMMARY batch=smokefail fail_items=1 created=1 updated=0 ' "$ROOT/fb1.out"; then
        ok "feedback created exactly 1 bead"
    else
        bad "feedback create summary wrong: $(grep FEEDBACK_SUMMARY "$ROOT/fb1.out" || echo '(none)')"
    fi
    NEWID="$(grep -E '^FEEDBACK_ITEM\tcreate' "$ROOT/fb1.out" | awk -F'\t' '{print $5}')"
    [ -n "$NEWID" ] && ok "fleet bead id captured: $NEWID" || bad "no created fleet bead id"

    # =======================================================================
    # Phase D — IDEMPOTENCY: same artifact re-run + DIFFERENT-run artifact both
    # update the existing bead; fleet ends with exactly one feedback bead.
    # =======================================================================
    echo "[smoke] --- Phase D: cross-run dedup (idempotency) ---"
    $SD feedback "$ROOT/art1.json" --batch smokefail >"$ROOT/fb2.out" 2>&1; f2=$?
    assert_eq 0 "$f2" "feedback (re-run same artifact) exits 0"
    if grep -qE '^FEEDBACK_SUMMARY batch=smokefail fail_items=1 created=0 updated=1 ' "$ROOT/fb2.out"; then
        ok "re-run same artifact: 0 created, 1 updated (idempotent)"
    else
        bad "re-run summary wrong: $(grep FEEDBACK_SUMMARY "$ROOT/fb2.out" || echo '(none)')"
    fi

    # art2 = a DIFFERENT run (different run-id/timestamp/tmpdir) of the SAME logical
    # failure. This is the real cross-run dedup proof: a fresh run must NOT mint a
    # duplicate bead, because its normalized fail_signature matches the first run's.
    $SD feedback "$ROOT/art2.json" --batch smokefail >"$ROOT/fb3.out" 2>&1; f3=$?
    assert_eq 0 "$f3" "feedback (different run, same logical failure) exits 0"
    if grep -qE '^FEEDBACK_SUMMARY batch=smokefail fail_items=1 created=0 updated=1 ' "$ROOT/fb3.out"; then
        ok "cross-run dedup: different run updates SAME bead, 0 created"
    else
        bad "cross-run summary wrong: $(grep FEEDBACK_SUMMARY "$ROOT/fb3.out" || echo '(none)')"
    fi
    DUPID="$(grep -E '^FEEDBACK_ITEM\tupdate' "$ROOT/fb3.out" | awk -F'\t' '{print $5}')"
    assert_eq "$NEWID" "$DUPID" "cross-run update targets the SAME fleet bead id"

    NBEADS="$( cd "$FLEET" && br list --label scratch-feedback --json 2>/dev/null | jq '(.issues // []) | length' )"
    assert_eq 1 "$NBEADS" "throwaway fleet holds exactly ONE feedback bead after 3 feedback runs"

    # NEGATIVE no-merge at the FEEDBACK/bead level: two DIFFERENT failures (real
    # fold-derived signatures na/nb from Phase A2) must produce TWO beads, not one.
    # Build a 2-item results artifact and feed it to a FRESH throwaway fleet.
    FLEET2="$ROOT/fleet2"; mkdir -p "$FLEET2"
    git -C "$FLEET2" init -q
    git -C "$FLEET2" config user.email smoke@harmonik.local
    git -C "$FLEET2" config user.name "Smoke Runner"
    if ( cd "$FLEET2" && br init --prefix sm2 >/dev/null 2>&1 ) && [ -d "$FLEET2/.beads" ]; then
        jq -n --arg s1 "$na" --arg s2 "$nb" '[
            {bead:"hk-neg1",run_id:"run-na",verdict:"fail",fail_signature:$s1},
            {bead:"hk-neg2",run_id:"run-nb",verdict:"fail",fail_signature:$s2}
        ]' > "$ROOT/neg-results.json"
        SCRATCH_FEEDBACK_FLEET_ROOT="$FLEET2" $SD feedback "$ROOT/neg-results.json" --batch negtest >"$ROOT/fbneg.out" 2>&1
        if grep -qE '^FEEDBACK_SUMMARY batch=negtest fail_items=2 created=2 updated=0 ' "$ROOT/fbneg.out"; then
            ok "two DISTINCT failures create TWO beads (no false-merge at dedup level)"
        else
            bad "distinct failures did not create 2 beads: $(grep FEEDBACK_SUMMARY "$ROOT/fbneg.out" || echo '(none)')"
        fi
        NB2="$( cd "$FLEET2" && br list --label scratch-feedback --json 2>/dev/null | jq '(.issues // []) | length' )"
        assert_eq 2 "$NB2" "fresh fleet holds exactly TWO beads for two distinct failures"
    else
        bad "could not br-init second throwaway fleet for the no-merge check"
    fi

    unset SCRATCH_FEEDBACK_FLEET_ROOT
fi

# ===========================================================================
# Phase E/F — GATED heavy phases (real daemon lifecycle + remote-substrate scenario).
# ===========================================================================
if [ "$FULL" = "1" ]; then
    echo "[smoke] --- Phase E: REAL daemon lifecycle (clone + build + up + down) ---"
    CLONE="$ROOT/clone"
    REPO_ROOT="$(git -C "$SELF_DIR" rev-parse --show-toplevel)"
    # Clone the LOCAL checkout (throwaway, offline, fast) rather than origin — still a
    # fully independent clone with its own socket/tmux/binary, but no network round-trip.
    if $SD init "$CLONE" "$REPO_ROOT" >"$ROOT/init.out" 2>&1 \
        && $SD build "$CLONE" >"$ROOT/build.out" 2>&1; then
        # Operator config step: `harmonik init` ships the G-liveness governor key
        # commented under the `sentinel:` block, but daemon.Start REQUIRES it set
        # (fail-loud, no compiled default — the no-hardcoded-thresholds rule). Set it
        # explicitly to 0 (disable G-liveness) so a throwaway daemon never self-kills.
        # Only append if there is no ACTIVE (uncommented) key already.
        if ! grep -qE '^[[:space:]]+liveness_no_progress_n:' "$CLONE/.harmonik/config.yaml"; then
            awk '{print} /^sentinel:/{print "  liveness_no_progress_n: 0  # set by scratch-daemon-smoke (Phase E)"}' \
                "$CLONE/.harmonik/config.yaml" > "$CLONE/.harmonik/config.yaml.tmp" \
                && mv "$CLONE/.harmonik/config.yaml.tmp" "$CLONE/.harmonik/config.yaml"
        fi
        # --no-auto-pull (via SCRATCH_DAEMON_FLAGS): a throwaway daemon must NOT auto-pull
        # from its origin — here the origin is the live checkout the fleet is actively
        # committing to, and that git fetch can block past the 45s socket wait. Mirrors
        # smoke-scratch.sh, which starts its scratch daemon with --no-auto-pull too.
        if SCRATCH_DAEMON_FLAGS="--no-auto-pull ${SCRATCH_DAEMON_FLAGS:-}" $SD up "$CLONE" >"$ROOT/up.out" 2>&1; then
            # `up` returns as soon as the socket FILE exists; the daemon writes its
            # pidfile a beat later, so poll status briefly for the RUNNING line.
            running=0
            for _ in 1 2 3 4 5 6 7 8 9 10; do
                if $SD status "$CLONE" >"$ROOT/status.out" 2>&1 && grep -q 'daemon  : RUNNING' "$ROOT/status.out"; then
                    running=1; break
                fi
                sleep 1
            done
            if [ "$running" = "1" ]; then
                ok "scratch daemon came up RUNNING (isolated socket/tmux/binary)"
            else
                bad "scratch daemon status not RUNNING (last status: $(grep 'daemon  :' "$ROOT/status.out" | tr -s ' '))"
            fi
            $SD down "$CLONE" >"$ROOT/down.out" 2>&1 \
                && ok "scratch daemon down clean" \
                || bad "scratch daemon down failed (see $ROOT/down.out)"
        else
            bad "daemon up failed (tail: $(tail -2 "$ROOT/up.out" | tr '\n' ' '))"
        fi
    else
        bad "daemon lifecycle init/build failed (see $ROOT/{init,build}.out)"
    fi

    echo "[smoke] --- Phase F: remote-substrate scenario path ---"
    # The remote-substrate path is exercised by a Go scenario test, NOT a queue
    # submission — `batch --file` is the real entry for live bead PROCESSING, but the
    # remote-substrate localhost e2e is TestScenario_RemoteSubstrate_Localhost_E2E.
    # Running it to PASS needs BOTH a real claude binary (it spawns an agent on the
    # remote worker) AND passwordless `ssh localhost`; in a CI/dev box without a real
    # claude it fails at agent-launch. So by default Phase F COMPILE-CHECKS the scenario
    # path (proves it builds under -tags=scenario and the test symbol exists) and runs
    # the full e2e only when SMOKE_SCENARIO_RUN=1 is set (honest PASS/SKIP/FAIL).
    if grep -q 'func TestScenario_RemoteSubstrate_Localhost_E2E' \
        "$REPO_ROOT/internal/daemon/scenario_remote_substrate_localhost_test.go" 2>/dev/null; then
        ok "remote-substrate scenario test symbol present"
    else
        bad "remote-substrate scenario test symbol missing"
    fi
    if go test -C "$REPO_ROOT" -tags=scenario -run '^$' ./internal/daemon/ >"$ROOT/scenario_compile.out" 2>&1; then
        ok "scenario-tagged daemon tests COMPILE (-tags=scenario, no test executed)"
    else
        bad "scenario-tagged tests do NOT compile (tail: $(tail -3 "$ROOT/scenario_compile.out" | tr '\n' ' '))"
    fi
    if [ "${SMOKE_SCENARIO_RUN:-0}" = "1" ]; then
        echo "[smoke] SMOKE_SCENARIO_RUN=1 — executing the full remote-substrate e2e (needs real claude + ssh localhost)"
        if go test -C "$REPO_ROOT" -tags=scenario -count=1 -v \
            -run TestScenario_RemoteSubstrate_Localhost_E2E ./internal/daemon/ >"$ROOT/scenario.out" 2>&1; then
            if grep -qE '^[[:space:]]*--- SKIP' "$ROOT/scenario.out"; then
                ok "remote-substrate e2e SKIPPED (no passwordless ssh localhost) — not a failure"
            else
                ok "remote-substrate localhost e2e scenario test PASSED"
            fi
        else
            bad "remote-substrate e2e FAILED (needs a real claude binary; tail: $(tail -3 "$ROOT/scenario.out" | tr '\n' ' '))"
        fi
    else
        echo "[smoke] NOTE: full remote-substrate e2e not executed (set SMOKE_SCENARIO_RUN=1 to run it; requires a real claude binary + passwordless ssh localhost)"
    fi
else
    echo "[smoke] (skipping gated Phases E-F: real daemon lifecycle + remote-substrate scenario — pass --full or SMOKE_SCENARIO=1 to run)"
fi

# ===========================================================================
echo "[smoke] ================================================================"
echo "[smoke] RESULT: $PASS passed, $FAIL failed"
if [ "$FAIL" -eq 0 ]; then
    echo "[smoke] SMOKE PASS"
    exit 0
else
    echo "[smoke] SMOKE FAIL"
    exit 1
fi
