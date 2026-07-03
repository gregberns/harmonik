# Research — C. Non-committing agentic mode (hk-69asi)

> Pass 3 (`research`) of `attractor-parity`. Component C per `02-components.md`. Relaxes the HEAD-advance hard-fail (`dot_cascade.go:589`) for analysis/review nodes via a node attr. Resolves OQ-3: `non_committing` vs reusing kilroy's `auto_status`.

## Research questions

- RQ-C1. What is the exact invariant being relaxed, and is it agentic-only?
- RQ-C2. Does the Outcome contract already permit SUCCESS-without-commit (i.e. is this removing an over-strict check, not adding a contract)?
- RQ-C3. OQ-3: `non_committing` or `auto_status`? What does kilroy's `auto_status`/`fidelity` actually mean, and do the semantics match?
- RQ-C4. How is SUCCESS *derived* for a non-committing node if not from HEAD advance?
- RQ-C5. What attrs do the live kilroy analysis/review nodes carry, and what would break without the relaxation?

## Findings

### F-C1 — The invariant is a hard-fail at one site, agentic-implementer-only (RQ-C1)

`internal/daemon/dot_cascade.go:582-592` (in `dispatchDotAgenticNode`, the implementer-class branch):

    postHeadSHA, headErr := resolveWorktreeHEAD(ctx, wtPath)
    if headErr != nil { return core.Outcome{}, fmt.Errorf(...) }
    if postHeadSHA == preHeadSHA {
        return core.Outcome{}, fmt.Errorf("node %q (implementer) exited without advancing HEAD past %s", node.ID, preHeadSHA)
    }
    return core.Outcome{Status: core.OutcomeStatusSuccess}, nil

Crucially:
- The check is **only on the implementer-class branch.** Reviewer-class nodes (`isReviewer`, lines 565-580) derive their Outcome from `review.json` and have NO HEAD-advance requirement — they already legitimately do not commit. So the relaxation only needs to gate the implementer path.
- It is also **agentic-only.** Non-agentic / tool nodes (component A) never reach this code. So C is scoped to "implementer-class agentic nodes that should be allowed to not commit."
- The error returned propagates up as a `nodeErr` (`dot_cascade.go:215-221`), which terminates the whole run as `needsAttention` (a reopen). So today a no-commit investigate node *fails the entire pipeline.*

The spec anchor is EM-015d (cited in the code comment as "EM-015d: the implementer MUST produce a commit"). That requirement is **review-loop-mode-specific** — it is the hardcoded two-node implementer→reviewer cycle (`execution-model.md:327`). The `dot`-mode driver inherited the check by analogy when generalizing the review-loop driver (see the `dot_cascade.go` header comment), but a general DOT graph has node classes (investigate/dedup/review-analysis) where no-commit is correct. **So the relaxation is: make the HEAD-advance check a per-node opt-out in `dot` mode; it stays mandatory for review-loop mode (unchanged) and for `dot` implementer nodes that do not carry the attr.**

### F-C2 — SUCCESS-without-commit is already legal in the Outcome contract (RQ-C2)

EM-005 (`execution-model.md:113-119`): an Outcome carries `status ∈ {SUCCESS, FAIL, RETRY, PARTIAL_SUCCESS}` — no field ties SUCCESS to a commit. EM §4.5 (`execution-model.md:513`) on durability: a FAIL/RETRY transition is non-durable, but a SUCCESS transition's durability is the daemon's checkpoint decision (EM-023a), and the durability checkpoint is the task-branch commit range — which can legitimately be empty for a node that produced no commit. There is **no normative requirement that every agentic node advance HEAD**; EM-015d's "implementer MUST commit" is scoped to the review-loop two-node cycle. The HC-058 agentic-node row (`handler-contract.md:230`) lists legal statuses with no commit obligation.

**Verdict: this is removing an over-strict implementation invariant, not adding/relaxing a normative contract.** The spec change is small — a clarifying note that a `dot`-mode agentic node MAY return SUCCESS without advancing HEAD when it carries the non-committing attr. The Outcome envelope is untouched (per the constraint in `01-problem-space.md` §4 / `02-components.md` §5).

### F-C3 — OQ-3 resolution: prefer `non_committing`; treat `auto_status` as a separate (deferred) concept (RQ-C3)

kilroy's two attrs (from both pipelines' `box` nodes):
- `auto_status=true` — "auto-derive the outcome status from the work product" (vs an explicit handler-emitted status). In kilroy this means: the agent writes a work product and the runner infers success/fail from it (e.g. a written `.ai/*.md`, or an embedded `{"status": "partial_success"}` JSON marker that several kilroy prompts emit, e.g. `fix_issues`: `write {"status": "partial_success"}`).
- `fidelity="full"` — an unrelated kilroy knob (context-fidelity hint); not relevant to committing.

These are NOT the same concept as "may I skip the commit." `auto_status` is about *how status is derived*; the HEAD-advance relaxation is about *what counts as success*. Reusing `auto_status` would conflate two axes:
1. **commit-or-not** (the thing C actually needs to relax),
2. **status-derivation source** (HEAD advance vs work-product vs embedded JSON marker — a richer, separate feature).

