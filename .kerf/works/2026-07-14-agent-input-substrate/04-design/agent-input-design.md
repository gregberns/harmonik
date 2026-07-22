# 04-design / agent-input-design — NEW spec `specs/agent-input.md` (prefix AIS)

> Pass 4 (Change Design) for M2 area C1/C2/C3(boundary)/C4/C7. Subordinate to
> `04-design/00-decisions.md` (D0–D10) — this file MUST NOT re-decide any of them; where a
> decision carries `PLANNER-RECONCILE:`, the tag is carried inline at the requirement it
> touches. Sibling design files own the PL amendment (`process-lifecycle-design.md`), the HC
> seam widening (`handler-contract-design.md`), the keeper carve-out reconciliation
> (`session-keeper-design.md`), and the C5 harness/DoD (`harness-acceptance-design.md`).
> Grounded in `03-research/{seam-contract,driver,capture-tee}/findings.md`, `02-components.md`,
> `01-problem-space.md`. This is a DESIGN doc; the normative spec text is Pass 5.

---

## Current state

**New file — no `specs/agent-input.md` exists.** The `AIS` prefix and the `agent-input` spec-id
are unreserved in `specs/_registry.yaml`. The seam today (`internal/handler/substrate.go`) has a
single-method `Substrate` (`SpawnWindow`), a deliberately input-free `SubstrateSession`
(Kill/Wait/Outcome/PID/Stdout), and a `substrateSessionAdapter` whose `SendInput`/`CloseStdin` are
silent no-ops (`:140`/`:173`). Agent input flows out-of-band through six type-asserted
side-interfaces in `internal/daemon/pasteinject.go` (`enterSender`/`paneCapturer`/`quitSender`/
`paneOutputSizer`/`paneLivenessChecker`/`commandRunnerProvider`) driving a tmux
`load-buffer→paste-buffer→send-keys` dance; `exit 0` is the only "ack" and it lies
(`pasteinject.go:131`). The handler↔tmux depguard "boundary" is doc-comment-only, not a machine
rule. `apptap.Tap` has zero production consumers. There is no bounded-liveness clause for input.

## Target state

New spec `specs/agent-input.md`, `requirement-prefix: AIS`, reserved in `specs/_registry.yaml` in
the SAME commit (registry lint rule). Front-matter v1.1 exactly per RS/SK: `title` / `spec-id:
agent-input` / `requirement-prefix: AIS` / `status: draft` / `spec-shape: requirements-first` /
`spec-category: runtime-subsystem` / `version: 0.1.0` / `spec-template-version: 1.1` / `owner` /
`last-updated` / `depends-on: [replay-substrate, process-lifecycle, handler-contract,
session-keeper]`. 12-section layout: **1 Purpose, 2 Scope, 3 Glossary, 4 Normative requirements,
5 Invariants, 6 Schemas & data shapes, 7 Protocols & state machines, 8 Error taxonomy,
9 Cross-references, 10 Conformance, 11 Open questions, 12 Revision history.** §2 carries an
RS-023-style disambiguation clause (**AIS-000**) stating this spec sits ON the *process-spawn*
sense of "substrate" — it extends `internal/handler.Substrate` / the PL-021b Substrate seam with a
typed input verb, and MUST NOT be conflated with the record→replay `internal/substrate` seam (cite
RS-023), the cognition-session substrate, or the transport substrate.

> PLANNER-RECONCILE: final filename/prefix (`agent-input.md`/`AIS` vs `agent-input-protocol.md`/
> `AIP`) is a small registry call (D10). This design proceeds with `agent-input.md`/`AIS`.

### §4 Normative requirements (each `#### AIS-00x — <name>` + `Tags:` footer)

**4.1 The seam input port & the retirements** *(C1 · D1)*

- **AIS-001 — The `InputPort`.** A new consumer-owned narrow port (RS-004 idiom: 2 methods, no
  Result/Option/Either) MUST be declared handler-side:
  `SubmitInput(ctx, InputRequest) (Ack, error)` (blocks until ack-or-stale per AIS-INV-001) and
  `CloseInput(ctx) error` (end-of-input; replaces the `CloseStdin` no-op). `SubstrateSession` is
  widened so a session MAY satisfy `InputPort`; the daemon obtains it by structural assertion at
  the seam. The six type-asserted side-interfaces and the `substrateSessionAdapter.SendInput`/
  `CloseStdin` no-ops MUST be retired as the input mechanism. *PLANNER-RECONCILE: input verb on
  `SubstrateSession` directly vs a separately-asserted `InputPort` — this design picks the separate
  narrow port.*
