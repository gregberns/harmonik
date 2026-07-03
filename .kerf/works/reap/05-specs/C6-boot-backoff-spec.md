# C6 — Boot-level daemon restart backoff — Change Spec

**Component:** Throttle rapid sequential daemon boots with a crash-aware exponential backoff backed by a durable last-boot marker, so a crash-and-re-pull loop cannot produce 10 boots/day.
**Bead:** hk-7t9g1 · **Goal:** G5 · **Analysis gap:** #7
**Spec home:** `specs/process-lifecycle.md` §4.2 PL-005 (new sub-clause **PL-005c** boot backoff) + §4.1 file-surface inventory; `specs/operator-nfr.md` §8 (new exit code **26** `daemon-boot-throttled`); `specs/event-model.md` §8.7 (new `daemon_boot_throttled` event).

---

## Requirements (carried forward from 03-components.md)

- **C6-R1** — The daemon MUST persist a durable, project-scoped last-boot record (timestamp + exit-disposition of the prior boot) that survives process death.
- **C6-R2** — On `daemon.Start`, if the prior boot started within the backoff window AND exited abnormally, the daemon MUST apply an exponential backoff (delay before proceeding, or refuse with a retry-after diagnostic) keyed on the recent-boot count.
- **C6-R3** — The backoff MUST distinguish an operator-intended restart (prior clean shutdown) from a crash-loop (prior abnormal exit): a clean prior shutdown MUST NOT be throttled.
- **C6-R4** — The backoff envelope (base, cap, window) MUST have finite defaults and be operator-configurable, mirroring the supervisor backoff knobs.
- **C6-R5** — The applied backoff MUST be observable (log + event, `daemon_boot_throttled` with the delay and recent-boot count).
- **C6-R6** — The backoff state MUST be crash-safe and self-healing: a stale last-boot marker from long ago MUST NOT throttle a legitimate boot (window-bounded).
- **C6-R7** — A test MUST reproduce the 10-boots-in-a-window shape and assert that under the default backoff the Nth rapid crash-boot is delayed/refused, while a clean-shutdown restart is not throttled.

## Research summary (from 04-research/C6)

**Fully greenfield at the boot level.** No `last.boot` / `daemon_boot_throttled` / boot-backoff anything exists (`grep` → zero matches). `daemon.Start` emits `daemon_started` and proceeds immediately; the pidfile lock (PL-002) prevents CONCURRENT daemons but NOT rapid sequential re-boots (each boot is a fresh OS process acquiring the auto-released lock cleanly) — exactly the incident's 10-boots-in-a-day, the last 5 within ~14 minutes. The reusable algorithm is the SUPERVISOR's exponential-backoff-with-jitter + dual crash-loop detector (`internal/supervise/supervisor.go`: `backoffWithJitter`, base 1s→cap 60s ×2, `MaxRestarts=5`, absolute cap + sliding-window) — but it is IN-PROCESS (one supervisor over many restarts keeps `restartTimes` in memory), whereas C6 spans many processes so the state MUST be on disk. **Marker format (research RQ3, Option B):** an append-only `.harmonik/boots.jsonl` of `{started_at, exit_disposition}` rows; the window count is derived by scanning rows newer than `now - window`; window-bounding + self-healing fall out naturally; prune rows older than the window on each boot. **Clean-vs-crash (RQ4):** a start row with a matching `exit_disposition=clean` (written by the PL-011 graceful drain before exit) = clean restart, NOT throttled; a start row with NO matching exit row (process died before writing disposition — SIGKILL/panic/OOM) = abnormal, the SAFE default. **Refuse-vs-delay (RQ5):** DELAY (sleep then proceed) for early window positions, escalating to REFUSE (exit + retry-after) once the recent-boot-count exceeds a hard ceiling — mirroring the supervisor's delay-then-crash-loop-error shape. **Placement (RQ6):** the throttle MUST live INSIDE `daemon.Start` (an external wrapper cannot be assumed), AFTER the pidfile-lock acquire (so a concurrent boot exits 5 first) but BEFORE auto-pull/dispatch (so a throttled boot does not spawn). New event `daemon_boot_throttled`; new exit code (coordinate with C5=24 → C6=**26**, since 25 is the interim supervisor-running code).

