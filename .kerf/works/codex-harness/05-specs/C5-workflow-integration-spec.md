# C5 — Workflow / review-loop integration — change spec

## Requirements (from 03-components.md)
R5.1 cascade selects launch-spec builder + adapter from resolved harness; end-to-end codex run does
implement→commit_gate→review→close on `standard-bead.dot` unchanged. R5.2 review-loop resolves the
reviewer's harness (default = same as implementer; optional independent override). R5.3 codex reviewer
writes the verdict; existing verdict-parsing/iteration reused (MUST-TEST in C6 R6.5).

## Research summary
The reviewer reuses the **same** launch path as the implementer (`reviewloop.go:227` vs `:814`),
differing only by `phase` (`04-research/current-harness/findings.md` §D.2). So if the seam is at the
launch-spec/adapter level (C1) and selection resolves once per run (C4), **the review-loop inherits
harness selection for free** — the cascade already chooses phase via `nodeIsReviewer(node)`
(`dot_cascade.go:248,461-466,802-805`). Completion-mode (C1 R1.1) makes the shared loop branch
deterministically for codex's ProcessExit vs claude's EventStreamThenQuit, so the review-loop
control flow needs no per-harness special-casing beyond consulting `Completion()`.

## Approach
- **Implementer + reviewer both route through the resolved harness.** In the cascade, after
  `ResolveHarness` (C4), obtain the harness once for the run; both the implementer node and the
  reviewer node fetch their `LaunchSpec`/`Seed`/`Retask`/`Teardown` from it. The DOT cascade's
  existing phase split (`nodeIsReviewer`) is unchanged; only the *builder it calls* becomes
  harness-dispatched.
- **Completion-mode branch.** The `/quit` + post-quit-kill + heartbeat-staleness machinery is
  entirely inside **`pasteInjectQuitOnCommit` (`pasteinject.go`)**, which is **launched from
  `dot_cascade.go:643`** (`go pasteInjectQuitOnCommit(...)`) — NOT from `workloop.go` (which holds
  only the bare `sess.Wait` at `:2265`). The branch goes at that launch site: consult
  `harness.Completion()` there — `EventStreamThenQuit` → launch `pasteInjectQuitOnCommit` as today
  (claude); `ProcessExit` → **do NOT launch it**; instead wait on `sess.Wait` (codex process exit),
  skipping the heartbeat-staleness kill (C2 R2.7 / decompose-review B1) and keeping only the absolute
  `commitHardCeiling` (90m). Apply the same gate at the analogous reviewer launch site.
- **Reviewer harness (R5.2):** default reviewer harness = implementer harness. Add an optional
  `reviewer_harness` selector (DOT review-node attr or `Config.ReviewerHarness`) so an operator can
  pin an always-claude reviewer — valuable while codex's structured-verdict reliability is unproven
  (C6 R6.5). If the override resolves to claude on a codex implementer run, the reviewer launches via
  `ClaudeHarness` with its own (minted) session id (reviewer always mints fresh, CHB-009).
- **Verdict (R5.3):** the codex reviewer is seeded with the same "write the verdict" instruction;
  the existing verdict-parsing path (`.harmonik/review.json` / reviewer_verdict event) is reused. If
  the MUST-TEST (C6 R6.5) shows codex doesn't reliably emit the structured verdict, the default
  reviewer_harness falls back to claude.

## Files & changes
- **MODIFY** `internal/daemon/dot_cascade.go:499-525` — fetch the resolved harness; implementer +
  reviewer builders come from it.
- **MODIFY** `internal/daemon/dot_cascade.go:643` — gate the `go pasteInjectQuitOnCommit(...)` launch
  (and the analogous reviewer launch site) behind `harness.Completion()`: launch it for
  `EventStreamThenQuit` (claude); for `ProcessExit` (codex) skip it and rely on `sess.Wait` + the
  absolute `commitHardCeiling`. **This is the load-bearing site — the bypass must land here, not in
  `workloop.go`.**
- **MODIFY** `internal/daemon/reviewloop.go` — reviewer launch goes through the resolved (or override)
  harness; reuse phase/verdict logic.
- **NOTE** `internal/daemon/workloop.go` holds only the bare `sess.Wait` (`:2265`); it needs no
  completion-mode edit beyond consuming whichever signal the gated path produces.
- **MODIFY** `internal/core/node.go` / `dotparser.go` — optional `reviewer_harness` attr (additive).

## Acceptance criteria
- AC5.1 An end-to-end codex run (twin substrate) traverses `standard-bead.dot`
  (implement→commit_gate→review→close) and closes the bead, asserted via `.harmonik/events/events.jsonl`
  (`run_started`, `reviewer_launched`, `reviewer_verdict`, `run_completed`).
- AC5.2 For a codex run, the workloop does NOT invoke `/quit` and does NOT fire the
  heartbeat-staleness kill; it completes on `sess.Wait` (process exit). Asserted by event absence +
  the `Completion()` branch unit test.
- AC5.3 With `reviewer_harness=claude` on a codex implementer run, the reviewer launches via
  `ClaudeHarness` (asserted) and the verdict parses normally.
- AC5.4 A claude run is byte-identical to today (regression — C6 R6.1).

## Verification
```
go test ./internal/daemon/... -run 'Cascade|ReviewLoop|Completion'
# scenario (twin): codex bead full lifecycle; assert the four events above and a Refs: commit on main
```

## Error handling / edge cases
- Reviewer-harness override resolves to an unavailable harness → fail closed (C3/C1 errors apply).
- codex reviewer emits no parseable verdict → existing `verdict absent` handling
  (`reference_harmonik_verdict_absent_salvage`) applies; default reviewer_harness=claude avoids it.
- Mixed-harness run (implement-codex / review-claude) increases test surface → covered by AC5.3;
  default keeps reviewer=implementer to limit it.

## Migration / back-compat
Additive attrs; default reviewer = implementer harness = claude when unset. claude lifecycle
unchanged (the `Completion()` branch defaults to the existing EventStreamThenQuit path for claude).
