# I1 — Captain Boot Context Cost Budget

**Date:** 2026-06-20
**Investigator:** captain-economy I1
**Complaint:** "When the captain starts up, after the procedure we're already at over 100k context and we only want to get to 200k."

Token estimates use ~1 token ≈ 4 chars (measured via `wc -c`). The CLAUDE.md / AGENTS.md project file (~symlink) and the global `~/.claude/CLAUDE.md` are loaded by the harness before STARTUP.md even runs and are not counted in the "procedure" below, but they are noted as fixed overhead.

---

## Line-item budget

### A. Files the documented boot procedure READS

| # | Item | Source instruction | chars | ~tokens | Notes |
|---|------|--------------------|-------|---------|-------|
| 1 | `.harmonik/context/project.yaml` | STARTUP.md §0a (`cat`) | 4,796 | 1,199 | tier-3 |
| 2 | `.harmonik/context/captain-lanes.md` | STARTUP.md §0b (`cat`) | 3,584 | 896 | tier-2 |
| 3 | `.claude/skills/captain/SKILL.md` | STARTUP.md §1.1 | 42,848 | **10,712** | biggest single file |
| 4 | `.claude/skills/captain/STARTUP.md` | STARTUP.md §1.2 (this file) | 31,397 | **7,849** | |
| 5 | `docs/orchestrator-rules.md` | STARTUP.md §1.3 | 12,749 | 3,187 | |
| 6 | `HANDOFF.md` | STARTUP.md §1.4 | 4,399 | 1,099 | |
| 7 | `HANDOFF-captain.md` | implied on restart-now resume (STARTUP.md §6, SKILL.md §10) | 8,649 | 2,162 | present in repo now |
| 8 | `AGENT_INDEX.md` | CLAUDE.md "Start here" reading order | 15,354 | 3,838 | |
| 9 | `STATUS.md` | CLAUDE.md "Start here" | 7,417 | 1,854 | |
| 10 | `TASKS.md` | CLAUDE.md "Start here" | 13,371 | 3,342 | |

**Subtotal A (explicit reads): ~36,138 tokens** (counting HANDOFF-captain once; STARTUP.md itself counted since it is consumed as the runbook).

### B. Composed skills auto-injected by the harness

The captain skill's frontmatter and CLAUDE.md instruct loading `agent-comms`, `beads-cli`, `harmonik-dispatch` "alongside" the captain skill, and the keeper skill is named in the captain-skill ecosystem. In Claude Code these SKILL.md bodies are injected on skill activation, so they land in context whether or not the LLM "reads" them.

| # | Skill SKILL.md | chars | ~tokens |
|---|----------------|-------|---------|
| 11 | `agent-comms` | 10,224 | 2,556 |
| 12 | `beads-cli` | 7,134 | 1,783 |
| 13 | `harmonik-dispatch` | 9,210 | 2,302 |
| 14 | `keeper` | 27,118 | **6,779** |
| 15 | `crew-launch` (referenced, often loaded) | ~ (not read here) | ~2,500 est |

**Subtotal B (composed skills): ~13,420 tokens** (skills 11–14; +~2.5k if crew-launch loads).

### C. Live command output the captain must process (Steps 2, 4, 5d, 6)

The boot-digest **actual runtime output measured today = 19,525 chars ≈ 4,881 tokens** (one `captain-boot-digest.sh` run: daemon status, comms who, crew list, tmux fleet, paused queues, recent comms, ready beads, open epics, kerf next, kerf map).

If the captain instead runs the individual Step 2a–2g + Step 4 commands raw (the digest is optional), output is comparable-to-larger (~5–8k tokens) plus per-turn tool-call framing overhead across ~12 separate calls.

