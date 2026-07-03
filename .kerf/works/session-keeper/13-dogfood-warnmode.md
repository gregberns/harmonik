# 13 — Dogfood: session-keeper Phase-1 warn-mode (live binary)

**Date:** 2026-06-03
**Binary:** `/Users/gb/go/bin/harmonik` (built Jun 3 12:23 = HEAD `35d8750c feat(keeper): Phase-1 warn-mode`)
**Env:** tmux 3.6a, jq 1.7.1, Claude Code v2.1.161, macOS (darwin 25.3.0)
**Harness:** throwaway agent `skdog`, throwaway tmux session `skdog-pane` (plain bash shell as injection target — chosen over a real Claude session for determinism/safety; the statusline script + injector mechanism were each exercised against real tmux).
**Method:** drove the REAL `scripts/keeper-statusline.sh` and REAL `harmonik keeper` against the live `.harmonik/keeper/` gauge surface. No mocks except the injection-target pane.

## Verdict summary

| Gate | Result | One-liner |
|------|--------|-----------|
| G1 statusline writes correct `.ctx` | **PASS** | `{pct,session_id,ts}` well-formed; skips on absent/NA pct. |
| G2 `.managed` opt-in gate | **PASS** | No marker → no-op exit 0; marker → watcher runs; 2nd keeper → exit 2 (lock). |
| G3 exactly-one injection on first upward crossing | **FAIL (injection) / PASS (event-half)** | Warn fires once and does NOT repeat, but the tmux injection **never happens** — idle-gate bug. |
| G4 `session_keeper_warn` in events.jsonl | **FAIL-by-design** | Standalone CLI wires `NoopEmitter{}`; event is discarded. Only stdout/slog signal exists. |
| G5 no-gauge self-check, non-destructive | **PASS (non-destructive) / FAIL (observability)** | No destructive action taken; but `no_gauge` is invisible (NoopEmitter + no stdout fallback). |
| Non-destructive overall | **PASS** | No `/clear`, no handoff, no file writes anywhere by the keeper. |

## Evidence

### G1 — statusline gauge (PASS)
Real `keeper-statusline.sh`, sample stdin `{"session_id":"sess-skdog-real","context_window":{"used_percentage":42.7}}`:
```
{"pct":42.7,"session_id":"sess-skdog-real","ts":"2026-06-03T20:45:38Z"}   # jq shape check: true
```
- after-/clear `{"session_id":"after-clear"}` (no pct) → exit 0, **no `.ctx` written**.
- explicit `"used_percentage":"NA"` → exit 0, **no `.ctx` written**.

Field path `.context_window.used_percentage` is correct and matches the script comment + statusline_test.go.

### G2 — `.managed` opt-in (PASS)
- No marker: `keeper: skdog not opted-in (.managed marker missing); no-op`, exit **0**.
- Marker present + below-threshold gauge: `keeper started for skdog (warn-pct=80, tmux="skdog-pane")`, process stays alive (watcher loop), lockfile holds live PID.
- Second concurrent keeper: `agent "skdog" already has a live keeper; exiting`, exit **2**. (single-keeper flock works)

### G3 — first upward crossing (injection FAILS)
Sequence: boot with gauge=50.0 (below), wait 7s, then write 85.0 ONCE and hold untouched 30s.
Keeper output:
```
keeper started for skdog (warn-pct=80, tmux="skdog-pane")
2026/06/03 13:46:27 WARN keeper: context window warn threshold crossed agent=skdog pct=85 warn_pct=80
keeper: warn — agent "skdog" context window at 85.0% (threshold 80.0%)
```
- Warn line count = **1** (correct: exactly one per upward crossing, no repeat while above).
- `tmux capture-pane -t skdog-pane`: shows only `skdog$` — **injected text count = 0**.

Re-test G3-alt (gauge pre-aged 12s above threshold BEFORE boot, untouched 25s): warn still fires once, injected-text count still **0**.