**Decision: introduce `non_committing` (boolean) as the C-scoped attr.** It says exactly one thing: "this agentic node returns SUCCESS without requiring HEAD to advance." Rationale: (a) it is the minimal relaxation matching the actual invariant being lifted; (b) it does not over-claim a status-derivation mechanism harmonik does not yet have; (c) `auto_status`'s work-product-status-derivation (parsing `{"status": ...}` out of agent output / files) is a genuinely separate, larger capability (it folds into the E2 note in the parity research and overlaps the Outcome-emission path) that should NOT be smuggled in under the no-commit relaxation. **Map kilroy's `auto_status=true` → harmonik `non_committing=true` at the porting layer for v1** (the live kilroy analysis nodes use `auto_status=true` purely to mean "I don't commit, derive my status"), and reserve a future `auto_status` proper for the status-derivation feature. Flag this porting-time alias for the integration pass and the canonical-example sidecar.

### F-C4 — How SUCCESS is derived without HEAD advance (RQ-C4)

For a `non_committing` node, the implementer branch's "HEAD must advance → else fail" becomes "agent exited cleanly → SUCCESS" (no HEAD check). The clean exit is already detected by the existing `waitWithSocketGrace` / watcher path (`dot_cascade.go:546-558`); a non-`agent_failed` terminal is a clean exit. So the derivation is: **clean agent exit ⇒ `Outcome{Status: SUCCESS}`**, identical to the committing path's return at line 592 but without the line 589 guard. This is the *floor* (v1): we do NOT at v1 parse a work-product or an embedded `{"status": ...}` marker to derive FAIL — that is the deferred `auto_status` feature. Consequence: a `non_committing` investigate node that the agent *fails* internally (writes nothing useful) still returns SUCCESS unless the agent crashes; the downstream tool node (component A — e.g. `assess_confidence` grepping `.ai/confidence.txt`) is what catches a bad analysis and routes to `exit_skip`. This is exactly how the kilroy pipelines work: the `box` produces a file, the next `parallelogram` validates it and exit-codes the routing. **So C's floor + A's exit-code routing together cover the real workload; C alone does not need work-product status derivation.** Document this division explicitly.

### F-C5 — Live kilroy nodes needing the relaxation + blast radius (RQ-C5)

Nodes carrying `auto_status=true` (would map to `non_committing=true`):
- sentry-triage: `investigate` (writes `.ai/investigation.md`), `dedup_check` (writes `.ai/dedup.md`), `create_issue` (writes `.ai/created_issue.txt`, runs `gh issue create` — no repo commit).
- sentry-bugfix: `gather_reproduce`, `fix_reproduce`, `investigate_fix`, `fix_issues`, `code_review`, `scan_and_pr`. Note `scan_and_pr` DOES `git commit` + `git push` internally — so it advances HEAD and would pass the check even *without* the attr; but it's still marked `auto_status` in kilroy because kilroy derives its status from the work product, not the commit. Under harmonik's v1 floor, marking it `non_committing` is harmless (the check is skipped, clean exit ⇒ SUCCESS) and correct.

**Without the relaxation:** every one of these nodes that does not commit fails the run at `dot_cascade.go:589`. Since `investigate` is the 2nd node in sentry-triage and `gather_reproduce` is the 3rd in sentry-bugfix, **neither pipeline can get past its first analysis node** — confirming C is on the critical path for both target workloads (alongside A).

## Patterns to follow

- Reviewer-class no-commit path already exists: `dot_cascade.go:565-580` (derives Outcome from `review.json`, no HEAD check) — C makes the implementer path optionally behave like this w.r.t. the commit requirement.
- Permissive→strict attr promotion: `parser.go:644-666`.
- Per-node behavior gating in the implementer branch: the `isReviewer` switch at `dot_cascade.go:387-398` is the existing precedent for per-node-class behavior selection.

## Risks / conflicts

- **R-C1 (medium, flag for integration):** the `non_committing` vs `auto_status` naming divergence from kilroy means harmonik-native ports must rename the attr; document the porting alias in the canonical example + integration pass. Do NOT silently accept `auto_status` as a harmonik attr at v1 (it would mislead authors into expecting status-derivation that does not exist).
- **R-C2 (low):** v1 floor (clean-exit ⇒ SUCCESS, no work-product status parse) means a non-committing node cannot itself signal FAIL except by crashing; downstream tool nodes (A) carry the validation. This is the kilroy pattern and is acceptable — but it MUST be documented so authors pair every `non_committing` box with a validating tool node.
- **R-C3 (low):** merge coupling with component B on `dispatchDotAgenticNode` (different region; co-design or sequence per decompose).
- **R-C4 (low):** EM-015d's "implementer MUST commit" wording is review-loop-scoped; the spec change must be careful to relax it ONLY for `dot`-mode `non_committing` nodes and leave review-loop mode's invariant intact (the proven v69 review-loop must not regress — `01-problem-space.md` §4 constraint).
