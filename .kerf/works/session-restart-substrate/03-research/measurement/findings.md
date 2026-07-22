# Measurement — Research Findings (pass 3)

Work: `session-restart-substrate`. Proof standard: **replay-vs-frozen-baseline, not live A/B**
(02-components.md SB-R10 / SK-R10). Every number below was recomputed from the real frozen
baseline on 2026-07-13; every command shown was actually run.

Baseline root: `/Users/gb/github/harmonik/.harmonik/events/baseline-2026-07-13/`
(`$B` below = that path).

---

## 1. Baseline anatomy

### Contents

| File | Bytes | Lines | Notes |
|---|---|---|---|
| `events.jsonl` | 89,089,568 (85M) | **237,099** | append-only event log, UUIDv7 `event_id` |
| `issues.closed.jsonl` | 3,386,783 (3.2M) | — | beads closed set |
| `issues.jsonl` | 5,809,834 (5.5M) | — | beads open set (mode `r--------`) |

All files are read-only (`r--r--r--` / dir `dr-xr-xr-x`) — the freeze is enforced by permissions.

### Record format

One JSON object per line:

```json
{"event_id":"019ea6b6-dbfc-7a0a-864e-fa3c9605b7a4","schema_version":1,
 "type":"session_keeper_handoff_started","timestamp_wall":"2026-06-08T10:11:02.268658Z",
 "source_subsystem":"internal/keeper",
 "payload":{"agent_name":"skdog1","cycle_id":"cyc-20260608T101057-000001","session_id":"dogfood-abort-1"}}
```

Envelope fields: `event_id` (UUIDv7), `schema_version` (1), `type`, `timestamp_wall`,
optional top-level `run_id`, `source_subsystem`, `payload` (type-specific).
Run-scoped events (e.g. `handler_capabilities`) carry a top-level `run_id`; **all 78,069
events with `source_subsystem == "internal/keeper"` have NO top-level `run_id`** (verified:
`grep '"source_subsystem":"internal/keeper"' $B/events.jsonl | jq -r 'has("run_id")' | sort | uniq -c`
→ `78069 false`). This is the "zero run_id" joinability defect (EV-U2).

### Headline numbers — confirmed, with commands

```bash
B=/Users/gb/github/harmonik/.harmonik/events/baseline-2026-07-13

wc -l $B/events.jsonl                                   # 237099   total events

# Run completion (fleet-level context metric)
grep -c '"type":"run_started"'   $B/events.jsonl        # 2142
grep -c '"type":"run_completed"' $B/events.jsonl        # 1155  → 1155/2142 = 53.9% ≈ 54%

# Restart cycle events (keeper vertical)
grep -c '"type":"session_keeper_handoff_started"'  $B/events.jsonl   # 507
grep -c '"type":"session_keeper_cycle_complete"'   $B/events.jsonl   # 427  → 427/507 = 84.2% ≈ 84%
grep -c '"type":"session_keeper_cycle_aborted"'    $B/events.jsonl   # 79
grep -c '"type":"session_keeper_clear_unconfirmed"' $B/events.jsonl  # 347
```

Full type census: `jq -r .type $B/events.jsonl | sort | uniq -c | sort -rn`.

### Restart-cycle reconciliation (the ~476-vs-507 question — ANSWERED)

`payload.cycle_id` alone is **not unique**: the generator (`internal/keeper/cycle.go:434
newCycleIDGen`) makes `cyc-<keeper-process-start-ts>-<seq>`, which resets per keeper process,
so restarts of a same-pattern keeper collide.

```bash
grep '"type":"session_keeper_handoff_started"' $B/events.jsonl \
  | jq -r '.payload.cycle_id' | sort -u | wc -l          # 476  ← where "~476" comes from
grep '"type":"session_keeper_handoff_started"' $B/events.jsonl \
  | jq -r '"\(.payload.agent_name)|\(.payload.cycle_id)"' | sort -u | wc -l   # 507
```

**The correct current join key for a restart cycle is the composite
`(payload.agent_name, payload.cycle_id)` — 507 unique cycles**, exactly one key per raw
`handoff_started` event. (`session_id` is NOT usable: it changes mid-cycle by design, and
`session_keeper_warn` events carry `agent_name`+`session_id` but **no** `cycle_id`, so warns
cannot be strictly joined to a cycle — only heuristically by agent+time window.)

