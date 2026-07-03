# hitl-decisions — Research findings (patterns to mirror)

Read-only research over the FINALIZED `agent-comms` spec + the harmonik comms/event/subscribe/keeper code. Every item is anchored to file:line. This is the substrate the change-spec mirrors — no new transport, datastore, or always-on service.

## Spec analog to parallel
`.kerf/works/agent-comms/05-spec-draft.md` (FINALIZED, peer sign-off 2026-06-01). On finalize the new spec lands in `specs/`. Mirror its skeleton: §1 Event schemas → §2 CLI surface (`harmonik decisions` list/answer/show) → §3 projection/subscribe semantics → §4 durable delivery → §5 NORMATIVE conditions (N1/N2/N3 numbered) → §6 integration seams (kerf/keeper) → risks. Copy N3 (dedupe-on-`event_id`, at-least-once) verbatim-in-spirit. Preserve the agent-comms non-goal: a `decisions answer` MUST leave `git status` unchanged (events live under gitignored `.harmonik/`).

## Event model (where the two/three types slot in)
- Type constants: `internal/core/eventtype.go` — `type EventType string`, **open**, no closed enum / no panic-on-unknown. Additive.
- Register constructors: `RegisterEventType(name, func() EventPayload{...})` at `internal/core/eventregistry.go:79` (dup-guarded; name must be §8-declared per EV-025).
- Envelope: `core.Event` (`internal/core/event.go:27`); `event_id` is bus-minted (`b.idGen.Next()`, `busimpl.go:308`), UUIDv7.
- Payload analog: `AgentMessagePayload` (`internal/core/agentcommspayloads_djqc9.go:77`) with a `Valid()` method — model `DecisionNeededPayload` / `DecisionResolvedPayload` / `DecisionWithdrawnPayload` on it.
- **HARD CONSTRAINT 1 (durability):** F-class (fsync-on-write) event types MUST be added to `fsyncBoundaryEventTypes` (`busimpl.go:115-131`). `agent_message` is F-class. **Make all three decision events F-class** — otherwise a `decision_resolved` can be lost on crash *before the blocked agent reads it* (silent un-wake).

## CLI wiring (`harmonik decisions`)
1. Top-level route: `cmd/harmonik/main.go:465` — add a `"decisions"` branch beside `"comms"`.
2. Verb switch: copy `runCommsSubcommand` (`cmd/harmonik/comms.go:88`) as `runDecisionsSubcommand` (list / answer / show / withdraw).
3. Transport: Unix socket `<project>/.harmonik/daemon.sock`; request `{"op":...,"payload":{}}` → `{ok,result,error}`; exit 17 when socket absent (mirror `comms.go:247-306`).
4. Daemon dispatch: op-switch at `internal/daemon/socket.go:394` (`case "comms-send"` ~481) — add `decisions-list` / `decisions-answer`. Handler iface `socket.go:33`, wired in `daemon.go:1473/1497`.
5. Request/result structs: model on `CommsSendRequest` / `CommsSendResult{EventID}` (`internal/daemon/commshandler_nbrmf.go:39`).

## Block-and-wake (the blocked-agent contract)
- Subscribe type-filter accepts arbitrary new type strings (`subscribe.go:313-319`, exact-match `offer()` `:567`). `harmonik subscribe --types decision_resolved` arms-then-blocks correctly; arming before the type ever flows just waits — the desired semantics.
- Heartbeat keep-alive every N s (default 60, clamp 10-600), reset on each real event (`subscribe.go:456,506-542`).
- **HARD CONSTRAINT 2 (PULL-only delivery — load-bearing):** an idle agent does NOT wake on event arrival unless it is *holding an open subscribe/recv stream* (`docs/retro/2026-06-10/A2-crew-retro.md:21-29`). The blocked-wait contract MUST say: to wait on `decision_resolved`, **hold an open `harmonik subscribe --types decision_resolved` (or `recv --follow`) stream; do not idle bare.** The closest existing analog is `comms recv --follow`'s blocking `dec.Decode()` loop (`comms.go:1505-1620`).

## Projection (open-set, no new service) — VIABLE
- Forward replay: `eventbus.ScanAfter(path, sinceID)` (`jsonlwriter.go:312`), no max-scan limit; torn-tail partial lines skipped; EV-020 forbids rewrite/truncate/reorder (no rotation).
- Mirror `ComputePresenceRegistry()` (`comms.go:759-847`): single `ScanAfter` from zero, fold event types into an in-memory map, emit the current set — **no daemon/socket needed.** Fold: collect `decision_needed`, subtract those with a matching terminal (`decision_resolved` ∪ `decision_withdrawn`), dedupe on `event_id`, key on `decision_id`.
- Restart survival is free — the daemon already replays the log on boot (`daemon.go:1514-1528`).
- **Correlation key:** use the `decision_needed` event_id (UUIDv7) AS the `decision_id`, echoed in `decision_resolved.decision_id` / `decision_withdrawn.decision_id` (mirrors `agent_message.in_reply_to`, `agentcommspayloads_djqc9.go:81`).

## Seams: kerf + session-keeper
- **kerf:** NO existing "what-needs-me"/blocked-on-human concept (`docs/components/internal/kerf.md:65` leaves "what-to-work-on-next" open; kerf "decision" language is planning-phase Commander's-Intent, not runtime). **No collision** — `decision_needed` is the first structured runtime "human-input-awaited" signal. Note the orthogonality so operators don't conflate it with kerf planning decisions.
- **session-keeper (sharpest constraint):** staleness = 120s (`internal/keeper/watcher.go:208`); a gauge un-updated 120s fires `session_keeper_no_gauge` (treated hung). Only gates are `CrispIdle` / `HoldingDispatch` (`internal/keeper/gates.go:53-88`) — **no "awaiting-human-input" exemption.** A silent blocked agent IS reaped. Spec resolves via **HARD CONSTRAINT 2**: the open subscribe stream's heartbeat keeps the agent observably alive (cheaper than a new gate, reuses machinery). Belt-and-suspenders alternative: a `.decision_waiting` marker added to the staleness-exemption (mirroring the `.dispatching` gate at `gates.go:79`).

## Two clauses the change-spec MUST carry
1. **F-class durability:** all decision events fsync-bounded (`fsyncBoundaryEventTypes`).
2. **Blocked-wait contract:** the blocked agent holds an open `subscribe`/`recv --follow` stream — this simultaneously (a) wakes it on the answer and (b) keeps session-keeper from reaping it. One clause, both problems.
