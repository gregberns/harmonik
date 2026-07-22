# 04-design / harness-acceptance-design — C5 replay harness, L0–L3 taxonomy, fault DoD (M2)

> Pass 4 (Change Design), C5 area. Subordinate to `04-design/00-decisions.md` (esp. **D3**
> bounded liveness / **D9** harness taxonomy — conformed to here, NOT re-decided). Grounded in
> `03-research/harness/findings.md` (direct template) and `03-research/capture-tee/findings.md`
> (corpus provenance). **These are DoD / acceptance-and-test items, NOT normative spec prose**
> (M2-5 card). This file produces NO new normative requirements; it documents the acceptance
> intent, cross-references `specs/replay-substrate.md` (RS), and feeds the Tasks pass. The one
> conformance obligation it carries — the **AIS-INV-001** output-or-stale test — is owned as a
> requirement by the agent-input design sibling; C5 supplies its executable oracle.

---

## Current state

- **No M2 harness exists.** `testdata/` holds only codex (`codex-app-server/corpus/`) and P1
  keeper (`keeper-cycles/`) corpora. No captured claude input corpus exists on the tree.
- **Two working templates to copy.** `internal/codextest/` (L0–L3 + drift canary, Makefile L0-L2
  / L3 gates, no N-consecutive gate) and — the *direct* template — P1's `internal/keepertest/` +
  `internal/keepertwin/` (T10–T14: fault matrix, virtual-time bounded-liveness oracle, N=10
  script, per-file coverage floor, no-regress metrics, ACCEPTANCE bundle).
- **Generic replay engine is in place and vertical-neutral.** `internal/substrate/replay.go`
  (`Twin[E]`, `FaultConfig{Mode,EventN}`, `ReplayCodec[E]`), `clock.go`/`fakeclock.go`
  (deterministic virtual time), and the `substrate.Run[E,A]` seam. The four fault modes are
  **closed by RS-012** ("MUST implement … exactly these modes").
- **SC6 enforcement is not yet wired.** `.golangci.yml` has forbidigo (`fmt.Print`, `panic`) and
  per-component depguard, but **no `time.Sleep`/`time.After` ban and no `capture-pane` grep gate.**
  T13-style oracle scripts are **not** in `make check` — they need an explicit home or they rot.

## Target state

### Package layout (final naming = small Tasks-pass call)
Copy the keepertest shape wholesale. Two packages, `<v>` ∈ {`aistest`/`aistwin`} or
{`claudetest`/`claudetwin`} (pick one, reserve alongside the `AIS` spec prefix — D10):

- `internal/<v>test/` — `l0_wire`, `l1_contract`, `l2_integration`, `l2_fault_matrix`, `l3_live`,
  `canary`, plus `metrics_export` + `helpers` (per T13/T10).
- `internal/<v>twin/` — `codec` (the vertical `ReplayCodec[E]` for the claude wire), `synthesizer`
  (builds discrete stimulus corpora for the matrix), `twin` (thin `NewTwin` wrapper
  **re-exporting substrate fault types as aliases**, keepertwin/codec.go:15-31 idiom).

### The replayed event type E (the seam-gap resolution)
M2's headline failures live on the **effector/output (write-toward-agent) side**, but `Twin[E]`
injects faults into the **event stream feeding the reactor** — the same direction mismatch P1 hit.
**Resolution (D9): model the driver's ack/response stream as E.** The corpus records the driver's
inbound acks/responses (`Accepted`/`Rejected`/`Degraded` acks, protocol deltas, turn-terminals —
D2's ack vocabulary), and the four write-side failures map onto the closed fault modes:

| Real input failure | Fault mode | Asserts |
|---|---|---|
| Paste discarded by TUI (write lost) | `FaultDropAfter` | delivered-then-lost → `DisconnectEvent` terminal |
| TUI stall (rendered, never submitted) | `FaultStall` | consumer reaches output-or-stale by ctx/timeout (the SC6 stalled case) |
| Partial paste / partial frame | `FaultTruncate` | frame N replaced by `ErrorEvent` transport terminal |
| Double-submit | `FaultDup` | idempotence probe (replay.go:39) — reactor stays single-terminal |

The `<v>twin` codec supplies protocol-specific `ErrorEvent`/`DisconnectEvent` constructors, and —
following P1's **§5 SEAM-GAP path-A move** — defines **sentinel kinds** (`twin_transport_error` /
`twin_disconnected`, keepertwin/codec.go:47-54) that the reactor's total transition *ignores*, so
the reactor reaches its **own** timeout-driven terminal. This is a bounded-**liveness** invariant,
not a terminal-**type** mandate. **RS-012 is closed → we add codec sentinels, never new fault
modes or reactor vocab** (adding a mode would be a replay-substrate spec change).

