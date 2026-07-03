# Dimension 3 — Where the harness-abstraction seam belongs (design research)

> Synthesizes the code map (dim 1) into a concrete seam proposal. The change-spec (`05-specs/`)
> formalizes it; this captures the reasoning + options + the key spawn-per-turn de-risking.

## The seam, in one sentence

Introduce a `Harness` interface that captures **only** the harness-VARYING surface, register a
`CodexAdapter` under `core.AgentTypeCodex`, and route the existing `deps.launchSpecBuilder` function
field + `AdapterRegistry.ForAgent(...)` off a resolved harness. Everything below the seam (tmux
substrate, worktree mgmt, git commit-detection, merge-one-at-a-time, DOT cascade, review-loop control
flow, watchdog *timers*) stays shared and unchanged.

## Proposed `Harness` interface (Go sketch — formalized in change-spec)

```go
type Harness interface {
    // Binary + argv + env + cwd for one spawn. env MUST apply the harness's
    // credential strip+empty-override (claude: ANTHROPIC_*; codex: OPENAI_API_KEY/CODEX_API_KEY).
    LaunchSpec(rc RunCtx) (LaunchSpec, error)

    // Deliver the first-turn task to a freshly-spawned session.
    // claude: splash-dismiss + bracketed-paste of agent-task.md.
    // codex:  no-op (task delivered via argv/stdin in LaunchSpec) OR write the prompt file.
    Seed(sess Session, rc RunCtx) error

    // Deliver review feedback for iteration >=2 to a (re-spawned) session.
    // claude: paste combined task+feedback into the claude --resume REPL.
    // codex:  the LaunchSpec for iter>=2 already used `codex exec resume <thread_id> "<feedback>"`,
    //         so Retask is a no-op OR writes the feedback file.
    Retask(sess Session, feedback string, rc RunCtx) error

    // End the session so the shared loop's sess.Wait returns.
    // claude: /quit + 60s grace + Kill. codex: no-op (exec self-terminates on turn completion).
    Teardown(sess Session) error

    // Map harness stdout/events to harmonik's agent_ready / heartbeat signals.
    // claude: NDJSON agent_ready. codex: thread.started / item.updated JSONL events.
    DetectReady(ev Event) bool

    // Session-id policy: claude mints a UUIDv7 up front; codex captures thread_id post-launch.
    SessionIDPolicy() SessionIDPolicy // {Minted | Captured}
}
```

The interface is deliberately the **same five operations** the claude path already performs (Part E
table in `current-harness/findings.md`); we are *naming an existing implicit contract*, not adding
capability.

## KEY DE-RISK: spawn-per-turn codex is structurally identical to claude's iter≥2

The decompose review worried that codex `exec` exiting per turn breaks harmonik's review-loop (which
"re-tasks a live claude session"). **It does not**, because claude's iteration ≥2 is *already a fresh
process*:
- Research (`current-harness` §B.2) shows iter≥2 is `pasteInjectImplementerResume` injecting into a
  **fresh `claude --resume <uuid>` REPL** — a NEW process spawned into a NEW pane, with a *bounded
  Enter retry because the fresh resume REPL is not yet input-ready*. The implementer session from
  iter1 was already `/quit`'d on commit.
- So claude's model is: **spawn-with-resume-id-then-deliver-feedback**. codex's model is: **spawn
  `codex exec resume <thread_id>` with feedback-as-arg**. These are the SAME shape; only (a) feedback
  delivery (paste vs arg) and (b) session id (minted vs captured) differ — exactly what `Seed`/
  `Retask`/`SessionIDPolicy` abstract.

**No shared infrastructure assumes a long-lived CROSS-iteration process** (even claude re-spawns
`claude --resume` per iteration). The watchdog timers (`launchHeartbeatTimeout` 180s,
`heartbeatStalenessThreshold` 8m, `commitPollTimeout` 30m, `commitHardCeiling` 90m) and `sess.Wait`
operate within ONE spawn.

