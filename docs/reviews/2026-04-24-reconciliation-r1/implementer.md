# Round 1 Implementer Review — reconciliation.md v0.2.0

## Verdict summary

**Implementable with substantial caveats.** The taxonomy-first §8 is the strongest part of the spec: 11 detection categories with detection rule, default response, escalation path, emitted event, and investigator/auto-resolver flags per category. The action-mapping table at §8.12 and its canonical twin at `schemas.md §6.3` give an implementer a dispatch contract that compiles as a `switch` on a `ReconciliationCategory` enum without further interpretation. The verdict vocabulary (six enum values, `schemas.md §6.1 Verdict`) and the verdict-execution table (`schemas.md §6.2`) are concrete enough to build typed handlers against.

The spec is blocked on three classes of defect, in rough priority order:

1. **Pervasive broken cross-references.** Every cited section in `beads-integration.md`, `workspace-model.md`, `process-lifecycle.md`, `event-model.md`, and internal `§9.N` citations uses an obsolete numbering scheme. The spec cites `beads-integration.md §10.4, §10.7, §10.8, §10.8a` (the real spec uses `§4.4, §4.7, §4.8, §4.10`); `workspace-model.md §5.1, §5.3, §5.8, §5.9` (real: `§4.2, §4.7, §4.6, §4.9`); `process-lifecycle.md §8.2` (real: `§4.2`); `event-model.md §3.2, §3.4, §3.6, §4.1` (real: `§8.x, §4.4, §4.5, §4.2`). Internal self-citations at §8.5 reference "RC-025" correctly but cite `[execution-model.md §6.2]` which does exist. Every downstream reader that tries to resolve the dependency breaks. An implementer cannot compile against un-resolvable references. This is mechanical and fixable, but it is load-bearing because many requirements name the consumed mechanism by section number rather than by requirement ID.

2. **Several detector categories leave the detection rule partially to the investigator agent.** Cat 2 detection (§8.3) checks "no `run_completed` or `run_failed` event for the run since that checkpoint" — that requires reading JSONL for state facts, but RC-014 forbids exactly that use. The correct read (querying the event bus / observational replay) is not wired up. Cat 6a (§8.11) lists four detection rules OR'd together; each is implementable but the boundary between "Cat 6a (investigator required)" and "Cat 3 generic (investigator required)" is fuzzy on the first bullet (workspace-path-missing).

3. **Reconciliation-as-workflow (RC-001 / RC-002) interacts with execution-model checkpoint invariants in a way the spec partially declares but does not fully reconcile.** EM-024 (task-branch-tip is last-durable-state, and EM-025 / EM-024a require the tip to be a fast-forward descendant) is cited; RC-002 says reconciliation workflows emit exactly one checkpoint commit. But `execution-model.md §4.5 EM-026` calls out reconciliation as the explicit exception via a `workflow_class = reconciliation` metadata tag. The tag is NOT documented in the `Workflow` record declared at `execution-model.md §6.1` nor in `schemas.md §6.1` of this spec. An implementer cannot `if run.Workflow.Class == "reconciliation"` without a declared field.

Roughly 60% of the requirements I probed are IMPLEMENTABLE as-is; 30% are PARTIALLY implementable (an experienced Go implementer can ship with defensible invented contracts for the gaps, but choices will diverge between implementations); 10% are BLOCKED pending upstream spec touch-up.

## Requirements I attempted to implement

### RC-001 — Reconciliation runs as a harmonik workflow — PARTIALLY

```go
type WorkflowClass string

const (
    WorkflowClassNormal         WorkflowClass = ""
    WorkflowClassReconciliation WorkflowClass = "reconciliation"
)

func (d *Daemon) dispatchReconciliation(run *Run, cat ReconciliationCategory) error {
    wfDef := d.reconciliationLibrary.Lookup(cat) // S01-owned; RC-004
    if wfDef.Metadata["workflow_class"] != string(WorkflowClassReconciliation) {
        return fmt.Errorf("library workflow for %s not tagged workflow_class=reconciliation", cat)
    }
    invRun := d.CreateRun(wfDef, reconcileInput(run, cat))
    return d.dispatcher.Dispatch(invRun)
}
```

Stuck points:

1. `workflow_class = reconciliation` is the keying tag per RC-002 and `execution-model.md §4.5 EM-026`, but it is declared as a `metadata` map entry on `Workflow` rather than as a typed field. The spec does not say `metadata["workflow_class"]` explicitly — §3 glossary says "a DOT workflow tagged `workflow_class = reconciliation`" without stating where the tag lives. Implementer infers it's a DOT attribute on the root workflow node mapped to `Workflow.metadata["workflow_class"]`. Add a normative statement.
2. "The S01-shipped library" (RC-004) is named but its registry / lookup contract is not. Where does `d.reconciliationLibrary.Lookup(cat)` live? Is it a compiled-in map from category to DOT path, or a filesystem scan of `s01/reconciliation/`? Implementer must invent.

### RC-002 — Reconciliation workflows emit exactly one checkpoint commit — IMPLEMENTABLE

```go
// In the checkpoint sequencer (execution-model.md §7.2):
func (cp *Checkpointer) Write(run *Run, tx *Transition) (*Checkpoint, error) {
    if run.Workflow.Class == WorkflowClassReconciliation && !tx.IsVerdictCommit() {
        return nil, nil // no-op; intermediate reconciliation state not durable
    }
    // ... normal checkpoint path ...
}
```

The rule is crisp: one verdict commit; intermediate transitions do not land as commits. Implementable modulo the `WorkflowClass` field gap flagged above. `.harmonik/reconciliation/<investigator_run_id>/` as the evidence-path convention is declared; I can write evidence files to that directory.

### RC-003 — Bounded recursion — IMPLEMENTABLE

Follows for free from RC-002: a crash mid-investigation leaves no intermediate checkpoints, so on restart the outer run's state is unchanged from its original classification. The detector re-runs and re-classifies identically. No implementer work beyond RC-002.

One subtle point: RC-003 states reconciliation-workflow nodes "MUST NOT be classified under §8 Cat 1 or Cat 2." The implementation: when the detector scans for in-flight runs (RC-INV-005, RC-010), any run whose `workflow_class == reconciliation` is excluded from classification. But a crash during the verdict-execution step of RC-025 DOES leave a durable state — the verdict commit without the `Harmonik-Verdict-Executed` trailer. That's Cat 3b (§8.5), which is an auto-resolver. Good; the spec has thought this through.

### RC-004 / RC-005 / RC-006 — S01 library ownership and release discipline — PARTIALLY

RC-004 enumerates what S01 ships: (a) DOT per investigator-required category, (b) YAML policies with `wall_clock_seconds` and skill-injection set, (c) investigator-agent prompt templates. RC-005 says detectors and verdict-execution live in daemon Go code. RC-006 says daemon + library ship together.