Per-cycle terminal reconciliation over the composite key (script below, actually run):

| Fact | Value |
|---|---|
| cycles started (`handoff_started`) | **507** |
| reached `cycle_complete` | **427** (84.2%) |
| reached `cycle_aborted` | **79** — reason `handoff_timeout` in **79/79** |
| `complete ∩ aborted` | **0** (terminals mutually exclusive — clean) |
| terminal without a `handoff_started` | 0 |
| duplicate `(type, agent, cycle_id)` events | 0 |
| **unterminated** (started, no terminal ever) | **1** — `kk-test / cyc-20260610T215853-000004` (a recorded real instance of the SR9 silence class) |
| cycles with `clear_unconfirmed` | **347** — and **all 347 also reached `cycle_complete`** |

So `clear_unconfirmed` is a **degraded-completion marker**, not a failure terminal:
**347/427 = 81.3% of "successful" completions fired the brief without a confirmed `/clear`**
(the `completeCycleTail` backstop-exhausted path, `internal/keeper/cycle.go:1101` comment and
`emitClearUnconfirmed` at `cycle.go:1614`). This is the single most damning baseline quality
number for the rebuild to improve.

Cycle date range: first `handoff_started` 2026-06-08T10:11:02Z, last 2026-07-13T04:46:30Z.

Reconciliation script (reproducible):

```bash
B=/Users/gb/github/harmonik/.harmonik/events/baseline-2026-07-13
jq -c 'select(.type|IN("session_keeper_handoff_started","session_keeper_cycle_complete",
                       "session_keeper_cycle_aborted","session_keeper_clear_unconfirmed"))
       | {t:.type, k:"\(.payload.agent_name)|\(.payload.cycle_id)"}' $B/events.jsonl \
| python3 -c '
import json,sys
sets={t:set() for t in ("session_keeper_handoff_started","session_keeper_cycle_complete",
                        "session_keeper_cycle_aborted","session_keeper_clear_unconfirmed")}
for l in sys.stdin: o=json.loads(l); sets[o["t"]].add(o["k"])
s,c,a,u = (sets[k] for k in sets)
print("started",len(s),"complete",len(c),"aborted",len(a),"clear_unconf",len(u))
print("unterminated:",s-c-a); print("complete∩aborted:",len(c&a),"clear⊆complete:",u<=c)'
```

### The join-key problem and how the 4 new events fix granularity

Today a cycle at BOUNDARY granularity is 2–3 durable events: `handoff_started` →
[`clear_unconfirmed`] → `cycle_complete` | `cycle_aborted`. The INTERIOR phases exist only in
the overwritten journal file `.harmonik/keeper/<agent>.cycle` — `CycleJournal` phases
`"opened" → "handoff_injected" → "confirmed" → "cleared" → "resumed" → "complete"/"aborted"`
(`internal/keeper/cycle.go:22-33`, atomic overwrite via `writeJournalFile` at `cycle.go:471`)
— i.e. the history is destroyed on every transition (dossier
`plans/2026-07-13-code-revamp/research/02-session-restart.md` §2).

The rebuild (EV-U1/U1a/U2, 02-components.md:186-200) adds 4 durable interior events —
`session_keeper_handoff_written`, `session_keeper_model_done`, `session_keeper_clear_sent`,
`session_keeper_new_session_up` — each carrying a REQUIRED, real (non-colliding) payload
`cycle_id`, plus a prohibition on zero envelope `run_id`. After the change a cycle is ~7
ordered durable events, and SR4 ("`/clear` never before model-done") becomes *checkable from
the log* — today it is unobservable because neither `model_done` nor `clear_sent` exists in
any of the 74 event types in the baseline census.

---

## 2. The replay corpus for keeper

### Does one exist? **No — it must be built.**

- `internal/keeper/conformance_keeper_test.go:32-38` (`TestKeeperConformance`) is
  **scenario delegation**, not recorded replay: it re-runs four named existing unit tests
  (`floor/band-min-200k-1m` → `TestMinAbsOrPctCeil`, etc.). Same for
  `conformance_keeperx_test.go` and `conformance_keeper_integration_test.go`.
- There is **no** `internal/keeper/testdata/` directory (verified by `ls`).
- The only recorded-corpus assets in the repo are codex-side:
  `testdata/codex-app-server/corpus/raw-session-01.jsonl` (path built at
  `internal/codextest/l1_contract_hkoe86p_test.go:30-36`), plus
  `testdata/codex-app-server/{gen,reactor-scenarios,protocol-schema.json}`.

