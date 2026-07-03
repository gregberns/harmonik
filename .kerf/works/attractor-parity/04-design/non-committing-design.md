# Change Design — C. Non-committing agentic mode (hk-69asi)

> Pass 4 (`change-design`) of `attractor-parity`. Normative design for the non-committing agentic mode. Grounded in `03-research/non-committing/findings.md`. Resolves OQ-3 (`non_committing` vs `auto_status`).

## 1. Design decision (summary)

Add a `non_committing` (boolean) optional attr to **agentic** nodes. An implementer-class agentic node so marked returns SUCCESS on clean agent exit **without requiring HEAD to advance** — relaxing the over-strict hard-fail at `dot_cascade.go:589`. This removes an implementation invariant that was inherited (by analogy) from review-loop mode's EM-015d; it does NOT change the Outcome contract (SUCCESS-without-commit is already legal per EM-005).

## 2. OQ-3 resolution: `non_committing`, NOT `auto_status`

**Pick `non_committing`.** Rationale (F-C3): kilroy's `auto_status` conflates two axes — (1) commit-or-not, (2) status-derivation source (HEAD advance vs work-product vs embedded `{"status":...}` JSON marker). Component C needs only axis (1). `auto_status`'s work-product status-derivation is a separate, larger capability (the E2 note) that harmonik does NOT have at v1 and must not smuggle in under the no-commit relaxation. `non_committing` says exactly one thing: "this agentic node returns SUCCESS without requiring HEAD to advance."

**Porting alias (flag for integration):** harmonik-native ports of the kilroy pipelines map `auto_status=true` → `non_committing=true`. Harmonik does NOT accept `auto_status` as a node attr at v1 (it would mislead authors into expecting status-derivation that does not exist). Reserve `auto_status` for the future status-derivation feature.

## 3. Attr name + WG-002 row

Add to `specs/workflow-graph.md` §4 WG-002, the `agentic` row's **optional attrs** column:

| attr | type | meaning |
|---|---|---|
| `non_committing` | boolean | when `true`, the agentic (implementer-class) node returns SUCCESS on clean exit without requiring HEAD to advance. Default `false` (HEAD-advance required, today's behavior). |

Amended `agentic` row optional-attr column (also gains `prompt` from component B): `prompt`, `non_committing`, `idempotency_class`, `axis_tags`, `skills_ref`, `freedom_profile_ref`, `budget_ref`, `hook_ref`, `guard_ref`.

## 4. WG-031 reserved-set addition

Add `non_committing` to the WG-031 reserved set (`workflow-graph.md:388`). Parser gains `case "non_committing":` in `buildNode` (`parser.go:599-668`), parsing `"true"`/`"false"` into a typed `Node.NonCommitting bool` (add to `ast.go`); reserved-out-of-position strict error elsewhere. `non_committing` on a non-agentic/gate node: validation warning at v1 (inert — those types don't reach the HEAD-advance check). Lean warn, not error.

## 5. SUCCESS-derivation design (the relaxation)

In `dispatchDotAgenticNode`, the implementer-class branch (`dot_cascade.go:582-592`):

    postHeadSHA, headErr := resolveWorktreeHEAD(ctx, wtPath)
    if headErr != nil { return core.Outcome{}, fmt.Errorf(...) }
    if !node.NonCommitting && postHeadSHA == preHeadSHA {        // GATED on the attr
        return core.Outcome{}, fmt.Errorf("node %q (implementer) exited without advancing HEAD past %s", node.ID, preHeadSHA)
    }
    return core.Outcome{Status: core.OutcomeStatusSuccess}, nil

When `node.NonCommitting` is true, the HEAD-advance guard is skipped; a clean agent exit (non-`agent_failed` terminal, already detected by `waitWithSocketGrace` at `dot_cascade.go:546-558`) yields `Outcome{Status: SUCCESS}`. The `headErr` (cannot resolve HEAD at all) guard is retained for both modes (a broken worktree is still a real error).

**v1 floor (F-C4):** clean exit ⇒ SUCCESS. We do NOT at v1 parse a work product or an embedded `{"status":...}` marker to derive FAIL — that is the deferred `auto_status` feature. A `non_committing` node that produces a bad analysis still returns SUCCESS; the DOWNSTREAM tool node (component A — e.g. `assess_confidence` grepping `.ai/confidence.txt` and `exit 1`) catches it and routes the failure. This is exactly the kilroy pattern (box writes a file; the next parallelogram validates + exit-codes the routing). **Authoring rule to document: pair every `non_committing` node with a validating tool node.**

## 6. handler-contract anchor

`handler-contract.md` §4.2a (the HC-058 agentic-node Outcome-obligation area, `handler-contract.md:222-247`) gains a clarifying note: a `dot`-mode agentic node MAY return `status = SUCCESS` without a commit when the node is `non_committing`; SUCCESS-without-commit is already legal per EM-005 and is not a new Outcome shape. No new HC requirement is needed — this is a clarifying note that the existing agentic-node Outcome obligations already permit it.

## 7. Backwards compatibility

- **Additive.** New optional boolean `non_committing`, default `false`; no existing attr changes.
- **Minor schema bump; N-1 readable (WG-034).** No new type/enum/edge-field.
- **Outcome envelope untouched** (EM-005): SUCCESS-without-commit is already legal; C removes an over-strict check, it does not relax a contract.
- **review-loop (v69) unaffected — CRITICAL (R-C4):** the relaxation is gated on the per-node `non_committing` attr AND lives only in the `dot`-mode `dispatchDotAgenticNode` path. review-loop mode's hardcoded implementer-MUST-commit invariant (EM-015d, `runReviewLoop`) is untouched. EM-015d's "implementer MUST produce a commit" wording is review-loop-scoped; the spec change relaxes it ONLY for `dot`-mode `non_committing` nodes. The proven v69 review-loop does not regress.

## 8. Open items flagged for the integration pass

- I-C1 (R-C1): document the `auto_status` → `non_committing` porting alias in the canonical example + integration notes; confirm harmonik does NOT accept `auto_status` at v1.
- I-C2 (R-C2): authoring rule "pair `non_committing` with a validating tool node" — document in the shell/agentic authoring notes and the canonical example sidecar.
- I-C3 (R-C3): co-design or strictly sequence with component B (both touch `dispatchDotAgenticNode`; C edits the post-launch outcome-derivation region, B edits the pre-launch launch-spec build — non-overlapping but one function).
- I-C4 (R-C4): ensure the EM-015d prose edit is scoped to `dot`-mode `non_committing` and leaves review-loop intact; the integration pass must verify no review-loop test regresses.
- I-C5 (deferred): the `auto_status` proper status-derivation feature (work-product / embedded JSON marker → FAIL) is a separate future capability — file a follow-up bead; out of scope for parity v1.
