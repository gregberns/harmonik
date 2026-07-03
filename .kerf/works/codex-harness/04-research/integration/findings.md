# Dimension 5 — Integration design (how a run selects its harness)

> How harness selection plumbs through harmonik, defaulting to claude, N-1 safe. Formalized in
> `06-integration.md`; this captures the options + reasoning.

## Resolution model (chosen): layered precedence, default claude

```
per-bead override  >  per-queue default  >  global default (claude)
```

A single pure resolver `ResolveHarness(bead, queue, node, globalCfg) → core.AgentType`:
1. **Per-bead override** — a bead label `harness:codex` (uses the existing `codename:`-style label
   convention; bare functional label `harness:<name>`). Highest precedence: an operator marks a
   specific bead to run on codex.
2. **Per-queue default** — a queue-level field (the named-queues work, `project_named_queues_work`,
   already gives per-queue config) defaulting the harness for everything in that queue. Use case:
   "the experimental queue runs codex; main runs claude."
3. **DOT node attribute** — `harness=codex` on an agentic node (`dotparser.go` generic attr-map).
   This selects the harness for that node within a workflow (e.g. implement-on-codex,
   review-on-claude). Resolves *within* a run once the run's base harness is chosen.
4. **Global default** — `Config.DefaultHarness` (new), default `claude`. Absent everything else →
   claude. **This is the N-1 back-compat anchor.**

Absent all four → `claude`, byte-identical to today.

## Why this layering (options & tradeoffs)

| Option | Pros | Cons | Verdict |
|---|---|---|---|
| **A. Layered bead>queue>node>global (chosen)** | Operator can target one bead, one queue, or flip the global default; matches existing label + named-queue + DOT-attr surfaces; nothing new invented | Four tiers to document | **Chosen** — each tier reuses an existing surface; no new primitive |
| B. Per-bead only | Simple | No "run this whole experimental queue on codex" without labeling every bead; no global flip | Rejected — too granular for fleet-level choices |
| C. Global only | Trivial | Can't mix harnesses across beads/queues in one daemon — defeats "selectable per run" | Rejected — fails G5 |

## Where each tier physically lives

| Tier | Surface | File(s) |
|---|---|---|
| per-bead | bead label `harness:codex` | resolver reads `br show` labels; no schema change |
| per-queue | queue config field | `internal/queue/types.go` (`QueueSubmitRequest`/`Group`), the named-queues config |
| per-node | DOT attr `harness` | `internal/workflowvalidator/dotparser.go`, `internal/core/node.go` |
| global | `Config.DefaultHarness` | `internal/daemon/daemon.go` |

## DOT + reviewer references

- The cascade (`dot_cascade.go:499-525`) already resolves the launch-spec builder via
  `deps.launchSpecBuilder`; it now first calls `ResolveHarness(...)` and selects the matching
  builder + `AdapterRegistry.ForAgent(agentType)`.
- **Reviewer harness (R5.2):** default = **same harness as the implementer** (a codex run is
  reviewed by codex). Optional independent override `reviewer_harness=claude` on the review node or
  in config, so an operator can pin an always-claude reviewer (useful while codex's structured-verdict
  reliability is unproven — see C6 R6.5 MUST-TEST). Spec the default; gate the override behind a
  cheap flag.

## Migration / back-compat (N-1 safety)

- **Default unchanged:** `Config.DefaultHarness` defaults `claude`; `Config.HandlerBinary` keeps its
  `claude` default and claude semantics (R4.4). No existing bead/queue/workflow carries a harness
  selector, so all resolve to claude — **byte-identical launch behavior** (regression test R6.1).
- **Additive only:** the `harness` DOT attr, the bead label, the queue field, and `DefaultHarness`
  are all *additive*; absent → claude. No field renamed, no published contract changed → N-1 safe.
- **Rollout:** ship the seam + claude-as-`ClaudeHarness` first (no behavior change), then the codex
  adapter behind an off-by-default selection, then enable codex on an opt-in queue/bead. The daemon
  can refuse codex selection if `codex login status` is not a ChatGPT plan (C3 pre-flight),
  fail-closed.

## Operator surface (summary for docs, R6.3)

```bash
# one-off: run a single bead on codex
br label add hk-xxxx harness:codex

# a whole queue on codex (named-queues)
harmonik queue submit codex-queue.json     # group/queue carries harness:codex default

# flip the global default (rare)
harmonik --project ... --default-harness codex   # or branching.yaml-style config
```
Pre-req once per box: `codex login` (ChatGPT subscription) + verify `codex login status` shows a
plan, + pin `forced_login_method = "chatgpt"` in `$CODEX_HOME/config.toml`.

## Risks / unknowns
- **U4.** Per-queue harness field interacts with the named-queues work (still in-flight) — coordinate
  so the field name is consistent; if named-queues hasn't landed the queue field, ship bead+node+
  global first and add the queue tier when named-queues lands (no blocker).
- **U5.** Mixing harnesses within one workflow (implement-codex / review-claude) is supported by the
  node-attr tier but adds test surface; default keeps reviewer = implementer harness to limit it.
