# M2-7 design pass — fold in the codex daemon-harness WAL-guard

**Task:** TASKS.md M2-7 (`plans/2026-07-13-code-revamp/TASKS.md:104`) — "(C7) Fold in the
codex daemon-harness WAL-guard." Census home: `plans/2026-07-12-codebase-census/REPORT.md:35`
("the codex WAL guard is 380 lines of symptom-treatment"). Roadmap home: `ROADMAP.md:73`/`:98`
(WAL-guard → M2). Spec clause: AIS-017 (`.kerf/works/2026-07-14-agent-input-substrate/05-spec-drafts/agent-input.md:229-233`).

**Verdict up front: ADAPT, not delete. AIS-017 confirmed with code evidence.** The real Ack
does NOT make it redundant — different concern. But the guard's *frequency and home* change:
per-launch symptom-scrubber → boot-time / lifecycle crash-recovery, rehomed to codex process
lifecycle, with graceful termination in the structured driver as the primary prevention.

Rides M2-2 (structured driver supplies graceful term) + M2-5 (replay harness proves
stalled→structured-stale, not SIGKILL-and-scrub). **NOT on the critical path.**

---

## 1. What the guard actually is (the code)

- File: `internal/daemon/codexwalguard.go`, **380 lines** (`wc -l` = 380 — matches the census
  claim exactly).
- Entry point: `cleanCodexStaleWAL(projectRoot, codexHome)` (`codexwalguard.go:128`).
- Call site: `CodexHarness.LaunchSpec` (`internal/daemon/codexharness.go:98`) — **runs once per
  codex launch**, before `buildCodexLaunchSpec` builds the spawn spec. This is the "per-launch
  symptom-treatment" the census names.
- Machinery in the 380 lines: glob `$CODEX_HOME/state_*.sqlite-wal`; `lsof` unheld-check on both
  the `-wal` and its base `.sqlite` (`fileHasOpenHandle`, `:341`); backup `-wal`+`-shm` into
  `$CODEX_HOME/.wal-backup-<ns>/` (`:199-221`); a TOCTOU re-check (`:232-247`) because
  `$HOME/.codex` is **shared across all concurrent codex runs** and the backup copy is slow; then
  `os.Remove` the sidecars (`:249-256`); reap old backup dirs to 5 (`reapCodexWALBackupDirs`,
  `:289`). A required config key `codex.stale_wal_max_bytes` is read fail-loud but is only a
  **secondary log-classification signal**, NOT a cleanup gate (`:29-39`, hk-xisvb).

Note: `internal/daemon/walcheckpoint.go` is a **different WAL** (the `beads.db` WAL, checkpointed
for `br` write latency — `walcheckpoint.go:2-20`). Out of scope for M2-7; do not touch it.

## 2. Root cause it treats as a symptom (file:line)

Cause chain, from the guard's own header (`codexwalguard.go:7-12`) plus the kill path:

1. codex persists session/thread state in `$CODEX_HOME/state_*.sqlite` with SQLite `-wal`/`-shm`
   sidecars (`codexwalguard.go:7-9`).
2. The daemon terminates a codex run via `killProcessWithGrace`: **SIGTERM → grace poll →
   SIGKILL** (`workloop.go:5872-5875`). When the SIGKILL lands (hung child ignoring SIGTERM, or a
   daemon SIGKILL / OOM / host crash) codex dies **before it clean-closes / checkpoints its SQLite
   WAL**.
3. The leftover stale `state_*.sqlite-wal` corrupts codex's session state on the **next** launch:
   codex exits in <10s with "exited without advancing HEAD" — a silent fast-fail (`codexwalguard.go:9-12`).

So the guard treats the **symptom** (a stale WAL sidecar sitting on disk) rather than the two
**causes**: (a) ungraceful SIGKILL of a *stateful* child, and (b) the absence of a crash-recovery
step, which forces recovery to be re-done on *every* launch instead of once after a kill/crash.
It is a per-launch scrubber precisely because there is no lifecycle recovery seam today.

## 3. Delete vs. adapt — with evidence

**Does the structured driver's real Ack (AIS-003) make it redundant? NO.**

