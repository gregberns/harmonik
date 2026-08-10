# Runbook — turn graph findings into executable architecture work

## Purpose

Use this runbook to repeat the process that produced `BEAD-BACKLOG.md`.
The process starts with detector output and ends with reviewed, ordered beads.

The detector ranks places to inspect.
It does not decide what to change.

## Inputs

Collect these inputs before the review:

- the exact Harmonik revision that the detector scanned
- the current integration revision
- the detector catalog and raw finding files
- the delete-and-rewrite charter
- `PRINCIPLES.md`
- the active bead ledger
- the current package dependency graph

Record both revisions.
Never present an old detector result as a current-tree fact.

## Output

Produce these artifacts:

1. A durable review note with accepted and rejected finding classes.
2. Beads with a problem, scope, acceptance checks, and limits.
3. Dependency edges that make the safe execution order explicit.
4. A list of old beads that overlap the new findings.
5. A short record of detector defects found during triage.

## Procedure

### 1. Read the project target

Read `PRINCIPLES.md` and the delete-and-rewrite charter.
Write the core path in one line.

For this review, the path was:

> config → event bus → queue → bead ledger → worktree → harness → run loop → merge

Use that line to rank findings.
A finding on this path outranks unrelated cleanup.

### 2. Pin the detector snapshot

Read the revision from every generated finding file.
Name the integration ref that will receive the work.
Resolve that ref instead of the lane worktree `HEAD`.

```sh
review_integration_ref=work/alpha-integration-merge
git rev-parse --verify "${review_integration_ref}^{commit}"
jq -r '.revision' <finding-file>
```

Record the integration ref and its resolved commit.
Stop if the integration ref does not resolve.

If they differ, treat all generated facts as candidates.
Re-run the detector when possible.
Otherwise, re-check each selected site by hand.

### 3. Understand the detector class

Read the detector rule before reading individual findings.
Write what the detector can and cannot prove.

Use this table as the starting point:

| Class | What it can show | What it cannot show |
| --- | --- | --- |
| A1 | A literal equals a declared constant | Both names mean the same concept |
| A2 | A literal repeats | The repeats need one owner or constant |
| A4 | Control flow reads error text | A sentinel is possible at that boundary |
| B1 | A function mixes likely pure statements and effects | The suggested split is a good contract |
| B2 | A complex function calls effect-like APIs | Every call belongs behind a port |
| B4 | A structure has many fields | Consumers receive fields they do not use |

Do not turn a class description into a work order.

### 4. Check detector precision with samples

Sample at least one high-density result and one core-path result per class.
Open the code and identify the concept owner.

Reject a finding when any of these conditions apply:

- equal text represents two different protocols
- a test twin repeats a wire value to stay independent
- the literal is a struct tag or CLI syntax
- a wide structure is a wire record or immutable state value
- the effect is already at the correct outer boundary
- the proposed constant would add an outward dependency

Record detector defects separately from Harmonik defects.

### 5. Re-derive high-value findings on the current tree

Search declarations, producers, consumers, tests, and open beads.

```sh
rg -n '<value-or-symbol>' internal cmd
br search --format json '<plain description>'
git log --all --oneline -- '<relevant path>'
```

Do not trust a count without checking how it was produced.
Do not trust an old bead without checking the current code.

The event audit showed why this step matters.
The report called seven event types unregistered.
Current code showed that five register in leaf packages but lack compatibility metadata.

### 6. Select work by architectural leverage

Prefer findings that meet most of these conditions:

- The compiler will enforce a relationship after the change.
- The concept owner is clear.
- The change removes repeated conversions or text parsing.
- The change creates a pure decision with value inputs.
- The change sits on the core bead path.
- The behavior change is absent or tightly bounded.
- A focused test can prove the claim.
- The task makes a later package cut safer.

Defer findings that only improve a metric or reduce a count.

### 7. Find the root boundary

Do not fix every downstream symptom first.
Find the field, API, or adapter that permits the bad relationship.

Examples from this review:

