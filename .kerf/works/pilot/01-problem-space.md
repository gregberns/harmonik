# Pilot — Problem Space

**Work:** `pilot` (jig: spec) — Pi-driven dispatch & control plane
**Primary source:** `docs/flywheel/2026-05-30-lifecycle-feasibility-and-gaps.md`
**Date:** 2026-05-31

## 1. Summary — what is changing and why

The 2026-05-30 credit-burn incident exposed that the flywheel lifecycle the user
actually wants is **not yet wired end-to-end**. The S1–S10 feasibility table in
the assessment shows three steps **outright missing** — S2 (a quiet daemon that
does nothing on its own), S5 (Pi-driven dispatch), S9 (an agent-callable pause) —
and these three absences are exactly what produced the incident. The daemon, with
no idle mode, fell back to `br.Ready()` and auto-drained ready work into a fresh
wave on every one of 10 boots that day; Pi had no way to dispatch the *curated*
selection it was supposed to drive; and Pi had no way to *stop* dispatch once it
detected the runaway.

This work specifies and wires the **Pi-driven dispatch & control plane** so the
desired 10-step lifecycle becomes possible:

1. The daemon is **quiet by default** — a queue-only / no-auto-pull topology where
   a bare daemon boot dispatches **nothing** until something is explicitly submitted
   to its queue (closes S2; the `br ready` fallback becomes opt-in).
2. **Pi curates and dispatches** — the flywheel harness reads `kerf next`,
   dependency-orders the candidates, and submits/appends them to a **stream** queue
   via `harmonik queue submit` / `harmonik queue append`, implementing CL-070..073
   (closes S5/S6/S10).
3. **Pi can pause and resume** — an agent-callable `harmonik pause` / `resume`
   surface drives a real `operator_pause_status` producer so that "Pi detects an
   issue → no new jobs dispatched, in-flight runs drain" is reachable in production,
   and the `br-ready` fallback path honors the same pause state (closes S9).
4. **Two-phase done verification** — the harness implements CL-051 (require BOTH a
   `run_completed{success}` event AND a `git log origin/main --grep "Refs: hk-…"`
   trailer) before it marks a bead DONE, so a push-failed run can no longer look
   complete (closes the S8 verification half).

This work is the **lifecycle build-out**. The sibling work `credfence` (credential
& spend safety) is the safety prerequisite and is **out of scope here**; `reap`
(crash-recovery & orphan remediation) hardens failure modes and is also out of
scope. `pilot` assumes those land alongside but does not depend on their internals.

## 2. Goals — what should be true about the system after this change

- **G1 (S2 — quiet daemon).** `specs/execution-model.md` defines a queue-only /
  no-auto-pull daemon topology in which a freshly-booted daemon with no submitted
  queue dispatches **zero** runs and consumes **zero** credit. The `br.Ready()`
  fallback (`internal/daemon/workloop.go:833-858`) becomes **opt-in**, off by default
  for the flywheel topology. The spec already says the daemon "MUST NOT fall back to
  br ready" (`execution-model.md:1395,1647`) — the code and the default contradict it;
  this work reconciles them and names the operator flag (`--no-auto-pull` /
  `--queue-only`) that hk-exd7m tracks.

- **G2 (S5/S6/S10 — Pi-driven curated dispatch).** `specs/cognition-loop.md`
  CL-070..073 are realized by an actual harness path: on slot release the harness
  reads `kerf next --format=json --only=bead`, applies the CL-072 pre-screen guards,
  dependency-orders survivors, and **submits/appends** them to a stream queue via
  `harmonik queue submit` / `harmonik queue append`. The spec defines this as the
  normative Pi drive surface; the `.pi/extensions/flywheel/` harness implements it
  (today it shells out only to read-only `subscribe` + `digest`).

- **G3 (S9 — agent-callable pause/resume with a real producer).** The specs define
  an agent-callable pause/resume control surface (`harmonik pause` / `resume`, or the
  `supervise pause/resume` form named by CL-080) that drives a production producer of
  `operator_pause_status{pausing|paused}` / `operator_resuming`. The state machine
  and the queue-side consumer already exist (`internal/operatornfr/`,
  `queue_operatoreventconsumer_7urls.go`, QM-054); this work closes the loop by
  defining the **CLI/RPC verb + producer wiring** and gating the `br-ready` fallback
  path on the **same** pause state (today only handler-pause gates it).

- **G4 (S8 — two-phase-done verification).** The specs define, and the harness
  implements, CL-051's two-phase gate: a bead is DONE only when BOTH a
  `run_completed{success}` event is observed AND `git log origin/main --grep
  "Refs: hk-…"` is non-empty. Trailer-without-event routes to Tier-2 reconciliation
  (`loop_observed_phantom_done`); event-without-trailer is treated as in-flight and
  re-polled. `bridge.ts` no longer marks deterministic completions done without the
  git-trailer check.