Implementable in principle, but the structural gap: the daemon's Go code and the S01 library live in the same release, but they are still two separate artifacts with separate loading paths. When does the daemon load the library? At startup (in `§PL-005` of process-lifecycle, which this spec miscites as `§8.2`)? Hot-reloaded on operator pause? A DOT change without a daemon restart?

Recommend adding a requirement that says "the reconciliation library is loaded at daemon startup (or post-upgrade exec-replacement) and is immutable for the daemon's lifetime." Otherwise the library-vs-daemon versioning story is silent.

### RC-007 / RC-008 — Action-mapping dispatch contract — IMPLEMENTABLE

```go
type CategoryActionMap map[ReconciliationCategory]Action

type Action struct {
    Kind              ActionKind // autoResume, autoResolve, dispatchInvestigator, halt
    InvestigatorDOT   string     // non-empty iff Kind == dispatchInvestigator
    AutoResolverFunc  func(context.Context, *Run) error
}

// Defaults (per §8.12):
var defaultActionMap = CategoryActionMap{
    Cat0:  {Kind: Halt,                AutoResolverFunc: waitAndRetry},
    Cat1:  {Kind: AutoResume,          AutoResolverFunc: respawnNode},
    Cat2:  {Kind: DispatchInvestigator,InvestigatorDOT: "reconciliation/cat-2.dot"},
    Cat3:  {Kind: DispatchInvestigator,InvestigatorDOT: "reconciliation/cat-3.dot"},
    Cat3a: {Kind: AutoResolve,         AutoResolverFunc: adapterReissue},
    Cat3b: {Kind: AutoResolve,         AutoResolverFunc: reexecuteVerdict},
    Cat3c: {Kind: AutoResolve,         AutoResolverFunc: mechanicalCloseBead},
    Cat4:  {Kind: AutoResume,          AutoResolverFunc: rearmRetryOrGate},
    Cat5:  {Kind: AutoResume,          AutoResolverFunc: noop},
    Cat6a: {Kind: DispatchInvestigator,InvestigatorDOT: "reconciliation/cat-6a.dot"},
    Cat6b: {Kind: Halt,                AutoResolverFunc: escalateOperator},
}
```

The table at §8.12 (lines 230–242) gives every row with: category, default action, investigator-spawned?, typical verdict, auto-resolver present?. I can compile this directly. RC-007 says deviations must be declared in YAML with rationale; implementer treats this as a per-run policy override flag on the `Action` struct that the orchestrator consults before dispatch.

One gap: the per-run policy override surface (deviating from default action) is named in RC-007 but its schema is not declared. What's the YAML key? What rationale-string grammar? Implementer invents.

### RC-010 — Detectors operate on runs, not beads — IMPLEMENTABLE

```go
type RunSet map[RunID]*InFlightRun

func collectInFlightRuns(repo *git.Repo, br beads.Adapter) (RunSet, error) {
    runs := make(RunSet)
    // Walk git for every commit carrying a Harmonik-Run-ID trailer
    // (must do this for every branch; see "active-run discovery" gap below)
    checkpoints := walkGitForRunTrailers(repo) // returns commits grouped by run_id
    for runID, runCheckpoints := range checkpoints {
        runs[runID] = &InFlightRun{
            RunID:        runID,
            Checkpoints:  sortByDAGParentage(runCheckpoints), // RC-011
            TaskBranch:   findTaskBranch(repo, runCheckpoints),
        }
    }
    // Attach bead-ids (from trailers) and query br for each bead
    for _, run := range runs {
        if beadID := trailer(run.Tip(), "Harmonik-Bead-ID"); beadID != "" {
            run.Bead, _ = br.Get(beadID)
        }
    }
    return runs, nil
}
```

Stuck point inherited from execution-model: `walkGitForRunTrailers` needs a starting point. Every branch? A task-branch registry? The execution-model implementer review flagged this gap (EM-031); RC-010 consumes the same gap. Tracked there, not here.

### RC-011 — DAG parentage ordering — IMPLEMENTABLE

Standard graph walk. `sortByDAGParentage` is `git rev-list --topo-order` against each run's commit set. The tip is the commit in the run's set with no child in the run's set. Implementable directly.

### RC-012 — Cat 0 pre-check before any other detector — IMPLEMENTABLE

```go
func cat0PreCheck(ctx context.Context, cfg DaemonCfg) error {
    for _, check := range []struct{ name string; fn func(context.Context) error }{
        {"br --version", checkBrVersion},
        {"br list --limit 1", checkBrTrialCall},
        {"git rev-parse HEAD", checkGitRepo},
        {".harmonik writable", checkHarmonikWritable},
    } {
        ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
        defer cancel()
        if err := check.fn(ctx); err != nil {
            emit("infrastructure_unavailable", failedPrerequisite(check.name))
            return err
        }
    }
    return nil
}
```

RC-012 (lines 327–339) gives the enumerable checks with the T=5s timeout. The wiring is process-lifecycle's `§PL-005 step 3` (which the spec miscites as `§8.2 step 3`). Implementable.

Nit: RC-012 says "retry at a configurable cadence" but the config knob is named only in operator-nfr §4.5 ON-017 ("Cat 0 pre-check retry cadence"). Fine by cross-reference, but the spec doesn't name a default retry cadence. Implementer picks one (say 30s).

### RC-013 — Emit `reconciliation_category_assigned` before any dispatch — IMPLEMENTABLE

```go
for runID, run := range inFlightRuns {
    cat := detectCategory(run)
    emit("reconciliation_category_assigned", runID, cat, detectorRuleName(cat))
    switch action := actionMap[cat]; action.Kind {
    case DispatchInvestigator:
        dispatchReconciliation(run, cat)
    case AutoResolve, AutoResume:
        action.AutoResolverFunc(ctx, run)
    case Halt:
        // Cat 0 or Cat 6b: emit is enough; no dispatch
    }
}
```

RC-013 says emission MUST precede dispatch. Mechanically: emit first, then call dispatch. Straightforward.

### RC-014 — JSONL divergence-evidence scope — PARTIALLY

The permitted / forbidden list at lines 352–365 is clear at the bullet level. The implementable side:

```go
type DivergenceEvidence interface {
    CheckMissingCheckpointInGit(eventRef JSONLRef) (bool, error)    // Cat 6b trigger
    CheckCorruptJSONL(path string) (offset int64, err error)         // Cat 6b trigger
    CheckSiblingFileAbsent(eventRef JSONLRef) (bool, error)          // Cat 3 trigger
    // forbidden: reading state_id, run_id, transition_id, bead status from JSONL
}
```

Stuck points:

1. **Cat 2 detector (§8.3) needs to check "no `run_completed` or `run_failed` event for the run since that checkpoint."** That is a JSONL state-fact read; it is forbidden by RC-014 ("Use JSONL as the source of last-known `run_id`, `state_id`, or `transition_id`"). The intended reading is probably: query the event bus / the daemon's in-memory model rebuilt from git + Beads for run-terminal status. But the Cat 2 detection rule in §8.3 literally says "no `run_completed` or `run_failed` event" — events ARE JSONL-sourced in practice. Either the Cat 2 detection rule is sloppy wording (should be "Beads bead status is `in_progress` AND git tip has no `run_completed`-equivalent terminal marker") or RC-014 is too strict and needs a carve-out. The tension is unresolved.