- **AIS-002 — The handler↔tmux boundary is a machine rule.** The spec MUST require a real depguard
  deny (RS-005 leaf style) that `internal/handler` does not import `internal/lifecycle/tmux`,
  landed WITH the seam change — replacing today's doc-comment-only "boundary." *(seam-contract Q1,
  risk #3; PLANNER-RECONCILE carried from D1: land the real rule.)*

**4.2 The Ack** *(C1 · D2 · D0)*

- **AIS-003 — `Ack` contents.** `Ack` MUST carry (a) an acceptance **class** `Accepted | Rejected |
  Degraded`; (b) a driver-internal **monotonic input sequence id** (codec-owned, RS-008); (c) an
  optional **protocol-level acceptance token** (the turn id the input opened, when the wire supplies
  one). `Degraded` = "written, but acceptance could not be positively confirmed" — the interim
  tmux/paste impl during the bake window MUST return exactly `Degraded`, never a false `Accepted`.
- **AIS-004 — Dual signal: synchronous return AND emitted event.** `SubmitInput` MUST return the
  `Ack` synchronously to the caller AND the driver MUST emit a durable `agent_input_acked`-class
  event (and `agent_input_stale` on the timeout terminal, AIS-INV-001) so async observers see the
  ack on the event stream without compile-coupling M2 to any consumer's State type. *PLANNER-
  RECONCILE (D0 headline): the planner brief said M3 owns/defines this contract FIRST; the repo task
  graph (5 concordant TASKS.md citations) says M2 owns it. This design follows the repo — M2 OWNS.
  The dual sync-return+event shape is direction-agnostic and survives either reconciliation; confirm
  before either work implements.*
- **AIS-005 — Front-stop, not replacement.** The synchronous ack MUST be an earlier positive
  acceptance signal in FRONT of the existing async watchdogs (`agent_heartbeat` HC-057, commit-poll);
  it MUST NOT remove or weaken them (M2 non-goal "NOT the hook-bridge transport"). M3 owns dissolving
  the kill-ladder watchdog; the M2/M3 cut line (M2-1 → M3-4) is stated here, not rebuilt in M3.

**4.3 The structured-protocol driver** *(C2 · D4)*

- **AIS-006 — Second `Substrate` impl over `substrate.Run[E,A]`.** The driver MUST be designed
  protocol-abstractly as the codex quartet analog: a `claudewire`-shaped codec (single method
  registry + `Extra map[string]json.RawMessage` on every payload + unknown-method-as-Raw passthrough)
  → flat `Event`/`Action` vocabulary (omitempty, JSON-round-trippable) + a pure `Step(ev) []Action`
  (no goroutines, no I/O, returns nil not empty) → a `Run` one-liner over
  `substrate.Run(ctx, src, Step, eff)`, timed exclusively via `ClockPort`. Seam instantiations MUST
  use type aliases, not defined types (RS-021). The protocol is designed AGAINST the assumption that
  claude headless supports NDJSON stdin input (`--input-format stream-json` / Agent SDK stdin) with a
  turn-based handshake analogous to codex app-server (initialize → turn/start-like input frame →
  streamed `*/delta` → turn/completed-like terminal; steer/interrupt analogs).
- **AIS-007 — Corpus-first, codec-freeze gated on a T0 capture spike.** The FIRST implementation
  task MUST be a T0 apptap-tee capture over a real claude headless session; the codec MUST NOT be
  frozen until the spike pins the actual input framing. *PLANNER-RECONCILE: the claude input side is
  UNPROVEN in-repo — `--input-format stream-json` bidirectional persistent-stdin appears nowhere in
  the tree. The T0 spike MUST confirm input framing + steer/interrupt support before codec freeze.*
- **AIS-008 — Billing gate before C2 commitment.** Headless `stream-json` mode MUST be confirmed to
  bill the Max subscription (not the API credit pool) before C2 is committed. *PLANNER-RECONCILE: the
  project's strongest historical constraint (subscription-not-API, c2-spec.md:104-109 rejected the
  Agent SDK sessions HTTP API on this ground) has never been evaluated against headless stream-json;
  operator confirmation required.*

**4.4 Process ownership & stdio** *(C2/C3 · D5 — LOCKED)*

