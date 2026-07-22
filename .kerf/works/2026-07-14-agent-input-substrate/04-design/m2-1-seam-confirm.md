# M2-1 design-pass confirmation — seam input method + Ack contract

Design-pass agent, 2026-07-14. Scope: confirm M2-1's four design-pass questions
are settled by the AIS spec draft (ready+square per COORD c005) + the sibling
handler-contract draft; reconcile M2-1's stale `pending-design` status; and check
the reported event-model §8.21 `class`-field drift.

Sources read: `plans/2026-07-13-code-revamp/TASKS.md` (M2 block, rows M2-1..M2-7);
`.kerf/works/2026-07-14-agent-input-substrate/05-spec-drafts/{agent-input.md,event-model.md,handler-contract.md}`;
`internal/handler/substrate.go`; `internal/substrate/seam.go`.

---

## 1. M2-1 design-pass resolution — SETTLED

M2-1's acceptance names four things the design pass must resolve:
*port placement, Ack contents, sync-return-vs-awaited-event, the M2 bounded-liveness
analog of SR9.* All four are resolved concretely in the AIS spec and mirrored at
seam altitude in handler-contract (HC-069/070/071 + HC-INV-008, status `reviewed`).

### (a) Port placement — RESOLVED (AIS-001, HC-069)
- A new **consumer-owned narrow `InputPort`** declared **handler-side**, following
  the RS-004 port idiom: exactly two methods, no Result/Option/Either container.
  - `SubmitInput(ctx, InputRequest) (Ack, error)` — blocks until ack-or-stale.
  - `CloseInput(ctx) error` — replaces/retires the `substrateSessionAdapter.CloseStdin`
    no-op (`internal/handler/substrate.go:173`).
  Ref: `agent-input.md` §4.1 AIS-001 (lines 85-96) + §6.1 (lines 278-289).
- **Depguard inversion preserved exactly as the acceptance demands.** The handler
  package *declares* the port (`internal/handler`); the daemon composition root
  *supplies* the driver — the same inversion already documented at
  `internal/handler/substrate.go:5-8`. `SubstrateSession` is widened so a session
  MAY satisfy `InputPort`; the daemon obtains it by **structural assertion** at the
  process-spawn seam. The single-method `Substrate.SpawnWindow` stays intact.
- **The boundary becomes a machine rule (AIS-002 / HC-071):** a real depguard deny
  — `internal/handler` MUST NOT import `internal/lifecycle/tmux` — lands *with* the
  port, replacing today's doc-comment-only boundary (`substrate.go:5-8` prose).
- The six type-asserted side-interfaces (`enterSender`, `paneCapturer`,
  `quitSender`, `paneOutputSizer`, `paneLivenessChecker`, `commandRunnerProvider` —
  `pasteinject.go:187/206/236/254/280/493`) and the no-op
  `substrateSessionAdapter.SendInput`/`CloseStdin` (`substrate.go:140`,`:173`) are
  retired as the input mechanism.
