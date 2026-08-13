#!/usr/bin/env bash
# core-loop-matrix-preflight-test.sh — self-test for the pi readiness preflight in
# scripts/core-loop-matrix.sh (hk-odimo).
#
# WHY THIS EXISTS. The preflight decides whether a pi cell runs at all. It got that
# decision wrong twice out of two runs: it fired one 12-second probe straight after a
# ~280-second cell, read the few seconds of contention that follow an agent run as a dead
# endpoint, and skipped the review round-trip cell before it started. The same probe
# answered in 1.2 seconds a moment later. The retry that fixes it is invisible when the
# endpoint is healthy, so nothing in a green matrix run would ever exercise it, and the
# next person to "simplify" the classification would not hear a sound. This test is the
# sound.
#
# ZERO TOKENS, NO DAEMON, NO NETWORK. It runs pi_probe_once and pi_preflight — the real
# functions, extracted from the real script — against a stdlib-python stub on loopback.
# Budgets are scaled down per case (PI_PROBE_TIMEOUT / PI_PROBE_TRIES / PI_PROBE_BACKOFF),
# so the whole run takes seconds. The SHIPPED defaults are asserted separately, by
# arithmetic, in the "budget" cases at the end.
#
# There is no make target on purpose. Run it by hand, like its sibling
# scripts/core-loop-assert-test.sh:
#
#     bash scripts/core-loop-matrix-preflight-test.sh
#
# Exit 0 iff every case matches.

set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MATRIX="$ROOT/scripts/core-loop-matrix.sh"
command -v jq      >/dev/null 2>&1 || { echo "jq required" >&2; exit 2; }
command -v curl    >/dev/null 2>&1 || { echo "curl required" >&2; exit 2; }
command -v python3 >/dev/null 2>&1 || { echo "python3 required" >&2; exit 2; }

TMPD="$(mktemp -d "${TMPDIR:-/tmp}/preflight-test.XXXXXX")"
SRV_PIDS=()
cleanup() { local p; for p in "${SRV_PIDS[@]:-}"; do [ -n "$p" ] && kill "$p" 2>/dev/null || true; done; rm -rf "$TMPD"; }
trap cleanup EXIT INT TERM

# ---- the functions under test ---------------------------------------------
# Extracted from core-loop-matrix.sh, NOT copied into this file. A copy is a second
# source of truth that silently disagrees, which is the exact failure the comments in
# that file are about. The script runs its whole body at top level and dies without a
# scratch path, so the block is cut out and sourced instead. If the boundary markers move,
# the two greps below fail loudly rather than testing nothing.
FNS="$TMPD/preflight.inc"
awk '/^PI_PROBE_TIMEOUT=/ { on=1 } /^# ---- model resolution for the cell specs/ { on=0 } on' \
    "$MATRIX" > "$FNS"
grep -q '^pi_probe_once()' "$FNS" || { echo "extract failed: pi_probe_once not found in $MATRIX" >&2; exit 2; }
grep -q '^pi_preflight()'  "$FNS" || { echo "extract failed: pi_preflight not found in $MATRIX"  >&2; exit 2; }

log() { echo "        [preflight] $*"; }
# The four below are the preflight's inputs. shellcheck cannot see the `source` under them,
# so it reads every one as unused.
# shellcheck disable=SC2034
SCRATCH="$TMPD/fake-scratch"
# shellcheck disable=SC2034
PI_KEY_FILE="$TMPD/no-such-key"
PI_MODEL="nemotron"
PI_BASE_URL=""
# shellcheck source=/dev/null
source "$FNS"

# The shipped values, captured before any case overrides them.
DEF_TIMEOUT="$PI_PROBE_TIMEOUT"; DEF_TRIES="$PI_PROBE_TRIES"; DEF_BACKOFF="$PI_PROBE_BACKOFF"

# ---- the stub endpoint ----------------------------------------------------
STUB="$TMPD/stub.py"
cat > "$STUB" <<'PY'
import json, sys, time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

MODE  = sys.argv[1]
ARG   = float(sys.argv[2])          # mode-dependent: request count or seconds
COUNT = sys.argv[3]                 # one byte appended per request; the test counts them
OK    = json.dumps({"choices": [{"text": " pong pong pong"}]})
CHAT  = json.dumps({"choices": [{"message": {"content": "pong"}}]})
START = time.time()

