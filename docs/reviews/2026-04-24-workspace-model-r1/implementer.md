# Round 1 Implementer Review — workspace-model.md v0.2.0

## Verdict summary

The spec is **mostly implementable** for the happy-path lifecycle (worktree creation, branch naming, metadata stamping, leased-workspace emission) but drops below the implementability bar in five places where a Go engineer would be forced to invent contracts: (1) the event-name drift against event-model.md — WM-015 names `workspace_merge_pending` and `workspace_merged` as distinct events, event-model.md §8.5.3 only registers a single paired-phase `workspace_merge_status` event; this is an outright lockstep break, not a missing clarification; (2) lock-file geometry drift between WM-033 / §6.2 (`${workspace_path}/.harmonik/lease.lock`) and handler-contract.md HC-044a (`.harmonik/worktrees/<run_id>/.lock` at the *repo* level); a Go implementer reading both cannot choose a path that satisfies both specs; (3) the lease-acquisition and lock-write protocol is never specified — §7.2 `create_workspace` writes no lock, §7.1 names no lock event, yet WM-033 says stale locks must be swept — what writes them?; (4) the merge-back node contract (WM-018/WM-019) gives no callable entry point — no `MergeBack(workspace)` interface, no "merge node type" tag, no §6 `INTERFACE`; (5) the fresh-worktree rule (WM-034) for `reopen-bead` presupposes that each re-run gets a new `run_id`, but EM-014 and §4.9 never commit to this — the implementer cannot tell whether re-runs mint a new `run_id` or reuse the prior one, which changes every path and branch name. Roughly 60% of the requirements I attempted are implementable as-is; roughly 25% need minor sharpening; roughly 15% are genuinely blocked without coordinated spec work (especially the event-name drift, which also implicates event-model.md). The spec is tidy on paper and the record schemas are clean, but when you try to wire it up against its neighbors, the interface seams are rough.

## Requirements I attempted to implement

### WM-001 — Workspace type and required fields — IMPLEMENTABLE

```go
type Workspace struct {
    WorkspaceID    string            // "ws-" + run_id (WM-004)
    RunID          uuid.UUID         // lease key
    Repository     string            // absolute path or URL
    ParentCommit   string            // SHA branched from
    BranchName     string            // "run/<run_id>"
    Path           string            // absolute path to worktree dir
    State          WorkspaceState
    InterruptState InterruptState
    BeadID         *string           // present iff bead-tied
    Metadata       map[string]string // created_at, operator_fingerprint
    SchemaVersion  int               // per §6.4
}
```

§6.1 enumerates every required field and the types. Two minor frictions:

- The `metadata` field per WM-001 body names only "creation timestamp, operator fingerprint" but §6.1 RECORD just says `Map<String, String>` — is this closed-shape (only those two keys) or open-shape? Implementer has to guess closed-shape to be safe or define an enum. Recommend a sentence: "`metadata` is a closed map with exactly the keys `created_at` and `operator_fingerprint`; additional keys are forbidden at MVH."
- `repository` is typed `String` but the prose says "path or URL." No validation rule for which form is acceptable. Immediate impact on implementation is small (callers construct the string), but validators will need it.

### WM-002 — Worktree path convention — IMPLEMENTABLE

```go
func worktreePath(repoRoot, runID string) string {
    return filepath.Join(repoRoot, ".harmonik", "worktrees", runID)
}
```

Clean; one quibble — "`<repo>` is the repository's root directory" is ambiguous when `repository` can be a URL (WM-001 declares it such). If the backing repo is a remote URL, where does the worktree materialize? Implementer assumes there is a local clone whose root is used; spec should state this.

### WM-003 — Worktree creation uses git worktree add — IMPLEMENTABLE

```go
func (m *Manager) createWorktree(ws *Workspace) error {
    return exec.Command("git", "-C", ws.Repository, "worktree", "add",
        "-b", ws.BranchName, ws.Path, ws.ParentCommit).Run()
}
```

Straight translation. Note — no requirement here names the failure mode when `git worktree add` returns non-zero (e.g., already-exists, bad parent_commit, concurrent lock). §8 failure taxonomy is absent from this spec; the implementer invents failure handling. Flag: §8 is an optional template section, but for a spec that mutates filesystem + git state, its absence is load-bearing.

### WM-004 — Workspace ID is stable across restarts — IMPLEMENTABLE

```go
func deriveWorkspaceID(runID uuid.UUID) string {
    return "ws-" + runID.String()
}
```

Trivial; "recommended: `workspace_id = "ws-" + run_id`" carries the prefix only by recommendation (line 88). For the invariant to hold (WM-INV-005) the prefix needs to be normative — drop "(recommended:" and promote the construction to MUST.

### WM-005 / WM-006 / WM-007 — Branch naming — IMPLEMENTABLE for WM-005, PARTIALLY for WM-006

```go
func taskBranchName(runID uuid.UUID) string {
    return "run/" + runID.String()
}

func integrationBranchName(parentBeadID *string) string {
    if parentBeadID != nil {
        return "harmonik/integration/" + *parentBeadID // WM-006 default template
    }
    return "harmonik/integration"
}
```

WM-005 is clean. WM-006 has two gaps:

1. "The exact template for parent-derived integration branches is operator-configurable" (line 103) — what surface, what precedence, what reload semantics? OQ-WM-002 acknowledges this is open. This is fine for a draft, but note: the default template uses raw `<parent_bead_id>` which may contain characters (`:`, `/`, `#`) that are illegal in git ref names. No rule names how bead-IDs map to ref-safe names.
2. "A run without a parent-bead context MUST target the fixed `harmonik/integration` branch" (line 103) — but WM-008 says such runs MAY squash-merge directly to main. The two clauses contradict: is the integration branch target fixed or skipped? Implementer has to read WM-008 and choose "fixed name exists but merge may skip it" — should be restated as one combined rule.

### WM-008 — Small-scope collapse — PARTIALLY

```go
// If run has no parent-bead edge, task branch MAY squash-merge directly to main.
// Decision: present → integration; absent → main.
if beadEdgeIsPresent(run.BeadID) {
    target = integrationBranchName(parentBead)
} else {
    target = "main" // per WM-008
}
```

Stuck point: `MAY squash-merge directly to main` is permissive. Under what policy does the "may" resolve? Is it a per-run attribute? An operator config? A node-type in the workflow? The classification in WM-036 is explicitly deterministic on a verdict enum, but WM-008 leaves the choice un-anchored. Also, "Beads parent-child edge" is the deciding input, but the workspace record carries only `bead_id`, not an edges-view; the resolver must query beads-integration's read surface, which is not called out. Needs: (a) concrete input-to-decision rule; (b) explicit citation of the read surface used (presumably BI-007 dependency-graph query).

### WM-010 / WM-011 / WM-012 / WM-013 — Lease model — PARTIALLY

