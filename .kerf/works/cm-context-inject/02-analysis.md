# 02 — Analysis: current state of affected areas (Route 1)

Factual map of the code Route 1 touches. Line numbers verified against the tree
on `main` at analysis time; treat as anchors, re-confirm before editing.

## Area A — Tool-node dispatch & env (the ONE Go change)

**`internal/daemon/dot_cascade.go`**

- `driveDotWorkflow(...)` — the per-bead cascade driver. Signature (lines
  171–178) already carries everything Route 1 needs in scope:
  `beadID core.BeadID`, `beadRecord core.BeadRecord`, `beadTitle string`,
  `beadDescription string`.
- The node loop (`for visits ...` ~line 397) switches on `node.Type`. The shell
  tool-node branch is `case node.ToolCommand != "" && node.HandlerRef == "shell"`
  (~line 415).
- **Env construction (the seam):** ~lines 436–440:
  ```go
  gateEnv := deps.handlerEnv
  if parentSHA != "" {
      gateEnv = append(... deps.handlerEnv ..., "HK_GATE_BASE_SHA="+parentSHA)
  }
  toolOutcome, toolErr := dispatchDotToolNode(ctx, deps.bus, runID, runner, wtPath, node, gateEnv)
  ```
  `deps.handlerEnv` is **project-level only** (HARMONIK_PROJECT_HASH etc.,
  credential-safe per CI-001) — no bead data. This is **Gap 1**. The fix: layer
  per-bead vars (e.g. `HK_BEAD_ID`, `HK_BEAD_TITLE`, `HK_BEAD_DESCRIPTION`) from
  the in-scope `beadID`/`beadTitle`/`beadDescription` into `gateEnv` before the
  `dispatchDotToolNode` call. `HK_GATE_BASE_SHA` (line 438) is the exact
  precedent to mirror.
- `dispatchDotToolNode(ctx, bus, runID, runner, wtPath, node, env []string)` —
  ~line 1879. Local path (line 1907–1911): `exec.CommandContext(ctx,"/bin/sh","-c",node.ToolCommand)` with `cmd.Env = append(os.Environ(), env...)`, `cmd.Dir = wtPath` (worktree). Remote path (~1933): builds `/bin/sh -lc 'export K=V; … cd <wt> && <ToolCommand>'` — so layered env vars **propagate to remote workers too** (the `export` inlining at ~1859–1863). No change needed here beyond receiving the richer env.
- **No `tool_command` interpolation exists** — the command runs verbatim. So bead
  data must arrive as **env vars** the command references (`"$HK_BEAD_TITLE"`),
  not as `${...}` template substitution.
- Output today: exit 0 → `OutcomeStatusSuccess`; non-zero → FAIL. stdout tail →
  `Outcome.Notes` (opaque). Route 1 does NOT rely on structured output (that is
  Gap 2 / Route 2); the node writes a worktree file instead.

**Constraint:** the change must be additive — existing shell nodes (e.g. the
commit_gate) keep working unchanged; they simply gain extra env vars they ignore.

## Area B — DOT authoring (no engine change)

**`specs/examples/*.dot`** (e.g. `standard-bead.dot`) — the workflow graphs.

- Node attrs available: `type`, `handler_ref`, `agent_type`, `tool_command`,
  `timeout`, `prompt`, `role`, `model`, `effort`, `harness`, etc. (parser:
  `internal/workflow/dot/ast.go`; node-type enum: `internal/core/nodetype.go`).
- A cm step is a non-agentic shell node placed after `start`, before `implement`:
  ```dot
  load_context [
    type="non-agentic", handler_ref="shell",
    tool_command="mkdir -p .harmonik && cm context \"$HK_BEAD_TITLE\" --json --limit 5 > .harmonik/cm-lessons.md 2>/dev/null || true",
    timeout="20"
  ];
  start -> load_context;
  load_context -> implement;
  ```
  The `|| true` makes it best-effort (decision B): cm missing/erroring still
  yields exit 0 → SUCCESS → cascade proceeds.
- **Per-node `role`/`prompt`** is prepended into the agent's ExtraContext at
  `dot_cascade.go` ~1212–1223 (written to `.harmonik/agent-task.md` via
  `workspace.WriteAgentTaskVia`, `claudelaunchspec.go:329`). The `implement`
  node's `role` gains: "If `.harmonik/cm-lessons.md` exists, read it first for
  relevant prior lessons." No code change — pure authoring.
- This is opt-in: graphs without `load_context` are byte-identical to today
  (success criterion 3).

## Area C — worktree file convention

- The shell node runs with `cmd.Dir = wtPath` (the bead's worktree), the shared
  substrate the agent also runs in. Writing `.harmonik/cm-lessons.md` there is
  the hand-off channel. `.harmonik/` is already used in the worktree
  (agent-task.md lives there); the `mkdir -p` guards a fresh worktree.

## Area D — cm availability / PATH

- `dispatchDotToolNode` inherits `os.Environ()` (line 1911), so `cm` resolves
  only if the daemon's PATH includes its install dir (`~/.local/bin`). Document
  as a deployment note; `|| true` makes a missing `cm` non-fatal regardless
  (success criterion 4).

## Area E — testing conventions

- Tool-node behavior is exercised in `dot_cascade` tests (look for
  `*_test.go` in `internal/daemon` covering `dispatchDotToolNode` / shell nodes;
  the `tool-node.dot` example is a fixture). Pattern: drive a graph with a shell
  node and assert Outcome.Status + side effects.
- New tests Route 1 needs:
  1. **env reaches the command:** a `tool_command` that writes `$HK_BEAD_ID`
     (and title) to a file; assert the file contains the bead's values.
  2. **best-effort:** a `tool_command` that exits non-zero under `|| true`
     (or a missing-binary command) still yields SUCCESS and the cascade
     advances; worker launch still occurs.
  3. **off-path unchanged:** a graph without the cm node produces an identical
     agent-task.md seed (golden/byte compare) — guards criterion 3.
  4. **(example/e2e):** an opt-in example workflow that includes `load_context`
     produces `.harmonik/cm-lessons.md` in the worktree (cm stubbed/faked on
     PATH).

## Constraints to preserve

- Daemon is live → implement in an isolated worktree; do not edit `main`'s tree.
- Additive env only; existing shell nodes (commit_gate) must be unaffected.
- Bead title/description are not secrets, but env values must be referenced
  quoted in `tool_command` to avoid word-splitting; no secret keys added.
- Remote-worker parity: layered env must survive the `/bin/sh -lc 'export …'`
  remote path (it does, by construction).

## Recent git activity (context)

- Active work around the Pi harness launch spec and gate base-SHA
  (`HK_GATE_BASE_SHA`, hk-t1t00) recently touched this exact env-construction
  region — confirms the `gateEnv` seam is the live, correct place and that the
  HK_-prefixed env convention is current.