class H(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.0"
    def log_message(self, *a): pass
    def do_POST(self):
        n = int(self.headers.get('Content-Length', 0)); self.rfile.read(n)
        with open(COUNT, 'ab') as f: f.write(b'.')
        seen = len(open(COUNT, 'rb').read())
        busy = time.time() - START < ARG
        if MODE == 'ok':            return self.send(200, OK)
        if MODE == 'ok_chat':       return self.send(200, CHAT)
        if MODE == 'always503':     return self.send(503, '{"error":{"message":"overloaded"}}')
        if MODE == 'always500':     return self.send(500, '')
        if MODE == 'notfound':      return self.send(404, json.dumps({"error":{"message":"The model `nope` does not exist."}}))
        if MODE == 'unauth':        return self.send(401, json.dumps({"error":{"message":"bad api key"}}))
        if MODE == 'err200':        return self.send(200, json.dumps({"error":{"message":"context length exceeded"}}))
        if MODE == 'empty200':      return self.send(200, json.dumps({"choices":[{"text":""}]}))
        if MODE == 'emptybody':     return self.send(200, '')
        if MODE == 'garbage':       return self.send(200, 'not json at all\nsecond line')
        if MODE == 'stall':         return time.sleep(600)
        if MODE == 'stall_then_ok': return time.sleep(600) if busy else self.send(200, OK)
        if MODE == 'busy429_n':     return self.send(429, '{"error":{"message":"busy"}}') if seen <= ARG else self.send(200, OK)
        return self.send(500, '')
    def send(self, code, body):
        b = body.encode()
        self.send_response(code)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(b)))
        self.end_headers()
        self.wfile.write(b)

ThreadingHTTPServer.daemon_threads = True
srv = ThreadingHTTPServer(('127.0.0.1', 0), H)
print(srv.server_address[1], flush=True)
srv.serve_forever()
PY

srv_n=0
start_srv() {   # start_srv <mode> [arg] — sets SRV_URL and SRV_COUNT
    srv_n=$((srv_n+1))
    SRV_COUNT="$TMPD/count.$srv_n"; : > "$SRV_COUNT"
    local portfile="$TMPD/port.$srv_n" port=""
    python3 "$STUB" "$1" "${2:-0}" "$SRV_COUNT" > "$portfile" 2>/dev/null &
    SRV_PIDS+=("$!")
    for _ in $(seq 1 100); do
        port="$(cat "$portfile" 2>/dev/null || true)"
        [ -n "$port" ] && break
        sleep 0.05
    done
    [ -n "$port" ] || { echo "stub '$1' did not report a port" >&2; exit 2; }
    SRV_URL="http://127.0.0.1:$port/v1"
}
requests_seen() { wc -c < "$SRV_COUNT" | tr -d ' '; }

# ---- the harness ----------------------------------------------------------
pass=0; fail=0
ok() { pass=$((pass+1)); echo "ok   — $1"; }
no() { fail=$((fail+1)); echo "FAIL — $1" >&2; }
# want_eq <name> <want> <got>
want_eq() { if [ "$2" = "$3" ]; then ok "$1"; else no "$1: want '$2' got '$3'"; fi; }
# want_in <name> <needle> <haystack>
want_in() { case "$3" in *"$2"*) ok "$1";; *) no "$1: '$3' does not contain '$2'";; esac; }
# want_between <name> <lo> <hi> <got>
want_between() {
    if [ "$4" -ge "$2" ] && [ "$4" -le "$3" ]; then ok "$1"; else no "$1: want ${2}s..${3}s got ${4}s"; fi
}

RC=0; SECS=0; DETAIL=""
drive() {   # drive <base_url> — runs the REAL pi_preflight; sets RC, SECS, DETAIL
    # The call runs in a SUBSHELL. Under `set -u` a bad array index inside the preflight
    # kills the shell it runs in, and in this harness that would take the whole test down
    # silently — a broken preflight would print nothing at all instead of a FAIL line.
    # An abort is a verdict here, and it is reported as rc=99.
    # shellcheck disable=SC2034  # read by the sourced pi_probe_once
    PI_BASE_URL="$1"
    local t0=$SECONDS out="$TMPD/drive.out"
    : > "$out"
    (
        rc=0
        pi_preflight || rc=$?
        printf '%s\t%s\n' "$rc" "$PI_PREFLIGHT_DETAIL" > "$out"
    ) || true
    SECS=$((SECONDS-t0))
    if [ -s "$out" ]; then
        IFS=$'\t' read -r RC DETAIL < "$out"
    else
        RC=99; DETAIL="(the preflight aborted without reaching a verdict)"
    fi
}