### Fault matrix (`l2_fault_matrix`)
`4 modes × strata × every 1-based EventN of that stratum's stripped discrete stimulus`, **100%
pass required** ("invariants, not statistics; one silence = fail"). Per-cell uniform assertion:
exactly ONE terminal (accepted-ack XOR stale) within the **D3 bounded VIRTUAL window**
(`InputAckTimeout + injection overhead`, measured on `FakeClock`, never wall-clock); never-silence
enforced via three converters (in-flight-nothing-pending → fail, drainTwin idle timer → fail,
step-count guard → fail). **Entry-foreclosed cells** (`FaultStall@1` / `FaultTruncate@1` never open
a submit) assert the no-cycle / no-submit shape rather than a terminal.

### Bounded-liveness oracle — the AIS-INV-001 conformance test
For every `SubmitInput`, exactly one terminal — `Ack{Accepted|Rejected|Degraded}` OR an emitted
`agent_input_stale`-class event — within the bounded window; **silence is FORBIDDEN**. Expressed
in **FakeClock virtual time**: arm the driver's ack-timeout, `BlockUntil` the reactor arms its
sleep (avoids advance-before-arm race, fakeclock.go:188-198), `Advance` past the window, assert the
stale terminal fired. This is the executable oracle for the requirement the agent-input sibling
owns; it is the M2 peer of P1's SR9 (SK-INV-005), **phrased in M2's own vocabulary — it does NOT
cite STEP-0a** (STEP-0a lives only in `plans/`, not `specs/`).

### N-consecutive gate, coverage floor, no-regress metrics
- **N=10 script** (`scripts/<v>-oracle-n10.sh`, mirrors `keeper-oracle-n10.sh`): run the `<v>test`
  packages N times, fail fast on first red. Replay is deterministic (fake clock, virtual time) →
  **any flake is a finding, not noise.** Zero-daemon, zero-token.
- **Coverage floor** (`scripts/<v>-coverage-gate.sh` + `.baseline`): per-FILE statement coverage of
  the new driver reactor files (wire/reactor/step/ports) gated against ratified floors —
  measured-and-stated **ratchet**, not "suite passes".
- **No-regress metrics** (`scripts/<v>-metrics.sh`): input-ack metrics recomputed **from raw
  artifacts via jq/grep**, FROZEN (baseline log) vs REPLAY (persisted replayed stream from an
  env-gated `TestMetricsExport_ReplayedStream`) — **never from the driver's own report** (D13).

