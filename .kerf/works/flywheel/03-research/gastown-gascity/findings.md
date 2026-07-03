# Research — Gastown / Gascity

> Component: `gastown-gascity`. Source: research sub-agent (sonnet) over gastownhall/gastown + /gascity, 2026-05-27. **The closest existing implementation of what flywheel is designing.**

## TL;DR
- Gastown's answer to "indefinite running" is **session cycling, NOT session persistence**: the Claude context is explicitly ephemeral, killed+respawned on every compaction/step boundary; all durable state lives in Beads (SQLite+JSONL) and git. No LLM context ever grows unbounded.
- State handoff between agent generations needs **NO explicit payload**: the new session runs `gt prime --hook` on startup, reads its hook bead from Beads, reconstructs its mission from the durable store alone — no sliding windows, no compaction, no growing conversation. *"The beads state IS the handoff."*
- **Gascity formalizes this into a `convergence` primitive** — a named loop (work-wisp → agent runs → evaluate → gate → iterate/approve/terminate) where the *controller* drives cycling, not the agent, with idempotency keys for crash-safe recovery. **This primitive IS the "custom long-running loop runtime" flywheel is designing — expressed as a bead-backed formula loop rather than a process loop.**

## 1. Continuous loop + context bounding
Each role (Witness/Refinery/Deacon/Polecat) is a *persistent tmux session* containing an *ephemeral* Claude process. Session permanent; Claude inside it not. Three bounding mechanisms:
- **`gt handoff --cycle`** (`internal/cmd/handoff.go: runHandoffCycle()`): on Claude's `PreCompact` hook → `collectHandoffState()` scrapes inbox+ready-beads+hook-bead into a short digest → sends handoff-mail-to-self (a Beads bead, auto-hooked next session) → writes `.runtime/handoff-marker` → `tmux respawn-pane --continue` restarts Claude with fresh context + only "Context compacted. Continue your previous task."
- **`gt handoff --auto`**: lighter; saves state to Beads without respawning (used outside tmux).
- **`gt patrol report`**: patrol agents atomically close current patrol-wisp + create new + `gt handoff` → fresh session every iteration. **Handoff cooldown** (`.runtime/last_handoff_ts`) prevents tight restart loops.
- Invariant (`docs/concepts/polecat-lifecycle.md`): *"Session cycling is normal operation, not failure."*

> ⚠️ DISCREPANCY TO VERIFY: this agent reports `--cycle` respawns with `--continue` for a "fresh context," but the Claude-Code-SDK research says `--continue` *replays the full transcript*. Either Gastown clears the session JSONL before respawn, or "fresh" is aspirational. **Flag for the verification pass** — it bears directly on whether respawn-with-continue actually gives a fresh context or a replayed one.

## 2. State continuity across restarts
State is NOT in conversation history. Three durable layers: **git** (commits/branch survive), **Beads** (molecule progress, `hook_bead`, agent fields `agent_state`/`cleanup_status`), **hook state** (`hook_bead` persists). New session discovers position via `gt prime --hook` (Claude `SessionStart` hook) → reads hook bead, renders formula checklist inline → a fixed, deterministic, **cache-stable prefix**. No history replayed. `docs/design/polecat-lifecycle-patrol.md §2.4`: *"No explicit 'handoff payload' is needed. The beads state IS the handoff."* = exactly flywheel's "fixed cache-stable prefix + small deterministic digest."