# Fast budgets for the classification cases: one try, so a wrong "retry this" class shows
# up as the wrong request count rather than as a slow test.
one_try()  { PI_PROBE_TIMEOUT=3; PI_PROBE_TRIES=1; PI_PROBE_BACKOFF="1"; }
three_try(){ PI_PROBE_TIMEOUT=3; PI_PROBE_TRIES=3; PI_PROBE_BACKOFF="1 1"; }

echo "== classification: what each answer means =="
three_try
start_srv ok;        drive "$SRV_URL"; want_eq "200 + choices[].text is live"        "0" "$RC"; want_eq "  ...and it probes once"  "1" "$(requests_seen)"
start_srv ok_chat;   drive "$SRV_URL"; want_eq "200 + choices[].message.content is live" "0" "$RC"
start_srv always503; drive "$SRV_URL"; want_eq "503 fails after the retries"         "1" "$RC"; want_in "  503 reads as busy" "HTTP 503 (busy or not ready)" "$DETAIL"
start_srv always500; drive "$SRV_URL"; want_eq "500 fails after the retries"         "1" "$RC"; want_in "  the 5?? glob catches 500" "HTTP 500 (busy or not ready)" "$DETAIL"
start_srv notfound;  drive "$SRV_URL"; want_eq "404 unknown model fails"             "1" "$RC"; want_in "  404 reads as a refusal, with the reason" "HTTP 404 and refused the request: The model" "$DETAIL"
start_srv unauth;    drive "$SRV_URL"; want_eq "401 bad key fails"                   "1" "$RC"; want_in "  401 reads as a refusal" "HTTP 401 and refused the request: bad api key" "$DETAIL"
start_srv err200;    drive "$SRV_URL"; want_eq "200 carrying .error fails"           "1" "$RC"; want_in "  a 200 .error reads as a refusal" "answered but refused the request: context length exceeded" "$DETAIL"
start_srv empty200;  drive "$SRV_URL"; want_eq "200 with an empty completion fails"  "1" "$RC"; want_in "  an empty completion says so" "answered with an empty completion" "$DETAIL"
start_srv emptybody; drive "$SRV_URL"; want_eq "200 with a 0-byte body fails"        "1" "$RC"; want_in "  a 0-byte body reads as an empty completion" "answered with an empty completion" "$DETAIL"
start_srv garbage;   drive "$SRV_URL"; want_eq "200 with a non-JSON body fails"      "1" "$RC"; want_in "  non-JSON reads as an empty completion" "answered with an empty completion" "$DETAIL"
# A multi-line non-JSON body also proves the http_code split: the status is taken from the
# LAST line, so a body with newlines in it must not shift the status.
want_in "  a multi-line body does not shift the status" "empty completion" "$DETAIL"

echo
echo "== a refusal is never retried; a busy answer always is =="
one_try
start_srv notfound; PI_PROBE_TRIES=3; drive "$SRV_URL"
want_eq "404 stops after ONE probe" "1" "$(requests_seen)"
start_srv unauth;   PI_PROBE_TRIES=3; drive "$SRV_URL"
want_eq "401 stops after ONE probe" "1" "$(requests_seen)"
start_srv err200;   PI_PROBE_TRIES=3; drive "$SRV_URL"
want_eq "a 200 .error stops after ONE probe" "1" "$(requests_seen)"
three_try
start_srv always503; drive "$SRV_URL"
want_eq "503 uses every try" "3" "$(requests_seen)"
start_srv empty200;  drive "$SRV_URL"
want_eq "an empty completion uses every try" "3" "$(requests_seen)"

echo
echo "== the defect itself: a briefly-busy endpoint must recover =="
# 429 twice, then a real completion — how a loaded vLLM says "not now".
three_try
start_srv busy429_n 2; drive "$SRV_URL"
want_eq "429 twice then 200 passes"        "0" "$RC"
want_eq "  ...on the third probe"          "3" "$(requests_seen)"
want_in "  and it reports the answer"      "the pi endpoint answered" "$DETAIL"
# The reproduced shape: the endpoint accepts the connection and answers nothing while the
# previous cell's agent lets go, then answers normally. One shot calls that dead.
PI_PROBE_TIMEOUT=2; PI_PROBE_TRIES=3; PI_PROBE_BACKOFF="1 1"
start_srv stall_then_ok 3; drive "$SRV_URL"
want_eq "stalls for 3s, then passes"       "0" "$RC"
want_between "  ...within the scaled budget" 3 9 "$SECS"

