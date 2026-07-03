# 02 — Analysis: current code map for the codex-harness change

> Plan-jig Pass 2. Factual map of the territory, traceable to `file:line`. Drawn from the
> code-reading research in `04-research/current-harness/findings.md`. No proposals here — see
> `05-specs/` and `06-integration.md` for those.

## Affected areas (by package/file)

### A. Launch-spec construction — `internal/daemon/claudelaunchspec.go`
The single point where Claude-specific behavior is concentrated. `buildClaudeLaunchSpec`
(`:220-424`) builds argv (`:357-376`), sets `spec.Binary = rc.handlerBinary` (`:408`), `spec.WorkDir
= rc.workspacePath` (`:411`), materializes `.claude/settings.json` (`:233-236`) and `~/.claude.json`
trust (`:241`), and assembles `PreExecMessages` (`:384-397`). **This is the primary file the seam
extracts from.** It is invoked via the swappable `deps.launchSpecBuilder` function field
(`dot_cascade.go:521-523`).

### B. Handler / session-id minting + env — `internal/handler/claudehandler_chb006_024.go`
`MintClaudeSessionID` (`:147`) mints caller-minted UUIDv7. `ClaudeEnvVars` (`:232-310`) builds the
child env, including the **credential strip + empty-override guard** for `ANTHROPIC_API_KEY` /
`ANTHROPIC_AUTH_TOKEN` / `CLAUDE_CODE_OAUTH*` (`:196-204, 246-253, 292-296`). Forbidden-flag
deny-list (`:52-62`). **The codex adapter needs analogous env handling for `OPENAI_API_KEY` /
`CODEX_API_KEY`.**

### C. Prompt injection / re-task / teardown — `internal/daemon/pasteinject.go`
TUI-coupled: splash-dismiss, bracketed-paste of `agent-task.md` (`:879-910`), resume re-task
(`:932-1014`), `/quit` + watchdogs (`:451-711`). **The codex adapter replaces all of this with
spawn-per-turn `codex exec`** — most of this file is *not reused* for codex.

### D. tmux substrate — `internal/daemon/tmuxsubstrate.go`, `osadapter.go`, `substrate.go`, `handler.go`
`SpawnWindow(ctx, SubstrateSpawn)` (`substrate.go:30-37`) takes opaque argv
`append([]string{spec.Binary}, spec.Args...)` (`handler.go:291,299`). new-window with cwd + env
`-e` injection (`osadapter.go:486-504`). Spawn semaphore (`tmuxsubstrate.go:309-349`). PID-liveness
(`:900-989`). **Harness-blind — reused unchanged.** (codex needs no TUI, but running `codex exec`
in a tmux window is still viable and keeps inspectability; alternatively a plain subprocess.)

### E. Worktree lifecycle — `internal/workspace/createworktree.go`, `internal/daemon/workloop.go`
`CreateWorktree` → `git worktree add -b run/<run_id> <path> <parentCommit>`
(`createworktree.go:123-136`). Merge-one-at-a-time `lockedMergeRunBranchToMain` (`workloop.go:3447`)
under `deps.mergeMu`, rebase+FF onto `deps.targetBranch` (default `main`, `:576-584`). Remove
(`:2896-2908`). Reviewer detached-HEAD worktree (`createworktree.go:37-72`). **Pure git —
harness-agnostic, reused unchanged.**

### F. Commit / completion detection — `internal/daemon/workloop.go`, `sessioncontext_chb023.go`
`resolveWorktreeHEAD` vs captured `headSHA` (`workloop.go:2382-2385`); `Refs:<beadID>` trailer
subsumption (`beadAlreadySubsumedInMain`, `:2668-2684`); noChange merge logic (`:3497-3529`);
reopen-on-no-commit (`:2649-2656`). **Pure git + trailer convention — harness-agnostic, reused
unchanged.** The completion *signal* differs: claude → `sess.Wait` returns after `/quit`/kill;
codex `exec` → process exit. Both surface through the same `sess.Wait` boundary if codex is run as a
substrate session.

### G. Adapter registry + ready-detection — `internal/handlercontract/adapterregistry_hc012.go`, `adapter_claudecode.go`
`AdapterRegistry` keyed by `core.AgentType` (`adapterregistry_hc012.go:67`); `ForAgent(
AgentTypeClaudeCode)` (`dot_cascade.go:587`, `reviewloop.go`). `ClaudeCodeAdapter.DetectReady` keyed
on `agent_ready`/NDJSON (`adapter_claudecode.go:88-95`). **The natural home for a
`CodexAdapter` + `core.AgentTypeCodex`.**

