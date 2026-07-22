# 04-Design / measurement — Change Design (pass 4)

> **Component:** measurement (SB-R10, SK-R8, SK-R10). Realizes **D13** of
> `04-design/00-decisions.md` within its pins; grounded on
> `03-research/measurement/findings.md` and the substrate/session-keeper findings.
> This is the design a spec-drafter (RS/SK measurement sections) and an implementer will follow —
> concrete scripts, file:line, package homes, exact commands. It does NOT restate D2/D7/D13; it
> obeys them.
>
> **Pins this design is bound by (00-decisions):** D2 fused `ReplayCodec[E]` (one type param, no
> seq in the seam); D3 generic fault vocabulary (`DisconnectEvent`/`ErrorEvent`); D4 `ClockPort` in
> substrate; D7 composite join key **`(agent_name, cycle_id)` = 507 cycles**; D9 harness sorts by
> `event_id`; D11 `Step` reactor + `InCycle` freeze; D12 model-done fail-open + old-corpus
> `ModelDone`-synthesis carve-out; D13 trace-driven Twin + old-vs-new differential + out-of-band
> oracle + frozen anchors. All numbers below are the frozen-baseline numbers recomputed on
> 2026-07-13; every command shown was run.

Baseline root `$B = /Users/gb/github/harmonik/.harmonik/events/baseline-2026-07-13/`
(`events.jsonl`, 237,099 lines, read-only). Repo root paths throughout.

**New homes this design creates (mirroring the codex stack 1:1):**

| Codex | Keeper analog (new) | Role |
|---|---|---|
| `testdata/codex-app-server/corpus/` | **`testdata/keeper-cycles/baseline-2026-07-13/`** | recorded corpus + goldens |
| `internal/codexdigitaltwin/` | **`internal/keepertwin/`** | trace-driven Twin + `ReplayCodec[keeper.Event]` |
| `internal/codextest/` | **`internal/keepertest/`** | L0–L3 tier tests + canary |
| `scripts/` (capture-fixtures target) | **`scripts/extract-keeper-corpus.py`** | one-shot corpus builder (ledgered) |
| `test-codex-l012` / `test-codex-live` | **`test-keeper-l012` / `test-keeper-live`** | Makefile gate pair |

---

## 1. The keeper replay corpus — build design

### 1.1 Confirmed: no corpus exists today

Verified this session: `ls internal/keeper/testdata testdata/keeper-cycles` → absent. The only
"conformance" assets are **scenario-delegation**, not recorded replay:
`internal/keeper/conformance_keeper_test.go:32-38` (`TestKeeperConformance`) re-runs four named unit
tests; `conformance_keeperx_test.go` / `conformance_keeper_integration_test.go` do the same. The
only recorded corpus in the repo is codex-side (`testdata/codex-app-server/corpus/raw-session-01.jsonl`).
**The keeper corpus must be built from the frozen baseline.** This is a one-shot, ledgered
extraction — the keeper analog of `capture-fixtures`, except its source is the frozen log, not a
live process, so it is fully deterministic and re-runnable.

### 1.2 Layout

```
testdata/keeper-cycles/baseline-2026-07-13/
  manifest.json                                   # aggregate self-check (see 1.4)
  EXTRACT-LOG.md                                  # ledger: source, date, script sha, counts
  cycles/
    skdog1__cyc-20260608T101057-000001.jsonl      # per-cycle event stream (log order)
    skdog1__cyc-20260608T101057-000001.summary.json
    ... (507 cycle pairs)
```

Filename = the composite key `(agent_name, cycle_id)` sanitized `[^A-Za-z0-9._-] → _`, joined by
`__` (the raw `|` join key is kept inside `summary.json.ckey`). **507 `.jsonl` + 507
`.summary.json`.** Each `.jsonl` holds the 2–3 boundary events for that cycle today; after the 4
interior events land (EV-U1) a re-extraction yields the 6–7 event interior stream and the same 507
files gain interior granularity — the extractor is granularity-agnostic (it slices by
`source_subsystem=="internal/keeper" && payload.cycle_id != null`, which captures both cohorts).

### 1.3 Finalized extraction script — `scripts/extract-keeper-corpus.py`

Single deterministic pass; writes both artifacts + manifest. Run once per baseline; re-run after the
4 events register to lift the corpus to interior granularity.