echo
echo "== a dead endpoint still fails, in bounded time =="
# Nothing listening: curl refuses at once, so the cost is only the waits.
PI_PROBE_TIMEOUT=5; PI_PROBE_TRIES=3; PI_PROBE_BACKOFF="1 1"
# Port 1 on loopback: reserved, and nothing binds it.
drive "http://127.0.0.1:1/v1"
want_eq "nothing listening fails"          "1" "$RC"
want_in "  ...and says so"                 "nothing accepted a connection" "$DETAIL"
want_between "  ...paying only the waits"  0 4 "$SECS"
# Accepts the connection and never answers: the cost is every timeout plus every wait.
PI_PROBE_TIMEOUT=2; PI_PROBE_TRIES=3; PI_PROBE_BACKOFF="1 1"
start_srv stall; drive "$SRV_URL"
want_eq "no answer at all fails"           "1" "$RC"
want_in "  ...and names the budget"        "3 tries of 2s over 2s of waits" "$DETAIL"
want_between "  ...bounded by tries+waits" 8 11 "$SECS"

echo
echo "== the backoff index arithmetic =="
# More tries than backoff entries: the LAST entry repeats. 4 tries, backoff "1 2" =>
# waits of 1, 2, 2 = 5s. The reported total is the proof; an off-by-one in the index
# would read 1,1,2 or run off the end of the array.
PI_PROBE_TIMEOUT=2; PI_PROBE_TRIES=4; PI_PROBE_BACKOFF="1 2"
start_srv always503; drive "$SRV_URL"
want_eq "4 tries make 4 probes"            "4" "$(requests_seen)"
want_in "  the last backoff entry repeats" "4 tries of 2s over 5s of waits" "$DETAIL"
# An empty backoff must not read off the end of the array under `set -u`.
PI_PROBE_TIMEOUT=2; PI_PROBE_TRIES=2; PI_PROBE_BACKOFF=""
start_srv always503; drive "$SRV_URL"
want_eq "an empty backoff still fails cleanly" "1" "$RC"
want_in "  ...falling back to 5s"          "2 tries of 2s over 5s of waits" "$DETAIL"

echo
echo "== bad config refuses, and never touches the network =="
PI_PROBE_TIMEOUT=2; PI_PROBE_TRIES=3; PI_PROBE_BACKOFF="1 1"
start_srv ok
saved_model="$PI_MODEL"; PI_MODEL=""
drive "$SRV_URL"
want_eq "no model in the config fails"     "1" "$RC"
want_in "  ...naming the missing key"      "no harnesses.pi.model" "$DETAIL"
PI_MODEL="$saved_model"
drive ""
want_eq "no base_url in the config fails"  "1" "$RC"
want_in "  ...naming the missing key"      "no harnesses.pi.base_url" "$DETAIL"
want_eq "  ...and neither probed anything" "0" "$(requests_seen)"

echo
echo "== the shipped budget is the documented one =="
# These guard the numbers the comment in core-loop-matrix.sh reasons from. A free endpoint
# answers in ~1.2s. The last try STARTS at tries-1 timeouts plus every wait, so that is the
# busy window the preflight reliably survives; the budget expires one timeout later.
want_eq "default per-try timeout" "12"   "$DEF_TIMEOUT"
want_eq "default tries"           "3"    "$DEF_TRIES"
want_eq "default backoff"         "5 10" "$DEF_BACKOFF"
waits=0; for w in $DEF_BACKOFF; do waits=$((waits+w)); done
want_eq "waits total 15s"                       "15" "$waits"
want_eq "the last try starts at 39s"            "39" "$(( (DEF_TRIES-1)*DEF_TIMEOUT + waits ))"
want_eq "the budget expires at 51s"             "51" "$(( DEF_TRIES*DEF_TIMEOUT + waits ))"
# ~18% of the ~280s a cell takes. The SKIP exists so the daemon does not spend minutes on a
# dead model; it must not spend minutes proving one is dead either.
if [ "$(( DEF_TRIES*DEF_TIMEOUT + waits ))" -lt 60 ]; then ok "a dead endpoint costs under a minute"
else no "a dead endpoint costs a minute or more"; fi

echo "-----"
echo "core-loop-matrix preflight self-test: pass=$pass fail=$fail"
[ "$fail" -eq 0 ]
