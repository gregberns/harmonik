# I5 — Prior-Work Gap Audit (adversarial critic)

**Question:** the leanfleet / tokenaudit / sleep-wake initiatives claimed done, yet the
operator still complains of boot >100k, restart issues, and busy-work. Where did the
work NOT actually solve the **captain-economy** problem?

**Verdict bias applied:** "NOT solved" unless hard evidence exists on `main`.

---

## TL;DR

The audit (`docs/retro/2026-06-17/token-burn-analysis.md`) correctly diagnosed the cost:
**95.9% of spend = cache-read = context-size × turns × sessions**, and named the bill as
the **long-lived Opus captain+crew sessions**. The fixes that LANDED overwhelmingly hit
the **keeper bands** and **crew/daemon plumbing**. The two levers that directly shrink the
**captain's per-turn context and turn count** — boot-digest cheap-resume (TA2) and
admin-offload (D4/#6) — landed as **orphan deliverables that were never wired into the
captain's actual behavior**. So the captain is still fat at boot and still runs the same
heavy 12-minute tick. The operator complaints are about exactly the un-wired pieces.

---

## Q1 — Did the fixes attack the CAPTAIN's per-turn context & turn count?

**Mostly NO. They attacked keeper bands and crew/daemon mechanics.**

Landed and real (keeper / daemon lane — PAUL):
- Band retune warn=200K/act=215K/force=240K + invariant (`97e1787c`,
  `internal/keeper/thresholds.go`) and the young-session + clean-handoff guards
  (`fa98f7c1`) — **genuinely on main**, hk-8hr1 legitimately complete.
- Noise reduction: 30s inject back-off, no_gauge re-emit 120→300s, dip-rise WarnCooldown
  (`d83bc987`, `internal/keeper/watcher.go:395-401,497`, tests in
  `watcher_noise_reduction_test.go`) — **real**, hk-sol6 legit.
- Sleep/wake CLI + daemon quiesce + genuine-drain oracle (`20d18220`, `848fda0b`,
  `402d4cf4`, `6afdec00`, `903b16ce`) — **real**, substantial.

What this does NOT touch: the captain's **per-turn context** (STARTUP.md 567 lines +
SKILL.md 772 lines = 1865 lines of skill text the Opus captain re-sends every turn) and
its **turn count** (the `/loop 12m` tick + boot discovery turns). Restarting *earlier*
(215K vs old ~270K) caps the ceiling of a single session, but each captain session still
boots heavy and each tick is still a full Opus turn. The 95.9%-cache-read driver for the
captain specifically was **not** reduced — only the keeper band that bounds it was lowered.

---

## Q2 — Boot-digest (TA2 / hk-n3w1): WIRED or orphan?  **HOLLOW.**

The bead spec was explicit (operator-greenlit, 2026-06-17 23:02 comment):
> "place scripts IN-REPO so the daemon worker can build+land them:
> `scripts/captain-boot-digest.sh` and `scripts/crew-boot-digest.sh` (NOT
> `~/.claude/captain-tools/`, which is outside git)... Add `scripts/README-boot-digest.md`.
> Acceptance: both scripts executable, run clean against the live project."

Reality on `main`:
- `scripts/captain-boot-digest.sh` — **DOES NOT EXIST**.
- `scripts/crew-boot-digest.sh` — **DOES NOT EXIST**.
- `scripts/README-boot-digest.md` — **DOES NOT EXIST**.
- The TA2 commit `7127c924` touched **only two files**: `STARTUP.md` (+17) and
  crew-launch `SKILL.md` (+10). It added *references* to scripts at
  `~/.claude/captain-tools/captain-boot-digest.sh` (STARTUP.md:105, 202) and
  `~/.claude/captain-tools/crew-boot-digest.sh` (crew-launch SKILL.md:46).
- Those scripts DO exist out-of-repo at `~/.claude/captain-tools/` (timestamped Jun 17
  16:05/16:06), authored by hand — **outside git, on this one machine only**.

This is the exact anti-pattern the bead explicitly forbade. The acceptance criterion
("scripts in-repo, daemon-landable") was **NOT met**, yet hk-n3w1 was closed `done` the
same day. The "cheap resume" premise that makes earlier restarts net-positive (TA1's
stated dependency) is therefore **unrealized on any fresh deployment** and not under
version control. A captain booting from the repo skill still reads raw files turn-by-turn.

**Double-counting flag:** the commit message claims "Links both scripts from STARTUP.md
Steps 2/4" — true — but links to a non-versioned path, so the closure over-claims a
deliverable that does not exist on main.

---

## Q3 — Admin-offload (priority #3 / D4 / #6): decomposed & landed?  **PARTIALLY — but un-wired to the captain.**

Landed (`70facd90` hk-k2px, `f6a007e5` hk-7xr9, `internal/daemon/opsmonitor_schedule.go`):
- `scripts/ops-monitor-check.sh` exists (real, 357 lines) — a **deterministic bash**
  one-pass health check (daemon-up / paused-queues / single-mode / crew-staleness /
  ready-unstaffed / idle-fleet), writes `.harmonik/ops-monitor/latest.json`, comms only on
  signal.
- Auto-registered on every daemon startup as an `every@5m` `ActionKindCommand` job
  (`opsmonitor_schedule.go:40-41`) — actually *cheaper* than the designed "Sonnet crew on
  ops-q" because it's pure Go/bash, no LLM turn.