### Extraction — baseline → per-cycle streams

Slice all keeper events by the composite key. Warn events lack `cycle_id`, so the strict
corpus is the cycle-keyed events only; warns can be attached as advisory context by
`(agent_name, time-window)` if wanted.

```bash
B=/Users/gb/github/harmonik/.harmonik/events/baseline-2026-07-13
OUT=testdata/keeper-cycles/baseline-2026-07-13     # proposed home, mirroring codex corpus layout

# one file per cycle, events in log (UUIDv7/arrival) order:
jq -c 'select(.source_subsystem=="internal/keeper" and .payload.cycle_id != null)
       | . + {ckey: "\(.payload.agent_name)|\(.payload.cycle_id)"}' $B/events.jsonl \
| python3 -c '
import json,sys,os,re
os.makedirs("cycles",exist_ok=True)
for l in sys.stdin:
    o=json.loads(l); k=re.sub(r"[^A-Za-z0-9._-]","_",o.pop("ckey"))
    open(f"cycles/{k}.jsonl","a").write(json.dumps(o)+"\n")'
# → 507 files; 2–3 events each at today's boundary granularity
```

Plus a `summary.json` per cycle (derived by the reconciliation script above): outcome
(`complete`|`aborted:handoff_timeout`|`unterminated`), `clear_unconfirmed` flag, start/terminal
timestamps, duration. The 507 summaries are the **golden expectations** for replay.

### What a replayed cycle contains

- **Today (boundary granularity):** `handoff_started` → [`clear_unconfirmed`] → terminal.
  Enough to golden-check *outcome + degraded-flag + bounded-time*, NOT interior ordering.
- **After the 4 events (interior granularity):** `handoff_started` → `handoff_written` →
  `model_done` → `clear_sent` → `new_session_up` → [`clear_unconfirmed`] → terminal.
  Enough to golden-check SR3 (handoff_written before clear_sent), SR4 (model_done before
  clear_sent), SR6 (brief only after new_session_up), SR7 (no interleaved cycle_ids per
  agent), SR9 (terminal within bound) — directly from the log, via the typed-decode reader
  (EV-U3).

### The input-synthesis caveat (design-critical, so Change-Design doesn't get this wrong)

The baseline records keeper **outputs**, not keeper **inputs** (gauge readings, handoff-file
appearance, session-id flips are not in the event log). So the keeper Twin is
**trace-driven**: for each recorded cycle the summary determines the stimulus schedule the
Twin presents on the ports — e.g. `aborted:handoff_timeout` ⇒ nonce never appears;
`complete`+`clear_unconfirmed` ⇒ nonce appears, session-id flip is delayed past the
clear-confirm backstop; clean `complete` ⇒ nonce and flip appear promptly. The in-repo
precedent for exactly this reactive fake already exists:
`internal/keeper/cycle_reactive_harness_test.go` (`reactiveSession`, lines 44+ — mutable
gauge + handoff state, InjectFn pattern-matches `/session-handoff`, `/clear`, `agent brief`
and mutates state "the way a real claude session would"). The Twin generalizes that fake to
be parameterized by a recorded-cycle summary, with time driven by the SB ClockPort (virtual
time — 507 cycles replay in milliseconds, not the recorded ~35 days).

---

## 3. Replay-regression harness design (old-logic vs new-logic, same streams)

Template: the codex L0–L3 stack, `internal/codextest/l0_wire_hkoe86p_test.go:1-19`
(tier map), `l1_contract_hkoe86p_test.go`, `l2_integration_hkoe86p_test.go`,
`internal/codexdigitaltwin/twin.go` (corpus → wire parse → Event → fault injection →
EventSource channel → reactor → FakeEffector; see the architecture diagram at twin.go:19-29
and `TestL1_TwinProducesExpectedActions` at l1_contract:181-215 for the golden-action
pattern).

Keeper mirror:

| Tier | Codex analog | Keeper replay harness |
|---|---|---|
| **L0 unit** | codexwire round-trip | pure `Step(state, input) → (state, []Action)` table tests + property tests (no IO, no clock) |
| **L1 contract** | corpus → twin → golden action seq (l1_contract:203-214) | 507-cycle corpus → keeper Twin → new reactor → emitted-event sequence per cycle must match the cycle's golden summary (outcome, clear_unconfirmed flag, event order); decode via the EV-U3 typed-decode registry so payload shape is also contract-checked |
| **L2 integration** | twin → reactor → HarmonikBridgeSink faked comms/queue/beads (l2_integration:23-57) | Twin → reactor → fake ports (PanePort/GaugePort/HandoffPort/EmitterPort/ClockPort) recording sink; assert WHAT would be injected/emitted without tmux |
| **L3 live** | CODEX_LIVE=1 gate (l3_live:1-13) | real tmux one-cycle smoke, env-gated (`KEEPER_LIVE=1`), analogous to existing `restartnow_smoke_integration_test.go` / `cycle_operator_attached_integration_test.go` |

**Old-vs-new differential over identical streams.** Both sides are already/will be drivable
offline:

- **Old logic** = the current `Cycler` (`internal/keeper/cycle.go:629 NewCycler`,
  `runCycle:935`, `completeCycleTail:1101`). It is fully fakeable today: `CyclerConfig`
  exposes injectable deps (`CycleIDGen`, `IsManagedFn`, `HandoffFilePath`, `ReadHandoff`,
  `HandoffModTimeFn`, … from `cycle.go:87` on) and its emissions go through the 3-method
  `Emitter` interface (`internal/keeper/watcher.go:20-30`) — a recording emitter captures its
  output events.
- **New logic** = the SK pure `Step` reactor behind the 5 ports.
- **Differential test:** for each of the 507 cycle descriptors, instantiate ONE Twin stimulus
  schedule; run old Cycler with the reactive fake, run new reactor with the same schedule on
  its ports; diff `(terminal type, terminal reason, clear_unconfirmed presence, relative
  event order, "did it terminate at all within the virtual-time bound")`. Permitted
  divergences are an explicit allowlist: only where a property test deliberately tightens
  the missing SR4 invariant (SK-R9 parity carve-out, 02-components.md:158-161), plus the 4
  extra interior events themselves (new-only, order-checked, never outcome-changing).

This construction is what catches the hang / false-close / ordering regression class: a hang
shows as new-side "no terminal within bound" where old side terminated (or vice versa —
including reproducing the 1 recorded unterminated cycle as a *known-divergence* the new side
must FIX, not match); a false-close shows as terminal-type divergence; an ordering bug shows
as interior-event order divergence.

The old-side differential is a **transition scaffold**: it lives while the carve is in
flight and is deleted when the old `runCycle` path is deleted; the L1 golden-vs-baseline
corpus test is the permanent regression net.

---

## 4. Fault-injection pass-rate (the 4 twin faults → SR9)

The four modes are lifted verbatim from `internal/codexdigitaltwin/twin.go:49-68`
(`FaultDropAfter`, `FaultStall`, `FaultTruncate`, `FaultDup`; applied at the configured
1-based `EventN`, twin.go:156-189). SK-R8 requires the invariants verified over the corpus
"+ the four SB fault modes". SR9 = SK-R6: every cycle reaches a terminal event within a
bounded window or emits a `restart_failed`-class event — **never silence**.

Keeper mapping and the "terminal signal never silence" assertion for each:

| Fault | Keeper stimulus (Twin) | Must produce (bounded virtual time, per SR9) |
|---|---|---|
| **drop** | session dies mid-cycle: gauge stops updating / pane vanishes after event N (analog of Disconnected injection, twin.go:159-166) | `cycle_aborted` (or `restart_failed`-class) with explicit reason; state machine returns to idle; NO half-open journal left claiming an in-progress cycle |
| **stall** | handoff nonce never appears (the `writeNonce=false` toggle already in `cycle_reactive_harness_test.go:20-23`) | `cycle_aborted{reason:handoff_timeout}` at `HandoffTimeout` (default 300s, `cycle.go:73`) — baseline proves this path fires (79/79 aborts are `handoff_timeout`); with ClockPort this asserts in ms |
| **truncate** | corrupt/truncated gauge ctx file or handoff read error at step N | explicit error-classed event or abort — a parse failure may NEVER be swallowed into silence (mirror of twin.go:173-177 replacing the event with an Error event) |
| **dup** | duplicate stimulus: nonce written twice, session-id flip observed twice, watcher tick re-delivered (mirror of twin.go:179-187; reactor-side dedup analog of codexreactor I2) | EXACTLY one terminal per `cycle_id`; no second `/clear` injection; no overlapping cycle started (SR7) — precedent: `internal/keeper/no_double_restart_hk1ryc_test.go` |