2. **Cross-reference cites for `event-model.md` are wrong.** RC-014 cites `[execution-model.md §4.7]` for state reconstruction (correct — that section exists) but also `[event-model.md §3.6]` (nonexistent; the real section is `§4.5 Replay semantics` or `§8.6.8` for `store_divergence_detected`). The forbidden-use rationale is load-bearing on EV's observational-vs-state split; I can't confirm alignment without a valid citation.

### RC-015 — Investigator inputs bound to snapshot token — IMPLEMENTABLE

```go
type SnapshotToken struct {
    GitHeadHash         string
    BeadsAuditEntryID   string
    CapturedAt          time.Time // advisory display only
}

type InvestigatorInput struct {
    Token              SnapshotToken
    TargetRunID        uuid.UUID
    TargetWorkflowID   uuid.UUID
    TargetWorkflowVer  string
    TargetBeadID       *string
    BeadRecord         *beads.BeadRecord
    LastCheckpoint     *Checkpoint
    LastTransition     *Transition
    JSONLTail          []event.Envelope // observational only per RC-014
    WorkspaceState     WorkspaceState
    SessionLogRef      *string
    Category           ReconciliationCategory
    PlaybookRef        string
    WallClockBudget    time.Duration
}
```

The `schemas.md §6.1 InvestigatorInput` record is explicit (lines 26–42 of schemas). Every field has a type. Cross-references for `BeadRecord`, `EventEnvelope`, `Checkpoint`, `Transition` point at their owning specs. The one gap: `session_log_ref` cites `[workspace-model.md §5.3]` which does not exist; the real CASS-reference surface is in `workspace-model.md §4.7 WM-025` and it is a directory path, not a CASS handle. Fine at an implementer level (I can deref the path), but the citation drift is load-bearing for anyone validating schema alignment.

RC-015's "commits in git-DAG-parentage order" clause is straightforward.

### RC-017 — Wall-clock budget — IMPLEMENTABLE

```go
type ReconciliationPolicy struct {
    WallClockSeconds int `yaml:"wall_clock_seconds"`
}

func (d *Daemon) enforceBudget(ctx context.Context, invRun *Run, budget time.Duration) {
    deadline := invRun.StartedAt.Add(budget)
    go func() {
        <-time.After(time.Until(deadline))
        if invRun.IsAlive() {
            d.emit("reconciliation_budget_exhausted", invRun.ID, invRun.WorkflowID,
                int(budget.Seconds()), int(time.Since(invRun.StartedAt).Seconds()))
            d.fallbackVerdict(invRun, "escalate-to-human")
            invRun.Kill() // per handler-contract.md §4.6
        }
    }()
}
```