| # | Item | ~tokens |
|---|------|---------|
| 16 | Boot-digest output (or equivalent raw Step 2+4 commands) | ~4,881 |
| 17 | Step 5d verification: `comms who`, `capture-pane` (~25 lines × N crews), `comms log`, `queue status`, events.jsonl grep | ~1,500–3,000 (scales with crew count) |
| 18 | Step 5a mission-file writes (read-back of what it wrote) + spawn exit handling | ~500–1,000 |
| 19 | Tool-call/turn framing overhead across the procedure's ~20–30 calls | ~3,000–5,000 |

**Subtotal C (live output + framing): ~10,000–14,000 tokens.**

### D. Fixed harness overhead (not "procedure" but always present)

| Item | chars | ~tokens |
|------|-------|---------|
| Project `CLAUDE.md`/`AGENTS.md` | ~9,000 | ~2,250 |
| Global `~/.claude/CLAUDE.md` | ~13,000 | ~3,250 |
| Auto-memory `MEMORY.md` index (large) | ~12,000 | ~3,000 |
| System prompt + tool schemas | — | ~10,000–15,000 |

**Subtotal D: ~18,000–23,000 tokens** (present before the runbook starts).

---

## Totals

| Bucket | ~tokens |
|--------|---------|
| A — explicit file reads | 36,000 |
| B — composed skills | 13,400 |
| C — live command output + framing | 12,000 |
| D — fixed harness overhead | 20,000 |
| **TOTAL estimated boot cost** | **~81,000** |

This lands just under the operator's "over 100k" — consistent given (a) my estimates are conservative chars/4, (b) reasoning/response tokens the captain itself emits while working through the 6 steps add easily 15–30k, and (c) crew-count scaling in bucket C. **The operator's "over 100k after the procedure" is fully explained: ~80k of ingested material + ~20–30k of the captain's own narration/reasoning across the multi-step runbook.**

---

## 1. Biggest line items

1. **captain/SKILL.md — 10,712 tok** (772 lines). Largest single artifact. Heavily redundant with STARTUP.md (both restate §0 autonomy, §8 surface-and-await, §10 keeper/WARN procedure, the boot sequence).
2. **captain/STARTUP.md — 7,849 tok** (567 lines). The runbook is itself huge and restates large blocks of SKILL.md verbatim.
3. **keeper/SKILL.md — 6,779 tok** (484 lines). Auto-injected; the captain only needs ~10 lines of it at boot (band values, restart-now, await-ack).
4. **AGENT_INDEX + STATUS + TASKS — 9,034 tok combined.** Read in full every boot per CLAUDE.md, but Step 2 ground-truths most of what they claim.
5. **SHUTDOWN.md — 4,608 tok.** Not part of boot, but the captain skill ecosystem can pull it in.

## 2. What is REDUNDANT

- **STARTUP.md ⇄ SKILL.md duplication (~5–6k tokens of pure overlap).** The keeper WARN text is quoted **verbatim three times** (STARTUP.md §6 line 345, SKILL.md §10 lines 668 & 668-area, SHUTDOWN.md line 378). The §0 autonomy bright-line, the §8 four-case list, the boot-sequence contract, and the "bad boot" cautionary tale all appear in BOTH STARTUP.md (lines 22–26, 196–242) and SKILL.md (§0, §0.5 lines 161–197).
- **STATUS.md/TASKS.md/AGENT_INDEX.md vs. tier-2/tier-3 + Step 2 ground-truth.** STARTUP.md §0a/§0b explicitly say project.yaml + captain-lanes.md encode phase/locked-decisions/active-lanes, and Step 2 re-derives live state — yet CLAUDE.md still has the captain read the full AGENT_INDEX→STATUS→TASKS chain (9k tok) whose live claims Step 2 overrides anyway.
- **Boot-digest output vs. raw Step 2/4 commands.** The digest (§2 lines 103–112) is designed to REPLACE the raw commands, but STARTUP.md still prints all raw commands inline (Step 2a–2g, Step 4) — a captain that runs both pays twice (~5k duplicated).
- **HANDOFF.md + HANDOFF-captain.md** partially overlap (both narrate prior-session state, 3,261 tok combined) and STARTUP.md says ground-truth overrides them anyway.