Uniform assertion shape (all four): run reactor under a virtual-time deadline; collect the
recording emitter's events for the cycle; assert
`len(terminals) == 1 && loop exited && (fault ⇒ terminal.reason != "")`. A test that times
out with zero terminals IS the silence bug — it must fail, not hang: bound with
`context.WithTimeout` wall-clock backstop exactly as the twin's ctx-cancel discipline does
(twin.go:107-115).

**Pass-rate metric:** 4 faults × a stratified cycle sample (minimum: 1 clean-complete, 1
clear_unconfirmed-complete, 1 handoff_timeout-abort, the 1 unterminated) × every applicable
`EventN` position = the fault matrix; required pass rate **100%** (these are invariants, not
statistics — one silence = fail).

---

## 5. Acceptance oracle (out-of-band, off-daemon)

Source: `plans/2026-07-12-codebase-census/PLAN.md:29-58` — four conditions, never a single
green run: (1) **N consecutive clean runs**, default N=10; (2) **fault injection produces a
terminal signal — never silence, never a fabricated close**; (3) **out-of-band verification**
— evidence that does not route through the path being fixed (diff-content assertion, coverage
number, direct filesystem/git check); (4) **measured coverage floor** on the carve target.

For THIS vertical (keeper runs per-session, daemon not involved — Constraint 1,
02-components.md:312):

1. **N consecutive:** `for i in $(seq 10); do go test -count=1 ./internal/keeper/... || exit 1; done`
   — the full replay-regression + fault-matrix suite 10× green. Replay is deterministic
   (ClockPort virtual time), so any flake across the 10 runs is itself a finding, not noise.