Defaults are named: Cat 2 = 600s, Cat 3 generic = 300s, Cat 6a = 900s. YAML policy field name `wall_clock_seconds` is specified. `budget_ref` on DOT per `control-points.md §6.5` is the binding mechanism — which lands in control-points (real section: `§4.7` or similar; haven't re-checked). Implementable.

### RC-018 — Budget exhaustion fallback — IMPLEMENTABLE

Per above. Key observation: the spec explicitly says "NOT write a reconciliation commit. The budget-exhausted event + daemon-default verdict are the durable trace" (line 414). This is the sensible choice, but note it subtly breaks RC-INV-003 ("the pair (verdict-emitted, verdict-executed) is the complete durable record"). Budget-exhausted runs produce neither the `reconciliation_verdict_emitted` commit nor a `Harmonik-Verdict-Executed` commit — only events. On the next restart, how does the detector know this investigator-run was budget-killed vs just crashed mid-flight? The answer (from RC-003 + RC-002) is that there's no in-flight reconciliation state to classify, so the outer run re-classifies from scratch. OK — the spec's bounded-recursion rule makes this consistent. But the invariant as stated in RC-INV-003 is a little too strong; it should say "for every reconciliation workflow that **reaches a verdict**" (which it does), but the "verdict-executed commit" pairing is not applicable to budget-exhausted runs. Minor wording nit.

### RC-019 — WIP capture before `reopen-bead` — PARTIALLY

```go
func captureWIP(investigatorRun *Run, outerRun *Run) ([]byte, error) {
    wt := outerRun.Workspace.Path
    cmd := exec.Command("git", "status", "--porcelain")
    cmd.Dir = wt
    porc, _ := cmd.Output()
    // diff + file listing
    diff, _ := exec.Command("git", "-C", wt, "diff").Output()
    // write into .harmonik/reconciliation/<inv_run_id>/wip-capture/
    ...
}
```

Implementable mechanically. Stuck point: RC-019 says the capture goes "in the reconciliation commit's body and/or as annotated files" — the "and/or" means the implementer picks. Pick "annotated files at `.harmonik/reconciliation/<inv_run_id>/wip-capture/` with a one-line reference in the commit body." Fine, but two implementations that both conform differ on where the capture actually lives, which breaks cross-implementation audit.

Also: the capture runs against the outer run's worktree — which may not exist (Cat 6a bullet 1 triggers precisely on "workspace path referenced by in-flight bead does not exist"). If the worktree is gone, there's nothing to capture. Spec doesn't say what WIP-capture does in that case. Implementer infers "empty capture with a note," but say it.

### RC-020 — Verdict vocabulary is the six-value enum — IMPLEMENTABLE

```go
type Verdict string

const (
    VerdictResumeHere           Verdict = "resume-here"
    VerdictResumeWithContext    Verdict = "resume-with-context"
    VerdictResetToCheckpoint    Verdict = "reset-to-checkpoint"
    VerdictReopenBead           Verdict = "reopen-bead"
    VerdictAcceptCloseWithNote  Verdict = "accept-close-with-note"
    VerdictEscalateToHuman      Verdict = "escalate-to-human"
)
```

Straightforward. `schemas.md §6.1` enumerates the six values with one-line semantics each. Every verdict has a mechanical-action row in `schemas.md §6.2`. Implementable.

### RC-021 — Exactly one verdict event per reconciliation workflow — IMPLEMENTABLE

```go
type ReconciliationState struct {
    VerdictEmitted  bool
    sync.Mutex
}

func (s *ReconciliationState) EmitVerdict(...) error {
    s.Lock(); defer s.Unlock()
    if s.VerdictEmitted {
        return errMultipleVerdicts // triggers RC-023 malformed-verdict handling
    }
    s.VerdictEmitted = true
    // ...
}
```

`MalformationReason` enum includes `multiple-verdicts` and `verdict-after-terminal` (schemas.md §6.1), so the invariant mapping to RC-023 is explicit. Implementable.

### RC-022 — Verdict commit lands atomically on the task branch — PARTIALLY

Carries the same "atomic git commit" undefined-plumbing gap as `execution-model.md §4.4 EM-016` — git does not offer a single-syscall atomic commit; `write-tree` + `commit-tree` + `update-ref` is the standard sequence with `update-ref` as the atomicity boundary. Inheriting the EM gap, not new to this spec.

### RC-023 — Malformed-verdict handling — IMPLEMENTABLE

```go
type MalformationReason string

const (
    MalformationUnknownVerdictValue  MalformationReason = "unknown-verdict-value"
    MalformationMissingRequiredField MalformationReason = "missing-required-field"
    MalformationExtraFields          MalformationReason = "extra-fields"
    MalformationWrongType            MalformationReason = "wrong-type"
    MalformationMultipleVerdicts     MalformationReason = "multiple-verdicts"
    MalformationVerdictAfterTerminal MalformationReason = "verdict-after-terminal"
)

func handleMalformed(invRun *Run, target *Run, raw []byte, reason MalformationReason) {
    emit("reconciliation_verdict_malformed", invRun.ID, target.ID, reason, excerpt(raw))
    fallback := &VerdictEvent{Verdict: VerdictEscalateToHuman, TargetRunID: target.ID}
    executeVerdict(invRun, fallback) // via RC-025
    invRun.Kill() // terminate without writing a malformed commit
}
```

Malformation enum exhaustive (six cases). Behavior is concrete: emit the malformed event, synthesize `escalate-to-human`, kill the subprocess. "MUST NOT attempt to interpret the malformed payload" is a compliance lever for reviewers. Implementable.

One subtle gap: the fallback-escalation verdict synthesized by RC-023 needs a snapshot token for RC-024's staleness check. Does the daemon re-capture a snapshot at fallback time? The spec is silent; implementer uses the fallback's emission time as the snapshot and proceeds.

### RC-024 — Verdict staleness check — IMPLEMENTABLE

```go
func isStale(snap SnapshotToken, target *Run) (bool, StaleDivergenceReason) {
    currentTip, _ := git.RevParse(target.TaskBranch)
    if currentTip != snap.GitHeadHash /* as scoped to target branch */ {
        return true, StaleReasonGitBranchAdvanced
    }
    currentAuditID, _ := br.LatestAuditEntryID(target.BeadID)
    if currentAuditID != snap.BeadsAuditEntryID {
        return true, StaleReasonBeadsAuditAdvanced
    }
    return false, ""
}
```

Implementable. `StaleDivergenceReason` enum is exhaustive (two values). The "scoped to target branch" in the git-head comparison is slightly ambiguous: `git_head_hash` in `SnapshotToken` (schemas.md §6.1) is documented as "SHA of project HEAD" — but the staleness check cares about the target run's task branch tip, not project HEAD. Either the snapshot captures the task-branch tip (not project HEAD) or the staleness check looks up the task branch separately. The spec text reads as the latter; the schemas comment reads as the former. Pick one.

Only **target run's** git branch or target bead's Beads audit entries count (line 472). Sibling beads or JSONL changes do NOT trigger staleness — good scoping.

### RC-025 — Verdict execution is durable and idempotent — IMPLEMENTABLE

The verdict-execution table at `schemas.md §6.2` gives the mechanical action, idempotency key / rule, and emitted events per verdict. Concrete enough to build:

```go
func executeVerdict(invRun *Run, v *VerdictEvent) error {
    switch v.Verdict {
    case VerdictReopenBead:
        key := fmt.Sprintf("%s:reopen", v.TargetRunID)
        if err := brAdapter.Reopen(v.TargetBeadID, key); err != nil { return err }
    case VerdictResumeHere, VerdictResumeWithContext:
        // re-dispatch target run; dispatcher idempotency check (outer run already running → no-op)
        dispatchNextNode(v.TargetRunID, v.Verdict == VerdictResumeWithContext, v.Context)
    case VerdictResetToCheckpoint:
        // transition_kind = architectural-rollback, rollback_to_state_id = v.CheckpointRef
        rollback(v.TargetRunID, *v.CheckpointRef)
    case VerdictAcceptCloseWithNote:
        annotateReconciliationCommit(invRun, evidence)
        if !alreadyClosed(v.TargetBeadID) {
            brAdapter.Close(v.TargetBeadID, fmt.Sprintf("%s:close", v.TargetRunID))
        }
    case VerdictEscalateToHuman:
        emit("operator_escalation_required", v.TargetRunID)
        markQuarantined(v.TargetRunID) // per operator-nfr.md §4.3
    }
    emit("reconciliation_verdict_executed", v)
    git.Commit(invRun.TaskBranch, emptyTree, "verdict-executed",
        trailer("Harmonik-Verdict-Executed", "true"))
    return nil
}
```

Idempotency keys are explicit per verdict. Each action's idempotent semantics are declared:

- `reopen-bead`: `<target_run_id>:reopen`, no-op if already open.
- `resume-*`: dispatch-layer idempotency (the outer run is either running — skip — or has a next node ready — dispatch).
- `reset-to-checkpoint`: state-id idempotency (if already at target state, no-op).
- `accept-close-with-note`: annotation append is idempotent on re-run; close-write is adapter-idempotent.
- `escalate-to-human`: deduplicated by target_run_id.

Gap: `resume-with-context` injects `v.Context` into the outer run's shared context per `execution-model.md §4.1 EM-005`. The spec says the injection "is additive (repeated application produces the same context map)." That's only true if the keys don't collide — if the investigator ran twice and synthesized different context values for the same key (e.g., `observations` as a string), the second application overwrites the first, which isn't idempotent. Implementer workaround: prefix context keys with the investigator run_id. Worth a note.

### RC-026 — Verdict-execution discovery on restart — IMPLEMENTABLE

```go
func classifyReconciliationRun(invRun *Run) ReconciliationCategory {
    tip := gitTip(invRun.TaskBranch)
    if hasVerdictCommit(tip) && !hasVerdictExecutedTrailer(tip) {
        return Cat3b // auto-resolver re-executes
    }
    return Cat5 // reconciliation is resolved
}
```

Clear mapping: verdict commit without `Harmonik-Verdict-Executed` trailer = Cat 3b. Cat 3b's auto-resolver is RC-026's re-execution path with a fresh staleness check. Implementable.

### RC-028 — `reopen-bead` triggers new run with fresh worktree + run_id — IMPLEMENTABLE

```go
// After reopen-bead verdict executes:
// Clear in-flight tracking for the bead (no active run_id in daemon state)
// Next claim spawns create_run() which allocates a fresh run_id
// Workspace manager's create_workspace path materializes a new worktree at <repo>/.harmonik/worktrees/<new_run_id>/
```

Fresh run_id, fresh worktree, fresh branch. Routes cleanly through `execution-model.md §4.3 EM-014` (one-bead-many-runs) and `workspace-model.md §4.9 WM-035` (re-run rule). Cross-reference number is wrong in the reconciliation spec (cites `§5.9`) but the mechanism is sound.

### RC-029 — `reset-to-checkpoint` preserves worktree and run_id — IMPLEMENTABLE

Intra-run rollback via `transition_kind = architectural-rollback`. The `execution-model.md §4.10 EM-044` citation resolves (that section exists). The worktree + branch are preserved; a new transition record is written with `rollback_to_state_id` = the target checkpoint's state_id. Implementable.

### RC-031 — Crash-recovery-tested — PARTIALLY

"Every detector under §4.3 / §8, every verdict-execution path under §4.5, and every staleness-check path under RC-024 MUST be exercised by at least one crash-recovery scenario test before landing."

Implementable in principle — the test scaffolding for crash-recovery is named in `docs/methodology/TESTING.md`. But: that file doesn't exist in specs/, and the spec acknowledges this (OQ-RC-003 promises migration to `testing.md §<layer>` once testing.md is finalized). Until then, the test obligation is named only in prose. An implementer can read prose — fine — but can't write an automated test-obligation compliance check that parses this spec. This is acceptable at MVH per the spec's own disclaimer; the follow-up is tracked.

## Detection categories (walk)

Walking each Cat and probing the detection rule for mechanical precision:

### Cat 0 — Infrastructure unavailable (§8.1 line 77)

**Detection rule:** `br` missing / wrong version / >5s timeout; Beads SQLite locked by non-harmonik; git index locked; `.harmonik/` unwritable; filesystem full.

**Mechanically precise?** Yes. Each is a direct filesystem/process probe. "Wrong version" needs a pin specification (cites `beads-integration.md §10.8` which does not exist; real cite is `§4.1 BI-002` or `§4.8`). "Filesystem full" is `statfs`. Timeout is 5s by default.

**Decision lives:** Detector code. No investigator.

### Cat 1 — Idempotent rerun (§8.2 line 89)

**Detection rule:** Last checkpoint's node has `idempotency_class = idempotent` per `execution-model.md §4.2 EM-009`.

**Mechanically precise?** Yes — a single field lookup on the node's DOT attribute. Concrete.

**Decision lives:** Detector code. No investigator. Auto-resolver: re-spawn the node.

**Edge case:** What if the node's DOT was amended between the original run and the restart (same node_id, new `idempotency_class`)? The spec is silent; implementer uses the DOT as-loaded at the current daemon start.

### Cat 2 — Non-idempotent in-flight (§8.3 line 105)

**Detection rule:** Node's `idempotency_class ∈ {non-idempotent, recoverable-non-idempotent}` AND bead `in_progress` AND no `run_completed` or `run_failed` event since the checkpoint.

**Mechanically precise?** The first two conjuncts are precise (DOT field + Beads read). The third is ambiguous per RC-014 discussion — "no `run_completed` event" reads like a JSONL state-fact query, which is forbidden. Rephrase as "no run-terminal marker on the task branch" (which is a git read: no merge commit, no terminal checkpoint, etc.) and the rule becomes precise.

**Decision lives:** Detector code dispatches; investigator agent emits the verdict.

**Example concreteness:** "Builder mid-implementation, merge agent mid-merge" is helpful.

### Cat 3 — Store disagreement generic (§8.4 line 119)

**Detection rule:** "git, Beads, and JSONL tell inconsistent stories about the same run, and no Cat 3 sub-category matches."

**Mechanically precise?** Partially. "No Cat 3 sub-category matches" is a catch-all — order-dependent on the sub-category matches (3a, 3b, 3c). An implementer can make this precise: evaluate 3a then 3b then 3c, fall through to generic Cat 3. The two examples ("duplicate `transition_id` across commits in same run"; "bead `in_progress` but worktree missing without a run-terminal marker") are implementable as explicit rules.

**Decision lives:** Detector code for classification; investigator for verdict.

### Cat 3a — Torn Beads write (§8.4a line 131)

**Detection rule:** Intent-log at `.harmonik/beads-intents/` records an outstanding `br` write for `(target_run_id, transition_id, op)` AND Beads audit log either shows no corresponding entry OR shows an entry matching the idempotency key.

**Mechanically precise?** Yes — two file-system / CLI reads. The auto-resolver path routes to `beads-integration.md §4.10 BI-031` (audit-log check + re-issue). Detection boundary is crisp.

**Decision lives:** Detector + auto-resolver. No investigator.

### Cat 3b — Verdict-unexecuted (§8.5 line 143)

**Detection rule:** Investigator task branch has `reconciliation_verdict_emitted` commit AND no subsequent `Harmonik-Verdict-Executed: true` commit.

**Mechanically precise?** Yes — two trailer-scans on the branch tip + parent chain. Implementable directly.

**Decision lives:** Detector + auto-resolver (RC-026 re-execution path). No investigator.

### Cat 3c — Inverse premature-close (§8.6 line 155)

**Detection rule:** Merge commit on target branch tagged `Harmonik-Run-ID R` (for a run R whose workflow reached a success terminal state) AND bead for R is still `in_progress` AND no subsequent in-flight checkpoints for R.

**Mechanically precise?** Yes — three checks on git + Beads. "Reached a success terminal state" means the workflow's last transition's `outcome.status ∈ {SUCCESS, PARTIAL_SUCCESS}` (per `execution-model.md §4.3 EM-015c`) — detectable from the transition record on the merge's parent commit. Auto-verdict is `accept-close-with-note`.

**Decision lives:** Detector + auto-resolver. No investigator.

### Cat 4 — Recoverable known state (§8.7 line 169)

**Detection rule:** Agent was in a well-defined retry/backoff state at crash time (rate-limited pending retry timer, waiting for human gate).

**Mechanically precise?** Partially. "Rate-limited pending retry" is observable via the last event being `agent_rate_limited` without a subsequent `agent_rate_limit_cleared` (via JSONL tail, which is... not forbidden by RC-014 as long as it's observational-only and not state-reconstruction). "Waiting for a human gate" is observable via `control-points.md` gate state. Both mechanically detectable in principle but the detection bits are not enumerated in the spec — implementer infers.

**Decision lives:** Detector + auto-resolver (re-arm the retry timer or re-present the gate). No investigator.

### Cat 5 — Clean restart (§8.8 line 181)

**Detection rule:** Nothing in-flight for this run.

**Mechanically precise?** Yes — the empty case. Implementable as the fallthrough after all other categories fail to match.

**Decision lives:** Detector. Auto-resolver is no-op.

### Cat 6a — Integrity violation, LLM-triageable (§8.11 line 193)

**Detection rule:** Four OR'd conditions:
1. Workspace path referenced by in-flight bead does not exist AND transition-record sibling file absent.
2. Trailer-vs-sibling-file mismatch on checkpoint commit.
3. Worktree has in-progress git operation (rebase/merge/cherry-pick/bisect) not tracked by harmonik — detected via `.git/rebase-merge`, `.git/MERGE_HEAD`, etc.
4. Bead `in_progress` with two or more task branches each advertising `Harmonik-Run-ID` without `Harmonik-Verdict-Executed`.

**Mechanically precise?** Three of four are precise. Bullet 1 conflates two concerns: (a) the workspace is missing (a `WorkspaceState.path_exists == false` check — Cat 6a bullet) vs. (b) the workspace is missing AND no terminal marker exists (which overlaps Cat 3 bullet 2 "bead `in_progress` with worktree missing and no terminal marker"). Either the workspace-missing-but-no-terminal case is both Cat 3 generic and Cat 6a bullet 1 (precedence?), or the carve between Cat 3 and Cat 6a's bullet 1 hinges on "transition-record sibling file absent" (which is Cat 6a-only). The precedence rule should be: if the sibling file is absent, Cat 6a; if only the workspace is missing but the sibling file is present, Cat 3 generic. Implementer infers; the spec should state.

**Decision lives:** Detector dispatches; investigator for verdict (default `escalate-to-human`, MAY downgrade).

### Cat 6b — Integrity violation, mechanically unrecoverable (§8.11a line 210)

**Detection rule:** JSONL corrupt past byte offset; OR JSONL references checkpoint hash missing from git object database; OR `git fsck` fails.

**Mechanically precise?** Yes — direct checks. The "JSONL is corrupt past a byte offset" is defined in `event-model.md` mid-file corruption rule (real cite: `§8.9(e)` or similar; the reconciliation spec miscites `§3.6`).

**Decision lives:** Detector. No investigator. Auto-escalate.

## Action-mapping layer (§8.12) — precision

The table at lines 230–242 gives 11 rows with columns: category, default action, investigator spawned, typical verdict, auto-resolver present. Every category has a default resolution:

- Cat 0 → halt + degraded status (auto-resolver = wait-and-retry).
- Cat 1 → auto-resume (auto-resolver = spawn node).
- Cat 2 → dispatch investigator.
- Cat 3 → dispatch investigator.
- Cat 3a → auto-resolve via adapter re-issue.
- Cat 3b → auto-resolve via verdict re-execution.
- Cat 3c → auto-verdict `accept-close-with-note` + mechanical close.
- Cat 4 → auto-resume with pending action.
- Cat 5 → no-op.
- Cat 6a → dispatch investigator.
- Cat 6b → auto-escalate operator without investigator.

**Implementable without ambiguity?** Mostly yes. One caveat: the table is duplicated at `schemas.md §6.3` as "the canonical copy" and §8.12 says "divergence is a lint failure." For an implementer, this is a tooling requirement (a lint script that asserts the two tables match row-for-row) — the spec doesn't specify the script's contract but the two copies I read are consistent.

Every category has a default action: yes (all 11 rows populated).
Every auto-resolver category has a named resolver: yes per RC-008 (lines 302–305).
Every investigator-required category has a playbook: required by RC-016 (lines 391–395) but the playbook specifics live in S01-owned YAML per RC-004, post-MVH.

**Dispatch is deterministic:** a single switch on `ReconciliationCategory` compiles directly from §8.12.

## Detectors (§4.3)

Detectors are pure mechanical checks. RC-005 (line 281) is explicit: "The §4.3 detectors MUST live in the daemon's Go code (mechanism-tagged functions), NOT in the S01 workflow library." RC-013 (line 341) says the detector emits `reconciliation_category_assigned` before any dispatch; this is a deterministic, non-agentic step.

**Interface contract between detector and daemon:**

```go
type Detector interface {
    Classify(ctx context.Context, run *InFlightRun, evidence *Evidence) (ReconciliationCategory, string, error)
    // Second return is the detection-rule-name for observability per RC-013.
}

type Evidence struct {
    GitState        *GitReader             // access to git DAG, trailers, sibling files
    BeadsAdapter    beads.Adapter          // query bead status
    JSONLReader     *JSONLDivergenceReader // scoped use only per RC-014
    IntentLog       *IntentLogReader       // .harmonik/beads-intents/ per §4.10 BI-030
    Workspace       *WorkspaceReader       // path-exists, git-in-progress-op
}
```

Detectors are ordered (Cat 0 first per RC-012, then Cat 3a/3b/3c before Cat 3 generic per §8.4, then the rest). The daemon runs them in sequence per in-flight run; first-match wins.

**The spec does not declare the interface type.** An implementer must invent it. The interface surface is discoverable from the detectors enumerated in §8 and the permitted/forbidden JSONL reads in RC-014, but naming the interface would tighten the contract.

## Investigator-agent contract (§4.4)

RC-015 declares the investigator-input shape with precision via `schemas.md §6.1 InvestigatorInput` (10 fields, each with a type). RC-016 says S01 owns per-category playbooks with check sequences and verdict-selection rubrics — content deferred to S01 subsystem spec, which is post-MVH.

**Does it match HC's handler interface?** Mostly. Investigators are cognition-tagged nodes (RC-015 tags `cognition`) executed by Claude Code agent subprocesses, per the `[role: investigator]` INFORMATIVE note at line 387–388. They consume handler-contract's subprocess + watcher protocol (progress-stream messages, outcome emission per `handler-contract.md §4.2 HC-007`). The output of an investigator is a `VerdictEvent` which rides on top of the `outcome_emitted` progress-stream message.

**Concrete I/O:** Yes. Input = `InvestigatorInput` record; output = `VerdictEvent` record. Both have explicit field schemas.

**Gap:** The investigator's output path is "emit a `VerdictEvent` as the outcome payload of a `handler-contract.md` subprocess." The spec does not say this explicitly — it says "emit a verdict event" abstractly. The plumbing from "agent emits verdict in progress-stream" → "daemon validates, writes verdict commit, executes" is spread across RC-021 (one verdict), RC-022 (atomic commit), RC-023 (malformation handling). Concrete but requires careful reading.

## Verdict vocabulary and execution (§4.5)

**Enum exhaustive?** Six values enumerated in `schemas.md §6.1` (line 75–82). Every value has a one-line semantic gloss. Every value has a mechanical-action row in `schemas.md §6.2`. Every value's idempotency rule is declared.

**Which subsystem consumes each verdict?**

- `resume-here`, `resume-with-context` → execution-model dispatcher (`schemas.md §6.2`: re-dispatch the outer run's current node).
- `reset-to-checkpoint` → execution-model rollback (transition_kind = architectural-rollback per `execution-model.md §4.10 EM-044`).
- `reopen-bead` → beads-integration adapter (reopen write with idempotency key) + workspace-model re-run rule (next claim gets fresh worktree).
- `accept-close-with-note` → beads-integration adapter (close write) + reconciliation commit annotation.
- `escalate-to-human` → event-model emit (`operator_escalation_required`) + operator-nfr quarantine (`§4.3`).

Every consumer is named. `schemas.md §6.2` is the authoritative table.

**Verdict-executed commit mechanics** (RC-025 bullet 3): append a second commit to the investigator's task branch carrying `Harmonik-Verdict-Executed: true` trailer. Payload-free, presence-only. This trailer is declared in `execution-model.md §6.2` (which exists; I verified). Implementable via `git commit --allow-empty` with the trailer.

## Reconciliation-as-workflow (§4.1)

**What's the workflow graph?** The spec says S01 ships a DOT workflow per investigator-required category (Cat 2, Cat 3 generic, Cat 6a) — three DOT files. The spec does NOT specify node content — that's explicitly out-of-scope (line 49: "DOT authoring details for the reconciliation workflow library ... owned by the S01 Orchestrator Core subsystem spec, post-MVH"). An implementer can't compile against a graph that hasn't been drawn; at MVH, S01 produces placeholder workflows and the spec will tighten later.

**Bootstrapping rule:** RC-001 says "dispatched deterministically by the daemon" — but the bootstrapping question ("when does the daemon load reconciliation workflows?") is not explicitly stated. Inferred answer from process-lifecycle.md §4.2 PL-005 step 7: reconciliation dispatch happens during startup, AFTER orphan sweep, AFTER Cat 0 pre-check, AFTER git-walk + Beads-query, AFTER in-memory model build. So reconciliation workflows are dispatched WHEN there are in-flight runs; the library is loaded as part of daemon startup before step 7.

**"Reconciliation workflow runs before other workflows" — true?** Partially. Reconciliation workflows are DISPATCHED before the daemon goes `ready`, but they EXECUTE in parallel with `ready` (per `process-lifecycle.md §PL-009`: "dispatched investigator workflows MAY remain in-flight and MUST NOT block the `ready` transition"). So the daemon is `ready` to accept new enqueues while reconciliation workflows are still running. An implementer needs to reconcile: can new runs start while a reconciliation for an old run is in-flight? The spec implies yes. But then: can a new run's checkpoint collide with a reconciliation's verdict commit? No, because reconciliation has its own task branch. OK — consistent, but should be stated as an invariant.

## Re-run vs. intra-run (§4.6)

**Mechanical distinction:**

- `reopen-bead` verdict → new run_id, new worktree, new branch (RC-028).
- `reset-to-checkpoint` verdict → same run_id, same worktree, same branch; revert to named checkpoint (RC-029).

The decision is keyed on the verdict enum value — completely deterministic. Implementable as a `switch v.Verdict` in the verdict-execution path.

**Routing:** `reopen-bead` routes to `workspace-model.md §4.9 WM-035` (re-run rule) which the spec miscites as `§5.9`. `reset-to-checkpoint` routes to `execution-model.md §4.10 EM-044` (correctly cited).

## Testing obligation (§4.7) — test surface

RC-031 names three test surfaces:
1. Every detector in §4.3 / §8.
2. Every verdict-execution path in §4.5.
3. Every staleness-check path in RC-024.

Each must be "exercised by at least one crash-recovery scenario test before landing." `docs/methodology/TESTING.md` names the crash-recovery test layer. I haven't read that file but its existence is declared.

**Concrete test scaffolding needed:**

- 11 crash-recovery scenarios, one per category.
- 6 verdict-execution idempotency tests, one per verdict.
- 2 staleness-check tests (git-branch-advanced, beads-audit-advanced).
- Malformation handling test per `MalformationReason` enum (6 cases).
- Budget-exhaustion test (RC-018).
- WIP-capture test for `reopen-bead` (RC-019).

Test surface is concrete enough to enumerate. The harness is named but not specified in this spec; the migration to `testing.md §<layer>` is queued (OQ-RC-003).

## Cross-spec reach-ins

This is the most concerning surface for implementability because the citations are broken.

**Process-lifecycle (PL)** — `§8.2` cited throughout; real is `§4.2 PL-005`. The spec assumes PL's startup sequence invokes reconciliation at step 7. Reading PL's actual §4.2 PL-005 confirms: steps 1–8 deterministic; step 7 dispatches reconciliation per `[reconciliation.md §9.2a]` (which is this spec's action-mapping — miscited by PL too). Both specs miscite each other but converge on the right intent.

**Workspace-model (WM)** — `§5.1` (Lease-by-run, real is `§4.3`), `§5.3` (session log, real is `§4.7`), `§5.8` (branching, real is `§4.6`), `§5.9` (re-run rule, real is `§4.9 WM-035`). RC-028's dependency on WM-035 (fresh worktree on reopen) resolves correctly once you find the real section. RC-015's dependency on the session-log metadata resolves to WM-025 (session-log directory layout).

**Execution-model (EM)** — `§4.5 EM-023`, `§4.4 EM-018`, `§4.10 EM-044`, `§4.7`, `§5 EM-INV-005`, `§6.2` — these are all **correct** citations. EM's numbering is stable and the reconciliation spec's references to EM resolve. RC-INV-001 cites `EM-INV-005` (git wins) — exists; RC-028 cites `EM-014` (one-bead-many-runs) — exists; RC-029 cites `EM-044` (architectural-rollback) — exists.

**Beads-integration (BI)** — `§10.8a` (adapter idempotency, real is `§4.10` + BI-029/BI-030/BI-031), `§10.4` (real is `§4.4`), `§10.7` (real is `§4.7`), `§10.8` (version pin, real is `§4.8`), `§10.3` (real is `§4.3`). BI's content is consistent with what reconciliation assumes (adapter re-issue for torn writes, store-authority rules), but every citation misses. Systematic.

**Event-model (EV)** — `§3.2` (real is `§8.x` for taxonomy, `§6.3` for payloads), `§3.4` (real is `§4.4 Durability classes`), `§3.6` (real is `§4.5 Replay semantics`), `§4.1` (real is `§4.1 Envelope`... wait, that's a collision since real EV has `§4.1 Envelope` — but reconciliation cites `§4.1` for UUID v7 which is in real EV `§4.2`). Events themselves are declared in `event-model.md §8.6` (reconciliation lifecycle). RC's §6.5 enumerates nine co-owned events; every one of them is declared in EV's §8.6 / §8.7 tables (I verified). Semantics align; section numbers don't.

**Handler-contract (HC)** — `§4.6` (error propagation / cleanup, real exists at `§4.6`) — cited correctly. `§4.11` (skill injection) — real is at `§4.11` — correct.

**Control-points (CP)** — `§6.5` (policy surface, cited by RC-017 for `budget_ref`), `§6.11` (skill declaration) — I didn't verify CP section numbers, but RC cites them consistently.

**Architecture** — `§4.2 ZFC test` (cited correctly), `§4.6 Amendment protocol` (cited correctly).

**Summary:** EM, HC, Architecture citations resolve. BI, WM, PL, EV citations are **systematically wrong** — a stale numbering scheme. An implementer has to re-resolve each citation by requirement-ID or section-title search. This is a correctness-preserving transformation but imposes friction.

## Does RC cite or silently consume?

- **PL startup sequence:** Cited (RC-012 relies on `process-lifecycle.md §8.2` step 3; the orphan sweep precondition at line 75 cites `§8.2 step 1a`). Cites, not silent — modulo the broken numbering.
- **WM workspace states:** Cited (RC-015 InvestigatorInput includes `workspace_state`, `session_log_ref`; `§9.3 Co-references` enumerates four WM sections). Cites.
- **EM run reconstruction:** Cited (RC-014 forbidden-uses cites `execution-model.md §4.7`; RC-015 references EM's `§6.1` via `Checkpoint` / `Transition` types).
- **BI bead-writes:** Cited (RC-025 reopen/close verdicts route through `beads-integration.md §10.8a`; verdict-execution table `schemas.md §6.2` cites `§10.8a` for adapter idempotency). Cites.
- **EV events:** Cited (nine reconciliation events, each cites `event-model.md §3.2` for payload schema; §6.5 enumerates). Cites.

No silent consumption observed — but the cited section numbers are wrong half the time. This is a mechanical-fix review note, not a semantic gap.

## Implementability score

**Shape: taxonomy-first, 11 categories, concrete dispatch.** 8/10.
**Verdict vocabulary and execution table.** 9/10 — mechanical-action table at `schemas.md §6.2` is exemplary.
**Investigator-agent I/O contract.** 7/10 — `InvestigatorInput` schema is concrete; the agent-subprocess-to-daemon output path (VerdictEvent → outcome_emitted progress-stream message) is inferable from HC but not explicitly drawn.
**Detector interface contract.** 6/10 — the 11 detection rules are precise; a `Detector` interface is not declared; ordering (Cat 0 first; 3a/3b/3c before 3 generic; 6a vs 3 precedence) is implicit in the prose.
**Cross-spec citation correctness.** 3/10 — EM, HC, Arch resolve; BI, WM, PL, EV citations use a stale numbering scheme.
**Reconciliation-as-workflow mechanics.** 7/10 — RC-001/002/003 are crisp; `workflow_class` tag mechanism needs typing; S01 library lookup interface needs naming.
**Auto-resolver determinism.** 8/10 — every auto-resolver category has a declared resolver path; some are compositions of cross-spec mechanics and require careful chaining.
**Testing surface.** 7/10 — enumerable but the test-harness citation is a placeholder.
**Invariants (RC-INV-001..005).** 9/10 — each invariant is sharp and maps to a single implementation check.

**Overall: 6.5 / 10.** The spec is implementable by a careful Go engineer who is willing to (a) re-resolve every cross-spec citation by title/requirement-ID search, (b) invent the daemon ↔ S01-library loading interface, (c) invent the `Detector` Go interface type, (d) interpret the ambiguous Cat 2 JSONL-read-vs-forbidden-state-read tension, and (e) pin a precedence rule between Cat 3 generic and Cat 6a bullet 1 (workspace missing).

## Affirmations

1. **The taxonomy-first reading order works.** Putting §8 before §4 is the right call — an implementer reads the 11 categories first and builds a mental model of "what can go wrong" before meeting the requirements that reference the taxonomy. Following `spec-shape: taxonomy-first` discipline across the whole spec produces a tighter artifact than a requirements-first shape would have.

2. **The dual table (§8.12 + `schemas.md §6.3`) with lint-equivalence obligation** is a good pattern. Lets taxonomy-first reading preserve its locality (dispatch row next to the category description) without duplicating authority (schemas.md declares "canonical").

3. **RC-023 malformed-verdict handling converts LLM-generated-text unreliability into a deterministic escalation.** This is exactly the ZFC discipline architecture.md §4.2 demands: every ambiguity in an LLM's output maps to a bounded daemon action. The six `MalformationReason` enum values are exhaustive (unknown value, missing field, extra fields, wrong type, multiple verdicts, verdict-after-terminal) — covers the realistic failure modes.

4. **RC-024 staleness check is scoped correctly.** Sibling beads and daemon JSONL don't trigger staleness — only the target run's git branch and target bead's Beads audit entries count (line 472). Without this scoping, every parallel run would invalidate every in-flight investigator's snapshot — the daemon would thrash re-dispatching reconciliation. The scoping rule is correct and explicit.

5. **Cat 3a / 3b / 3c as auto-resolvers (vs. investigator dispatches).** The insight that these three sub-categories are mechanically resolvable (adapter re-issue, verdict re-execution, direct close-write) without LLM reasoning saves tokens and keeps the hot-path deterministic. Good engineering judgment baked into the taxonomy.

6. **The bounded-recursion argument (RC-002 + RC-003 + RC-INV-002).** One verdict commit + no intermediate checkpoints = no mid-investigation durable state to re-classify on crash. This is elegant: the recursion-of-reconciliation never arises because there's nothing to see. The argument fits in three requirements and an invariant.

## Recommendations

1. **Fix the cross-spec citation numbering wholesale.** Every reference to `beads-integration.md §10.x`, `workspace-model.md §5.x`, `process-lifecycle.md §8.x`, `event-model.md §3.x` should be migrated to the current numbering (§4.x etc.). Use a citation-by-requirement-ID format where possible (e.g., `[beads-integration.md BI-031]`) to survive future re-numbering.

2. **Declare `Workflow.class` (or `Workflow.metadata["workflow_class"]`) as a typed field in `schemas.md §6.1`.** The taxonomy keys (RC-002, EM-026) rely on this tag but it's currently a prose-only attribute.

3. **Resolve the Cat 2 detection-rule vs. RC-014 tension.** Either rephrase the Cat 2 rule as "no run-terminal marker on the task branch" (a git-state fact, not a JSONL event read), or carve out a permitted JSONL-use for detecting run-terminal markers.

4. **State the Cat 3 generic vs. Cat 6a bullet 1 precedence rule.** If the workspace-path is missing AND the transition-record sibling file is absent, Cat 6a wins. If only the workspace is missing, Cat 3 generic. Write this down.

5. **Declare the `Detector` Go interface in §4.3 or `schemas.md §6.x`.** Currently implicit; a named interface tightens the detector-vs-daemon contract.

6. **Specify the reconciliation library loading + lookup surface.** When does the daemon load the S01-shipped DOT + YAML? How is the per-category lookup performed? Add a requirement under §4.1 (Reconciliation-as-workflow) or §4.4 (investigator contract).

7. **Clarify RC-INV-003 wording.** "Complete durable record is the (verdict-emitted, verdict-executed) pair" — but budget-exhausted runs produce neither commit. Rephrase: "for every reconciliation workflow that emits a verdict, the durable record consists of exactly two commits." Budget-exhausted and malformed-verdict paths produce events, not commits, and are exempt.

8. **Add a fallback-snapshot rule for RC-023.** When malformation handling synthesizes an `escalate-to-human` fallback, which snapshot token binds the fallback? Probably: a fresh snapshot captured at malformation-detection time. State it.

9. **Name a default retry cadence for Cat 0 pre-check.** RC-012 says "configurable" but no default; pick one.

10. **State the `resume-with-context` context-merge rule.** If repeated `resume-with-context` verdicts apply different contexts with the same key, the later overwrites the earlier. Either state that contexts are keyed by investigator_run_id, or state that "additive" means "no-conflict-by-construction" (last-wins).
