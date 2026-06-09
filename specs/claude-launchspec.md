# Claude Launch Spec

```yaml
---
title: Claude Launch Spec
spec-id: claude-launchspec
requirement-prefix: CLS
status: draft
spec-category: runtime-subsystem
spec-shape: requirements-first
version: 0.1
spec-template-version: 1.1
owner: foundation-author
last-updated: 2026-05-19
depends-on:
  - handler-contract
  - claude-hook-bridge
  - workspace-model
  - execution-model
  - process-lifecycle
---
```

## 1. Purpose

This spec defines the `buildClaudeLaunchSpec` assembly function and the `claudeRunCtx`/`claudeRunArtifacts` data contracts that bridge between harmonik's run-dispatch layer and the Claude Code (or twin) subprocess launch. It also owns the normative lifecycle state machine for the `review-loop` phase sequence (`implementer-initial` → `reviewer` → `implementer-resume` → `reviewer` → …) and the input validation constraints that guard the assembly path.

Prior to this spec, the assembly obligations were spread across:
- [handler-contract.md §4.2 HC-005, HC-006, HC-006a] — LaunchSpec field definitions and per-phase invariants
- [claude-hook-bridge.md §4.2–4.3 CHB-006–CHB-009, §4.7 CHB-018, §4.9 CHB-024, §4.11 CHB-028, §4.12 CHB-029] — env-var schema, session-ID minting, settings materialization

This spec does **not** re-state those rules. It consolidates three things those specs do not cover as a unit:

1. The ordered assembly sequence as a single normative contract.
2. The review-loop phase lifecycle state machine (transition table, input constraints per state).
3. The `claudeRunCtx` field validity rules — which fields are required, which are phase-conditional, and what constitutes a structural error on violation.

## 2. Scope

### 2.1 In scope

- The `claudeRunCtx` input struct: field semantics, phase-conditional presence rules, and shape constraints.
- The `claudeRunArtifacts` output struct: field semantics and caller obligations.
- The 9-step `buildClaudeLaunchSpec` assembly sequence and its ordering invariants.
- The review-loop phase lifecycle: state machine, phase transition table, per-phase input rules.
- Input validation: `ModelPreference` shape constraints (model, effort); the worktree path-check for `--dangerously-skip-permissions`.
- Error classification: which failures are `ErrStructural` vs. transient.

### 2.2 Out of scope

- The content and schema of `.claude/settings.json` — [claude-hook-bridge.md §4.1 CHB-001..CHB-005].
- The env-var schema produced by `ClaudeEnvVars` — [claude-hook-bridge.md §4.2 CHB-006].
- The session-ID minting and `--session-id` / `--resume` semantics — [claude-hook-bridge.md §4.3 CHB-008, CHB-009].
- The forbidden-flag deny-list — [claude-hook-bridge.md §4.2 CHB-007].
- The pre-exec progress message schema — [claude-hook-bridge.md §4.7 CHB-018].
- The settings-shadow verification logic — [claude-hook-bridge.md §4.9 CHB-024].
- The agent-task.md content shape by phase — [claude-hook-bridge.md §4.11 CHB-028].
- The worktree trust pre-seed — [claude-hook-bridge.md §4.12 CHB-029].
- The LaunchSpec wire-protocol record shape — [handler-contract.md §6.1].
- The review-loop dispatcher state machine (what the daemon does with outcomes) — [process-lifecycle.md].

## 3. Glossary

- **assembly function** — `buildClaudeLaunchSpec`; the single Go function in `internal/daemon/claudelaunchspec.go` that produces a `handler.LaunchSpec` and `claudeRunArtifacts` from a `claudeRunCtx`.
- **claudeRunCtx** — the read-only per-launch input struct assembled by the daemon's claim/dispatch path before calling `buildClaudeLaunchSpec`.
- **claudeRunArtifacts** — the output struct carrying the Claude session ID, session log path, handler session ID, and pre-exec message payloads produced by the assembly function.
- **phase** — a string discriminant from the closed enum `{implementer-initial, implementer-resume, reviewer, ""}`. Empty string means `workflow_mode = single` (no review loop).
- **review-loop cycle** — one iteration of the sequence `(implementer-* phase, reviewer phase)` within a `workflow_mode = review-loop` run. The first cycle uses `implementer-initial`; subsequent cycles (after a `REQUEST_CHANGES` verdict) use `implementer-resume`.