2. **Fault → terminal:** §4's matrix at 100% is the oracle's condition (2) verbatim.
3. **Out-of-band check — the checker is `jq` over the append-only log + direct filesystem
   reads, never the keeper's own report:**
   - Recompute every §6 metric with raw `jq`/`grep` against `$B/events.jsonl` (frozen — must
     be bit-identical, it's read-only) AND against the live `.harmonik/events/events.jsonl`
     window after a dogfood soak. The keeper does not grade itself; the log file does.
   - Direct filesystem assertions after a live cycle: HANDOFF file contains the nonce line
     (`nonceMarker` format `<!-- KEEPER:<id> -->`, `cycle.go:502`); the journal
     `.harmonik/keeper/<agent>.cycle` is in a terminal phase, not wedged mid-phase; the gauge
     file's `session_id` actually changed across the cycle.
   - This satisfies "the thing under repair cannot be its own oracle": the pipeline under
     repair is keeper cycle logic; the verification path is jq + stat + file-content diff.
4. **Coverage floor:**
   `go test -coverprofile=/tmp/keeper.out ./internal/keeper/... && go tool cover -func=/tmp/keeper.out`
   — record the measured line coverage of the new `Step`/reactor files and of the touched
   `cycle.go` paths in the change record; the floor value is a Change-Design decision, but it
   must be *measured and stated*, not "suite is green".

Everything above runs with zero daemon processes: `go test` + `jq` + file reads.

---

## 6. Metrics the rebuild must not regress (with recompute commands)

`B=/Users/gb/github/harmonik/.harmonik/events/baseline-2026-07-13`; substitute the live
`.harmonik/events/events.jsonl` (window: events after the deploy timestamp) for post-change
live recomputation. Replay-side, metrics 2–6 are asserted per-cycle by the L1 golden test.

| # | Metric | Frozen baseline | Bound | Recompute command |
|---|---|---|---|---|
| 1 | Restart-completion (cycles complete / cycles started, composite key) | **427/507 = 84.2%** | must not drop | `grep '"type":"session_keeper_handoff_started"' F \| jq -r '"\(.payload.agent_name)\|\(.payload.cycle_id)"' \| sort -u \| wc -l` and same for `session_keeper_cycle_complete`; divide |
| 2 | `clear_unconfirmed` per completion (degraded-completion rate) | **347/427 = 81.3%** | must not rise (rebuild should push it down — SR6 target) | `grep -c '"type":"session_keeper_clear_unconfirmed"' F` over completes as in #1 |
| 3 | Unterminated cycles (started, never terminal) — SR9 | **1** (`kk-test/cyc-20260610T215853-000004`) | **0** after rebuild | §1 reconciliation script, `unterminated:` line |
| 4 | Aborts are always explicit-reasoned | 79/79 `handoff_timeout` | every abort has non-empty `payload.reason` | `grep '"type":"session_keeper_cycle_aborted"' F \| jq -r '.payload.reason' \| sort \| uniq -c` (no empty bucket) |
| 5 | Terminal exclusivity + no dup terminals per cycle | 0 overlaps, 0 dups | stays 0 | §1 reconciliation script (`complete∩aborted`, dup count) |
| 6 | Interior ordering SR3/SR4/SR6 (post-change only — types don't exist in baseline: verify absence with `grep -c '"type":"session_keeper_model_done"' $B/events.jsonl` → 0) | n/a | per-cycle: `handoff_written < model_done < clear_sent < new_session_up < terminal` | typed-decode replay checker (EV-U3) over the live log; jq order-check per composite key |
| 7 | Fleet run-completion (context guard — keeper change must not disturb it) | **1155/2142 = 53.9%** | informational floor | `grep -c '"type":"run_started"' F`, `grep -c '"type":"run_completed"' F` |
| 8 | Fault-matrix pass rate (§4) | n/a (new) | **100%** | `go test -run 'TestKeeperReplay_Fault' ./internal/keeper/... -count=1` (name per Tasks pass) |
| 9 | Replay determinism / oracle N-run | n/a (new) | 10/10 green | `for i in $(seq 10); do go test -count=1 ./internal/keeper/... \|\| exit 1; done` |

Metric 1 nuance for honest reporting: baseline denominators mix dogfood/test agents
(`skdog1`, `kk-test`, `keeper-dogfood`, `keeper-smoke-live`) with production crews. The
frozen ratio 427/507 is the anchor number per SK-R10; any live re-baseline after deploy
should ALSO report the ratio excluding `agent_name` prefixes used only by tests, but the
frozen-number comparison stays apples-to-apples by using the same unfiltered rule.

---

## Appendix — source-of-truth citations

- Baseline: `/Users/gb/github/harmonik/.harmonik/events/baseline-2026-07-13/{events.jsonl,issues.jsonl,issues.closed.jsonl}`
- Cycle logic (old side): `internal/keeper/cycle.go` — `CycleJournal` phases :22-33,
  `CyclerConfig` injectables :38-100, `newCycleIDGen` :434, `nonceMarker` :502,
  `NewCycler` :629, `runCycle` :935, `completeCycleTail` :1101, emitters :1579-1625;
  `Emitter` interface `internal/keeper/watcher.go:20-30`.
- Reactive fake precedent: `internal/keeper/cycle_reactive_harness_test.go` (header + `reactiveSession` :44).
- Conformance (scenario-delegation, NOT a replay corpus): `internal/keeper/conformance_keeper_test.go:32-38`,
  `conformance_keeperx_test.go`, `conformance_keeper_integration_test.go`.
- Twin template: `internal/codexdigitaltwin/twin.go` (fault modes :49-68, injection :156-189,
  architecture :19-29); tiers `internal/codextest/l0_wire_hkoe86p_test.go:1-19`;
  golden-action L1 `internal/codextest/l1_contract_hkoe86p_test.go:181-215`;
  faked-sink L2 `internal/codextest/l2_integration_hkoe86p_test.go:23-57`;
  live-gate L3 `internal/codextest/l3_live_hkoe86p_test.go:1-13`.
- Codex corpus layout to mirror: `testdata/codex-app-server/corpus/raw-session-01.jsonl`
  (path constructed at `l1_contract_hkoe86p_test.go:30-36`).
- Requirements: `/Users/gb/.kerf/projects/gregberns-harmonik/session-restart-substrate/02-components.md`
  — SK-R4/R5/R6/R8/R9/R10 :140-164, EV-U1a event names :190-195, EV-U2 cycle_id :196-200,
  EV-U3 typed-decode :201-203, goals map :308-310, constraints :312-315.
- Acceptance Oracle: `plans/2026-07-12-codebase-census/PLAN.md:29-58`.
- Interior-phases-are-journal-only dossier: `plans/2026-07-13-code-revamp/research/02-session-restart.md`.