- Type `Event.Type` instead of replacing hundreds of strings one at a time.
- Classify external `br` output in the adapter instead of parsing it in the scheduler.
- Pass acceptance time into queue policy instead of faking the clock in tests.
- Extract queue construction instead of adding ports around every helper.

The root boundary should make later mistakes fail at compile time or in one small test.

### 8. Split the work into safe beads

Give each bead one architectural claim.
Keep serial repository-wide changes separate from local changes.

Each bead description must contain four sections:

1. **Problem** — the current architectural defect.
2. **Scope** — the exact code and behavior to change.
3. **Acceptance** — commands and behavior that prove completion.
4. **Limits** — tempting work that does not belong in the bead.

Use plain titles that describe the change.
Do not use a detector code as the title.

### 9. Search for existing beads

Search by the problem text, symbol, and behavior.

```sh
br search --format json 'event registry'
br search --format json 'blocked claim error'
br search --format json 'queue acceptance time'
```

Reuse an old bead when its evidence and scope still match.
Add a comment when the current check changes its interpretation.
Create a new bead when the old bead describes a different defect.

### 10. Build the dependency graph

Add a dependency only when one change creates the safe input for another.
Do not use dependencies only to express preference.

Examples:

- Type the registry API before typing every event envelope.
- Type adapter claim errors before removing scheduler text parsing.
- Inject queue time before extracting the pure submit builder in the same files.

Check the result:

```sh
br dep list <bead-id>
br dep cycles
```

### 11. Define proof before implementation

Choose the proof that matches the change.

| Change | Strong proof |
| --- | --- |
| Literal to constant | Compiled output or wire output stays identical |
| String to named string type | JSON bytes stay identical and plain variables stop compiling |
| Error classification | Message wording changes while `errors.Is` stays true |
| Clock injection | A fixed input produces exact timestamps |
| Pure extraction | Table tests use values only and mutation breaks the correct case |
| Registry coverage | A new declaration without registration makes the check fail |

Run the focused baseline before editing.
A red baseline cannot prove a regression.

### 12. Plan file ownership and review

Group tasks by file ownership.
Do not let two agents edit one file at the same time.

Use one distinct worktree per concurrent lane.
Give each worktree its own branch.
Keep implementation commits small enough for one reviewer to understand.

Before commit:

1. Run focused tests with `-count=1`.
2. Confirm the reviewed files match the index.
3. Get an independent review.
4. Record the exact verdict in the commit trailer.
5. Merge only after the branch still applies cleanly to integration.

## Detector-specific rules from this review

### A1 literals

Same-package findings are the safest set.
Cross-package findings need an ownership check.
Ambiguous findings are scout work, not edit work.

Run the root type change before a large literal cleanup.
The compiler can replace much of the detector work.

### A2 repeated literals

Do not schedule generated A2 work until each finding records all touched files.
Exclude struct tags before counting.
Classify syntax roles before proposing a constant.

### A4 error text

Trace the message to its producer.
If Harmonik owns the producer, return a typed error.
If an external tool owns it, parse once at the adapter boundary.
Keep the raw error available through `Unwrap`.

### B1 and B2 effect findings

Use one pilot before a wave.
Name the pure input and output contract first.
Leave effects in a thin shell.
Reject a split that only adds interface ceremony.

### B4 wide structures

Require consumer-use evidence.
Do not split payloads, configuration, or state records only because they are wide.

## Completion checklist

- [ ] The detector revision and current revision are recorded.
- [ ] Each selected finding was checked on the current tree.
- [ ] Each concept has a named owner.
- [ ] Existing beads were searched before new beads were created.
- [ ] Each bead has problem, scope, acceptance, and limits.
- [ ] Serial work is marked and ordered.
- [ ] File-overlap conflicts are removed.
- [ ] Tests prove behavior, not only compilation.
- [ ] The dependency graph has no active cycle.
- [ ] Rejected detector classes are documented.
- [ ] The durable backlog exists outside the local bead ledger.

## Reference result

The first use of this process produced `BEAD-BACKLOG.md`.
It created eleven new beads and reused two existing beads.
It rejected bulk A2, ambiguous A1, field-count-only B4, and bulk effect extraction.
