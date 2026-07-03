# 01 — Problem Space: cm-context-inject (Route 1)

## Goal / motivation

Dispatched workers start every bead from a cold context. They re-derive
project conventions and re-hit traps that earlier sessions already learned and
recorded. We now run `cm` (the cass-memory system) which holds a
confidence-ranked playbook of those lessons (rules + anti-patterns), retrievable
by relevance with `cm context "<task>" --json`.

The goal is to surface the *relevant* prior lessons to a worker **at dispatch
time**, so it avoids repeating known mistakes — without coupling the daemon core
to `cm` and without forcing the behavior on deployments that don't want it.

## Who benefits

Every daemon-dispatched worker that runs under a workflow whose DOT opts in.
Because all harnesses (claude / codex / pi) read the same `.harmonik/agent-task.md`
seed and run inside the worktree, one DOT-level mechanism reaches all three.

## Key architectural decision (locked this session)

This must live in the **DOT workflow graph, not the daemon dispatch code.**
Baking `cm` into `internal/daemon/...` would force the coupling on every harmonik
deployment (gated only by global config) and tie the daemon core to an external,
optional tool. Expressing it as a node in the DOT makes it **opt-in per process**:
only workflows whose graph includes the cm step get it; the daemon stays agnostic.
This follows the locked principle "composable node graphs are the scaling
mechanism; don't invent a feature primitive."

Answer to "do all users need it?": **No.** With the DOT approach it is per-process
opt-in, chosen by whoever authors/selects the workflow.

## The two engine gaps this surfaced

Investigation of the workflow engine (see `internal/daemon/dot_cascade.go`,
`internal/workflow/dot/`, `internal/core/{nodetype,outcome}.go`) found:

- **Gap 1 — tool nodes get no per-bead data.** A non-agentic shell node
  (`type="non-agentic", handler_ref="shell", tool_command="…"`,
  `dispatchDotToolNode` ~`dot_cascade.go:1828-2012`) is invoked with `gateEnv`
  = `deps.handlerEnv` (+ optional `HK_GATE_BASE_SHA`), which is project-level
  only — no bead id / title / description. `tool_command` is not interpolated.
  So a shell node cannot currently feed `cm` the bead it is running for.
- **Gap 2 — node output does not reach a downstream agent's context.** The spec
  defines a `context_updates` flow (EM-041a; `Outcome.ContextUpdates`;
  `workflow.ValidateAndApplyContextUpdates`; graph `context_keys` allowlist) but
  it is **not wired** into the cascade loop, and per-node agent prompt context is
  static (set once at `driveDotWorkflow`, reused for all nodes).

## Scope — Route 1 (this work)

Route 1 deliberately sidesteps Gap 2 by passing context through the **shared
worktree filesystem** instead of `run.Context`:

1. **Close Gap 1 (minimally):** give non-agentic shell/tool nodes access to the
   current bead's identity (at least bead id; title/description if cheap) so a
   `tool_command` can query `cm`. Mechanism (env vars vs interpolation) is a
   design-pass decision; the requirement is "a shell node can reference the bead
   it runs for."
2. **A cm step expressed in DOT:** a shell node near workflow start runs
   `cm context "<bead text>" --json` (formatted to markdown) and writes the
   result to a known worktree file, e.g. `.harmonik/cm-lessons.md`.
3. **The implementer node reads it:** the agentic node's `role`/`prompt`
   instructs the agent to read `.harmonik/cm-lessons.md` first if present.

The daemon learns nothing about `cm`; it only learns "shell nodes can see the
bead." The cm wiring is entirely in a DOT file + the worktree convention.

## Out of scope

- **Route 2** — the structured, reusable node→node context-passing primitive
  (wiring EM-041a `context_updates` end-to-end into downstream prompts). Captured
  separately as a standing design objective; Route 1 is the interim.
- **Crew launch injection** — crews don't always carry a bead.
- **cm write-back / feedback** — feeding run outcomes back to `cm` (`cm outcome`)
  is a later, separate piece.
- **Making `cm` a hard dependency** — it stays optional.

## Constraints

- The harmonik daemon is live during development → implement in an isolated git
  worktree; never edit `main`'s working tree (worktree-escape detector / checkout
  revert).
- **No secrets** passed to `cm` — bead title/description/id only.
- **Off by default:** workflows that don't include the cm node behave exactly as
  today (byte-identical seed).
- **Bounded:** the cm call must have a timeout and a bounded output (token
  budget / `--limit`) so it cannot slow or wedge dispatch.
- **Best-effort runtime (decision B):** if `cm` is missing, times out, errors, or
  returns nothing, the workflow proceeds and the worker launches without the
  lessons file. A memory-enrichment hiccup must never fail a real bead. (This is
  a deliberate, scoped exception to the project's fail-loud mandate; it applies
  only to the runtime enrichment, not to any required config.)

## Success criteria (concrete, verifiable)

1. A non-agentic shell node in a workflow can reference the current bead (e.g. its
   id) and successfully invoke `cm context` for that bead.
2. When a workflow includes the cm node, the dispatched worker's worktree contains
   `.harmonik/cm-lessons.md` populated from `cm context`, and the implementer
   node's seed directs the agent to read it.
3. When a workflow does NOT include the cm node, dispatch output is byte-identical
   to today.
4. When `cm` is absent / errors / times out / returns empty, dispatch still
   succeeds and the worker launches (no lessons file, or an empty/omitted one).
5. The behavior is identical across claude / codex / pi harnesses (all read the
   same worktree + agent-task.md seed).

## Open question carried into Analyze

- Exactly how to expose bead identity to shell nodes (per-bead env vars layered
  into `gateEnv` vs `tool_command` interpolation) — Analyze/design-pass decision.
- Whether the shell node calls `cm` directly with title/description, or calls
  `br show <id>` to assemble the query text inside the node — depends on what Gap-1
  exposes.
