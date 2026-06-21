# Remote node telemetry + reporting — overview & plan of record

**Date:** 2026-06-20
**Origin:** operator question on top of the chani / remote-substrate lane — *"To run remote jobs we apparently need a harmonik instance on the remote box. If so, maybe we do more than use it as a channel: report resource state back, know when things max out, know when there are problems."*

**This folder is planning only.** It does not touch chani's unmerged remote-substrate fix or re-enable the worker. The reporting work lives in `internal/workers/` (the health-probe loop) and does **not** collide with chani's substrate commits (SSHRunner / tmuxsubstrate / reviewloop).

Backing docs: `01` current architecture · `02` telemetry+autoscale design · `03` expanded central-leverage · `04` prior-art & naming. **`05-phase1-spec.md` is the concrete first slice.**

---

## The premise, corrected

> "We need a harmonik daemon on the remote."

**Not true, and not a prerequisite.** The remote box runs **no persistent harmonik process**. The central daemon drives it through one-shot `ssh worker -- <cmd>` calls; the only long-lived process per run is a reverse SSH tunnel, and **it lives on the central box, not the remote.** The harmonik *binary* merely has to exist on the worker as a hook handler. (`01`, with file:line refs.)

So the control plane is already 100% central and the worker is dumb. Reporting resource state back needs exactly **one new thing: a worker→central signal** — and the existing health-probe loop can carry it with **no remote daemon at all.**

---

## DECISIONS (operator, 2026-06-20)

1. **Hardcoded max stays.** No dynamic concurrency controller in v1. `max_slots` remains the human-set ceiling. The dynamic autoscaler ("AIMD") is **deferred** — we build it *after* we've tracked real load numbers over time and can choose a sensible max from data, instead of guessing the control law up front.
2. **Reporting first.** The first build answers the two operator questions, nothing more:
   - **"Are there issues?"** — problems the box knows about and currently never says (orphaned claude, leaked worktrees, disk pressure, repeated no-commit).
   - **"At what point are we maxing out?"** — resource load (CPU/RAM/swap/disk), tracked over time so the eventual max is evidence-based.
3. **No metaphor name.** Central node stays plain in code (`hub` / `control node`); we'll name it if something obvious shows up. Not a blocker.

---

## Two questions, two transports (the key structural point)

These are different questions with different freshness needs, so they get different channels:

- **"Can I send this box more work / what's its baseline load?"** — slow and coarse. A periodic poll on the existing health-probe timer is plenty. Fine that it only runs on a timer; fine that it's central-pull.
- **"Is a job running *right now* pushing the box to its limit?"** — live and event-driven. A once-a-minute poll is too slow. You want the moment a threshold (e.g. **>80% CPU for N seconds**) is breached, pushed up *while the job runs* over the tunnel that's **already open** during a run. When the box is idle / daemon down, it stays silent — no phone-home, as you wanted.

---

## Phases

### Phase 1 — Reporting (slow poll). ✅ LANDED 2026-06-20 (main `df89cbe8`; beads WR1–WR5, `codename:worker-report`).
Extended the periodic health probe to collect and surface, per worker:
- a **resource snapshot** (load, mem, swap, disk, claude-process count) — logged over time so we can later pick a max from real data;
- **problem flags** answering "are there issues?" (orphaned claude, worktree leak, disk pressure).
`max_slots` stays hardcoded; dispatch behavior is unchanged. Pure observability. Off-by-default; no-op when no workers configured. Code: `internal/workers/telemetry.go` + `report_poll.go`. Spec: `05-phase1-spec.md`. Doc: `docs/remote-substrate/worker-reporting.md`.

### Phase 2 — Live breach alerts (during a run). ✅ LANDED 2026-06-20 (main `24ae1aef`; beads PB1–PB4, `codename:worker-breach`).
The grounding flip: ended up **entirely central-side** (no worker process / no relay change / no teardown) because chani's completion-detection fix already polls the worker per-run and the detector emits straight to the bus. A pure 4-state breach detector (`internal/workers/breach.go`) runs in `RunReportLoop` with **adaptive cadence** — slow 60s when idle (baseline report only), fast 5s sampling while a run is in flight — and emits one `resource_breach{breach}` after a sustained dwell (CPU load-ratio >0.85 for 20s / low mem / swap), one `{clear}` on recovery, silent when idle. Spec: `phase2/03-phase2-spec.md`.

