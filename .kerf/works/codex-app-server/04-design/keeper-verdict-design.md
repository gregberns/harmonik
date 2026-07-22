# 04 — Design: Keeper verdict for a Codex app-server orchestrator

> codename:codex-app-server · Pass 4 · the headline operator question · synthesizes C1 × C3.
> Question: can a resident long-context Codex session RETIRE the keeper/compaction machinery?

## Verdict

**Mostly YES — retire the bulk; reshape a thin token-pressure trigger; retain the substrate-neutral
liveness/presence remainder.** Concretely: a Codex crew running on an app-server thread does **not**
spawn the keeper window (`crewstart.go:288`) and does **not** run the handoff→`/clear`→
`/session-resume` cycle at all.

## Why (the C1 × C3 join)

The keeper exists to manage a **client-side context window that fills and forces a handoff before
the tmux pane overflows** (C3). App-server holds the conversation **server-side** — history in
rollout JSONL + SQLite, reconstructed on `thread/resume`, token-accounted via
`thread/tokenUsage/updated`, compacted via `thread/compact/start` (C1 §4). There is no client-side
growing buffer and no `/clear` to drive. The specific precondition the keeper manages is **absent**
on this substrate.

## Component-by-component (from C3's inventory)

**RETIRES (exists only for the Claude client window — ~70–80% of keeper mass):**
- The whole threshold band (WARN 200k / ACT 215k / FORCE-ACT 240k / HARD-CEILING 280k) and the
  `min(abs, pct×window)` formula.
- The statusLine context-fill gauge writer (`.harmonik/keeper/<agent>.ctx`) and the 5s watcher poll.
- The handoff → nonce-poll → `/clear` → `/session-resume` reset cycle and its nonce invariant.
- `.managed` / CrispIdle / operator-attached gates; boot-grace; restart-now/restart-verify.
- The hold/release co-working override (it suspends the ACT cutoff — moot with no cutoff).

**RESHAPES (depends on OQ-1, auto-vs-manual compaction):**
- If app-server auto-compacts at window pressure → the token-pressure logic **fully deletes**.
- If compaction is caller-triggered only → a thin trigger remains: watch `thread/tokenUsage/updated`
  and call `thread/compact/start` at a threshold. This is a few lines reacting to a server event —
  NOT the client keeper (no gauge file, no `/clear`, no handoff, no pane-overflow race).

**RETAINS (substrate-neutral — never was about context):**
- Process/sidecar liveness + single-instance lock (now the app-server watchdog, C4 §4).
- `--respawn-cmd` / revival semantics (→ the sidecar supervisor).
- Comms presence/TTL beats — but **daemon-proxied** now (C5), not client-driven.
- The `br --assignee` durable mirror + idempotent inbox (bead-side, substrate-neutral).
- The liveness (ping/await-ack) half of the ACK handshake.

## Restart continuity, restated

Keeper-restart re-hydration (handoff→clear→resume) **collapses** into: `thread/resume <thread_id>`
+ replay comms/queue events since the last-seen cursor (C5). The daemon persists thread_id, queue,
epic_id, comms identity, subscription cursor; the server holds all reasoning/conversational state.
The C3 mission handoff file shrinks from a load-bearing rehydration seed to an
attribution-redundancy record.

## Net effect on `hk-6z72r` (keeper compat bead)

`hk-6z72r` becomes **primarily a deletion/branch, not new machinery**: at crew-start, the codex
branch skips the keeper window; the only *additive* piece is the conditional token-pressure trigger,
and only if OQ-1 resolves to "manual compaction." This is the central bet of the epic, and the
research confirms it holds.

## Load-bearing caveat

Retiring the *client keeper* does not abolish *window exhaustion* — the model still has a finite
server-side window. Management **relocates** from a client keeper to server-side compaction +
(optionally) a thin client trigger. The win is real (delete ~70–80% of a fragile, pane-coupled
subsystem and its whole failure surface) but it is a relocation, not a magic-wand elimination —
stated plainly so the review gate weighs it honestly.