## 3. What could be OFFLOADED to a deterministic script/digest

- **Steps 2 + 4 live discovery → already offloaded to `captain-boot-digest.sh`** (one ~4,881-tok digest instead of ~12 raw command turns). This is the right pattern; extend it.
- **Step 5d verification → a `captain-verify-fleet.sh`** that runs the comms-who / capture-pane / queue-status / events-grep checks per crew and emits a one-line-per-crew PASS/FAIL table (saves ~1.5–3k tok of raw pane dumps).
- **tier-3/tier-2 + STATUS/TASKS/AGENT_INDEX → a single `captain-context-digest`** that emits only: current phase, locked decisions, the active-lanes table, and the top-N TASKS lines — instead of the captain reading 4 full files (~12k → ~2k).
- **keeper band/restart facts → a 15-line `keeper-cheatsheet`** injected instead of the full 6,779-tok keeper SKILL.md at captain boot (the captain is not a keeper operator; it needs warn-band values + restart-now + await-ack only).

## 4. Is the TA2 boot-digest WIRED IN?

**Partially, and weakly.** `captain-boot-digest.sh` exists at `~/.claude/captain-tools/captain-boot-digest.sh` and **is referenced** in STARTUP.md as a "One-call shortcut" (Step 2 lines 103–112) and again in Step 4 (lines 200–205). BUT:

- It is framed as an **optional shortcut** ("run the boot digest first … then skip to Step 3"), not the mandated path. The full raw Step 2a–2g and Step 4 command blocks are still printed inline immediately below it (lines 117–141, 207–212), so a literal-minded captain runs BOTH.
- It is **not wired into `captain-launch.sh`** — the launch script mints the session-id and arms the keeper but does not pre-run the digest or inject its output, so the digest only fires if the LLM chooses to.
- Net: the offload exists but the redundant raw commands remain in the runbook, defeating ~half the savings. It is "sitting half-used," not fully wired.

## 5. Concrete cuts to get boot well under 100k

Ordered by tokens saved per unit effort:

1. **De-duplicate STARTUP.md ⇄ SKILL.md (save ~5–6k).** Make SKILL.md the single home for §0 autonomy / §8 four-case / keeper-WARN text; have STARTUP.md *reference* them (`see SKILL.md §0`) instead of re-quoting. Cut the cautionary "bad boot" narrative to one line. STARTUP.md lines 22–26, 196–242, 342–369 are the prime targets.

2. **Stop full-reading AGENT_INDEX + STATUS + TASKS at captain boot (save ~7–9k).** The captain is an orchestrator, not an implementer; its needs are phase + locked decisions + lane table + open backlog — all in project.yaml/captain-lanes.md + the digest's kerf-next/br-ready sections. Replace the CLAUDE.md reading-order mandate for the captain with "digest only; read STATUS/TASKS sections on demand."

3. **Inject a keeper cheatsheet, not the full keeper SKILL.md (save ~6k).** Captain boot needs ~15 lines of keeper facts; drop the 484-line skill from the captain's auto-load set.

4. **Make the boot-digest MANDATORY and delete the inline raw command blocks (save ~4–5k + avoids double-run).** Replace STARTUP.md Step 2a–2g and Step 4 raw commands with "run `captain-boot-digest.sh`, read the digest; rerun an individual command only if a section needs a deeper look." Optionally have `captain-launch.sh` pre-run the digest to a file the captain reads.

5. **Add `captain-verify-fleet.sh` for Step 5d (save ~1.5–3k, scales with crews).** Emit one PASS/FAIL line per crew instead of raw capture-pane dumps.

**Projected post-cut boot cost:** cuts 1–4 remove ~22–26k of ingested tokens, dropping bucket A+B from ~49k to ~25k and total ingested boot cost from ~81k to **~55–60k** — comfortably under 100k and leaving 140k+ of the 200k window for actual orchestration.
