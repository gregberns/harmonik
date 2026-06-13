# Dogfood Smoke Test TRACE: hk Subprocess Invocation and Verdict Expectations

This document captures the exact contract between hk (the harmonik daemon) and the Claude CLI subprocess it spawns in production, plus the return artifacts it expects. This is reference material for smoke-2 implementer before running hk live against a real Claude subprocess.

## 1. Subprocess argv

**Handler entrypoint:**  `internal/handler/handler.go:Launch` (line 122).

**argv construction:**
```
cmd := exec.CommandContext(ctx, spec.Binary, spec.Args...)
```

Where:
- `spec.Binary`: Configured handler binary path. If empty, defaults to `"claude"` per `internal/daemon/workloop.go:192`.
- `spec.Args`: Extra arguments from `daemon.Config.HandlerArgs` (injected by caller; see hk-4e5b5 for injection discipline).

**Example (MVH single mode):**
```
claude [--any-args-from-HandlerArgs]
```

**Example (review-loop, implementer phase 2+):**
```
claude [--any-args-from-HandlerArgs]
```

The `phase` and `iteration_count` metadata are **not** passed as argv; they are delivered via `LaunchSpec` on stdin (see §2 below). The handler subprocess fetches the bead body separately via `br show <bead-id>` per `internal/workspace/workspace_model.md`.

**Cite:** `internal/handler/handler.go:122–129`; `internal/daemon/workloop.go:486–492` (single-mode LaunchSpec construction), `internal/daemon/reviewloop.go:142–148` (implementer-mode LaunchSpec).

---

## 2. LaunchSpec delivery (stdin)

**When:** During `Handler.Launch`, immediately after `cmd.Start()`.

**How:** Daemon delivers the LaunchSpec as JSON on subprocess stdin (≤1 MiB) or via `--launch-spec <path>` argument for larger specs. For MVH, all beads are expected to stay ≤1 MiB, so stdin is the default.

**What is in the LaunchSpec:**

```go
RECORD LaunchSpec {
    run_id                : UUID                    // [execution-model §4.3 Run]
    workflow_id           : UUID                    // [execution-model §4.1 Workflow]
    node_id               : String                  // node_id within workflow
    agent_type            : String                  // agent-type identifier (e.g., "claude-code")
    workspace_path        : String                  // absolute path to the run's worktree
    required_skills       : List<String>            // resolved skill names
    skill_search_paths    : List<String>            // ordered list of paths to search for skills
    timeout               : Integer                 // wall-clock seconds for work; positive
    provisioning_timeout  : Integer                 // seconds for skill provisioning only
    budget                : BudgetRef               // [control-points §4.5]
    freedom_profile_ref   : String                  // [control-points §6.7]
    bead_id               : String | None           // present when bead-tied
    snapshot_token        : String | None           // present for reconciliation handlers
    workflow_mode         : Enum | None             // {single, review-loop, dot}; omitted if default
    phase                 : Enum | None             // {implementer-initial, implementer-resume, reviewer}; multi-phase modes only
    iteration_count       : Integer | None          // 1..3 for review-loop; present iff phase present
    claude_session_id     : String | None           // present iff phase = implementer-resume
    schema_version        : Integer                 // N-1 readable per [operator-nfr §4.5]
}
```

**Critical fields for the subprocess:**

1. **`workspace_path`** — absolute path to the worktree root. The subprocess MUST write all artifacts here (e.g., `.harmonik/review.json` for reviewers).

2. **`phase`** (review-loop only):
   - `implementer-initial`: First implementer launch; no prior session.
   - `implementer-resume`: Subsequent implementer launch; `claude_session_id` is present.
   - `reviewer`: Reviewer phase; subprocess MUST write verdict to `.harmonik/review.json`.

3. **`iteration_count`** (review-loop only): 1–3. Tells the subprocess which iteration it is (for logging, verdict archive paths).

4. **`claude_session_id`** (implementer-resume only): Session ID from prior implementer launch. Handler SHOULD invoke `claude --resume <id>` to reconnect to the prior session.

5. **`required_skills`** and **`skill_search_paths`**: Handler MUST resolve and provision these before `agent_ready` (see §4 below).

**How the subprocess reads it:**
The handler subprocess MUST parse the JSON from stdin or the file referenced by `--launch-spec`. Per `specs/handler-contract.md §4.2.HC-005`, this is the handler's responsibility, not the daemon's.

**Cite:** `internal/handler/handler.go:41–65` (`LaunchSpec` struct); `specs/handler-contract.md §4.2.HC-006` (field definitions and semantics).

