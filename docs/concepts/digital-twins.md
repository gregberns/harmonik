---
title: Digital Twins for Agent Processes
status: seed
type: concept
source: harmonik design conversation, 2026-04-19
related: [docs/goals/end-to-end-testability.md, docs/subsystems/scenario-harness.md, docs/subsystems/agent-runner.md]
created: 2026-04-19
updated: 2026-04-19
---

# Digital Twins for Agent Processes

## What It Is

A digital twin is a separate, independently-built binary that mimics a real agent process at the OS level. It is launched the same way as the real agent (same command, same args, same environment, same working directory), produces output in the same format on the same channels (stdout, stderr, log files), responds to the same lifecycle events, and exits with the same kinds of codes -- but it does not call any model. Its output is scripted by a scenario, not generated.

The pattern: for every agent runner type harmonik supports (Claude Code, Pi, future additions), there is a corresponding twin binary (`claude-twin`, `pi-twin`, ...). The agent runner does not know or care which it is launching. Selection happens upstream, in workflow or policy configuration.

## Why Twin Binaries, Not In-Process Mocks

A twin could be implemented as an in-process mock: a Go interface with a real-agent implementation and a mock-agent implementation, swapped via dependency injection. This was considered and rejected. Reasons:

1. **Process-boundary fidelity.** Real agents are external processes. They have stdout/stderr buffering, signal handling, exit codes, file descriptor inheritance, environment-variable behavior, and working-directory semantics that a Go-interface mock cannot replicate without extensive wrapping. The wrapping itself becomes untested code.
2. **No mode-switching in the runner.** With in-process mocks, the agent runner contains "is this a test?" branches. With twin binaries, the runner contains zero test-aware code -- it launches whatever binary the configuration names.
3. **Honest interface.** The handler's contract is "launch this binary, parse its output, react to its lifecycle events." If the twin satisfies that contract, the real agent must satisfy it too. The interface is forced to be process-shaped, not Go-shaped.
4. **Polyglot freedom.** A twin can be written in any language. The Pi twin could be Python; the Claude twin could be a shell script. They are not constrained to harmonik's implementation language.
5. **Ad-hoc debuggability.** A developer can run `claude-twin --scenario foo.yaml` directly from a shell to inspect its behavior. No harness required.

## Selection Mechanism

Workflow or policy configuration names which binary to use for each agent role. Concrete shape (subject to design):

```yaml
agent_roles:
  builder:
    type: claude
    binary: claude          # or "claude-twin" in test scenarios
  reviewer:
    type: pi
    binary: pi --model glm-4.6
```

The agent runner reads the binary string and execs it. Real or twin, same code path.

This selection sits in workflow definitions (default: real binaries) and is overridden in scenario test runs (override to twin binaries). The override mechanism is part of the scenario harness, not the agent runner.

## What a Twin Must Reproduce

For a twin to be a faithful drop-in:

| Aspect | Requirement |
|---|---|
| Launch | Same command form, same environment, same args, same working directory |
| Stdout/stderr | Same line format, same timing characteristics (configurable per scenario) |
| Log files | Same filesystem location, same format (e.g., `~/.claude/projects/<uuid>.jsonl` for Claude) |
| Lifecycle signals | Honors the same termination signals; emits the same ready-state markers |
| Exit codes | Uses the same code conventions for success, failure, rate-limit, timeout |
| Side effects | Performs whatever filesystem changes the scripted scenario specifies (e.g., "write file X with contents Y") |

What a twin does *not* do: call any model, consume tokens, contact any external service.

## Scenario Scripting

A twin reads a scenario script that specifies its behavior. Minimum capabilities:

- "Emit these output lines, in this order, with this timing"
- "Make these tool calls with these arguments" (if the agent supports tools)
- "Modify these files in the workspace"
- "Exit with this code"
- "Hang for N seconds before doing the above" (for timeout testing)
- "Emit a rate-limit error after N requests" (for rate-limit handling)

Scenarios are version-controlled alongside the harmonik repo. The scenario harness composes them into full workflow tests.

## Keeping Twins Honest

The risk: twin behavior drifts from real agent behavior over time. A real agent's output format changes; the twin keeps producing the old format; tests pass but production breaks.

Mitigations (open questions, not yet decided):
- Periodic conformance tests: run a small set of real agents against the same scenario inputs and diff their output against twin output.
- Schema-shared output: where possible, both real and twin emit output that conforms to a versioned schema; schema changes are caught at compile time.
- Dependency on agent-vendor stability: pinned agent versions per harmonik release, twin updated in lockstep with vendor changes.

## Relevance to Harmonik

Digital twins are the load-bearing mechanism for [G07: End-to-End Testability](../goals/end-to-end-testability.md). Without them:

- Every full-workflow test costs tokens and time.
- Edge cases (rate limits, hangs, malformed output) are not reliably reproducible.
- Self-build cycles (G06) have no fast regression net to verify they did not break harmonik's ability to build harmonik.
- Architecture decisions creep toward "this is hard to test, but it works in production" -- the path that produces unmaintainable systems at scale.

With them, the orchestration substrate (S01-S06, S08, S09) can be exercised exhaustively and cheaply, and the only remaining unknown in production is the model output itself.

## Cross-References

- [G07: End-to-End Testability](../goals/end-to-end-testability.md) -- The goal this concept enables
- [S04: Agent Runner](../subsystems/agent-runner.md) -- Handlers must support twin binaries as drop-in replacements
- [S07: Scenario Harness](../subsystems/scenario-harness.md) -- The subsystem that runs scenarios against twins
- [Zero Framework Cognition](zero-framework-cognition.md) -- Twins reinforce the mechanism/cognition split: the framework launches processes, the model (or twin) decides