The GAP: the design's whole point (D4) was that this **"shrinks the captain `/loop 12m`
tick."** It does NOT. The captain's tick at `STARTUP.md:398` still independently runs all
eight checks itself — daemon-up, paused-queues, crew-freshness, backlog-pull, lull-deploy,
quality-check, self-audit. There are **zero references** to `ops-monitor` or `latest.json`
in `STARTUP.md` (grep count = 0). So the captain re-derives the same signals the bash
monitor already computed. The offload **deliverable exists but was never connected to the
captain to reduce its turns** — the busy-work the operator complains about is precisely
this still-full tick.

---

## Q4 — 3-tier handoff (priority #4 / D5): built?  **YES — this one genuinely landed.**

- Tier files present: `.harmonik/context/project.yaml`, `.harmonik/context/captain-lanes.md`,
  `HANDOFF.md` (all on disk; project.yaml + captain-lanes.md actively edited Jun 18-19).
- `STARTUP.md` Step 0a/0b reads tier-3 then tier-2 (`STARTUP.md:47-72`), Step 2 verifies
  claims against ground truth (`:96-101`), with update discipline documented. Commit
  `7930bf28` (LF-A).

This is the one priority-#4 deliverable that is real, versioned, and wired into boot.
Caveat: it shrinks *discovery turns*, not the *static skill bulk* (1865 lines) the captain
re-sends each turn — so it helps turn-count but not per-turn context size.

---

## Q5 — Over-optimistic closures / deliverables missing on main

1. **hk-n3w1 (TA2 boot-digest) — CLOSED `done`, deliverable absent on main.** The
   mandated in-repo scripts + README do not exist; only out-of-repo hand-authored copies
   and skill references to a non-versioned path. **Hollow closure.** (See Q2.)

2. **hk-lsk5 (self-hint) — CLOSED `Fixed in d83bc987`, but the deliverable does NOT match
   its own spec.** The bead spec (and leanfleet design §2) required:
   - the hint to say `harmonik keeper restart-now --agent SELF` so the agent self-fires;
   - "C1 MUST-FIX: the hint text MUST enumerate unsafe-to-self-fire conditions (an armed
     Monitor, a pending sub-agent result, or an unverified file edit)."

   The text actually on main (`internal/keeper/watcher.go:1096`):
   > `"[KEEPER HINT] Context is at ~190K tokens. Consider wrapping up the current task and preparing a handoff soon."`

   It contains **no `restart-now` command** and **no unsafe-condition enumeration**. The
   latch + one-time injection plumbing is real (`watcher.go:976-986`), but the *behavioral
   payload* — a self-restart instruction with safety guard — was dropped. So the "agent
   self-fires a clean early restart" mechanism the design counted on does **not** exist;
   the agent gets a passive nudge it can ignore. **Partially hollow** (mechanism real,
   intended effect absent).

3. **D3 noise fix — captain 600s heartbeat: design said "drop it"; still present.**
   `STARTUP.md:382` still documents the `--heartbeat 60s` keepalive lineage and the design
   bullet "Drop the captain 600s subscribe heartbeat (STARTUP.md, logmine)" has no
   corresponding STARTUP.md removal in `7930bf28`. The `/loop 12m` tick (named in the
   sleep-wake research as "the single largest scheduled captain burn while idle") is still
   armed unconditionally (`STARTUP.md:396-398, 564`). The keeper-side noise (the operator's
   "[NOTICE] repeated" complaint) WAS fixed; the captain-side scheduled-burn was not.

---

## Bottom line for the captain-economy problem

| Lever | Target | Landed on main? | Captain still affected? |
|---|---|---|---|
| Keeper band earlier (hk-8hr1) | session ceiling | YES (real) | Helps cap, doesn't shrink turn/context |
| Keeper noise (hk-sol6) | keeper JSONL/inject spam | YES (real) | Fixes crew/keeper noise, not captain |
| Self-hint (hk-lsk5) | agent self-restart | PLUMBING yes, PAYLOAD no | Hint is passive; no self-fire |
| Boot-digest (hk-n3w1) | cheap captain resume | **NO (orphan, out-of-repo)** | **YES — boot still heavy** |
| Admin-offload (ops-monitor) | shrink captain /loop tick | SCRIPT yes, WIRING no | **YES — tick still full** |
| 3-tier handoff (LF-A) | cheap boot discovery | YES (real, wired) | Helped (turn-count only) |
| Sleep/wake | idle burn | YES (real) | Helps idle, not active-session size |

**The captain is still fat because the two levers aimed at it (cheap resume + tick offload)
are un-wired/orphan, and the self-restart payload was watered down to a passive nudge.**
Every fix that *fully* landed targeted keeper bands, crew noise, or daemon idle — not the
Opus captain's per-active-turn context. That is why the operator still sees boot >100k and
busy-work after these initiatives were marked done.

### Concrete remediation targets
- Land `scripts/captain-boot-digest.sh` / `crew-boot-digest.sh` / README **in-repo**;
  re-point STARTUP.md / crew-launch SKILL.md to `scripts/…`; reopen/redo hk-n3w1's
  acceptance. (Q2)
- Rewrite the captain `/loop 12m` tick (STARTUP.md:398) to **read `.harmonik/ops-monitor/
  latest.json`** instead of re-running all eight checks. (Q3)
- Restore the self-hint payload to the spec'd `restart-now --agent SELF` + unsafe-condition
  list, or downgrade the design claim. (Q5.2)
- Decide on the captain `/loop 12m` + 600s heartbeat per D3; trim the 1865-line static
  skill bulk if per-turn context is the true driver.
