# logmine — Recurring Self-Improvement Pipeline (SPEC)

**Epic hk-mhmaw · codename logmine · plan jig · 2026-06-11.** Single-reference assembly. Detail in the
pass docs: `01-problem-space.md`, `pipeline.md` (frozen method), `05-specs/recurring-pipeline-spec.md`,
`06-integration.md` (trigger wiring). Status: integration — gated on operator sign-off before `tasks`.

## Purpose

Continuously mine harmonik's own operational logs to find recurring friction and drive fixes:
**mine logs → document patterns → investigate+prioritize → file/route fix beads → confirm fixed next run.**
Validated twice end-to-end (2026-06-09 F1–F22, 2026-06-11 F23–F36); this spec makes it **recurring** instead
of a hand-run one-shot.

## The harvest method (FROZEN — `pipeline.md`)

A **6-slice read-only parallel fan-out** over the window since the last run: (1) failures/wedges,
(2) reconciliation/queue-lifecycle, (3) review subsystem, (4) daemon-log, (5) comms-bus, (6) process/workflow/docs+CI.
Each slice:
- reads the prior `findings*.md` register and **classifies every prior `Fxx` as FIXED-confirm / RECURRING / open**
  (the FIXED-vs-RECURRING delta is the payoff of recurrence);
- uses structured `jq` over `.timestamp_wall` — **never hand-greps events.jsonl by run_id** (F14, false negatives);
- anchors every finding to a durable artifact (event_id / file:line / sha); marks `[T]` when ≥2 slices triangulate.

Synthesis: dedup across slices **and against open `codename:logmine` beads** (enrich the existing bead with a
comment, don't file a duplicate); write `findings-iterN.md` with a prioritized register; **footer records the
high-water `event_id`** (the cross-run cursor — window start for the next run).

## Bead filing & lane routing

`br create --labels codename:logmine` (never pre-assign an assignee — wedges daemon claim). Route by lane:
- daemon/comms code (`internal/daemon/**`) → digest to captain for the **stilgar/daemon-infra** lane; do NOT
  dispatch from liet-q (avoids colliding with that lane).
- docs/skills/process/CI → **liet-q**, dispatchable (`harmonik queue submit --queue liet-q --beads …`).
- credential/infra/ops decisions → **surface to captain/operator** (no code bead).
Record the finding→bead map in the findings doc. Do NOT `br close` (daemon owns terminal transitions).

## The trigger (RESOLVED — operator 2026-06-11): DAILY · SUBSCRIPTION · crew-style

A **daily** OS scheduler (launchd LaunchAgent on darwin; cron fallback) fires `hk-logmine-daily.sh`, which —
after an overlap guard (`comms who` → skip if liet already online) — **spawns a fresh crew liet** via the
Captain & Crew path (**interactive `claude --remote-control`, subscription-billed**). The crew boots on
`.harmonik/crew/missions/liet.md`, runs the frozen harvest from the prior high-water cursor, files/routes
beads, digests to captain, and exits (clean context next day).

**Normative trigger conditions:**
- **T1 — subscription only.** The spawn MUST use `claude --remote-control` (crew path); MUST NOT use
  `claude -p` / any headless metered-API invocation (the credit-burn class).
- **T2 — overlap guard.** Skip the spawn only if a `liet` row in `comms who --json` has `status=="online"` — `comms who` also lists **stale/dead** rows, so a plain name-match would skip forever after the first run. A `flock` also guards against double-fires.
- **T3 — cursor.** Window start = the prior `findings-iterN.md` high-water `event_id` (each run writes its own footer). **First-run fallback:** when no footer exists yet, default to the last 24h by `.timestamp_wall`, never a full-log scan.
- **T4 — daemon-down degrade.** If the daemon is down, run the harvest read-only and defer bead dispatch;
  never start the daemon (supervisor/operator lane).
- **T5 — fresh-spawn default.** Prefer a fresh daily crew (clean context) over a 24/7 persistent session;
  persistent+re-task is a supported alternative.

## Acceptance (integration testing — `06-integration.md`)

Manual fire spawns liet → harvest → `findings-iterN.md` + high-water footer + captain digest; cursor advances
run-over-run; spawned process is `--remote-control` not `-p`; overlap guard prevents a double-spawn;
daemon-down still produces a read-only harvest.

## Deferred follow-ups (file in `tasks`)

- A **harmonik-native scheduled-job** primitive (so the trigger isn't OS-scheduler-dependent).
- Add the high-water footer requirement into `pipeline.md`.
- Install ownership: the operator runs the 3 install commands (`06-integration.md`); this work edits neither
  `~/Library/LaunchAgents` nor the repo tree.

## Open flags for sign-off

1. Confirm **fresh-spawn** (vs persistent-re-task) for the daily crew.
2. Confirm the **daily clock time** (placeholder 09:30 local in the plist).
3. Approve the **install step** (operator-run) vs. routing it as a liet-q bead.