```python
#!/usr/bin/env python3
# scripts/extract-keeper-corpus.py — build the keeper replay corpus + goldens from a frozen log.
# Usage: python3 scripts/extract-keeper-corpus.py \
#          .harmonik/events/baseline-2026-07-13/events.jsonl \
#          testdata/keeper-cycles/baseline-2026-07-13
import json, sys, os, re, hashlib

SRC, OUT = sys.argv[1], sys.argv[2]
CYC = os.path.join(OUT, "cycles"); os.makedirs(CYC, exist_ok=True)

TERMINALS = {"session_keeper_cycle_complete", "session_keeper_cycle_aborted"}
STARTED   = "session_keeper_handoff_started"
UNCONF    = "session_keeper_clear_unconfirmed"

cycles = {}   # ckey -> list[event]  (log/arrival order; UUIDv7 event_id preserves it)
with open(SRC, "rb") as f:
    for raw in f:
        try: o = json.loads(raw)
        except json.JSONDecodeError: continue
        if o.get("source_subsystem") != "internal/keeper": continue
        p = o.get("payload") or {}
        cid, agent = p.get("cycle_id"), p.get("agent_name")
        if cid is None or agent is None: continue           # warns lack cycle_id -> excluded (strict)
        ckey = f"{agent}|{cid}"
        cycles.setdefault(ckey, []).append(o)

def ts(ev): return ev.get("timestamp_wall")
def eid(ev): return ev.get("event_id")

agg = {"started":0,"complete":0,"aborted":0,"clear_unconfirmed":0,"unterminated":0,
       "abort_reasons":{}, "count":0}
for ckey, evs in cycles.items():
    evs.sort(key=lambda e: (eid(e) or ""))                 # D9: sort by UUIDv7 event_id, not file order
    agent, cid = ckey.split("|", 1)
    types = [e["type"] for e in evs]
    started = STARTED in types
    term = next((t for t in types if t in TERMINALS), None)
    unconf = UNCONF in types
    reason = None
    if term == "session_keeper_cycle_aborted":
        reason = next((e["payload"].get("reason") for e in evs
                       if e["type"] == "session_keeper_cycle_aborted"), None)
    outcome = ("complete" if term == "session_keeper_cycle_complete"
               else "aborted" if term == "session_keeper_cycle_aborted"
               else "unterminated")
    start_ev = next((e for e in evs if e["type"] == STARTED), evs[0])
    term_ev  = next((e for e in reversed(evs) if e["type"] in TERMINALS), None)
    summary = {
        "ckey": ckey, "agent_name": agent, "cycle_id": cid,
        "outcome": outcome, "abort_reason": reason, "clear_unconfirmed": unconf,
        "started_at": ts(start_ev), "terminal_at": ts(term_ev) if term_ev else None,
        "session_id_start": start_ev.get("payload", {}).get("session_id"),
        "event_count": len(evs), "types": types,
    }
    base = os.path.join(CYC, re.sub(r"[^A-Za-z0-9._-]", "_", ckey))
    with open(base + ".jsonl", "w") as fh:
        for e in evs: fh.write(json.dumps(e, separators=(",", ":")) + "\n")
    with open(base + ".summary.json", "w") as fh:
        json.dump(summary, fh, indent=2, sort_keys=True)
    agg["count"] += 1; agg["started"] += started
    if outcome == "complete": agg["complete"] += 1
    if outcome == "aborted":
        agg["aborted"] += 1; agg["abort_reasons"][reason or ""] = agg["abort_reasons"].get(reason or "", 0)+1
    if outcome == "unterminated": agg["unterminated"] += 1
    if unconf: agg["clear_unconfirmed"] += 1

with open(os.path.join(OUT, "manifest.json"), "w") as fh:
    json.dump(agg, fh, indent=2, sort_keys=True)
print(json.dumps(agg, indent=2, sort_keys=True))
```

### 1.4 `summary.json` schema (per cycle — the golden)

| Field | Type | Meaning |
|---|---|---|
| `ckey` | string | `"<agent_name>|<cycle_id>"` — the D7 composite join key |
| `agent_name` | string | payload agent (denominator-strata source, §7 metric-1 nuance) |
| `cycle_id` | string | `cyc-<ts>-<seq>` (non-unique alone; unique with agent) |
| `outcome` | enum | `"complete"` \| `"aborted"` \| `"unterminated"` |
| `abort_reason` | string\|null | `"handoff_timeout"` for all 79 aborts; null otherwise |
| `clear_unconfirmed` | bool | **degraded-completion marker** (§7 metric-2); 347 true |
| `started_at` | RFC3339 | `handoff_started` timestamp_wall |
| `terminal_at` | RFC3339\|null | terminal timestamp_wall; null for the 1 unterminated |
| `session_id_start` | string | SID at cycle open |
| `event_count` | int | 2–3 (boundary) / 6–7 (interior after EV-U1) |
| `types` | []string | ordered type list (interior-order golden after EV-U1) |

