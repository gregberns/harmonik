# Change Design — A3: `specs/operator-nfr.md`

**Area:** A3 — agent-callable pause/resume command verb + `operator_pause_status` producer
**Maps to:** G3 (S9). A3 is the **upstream** piece; A1/A2/A4 reference its output.
**Authoring order:** first (per 02-components.md §Dependency Map).
**Research:** `03-research/operator-nfr/findings.md`.

---

## 0. One-line summary

Wire a pre-existing-but-unwired pause/resume **command verb** to the already-fully-specced operator-control state machine, add the explicit **agent-callable** obligation (Pi may issue it without a human), and confirm the verb's emission of `operator_pause_status` is the single production producer feeding the queue consumer (QM-054) and the br-ready fallback gate (A1).

## 1. Current state — what the spec says now

The hard machinery already exists and is normative:

- **State machine (ON-011, §7.1).** States `running / pausing / paused / resuming / stopped / upgrading`; the `running → pausing → paused → resuming → running` chain is the §7.1 transition table (`operator-nfr.md:774-788`).
- **Between-task drain gate (ON-008, `:209`).** A `pause` issued while `ready` MUST NOT interrupt in-flight runs; transitions `running → pausing`, runs all eight ON-027 drain steps, then `pausing → paused` gated on drain-completion.
- **Producer emission obligations (ON-013, `:254-260`).** One `operator_pause_status{status=pausing}` on `running→pausing`; one `{status=paused}` (with `drain_summary`) on `pausing→paused`; `operator_resuming` on `paused→resuming`. Paired-phase, re-emit-once.
- **Discriminator (ON-012, `:250`).** `pause_reason ∈ {operator, improvement}` distinguishes the two pause origins on the **same** state table. Separate improvement-pause states are retired.
- **Transport grouping (ON-013a, `:275`).** The goroutine handling `pause`, `stop`, `upgrade`, `attach`, and the `queue-submit`/`queue-append`/`queue-status`/`queue-dry-run` JSON-RPC methods (per process-lifecycle.md §4.1 PL-003a) MUST install a panic barrier — i.e. `pause` is already framed as a PL-003a Unix-socket RPC method co-located with the queue verbs.
- **Idempotency (ON-013c, `:281`).** No-op transitions return exit 0; `pause` while `paused` re-emits `{status=paused}` at most once; `resume` while `running` is a no-op.
- **Durable marker (ON-030a, `:479`).** `.harmonik/daemon.state` persists the operator-control state + `pause_reason` across crashes; PL-005 step 0 re-initializes into it.
- **Drain ownership (ON-027, `:441`; §7.2 pseudocode `:792`).** Eight strictly-sequential drain steps; step 1 transitions the queue to `paused-by-drain`.

**What is NOT in the spec / NOT wired:**

1. **No agent-callable obligation.** ON-007..013 frame pause as an *operator* command. Nothing states a non-human agent (Pi) MAY issue it. This is the actual S9 gap.
2. **No named command-verb requirement.** The `CommandPause` constant exists in code (`internal/operatornfr/commandcodes.go:34`, comment cites ON-007..010) but **is reserved-but-unwired**; `cmd/harmonik/supervise_cmd.go:42-62` exposes only `start/stop/status/attach/restart/logs/_shim` — no `pause`/`resume`. The spec never states which CLI verb form is canonical, so the code constant (`harmonik pause`, top-level) and CL-080 (`harmonik supervise pause/resume`) disagree.
3. **No statement that the verb's emission is *the* production producer.** The consumer side (QM-054) subscribes to an `operator_pause_status` that **nothing emits in production** today.

## 2. Target state — what the spec should say after the change

A3 adds a small cluster of new ON requirement IDs in §4.3, plus annotations. **No new event type, no new state, no change to ON-027 / ON-011 / §7.1 transition rows** (constraint C4/N3 — the producer triggers existing transitions, it does not alter them).

### T1 — New requirement: agent-callable pause/resume command verb (`ON-014a`)

