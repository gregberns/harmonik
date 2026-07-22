# Hardening input 3 — Coverage gaps + findings-traceability + isolation safety (adversarial pass)

> Produced 2026-07-18 by an adversarial hardening agent. Ready-to-paste additions to PLAN.md.
> Angle: "what's not exercised; which fixes have no revert-red cell; how to guarantee isolation."

## A. COVERAGE GAPS → new cells/suites (with the assertion each must make)

Matrix note: **"substrate" is undefined and collapses into local/remote** (tmux is the only
substrate) — pin it or drop the axis so it can't hide cells. Missing behaviors:

- **G1 — Operator control path (C1).** No suite drives a run to `confirm_required` then `harmonik confirm-verdict`/`veto-verdict`. *Assert:* blocked run released by the command; reverting C1 → command exits 1 / run stuck. The review's worst finding, zero cells today.
- **G2 — Daemon crash-recovery.** SIGKILL-the-daemon-mid-dispatch → restart. *Assert:* in-flight runs re-adopt, `queue.json` RunID not durably `""` (RU-01a), no double-dispatch of `(run_id,node_id)`. Ties to `internal/scenario/crashrecovery.go`.
- **G3 — DOT merge-vs-strand (A7).** *Assert two cells:* (a) committed tip that SHOULD merge is merged (not stranded); (b) un-reviewed/cap-hit tip that SHOULD strand is stranded. Plus a graph whose gate node isn't named `"commit_gate"` (dot_cascade.go:1122/851) — cap-hit salvage + gate feedback still fire.
- **G4 — Review-loop feedback threading.** *Assert:* REQUEST_CHANGES writes reviewer feedback, agent re-drives, verdict re-threads — **on a remote worker** (A8: review-loop vs DOT diverge on remote feedback; H4/H5).
- **G5 — Promote, push-mode AND PR-mode.** No suite drives `harmonik promote`. *Assert:* reviewed work advances to target; temp worktree cleanup leaves no registered worktree (RU-17); build-gate doesn't re-run on unchanged tree. Against a throwaway remote.
- **G6 — Supervisor revive race + orphan-sweep false-reap.** *Assert:* SIGTERM during in-flight → supervisor revives exactly once (no double-daemon); boot sweep does NOT reap a crew (re)started seconds earlier.
- **G7 — Comms at-least-once UNDER redelivery.** Hamlet is sequential — never forces a dup. *Assert:* force redelivery (consumer reconnect/replay) so same `event_id` arrives twice, dedupe drops the second (N3).
- **G8 — SSH tunnel DROP mid-run** (distinct from H4/H5 truncation). *Assert:* kill SSH transport during remote dispatch → run inconclusive/retry (not silent absent); remote tmux window cleaned via the remote adapter (RU-05a); H8 Kill routes remote, no local PID signalled.
- **G9 — Keeper hold/release override.** *Assert:* under `hold`, context crossing ACT does NOT force handoff; `release` restores the cutoff.
- **G10 — Config honored, not silently dropped.** *Assert:* a partial `config.yaml` (only `watch.absent_thresh_s`, no `schema_version`) is applied, not discarded (projectconfig.go:1287); unknown keeper key rejected.
- **G11 — Oversized-but-valid event line.** *Assert:* a >64 KB `reviewer_verdict`/directive doesn't abort the 64 KB `bufio.Scanner` (smoke.go:381, goalkeeper_cmd.go:156).
- **G12 — EV-036 secret-field startup guard (A4) + EV-034 sealing.** *Assert:* startup fails-closed on a registered payload with a secret-looking field; post-dispatch registration rejected.
- **G13 — Version negotiation (HC-009).** *Assert:* handler advertising `SupportedVersions:[2]` gets `ErrProtocolMismatch`; a handler never emitting `handler_capabilities` aborts at the 5 s timeout (no hang).
- **G14 — Positive worktree GC.** Add to H2/H2b (non-reaping): a genuinely stale/aged worktree with a dead lease IS reaped (else the fail-safe over-quarantines and never GCs).

**Meta-gap (A3, load-bearing on the campaign itself):** `ExpandMatrix` + `CheckPostSuiteLeaks`
are **not wired into the production runner** (harness.go:356) and the timeout branch never
evaluates assertions (harness.go:481). *Assert up front:* a `matrix:` YAML with `{{.param}}`
actually expands to N substituted runs and post-suite leak-check runs — else the matrix silently
executes once, unsubstituted, and S6 leak detection is a no-op. If unwired, run matrix-expansion +
leak-sensor out-of-band (S6 already uses `subscribe --json`) and flag it.

## B. UNTRACED FINDINGS (critical/high with no cell that goes red on revert)

