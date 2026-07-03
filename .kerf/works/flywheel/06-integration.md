# Integration Review

> Composes the 4 spec drafts (`05-spec-drafts/`) + the 3 round-4 vetting results into a coherent picture: what edits land where, what the bead dependency graph looks like, and which specs/code packages each bead touches.

## Cross-Reference Checks Performed
Every internal `[spec.md §X.Y ID]` reference in the 4 spec drafts was checked against (a) the existing `specs/` corpus and (b) the other drafts in the same set.
- `cognition-loop.md` refs: PL-018 / PL-019 / PL-002a / PL-008a / PL-011 / PL-014a / PL-021b / PL-021c / WM-026 / EV-INV-001 / EV-022 / EV §4.1 / §4.3 / §4.5 / §6.3 / §6.4 / §8.7.14 / EV-027 / EM-053 / RC-002a / RC §4.4 / BI-009 / BI-013 / BI-016 / ON-018 / ON-035 / QM-002 / QM-035.
- `process-lifecycle.md` diffs ref: PL-002a/2b/3a/4/6/6a/6c/8a/11/14a/18/19/21c/28/28c + WM-026 + ON-018 + ON §8 + EV §6.3.
- `event-model.md` diffs ref: EV-009/14d/22/27/36/INV-001 + §4.3/4.5/6.3/6.4/8.5.6/8.7.14/8.9/8.10.4/8.11 + `internal/daemon/subscribe.go` + `cmd/harmonik/subscribe.go:14,200` + `.harmonik/decision_acks/`.
- `execution-model.md` diffs ref: EM-015a/15f/17a/49/50/51/52/53/54 + QM-002/3/35/40 + `internal/daemon/queuestore_hkj808w.go:97-104` + `internal/queue/state.go:283` + BI-009 + RC §4.4.

All references resolve to either (a) an existing requirement in `specs/`, (b) a sibling clause within this drafts set, or (c) a code file:line citation in the harmonik tree. **No dangling references.**

## Contradictions Found
**None across the 4 drafts.** All four were drafted from the same `02-components.md` + `04-design/self-managing-architecture.md` + `review-synthesis.md` baselines and reference each other consistently.

**One cross-spec edit must land atomically with the existing corpus** (same content in three places):
- `daemon_orphan_sweep_completed` payload schema in `event-model.md §8.7.14` must gain `coordinator_sessions_skipped` (per PL-006d). One spec-edit bead (B0 below) bundles all three edits (PL + EV).

## Consistency Issues Found
- **PL `PAUSED` vs CL `Suspended` vs HC `HandlerStatus.paused`.** Three distinct uses of "pause" at different scopes (per-session lifecycle in HC-064; per-handler-type in handler-pause.md; daemon-level in process-lifecycle.md §7.1). Resolved by agent-lifecycle FSM vet bead 1: rename per-session state to **Suspended** + add disambiguation entries in §3 glossaries of all three specs.
- **"Watch-restart shim" sized as 30-50 LOC in PL-019(f) vs ~250-350 LOC in supervisor-restart-policy vet.** Resolved: spec language stays normative ("a lightweight wrapper shim"); LOC was informative aside; richer `internal/supervise/` fulfills the same shim contract while adding max-restarts + backoff + crash-loop guard.
- **Band names in CL-011 (`{nominal,soft,strong,critical}`) collide with gateway vocabulary (`{healthy,warning,critical,emergency}`).** Resolved by context-health vet's single docs bead: rename to gateway vocabulary; behavior unchanged.

## Cross-Reference Validity
All `[spec.md §X.Y ID]` cross-refs resolve. Three NEW IDs that must coordinate atomically on merge:
- `lifecycle_transition` event type (agent-lifecycle FSM bead 1+3) — adds to event-model §8.3.x, schema-version bump per EV §6.4 additive rule.
- `decision_required` / `decision_acknowledged` event types (event-model draft EV-042..044) — also EV §6.4 additive.
- `coordinator_sessions_skipped` field on `daemon_orphan_sweep_completed` (PL-006d) — EV §8.7.14 schema extension.
All three are additive; none breaking. **They share ONE schema-version bump on event-model.**

