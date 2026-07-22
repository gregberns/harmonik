# 03-research / harness — C5 replay harness + L0-L3 taxonomy + fault injection

> Pass 3 (Research), harness component (C5). Grounds Change-Design for the M2 input-path test
> taxonomy. All file:line verified against the tree on `phase1-session-restart-substrate`,
> 2026-07-14 (parent-written; sub-agent returned text).

## Research questions
1. Replay API — Twin[E], FaultConfig, ClockPort/FakeClock.
2. codextest exemplar tiers + Makefile gates + N-consecutive pattern.
3. P1 keeper harness (T10-T14) — fault matrix, bounded-liveness oracle, N=10, coverage floor.
4. Input-path faults vs the generic fault vocabulary.
5. Typed decode (D6 / EV-033) reuse.
6. Enforcement of SC6 "zero sleeps / zero capture-pane scraping".

---

## Q1 — Replay API: Twin[E], FaultConfig, ClockPort/FakeClock

**Twin[E]** (`internal/substrate/replay.go:82-121`): generic replay engine presenting a captured
NDJSON corpus (`io.Reader`) as an `EventSource[E]`. `NewTwin[E](corpus, fault, codec, opts...)`
(:104). `Events(ctx)` (:114) spawns one goroutine, channel buffer 16, closed on corpus
exhaustion / ctx cancel / fault-driven early stop. Scanner buffer default 1 MB (RS-010, :10-13),
override `WithBufferSize` (:97).

**FaultConfig** (:46-49): `{Mode FaultMode, EventN int}`, EventN 1-based over the post-skip
stream. Vocabulary (:22-41), vertical-neutral (RS-012):
- `FaultDropAfter` — deliver 1..N, then `codec.DisconnectEvent()`, end (:164-170).
- `FaultStall` — deliver 1..N-1, then block before N until ctx cancelled (:172-175).
- `FaultTruncate` — replace N with `codec.ErrorEvent("substrate: truncated frame")`, end (:177-180).
- `FaultDup` — deliver N twice ("an idempotence probe", :38-40, 182-190), then continue.

**ReplayCodec[E]** (:59-75): vertical supplies `DecodeLine(line) (ev, emit, err)` tri-state, plus
`ErrorEvent(msg)` and `DisconnectEvent()` constructors the injector needs. Fatal decode →
`ErrorEvent(err)` + close (:148-153). Codec may be stateful; seq/dedup lives in the codec (RS-008).

**Driving a reactor**: seam free-function `substrate.Run[E,A](ctx, src, step, eff)`
(`internal/substrate/seam.go:27-36`) — ranges `src.Events(ctx)`, applies pure `step(E) []A`,
executes each action on `Effector[A]`; nil on channel close, first effector error otherwise.

**Deterministic time**: `ClockPort` (`internal/substrate/clock.go:10-18`) — `Now/Since/NewTicker/
Sleep(ctx,d) bool`; "A bare Sleep(d) that cannot honor cancellation is non-conformant" (:15-17).
`FakeClock` (`internal/substrate/fakeclock.go:18-23`): virtual now moves ONLY on explicit
`Advance(d)` (:114-128); first fake tick after one full interval (:75-89); `BlockUntil(n)`
(:188-198) lets a test wait for the reactor to arm its sleep before advancing (avoids the
advance-before-arm race).

## Q2 — codextest exemplar

Five files in `internal/codextest/`:
- **L0** `l0_wire_hkoe86p_test.go` — unit: codexwire serializer golden frames / round-trip /
  malformed. Tier doc header :1-19.
- **L1** `l1_contract_hkoe86p_test.go:5-9` — every corpus frame parses, is a known method (no
  FrameKindRaw), no unmodeled Extra fields, re-serializes semantically equal; twin produces the
  full expected reactor-action sequence.
- **L2** `l2_integration_hkoe86p_test.go:24-36` — corpus → twin → reactor → `HarmonikBridgeSink`
  (test-only Effector faking comms/queue/beads in-memory).
