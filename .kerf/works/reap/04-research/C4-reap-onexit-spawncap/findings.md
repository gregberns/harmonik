# C4 — Reap-on-exit + concurrent-spawn cap — Research findings

Component: when the supervisor/daemon dies, kill its provenance-marked child claude+tmux tree
(reap-on-exit); and enforce a hard upper bound on concurrently-spawned child sessions (spawn cap)
that refuses the (cap+1)th spawn. Beads hk-xb5yi, analysis gaps #4 #5, goals G3 G4.
Anchors verified at `2e49a8df`.

## RQ1 — What does the supervisor do on exit today, and what survives it (C4-R1)?

**Finding:** The supervisor terminates only its DIRECT child; tmux-substrate-hosted claude panes survive.

- `internal/supervise/supervisor.go:185` `Run` is the blocking loop (spawn → policy → backoff → respawn). `buildCmd` (`:330`) sets `SysProcAttr.Setpgid: true` (`:342`) so the direct child is its own PGID; `cmd.Env = append(os.Environ(), s.spec.Env...)` (`:336`). `terminateChild` (`:349`) does SIGTERM→bounded-wait→SIGKILL (PL-011), `forwardSignal` (`:366`) signals the child's PGID.
- **Gap:** the spawned `claude` implementer/reviewer sessions run in TMUX panes (daemon-side, via the tmux substrate), NOT in the supervisor's PGID. PGID-forwarded SIGTERM does not reach them. When `Run` returns (ctx-cancel, Stop, crash-loop at `:279-296`, clean exit) there is NO enumerate-and-kill of provenance-marked claude/tmux children. This is the incident's "8 live claude + a pi still spending" after the supervisor died.

## RQ2 — What provenance + enumeration primitives exist to drive reap-on-exit (C4-R1, R2)?

**Finding:** All the kill/enumerate primitives the next-boot sweep uses already exist and are reusable on the exit path:
- **Tmux session enumeration + kill by prefix** — `internal/lifecycle/tmux/orphansession.go:12-21` (`SweepOrphanTmuxSessions`) kills sessions whose name matches the exact `harmonik-<12-char-hash>-` prefix; a different project's hash is untouched (`orphansession_test.go:225`). This covers `harmonik-<project_hash>-flywheel` AND the per-run windows.
- **Handler/claude subprocess kill** — `lifecycle.SweepOrphanHandlers` (SIGTERM→SIGKILL, init-reparented + provenance-marked) and `lifecycle.SweepOrphanBr`.
- **PL-006a provenance marker** — env var (Linux) / PGID (darwin) / tmux `harmonik-<project_hash>-` prefix (`internal/lifecycle/provenance.go`, `provenance_pl006a_test.go`). A child lacking the marker MUST NOT be killed (C4-R1).
- **PL-006d live-supervisor exclusion (C4-R2)** — sentinel `.harmonik/cognition/supervisor.sentinel` + `supervisor.pid` + `kill(pid,0)`; `orphansweepactiveheld_pl007_test.go` covers skipping a LIVE coordinator session. Reap-on-exit reaps only its OWN descendants and must skip a DIFFERENT live supervisor's session via the same check.

**So C4-R1/R2 is "call the existing sweep primitives on the exit path", not new kill machinery.** The new work is the trigger (defer in `Run`, and in `stop.go`) + scoping the enumeration to THIS supervisor's spawned set.

## RQ3 — Tradeoff: how to scope reap-on-exit to "my own children" (C4-R1)?

- **Option A — reuse the project-prefix sweep on exit** (kill all `harmonik-<project_hash>-` tmux + provenance-marked claude, minus the live-supervisor sentinel). Pro: zero new tracking, reuses tested code. Con: in a (forbidden but possible) two-daemon-same-project state it could kill the other's children — but C5's single-flywheel lock makes that state unreachable, so this is safe under the C5 invariant.
- **Option B — track spawned run_ids/session names in-supervisor and kill only that set.** Pro: precise. Con: in-memory set is lost on SIGKILL of the supervisor itself (the very case that matters) → must be durable, which converges on C2's run-ledger.
- **Recommendation:** Option A on the interceptable-exit path (ctx-cancel/Stop/clean/crash-loop-return), relying on C5's single-flywheel invariant for safety; the uninterceptable SIGKILL case is covered by the next-boot sweep (C4-R3 documents this, not a new requirement).

## RQ4 — Daemon-side reap (C4-R3) and the SIGKILL boundary

