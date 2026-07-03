# logmine — Integration: the daily crew-style trigger

**Pass 6 (integration)** · crew liet · 2026-06-11. Wires the operator-resolved trigger (DAILY · SUBSCRIPTION-billed · crew-style) to the twice-validated 6-slice harvest (`pipeline.md`). Assembles the recurring pipeline into one coherent loop. Design-only artifacts are ready-to-install; the harvest *method* is frozen — only *initiation* is new.

## How the components connect (integration order)

```
[1] launchd/cron  --daily-->  [2] hk-logmine-daily.sh
                                     |  (presence-check: skip if liet already online)
                                     v
                               [3] harmonik crew start liet  (interactive claude --remote-control, SUBSCRIPTION)
                                     |  boots with mission .harmonik/crew/missions/liet.md + crew-launch skill
                                     v
                               [4] crew liet reads prior findings high-water event_id  -> sets window
                                     v
                               [5] 6-slice read-only harvest (pipeline.md, iter-2 validated)
                                     v
                               [6] synthesize + dedup vs prior register + open beads + FIXED-delta -> findings-iterN.md (+ high-water footer)
                                     v
                               [7] file/route beads (codename:logmine; daemon-lane -> captain, liet-lane -> liet-q)
                                     v
                               [8] digest to captain (comms) + epic-journal comment (hk-mhmaw)
                                     v
                               [9] crew idles/exits  (fresh context next day)
```

## Trigger design decisions

- **Scheduler = OS-native, not harmonik (yet).** Platform is darwin → **launchd LaunchAgent** is primary (survives reboot, user-session-scoped, `StartCalendarInterval`). `cron` is the portable fallback. A **harmonik-native scheduled-job** primitive is the deferred follow-up (filed as a bead) so this isn't OS-scheduler-dependent long-term.
- **Spawn-fresh, not persistent (recommended).** Each day spawns a **fresh** crew liet with clean context, which runs and exits. *Why over a 24/7 persistent crew:* a persistent session accumulates context over days → needs session-keeper context-clears and risks the ~200k-context refusal class (crew memory). Fresh-spawn sidesteps that entirely. The mission ("run the NEXT logmine iteration") is already cold-boot-complete. *(Persistent + daily comms re-task remains a supported alternative if the operator prefers one warm session — note: idle crews need a pane-nudge/armed `recv --follow` to wake.)*
- **Billing safety (LOAD-BEARING).** The spawn MUST use the Captain & Crew path — interactive `claude --remote-control` in a detached tmux (subscription-billed) — and MUST NOT use `claude -p` / any headless invocation (the metered-API / 2026-05-30 credit-burn class). This is the operator's explicit constraint.
- **Overlap guard.** The script checks `harmonik comms who --json` and skips only if a `liet` row has **`status=="online"`** — `comms who` also emits **stale/dead** rows (a crew that exited yesterday still appears as `stale`), so a plain name-grep would match a dead crew and skip *every* subsequent day. A `flock` also guards the check-then-spawn against two near-simultaneous fires.

## Shared state & cross-run cursor

- **Cross-run cursor = the high-water `event_id`** recorded in each `findings-iterN.md` footer. The daily run reads the latest findings doc's footer to set its window start (since-last-run), so it never re-scans the whole log. **Wiring gap to close:** iter-1/iter-2 findings docs did NOT record a high-water mark — the trigger spec adds a `> high-water: <event_id>` footer line as a hard requirement of the synthesis step. **First-run fallback (P2-b):** when no footer exists yet (the very first scheduled run, or a back-filled history), default the window to **the last 24h by `.timestamp_wall`** (the daily cadence) rather than a full-log scan; the mission must state this fallback explicitly so the first run is bounded.
- **events.jsonl** (read-only source), **beads ledger** (write via `br`), **comms bus** (report) — all reused, no new store.

## Cross-cutting concerns

