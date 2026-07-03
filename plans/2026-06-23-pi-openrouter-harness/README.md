# Pi + OpenRouter — New Token Sources via the Pi Harness — Objectives

> **Status:** objectives + scoping brief (admiral, 2026-06-23, operator-paired; synthesized from 4
> parallel research agents). NOT a design spec.
> **Next:** captain dispatches a crew to produce the thorough design/spec from this brief.
> **Review gate:** the produced design MUST be reviewed by reviewers **with critics** before build.
> **Author:** admiral. **Owner of build:** captain → crew.

## Why

Harmonik scales by adding token sources. Claude subscriptions are the best quality-per-token but
finite; codex (ChatGPT-billed) added a second source. **Pi is a universal model gateway** — it can
drive *every model/provider Pi supports* (OpenRouter, direct APIs for Anthropic/OpenAI/Google, etc.).
The goal is NOT to box us into free models — it is to **open harmonik to any model**, routing each
piece of work to whatever provider/model fits. Free OpenRouter is one option among many (free tokens,
with caveats below); the broader win is provider-agnosticism + the ability to **trade tokens for time**
(weaker/cheaper implementers wrapped in heavier test + review loops) on capacity *off* the Anthropic
budget.

## What Pi is (confirmed)

**Pi = `earendil-works/pi`** (Mario Zechner's pi-coding-agent): a minimal open-source headless coding
agent — built-in read/write/edit/bash/grep/find/ls tools, `--mode json` (NDJSON event stream) or
`--mode rpc`, native OpenRouter provider (`--provider`, `--model provider/id`), positional/stdin task
input, auto prompt-caching per provider. It edits files + runs shell in a worktree exactly the way the
daemon expects — a clean fit for the existing harness machinery.

## The two scopes (operator-named)

1. **Pi as a per-bead implementer harness** — run a single bead's implementation on Pi/OpenRouter,
   selected like codex. **Lower risk, mostly built substrate.**
2. **Pi as a primary crew runner** — run a whole crew *orchestrator* on a cheap model. **Bigger change;
   the crew launch path is hard-coded to Claude today.**

## Phased path (de-risking order)

### Phase 0 — Pi per-bead implementer harness  *(do first; lowest blast radius)*
The extension point already exists: the `Harness` interface (`internal/handlercontract/harness.go`),
the 4-tier resolver (`internal/daemon/harnessresolve.go`), the registry, and **`AgentTypePi` is already
declared** (`internal/core/agenttype.go:19`) but unimplemented. Codex is the working template.

**Recommended shape:** make Pi a **ProcessExit / session-captured** harness like codex — that makes the
agent-ready gate (HC-056), paste-inject gating, and the commit-fallback DOT+workloop wiring kick in
**automatically** (those gates are harness-blind, keyed on `Completion()`).

**What's involved (mirrors codex):**
- *New:* `piharness.go` (8 interface methods), `pilaunchspec.go` (argv/env/cwd, OpenRouter key, resume
  argv), `picommit.go` (Refs-trailer commit fallback, **routed through the run's runner** so remote works),
  `adapter_pi.go`, + unit tests.
- *Edit (2):* register Pi in `internal/daemon/harnessregistry.go`; register the Pi adapter at startup.
- *Pi-specific decisions:* credential env to strip/inject (`OPENROUTER_API_KEY`), argv shell-quoting
  (test against the remote SSH substrate — this bit codex), session-id capture from Pi's NDJSON,
  billing/auth model (a `pibillingguard.go` analog if needed). `DetectReady` MUST NOT fire on
  `launch_initiated` (HC-041 hard rule).
- *Free, harness-agnostic (daemon already provides):* worktree, agent-task.md write, agent-ready gate,
  paste-inject, commit detection, merge, review-loop.
- **Selection:** `harness:pi` bead label (tier-1) or a queue default (tier-2) → **fence Pi to a lane of
  mechanical, deterministically-checkable beads** (grep=0 / failing-test→green), behind the existing
  DOT test+review gate.
- **Model/provider/key are OPERATOR CONFIG — no hardcoded default model in code** (see Configuration
  principle below). The harness passes through Pi's full provider/model surface; the operator picks per
  lane/bead. Free OpenRouter (e.g. Qwen3 Coder `:free`) is one selectable option, not the default.

### Phase 1 — single mechanical crew on Pi (pilot)  *(after Phase 0 proves the harness)*
Good news from the scope: the **crew data plane is fully model-agnostic** — every crew tool is a shell
CLI (`br`, `harmonik comms`, `queue submit/subscribe`) emitting JSON parsed with `jq`. **No Anthropic
function-calling / MCP required**, and standing context is small (~12–15K). So a competent cheap model
can plausibly run a *narrow, well-specced* crew.
- **Route:** point the crew's `claudeBinary` at an OpenRouter/Pi **shim** that accepts the Claude-style
  control flags (`--remote-control`/`--session-id`/`--model`) and runs the read-skills→loop behavior.
- **Pilot target:** **gurney** (cmd/harmonik tools lane, 2 well-specced beads) — strongest candidate.
- **Context management:** the keeper context-gauge is Claude-Code-native and already OFF for crews → use
  the existing **stop / restart-with-fresh-mission** loop (durable state lives in the mission file +
  beads `--assignee` mirror), not the gauge.

### Phase 2 — generalize the crew launch path  *(the bigger change)*
Today crew launch hard-codes `claude` (`internal/daemon/crewlaunchspec.go:100`, `cmd/harmonik/captain.go:203`)
and does NOT go through `resolveHarness`. Generalize crew launch through a provider abstraction (the way
the per-bead path already is), then widen Pi crews to other mechanical lanes.

## Candidate vs no-go (crew runner)
- **Cheap-model candidates** (mechanical, normative spec, "don't redesign"): gurney (pilot), leto, jamis,
  chani (SSH-triage caveat), the `_TEMPLATE` shape.
- **Must stay Claude** (judgment/design/oversight): paul (kerf design), stilgar (architecture), admiral
  (oversight), irulan (root-cause) — and the **captain unconditionally**.

## Configuration principle (operator directive)
- **Pi is a universal model gateway — open to every model/provider Pi supports.** Do NOT default to any
  one model or provider.
- **Provider + model + credentials are OPERATOR CONFIG, never hardcoded defaults in code** (consistent
  with the no-hardcoded-thresholds mandate + the R1 de-hardcode project). The harness requires explicit
  config and **fails loud** if a provider/model/key is unset — no silent default model. The operator
  configures Pi before use.
- **Data-training (free OpenRouter):** fine for this project (open-source repo); it's a per-provider
  note the config should surface, NOT a blocker.
- **Spend note (informational):** free OpenRouter needs a one-time $10 to lift 50/day → 1000/day; paid
  providers and direct APIs have their own billing. None of this is a code default — it's config.

## Risks / catches (design must plan around)
- **Rate limits:** 20 req/min hard, ~1000 req/day (post-$10). A bead burns many tool-turns → caps
  concurrency to a modest pilot, not a fleet.
- **No SLA:** free routing throttles/pauses/404s at peak → mandatory paid fallback path.
- **Caching is moot on free tier** (no spend to cut) — do NOT justify the plan on caching savings.
- **"Trade tokens for time" only holds for mechanical/deterministic beads.** Architecture/ambiguous
  specs make weak models thrash the review loop — net-negative. Hard scope boundary.
- **Stranded-in_progress** on free-model failure (no reset verb) — bound with re-submit-once → escalate.

## Open questions for the design crew
1. Pi completion mode — confirm ProcessExit/Captured (recommended) vs EventStreamThenQuit.
2. Pi NDJSON → does it carry a machine-readable session id + a clean done signal? (exit codes undocumented.)
3. The OpenRouter/Pi crew **shim** contract for Phase 1 (which Claude control flags it must emulate:
   keep-alive stream + pane-wake).
4. Billing/auth guard for Pi (analog to the codex ChatGPT guard) — what does fail-closed look like.
5. **Config surface for provider/model/key selection** — per-bead label? per-queue/lane default?
   global? — and how it FAILS LOUD when unset (no silent default model). Must expose Pi's full
   provider/model range, not a curated subset.

## Constraints
- Spec-first (kerf work). Reviewers **with critics** review the design before build.
- Planned/built **by a crew**, not the captain inline.
- Phase 0 → Phase 1 → Phase 2 ordering; each phase gated on the prior proving out.