**Finding:** The daemon's graceful-shutdown path (PL-011 drain) should best-effort kill its spawned implementer/reviewer claude+tmux on the way down — closing the live-spend window between death and next boot. SIGKILL of the daemon is uninterceptable; that case relies on the next-boot `RunOrphanSweep` (area A, already wired) which kills `harmonik-<project_hash>-` tmux sessions. C4-R3 is therefore: add a best-effort reap to the daemon drain path (reusing the same sweep primitives) + DOCUMENT that SIGKILL relies on next-boot. No new requirement for the SIGKILL case.

## RQ5 — Spawn cap: where is the spawn site and what is the live gauge (C4-R4..R6)?

**Finding:** The daemon spawns N child claude via the workloop, gated today only by the operator-set `--max-concurrent` (EM-049 capacity gate):
- `internal/daemon/workloop.go:478,595` — "if `runRegistry.Len() >= maxConcurrent`: sleep and retry (at capacity)"; `maxConcurrent` normalized zero→1 (`:422-423`). The spawn happens at the goroutine-per-bead site (`:1093` "Register the run and spawn a goroutine"; Register/Unregister/Len on `RunRegistry`, `internal/daemon/runregistry.go:79,94,102,117`).
- **`RunRegistry.Len()` IS the live-child gauge** (C4-R6): incremented on `Register` at spawn, decremented on `Unregister` at watcher-goroutine reap (PL-014/HC-011). It is a live gauge, not a monotonic counter — exactly what C4-R6 needs.
- **Gap:** `--max-concurrent` is operator-set and UNBOUNDED-by-default in the auto-pull path; there is NO hard safety ceiling independent of it. C4-R4 adds a finite-default hard cap C; C4-R5 refuses the (C+1)th spawn (do NOT launch) and emits a typed `spawn_cap_exceeded` event rather than spawning silently.

**Tradeoff (cap enforcement point):**
- **Option A — gate at the same `runRegistry.Len() >= cap` check** alongside the `>= maxConcurrent` check (cap = `min` ceiling). Pro: one gauge, one site, trivially correct decrement-on-exit. Con: cap and max-concurrent semantics must be clearly distinct (cap = safety ceiling that REFUSES + emits; max-concurrent = throttle that SLEEPS+retries). The change spec must specify: at-cap = refuse+emit+do-not-retry-spawn (a hard stop / pause signal), NOT sleep-retry.
- **Option B — a separate atomic counter at the OS-spawn boundary.** Pro: catches spawns outside the RunRegistry path (reviewer worktrees, resume sessions). Con: duplicate gauge to keep in sync. **Recommendation:** Option A for the implementer-run path (the dominant fan-out) + audit whether reviewer/resume spawns bypass RunRegistry (they may — `CreateReviewerWorktree` is a separate path); if so, the cap must count them too. Flag as a change-spec investigation.

## RQ6 — Interaction with EM-049 and `credfence` (scope boundary)

**Finding:** The spawn cap is a SAFETY ceiling distinct from `--max-concurrent` (operator throughput knob) and distinct from `credfence`'s per-day-USD/max-runs spend ceiling. reap's cap is a count of concurrently-LIVE children (blast-radius limit); credfence's is a cumulative dollar/run meter (cost limit). They compose: cap bounds instantaneous fan-out, budget bounds cumulative spend. The change spec must state they are independent gates and reap does NOT own the dollar meter (credfence does).

## Risks / unknowns
- **R1 (reviewer/resume spawns outside RunRegistry):** the cap gauge must count ALL spawned claude (implementer + reviewer + resume) or it under-counts blast radius — investigation deliverable.
- **R2 (refuse-vs-sleep semantics):** at-cap behavior differs from at-max-concurrent (refuse+emit vs sleep+retry); must be spec'd to avoid a busy-spin or a silent stall.
- **R3 (exit-path reap killing a healthy concurrent run):** under the single-daemon invariant (pidfile lock) there is at most one daemon per project, so reap-on-exit cannot hit a sibling's healthy run; this depends on C5 holding — note the coupling.

## No-blocker assertion
No blocker prevents a C4 change spec. C4-R1/R2 reuse existing sweep+provenance primitives on a new
trigger; C4-R4..R6 extend the existing RunRegistry gauge with a hard ceiling + typed refusal. Two
change-spec investigations: (a) do reviewer/resume spawns bypass RunRegistry (cap-counting), (b)
refuse-vs-sleep semantics at cap. C4 couples to C5 (single-flywheel) for exit-reap safety.
