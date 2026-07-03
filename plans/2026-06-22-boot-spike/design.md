# Boot Spike Design — 2026-06-22

## What the spike is

The 24h analysis shows $1,025 spent in the 08:00 MST hour — all orchestrators
booting simultaneously at session start. This is the single most expensive hour
in the 24h window, representing 25% of total fleet spend in 60 minutes.

### Mechanism

1. Captain launches (`harmonik start captain`) → `claude --remote-control --session-id <uuid>`
   with a stable session ID.
2. The seed prompt delivered to the captain's pane is one line:
   `"Please read <handoffPath> and run /session-resume on it, then begin your operating loop."`
3. `/session-resume` triggers a full context load (all skills files: `captain/STARTUP.md`,
   `captain/SKILL.md`, `orchestrator-rules/SKILL.md`, `agent-comms/SKILL.md`,
   `beads-cli/SKILL.md`, `harmonik-dispatch/SKILL.md`, plus `HANDOFF.md`,
   `captain-lanes.md`, `project.yaml`) — each read generates `cache_creation` tokens.
4. Crew boots follow immediately: each crew reads `crew-launch/SKILL.md` + its mission
   file and calls `harmonik comms join`, posting a boot status.
5. All 5–8 crews boot in the same minute, creating a simultaneous `cache_creation`
   spike across all Opus sessions. At $18.75/M for cache_creation (Opus), the spike
   is dominated by this initial cache warm-up.

### Why it costs so much

- Opus cache_creation is $18.75/M tokens — 12.5x the read cost ($1.50/M).
- The captain + crew sessions each create a large stable prefix from scratch at boot.
- Simultaneous boot multiplies this: 8 sessions × large prefix = $1,025 in one hour.
- After the first boot, sessions re-use the cached prefix (95.6% cache-read rate),
  so costs drop to $100-250/hour for the rest of the day.

---

## Lever 1 — Staggered boot (highest ROI, low effort)

**What:** Space captain + crew boots by 2–5 minutes rather than launching all
simultaneously. The first captain boot caches the shared system prompt; crew boots
that follow can hit the warm cache rather than each creating their own cold prefix.

**Mechanism:** `harmonik start captain` already exists and is sequential. The captain
currently launches all crews during its Step 5 boot loop. Staggering means: launch
captain, wait for `comms who` confirmation (~30-60s), then launch crews one at a time
with a 2-3 minute gap between each. The captain's STARTUP.md Step 5 can be annotated
to do this; no Go code change required.

**Implementation:** Pure convention change. Add to STARTUP.md Step 5: "launch crews
ONE AT A TIME with a 2-minute wait between each; confirm comms-online before launching
the next. Do NOT batch `crew start` calls."

**Rough $ impact:** The simultaneous spike is ~$800 above a staggered baseline (the
$225/hour sustained rate is the reference). Staggering 8 crews over 16 minutes instead
of 1 minute cuts the spike by ~$600-800 per fleet boot. At one fleet boot/day, this
saves ~$600-800/day for no build cost.

**Risk:** Low. Increases boot wall-clock time by ~16 min. Crews start working later,
but the fleet is still fully established within 20 minutes. No Go code change.

---

## Lever 2 — Slim the captain seed (medium ROI, medium effort)

**What:** The captain's `/session-resume` reads 6 skills files + 3 context files at
boot, totaling ~2,000+ lines of text. Many of these are loaded in full but only
sections are needed at boot (e.g., all of `agent-comms/SKILL.md` is read, but the
captain only needs comms join/send/recv patterns). A "slim boot" variant loads only
tier-3 + tier-2 + the boot digest, deferring full skill reads to on-demand.

**Mechanism:** STARTUP.md already has a lean resume path (keeper-restart resume vs
cold boot). The cold boot path loads more than needed. A slim boot protocol:
1. Load tier-3 (`project.yaml`) + tier-2 (`captain-lanes.md`) — small files, stable.
2. Run `captain-boot-digest.sh` for live state.
3. Defer full `agent-comms`, `beads-cli`, `harmonik-dispatch` skill reads until
   the specific operation is needed.

The captain skill already says (M5/hk-039z): "Does NOT boot-read: AGENT_INDEX.md,
STATUS.md, product/docs/ knowledge base." The gap is that the full skill body for
`orchestrator-rules`, `agent-comms`, `beads-cli`, and `harmonik-dispatch` are still
loaded. These four together are likely 3,000-5,000 tokens of cache_creation.

**Rough $ impact:** Harder to estimate without token-level instrumentation (GAP-3 in
the 24h analysis — no model field on run events). Rough: if 4 skill files × 1,500
tokens each × 8 sessions × Opus $18.75/M = ~$0.90/boot at session-start alone —
across 20 keeper restarts/day per session that adds up to ~$18/day. Eliminating one
cold boot load of these files: ~$7-15/day. Lower ROI than staggered boot, but
compounds with every restart.

**Risk:** Medium. A captain that defers skill reads may miss a pattern early in its
session. Need to identify the minimum viable boot context. Requires STARTUP.md
rewrite + captain discipline to not load eagerly.

