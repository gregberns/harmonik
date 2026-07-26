# 04-design / m2-5-replay-fault-harness — C5 replay harness, L0–L3 taxonomy, fault injection (M2)

> Pass 4 (Change Design), C5 area. The **acceptance oracle for M2-6** (retire the flaky paste
> heuristics). Subordinate to `04-design/00-decisions.md` (D3 bounded liveness / D9 harness
> taxonomy — conformed to, NOT re-decided) and to the FINALIZED spec draft
> `05-spec-drafts/agent-input.md` (AIS-003 / AIS-018 / AIS-INV-001). **These are DoD /
> acceptance-and-test items, NOT normative spec prose** (M2-5 card). No new AIS-nnn requirements;
> the one conformance obligation carried is **AIS-INV-001** output-or-stale, whose requirement is
> owned by the agent-input sibling — C5 supplies its executable oracle.
>
> **Supersedes the stale framing in `harness-design.md` + `harness-acceptance-design.md`.** Both
> predate the hook-sourced-ack rescope (COORD c021, revision-history entry 2 in the spec draft).
> They model the Claude path as a `claudewire` codec whose corpus is the driver's wire
> ack/response stream. Per AIS-003 (SETTLED) there is **no Claude wire protocol** — Claude's
> positive ack is a **Claude-hook-bridge durable event** (`outcome_emitted` on `Stop`,
> `agent_ready` on `SessionStart`), never a wire frame and never a pane scrape. This file
> re-homes the Claude-path oracle onto the `internal/replay` durable-event Checker layer and
> reconciles the two corpora. The deltas are enumerated in §9.

---

## 1. Current state (what P1 landed = the template)

P1 landed a **two-layer** replay harness and its second instantiation. M2-5 copies both layers;
it invents no new substrate machinery.

- **Layer A — wire-level replay** (`internal/substrate/replay.go`): `Twin[E]`,
  `FaultConfig{Mode,EventN}`, `ReplayCodec[E]`, the four fault modes (`FaultDropAfter`,
  `FaultStall`, `FaultTruncate`, `FaultDup` — `replay.go:22-41`) **closed by RS-012** (exactly
  these, never extend), plus `clock.go`/`fakeclock.go` virtual time and the `substrate.Run[E,A]`
  seam.
- **Layer B — durable-event invariant replay** (`internal/replay/replay.go`, `checkers.go`):
  `Replay(path, since, strict, checkers)` scans an `events.jsonl` via `eventbus.ScanAfter`
  (EV-047), EventID-sorts (D9, `replay.go:219-225`), decodes each envelope **through the
  typed-decode registry** (`DispatchObservational` tolerant / `DecodePayloadStrict` strict —
  `replay.go:255-266`, EV-048/EV-049) and validates schema version
  (`ValidateEnvelopeSchemaVersion`, `replay.go:240-241`), then feeds each `Checker`
  (`checkers.go:43`) and finalizes via the optional `Finalizer` (`checkers.go:55`,
  `replay.go:209`). **SR9 is the exact shape M2 reuses:** `SR9Checker.Finalize` flags every
  unterminated cycle ("unterminated 1 → must be 0", `checkers.go:149-181`) — the bounded-liveness
  anchor.
- **Second instantiation = the direct template.** `internal/keepertest/` (l0_step, l1_contract,
  l2_integration, **l2_fault_matrix**, l3_live, canary, metrics_export, helpers) +
  `internal/keepertwin/` (codec, synthesizer, twin). M2-5 mirrors this package pair wholesale
  (RS-021/RS-022 pattern). `internal/codextest/` is the sibling reference for the structured
  (Codex) driver's wire tier.
- **Corpora on tree today:** `testdata/codex-app-server/corpus/` (codex wire) and
  `testdata/keeper-cycles/` (durable keeper events). **No M2 input corpus exists yet.**
- **SC6 machine gates not yet wired.** `.golangci.yml` has forbidigo (`fmt.Print`, `panic`) and
  per-component depguard, but **no `time.Sleep`/`time.After` path-ban and no `capture-pane` grep
  gate** on a driver package (which does not exist until M2-2).

## 2. The seam-gap resolution: two verticals, two layers

M2 is **two peer input paths** (AIS-003: "PEERS, not tiers"), and they ack over **different
channels** — so the harness covers each on the layer that matches its ack channel:

| Path | Ack channel | Replay layer | Corpus | Template |
|---|---|---|---|---|
| **tmux / Claude** (first-class paste transport) | Claude-hook-bridge durable event on the daemon bus (`agent_ready`/`outcome_emitted`, AIS-003) | **Layer B** — `internal/replay` Checkers over the hook-event `events.jsonl`, decoded via the typed-decode registry | durable hook-event corpus (event-sourced, EV-047) | P1 `internal/replay` + `keepertest` |
| **structured Codex driver** | codec `InputAcked`/`turn/completed` on the app-server wire (AIS-006, §7.1) | **Layer A** — `substrate.Twin[E]` wire replay | codex wire corpus (byte-tee) | `codextest` / `keepertwin` |

The M2-5 headline oracle — **hook-acked-XOR-stale, never silence** (the resume-hang fix) — lives
on **Layer B for the Claude path**, because the awaited positive signal *is a durable event*, not
a wire frame. This is the crisp correction over the prior design files, which tried to force the
Claude ack through a `claudewire` `Twin[E]` codec. The Codex path keeps a Layer-A `Twin[E]` wire
harness (codextest-shaped) because it genuinely has a wire ack.

The `Twin[E]` **direction mismatch** (D9: faults inject into the event stream *feeding* the
reactor, but M2's failures are on the *write-toward-agent* side) is resolved identically to P1:
**model the ack/response stream as E**. On the Codex path E = the wire ack/turn events; on the
Claude path the "stream" is the hook-event corpus and the injected fault is a missing/late/dup
hook event. The four write-side failures map onto the closed modes via **codec sentinels**
(`twin_transport_error` / `twin_disconnected`, the keepertwin/codec.go path-A move), never new
fault modes — RS-012 is closed, and adding a mode would be a normative replay-substrate change.

## 3. Fault matrix — which faults map to real input failures

`4 modes × strata × every 1-based EventN of the stratum's stripped discrete stimulus`,
**100% pass required** ("invariants, not statistics; one silence = fail"). Strata =
{fresh-seed/start, resume-brief} for the Claude path; {handshake, mid-turn} for Codex. Each cell
asserts **exactly one terminal within the D3 bounded VIRTUAL window** (`InputAckTimeout +
injection overhead`, measured on `FakeClock`, never wall-clock).

**Claude / hook-ack path (Layer B) — the load-bearing matrix:**

| Injected fault | Real input failure (evidence) | Required terminal |
|---|---|---|
| `FaultStall` @ ack anchor | **hook never arrives** — `/clear`-resume that never fires `agent_ready`; env wedge / `[Y/n]` splash race (the resume-hang class). **This is the stalled-agent injection.** | no hook within window → `agent_input_stale` (AIS-INV-001) |
| `FaultDropAfter` @ post-submit | pasted-but-`Stop` never fires / Enter lost (hk-3qjwl, hk-76n5g); child exits pre-ack | `DisconnectEvent` → `agent_input_stale{reason}` |
| `FaultTruncate` @ ack anchor | paste discarded by TUI / partial submit (pasteinject.go) — no valid ack event lands | `ErrorEvent` transport terminal → `agent_input_stale` |
| `FaultDup` @ ack anchor | double submit / blind `Enter×3` (hk-8cq23) — two `Stop`/`agent_ready` for one submit | exactly-one `agent_input_acked` per `input_seq` (IN-INV-002 idempotence, `replay.go:39` probe) |
| `FaultNone` | baseline | clean golden: `agent_input_acked` fires |
| (assertion, ALL cells) | silent no-ack → false-close / infinite wait (hk-4ie1z, hk-trjef) | **zero-silence: some terminal ALWAYS** |

**Codex structured path (Layer A):** the same four modes over the wire ack stream — Stall @
handshake → `handshake_timeout`; DropAfter @ N=1 → `driver_exited`; Truncate → transport-error →
`Rejected`/stale; Dup → single ResolveSubmit. Largely covered by extending `codextest` shape.

**Entry-foreclosed cells** (`FaultStall@1` / `FaultTruncate@1` — a fault that lands before any
submit opens) assert the no-cycle / no-submit shape, not a terminal.

## 4. The output-or-stale oracle (hook-acked XOR stale)

The **AIS-INV-001 conformance test**, expressed in FakeClock virtual time. For every
`SubmitInput`: exactly one terminal — `Ack{Delivered}` resolved by an observed
`agent_input_acked` (whose SOURCE on the Claude path is the hook-bridge `outcome_emitted` /
`agent_ready` event, AIS-003) **XOR** an emitted `agent_input_stale` — within the bounded window.
**Silence is FORBIDDEN.** Two independent expressions, both required (RS-020 out-of-band, D13
"never self-grading"):

1. **Structural (Layer-B Finalizer, the SR9 shape).** An `INInvAckOrStale` Checker keyed on
   `(run_id, input_seq)` over the three `agent_input_*` events plus the two hook-source events
   (`outcome_emitted`, `agent_ready`): every `agent_input_submitted` reaches `agent_input_acked`
   XOR `agent_input_stale`; the `Finalizer` flags every unterminated submit at end-of-scan (the
   `checkers.go:168-181` template — "unterminated must be 0"). Ordering checker
   `submitted < acked` on EventID order after sort (`replay.go` sort, EV-INV analog).
2. **Timer (virtual-time stalled-agent injection).** Arm the driver's ack-timeout, `BlockUntil`
   the reactor arms its sleep (avoids the advance-before-arm race, `fakeclock.go`), inject
   `FaultStall` so **no hook event arrives**, `Advance` past the window, assert the
   `agent_input_stale` terminal fired and the daemon recovered (did not wedge). This is the
   concrete "stalled-agent injection → assert `agent_input_stale`" the M2-5 card names.

The oracle **reads the persisted replayed event stream via jq/grep and the typed-decode Checker
log**, never the driver's own report (D13). **No pane-scrape appears in the ack path** — the
oracle's positive signal is a registered hook event, asserted structurally; a `capture-pane`
grep gate (§6) enforces its absence.

## 5. Corpus provenance and size (production-captured, not spike)

**Two corpora, two provenances** — the key reconciliation over the prior files' single
claude-wire corpus:

- **Claude / hook-ack corpus (the primary oracle subject) — event-sourced, EV-047.** Because the
  hook ack IS a durable event, the Claude-path corpus is captured by scanning production
  `events.jsonl` (`eventbus.ScanAfter`) for the `agent_input_*` + `agent_ready`/`outcome_emitted`
  cohort over **real bead runs** — exactly how P1 sources `testdata/keeper-cycles/`. It lands at
  `testdata/<v>/corpus/*.jsonl` with a **mechanically-emitted `CAPTURE-LOG` ledger** (keeper's
  `EXTRACT-LOG.md` executed model; codex's declared-but-never-written `CAPTURE-LOG.md` is the
  anti-pattern to avoid). Multi-appender corpora are **EventID-sorted before replay**
  (RS-INV-004; `replay.go` already does this). **This LOOSENS the M2-4 hard dependency** for the
  headline oracle: the hook-event corpus does not need the C4 apptap byte-tee — it is already
  recorded by the event system. (Prior `harness-acceptance-design.md` §PLANNER-RECONCILE 1
  asserted "C4 capture is a hard prerequisite for C5's matrix"; that holds for the *Codex wire*
  corpus only — see §9 delta.)
- **Codex wire corpus — byte-tee, C4/M2-4.** The structured driver's stdio wire is
  production-captured verbatim by the M2-4 apptap tee into `testdata/<codex-v>/corpus/*.jsonl`.
  This is the corpus that genuinely needs M2-4.

**Size / drift canary (RS-018 item 2):** each corpus asserts non-empty, every line valid JSON,
parses with **zero `FrameKindRaw`/unknown-type frames**, and meets a minimum frame/cycle count
(codextest 4-gate shape, `canary_hkoe86p_test.go:34-90`; ≥10 frames precedent). The synthesizer
derives the fault matrix's discrete stripped stimuli FROM the captured corpus (keepertwin
synthesizer idiom); only the fault schedules are synthetic.

