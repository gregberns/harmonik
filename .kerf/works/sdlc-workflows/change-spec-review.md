# Change Spec Review (autonomous, round 1)

Reviewer lens on `05-specs/corpus-landing-spec.md` against `03-components.md` + `04-research/`.

## Checklist
- **Every requirement has a spec section.** PASS — C1→S1+S3; C2→S1+S2; C4→S4+S5; C5→S6; epic/scope→S7.
- **No unbacked spec content.** PASS — S1–S7 each trace to a component requirement or the README discipline.
- **Acceptance concrete + testable.** PASS — S1 acceptance names `go test ./internal/workflow/...`,
  the Layer-1 round-trip, and the README-anchor obligation; S2 enumerates the exact paths per class.
- **Real paths.** PASS — `specs/examples/<name>.dot`, `specs/examples/README.md`,
  `internal/workflow/scenario_*_test.go`, `reviewverdict.go:528`, `dot_cascade.go` all verified.
- **Runnable verification.** PASS — test pattern matches `scenario_reviewloop_full_hkisp3y_test.go`
  (`LoadDotWorkflow` + `DecideNextNode` + `NewCycleCounter`), confirmed present in the codebase.
- **Edge cases addressed.** PASS — cap-hit (Failed decision, not fallthrough), terminal-by-identity,
  failure-class synthetic-vs-live split (hk-1xsyu), reviewer-commit smoke fallback (S3).
- **Consistent with research.** PASS — S3 encodes Q1's chosen option (A) + smoke mitigation; S2
  encodes Q2's test pattern; S1.3 encodes Q3's reference-wiring-only conclusion; S4/S5 encode Q4.

## Findings
- NIT: S3 fallback (implementer-class reviewers) is described but not given its own bead — correct;
  it is a contingency, filed only if the smoke fails.
- NIT: tool-node validator-acceptance (Q4) is left "to be confirmed at implementation" for #15–17 —
  acceptable; those beads are capability-gated and the confirmation happens at hk-l8rpd time.

## Verdict
APPROVE. No unresolved findings. Advance to integration.