## Composition picture (how the new specs wire)
```
                  ┌──────────────────────────────────────┐
                  │   specs/cognition-loop.md (NEW)      │
                  │   CL-001 … CL-100 + 5 invariants     │
                  │   + CL-014/CL-015 (band rename)      │
                  └────────┬──────────────────┬──────────┘
                consumes   │                  │ governs
                           ▼                  ▼
        ┌─────────────────────────┐  ┌──────────────────────────┐
        │ specs/event-model.md    │  │ specs/process-lifecycle  │
        │  + EV-037…044           │  │  + PL-019 promotion      │
        │  + decision_required    │  │  + PL-006d carve-out     │
        │  + decision_acknowledged│  │  + PL-028 / PL-028d      │
        │  + §10.3 CLI fix        │  │                          │
        └────────┬────────────────┘  └──────────────────────────┘
                 │ decision_required             ▲ supervised by
                 ▼                               │ harmonik supervise
        ┌─────────────────────────┐  ┌──────────────────────────┐
        │ specs/execution-model   │  │ internal/supervise/      │
        │  + EM-062 / EM-063      │  │  (Go port of             │
        │  + EM-064 / EM-065      │  │  gateway supervisor)     │
        │  + EM-NOTE-WAKE         │  │                          │
        │  + EM-NOTE-STREAM-CONC  │  │                          │
        └─────────────────────────┘  └──────────────────────────┘

           ┌─────────────────────────────────────────────────┐
           │ specs/handler-contract.md                       │
           │  + HC-064…067 (per-session lifecycle FSM)       │
           │  + internal/handlercontract/lifecycle/ Go port  │
           │  + watcher integration + lifecycle_transition   │
           └─────────────────────────────────────────────────┘
                                  ▲
                                  │ uses HC FSM
                       ┌──────────┴──────────┐
                       │ ./.pi/extensions/   │
                       │ flywheel/ (TS Pi)   │
                       │  + note tool        │
                       │  + reset_context    │
                       │  + 70/90/100 floor  │
                       │  + event bridge     │
                       │  + custom TUI panel │
                       └─────────────────────┘
```

## Dependency order for the bead set
Numbered phases; within a phase, beads can be done in parallel.

```
PHASE 0 — spec adoption (atomic bundle)
  └─ B0  Spec edits: PL-019 promotion + PL-006d + PL-028d (closes hk-hc3qq);
         EV-037…044 + decision_required (additive); EM-062…065 + EM-NOTEs;
         HC-064…067 + CL-014/CL-015 amendments; NEW cognition-loop.md.
         ONE PR; ONE schema-version bump on event-model.

PHASE 1 — Go foundations (parallel)
  ├─ B1  harmonik digest Go subcommand + JSON schema +
  │      pure-code status sheet.                    (CL-030..033)
  ├─ B2  internal/handlercontract/lifecycle/ FSM Go package.   (HC-064..067)
  └─ B3  internal/supervise/ supervisor package
         (spawn loop + onExit + restart policy + backoff
         + max-restarts + crash-loop guard).         (PL-019(f))

PHASE 2 — Go integrations (depends on phase 1)
  ├─ B4  Wire FSM into handler/Session/watcher; emit
  │      lifecycle_transition event.                 (depends on B2)
  ├─ B5  cmd/harmonik/supervise/ CLI surface
  │      (start/stop/status/attach/restart/logs);
  │      --watch-restart uses internal/supervise.    (depends on B3, B1)
  └─ B6  harmonik digest --watch live TUI loop.       (depends on B1)

PHASE 3 — Pi extension wire-up (parallel where possible)
  ├─ B7  Event bridge in Pi extension: tail harmonik subscribe NDJSON →
  │      followUp queue; ~400ms debounce; watchdog timers.
  ├─ B8  Stratified model routing in prepareNextTurn
  │      (Haiku tier 1 / Sonnet tier 2 / Opus tier 3); budget kill-switch.
  └─ B9  Custom TUI status panel (setWidget) rendering the digest
         with durations + ages + fullness %.

PHASE 4 — Operator surfaces
  ├─ B10 Fat-skills initial catalog: triage-failure.md / investigate-run.md /
  │      compose-batch.md / escalate.md / reconcile-state.md (under .flywheel/skills/).
  └─ B11 Integration smoke: harmonik supervise start; loop runs unattended
         for 4h; cache_read_input_tokens monitored; 10-in-flight crash
         scenario passes.

PHASE 5 — close hk-hc3qq + stale-CLI-help fix + N-1 readers green
```