`manifest.json` is the extractor's own regression check — the L1 canary asserts it against the
frozen anchors: `started:507, complete:427, aborted:79, clear_unconfirmed:347, unterminated:1,
abort_reasons:{handoff_timeout:79}`. If a future re-extraction shifts any of these, the canary fails
before any replay runs (D13 out-of-band principle applied to the corpus itself).

### 1.5 Boundary vs interior granularity (stated explicitly for the drafter)

- **Today (boundary):** each `.jsonl` = `handoff_started → [clear_unconfirmed] → terminal`. Enough
  to golden-check **outcome + degraded-flag + bounded-time**, NOT interior ordering.
- **After EV-U1 (interior):** `handoff_started → handoff_written → model_done → clear_sent[×n] →
  new_session_up | clear_unconfirmed → terminal`. Enough to golden-check SR3/SR4/SR6/SR7/SR9
  directly from the log via the EV-U3 typed-decode reader. The corpus home is the same; only
  `event_count`/`types` grow. **No structural rework** — the extractor and the Twin are written
  once against the boundary corpus and inherit interior granularity for free on re-extraction.

---

## 2. The trace-driven Twin design

### 2.1 The design-critical fact and its consequence

The baseline records keeper **outputs** (durable events), not keeper **inputs** (gauge readings,
handoff-file appearance, SID flips — none are in the log). So the corpus cannot be fed *directly*
into the new `Step` reactor, whose event vocabulary is the *input* set (`GaugeTick`, `NonceObserved`,
`ModelDone`, `SessionChanged`, `TimerFired`, …; session-keeper findings §3b). **The keeper Twin is a
trace-driven stimulus generator:** it reads each cycle's `summary.json`, synthesizes the *input*
schedule that would have produced that recorded outcome, and delivers it as
`substrate.EventSource[keeper.Event]`. The reactor's *emitted output* events are then compared back
to the corpus goldens. This generalizes the in-repo precedent
`internal/keeper/cycle_reactive_harness_test.go`'s `reactiveSession` (confirmed this session: it
holds mutable gauge+handoff state, pattern-matches injected `/session-handoff` / `/clear` /
`agent brief`, and mutates "the way a real claude session would", with `writeNonce` / `flipOnClear`
toggles) — from a hand-wired single scenario into a `summary`-parameterized generator over the ports.

### 2.2 Architecture (fault injection reused from the substrate seam)

```
summary.json ──► StimulusSynthesizer ──► []keeper.Event  (virtual-time-stamped, ClockPort)
 (outcome)         (trace-driven; §2.4)     serialized as an in-memory stimulus stream
                                                       │
                                    substrate.Twin[keeper.Event]        (D2/D3 fault injection @ EventN)
                                    + keepertwin.keeperCodec            (ReplayCodec[keeper.Event])
                                                       │  Events(ctx) <-chan keeper.Event
                                                       ▼
   discrete-event harness loop  ──►  substrate.Run(ctx, twin, reactor.Step, effector)
   (advances fake ClockPort to next armed timer when no external stimulus pends → TimerFired)
                                                       │
                                              recording effector / KeeperBridgeSink
                                                       ▼
                                   emitted output events  ──compare──►  summary.json + .jsonl golden
```

Routing the synthesized stimulus **through `substrate.Twin[keeper.Event]`** (rather than a bespoke
source) is the decisive choice: it means the four fault modes (D3) apply to keeper stimuli *for
free*, with zero keeper-specific fault code (§5). The Twin's `bufCap` defaults to 1 MB (D3 / substrate
R4) so an oversized synthesized line never truncates silently.

### 2.3 `keepertwin.keeperCodec` — the `ReplayCodec[keeper.Event]` (D2)

`internal/keepertwin/codec.go`:

```go
type keeperCodec struct{ /* stateful: seq uint64 for synthesized inputs */ }

// DecodeLine decodes one synthesized stimulus line into a keeper input Event.
//   emit=false,err=nil : advisory line (e.g. an attached warn) — skip.
//   err!=nil           : FATAL — corrupt stimulus; twin emits ErrorEvent(err) and closes.
func (c *keeperCodec) DecodeLine(line []byte) (keeper.Event, bool, error) { … }

// ErrorEvent = keeper's transport-error INPUT: a GaugeReadError/HandoffReadError input
// event that drives the reactor to an explicit abort (FaultTruncate, decode-fatal).
func (c *keeperCodec) ErrorEvent(msg string) keeper.Event { … }

// DisconnectEvent = keeper's connection-lost INPUT: a PaneVanished/SessionDied input
// event (the restart_failed-class stimulus; FaultDropAfter).
func (c *keeperCodec) DisconnectEvent() keeper.Event { … }
```

Per D3/substrate-R1, keeper has no native "Disconnected"/"Error" event, so `DisconnectEvent()` and
`ErrorEvent()` return keeper's **`restart_failed`-class input events** — the pane-vanished and
gauge/handoff-read-error stimuli that the reactor must convert to an explicit terminal (never
silence). Seq stays codec-internal (D2 / substrate-R2): it never appears in the substrate surface.

### 2.4 Outcome → port-stimulus timeline (the synthesizer's decision table)

`StimulusSynthesizer(summary, clock) []keeper.Event`. Every event carries a virtual-clock `at`
stamp; the harness advances the fake `ClockPort` deterministically. External stimuli come from the
Twin; `TimerFired` events are produced by the *shell* reacting to the reactor's `ArmTimer` actions
against the fake clock (the harness fires the earliest-armed timer whenever no external stimulus is
pending — standard discrete-event simulation).

| Recorded outcome (`summary`) | Synthesized input schedule → reactor behavior | Terminal asserted |
|---|---|---|
| **clean complete** (`complete`, `clear_unconfirmed:false`) | `GaugeTick`(gates pass, pct≥act) → reactor opens+injects, arms `handoff_timeout` → `NonceObserved`(before timeout) → arms `model_done_timeout` → `ModelDone`(before timeout, `.idle` flip) → reactor `/clear`, arms `clear_settle`+`clear_backstop` → `SessionChanged`(before backstop) → brief | `cycle_complete`, no `clear_unconfirmed` |
| **degraded complete** (`complete`, `clear_unconfirmed:true`) | identical **until Clearing**, then `SessionChanged` is scheduled **after** `clear_backstop` (or never within window) → `TimerFired(clear_backstop)` → `clear_unconfirmed` emitted, brief still fires | `cycle_complete` **with** `clear_unconfirmed` |
| **abort** (`aborted:handoff_timeout`, all 79) | `GaugeTick`(gates pass) → reactor injects handoff → `NonceObserved` **never scheduled**, no `HandoffFreshSeen` → `TimerFired(handoff_timeout)` no-fresh | `cycle_aborted{reason:handoff_timeout}` — never sends `/clear` |
| **unterminated** (the 1: `kk-test/cyc-20260610T215853-000004`) | the recorded SR9 hang: nonce+model-done land, `/clear` sent, then `SessionChanged` **never** and (in the old wedge) no backstop fired → total stall after Clearing | **known-divergence:** OLD wedges (no terminal); NEW **must** emit a terminal within bound (`clear_backstop`→`clear_unconfirmed`+`complete`, or `restart_failed`) — the new side FIXES it, does not match it |

The synthesizer is the single choke point where "recorded output → plausible input" lives; it is
deliberately small and table-driven so the mapping is auditable. Virtual time means all 507 cycles
replay in **milliseconds**, not the recorded ~35 days.

### 2.5 Old-corpus `ModelDone` carve-out (D12 / SK-R9)

The boundary corpus predates `model_done`. Per D12, when replaying a **boundary** cycle the
synthesizer schedules `ModelDone` **immediately after** `NonceObserved` (zero virtual delay), so the
old-corpus action goldens are byte-identical to today's clear-immediately behavior. Only
**interior** corpora (re-extracted after EV-U1) schedule `ModelDone` at its real `.idle`-flip offset
and assert the real SR4 ordering. This is the Constraint-4 parity carve-out, isolated to one line of
the synthesizer.

---

## 3. The L0–L3 keeper tier design

Mirrors codex (`internal/codextest/{l0,l1,l2,l3}_*_test.go` + `canary_*`), package
`keepertest_test` under `internal/keepertest/`.