## 4. Normative requirements

### 4.a Subsystem envelope

#### CLS-ENV-001 — Envelope declaration

Envelope for the claude-launchspec subsystem per [architecture.md §4.0 AR-053]. This subsystem is the `buildClaudeLaunchSpec` assembly function (`internal/daemon/claudelaunchspec.go`) that bridges the daemon's run-dispatch layer to the Claude Code (or twin) subprocess launch. It is a pure assembly seam: it transforms a read-only `claudeRunCtx` into a `handler.LaunchSpec` plus `claudeRunArtifacts`; it neither emits bus events itself nor persists state — those obligations are the daemon caller's (CLS-030, CLS-031).

(a) Events produced: none directly. The function returns `claudeRunArtifacts.preExecMsgs` — 4 ordered NDJSON pre-exec messages per [claude-hook-bridge.md §4.7 CHB-018]; the daemon caller MUST emit them on the bus before `handler.Launch` per §4.4 CLS-030. Emission ownership is the caller's, not this subsystem's.

(b) Events consumed: none. The assembly function performs no bus reads; all inputs arrive via the `claudeRunCtx` struct (§4.1).

(c) Types introduced (cross-subsystem):
  | Type | `Tags:` | `Axes:` (if non-baseline) |
  |---|---|---|
  | `claudeRunCtx` (§4.1) | mechanism | baseline |
  | `claudeRunArtifacts` (§3, §4.4) | mechanism | baseline |
  | `ModelPreferenceError` (§4.1 CLS-004, §5) | mechanism | baseline |

(d) Handlers implemented: none. This subsystem assembles the `LaunchSpec` for the `claude-code` handler defined in [handler-contract.md §4.1]; it does not itself implement a handler-contract handler.

(e) State owned: none. `claudeRunCtx` is a read-only per-launch input (§3); `claudeRunArtifacts` is a returned value. Durability of `claudeRunArtifacts.claudeSessionID` for implementer-resume is the daemon's responsibility per §4.4 CLS-031 and [claude-hook-bridge.md §4.8 CHB-023], not this subsystem's.

(f) Control points provided: none. The assembly function is mechanism-tagged; the credential-env deny-list strip (§4.1) and the `isHarmonikManagedWorktree` positive-allowlist check (§4.5 CLS-040) are deterministic guards internal to assembly, not control-points primitives per [control-points.md §4.1].

(g) NFRs inherited / overridden:
  - Inherited: credential-isolation `CI-002`/`CI-003` — the env deny-list strip of §4.1 enforces the credential-holder discipline at the launch boundary.
  - Inherited: `HC-INV-004` pre-exec-before-launch ordering — surfaced as the CLS-030 caller obligation.
  - Overridden: none.

