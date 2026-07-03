# session-keeper — Problem Space

**Bead:** hk-ekap1 (P2, feature) · **Codename:** session-keeper · **Jig:** plan
**Parent context:** spin-off of the `flywheel` kerf work (long-running agent loop with managed context). Sibling of `agent-comms` (hk-uxm0j) — both are "a supervisor coordinating long-running orchestrator agents."

## Summary

A long-running orchestrator agent (flywheel, named-queues, controlpoints) eventually fills its context window. Today the only backstop is Claude Code's **auto-compaction**, which is lossy — it silently drops intent, decisions, and in-flight state. `session-keeper` replaces that lossy reset with an **intent-preserving** one: an external supervisor watches each orchestrator's context fill level and, on a clean idle boundary, drives `/session-handoff → /clear → /session-resume NAME` so the agent restarts fresh while carrying its intent forward through `HANDOFF.md`. The result is *indefinite* orchestrator execution without compaction loss.

## Goals

- **G1.** Surface context-fill level for a running orchestrator agent as a machine-readable signal an external watcher can poll.
- **G2.** Inject a non-destructive **wrap-up warning** at a high-water mark (~80%) so the agent can start winding down its own work. (Phase 1.)
- **G3.** Drive a full, intent-preserving **handoff → clear → resume** cycle when context approaches the compaction threshold (~90–95%) *and* the agent is idle. (Phase 2.)
- **G4.** Guarantee the reset never happens mid-dispatch or mid-tool-call (idle-gated), never loops (anti-reset-loop guard), and never collides with the daemon's bead dispatch (daemon-coordination).
- **G5.** Pre-empt Claude Code's own auto-compaction (so ours fires first, not theirs).

## Non-goals

- **NG1.** Not a built-in Claude Code feature — this is an *orchestration pattern* built on documented hooks/skills. (Feasibility confirmed 2026-06-01.)
- **NG2.** Not changing what `/session-handoff` / `/session-resume` *write* — session-keeper triggers those existing skills; it does not redesign the handoff format.
- **NG3.** Not managing bead-implementer claudes' context — those are short-lived per-bead sessions already reaped by the daemon. Scope is **orchestrator** claudes only. (This is the key extension of `harmonik supervise`.)
- **NG4.** Not solving cross-agent intent transfer — each agent resumes *itself* via its own HANDOFF-<role>.md.

## Constraints

- **C1.** Signal source: a **statusLine script**. Its stdin JSON exposes `context_window.used_percentage` + token breakdown; it updates after each assistant message (debounced ~300ms). The session `.jsonl` transcript does **not** carry per-message token counts → that path is a dead end.
- **C2.** Hard prereq: **Claude Code v2.1.132+** (for the statusLine context-percentage field).
- **C3.** Idle detection: the **Stop hook** fires after each response before control returns to the user → the only safe-to-inject window.
- **C4.** Compaction backstop: the **PreCompact hook** fires before auto-compaction and can **block** it (`decision:block` / exit 2) → use it to prevent lossy compaction and trigger a handoff instead.
- **C5.** Hard prereq: the session **must be named** (`/rename` or `--name`), or `/session-resume` opens an interactive picker and the injector **hangs**.
- **C6.** Injection mechanism reuses harmonik **pasteinject** (tmux `send-keys`) + the **event bus**.
- **C7.** Lane boundary: this **extends `harmonik supervise` + pasteinject**, which are named-queues' lane. Design/spec is flywheel's; supervise-touching implementation is **co-designed with named-queues**.

## Phasing (simpler-first)

- **Phase 1 — warn-only, non-destructive.** statusLine → per-session file (e.g. `/tmp/claude-context-<SESSIONID>`) → external watcher polls → at ~80% injects a wrap-up *warning* only. Validates the signal + injection path safely, with **no reset**.
- **Phase 2 — full auto cycle.** At ~90–95% **when idle** (Stop hook): inject `/session-handoff` → wait for `HANDOFF.md` mtime change → `/clear` → `/session-resume NAME`. Adds: named-session enforcement, mtime-confirmed handoff, anti-reset-loop guard, daemon-coordination (no reset mid-dispatch), PreCompact backstop.

## Success criteria (concrete, verifiable)

- **S1.** A running, named orchestrator session writes its `used_percentage` to a per-session file that updates within ~1s of each assistant message. *(Phase 1)*
- **S2.** When that file crosses the 80% mark, the watcher injects exactly one wrap-up-warning prompt into the correct tmux pane, at an idle (Stop) boundary, with no other side effects. *(Phase 1)*
- **S3.** When context crosses ~90% while the agent is idle, the supervisor completes `/session-handoff → /clear → /session-resume NAME` and the resumed session reads the freshly-written `HANDOFF.md` (mtime-confirmed) and continues its lane. *(Phase 2)*
- **S4.** The cycle never fires mid-dispatch (verified against the daemon's live queue) and never re-triggers within one post-resume settling window (anti-loop). *(Phase 2)*
- **S5.** With the PreCompact backstop installed, Claude Code's native auto-compaction does not fire on a supervised session — session-keeper's handoff fires first. *(Phase 2)*

## Open decisions (to confirm in this pass)

- **D1 (scope).** Design Phase 1 + Phase 2 together in this one work for a coherent spec, but **task and ship Phase 1 first** (independently validatable, non-destructive). — *Proposed default; proceeding unless redirected.*
- **D2 (lane).** Coordinate the supervise/pasteinject-touching parts with named-queues (per C7). — *Flywheel owns design; co-design implementation. Handled via comms, not a user gate.*
