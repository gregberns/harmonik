# Dimension 3 spec — where the harness-abstraction seam belongs

> Research dimension `seam-design` (04-research/seam-design/findings.md) → implementation.
> **Authoritative detail: `C1-harness-interface-spec.md` (the interface) + `C4-selection-config-spec.md`
> + `C5-workflow-integration-spec.md` (the routing).** Dimension→component crosswalk + normative
> summary.

## Normative decision
The smallest seam that supports both harnesses has **two declared insertion points** and nothing
else:
1. **`launchSpecBuilder` / `AdapterRegistry` lookup** — the cascade resolves a harness
   (`ResolveHarness`, C4), then obtains its launch-spec builder + adapter from the registry
   (`AdapterRegistry.ForAgent`, C1). Replaces the always-claude `deps.launchSpecBuilder` default.
2. **`Completion()` gate at `dot_cascade.go:643`** — the `go pasteInjectQuitOnCommit(...)` launch is
   gated on `harness.Completion()`: launched for `EventStreamThenQuit` (claude), **skipped** for
   `ProcessExit` (codex) so the heartbeat-staleness kill path is bypassed and the run completes on
   `sess.Wait` + `commitHardCeiling`. This is load-bearing — the bypass lands HERE, not in
   `workloop.go` (which holds only the bare `sess.Wait`).

Everything else (tmux substrate, worktree mgmt, commit-detection, merge, review-loop control flow,
queue/dispatch) is SHARED and MUST NOT be branched per harness. The key de-risk: claude's iteration
≥2 is **already** a fresh `claude --resume` process, so codex's spawn-per-turn breaks no shared
cross-iteration-process assumption.

## Implementing components
- **C1** — the `Harness` interface (the seam type).
- **C4** — `ResolveHarness` + config (what feeds the registry lookup).
- **C5** — the cascade/review-loop routing + the `Completion()` gate at `:643`.

## Rejected alternatives
Forking the dispatch path per harness (duplication/drift); a full plugin marketplace (speculative,
out of scope). Chosen seam = smallest, reuses existing swappable hooks.