A new requirement (proposed ID **ON-014a**, slotting after ON-013d, before the existing ON-014 reconciliation-override — or the next free ON-01x; final ID assigned at spec-draft) stating:

- The daemon MUST expose a **`pause` and a `resume` operator command** reachable over the PL-003a Unix-socket JSON-RPC transport, co-located with the queue methods per ON-013a.
- The canonical operator-facing CLI form is **`harmonik supervise pause`** and **`harmonik supervise resume`** (resolves OQ1 — see §4 Decisions). The RPC method/`CommandName` wire values remain `pause` / `resume` (the existing `commandcodes.go` constants).
- **Agent-callable (the S9 closure):** an autonomous agent (the cognition loop / Pi, per cognition-loop.md §4.10 CL-080) MAY issue `pause`/`resume` **without human intervention**, by shelling the CLI which frames the PL-003a RPC, exactly as a human operator would. There is no human-only gate on these verbs. The agent-callable path and the human path are the **same** command surface.
- Issuing `pause` drives the **existing** `running → pausing` transition (§7.1) with `pause_reason=operator`; issuing `resume` drives `paused → resuming → running`. The drain (ON-027), the gate (ON-008), the emission (ON-013), the durable marker (ON-030a), and idempotency (ON-013c) are all **inherited unchanged**.

### T2 — New requirement: the command verb is the production producer of `operator_pause_status` (`ON-014b`)

A new requirement (proposed ID **ON-014b**) stating:

- The pause/resume command verb of T1 is the **production producer** of `operator_pause_status{status, pause_reason=operator}` / `operator_resuming`. It emits the **existing** ON-013 events through the **existing** §7.1 transitions; it introduces no new event type (C4).
- The emitted `operator_pause_status` is the **single source of pause truth** consumed by BOTH (a) the queue-model consumer that transitions `active → paused-by-drain` (queue-model.md §8.5 QM-054), AND (b) the execution-model br-ready fallback dispatch gate (A1 / execution-model §7.4). Both read sides observe the same event; A3 owns the producer, A1 and A4 are read-side consumers.
- `pause_reason=operator` tagging happens at the emission site per ON-013 `:258` ("the emission site is responsible for tagging the pause reason"). The agent-callable origin is structurally identical to the improvement-pause path (`pause_reason=improvement`); the shared state table (ON-012) handles coexistence by construction.

### T3 — Reconcile the verb name in `commandcodes.go` (documentation alignment, tasked, not spec text)

The `CommandPause` comment (`commandcodes.go:31-34`) says "`harmonik pause`" (top-level). T1 makes the canonical CLI form `harmonik supervise pause`. The spec records the canonical form; the *code-comment correction* (reconcile `commandcodes.go` to `harmonik supervise pause`, keep `pause` as the wire `CommandName`) is a **07-tasks item** tied to hk-5bw7a, NOT spec text. Same for the `budget.ts:8` / `circuit-breaker.ts:5` comments that reference `harmonik supervise resume` — these become **correct** once T1 wires the verb; they are dangling-but-correct-in-intent today.

### T4 — Conformance scenario (SC3), §10-style prose obligation

Attach to ON-014a a prose conformance obligation (house style — §4/§10 prose keyed to the requirement ID):

> **Agent-callable pause/resume (ON-014a/ON-014b).** With ≥1 run in-flight, an agent issues `harmonik supervise pause` over PL-003a: verify (a) `operator_pause_status{status=pausing, pause_reason=operator}` is emitted; (b) NO new `run_started` is emitted while the daemon is `pausing`/`paused`; (c) every in-flight run reaches a terminal state (no abort); (d) `operator_pause_status{status=paused}` with `drain_summary` is emitted only after all ON-027 steps complete; (e) the queue transitions `active → paused-by-drain` (QM-054); (f) on `harmonik supervise resume`, `operator_resuming` is emitted, the daemon returns to `running`, and dispatch resumes. No human action is required at any step.

## 3. Rationale