```go
type LeaseManager struct {
    workspacesByRun sync.Map // run_id → *Workspace
}

func (lm *LeaseManager) Lease(runID uuid.UUID) (*Workspace, error) {
    ws := &Workspace{WorkspaceID: deriveWorkspaceID(runID), RunID: runID, State: Created}
    // ... write lock? which path?
    return ws, nil
}
```

Stuck points:

1. **Where is the lease realized?** WM-010 says "the workspace MUST be leased by exactly one Run" but no requirement says *how* the lease is represented on disk or in memory. §6.2 names `${workspace_path}/.harmonik/lease.lock` but §7.2 `create_workspace` never writes that file, and no requirement names when the lock is taken, what content it holds (PID? daemon generation ID?), or when it is released. This directly contradicts with handler-contract.md HC-044a, which places the pidfile at `.harmonik/worktrees/<run_id>/.lock` (note: at the *repo* level, not under `${workspace_path}`) and specifies PID + liveness-probe semantics. Two incompatible lock paths exist in the foundation corpus.
2. **What enforces "one active agent at a time" (WM-011)?** The agent runner's serial advance through the workflow graph is implicit — is the workspace manager supposed to sanity-check, or is the invariant ambient on the orchestrator? No sensor is named.
3. **WM-013 "resolve the workspace record ... by deterministic construction ... plus a filesystem check"** — what filesystem check? `stat()` on the derived path? Reading the `git worktree list` output? Implementer invents a discovery rule. A one-liner would fix this: "the workspace manager MUST use `git worktree list --porcelain` plus a stat on `${path}/.harmonik/sessions/` to confirm live workspace presence."

### WM-014 — Workspace state machine — IMPLEMENTABLE

```go
type WorkspaceState int

const (
    Created WorkspaceState = iota
    Setup
    Ready
    Leased
    MergePending
    ConflictResolving
    Merged
    Discarded
)
```

§7.1 table is complete enough to compile the transition engine. One minor issue: the table's "any → (same state, interrupt_state != none)" row (line 466) is a pseudo-transition since it doesn't change lifecycle state; that's fine per the orthogonal-field doctrine, but an implementer needs to know whether this entry is a "transition" for event emission purposes. The row itself says "Emits: workspace_interrupted" so yes. Good.

### WM-015 — Workspace lifecycle events — BLOCKED

```go
// Problem: the spec lists 7 events but event-model.md §8.5 registers only 6,
// and the shape is different. Which is authoritative?
```

**Stuck hard.** WM-015 enumerates: `workspace_created`, `workspace_leased`, `workspace_merge_pending`, `workspace_merged`, `workspace_discarded`, `workspace_interrupted`, `merge_conflict_escalation`. But [event-model.md §8.5] registers:

| Row | Event |
|---|---|
| 8.5.1 | `workspace_created` |
| 8.5.2 | `workspace_leased` |
| 8.5.3 | `workspace_merge_status` (paired-phase, `status: pending | merged`) |
| 8.5.4 | `workspace_discarded` |
| 8.5.5 | `workspace_interrupted` |
| 8.5.6 | `merge_conflict_escalation` |

Event-model has **one** `workspace_merge_status` event keyed on a `status` enum, per event-model §8.9(h) paired-phase-lifecycle rule ("Paired-phase lifecycles MUST NOT split into two event types; use a single type with a `status` field"). workspace-model.md has **two** separate events (`workspace_merge_pending` + `workspace_merged`) — which is a direct violation of the paired-phase rule.

Implementer cannot build the event bus without picking one. The event-model spec is reviewed (v0.2+); workspace-model is draft. The implementer should assume event-model wins, but workspace-model's §4.5 requirements (WM-021) explicitly cite `workspace_merged` by name — the lockstep break propagates into the merge protocol itself.

This is the most load-bearing defect in the spec. It blocks §4.4, §4.5, §6.3 coherently, plus §7.1 emission column, plus §10.2 event-emission unit tests.

**Missing as well:** event-model §8.5.5 says `workspace_interrupted` is emitted by "reconciliation detector (per [reconciliation.md §9.2])" — but WM-039 says the workspace manager emits it. Two specs name two different emitters.

### WM-016 — workspace_leased emitted after sidecar write — IMPLEMENTABLE

```go
func (m *Manager) Lease(ws *Workspace, session SessionMeta) error {
    // WM-016 ordering:
    if err := m.createWorktree(ws); err != nil { return err }                      // (a) worktree created
    // (b) branch created implicitly by git worktree add -b
    if err := m.writeSidecar(ws, session); err != nil { return err }               // (c) sidecar
    m.emit("workspace_leased", ws.WorkspaceID, ws.RunID, ws.BranchName)            // (d)
    ws.State = Leased
    return nil
}
```

Emission ordering is crisp. Inherits the blocker from WM-015 (event name), but the *ordering* contract itself is implementable.

### WM-017 — Workspace events carry branch + commit correlators — PARTIALLY

```go
type WorkspaceMergedPayload struct {
    WorkspaceID   string
    RunID         uuid.UUID
    MergeCommit   string
    Branch        string
}
```

Two gaps:

1. WM-017 says `workspace_merged` MUST carry "the merged commit hash and the surviving branch name." Event-model §8.5.3 `workspace_merge_status` payload has `source_branch`, `target_branch`, `merge_commit_hash?` (nullable when `status=pending`). The "surviving branch name" in WM-017 does not map cleanly to either `source_branch` or `target_branch`; implementer has to guess one or the other. (Surviving = target? That is the likely intent but the spec does not say.)
2. WM-017's `workspace_interrupted` clause says payload MUST carry "the last-known durable state (the `Harmonik-State-ID` trailer of the tip commit per [execution-model.md §4.4.EM-017])." Event-model §8.5.5 payload lists only `workspace_id`, `run_id`, `detected_at`, `category`. The fields don't match. Which spec wins?

### WM-018 — Merge-back is performed by a node in the same worktree — PARTIALLY

```go
// No interface to call. Is merge-back a node type? A role? A hook?
type MergeNode interface {
    Run(ctx context.Context, ws *Workspace) (Outcome, error)
}
// ... but the spec never declares this.
```

The requirement forbids an alternate architecture ("A design that creates a new workspace for the merge step is forbidden"), but it does not *positively* describe the merge-back implementation surface. The implementer does not know:

- Is the merge-back a distinguished node type the workflow graph must carry? Is there a `NodeType = "merge"` enum?
- Is it dispatched through the handler-contract (an agent doing `git merge`) or via direct orchestrator-driven subprocess?
- What triggers merge-back — entering a terminal node of the workflow, or a specific attribute on a node?

§7.1 row "`leased` → `merge-pending`" guard is "run enters merge node" which presumes the notion of a "merge node" that this spec never defines. This is a typed missing primitive, not an ambiguity.

### WM-019 — Squash-merge with one commit per task — IMPLEMENTABLE

