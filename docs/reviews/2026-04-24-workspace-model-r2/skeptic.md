# Round 2 Skeptic Review — workspace-model.md v0.3.0

## Verdict

Round-1 integration did the big moves honestly: the state-machine contradiction
is resolved by retiring `setup` (lines 271-279, 668-688), the lease-lock gets
a birth/death/discovery triple (WM-013a/b/c/d, lines 235-269), WM cedes event
wire-format to EV throughout (WM-015, WM-017 retired, §6.5 renumbered and
rebuilt, lines 281-310, 647-665), the original-implementer gets a mechanical
record field (`implementer_handler_ref` + trailer walk, WM-022 line 349-355),
and §5 invariants no longer read as §4 copy-paste. That is a lot of work to
land in one revision and most of it survives pressure.

But the integration introduced one load-bearing fabrication, carried forward
two genuine cross-spec conflicts as OQs that are probably too load-bearing
to defer, under-delivered on the §A.3 rationale refresh, and dressed one
invariant up rather than satisfying the selection test. The most acute
single defect: **WM-022 cites a git trailer (`Harmonik-Actor-Role`) that
execution-model does not declare** — EM-017 enumerates exactly five
trailers (`Harmonik-Run-ID`, `Harmonik-State-ID`, `Harmonik-Transition-ID`,
`Harmonik-Schema-Version`, optional `Harmonik-Bead-ID`) at line 212, and
`Harmonik-Actor-Role` appears nowhere in the corpus outside of two
workspace-model.md sites (lines 351 and 795). The critic R1's proposed
fix prescribed the walk-trailers shape; the author adopted it without
verifying the trailer exists, and now WM-022's identification mechanism
does not compile against its own cited source.

Recommendation: **one more pass before `reviewed`**, focused on the three
load-bearing issues below (Challenges A, B, C). The selection-test
regression (Challenge D) and the invariant-sensor double-book (Hidden
Assumption 3) are cleanup that can ride the same pass.

Top 3-5 findings:

1. **WM-022's trailer-walk cites a non-existent trailer.** `Harmonik-Actor-Role`
   is fabricated; EM-017 does not declare it. The entire mechanical
   identification procedure dangles. See Challenge A.
2. **OQ-WM-005 (lock-path triangle) is filed but is probably not
   deferrable.** Three specs disagree on filename, format, and staleness
   semantics of the same file, and v0.3 formally pinned WM's version
   (WM-013a) while declaring via OQ that implementers cannot pick one
   that satisfies all three. WM-033's "match whichever filename was
   written by WM-013a on this daemon generation" is a single-daemon
   escape hatch that doesn't resolve what HC-044a fail-fast reads. See
   Challenge B.
3. **OQ-WM-006 (workspace_interrupted emitter identity) is deferred but
   WM-038's SOLE-WRITER MUST + EV §8.5.5's SOLE-EMITTER combination
   over-constrains operator-driven interrupts out of the wire.** An
   operator pause transitions `interrupt_state` per WM-038, but no
   event fires because the only emitter is reconciliation and the only
   detection category is Cat 6. See Challenge C.
4. **WM-INV-003's sensor is a cross-spec IOU.** It cites EM-024a as the
   sensor but EM-024a is a branch-tip monotonicity check, not an
   amend/rebase detector; the sensor does not cover the invariant's
   claim. See Challenge D.
5. **§A.3 rationale is missing the "why the three lock paths remained
   three" and the "why WM walks trailers that don't exist" honesty-
   notes.** The integration prose in §12 claims the lock-path
   coordination was "deferred to cross-spec cycles" — reasonable — but
   §A.3 now reads like the lock path is settled. See R1 regressions §3.

## Integration-fix audit — did round-1 fixes actually fix things?

Genuine fixes (all critic + implementer items landed as structural text):

- **C1/C9 `setup` state-machine contradiction.** Retired, §7.1/§7.2/WM-016
  aligned.
- **C3 / SP-2, SP-5 lease lifetime.** WM-013a/b/c/d (lock birth, release,
  discovery, anti-reuse) — but filename still contested across three
  specs. See Challenge B.
- **C4 / SP-4 verdict-enum.** WM-036 against six-value enum; `abandon`
  removed; `accept-close-with-note` added; OQ-WM-009 reasonable defer.
- **C5 interrupt-state terminal leak.** WM-037a + §7.1 row narrowing.
- **C6 / SP-1 event-taxonomy leak.** WM-015 cedes to EV; WM-017 retired;
  paired-phase `workspace_merge_status` adopted.
- **C7 sub-workflow branching.** WM-005a.
- **C8 WM-008 MUST/MAY.** Operator-policy-gated rewrite.
- **SP-3 run_id minting on reopen-bead.** WM-034 + WM-013d.
- **SP-6 merge-back node primitive.** WM-018a two shapes. Carries
  Outcome-attribution hidden assumption — see Hidden Assumption 4.
- **Front-matter / envelope / citation migration.** `spec-category`,
  8-spec `depends-on`, WM-ENV-001, 32-site migration, §A.4 reverse-drift
  map, OQ-WM-011 reasonable defer.

Partial / dressed-up:

- **C2 / SP-7 original implementer.** `implementer_handler_ref` + trailer
  walk. **Trailer is fabricated — see Challenge A.**
- **SP-9 invariant sensors.** Each invariant carries a `Sensor:` line,
  but INV-003's sensor doesn't span the claim. See Challenge D.
- **§5 invariant cross-subsystem rewrite.** INV-001/002/005 reworded
  OK; INV-003 reads as §4 rule with downstream subsystem list, not a
  cross-subsystem spanning rule. See Challenge D.