- **AIS-009 — The driver owns the child's stdio directly.** The structured driver MUST own the
  child's stdin + stdout pipes (apptap tees both), the codex app-server pattern — because a real
  bidirectional protocol is incompatible with tmux owning the pty while the daemon reads the child's
  stdout for protocol events. Existing house machinery to reuse: `handler.Session` StdinPipe +
  `SendInput`/`CloseStdin` (the daemon's native NDJSON-to-child-stdin shape) and `apptap` pipe
  splice. *(driver Q4)*
- **AIS-010 — `SubstrateSpawn.StdinDevNull` disposition.** The spec MUST state what happens to
  `StdinDevNull` (today: set for codex `< /dev/null`, FORBIDDEN for claude paste-harness). Under a
  real input protocol the driver owns stdin per harness; the flag's meaning changes and MUST be
  respecified (the claude structured driver keeps stdin OPEN and owned; the paste-harness case is
  retired with the input stack). *(seam-contract risk #6)*

**4.5 Observation-only tmux boundary** *(C3 · D5 · D6)*

- **AIS-011 — tmux is observation-only on the production input path.** The spec MUST state the
  boundary: no `load-buffer`/`paste-buffer`/`send-keys` on the production input path; at most
  `capture-pane` survives as a human-facing read/observation window. "Inspectability" (locked
  guardrail) is satisfied by the C4 capture tee (tail the recorded wire) + an OPTIONAL tmux
  observation window fed the captured stream — NOT by tmux owning the process. This is a BOUNDARY
  STATEMENT; the PL-021d demotion detail is owned by the sibling `process-lifecycle-design.md`.
  *PLANNER-RECONCILE (locked-decision boundary, `project.yaml:26` "tmux inspectability required"): a
  headless stream-json claude is not a TUI (a tmux pane shows raw NDJSON), so "inspectability"
  reinterprets from "watch the TUI in tmux" to "tail the captured wire (+ optional observation
  pane)." This design does NOT reopen the decision — it reinterprets. Operator must confirm a
  capture-tee-backed observation window satisfies the guardrail before design lock; the FIFO-stdin +
  side-pipe-stdout hosting variant is the heavier fallback.*
- **AIS-012 — Deletion boundary & carve-outs recorded (C6 gate + keeper/CLI carve-out).** The spec
  MUST record that write-verb retirement is scoped to the **daemon-RUN input path**, and MUST NOT
  delete the PL-021d `load-buffer`/`paste-buffer`/`send-keys` verbs that KEEPER (SK-002, off-daemon,
  session-id-addressed, PL-021d paste) and the CLI boot-seed/wake paths (`captain.go`, `crew.go`,
  `comms.go`) depend on. The spec amends SK-002's cross-reference rather than contradicting it; the
  actual SK reconciliation is owned by the sibling `session-keeper-design.md`. Deletion itself (C6)
  is gated behind the C5 bake window and is DoD/teardown, not new normative prose here.
  *PLANNER-RECONCILE (D6): TASKS.md M2-3/M2-6 state "keeper MIGRATED to the structured driver" before
  C6 deletes; SK-002 (normative 2026-07-13) requires keeper's PL-021d paste. This design resolves the
  A11 deletion hazard via carve-out + narrowed C6 scope (safe, unblocks M2) and defers migrate-vs-
  carve-out to the planner; migration would add a session-id-keyed input contract task + an SK-002
  re-draft. (seam-contract risks #1, #2, #5.)*

**4.6 Live capture tee** *(C4 · D8)*

- **AIS-013 — Pipes-only, best-effort tee.** The tee MUST be a pipes-only `MultiWriter` splice (add
  a pipes-in constructor to `apptap`; do NOT force `Tap.Run`'s exec+Wait ownership onto a driver that
  owns its own process). Per RS-016 the tee owns no file format and opens no file. Capture MUST be
  best-effort / degrade-to-uncaptured — an explicit reversal of apptap's current fail-closed
  MultiWriter semantics — so a capture-disk problem MUST NOT abort a live agent run (see AIS-INV-002).
- **AIS-014 — Persistence, redaction, corpus layout, retention.** Persistence + redaction live in the
  CONSUMER, not the tee. Corpus lands at `${workspace_path}/.harmonik/sessions/${session_id}/` (WM
  §4.7), one file per direction, with a **mechanical** `CAPTURE-LOG` ledger (keeper's `EXTRACT-LOG.md`
  is the executed model; codex's declared-but-never-written `CAPTURE-LOG.md` is the anti-pattern).
  Redaction: verbatim in-memory tee + an HC-032-style value-pattern scrub in the persisting writer
  (OUTPUT direction is the secret risk; INPUT is structurally secret-free per HC-028). Retention MUST
  follow a keep-N / age-prune rule (brhistoryrotate / orphansweep precedents) — the spec MUST NOT
  inherit the unrotated-89.5 MB `events.jsonl` defect.

**4.7 Composition-root selection & remote seam** *(C2 · driver Q3 · seam-contract risk #7)*

- **AIS-015 — Substrate-selection axis.** No substrate-selection mechanism exists today (two roots
  hardcode tmux; only `DefaultHarness` selects argv shape, orthogonal to hosting). The spec MUST
  define a selection axis — a new composition-root config/flag OR keyed off the harness — that also
  works in the L2/L3 test harnesses. The structured driver MUST remain **twin-blind**: a
  `harmonik-twin-claude` speaking the same NDJSON wire on stdio is the L2/L3 integration double
  (twin-binaries locked decision), corpus-replay `Twin[E]` covers the unit tier.
- **AIS-016 — Remote seam preserved.** Every retired side-interface has a `…Via(runner)` remote-SSH
  twin. The new input driver MUST carry the `CommandRunner`/remote seam so remote workers (M4) do not
  regress, even though M4 owns the transport rebuild. Keep the seam remote-capable.

**4.8 WAL-guard fold-in** *(C7 · D7)*

- **AIS-017 — Adapt, don't delete.** The 380-line `codexwalguard.go` compensates for ungraceful
  SIGKILL of a stateful codex child (leftover `state_*.sqlite-wal` corrupts the next launch), NOT for
  lost input — a real input ack does not by itself make it redundant. The structured driver reduces
  the need via (a) graceful turn-termination (`turn/interrupt`) instead of SIGKILL and (b) positive
  fast-fail (no handshake within bound → structured launch-failure event instead of the 8s silent
  exit-0). Retain a boot-time recovery step for residual SIGKILL/crash paths; remove per-launch
  symptom-treatment where graceful shutdown covers it. C7 is codex-specific and claude-irrelevant —
  its post-M2 home is codex process lifecycle, NOT the claude driver; off critical path (rides C2/C5).

### §5 Invariants

- **AIS-INV-001 — Bounded liveness: output-or-stale, never silence.** *(D3 — M2's SR9 analog.)* For
  every `SubmitInput`, the call MUST reach exactly one terminal — an `Ack{Accepted|Rejected|Degraded}`
  OR an emitted `agent_input_stale`-class event — within a bounded window `InputAckTimeout + injection
  overhead`. Silence is FORBIDDEN. Structurally, every `TimerFired` edge in the driver's `Step` MUST
  land in a state with an outgoing action, so the machine cannot wedge silently. The window MUST be
  measured via `ClockPort` (never wall-clock). Phrased in M2 vocabulary, NOT by citing STEP-0a (which
  lives only in plans, not specs). M3-5 defines the daemon-run peer.
- **AIS-INV-002 — Capture never aborts the run.** *(D8.)* A capture-writer error (full disk, slow
  sink, redaction-scrub failure) MUST degrade the capture to uncaptured and MUST NOT back-pressure or
  abort the live agent input/output stream. The live stream's liveness is independent of the
  side-channel.

### §6 Schemas & data shapes

Pinned Go surface (restated verbatim in the spec): `InputPort` (2 methods), `InputRequest`
(payload + turn intent), `Ack{Class, Seq, Token}`, `AckClass` enum, the flat `Event`/`Action`
structs (omitempty), and the `claudewire` method-registry entry shape. Types are stdlib + the
vertical's `E`/`A` parameters.

### §7 Protocols & state machines

Two machines: (1) the **wire protocol** — the claude headless stream-json handshake/turn framing
(initialize → turn-start → `*/delta` stream → turn-completed; steer/interrupt), pinned by the T0
corpus (AIS-007), frozen only after the spike. (2) the **driver `Step` state machine** — the pure
`func(Event) []Action` with the bounded-liveness property: enumerate states, the `TimerFired`
edges, and show every timer edge lands in an emitting state (AIS-INV-001).

### §8 Error taxonomy

Enumerate the acceptance/failure classes and their emitted events: `Accepted` (with token),
`Rejected` (protocol-level refusal), `Degraded` (written, unconfirmed — interim impl), and the
timeout terminal `agent_input_stale`. Plus launch-failure (no handshake within bound), disconnect
(child stdout closed mid-turn), and codec `ErrorEvent`/`DisconnectEvent` (supplied by the codec,
NOT by extending the four substrate fault modes — RS-012 is closed).

### §10 Conformance

Baseline-anchored (RS/SK style): the C5 harness (sibling `harness-acceptance-design.md`) is the
oracle — AIS-INV-001's output-or-stale is proven by a FakeClock virtual-time fault-injection test
across the four fault modes + an N=10 consecutive-clean-cycles gate + an out-of-band `jq`/`grep`
oracle (never self-grading). SC6's "zero sleeps / zero capture-pane scraping" is a forbidigo +
depguard + grep DoD gate. These are DoD, not new prose beyond AIS-INV-001's conformance obligation.

### §11 Open questions

Carry the two D4 PLANNER-RECONCILEs (input-side unproven; billing), the D5 inspectability operator
confirmation, and the D6 migrate-vs-carve-out planner call.

## Rationale

**Why.** `exit 0 ≠ TUI accepted` (`pasteinject.go:131`); the only positive acceptance signals today
are indirect and async (heartbeat, commit) behind ~600 lines of fused kill-ladder — a boundary that
structurally cannot confirm acceptance, carrying 44 tmux incident beads across 4 workaround
generations. AIS-001/AIS-003/AIS-004 give the seam a first-class ack (SC1/SC2); AIS-INV-001 gives it
the bounded-liveness guard that lets C6 delete the escape hatch without re-importing the resume-hang
(the exact census risk, PLAN.md:205-208).

**Which 02-components requirements it satisfies.** C1 (AIS-001/002/003/004/005 + AIS-INV-001); C2
(AIS-006/007/008/009/010/015/016); C3 boundary (AIS-011/012); C4 (AIS-013/014 + AIS-INV-002); C7
(AIS-017). C5/C6 are DoD/teardown owned by siblings; this spec supplies only their conformance
obligation and boundary/carve-out statements.

**How research informed it.** seam-contract findings supplied the exact retirement list + the proven
keeper `ports.go` port+fn-adapter migration idiom (AIS-001), the doc-comment-only depguard gap
(AIS-002), the SK-015 bounded-liveness shape (AIS-INV-001), and the StdinDevNull/remote-seam risks
(AIS-010/016). driver findings supplied the codex quartet template + type-alias/flat-struct/method-
registry idioms (AIS-006), the corpus-first T0 discipline and the two unproven-input/billing gates
(AIS-007/008), the direct-child-stdio precedents (AIS-009), and the missing selection axis
(AIS-015). capture-tee findings supplied the pipes-only + best-effort reversal (AIS-013), the
persistence/redaction/ledger/retention shape and the events.jsonl-89.5MB anti-pattern (AIS-014).

## Requirements traceability

**Success criteria → AIS.**

| SC (01) | AIS requirement(s) |
|---|---|
| SC1 real input method + ack | AIS-001, AIS-003, AIS-004 |
| SC2 side-interfaces retired | AIS-001, AIS-002 |
| SC3 tmux observation-only | AIS-011, AIS-012 |
| SC4 input stack deleted | C6 teardown (sibling DoD); boundary/carve-out recorded by AIS-011/AIS-012 |
| SC5 live capture tee | AIS-013, AIS-014 |
| SC6 L0–L3 + fault harness | AIS-INV-001 (conformance obligation); harness DoD = sibling |
| SC7 abort/rollback + bake window | C6/C5 gate (sibling); escape-hatch-survives boundary recorded by AIS-012 |

**Decisions → AIS.**

| Decision | Lands in this spec as |
|---|---|
| D0 contract-ownership (M2 owns) | AIS-004 (dual sync+event, direction-agnostic) + headline PLANNER-RECONCILE |
| D1 input port placement | AIS-001, AIS-002 |
| D2 ack contents + dual signal | AIS-003, AIS-004, AIS-005 |
| D3 bounded liveness | AIS-INV-001 |
| D4 wire protocol corpus-first | AIS-006, AIS-007 (reconcile-1), AIS-008 (reconcile-2) |
| D5 process ownership + inspectability | AIS-009, AIS-010, AIS-011 (reconcile) |
| D6 keeper carve-out | AIS-012 (recorded; SK reconciliation = sibling; reconcile inline) |
| D7 WAL-guard adapt | AIS-017 |
| D8 capture tee shape | AIS-013, AIS-014, AIS-INV-002 |
| D9 harness taxonomy | §10 conformance obligation only (DoD = sibling) |
| D10 new spec naming/prefix/template | front-matter + AIS-000 disambiguation + §meta (reconcile) |
