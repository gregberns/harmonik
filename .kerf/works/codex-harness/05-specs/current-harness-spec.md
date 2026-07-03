# Dimension 1 spec — the implicit Harness interface (current harness)

> Research dimension `current-harness` (04-research/current-harness/findings.md) → implementation.
> **Authoritative detail: `C1-harness-interface-spec.md`** (this file is the dimension→component
> crosswalk + normative summary; the work is organized on two axes — 5 research dimensions ×
> 6 implementation components C1–C6).

## Normative decision
The implicit contract harmonik assumes today is exactly five operations + two policies (Part E of the
research): `LaunchSpec`, `Seed`, `Retask`, `Teardown`, `DetectReady`, plus `SessionIDPolicy`
(Minted/Captured) and `Completion` (EventStreamThenQuit/ProcessExit). We name this as a Go `Harness`
interface and reimplement the claude path as `ClaudeHarness` with **no behavior change**, inserting
at the existing `deps.launchSpecBuilder` seam (`dot_cascade.go:521-523`) + the `AdapterRegistry`
keyed by `core.AgentType`. Everything below the seam (tmux substrate, worktree mgmt, git
commit-detection via the `Refs:<bead>` trailer, merge-one-at-a-time, review-loop control flow) is
SHARED and untouched.

## Implementing component
- **C1** (`C1-harness-interface-spec.md`): `Harness` interface, `CompletionMode`/`SessionIDPolicy`
  enums, `core.AgentTypeCodex`, `ClaudeHarness`, registry/`launchSpecBuilder` wiring.

## Acceptance (summary; full AC in C1)
- `ClaudeHarness.LaunchSpec` pure `(argv,env,cwd)` byte-identical to pre-refactor (golden, C1 AC1.2);
  shared scaffolding side-effects still fire from the caller (C1 AC1.6).
- `ClaudeHarness.Completion()==EventStreamThenQuit`, `SessionIDPolicy()==Minted` (C1 AC1.3).
- Daemon smoke on a real claude bead unchanged (C1 AC1.4).

## Anchor corrections (from change-spec review)
`DetectReady` = `internal/handler/adapter_claudecode.go:99`; `MintClaudeSessionID` =
`claudehandler_chb006_024.go:119`; `AgentType` enum = `internal/core/agenttype.go:14-18`.
