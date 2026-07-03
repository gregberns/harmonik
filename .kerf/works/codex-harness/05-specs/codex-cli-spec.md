# Dimension 2 spec — the codex CLI adapter

> Research dimension `codex-cli` (04-research/codex-cli/findings.md) → implementation.
> **Authoritative detail: `C2-codex-adapter-spec.md`.** Dimension→component crosswalk + normative
> summary.

## Normative decision
codex `exec` is a one-shot run-to-exit JSONL surface. The codex adapter is therefore
**spawn-per-turn** with a **captured** session id, NOT spawn-once-and-inject like claude:
- Launch: `codex exec --json --sandbox workspace-write -a never -C <worktree>` (task via stdin/arg).
- Session id: caller CANNOT mint (NOT_PLANNED #25111) → capture `thread_id` from the first
  `thread.started` JSONL event; `SessionIDPolicy()==Captured`.
- Re-task (review-loop iter ≥2): `codex exec resume <thread_id> "<feedback>"` — a fresh process
  (structurally identical to claude's `claude --resume`, so the shared loop accommodates it).
- Completion: process exit + terminal `turn.completed`/`turn.failed`; `Completion()==ProcessExit`;
  `Teardown` is a no-op (no `/quit`, no splash/dual-Enter/post-quit-kill).
- Commit: codex's commit is a non-deterministic model decision (`git add -A` footgun #8548) →
  instruct + verify the `Refs:<bead>` trailer, with a deterministic **commit-after-exit fallback**.

## Implementing component
- **C2** (`C2-codex-adapter-spec.md`): `CodexHarness`, `buildCodexLaunchSpec`, JSONL parser, captured
  thread_id run-state, commit-trailer guarantee.

## Acceptance (summary; full AC in C2)
Twin-substrate scenario tests for all four variants (trailer-commit / edits-no-commit → fallback /
no-edits → noChange / `turn.failed`); resume argv asserted; `Completion()`/`SessionIDPolicy()` values
asserted.

## Edge cases
No `thread.started` before exit → fail closed (id not captured). MCP servers silently disable
`--json`+`--output-schema` (#15451) → forbid MCP in the codex launch env for the structured path.