- **G5 (coherent end-to-end contract).** The four pieces above are specified as **one
  coherent control-plane contract** across `execution-model.md`, `cognition-loop.md`,
  and `queue-model.md`, with the stale-doc corrections (S7 `harmonik subscribe` IS
  implemented; `supervise` has no pause verb today) reconciled so the next reader is
  not misdirected.

## 3. Non-goals — explicitly out of scope

- **N1 — Credential isolation & spend governance.** Key-scrub at spawn boundaries,
  per-day-USD / max-runs daemon-side ceilings, the finite-budget default, `.env`
  gitignore. These are `credfence`'s contract. `pilot` may *reference* the pause
  hook that a `budget_exhausted` event drives, but does not specify the budget meter
  itself.

- **N2 — Crash recovery & orphan remediation.** Remediating orphan sweep, reap-on-exit,
  restart backoff, single-flywheel-per-project lock, reconciling `dispatched` items on
  boot. These are `reap`'s contract.

- **N3 — Changing the daemon's run-state ownership model.** The loop remains a *second
  consumer* (CL-050): it MUST NOT take reconciliation locks, walk `events.jsonl` to
  reconstruct run state, issue `Refs:` trailer commits, or touch `queue.json` /
  `beads-intents/` directly. This work does not move any of those responsibilities.

- **N4 — New queue primitives or a new dispatch verb.** The wave/stream model and the
  `queue submit | append | status | dry-run` surface already exist
  (`specs/queue-model.md`). `queue submit` already *is* "start" (returns
  `status: active`, dispatches immediately). This work does **not** add a separate
  "start" verb (S6 is satisfied by submit); it wires Pi to *call* the existing surface.

- **N5 — DOT/general-workflow dispatch shape.** This work concerns *which beads get
  dispatched and when*, not the per-run node graph. `workflow_mode` selection is
  unchanged.

- **N6 — The Pi model-tier / judgment-model knob.** Operator override of the Pi
  judgment model (opus → sonnet) is a separate cost-lever bead, not part of the
  dispatch/control contract.

## 4. Constraints

- **C1 — Mechanism/cognition separation (CL-013, CL-INV-001).** Curated dispatch is
  cross-cutting: `kerf next` ranking, dependency-ordering, and submit are **mechanism**
  (pure-code eager refill, MUST NOT consult the model per CL-071); waking the model at
  the empty-queue boundary is **cognition** (CL-073). The spec must keep the line
  byte-clean. "Harness picks the bead and tells the model" cross-tagged structures are
  FORBIDDEN.

- **C2 — Loop is a second consumer (CL-050).** All new harness reactions must be
  idempotent with their own durable watermark (CL-052..056). The two-phase-done check
  and the eager-refill submit must each be keyed for effectively-once across crashes
  (`dispatch_intent:<event_id>:<bead_id>`).

- **C3 — Backward compatibility of the `br-ready` path.** The fallback poll path is the
  historical default and must keep working for non-flywheel daemons. Making it opt-out
  must be a *topology default flip*, not a removal — existing single-daemon users who
  rely on auto-pull keep it via the flag.

- **C4 — Existing queue-side pause semantics are correct and must not regress.** The
  `operator_pause_status` consumer (`queue_operatoreventconsumer_7urls.go:118-150`,
  QM-054) correctly transitions active→paused-by-drain and idle-waits for in-flight
  runs. This work adds a **producer**, not new consumer semantics. No auto-resume across
  daemon restart (QM, `queue-model.md:271`).

- **C5 — `queue submit --beads` default group kind.** `harmonik run --beads` defaults to
  a **wave** group, which does **not** accept appends. Pi-driven incremental curation
  requires a **stream** queue. The spec must make Pi's dispatch path use stream groups
  (or explicitly build the queue JSON) so CL-071 appends are accepted.

- **C6 — Wake-on-submit guarantee already holds (EM-NOTE-WAKE, hk-24xn1 closed).** The
  workloop wakes at sub-poll-interval latency on `queue-submit`/`queue-append`. CL-071
  eager refill may rely on this; the spec must not re-introduce a poll-interval-wait
  assumption.

- **C7 — Naming consistency.** CL-080 already names `harmonik supervise pause/resume`;
  the recovered-issue table names `harmonik pause/resume`. The spec draft must pick one
  canonical verb form and reconcile `budget.ts:8` / `circuit-breaker.ts:5` comments that
  reference a non-existent command, plus the `CommandPause` reserved-but-unwired code in
  `internal/operatornfr/commandcodes.go:34`.

## 5. Success criteria — concrete statements about the specs after this work

- **SC1.** `specs/execution-model.md` defines a **no-auto-pull / queue-only daemon
  topology**: names the operator flag, states that a bare daemon in this topology
  dispatches nothing, and reconciles the existing "MUST NOT fall back to br ready"
  language (`:1395,:1647`) with the `workloop.go:833-858` fallback by making it
  opt-in. A conformance scenario asserts "boot daemon, submit nothing, observe zero
  `run_started` events."