### Phase 3 — Resident node agent + data-driven autoscale. ⏸ NOT STARTED (deferred by design).
The always-on process that phones home even between jobs, self-diagnoses, advertises capability — and, with real load history in hand, the dynamic controller that turns the hardcoded `max_slots` into a center-computed target. **Do not start before the pickup checklist below is satisfied.** (Detail preserved in `02`/`03`/`04`.)

---

## STATUS (2026-06-20) — where we got to

**Phases 1 and 2 are complete and on main.** 9 beads (WR1–WR5, PB1–PB4) built → independently reviewed → merged. The whole telemetry + live-breach layer is in `internal/workers/`, **off-by-default**, unit-tested and race-clean — but it has **never run against a live worker** (gb-mbp has been `enabled:false` throughout, behind chani's remote-substrate e2e lane). So the code is correct in isolation but the *values* (thresholds, page-size math, load-ratio realism) are unvalidated against real hardware.

## Remote-node Phase 3 — pickup checklist (do these BEFORE building autoscale)

> "Remote-node Phase 3" = this initiative's node-agent + autoscale phase. NOT the global ROADMAP "Phase 3" (DOT-defined processes) — different thing.

Ordered roughly by dependency:

1. **Get the remote e2e actually green first (umbrella gate).** Phases 1–2 ride the remote-substrate path, whose end-to-end has been blocked on gb-mbp. chani's completion-detection fix landed (`d64c6602`) but a clean full remote run must be confirmed before telemetry can be live-exercised. *Nothing below can happen until a job actually runs on the worker.*
2. **Live-validate Phase 1+2 against the real worker (the #1 data precondition).** In a confirmed quiet window, enable gb-mbp, run real jobs, and confirm with `harmonik subscribe --types worker_report,resource_breach`: the resource snapshot reads sane values (verify the Apple-Silicon page-size math against real `vm_stat`; sanity-check load-ratio vs the box's core count); the problem flags don't false-fire; `resource_breach` fires at realistic thresholds (is **0.85 load-ratio / 20s dwell** right for gb-mbp, or does it need tuning?). Autoscale on unvalidated thresholds is the exact "guess the control law" trap we deliberately deferred.
3. **Accumulate a real load-history sample.** Let `worker_report` + `resource_breach` collect across real runs. Remote-node Phase 3's autoscaler picks the per-box max *from this data* — that was the whole reason for deferring it.
4. **Lift the V1 single-worker registry cap.** `workers.NewRegistry` holds at most one worker, and `InFlight()`/orphaned_claude carry a documented single-worker assumption (PB3 already keys breach state per worker name). Per-box autoscale, capability routing, and the resident-agent model all need N≥2 to be meaningful — so a per-worker `InFlight` map keyed by name is a prerequisite.
5. **Decide the resident-agent + push/pull architecture (operator-gated).** Remote-node Phase 3 introduces the always-on "node agent." Prior-art (`04`) recommends **hybrid: push placement, pull pickup**, central stays the sole scheduler. This is a new normative architectural decision — get operator sign-off before building. (This is "Path B" from the fork above.)
6. **Validate the CPU proxy.** `cpu_source: load` shipped; `cpu_source: top` (true %CPU via `top -l 1`) is pre-wired as a config flip. Once real data exists, decide whether load-ratio is a good-enough %CPU proxy. Cheap to change later — not a blocker, just a known open question.

Tracking bead for the follow-up: **`hk-e6gs`** (deferred epic, `codename:worker-report`) — the checklist above is mirrored in its description.

---

## What's deferred (and why it's fine to defer)
- **Dynamic autoscale / AIMD** → Phase 3. Needs Phase-1 data first; guessing the control law now is premature.
- **Capability-aware routing, per-box cost attribution, warm-worktree affinity** → want a 2nd worker + the resident agent (`03`).
- **Naming the central node** → cosmetic; decide later.

## Next action
**None active — initiative parked after Phase 2.** Phases 1+2 are on main and off-by-default. Resume at the remote-node Phase 3 pickup checklist above, gated on the remote e2e going green + a live-validation window. Tracked for follow-up in ROADMAP.md (tier-4) and captain-lanes.md (tier-2).