Isolation proof — the injector mechanism itself WORKS. Running the injector's exact 3 steps by hand
(`load-buffer -b hk-keeper-warn -` → `paste-buffer -b … -d` → `send-keys Enter`) delivered the full
wrap-up text into the pane (count = 1). So the defect is NOT the injector; it is the watcher's gate.

### G4 — events.jsonl (FAIL-by-design)
- `jq 'select(.type=="session_keeper_warn" or .type=="session_keeper_no_gauge")'` over events.jsonl = **0** typed events.
- (A bare `grep session_keeper_warn` returns 1 hit, but it is a substring inside a `reviewer_verdict`'s free-text `notes`, not a keeper-emitted event.)
- Root cause is intentional: `cmd/harmonik/keeper_cmd.go:103` → `keeper.NewWatcher(cfg, keeper.NoopEmitter{})`. The standalone process has no in-process event bus, so warn/no_gauge events are silently discarded. The bead's own reviewer APPROVE-notes already flagged this.

### G5 — no-gauge self-check + non-destructive (PASS non-destructive)
Removed `.ctx`, started keeper:
```
keeper started for skdog (warn-pct=80, tmux="skdog-pane")
```
- Pane: untouched (`skdog$` only) — no injection, no `/clear`.
- `session_keeper_no_gauge` in events.jsonl = **0** (NoopEmitter; and `emitNoGauge` has NO `fmt.Printf` fallback, unlike `emitWarn`) → the no_gauge self-check is **completely invisible** in the standalone CLI: no event, no stdout, only a discarded slog call.
- No destructive action: HANDOFF.md mtime unchanged, no new files, no `/clear`. **Non-destructive confirmed.**

Unit tests (`go test ./internal/keeper/`) all pass (13 tests) — they validate the event state-machine via `RecordingEmitter` with `IdleQuiesce:1ms` and `TmuxTarget:""`, so they NEVER exercise the live injection idle-gate. That gap is why the injection bug shipped green.

## Bugs found (candidate follow-up beads)

### BUG-1 (P1, real) — idle-gate makes the warn injection dead code
`internal/keeper/watcher.go:222-234`: the injection is gated behind `gaugeQuiesced` *inside the same
`warnArmed && !warnFired` block that fires the warn*. On the tick where the upward crossing is first
detected, the gauge mod-time has JUST changed (real crossing) or `lastModTime` is still zero (boot),
so `gaugeQuiesced` (`modTime.Equal(lastModTime) && time.Since(modTime) >= IdleQuiesce`) is ALWAYS
false on that tick. `warnFired=true` then permanently blocks re-entry, so injection is never retried.
**Net: the warn event/log fires, but the tmux injection can essentially never fire on a real crossing.**
- **Design implication:** decouple injection retry from the warn-fire latch — e.g. keep a `pendingInject`
  flag set when the warn fires, and attempt injection on each subsequent tick until the pane quiesces
  (then clear it), rather than one-shotting it on the crossing tick. Add a live-tmux integration test
  that asserts the pane actually received the text.

### BUG-2 (P2, observability) — standalone keeper discards its own events
`cmd/harmonik/keeper_cmd.go:103` passes `NoopEmitter{}`, and `emitNoGauge` has no stdout fallback, so
`session_keeper_no_gauge` produces ZERO observable signal in the deployed CLI; `session_keeper_warn`
survives only via `fmt.Printf`/slog, never reaching events.jsonl.
- **Design implication:** give the standalone keeper a real file/JSONL emitter (append to
  `.harmonik/events/events.jsonl`) or at minimum a `fmt.Printf` fallback in `emitNoGauge`, so the
  "missing statusLine.command" signal the no_gauge event exists to surface is actually visible.

### BUG-3 (P3, minor) — `--tmux`-only target; no convention-derived fallback
`keeper_cmd.go` only wires the `--tmux` override; there is no provenance/convention-based target
derivation, so omitting `--tmux` silently disables injection (warn still emits). Flagged by the bead's
own reviewer too.
- **Design implication:** derive the tmux target from the agent/run convention when `--tmux` is absent,
  or log a one-line "injection disabled (no --tmux)" notice so the no-op is visible.

## What is solid
Statusline gauge contract, `.managed` opt-in fail-safe, single-keeper flock (exit 2), warn
state-machine arithmetic (exactly-one-per-crossing + reset-on-drop), and the non-destructive posture
(no `/clear`/handoff anywhere) are all correct against the live binary. The injector primitive works
in isolation. The Phase-1 *warning* fires; only its *delivery into the pane* is broken (BUG-1).

---

## Re-dogfood (fix 532c863c)

**Date:** 2026-06-03
**Binary:** `/tmp/harmonik-skfix` — fresh `go build -o /tmp/harmonik-skfix ./cmd/harmonik` at HEAD `d76b2128` (fix `532c863c` "decouple warn-inject delivery from crossing tick; wire FileEmitter" is in history). The installed binary and running daemon were NOT touched.
**Harness:** throwaway agent `skfix`, throwaway tmux session `skfix-pane` (plain bash shell as injection target, same convention as the original dogfood). Drove the REAL gauge surface `.harmonik/keeper/skfix.ctx` directly (confirmed `watcher.go`/`gauge.go` just `os.Stat` + `os.ReadFile` the file — direct writes are gauge-equivalent to the statusline script).
**Method:** engineered the EXACT previously-fatal condition — gauge mtime freshly updated ON the crossing tick (`gaugeQuiesced=false` that tick), then held untouched so a later tick quiesces.

### Verdict summary (re-test)

| Gate | Prior | Now | One-liner |
|------|-------|-----|-----------|
| G3 exactly-one injection on first crossing | **FAIL** | **PASS** | Inject delivered into pane (capture-pane), once, even though the crossing tick had a just-updated mtime. No double-inject. |
| G4 `session_keeper_warn` in events.jsonl | **FAIL-by-design** | **PASS** | FileEmitter wrote one typed `session_keeper_warn` event to `.harmonik/events/events.jsonl`. |
| Regression: inject not permanently latched off | — | **PASS** | Deferred inject delivered on a later quiesced tick; `pendingInject` retry path works. |
| Non-destructive | **PASS** | **PASS** | No `/clear`, no handoff write, HANDOFF.md mtime unchanged, no tracked-file changes. |

### G3-fix — injection delivers on the previously-fatal crossing tick (PASS)

Timeline (PollInterval=5s, IdleQuiesce=8s, both code defaults — not CLI-overridable):
- t0: keeper booted with gauge=50.0 (below). Waited ~12s so two below-threshold polls ran (`warnArmed=true`, `lastModTime` set from the 50% file).
- t≈12s **(crossing)**: wrote gauge=85.0 at `21:05:03Z` — mtime freshly bumped this tick.
- Next poll at `21:05:07`: `pct>=80`, `warnArmed && !warnFired` → **warn emitted, `warnFired=true`, `pendingInject=true`**, and because the gauge mtime had JUST changed `gaugeQuiesced=false` → **inject deferred** (this is the exact case that was dead code before).
- Gauge held untouched; after ≥8s quiescence a later tick had `modTime.Equal(lastModTime) && time.Since>=8s` → `gaugeQuiesced=true` → **inject delivered**.

Keeper log (`/tmp/skfix-keeper.log`):
```
keeper started for skfix (warn-pct=80, tmux="skfix-pane")
2026/06/03 14:05:07 WARN keeper: context window warn threshold crossed agent=skfix pct=85 warn_pct=80
keeper: warn — agent "skfix" context window at 85.0% (threshold 80.0%)
```
`tmux capture-pane -t skfix-pane -p`:
```
skfix$ Context window is approaching its limit. Please wrap up your current work: commit any in-progress changes, write a brief handoff note if needed, then run /quit.
zsh: command not found: Context
skfix$
```
- Injected wrap-up-warning text count in pane = **1** (re-checked after 15+ more ticks: still 1 — **no double-inject**).
- Warn-log count = **1**; inject-error count = **0** in the keeper log.
- The `command not found: Context` line is the plain-bash target executing the pasted text — expected for a bash injection target; in a real Claude pane it lands as a prompt. The injector mechanism is the same one the prior dogfood proved works in isolation.

Code path that makes it work: `internal/keeper/watcher.go:304-331`. The crossing block (304-316) no longer gates the inject behind `gaugeQuiesced`; it sets `pendingInject=true` and the separate retry block (321-331) delivers `if pendingInject && gaugeQuiesced` on the crossing tick OR any later tick, clearing `pendingInject` only on success. `warnFired` is latched at the crossing but no longer cuts off the inject (the BUG-1 latch-off).

### G4-fix — `session_keeper_warn` reaches events.jsonl via FileEmitter (PASS)

`cmd/harmonik/keeper_cmd.go:103` now wires `keeper.NewFileEmitter(projectDir)` (was `NoopEmitter{}`). `NewFileEmitter` (`internal/keeper/watcher.go:44-49`) targets `projectDir/.harmonik/events/events.jsonl`; `EmitWithRunID` (`watcher.go:54-97`) appends one JSON line under a mutex.

events.jsonl baseline before run = 7522 lines. The emitted event landed at **line 7523**:
```
$ grep -n '"type":"session_keeper_warn"' .harmonik/events/events.jsonl
7523:{"event_id":"019e8f4d-e5e7-7c29-a3ad-b9c3222f835a","schema_version":1,"type":"session_keeper_warn","timestamp_wall":"2026-06-03T21:05:07.559797Z","source_subsystem":"internal/keeper","payload":{"agent_name":"skfix","pct":85,"warn_pct":80,"session_id":"sess-skfix"}}
```
`jq 'select(.type=="session_keeper_warn")'` returns exactly this one event: `source_subsystem=internal/keeper`, payload `{agent_name:skfix, pct:85, warn_pct:80, session_id:sess-skfix}`, timestamp `21:05:07.559` = the crossing tick. Exactly one typed event, no duplicate.

### Regression — inject no longer permanently latched off (PASS)
The fact that the inject delivered at all (after being deferred on the fatal crossing tick) directly proves BUG-1's "permanently cut off" latch is gone. `pendingInject` survives the `warnFired=true` latch and the retry block re-attempts every tick until quiescence, then clears on success. After delivery, no further injects fire (verified count stays 1) — the success-clear correctly latches it off for the current crossing without ever re-latching the bug.

### Non-destructive (PASS)
- `git status` after the run: only `.beads/issues.jsonl` modified (pre-existing, from the live daemon's beads sync — NOT the keeper). All `.harmonik/` paths gitignored (`git check-ignore` confirms events.jsonl + skfix.ctx).
- `HANDOFF.md` mtime = `Jun 2 17:54` — **unchanged**. No handoff file written.
- No `/clear` executed; the only "handoff"/"/quit" strings in the pane are inside the injected *prompt content* (which merely asks the agent to wrap up) — the keeper itself takes no destructive action, it only pastes text.

### Bug status after fix
- **BUG-1** (idle-gate killed the inject) → **FIXED & VALIDATED** (G3-fix above). Close.
- **BUG-2** (standalone keeper discards its own events) → **FIXED & VALIDATED** for `session_keeper_warn` (G4-fix; FileEmitter now wired). Note: `session_keeper_no_gauge` is also now routed through the same FileEmitter, but this re-dogfood exercised only the warn path; the no_gauge emission via FileEmitter is covered by inspection (`watcher.go:373` uses the same `w.emitter`) but was not separately driven here.
- **BUG-3** (no convention-derived `--tmux` fallback) → **unchanged** (out of scope for fix 532c863c; `--tmux` was supplied explicitly). Still a candidate P3 follow-up.

**Conclusion:** Both gates that failed in the original dogfood (G3 injection, G4 event emission) now PASS against the fix, including the exact crossing-tick condition that was previously dead code, with no double-inject and no regression to a permanently-latched-off state.
