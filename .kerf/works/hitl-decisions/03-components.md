# hitl-decisions — Components

Decomposition the change-spec is organized around. Seven components; all on the harmonik/comms layer (D1), event-log-projection-primary (D4). Lane: design-only — paul owns daemon-infra impl.

| # | Component | Responsibility | Addresses | v1 |
|---|-----------|----------------|-----------|-----|
| K1 | **Event contract** | Define `decision_needed` / `decision_resolved` / `decision_withdrawn` typed bus events + payload schema (keyed on `decision_id`). | G1, C1, C2, C7 | yes |
| K2 | **Agent raise + block/resume** | Agent emits `decision_needed` and blocks cleanly on its `decision_id`; wakes on `decision_resolved`/`decision_withdrawn`; agent-initiated `withdraw` when self-obsoleted. | G2, G6, C2, C6 | yes |
| K3 | **Open-decision projection** | On-demand fold over `events.jsonl`: `needed − (resolved ∪ withdrawn)`, deduped on `event_id`, keyed on `decision_id`. The shared source of truth; no always-on service. | G3, G5, C3, C4, D4 | yes |
| K4 | **Operator surface** (`harmonik decisions`) | `list` (the what-needs-me queue), `answer <decision_id> <option>` (emits `decision_resolved` → unblocks agent), `show <decision_id>`, `withdraw`. | G3, G4, D2 | yes |
| K5 | **Orphan reaper** | Daemon/keeper reaps an open decision whose agent died / is no longer waiting → emits `decision_withdrawn(reason=orphaned)`. No zombies. | G6, S7 | yes |
| K6 | **session-keeper seam** | Keeper consults the K3 projection before reaping a blocked agent; blocked-on-decision = known idle state, not a hang. | C6 | yes |
| K7 | **kerf reader view** | kerf "what-needs-me across works" reads the **same** K3 projection (+ optional daemon-written bead marker for visibility). | G3, D2 | v1-second |

## Component contracts (each gets a change-spec section)

- **K1 Event contract.** `decision_needed` payload: `decision_id` (stable, unique), `question`, `options[]` (enumerated), `context_link` (free-form: bead/work/thread/run), `blocked_agent` (who's blocked), optional reserved `value` field (v1.1 free-text hook, unused in v1). `decision_resolved`: `decision_id`, `chosen_option`, optional `value`, `resolver`. `decision_withdrawn`: `decision_id`, `reason` (`self_obsoleted` | `orphaned`). Registered alongside the existing comms event types; emitted via the same daemon op path as `agent_message`.
- **K2 Agent raise + block/resume.** The raise primitive emits `decision_needed`, then the agent blocks using the existing subscribe/wake machinery filtered to its `decision_id` (not a busy-wait). Wake on a matching `decision_resolved`/`decision_withdrawn`; **idempotent** — dedupe on `event_id` (C2) AND no-op a second terminal for the same `decision_id` (C7). On restart, the agent re-derives its open decisions from K3 and re-establishes the wait (S7b).
- **K3 Projection.** A pure function of the event log → the open set. Computable by the CLI or the daemon with no persistent aggregator (C3). Survives restart because the log does (S5). This is what BOTH K4 and K7 read — one contract, two readers (D2).
- **K4 Operator surface.** `list` renders each open decision as `question + options + blocked_agent + context_link`. `answer` validates the `decision_id` is open, emits `decision_resolved`; resolving an unknown/terminal id is a **no-op** (C7, S8). Single-human v1: first answer wins.
- **K5 Orphan reaper.** Runs in the daemon's existing sweep cadence (reuse, not a new service): for each open decision whose `blocked_agent` is absent from presence / not waiting, emit `decision_withdrawn(reason=orphaned)`. The agent, not being present, never needed the answer (S7a).
- **K6 session-keeper seam.** Before reaping an idle agent, the keeper checks K3 for an open decision naming it; if found, the agent is *blocked*, not *hung* — do not reap (C6). The reaper (K5) is the complement: it reaps the *decision* when the agent is truly gone, not the agent when it's legitimately blocked.
- **K7 kerf reader view.** Out-of-band consumer of K3; no new transport. Optional: the daemon writes a derived `blocked-on-human` bead marker so the decision shows in bead/kerf views — **daemon-written only**, never by the agent (C5/D4). Sequenced after the harmonik surface ships.

## Out of scope (NG-fenced)

Routing rules / multi-approver / SLA / escalation (NG1); GUI (NG2); re-implementing transport/cursor/dedupe (NG3); auto-decide / timeout-default (NG4, D3); a new normative bead lifecycle (NG5 — the marker is derived, not a state machine).