- **L3** `l3_live_hkoe86p_test.go:5-13` — env-gated (`skipUnlessLive` :30-35, `CODEX_LIVE=1`),
  token-capped ("one minimal text turn, 'ping', 90s"), wire-canary only ("asserts the handshake
  completes, not content"). "the PRE-DEPLOY E2E GATE".
- **Drift canary** `canary_hkoe86p_test.go:4-18` — corpus parses with ZERO FrameKindRaw, corpus
  non-empty, every line valid JSON.

**Makefile gates**: codex L0-L2 :113; L3 :121 (`CODEX_LIVE=1 ... -run TestL3_`). No N-consecutive
gate for codex — that pattern arrived with P1 keeper (Q3).

## Q3 — P1 keeper harness (the direct template)

**Layout**: `internal/keepertest/` (l0_step, l1_contract, l2_integration, l2_fault_matrix,
l3_live, canary, metrics_export, helpers) + `internal/keepertwin/` (codec, synthesizer, twin).
Makefile pair :144-155: `test-keeper-l012` (KEEPER_LIVE=0, "GATE: must be green before any keeper
cycle change deploys") and `test-keeper-live`.

**Fault matrix** (`internal/keepertest/l2_fault_matrix_test.go`, `TestKeeperReplay_FaultMatrix`
:120): 4 modes × 4 strata × every 1-based EventN of that stratum's STRIPPED discrete stimulus =
11 positions × 4 modes = 44 cells. 100% pass required ("invariants, not statistics; one silence =
fail"). Per-cell (:20-32): exactly ONE terminal (complete XOR aborted) within the SK-015 bounded
VIRTUAL window (~520s virtual = HandoffTimeout+ModelDoneTimeout+ClearConfirmBackstop+overhead,
`sr9VirtualBound` :74-77); one handoff_started (SR7); terminal journal phase; abort ⇒ non-empty
reason. Never-silence via three converters: in-cycle-nothing-pending → fail (`runDiscrete` :187),
drainTwin idle timer → fail (:153-171, 2s), 100k-step guard → fail. Entry-foreclosed cells
(:53-58): FaultStall@1 / FaultTruncate@1 never open a cycle → assert the no-cycle shape.

**Bounded-liveness oracle — SR9, quoted.** `specs/session-keeper.md:264-266` (SK-INV-005): "For
every cycle `c`, `handoff_started(c)` MUST reach exactly one terminal outcome within the SK-015
bounded window, or emit a `restart_failed`-class event. A cycle that produces neither ... within
the window is a conformance failure. Every `TimerFired` edge lands in a state with an outgoing
action, so the machine cannot wedge silently." SK-015 (:191-193): "≈ 520s — or emit a
`restart_failed`-class event. **Silence is FORBIDDEN.**" Substrate-level analog RS-INV-003
(`specs/replay-substrate.md:260-262`): "Every fault mode MUST yield a terminal signal, never
silence ... `FaultStall` terminates by consumer ctx-timeout ... A vertical ... MUST NOT be able to
observe an indefinitely silent stream." (STEP-0a is the daemon resume-hang analog, only in plans:
`plans/2026-07-13-code-revamp/ROADMAP.md:52-54`, NOT in specs/.)

**N=10 oracle** (`scripts/keeper-oracle-n10.sh`): runs the keeper test packages N times (default
10), fails fast on first red; ":8-10: Replay is deterministic (fake ClockPort, virtual time), so
ANY flake ... is a finding, not noise". Zero-daemon, zero-token. T14 evidence (commit d28d031a):
N=10 → 10/10, 73s; fault matrix 100% (44 cells).

**Coverage floor** (`scripts/keeper-coverage-gate.sh:4-15`): per-FILE statement coverage of the
new reactor files (step/shell/ports) gated against ratified floors in
`scripts/keeper-coverage-floor.baseline` — "measured-and-stated (a ratchet), not 'suite passes'".
T14 floors: step 92.1% / shell 87.4% / ports 92.3%.

**No-regress metrics** (`scripts/keeper-metrics.sh:4-9`): 9 metrics recomputed from raw artifacts
with jq/grep — "NEVER from the keeper's own report (D13: the thing under repair cannot be its own
oracle)". FROZEN = jq over the read-only baseline log; REPLAY = jq over the persisted replayed
stream from env-gated `TestMetricsExport_ReplayedStream`
(`internal/keepertest/metrics_export_test.go:20-23`). T13 scripts are NOT Makefile targets — only
the tier pair (T10) is.

## Q4 — Input-path faults vs the generic vocabulary

M2 real failures (`01-problem-space.md:35-48`): exit-0-≠-accepted paste (no ack on the write),
skippable screen-scrape verify, blind submit-Enter ×3.

| Input failure | Generic mode | Fit |
|---|---|---|
| Paste discarded by TUI | `FaultDropAfter` | good — delivered-then-lost + disconnect signal |
| TUI stall (rendered, never submitted) | `FaultStall` | good — the SC6 stalled case; consumer must timeout-or-stale (RS-INV-003) |
| Partial paste | `FaultTruncate` | good — event replaced by transport-error terminal |
| Double-submit | `FaultDup` | exact — replay.go:39 "an idempotence probe" |

Generic vocab is sufficient AT THE TWIN LAYER, with the known seam-direction gap: the Twin injects
faults into the *event stream feeding the reactor*; M2's failures live on the *effector/output*
side (input written toward the agent). P1 hit the same mismatch and resolved it with the "§5
SEAM-GAP DECISION (path A)" (`l2_fault_matrix_test.go:36-52`): keepertwin defines SENTINEL kinds
`twin_transport_error` / `twin_disconnected` (`internal/keepertwin/codec.go:47-54`) that the total
reactor transition ignores; the reactor reaches its OWN timeout-driven terminal — "a
bounded-LIVENESS invariant, not a terminal-TYPE mandate." M2 should model the driver's
*responses/acks* (ack, nack, timeout, dup-ack) as the replayed event stream E and express
paste-discard/stall/partial/double-submit as fault positions in it — no protocol-specific fault
modes needed; the vertical codec supplies protocol-specific ErrorEvent/DisconnectEvent mappings.
RS-012 (replay-substrate.md:146-155) makes the four modes exhaustive/vertical-neutral BY SPEC
("MUST implement ... exactly these modes") — adding a mode would be a spec change; the
codec/sentinel pattern avoids it.

## Q5 — Typed decode (D6 / EV-033)

Registry: `internal/core/eventregistry.go:101` (`RegisterEventType`), :115
(`RegisterEventTypeAtVersion`). Adoption: `specs/event-model.md:722` — the typed-decode registry
path is "declared ADOPTED ... as the decode-and-assert layer of a sanctioned observational
replay-invariant-checking consumer (the `internal/replay` harness) ... the registry's first
production reader." Policy: schema-version mismatch = recorded finding, never fatal; unknown type
counted-and-skipped in tolerant mode. Usage: `internal/replay/replay.go:1-25` decodes each
envelope via the typed registry and runs SR3/SR4/SR6/SR7/SR9 checkers
(`internal/replay/checkers.go:23-30`); events SORTED by EventID first (RS-INV-004).

**Reuse for M2?** Yes for any invariant expressed over durable bus events (e.g. an input-ack
ordering invariant emitted as §8.20-class events). But M2's L0-L2 tiers primarily check *actions*
via `FakeEffector.Actions()` (RS-020(3), out-of-band jq/grep), which does not need the registry.
If M2 registers new durable input events, EV-027 registration is mandatory (event-model.md:499:
EV-026-internal events MUST NOT be passed to RegisterEventType).

## Q6 — Enforcement of SC6

- **forbidigo** (`.golangci.yml:39,60-63`): bans `^fmt\.Print.*$`, `^panic$`; path exclusions for
  tools/ and testhelpers/ (:44-54). **No `time.Sleep` ban today.**
- **depguard** (:64+): per-component import rules (BI-002 SQLite, PL-INV-002 LLM-SDK) —
  import-level only; cannot ban `time.Sleep` or `capture-pane` strings.
- **Coverage ratchet**: Tier-2 `check` runs `scripts/coverage-gate.sh` (Makefile:311) + keeper
  per-file floor gate.
- SC6 framed as an enforcement-ratchet item composing with testing-strategy-uplift/validation-net
  (`01-problem-space.md:240`).

Recommendation: (a) forbidigo pattern ban SCOPED BY PATH — deny `^time\.Sleep$` AND
`time\.After`/`time\.NewTimer` within the new input-driver packages (a literal-Sleep ban alone is
insufficient: problem-space :64-68 notes blind waits are `time.After`-in-`select`), the positive
complement being the ClockPort conformance rule (clock.go:15-17). (b) `capture-pane` is an exec-arg
string, not an import — cheapest ratchet is a grep-based Makefile/script gate over the driver
packages, OR (once SC3/SC4 land) a depguard deny of the input packages importing
`internal/lifecycle/tmux` (import-expressible). Carve-out: FakeClock's own `time.Sleep(time.Millisecond)`
spin in `BlockUntil` (fakeclock.go:196).

## Patterns to follow
1. Copy the keepertest package shape wholesale: `internal/<v>test/` (l0_step/l1_contract/
   l2_integration/l2_fault_matrix/l3_live/canary) + `internal/<v>twin/` codec re-exporting
   substrate fault types as aliases (keepertwin/codec.go:15-31). RS-018 (replay-substrate.md:198-210)
   is the normative artifact checklist.
2. Fault matrix = modes × strata × EventN positions, 100% required, per-cell uniform assertion, with
   entry-foreclosed cells asserting the no-cycle shape.
3. Never-silence via three converters, no golden values; `go test -timeout` outermost.
4. Sentinel-event seam-gap move (keepertwin/codec.go:35-54) — do NOT extend reactor vocab or
   substrate fault modes.
5. Acceptance = RS-020's four-part shape (replay-substrate.md:218-220): N-consecutive green, 100%
   fault matrix, out-of-band jq/grep oracle (never self-grading, D13), measured-and-stated coverage
   floor. T14's ACCEPTANCE bundle is the deliverable template.
6. FakeClock + virtual-time bounded window: express the M2 output-or-stale deadline in virtual time.

## Risks / conflicts
- **Direction mismatch (real):** substrate Twin replays an inbound event stream; M2's headline
  failures are on the write/ack path. The harness needs the driver's ack/response stream captured
  (via apptap.Tap, SC5/C4) as the corpus E-type. No recorded input corpus exists today (testdata/
  has codex + keeper-cycles only) — corpus capture (C4) is a prerequisite for the matrix.
- **RS-012 closed ("exactly these modes")** — protocol-specific fault modes need a replay-substrate
  amendment; plan on codec-level sentinels.
- **"Zero sleeps" needs a definition before enforcement** — the enforceable positive form is "all
  waits via ClockPort", checkable by banning `time.Sleep` AND `time.After`/`NewTimer` in the driver
  packages.
- **T13-style oracle scripts are not in `make check`** — M2's SC6 N-consecutive gate needs an
  explicit home (pre-deploy vs CI) or it rots.
- **Makefile keeper-conformance targets (:342-348, hk-urxa3) are a separate older set** from the
  T10-T14 taxonomy — don't conflate.
- SR9 wording lives in `specs/session-keeper.md` (SK-INV-005/SK-015), not replay-substrate.md
  (RS-INV-003); STEP-0a only in plans — M2's spec must state its OWN output-or-stale invariant, not
  cite STEP-0a normatively.
