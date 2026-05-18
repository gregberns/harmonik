# DRAFT — Amendments to `specs/process-lifecycle.md`

Status: draft. Additive only. No existing PL clauses revised. Ready for paste into the spec file as new clauses adjacent to PL-021, PL-021a, and PL-028.

---

## §4.7 — addition adjacent to PL-021a

### PL-021b — Direct-tmux substrate (MVH alternative to ntm adapter)

**Axes:** llm-freedom=mechanical; io-determinism=deterministic; replay-safety=replay-safe; idempotency=idempotent (within a process lifetime).

For the MVH the daemon MUST consume a direct-tmux substrate in place of the ntm adapter described in PL-021. The direct-tmux substrate is implemented by package `internal/lifecycle/tmux` and exposes the following obligations:

1. **Pane creation.** On every handler-subprocess spawn whose `agent_type` requires interactive-pty hosting, the daemon MUST create the subprocess via `tmux new-window -d -t <session>: -n <window-name> -c <cwd> -e KEY=VALUE [...] -- <binary> <argv...>`. The daemon MUST NOT spawn such subprocesses via `exec.CommandContext` directly. Subprocesses whose adapter does not request substrate hosting (e.g. unit-test twin invocations outside the daemon) remain on the direct-exec path; this carve-out preserves twin parity per CHB-022 because adapter-registry dispatch — not binary-name branching — selects the path.
2. **Tmux availability check.** The daemon MUST probe tmux at PL-005 step 4 (Cat 0 pre-check) by invoking `tmux -V` and asserting major version ≥ 3.0. On failure the daemon MUST exit with ON §8 code 22 (`tmux-unavailable`, retitled from the v0.4.x ntm-unavailable). This obligation supersedes PL-021a's `ntm`-targeted absence-detection for the duration of the MVH; PL-021a remains in force as the long-term contract once an ntm adapter ships.
3. **Session resolution.** Before dispatching the first handler subprocess the daemon MUST resolve the tmux session it will host windows in:
   - If the environment variable `TMUX` is set at daemon startup, the daemon MUST use the session named in `$TMUX` (the operator's existing session). Windows the daemon creates in this session MUST carry a sentinel prefix `hk-<hash6>-` where `<hash6>` is the first 6 hex chars of the project hash. The daemon does NOT create or kill the operator's session.
   - If `TMUX` is unset, the daemon MUST refuse to spawn any handler subprocess and MUST surface a directive instructing the operator to run `hk tmux-start` before retrying. The daemon MUST exit with a non-zero status surfaced via ON §8 code 24 (`tmux-session-unavailable`, declared PL-INTERIM pending ON absorption). The daemon MUST NOT silently create its own session when `TMUX` is unset; the operator must opt in via the start subcommand.
4. **Window naming.** Window names MUST be a deterministic function of `(bead_id, phase, iteration_count, project_hash, owns_session)` per workspace-model.md WM-002a. The function MUST be replay-stable: replaying a recorded run reproduces the exact window name. In the `owns_session=true` mode the name is `<bead_id>` (workflow:single) or `<bead_id>/i<n>` / `<bead_id>/r<n>` (workflow:review-loop implementer / reviewer). In the `owns_session=false` ($TMUX-reuse) mode the same name is prefixed with `hk-<hash6>-`.
5. **No pane-output consumption.** The daemon MUST NOT read pane stdout/stderr through `tmux pipe-pane` or any other channel. All bridge-protocol messages (CHB-018 pre-exec messages, CHB-019 heartbeats, CHB-020 terminal events, CHB-025 outcome dedup) flow through the daemon's Unix socket per PL-003a and the `harmonik hook-relay` subcommand per CHB-010. The pty exists exclusively for operator ergonomics (interactive attach).
6. **Substrate seam.** The handler-package `LaunchSpec` carries an optional substrate handle. When non-nil, `Handler.Launch` MUST route subprocess creation through the substrate. The substrate handle is constructed at the daemon composition root and threaded via the adapter registry; daemon code MUST NOT branch on `LaunchSpec.Binary` to decide substrate engagement (CHB-022 twin-blindness).
7. **Wait/kill discipline.** The substrate `Wait` operation MUST satisfy the PL-014 single-`cmd.Wait()` invariant in spirit — for substrate-hosted sessions the daemon has no `*exec.Cmd` to wait on; the substrate observes pane death by polling `tmux list-panes` at a 100ms cadence (matching PL-006 sweep cadence) and reports exit semantics via the Outcome type. The substrate `Kill` operation MUST issue `tmux kill-window`; SIGKILL escalation is delegated to tmux itself.

### PL-021c — Pane orphan recovery within PL-006

**Axes:** llm-freedom=mechanical; io-determinism=deterministic; replay-safety=replay-safe; idempotency=idempotent.

The orphan sweep of PL-006 MUST be extended to cover orphan tmux **windows** in addition to orphan tmux **sessions**. The extension is required because the PL-021b $TMUX-reuse mode places harmonik-created windows inside an operator-owned session whose name does NOT match the `harmonik-<project_hash>-` prefix that PL-006 enumerates.

The extended sweep MUST:

1. Enumerate all live tmux sessions via `tmux list-sessions -F '#{session_name}'`.
2. For each session, enumerate its windows via `tmux list-windows -t <session> -F '#{window_name}'`.
3. For every window whose name begins with `hk-<hash6>-` where `<hash6>` is the first 6 hex chars of *this* daemon's project hash, the daemon MUST issue `tmux kill-window -t <session>:<window>`.
4. After issuing kill-window commands, the daemon MUST poll at 100 ms cadence up to a 2-second ceiling for the windows to disappear; after the ceiling, the daemon MUST proceed regardless.
5. The `daemon_orphan_sweep_completed` event payload MUST gain a new field `tmux_windows_killed: <integer ≥ 0>`.

The session-level sweep of PL-006 is NOT modified.

Cross-spec coordination: event-model.md §8.7 `daemon_orphan_sweep_completed` payload schema requires the `tmux_windows_killed` field addition.

---

## §4.10 — refinement adjacent to PL-028

### PL-028 — refinement: `hk tmux-start` subcommand replaces `harmonik runner` tmux duties for MVH

The `harmonik runner` four-step lifecycle of PL-028 obligates the daemon (or runner wrapper) to open a tmux session in step 3. For the MVH this obligation is satisfied by a distinct subcommand `hk tmux-start`:

1. **Trigger conditions.** `hk tmux-start` is invoked by the operator explicitly when starting work from a non-tmux shell. MUST NOT be invoked automatically by the daemon. When the operator is already inside a tmux session (`$TMUX` set), `hk tmux-start` MUST refuse with a friendly message (exit code 0) and SHOULD print the session name they are already in.
2. **Steps.** `hk tmux-start` MUST execute:
   - **i.** Verify `$TMUX` is unset. If set, exit 0 with the directive.
   - **ii.** Compute the session name `harmonik-<project_hash>-default` per PL-006a provenance. `--session-name` flag MAY override; override MUST still carry the `harmonik-<project_hash>-` prefix.
   - **iii.** Invoke `tmux new-session -d -s <session-name> -c <project_dir>`. Idempotent if exists.
   - **iv.** `execve` `tmux attach-session -t <session-name>`, replacing the `hk tmux-start` process.
3. **`hk` started inside an `hk tmux-start`-created session.** When the operator runs `hk` from inside the session created by step 2.iv, `$TMUX` is set. `hk` therefore takes the PL-021b $TMUX-reuse path, creates handler windows in that same session.
4. **Relationship to PL-028 `harmonik runner`.** `harmonik runner` step-3 obligation is satisfied for the MVH by `hk tmux-start`; `harmonik runner` MAY be implemented as a convenience or deferred entirely until post-MVH.
5. **Exit codes.** 22 if `tmux -V` probe fails; 24 (PL-INTERIM) for any other unrecoverable failure during steps i–iv. Code 0 for the "$TMUX already set" no-op path.

**Axes:** llm-freedom=mechanical; io-determinism=deterministic; replay-safety=replay-safe; idempotency=idempotent.

### PL-028b — `hk` daemon refusal when `$TMUX` is unset

**Axes:** llm-freedom=mechanical; io-determinism=deterministic; replay-safety=replay-safe; idempotency=idempotent.

When `$TMUX` is unset at `harmonik daemon` startup, the daemon MUST refuse to enter the ready state and MUST exit with code 24 (`tmux-session-unavailable`, PL-INTERIM) after printing a directive that names `hk tmux-start` as the operator action. The refusal MUST occur during PL-005 step 5 (post Cat 0 pre-check, pre socket-bind step 3a), so no pidfile, socket, or event-bus state is established by a daemon that cannot dispatch.

---

## Conformance test obligations (additive to §10.2)

- **PL-021b probe test.** Seed PATH without tmux, assert daemon exits 22 at PL-005 step 4.
- **PL-021b session-resolution test.** With `$TMUX=foo`, assert daemon resolves session=`foo` and `owns_session=false`. With `$TMUX` unset, assert daemon exits 24 with the `hk tmux-start` directive in stderr.
- **PL-021b window-name determinism test.** Same `(bead_id, phase, iter, project_hash, owns_session)` → identical window name across two daemon invocations.
- **PL-021c window-sweep test.** Seed tmux with operator session containing window named `hk-<hash6>-stale`, start daemon, assert window killed and `tmux_windows_killed=1` in event payload.
- **PL-028 `hk tmux-start` test.** With `$TMUX` unset, run `hk tmux-start`, assert session `harmonik-<hash>-default` exists post-call.
- **PL-028b refusal test.** With `$TMUX` unset, run `harmonik daemon`, assert exit 24 within Cat 0 window.

---

## Change-log entry (to be added to §10 history table)

| 2026-05-12 | 0.4.x-draft | bridge-integration | Additive amendments for the direct-tmux substrate at MVH. **PL-021b** introduces direct-tmux substrate obligations (replacing ntm at MVH while leaving PL-021..PL-023 intact). **PL-021c** extends PL-006 orphan sweep with window-level pass keyed on `hk-<hash6>-` sentinel prefix. **PL-028 refinement** specifies the `hk tmux-start` subcommand. **PL-028b** specifies daemon refusal when `$TMUX` is unset. Cross-spec coordination: ON §8 to absorb code 24; EV §8.7 payload to add `tmux_windows_killed`; WM to add WM-002a deterministic window-name clause; HC §6.1 to refine `Session.Attach()`. No existing PL IDs renumbered. |