- **SC2.** `specs/cognition-loop.md` CL-070..073 are amended (or annotated) to state
  the **concrete Pi drive surface**: the harness calls `kerf next --format=json
  --only=bead`, applies CL-072 guards, dependency-orders, and `harmonik queue submit`
  /`append`s to a **stream** queue. A conformance scenario asserts "slot frees →
  harness appends the next ranked, non-skipped bead via `queue append` without waking
  the model."

- **SC3.** The specs define an **agent-callable pause/resume verb** and name its
  **producer of `operator_pause_status`**: which process emits it (CLI→RPC or signal),
  which states it drives, and that the `br-ready` fallback path is gated on the same
  pause state as the queue path. A conformance scenario asserts "Pi issues pause →
  no new `run_started` while in-flight runs reach terminal → resume → dispatch
  resumes."

- **SC4.** `specs/cognition-loop.md` CL-051 is realized by a named harness obligation:
  `bridge.ts` marks a bead DONE only after BOTH the event AND the origin/main trailer;
  the two single-condition windows route per CL-051 (`loop_observed_phantom_done` →
  Tier-2; event-only → re-poll). A conformance scenario asserts "completion event
  arrives but trailer absent on origin/main → bead is NOT marked done."

- **SC5.** A **changelog** enumerates every new/changed control-point ID across
  `execution-model.md`, `cognition-loop.md`, and `queue-model.md`, and the stale-doc
  corrections (S7 subscribe implemented; no supervise pause verb today) are applied so
  CLAUDE.md / spec comments no longer misdirect.

- **SC6.** The five attached beads (`hk-ry8q1`, `hk-3ix6o`, `hk-ytj2r`, `hk-dg42b`,
  `hk-5bw7a`) each map to a named task in the eventual `07-tasks.md` and to a specific
  control-point change, with no orphaned bead.

## 6. Preliminary list of affected spec areas

| Spec | What changes | Drivers |
|------|--------------|---------|
| `specs/execution-model.md` | No-auto-pull / queue-only daemon topology; reconcile the `br-ready` fallback (`workloop.go:833-858`) with the existing "MUST NOT fall back to br ready" language (`:1395,:1647`); gate the fallback path on operator-pause state. | S2 (G1), S9 br-ready gate (G3) |
| `specs/cognition-loop.md` | Realize CL-070..073 as the concrete Pi dispatch surface (`kerf next` → guards → dependency-order → stream `submit`/`append`); realize CL-051 two-phase-done in the harness; reconcile CL-080 pause-verb naming. | S5/S6/S10 (G2), S8 (G4), S9 naming (G3) |
| `specs/queue-model.md` | Confirm stream-group append contract is the Pi curation path; name the `operator_pause_status` **producer** side feeding the existing consumer (QM-054); confirm wave-vs-stream selection for Pi-driven dispatch. | S5/S10 (G2), S9 producer (G3) |
| `specs/operator-nfr.md` (referenced, possibly touched) | Pause/resume command surface (ON-027 drain ordering, ON-012/013c re-emit-once) — confirm the new CLI verb maps onto the existing ON state machine rather than inventing a parallel one. | S9 (G3) |

**Code surfaces these spec changes bind (for the eventual implementation tasks, not
this pass):** `internal/daemon/workloop.go:833-858` (auto-pull fallback),
`cmd/harmonik/supervise_cmd.go:42-54` (verb list — no pause/resume today),
`internal/operatornfr/commandcodes.go:34` (`CommandPause` reserved, unwired),
`queue_operatoreventconsumer_7urls.go:118-150` (consumer exists, no producer),
`.pi/extensions/flywheel/{bridge,router,index}.ts` (no `queue submit/append`, no
`kerf next`, no pause call, `buildMinimalDigest` stub).

## 7. Open questions to resolve in later passes (not blockers)

- **OQ1.** Canonical pause verb: `harmonik pause/resume` (top-level) vs `harmonik
  supervise pause/resume` (CL-080's current wording). Decide in Change Design;
  lean toward CL-080's `supervise pause/resume` to avoid a parallel surface, unless
  the agent-callable path argues for a top-level verb. Reconcile `budget.ts` /
  `circuit-breaker.ts` comments to the chosen form.
- **OQ2.** Pause producer transport: CLI→daemon Unix-socket RPC (consistent with the
  queue verbs over PL-003a) vs OS signal. Lean RPC for the agent-callable path.
- **OQ3.** Does the no-auto-pull default ship as a global daemon default or a
  flywheel-topology-only default? Lean topology-scoped to preserve C3 backward compat.
- **OQ4.** Where the dependency-ordering for curated dispatch lives: rely on `kerf
  next` rank order + cross-group QM-031 sequencing, or have the harness build explicit
  groups. Lean on `kerf next` rank + stream append for v0.1 (matches CL-071's
  rank-order loop).
