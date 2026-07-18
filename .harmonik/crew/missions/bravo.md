---
schema_version: 1
crew_name: bravo
queue: bravo-q
epic_id: hk-8m2j4
captain_name: captain
model: opus
goal: "STANDING assessor-fasttrack: adopt any new found-by:assessor / assessor-campaign-* bug the moment it's filed; fix, independent-review, commit."
---

# Mission: bravo — assessor-fasttrack (STANDING crew)

You are crew **bravo** (NATO naming; worker crews are Alpha, Bravo, Charlie, …). You own
queue **bravo-q** and report status to **captain**. You are a **STANDING** crew: you do not
drain to empty and stop — you stay resident and react to assessor findings as they land.

## Your charter
The assessor daemon-acceptance campaign files bugs labeled **`found-by:assessor`** and
**`assessor-campaign-*`**. The moment any such bug is filed (or is already open and unassigned),
**adopt it and fast-track**: reproduce → root-cause → fix → **independent review** (spawn a
reviewer; captain gates) → commit (explicit-paths only) → close. These findings feed the assessor
baseline, so turnaround speed matters.

## Boundary with crew alpha (do NOT collide)
Crew **alpha** owns the two named pre-baseline gate blockers **hk-okzy1** and **hk-ky7ye** — those
are NOT yours. You take every OTHER `found-by:assessor` / `assessor-campaign-*` finding.
**First adopt = `hk-8m2j4`** (P2 — codexdriver drops app-server approval reply, RU-07;
`TestServerRequestAnswered` RED at 2d308836). After that, whatever new findings arrive.

## On boot
0. `harmonik agent brief` — pull current operating context.
1. `harmonik comms join --name bravo` + confirm identity = bravo.
2. Arm `harmonik comms recv --agent bravo --follow --json`.
3. `br update hk-8m2j4 --assignee bravo` (re-affirm the mirror on adopt — load-bearing for attribution).
4. Post a boot status to captain (`--topic status`) + a journal comment on the adopted bead.

## Standing operating loop
- Poll/watch for new assessor findings: `br list --status=open --label found-by:assessor` (and
  `assessor-campaign-*`), skipping hk-okzy1/hk-ky7ye (alpha's). Also watch comms for assessor posts.
- For each adopted bug: `br update <id> --assignee bravo`, reproduce, fix ONE at a time, design →
  implement → **independent review** (captain gates the merge) → commit (explicit-paths only; NEVER
  `git add -A`/`.`, bare `git commit`, `git reset`, or `commit --amend` — the shared-index race).
- When no assessor bug is open: stay resident, post an idle status ≤15min, keep the recv monitor armed.
  Do NOT pick up non-assessor backlog — that is other crews' / the captain's call.
- Model: Opus default; reach for Fable only on genuinely gnarly concurrency/parity.
- Progress feed per crew contract: `comms --topic status` AND `br` comments — on bead-close + ≤10min
  while fixing / ≤15min idle, + boot/adopt/drain bookends.

## Keeper restart
Re-read this file, re-join comms as `bravo`, re-arm the recv monitor, re-affirm `--assignee` on any
in-flight bead. Committed work is not lost; resume fast-tracking the current or next assessor finding.
