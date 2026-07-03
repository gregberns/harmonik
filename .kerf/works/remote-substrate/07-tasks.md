# remote-substrate Phase 1 — Tasks (build beads)

> Each bead lands in ONE daemon run: a focused change + a FAST test the daemon gate runs
> (no `//go:build scenario`, no real ssh, no daemon boot). Acceptance criteria are
> deterministic. Dispatch order + serialization per `03-components.md` dependency graph.
> All beads carry label `codename:remote-substrate`. Epic: (created as hk-…).

---

## B1 — `CommandRunner` interface + `LocalRunner`; `OSAdapter` runner field
**Files:** `internal/lifecycle/tmux/runner.go` (new), `internal/lifecycle/tmux/osadapter.go`.
**Do:** Define `type CommandRunner interface { Command(ctx context.Context, name string, args ...string) *exec.Cmd }`.
`LocalRunner{}` returns `exec.CommandContext(ctx, name, args...)`. `OSAdapter` gains an
unexported `runner CommandRunner` field defaulting to `LocalRunner{}` (constructor + a
`WithRunner` option; existing constructors keep working). Route EVERY
`exec.CommandContext(ctx,"tmux",…)` in osadapter.go through `o.runner.Command(...)`.
**Test (gate-runnable):** a `recordingRunner` capturing argv; assert that with the default
`LocalRunner` the produced argv for each method is byte-identical to the previous literal
(no-regression). All existing tmux adapter tests pass unchanged.
**Accept:** `grep -n 'exec.CommandContext(ctx, "tmux"' internal/lifecycle/tmux/osadapter.go`
returns 0 (all routed through the runner); default behavior unchanged.

## B2 — `SSHRunner` + verify full OSAdapter wrapping
**Files:** `internal/lifecycle/tmux/runner.go`, `runner_test.go`.
**Do:** `SSHRunner{Host string; Opts []string}` → `exec.CommandContext(ctx, "ssh", append(append(Opts, Host, "--", name), args...)...)`.
MUST forward stdin (caller sets `cmd.Stdin`; SSH passes it to the remote command — needed for
`tmux load-buffer -`). Add a single audited shell-safety path: pass argv as discrete args to
`ssh` (no manual concatenation) so remote `tmux`/`git` receive exactly one token per arg.
**Test:** with `SSHRunner{Host:"worker-mac-1"}`, assert `NewWindowIn` argv ==
`["ssh","worker-mac-1","--","tmux","new-window","-P","-F","#{pane_id}","-d","-t",…,"-c","<cwd>","-e","K=V","--",<argv>…]`;
assert a `-c` worktree path containing a space and a window name containing `/` survive intact;
assert `load-buffer -` forwards stdin (recordingRunner exposes the `*exec.Cmd`, check `.Stdin != nil`).
**Accept:** all 14 OSAdapter methods produce a correct `ssh <host> -- …` argv under SSHRunner;
space/slash test green.

## B3 — `workers.yaml` schema + loader
**Files:** `internal/workers/workers.go` (new), `workers_test.go`. Mirror `internal/branching`.
**Do:** Struct `Worker{Name,Transport,Host,OS,RepoPath string; MaxSlots int; Enabled bool}` and
`Config{Version int; Workers []Worker}`. `Load(repoRoot string) (Config, error)` reads
`.harmonik/workers.yaml` with `yaml.v3`; absent file → zero Config + nil err; malformed →
typed error; `version != 1` → typed error. V1 invariant: at most one worker (warn/err if >1).
**Test:** golden fixture parses to the expected struct; absent file → empty+nil; bad version →
error; >1 worker → the documented v1 behavior.
**Accept:** FR10 fields parse; missing file is non-fatal.

## B4 — workers.yaml boot-wiring + CLI override
**Files:** `cmd/harmonik/…` (daemon start), `internal/daemon/…Config`.
**Do:** call `workers.Load(repoRoot)` in daemon startup after `branching.Load`; store on daemon
`Config`. CLI flag(s) override the file (mirror branching precedence: flag > file > default).
**Test:** flag beats file beats default for worker host/enabled.
**Accept:** loaded worker config reaches the daemon `cfg`; precedence asserted.

## B5 — worker registry + per-bead selection + live-disable
**Files:** `internal/daemon/…` (or `internal/workers/registry.go`).
**Do:** A registry built from the loaded config exposes `SelectWorker() *Worker` returning the
enabled+healthy worker or `nil` (→ local). Selection re-reads `Enabled` each call (live-disable,
FR12). Capacity: respect `MaxSlots` (count in-flight remote runs).
**Test:** enabled worker → selected; `Enabled=false` → nil (local fallback); slots exhausted →
nil; flipping enabled between two calls flips the result.
**Accept:** FR9/FR12; zero/disabled workers ⇒ today's local path (NFR7).

## B6 — boot health-check (depends: B2 merged)
**Files:** `internal/daemon/…`/`internal/workers/health.go`.
**Do:** For each enabled worker, run via `SSHRunner`: `tmux -V`, `claude --version`,
`git -C <repo_path> rev-parse HEAD`, and `test -z "$ANTHROPIC_API_KEY"`. Any failure → mark the
worker **unhealthy + skip** (DO NOT delete the config entry) and emit a typed
`worker_unhealthy` event with the failing probe. Re-checkable.
**Test (fake runner):** runner that fails `claude --version` → worker marked unhealthy, config
entry retained, event emitted. Runner that reports a non-empty `ANTHROPIC_API_KEY` → unhealthy
(fail-closed, NFR4).
**Accept:** FR11 + the API-key fail-closed; healthy worker becomes selectable.

