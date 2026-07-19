# C6 — How disruptive are keeper restarts for CREWS, really?

Research groundwork for `plans/2026-07-17-keeper-restart-timing/_plan.md` item 4.
Read-only. Method: `.harmonik/events/events.jsonl` (keeper cycle events) + sampled
Claude Code crew session transcripts. Author: bravo, 2026-07-18.

## TL;DR

Crews do **not** primarily suffer the C1/C4 pain that motivated this plan (interrupt
mid-operator-conversation / swept-in operator input) — that is a **captain/admiral**
problem. Operators don't type into crew panes, and crews restart with almost no
preceding WARN. Crews show **two different** pathologies:

1. **WARN-less churn.** Crews cross straight to restart; the graded "informational
   WARN → keep working" tier essentially never engages for them.
2. **A ~20% restart-abort rate**, concentrated in stuck/hung sessions — a keeper
   *reliability* gap, distinct from the *timing* gap the plan is about.

## Quantitative backbone (event log, 12 crews, 138 restarts)

| crew | restarts | complete | abort | warn | abort% | median gap | warn/restart |
|---|--:|--:|--:|--:|--:|--:|--:|
| gurney | 34 | 20 | 14 | 22 | 41% | 0.9h | 0.65 |
| leto | 19 | 16 | 3 | 3 | 16% | 3.0h | 0.16 |
| paul | 17 | 13 | 4 | 14 | 24% | 3.0h | 0.82 |
| jessica | 16 | 16 | 0 | 0 | 0% | 1.3h | 0.00 |
| kynes | 15 | 15 | 0 | 0 | 0% | 1.8h | 0.00 |
| jamis | 8 | 7 | 1 | 0 | 12% | 4.3h | 0.00 |
| stilgar | 6 | 6 | 0 | 0 | 0% | 4.5h | 0.00 |
| thufir | 6 | 5 | 1 | 0 | 17% | 2.0h | 0.00 |
| duncan | 5 | 5 | 0 | 0 | 0% | 5.8h | 0.00 |
| chani | 5 | 4 | 1 | 0 | 20% | 5.1h | 0.00 |
| hawat | 4 | 4 | 0 | 0 | 0% | 4.1h | 0.00 |
| yueh | 3 | 0 | 3 | 0 | 100% | 2.0h | 0.00 |
| **TOTAL** | **138** | **111** | **27** | **39** | **20%** | — | **0.28** |

- **warn/restart = 0.28** for crews vs **1.67** for the captain (500 warns / 299
  restarts). 7 of 12 crews got **zero** WARNs. The graded band (WARN=200k →
  ACT=215k) does not engage for crews: they idle constantly (55,946
  `idle_crew` events fleet-wide), so ACT fires at the first idle boundary
  before/around the WARN's informational purpose can land. This is arguably
  *good* (restart at a natural pause) but means "escalating reminders" (idea I5)
  and "softer WARN language" (I2) are near-irrelevant for crews.
- **20% abort rate.** Aborts are NOT spread evenly — they cluster in specific
  crews: gurney (14), paul (4), leto (3), yueh (3). Healthy crews (jessica,
  kynes, duncan, stilgar, hawat) had **0% abort** and clean ~1–6h cadence. So the
  abort storm is a *symptom of a stuck/hung crew*, and the keeper's repeated
  abort attempts against it are wasted flailing — not the keeper breaking a
  healthy crew.
- **Worst cases:** yueh restarted 3×, completed 0 — never successfully cycled
  (session `a5844cfe`, aborts at 07-07 07:24, 07-08 23:27, 07-09 01:27 all on the
  same session id). gurney fired **10 consecutive aborts** 06-30 17:02–18:56 on
  one wedged session (`312968ed`).

## Transcript sampling (done — 3 parallel readers, 2026-07-18)

Sampled: **kynes** 4-session clean chain (07-10/11), **leto** 2-session chain
(07-11), **yueh** abort-storm session `a5844cfe`, **chani** aborted session
`13318b46` + recovery `27335629`.

### The unifying finding: crews survive on DURABLE SUBSTRATE, not on the handoff

Across all four crews, restart continuity came from **on-disk state**, not the
keeper's handoff document:
- `HARMONIK_AGENT=<crew>` env → identity is free to restore.
- `.harmonik/crew/missions/<crew>.md` → `{crew, queue, epic_id}` re-derived directly.
- `config.yaml`, bead comments, `events.jsonl`, live daemon queue state → work state.

leto and kynes **re-derived from disk and never even relied on `HANDOFF-<crew>.md`**;
kynes boundary-2 skipped the wake brief entirely (an inbound task-notification +
the working tree carried enough context to go straight to productive work, ~0
re-orient turns). Because identity/queue/epic are externalized, crew restart is
**materially more robust than a captain/orchestrator restart** (whose identity must
be reconstructed from a prose summary).

### Healthy restarts (kynes, leto): near-zero disruption
- **Landed at natural pauses**, not mid-edit. Every sampled inject hit a turn/idle
  boundary — crew had just finished a unit and was holding for a verdict or the
  captain. (Consistent with the 0.28 warn/restart backbone: crews idle constantly,
  so ACT fires at a pause.)
- **State survived; work advanced monotonically.** No redo of completed work, no
  re-dispatch of an already-dispatched bead, no `{epic,queue}` confusion. kynes even
  *reconciled a now-stale handoff plan against live daemon state* rather than blindly
  replaying it (leto did the same, applying its "never double-dispatch" rule).
- **Re-hydration cost small and mostly inherent:** ~0–12 tool calls, most of which
  were legitimately re-checking in-flight background runs, not orientation overhead.