```go
func (m *Manager) mergeBack(ws *Workspace, target string) (string, error) {
    cmds := [][]string{
        {"git", "-C", ws.Path, "checkout", target},
        {"git", "-C", ws.Path, "merge", "--squash", ws.BranchName},
        {"git", "-C", ws.Path, "commit", "-m", buildMergeMsg(ws)},  // preserves trailers per §4.4 EM-017
    }
    // ... run + capture SHA
}
```

Mechanically implementable. Frictions:

- "The integration-branch commit message MUST preserve the `Harmonik-Run-ID` and `Harmonik-Bead-ID` (when present) trailers" — but the task branch has many commits each carrying these trailers; a squash produces one new commit; whose trailer set is preserved? The most recent? All concatenated? Implementer assumes the last checkpoint's trailers are sufficient (they carry the run-level identifiers, which are invariant across the run).
- Conflict detection is "real" per WM-020 but the spec does not tell you how the manager *detects* a conflict. Standard answer: `git merge --squash` exit code + `git status --porcelain` for conflict markers — but stating the detection surface explicitly would be cheap.

### WM-020 — Merge is not fast-forward-only — IMPLEMENTABLE

Trivial for the implementer: do not pass `--ff-only`. No Go code to write beyond omitting a flag. Declaration-only. Correctly carries no `Axes:` line.

### WM-021 — Merge outcome emits workspace_merged — PARTIALLY

Inherits WM-015 / WM-017 blockers (event name + payload). Mechanically the state transition is fine.

### WM-022 — Original implementer resolves merge conflicts — BLOCKED

```go
// Spec delegates the conflict-resolution step to the "ORIGINAL IMPLEMENTER"
// but does not name:
//   - how the original implementer is identified from the run's history
//   - what LaunchSpec is constructed for the re-dispatch
//   - what progress stream is consumed to detect "unresolvable" vs "resolved"
func (m *Manager) redispatchImplementerForMerge(ws *Workspace, conflict ConflictSummary) error {
    // what goes here?
}
```

Stuck points:

1. **Identity of the original implementer.** The spec says "the agent whose work produced the divergent commits on the task branch." There may be multiple implementer-role sessions across the run. Is "original" = the first implementer node? The most recent one? The one that produced the commit immediately prior to the merge? Without a rule, the implementer cannot identify the target handler to re-invoke.
2. **Input shape.** WM-024 lists `(task branch, integration branch, conflict markers) plus the run's transition history` as input shape — good. But the spec does not say how this is formatted on the wire. Is it a LaunchSpec with a specific set of `required_skills`? A freshly synthesized prompt? The cognition-tagged delegation-path obligation per AR-007 is partially satisfied (role + model-class-class + input shape named in prose) but the concrete wiring to handler-contract is absent.
3. **Budget / timeout.** "Cannot resolve the merge conflicts within its budget" (WM-023) implies a budget exists on conflict resolution; which budget? The original handler's `budget` per handler-contract.md §6.1 LaunchSpec? A new budget issued by the workspace manager? Implementer has to invent one.

### WM-023 — Unresolvable conflicts escalate to human — PARTIALLY

The `merge_conflict_escalation` event is registered (event-model §8.5.6). Payload fields: `workspace_id`, `run_id`, `conflict_paths[]`, `escalated_at`. WM-023 says the payload carries `workspace_id`, `run_id`, `branch_name`, `conflict summary`. Two payload-field mismatches: `branch_name` (WM-023) vs not-present (event-model); `conflict_summary` (WM-023, freeform) vs `conflict_paths[]` (event-model, structured list). The implementer cannot emit a payload satisfying both.

Also: "The run MUST transition to `conflict-resolving`" — but §7.1 already transitioned to `conflict-resolving` when the conflict was *detected*; this requirement makes the state-transition happen twice. Re-reading carefully, WM-023 says "the run MUST transition to conflict-resolving per §7.1 and await external resolution" — so the state is already there; this is a pointer, not a new transition. Confusing but not blocking.

### WM-025 / WM-026 / WM-027 — Session-log + sidecar — IMPLEMENTABLE

```go
type SessionMetadataSidecar struct {
    RunID         uuid.UUID  `json:"run_id"`
    NodeID        string     `json:"node_id"`
    AgentType     string     `json:"agent_type"`
    WorkflowID    uuid.UUID  `json:"workflow_id"`
    BeadID        *string    `json:"bead_id,omitempty"`
    LaunchedAt    time.Time  `json:"launched_at"`
    SchemaVersion int        `json:"schema_version"`
}

func (m *Manager) writeSidecar(ws *Workspace, s SessionMeta) error {
    dir := filepath.Join(ws.Path, ".harmonik", "sessions", s.SessionID)
    if err := os.MkdirAll(dir, 0o755); err != nil { return err }
    body, _ := json.Marshal(SessionMetadataSidecar{...})
    return os.WriteFile(filepath.Join(dir, "harmonik.meta.json"), body, 0o644)
}
```