## Bead-to-spec mapping
| Bead | Implements |
|---|---|
| B0  Spec edits bundle | All 4 spec drafts + HC-064..067 + CL-014/CL-015 |
| B1  harmonik digest | CL-030..033 + new short `specs/digest-command.md` (OQ-CL-002) |
| B2  HC lifecycle FSM Go package | HC-064..067 (handler-contract amendment in B0) |
| B3  internal/supervise/ | PL-019(f) (no spec change beyond B0) |
| B4  FSM watcher integration | HC-064..067 + new lifecycle_transition event (B0) |
| B5  supervise CLI surface | PL-028d (B0 covers spec) |
| B6  harmonik digest --watch | CL-082 |
| B7  Pi event bridge | CL-060..064 |
| B8  Stratified model routing + budget | CL-070..073 + CL-090..091 |
| B9  Custom TUI panel | CL-081..082 |
| B10 Fat-skills catalog | Operational; not normative (layered-instructions L3) |
| B11 Integration smoke | CL conformance scenarios 1-5 |

## What B0 bundles
One atomic merge of 5 files in `specs/`:
1. **`specs/cognition-loop.md` (NEW)** — copy from `05-spec-drafts/cognition-loop.md`, **plus** CL-014/CL-015 from context-health vocabulary vet §5.
2. **`specs/process-lifecycle.md`** — apply the 3 changes (PL-019 promotion, PL-006d carve-out, PL-028d command-surface).
3. **`specs/event-model.md`** — apply the 3 changes (§4.11 consumer contract + §8.12 decision_required + §10.3 CLI fix), **plus** new `lifecycle_transition` event-type registration (additive, shared schema-version bump with decision_required).
4. **`specs/execution-model.md`** — apply the 3 changes (eager refill, check-observed guard, EM-NOTE corrections).
5. **`specs/handler-contract.md`** — add HC-064..067 from agent-lifecycle FSM vet bead 1.

B0 closes **hk-hc3qq** (PL-006d carve-out is its fix) and authoritatively resolves the three corpus-wide consistency issues named above.

## Spec-edit ordering within B0's PR
1. event-model.md first (registers `decision_required`, `decision_acknowledged`, `lifecycle_transition` event types; schema-version bump).
2. process-lifecycle.md second (PL-006d refs the EV schema extension; PL-019 refs cognition-loop.md).
3. cognition-loop.md third (NEW; depends on EV + PL already-edited).
4. execution-model.md fourth (independent; any order).
5. handler-contract.md fifth (HC-064..067 refs EV lifecycle_transition registration; last).

## Changelog Verification
`05-changelog.md` rollup matches the four drafts in `05-spec-drafts/` 1:1. The "pending leverage items" section called out the three vets; this integration pass folds all three (resolved → B0 + B2 + B3 + B4 + B5 above). 

## Final Assessment
The corpus is internally consistent after this change and across the existing specs. One atomic bundle (B0) lands all spec text together; the schema-version bump on event-model.md is the only cross-spec coordination concern (additive per EV §6.4, no breaking change). The bead graph has clear dependencies: B0 first; B1/B2/B3 in parallel after B0; B4/B5/B6 after their phase-1 dependencies; B7/B8/B9 after B0 (B7 can run alongside phase-2); B10/B11 last. The flywheel becomes operationally testable at B11. **Ready to advance to tasks.**

## Open questions that survived the integration pass
- **OQ-CL-002.** `harmonik digest` ships as a subcommand. **B1's acceptance criteria pin this answer for v0.1.**
- **OQ-CL-003.** Substrate is Pi TS for v0.1 (Rust port shake-down deferred). Recorded; unblocked.
- **OQ-CL-004.** Cognition-loop's own events go to `.harmonik/cognition/cognition-events.jsonl` at v0.1. **B7's acceptance criteria pin this.**
- **OQ-CL-005.** Loop does NOT wake on `reviewer_verdict{APPROVE}`. **B7's wake-filter table pins this.**

No NEW open questions surfaced.
