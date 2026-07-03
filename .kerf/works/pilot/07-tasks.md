# Implementation Tasks — `pilot` (Pi-driven dispatch & control plane)

Breaks the four spec changes (`05-spec-drafts/`) into implementation tasks. Each task makes the codebase match the updated spec. **Every task maps to an already-created `codename:pilot` bead** (reconciled via `br list --label codename:pilot`); no new beads are created here. Where a needed task has no bead, it is flagged explicitly as a GAP.

## Bead reconciliation (source of truth)

Pulled `br list --label codename:pilot` — 11 open beads:

| Bead | Pri | Type | Role |
|---|---|---|---|
| hk-ry8q1 | P0 | feature | Impl: agent-callable pause/resume verb + `operator_pause_status` producer + br-ready operator-pause gate |
| hk-3ix6o | P1 | feature | Impl: CL-051 two-phase-done in `bridge.ts` |
| hk-dg42b | P2 | feature | Impl: Pi-side curated dispatch (`queue submit`/`append`) |
| hk-ytj2r | P2 | task | Impl: replace `buildMinimalDigest` stub with `harmonik digest --json` |
| hk-5bw7a | P3 | docs | Impl: correct stale docs (subscribe landed; supervise pause/resume now exists) |
| hk-h5lv2 | P2 | task | scenario-test: execution-model (EM-066 quiet daemon + EM-067 fallback pause-gate) |
| hk-ynjnf | P2 | task | exploratory-test: execution-model (`--no-auto-pull`/`--queue-only` flag surface) |
| hk-95a2r | P2 | task | scenario-test: operator-nfr (agent-issued pause/resume end-to-end, ON-056/057) |
| hk-rnlxh | P2 | task | exploratory-test: operator-nfr (human + agent same surface, no human gate, ON-056) |
| hk-iht2w | P2 | task | scenario-test: cognition-loop (CL-051 two-phase done routing) |
| hk-va7z2 | P2 | task | exploratory-test: cognition-loop (eager-refill without waking the model, CL-071/073) |

There is NO `--no-auto-pull` daemon-flag implementation bead distinct from the pause work, and NO standalone EM-066 implementation bead carrying `codename:pilot` — see **GAP-1** below. All other tasks map to a bead.

---

## Implementation tasks

### IT-1 — Quiet-by-default daemon: `--no-auto-pull` topology + operator-pause fallback gate (EM-066 + EM-067)

- **Implements:** `execution-model.md §4.11 EM-066`, `§4.11 EM-067`, `§7.4` main-loop pseudocode (the flag-gated `queue IS None` branch + the primary loop-top ON-008 gate clarification), `§10.1`/`§10.2` conformance.
- **What to build:**
  1. Add a startup-time boolean flag `--no-auto-pull` (alias `--queue-only`) to the daemon CLI; seal it at startup (no re-read for the process lifetime, parity with EM-051). Surface via `process-lifecycle.md §4.1`.
  2. In `internal/daemon/workloop.go` (the `queue IS None` branch, currently `workloop.go:833-858` where it falls back to `br.Ready()`): when `--no-auto-pull` is set, take the `idle_wait_for_queue_submission()` path and MUST NOT call `br ready`, MUST NOT emit `run_started`, MUST NOT spawn an agent.
  3. When the flag is unset (historical topology), retain the `br ready` fallback BUT add the EM-067 operator-pause defense-in-depth re-assert: if the operator-control state is `pausing`/`paused`, idle-wait instead of dispatching. The gate reads the single source of pause truth (`operator_pause_status` from ON-056/057). Note per the EM-067 reframing: the loop-top `should_pause_between_runs()` (ON-008) is the PRIMARY gate; this inline re-assert is belt-and-suspenders + the explicit binding of the fallback path to the operator-pause-truth source.
  4. Default the flag topology-scoped: ON for the supervised (flywheel) topology launched via `process-lifecycle.md §4.9` / `operator-nfr.md §4.3`; OFF (fallback retained) for the historical single-daemon topology.
