# Doc/Instruction Audit — `.harmonik/context/` Deep-Dive

**Date:** 2026-06-20
**Scope:** `.harmonik/context/project.yaml` (prime concern-mixing example) + `.harmonik/context/captain-lanes.md`
**Author:** investigation sub-agent

---

## 1. WHO reads these files, and WHEN

### Finding: 100% LLM-consumed, ZERO machine-parsed.

Grep across `cmd/` + `internal/` (`.go` files) returns **no hits** for `project.yaml`, `captain-lanes`, or `.harmonik/context`. The Go daemon never opens, parses, or validates these files. Despite the `.yaml` extension, `project.yaml` is **not config** in any machine sense — nothing reads `phase:` or `forbidden_actions:` as structured data. It is a prose document that happens to be YAML-shaped.

Grep across `.claude/skills/crew-launch/` returns **no hits** — crews never read either file.

All references live in **one consumer**: the captain skill. Two mirrored copies exist and stay in sync:
- Live: `/Users/gb/github/harmonik/.claude/skills/captain/`
- Embedded asset mirror: `/Users/gb/github/harmonik/cmd/harmonik/assets/skills/captain/` (shipped into the binary; re-synced per hk-039z)

### Read path (captain boot — STARTUP.md):
- **Step 0a** — `cat .harmonik/context/project.yaml` → "Encodes: phase, forbidden_actions, locked_decisions." Read BEFORE skills or handoff.
- **Step 0b** — `cat .harmonik/context/captain-lanes.md` → "Encodes: active_lanes table, operator_initiatives, parked, pipeline."
- **Step 2** ground-truths every live claim these files carry (daemon up, crews online, epics still assigned). So tier-2 lane claims are treated as a CACHE to be verified, not gospel.

### Write path (captain shutdown — SHUTDOWN.md):
- **Step 5a** — writes `captain-lanes.md` as "the SINGLE source of record" for lane state (M9/hk-039z), explicitly NOT SKILL.md §A. `project.yaml` has **no documented writer** — it's hand-edited "when phase changes, new decisions are locked, or operator policies shift."

### Declared tier model (from the file headers):
- **Tier-3** = `project.yaml` — "weeks cadence" — phase / locked decisions.
- **Tier-2** = `captain-lanes.md` — "days cadence" — lane registry.
- **Tier-1** = `HANDOFF.md` — per-session ephemeral state.

**The core defect:** `project.yaml`'s own header says "Do NOT record ephemeral state here — that belongs in HANDOFF.md (tier1)" and "weeks cadence" — yet the file is **dominated** by `operator_directives`, `priority_order`, and `paused_initiatives` blocks all stamped **2026-06-19** (i.e. yesterday — a days-or-faster cadence). The file violates its own stated contract.

---

## 2. Section-by-section decomposition — `project.yaml`