- **CWD discipline:** the crew operates from repo root (`git -C`), never inside a worktree.
- **Method invariants (frozen):** no hand-grep by run_id (F14); dedup vs open beads before filing; `[T]` triangulation; FIXED-vs-RECURRING classification every run.
- **Daemon dependency:** the harvest is read-only file analysis + comms/br; it needs the daemon up only for comms reporting & bead dispatch. The script **checks** the daemon (`harmonik queue status`) and proceeds read-only if down, deferring bead dispatch — it does NOT start the daemon (supervisor/operator lane).
- **Observability of the trigger itself:** absence of a daily captain digest = the run didn't fire. launchd `StandardErrorPath` logs spawn failures. *(Nice future tie-in: a missed daily run could itself emit a `decision_needed` via the hitl-decisions surface this crew just designed.)*

## Ready-to-install artifacts

### `hk-logmine-daily.sh` (the trigger)
```bash
#!/usr/bin/env bash
# Daily crew-style logmine trigger. Subscription-billed (crew path), never claude -p.
set -euo pipefail
PROJ=/Users/gb/github/harmonik
cd "$PROJ"
# Single-fire lock (P2-a): guard the check-then-spawn against two near-simultaneous fires.
exec 9>/tmp/hk-logmine-daily.lock
flock -n 9 || { echo "$(date): another fire holds the lock — exiting."; exit 0; }
# Overlap guard (T2): skip ONLY if a liet crew is actually ONLINE. `comms who` also lists STALE/dead rows,
# so a plain name-grep would match yesterday's exited crew and skip forever — filter on status=="online".
if harmonik comms who --json 2>/dev/null | jq -e 'select(.agent=="liet" and .status=="online")' >/dev/null 2>&1; then
  echo "$(date): liet online — skipping daily spawn." ; exit 0
fi
# Spawn a fresh crew liet on the logmine mission (interactive remote-control = subscription billing).
# --queue is REQUIRED (verified via `harmonik crew start --help`); the harvest routes liet beads to liet-q.
exec harmonik crew start liet --queue liet-q --mission "$PROJ/.harmonik/crew/missions/liet.md"
#   ^ MUST be the crew-start path (claude --remote-control). NEVER `claude -p` (metered API / credit burn).
```
*(`harmonik crew start <name> --queue <q> --mission <handoff-path>` — confirmed against `--help`; `--queue` required.)*

### `com.harmonik.logmine-daily.plist` (launchd LaunchAgent, macOS)
```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>com.harmonik.logmine-daily</string>
  <key>ProgramArguments</key><array>
    <string>/Users/gb/github/harmonik/scripts/hk-logmine-daily.sh</string>
  </array>
  <key>StartCalendarInterval</key><dict><key>Hour</key><integer>9</integer><key>Minute</key><integer>30</integer></dict>
  <key>StandardOutPath</key><string>/tmp/hk-logmine-daily.log</string>
  <key>StandardErrorPath</key><string>/tmp/hk-logmine-daily.err</string>
</dict></plist>
```
**Install (operator step — system-level, surfaced not auto-applied):**
```bash
install -m755 hk-logmine-daily.sh /Users/gb/github/harmonik/scripts/hk-logmine-daily.sh
cp com.harmonik.logmine-daily.plist ~/Library/LaunchAgents/
launchctl load ~/Library/LaunchAgents/com.harmonik.logmine-daily.plist
```
*(cron fallback: `30 9 * * * /Users/gb/github/harmonik/scripts/hk-logmine-daily.sh`)*

## Integration testing strategy

1. **Manual fire (no waiting a day):** run `hk-logmine-daily.sh` directly → assert it spawns liet, liet joins comms, runs the harvest, and produces a `findings-iterN.md` with a high-water footer + a captain digest.
2. **Cursor advance:** assert run N+1's window starts at run N's high-water `event_id` (no full-log re-scan).
3. **Billing path:** assert the spawned process is `claude --remote-control` (subscription), never `claude -p` — grep the process table after a manual fire.
4. **Overlap guard:** run the script while liet is online → assert no second spawn.
5. **Daemon-down degrade:** stop the daemon, fire the script → assert the harvest still runs read-only and defers bead dispatch (no crash).

## Open items for `tasks` pass (gated on operator sign-off)

- Add the high-water footer requirement to `pipeline.md` synthesis step.
- File the **harmonik-native scheduler** follow-up bead (so it's not launchd-dependent).
- Decide install owner (operator runs the 3 install commands; this work does not edit `~/Library/LaunchAgents` or the repo tree itself).