### H. DOT workflow model — `internal/workflowvalidator/dotparser.go`, `internal/core/node.go`, `standard-bead.dot`
Nodes parsed into a generic attr-map: `type`, `handler_ref`, `agent_type`, `model`, `effort`,
`prompt`, `role` (`dotparser.go:156-216`; `node.go:13-104`). **`handler_ref` does NOT select a
binary** — only role/phase (`dot_cascade.go:216,802-805`). Binary is always `deps.handlerBinary`
(`Config.HandlerBinary` default `"claude"`, `daemon.go:92-99`, `workloop.go:516-518`). **Adding a
`harness`/`agent_runtime` node attribute is free (generic map); the cascade must route off it.**

### I. Review-loop — `internal/daemon/reviewloop.go`, `dot_cascade.go`
Reviewer reuses the **same** `buildClaudeLaunchSpec` + substrate + pasteinject path as the
implementer, differing only by `phase` (`reviewloop.go:227` vs `:814`). **If the seam is at the
launch-spec/adapter level, the review-loop inherits harness selection for free** — a codex run's
reviewer is also codex (or per design, the reviewer harness could be pinned independently).

### J. Config + daemon wiring — `internal/daemon/daemon.go`, `Config`
`Config.HandlerBinary` (default `"claude"`, `daemon.go:92-99`) is the *only* binary selector today,
and it is global. **Harness selection needs a richer config surface** (default harness + per-bead /
per-queue override) plumbed to `deps.launchSpecBuilder` and the adapter registry.

## Existing constraints / invariants to preserve

1. **Opaque-argv substrate (D).** `SpawnWindow` must keep taking an opaque argv; do not leak
   harness specifics into the substrate. The seam lives *above* the substrate.
2. **Caller-minted UUIDv7 session contract (B).** harmonik keys run state on a session id it mints.
   codex cannot mint → the seam must tolerate a **captured** session id (set after launch), not
   assume a pre-known one. This is the single biggest interface-shape constraint.
3. **`Refs:<beadID>` trailer convention (F).** Completion detection depends on it. The codex adapter
   MUST ensure the commit carries this trailer (instruct + verify, or commit-after-exit wrapper).
4. **Credential strip + empty-override pattern (B).** The codex env path MUST mirror this for
   `OPENAI_API_KEY`/`CODEX_API_KEY`, or it regresses the credit-burn guard.
5. **N-1 back-compat (J,H).** With no harness specified, everything resolves to `claude` exactly as
   today: `Config.HandlerBinary` default stays `"claude"`; absent node `harness` attr → claude.
6. **`deps.launchSpecBuilder` is already a function-field seam (A).** Prefer extending it over
   inventing a parallel dispatch path — the project rule is "smallest seam, no speculative layers."

## Conventions to follow

- **Adapter pattern** already exists (`AdapterRegistry` keyed by `core.AgentType`) — extend it, do
  not invent a new registry.
- **Spec-first:** the `Harness` contract is normative → a `specs/` artifact + a DOT-attribute spec.
- **Env handling:** allowlist + strip + empty-override (claude precedent at
  `claudehandler_chb006_024.go:196-204,292-296`).
- **Materialized config files:** claude materializes `.claude/settings.json` / `~/.claude.json`;
  codex's analog is `$CODEX_HOME/config.toml` (e.g. `forced_login_method = "chatgpt"`).
- **Phase enum** (`ReviewLoopPhase*`) distinguishes implementer/reviewer — reuse, don't fork.

## Relevant recent history / related work

- The whole launch/session/spawn area is actively maintained (hk-9vp51 session-nesting fix-forward
  `ff23633c`, 2026-06-08; spawn-semaphore wedge hk-4l7zs). **Implication:** the codex adapter must
  not perturb the live-session resolution or spawn-semaphore logic — it plugs in *above* them.
- The credit-burn incident (`ANTHROPIC_API_KEY`-in-`.env`) is the direct precedent for the codex
  env guard; the fix lives in `ClaudeEnvVars` and must be paralleled, not bypassed.

## Code-health notes

- `buildClaudeLaunchSpec` is large (`:220-424`) and mixes argv, env, file-materialization, and
  pre-exec messages. Extracting the **Harness** seam is a natural opportunity to split the
  claude-specific body from the shared scaffolding — but keep the refactor minimal (rename/relocate,
  not rewrite) to avoid destabilizing a hot path.
- There is no existing notion of "two harnesses"; `HandlerBinary` being a single global string is
  the load-bearing assumption the change relaxes.
