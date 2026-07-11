<!-- DRAFT — proposed replacement for /HANDOFF-captain.md (repo root)
     (startup-doc revamp Stage 2 companion, per 02-cutover §2.1[B] + step 4.1).
     NOT a delete-tombstone — this file CANNOT be removed. It is the tier-1 STORAGE SLOT the
     boot pipeline reads: `internal/agentmanifest/brief.go` readHandoff() embeds
     <root>/HANDOFF-captain.md into `harmonik agent brief`, and the keeper's restart gate
     (`internal/keeper/restartnow.go`) refuses to /clear when it is missing or stale. The
     redirect therefore lives as a STANDING BANNER inside the file; the body below stays the
     live handoff the captain overwrites each keeper cycle. The keep-the-banner instruction is
     owned by SHUTDOWN.md (see drafts/SHUTDOWN.md §1). Body shown = the live 2026-07-11 ~06:45Z
     handoff, carried as illustration and still-current claim at draft time — on deploy, keep
     whatever body is then current and prepend the banner. This DRAFT comment is removed on
     deploy; the ═══ banner below is NOT. -->

<!-- ═══ NOT A BOOT DOC — DO NOT READ THIS FILE DIRECTLY ═══
     This is the STORAGE SLOT for the captain's tier-1 handoff. It reaches the captain already
     EMBEDDED in `harmonik agent brief` — the brief's output IS the complete boot context.
     Booting? Run: harmonik agent brief --wake fresh|keeper-restart|trigger:<id>
     Everything below is a CLAIM by the previous session, not ground truth — one
     `harmonik digest` pass overrides every statement here.
     WRITERS (captain, at handoff time): overwrite the body; KEEP this banner and the
     KEEPER-IDENTITY block; include the KEEPER nonce. Tier-2 state (lanes.json,
     captain-lanes.md, direction-log.md) must already be updated and committed BEFORE this
     file is written — the handoff POINTS at state, it does not duplicate it. -->
<!-- PP-TRIAL:v2 2026-07-11 main -->
<!-- KEEPER:cyc-20260703T171635-000094 -->

# Captain handoff — 2026-07-11 ~06:45Z

**State: clean.** Fleet healthy, 5 lanes + watch + admiral up. No broken/blocked work. ONE live
operator decision is open and awaiting reply (see NEXT). This was a keeper-restart resume; my own
context work (lane-doc prune, piter re-task) is committed + pushed.

## Glossary (plain English)
- **piter** — the crew owning the Codex-as-crew lane (run a crew orchestrator on Codex, not Claude).
- **Codex Option B** — daemon re-invokes `codex exec resume` per wake. **KILLED by operator this
  session — hard no, never revisit or discuss again.**
- **codex app-server** — Codex's resident JSON-RPC server that holds conversation state SERVER-SIDE.
  The path piter now researches: a resident orchestrator that could retire most of the keeper.
- **hk-l63b9** — the crew-harness-select seam bead; OPEN-PARKED until the app-server design is ratified.
- **B1/B2** — comms-bus fixes: B1=hk-8xspi (`recv --agent` own cursor), B2=hk-qw63o (idle `--follow`
  presence-beat so quiet crews don't age out of `comms who`). Operator-ratified + dispatched to yueh.

## What happened this session
1. **Codex Option B killed** (operator direct). Recorded in direction-log; piter re-tasked to a FULL
   kerf work `codex-app-server` (deep research → design). Mission file rewritten.
2. **piter ran the whole kerf cycle** (problem-space → research(5 comps) → change-design), passed
   independent adversarial review. **Now at the DESIGN-REVIEW GATE awaiting operator ratification.**
3. **B1/B2 dispatched** — I nudged yueh (was idle, hadn't picked up the 05:59Z assignment); it created
   + dispatched both beads.
4. **captain-lanes.md pruned** 605→55 lines (deleted superseded history back to 06-19), committed+pushed
   (4b3fa53d). Fleet lane table now current.

## NEXT (the one open thread — DECISION AWAITING OPERATOR, admiral surfaces it)
piter's `codex-app-server` design is ratification-ready. Verdict: a resident app-server orchestrator
**retires ~70-80% of the keeper machinery** (server-side context, no client window, no `/clear` cycle) —
honestly a *relocation* to server-side compaction, not elimination. Real cost = a net-new JSON-RPC
sidecar subsystem (hk-nzzos). Surfaced 3 options + recommended **(b) ratify but gate implementation
on a cheap backend-auth spike first** (auth is the one unknown that could break the ChatGPT-billing
premise). **Options:** (a) ratify+build now, (b) ratify+gate on auth spike [my rec], (c) send back.
Do NOT start implementation / un-park hk-l63b9 until the operator picks. Design docs:
`~/.kerf/projects/gregberns-harmonik/codex-app-server/04-design/`.

## Other pending operator decisions (non-blocking — tracked in admiral-initiatives.md)
hk-0639 close-or-keep · hk-4u1mb defer · governor liveness_no_progress_n=10.

## State pointers (tier-2 is the record; this section only points)
- `.harmonik/context/captain-lanes.md` — current 5-lane snapshot (lanes.json is the registry).
- `.harmonik/context/direction-log.md` — newest entry = the Option-B kill + piter pivot.

## Watch out
- **admiral tmux pane = LIVE OPERATOR session** — do NOT send keystrokes or relaunch admiral.
- `paused-queue:yueh-q` ops-monitor alerts = known noise (hk-1x8az dep-blocked on hk-thbbv). Ignore.
- Presence-staleness ≠ death: a crew absent from `comms who` but showing a live pane spinner/idle-box
  is alive (the B2 bug). Verify pane-truth before reconciling — do NOT `crew stop`.

<!-- KEEPER-IDENTITY -->
**Agent identity (keeper-authoritative):** You are `captain`. Your HARMONIK_AGENT environment variable is `captain`. Use `harmonik comms send --from captain` (or rely on $HARMONIK_AGENT). Do not reconstruct identity from conversation history — trust this line.
<!-- /KEEPER-IDENTITY -->