## 3. Multi-agent roles (a dispatcher fleet — the path flywheel's single-vs-multi thread rejects for now)
Strict role hierarchy, no peer-to-peer, all coordination via bead/mail bus:
| Role | Lifecycle | Function |
|---|---|---|
| Deacon | singleton persistent | town watchdog; heartbeats; monitors Witnesses |
| Witness | one/rig persistent | detects stalled/zombie polecats; respawns crashed sessions |
| Refinery | one/rig persistent | merge-queue; squash-merges; serializes pushes via slot lock |
| Polecat | pool of N | worker; ephemeral sessions; Witness-managed |
| Mayor | singleton | human-facing; no mechanical loop role |
Deacon ≈ daemon: receives heartbeat pokes from a Go process (`internal/deacon/heartbeat.go`), writes `heartbeat.json`, Go checks staleness (5min/20min) to poke/restart. **No shared-memory coordination**; concurrency = git worktrees (one/polecat) + per-rig merge slot lock. Polecats write findings to Beads, close beads, send mail; Witness reads bead transitions. **Convoys** (`docs/concepts/convoy.md`) = batch tracking (a convoy bead tracks N work-beads; all-close → notify) — monitoring, not scheduling.
> Note: this is Architecture C (role fleet) — needed because Gastown has *no central controller*. Harmonik HAS the daemon, so the single-vs-multi thread argues harmonik should NOT copy this fleet. Cite as the path-not-taken-yet.

## 4. tmux usage (inspectability)
Each role = named tmux session; `session.StartSession()` (`internal/session/lifecycle.go`) sets env, role color theme, `remain-on-exit` so the pane survives process death during handoff. `gt deacon attach`/`gt witness attach` = operator inspectability. On cycle, `tmux respawn-pane` atomically kills+restarts Claude in the same pane (path unchanged → attached operators keep their view; tmux scrollback cleared). A `PreCompact` hook fires `gt handoff --cycle` *before* Claude's own compaction → Gas Town controls cycle timing.

## 5. Gascity convergence primitive (most relevant)
`internal/convergence/` formalizes the indefinite loop as a typed SDK primitive. A convergence (root) bead runs forever:
```
pour_wisp(formula, idempotency_key=iter_N)
  → agent works formula steps → controller injects "evaluate" step
  → agent writes bd meta set convergence.agent_verdict=<approve|iterate|reject>
  → gate evaluates: condition script (exit 0=pass) + agent_verdict
  → pass → close root bead (terminate); fail → N++ → pour_wisp(iter_N+1)
  → no_convergence (max iters) → wait_manual
```
Gate modes (`gate.go`): `manual`/`condition`/`hybrid`; timeout action: iterate/retry/manual/terminate. On crash, `Reconciler.ReconcileBeads()` scans in-progress convergence beads and repairs to consistent state via idempotency key `converge:<bead-id>:iter:<N>` — no iteration double-counted/lost. **The agent doesn't know it's in a loop; it sees one wisp at a time; the controller drives iteration; state survives crash via idempotency keys in the bead store.** This is harmonik's consistency-thread design, already built in another system — strong corroboration.

## 6. Context-window handling: `--cycle` vs `--auto`
`--auto` (PreCompact default): save state to Beads, marker, **let Claude compact normally**; post-compact prime uses a lighter continuation. `--cycle` (full replacement): save, marker, `respawn-pane --continue` brand-new process = the "no compaction, fresh context" path. `--cycle` is the path that eliminates compaction; both avoid a growing sliding-window conversation.

## What flywheel takes
- **Session cycling not persistence** (= fresh context per cycle, store is the handoff) — exactly flywheel's model, validated in production.
- **`gt prime`-style deterministic startup context** from the durable store = the digest/prefix builder.
- **Gascity convergence + idempotency keys + Reconciler** = corroboration for the consistency-thread watermark/dedup design.
- **PreCompact-hook-driven cycle timing** (control the recycle *before* compaction can fire) — a concrete trigger mechanism for "recycle near the limit."
- **Handoff cooldown** — prevents tight restart loops (adopt for flywheel's recycle).

## Files
gastown: internal/cmd/handoff.go, prime.go/prime_session.go, patrol_report.go, internal/session/lifecycle.go, internal/deacon/heartbeat.go, docs/concepts/polecat-lifecycle.md, docs/design/polecat-lifecycle-patrol.md, docs/HOOKS.md. gascity: internal/convergence/{handler,gate,reconcile,formula}.go, internal/session/state_machine.go, docs/getting-started/coming-from-gastown.md.