(h) Boundary classification per operation:
  | Operation | `Tags:` | Axes |
  |---|---|---|
  | `build_launch_spec` (§4.2 CLS-010) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent` |
  | `mint_claude_session_id` (§4.2 step 1) | mechanism | `llm-freedom=none; io-determinism=nondeterministic; replay-safety=unsafe; idempotency=non-idempotent` |
  | `validate_model_preference` (§4.1 CLS-004) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent` |
  | `worktree_path_check` (§4.5 CLS-040) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent` |

Tags: mechanism

### 4.1 claudeRunCtx field contract

#### CLS-001 — Unconditional required fields

Every `claudeRunCtx` MUST supply:

- `runID` — non-zero `core.RunID` (UUIDv7).
- `beadID` — non-empty string.
- `workspacePath` — non-empty absolute path; the directory MUST exist before `buildClaudeLaunchSpec` is called.
- `daemonSocket` — non-empty path; the daemon MUST be listening on this socket before the assembled `LaunchSpec` is passed to `handler.Launch`.
- `handlerBinary` — non-empty string; the binary name or absolute path for `LaunchSpec.Binary`.
- `daemonBinaryPath` — non-empty absolute path; set from `os.Executable()` at daemon startup per PL-006a.

Violation of any required-field rule returns `ErrStructural` from `buildClaudeLaunchSpec`.

#### CLS-002 — Phase field and workflow mode

`phase` encodes the review-loop position for this launch:

| `phase` value | Meaning | Required `workflowMode` |
|---|---|---|
| `""` (empty) | Single-mode launch; no review loop | `single` or `""` |
| `"implementer-initial"` | First implementer launch in a review-loop cycle | `review-loop` |
| `"implementer-resume"` | Subsequent implementer launch after a `REQUEST_CHANGES` verdict | `review-loop` |
| `"reviewer"` | Reviewer launch evaluating the implementer's output | `review-loop` |

Any other `phase` value is a structural error.

#### CLS-002a — Remaining unconditional fields

The following `claudeRunCtx` fields are always present but have relaxed constraints:

| Field | Semantics | Empty/zero behavior |
|---|---|---|
| `beadTitle` | Human-readable bead title from the Beads ledger. Used as the `title:` header in `agent-task.md` (CHB-028). | Falls back to `beadID`. |
| `beadDescription` | Bead body verbatim from the Beads ledger. Used as the `## Task Description` body in `agent-task.md`. | Falls back to `beadTitle`, then `beadID`, so the file is never structurally empty. |
| `baseEnv` | Environment inherited from daemon `Config.HandlerEnv`. MUST already include `HARMONIK_PROJECT_HASH` per PL-006a. CHB-006 vars are appended (or overwrite) by `ClaudeEnvVars`. Env assembly additionally removes the **credential env deny-list** keys (`{ANTHROPIC_API_KEY, ANTHROPIC_AUTH_TOKEN, CLAUDE_CODE_OAUTH*}`, per [credential-isolation.md §4.1 CI-002, §4.2 CI-003]) from the constructed child env, symmetric with the existing `HARMONIK_SECRET_*` strip. This is distinct from the forbidden-flag deny-list of [claude-hook-bridge.md §4.2 CHB-007]. | nil slice is valid; results in an env built solely from CHB-006 vars. |
| `workflowMode` | Resolved workflow mode for this run (e.g. `"single"`, `"review-loop"`). Must be consistent with `phase` per CLS-002. | Empty string is treated as `"single"`. |
| `agentTaskReAttach` | Signals that this launch is on the re-attach path (daemon restart mid-session). When `true`, `WriteAgentTask` skips collision check and returns nil if `agent-task.md` already exists (CHB-028 re-launch semantics). | `false` (default, non-re-attach). |
| `extraContext` | Optional operator-supplied free-form string injected into `agent-task.md` as a `## Extra Context` section (hk-boiwe). | Empty means no section is rendered. |

#### CLS-003 — Phase-conditional fields

The following `claudeRunCtx` fields are only valid for specific phases; supplying them for the wrong phase is a no-op (the assembly function ignores them) but SHOULD NOT be done by callers:

| Field | Required when | MUST be absent / zero when |
|---|---|---|
| `priorClaudeSessID` | `phase = implementer-resume` — carries the Claude session ID minted by the prior `implementer-initial` launch | All other phases — MUST be nil |
| `priorVerdictFile` | `phase = implementer-resume` — absolute path to `.harmonik/review.iter-<N-1>.json` | All other phases — empty |
| `priorVerdictSummary` | `phase = implementer-resume` | All other phases — empty |
| `reviewBaseSHA` | `phase = reviewer` — base commit SHA for the diff under review | All other phases — empty |
| `reviewHeadSHA` | `phase = reviewer` — head commit SHA for the diff under review | All other phases — empty |
| `iterationCount` | `phase ∈ {implementer-initial, implementer-resume, reviewer}` — 1-based iteration index, bounded by ON-004 cap (3) | `phase = ""` (single-mode) — MUST be 0 or negative |

