# Crew Missions Audit — `.harmonik/crew/` cleanup (2026-06-20)

Scope: stale mission artifacts under `.harmonik/crew/`, NOT the crew mechanism (which works fine).

## Ground truth — live crews (have a `.json` registry, 2026-06-20)

| crew | session_id | queue | epic (in json) | started_at |
|------|-----------|-------|----------------|-----------|
| chani   | 33947d58… | chani-q   | "" (empty) | 2026-06-20 14:41Z |
| irulan  | b46d6340… | irulan-q  | "" (empty) | 2026-06-20 14:46Z |
| jamis   | cf578826… | jamis-q   | "" (empty) | 2026-06-20 14:51Z |
| logmine | 933c65cc… | logmine-q | "" (empty) | 2026-06-20 14:46Z |
| paul    | 8eb1eff2… | paul-q    | "" (empty) | 2026-06-20 17:13Z |

Note: **every live registry has `epic:""`** — the registry no longer carries epic attribution (that now lives in the `br show <epic> --assignee` mirror per the captain skill Gap-1). So "matching live json" = name match only.

The whole `.harmonik/crew/` tree is **gitignored** — `git log` shows nothing for these files (one stray exception: `missions/ops-monitor.md` was committed by the `leanfleet LF-B` commit). Verdicts rely on **mtime + content + epic status**.

## Mission file table (20 files)

| file | crew | live json? | mtime | epic / lane | epic status | verdict | rec |
|------|------|-----------|-------|-------------|-------------|---------|-----|
| audit.md | audit | no | 06-17 | hk-bsdr (token-burn analysis) | open | STALE | ARCHIVE |
| chani-smoke.md | chani-smoke | no | 06-09 | hk-h0249 (T10/T15 sandbox smoke) | NOT_FOUND | SMOKE-TEST-ARTIFACT | DELETE |
| chani.md | chani | **YES** | 06-19 | hk-rs-phase1-qfn1 (remote-substrate worker) | open | LIVE | KEEP |
| codexcrew.md | codexcrew | no | 06-16 | hk-0639 (codex-harness live-test) | open | STALE | ARCHIVE |
| duncan.md | duncan | no | 06-10 | hk-w4tmz (codex-harness lane) | closed | DEAD | DELETE/ARCHIVE |
| example-handoff.md | alpha | no | 06-09 | hk-tigaf (named-queues, "alpha" sample) | closed | TEMPLATE | KEEP (rename) |
| feyd.md | feyd | no | 06-15 | hk-3js5m (daemon reliability) | closed | DEAD | DELETE |
| gurney.md | gurney | no | 06-12 | hk-3js5m (daemon reliability) | closed | DEAD | DELETE |
| harah.md | harah | no | 06-17 | hk-9gkwa (churn/hunt lane) | open | STALE | ARCHIVE |
| irulan.md | irulan | **YES** | 06-17 | hk-kwyv (GH bug-fix lane) | open | LIVE | KEEP |
| jamis.md | jamis | **YES** | 06-20 | "" (stand-by, no work) | n/a | LIVE | KEEP |
| korba.md | korba | no | 06-13 | hk-w02d (keeper hardening) | closed | DEAD | DELETE |
| kynes.md | kynes | no | 06-14 | hk-0oca (flywheel design) | open | STALE | ARCHIVE |
| leto.md | leto | no | 06-12 | hk-rqq (scheduler primitive) | closed | DEAD | DELETE |
| liet.md | liet | no | 06-12 | hk-cq1 (auto_status design) | closed | DEAD | DELETE |
| logmine.md | logmine | **YES** | 06-17 | hk-mhmaw (logmine harvest) | open | LIVE | KEEP |
| ops-monitor.md | ops-monitor | no | 06-17 | hk-itoc (leanfleet ops watchdog) | open | STALE | ARCHIVE |
| paul.md | paul | **YES** | 06-17 | hk-rl4b (fleet sleep/wake) | open | LIVE | KEEP |
| stilgar.md | stilgar | no | 06-13 | hk-nboa (fleet-portability) | closed | DEAD | DELETE |
| thufir.md | thufir | no | 06-17 | hk-3js5m (daemon reliability) | closed | DEAD | DELETE |

