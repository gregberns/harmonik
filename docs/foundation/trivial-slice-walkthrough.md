# Trivial-Slice Paper Walkthrough

**Date:** 2026-05-09
**Bead:** `hk-kle6.1`
**Status:** v0.1 — initial walkthrough
**Purpose:** Paper dry-run of the trivial non-agentic slice against the 345-bead bootstrap subset. Maps each of the ~25 atomic runtime operations to at least one owning bootstrap bead. Operations with no bead owner are surfaced as corpus gaps.

> This is the "validated" criterion at the Phase-0/Phase-1 boundary per `phase-1-readiness-gap-analysis.md` §E. A dry-run walkthrough — cheaper than running code (the daemon doesn't exist yet) and cheaper than `br dep cycles` (which only catches one bug class).

---

## §1. Trivial slice definition

```
Input:    one hand-authored "hello" bead (bead_id = hello-001) marked open/dispatchable
Workflow: a 1-node DOT with kind:non-agentic that emits an event and commits
Expected: one git checkpoint commit
          + one terminal event in JSONL (run_completed)
          + bead closed (Beads status = done)
```

This is working-definition item 2 from `bootstrap-subset.md` §1: "Accept one trivial bead (e.g. `kind:non-agentic` no-op) via `br`, resolve it to a static linear DOT workflow (1–2 nodes), and execute end-to-end." Item 3 (twin handler subprocess) is NOT exercised by this slice — that is the follow-up cycle.

**No reconciliation:** the slice runs on a clean daemon start with no prior state. Cat 0 pre-check fires and finds no stale leases; Cat 5 clean-restart is not needed. RC beads are dormant this run.

---

## §2. Operation table — 25 atomic operations

Operations are ordered by the runtime sequence: daemon startup → bead intake → workspace acquisition → workflow execution → event capture → checkpoint commit → bead close. Each row lists the operation, the owning bootstrap bead(s), and a brief rationale.

### Group A — Daemon startup (7 operations)

| # | Operation | Owning bead(s) | Notes |
|---|---|---|---|
| A1 | Write pidfile at `.harmonik/daemon.pid`; acquire `flock` | `hk-8mup.3` (PL-002) · `hk-8mup.4` (PL-002a) · `hk-8mup.5` (PL-002b) | Atomic truncate-rewrite-keep-fd; uniqueness invariant. |
| A2 | Open socket at `.harmonik/daemon.sock` (mode 0600); configure JSON-RPC over NDJSON | `hk-8mup.6` (PL-003) · `hk-8mup.7` (PL-003a) | Wire format for agent ↔ daemon communication. |
| A3 | Run orphan sweep (empty on first start) | `hk-8mup.11` (PL-006) · `hk-8mup.12` (PL-006a) · `hk-8mup.13` (PL-007) | Deterministic; completes before reconciliation. |
| A4 | Run Cat 0 pre-check: `br --version`, `git rev-parse`, `.harmonik/` writable | `hk-8mup.10` (PL-005 step 4) · `hk-872.26` (BI-024a `br --version`) · `hk-63oh.62` (RC Cat 0 taxonomy) · `hk-63oh.16` (RC-012) | Cat 0 finds no failure; daemon proceeds. BI-024a verifies Beads CLI version; ON-016 verifies queue schema version. |
| A5 | Verify queue schema version (ON-016) | `hk-sx9r.20` (ON-016) | Blocks dispatch on Beads/harmonik schema mismatch. |
| A6 | Instantiate cross-subsystem registries at composition root | `hk-8mup.33` (PL-020a) | Handler registry, event registry, control-point registry (empty). |
| A7 | Emit `daemon_started` then `daemon_ready`; transition to `ready` state | `hk-8mup.16` (PL-009 ready criteria + `daemon_ready` emission) · `hk-hqwn.59.57` (EV `daemon_started`) · `hk-hqwn.59.58` (EV `daemon_ready`) | Ready-state transition. PL-009 waits for this event before accepting work. |

### Group B — Bead intake (3 operations)

| # | Operation | Owning bead(s) | Notes |
|---|---|---|---|
| B1 | Query `br ready` to discover dispatchable beads | `hk-872.13` (BI-013 `br ready` read surface) | Dispatch-loop input; daemon polls or receives notification. |
| B2 | Read bead detail for `hello-001` | `hk-872.15` (BI-015 bead-detail query) | Loads bead content, labels, dependencies. |
| B3 | Atomic claim: transition bead from `open` → `in_progress` | `hk-872.9` (BI-009 atomic-claim invariant) · `hk-872.10` (BI-010 terminal-transition write) · `hk-872.12` (BI-012 route every write through adapter) | The dispatch invariant. After this point the bead is owned by this run. |

### Group C — Workspace acquisition (4 operations)

| # | Operation | Owning bead(s) | Notes |
|---|---|---|---|
| C1 | Resolve `run_id` (UUIDv7) and create Workspace record | `hk-b3f.13` (em-013 run_id) · `hk-8mwo.3` (wm-001 Workspace record) · `hk-8mwo.7` (wm-004 workspace_id = "ws-" + run_id) | run_id is the join key across git/Beads/JSONL. |
| C2 | Create git worktree at `.harmonik/worktrees/<run_id>/` on branch `run/<run_id>` | `hk-8mwo.4` (wm-002 canonical path) · `hk-8mwo.5` (wm-003 `git worktree add -b`) · `hk-8mwo.8` (wm-005 `run/<run_id>` branch) | Git ≥ 2.34 required (wm-env-002). |
| C3 | Write lease-lock file (JSON + atomic write+fsync) | `hk-8mwo.19` (wm-013a canonical lease-lock path + JSON + atomic write) | Lease held by run, not agent. |
| C4 | Write `.gitignore` hygiene; emit `workspace_created` then `workspace_leased` (4-step ordering) | `hk-8mwo.23` (wm-013e `.gitignore` write) · `hk-8mwo.26` (wm-016 4-step ordering) · `hk-hqwn.59.37` (EV `workspace_created`) · `hk-hqwn.59.38` (EV `workspace_leased`) | The 4-step ordering: worktree → branch → sidecar → lock. Sidecar write (`hk-8mwo.38`/`.39`) precedes emission. |

### Group D — Workflow execution (4 operations)

| # | Operation | Owning bead(s) | Notes |
|---|---|---|---|
| D1 | Resolve workflow: load 1-node DOT for `hello-001`; run pre-run validator | `hk-b3f.1` (em-001 Workflow record) · `hk-b3f.51` (em-038 pre-run validator) · `hk-b3f.52` (em-039 validator is mechanism-tagged) | Validator rejects malformed DOT and missing handler_refs before dispatch. |
| D2 | Emit `run_started`; start State/Transition record lifecycle | `hk-b3f.16` (em-015a run_started emission) · `hk-b3f.3` (em-003 State) · `hk-b3f.4` (em-004 Transition) · `hk-hqwn.59.1` (EV `run_started`) | First event after atomic-claim. |
| D3 | Execute non-agentic node: outcome spine threading (pass-through hook/gate); produce `kind=default` Outcome | `hk-b3f.35` (em-027 outcome spine threading) · `hk-b3f.5` (em-005 Outcome + kind discriminator) · `hk-b3f.54` (em-041 deterministic edge-selection cascade) | No CP gate nodes; cascade evaluates the single outgoing edge. Handler_ref not invoked (non-agentic). |
| D4 | Emit `outcome_emitted`; evaluate edge selection; enter terminal state | `hk-hqwn.59.8` (EV `outcome_emitted`) · `hk-b3f.18` (em-015c terminal-state detection rule) | outcome_emitted drives the orchestrator's next-node decision; terminal-state detection closes the main loop. |

### Group E — Event capture (3 operations)

| # | Operation | Owning bead(s) | Notes |
|---|---|---|---|
| E1 | JSONL Emit: redact → fsync-append to `.harmonik/events/events.jsonl` → sync-dispatch | `hk-hqwn.19` (EV-014a dispatch semantics) · `hk-hqwn.23` (EV-015 JSONL at `.harmonik/events/events.jsonl`) · `hk-hqwn.24` (EV-016/016a durability class + per-class fsync) | Every event in this slice has durability class `F` (durable) or `O` (observable). |
| E2 | Validate envelope fields: `event_id` (UUIDv7), `source_subsystem`, `timestamp_wall`, `schema_version` | `hk-hqwn.1` (EV-001 common envelope) · `hk-hqwn.2` (EV-002 UUIDv7) · `hk-hqwn.3` (EV-002a monotonic within process) · `hk-hqwn.41` (EV-032 tagged-union envelope + payload-constructor registry) | Type dispatch deterministic on `type` field (EV-033). |
| E3 | Deliver `run_completed` event; terminate bus dispatch for this run | `hk-b3f.17` (em-015b run_completed emission) · `hk-hqwn.59.2` (EV `run_completed`) | Terminal event pair (`run_completed`). `run_failed` is `hk-hqwn.59.3` — not fired on happy path. |

### Group F — Checkpoint commit (2 operations)

| # | Operation | Owning bead(s) | Notes |
|---|---|---|---|
| F1 | Squash-merge worktree branch into `harmonik/integration` via scratch merge-worktree; write structured trailers | `hk-8mwo.29` (wm-019 squash-merge + trailers + author/committer split) · `hk-8mwo.30` (wm-019a scratch merge-worktree 7-step lifecycle) · `hk-b3f.20` (em-017 structured trailers) · `hk-b3f.85` (em-schema.checkpoint-trailers 7-key registry) | Trailers required: `Harmonik-Run-ID`, `Harmonik-Workflow-ID`, `Harmonik-Node-ID`, `Harmonik-Bead-ID` (conditional), plus actor/committer split. |
| F2 | Emit `transition_event` + `checkpoint_written` (in emission-ordering contract); emit `workspace_merge_status` | `hk-b3f.33` (em-025a emission ordering: update-ref → transition event → checkpoint-written → state-entered) · `hk-hqwn.59.6` (EV `transition_event`) · `hk-hqwn.59.7` (EV `checkpoint_written`) · `hk-8mwo.32` (wm-021 `workspace_merge_status` emission) · `hk-hqwn.59.39` (EV `workspace_merge_status`) | Without emission ordering, observers see ghost commits. |

### Group G — Bead close + lease release (2 operations)

| # | Operation | Owning bead(s) | Notes |
|---|---|---|---|
| G1 | Close bead: transition `in_progress` → `done` via adapter; propagate bead_id through run metadata, checkpoint trailer, event payload, session-log | `hk-872.10` (BI-010 terminal-transition write `close`) · `hk-872.18` (BI-017 bead_id → run metadata) · `hk-872.19` (BI-018 bead_id → checkpoint trailer) · `hk-872.20` (BI-019 bead_id → event payload) · `hk-872.21` (BI-020 bead_id → session-log metadata) | Byte-equal propagation across four sinks is the BI-017..020 contract. |
| G2 | Release lease-lock: atomic delete + `git worktree remove`; emit no further workspace events | `hk-8mwo.20` (wm-013b lease release on terminal transitions) · `hk-8mwo.27` (wm-018 merge-back inside same lease) | Lease released after merge-back completes. Orphan sweep on next restart will find nothing to clean. |

---

## §3. Bead coverage summary

Total distinct bootstrap beads exercised in the trivial slice: **55** (across all clusters).

| Cluster | Bootstrap beads exercised | Selected (representative) |
|---|---|---|
| PL (`hk-8mup`) | 8 | `.3`, `.4`, `.5`, `.6`, `.7`, `.10`, `.11`, `.12`, `.13`, `.16`, `.33` |
| BI (`hk-872`) | 10 | `.9`, `.10`, `.12`, `.13`, `.15`, `.18`, `.19`, `.20`, `.21`, `.26` |
| WM (`hk-8mwo`) | 10 | `.3`, `.4`, `.5`, `.7`, `.8`, `.19`, `.20`, `.23`, `.26`, `.27`, `.29`, `.30`, `.32`, `.38`, `.39` |
| EM (`hk-b3f`) | 13 | `.1`, `.3`, `.4`, `.5`, `.13`, `.16`, `.17`, `.18`, `.20`, `.33`, `.35`, `.51`, `.52`, `.54`, `.85` |
| EV (`hk-hqwn`) | 13 | `.1`, `.2`, `.3`, `.19`, `.23`, `.24`, `.41`, `.59.1`, `.59.2`, `.59.6`, `.59.7`, `.59.8`, `.59.37`, `.59.38`, `.59.39`, `.59.57`, `.59.58` |
| RC (`hk-63oh`) | 2 | `.62`, `.16` — Cat 0 pre-check fires and finds nothing; Cat 5 not needed |
| ON (`hk-sx9r`) | 2 | `.20` (queue-schema version check) |
| HC (`hk-8i31`) | 0 | Non-agentic node: no handler spawned, no twin invoked |
| AR (`hk-zs0`) | 0 | Structural conformance; no sensor beads exercised at runtime |
| SH (`hk-i0tw`) | 0 | Scenario harness not driven by this dry-run |
| CP (`hk-a8bg`) | 0 | Fully deferred; no gate nodes in 1-node DOT |

**HC is zero** because the trivial slice uses `kind:non-agentic` — no twin binary is spawned. HC's 46 bootstrap beads become essential for the follow-up cycle (§1 item 3).

---

## §4. "No owner" findings

Operations in §2 that could not be traced to a bootstrap bead at write time of this document.

### Gap 1: Session-log sidecar atomic write for non-agentic node

**Operation:** `hk-8mwo.38` (wm-026 `harmonik.meta.json` atomic write) and `hk-8mwo.40` (wm-028 `bead_id` propagates into session metadata) are in the bootstrap subset. However, the non-agentic execution path does not spawn an agent session, so whether `harmonik.meta.json` is written by a non-agentic node is ambiguous in the current spec.

**Resolution:** The sidecar write belongs to the workspace lifecycle regardless of node type — `wm-025`/`wm-026` do not restrict to agentic nodes. The beads are correctly included; the walkthrough is not a gap. Flag as implementation-guidance: the non-agentic compositor SHOULD still write a minimal sidecar so the workspace teardown path is uniform.

### Gap 2: `state_entered` / `state_exited` event types

**Operation:** D2 (run_started) and D4 (terminal-state detection) implicitly traverse the State machine, which per EM §4.4 may emit `state_entered` and `state_exited` events.

**Current status:** `ev-bootstrap.md` marks these as AMBIGUOUS (§7). They are not in the 16 §8 row INCLUDE beads enumerated for EV. If the EM implementer determines these events are emitted for the linear 1-node DOT walk, the missing §8 row beads need to be PULL_INed.

**Finding:** Soft gap. If the EM implementation emits `state_entered`/`state_exited` for non-agentic transitions, add `hk-hqwn.59.4` and `hk-hqwn.59.5` to the bootstrap INCLUDE set via `br update --add-label scope:bootstrap`. This bead body surfaces to `hk-kle6.2` (corpus label reconciliation) for follow-up.

### Gap 3: Daemon command surface invocation

**Operation:** The trivial slice assumes the daemon is started via a command (e.g., `harmonik start`). The command-surface bead `hk-8mup.43` (PL-028, 8 entry points) is in the bootstrap INCLUDE set. However, no bead is found that owns the specific entrypoint used by the test harness to wait for `daemon_ready` (the PL-009b ready-protocol surface, `hk-8mup.18`).

**Status:** `hk-8mup.18` IS in the bootstrap INCLUDE set (`hk-8mup.18` — Ready-protocol surface for external callers — 3 mechanisms). No gap; this was a write-time oversight in listing. Both `.43` and `.18` are covered.

### Gap 4: `node_dispatch_requested` event

**Operation:** Between D1 (workflow resolved) and D2 (run_started), the spec may require a `node_dispatch_requested` event to be emitted before a non-agentic node runs.

**Current status:** `ev-bootstrap.md` §3 (§8 row children — non-bootstrap) includes `node_dispatch_requested` in the "§8.1.9–.11" exclude group, classifying it as sub-workflow or reconciliation-origin dispatch. If this event is required for the linear single-node happy path, it is a corpus gap: no `scope:bootstrap` bead exists for `hk-hqwn.59.*` covering `node_dispatch_requested`.

**Finding:** Potential gap. Implementers of the non-agentic execution path should verify whether EM-038 (the pre-run validator) or EM-015a (run_started) is preceded by `node_dispatch_requested`. If yes: surface to `hk-kle6.2`; add the corresponding §8 row bead to `scope:bootstrap`.

---

## §5. Dependency-closure assertion

The 55 beads exercised by this slice are a proper subset of the 345 `scope:bootstrap` beads. Spot-check the critical chain:

```
PL startup (A1..A7)
  → BI read surface (B1..B3)   depends on: hk-8mup.10 → hk-872.13
  → WM worktree (C1..C4)       depends on: hk-8mup.10 step 4 / BI read
  → EM execution (D1..D4)      depends on: WM worktree path / BI claim
  → EV bus (E1..E3)            depends on: EM emit surface
  → F checkpoint (F1..F2)      depends on: WM merge-back + EM trailer schema
  → G bead close (G1..G2)      depends on: BI write surface + WM lease release
```

All dependency edges in this chain resolve to beads already in `scope:bootstrap`. No PULL_IN addition is required for the non-agentic trivial slice. (HC beads are not in this chain; they become necessary for the twin-handler follow-up cycle.)

---

## §6. Validation conclusion

**This walkthrough confirms:** the 345-bead bootstrap subset is operationally closed for the trivial non-agentic slice. Every runtime operation in the sequence has at least one owning bootstrap bead. The two soft gaps (§4, Gaps 2 and 4) are contingent on EM implementation choices and should be resolved when the EM executor is authored.

**What this does NOT confirm:**

- Twin-handler slice (§1 item 3) — HC cluster (46 beads) is not exercised here.
- Crash-recovery slice (§1 item 4) — Cat 5 + WM orphan-sweep are not triggered by a clean first run.
- Scenario-harness validation — SH cluster (54 beads) drives the conformance net; this paper walkthrough is a prerequisite check, not a substitute.

**Phase-1 entry gate status (§E from `phase-1-readiness-gap-analysis.md`):** this document satisfies gate item 4 ("Trivial-slice paper walkthrough authored, mapping the 25-op walk to the bootstrap beads, surface any 'no owner' findings"). Two soft gaps surfaced; neither is a blocker for Phase-1 entry.

---

## §7. Companion bead

`hk-kle6.2` — corpus label reconciliation — is the follow-up bead that should address:
- Gaps 2 and 4 from §4 above (contingent on EM implementation choices).
- The 527 untagged beads (§A2 of `phase-1-readiness-gap-analysis.md`).

---

## §8. Revision history

- **v0.1 (2026-05-09).** Initial paper walkthrough. 25 atomic operations across 7 groups; 55 bootstrap beads exercised; 2 soft gaps surfaced (state_entered/state_exited ambiguity; node_dispatch_requested contingency). 2 write-time false alarms resolved inline (session-log sidecar, daemon command surface).