### Corpus provenance (production-captured via C4, not a spike)
Per capture-tee findings, the corpus is **production-captured by the C4 apptap tee** over real
claude headless sessions — the RS-018 vertical template: `testdata/<v>/corpus/*.jsonl` + a
**mechanically-emitted `CAPTURE-LOG` ledger** (keeper's `EXTRACT-LOG.md` is the executed model;
codex's declared-but-never-written `CAPTURE-LOG.md` is the anti-pattern to avoid). Multi-appender
corpora are **EventID-sorted before replay** (RS-INV-004). The synthesizer derives the matrix's
discrete stripped stimuli from this captured corpus; the **drift canary** asserts the corpus parses
with ZERO `FrameKindRaw`, is non-empty, and every line is valid JSON.

### SC6 enforcement ("zero sleeps / zero capture-pane scraping")
The enforceable *positive* form is "all waits via `ClockPort`". Three machine gates:
1. **forbidigo, path-scoped to the driver packages:** ban `^time\.Sleep$` **AND**
   `time\.After`/`time\.NewTimer` — a literal-`Sleep` ban alone is insufficient (the live path's
   blind waits are `time.After`-in-`select`). Carve-out: FakeClock's own spin
   (`fakeclock.go:196`) stays outside the scoped path.
2. **depguard deny** driver → `internal/lifecycle/tmux` — import-expressible **once C3/C6 land**;
   asserts the input path never re-imports the tmux write verbs.
3. **`capture-pane` grep gate** (Makefile/script over the driver packages) — `capture-pane` is an
   exec-arg string, not an import, so a grep ratchet is the cheapest enforcement.

### L-tier / gate → what it asserts

| Tier / gate | Asserts |
|---|---|
| **L0 `l0_wire`** | claude wire codec: golden frames, round-trip, malformed-input handling |
| **L1 `l1_contract`** | every corpus frame parses to a known method (no `FrameKindRaw`), no unmodeled Extra, re-serializes semantically equal; twin → full expected action sequence |
| **L2 `l2_integration`** | corpus → twin → reactor → `FakeEffector` (in-memory ack/comms/queue), actions checked out-of-band |
| **L2 `l2_fault_matrix`** | the 4×strata×EventN matrix at 100%, per-cell single-terminal-in-bound (AIS-INV-001) |
| **L3 `l3_live`** | env-gated (`<V>_LIVE=1`), token-capped one-turn **wire canary** — the PRE-DEPLOY E2E gate; asserts the handshake/ack completes, not content |
| **drift `canary`** | corpus parses zero-raw, non-empty, valid JSON — catches wire drift |
| **N=10 / coverage / metrics** | determinism (no flake), coverage ratchet, no-regress vs baseline |

Acceptance = **RS-020's four-part shape**: N-consecutive green + 100% fault matrix + out-of-band
jq/grep oracle (never self-grading — D13) + measured-and-stated coverage floor. P1's T14 ACCEPTANCE
bundle is the deliverable template.

## Rationale

- **D3 / D9 (conformed).** D9 mandates the keepertest package shape, the 4-mode matrix, the
  virtual-time oracle, sentinel seam-gap, N=10, coverage floor, and the SC6 gate trio. D3 fixes the
  oracle's content: output-or-stale, silence forbidden, timeout on `ClockPort`.
- **RS-012 closed → codec sentinels, not new fault modes.** Because RS-012 makes the four modes
  exhaustive *by spec*, the write-side/read-side direction mismatch is bridged by modeling **acks
  as E** and adding **codec-level sentinels** — extending the mode set would be a normative
  replay-substrate change, which C5 deliberately avoids.
- **RS-018 / RS-020.** RS-018 is the corpus/ledger/canary/Makefile-gate-pair template C5 fills;
  RS-020 is the acceptance shape. RS-INV-003 (substrate never-silence) is the substrate-level peer
  of the M2 oracle.
- **D13 "not its own oracle."** The metrics + acceptance oracle recompute from raw artifacts via
  jq/grep — the driver under repair cannot grade itself. This is *why* the matrix asserts via
  `FakeEffector.Actions()` and the metrics script reads the persisted stream, not the driver's log.

## Requirements traceability

| Success criterion / decision | C5 deliverable |
|---|---|
| **SC5** (capture tee feeds corpus) | C4-captured `testdata/<v>/corpus/*.jsonl` + `CAPTURE-LOG` is the matrix's input; drift canary guards it |
| **SC6** (L0–L3 + fault injection + N-consecutive + zero sleeps/scraping) | full tier set + 100% fault matrix + N=10 script + the SC6 gate trio (forbidigo path-ban, depguard tmux deny, capture-pane grep) |
| **SC7** (abort/rollback + bake gated on this harness) | C5 is the **acceptance oracle for C6's deletion**; the N-green + 100%-matrix bundle IS the bake-pass gate |
| **D3** bounded liveness | the AIS-INV-001 virtual-time output-or-stale oracle (per-cell + dedicated test) |
| **D9** taxonomy | the package layout, tier files, matrix dimensions, sentinel move, N=10, coverage floor |

## PLANNER-RECONCILE (carry to Tasks)

1. **Ack-direction corpus dependency.** The matrix requires the **driver's ack/response stream
   captured as the corpus E-type** (via apptap, SC5/C4). No input corpus exists today — **C4
   capture is a hard prerequisite for C5's matrix.** Tasks must order C4 → C5 and specify that C4
   records the ack/response direction, not just raw child stdout.
2. **SC6 gate home must be chosen or it rots.** T13-style oracle scripts are **not** in `make
   check`. Decide the N=10 + coverage + metrics gate home: **pre-deploy** (mirrors keeper's
   "GATE: green before any cycle change deploys" Makefile pair) vs **CI**. Recommendation: a
   `test-<v>-l012` / `test-<v>-live` Makefile pair as the pre-deploy gate (keeper precedent), with
   the N=10 script wired into the same pre-deploy step — this is the C6-deletion gate, so it must
   block a deploy, not merely a CI branch check.
3. **Final `<v>` package naming** (`aistest`/`aistwin` vs `claudetest`/`claudetwin`) — small call,
   settle it with the D10 spec-prefix decision so the corpus dir, twin package, and Makefile
   targets share one stem.