### Verdict tallies
- **LIVE: 5** — chani, irulan, jamis, logmine, paul (all have a matching live `.json`).
- **DEAD: 9** — duncan, feyd, gurney, korba, leto, liet, stilgar, thufir (closed epic, no live json), plus chani-smoke is separately a smoke artifact.
- **STALE: 5** — audit, codexcrew, harah, kynes, ops-monitor (epic still open, but no live crew owns it now; the lane may be re-staffed under a new name later — these are re-runnable history, not garbage).
- **SMOKE-TEST-ARTIFACT: 1** — chani-smoke (hk-h0249 doesn't even exist anymore).
- **TEMPLATE: 1** — example-handoff.md (the canonical sample mission; keep, but rename to make its template role explicit, e.g. `_TEMPLATE.md` or `example.md`).

Note on naming collision: `chani-smoke.md` is NOT chani's mission — it is a throwaway sandbox crew that happens to share the "chani" prefix. Easy to mistake for live. DELETE.

## Other artifacts

### `*.json` registry files
5 files: chani, irulan, jamis, logmine, paul. **No orphans** — every live json has a matching mission, and all 5 live missions have a json. No json points at a missing mission.

### `duncan-HANDOFF.md` (repo `.harmonik/crew/` root, 06-10)
HELD/idle handoff for the now-**closed** codex-harness epic hk-w4tmz, blocked on hk-bqf1q (long since deployed). **DEAD → DELETE.** Also note the location inconsistency: every other handoff lives in `handoffs/`, this one sits at the crew root.

### `handoffs/` (2 files)
- `codexcrew.md` (06-09) — "DONE & clean", codex-harness research/design plan-jig complete. DEAD → ARCHIVE/DELETE.
- `gurney-resume.md` (06-12) — resume handoff for gurney on closed epic hk-3js5m. DEAD → DELETE.

Both are completed-lane handoffs for closed/dead lanes. No live crew reads them.

### `designs/` (3 files)
- `build-brief-hk-0es.md` (06-12) — build brief for `harmonik schedule` (hk-0es / epic hk-rqq). hk-rqq is **closed** → the scheduler shipped. Historical. ARCHIVE.
- `doc-overhaul-plan.md` (06-13) — 4-agent doc-audit plan. Historical planning artifact. ARCHIVE (or move under `plans/`).
- `scheduler-primitive.md` (06-12) — design note for `harmonik schedule`, "DRAFT — surfaced to captain." Epic shipped. ARCHIVE.

These are real design documents that ended up under the *gitignored* `.harmonik/crew/designs/` — meaning they are **not in version control**. If any have lasting value they should be moved to `docs/` or `plans/` so they survive. Otherwise ARCHIVE/DELETE.

## META — mission-file lifecycle

### The problem
A mission file is written once at crew-spawn/re-task and then **never deleted**. Crews are ephemeral (a session that ends, gets /clear-restarted under a new name, or is re-tasked to a new lane), but the `.md` artifact is durable. Over ~12 days, 20 mission files accumulated for what is at any moment ~5 live crews — a **4:1 dead-to-live ratio**. Worse, the registry `.json` (which IS the authoritative liveness signal) and the mission `.md` are decoupled: nothing prunes a mission when its crew's json disappears.

### Intended lifecycle (proposed, normative)
1. **Spawn / re-task** → captain writes/overwrites `missions/<crew>.md`. One mission file per *live crew name*, overwritten on re-task (never a new file per lane).
2. **Live** → the mission is the crew's operating contract; the matching `<crew>.json` is the liveness token.
3. **Crew dies** (`harmonik crew stop`, or json removed) → the mission `.md` becomes orphaned. **This is the gap: nothing reaps it.**

### Proposed cleanup rule
**A mission `.md` is LIVE iff a `<crew>.json` of the same name exists in `.harmonik/crew/`. Otherwise it is reapable.**

- **Auto-prune:** on `harmonik crew stop` (and on daemon/captain startup reconcile), move any `missions/<name>.md` with no matching `<name>.json` into `missions/_archive/<date>/`. Archive, don't hard-delete, the first time — the captain may re-task a name and want the prior brief. A second pass deletes archives older than N days.
- **Naming/location convention to prevent pileup:**
  - Keep one mission per live crew name; overwrite on re-task (already the norm — enforce it).
  - Move historical/handoff/design content OUT of `missions/` — `handoffs/` and `designs/` already exist; use them, and put anything with durable value under version-controlled `docs/` or `plans/` (the current `designs/` files are gitignored and would be lost).
  - Reserve a `_TEMPLATE.md` / `example.md` name for the sample mission so it's never mistaken for a live crew (rename `example-handoff.md`).
  - Smoke/sandbox crews should write to a `missions/_smoke/` subdir (or carry a `smoke: true` front-matter flag) so they're trivially batch-reapable (kills the chani-smoke ambiguity).

### Immediate manual cleanup (this initiative)
- **DELETE now (9 dead + 1 smoke + handoffs + duncan-HANDOFF):** duncan.md, feyd.md, gurney.md, korba.md, leto.md, liet.md, stilgar.md, thufir.md, chani-smoke.md, duncan-HANDOFF.md, handoffs/codexcrew.md, handoffs/gurney-resume.md.
- **ARCHIVE (5 stale + 3 designs):** audit.md, codexcrew.md, harah.md, kynes.md, ops-monitor.md, designs/* → `missions/_archive/2026-06-20/` (or move the 3 designs to `docs/`/`plans/` if worth keeping in git).
- **KEEP:** chani.md, irulan.md, jamis.md, logmine.md, paul.md, example-handoff.md (rename → `_TEMPLATE.md`).