## 6. Typed-decode registry reuse (P1 D6 / EV-033 / EV-048)

**Resolved: the Claude-path harness reuses the P1 typed-decode registry directly; it writes no
bespoke JSON parse.** Per EV-048 the registry path — `Event.DecodePayload` /
`DecodePayloadStrict`, `core.ValidateEnvelopeSchemaVersion`, `DispatchObservational` /
`DispatchSynchronous`, `pertypecompat` `LookupPayloadCompatEntry` — is the **sanctioned
decode/assert layer for the `internal/replay` observational consumer** (EV-033's "observational
consumer"). Concretely:

- The Layer-B `Replay()` over the hook-event corpus decodes every `agent_input_*` /
  `agent_ready` / `outcome_emitted` envelope through `DispatchObservational` (tolerant: unknown
  type counted-and-skipped, EV-033) for **historical** corpora, and through
  `DecodePayloadStrict` (EV-049, `DisallowUnknownFields`) in **strict mode** when replaying the
  harness's OWN freshly-recorded corpus — where an unknown field means a `mustRegister` was
  forgotten or a writer drifted. This is exactly `internal/replay/replay.go:255-266` reused
  unchanged; M2 adds only its Checkers, not a decode path.
- **Prerequisite (event-model, not M2-5):** the three `agent_input_*` payload types must be
  registered per EV-027 (`mustRegister`) with a `pertypecompat` N-1 entry — owned by event-model
  §8/§6.3 per AIS-004, consumed here. `agent_ready`/`outcome_emitted` are already registered
  (claude-hook-bridge). M2-5 asserts the registry entries exist as an L1 gate (registered-type
  lookup), catching a forgotten registration.
- A schema-version mismatch is a recorded **finding** (writer/reader drift via
  `CompatWindowHolds`), never fatal (EV-048) — same as SR replay.

The Codex Layer-A path does NOT use the event registry (it decodes wire frames via its
`ReplayCodec[E]` / `codexwire`); the registry reuse is specific to the durable-event Layer-B
oracle.

## 7. The L0–L3 split (zero-token L0/L1/L2, env-gated live L3, drift canary)

Package pair `internal/<v>test/` + `internal/<v>twin/` (final `<v>` stem = a small Tasks-pass
call, settle with the D10 spec-prefix — `aistest`/`aistwin` recommended so corpus dir, twin
package, and Makefile targets share one stem with the `AIS` prefix). RS-017/018 conformance:

| Tier / gate | Asserts | Cost |
|---|---|---|
| **L0 `l0_step` / `l0_wire`** | pure `Step` transitions (the AwaitingAck table, spec §7.2) + codec golden-decode + malformed-line table via `SyntheticSource`+`FakeEffector` | zero-token, no IO |
| **L1 `l1_contract`** | whole-corpus decode: zero unknown types / zero `FrameKindRaw`, no unmodeled `Extra`, re-serialize semantically equal; **registered-type lookup gate** (§6); golden action-sequence `Twin`/`Replay` → reactor → `FakeEffector` → `DeepEqual` | zero-token |
| **L2 `l2_integration`** | corpus → replay → reactor → per-vertical bridge sink (in-memory ack/comms/queue), actions checked out-of-band | zero-token |
| **L2 `l2_fault_matrix`** | the §3 4×strata×EventN matrix at 100%, per-cell single-terminal-in-bound (§4 oracle) | zero-token |
| **L3 `l3_live`** | env-gated (`<V>_LIVE=1`), token-capped one-turn wire/hook canary — the PRE-DEPLOY E2E gate; asserts handshake/ack completes, not content. Doubles as the corpus recorder (codex `Makefile:123-129` `capture-fixtures` precedent) | env-gated, budget-capped |
| **`canary`** (drift) | corpus non-empty, valid JSON, zero-raw/zero-unknown, ≥min frames (§5) | zero-token |
| **N=10 / coverage / metrics** | determinism (fake clock → any flake is a finding), coverage ratchet, no-regress vs frozen baseline | zero-token |

L0–L2 MUST be **zero-token and run with the live gate off** (RS-017; codex "~95% zero-token"
property). Only L3 requires the gate. Substitution is by which `EventSource`/`Effector` value is
wired — **no runtime test-branch** (RS-017 / RS-INV-... no-test-branch, consistent with the
substrate-selection axis AIS-015).

**SC6 machine gates ("zero sleeps / zero pane-scrape in the ack path") — three ratchets:**
1. **forbidigo, path-scoped to the driver packages:** ban `^time\.Sleep$` **AND**
   `time\.After` / `time\.NewTimer` (a literal-`Sleep` ban alone is insufficient — blind waits
   are `time.After`-in-`select`). Carve-out: FakeClock's own spin stays outside the scoped path.
2. **depguard deny** driver → `internal/lifecycle/tmux` — import-expressible once C3/C6 land
   (AIS-002); asserts the input ack path never re-imports the tmux write verbs.
3. **`capture-pane` grep gate** (Makefile/script over the driver packages) — `capture-pane` is an
   exec-arg string not an import, so a grep ratchet is the cheapest enforcement of "no pane-scrape
   in the ack path" (AIS-011).

**Gate home (RS-019):** a `test-<v>-l012` / `test-<v>-live` Makefile target pair + single
`<V>_LIVE` env gate (mirrors codex `CODEX_LIVE`, `Makefile:111-121`). The **N=10 script**
(`scripts/<v>-oracle-n10.sh`, mirrors `keeper-oracle-n10.sh`) wires into the **pre-deploy** step,
not merely CI — because this bundle IS the M2-6-deletion bake gate (SC7), it must block a deploy.
Coverage floor (`scripts/<v>-coverage-gate.sh` + `.baseline`, per-FILE statement coverage of the
new reactor/driver/step/ports files) and no-regress metrics (`scripts/<v>-metrics.sh`, recomputed
from raw artifacts via jq/grep, FROZEN baseline vs env-gated `TestMetricsExport_ReplayedStream`)
follow the P1 D13 discipline; coverage numbers are measured-and-stated at implementation, never
invented here.

## 8. Acceptance = RS-020's four-part shape (the M2-6 bake gate)

1. **N=10** consecutive green `test-<v>-l012` + driver integration (determinism).
2. **Fault matrix 100%** — every mode×stratum×EventN yields a terminal, never silence.
3. **Out-of-band oracle** — jq/grep/`stat` + the typed-decode Checker log read the persisted
   replayed stream; the harness never grades itself (D13).
4. **Measured-and-stated coverage floor** on the new reactor/driver files.

Plus the three SC6 machine gates (§7) green. P1's T14 ACCEPTANCE bundle is the deliverable
template. **This bundle passing IS the precondition M2-6 consumes to delete the flaky paste
heuristics** (SC7); a Stage-B live bake (N consecutive live bead runs, zero input-class incidents;
abort → config back to `tmux`) gates the physical deletion, not this offline oracle.

## 9. Deltas vs the prior two harness design files (what this pass changes)

1. **Claude ack channel corrected: hook event, not wire frame.** `harness-design.md` §1-2 and
   `harness-acceptance-design.md` §"replayed event type E" model a `claudewire` `Twin[E]` codec
   over the driver's wire ack stream. Per AIS-003 (SETTLED post-c021) there is no Claude wire
   protocol — the Claude ack is a durable hook-bridge event. **The Claude-path oracle moves to
   the Layer-B `internal/replay` Checker surface** (typed-decode registry), keeping `Twin[E]`
   Layer-A only for the Codex structured driver.
2. **Two corpora, not one.** Split into (a) the event-sourced Claude hook-event corpus (EV-047,
   like keeper-cycles) and (b) the byte-tee Codex wire corpus (M2-4).
3. **M2-4 dependency loosened for the headline oracle.** `harness-acceptance-design.md`
   PLANNER-RECONCILE 1 called C4 "a hard prerequisite for C5's matrix." Corrected: the
   hook-event corpus is event-sourced and does **not** need the byte-tee; only the *Codex wire*
   corpus needs M2-4. The stalled-agent hook-ack oracle — the M2-6-gating deliverable — can be
   built from event capture as soon as M2-3 emits the `agent_input_*` cohort.
4. **Typed-decode reuse made concrete.** Prior files gestured at `internal/replay` Checkers
   (`harness-design.md` §4); this pass pins the EV-048 tolerant/EV-049 strict decode path
   (`replay.go:255-266`) as the exact reused surface and names the registration prerequisite.

`harness-design.md` §3's eight-incident mapping and §5's baseline-proxy framing remain useful and
are folded into §3/§7 here.

## 10. Residual PLANNER-RECONCILE (carry to Tasks)

1. **M2-5 status → still `pending-design` until deps land, but design-complete after this pass.**
   The **P1 seam is already proven** (`internal/substrate` + `internal/replay` + `keepertest`
   landed), so the P1 gate is cleared. M2-5's remaining hard prereqs are **M2-2** (driver `E`/`A`
   types + Codex `ReplayCodec`), **M2-3** (`ready` — emits the `agent_input_*` + hook-ack cohort
   the Layer-B corpus/oracle consume), and **M2-4** (`pending-design` — the Codex wire corpus
   only; NOT the Claude hook oracle). Acceptance-delta: add "hook-acked-XOR-stale Layer-B oracle
   over the event-sourced hook corpus (EV-048 decode); Codex wire matrix on Layer-A" and record
   that the headline oracle is unblocked by M2-3 alone.
2. **Registration prerequisite is event-model's, consumed here.** The three `agent_input_*`
   payloads must be `mustRegister`'d with N-1 `pertypecompat` entries (AIS-004, event-model
   §8/§6.3) before the Layer-B strict decode passes. Order: event-model registration → M2-5.
3. **Final `<v>` package stem** (`aistest`/`aistwin` recommended) — settle with the D10/OQ-AIS-005
   spec-prefix call so corpus dir, twin package, and Makefile targets share one stem.
4. **SC6 depguard rule is import-expressible only once C3/C6 land** (a driver package exists to
   deny); until then gates 1 (forbidigo) and 3 (`capture-pane` grep) carry SC6, and the depguard
   deny is added in the M2-6 change.