- **Deliverables:** `internal/daemon/workloop.go` (fallback branch), daemon CLI flag wiring (`cmd/harmonik` / `main.go` startup), topology-default wiring in the supervise launch path.
- **Acceptance criteria:**
  - Boot with `--no-auto-pull`, submit nothing → zero `run_started` over a bounded window, no agent subprocess, no credit (EM-066). (Validated by hk-h5lv2.)
  - Boot WITHOUT the flag with ≥1 ready bead and no queue → fallback dispatches `ready[0]` (EM-066 opt-in branch).
  - Flag is read once at startup, not re-read (sealing test).
  - With the fallback enabled and operator-control driven to `paused` via the ON-056/057 producer → no new `run_started` from the fallback while paused; on `resume` → fallback dispatch resumes (EM-067). (Validated by hk-h5lv2.)
- **Bead:** primarily **hk-ry8q1** (its description explicitly includes "gate the br-ready path on the same pause state" = EM-067) for sub-task 3; **GAP-1** covers sub-tasks 1, 2, 4 (the `--no-auto-pull` flag + the EM-066 quiet-branch wiring). See GAP-1.
- **Depends on:** IT-2 (the operator-pause producer must exist for sub-task 3 to be testable; EM-066's quiet branch — sub-tasks 1/2 — has no dependency and can land first).

### IT-2 — Agent-callable pause/resume verb + `operator_pause_status` producer (ON-056 + ON-057)

- **Implements:** `operator-nfr.md §4.3 ON-056`, `§4.3 ON-057`, `§7.1` state-table rows (`running|pause`, `paused|resume`), the ON-056/057 conformance obligation; drives the existing `running → pausing → paused` / `paused → resuming → running` transitions; consumed by `queue-model.md §8.5 QM-054` and `execution-model.md §7.4 EM-067`.
- **What to build:**
  1. Add `pause` and `resume` RPC methods to the daemon's Unix-socket JSON-RPC transport (`process-lifecycle.md §4.1 PL-003a`), co-located with the `queue-*` methods, under the ON-013a panic barrier. Wire `operatornfr/commandcodes.go:34` `CommandPause` (currently reserved/unwired) + a `CommandResume`.
  2. Add the operator-facing CLI verbs `harmonik supervise pause` / `harmonik supervise resume` to `cmd/harmonik/supervise_cmd.go` (currently start/stop/status/attach/restart/logs/_shim only).
  3. Make the verb the PRODUCTION producer of `operator_pause_status{status, pause_reason=operator}` and `operator_resuming` — emit the EXISTING ON-013 events through the EXISTING §7.1 transitions; introduce no new event type, no new state. Tag `pause_reason=operator` at the emission site per `event-model.md §6.3`.
  4. Inherit unchanged: ON-027 drain ordering, ON-008 between-task gate, ON-013 emission, ON-013c idempotency-on-no-op, ON-030a durable marker, ON-010 reconciliation carve-out.
  5. Agent-callable obligation: NO human-only gate; the CLI path (framing the PL-003a RPC) is the same surface for human and agent. The cognition loop (CL-080) MAY issue it without human intervention.
  6. On the `status=paused` emission, include `drain_summary` (per ON-013) — depends on the EV §8.7.6 `drain_summary?` payload extension (cross-spec coordination item, tracked; see COORD-1).
- **Deliverables:** `cmd/harmonik/supervise_cmd.go` (new `pause`/`resume` subcommands), `internal/operatornfr/commandcodes.go` (wire CommandPause/Resume), the daemon RPC handler + the `operator_pause_status` emission site (the real producer that `queue_operatoreventconsumer_7urls.go` already consumes), CLI→RPC framing.
- **Acceptance criteria (ON-056/057 conformance, from operator-nfr draft):** with ≥1 in-flight run, an agent issues `harmonik supervise pause` over PL-003a: (a) `operator_pause_status{status=pausing, pause_reason=operator}` emitted; (b) no new `run_started` while pausing/paused; (c) every in-flight run reaches terminal without abort; (d) `operator_pause_status{status=paused}` with `drain_summary` only after all ON-027 drain steps complete; (e) queue transitions `active → paused-by-drain` (QM-054); (f) `resume` → `operator_resuming`, daemon returns to `running`, dispatch resumes. No human action required. (Validated by hk-95a2r, hk-rnlxh.)
- **Bead:** **hk-ry8q1** (P0). Sub-task 6's doc-comment side overlaps hk-5bw7a (IT-6).
- **Depends on:** none (it produces the signal IT-1 sub-task 3 consumes).

### IT-3 — CL-051 two-phase-done verification in the harness (CL-051)

- **Implements:** `cognition-loop.md §4.7 CL-051` (amended), `§4.9 CL-050` (loop is a second consumer), CL-055 idempotency key, `§7` acceptance scenario 7.
- **What to build:** Replace the deterministic-completion mark in `.pi/extensions/flywheel/bridge.ts` (currently `bridge.ts:253-258` marks completions processed with no git-trailer check) with the two-phase gate:
  1. Condition 1: `run_completed{success}` with `bead_id` observed in `events.jsonl`.
  2. Condition 2: `git log origin/main --grep "Refs: hk-XYZ" --max-count=1` non-empty.
  - Mark DONE only when BOTH hold. A deterministic-tier `run_completed` that advances the watermark MUST NOT, by that advance, mark the bead done.
  - Condition-1-only (event, no trailer on `origin/main`) → treat as in-flight, re-poll (daemon terminal-multi-step window, `execution-model.md §4.12 EM-052/EM-053`).
  - Condition-2-only (trailer, no terminal event) → emit `loop_observed_phantom_done{bead_id}` warning and route to Tier-2 reconciliation (the investigator-required-category path, `reconciliation/spec.md §4.4`); MUST NOT act directly.
  - Idempotency-key the confirmation per CL-055 against the triggering `run_completed.event_id` (reacted-ledger; effectively-once across crashes).
- **Deliverables:** `.pi/extensions/flywheel/bridge.ts` (replace lines ~253-258 with the two-phase gate + `loop_observed_phantom_done` emission).
- **Acceptance criteria:** `run_completed{success}` whose `Refs:` trailer is absent on `origin/main` does NOT mark the bead done (re-polls); a `Refs:` trailer present with no terminal event emits `loop_observed_phantom_done` and routes to Tier-2 without direct action. (Validated by hk-iht2w.)
- **Bead:** **hk-3ix6o** (P1).
- **Depends on:** none.

### IT-4 — Pi-side curated dispatch via `queue submit`/`append` (CL-071, CL-072, CL-073; mirrors EM-062..065)

- **Implements:** `cognition-loop.md §4.9 CL-071` (eager refill), CL-072 (pre-screen guards), CL-073 (wake-at-empty-queue boundary), `§7` acceptance scenario 6; dispatch surface `queue-model.md §2.4`/`§7 QM-040` (stream group), `§8.1 QM-050` (submit-as-start); daemon-side mirror `execution-model.md §4.13 EM-062/063`, `§4.14 EM-064/065`.
- **What to build:** in the flywheel harness (`.pi/extensions/flywheel/`, currently shells only `subscribe` + `digest`, both read-only):
  1. On `run_completed`/`run_failed`/`run_canceled` with ≥1 free slot: `kerf next --format=json --only=bead`; apply CL-072 pre-screen guards in rank order until a non-skipped survivor is found.
  2. First fill (no active queue): create the stream group via `harmonik queue submit` (`queue-submit` returning `status: active` IS the start semantics; no separate start verb). Refill on subsequent slot releases: `harmonik queue append` against the active stream group (wave groups reject the append).
  3. Eager refill MUST NOT wake the model; wake the model only when `kerf next` is empty or yields only pre-screened-out candidates (CL-073 empty-queue boundary).
  4. `kerf next` is advisory; `queue.json` is authoritative on disagreement (resolved OQ-CL-001, EM-064 tier 1).
- **Deliverables:** new harness dispatch module under `.pi/extensions/flywheel/` (the curated-dispatch path: read `kerf next`, dependency-order, submit/append), wired into the `router.ts`/`bridge.ts` slot-release path.
- **Acceptance criteria:** on slot release with a non-empty `kerf next`, the harness appends the next ranked non-pre-screened bead to the active stream group via `harmonik queue append` WITHOUT waking the model; the model is woken only when `kerf next` is empty/all-pre-screened. (Validated by hk-va7z2.)
- **Bead:** **hk-dg42b** (P2).
- **Depends on:** none (the daemon-side `queue submit`/`append` CLI + EM-062..065 mirror already exist per the assessment doc S5/S10).

### IT-5 — Recycle-path digest: replace `buildMinimalDigest` stub (CL-030)

- **Implements:** `cognition-loop.md §4.4 CL-030` (amended — recycle-path seed digest MUST come from `harmonik digest`; substrate placeholder is non-conforming).
- **What to build:** replace the `index.ts:343 buildMinimalDigest` stub with a real `harmonik digest --json` call on recycle.
- **Deliverables:** `.pi/extensions/flywheel/index.ts` (recycle path).
- **Acceptance criteria:** on recycle, the seed digest is the output of `harmonik digest --json`, not the placeholder.
- **Bead:** **hk-ytj2r** (P2).
- **Depends on:** none.

### IT-6 — Stale-doc correction (documentation alignment)

- **Implements:** `cognition-loop.md §4.10 CL-080` INFORMATIVE note (budget.ts/circuit-breaker.ts comments correct-in-intent once the verb lands), `operator-nfr.md §4.3 ON-056` (canonical verb form); operator-nfr design T3 (reconcile `commandcodes.go` comment).
- **What to build:**
  1. Correct `CLAUDE.md` / `AGENTS.md` text that frames `harmonik subscribe` as a future gap (it landed: `cmd/harmonik/subscribe.go`, hk-6ynv4).
  2. Update `.pi/extensions/flywheel/budget.ts` and `circuit-breaker.ts` comments that reference a then-nonexistent `harmonik supervise resume` — now correct-in-intent once IT-2 lands; align the comment to the canonical verb form.
  3. Reconcile `operatornfr/commandcodes.go` comment to the wired verb.
- **Deliverables:** `CLAUDE.md`/`AGENTS.md`, `.pi/extensions/flywheel/budget.ts`, `.pi/extensions/flywheel/circuit-breaker.ts`, `internal/operatornfr/commandcodes.go` (comment only).
- **Acceptance criteria:** no doc/comment references a nonexistent command; subscribe is documented as implemented.
- **Bead:** **hk-5bw7a** (P3).
- **Depends on:** IT-2 (the `supervise resume` verb must actually exist before the comments are "correct"; the subscribe-doc half has no dependency).

---

## Test tasks (required before advancing to Ready)

Per the Tasks-pass gate, both a scenario-test and an exploratory-test must exist per substantially-changed spec area, listed as explicit tasks dependent on the impl they validate. The Spec-Draft pass already filed all six; they are listed here with their gating dependencies. Neither this work nor its impl beads may close until these test beads also close.

### TT-1 — scenario: execution-model (EM-066 + EM-067)
- **Validates:** IT-1. **Bead:** **hk-h5lv2** (P2).
- Boot daemon with `--no-auto-pull`, submit nothing → assert zero `run_started` (EM-066). With fallback enabled (flag unset) and operator-control driven to `paused` → assert br-ready fallback does not dispatch; on resume, dispatch resumes (EM-067).
- **Depends on:** IT-1, IT-2.

### TT-2 — explore: execution-model (`--no-auto-pull`/`--queue-only` flag surface)
- **Validates:** IT-1 (operator-facing CLI surface). **Bead:** **hk-ynjnf** (P2).
- Operator invokes a no-auto-pull daemon boot and a historical-topology boot; observe the flag surface and zero-dispatch quiet behavior.
- **Depends on:** IT-1.

### TT-3 — scenario: operator-nfr (ON-056/ON-057 end-to-end)
- **Validates:** IT-2. **Bead:** **hk-95a2r** (P2).
- Agent issues `harmonik supervise pause` over PL-003a with a run in-flight; assert `operator_pause_status{pausing}`, no new `run_started`, in-flight run reaches terminal, `{paused}`+`drain_summary`, queue `active → paused-by-drain`, resume restores dispatch.
- **Depends on:** IT-2.

### TT-4 — explore: operator-nfr (same surface, human + agent, no human gate)
- **Validates:** IT-2 (operator-facing CLI surface). **Bead:** **hk-rnlxh** (P2).
- Human and agent both invoke `harmonik supervise pause/resume`; confirm same command surface, agent-callable without a human gate.
- **Depends on:** IT-2.

### TT-5 — scenario: cognition-loop (CL-051 two-phase done)
- **Validates:** IT-3. **Bead:** **hk-iht2w** (P2).
- `run_completed{success}` for a bead whose `Refs:` trailer is absent on `origin/main` does NOT mark the bead done (re-poll); trailer-without-event emits `loop_observed_phantom_done` → Tier-2.
- **Depends on:** IT-3.

### TT-6 — explore: cognition-loop (eager-refill without waking the model)
- **Validates:** IT-4 (operator/harness-facing dispatch surface). **Bead:** **hk-va7z2** (P2).
- Harness eager-refill on slot release shells `kerf next` + `harmonik queue append` against a stream group without waking the model; wakes the model only at the empty-queue boundary.
- **Depends on:** IT-4.

---

## Dependency graph (DAG)

```
IT-2 (pause/resume verb + producer, P0)
  ├─→ IT-1 (quiet daemon + fallback pause-gate)   [IT-1 EM-066 quiet branch has NO dep; IT-1 EM-067 gate needs IT-2]
  ├─→ IT-6 (doc correction)                        [supervise-resume comment half needs IT-2; subscribe-doc half independent]
  ├─→ TT-3 (scenario operator-nfr)
  └─→ TT-4 (explore operator-nfr)

IT-1 ─→ TT-1 (scenario execution-model)            [also needs IT-2 for the EM-067 pause-gate assertion]
IT-1 ─→ TT-2 (explore execution-model)

IT-3 (CL-051 two-phase done, P1) ─→ TT-5 (scenario cognition-loop)

IT-4 (Pi curated dispatch, P2) ─→ TT-6 (explore cognition-loop)

IT-5 (recycle digest, P2)   [no deps, leaf]
```

- **No cycles.** Every edge points from a prerequisite to a dependent.
- **No missing prerequisites:** the daemon-side `queue submit`/`append` CLI + EM-062..065 mirror and the queue-side `operator_pause_status` consumer (QM-054) already exist per the assessment doc — IT-4 and IT-2's consumer side have no unbuilt prerequisite inside this bundle.

## Parallelization plan

- **Wave A (no deps, fully parallel):** IT-2 (P0), IT-3 (P1), IT-4 (P2), IT-5 (P2), and the EM-066-quiet-branch portion of IT-1. Five independent code surfaces (`supervise_cmd.go`/RPC; `bridge.ts`; flywheel dispatch module; `index.ts`; `workloop.go` quiet branch). The IT-1 quiet branch and IT-2 both touch daemon Go but in different files (`workloop.go` vs `supervise_cmd.go`/`commandcodes.go`) — declared non-conflicting.
- **Wave B (after IT-2):** the EM-067 fallback-pause-gate portion of IT-1; IT-6 supervise-resume-comment half.
- **Wave C (tests, after their impl):** TT-3/TT-4 (after IT-2), TT-5 (after IT-3), TT-6 (after IT-4), TT-1 (after IT-1 **and** IT-2), TT-2 (after IT-1).
- **Realism note:** IT-1 is the only task split across waves (its EM-066 half is Wave A, its EM-067 half is Wave B). An implementing agent MAY land IT-1 whole in Wave B if it prefers a single PR; the split only states what is parallelizable, not what must be separate commits.

## Spec traceability + changelog coverage

Every `05-changelog.md` entry has ≥1 implementing task:

| Changelog entry | Spec IDs | Task(s) | Bead(s) |
|---|---|---|---|
| operator-nfr A3 — ON-056, ON-057 | ON-056, ON-057, §7.1 table, conformance | IT-2 | hk-ry8q1 |
| execution-model A1 — EM-066 | EM-066, §7.4 quiet branch, §10.1/§10.2 | IT-1 (EM-066 half) | GAP-1 (+ hk-ry8q1 for the gate) |
| execution-model A1 — EM-067 | EM-067, §7.4 fallback gate, §10.2 | IT-1 (EM-067 half) | hk-ry8q1 |
| cognition-loop A2 — CL-051 | CL-051, §7 scenario 7 | IT-3 | hk-3ix6o |
| cognition-loop A2 — CL-071 | CL-071/072/073, §7 scenario 6 | IT-4 | hk-dg42b |
| cognition-loop A2 — CL-080 | CL-080 note | IT-6 | hk-5bw7a |
| cognition-loop A2 — CL-030 | CL-030 | IT-5 | hk-ytj2r |
| queue-model A4 — §2.4/§8.5/§8.1 notes | QM-040/QM-050/QM-054 (annotation-only) | no code change (informative); consumed by IT-1/IT-2/IT-4 | covered transitively |

The queue-model A4 changes are annotation-only (no new requirement IDs, no consumer-semantics change) — they confirm existing behavior that IT-1/IT-2/IT-4 already exercise, so they require no dedicated implementation task. This is the one changelog row without a 1:1 task; it is intentionally annotation-only (recorded in the changelog and integration doc) and is therefore satisfied by the tasks that consume the annotated surfaces.

---

## Task → bead GAP analysis

Per the work brief: every task must map to an existing bead; where a task has NO bead, flag it as a GAP (do NOT create beads).

- **GAP-1 — the `--no-auto-pull` / `--queue-only` daemon flag + EM-066 quiet-branch wiring has no dedicated `codename:pilot` implementation bead.** hk-ry8q1 (P0) covers the agent-callable pause/resume verb, the producer, AND "gate the br-ready path on the same pause state" (= EM-067). It does NOT explicitly cover sub-tasks IT-1.1/IT-1.2/IT-1.4 — adding the `--no-auto-pull` flag, sealing it, wiring the EM-066 quiet `idle_wait_for_queue_submission` branch in `workloop.go`, and the topology-scoped default. EM-066 is the incident-root mechanism (S2) and is a distinct code surface (`workloop.go` fallback branch + CLI flag) from the pause verb (`supervise_cmd.go`). The recovered-issue table in the assessment doc lists "Flip daemon `br ready` auto-pull to OPT-IN ... land the `--no-auto-pull`/`--queue-only` flag tracked by **hk-exd7m**" — hk-exd7m is the pre-existing daemon-idle bead that EM-066 escalates, but **hk-exd7m carries no `codename:pilot` label**, so it is outside this work's bead set and was not surfaced by `br list --label codename:pilot`. **Recommendation (for the orchestrator, not actioned here per the no-create-beads constraint):** either (a) add the `codename:pilot` label to hk-exd7m and treat it as IT-1's EM-066 bead, or (b) widen hk-ry8q1's scope/description to explicitly include the `--no-auto-pull` flag + EM-066 quiet branch. Until then, IT-1's EM-066 half (sub-tasks 1/2/4) is bead-gapped within the pilot set; IT-1's EM-067 half (sub-task 3) is covered by hk-ry8q1.

- **No other gaps.** IT-2→hk-ry8q1, IT-3→hk-3ix6o, IT-4→hk-dg42b, IT-5→hk-ytj2r, IT-6→hk-5bw7a, and all six test tasks (TT-1..TT-6) → hk-h5lv2/hk-ynjnf/hk-95a2r/hk-rnlxh/hk-iht2w/hk-va7z2 respectively.

## Cross-spec coordination items (tracked, non-blocking, no pilot bead)

- **COORD-1 — EV `event-model.md §8.7.6` `drain_summary?` extension.** ON-013/ON-057 require the `status=paused` emission to carry `drain_summary`; the EV payload extension is a pre-existing cross-spec coordination request owned by operator-nfr → event-model. IT-2 sub-task 6 depends on it. Not a pilot implementation task; surfaced so the EV extension is not lost. (No `codename:pilot` bead; intentionally out-of-bundle.)
