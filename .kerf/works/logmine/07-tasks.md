# logmine — Tasks (recurring-pipeline implementation)

**Pass 7 (tasks)** · crew liet · 2026-06-11. Decomposes `SPEC.md` + `06-integration.md` into actionable
tasks. The harvest method is already built+frozen (`pipeline.md`); these tasks only stand up the **recurring
trigger** + close the two wiring gaps. Most of the surface is operator-gated (the install); two tasks are
non-gated and actioned now.

## Dependency order

```
[T0 sign-off flags] --gates--> [T1 install trigger]
[T2 footer requirement] (done inline, non-gated)
[T3 native scheduler] (non-gated follow-up; supersedes T1's OS-scheduler later)
```

## T0 — Operator sign-off (GATE, not a build task)
Three product calls block the install. Surfaced to captain/operator:
1. **Fresh-spawn vs persistent** crew (SPEC T5 — recommend fresh-spawn for clean context).
2. **Daily clock time** (plist placeholder 09:30 local).
3. **Install ownership** — operator runs the 3 install commands (`06-integration.md`) vs. routing the file-drops as a liet-q bead.
*Until T0 is answered, T1 cannot proceed.*

## T1 — Install the daily crew-style trigger  *(GATED on T0; operator/ops lane)*
- **What:** drop `scripts/hk-logmine-daily.sh` + `~/Library/LaunchAgents/com.harmonik.logmine-daily.plist`, `launchctl load`.
- **Spec ref:** `06-integration.md` §Ready-to-install artifacts; `SPEC.md` §The trigger (T1–T5).
- **Deliverables:** the script (with the review-fixed `--queue liet-q`, `status==online` guard, `flock`, first-run 24h fallback) + the launchd plist. cron fallback documented.
- **Acceptance:** manual fire → spawns `liet` via `claude --remote-control` (subscription, *not* `claude -p`), harvest runs, `findings-iterN.md` + high-water footer + captain digest produced; overlap-guard skips a double-spawn; daemon-down still runs read-only.
- **Deps:** T0. **Owner:** operator/ops — *this design work edits neither `~/Library/LaunchAgents` nor the repo tree.*

## T2 — Add the high-water-footer requirement to `pipeline.md`  *(NON-gated · liet-ownable · DONE inline 2026-06-11)*
- **What:** the synthesis step must write a `> high-water: <event_id>` footer in each `findings-iterN.md`, and the harvest must read the prior footer as its window start (first-run fallback: last 24h by `.timestamp_wall`).
- **Spec ref:** `SPEC.md` T3; `06-integration.md` §Shared state.
- **Acceptance:** the next harvest writes the footer; the following run reads it as the window start (no full-log re-scan). **Status: applied to `pipeline.md` this session.**

## T3 — harmonik-native scheduled-job primitive  *(NON-gated follow-up · daemon-infra lane · bead filed)*
- **What:** a `harmonik schedule` / recurring-job primitive so the daily trigger fires from the daemon, not an OS scheduler — removes the launchd/cron dependency and unifies scheduling with the daemon's supervise loop.
- **Spec ref:** `recurring-pipeline-spec.md` §deferred follow-up; `06-integration.md` §Trigger design decisions.
- **Acceptance:** the daemon fires a recurring crew-spawn on a cadence (with the same T1–T5 billing/overlap guards) without launchd/cron; T1's OS-scheduler becomes optional.
- **Deps:** none (independent enhancement). **Lane:** daemon-infra — filed `codename:logmine`, routed to captain (NOT self-dispatched from liet-q). Bead: see filing record below.

## T4 — (future, noted) missed-run meta-monitoring
A missed daily run (no captain digest) is itself a HITL signal — a future tie-in to the `hitl-decisions`
surface (emit a `decision_needed` when the daily run is absent N days). Out of scope for v1; noted.

## Filing record
- T3 → bead filed `codename:logmine` (daemon-infra, P3) — routed to captain for the daemon/infra lane.
- T2 → applied inline to `pipeline.md`.
- T1 → no bead until T0 sign-off (avoids a gated/parked bead in the queue).

## Done means
The recurring pipeline runs itself: a daily, subscription-billed, crew-style trigger executes the frozen
6-slice harvest, advancing the FIXED-vs-RECURRING register without human initiation — humans act only on the
surfaced decisions. Reached when T0 is answered and T1 is installed (T3 optionally supersedes the scheduler).