- **Wire, don't invent (research F1, F4, F5).** The state machine, events, drain, discriminator, durable marker, and idempotency are all already normative. The *only* genuine gaps are the agent-callable obligation (F5) and the named command verb (F2). Keeping the change to two new requirements + annotations honors the project's spec-first / no-new-abstraction directives and constraint C4 ("add a producer, not new consumer semantics").
- **Single source of pause truth (research F5, problem-space C4).** A1's br-ready gate and A4's queue consumer must observe the *same* pause signal, or the daemon could be "paused" for the queue path but still dispatch on the fallback path — re-creating a partial-pause hole. T2 makes the producer canonical and names both read sides.
- **`supervise pause/resume` canonical (research F2/F4, OQ1).** CL-080 (`cognition-loop.md:163`) and CL-090 (`:169`) already say `harmonik supervise pause/resume`; PL §4.9 + ON §4.3 group daemon-lifecycle verbs under `supervise`. Choosing the top-level `harmonik pause` would create a parallel surface (forbidden by C4/N3). The bare `pause`/`resume` survive only as the RPC `CommandName` wire values.
- **RPC not signal (research F3, OQ2).** ON-013a already groups `pause` with the JSON-RPC queue methods. Signals can't carry the `pause_reason` discriminator (ON-012) and don't fit the structured agent-callable request shape. PL-003a Unix-socket RPC is the existing, consistent transport.

## 4. Decisions recorded (resolving the problem-space open questions)

- **OQ1 (verb name) → `harmonik supervise pause/resume` canonical.** Bare `pause`/`resume` retained as RPC `CommandName` wire values. (research F2/F4.)
- **OQ2 (transport) → CLI → daemon PL-003a Unix-socket RPC.** Signal alternative rejected. (research F3.)

## 5. Requirements traceability

| Goal / SC (problem-space) | Target state | New/changed requirement |
|---|---|---|
| G3 / S9 — agent-callable pause | T1 (command verb + agent-callable obligation) | **ON-014a (new)** |
| G3 / S9 — real producer | T2 (verb is the production `operator_pause_status` producer; single source of truth) | **ON-014b (new)** |
| G3 / SC3 — pause→no-dispatch→resume scenario | T4 | conformance obligation on ON-014a/b |
| G5 — stale-doc fix | T3 | 07-tasks (hk-5bw7a), not spec text |

Every requirement in 02-components.md §A3 is addressed:
- "command verb reachable by an agent, transport named" → T1.
- "emits the existing `operator_pause_status`, re-emit-once preserved, no new event type" → T2.
- "single source of pause truth for the queue consumer AND the br-ready gate" → T2.
No target state lacks a driver: T1↔G3 verb, T2↔G3 producer, T3↔G5, T4↔SC3.

## 6. Constraints honored

- **C4 (no consumer-semantics regress, no new event/state).** T1/T2 add a verb + producer; they reuse `operator_pause_status` / `operator_resuming`, the §7.1 transitions, ON-027 drain, ON-013c idempotency, ON-030a marker — all unchanged. Explicitly stated "emit into ON-027, do not alter ON-027."
- **N3 (no run-state-ownership change).** The producer triggers a transition; it does not walk events.jsonl, take reconciliation locks, or touch queue.json directly. The queue transition is the consumer's job (QM-054).
- **R2 (improvement-pause coexistence).** Handled by ON-012's shared state table + `pause_reason` tag at the emission site; T2 notes the tagging site.
- **R3 (drain_summary / EV §8.7.6).** ON-013 already requests EV extend §8.7.6 with `drain_summary?`. T2/T4 rely on that optional field; if EV has not landed it, the `status=paused` emission degrades gracefully (field omitted). Flagged for Integration pass cross-spec coordination, not a blocker.

## 7. Out of scope (deferred to siblings / non-goals)

- Budget/spend pause wiring (`budget_exhausted` → pause) is **credfence**'s contract (N1); A3 only provides the verb the budget path could drive.
- The `harmonik supervise pause/resume` CLI *flag surface* details (exit codes beyond the existing §8 code 16, help text) defer to a PL §4.1 / ON §4.3 cross-reference; A3 names the verb + agent-callable obligation, not the full flag-parsing surface.