---

## 3. Working directory (CWD)

**Where:**
```
cmd.Dir = spec.WorkDir
```

**Value:** The absolute path to the git worktree, constructed by daemon at claim time. Example: `/Users/gb/github/harmonik/.claude/worktrees/run-<run-id>`.

**Branch:** The subprocess's git branch is `worktree-<run-id>`, diverged from `HEAD` at claim time (not main; not the developer's main worktree).

**What the subprocess sees:**
- A fresh, isolated git checkout at the claimed parent commit.
- All `go.mod`, `.git/`, `.beads/` from the parent commit.
- No prior workspace artifacts (fresh checkout per `internal/workspace/createworktree.go`).

**Cite:** `internal/daemon/workloop.go:453` (WorktreePath resolution); `internal/handler/handler.go:127` (`cmd.Dir = spec.WorkDir`).

---

## 4. Environment variables

**Daemon construction:**

The daemon builds `handlerEnv` in its startup path and passes it via:
```
cmd.Env = spec.Env
```

**What is in handlerEnv:**

This is **fully** under daemon config control. At MVH:
- The daemon MUST inject `HARMONIK_PROJECT_HASH=<hash>` per `internal/lifecycle/provenanceenvvar.go`.
- Additional env vars are configured via `daemon.Config.HandlerEnv` (empty at MVH baseline).
- If `spec.Env` is `nil`, the subprocess inherits **no environment** (not the parent's `os.Environ()`).

**Important difference from typical subprocesses:**

If the daemon sets `spec.Env = nil`, the subprocess will **not** inherit `PATH`, `HOME`, `USER`, or any parent process env vars — it starts with a blank slate. The daemon is responsible for injecting anything the handler needs.

**Cite:** `internal/daemon/workloop.go:86–89` (`handlerEnv` field); `internal/handler/handler.go:128` (`cmd.Env = spec.Env`); `internal/lifecycle/provenanceenvvar.go` (HARMONIK_PROJECT_HASH).

---

## 5. stdin and stdout/stderr pipes

**stdin:** Carries LaunchSpec JSON (see §2). After the handler parses it, stdin remains open until the handler closes it or the subprocess exits.

**stdout:** Routed to `handlercontract.SpawnWatcher` per `internal/handler/handler.go:136–141`. The watcher reads NDJSON progress-stream messages from stdout.

**stderr:** Drained into a ring buffer (last ~4 KiB) per `internal/handler/session.go:95` (`stderrRingCapBytes`), exposed via `Outcome.StderrTail` for debugging.

**Cite:** `internal/handler/handler.go:122–144`; `internal/handler/session.go:129–170` (pipe setup and watcher spawn).

---

## 6. Progress-stream protocol (subprocess → daemon)

**What hk expects to read from subprocess stdout:**

The subprocess MUST emit NDJSON progress-stream messages to stdout (or the socket per HC-007) following the envelope in `specs/handler-contract.md §4.2.HC-007a`.

**Critical message sequence for ready detection:**

Per `specs/handler-contract.md §7.2` (launch handshake pseudocode):

1. **`handler_capabilities`** (within 5s) — first message; declares supported versions.
2. **`session_log_location`** (within 10s) — path to the session log.
3. **`skills_provisioned`** (within `provisioning_timeout`, default 60s) — confirms skill installation.
4. **`agent_ready`** (within `spec.timeout`) — signals subprocess is ready for work.

**How daemon detects ready:**

```
func (ClaudeCodeAdapter) DetectReady(event) -> bool {
    return event.Type == "agent_ready"
}
```

**Cite:** `internal/handler/adapter_claudecode.go:88–95`; `specs/handler-contract.md §4.2.HC-007, §7.2, §4.9.HC-039`.

---

## 7. Verdict file expectations (review-loop mode)

**Path:** `${workspace_path}/.harmonik/review.json`

**Schema:** Agent-reviewer JSON schema v1 (must exist and be valid):

```json
{
  "schema_version": 1,
  "verdict": "APPROVE" | "REQUEST_CHANGES" | "BLOCK",
  "flags": [],
  "notes": "Free text; MUST be non-empty (1–3 sentences per agent-reviewer skill contract)"
}
```

**When daemon expects it:**

After `agent_ready` fires, the daemon dispatches the reviewer role (hk emits `reviewer_launched` event). The daemon **waits for the reviewer subprocess to exit**, then immediately calls:

```
verdict, err := workspace.ReadReviewVerdict(wtPath)
```

**Per `internal/daemon/reviewloop.go:250–262`:**

- If the file is absent (`err == nil && verdict == nil`), the daemon treats this as an error condition and emits `agent_failed` with `sub_reason = "verdict absent"`.
- If the file exists but is malformed, `ReadReviewVerdict` returns `ErrMalformed`; daemon emits `agent_failed`.
- If the file is valid, daemon reads it and routes on the `verdict` field:
  - `APPROVE` → run_completed, close bead (success).
  - `REQUEST_CHANGES` → loop back to next iteration (unless iteration cap hit).
  - `BLOCK` → run_failed, close bead with `needs_attention=true`.

**Archiving (multi-iteration):**

Before launching iteration N+1's reviewer, the daemon renames the iteration-N verdict file:

```
review.json → review.iter-N.json
```

Per `internal/daemon/reviewloop.go:212–216`, this archiving is non-fatal on failure (logged to stderr but doesn't abort the cycle).

**Cite:** `internal/workspace/reviewverdict.go:63–221` (verdict reading and archiving); `internal/daemon/reviewloop.go:250–276` (verdict reading and routing).

---

## 8. Session ID and resume handling

**Session ID sources:**

1. **Daemon-assigned:** `handlercontract.NewSessionID()` returns a UUIDv4 string unique per daemon generation. This is the daemon's session ID.

2. **Claude session ID (reviewer-specific):** At MVH, the daemon **does not** call `handlercontract.ParseClaudeSessionID` to extract a real Claude session ID from the subprocess output. Instead, it **synthesizes a dummy ID** per `internal/daemon/reviewloop.go:173–174`:

   ```go
   if state.iterationCount == 1 {
       state.claudeSessionID = rlSynthesiseClaudeSessionID()
   }
   ```

   This synthesized ID is passed to the next implementer launch as `claude_session_id` in the LaunchSpec, but **the real Claude CLI is not invoked in MVH** (you're running against the twin).

**Post-MVH TODO:**

`internal/daemon/reviewloop.go:171` notes:
> "Post-MVH: use handlercontract.ParseClaudeSessionID on the session stdout buffer."

This indicates that in production (non-MVH), the daemon will parse the real Claude session ID from the subprocess stdout and use it for `claude --resume <id>`.

**How implementer-resume works (future):**

1. Reviewer completes iteration N with `REQUEST_CHANGES`.
2. Daemon captures verdict, increments iteration, loops back to implementer.
3. LaunchSpec for iteration N+1's implementer includes `claude_session_id = <prior-session-id>`.
4. Implementer subprocess receives this and invokes `claude --resume <prior-session-id>` to reconnect.

**Cite:** `internal/daemon/reviewloop.go:169–174`; `specs/handler-contract.md §4.2.HC-006` (claude_session_id field semantics); `handlercontract.ParseClaudeSessionID` (TODO stub not yet called).

---

## 9. Workflow mode routing (single vs. review-loop)

**How hk resolves the mode:**

Per `internal/daemon/moderesolve.go:47–92` (four-tier precedence walk):

1. **Tier 1:** Per-bead `workflow:<mode>` label (e.g., `workflow:review-loop`). If exactly one such label exists and its value is valid, use it.
2. **Tier 2:** Per-project config (no-op at MVH; always absent).
3. **Tier 3:** Daemon-level default from `daemon.Config.WorkflowModeDefault` (or `workLoopDeps.workflowModeDefault` after normalization).
4. **Tier 4:** Hard fallback to `WorkflowModeSingle`.

**Dispatch routing:**

Once mode is resolved (per `internal/daemon/workloop.go:427–431`), hk routes to:

- **`review-loop`**: `internal/daemon/reviewloop.go:117–324` — multi-iteration implementer → reviewer cycle.
- **`single`**: `internal/daemon/workloop.go:485–542` — one-shot implementer launch.

**For smoke-1 (this bead) and smoke-2 (the run):**

If the smoke bead has no `workflow:<mode>` label and the daemon default is `single`, hk will dispatch in **single mode** (one implementer, exit on result; no reviewer cycle).

**Cite:** `internal/daemon/moderesolve.go:34–92`; `internal/daemon/workloop.go:427–431`, `465–483` (mode dispatch); `internal/core/workflowmode.go` (mode constants).

---

## 10. Failure and exit-status expectations

**Daemon watcher error detection:**

Per `internal/daemon/workloop.go:520–541` (single-mode completion logic):

```go
watcherErr := watcher.Err()
watcherFailed := watcherErr != nil && !errors.Is(watcherErr, handlercontract.ErrCanceled)

if outcome.ExitCode == 0 && !watcherFailed {
    // Success: close bead
} else {
    // Failure: reopen bead
}
```

The daemon considers the run failed if:
1. **Subprocess exit code is non-zero**, OR
2. **Watcher reported an error** (other than context cancellation).

**Watcher error sources:**

`internal/handler/adapter_claudecode.go:88–136` declares the adapter callbacks:
- `DetectReady(event)` → true iff `event.Type == "agent_ready"`.
- `DetectRateLimit(event)` → (true, duration) iff `event.Type == "agent_rate_limited"`.
- `CleanExitSequence(ctx, session)` → sends `/exit` line on stdin for graceful termination.

**What makes daemon treat run as FAILED:**

Per `internal/daemon/workloop.go:531–541`:
- Exit code non-zero.
- Watcher error (malformed NDJSON, line too long per HC-007a, missing agent_ready, etc.).
- Verdict file absent or malformed (review-loop only).

**What makes daemon REOPEN the bead:**

On any failure (exit non-zero or watcher error), daemon calls `ReopenBead` to return the bead to the ready queue for retry.

**Cite:** `internal/daemon/workloop.go:509–542`; `internal/handler/adapter_claudecode.go:88–145`.

---

## 11. Key fragile points (predicted failure surfaces)

### A. **Verdict file path or schema mismatch**

**Risk:** Reviewers are expected to write `.harmonik/review.json` at the exact path, with exact schema v1 fields. If:
- Verdict is written to a different path (e.g., `.harmonik/review.iter-1.json` before iteration N+1), daemon will not find it.
- Schema version is 0 or 2 instead of 1, daemon rejects it as malformed.
- `flags` field is missing (even as empty array), daemon rejects it.
- `notes` is empty string, daemon rejects it as malformed.

**Predicted failure:** Reviewer exits with code 0, but daemon can't find/parse verdict → daemon emits `agent_failed` with "verdict absent" or `ErrMalformed`, reopens bead.

**Mitigation for smoke-2:** Verify the twin reviewer writes exactly:
```json
{
  "schema_version": 1,
  "verdict": "APPROVE",
  "flags": [],
  "notes": "Review complete."
}
```

### B. **Rate-limit detection pattern mismatch**

**Risk:** `ClaudeCodeAdapter.DetectRateLimit` (per `internal/handler/adapter_claudecode.go:107–123`) parses a `retry_after_seconds` field from an `agent_rate_limited` event payload. If the real Claude CLI emits a different field name or schema, the adapter will fail to extract the duration.

**Predicted failure:** Rate limit occurs, subprocess emits `agent_rate_limited` with unexpected payload → adapter returns (true, 0) (rate limited but no retry hint) → daemon applies default backoff (may not match Claude's stated retry window).

**Mitigation for smoke-2:** Capture and inspect the exact `agent_rate_limited` event payload when the real Claude CLI hits a rate limit.

### C. **LaunchSpec deserialization in handler subprocess**

**Risk:** Daemon passes LaunchSpec as JSON on stdin. The handler (Claude CLI or twin) MUST parse it correctly. If the handler is a stub/twin that doesn't fully deserialize LaunchSpec, or if the JSON encoding differs from what the real handler expects:
- Numeric fields like `timeout`, `provisioning_timeout`, `iteration_count` may be parsed as wrong types.
- Enum fields like `phase`, `workflow_mode`, `verdict` may not round-trip correctly.
- `claude_session_id` (if present) may be lost or misinterpreted.

**Predicted failure:** Handler crashes or exits non-zero immediately after LaunchSpec parse → daemon sees exit code non-zero → reopens bead.

**Mitigation for smoke-2:** Inspect the JSON that hk passes to the twin on stdin (enable `-v` or logging in the twin to print the received spec).

---

## Summary of test-readiness checklist

Before smoke-2 implementer runs hk live:

1. ✓ Understand that hk launches subprocess with `handler.LaunchSpec` on stdin (§2).
2. ✓ Understand CWD is a worktree path on a worktree branch, isolated from main (§3).
3. ✓ Understand that review-loop mode expects a `.harmonik/review.json` verdict file post-reviewer (§7).
4. ✓ Understand that `agent_ready` on stdout is the ready-state marker (§6).
5. ✓ Understand that workflow mode defaults to `single` unless a bead label overrides it (§9).
6. ✓ Understand that exit code non-zero or watcher error → daemon reopens the bead (§10).
7. ✓ Know the three most-likely real-Claude failure surfaces (§11 A–C) and how to capture them in smoke-2 logs.

---

