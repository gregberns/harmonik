# Lens 2 — `GatherDrainFacts` fact-bundle schema (P1-a, bead hk-pfr4)

**Codename:** `fleet-state` · **Date:** 2026-06-20 · **Lens:** the read-only facts tool that replaces the `GenuineDrain` DRAINED control-signal.

This lens specifies the typed fact bundle that the captain (LLM) reads instead of the oracle deciding. The bundle reports facts per axis as **counts + lists** — it never short-circuits at the first sign of work and never collapses to a single DRAINED boolean. The decision ("therefore sleep") moves to the captain; Go keeps the facts as a tool and a one-directional veto.

---

## 1. The current oracle, and the 5 false-negative defenses it MUST preserve

Source: `internal/daemon/draindetect.go` (`DrainDetector.GenuineDrain`) + `internal/daemon/draindetect_epic.go` (`scanOpenEpics`) + `internal/brcli/ready.go` (`Ready` vs `ReadyAll`).

`GenuineDrain` returns a tri-state verdict `DrainResult{State, Reasons}` where `State ∈ {DRAINED, HAS_WORK, UNSURE}`. It is **fail-closed**: DRAINED requires positive emptiness on EVERY axis; the moment any axis sees work it flips to HAS_WORK; any read error or transient race returns UNSURE. Only DRAINED licenses sleep today (`draindetect.go:60-78`).

The 5 defenses (documented at `draindetect.go:34-48`), each a concrete check that the new shape must keep:

| # | Defense | Where (file:line) | What it does |
|---|---|---|---|
| 1 | **br-ready pagination** | `draindetect.go:184` calls `d.ready.ReadyAll` → `brcli/ready.go:112-114` (`br ready --limit 0`) | NEVER trusts the default-paginated `Ready()` (`ready.go:87-92`, capped at 20). A paginated empty is not evidence of no work. |
| 2 | **ledger-dep / epic gating** | queue portion `draindetect.go:234-236` (`ItemStatusDeferredForLedgerDep`); epic portion `draindetect_epic.go:47-100` (`scanOpenEpics` via `ledger.BlocksEdge`) | A queue item deferred-for-ledger-dep ⇒ work; an OPEN epic that blocks a non-terminal child ⇒ work (the child reads "blocked" while the epic is open, so `br ready` cannot see it). |
| 3 | **paused-by-failure / failed-archive** | `draindetect.go:155-162` (`failedArchives` glob `*.json.failed-*`) + `draindetect.go:211-216` (paused-by-failure / -drain / -budget queue statuses) | Any paused-by-* queue ⇒ work; an un-reconciled `.json.failed-*` archive on disk ⇒ work, scanned DIRECTLY (bypassing the archive filter). |
| 4 | **in-flight runs** | `draindetect.go:172-181`: `d.runs.Len() > 0` OR `liveWorktrees() > 0` (entries under `.harmonik/worktrees/*`) | A non-empty RunRegistry or a live worktree ⇒ a run is in flight. |
| 5 | **kerf-next-empty** | `draindetect.go:48` (policy, by omission) + `draindetect_epic.go:44-46` | The oracle MUST NOT consult `kerf next` (false-empty for works lacking a `bead_filter`). `br ready --limit 0` + the ledger are authoritative. |

Plus the **UNSURE-on-doubt** discipline: the QM race ("all items terminal but queue status not yet rolled") at `draindetect.go:246-248`, an unrecognised queue status at `draindetect.go:222-224`, a nil epic seam at `draindetect_epic.go:48-53`, and every read error → UNSURE.

**Note on what the oracle DROPS (the bug the new shape fixes):** `GenuineDrain` only inspects the *open-epic-blocked-child* slice of the backlog and the dispatchable-now `br ready` slice. It never enumerates `in_progress`, `draft`, `deferred`, or standalone-blocked beads as their own axes, and it has **no generative axis** (childless open epics). Per the `br ready` investigation (scaffold §"`br ready` findings"), those dropped buckets are exactly what makes "empty `br ready`" a false drain signal. The fact bundle adds them as first-class axes.

---

## 2. The proposed Go type

`GatherDrainFacts(ctx) (*FleetFacts, error)` replaces `GenuineDrain`. It returns the bundle below — counts + lists per axis, never a single verdict. The 5 defenses survive as the **sourcing** of each axis (see §3); they are no longer collapsed into a DRAINED/HAS_WORK boolean.