---

## Lever 3 — Lazy crew boot (conditional ROI, medium effort)

**What:** Do not boot all crew sessions at captain start. Instead, start crews
on-demand as lanes are ready. If kerf next shows only 2 ready lanes, only boot 2
crews. The captain's 12-minute health tick (`/loop 12m`) staffs new crews as ready
beads emerge.

**Mechanism:** The captain's STARTUP.md Step 5 already says "one crew per lane" and
"fill every non-conflicting free slot." The gap is that the captain typically boots
all 5-8 crews before knowing if their lanes have ready beads. Lazy boot: Step 5 first
runs `br ready --limit 0` and `kerf next`, then only starts crews for lanes with
ready work. Idle/blocked lanes don't get a crew at boot.

**Implementation:** Convention change to STARTUP.md Step 5 + captain behavior.
Add: "BEFORE `crew start`: check `br ready --limit 0` for this lane's epic. If no
ready beads, mark the lane as `PARKED — no ready beads` and do NOT boot a crew yet.
The 12-minute health tick staffs the lane when ready beads appear."

**Rough $ impact:** Depends on how many lanes are idle at boot. In the 24h window,
some lanes appear to have had thin ready-bead availability (the $9 at 00Z suggests
most lanes drained). If 3 of 8 crews boot unnecessarily at session start: 3 × Opus
cache_creation for skills load × $18.75/M ≈ $50-150 saved per fleet boot per day.
Lower ROI than staggered boot because the per-boot savings are smaller, and if lanes
later get ready beads the crew still needs to boot (just later, into warm cache).

**Risk:** Low-medium. Risk of missing a ready-bead window if the health tick is the
only staffing mechanism (12m lag). Mitigated: operator can manually `crew start` if
urgency requires it. The bigger risk is captain complexity — "is this lane ready" adds
judgment at boot.

---

## Summary — ranked by ROI

| Rank | Lever | Effort | Est. 24h saving | Bead |
|---|---|---|---|---|
| 1 | Staggered boot (2-3 min gap between crew starts) | XS (no build) | ~$600-800/boot | tbd |
| 2 | Slim captain seed (defer non-essential skill reads) | S (STARTUP.md rewrite) | ~$15-50/day | tbd |
| 3 | Lazy crew boot (only start crews with ready beads) | S (STARTUP.md + captain behavior) | ~$50-150/day | tbd |

**Priority note:** Staggered boot has by far the highest single-event ROI ($600-800
saved per fleet boot) with zero build cost. It is the highest-priority action.
Levers 2 and 3 are lower absolute savings but compound across every restart (20+/day).
Together with the Crew Sonnet default (GAP-4 from the 24h analysis), staggered boot
+ lazy crew boot could reduce daily fleet cost by $700-1,000.

---

## Implementation notes

### Staggered boot

Change to `.claude/skills/captain/STARTUP.md` Step 5:

```
5c — Start the crew (one call per lane; distinct name AND distinct queue):
  ...
  STAGGER RULE: After each `crew start`, wait for `comms who` to show the crew
  online (~30-60s), THEN wait an additional 2 minutes before launching the next
  crew. Do NOT batch crew starts. The 2-min gap lets each crew's cache prefix
  warm independently without competing with simultaneous cache_creation across
  all sessions.
```

This is a STARTUP.md edit. Also needed: embedded asset sync (`cp` to
`cmd/harmonik/assets/skills/captain/STARTUP.md`).

### Slim captain seed

Two-phase change:
1. Audit which skills sections are actually needed at cold-boot vs on-demand.
   (Candidate for deferral: full `agent-comms` SKILL.md body, full `harmonik-dispatch`
   SKILL.md body — only the daily-loop summary is needed at boot, not the full surface.)
2. Rewrite STARTUP.md Step 1 to explicitly list which files to read at cold boot vs
   load on-demand when the specific operation comes up.

This is a research + STARTUP.md edit task.

### Lazy crew boot

Add to STARTUP.md Step 4 / Step 5:

```
BEFORE crew start for a lane: run `br ready --limit 0 --label <epic_label>` to
check if ready beads exist under this epic. If the result is empty AND no
in-flight beads exist, mark the lane PARKED and skip the crew start. The 12m
health tick re-checks and staffs when beads become ready.
```

Also add: the captain's health-tick loop prompt needs a "check PARKED lanes and
staff them if ready beads now exist" clause.

---

## Source references

- Boot spike analysis: `plans/2026-06-22-token-usage-audit-tooling/24h-run-analysis.md` §6
- Seed prompt code: `internal/daemon/crewstart.go` `pasteCrewMission()` line 385
- Captain cold boot: `.claude/skills/captain/STARTUP.md` §Step 1, §Step 5
- Crew boot: `.claude/skills/crew-launch/SKILL.md` §Boot sequence
- Runner/planner gap analysis: `plans/2026-06-22-token-usage-audit-tooling/leanfleet-runner-planner-gaps.md`
- Crew Sonnet default (complementary lever): GAP-4 in 24h analysis
