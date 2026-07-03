# Idea 3 — Crew on Pi (more model options)

**Scoping stub.** Date 2026-06-30. Builds directly on the existing Pi/OpenRouter brief
(`plans/2026-06-23-pi-openrouter-harness/README.md`) — this is the *crew* slice of that work.

---

## The vision (operator, distilled)

> "I want to figure out how to run the crew on Pi. This will give us more options on what models to
> run the crew with."

"Pi" = **`earendil-works/pi` (Mario Zechner's pi-coding-agent)** — a minimal open-source headless
coding agent with native OpenRouter support. **NOT a Raspberry Pi.** The point is provider-agnostic
crew: a crew driven by any model OpenRouter/Pi supports, not only Claude.

---

## What already exists (don't re-derive)

The Pi/OpenRouter brief already scoped this. Key facts to build on:

- **Two scopes already named.** (1) Pi as a per-bead *implementer harness* (lower risk; substrate
  mostly built — `AgentTypePi` is already declared at `internal/core/agenttype.go:19` but
  unimplemented; codex is the template). (2) Pi as a primary **crew runner** (bigger — crew launch
  hard-codes `claude`). **This idea is scope (2).**
- **Phased path already sketched:** Phase 0 (per-bead Pi harness, fenced to mechanical
  grep=0/failing-test→green beads behind the DOT test+review gate) → **Phase 1 (single mechanical
  crew pilot, target crew `gurney`, via an OpenRouter/Pi shim emulating Claude control flags)** →
  Phase 2 (generalize crew-launch through a provider abstraction). This idea = Phase 1 → 2.
- **The crew data plane is already model-agnostic.** Every crew tool is a shell CLI (`br`, `harmonik
  comms`, `queue submit/subscribe`) emitting JSON parsed with `jq` — **no Anthropic
  function-calling / MCP required.** So the *protocol* a Pi crew speaks already works; the gap is the
  *launch + control-flow shim*, not the comms surface.
- **Locked config principle:** provider + model + credentials are **operator config, never
  hardcoded defaults; fails loud if unset.** Consistent with the no-hardcoded-thresholds mandate.
- **Risks already named:** OpenRouter rate limits (~20 req/min, ~1000/day), no SLA → mandatory paid
  fallback; "trade tokens for time" only holds for *mechanical/deterministic* beads; stranded
  `in_progress` on free-model failure.

---

## The real gap (for the full sketch)

Crew launch hard-codes `claude --remote-control --session-id ...` with bracketed-paste seeding and
`--resume` (the Captain & Crew mechanism). A Pi crew needs a **shim** that emulates the Claude
control flags the crew lifecycle depends on:

- **keep-alive stream** (the long-lived interactive session crew run inside),
- **pane-wake** (how an idle crew is nudged awake on a new comms message — the known "idle crews
  don't wake on a bare comms send" gap),
- **session resume / context-clear** (keeper-driven `/clear` → `/session-resume` cycle).

This shim is exactly **open question #3** in the Pi brief, and it's where idea 4
(auto-comms-on-startup) intersects: a Pi crew that auto-subscribes on boot needs less of the
pane-wake machinery, which is *why idea 4 is sequenced right after idea 1 and before this.*

## Open questions (for the full sketch)

- What is the minimal control-flag surface the shim must emulate for `crew-launch`'s boot sequence
  (join comms, mirror assignee, subscribe inbox, drain queue) to work unmodified?
- Does the keeper's warn/act → `/clear` → resume cycle even apply to a Pi session, or does Pi need a
  different context-management story?
- Which mechanical bead classes are safe for a Pi crew given the rate-limit + no-SLA risk?
- How does this compose with idea 1 (a Pi crew running on a *remote node*) and idea 2 (a Pi crew in
  a *container*)?

**Status: stub — flesh out after idea 1 and idea 4. Cross-reference the Pi brief; don't duplicate
it.**