```go
// FleetFacts is the read-only fact bundle the captain reads to decide whether
// to wind the fleet down. It reports facts per axis as counts + lists; it never
// renders a DRAINED/HAS_WORK decision and never short-circuits at the first
// sign of work. The ZFC invariant (§4): facts, never a decision; generative
// categories are flagged, not scored. Produced by GatherDrainFacts.
type FleetFacts struct {
	// ---- Dispatchable-now (defense #1, #5: br ready --limit 0) ----
	Ready BeadAxis `json:"ready"`

	// ---- In-flight work (defense #4) ----
	InProgress BeadAxis `json:"in_progress"` // beads the ledger reports in_progress
	Runs       RunAxis  `json:"runs"`        // RunRegistry + live worktrees

	// ---- Lined-up / queued-but-not-yet-dispatchable (defense #2 queue, #3) ----
	Queued QueueAxis `json:"queued"`

	// ---- Standalone blocked-by-an-open-epic (defense #2 epic) ----
	BlockedByOpenEpic []EpicBlockEdge `json:"blocked_by_open_epic"`

	// ---- The other dropped buckets (not dispatchable, not lost) ----
	NeedsAttention BeadAxis `json:"needs_attention"` // needs-attention label set
	Draft          BeadAxis `json:"draft"`           // loaded-but-not-dispatchable
	Deferred       BeadAxis `json:"deferred"`        // operator-deferred

	// ---- The ONE generative category: flagged, never scored ----
	NeedsDecomposition []core.BeadID `json:"needs_decomposition"` // childless OPEN epics

	// ---- Read quality (NOT a control signal) ----
	// Unsure is true when any axis hit a read error or a transient
	// inconsistency (e.g. the QM "all items terminal, status not yet rolled"
	// race, or an unrecognised queue status). It is a data-quality caveat the
	// captain weighs, NOT a verdict and NOT a license/veto. UnsureReasons
	// carries the diagnostic detail.
	Unsure        bool     `json:"unsure"`
	UnsureReasons []string `json:"unsure_reasons,omitempty"`

	// GatheredAt is the wall-clock read time, for staleness reasoning by the
	// captain / the P2 fold.
	GatheredAt time.Time `json:"gathered_at"`
}

// BeadAxis is one bead bucket: a count plus the bead ids that compose it.
// Count == len(Beads) by construction; both are emitted so a JSON consumer can
// read the count without walking the list.
type BeadAxis struct {
	Count int           `json:"count"`
	Beads []BeadFact    `json:"beads"`
}

// BeadFact is the minimal per-bead fact (id + human context). It is a
// projection of core.BeadRecord — id + title + type + the labels that explain
// why the bead landed in its axis (e.g. needs-attention).
type BeadFact struct {
	ID     core.BeadID `json:"id"`
	Title  string      `json:"title"`
	Type   string      `json:"type"`
	Labels []string    `json:"labels,omitempty"`
}

// RunAxis is the in-flight-runs axis (defense #4): registry runs + live
// worktree dirs, reported separately so a stale-worktree-with-empty-registry
// case is legible.
type RunAxis struct {
	RegistryCount  int      `json:"registry_count"`  // RunRegistry.Len()
	LiveWorktrees  int      `json:"live_worktrees"`  // entries under .harmonik/worktrees
	WorktreePaths  []string `json:"worktree_paths,omitempty"`
}

// QueueAxis is the lined-up / queued-but-not-dispatchable axis (defenses #2
// queue-portion, #3). Each entry is a queue with its status and the non-terminal
// items keeping it from drained. PausedQueues and FailedArchives are the
// defense-#3 hold-the-fleet-awake signals reported as facts.
type QueueAxis struct {
	NonTerminalItems []QueueItemFact `json:"non_terminal_items"`
	PausedQueues     []PausedQueue   `json:"paused_queues"`    // paused-by-failure/-drain/-budget
	FailedArchives   []string        `json:"failed_archives"`  // un-reconciled *.json.failed-*
	Count            int             `json:"count"`            // total non-terminal items
}

// QueueItemFact is one non-terminal queue item: which queue, which bead, and
// its item status (pending / dispatched / deferred-for-ledger-dep / …).
type QueueItemFact struct {
	Queue   string `json:"queue"`
	BeadID  string `json:"bead_id"`
	Status  string `json:"status"`
}

// PausedQueue names a paused queue and the pause reason (the QueueStatus value).
type PausedQueue struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// EpicBlockEdge is one open-epic → otherwise-ready-child blocks edge
// (defense #2 epic-portion). The child is genuine pending work `br ready`
// cannot see.
type EpicBlockEdge struct {
	EpicID  core.BeadID `json:"epic_id"`
	ChildID core.BeadID `json:"child_id"`
}
```

