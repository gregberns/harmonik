<!-- PP-TRIAL:v2 2026-06-08 main — controlpoints thread. NEW DESIGN WORK this session: the `captain` kerf work (Captain & crew). CLEAN, nothing in flight. The prior controlpoints lane (productization onboarding-docs) stayed DONE; this session pivoted to a new design at the operator's request. Do NOT clobber the shared HANDOFF.md or the flywheel/named-queues threads. -->

ROLE: orchestrator (design mode). The `captain` kerf work is a PLAN, not dispatchable code yet — no daemon work this session.

# Where we are — CLEAN, design parked mid-kerf
Designing **Captain & crew**: a long-lived "captain" orchestrator that spawns + coordinates long-lived "crew" agents (each owns one epic + its own queue), wired by comms + a new `epic_completed` event. **In scope = the wiring** (start captain+crew, comms between, the completion event). **Out of scope = the captain's judgment** (ranking initiatives, handling stuck/failed ones).

## Status: kerf work `captain` (plan jig) at the **research** pass
Artifacts 01–04 are on the bench `.kerf/works/captain/` (gitignored, durable). **Next step = write the C1+C2 change-spec.** The operator wants a check-in at the change-spec and again at the final task list. Then integration → tasks. Resume with `kerf show captain`.

## Decisions locked (don't re-litigate)
- **(a) long-lived autonomous crew.** Spawn/address/restart via **`claude remote-control` + `session_id`/`--resume`** (Agent SDK / `claude -p --resume`), NOT tmux-paste. The operator CORRECTED an earlier wrong finding of mine — programmatic remote-control drive IS documented and works. Settled.
- **4 components:** C1 `epic_completed` event (code), C2 `harmonik crew start/stop` + per-crew registry (code), C3 crew launch context + shared handoff schema (instructions), C4 captain operating context (instructions). **No Go supervisor.**
- Reviewer catches folded in: C1 needs an idempotent at-most-once emit + a `brShowEdge` status-field change; the handoff schema is the shared C2/C3/C4 contract; progress feed (#3) is owned via `comms --topic status` + `br comments`. Crew identity = `session_id` (resolves the keeper↔pane gap).

## Loose threads (non-blocking)
- Peer broadcast asking who validated `--remote-control` — no reply yet; doesn't block (docs confirmed it).
- Keeper token-threshold bug (90%-of-1M ≈ 900k tokens; should gate on absolute ~200–300k) sent to **flywheel** via comms — they own folding it into session-keeper, NOT this work.

## Files to open first
`.kerf/works/captain/` → `01-problem-space.md`, `03-components.md`, `04-research/findings.md`.

# Translations glossary
- **captain / crew** — captain = top orchestrator that hands epics to long-lived "crew" agents; crew each own an epic + their own queue.
- **epic_completed** — proposed new event fired when an epic's last child bead closes; the captain's structural trigger.
- **C1–C4** — the four design components above.
- **keeper** — harmonik's context-watcher that restarts a session near context-full (session-keeper work, flywheel's lane).
- **comms** — `harmonik comms`, the inter-agent message bus (send/recv/--topic).
- **session_id / --resume** — Claude Code's headless session id + resume flag; the crew addressing/restart primitive.
- **productization** — the prior controlpoints lane (onboarding docs); DONE, not active.
