# Design verdict — hk-jjbwj (Cat-BL1 git-error handling)

> Codename bead-ledger-worktree-merge · Epic hk-8fa9a · Author: kynes (design crew), 2026-06-26.
> Follow-up design decision on the SHIPPED BL-MRG work (kerf work finalized v0.7.0, 2026-06-21).
> This is NOT a new kerf pass — it resolves one P3 correctness decision left open by the build.

## Context — the lane is otherwise done

The `.beads/issues.jsonl` worktree-merge design is complete and shipped: union-by-ID
`beads-union` merge driver (`cmd/harmonik/beadsmerge.go`, daemon auto-config), post-merge
`br sync --import-only` + `bead_sync_failed`, and reconciliation Cat-BL1/BL2/BL3 detectors wired into
daemon boot. Normative in `specs/beads-integration.md §4.8b (BL-MRG)`, `specs/reconciliation/spec.md
§8.BL`, `specs/event-model.md §8.15`. Both problem halves (child-bead-spawn safety;
integration-branch coordination, with a documented Phase-1 visibility-gap limitation) are solved.
The only open thread is this P3.

## The defect (confirmed in code)

`internal/daemon/reconciliation.go:230–241`:

```go
out, err := cmd.Output()  // git -C <dir> log -1 --grep "Refs: <parent>" --format=%H <targetBranch>
if err != nil {
    // git log exits non-zero when the branch doesn't exist or no match.
    // Treat as "no merge commit" — conservative: won't falsely close a live bead.
    return false, nil //nolint:nilerr // intentional: scan failure is non-fatal
}
return strings.TrimSpace(string(out)) != "", nil
```

Two things are wrong:

1. **The comment's git-semantics claim is false.** `git log --grep` with a resolvable revision and
   **no matching commit exits 0 with empty output** — already handled correctly by line 240
   (`TrimSpace(out) != ""` → `false`). `err != nil` is reached **only** on genuine failure: the
   target branch / revision can't be resolved (fresh project before the integration/main branch
   exists), or git itself fails to exec. So the `err != nil` branch is **not** "no match" — it is
   "could not check."
2. **The implementation contradicts its own godoc contract.** Lines 222–223 promise `(false, err)`
   on git execution failure; the body swallows the error to `(false, nil)`.

**Consequence:** `RunCatBL1StartupSweep` reads `(false, nil)` as "orphan positively confirmed" and
**auto-closes the OPEN bead** (escalates if in_progress). On a fresh project before the target branch
exists, this closes **every** `parent:hk-*` open bead. That is the exact opposite of the comment's
"conservative: won't falsely close a live bead."

## Verdict — skip-on-git-error (NOT document-as-intended)

Reconciliation's load-bearing invariant is **never destroy live state on uncertainty** — it is
precisely why Cat-BL1's auto-close is only safe on *positively-confirmed* orphan-ness. "I could not
run the check" is not confirmation; auto-closing on it is a correctness violation, and on a
fresh/misconfigured project it is silently destructive. Documenting the current behavior as intended
is rejected: it would bless a footgun that contradicts both the inline comment and the function's
godoc.

### Fix spec (build-lane work — design crew does not dispatch implementation)

> **The code change is a ONE-LINER** — the caller is already wired for the contract (reviewer
> Finding 3). `RunCatBL1StartupSweep` at `reconciliation.go:125–129` ALREADY does
> `if gitErr != nil { Fprintf(... "(skipping)"); continue }`. So the skip-on-error path the verdict
> wants is **pre-existing**; only `hasParentMergeCommit` is wrong. Items 2 is already satisfied; the
> real work is item 1 + the test rewire (item 3) + the diagnostic promotion (item 5).

1. **`hasParentMergeCommit` MUST honor its godoc:** return `(false, err)` on `cmd.Output()` error —
   **delete the `//nolint:nilerr` swallow at lines 235–239.** A clean exit-0 no-match already
   correctly returns `(false, nil)` via line 240 (verified: branch-resolves-no-match → exit 0 empty;
   bad/absent branch / non-repo → exit 128). This is the only behavioral change.
2. **Caller — ALREADY DONE.** `RunCatBL1StartupSweep:125–129` already treats `gitErr != nil` as
   SKIP+continue. No change needed beyond confirming the skip guard precedes the orphan-action path
   (it does, 125 → 162/146). Only a clean `(false, nil)` → positively-confirmed orphan → Cat-BL1 action.
3. **Tests — MUST rewire the existing unit test (reviewer Finding 4, load-bearing).**
   `internal/daemon/reconciliation_bl1_unit_hk1zixl_test.go` deliberately uses a bare `t.TempDir()`
   (NOT a git repo) and **depends on the inverted behavior** ("git log exits non-zero there →
   every candidate is an orphan → drives the close + escalate paths"). After the fix that TempDir
   errors → SKIP → no close, no escalate → the existing close/escalate assertions go **RED**. The
   build bead MUST migrate that test's seam to a **real git repo with the target branch present but
   no `Refs:` commit** (to reach the clean exit-0 orphan path), and ADD: (a) git-error/branch-absent
   → bead SKIPPED not closed (fresh-project case); (b) clean exit-0 empty → still orphan (no
   regression); (c) match → not orphan. A build agent that lands item 1 without rewiring this test
   will see it go red and may misread it as a regression — call this out in the bead.
4. **Spec touch:** add a one-line clause under `specs/reconciliation/spec.md §8.BL1` — "an
   indeterminate parent-merge check (git error / target branch absent) is SKIPPED, never treated as
   orphan confirmation" — so the conservative invariant is contractual, not just code.
5. **Diagnostic — promote from optional to REQUIRED (reviewer Finding 5).** The current skip emits a
   **stderr line only** (`reconciliation.go:126`). A persistently-misconfigured project (wrong
   `TargetBranch`, non-repo) would then skip every orphan **silently forever**. Emit **one**
   structured visibility signal when the target branch is absent / the check persistently errors
   (a debug event or a single `operator_escalation_required`), so "can't check" is observable, not
   invisible. Not stderr-only.

### Proportionality note (gate scaling)

This is a single-function correctness fix, not a new subsystem or contract — so it gets the **normal
per-commit agent-review at build time**, not the full reviewers-with-critics design gate that the
from-scratch Pi harness warranted. One independent design reviewer (APPROVE) verified the
git-exit-code reasoning **by direct execution** (no-match → exit 0; bad branch → exit 128), the
defect trace, the single-caller safety, and surfaced the test-rewire + diagnostic refinements folded
in above. Decomposition: a single build bead (BL-MRG follow-up), unassigned, for a later build lane —
NOT dispatched by kynes.
</content>