Cross-ref: [handler-contract.md §4.2 HC-006a] (per-phase LaunchSpec field table, which cites `buildClaudeLaunchSpec` as implementation evidence).

#### CLS-004 — ModelPreference validation

When `claudeRunCtx.model` is non-empty it MUST match `^[A-Za-z0-9._:/-]+$` and be ≤ 128 characters. When `claudeRunCtx.effort` is non-empty it MUST be one of `{low, medium, high, xhigh, max}`. Violation of either rule returns `*ModelPreferenceError` (a typed `ErrStructural`) before any file-system or IPC operations are attempted.

Cross-ref: [handler-contract.md §4.10 HC-055a] (invariants on the `ModelPreference` descriptor).

### 4.2 Assembly sequence and ordering invariants

#### CLS-010 — The assembly sequence is normative (9 steps, steps 3a/3b are sub-steps of step 3)

`buildClaudeLaunchSpec` MUST execute the following steps in order. No step may be skipped; no step may reorder relative to its predecessor unless explicitly noted. The numbering matches the step comments in `internal/daemon/claudelaunchspec.go`.

1. **MintClaudeSessionID** — per [claude-hook-bridge.md §4.3 CHB-008/CHB-009]: mint a fresh UUIDv7 for `phase ∈ {single, implementer-initial, reviewer}`; reuse `priorClaudeSessID` for `phase = implementer-resume`. Returns `mintResult.ResumeMode`.

2. **DeriveCIaudeTranscriptPath** — per [claude-hook-bridge.md §4.7 CHB-018 step 2]: derive the absolute session-log path from `workspacePath` and the minted session ID. No I/O.

3. **MaterializeClaudeSettings** — per [claude-hook-bridge.md §4.1 CHB-001..CHB-005]: atomically write `.claude/settings.json` into the workspace. MUST use `daemonBinaryPath` (not the bare binary name) for the hook `command` field.

   3a. **EnsureWorktreeTrust** — per [claude-hook-bridge.md §4.12 CHB-029] and [workspace-model.md §4.7b WM-040b]: pre-seed `~/.claude.json` with `hasTrustDialogAccepted: true` for the workspace path. MUST run AFTER step 3 and BEFORE step 8.

   3b. **WriteAgentTask** — per [claude-hook-bridge.md §4.11 CHB-028]: atomically write `.harmonik/agent-task.md` with phase-correct content. MUST run AFTER steps 3 and 3a.

4. **CheckSettingsLocalJSON** — per [claude-hook-bridge.md §4.9 CHB-024]: fail-fast if `settings.local.json` shadows bridge hooks. Returns `ErrStructural` on violation.

5. **Env assembly** — per [claude-hook-bridge.md §4.2 CHB-006]: call `ClaudeEnvVars` with a fully-populated `ClaudeEnvConfig` to derive the subprocess environment slice. `ClaudeEnvVars` MUST strip every credential env deny-list key per [credential-isolation.md §4.2 CI-003] (the daemon-side scrub boundary); the substrate handoff is an assertion point per [credential-isolation.md §4.2 CI-004], not a second scrub site.

6. **ModelPreference validation + Argv construction** — per [handler-contract.md §4.10 HC-055, HC-055a, HC-055b]: validate `model` and `effort` fields (CLS-004); emit `--session-id` or `--resume` per `mintResult.ResumeMode`; append `--model` / `--effort` when non-empty; append `--dangerously-skip-permissions` when `isHarmonikManagedWorktree` is true.