## Approach

A durable boots-log + a boot-backoff gate inside `daemon.Start`. Decisions recorded:

- **Decision 1 (marker format, research Option B):** append-only `.harmonik/boots.jsonl`, one row `{boot_id, started_at, daemon_generation, exit_disposition}` per boot. `exit_disposition ∈ {clean, abnormal, unknown}`; written `unknown` at start, updated to `clean` by the PL-011 drain or left `unknown` on crash. The `daemon_generation` is a monotonic counter (max prior generation + 1) — this IS the generation tag C2's run-ledger and C5's marker reference.
- **Decision 2 (window-bounded count, C6-R6):** the recent-boot count = number of rows with `started_at > now - WINDOW`. Rows older than `WINDOW` are pruned on each boot (self-healing: a months-old marker never throttles). Default `WINDOW = 10 minutes`.
- **Decision 3 (clean-vs-crash, C6-R3):** the gate considers only PRIOR rows whose `exit_disposition ∈ {abnormal, unknown}` for throttle accounting. A prior row with `exit_disposition=clean` is NOT counted — an operator-intended restart after a clean stop is never throttled. The "missing disposition = abnormal" default errs toward throttling (the safe, under-spending direction; research R2 asymmetry note).
- **Decision 4 (backoff envelope, C6-R4):** mirror the supervisor envelope — `base = 1s`, `cap = 60s`, `×2` exponential keyed on the in-window abnormal-boot count, with jitter (`backoffWithJitter` reuse). Finite defaults, operator-configurable via daemon config/env (`HARMONIK_BOOT_BACKOFF_BASE`, `_CAP`, `_WINDOW`, `_CEILING`).
- **Decision 5 (delay-then-refuse, research RQ5):** for in-window abnormal-boot count `k`: if `k < CEILING` (default 5) → DELAY `min(base × 2^(k-1), cap)` (sleep, jittered) then PROCEED; if `k >= CEILING` → REFUSE with exit code 26 (`daemon-boot-throttled`) + a retry-after diagnostic (the next-eligible wall-clock time). This caps a wrapper-driven process pile-up (a pure-delay design risks unbounded sleeping processes).
- **Decision 6 (placement, research RQ6):** the gate runs in `daemon.Start` at a new PL-005 step **1.6** — AFTER the pidfile-lock acquire (PL-005 step 1, so a concurrent boot exits 5 first) and AFTER the flywheel-lock acquire gate (C5 PL-002c, PL-005 **step 1.5**, if flywheel topology), but BEFORE the orphan sweep (step 3) / queue-load / auto-pull. The boot-backoff gate is step 1.6 — a DISTINCT label from C5's flywheel-lock gate at step 1.5; the two gates do not collide. A delayed boot still runs the full normal startup after the sleep; a refused boot exits before the sweep.
- **Decision 7 (observability, C6-R5):** emit `daemon_boot_throttled{delay_ms, recent_abnormal_boot_count, window_ms, action}` (`action ∈ {delayed, refused}`) — a NEW §8.7.x event. The boots.jsonl start-row is written before the gate so a refused boot is still recorded (counts toward the next boot's window).

### Spec text to add — PL-005c (process-lifecycle.md §4.2, after PL-005's startup-order list)

> **PL-005c — Boot-level restart backoff**
>
> The pidfile lock (§PL-002) prevents CONCURRENT daemons but NOT rapid SEQUENTIAL re-boots: each `harmonik daemon` invocation is a fresh OS process that acquires the auto-released lock cleanly. A crash-and-re-pull loop (a crashed flywheel daemon repeatedly re-launched by an operator or wrapper) can therefore produce many boots in a short window — the 2026-05-30 incident booted 10 times in a day, the last 5 within ~14 minutes, each immediately auto-dispatching paid sessions. The daemon MUST throttle rapid sequential boots with a crash-aware backoff backed by a durable marker.
>
> **Boots log.** The daemon MUST maintain an append-only `.harmonik/boots.jsonl` (within the §PL-004 file surface). Each row records `{boot_id, started_at, daemon_generation, exit_disposition}` where `exit_disposition ∈ {clean, abnormal, unknown}` and `daemon_generation` is a monotonic counter (max prior generation + 1). The daemon MUST append a row with `exit_disposition=unknown` early in `daemon.Start` (so the row survives a subsequent crash); the §PL-011 graceful-drain path MUST update the row to `exit_disposition=clean` before exit; a crash (SIGKILL / panic / OOM) leaves the row `unknown`. The `daemon_generation` of the current boot is the generation tag referenced by §PL-006f (run-ledger) and §PL-002c (flywheel-lock marker).
>
> **Backoff gate.** The gate runs at a new startup **step 1.6** — after the pidfile-lock acquire (step 1) and after the flywheel-lock acquire gate of §PL-002c at **step 1.5** (when flywheel topology), and BEFORE the orphan sweep (step 3) / queue-load / auto-pull. (Step 1.5 is the flywheel-lock gate per §PL-002c; step 1.6 is this boot-backoff gate — two distinct labels, no collision.) The gate computes the recent-abnormal-boot count `k` = the number of `boots.jsonl` rows whose `started_at > now - WINDOW` AND whose `exit_disposition ∈ {abnormal, unknown}` (a prior row with `exit_disposition=clean` is NOT counted — an operator-intended restart after a clean stop is never throttled). Rows older than `WINDOW` MUST be pruned on each boot (self-healing: a stale marker from long ago never throttles).
>
> Given `k` and a configurable envelope (`base`, `cap`, `WINDOW`, `CEILING`; finite defaults `base=1s`, `cap=60s`, `WINDOW=10m`, `CEILING=5`, exponential `×2` with jitter, operator-overridable via daemon config / environment, mirroring the §PL-019 supervisor backoff knobs):
>
> - if `k < CEILING` → the daemon MUST DELAY by `min(base × 2^(k-1), cap)` (jittered) then PROCEED with the normal startup;
> - if `k >= CEILING` → the daemon MUST REFUSE the boot with [operator-nfr.md §8] code 26 (`daemon-boot-throttled`) and a diagnostic naming the next-eligible wall-clock time (`now + min(base × 2^(k-1), cap)`).
>
> The applied backoff MUST be observable: the daemon MUST emit `daemon_boot_throttled` (per [event-model.md §8.7]) carrying `{delay_ms, recent_abnormal_boot_count, window_ms, action}` (`action ∈ {delayed, refused}`), and MUST log it per [operator-nfr.md §4.9 ON-035]. The boots.jsonl start-row is written BEFORE the gate so a refused boot is still recorded and counts toward the next boot's window.
>
> The backoff MUST be crash-safe (file-based, not in-memory — the marker spans process deaths) and window-bounded (a stale marker MUST NOT throttle a legitimate boot, C6-R6). A clean prior shutdown MUST NOT be throttled (C6-R3).
>
> Tags: mechanism
> Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

### Spec text to add — operator-nfr.md §8 + event-model.md §8.7

- **operator-nfr §8** — add row: `26 | daemon-boot-throttled | The daemon's boot-backoff gate per [process-lifecycle.md §4.2 PL-005c] found the recent-abnormal-boot count at or above CEILING within the window. | `daemon_boot_throttled{action=refused}` | Wait for the retry-after time in the diagnostic; investigate the crash cause (the daemon is crash-looping). ` + ON-001/ON-003 catalog entry + `CommandExitCodeSets` registration for the `CommandDaemon` command. **Ordering obligation (shared with C5):** code 26 MUST be added AFTER C5's ordered §8 obligation has (1) absorbed code **25** into the registry and (2) added the `CommandSupervise` `CommandName`, so the registry is contiguous (0–25) before 26 is appended. C5 owns steps (1)-(2); C6 appends 26. If C5 and C6 land in one integration pass, perform: absorb 25 → add `CommandSupervise` → add 24 → add 26. `VerifyCommandExitCodeSets` MUST resolve 24, 25, AND 26 after the combined add.
- **event-model §8.7** — add `daemon_boot_throttled` (class O, daemon-core, observability+audit), payload `{delay_ms, recent_abnormal_boot_count, window_ms, action, throttled_at}`, next free §8.7.x number. N-1 tolerated.

### Spec text to amend — PL-005 startup-order list + PL-011

- **PL-005 step list** — insert TWO inserted gates between step 1 (pidfile lock) and step 3 (orphan sweep), retaining existing integer labels: step **1.5** ("Acquire the flywheel lock per §PL-002c when flywheel topology", C5) and step **1.6** ("Apply the boot-backoff gate per §PL-005c", this clause). Step 1.5 is C5's flywheel-lock gate; step 1.6 is this boot-backoff gate. (If C5/PL-002c is not yet merged at integration time, step 1.5 is reserved for it and the boot-backoff gate remains 1.6 — the integration pass MUST land both labels together so the numbering is unambiguous in the final merged PL-005 list.)
- **PL-011 drain** — add: the graceful-drain path MUST update the current boot's `boots.jsonl` row to `exit_disposition=clean` before the event-bus flush (step 4-ish), so an operator-intended restart is not throttled.

## Files & changes

| File | Change | Why |
|---|---|---|
| `specs/process-lifecycle.md` | Add PL-005c; insert PL-005 step 1.6 (boot-backoff gate; step 1.5 is C5's flywheel-lock gate); amend PL-011 (clean-disposition write); PL-004 inventory adds `.harmonik/boots.jsonl`; changelog row. | Normative boot-backoff. |
| `specs/operator-nfr.md` | Add §8 code 26 + ON-001/ON-003 catalog + register on `CommandDaemon`; changelog row. MUST follow C5's 25-absorption + `CommandSupervise` add (registry contiguous 0–25 before 26). | Exit-code registration. |
| `specs/event-model.md` | Add `daemon_boot_throttled` §8.7.x row. | Event registration. |
| `internal/lifecycle/bootlog.go` (new) | `boots.jsonl` reader/writer/prune + generation counter, behind a `BootLog` seam for test injection (clock injectable for window tests). | Marker mechanics. |
| `internal/lifecycle/bootbackoff.go` (new, modeled on `supervisor.go` backoff) | `backoffWithJitter` reuse; compute `k`; delay-vs-refuse decision. | Backoff algorithm. |
| `internal/daemon/daemon.go` | Append the start row early; insert the step-1.5 gate (delay or exit 26 + emit `daemon_boot_throttled`); update row to `clean` on PL-011 drain. | Production wiring. |
| `internal/operatornfr/exitcode.go` + `commandcodes.go` | Add code 26 to §8 registry + `CommandExitCodeSets`. | `VerifyCommandExitCodeSets` passes. |
| `core/...` | `DaemonBootThrottledPayload`. | Event payload. |

## Acceptance criteria

- **AC1 (C6-R7 crash-loop, the reproduce-first test):** Inject `boots.jsonl` with `k` prior abnormal rows within `WINDOW` (clock injected). For `k=1..CEILING-1`, `daemon.Start` DELAYS by `min(base × 2^(k-1), cap)` (assert the sleep duration via the injectable clock) then proceeds. For `k>=CEILING`, `daemon.Start` REFUSES with exit 26 and a retry-after diagnostic; `daemon_boot_throttled{action=refused}` emitted.
- **AC2 (C6-R3 clean restart NOT throttled):** `boots.jsonl` with `k` prior rows ALL `exit_disposition=clean` within `WINDOW` → no delay, no refusal (an operator-intended restart proceeds immediately).
- **AC3 (C6-R6 self-healing):** `boots.jsonl` with `k >= CEILING` rows ALL older than `WINDOW` → not counted → boot proceeds; the stale rows are pruned.
- **AC4 (C6-R1 crash-safe):** A start row written `unknown`; SIGKILL before drain; the next boot reads the `unknown` row and counts it as abnormal (the safe default).
- **AC5 (C6-R5 observability):** `daemon_boot_throttled{delay_ms, recent_abnormal_boot_count, window_ms, action}` emitted on both delay and refusal; logged per ON-035.
- **AC6 (placement):** The gate runs at step 1.6 — AFTER the pidfile lock (step 1; a concurrent boot still exits 5, not 26), AFTER the flywheel-lock gate (step 1.5, C5), and BEFORE the orphan sweep (step 3; a refused boot does NOT run the sweep or spawn).
- **AC7 (registry):** `VerifyCommandExitCodeSets()` passes with codes 24, 25, AND 26 all resolvable to §8 entries (the registry is contiguous 0–26 after the C5+C6 ordered add); code 26 resolves to a §8 entry.

## Verification

```bash
go test ./internal/lifecycle/... -run 'BootLog|BootBackoff|Throttle|Generation' -count=1
go test ./internal/daemon/...    -run 'BootBackoff|Start|Throttle|Generation'   -count=1
go test ./internal/operatornfr/... -run 'VerifyCommandExitCodeSets|ExitCode'    -count=1
```

Manual: simulate the incident — write 5 abnormal boot rows within 14 minutes; boot; confirm the 6th is refused with exit 26 + a retry-after time. Stop the daemon cleanly; immediately re-boot; confirm no throttle.

## Error handling & edge cases

- **Missing exit disposition** — a start row with no matching `clean` write = abnormal (the safe default; errs toward throttling / under-spending, research R2).
- **Clock regression** — window computation uses wall-clock UTC (a minutes-scale window; monotonic does not survive reboot, research R3); a backward clock jump could under- or over-count — bounded by the window; structured-log a warning if `started_at` rows are non-monotonic.
- **Corrupt boots.jsonl row** — a malformed line is skipped with a warning (ON-035), not fatal; the window count proceeds on the parseable rows.
- **Unbounded growth** — prune-older-than-window on each boot bounds the file.
- **Refused boot + wrapper retry storm** — the refused boot's start-row counts toward the window, so a wrapper retrying immediately keeps hitting exit 26 until the window slides — the intended throttle (the wrapper backs off or the operator investigates).

## Migration / backwards compatibility

Additive: a fresh daemon with no `boots.jsonl` has `k=0` → no throttle (first boot always proceeds). The file is created on first boot. Code 26 is a new §8 entry within the N-1 window; existing 0–25 mappings unchanged. The `daemon_boot_throttled` event is N-1 tolerated.

---

## Test beads (SHARED across C1–C6 — filed per the jig's Validation/Acceptance Tests gate)

Two beads cover the reap work end-to-end (scenario + exploratory), filed at the Change-Spec gate. The component-level scenario/exploratory criteria above map into these two beads; the bead IDs are recorded here for traceability across all six component specs.

**Filed bead IDs:** scenario = **hk-a31od**; exploratory = **hk-izs8s** (both labelled `codename:reap`, priority 1).

- **Scenario-test bead** (`scenario-test` label, **hk-a31od**): `scenario: reap — boot orphan-sweep remediates + reconciles to terminal JSONL counts`. Command under test: `harmonik daemon` (boot path, twin-substrate). Lifecycle states exercised: (a) orphaned run-worktree → removed (C1); (b) stuck `in_progress` bead with drained intent + absent queue.json + ledger row → reset (C2); (c) `dispatched` item with dead run → pending/failed/completed (C3); (d) crash-loop boots within window → throttled (C6). Observable terminal condition: `.harmonik/events/events.jsonl` carries `daemon_orphan_sweep_completed{worktree_dirs_removed, bead_in_progress_reset}`, `queue_item_reconciled{reason=dead_run_*}`, and `daemon_boot_throttled{action}` with the expected counts.
- **Exploratory-test bead** (`exploratory-test` label, **hk-izs8s**): `explore: reap — supervise stop reaps child tree; second flywheel start refused (exit 24)`. Commands under test: `harmonik supervise stop` (expected side-effect: no `harmonik-<project_hash>-` tmux sessions or spawned `claude` remain — C4); `harmonik supervise start` while a flywheel daemon is live (expected output: exit code 24 + stderr diagnostic naming the live owner PID + layer + lock path — C5); `harmonik daemon` while crash-looping (expected output: exit 26 + retry-after — C6).

> **Filing:** the two beads are created with `br create "<title>" --type task --label scenario-test` / `--label exploratory-test`, labelled `codename:reap`, and depended-on by the four implementation beads (hk-9eury, hk-xb5yi, hk-li14r, hk-7t9g1). Their IDs are recorded in the work's `spec.yaml` / change-spec-review.md after filing. (See the Integration pass for the dependency wiring.)
