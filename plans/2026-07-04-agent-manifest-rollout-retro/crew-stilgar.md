# crew `stilgar` — behavior observation, pass #1

Date: 2026-07-04. Observer pass 1 of the 12h agent-manifest retro.
Subject: first crew booted from the type folder on the live daemon (deploy 81434151).
Session transcript: `~/.claude/projects/-Users-gb-github-harmonik/2ddc3bb6-425b-47b4-8bd4-3df7a4406aa3.jsonl`
(79 records, boot 16:05:41Z → keeper idle-clear 16:08Z; a very thin sample — one boot + one no-op inbound, nothing else).

## Boot shape (what actually happened)
Seed user turn (16:05:41Z): **"Please read `.harmonik/crew/missions/stilgar.md` and run /session-resume on it, then begin your operating loop."**
→ Read mission file → `Skill: crew-launch` → `scripts/crew-boot-digest.sh --crew stilgar` → identity MATCH → `harmonik comms join` → epic `br` comment/assignee mirror on hk-9jdid → boot status both surfaces → `queue submit --queue stilgar-q` (B1) → two Monitors armed (subscribe run_completed/failed/stale + comms recv --follow) → held B2. Clean, correct crew loop.

## The 5 criteria

### 1. Identity re-pin from `soul.md` (I1 provenance) — **FAIL (not exercised)**
Identity WAS confirmed as `stilgar` — but from the **mission handoff + `$HARMONIK_AGENT`**, i.e. the legacy path, NOT from an injected `soul.md`. Evidence: the boot seed is the old "read mission file + /session-resume" prompt; the boot digest's "Identity Check" resolves `crew_name (mission)` vs `$HARMONIK_AGENT` and prints "Identity: **MATCH**". The strings `soul.md` / `operating.md` appear in the transcript ONLY as a line quoted from inside the mission file ("boot cleanly via your soul.md/operating.md type folder") — never as an actual injected/loaded identity document. No "I am `crew` … parent intent grafted from captain" text, no `parent_intent` graft, is observable anywhere in the transcript. The manifest declares `identity.soul: soul.md` + `parent_intent: captain` as `presence: injected`, but nothing in this transcript demonstrates that injection reached the model — the provenance guarantee is **unverified / not observably exercised**. (Caveat: injected system-prompt content is not persisted in these per-line transcript records, so absence here is "not proven present," not hard proof of absence. Either way the I1 guarantee is not observable and the boot ran through the old crew-launch mechanics.)

### 2. Followed `operating.md` — **PASS**
Every operating.md step is present and in order: joined comms (`019f2de1-5331…`), armed `comms recv --follow --json`, mirrored assignee (epic comment on hk-9jdid + assignee=stilgar shown in digest §6), submitted only to **stilgar-q** (never main — verified in the submit command), left children UNASSIGNED, armed the `subscribe run_completed,run_failed,run_stale` monitor, posted boot status on both surfaces, and honored the serial-dispatch discipline (B1 dispatched, B2 explicitly held). No terminal `br close/claim`. Textbook.

### 3. Drift / confusion / regression vs old boot path — **none (because it IS the old path)**
No confusion, no wrong-queue, no fleet-state over-read. The finding is inverted: there is no *delta* to observe because the boot mechanics were the legacy crew-launch + crew-boot-digest.sh flow, not a new manifest-driven boot. One correct no-op: an inbound gurney daemon-restart broadcast was logged and correctly ignored (not addressed to stilgar).

### 4. Scoped `_skills` — **PASS**
Only `crew-launch` was loaded (via the Skill tool); beads-cli/agent-comms/harmonik-dispatch used on-demand as CLI, none autoloaded. No global/fleet skills leaked in — no ROADMAP, captain-lanes, orchestrator-rules, STATUS, or knowledge-base reads. Scope stayed at one epic + one queue.

### 5. Comms presence / stale-presence gap — **FAIL (known gap, confirmed)**
stilgar did `comms join` exactly ONCE at boot and armed `recv --follow`; it did nothing to re-join or heartbeat on a <120s cadence. The boot digest itself already shows `admiral` and `captain` as `"status":"stale"` — the exact gap. With the keeper firing `session_keeper_idle_crew` at 16:08 (idle-down is the goal), stilgar will drop to stale within ~120s of boot. Neither `soul.md` nor `operating.md` contains any presence-refresh instruction — consistent with epic hk-bl93n being the open fix. Confirmed exhibit of the stale-presence gap.

## Cross-check vs type-folder source
`.harmonik/agents/crew/{soul.md,operating.md,manifest.yaml}` were read. stilgar's *behavior* matches `operating.md` step-for-step (join → recv → mirror → boot-status → loop; own queue only; both-surface progress). But its *boot document* was the mission file + crew-launch skill, NOT a surfaced soul.md — so the actual boot doc does not visibly match the manifest's `presence: injected` soul.md/operating.md contract. Whether injection is silently happening at the system-prompt layer is not determinable from the transcript; recommend an out-of-band check (inspect the rendered launch context) to close criterion #1.

## Proposed instruction edits (concrete)
1. **Add a presence-refresh line to `operating.md` `## Bounds`** (directly addresses criterion #5 / epic hk-bl93n):
   > `Re-run `harmonik comms join` on a ≤90s cadence (and on every restart / mid-session stream death) so `comms who` never shows you `stale` while alive — a stale entry makes the captain treat you as dead.`
2. **`operating.md` "On wake" step 3** — make the refresh, not just the one-shot join, load-bearing. Change step 3 to:
   > `3. `harmonik comms join` (announce presence — and re-announce on the ≤90s cadence below), then arm `harmonik comms recv --follow --json`.`
3. **Observability (not a behavior edit):** verify whether `soul.md` (identity + `parent_intent: captain` graft) is actually injected into the launch context. This boot showed zero soul.md surface; if injection is real it is invisible to the transcript, and criterion #1 cannot be scored on future passes without an explicit "confirm your soul/identity source" line. Suggest adding to `operating.md` "On wake" step 2:
   > `2. Confirm `$HARMONIK_AGENT == crew_name`, and state your identity source (soul.md graft) in the boot status so provenance is observable.`