7. **CheckForbiddenFlags** — per [claude-hook-bridge.md §4.2 CHB-007]: reject any forbidden flags in the constructed argv or env. Returns `ErrStructural` on violation.

8. **PreExecMessages** — per [claude-hook-bridge.md §4.7 CHB-018]: render the 4 ordered pre-exec progress messages (`handler_capabilities`, `session_log_location`, `skills_provisioned`, `agent_ready`). The caller MUST emit these on the event bus BEFORE calling `handler.Launch`.

9. **Assemble and return** — construct `handler.LaunchSpec` (Binary from `rc.handlerBinary`; Args, Env, WorkDir, Role from prior steps) and `claudeRunArtifacts` (claudeSessionID, sessionLogPath, handlerSessionID, preExecMsgs).

#### CLS-011 — On-error invariant

If any step returns a non-nil error, `buildClaudeLaunchSpec` MUST return the zero `handler.LaunchSpec`, zero `claudeRunArtifacts`, and the wrapped error. The caller MUST NOT call `handler.Launch` when `buildClaudeLaunchSpec` returns a non-nil error.

### 4.3 Review-loop phase lifecycle state machine

#### CLS-020 — Phase lifecycle state machine

This section owns the normative lifecycle for the review-loop `phase` field. The reviewer-verdict field is the `verdict` string from the agent-reviewer JSON schema v1 (per [workspace-model.md §4.7 WM-027a]).

```
                          (workflow_mode = review-loop)
                                       │
                                       ▼
                         ┌─────────────────────────────┐
                         │    implementer-initial       │ iteration = 1
                         │    (fresh Claude session)    │
                         └──────────────┬──────────────┘
                                        │ outcome_emitted
                                        ▼
                         ┌─────────────────────────────┐
                         │         reviewer             │ iteration = 1
                         │   (fresh Claude session)     │
                         └──────────────┬──────────────┘
                                        │
               ┌────────────────────────┼────────────────────────┐
               │ verdict=APPROVE        │ verdict=REQUEST_CHANGES  │ verdict=BLOCK
               ▼                        ▼                          ▼
         [cycle_complete:         ┌────────────────────┐    [cycle_complete:
          approved]               │ implementer-resume  │     blocked]
                                  │ iteration = N+1     │
                                  │ (resume prior       │
                                  │  Claude session)    │
                                  └────────┬───────────┘
                                           │ outcome_emitted
                                           │ (if iteration < cap)
                                           ▼
                                  ┌────────────────────┐
                                  │     reviewer        │ iteration = N+1
                                  │ (fresh session)     │
                                  └────────┬───────────┘
                                           │
                          (repeat until APPROVE, BLOCK, or cap)
                                           │
                                           ▼
                              [cycle_complete: cap_reached]
```

#### CLS-021 — Phase transition rules (normative table)

| Current phase | Trigger | Next phase | Notes |
|---|---|---|---|
| `—` (pre-launch) | dispatch, `workflow_mode = review-loop` | `implementer-initial` | `iteration = 1`; `priorClaudeSessID = nil` |
| `implementer-initial` | `outcome_emitted` received | `reviewer` | `iteration = 1`; reviewer mints fresh session (CHB-009) |
| `reviewer` (iteration ≤ cap–1) | `verdict = REQUEST_CHANGES` | `implementer-resume` | `iteration += 1`; carries `claude_session_id` from `implementer-initial` |
| `reviewer` (any iteration) | `verdict = APPROVE` | — (cycle complete) | `completion_reason = approved` |
| `reviewer` (any iteration) | `verdict = BLOCK` | — (cycle complete) | `completion_reason = blocked` |
| `reviewer` (iteration = cap) | `verdict = REQUEST_CHANGES` | — (cycle complete) | `completion_reason = cap_reached`; per [operator-nfr.md §4.1 ON-004] |
| `implementer-resume` | `outcome_emitted` received | `reviewer` | `iteration` unchanged; reviewer mints fresh session |
| `—` (pre-launch) | dispatch, `workflow_mode = single` | — (single-mode) | `phase = ""`; no review loop; no `iteration` |