**What changed vs `DrainResult`:** `DrainResult{State, Reasons}` (the verdict + diagnostic-only reasons) is gone. There is no `State` field and no `DrainState` enum to key control flow off. The `flagWork`/`merge`/`unsure` accumulator helpers are replaced by per-axis appends. `UNSURE` is demoted from a verdict to the `Unsure bool` read-quality flag (it no longer keeps the fleet awake — it caveats the read).

---

## 3. Per-axis sourcing (where each fact comes from; defenses preserved)

| Axis | Source | Defense preserved |
|---|---|---|
| `Ready` | `brcli.Adapter.ReadyAll(ctx)` — `br ready --limit 0 --sort priority` (`ready.go:112`). Apply the same needs-attention exclusion it already does (`ready.go:167-180`). | **#1** (never paginated `Ready()`), **#5** (no `kerf next`). |
| `InProgress` | `brcli.Adapter.ListBeadsByStatus(ctx, "in_progress")` (`listbystatus_em031a.go:41`, runs `br list --status in_progress --json`). | Adds the bucket the oracle DROPS. |
| `Runs` | `RunRegistry.Len()` (`runregistry.go:148`) + `liveWorktrees()` scan of `.harmonik/worktrees/*` (`draindetect.go:272`). | **#4** verbatim. |
| `Queued` | `QueueStore.AllQueues()` (`queuestore_hkj808w.go:199`) walked exactly as `scanQueues` does (`draindetect.go:205-251`): paused-by-* statuses → `PausedQueues`; non-terminal items (incl. `deferred-for-ledger-dep`) → `NonTerminalItems`; `failedArchives()` glob → `FailedArchives`. | **#2** (queue ledger-dep portion), **#3** (paused + failed-archive). |
| `BlockedByOpenEpic` | `scanOpenEpics` logic (`draindetect_epic.go`): open epics ∩ `ledger.BlocksEdge` over `open ∪ blocked` children. Emit ALL edges (don't short-circuit at the first — §4 below). | **#2** (epic portion). |
| `NeedsAttention` | `ListBeadsByStatus(ctx, "open")` (or "blocked") filtered to beads carrying the `needs-attention` label (`brcli/ready.go:43` `labelNeedsAttention`). | Adds a dropped bucket; makes the exclusion *visible* instead of silent. |
| `Draft` | `ListBeadsByStatus(ctx, "draft")` (`core.CoarseStatusDraft`, `coarsestatus.go:17`). | Adds a dropped bucket. |
| `Deferred` | `ListBeadsByStatus(ctx, "deferred")` (`core.CoarseStatusDeferred`, `coarsestatus.go:16`). | Adds a dropped bucket. |
| `NeedsDecomposition` | open epics from `ListBeadsByStatus(ctx, "open")` filtered to `BeadType == "epic"` (`draindetect_epic.go:26` `beadTypeEpic`) that have **no** child edges (no `BlocksEdge` to any bead). The generative category. | New — the ONE axis handed to the captain, not scored. |
| `Unsure` / `UnsureReasons` | Set on any axis read error OR the transient races (`draindetect.go:222-224` unrecognised status, `:246-248` QM race, `draindetect_epic.go:48-53` nil seam). | UNSURE discipline preserved, **demoted to a flag**. |

Sourcing uses the **same `DrainDetector` seams** already wired (`readySource`, `openBeadLister`, `queue.BeadLedger`, `*RunRegistry`, `*QueueStore`, `projectDir` — `draindetect.go:106-141`). No new dependency surface; `NewDrainDetector` constructs the same value. The needs-decomposition axis reuses the `lister` + `ledger` seams already present for the epic axis.

---

## 4. The ZFC invariant (what makes this clean)

> **`GatherDrainFacts` reports facts, never a decision.** The bundle never collapses to a single DRAINED boolean; it enumerates every axis as counts + lists without short-circuiting; generative categories are flagged for the captain, never scored.

Three concrete obligations the implementation must satisfy:

1. **No verdict field.** `FleetFacts` has no `State`/`DrainState`/`Drained bool`. Whether the fleet is "drained enough to sleep" is the captain's judgment (ZFC: completion-detection-as-decision is the model's, not the framework's — `zero-framework-cognition.md:32`). Go's only decision authority is the **veto-on-execute** in P1-c (`harmonik sleep` re-runs the facts and refuses if any axis shows dispatchable/in-flight work) — that is policy *enforcement* of a deterministic rule, not a sleep *decision*.
2. **No short-circuit.** Each axis emits its full count + list. The old `GenuineDrain` returned the moment one axis saw work (`flagWork` → HAS_WORK; `scanOpenEpics` returns on the first edge — `draindetect_epic.go:92-96`). The bundle must walk every axis to completion so the captain sees the whole picture (5 in-progress + 3 paused queues, not "HAS_WORK, stop reading"). This is the explicit scaffold requirement: *"counts + lists (NOT short-circuit at first sign of work)."*
3. **Generative ≠ scored.** `NeedsDecomposition` (childless open epics) is the one bucket the oracle is structurally blind to (synthesis §1: the captain *generates* work — decompose an epic — that is not yet a ready bead). It is emitted as a flagged list of epic ids, with **no priority, no rank, no "should-decompose" score**. The captain decides what to do with it.

`Unsure` stays on the right side of the line: it is a **read-quality caveat** (the data may be stale/inconsistent), not a control signal. It does not keep the fleet awake and does not veto sleep — it is a fact about the *read*, which the captain weighs.

---

## 5. How P2-a (`harmonik state`) and P2-b (the fold) consume the bundle

**P2-a — `harmonik state [--json]` (bead hk-gv04, dep hk-pfr4):**
`GatherDrainFacts` is the work-backlog section of the aggregator. `harmonik state --json` emits `FleetFacts` verbatim under a `drain_facts` (or `work`) key, unioned with the live-session / queue / run readers the command also fuses (the scaffold notes `captain-boot-digest.sh` already does ~half). The captain reads this one typed snapshot instead of running `br ready`, `br list`, and eyeballing queues — the highest-value ZFC "bone" (synthesis §3.1). Because the bundle is counts+lists, the human-readable mode can print, e.g.:
```
ready: 4   in_progress: 2   queued: 1 (1 paused-by-failure)
blocked-by-open-epic: 3   needs_decomposition: 1 epic (hk-xxxx)
[unsure: paused queue status not yet rolled]
```

**P2-b — the system-state fold (bead hk-w6q7, dep hk-pfr4):**
The ~50-line fold rolls per-session FSMs + `QueueStore` + `RunRegistry` into the labels **PROCESSING / WAITING / DRAINING / INACTIVE**. `FleetFacts` is the backlog input to that fold:
- `Ready.Count > 0` OR `Queued.Count > 0` with active runs ⇒ **PROCESSING**.
- `Runs.RegistryCount > 0` but `Ready.Count == 0` ⇒ **DRAINING** (finishing in-flight, nothing new to start).
- All bead axes empty AND `Runs` empty AND no paused/failed/blocked-epic ⇒ candidate **INACTIVE** — but the fold computes a *label*, it does NOT decide to sleep; the label gates which polls run (INACTIVE ⇒ stop run-watchers/gauges).
- `Unsure == true` ⇒ the fold MUST NOT emit INACTIVE (a doubtful read can't justify standing down watchers); it holds at WAITING/DRAINING.

The fold consumes facts and emits a *label*; the captain consumes the label + facts and emits a *decision*. The DRAINED control signal that `GenuineDrain` used to emit is gone from both consumers — exactly the decision-locus correction this initiative is about.

---

## Tricky-to-preserve flags (for the synthesizer)

- **Defense #2 epic-portion + the new `NeedsDecomposition` axis both walk open epics** — same `ListBeadsByStatus("open")` + `BlocksEdge`, but with OPPOSITE predicates (epic *has* a blocked child vs epic has *no* children). Compute both in one epic pass to avoid double br/ledger round-trips; the `scanOpenEpics` short-circuit-on-first-edge (`draindetect_epic.go:92`) must be REMOVED so all edges are emitted AND so the no-children determination is exhaustive.
- **Defense #3 failed-archive scan bypasses the archive filter** (`draindetect.go:258-265`, glob directly, NOT `EnumerateQueueNames`). The fact bundle must keep the direct glob; a naive reimplementation via the normal queue enumeration silently drops failed archives and re-opens a false-drain hole.
- **`Unsure` demotion is the subtlest change.** Today UNSURE *keeps the fleet awake* (control). In the bundle it's a caveat. The safety it used to provide must be re-homed in P2-b's fold (Unsure ⇒ never INACTIVE) and P1-c's veto (a veto should arguably also refuse on Unsure, or at least surface it). Flag for the veto-lens: decide whether `--force`-less `sleep` refuses when `Unsure == true`.
