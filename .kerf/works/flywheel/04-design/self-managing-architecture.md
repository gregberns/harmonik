# 04 — Change Design (Round 2): the self-managing architecture

> Supersedes the substrate/context-lean parts of `cognition-loop-design.md` after the round-2 research (self-management pivot). Round-2 findings: `03-research/{zero-framework-cognition, pi-self-management, agent-self-managed-context, turn-structure-and-cache, event-ingestion-and-timers}/findings.md`. Driven by the user's redirection: leverage agent capability (Yegge "zero-framework cognition"), let the agent manage ITSELF, build on Pi's composable parts (real codebase + TUI, not `-p`, not a shell script), keep it a persistent live agent (not respawn-per-wake).

## The shape (revised)
A **persistent, live agent** built as ONE extension over Pi's SDK (`@earendil-works/pi-coding-agent` → `pi-agent-core` + `pi-ai` + `pi-tui`). It stays running, is watchable in Pi's TUI, and sits beside harmonik's Go daemon, driving it through the same CLI/event surfaces a human uses. **No fork needed for v1** — every capability is on Pi's public extension API (`pi-self-management` findings).

## Posture: agent-led judgment, deterministic floor (the central decision, resolved)
Yegge/ZFC says route judgment to the model, keep the harness a dumb pipe. The failure research says the context-pressured model is the worst judge of its own reset. The reconciliation (and it's the heart of flywheel):
- **The agent owns judgment** — what to dispatch, what's wrong, when to reset its context, what to keep. (Why: the user's key point — only the thinking agent catches the dumb things workers do; pure-code can't.)
- **The harness owns a thin deterministic FLOOR** — it never decides *what* to do, but it guarantees safety: it shows the agent its own fullness, and if the agent fails to act before a hard limit, it forces a save+reset. (MemGPT's two-threshold pattern, proven.)

This keeps it agentic (Yegge) AND safe (the review's demand). The harness is a dumb pipe with one reflex: don't let the agent drive off a cliff.

## Self-managed context — the mechanism (answers "when do you reset?")
Pi already exposes the parts; we wire them in one extension:
- **The agent can see its own fullness.** Each turn, inject "context: 142k/200k (71%)" (Pi `getContextUsage()`; newer Claude tracks this natively). Inject as a %+instruction, not a raw number.
- **The agent has two tools:** `note(kind, refs, text)` (durable, append-only — survives any reset; Pi custom entries) and `reset_context(keep_hint)` (the agent's "I'm getting full, I've saved what matters, start me fresh").
- **Reset = a clean reseed, not a wipe.** On `reset_context`, the harness starts a fresh Pi context seeded with [stable instructions + the digest + open notes]. The agent continues, lighter. (Pi `newSession`/`session_before_compact`.)
- **The thresholds (MemGPT, the floor):** ~70% → inject "save notes + reset soon" (agent acts). ~90% → stronger nudge. **~100% → the harness forces it**: auto-build a digest, persist, reseed — whether or not the agent acted. A skipped tool call can never degrade the run.
- **"1 min vs 30 min" answered:** what survives a reset isn't "the last N minutes" — it's the digest (still-open items, any age) + the notes the agent chose to keep. Genuinely-recent reasoning is in the live conversation until a reset; resolved old chatter is dropped; anything dropped is re-fetchable from records.

## Two objections you raised — resolved precisely (`turn-structure-and-cache` findings)
- **"Modify context every model call? How does it think + make tool calls?"** → NO. A *turn* = one input that spins an inner loop of many model-calls (model→tool→result→model…) until the model stops. **Context is managed only at TURN BOUNDARIES** (Pi's per-turn hooks `shouldStopAfterTurn`/`prepareNextTurn`), never mid-think. Touching context mid-turn would orphan a tool-call/result the model is reasoning through (the API rejects it). So the agent thinks and tool-calls freely within a turn; we only reset/reshape between turns.
- **"Won't a changing digest bust the cache?"** → NO, if it sits BELOW the cache marker. The provider caches the stable top (instructions + tools) up to a marker; everything below is full-price but doesn't invalidate the cached top. Layout: `[tools + instructions] —cache marker— [digest] [conversation]`. The digest changes every turn at full price on a few hundred tokens; the big stable top keeps hitting cache at 1/10th price (~10× cheaper than putting the digest in the cached region). A reset re-sends the byte-identical top → cache hit. So agent-triggered resets are both reasoning-safe and cache-safe.

## Persistent + event-driven (not respawn) (`event-ingestion-and-timers` findings)
- The agent **stays alive**; events stream INTO the live session via Pi's `followUp`/`steer` queues, fed by a small **bridge** (a TS coroutine in the same process that tails `harmonik subscribe` NDJSON). When idle, an outer `Promise.race([wake, sleep(watchdog)])` blocks — **zero token cost while nothing happens.**
- **Startup is a real phase**: it does a stock-take, kicks off the first batch, then stays watching — no cold cram (fixes your startup concern).
- **Concurrency**: bursts (10 finish at once) are debounced (~400ms) into ONE refreshed digest → one turn, not ten. The daemon still does the merges; the agent learns outcomes from events + digest.
- **Urgent class** (merge_conflict) can interrupt the current turn (`harness.abort()` → reprompt); everything else queues to the next boundary.
- **Timers/watchdog** (your point): the outer loop wakes on a timer to check — no events for N min + active runs → verify daemon alive + runs progressing; a run's age past a stall threshold → investigate; daemon heartbeat stopped → flag "harmonik down," pause dispatch, reconnect with backoff.
- **Flywheel's own liveness**: runs in a named tmux pane; writes a heartbeat file; reuse the daemon's existing pane-liveness check. (No infinite who-watches-the-watcher.)

## Visibility (your hard requirement)
Pi's TUI, with a **custom panel** (extensions can `setWidget`/`setFooter`/`setStatus` without forking the TUI) rendering the same status sheet the agent reasons over — running/done/failed with durations + ages, what needs a decision, open notes, current context-fullness %. You and the agent look at the same screen. Plus a spend ceiling / kill-switch.

## What we keep from round 1
`harmonik digest` (Go) — still the load-bearing, testable, deterministic status-sheet builder, reading git(`origin/main`)+queue.json+events(`ScanAfter` bookmark)+beads. The bookmark + idempotent-reaction guard + loop-singleton lock (consistency). The "10-in-flight restart" scenario as the acceptance test. The required gates: replay test-harness, threat model (prompt-injection of a push-capable agent), cost kill-switch.

## Substrate/billing (corrected per your clarification)
You meant: from June 15 your Max plan gives ~$200/mo of credits usable for programmatic agents — so building on Pi/SDK is legitimate (not the banned OAuth trick). Decision: **build on Pi** (composable loop + TUI + multi-LLM; lets us run a cheap model for routine turns, a strong one for judgment — Yegge "model stratification"). Pi bills via API key / the credit pool. v1 = no fork (one extension); the only thing we'd ever fork is a non-aborting agent-callable compaction (a minor gap; deferred — we use deferred-to-turn-boundary reset instead).

## The slice (revised, concrete — all "existing tech")
1. **`harmonik digest`** (Go) — build + unit-test against recorded data; doubles as `harmonik digest --watch`. Useful to you immediately.
2. **One Pi extension**: registers `note` + `reset_context` tools; injects fullness-% each turn; turn-boundary reset logic with the 70/90/100 thresholds; the event bridge (tail `harmonik subscribe` → followUp); the cache layout (fixed marker after the stable prefix, digest below); the custom TUI status panel; the watchdog timers; heartbeat file.
3. **Run it persistent** against the real daemon, watch it in the TUI for hours; measure that the cache actually hits (`cache_read ≈ prefix size`); tune the 70/90/100 thresholds and the debounce/stall constants from observed behavior.

## Open items for the user
- Confirm: agent-led + deterministic-floor posture (vs more code-driven, or more pure-agent). Round-2 strongly supports agent-led+floor.
- Confirm: build on Pi (no fork v1) as the substrate.
- The threshold numbers (70/90/100) and debounce/stall constants are starting guesses → tuned in step 3, not pre-committed.
- Skill files (Yegge "fat skills"): encode "how to triage a failure / pick a batch / when to reset" as agent-read skills rather than baked logic — adopt? (Leans yes; ZFC-consistent.)