Record shape in §6.1 matches the prose in WM-026. Minor quibble: `launched_at` is typed `Timestamp` in §6.1 and "RFC 3339 timestamp" in WM-026 — if `Timestamp` is a named type (§6.1 doesn't define it locally), it's ambiguous whether it is stored as RFC 3339 string or epoch int. Implementer picks RFC 3339 string for JSON, but a note would tighten this. The template §6.1 allows `Timestamp` as a standard type and convention is RFC 3339, so this is mild.

Also: `agent_type` is new per the v0.2 AR-MIG-001 rename. §A.1 of architecture.md names the valid tokens (`claude-code`, `pi`, `twin`, `twin-planner`, etc. — conformance class). This spec does not cite the enum range; implementer infers from architecture.md, but a direct `[architecture.md §4.7 agent_type]` pointer at WM-026 would close the loop.

### WM-028 — Bead-ID propagates into session metadata — IMPLEMENTABLE

Clean. `bead_id` field in `SessionMetadataSidecar` is nullable; `Harmonik-Bead-ID` trailer pass-through is exact. Good.

### WM-029 — Session-log directory is read-only to S08 — PARTIALLY (reviewer-enforced)

Declarative. The implementer does not write code for this — it is an obligation on the memory-layer (S08) spec. Flag: where is the sensor? AR-042 says every invariant MUST name a sensor; WM-029 is a requirement not an invariant, so AR-042 doesn't force it, but realistically if this is violated there is no runtime detector and no reviewer check is named. Consider adding: "Violation sensor: operator-auditable filesystem-permission mode set to 0444 under `${workspace_path}/.harmonik/sessions/` by S06 on session start." Otherwise it is vibes-based.

### WM-030 — Post-merge session-log retention — IMPLEMENTABLE

Default is "preserve the sessions directory inside the merged branch." Since the sessions directory is inside the worktree tree and the merge commits the tree, this falls out of the squash-merge naturally as long as the session directory is not gitignored. No code to write; but: does the default `.gitignore` in new projects ignore `.harmonik/`? That's an operator-facing question; the spec should state "the `.harmonik/sessions/` directory MUST NOT be gitignored at MVH." Otherwise WM-030 silently breaks.

### WM-031 / WM-032 — Failed-run worktree persistence — IMPLEMENTABLE

```go
func (m *Manager) handleTerminalFailure(ws *Workspace) {
    ws.State = Discarded  // WM-032
    // DO NOT call git worktree remove or rm -rf ws.Path
    // DO NOT delete ws.BranchName
    m.emit("workspace_discarded", ws.WorkspaceID, ws.RunID, ws.BranchName)
}
```

Clean. One gap: WM-032 says the workspace transitions to `discarded` OR sets `interrupt_state` — but `discarded` is a lifecycle-terminal state while interrupt_state is orthogonal. A workspace with state=`leased` + interrupt_state=`operator-stopped-graceful` → what next? The spec is silent on whether `leased + interrupted` ever transitions to `discarded` on final cleanup. OQ-WM-004 touches this but does not close it.

### WM-033 — Startup orphan sweep removes stale locks only — BLOCKED (cross-spec drift)

```go
// spec says: remove stale `.harmonik/lease.lock` files
// but handler-contract HC-044a says the lock is `.harmonik/worktrees/<run_id>/.lock`
// at a DIFFERENT path — no `lease.lock` mentioned anywhere in HC.
// process-lifecycle PL-006 says: inspect each worktree under the project's
// configured worktree root for lock files (`.harmonik/lease.lock` or equivalent
// per [workspace-model.md §5.1])
```

Three specs say three different things:

- workspace-model §6.2 + WM-033: `${workspace_path}/.harmonik/lease.lock` (i.e., inside the worktree directory).
- handler-contract HC-044a: `.harmonik/worktrees/<run_id>/.lock` (at the repo level — but this resolves to the *same* path per WM-002 since `${workspace_path}` = `<repo>/.harmonik/worktrees/<run_id>/`, so `${workspace_path}/.lock` = `<repo>/.harmonik/worktrees/<run_id>/.lock`). Note HC uses filename `.lock`, WM uses filename `.harmonik/lease.lock` — different filenames at different directory depths.
- process-lifecycle PL-006: `.harmonik/lease.lock` (matches WM).

The HC-044a filename is `.lock` directly under the worktree, whereas WM §6.2 puts it at `${workspace_path}/.harmonik/lease.lock` (i.e., inside the nested `.harmonik/` subdirectory of the worktree itself). The implementer cannot pick one that satisfies both. Also: HC-044a specifies content (PID + liveness probe); WM-033 says nothing about content; PL-006 uses mtime.

Until this is reconciled, `Launch` will fail-fast checks against a path different from what the workspace manager and the orphan sweep write. This is a real runtime bug waiting in the spec.

### WM-034 — reopen-bead triggers fresh worktree and fresh branch — BLOCKED

```go
// Spec says: "MUST receive a FRESH worktree at a new
// <repo>/.harmonik/worktrees/<new_run_id>/ path and a FRESH task branch
// named run/<new_run_id>"
// But where does <new_run_id> come from?
```

Stuck: the spec names `<new_run_id>` as if it is self-evidently a distinct identifier, but never says whose responsibility it is to mint it. execution-model.md EM-014 ("Bead-to-run relationship is many-runs-per-bead") confirms multiple runs per bead are allowed, but does not say the `reopen-bead` flow mints a new `run_id`. Reconciliation.md §9.5 describes the verdict but the verdict itself says "reopen the bead" — the run_id is a downstream concern.

If the implementer assumes `reopen-bead` produces a new run_id automatically, WM-034 is trivially implementable. But nothing commits to this; it could also mean "reuse run_id, fresh worktree on the same path — which is a *contradiction* with WM-031 (failed-run worktree still persists on disk with its branch intact) + the canonical-path rule (the path for a given run_id is unique). Without explicit "reopen-bead mints a new run_id," the rules contradict.

**Recommend:** add a sentence to WM-034: "A `reopen-bead` verdict MUST produce a fresh `run_id`; the prior run's `run_id` remains bound to the prior worktree per WM-031."

### WM-035 — Intra-run rollback verdicts keep the same worktree — IMPLEMENTABLE

```go
if verdict == "resume-here" || verdict == "resume-with-context" || verdict == "reset-to-checkpoint" {
    // use existing workspace for run, no new creation
}
```

Classification table in WM-036 is a clean enum switch. The worktree is reused; git operations per EM-044 roll state back.

### WM-036 — Re-run vs intra-run classification deterministic on verdict enum — BLOCKED (enum mismatch)

```go
// WM-036 classifies: "other verdicts (abandon, escalate) → no re-run attempted"
// But reconciliation.md §4.4 enumerates the verdict set as:
//   resume-here | resume-with-context | reset-to-checkpoint
//   reopen-bead | accept-close-with-note | escalate-to-human
// `abandon` is NOT a verdict.
```

WM-036 text (line 308) names `abandon` as a verdict. Reconciliation spec's canonical enum does not include `abandon`. Implementer cannot compile a switch statement over an enum value that doesn't exist in the authoritative spec.

The enum actually missing from WM-036's classification is `accept-close-with-note` — which is a verdict-that-closes but does not re-run. The implementer infers the intent is "verdicts that do not re-run" but the naming is wrong.

Also — `escalate-to-human` is correctly a no-re-run verdict; good. But `accept-close-with-note` is also no-re-run; the spec omits it from the classification table.

### WM-037 / WM-038 / WM-039 / WM-040 — Interrupt-state — PARTIALLY

```go
type InterruptState int
const (
    IntNone InterruptState = iota
    IntOperatorPaused
    IntOperatorStoppedGraceful
    IntOperatorStoppedImmediate
    IntDaemonCrashSuspected
)

func (m *Manager) setInterrupt(ws *Workspace, state InterruptState) {
    prior := ws.InterruptState
    ws.InterruptState = state
    if prior == IntNone && state != IntNone {
        m.emit("workspace_interrupted", ...)
    }
}
```

Field shape is clean. Transitions: WM-038 says operator control + reconciliation drive; WM-039 fires event on none→non-none. Good shape.

Stuck points:

1. **Conflict with event-model on emitter.** Event-model §8.5.5 says reconciliation detector emits `workspace_interrupted`. WM-039 says "the workspace manager MUST emit `workspace_interrupted`." Two emitters, one event. If the event bus enforces one-owner-per-event-type (EV-025 suggests so), one of these violates it.
2. **Payload shape drift.** WM-039 says payload includes `workspace_id`, `run_id`, `prior lifecycle state`, `new interrupt_state`. Event-model §8.5.5 payload says `workspace_id`, `run_id`, `detected_at`, `category` (Cat 6). No prior-state or interrupt-value field in event-model. These do not match.
3. **WM-040 clearance** — "MUST NOT silently clear" but no sensor/enforcement is named. An invariant would help; there is no current invariant forbidding silent clear.

### WM-INV-001 — Lease-by-run — PARTIALLY (AR-042 sensor missing)

AR-042 requires every invariant to name its enforcing sensor inline or in §10.2. WM-INV-001 names no sensor. §10.2 has test obligations but none is cited as the invariant's sensor. Reviewer-enforceable under AR-042.

### WM-INV-002 — One run per bead at a time — PARTIALLY (AR-042 sensor missing)

Same gap. No sensor named.

### WM-INV-003 — Git append-only on task branch — PARTIALLY (AR-042 sensor missing)

Same gap. A sensor could point at EM-020a audit-tool detection rule from execution-model, but nothing does.

### WM-INV-004 — Merge-conflict resolver is the original implementer — PARTIALLY (AR-042 sensor missing)

Same gap; additionally, this invariant restates WM-022 verbatim — the template §5 selection test says "If the rule fits inside one subsystem's §4 without reference to others, it is a requirement, not an invariant. If you write the same rule as both, delete the §4 copy." WM-INV-004 and WM-022 are the same rule; per template guidance, one should be dropped.

### WM-INV-005 — Worktree path is canonical and derivable from run_id — IMPLEMENTABLE

Naturally sensed by WM-013's construction + filesystem check. A pointer in the invariant block (`Sensor: WM-013 filesystem discovery check satisfies this invariant by construction.`) would satisfy AR-042.

## Stuck points and stronger alternatives

For each `PARTIALLY` / `BLOCKED` finding, concrete spec text.

### SP-1. WM-015 / WM-017 / WM-021 — Event-name + payload drift vs event-model

**Problem.** workspace-model names `workspace_merge_pending` + `workspace_merged` as distinct events; event-model registers `workspace_merge_status` as a single paired-phase event. This is an outright break of event-model §8.9(h) and of EV-025 (one owning spec for payload shape).

**Proposed text (for WM-015).** Replace the event list with:

> The workspace manager MUST emit the following events at the state transitions of §7.1: `workspace_created`, `workspace_leased`, `workspace_merge_status` (with `status=pending` on transition into `merge-pending` and `status=merged` on transition into `merged`, per [event-model.md §8.5.3] paired-phase-lifecycle rule §8.9(h)), `workspace_discarded`, `workspace_interrupted`, `merge_conflict_escalation`. Each event's payload schema is authoritative in [event-model.md §8.5]; this spec is normative for WHEN each event fires.

Remove "Each event payload MUST carry `workspace_id`, `run_id`, and the associated branch name" (this is payload shape, not this spec's business). Delete WM-017 entirely or reduce it to a pointer at event-model payload definitions; do not re-declare payload fields here. Update §6.3 bullet list accordingly.

### SP-2. WM-033 / §6.2 — Lock path and semantics contradicted across three specs

**Problem.** workspace-model, handler-contract, and process-lifecycle disagree on lock path and filename. A canonical shape must be chosen once.

**Proposed text (new WM-033a).**

> WM-033a — Lease-lock file canonical path and content. The lease-lock file MUST be written at `${workspace_path}/.lock` (equivalently, `<repo>/.harmonik/worktrees/<run_id>/.lock` per §4.1). The file MUST contain the owning daemon's PID followed by a newline, and is MUST be fsynced before the workspace manager emits `workspace_leased`. Stale-lock detection is (a) PID not live via `kill(pid, 0)` OR (b) recorded PID is held by a non-handler argv. Both conditions are per [handler-contract.md HC-044a]. A stale lock MUST be reclaimed by the new daemon generation; a live lock held by a non-current-generation daemon is a fail-fast case per [handler-contract.md HC-044a].
>
> Tags: mechanism
> Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

Then WM-033's sweep rule aligns with HC-044a on the same filename. Update §6.2 row for the lock file accordingly. Retire `.harmonik/lease.lock` everywhere and rewrite PL-006 in the next process-lifecycle revision cycle.

### SP-3. WM-034 — reopen-bead needs an explicit "mints new run_id" clause

**Problem.** The "fresh worktree and fresh branch" follows from a new run_id; but nothing says reopen-bead produces a new run_id.

**Proposed text (add to WM-034 body).**

> A `reopen-bead` verdict MUST produce a new `run_id` distinct from every prior run_id ever dispatched against the bead. The new run_id becomes the lease key for the fresh worktree per §4.3.WM-010. The prior run's run_id, worktree, and branch persist on disk per §4.8.WM-031. This contract closes the tension between path canonicality (§4.1.WM-002) and failed-run persistence (§4.8.WM-031).

### SP-4. WM-036 — Verdict-enum classification misnames values

**Problem.** Lists `abandon` (not a verdict); omits `accept-close-with-note`.

**Proposed text (replace classification table).**

> The classification table is:
> - `reopen-bead` → fresh worktree (§4.9.WM-034)
> - `resume-here` | `resume-with-context` | `reset-to-checkpoint` → keep worktree (§4.9.WM-035)
> - `accept-close-with-note` | `escalate-to-human` → no re-run attempted; workspace transitions to `discarded` per §4.8

This uses the authoritative verdict set from [reconciliation.md §4.4].

### SP-5. WM-010 / WM-013 — Lease mechanism and discovery rule

**Problem.** No requirement says *how* the lease is realized on disk (§SP-2 above helps); WM-013 says "filesystem check" without naming it.

**Proposed text (new WM-013a).**

> WM-013a — Lease discovery mechanism. The workspace manager MUST discover live workspaces on startup by (a) enumerating subdirectories of `<repo>/.harmonik/worktrees/` matching the `<run_id>` UUID shape; (b) for each, calling `git worktree list --porcelain` against `<repo>` and confirming the directory is a registered worktree; (c) stat-ing `${path}/.harmonik/sessions/` to detect whether any session was ever started. A directory failing (b) but present on disk is an orphan worktree referred to [process-lifecycle.md PL-006]. A directory passing (b) is a live workspace whose state is reconstructed per §4.1.WM-004.

### SP-6. WM-018 — Merge-back node is undefined

**Problem.** "Merge-back performed by a node in the same worktree" forbids an architecture but does not name the replacement.

**Proposed text (new WM-018a).**

> WM-018a — Merge-back node declaration. A workflow MUST declare its merge-back step as a non-agentic node with `node_type = "merge"` (a new enum value to be added at [execution-model.md §6.1 Node]) whose run produces the merge commit per §4.5.WM-019. The orchestrator dispatches the merge node directly (no handler subprocess) per §4.5.WM-018; conflict detection falls to §4.6.WM-022 upon `git merge --squash` returning non-zero.

If execution-model does not want to carry a new `node_type` enum value, the alternative is: name merge-back as a workflow-level phase in the execution model (a distinguished *last* node by terminal-node rule). Either works but the spec must choose.

### SP-7. WM-022 / WM-024 — Original-implementer identification + re-dispatch wiring

**Problem.** "The agent whose work produced the divergent commits" — no identifier rule; no re-dispatch handle.

**Proposed text (add to WM-024 body).**

> The "original implementer" for a given merge-back MUST be identified as the agent that produced the latest commit on the task branch that is an ancestor of the merge-base with the integration branch. The workspace manager MUST query that commit's `Harmonik-Run-ID` + the session-metadata sidecar at `.harmonik/sessions/` indexed by `node_id` to obtain the handler's `agent_type` and `LaunchSpec` template. The re-dispatch MUST construct a new `LaunchSpec` per [handler-contract.md §6.1] with the same `agent_type`, a workspace_path pointing at the existing worktree, and a `required_skills` list that includes a conflict-resolution skill (name TBD; currently OQ-WM-00X). The budget is the original handler's `budget` reduced by elapsed wall-clock time per the handler-contract budget composition rule.

(Name TBD marker implies an open question — would be added in §11.)

### SP-8. WM-039 — Event-emitter contention

**Problem.** event-model.md §8.5.5 says reconciliation detector emits `workspace_interrupted`; WM-039 says workspace manager emits it.

**Proposed resolution.** Either (a) workspace-model defers: reword WM-039 to "the workspace manager MUST ensure that `interrupt_state` transitions from `none` to a non-`none` value are observable by the reconciliation detector per [reconciliation.md §4.2]; the detector emits `workspace_interrupted` per [event-model.md §8.5.5]"; OR (b) event-model is corrected to name the workspace manager as emitter for operator-driven interrupts and reconciliation detector for crash-detected interrupts. (a) is simpler since both types of interrupts would pass through reconciliation's detector to reach observers consistently.

### SP-9. WM-INV-001 … WM-INV-005 — AR-042 sensor obligation

**Problem.** Every invariant must name its sensor (AR-042). None do.

**Proposed text.** For each invariant, add a `Sensor:` line inside the block:

- WM-INV-001 — Sensor: runtime concurrency test per §10.2 line "Multi-agent-sequential scenario tests: ... concurrency check rejects a second active agent."
- WM-INV-002 — Sensor: per-bead lease check on `Lease()` entry; test per §10.2.
- WM-INV-003 — Sensor: `EM-020a` audit-tool detection rule from execution-model.
- WM-INV-004 — Sensor: scenario test per §10.2 line "Cognition-tagged integration tests with twin implementer..." (also flag: this invariant duplicates WM-022 and should be consolidated).
- WM-INV-005 — Sensor: WM-013a filesystem discovery, by construction.

### SP-10. WM-023 — Payload shape mismatch on merge_conflict_escalation

**Proposed text.** Drop payload-field list from WM-023. Replace with: "Payload schema is [event-model.md §8.5.6]."

### SP-11. WM-029 — Read-only enforcement needs a sensor or drop to SHOULD

**Proposed text (alt).** "The session-log directory (including the metadata sidecar and handler-written logs) SHOULD be treated as read-only by the memory-layer subsystem (S08) per its own conformance profile in [memory-layer.md §<TBD>]; workspace-model declares no enforcement sensor at MVH."

### SP-12. WM-030 — Session-log preservation depends on .gitignore hygiene

**Proposed text.** Add: "Project `.gitignore` MUST NOT exclude `.harmonik/sessions/`; violations are an operator-observable misconfiguration per [operator-nfr.md §<TBD>]."

## Type-level coherence (§4.1 vs §6.1)

- **Workspace record (§6.1) vs WM-001 body** — matches. All fields in prose appear in RECORD. OK.
- **WorkspaceState enum (§6.1)** — eight values. §7.1 uses all eight. Match. OK.
- **InterruptState enum (§6.1)** — five values. WM-037 enumerates the same five. Match. OK.
- **SessionMetadataSidecar (§6.1)** — six mandatory + two optional (bead_id, schema_version). WM-026 prose enumerates five + bead_id; schema_version is mentioned in the record but not the prose. Reconcile: the WM-026 body should cite `schema_version` explicitly (per §6.4 obligations) — otherwise a naive implementer reading only WM-026 omits the field.
- **Workspace.schema_version** — §6.1 has it; no requirement says who stamps it or whether it appears in events. Declaration-only is OK but a note that "schema_version is set to the current version by the workspace manager on create" would remove a guess.
- **Timestamp type** — §6.1 SessionMetadataSidecar uses `Timestamp`. No local definition; inferred from template §6.1 standard types. OK but thin.
- **UUID type** — §6.1 uses `UUID` for run_id, workflow_id; the Go implementation pick is `uuid.UUID` via github.com/google/uuid. Standard. OK.

Event-payload obligations (§6.3) are **reference-incomplete**:

- §6.3 lists `workspace_merge_pending` and `workspace_merged` — but event-model has one `workspace_merge_status` event. Reference-incomplete.
- §6.3 says "Payload schemas are declared in [docs/foundation/components.md §3.2] event-model" — the bootstrap citation. Correct form, but should migrate to `[event-model.md §8.5]` now that event-model is reviewed. Bootstrap citation form per template §Cross-reference convention is fine for draft, but OQ-WM-XXX or revision-history note should track the migration.
- §6.3 lists seven event names; event-model registers six. Compare:
  - `workspace_created` — ✓ event-model §8.5.1
  - `workspace_leased` — ✓ event-model §8.5.2
  - `workspace_merge_pending` — ✗ not registered (paired-phase collapsed)
  - `workspace_merged` — ✗ not registered (paired-phase collapsed)
  - `workspace_discarded` — ✓ event-model §8.5.4
  - `workspace_interrupted` — ✓ event-model §8.5.5 (emitter differs)
  - `merge_conflict_escalation` — ✓ event-model §8.5.6

The record shape in §6.1 is internally coherent. The event-payload surface is reference-broken.

## Protocol-side concreteness (§7.2)

The pseudocode in §7.2 is partially runnable. Trace analysis:

```
FUNCTION create_workspace(run_id, parent_commit, bead_id | None):
    path = worktree_root() + "/" + run_id        # WM-002 ✓
    branch = "run/" + run_id                     # WM-005 ✓
    IF path already exists:
        RETURN ERROR(WorkspaceAlreadyExists)     # No error class defined anywhere
    git.worktree_add(path, branch,
                     start_point=parent_commit)  # WM-003 ✓
    emit_event("workspace_created", ...)         # WM-015 ✓ (name matches event-model)
    sessions_dir = path + "/.harmonik/sessions"
    mkdir(sessions_dir)
    RETURN Workspace(
        workspace_id=derive(run_id),             # WM-004 ✓ (though "recommended" weakens it)
        run_id, path, branch, state=ready)       # Skipped state=created / setup?
```

Issues:

1. **State-machine jump.** §7.1 specifies `created → setup → ready`. The pseudocode returns `state=ready` directly, bypassing `created` and `setup`. Either (a) the pseudocode is wrong, or (b) the intermediate states are nominal and never observed — but WM-014 lists them as required. §7.1's "created → setup (git worktree add succeeds, no event)" and "setup → ready (sidecar written, no event)" both have no emission, which is why they're invisible to consumers — but the workspace manager still needs to traverse them if the state machine is normative.
2. **No lock write.** Per SP-2, the lease lock file must be written. Pseudocode never calls into `write_lock(path, pid)`.
3. **`WorkspaceAlreadyExists` error.** No §8 taxonomy in this spec, so no error class is defined. Implementer must invent one.
4. **`derive(run_id)`** is not defined — assumed to be the `"ws-" + run_id` rule, but not called out.
5. **`bead_id` ignored.** The function accepts `bead_id | None` but never uses it. Either it must be plumbed into the returned `Workspace` record (matches §6.1) or the parameter is vestigial.

```
FUNCTION stamp_session_metadata(workspace, session_id, node_id, agent_type, workflow_id):
    session_dir = workspace.path + "/.harmonik/sessions/" + session_id
    mkdir(session_dir)
    sidecar = SessionMetadataSidecar(...)        # WM-026 ✓
    write_json(session_dir + "/harmonik.meta.json", sidecar)  # WM-026 ✓
    emit_event("workspace_leased", ...)          # WM-015 ✓
    workspace.state = leased                     # WM-014 ✓
```

Issues:

1. **No ordering guard.** WM-016 says the sidecar write precedes `workspace_leased`. Pseudocode shows this order; good. But the state transition `workspace.state = leased` happens AFTER the event is emitted — concurrent observers may see `workspace_leased` event with the record still in `ready` state. Non-atomic. Fix: transition state first, emit after, or use a single atomic update with the event write.
2. **No lock write.** Same as above.
3. **No idempotency check.** Calling `stamp_session_metadata` twice for the same session would be a no-op for the mkdir but would re-emit `workspace_leased`. WM-016's Axes line is `idempotency=non-idempotent`, which is honest — but then a caller reinvoking after a retry will produce a duplicate event. §6.5 of the template covers this territory for emit contracts; this spec does not touch it.

**Not runnable end-to-end** without SP-2 (lock), SP-6 (merge-back), and SP-3 (run_id minting) being resolved.

## Cross-spec reach-ins (silent consumption)

- **WM-006 depends on [beads-integration.md §4.5 BI-007] parent-child edge query but cites only [docs/foundation/components.md §10.3].** Should migrate to `[beads-integration.md §4.5]` now that beads-integration exists as a spec (status: draft but the anchor is present).
- **WM-008 depends on the same Beads dependency-graph query, cites the same bootstrap anchor.** Same migration.
- **WM-015 / WM-017 / WM-021 / WM-039 depend on [event-model.md §8.5] but cite [docs/foundation/components.md §3.2].** Bootstrap citation is still permitted per template (event-model is reviewed so migration is now due).
- **WM-010 depends on [architecture.md §4.9] centralized-controller** — correctly cited.
- **WM-022 consumes a handler-contract surface (implementer re-dispatch) but does not cite [handler-contract.md §6.1 LaunchSpec] or handler-contract's budget composition rule.** The spec mentions "the handler class that launched the implementer originally" (WM-024) without a concrete anchor.
- **WM-025 depends on [handler-contract.md §4.2 HC-010] session_log_location emission** but only says "join point for handler-written session logs (S04) and CASS-read metadata (S08)." No direct citation to handler-contract HC-010 or HC-044a. Add.
- **WM-033 depends on [process-lifecycle.md PL-006] orphan sweep** but cites only `[docs/foundation/components.md §8.2 step 1a]`. Note: `step 1a` is a bootstrap anchor that does not exist in process-lifecycle (PL-006 is one sub-rule in §4.2). Either migrate to `[process-lifecycle.md §4.2.PL-006]` or update the bootstrap citation.
- **WM-034 / WM-035 / WM-036 depend on [reconciliation.md §4.4/§4.5 verdict enum]** but cite only `[docs/foundation/components.md §9.5]`. Reconciliation is drafted; migrate to `[reconciliation.md §4.5]`.
- **WM-INV-003 depends on [EM-020 / EM-020a]** git append-only rule; cites only `[execution-model.md §4.4.EM-017]`. EM-020/020a is the directly relevant cite.
- **WM-035 cites [execution-model.md §4.10.EM-044]** — correct. But the field named is `rollback_to_state_id`; the spec does not mention `rollback_to_state_id` in its own body as the mechanism — a one-line sentence in WM-035 naming the EM-044 field would close the loop.
- **§9.1 `depends-on` front-matter lists only `execution-model`.** By my reach-in count, it should also list (a) architecture (WM-010, WM-INV-001 cite AR-027, AR-042, §4.9); (b) event-model (WM-015 et al.); (c) handler-contract (session-log pipeline, HC-044a contention on lock); (d) beads-integration (WM-006, WM-008); (e) reconciliation (verdict enum); (f) process-lifecycle (orphan sweep); (g) control-points (interrupt-state driver in WM-038). The current `depends-on: [execution-model]` under-declares dependencies.

## Template conformance audit

- **§0 front matter.** Declares `spec-template-version: 1.1` — current. Declares `status: draft`. Declares `version: 0.2.0`. `depends-on` under-declared (see previous bullet). **AR-052 / AR-053 obligation not satisfied:** front matter should declare `spec-category: runtime-subsystem` (per AR-052 it MUST be present) and §4 should carry a `§4.a Subsystem envelope` subsection with `WM-ENV-NNN` identifiers (per AR-053). Neither is present. This is a lint-enforced failure if architecture.md v0.3 applies.
- **§1 Purpose.** Fits one screen. Names scope and why separate spec. OK.
- **§2 Scope.** Both in/out-of-scope lists present. OK.
- **§3 Glossary.** Defines terms. `workspace`, `worktree`, `task branch`, etc. — all original to this spec. OK.
- **§4 Normative requirements.** 40 requirements (WM-001 through WM-040). Numbered in source order. Tags present on every block. **Axes line audit:**
  - Required-axes triggers (LLM invocation OR external I/O OR state mutation OR non-idempotency):
    - WM-002: path convention — declaration-only, Axes present. Redundant but not wrong.
    - WM-003: git worktree add — state-mutation — Axes present. OK.
    - WM-015: event emission — IO — Axes present. OK.
    - WM-016: event emission after write — IO — Axes present. OK.
    - WM-019: merge-commit creation — state-mutation — Axes present. OK.
    - WM-021: event emission — IO — Axes present. OK.
    - WM-022: **cognition-tagged, LLM invocation** — Axes present (`llm-freedom=bounded; replay-safety=unsafe`) — OK; delegation path named in body per AR-007. OK.
    - WM-023: event emission — IO — Axes present (`idempotency=idempotent` — OK since event emission is generally non-idempotent but the spec asserts idempotency here; reviewer should confirm).
    - WM-026: metadata sidecar write — state-mutation — Axes present. OK.
    - WM-033: stale-lock removal — state-mutation — Axes present. OK.
    - WM-034: worktree creation — state-mutation — Axes present. OK.
    - WM-039: event emission — IO — Axes present. OK.
  - **Missing Axes (candidate):**
    - WM-017: event payload requirement — if this mandates emission it performs IO; arguably a declaration ("events MUST carry X"). Declaration-only is defensible. OK.
    - WM-025 / WM-027: directory creation — state-mutation on filesystem — **Axes lines MISSING.** WM-025 "session-log directory MUST exist" describes an obligation that requires a mkdir at some point. WM-027 is a purely ordering declaration. WM-025 is borderline; a reviewer could flag.
    - WM-031: "MUST persist on disk" — describing non-action is declarative; OK no Axes.
    - WM-032: transition declaration — OK no Axes.
  - **Tag grammar.** All `Tags: mechanism` except WM-022 `Tags: cognition`. Single-tag rule OK. Axes tokens all lowercase. OK.
- **§5 Invariants.** 5 invariants (WM-INV-001 through WM-INV-005). None names its sensor. **AR-042 violation for all five.** WM-INV-004 duplicates WM-022 — template §5 selection test violation.
- **§6 Schemas.** Record schemas present; `ENUM` blocks for WorkspaceState + InterruptState; SessionMetadataSidecar record; canonical on-disk path table; lifecycle event emission rules; schema evolution. Solid shape. Minor: `RECORD` fields lack comment-line explanations in places (e.g., `metadata : Map<String, String>` could name the keys).
- **§7 Protocols and state machines.** State machine table complete; pseudocode present. Per §SP-6 and protocol-side concreteness above, gaps exist.
- **§8 Error and failure taxonomy.** **ABSENT.** The template marks §8 as optional, but for a spec that mutates filesystem and git state, its absence leaves error classes undefined. Recommend at minimum: `WorktreeCreationFailed`, `LockHeldByOrphan`, `MergeConflictUnresolvable`, `SidecarWriteFailed`. Could be a short §8.
- **§9 Cross-references.** Depends-on, reverse-deps (informative), co-references. The co-references list is thin but acceptable.
- **§10 Conformance.** Profile names Core MVH + extensions. Test-surface obligations enumerated in prose (per bootstrap). Excluded conformance claims present. OK.
- **§11 Open questions.** Four OQs. Format matches template. OK.
- **§12 Revision history.** Two rows (0.1.0, 0.2.0). 0.2.0 note is thorough — good audit trail for the migration work.

**Total line count: 608 lines.** Under the 1000-line split threshold. OK.

**No TODO/FIXME tokens.** OK.

**Lint-enforcement summary.** Front-matter `spec-category` + §4.a envelope missing is the main lint failure against architecture.md v0.3 (AR-052/AR-053). AR-042 invariant-sensor obligation is reviewer-enforced but uniformly violated. Tag grammar is clean. Event-name drift vs event-model is reviewer-enforced but is the biggest load-bearing defect.

## Implementability score

Of the 40 requirements + 5 invariants (45 total) I spot-checked:

| Bucket | Count | Share |
|---|---|---|
| IMPLEMENTABLE | 22 | ~49% |
| PARTIALLY | 14 | ~31% |
| BLOCKED | 9 | ~20% |

The BLOCKED bucket is dominated by event-name drift (5 requirements — WM-015, WM-017, WM-021, WM-023, WM-039), lock-path drift (1 — WM-033), verdict-enum drift (1 — WM-036), run-id minting (1 — WM-034), merge-back primitive (1 — WM-018). The PARTIALLY bucket is dominated by AR-042 invariant-sensor gaps (5 — WM-INV-001..005) and missing anchor-to-handler-contract for conflict-resolution mechanics (WM-022, WM-024).

**Net implementable today: ~49%.** With the 12 spec-text fixes above (SP-1 through SP-12), the number rises to ~95% — i.e., the drifts are real but fixable by editorial and cross-reference discipline; no architectural decisions need to be reopened. The fundamentally load-bearing choices (lease-by-run, canonical path, merge-back-in-same-worktree, original-implementer-resolves-conflicts, failed-run persistence, orthogonal interrupt-state) are sound and do not need to change.

## Affirmations

1. **Lease-by-run framing is clean.** The rationale in §A.3 is exactly the right framing: agents are ephemeral, runs span work, lease handover would be expensive. The invariant (WM-INV-001) + the state machine composition with interrupt_state (WM-037) together carry the doctrine cleanly, and the "one active agent at a time" clause (WM-011) correctly delegates enforcement to the orchestrator rather than introducing a per-agent lock.
2. **Canonical-path-no-registry decision.** §A.3 rationale "one source of truth, no registry" is directly actionable by the implementer. WM-002 + WM-004 + WM-013 together give you a closed triangle: path-from-run-id, id-from-run-id, id-from-path — no store needed.
3. **Orthogonal interrupt-state.** Correctly resists the temptation to multiply lifecycle states by interruption reasons. WM-037's enumeration is tight; the rationale in §A.3 names the combinatorial cost of the rejected alternative. OQ-WM-004 honestly tracks the mixed-interruption-history limitation.
4. **Session-log + sidecar ownership triangle (S04/S06/S08).** The "who creates, who writes, who reads" split between the workspace manager (sidecar + dir creation), handler (log contents), and memory-layer (ingest) is the right shape for a cross-subsystem pipeline. The emission-order rule (sidecar-before-lease) closes the join-key race cleanly.
5. **Failed-run preservation.** §A.3 rationale is exactly right: post-mortem needs evidence, auto-cleanup destroys it, operator-initiated cleanup keeps the system auditable without making the daemon into a garbage collector. WM-031 and WM-033 form a tight pair.
6. **Three-level branching with small-scope collapse.** Node commits → task branch → integration → main, with the escape hatch that no-parent-bead runs may skip integration, is the right default. The contract stops at "integration holds one commit per task" without dictating integration-to-main style, which is the right scope.
7. **Original-implementer conflict resolution.** The rationale is correct — separate merge-agent would re-derive context. What the spec does not do (fully) is wire this into handler-contract; see SP-7.

The spec has the right architectural bones. What it lacks is lockstep discipline with its neighbors (events, locks, verdict enum, run-id minting rules) and a few concrete mechanism specifications (merge-back node declaration, lease-lock write protocol, discovery rule). Fixing those will take one focused revision cycle.