**CORRECTION (decompose-review B1): the heartbeat EMITTER is per-harness, not git-derived.**
`agent_heartbeat` is produced by a timer loop *inside the claude handler* —`RunHeartbeatLoop`
(CHB-019, `claudehandler_chb006_024.go:588-617`), every 300s "while Claude alive." The staleness
watchdogs CONSUME it and will kill a run that emits no heartbeat. So a `codex exec` running silently
for >8 min would be staleness-killed mid-run. This is the one place the "shared = harness-agnostic"
framing breaks. **Two clean fixes, the interface must support both:**
1. Add `Completion() CompletionMode {EventStreamThenQuit | ProcessExit}` to `Harness`. For
   `ProcessExit` (codex), the shared loop **bypasses** the heartbeat-staleness kill path entirely
   and relies on `sess.Wait` (process exit) + the absolute `commitHardCeiling` (90m). This is the
   simpler fix and matches codex's exit-on-completion shape.
2. OR the codex adapter runs its own heartbeat emitter mapped from codex `item.*`/`turn.*` JSONL
   progress events (keeps the staleness watchdog meaningful for codex too).

The change-spec picks fix #1 as primary (simplest, fewest moving parts), with the adapter still
mapping codex JSONL → a coarse heartbeat as a courtesy signal for operator visibility.

Otherwise codex's per-process exit makes `sess.Wait` *cleaner* than claude (process exit vs the
`/quit`→grace→Kill chain). The loop holds no stale pane handle across iterations (claude re-spawns
too), so spawn-per-turn is safe at that boundary.

## Files that change (named, smallest seam)

| Change | File(s) |
|---|---|
| Define `Harness` + `SessionIDPolicy` | new file under `internal/handlercontract/` (next to `adapterregistry_hc012.go`) |
| Add `core.AgentTypeCodex` | `internal/core/` (agent-type enum) |
| Extract claude impl behind `Harness` (no behavior change) | `internal/daemon/claudelaunchspec.go`, `internal/handler/claudehandler_chb006_024.go` |
| New `CodexAdapter` + codex launch-spec builder | new `internal/daemon/codexlaunchspec.go` + `internal/handlercontract/adapter_codex.go` |
| Route cascade/review-loop off resolved harness | `internal/daemon/dot_cascade.go` (the `launchSpecBuilder` seam `:521-523`), `reviewloop.go` |
| Harness resolution (bead/queue/node/global) | `internal/daemon/` (new resolver) + `internal/core/node.go` + `internal/workflowvalidator/dotparser.go` (the `harness` attr) |
| Config default-harness | `internal/daemon/daemon.go` (`Config`) |

**Unchanged (shared):** `tmuxsubstrate.go`, `osadapter.go`, `substrate.go`, `handler.go`,
`internal/workspace/createworktree.go`, the commit-detection/merge logic in `workloop.go`.

## Options & tradeoffs for the seam altitude

| Option | Pros | Cons | Verdict |
|---|---|---|---|
| **A. Harness interface + AgentType registry (chosen)** | Reuses the existing adapter-registry + `launchSpecBuilder` seam; smallest blast radius; review-loop inherits selection for free | Requires a careful no-behavior-change extraction of the claude path | **Chosen** — matches the "smallest seam, no new layer" constraint |
| B. Fork the whole dispatch path per harness (if-claude/if-codex branches in workloop) | Quick to prototype | Duplicates hot-path control flow; violates "no speculative layers"; review-loop must branch too | Rejected — duplication, drift risk |
| C. Generalize to a full plugin system (N harnesses, dynamic registration) | Future-proof | Speculative abstraction the user didn't ask for; over-builds for 2 harnesses | Rejected — out of scope (non-goal) |

## Risks / unknowns (carried to change-spec)
- **U1.** Whether running `codex exec` inside a tmux window (for inspectability parity) vs a plain
  subprocess is better — codex needs no TUI, but tmux keeps the operator-inspectability the project
  values. Lean: keep it in a tmux window (reuse the substrate verbatim), since the substrate is
  harness-blind and inspectability is a project value.
- **U2.** Mapping codex JSONL events → harmonik's heartbeat/ready event model needs a small
  translation layer in the adapter; low risk but must exist so the staleness watchdog has a signal.
- **U3.** Reviewer-harness independence (R5.2) is a product choice — default same-as-implementer.
