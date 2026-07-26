# 02 — Components (Decompose)

> codename:codex-app-server · Pass 2 · feeds Pass 3 research (`03-research/{component}/findings.md`)

The work decomposes into five research components. Each has a sharp question and a named evidence
source so Pass 3 can be fanned out to independent investigators without overlap.

## C1 — `codex-app-server` protocol & session surface
- **Question:** What is `codex app-server` concretely — its JSON-RPC method set, session/thread
  creation & lifecycle, how a caller sends a turn and receives streamed output, restart/reconnect
  and auth semantics, and (critically) how it handles **context growth / compaction** server-side?
- **Evidence:** OpenAI Codex app-server docs (developers.openai.com/codex/app-server), the
  `openai/codex` repo `codex-rs/app-server/` (README + proto), VS Code extension / desktop as
  reference clients. External + source.
- **Owns:** the keeper-question's factual half.

## C2 — harmonik orchestrator loop contract (what the substrate must satisfy)
- **Question:** What exactly does a harmonik orchestrator *do* over its lifetime that the
  substrate must support? Enumerate the loop: comms-join + presence refresh, named-queue
  submit/subscribe, event-wake, bead reads, re-task mid-session, drain/park. What today depends
  specifically on the Claude `--remote-control` live-paste channel vs. what is substrate-neutral?
- **Evidence:** `internal/daemon/crewlaunchspec.go` (`buildCrewLaunchSpec`), crew-launch / captain
  / orchestrator-rules skills, `docs/orchestration-protocol-v2.md`, comms + queue CLI surface.

## C3 — keeper / compaction machinery (what might be retired)
- **Question:** What is the keeper, mechanically? The two thresholds, the
  handoff→`/clear`→`/session-resume` cycle, the per-session watcher, restart re-hydration. Which
  parts exist *only* because a Claude client holds a bounded growing context window? If context is
  managed server-side by app-server (C1), which components become unnecessary, which reshape,
  which remain (daemon-side liveness, presence)?
- **Evidence:** the `keeper` skill + `.claude/skills/keeper/`, `docs/design/agent-wake-mechanism.md`,
  keeper memory notes.
- **Owns:** the keeper-question's harmonik half; joins to C1's verdict.

## C4 — daemon integration & harness-routing seam
- **Question:** Where would a JSON-RPC app-server client live relative to the daemon (in-process
  subsystem vs. sidecar)? How does `crew-start` / `buildCrewLaunchSpec` route to a
  Codex-app-server orchestrator instead of Claude (the parked `hk-l63b9` seam)? What new failure
  modes (reconnect, backpressure, auth, app-server crash) must the daemon own, and how do they map
  to the existing supervisor/health-window machinery?
- **Evidence:** `crewlaunchspec.go`, `codexharness.go` (existing Codex integration), daemon
  supervisor/harness-lifecycle code, `harmonik-lifecycle` skill.

## C5 — captain commissioning & restart continuity
- **Question:** How does the captain commission and drive a Codex-app-server crew (mission
  handoff, comms addressing, epic attribution) with no live pane to paste into? What is "restart
  continuity" when conversational state lives server-side in an app-server thread — does
  keeper-restart re-hydration collapse into "reconnect to thread_id"? How is presence/liveness
  maintained for a session the daemon fronts?
- **Evidence:** captain skill §10 (restart continuity), crew-launch §restart re-hydration, crew
  registry (`SessionIDCaptured`/thread_id capture), C1 + C4 outputs. Synthesizes.

## Dependency Map
- **C1 × C3 jointly answer the headline keeper question** — C1 (does app-server manage context
  server-side?) crossed with C3 (which keeper parts exist only for client-side context?). Both
  run first; the change-design pass renders the verdict.
- **C2** is the requirements baseline. **C4** and **C5** are integration design and depend on
  C1 + C2. C5 also depends on C4.
- C1 needs the most external/web grounding; C2–C5 are mostly harmonik source reading.

## Goal → Component Traceability
- Goal "describe app-server session model" → C1 + C5.
- Goal "answer the keeper question with evidence" → C1 + C3.
- Goal "grounded in the real app-server surface" → C1.
- Goal "name the concrete integration path (un-park hk-l63b9)" → C4 (+ C5 for commissioning).

## Out of scope (carried from 01)
Option A (bare TUI) and Option B (`codex exec resume` per-wake) — closed, do not re-survey.
Pi-as-orchestrator — flag generalization only. Implementation / routing code — not this phase.