| Tier | Codex analog (file:line) | Keeper design |
|---|---|---|
| **L0 unit** | `l0_wire_hkoe86p_test.go` round-trip + malformed table | Pure `keeper.Step(state, Event)→(state,[]Action)` **transition tables** (gate-ladder branches; terminal transitions `cycle_complete`/`cycle_aborted`/`clear_unconfirmed`) via `substrate.SyntheticSource[keeper.Event]` + `substrate.FakeEffector[keeper.Action]`; **property tests** (SR3/SR4/SR6/SR7 as pure postconditions over decoded order); `keeperCodec` golden-decode + malformed-line table; fake-`ClockPort` unit tests. **No IO, no wall clock, no tokens.** |
| **L1 contract** | `l1_contract_hkoe86p_test.go:181-215` golden-action replay | **507-cycle corpus → StimulusSynthesizer → `substrate.Twin[keeper.Event]` → new `Step` reactor → recording effector**; per cycle the emitted-event sequence must match the cycle's `summary.json` (outcome, `clear_unconfirmed` flag, and — interior corpus — `types` order). Corpus lines decoded via the **EV-U3 `internal/replay` typed-decode reader** (`Replay(path, since, strict, checkers)`, D6) so payload shape is contract-checked too. Corpus path via `runtime.Caller(0)` + `../../testdata/keeper-cycles/baseline-2026-07-13` (the codex `l1:30-36` idiom). **Permanent regression net.** |
| **L2 integration** | `l2_integration_hkoe86p_test.go:23-57` faked `HarmonikBridgeSink` | Twin → reactor → **`KeeperBridgeSink`** — a test-local recording fake of the 5 ports (`PanePort`/`GaugePort`/`HandoffPort`/`EmitterPort`/`ClockPort`, D10). Assert **what would be injected/emitted** (handoff cmd, `/clear`, brief, env, managed-session write, emitted events) with **no tmux**. Houses the fault matrix (§5). Sinks stay per-vertical (substrate R9 — no generic bridge sink). |
| **L3 live** | `l3_live_hkoe86p_test.go:30-35` `CODEX_LIVE=1` gate | `skipUnlessLive` env gate **`KEEPER_LIVE=1`**; **one real tmux pane, one scripted handoff→clear→resume cycle**; wire-canary assertions only. Analog of the existing `restartnow_smoke_integration_test.go` / `cycle_operator_attached_integration_test.go`. |
| **canary** | `canary_hkoe86p_test.go:34-90` | corpus non-empty, valid JSON, zero unknown event types, ≥N cycles, **and `manifest.json` == frozen anchors** (§1.4). |

**Makefile target pair** (mirroring `Makefile:112-121`, confirmed this session):

```makefile
.PHONY: test-keeper-l012
test-keeper-l012:  ## Keeper replay L0/L1/L2 gate (KEEPER_LIVE=0; corpus-driven)
	go test -count=1 ./internal/keepertest/... ./internal/keepertwin/... ./internal/keeper/...

.PHONY: test-keeper-live
test-keeper-live:  ## Keeper L3 live gate (KEEPER_LIVE=1 required; one-cycle tmux smoke)
	KEEPER_LIVE=1 go test -timeout 180s -count=1 -run TestL3_ ./internal/keepertest/...
```

`test-keeper-l012` is the pre-deploy gate for any keeper cycle change; `test-keeper-live` is the
one-cycle E2E smoke. No re-capture target is needed (unlike codex `capture-fixtures`) because the
corpus source is the frozen log — `scripts/extract-keeper-corpus.py` is a deterministic rebuild, not
a token-capped capture.

---

## 4. The old-vs-new differential harness (D13 transition scaffold)

**Purpose:** prove the new `Step` reactor is behavior-parity with the old blocking `Cycler` over the
identical stimulus, catching the hang / false-close / ordering regression classes before the old
path is deleted.

**Both sides are drivable offline today.** OLD = `internal/keeper.Cycler`
(`cycle.go:629 NewCycler`, `runCycle:935`, `completeCycleTail:1101`), fully fakeable via
`CyclerConfig`'s injectable fields (`cycle.go:87+`) with output through the 3-method `Emitter`
(`watcher.go:20-30`) — a recording emitter captures its events. NEW = the pure `Step` reactor behind
the 5 ports.

**Construction** (`internal/keepertest/differential_test.go`): for each of the 507 cycle descriptors,
`StimulusSynthesizer` builds **one** stimulus schedule, then:

1. Run OLD `Cycler` against a `reactiveSession`-style fake wired to the same schedule + recording `Emitter`.
2. Run NEW reactor over the same schedule on its ports + recording effector.
3. Diff the tuple:

   ```
   (terminal_type, terminal_reason, clear_unconfirmed_present,
    interior_event_order, terminated_within_virtual_bound)
   ```

**Permitted-divergence allowlist (exhaustive):**
- The **SR4 tightening** (D12): NEW may insert `model_done` before `clear_sent` and, in the degraded
  path, shift `/clear` timing — never changing the terminal *outcome*.
- The **4 new interior events** (`handoff_written`, `model_done`, `clear_sent`, `new_session_up`):
  NEW-only, order-checked, never outcome-changing.
- The **1 recorded unterminated cycle**: a *required* divergence — OLD produced no terminal; NEW
  MUST terminate within bound (the SR9 fix). The differential asserts this divergence exists and is
  in the fix direction; any *other* unterminated→unterminated match is a test failure, not a pass.

Anything outside the allowlist is a regression: a **hang** shows as NEW "no terminal within bound"
where OLD terminated; a **false-close** as a terminal-type divergence; an **ordering bug** as
interior-order divergence.

**Lifecycle:** the old-side differential is a **transition scaffold** — it lives only while the carve
is in flight and is **deleted with the old `runCycle` path**. The permanent net is the **L1
golden-vs-baseline corpus test** (§3), which needs only the new reactor.

---

## 5. The fault matrix (SK-R8, SR9)

The four modes are `substrate.FaultConfig{Mode, EventN}` (D2/D3), applied at the 1-based `EventN`
over the synthesized stimulus stream — **zero keeper-specific fault code**. SR9 (= SK-R6): every
cycle reaches a terminal within a bounded window or emits a `restart_failed`-class event — **never
silence**.

| Fault (substrate) | Keeper stimulus (via Twin) | Required terminal (bounded virtual time) |
|---|---|---|
| **FaultDropAfter** | after event N, `keeperCodec.DisconnectEvent()` (pane vanished / session died) then stream ends | the reactor's own armed-timer terminal, **bounded + explicit** — pre-nonce: `cycle_aborted{handoff_timeout}`; post-nonce: model_done fail-open → clear backstop → `cycle_complete` + `clear_unconfirmed`; state returns to `Idle`; **no half-open journal** claiming an in-progress cycle |
| **FaultStall** | block after event N (nonce never appears — the `writeNonce=false` analog) | position-dependent, always bounded: pre-nonce → `cycle_aborted{reason:handoff_timeout}` at `HandoffTimeout` (300s, `cycle.go:73`) — baseline proves this fires (79/79); post-nonce → the fail-open degraded `cycle_complete`; with fake `ClockPort` it asserts in ms |
| **FaultTruncate** | replace event N with `keeperCodec.ErrorEvent(...)` (corrupt gauge `.ctx` / handoff read error) | never silence: the sentinel replaces event N and the stream ends; the reactor proceeds to its armed-timer terminal within bound (pre-nonce abort / post-nonce degraded complete) — a parse/read failure **MUST NEVER** be swallowed into silence |
| **FaultDup** | deliver event N twice (nonce written twice / SID flip seen twice / tick re-delivered — reactor-side dedup analog of codex I2) | **exactly one** terminal per `cycle_id`; **no second `/clear`**; no overlapping cycle started (SR7); precedent `no_double_restart_hk1ryc_test.go` |

> **AMENDED 2026-07-13 (T12) — terminal-TYPE wording only; SR9 unchanged.** The original
> DropAfter/Truncate rows said "`cycle_aborted` or `restart_failed`-class" / "explicit
> error-classed event or abort". Reality per 00b R3: keeper's input vocabulary has no native
> disconnect/transport-error kind, so the codec's SENTINEL kinds (`twin_disconnected`,
> `twin_transport_error`) are ignored by the pure reactor's total transition; the terminal is
> **timeout-driven**, not disconnect-driven. SR9/SK-INV-005 is a bounded-LIVENESS invariant
> ("exactly one terminal within the bounded window — never silence"), not a terminal-type
> mandate, and `specs/session-keeper.md` SK-015 already carries exactly that wording — the table
> rows above are corrected to the verified reactor behavior; the invariant is not weakened.