- Placement call that was open ("verb on `SubstrateSession` directly vs separate
  narrow port") is RESOLVED to the **separate narrow port**, separately asserted —
  not a `Session` method (AIS-001 PLANNER-RECONCILE note, line 96; HC-069 changelog).

### (b) Ack contents — RESOLVED (AIS-003, HC-070, §6.2)
`Ack` carries exactly three fields (`agent-input.md` §6.2 lines 305-309):
```go
type Ack struct {
    Outcome DeliveryOutcome // {Delivered, Rejected} — binary; NO acceptance "class"/tier
    Seq     uint64          // driver-internal monotonic input seq, codec-owned (RS-008)
    Token   string          // optional protocol acceptance token (turn id), when supplied
}
```
- **Delivery outcome is binary**: `Delivered` (input handed to the driver;
  acceptance verdict arrives async) or `Rejected` (protocol-level refusal —
  structured drivers only; tmux cannot produce one). The two input methods
  (tmux-paste for Claude; the structured Codex driver) are **PEERS, not tiers** —
  there is no acceptance "class" and no capability hierarchy.
- **Positive acceptance is NOT an `Ack` value.** It is the async
  `agent_input_acked` event — its existence IS the ack. Sourced per driver:
  tmux/Claude path ← Claude-hook-bridge (`outcome_emitted` on `Stop` /
  `agent_ready` on `SessionStart`), never a `capture-pane` scrape; structured
  driver ← wire input-ack (AIS-003 lines 114-119).

### (c) Sync return vs awaited reactor event — RESOLVED as DUAL (AIS-004, HC-070)
Not either/or. `SubmitInput` **returns the `Ack` synchronously** (the
delivery-handoff verdict: Delivered/Rejected) **AND** the driver **emits a durable
event** on the stream — `agent_input_acked` on positive acceptance, `agent_input_stale`
on the timeout terminal. The dual delivery lets an async observer (the M3 runexec
reactor) see the ack on the event stream without compile-coupling this subsystem to
any consumer's `State` type. The sync `Ack` is a **front-stop** placed *in front of*
the existing async watchdogs (`agent_heartbeat` HC-057, commit-poll), not a
replacement (AIS-005). Ref: `agent-input.md` §4.2 lines 124-137; §6.2 line 312.

### (d) M2 bounded-liveness analog of SR9 (ack-or-stale, never silence) — RESOLVED (AIS-INV-001, HC-INV-008)
Every `SubmitInput` MUST reach **exactly one terminal** — an `Ack`
(Delivered/Rejected) OR an emitted `agent_input_stale`-class event — within a
bounded window (`InputAckTimeout` + injection overhead), measured via `ClockPort`
(never wall-clock). Silence is FORBIDDEN; structurally, every `TimerFired` edge in
the driver's `Step` MUST land in a state with an outgoing action. On the tmux/Claude
path the awaited positive signal is the Claude-hook-bridge event; a missing signal
converts to a recoverable `agent_input_stale` — **this is the resume-hang fix**.
This is the M2 peer of session-keeper SK-INV-005 (SR9), phrased in this subsystem's
vocabulary. Ref: `agent-input.md` §5 AIS-INV-001 (lines 256-263); §7.2 Step table
(lines 373-382); handler-contract §5 HC-INV-008 (line 1100).

### Verdict
**M2-1 design is settled — flip `pending-design` → `ready`.** All four
design-pass questions the M2-1 acceptance names have concrete, cited resolutions in
a spec that is ready+square (COORD c005) and mirrored by the `reviewed`
handler-contract HC-069/070/071 + HC-INV-008 family. The contract type-checks
before any driver exists (the interim tmux/paste impl satisfies `InputPort` by
returning `Delivered`), and the depguard inversion is preserved and hardened into a
machine rule.

**One residual to affirm (not a blocker):** AIS-004's PLANNER-RECONCILE headline
(lines 130-131) — contract-ownership direction. The brief once said M3 owns/defines
the Ack contract first; the repo task graph (five concordant TASKS citations) says
**M2 owns** it and M3/M4 are consumers. The draft already picks **M2-owns**; the
dual-shape survives either reconciliation. Planner should affirm M2-owns so M3/M4
implement against a fixed direction. This does not block M2-1 going `ready`.
(OQ-AIS-004 keeper migrate-vs-carve-out blocks only M2-6/C6 deletion; OQ-AIS-005
filename/prefix blocks only the `specs/_registry.yaml` reservation at finalize —
neither touches the M2-1 seam/ack contract.)

---

## 2. Loose end A — M2-1 status reconcile: STALE, flip to `ready`

M2-1 is currently `pending-design` while its own dependents in the same M2 block are
already `ready`: **M2-3** (C3, hook-sourced ack), **M2-3b** (C3b, restart done-gate),
**M2-6** (C6, retire flaky heuristics) all read `ready` and all list M2-1 in their
`Depends on`. A foundational seam-contract task cannot coherently be `pending-design`
while three tasks that build on it are `ready`. M2-1's only gate is "P1 seam proven,"
which the ready siblings already clear (they share that gate). The AIS spec settles
every M2-1 design-pass question and is ready+square.

**Recommendation: flip M2-1 `pending-design` → `ready`.** Its `pending-design`
status is stale relative to the AIS ready+square state and to its own ready
dependents. (Cosmetic note: the spec file's own `status:` front-matter still reads
`draft` — that is the spec-lifecycle field, distinct from the kerf pass state
ready+square per COORD c005; not a design residual.)

---

## 3. Loose end B — event-model §8.21 `class`-field drift: NO DRIFT FOUND (already reconciled)

The reported contradiction — the `AgentInputAckedPayload` struct still carrying a
`class` field while prose says "no acceptance class" — **does not exist in the
current draft.** The Accepted/Degraded terminology purge is fully complete here.

Evidence (all in `event-model.md`):
- **§8.21.1 taxonomy row** (line 513): payload columns are
  `run_id`, `input_seq`, `acceptance_token?`, `session_id?`, `acked_at` — **no `class`.**
- **§6.3 YAML for `agent_input_acked`** (lines 1543-1549): fields are
  `run_id`, `input_seq`, `acceptance_token`, `session_id`, `acked_at` — **no `class`.**
- **§6.3 prose** (line 1551): "The event carries no acceptance 'class': its existence
  IS the positive ack (COORD c019 — no capability hierarchy)."
- A scan for a literal `class`/`Class`/`acceptance_class`/`AcceptanceClass` field, or
  any `Accepted`/`Degraded` acceptance value, across the entire agent-input span
  (lines 505-1565) returns **zero** field-level hits (only "durability class" /
  "consumer class" prose, and the "no acceptance class" statements).
- Cross-check: the sibling handler-contract §6.1 record (line 1157) already reads
  `outcome : Enum {Delivered, Rejected}` + `input_seq`, explicitly "No acceptance
  'class'/tier." The Go struct `core.AgentInputAckedPayload` does not yet exist in
  the repo (unimplemented), so there is no code-side drift either.

**No fix required.** The binary Delivered/Rejected + async acked/stale model is
already consistently expressed in event-model §8.21 / §6.3, agent-input §6.2, and
handler-contract §6.1. If the planner's snapshot showed a `class` field, it predates
the c019/binary-model reconciliation already landed in this draft.

**Before/after: NONE.** (Nothing to change — text already matches the target.)