## B7 — remote worktree ops (runner-parametrized) (depends: B1 merged)
**Files:** `internal/workspace/createworktree.go`, `worktreepath.go`.
**Do:** Thread a `tmux.CommandRunner` into `CreateWorktree`/`RemoveWorktree`/`resolveWorktreeHEAD`
(via `WorktreeRootConfig` which already plumbs through). Switch git from `cmd.Dir=repoRoot` to the
explicit `git -C <repoRoot> …` form so the runner needs no remote `cd`. Default runner = local →
unchanged behavior.
**Test:** recordingRunner asserts `git -C <repo> worktree add -b run/<id> <path> <sha>` locally,
and ssh-prefixed under SSHRunner. Existing worktree tests pass with the local default.
**Accept:** DEC-B steps 3-4 emit correct (local & remote) git; local path unchanged.

## B8 — code-sync: fetch-base + push-branch + box-A fetch-before-merge (depends: B7 merged)
**Files:** `internal/daemon/…` (dispatch + merge path).
**Do:** For a remote-placed run: before worktree-add, ensure `baseSHA` is on origin then
`ssh worker -- git -C <repo> fetch origin <baseSHA>`; after commit-detect,
`ssh worker -- git -C <wt> push origin run/<id>`; on box A, `git fetch origin run/<id>` BEFORE
the existing `mergeRunBranchToMain` (which is otherwise UNCHANGED). Local runs: no new steps.
**Test (fake git runner):** assert the ordered argv (fetch-base → worktree → push → box-A fetch →
merge) for a remote run; assert local runs skip the fetch/push and go straight to the existing merge.
**Accept:** DEC-B sequence exact; `mergeRunBranchToMain` untouched; local unaffected.

## B9 — remote liveness probes (depends: B1 merged)
**Files:** `internal/daemon/pasteinject.go`.
**Do:** Route `hasAnyDirectChild` (`pgrep -P`), `commandMatchesLiveAgent` (`ps -o comm=`), and any
`worktreeActivityFingerprint`/`resolveWorktreeHEAD` through the run's `CommandRunner` (held on
`perRunSubstrate`) instead of bare `exec.Command`. Local default unchanged.
**Test (fake runner):** canned `pgrep`/`ps`/`rev-parse` output drives the liveness + commit-detect
decisions identically to today; under SSHRunner assert `ssh host -- pgrep -P <pid>` argv.
**Accept:** the pasteinject watchdog reads process/HEAD state via the runner; local unchanged.

## B10 — substrate wiring + run-metadata worker identity (depends: B2,B5,B7,B9 merged)
**Files:** `internal/daemon/tmuxsubstrate.go`, dispatch path, run-metadata/event payloads.
**Do:** When the registry selects a worker, build the per-run `tmuxSubstrate` over an `OSAdapter`
carrying `SSHRunner{Host}` + the remote worktree provider + remote prober; else local (NFR7).
Strip `ANTHROPIC_API_KEY` from the forwarded spawn `Env` (fail-closed). Record `Worker` + `WorkerOS`
on the run handle / `run_started` payload (FR13).
**Test:** zero workers → local substrate, argv unchanged; one healthy worker → SSH-backed substrate;
spawn env with `ANTHROPIC_API_KEY` → refused; run metadata carries worker name+os for a remote run,
empty for local.
**Accept:** placement deterministic; API-key refusal fires; FR13 metadata correct.

## B11 — offline / partition detection → recover (depends: B10 merged)
**Files:** `internal/daemon/…` (workloop / liveness).
**Do:** SSH/connection failure at spawn → skip worker + local fallback (or clean-fail with raise);
mid-bead liveness/SSH failure → existing `run_stale` recovery (re-queue or clean-fail) + a typed
`worker_offline` event; orphan remote worktree GC'd on the next sweep (via runner).
**Test (fake runner):** runner healthy then returns ssh exit-255 mid-run → run reaches a recoverable
terminal state (NOT a wedge) + `worker_offline` emitted.
**Accept:** FR7/NFR5 — worker death recovers the bead, never silently wedges; failure is raised.

## B12 — end-to-end ssh-to-localhost integration test  (SCENARIO — authored via worktree sub-agent, NOT a daemon-gated bead)
**Files:** `internal/daemon/scenario_remote_substrate_localhost_test.go` (`//go:build scenario`).
**Do:** Register a worker `{host:"localhost", repo_path:<temp clone>, os:"darwin", enabled:true}`;
`SSHRunner{Host:"localhost"}`; temp bare origin. Drive one fake-handler bead through the full
lifecycle: fetch-base → remote worktree-add → spawn a stub `claude` that writes a `Refs:` commit →
commit-detect over ssh → push run branch → box-A fetch → merge to main. `t.Skip` cleanly if
`ssh localhost true` fails.
**Accept:** the commit lands on box A's `main` via the REAL `ssh -- tmux/git` argv path, with no
second machine and no manual step.