Net: ~80% real resolution. Three items dressed the finding rather than
resolving it (C2 via fabricated trailer; lock-path OQ defer; INV-003
sensor-doesn't-span). The ceremony cost is low because v0.3's
structural discipline is tight; the risk is a reader treating the
three as settled because the surface text landed clean.

## Challenges

### Challenge A — WM-022's trailer-walk cites a trailer that does not exist

**What the spec says.** Line 351:

> the workspace manager MUST walk the task branch in reverse from tip to
> the merge-base with the integration branch and select the most recent
> commit whose `Harmonik-Actor-Role` trailer (per [execution-model.md
> §4.4]) resolves to an agentic node

And line 795 (§9.1 Depends on):

> **[execution-model.md §4.4]** — checkpoint trailer schema
> (`Harmonik-Run-ID`, `Harmonik-Bead-ID`, `Harmonik-Actor-Role`) consumed
> by §4.5.WM-019 merge-preservation and §4.6.WM-022 implementer
> identification.

**What execution-model actually declares.** EM-017 at execution-model.md
line 212:

> Every checkpoint commit MUST carry the trailers `Harmonik-Run-ID`,
> `Harmonik-State-ID`, `Harmonik-Transition-ID`, and
> `Harmonik-Schema-Version`. The trailer `Harmonik-Bead-ID` MUST be
> present when the run is tied to a bead per §4.3.EM-014 and MUST be
> absent otherwise.

Five trailers, not six. `Harmonik-Actor-Role` is not among them, and a
corpus grep finds the string in exactly two places, both inside
workspace-model.md. The `actor_role` attribute EXISTS — on the Transition
record, execution-model §6.1 line 685 — but it is a field in the
sibling-file JSON, not a commit trailer.

**The mechanism WM-022 declares, therefore, does not compile.** An
implementer cannot walk trailers for a name no commit carries. The
correct mechanism — reading the sibling file referenced by
`Harmonik-Transition-ID` and consulting its `actor_role` field — is
more expensive (a `git show <commit>:.harmonik/transitions/<run_id>/<txn_id>.json`
for every commit on the walk) but works against the actual trailer
schema. Alternately, amend EM-017 to add the trailer.

**Load-bearing because.** This is the fix that R1 critic C2 specifically
called blocking, and the R1 integration adopted the fix text without
checking the cited source. The first merge conflict against a multi-node
agentic run produces undefined behavior: the handler that should run is
identified by a field the authoritative source says isn't on the commit.

**How to resolve.**
- (a) Amend WM-022 to walk the sibling file, not the trailer: "select
  the most recent commit whose transition-record sibling file (at the
  canonical path of [execution-model.md §4.4 EM-018 / EM-019]) carries
  `actor_role` resolving to an agentic node per [execution-model.md
  §6.1 Transition]." This is the cheap fix.
- (b) Push a foundation amendment to EM-017 adding `Harmonik-Actor-Role`.
  More expensive; changes checkpoint commit format for every run, not
  just conflict paths.
- (c) Store the implementer handle per-session on the sidecar (which
  already carries `agent_type` per WM-026 line 397) and walk sessions
  instead of commits. Requires a per-session "committed?" marker, which
  the spec does not currently carry.

(a) is the minimum-surface fix and matches the corpus as it stands.

### Challenge B — OQ-WM-005 punts a three-way conflict that v0.3 made worse by pinning a structured lock file

**What the spec says.** WM-013a at line 235-249 defines the lease-lock
file as a structured JSON document at `${workspace_path}/.harmonik/lease.lock`
with schema `{run_id, pid, created_at, ttl_sec}` and atomic write + fsync
discipline. WM-033 line 446-452 adds a cross-spec coordination note
listing the three distinct filenames. The NOTE at line 246 says
"implementers MUST treat this spec's filename as authoritative for WM's
writer side, and MUST NOT assume HC-044a's fail-fast path shares a
filename with the WM-owned lock." OQ-WM-005 (lines 907-912) tracks the
cross-spec coordination.

**What the neighbors actually do.** HC-044a (handler-contract.md line
433-437) declares a pidfile at `.harmonik/worktrees/<run_id>/.lock`
(which resolves to `${workspace_path}/.lock`, one directory shallower)
with content "the PID" (unstructured, atomic write at subprocess spawn,
removed on clean session termination), staleness detected by PID
liveness + argv match. PL-006 (process-lifecycle.md line 125) says
"Worktree locks. The daemon MUST inspect each worktree ... for lock
files (`.harmonik/lease.lock` or equivalent) ... Locks whose mtime
predates the current daemon's start time MUST be removed." PL uses
mtime as the staleness criterion.

**What the spec silently assumes.** That implementers running the full
corpus will (a) write WM's structured JSON lock AND (b) write HC's
PID-only lock, because HC's fail-fast path reads its own filename and
WM's sweep reads its own filename. OQ-WM-005's `Default-if-unresolved`
(line 912) says exactly that: "implementations running both paths
SHOULD write both filenames with identical content until bilateral
alignment is reached." But the two files have incompatible content
schemas (JSON object vs. bare PID), so "identical content" is
undefined.

**Where the assumption could break.**
1. **PL's mtime staleness criterion vs. WM's structured JSON + HC's
   live-PID probe.** Three distinct staleness tests. A lock with
   a recent mtime but a dead PID passes PL's test (not stale), passes
   HC's test (PID not live → reclaim), fails WM's test (no rule in
   WM-013a). Three sweep actions, one lock.
2. **Same-filename race between daemon generations.** WM-013a's
   write-to-temp + rename is atomic for a single generation. Between
   generations (crash + restart + orphan sweep), the prior generation's
   structured JSON and the new generation's structured JSON cannot
   coexist at the same path — the rename overwrites. HC-044a's
   detection relies on the old file being present for the liveness
   probe; if the new daemon's WM rewrites the WM-filename lock on
   startup before HC's launch fires, HC's orphan detection
   disappears.
3. **TTL semantic ambiguity.** WM-013a declares `ttl_sec` "advisory
   lifetime; informative for the orphan sweep, does not enforce
   auto-expiry." WM-033 says the sweep uses mtime (via PL-006's rule).
   Why carry ttl_sec at all if it's informative and no rule reads it?
   Candidate answers: (a) future sweep config, (b) external-tooling
   hint, (c) human debugging. All three are post-MVH; the field is
   ceremony in v0.3.

**How to test or bound.**
- **Cheap bound.** Narrow WM-013a's content schema to the minimum that
  satisfies all three consumers: `{pid: Integer, mtime: via
  filesystem}`. Drop ttl_sec and created_at from the MVH schema. If
  WM/HC agree on the filename (single file, with `${workspace_path}/.lock`
  winning — one directory shallower — to match HC-044a's stated path),
  all three detectors can read the same file.
- **Honest bound.** Escalate OQ-WM-005 to blocking for this revision
  cycle. A three-spec filesystem artifact without a single authority
  is the exact shape §4.10 interrupt-state told us to avoid (one spec
  owns the mutation; others observe). The lease-lock file should have
  a single owner and a single filename.
- **Worst bound if deferred.** Name in this spec (and in HC, and in
  PL) an explicit "dual-write compatibility shim" requirement that
  both filenames MUST exist with identical content OR one filename
  MUST be a hardlink to the other, AND MUST NOT be symlinks (some
  filesystems don't honor link-target fsync). Neither spec currently
  names this.

**Load-bearing because.** The orphan-sweep on daemon restart is the
single place where WM, HC, and PL all act on the same artifact. If
WM-013a wrote `.harmonik/lease.lock` (one level deep from the worktree
root), HC-044a reads `.lock` (zero levels deep), and PL-006 reads
`.harmonik/lease.lock` (matches WM) — then HC's orphan detection looks
at a file WM never writes. The current state is that HC's fail-fast
path is dead code against WM's writer side. This is the corpus shape
the critic flagged as "the first daemon-crash scenario."

### Challenge C — OQ-WM-006 defers an emission-identity conflict that has no MVH fallback

**What the spec says.** WM-038 at line 505 declares WM as SOLE writer of
`interrupt_state`. WM-039 at line 511 declares that `workspace_interrupted`
is emitted by the reconciliation detector (NOT by WM) per EV §8.5.5, and
the payload schema is `{workspace_id, run_id, detected_at, category}`
with `category = Cat 6` per EV. OQ-WM-006 (lines 914-919) tracks the
unresolved coordination: "Operator-driven interrupts (pause, stop)
transition `interrupt_state` via WM-038 but are not Cat 6."
`Default-if-unresolved`: "WM does not emit `workspace_interrupted` for
any path. Operator-driven interrupts surface via the operator-control
events of [event-model.md §8.7]; the workspace manager updates
`interrupt_state` silently per WM-038 without a dedicated wire event."

**What the spec silently assumes.** That `interrupt_state` can transition
without any wire event being emitted for that SPECIFIC workspace-level
observation, because operator-control events (`operator_pause_status`,
`operator_stopped`, `operator_resuming`) are daemon-scoped, not
workspace-scoped. Per event-model.md §8.7.6 / §8.7.7 / §8.7.8, these
events carry no `workspace_id` or `run_id`. A consumer that wants to
know "which workspaces entered `interrupt_state != none` at 13:47:22?"
has to:
1. Observe an `operator_pause_status` with `status=pausing`.
2. Enumerate every in-flight workspace.
3. Infer that each of them will soon have `interrupt_state =
   operator-paused` applied by WM silently.
4. Not observe any workspace-scoped event confirming the transition.

**Where the assumption could break.**
1. **Reconciliation detector can't observe WM's silent mutation**
   without polling. WM-039 says the transition must be "durable and
   observable by the reconciliation detector" but declares no
   mechanism. Does the reconciliation detector read the Workspace
   record on every tick? Walk workspaces? Subscribe to some internal
   bus? Nothing named.
2. **Crash recovery loses the operator-paused information.** WM-004
   says workspace_id is derivable from run_id; state reconstruction
   uses git + Beads per EM §4.7. But `interrupt_state` is a runtime
   record field not mirrored in git or Beads. On restart, WM has to
   reconstitute `interrupt_state` from... what? The JSONL operator-
   control events? The spec explicitly forbids JSONL replay for state
   reconstruction (EM-INV-001). The `operator_paused` event carries
   no workspace-scope; how does WM know which workspaces to mark?
3. **WM-040 clearance sensor is unverifiable.** "Any mutation of
   `interrupt_state` to `none` not preceded by a matching causing
   event is a violation, detected by the reconciliation detector's
   transition-audit pass per [reconciliation.md §4.3 RC-010]." But
   if the CAUSING event is daemon-scoped (`operator_resuming`) and
   the CLEARANCE is workspace-scoped, the transition-audit pass
   cannot correlate them without a workspace-identity carrier.

**How to test or bound.**
- **Option A (cleanest, requires EV amendment).** EV §8.5.5 widens the
  emitter list to `{workspace-manager (S06), reconciliation detector}`
  and widens the payload to carry the interrupt origin. WM emits for
  operator-driven paths, RC emits for Cat 6. The EV-025 "one owning
  spec for payload shape" is preserved because EV still owns the
  payload schema; this is adding an emitter row, not a co-owner.
- **Option B (cleaner at WM, defers the question).** WM declares a
  separate workspace-scoped event — say, `workspace_interrupt_state_changed`
  — whose payload carries `{workspace_id, run_id, new_interrupt_state,
  cause_event_id}`. EV registers it. `workspace_interrupted` remains
  reconciliation-only for Cat 6. Two events; clean owner-per-event.
- **Option C (accept the defer honestly).** Add to §2.2 an explicit
  out-of-scope item: "Workspace-scoped visibility of operator-driven
  interrupt_state transitions. Consumers MUST join operator-control
  events to the in-flight run set via Beads and correlate by
  wall-clock window." State that consumers have no atomic
  workspace-interrupt observation at MVH.

**Load-bearing because.** Every reconciliation detector that routes on
"is this workspace in a clean state to take a reconciliation action" has
to know `interrupt_state`. The spec says WM writes it; it doesn't say
how RC reads it. The current OQ default ("update silently") is a race
the spec declines to characterize.

### Challenge D — WM-INV-003's sensor doesn't span what the invariant claims

**What the spec says.** WM-INV-003 at line 545:

> Any git observer of a run's task branch MUST reject commits whose new
> tip is not a fast-forward descendant of the prior tip (i.e., no amend,
> rebase, filter-branch, or force-push may rewrite history on a task
> branch that has emitted `workspace_leased`). Git's native commit
> semantics are the contract; this spec adds no transactional layer.
> The rule constrains the handler-contract (S04 handlers writing
> checkpoint commits per [handler-contract.md §4.1]), the workspace
> manager (merge-back in §4.5), and execution-model's checkpoint-cadence
> rule per [execution-model.md §4.5 EM-023].
>
> Sensor: [execution-model.md §4.5 EM-024a] branch-tip monotonicity
> check — the daemon persists the last-observed task-branch-tip SHA
> per in-flight run and routes any non-fast-forward discrepancy to
> reconciliation Cat 3 per [reconciliation.md §8.4].

**What EM-024a actually detects.** Let me quote the effective claim: it
persists the last-observed tip SHA and detects when the current tip is
not a fast-forward descendant of the stored value. That catches ONE
shape of violation — a non-FF rewrite the daemon didn't authorize — but
it does NOT catch:

1. **Amend where parent chain is preserved.** `git commit --amend --no-edit`
   rewrites the tip commit but the new commit's parent is the old commit's
   parent — the daemon observes a tip SHA change, which LOOKS like a
   non-FF rewrite, but the shape is fundamentally the same: the new tip
   is not an ancestor of the old tip. EM-024a catches this. OK.
2. **Rebase of a non-tip commit.** `git rebase -i HEAD~3` reorders
   history below the tip. The tip SHA changes; EM-024a catches this
   too.
3. **Force-push that advances to a valid descendant.** If an
   unauthorized actor pushes a descendant that IS a FF of the stored
   tip, EM-024a does not flag it. The invariant says "no rewrite" —
   but legitimate forward progress isn't a rewrite. So this is a
   false-positive-avoider, not a gap.
4. **Filter-branch or git-replace.** Rewrites DAG-internal history
   without changing the tip? EM-024a wouldn't notice.

Case 4 is the one the invariant claims to cover ("filter-branch") that
the sensor does not cover.

**Is this invariant even this spec's invariant?** The selection test in
the template §5 (at /Users/gb/github/harmonik/docs/foundation/spec-template.md
line 227) says: "If the rule fits inside one subsystem's §4 without
reference to others, it is a requirement, not an invariant." WM-INV-003
lists three subsystems it constrains (handler-contract, workspace-manager,
execution-model). But the rule — "no amend/rebase/filter-branch on task
branches after workspace_leased" — is a **discipline on anyone writing
to a task branch**, which is exactly what EM-INV-004 retired tried and
failed to be (per the execution-model R2 skeptic review at
/Users/gb/github/harmonik/docs/reviews/2026-04-24-execution-model-r2/skeptic.md
Challenge D). The shape is the same — "any subsystem writing to git MUST
NOT do X." The WM instance fits one subsystem's §4 (handler-contract's
checkpoint-write discipline, or workspace-manager's merge-back
discipline) just as well.

**How to bound.**
- **Cheap.** Accept that WM-INV-003 covers case 1-3 via EM-024a and
  name filter-branch as a post-MVH concern, with an OQ for the
  unaddressed case.
- **Honest.** Retire WM-INV-003 and add a bullet to EM-INV-004 (if
  that one survives its own R2 review) that extends to "any git
  observer MUST NOT permit any history rewrite on the run's task
  branch." One cross-subsystem invariant covers it.
- **Structural.** Amend the sensor clause to actually span: "Sensor
  composite: (a) EM-024a tip-monotonicity for amend/rebase/force-push;
  (b) reconciliation's Cat 6a detector for filter-branch and
  git-replace (trailer-vs-sibling-file mismatch)." Names two sensors
  because no single one suffices.

**Load-bearing because.** The invariant-discipline discipline (sic) is
what keeps §5 from becoming a §4 dump. The execution-model R2 review
held three of three surviving invariants to the selection test; the WM
R2 should do the same. Four of five WM invariants pass; this one
doesn't.

## Hidden assumptions v0.3 introduces (not flagged in R1)

1. **WM-013c's discovery mechanism assumes `git worktree list --porcelain`
   output is parseable across git versions.** Line 258-263 names the
   command as the authoritative live-workspace check. Git's porcelain
   format is documented as stable but its fields have changed across
   git 2.x releases (e.g., `locked` field added in 2.13). The spec
   declares no minimum git version. A daemon running against a repo on
   a git version whose porcelain output is missing a field parses as
   "worktree not registered" and falsely orphans it. Bound: WM declares
   a minimum git version OR declares which fields are required in the
   porcelain output.

2. **WM-013d "Released-workspace re-use is forbidden" conflicts with
   WM-034's run_id-freshness rule at the mechanical layer.** Line 265-269
   says the path MUST NOT be re-leased; line 457-461 says the run_id
   MUST be fresh. These are two different claims — one about paths, one
   about IDs — but since path is derived from run_id (WM-002), they
   collapse into one: a fresh run_id gives a fresh path. So WM-013d is
   a consequence of WM-034, not an independent rule. The spec says so
   in WM-013d's closing sentence, but the ordering matters: the
   `rejection_reason` in `RunIdReuseForbidden` (§8 line 776) is
   actually detecting WM-013d's condition at write time. Keep one,
   retire the other, or declare them as two-sides-of-same-coin
   explicitly.

3. **WM-INV-001's sensor correlates three data sources but defines no
   arbitration rule.** Line 533: "correlates each live lease-lock file
   against Beads' record of the owning `run_id` AND against the daemon's
   live-run registry; any workspace with a lease-lock whose `run_id`
   does not appear as an in-flight run (or whose lease-lock is missing
   while the run is in-flight) is a violation evidence route." The
   three sources: (a) lock file contents, (b) Beads bead status, (c)
   daemon's in-memory live-run registry. The arbitration rule on
   disagreement is not named. EM-INV-005 says "git wins on completion
   disagreement" but this isn't git vs. Beads; it's lock-file vs.
   Beads vs. in-memory. A reconciliation detector hitting the three
   with three different answers has no declared resolution. Bound:
   name the arbitration rule or declare all three MUST agree as a
   precondition of no violation (and route any disagreement to
   reconciliation Cat 3 or Cat 6a per the detector's own policy).

4. **WM-018a's merge-node `Outcome` attribution to `merge-pending →
   merged` transitions is not mechanically sourced.** Line 321-325:
   "the merge node's `Outcome` (per [execution-model.md §6.1]) carries
   the terminal merge outcome and drives the §7.1 transition from
   `merge-pending` to `merged` or to `conflict-resolving`." But
   non-agentic nodes don't traverse handler-contract's Outcome-emission
   pipeline (HC-008 outcome_emitted message is handler-side). A
   non-agentic merge node dispatched directly by the orchestrator
   (WM-018a shape (a)) produces an Outcome how? The orchestrator
   synthesizes it, presumably (EM-023a's daemon-synthesized-Outcome
   pattern at execution-model.md line 294, for context-restore, is
   the parallel precedent). WM-018a doesn't say so. Implementer has
   to invent the synthesis rule or adopt the EM-023a pattern by
   analogy. Bound: name the synthesis: "The orchestrator MUST
   synthesize an `Outcome` per [execution-model.md §4.5 EM-023a]
   daemon-produced-outcome rule for the non-agentic merge node;
   `actor_role = daemon`."

5. **WM-022's "latest agentic node whose commits are ancestors of the
   task-branch tip" presumes linear history on the task branch.**
   Line 349-355: "walk the task branch in reverse from tip to the
   merge-base with the integration branch and select the most recent
   commit...". WM-INV-003 constrains task branches to fast-forward-only,
   which means a single linear history. But sub-workflow expansion
   (WM-005a) says nested node commits land on the parent run's branch,
   sequentially — still linear. A conflict-resolution commit
   re-attempted (§7.1 `conflict-resolving → merge-pending`) adds a
   new commit on top of the prior tip, also linear. So linearity is
   correct in the current spec. But the walk-direction assumption
   ("most recent commit whose trailer resolves to agentic") bakes in
   that a later non-agentic commit doesn't "belong" to the agent that
   produced the divergent work — that the resolving implementer is
   keyed on WHICH commit carries the conflict, not WHO wrote the code
   at the merge-base. The R1 critic raised this (Challenge 2 case 1:
   "which agent, when there are many") and the R1 fix answered "most
   recent" but didn't justify why most-recent is the right choice.
   An intuitive counter-example: a planner-then-builder two-node
   agentic run, where the PLANNER produced the architectural choice
   that conflicts with integration, and the BUILDER wrote the code.
   The walk selects the builder (later agentic commit), but the
   planner has the context the reviewer needs. Bound: name the
   "most-recent-wins" choice as an explicit decision with rationale
   in §A.3 or soften to "the implementer role resolved by workflow
   author's declaration; in MVH, the most-recent agentic commit."

6. **WM-024's budget rule "fresh budget issued per the handler-contract
   LaunchSpec default" assumes re-dispatches don't accumulate budget
   pressure.** Line 380-382. Handler-contract §6.1 declares LaunchSpec
   budget fields; WM says the prior session's unused budget is NOT
   automatically inherited and a fresh one is issued. This is fine
   for a single re-dispatch. But the §7.1 transition
   `conflict-resolving → merge-pending` (line 680) permits the cycle
   to re-enter on a subsequent merge attempt if the resolution commit
   triggers a new conflict — and each pass gets a fresh budget. An
   adversarial or pathological workflow could loop forever. No budget-
   ceiling is declared for the full conflict-resolution cycle. Bound:
   declare a max-re-dispatch-count per workspace per merge-back (and
   name it normative) OR declare reconciliation Cat 5 (replay-loop)
   as the detection route for repeated conflict-resolve cycles.

7. **WM-027's "subsequent sessions sidecar write does NOT re-emit
   `workspace_leased`" is a claim about emission, not about state.**
   Line 404-408: "For subsequent sessions within a workspace (WM-010
   allows many sessions per workspace), the sidecar write precedes
   handler launch but does NOT re-emit `workspace_leased`; it proceeds
   as a stand-alone sidecar-write operation covered by WM-026." Good.
   But §7.1's `ready → leased` transition (line 676) is keyed on "first
   session's sidecar + lease-lock file written." A workspace that is
   already `leased` and then gets a second session's sidecar write is
   in state `leased` and stays in state `leased`. Fine. But the
   pseudocode §7.2 `launch_session` (line 718-742) has a state-check:
   `IF workspace.state == "ready":` (line 733). If the state is
   already `leased` (subsequent session), the branch is skipped,
   NO lease-lock write happens, NO emission fires. Correct. But the
   pseudocode doesn't name a sensor that detects accidentally-reentrant
   `ready` state (would fire the lease-lock write and emission
   twice). An implementer who forgets to transition `workspace.state
   = "leased"` before the emission (the ordering declared in WM-016
   line 300) could re-enter the ready-branch on a second session,
   double-writing the lock and double-emitting `workspace_leased`.
   WM-016's Axes line (`idempotency=non-idempotent`) acknowledges
   this. Bound: add an explicit sensor or invariant — a lease-lock
   file whose existence blocks re-write.

8. **§A.4's reverse-drift migration map implicitly declares that the
   legacy `§5.x` anchors are gone and not coming back.** The NOTE at
   line 984: "Each peer spec's next revision cycle SHOULD apply this
   mapping. The migration is tracked corpus-wide in OQ-WM-011."
   `SHOULD`, not `MUST`. If a peer spec punts its migration, the
   broken inbound citations (48 of them per OQ-WM-011) persist. No
   deadline is named. A reader of a peer spec following an inbound
   cite gets a 404 in the §5.x space. Bound: either make the
   migration MUST in OQ-WM-011 (and track the TODO in each peer's
   revision history), or accept the stale cites and declare the
   §A.4 map the permanent canonical-lookup table (i.e., the map
   itself is the authority, and peers never migrate).

## R1 regressions

1. **§A.3 rationale does NOT discuss the three-way lock conflict.**
   Lines 966-980 cover why lease-by-run, why original-implementer,
   why canonical path, why orthogonal interrupt-state, why sub-workflow
   shares workspace, why failed-run persists, AND why WM cedes event
   wire-format. Those are all cleanly captured. But the lock-file
   conflict — actively contested across three specs per OQ-WM-005 —
   receives no rationale entry. A reader of the spec looking for
   "why is there an OQ here?" has to read OQ-WM-005 in isolation. A
   one-paragraph §A.3 entry naming the three-spec conflict and the
   chosen-for-now filename would close the loop.

2. **§A.3 does NOT explain why the `implementer_handler_ref` walk
   terminates at "most recent agentic commit" rather than some other
   shape.** R1 critic asked this; R1 fix adopted "most recent" without
   rationale. See Hidden Assumption 5.

3. **WM-ENV-001's "Types introduced (cross-subsystem)" table declares
   `LeaseLockFile` with `replay-safety=safe` (line 105).** But the
   file's content includes wall-clock timestamps (`created_at`) and a
   PID which is process-instance-specific. Replaying the same daemon
   run on a different host or different process generation produces
   different values for both. The replay-safety=safe claim is
   suspicious; the file is not replay-sensitive in the execution-model
   sense (it doesn't carry transition state), but "safe" typically
   means "deterministic on inputs." Consider `replay-safety=safe` as
   shorthand for "not in the replay path" and call that out.

4. **§6.5 renumbering left §6.3 unused.** Template §6 has subsections
   §6.1 (records), §6.2 (YAML/JSON), §6.3 (tabular), §6.4 (evolution),
   §6.5 (co-owned events). The spec uses §6.1, §6.2, §6.4, §6.5 —
   skipping §6.3. Template §6.3 is optional (tabular schemas), so the
   omission is not a rule violation, but it creates a numbering gap
   a linter might flag. Either add a brief §6.3 (even just a note:
   "This spec declares no tabular schemas") or accept the gap and
   note in §12 that the absence is intentional.

5. **WM-017's retirement text (line 307-311) says "Do NOT reuse the
   ID" but does not say the ID is burned in the conformance profile.**
   §10.1 Core MVH at line 850 mentions retired IDs parenthetically:
   "Retired IDs (WM-017, WM-INV-004) MUST NOT be implemented". Fine.
   But the conformance profile at line 850-855 says requirements run
   "WM-001 through WM-040." An auditor checking completeness would
   find WM-017 missing and either ask "is this an intentional gap?"
   (correct read) or assume the spec is incomplete. Explicit
   "WM-001 through WM-040 excluding retired WM-017" — which the spec
   DOES say — is clean; mark this as affirm-not-regression.

## Over-specification vs under-specification

### Over-specified in v0.3

1. **WM-013a's `ttl_sec` field.** Declared required but no rule reads
   it. See Hidden Assumption 2 analog. Either make it load-bearing
   (reduces the orphan-sweep window) or drop to MAY/optional.

2. **§4.a envelope's eight elements (a)-(h) are fully populated.**
   Good hygiene, but (c) "Types introduced" declares `LeaseLockFile`
   with axes that are surface-plausible but not justified in the
   spec. See R1 regression 3.

3. **WM-036 six-row classification table.** The table is comprehensive
   but the two terminal rows (`accept-close-with-note`,
   `escalate-to-human`) both say "no re-run attempted; workspace
   remains in its current terminal state". Two rows with identical
   semantics. Collapse to one row or name why the two verdicts
   produce distinguishable behavior (they don't, per the current
   table).

### Under-specified in v0.3

1. **WM-022's trailer-walk mechanism does not exist.** See Challenge
   A. This is the biggest under-delivery in the revision.

2. **OQ-WM-006's default is "no wire event for operator-driven
   interrupts."** See Challenge C. A silent state transition is
   not a default; it is a gap.

3. **WM-033's liveness-probe discipline cites HC-044a by hyperlink
   but doesn't name the probe mechanism** (line 448-449: "the
   liveness-probe discipline of [handler-contract.md §4.10
   HC-044a]"). An implementer reading WM alone doesn't know the
   probe is `kill(pid, 0)`. One sentence would close it.

4. **WM-018a does not define a "merge node" in execution-model's
   node-type enum.** R1 implementer SP-6 asked for either (a) a new
   `node_type = "merge"` or (b) a workflow-level terminal-phase
   marker. v0.3 takes neither: the merge node is "any node of one
   of two shapes." An implementer has to either recognize the merge
   via some attribute (not declared) or treat every non-agentic
   node as potentially-merge-back (reading the workflow structure
   to find the merge target). The R1 alternative "name it as a
   workflow-level phase" is also not taken. Under-specified.

5. **§8 `InterruptOnTerminalWorkspace` failure class (line 781) says
   "silent reject; operator-observability log entry only."** But no
   normative requirement obliges the log entry. The class is declared
   but the enforcement is prose in §8. Move the obligation into
   WM-037a or into a new WM-037b.

## Cross-spec promises that aren't yet honored

Seven new OQs. Evaluating deferral-realism:

- **OQ-WM-001 (migrate test obligations to testing.md).** Realistic
  defer; testing.md doesn't exist.
- **OQ-WM-002 (operator-configurable integration-branch template).**
  Realistic defer.
- **OQ-WM-003 (post-merge session-log archival policy).** Realistic
  defer.
- **OQ-WM-004 (interrupt_state granularity for mixed operator+crash).**
  Realistic defer.
- **OQ-WM-005 (lock-path alignment WM/HC/PL).** **NOT realistic to
  defer.** See Challenge B. Three specs write to three different
  filesystem artifacts that the corpus asserts are the same thing.
  The OQ's default ("write both filenames with identical content")
  is unimplementable — the content schemas are incompatible. This
  should be blocking for `reviewed` status.
- **OQ-WM-006 (workspace_interrupted emitter identity for operator-driven
  interrupts).** **Marginally blocking.** See Challenge C. The
  default ("WM does not emit") leaves reconciliation without a
  wire-observable signal for operator-paused workspaces. If
  reconciliation can observe via direct Workspace-record read (which
  it can, since reconciliation reads state per RC-014), the defer is
  acceptable. But the spec doesn't name this reading-path.
- **OQ-WM-007 (conflict-resolution skill name).** Acceptable defer;
  the default (prompt-only dispatch) degrades gracefully.
- **OQ-WM-008 (failed-run retention window).** Acceptable defer.
- **OQ-WM-009 (worktree_disposition bilateral attribute on Verdict).**
  Acceptable defer; the six-value enum is stable.
- **OQ-WM-010 (post-MVH cleanup-workflow home).** Acceptable defer.
- **OQ-WM-011 (inbound citation migrations).** Acceptable defer; §A.4
  map is the workaround.

Net: 11 OQs, two (005, 006) that should probably block advancement.

## Definitional drift

- **workspace vs worktree / session vs run / in-flight vs terminal**:
  all clean. §3 glossary distinguishes, body usage consistent.
- **lease vs lock**: clean; minor hyphenation drift ("lease-lock" in
  WM-033 vs. `LeaseLockHeldByOrphan` in §8 line 778). Cosmetic.
- **original implementer**: clean. §3 line 73 makes the handler-class
  lineage (not process-identity) explicit; body and §A.3 agree.
  `handler_ref` resolves to a class (not instance) — §A.3 line 970
  carries this implicitly; could tighten with one line in WM-022.
- **"terminal" overloaded for workspace-terminal vs. run-terminal**:
  WM-014 / WM-037a use it for lifecycle (`merged`/`discarded`); WM-031
  uses it for run state ("terminal failure state"). Two state spaces
  share the word; context disambiguates. Minor; glossary could
  distinguish.

## Conformance against template v1.1

Front matter, §4.a envelope (WM-ENV-001), §5 invariants with sensors,
§7 state machine + pseudocode alignment, §8 error taxonomy, §10
conformance profile: **all OK** — the integration hit the v1.1
template hygiene marks cleanly.

- **§6 numbering gap.** §6.1 / §6.2 / §6.4 / §6.5 — no §6.3 (tabular
  schemas section is optional in template §6.3). Either add a brief
  "no tabular schemas" note or accept the gap.
- **§5 INV-003 sensor shape.** Challenge D.
- **§11 OQ count.** 11; two (OQ-WM-005, OQ-WM-006) should probably
  block advancement. See "Cross-spec promises."
- **Axes-line spot-checks.** WM-013b `idempotency=idempotent` defensible
  (double-release is no-op). WM-023 `idempotency=idempotent` is
  suspect — `merge_conflict_escalation` is terminal-distinct, a
  second emission is a second escalation not a no-op. Reviewer
  should confirm.
- **Tag grammar / ID monotonicity / retired-ID discipline**: OK.

## New failure modes surfaced by v0.3.0 text

1. **Abandoned session-log directories on crash mid-sidecar-write.**
   WM-025 creates `${workspace_path}/.harmonik/sessions/${session_id}/`
   and WM-026 writes the sidecar into it. A daemon crash between
   mkdir and sidecar-write leaves an empty session directory. WM
   declares no cleanup for this. The startup orphan sweep (WM-033)
   removes lock files only; session directories persist. The
   filesystem state is harmless (empty dir) but WM-013c's discovery
   step (d) — "stat-ing `${path}/.harmonik/sessions/` to detect
   whether any session was ever started" — is satisfied even for
   an empty session dir, which could falsely suggest a session
   existed. Name whether an empty session_id directory is an
   "orphan" or "never started."

2. **Subsequent-session launch after operator pause.** §7.1 permits
   in-flight states to compose with `interrupt_state != none`.
   WM-011 says "AT MOST ONE agent process MAY be actively writing."
   A workspace in `leased` + `interrupt_state = operator-paused`
   that receives a new session launch request: does it (a) wait
   for unpause, (b) reject the launch, (c) launch the session
   anyway (since lifecycle is `leased`)? Nothing in the spec
   names the precedence between lifecycle and interrupt for new
   session dispatch. Implementer has to invent.

3. **Re-entry into `merge-pending` from `conflict-resolving`.**
   §7.1 row (line 680): `conflict-resolving | implementer resolves
   | resolution commit exists on task branch and merge re-attempt
   succeeds | merge-pending`. That is a SECOND entry into
   `merge-pending`. WM-015 says `workspace_merge_status
   (status=pending)` emits on entry to `merge-pending`. Does it
   re-emit on the second entry? The paired-phase single-event
   rule (EV §8.9(h)) forbids "successive emissions with identical
   `status` for the same scoped entity." But the two emissions
   are SEPARATED by `conflict-resolving`; they're not successive
   at the event-stream level. EV-compliant? Unclear. Worse: if
   the second `workspace_merge_status(status=pending)` DOES NOT
   emit, consumers observing the state machine miss the re-entry.
   If it DOES emit, EV §8.9(h) might flag it. Name it.

4. **Lease lock race between WM's release and HC's fail-fast.**
   WM-013b releases the lease-lock AFTER the terminal event emits
   and flushes (per EV-015). HC-044a's fail-fast path reads its
   own path (`${workspace_path}/.lock`) at Launch. But between
   WM's event emission and its lock release, a new Launch
   against the same workspace (from a new run on a fresh path
   per WM-013d — actually blocked by WM-013d — wait, that's the
   same workspace_id, which shouldn't re-launch anyway) observes
   the lock file. A new run against the same workspace_id is
   forbidden by WM-012 (one run per bead). New run against a new
   bead on the same path is forbidden by WM-013d. So this race
   doesn't surface at correctness level, but the ordering is
   fragile — a future relaxation of any of those rules exposes
   the race.

5. **Merge-back idempotency on retry.** WM-018a's merge-back node
   is "one of two shapes." The non-agentic shape (a) runs
   `git merge --squash` + commit. If the commit succeeds but the
   subsequent state transition fails (some bug in the daemon), a
   retry runs `git merge --squash` again — which against the same
   branch tip is a no-op, but the subsequent `commit` step fires
   on a clean tree and fails. The retry should be idempotent; it
   isn't. Name the retry rule for WM-018a (a): "the orchestrator
   MUST detect the merge commit already landed via trailer check
   before re-executing."

6. **Investigator runs landing on task branches.** Reconciliation
   spec §8.11 (Cat 6a) says the investigator runs as a normal
   harmonik workflow. The workflow, per this spec's model, gets a
   workspace. Does that workspace have its own task branch? If so,
   WM-INV-002 ("one in-flight run per bead") is violated — the
   investigator and the target run are both in-flight against
   the same bead. If the investigator uses a different bead or
   no bead, WM-012's "one run per bead" is satisfied but the
   `implementer_handler_ref` walk (WM-022) may pick up investigator
   commits as "agentic" when it walks the target's task branch
   (the investigator typically doesn't touch the target's branch,
   but the spec is silent). Bound: name that investigator runs
   are on separate branches and do not appear on target-run task
   branches.

7. **Workspace manager observing `workspace_interrupted` emitted
   by reconciliation.** WM-ENV-001 (c) lists `workspace_interrupted`
   as CONSUMED by WM (line 96: "WM observes but does not emit
   this event; the emitter is the reconciliation detector"). But
   WM-038 says WM is the SOLE writer of `interrupt_state`, and
   reconciliation's `workspace_interrupted` emission is the
   RESULT of WM's `interrupt_state` transition (per the OQ-WM-006
   default). Reconciliation emits the event AFTER WM wrote the
   field. So WM CONSUMES an event that RECONCILIATION emitted in
   RESPONSE to WM'S WRITE. Is this a loop? In principle no (WM
   doesn't re-write on observing the event it caused), but the
   spec should name the de-loop: "WM MUST NOT mutate
   `interrupt_state` in response to `workspace_interrupted`."

8. **Rev-safe bead-ID transformation (WM-006a) is partially
   specified.** Line 185-189: lists illegal chars and the
   transformation rule "MUST be replaced with `-` or rejected at
   workspace-create time with a fail-fast error." Either-or, but
   which? The spec says "the transformation rule itself is part
   of OQ-WM-002." But a bead ID `task:42#urgent` has three
   illegal chars; the either-or semantics for each character are
   not declared. An implementer picks one global policy (always
   replace, always reject) without guidance. Name the per-character
   policy.

## Affirmations (decisions that survive R1 and R2)

1. **Retiring `setup` lifecycle state (§7.1 rewrite).** The v0.2
   `setup` state had no observable work; v0.3 removes it cleanly,
   preserves the state machine's locked ID non-reuse, and aligns
   §7.1 / §7.2 / WM-016. Exemplary integration move.
2. **Paired-phase `workspace_merge_status` adoption (WM-015,
   WM-021).** Honest ceding to EV §8.9(h); the two-event v0.2
   shape is retired with a pointer, not ignored.
3. **`implementer_handler_ref` field addition (§6.1).** Even with
   the trailer-walk error (Challenge A), the field design is the
   right call: mechanical, typed, discoverable from the Workspace
   record, gracefully null for all-mechanical paths (WM-022a).
4. **WM-018a merge-node two-shapes declaration.** The non-agentic
   vs. agentic split is a good authoring move that avoids inventing
   a new node-type enum (contra R1 implementer SP-6's proposal)
   while still naming the dispatch contract.
5. **§A.4 reverse-drift migration map publication.** The 48
   inbound cites need coordinated fixing; publishing the map in
   WM (the target, not the emitter) rather than leaving it
   implicit is the right discipline.
6. **§4.a envelope adoption.** Clean, comprehensive, per AR-053.
7. **§2.2 scope discipline.** Nine out-of-scope items each name
   their owning spec. Better than most R1→R2 revisions achieve.
8. **Verdict enum classification table (WM-036).** Honest adoption
   of the six-value RC enum; `accept-close-with-note` correctly
   present; `abandon` correctly retired; malformed-verdict
   handling cites RC-023. Clean cross-spec coupling.
9. **Interrupt-orthogonality narrowed to in-flight states
   (WM-037a + §7.1 row narrowing).** The R1 "any" row produced
   nonsensical terminal-state interrupts; v0.3 bounds the
   orthogonality honestly.

## Recommendation

**Proceed with one more focused pass before `reviewed`.** The three
load-bearing challenges (A: fabricated trailer, B: lock-path
unresolvability, C: interrupt emission gap) are addressable by text
edits; none requires reopening architectural decisions. The selection-
test regression (Challenge D) and the hidden-assumption cleanup items
are ride-along work.

Challenge A is the critical fix: WM-022 must walk the sibling file
rather than a non-existent trailer, or EM-017 must be amended to add
`Harmonik-Actor-Role`. The former is the minimum-surface change.

Challenges B and C should either be upgraded from OQ to blocking-for-
reviewed-status, or the spec should explicitly declare an MVH workaround
(e.g., "WM writes both filenames with a declared shim schema that
satisfies both HC and PL detectors" for B; "reconciliation detectors
read `interrupt_state` directly via state inspection, bypassing the
bus" for C).

Five of the eleven OQs are realistic defers. Two (005, 006) should
probably not be. Four are ceremony.

Estimated R3 delta: ~2–3 requirement edits (WM-022 trailer fix,
possibly WM-013a lock-file shape revision, possibly WM-INV-003
sensor), 1–2 OQ upgrades to blocking, 1–2 §A.3 rationale additions.
No structural rework needed.