### Abort cases (yueh, chani): NOT hangs — the ABORT label conflates two plumbing failures
The 20% abort rate is **not** the keeper interrupting healthy crews. Decomposed:

1. **Handoff never delivered (yueh).** All 3 "aborts" were *invisible no-ops* — the
   keeper never injected `/session-handoff` at all (zero nonce markers, zero
   `/clear`, zero interrupts across the whole 3.9MB / 43h session). Root causes:
   (a) yueh's **keeper watcher process had silently died** — `keeper doctor --agent
   yueh` returned exit 1, `live-watcher` FAILING; yueh manually restarted it. A dead
   watcher can't confirm a nonce. (b) For aborts #2/#3, yueh was **captain-parked on
   a 1-hour `ScheduleWakeup`** — the 300s nonce window cannot land on a crew that
   only wakes hourly. Damage: **zero work lost**, but the keeper delivered none of
   its value and yueh ran its entire 43h life on a bloated context it should have
   been cycled off of.
2. **Handoff delivered + written + then DISCARDED (chani).** chani cooperated fully
   and wrote `HANDOFF-chani.md` *with the correct nonce inside the window* — yet the
   cycle logged as an abort and the recovery **threw the handoff away**. Two plumbing
   bugs: (a) the `Write` tool's "File has not been read yet" guard blocked the write
   to the pre-existing custom-path handoff, fumbling it for ~30s (also seen in
   kynes); (b) the recovery boot (`agent brief --wake keeper-restart`) **did not read
   the custom `HANDOFF-chani.md`** — it reported "there's no handoff on record" and
   cold-re-derived from durable substrate. Additionally the restart **killed an
   in-flight in-daemon run uncleanly**, orphaning a pi process (pid 8748) that
   **polluted the box for 40+ min and contaminated chani's own later debugging**
   (it flip-flopped its diagnosis twice before the captain reaped the orphans). That
   process-orphan contamination was the single biggest concrete damage found — and
   it's a second-order effect of killing a run mid-flight, not a timing issue.

### Recurring low-grade friction (every restart, all crews)
- **Garbled inject args (defect):** the injected `/session-handoff` command-args
  concatenate the path and the instruction with no separator —
  `HANDOFF-kynes.mdIMPORTANT: include exactly this line verbatim...` — in **all 4**
  kynes cycles. Crews disambiguated correctly every time (right path + correct nonce
  embedded); cosmetic, zero functional damage, but a real bug.
- **Background monitors/sub-agents die on `/clear`.** Live run-watchers (Monitor,
  `subscribe`, review sub-agents) do not survive the restart; the crew must
  re-derive run state from `events.jsonl`/`subscribe`. Underlying work keeps draining
  independently, so nothing is lost, but it's a consistent re-hydration tax.
- **CLI-flag fumbles each reboot** (`comms join --agent`, `run show --run`) cost
  ~2–4 self-correcting calls because the exact syntax isn't in the wake brief.
- **`Write` "must Read first" guard** on the pre-existing handoff file costs 1 extra
  Read+Write each cycle.

## What C6 implies for the option space (feeds the direction memo, item 5)

1. **Crews need a LIGHTER treatment than captain/admiral — not the same.** The C1/C4
   pain (mid-conversation interrupt, swept-in operator input) that motivates the plan
   is **structurally absent for crews**: no operator conversation, warn/restart≈0.28,
   restarts already land at natural idle pauses. Ideas aimed at C1/C4 (I2 softer WARN
   language, I5 escalating reminders, I6/I4 "deliver the comms way") are **near-irrelevant
   for crews.** Don't spend crew design budget there.
2. **The crew-relevant failure is keeper RELIABILITY, not timing.** The two highest-leverage
   crew fixes are plumbing:
   - **(a) Dead-watcher detection / auto-revive.** yueh's watcher died silently; the crew
     only noticed by manually running `keeper doctor`. A restart can't happen if the
     watcher isn't alive. This is the root of the "never delivered" abort class.
   - **(b) The recovery boot must consume a successfully-written handoff.** chani wrote a
     valid nonce'd handoff that `agent brief --wake keeper-restart` never read. Whatever
     the timing policy, if a crew pays to write a handoff, the reboot must use it — and the
     `Write` "must-Read-first" guard on the pre-existing file should not fumble it.
3. **Clean-kill in-flight runs on restart.** chani's biggest real damage was an orphaned
   process from a run killed mid-flight. Restart should quiesce/checkpoint in-flight daemon
   runs, not SIGKILL them and leak processes.
4. **The parked-crew case is a genuine gap** (not a bug): a crew asleep on an hourly
   `ScheduleWakeup` is unreachable by a 300s nonce window. Either the keeper should
   recognize a parked crew and defer, or parking should lower the context enough that no
   restart is due.

### Candidate defects to file (clear bugs, not design)
- Garbled `/session-handoff` inject args (no separator between path and instruction).
- `agent brief --wake keeper-restart` does not read an existing custom-path `HANDOFF-<crew>.md`.
- Keeper watcher can die silently with no auto-revive; only surfaced by manual `keeper doctor`.
- Restart SIGKILLs in-flight in-daemon runs, orphaning processes that pollute the box.

## Status
**C6 = DONE** (plan item 4 answered with data). The direction memo (item 5) remains —
a with-operator step. Recommended threads to carry into it: crew-side reliability
(dead-watcher revive + handoff-consumption on reboot + clean-kill in-flight runs),
kept **separate** from the captain/admiral-side timing/delivery redesign, since the
two roles have genuinely different failure modes.