Cross-ref: [handler-contract.md §4.2 HC-006a] (per-phase LaunchSpec field table, authoritative for which fields differ across phases).

#### CLS-022 — claudeSessionID durability across implementer-resume

For `phase = implementer-resume`, the `priorClaudeSessID` value passed to `buildClaudeLaunchSpec` MUST be the exact `claude_session_id` minted by the corresponding `implementer-initial` launch. The daemon's durability obligation for this value is defined in [claude-hook-bridge.md §4.6 CHB-023]. The assembly function returns `ErrStructural` if `phase = implementer-resume` and `priorClaudeSessID` is nil or empty.

#### CLS-023 — Reviewer session independence

Each reviewer phase MUST receive a freshly minted Claude session ID. The assembly function MUST pass `nil` for `priorClaudeSessID` on reviewer launches. The `mintResult.ResumeMode` field MUST be `false` for reviewer launches; violation is caught by the MintClaudeSessionID implementation per CHB-009.

### 4.4 claudeRunArtifacts caller obligations

#### CLS-030 — Pre-exec message emission MUST precede handler.Launch

The `claudeRunArtifacts.preExecMsgs` slice returned by `buildClaudeLaunchSpec` contains 4 ordered NDJSON messages (per CHB-018). The caller MUST emit all 4 messages on the event bus **before** calling `handler.Launch`. Emitting after launch creates a race between the relay's first hook emission and the handler's first message read, violating HC-INV-004.

#### CLS-031 — claudeSessionID storage for implementer-resume

The caller MUST persist `claudeRunArtifacts.claudeSessionID` into the Run record after a successful `implementer-initial` launch so it can be passed as `priorClaudeSessID` on the next `implementer-resume` launch. Durability of this value is the daemon's responsibility (CHB-023).

### 4.5 Worktree path-check

#### CLS-040 — isHarmonikManagedWorktree positive-allowlist match

`--dangerously-skip-permissions` is emitted in argv if and only if `workspacePath` canonicalizes (via `os.EvalSymlinks`) to a path whose prefix is `worktreeRootPath + filepath.Separator`. Both paths are canonicalized before comparison. If either `os.EvalSymlinks` call fails, the function returns `false` and the flag is silently omitted; no error is returned. An empty `worktreeRootPath` always returns `false`.

This is a positive-allowlist match, not a negative-allowlist. The flag is operator-sanctioned for harmonik-managed worktrees where the operator owns the worktree directory tree.

Cross-ref: [handler-contract.md §4.10 HC-055b].

## 5. Error taxonomy

| Sub-reason | Error type | Trigger |
|---|---|---|
| `model_preference_invalid` | `*ModelPreferenceError` (ErrStructural) | `model` shape or `effort` enum violation (CLS-004) |
| `session_id_absent_for_resume` | ErrStructural | `phase = implementer-resume` with nil/empty `priorClaudeSessID` |
| `materialize_settings_failed` | ErrStructural (wrapped) | `MaterializeClaudeSettings` error (step 3) |
| `trust_seed_failed` | ErrStructural (wrapped) | `EnsureWorktreeTrust` error (step 4) |
| `task_file_empty` | ErrStructural (wrapped) | `WriteAgentTask` error (step 5) |
| `bridge_settings_shadowed` | ErrStructural (wrapped) | `CheckSettingsLocalJSON` detects shadow (step 6) |
| `forbidden_flag` | ErrStructural (wrapped) | `CheckForbiddenFlags` denial (step 9) |
| `pre_exec_messages_failed` | ErrStructural (wrapped) | `PreExecMessages` error (step 10) |

Cross-ref: [claude-hook-bridge.md §8] for the sub-reason strings owned by CHB steps.

## 6. Cross-references