| # | Section / chunk | Lines | Content | TIER | Belongs in |
|---|---|---|---|---|---|
| 1 | Header comments | 1–4 | "Tier-3, weeks cadence, captain reads on boot, no ephemeral state" | meta | keep (but fix — file violates it) |
| 2 | `phase: operational` | 6 | enum: operational/bootstrap/maintenance/winding-down | **LONG-TERM / PRIORITIES** (closest to CONFIG, but no machine reads it) | stays in project.yaml |
| 3 | `forbidden_actions` | 8–14 | NEVER-without-confirmation rules (force-push main, delete active queue, rm live worktree, crew→main submit, widen keeper band) | **BEHAVIORAL DIRECTIVE** | captain SKILL.md / AGENTS.md (these are "how to behave" guardrails, not state) |
| 4 | `locked_decisions` | 16–24 | 6 architectural decisions (single-daemon, worktrees+merges, twin-binaries, tmux, no-verifier, beads-owns-transitions) | **LONG-TERM / PRIORITIES** | stays — but it's a DUPLICATE of STATUS.md §Decisions (single-source risk) |
| 5 | `operator_directives` | 26–59 | 3-day scale-out push: one-at-a-time retired, scale out over sessions, DOT/Sonnet-triple-review HARD bar, captain-orchestrates-not-does, daemon-takes-precedence, 300k context cap + watchdog, flaky internet | **MIXED — mostly MEDIUM-TERM / PROCESS + BEHAVIORAL** with a time-boxed "3-day" stamp | SPLIT (see §4) — time-boxed campaign directives ≠ tier-3 weeks-cadence |
| 6 | `priority_order` | 61–67 | Ranked lane staffing order (remote-substrate #1, daemon-reliability #2, token/leanfleet #3, flywheel #4, then keeper/logmine/churn) | **SHORT-TERM / OPERATIONAL** (this is `kerf next` territory; changes session-to-session) | captain-lanes.md (tier-2) — it's lane/staffing state |
| 7 | `paused_initiatives` | 68–71 | codex PAUSED, gh-bugs constrained | **SHORT-TERM / OPERATIONAL** | captain-lanes.md (tier-2) |

### Tier tally
- **BEHAVIORAL DIRECTIVE:** forbidden_actions (§3), half of operator_directives (§5).
- **LONG-TERM / PRIORITIES:** phase (§2), locked_decisions (§4).
- **MEDIUM-TERM / PROCESS:** half of operator_directives (§5 — the campaign posture).
- **SHORT-TERM / OPERATIONAL:** priority_order (§6), paused_initiatives (§7).
- **CONFIG (machine-read):** NONE. `.yaml` extension is misleading.

**The concern-mixing verdict:** a single file blends (a) permanent behavioral guardrails, (b) permanent architectural decisions, (c) a time-boxed 3-day campaign posture, and (d) live session-to-session lane priorities. Four different cadences and three different rightful homes, in one tier-3-labeled file. The operator's "priorities/tasks + how-we-process + more all mixed into one" is exactly correct.

---

## 3. Section-by-section decomposition — `captain-lanes.md`

| # | Section | Lines | Content | TIER | Belongs in |
|---|---|---|---|---|---|
| 1 | Header | 1–4 | "Tier-2, days cadence, single source of record for lanes, ephemeral→HANDOFF" | meta | keep |
| 2 | `active_lanes` table | 6–13 | 0-crew lean park snapshot + dated logmine STOOD DOWN + SALVAGED-this-session narrative | **SHORT-TERM / OPERATIONAL** — and worse, the prose bullets (12–13) are TIER-1 ephemeral ("this session", run IDs) leaking into tier-2 | table stays (tier-2); the "this session" salvage narrative → HANDOFF.md |
| 3 | `operator_initiatives` | 15–20 | keeper-redesign, hk-rl4b sleep/wake, leanfleet, token-burn, codex, remote-substrate, flywheel — ranked, mostly not staffed | **MEDIUM-TERM / PROCESS** (initiative roadmap) | mostly OK for tier-2, but overlaps project.yaml priority_order (§6) — DEDUP |
| 4 | `parked` | 22–27 | daemon-reliability detail, stranded in_progress beads pending hk-53p3, hk-tagp/main paused, hk-rty1 | **SHORT-TERM / OPERATIONAL** (bead-level, changes fast) | tier-2 OK but heavily bead-ID-specific — verify-not-trust |
| 5 | `next_lane_pipeline` | 29–35 | Priority-ordered staffing pipeline | **SHORT-TERM / OPERATIONAL** | tier-2 — **but this is a 3rd copy of the same priority ranking** (also in project.yaml §6 priority_order AND operator_initiatives §3) |

**captain-lanes.md verdict:** healthier than project.yaml (it IS lane state, which is its job), but suffers two problems: (1) **tier-1 ephemeral salvage narrative** ("SALVAGED this session," run IDs) bleeds in — that's HANDOFF.md content; (2) the **priority ranking is duplicated three times** across this file's two sections plus project.yaml's `priority_order`.

---

## 4. Proposed split

### Principle
Separate by **cadence + rightful owner**, and stop pretending `.yaml` = config when nothing machine-reads it. Three destinations:
- **AGENTS.md / captain SKILL.md** — permanent behavioral rules (how to behave).
- **project.yaml** — slow, near-permanent project facts (phase + locked decisions only).
- **captain-lanes.md** — all live lane/priority/staffing state (one ranking, one place).
- **HANDOFF.md** — per-session narrative (already exists; reclaim what leaked out).

### `project.yaml` → SHRINK to genuine tier-3 (weeks cadence)

Keep ONLY:
```yaml
phase: operational
locked_decisions:        # mirror of STATUS.md §Decisions — or better, just point to it
  - ...
```
**Move OUT:**
- `forbidden_actions` → captain **SKILL.md** (or a `### Forbidden actions` block in AGENTS.md). These are behavioral guardrails, not state; they belong where behavior is defined and reviewed.
- `operator_directives` → **SPLIT by lifespan:**
  - The *permanent* rules (DOT/triple-review is the bar, captain orchestrates-not-does, daemon-takes-precedence, 300k cap) → captain SKILL.md as standing operating contract.
  - The *time-boxed campaign* rules ("3-day scale-out push," "one-at-a-time RETIRED for now," "internet flaky as of 2026-06-19") → a clearly-dated **`## Current campaign`** section in captain-lanes.md (tier-2), since they expire and override handoffs only for a window.
- `priority_order` → captain-lanes.md (merge into the single ranking, see below).
- `paused_initiatives` → captain-lanes.md `parked` / a `## Paused` section.

**Consider:** dedup `locked_decisions` against STATUS.md §Decisions — pick STATUS.md as canonical and have project.yaml reference it, OR keep the copy but add a "source: STATUS.md §Decisions" note so drift is catchable.

### `captain-lanes.md` → become the SINGLE live-state file

- **Collapse the 3 priority rankings into ONE** `## Priority ranking` section. Today the same order lives in project.yaml `priority_order`, captain-lanes `operator_initiatives`, AND `next_lane_pipeline`. One ranked list, period.
- **Evict tier-1 ephemera:** the "SALVAGED this session / run IDs / dated stood-down narrative" bullets (lines 12–13, parts of `parked`) move to HANDOFF.md. captain-lanes.md keeps the durable lane TABLE + initiative roster + pause list, not the play-by-play.
- Add the time-boxed **`## Current campaign`** block (from the evicted operator_directives) here, since it's a days-cadence override.

### Net result
- **project.yaml:** ~10 lines — phase + locked_decisions. Truly weeks-cadence. Matches its own header.
- **captain SKILL.md / AGENTS.md:** gains `forbidden_actions` + permanent operator directives as reviewable behavioral contract.
- **captain-lanes.md:** one priority ranking, one campaign block, the lane table, the parked list — all genuinely tier-2.
- **HANDOFF.md:** reclaims the per-session salvage narrative that was leaking up into tier-2/tier-3.

### Migration note
Because two mirrored copies of the captain skill exist (`.claude/skills/captain/` and `cmd/harmonik/assets/skills/captain/`), any rule moved INTO the skill must be applied to BOTH and re-synced (the hk-039z embed-mirror discipline). STARTUP.md Step 0a/0b and SHUTDOWN.md Step 5a comments must be updated to reflect the new, narrower file contents.