| Finding | Sev | In campaign? | Fix that must be provably red on revert |
|---|---|---|---|
| **C1** confirm/veto dead | CRIT | No | G1 |
| **H1** gitignore-hygiene commits to operator branch | HIGH | No (also isolation hazard) | boot uses `harmonik/gitignore-init` branch; real HEAD unchanged |
| **H2b** aged-reaper uses dir mtime | HIGH | No | edit-in-place worktree NOT reaped (G14 pair) |
| **H9** RC-024 staleness swallows audit-fetch error | HIGH | No | fail audit fetch mid-recapture → verdict refuses (fail-closed) |
| **H10** runShell busy-spins on ctx-cancel | HIGH | No | cancel with no timer → loop exits, no core-spin (or SKIP-with-reason logged) |
| **H12** BI-010c label guard bypassed by `--flag=value` | HIGH | No | `br update --label=workflow:dot` (joined form) blocked |
| **RC-002a** reconcile-lock unlock-before-unlink | HIGH | No | race two reconciliations on one `target_run_id` → only one runs |
| **A4** EV-036 secret guard uninvoked | HIGH | No | G12 |
| **A9** HC-044a/048a/013a/009/018 | HIGH | partial (S2 only HC-004) | one cell each — G13 + rotate-during-turn + Launch-before-callback orphan |
| A1/A2 dead packages | HIGH | No (static) | static assert: package deleted / zero non-test importers |

**Traced-but-weak (won't catch a revert):**
- **H13** (S2): a happy-path handshake passes even with the lost-wakeup bug. The cell must **hit the race window** (warm resume where `SessionStart` fires before `SetAgentReadyCallback`) and assert dispatch proceeds; reverting the latch must time this cell out.
- **H2/H6/H7/H8** (S7): add a **revert-confirm-red** step per fault (failed≠clean discipline) — otherwise assertion theater that passes on any tree.

## C. PRE-FLIGHT ISOLATION CHECK (all PASS before `daemon on`)

`REAL=$(git -C /Users/gb/github/harmonik rev-parse --show-toplevel)`, `SBX=$(realpath -m "$H_ROOT")`.

1. **Sandbox-not-inside-real (hard refuse).** `case "$SBX/" in "$REAL/"*) FAIL;; esac` AND reverse AND `[ "$SBX" != "$REAL" ]`. `realpath -m` first (symlink defeats leaf-only check, RU-22 class).
2. **Distinct git repo + no real remote.** `git -C "$SBX" rev-parse --show-toplevel` == `$SBX` (fresh init); `git -C "$SBX" remote -v` empty or throwaway, must NOT resolve to real harmonik remote (guards G5 promote/push).
3. **Distinct .beads proven empty.** `$SBX/.beads` real dir (not symlink into real); scoped `br list` == 0. `HK_PROJECT` **unset** (state_cmd.go:107 still honors retired var).
4. **Distinct socket + pidfile.** Under `$SBX/.harmonik` (or `$TMPDIR`); ≠ real socket; absent before boot; no pidfile collision.
5. **Distinct tmux — dedicated server socket.** `tmux -L h-assessor` (separate server), not just `crew-asr-` prefix. Assert `-L h-assessor list-sessions` empty AND default `tmux ls` has no `crew-asr-*`. (Prefix-only risky: keeper hooks derive AGENT from session name, no traversal guard, RU-24.)
6. **Distinct env proven fresh.** `TMPDIR=/tmp/h-assessor`, `GOCACHE=…/gocache`, `CODEX_HOME=…/codex`, each ≠ default + fresh. `OPENAI_API_KEY` unset. Worker binary in `workers.yaml` is the freshly-built one, not the `/Users/gb/go/bin/harmonik` fallback (RU-23).
7. **Read-leak ack.** `harmonik usage`/session-discovery hardcode real repo path + `$USER` (usage.go:424) — cannot sandbox. Assert campaign never invokes `harmonik usage`; log as known un-isolable read.
8. **Pre-snapshot real env (teardown baseline).** `git -C "$REAL" rev-parse HEAD`, `status --porcelain`, real `.beads` JSONL sha256+count, `git worktree list`, default `tmux ls`, `$HOME/.harmonik/keeper` file list, start epoch.
9. **Dry-boot round-trip (⑤ bind fix).** Health-only boot; assert daemon reports **`$SBX`** as project dir + binds sandbox socket. A daemon echoing the real path aborts the campaign.

## D. TEARDOWN VERIFICATION (zero residue — all PASS)

1. **Real branch untouched.** `git -C "$REAL" rev-parse HEAD` == snapshot; `status --porcelain` identical (catches H1, EmitTrip direct-append trip_ev043b.go:446).
2. **Real ledger untouched.** Real `.beads` sha256 + `br list` count == snapshot (catches H3-class false-close reaching real DB).
3. **No tmux residue.** `tmux -L h-assessor kill-server`; `-L h-assessor list-sessions` errors AND default `tmux ls` has no `crew-asr-*`.
4. **No processes.** `pgrep -f h-assessor` empty; pidfile+socket gone; no orphan agent/worker PIDs.
5. **No leaked worktrees.** `git -C "$SBX" worktree list` == main only; real `git worktree list` == snapshot.
6. **No leases/fds leaked.** S6 final: 0 held leases, fd count == baseline, 0 leaked goroutines (via leak-sensor).
7. **Nothing written outside sandbox.** `find "$REAL" -newermt @<start>` empty; `$HOME/.harmonik/keeper` == snapshot (catches RU-24 traversal); no new `$HOME/.codex`, real `.kerf`.
8. **Scratch removed.** `rm -rf /tmp/h-assessor` succeeds and gone.
9. **Residue diff committed** to RUN-LOG.md — a clean teardown is a logged, re-runnable assertion.

> D1/D2 double as the live regression test for H1 and H3 — a correct daemon leaves real branch +
> ledger byte-identical after a multi-hour hammer. Wire them as assertions, not an eyeball.