The Ack confirms **input acceptance** — "the agent accepted this input," the lost-input concern
(agent-input.md AIS-003, AIS-INV-001). The WAL corruption stems from **process termination**, not
input delivery: a perfect input-Ack says nothing about whether a SIGKILL'd child left a stale WAL
behind. Different failure class, different fix. Evidence: `codexwalguard.go:7-12` roots the bug in
"a codex run is killed mid-flight (… SIGKILLs a hung implementer)", never in input; AIS-017 states
the same ("compensates for ungraceful SIGKILL …, NOT for lost input; a real input ack does not by
itself make it redundant" — agent-input.md:233). **AIS-017's "adapt-not-delete" is CONFIRMED.**

**But the structured driver reduces the need, on two axes (AIS-017, agent-input.md:233):**

- **(a) Graceful turn-termination.** The structured Codex app-server/reactor driver (M2-2) can
  issue a protocol-level `turn/interrupt` / clean shutdown instead of the substrate's
  SIGTERM→SIGKILL escalation (`workloop.go:5872`). A codex that shuts down gracefully checkpoints
  and closes its SQLite WAL, leaving no stale sidecar — the corruption source, removed at the root
  for the common "daemon decides to stop this run" case.
- **(b) Positive fast-fail.** Today the guard exists partly to detect the *silent exit-0* fast-fail
  ("exited without advancing HEAD"). The structured driver instead emits a **structured
  launch-failure event** on no-handshake-within-bound (AIS-017 fast-fail; agent-input.md:394) — so
  a bad launch is a loud, typed event, not a symptom to be reverse-engineered from a <10s exit.

**Residual that graceful termination CANNOT cover** → why we adapt, not delete:

- Daemon SIGKILL of itself, OOM-kill, host crash, and a codex child that *ignores* SIGTERM all
  still SIGKILL codex and leave a stale WAL. `workloop.go:5872` keeps SIGKILL as the escalation
  backstop by design. So some recovery step MUST survive.

## 4. The adapt shape

Collapse the per-launch symptom-treatment into a **boot-time / lifecycle crash-recovery step**,
rehomed to codex process lifecycle:

1. **Primary prevention (in the M2-2 driver):** structured `turn/interrupt` graceful termination
   replaces SIGTERM→SIGKILL for the ordinary daemon-initiated stop; SIGKILL stays only as the
   escalation backstop. Structured launch-failure event replaces silent-exit detection.
2. **Residual recovery (the surviving guard):** run the stale-WAL sweep **once per daemon boot /
   codex-lifecycle recovery**, NOT on every `LaunchSpec`. This mirrors two existing precedents in
   the same package — `walcheckpoint.go`'s daemon-startup pre-flight and `orphansweep.go`'s
   SIGKILL-recovery sweep (`orphansweep.go:239`, `:883`). Retire the `codexharness.go:98`
   per-launch call site.
3. **Keep the conservative safety core** — `lsof`-unheld on `-wal`+base, the TOCTOU re-check
   (`walUnchanged`), and the pre-remove backup. These are NOT deletable: `$CODEX_HOME` is shared
   across concurrent runs (`codexwalguard.go:223-231`), so even a boot-time sweep must not yank a
   sidecar a freshly-dispatched concurrent codex is writing. The safety logic relocates unchanged;
   only its *frequency* (per-launch → per-recovery) and *home* (daemon harness → codex lifecycle)
   change.
4. **Rehome.** Per AIS-017, the guard's post-M2 home is "codex process lifecycle proper, not this
   agent-input driver spec" (agent-input.md:58, :233). M2-7 does not put WAL logic in the driver
   spec; it (a) has the driver adopt graceful term + fast-fail, and (b) demotes+relocates the
   guard to a codex-lifecycle recovery seam.

Net effect: the guard shrinks from a 380-line per-launch pre-flight to a lifecycle-scoped recovery
sweep. "The guard's concern is handled by the structured protocol, not a bolt-on" (M2-7 acceptance)
holds in the precise sense that **prevention** moves into the protocol (graceful term + fast-fail),
while a thin, correctly-scoped **recovery** remainder survives for residual SIGKILL/crash paths.

## 5. Dependencies / criticality

- **M2-2** supplies the graceful `turn/interrupt` termination and the structured launch-failure
  event — the prevention half. Without M2-2 there is nothing to fold *into*.
- **M2-5** replay harness proves the new equilibrium: a stalled-agent injection now resolves to a
  structured stale/launch-failure event (hook-acked-or-stale oracle), not to "SIGKILL then scrub
  next launch." That is the test that lets the per-launch call site be safely retired.
- **NOT on the critical path.** M2-7 rides M2-2/M2-5; the ack contract (M2-1) and the paste/hook
  path (M2-3) do not depend on it. The guard can keep running per-launch, harmlessly, until the
  fold-in lands.

## 6. Residual / open

- **VERIFY (M2-2 driver pass):** does codex actually checkpoint+close its SQLite WAL on a graceful
  `turn/interrupt` / SIGTERM shutdown? The whole prevention argument assumes it does. If codex
  leaves a WAL even on clean shutdown, graceful term buys nothing and the boot-recovery step
  carries the full load (still adapt-not-delete, but the frequency reduction is the only win). This
  is an open verification, not a settled fact.
- `codex.stale_wal_max_bytes` (secondary log-classification key, `codexwalguard.go:84`) travels
  with the relocated recovery step; it is not a cleanup gate and stays as-is.
- The backup-dir reaper (`walBackupKeepLast = 5`, `codexwalguard.go:72`) relocates unchanged.
- `walcheckpoint.go` is the beads.db WAL — explicitly out of scope; do not conflate.