| What | Where |
|---|---|
| LaunchSpec record shape (all fields) | [handler-contract.md §6.1] |
| Per-phase LaunchSpec field invariants (normative table) | [handler-contract.md §4.2 HC-006a] |
| Env-var schema | [claude-hook-bridge.md §4.2 CHB-006] |
| Session-ID minting | [claude-hook-bridge.md §4.3 CHB-008, CHB-009] |
| Forbidden-flag deny-list (CLI flags) | [claude-hook-bridge.md §4.2 CHB-007] |
| Credential env deny-list / scrub (env keys) | [credential-isolation.md §4.1 CI-002, §4.2 CI-003] |
| Settings materialization | [claude-hook-bridge.md §4.1 CHB-001..CHB-005] |
| Settings-shadow verification | [claude-hook-bridge.md §4.9 CHB-024] |
| Agent-task.md content shape | [claude-hook-bridge.md §4.11 CHB-028] |
| Worktree trust pre-seed | [claude-hook-bridge.md §4.12 CHB-029] |
| Pre-exec emission ordering | [claude-hook-bridge.md §4.7 CHB-018] |
| ModelPreference descriptor invariants | [handler-contract.md §4.10 HC-055a] |
| Worktree path-check for `--dangerously-skip-permissions` | [handler-contract.md §4.10 HC-055b] |
| Review-loop iteration cap | [operator-nfr.md §4.1 ON-004] |
| Reviewer verdict artifact | [workspace-model.md §4.7 WM-027a] |
| claudeSessionID durability | [claude-hook-bridge.md §4.6 CHB-023] |
| Review-loop dispatcher (what daemon does with outcomes) | [process-lifecycle.md] |

## 7. Test surface

#### CLS-T001 — Per-phase assembly invariants (gap filler for HC-006a sensor)

A dedicated test `TestCLS_PerPhaseLaunchSpecInvariants` in `internal/daemon/` SHOULD exercise all three review-loop phases through `buildClaudeLaunchSpec` and assert:
- `implementer-initial`: `--session-id` in argv, `priorClaudeSessID = nil` accepted, `phase` field in LaunchSpec = `"implementer-initial"`.
- `implementer-resume`: `--resume` in argv, `priorClaudeSessID` non-nil required, reused session ID matches input.
- `reviewer`: `--session-id` in argv, fresh session ID minted (not equal to any implementer session ID).

Cross-ref: [handler-contract.md §4.2 HC-006a] test-hookpoint sensor note naming `TestHC006a_PerPhaseLaunchSpecInvariants` as the target file.

## 8. Implementation note

The authoritative implementation is `internal/daemon/claudelaunchspec.go` (`buildClaudeLaunchSpec`). The spec is authoritative; the code follows. Where the code and spec diverge, the spec governs and a bead MUST be filed to realign the code.

## 9. Revision history

| Date | Version | Author | Summary |
|---|---|---|---|
| 2026-05-31 | 0.1.1 | agent (kerf `credfence` work) | Additive credential-scrub notes. §4 `baseEnv` row and the env-assembly step (step 5) now record that env assembly removes the credential env deny-list keys (`{ANTHROPIC_API_KEY, ANTHROPIC_AUTH_TOKEN, CLAUDE_CODE_OAUTH*}`) per [credential-isolation.md CI-002/CI-003], symmetric with the `HARMONIK_SECRET_*` strip, distinct from the CHB-007 forbidden-flag deny-list; the §6 Cross-references table gains a credential-env-deny-list row. No existing requirement changed. Source: kerf `credfence` change design. |
| 2026-05-19 | 0.1 | agent (hk-xpnfy) | Initial spec. Consolidates the assembly sequence, review-loop phase lifecycle state machine, and claudeRunCtx field contract from fragmented coverage across handler-contract.md §4.2 HC-005/006/006a and claude-hook-bridge.md §4.2–4.9 CHB-006..024. No new normative rules added beyond what the code already implements. |