**Stratified cycle sample** (the matrix rows) — minimum:
`{clean-complete, clear_unconfirmed-complete, handoff_timeout-abort, the-1-unterminated}` × **every
applicable `EventN` position** in that cycle's stimulus stream. 4 faults × 4 strata × N positions.
Applicable = every 1-based position of the STRIPPED discrete stimulus (T10 §2.2 harness). Two
combinations per stratum are **entry-foreclosed** — `FaultStall@1` withholds the cycle-opening
`GaugeTick` entirely and `FaultTruncate@1` replaces it with the ignored sentinel — so no cycle ever
opens and SR9 is vacuously satisfied; those cells assert the no-cycle shape instead (zero
`handoff_started`, zero terminals, zero journal writes, clean harness exit).

**Required pass rate: 100%** — these are invariants, not statistics; one silence = fail.

**Uniform assertion shape** (all cycle-opening cells): run the reactor under a virtual-time
deadline; collect the recording effector's events for the cycle; assert

```
len(terminals) == 1  &&  loopExited  &&  (terminal.kind == cycle_aborted ⇒ terminal.reason != "")
```

A run that times out with **zero** terminals **is** the silence bug — it must **fail, not hang**.
Two-layer bound (D11 makes timers explicit events, so this is clean): the **virtual-time deadline**
is the reactor's own armed-timer horizon
(`HandoffTimeout + model_done_timeout + ClearConfirmBackstop + injection overhead ≈ 520s virtual`,
session-keeper §4); the **wall-clock backstop** is a `context.WithTimeout` on the harness goroutine
(the codex twin's ctx-cancel discipline, `twin.go:107-115`) set to a few real seconds — it exists
only to convert a genuine code-hang into a test failure, never to gate the assertion.

---

## 6. The acceptance oracle (off-daemon)

The four census conditions (`plans/2026-07-12-codebase-census/PLAN.md:29-58`) instantiated for
keeper. **Every run is zero-daemon** — `go test` + `jq` + file reads only (Constraint 1;
`02-components.md:312`). The keeper runs per-session; the daemon is not involved.

1. **N=10 consecutive green.** `for i in $(seq 10); do go test -count=1 ./internal/keeper/... ./internal/keepertest/... ./internal/keepertwin/... || exit 1; done` — the full replay + fault suite 10× green. Replay is deterministic (fake `ClockPort` virtual time), so any flake across the 10 is itself a finding, not noise.
2. **Fault matrix 100%.** §5's matrix at 100% terminal-never-silence **is** census condition (2) verbatim.
3. **Out-of-band verification — the keeper does not grade itself; the log file does.** The checker is `jq`/`grep` over the append-only log + direct `stat`/file-content reads, never the keeper's own report:
   - Recompute every §7 metric with raw `jq`/`grep` against `$B/events.jsonl` (frozen → must be **bit-identical**, it is read-only) **and** against the live `.harmonik/events/events.jsonl` window after a dogfood soak.
   - Direct filesystem assertions after a live L3 cycle: HANDOFF file contains the nonce line (`nonceMarker` `<!-- KEEPER:<id> -->`, `cycle.go:502`); the journal `.harmonik/keeper/<agent>.cycle` is in a **terminal** phase, not wedged mid-phase; the gauge file's `session_id` **actually changed** across the cycle.
   - This satisfies "the thing under repair cannot be its own oracle": the pipeline under repair is keeper cycle logic; the verification path is `jq` + `stat` + file-content diff — a different code path entirely.
4. **Measured coverage floor** (stated, not "green"). `go test -coverprofile=/tmp/keeper.out ./internal/keeper/... && go tool cover -func=/tmp/keeper.out`; record the measured line coverage of the new `Step`/reactor/shell files and the touched `cycle.go` paths in the change record. **Design commitment:** the floor is *measured and stated*, not "suite passes". Provisional targets to ratify against first measurement: **≥85% on the pure `Step` file** (it is pure — high coverage is cheap and expected) and **≥70% on the shell/ports** (IO-bound; L3-only paths excluded). The ratified value becomes a ratchet in the change record — CI records the number; it may not drop below the ratified floor.

---

## 7. The 9 no-regress metrics

`F = $B/events.jsonl` (frozen). For post-change live recomputation substitute the live
`.harmonik/events/events.jsonl` windowed after the deploy timestamp. Replay-side, metrics 2–6 are
asserted per-cycle by the L1 golden test (§3).

| # | Metric | Frozen baseline | Bound | Recompute command |
|---|---|---|---|---|
| **1** | Restart-completion (complete / started, composite key) | **427/507 = 84.2%** | must not drop | `grep '"type":"session_keeper_handoff_started"' F \| jq -r '"\(.payload.agent_name)\|\(.payload.cycle_id)"' \| sort -u \| wc -l` and same for `session_keeper_cycle_complete`; divide |
| **2** | **`clear_unconfirmed` per completion (degraded-completion rate)** | **347/427 = 81.3%** | **must not rise — rebuild should push it DOWN (SR6)** | `grep -c '"type":"session_keeper_clear_unconfirmed"' F` over completes as in #1 |
| **3** | Unterminated cycles (started, never terminal) — SR9 | **1** (`kk-test/cyc-20260610T215853-000004`) | **0** after rebuild | §1 reconciliation, `unterminated:` line (manifest) |
| **4** | Aborts always explicit-reasoned | 79/79 `handoff_timeout` | every abort has non-empty `payload.reason` | `grep '"type":"session_keeper_cycle_aborted"' F \| jq -r '.payload.reason' \| sort \| uniq -c` (no empty bucket) |
| **5** | Terminal exclusivity + no dup terminals/cycle | 0 overlaps, 0 dups | stays 0 | §1 reconciliation (`complete∩aborted`, dup count) / manifest |
| **6** | Interior ordering SR3/SR4/SR6 (post-change only) | n/a — types absent from baseline (`grep -c '"type":"session_keeper_model_done"' F` → 0) | per cycle: `handoff_written < model_done < clear_sent < new_session_up < terminal` | EV-U3 typed-decode `internal/replay` checker over live log; jq order-check per composite key |
| **7** | Fleet run-completion (context guard — keeper change must not disturb it) | **1155/2142 = 53.9%** | informational floor | `grep -c '"type":"run_started"' F`; `grep -c '"type":"run_completed"' F` |
| **8** | Fault-matrix pass rate (§5) | n/a (new) | **100%** | `go test -run 'TestKeeperReplay_Fault' ./internal/keepertest/... -count=1` |
| **9** | Replay determinism / oracle N-run | n/a (new) | 10/10 green | `for i in $(seq 10); do go test -count=1 ./internal/keeper/... ./internal/keepertest/... \|\| exit 1; done` |

**Headline number (D13):** metric **2 — 81.3% degraded-completion (347/427)** — is the single most
damning baseline quality number, and the number the rebuild exists to improve. Today
`clear_unconfirmed` fires whenever the post-`/clear` SID flip does not confirm before the backstop
(`completeCycleTail` backstop-exhausted path, `cycle.go:1101` comment; `emitClearUnconfirmed`
`cycle.go:1614`) — the brief fires anyway and the cycle still records `cycle_complete`. SR6 (brief
only after `new_session_up` confirmed) plus the SR4 model-done wait are the levers expected to drive
this down: the L1 golden asserts it does not *rise*, and the live re-baseline reports the delta as
the headline improvement.

**Metric-1 denominator nuance (honest reporting).** The baseline denominators mix dogfood/test
agents (`skdog1`, `kk-test`, `keeper-dogfood`, `keeper-smoke-live`) with production crews. The frozen
ratio 427/507 is the anchor per SK-R10, so the frozen-vs-live comparison stays apples-to-apples by
using the same **unfiltered** rule. But any live re-baseline after deploy MUST **also** report the
ratio **excluding** test-only `agent_name` prefixes — otherwise a burst of test cycles silently moves
the headline. The change record states both numbers; the ratchet (metric 1 must-not-drop) is applied
to the unfiltered anchor.

---

## Traceability

| This design section | Requirement | Decision |
|---|---|---|
| §1 corpus build | SK-R10 (baseline anchor), SB-R6 (corpus artifact) | D7 composite key, D9 sort-by-event_id |
| §2 trace-driven Twin | SB-R10, SK-R8 | D2 ReplayCodec, D3 generic faults, D4 ClockPort, D12 carve-out |
| §3 L0–L3 tiers | SB-R6, SK-R8 | D6 typed-decode reader (L1) |
| §4 old-vs-new differential | SK-R9 (parity), SK-R10 | D13 scaffold + allowlist |
| §5 fault matrix | SK-R8, SR9 (=SK-R6) | D2/D3 faults |
| §6 acceptance oracle | SB-R10, Constraint 1 | D13 out-of-band |
| §7 no-regress metrics | SK-R10, Goal 6 | D13 frozen anchors |

**Deferred (not blocking this design):** the coverage-floor *value* (measured at implementation, §6);
interior-granularity re-extraction happens after EV-U1 lands (§1.5) — the boundary corpus is
sufficient to build and prove the entire harness first.
