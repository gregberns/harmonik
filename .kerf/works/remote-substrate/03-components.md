# remote-substrate Phase 1 — Decompose (components + converged decisions)

> Synthesis of a 3-architect independent design panel (2026-06-14). All four open
> decisions converged 3/3 → adopted. Source reports summarized in this doc; the bead
> specs are in `07-tasks.md`.

## Converged decisions (3/3 — LOCKED)

- **DEC-A — integration shape:** a `CommandRunner` interface in `internal/lifecycle/tmux`.
  `OSAdapter` gains a `runner CommandRunner` field, default `LocalRunner{}` (byte-identical to
  today → NFR7). `SSHRunner{Host}` rewrites argv → `ssh <host> [opts] -- <name> <args…>` run
  locally via `exec.CommandContext`, reusing one `ControlMaster`-multiplexed connection. The
  same runner is threaded through (a) every `OSAdapter` tmux call, (b) the `workspace`
  worktree git calls (switch to `git -C <repo>` form), and (c) the pasteinject liveness/commit
  probes (`pgrep -P`, `ps -o comm=`, `git … rev-parse HEAD`, `tmux display-message`). The
  "remote substrate" is just `tmuxSubstrate` built over an `OSAdapter` carrying an `SSHRunner`
  — NO new `handler.Substrate` sibling, so all of pasteinject / spawn-cap / crew machinery is
  reused unchanged. *Rejected:* a top-to-bottom `sshRemoteSubstrate` clone (would re-implement
  ~1500 lines of hard-won logic).
- **DEC-B — code-sync (DD1=GitHub):** box A keeps merge authority. Per bead: (1) box A resolves
  `baseSHA` locally; (2) box A guarantees `baseSHA` is on `origin` (it pushes main as steady
  state; push the tip/pin-ref if not yet there); (3) worker `git -C <repo> fetch origin <sha>`;
  (4) worker `git -C <repo> worktree add -b run/<id> <wtpath> <sha>`; (5) implementer commits on
  worker; (6) worker `git -C <wt> push origin run/<id>`; (7) box A `git fetch origin run/<id>`;
  (8) existing `mergeRunBranchToMain` under the merge mutex — UNCHANGED. Git runs on box A for
  1,2,7,8; on the worker (via SSHRunner) for 3-6 + teardown.
- **DEC-C — reviewer on box A** in Phase 1 (cheap, short, branch already fetched, zero
  review-loop change; remoting it is a clean Phase-1.5 follow-up).
- **DEC-D — test without a live remote:** (1) fake/recording `CommandRunner` unit tests assert
  the exact `ssh <host> -- tmux/git/pgrep …` argv (and that `LocalRunner` argv is byte-identical
  to today). (2) one `//go:build scenario` integration test with `SSHRunner{Host:"localhost"}`
  + a temp bare origin + a stub claude that commits `Refs:`, exercising the real ssh argv path
  end-to-end on ONE machine; `t.Skip` if `ssh localhost true` fails. The daemon gate skips
  scenario builds, so this is authored via the scenario-test convention, NOT a daemon-gated bead.

## Components (buildable units)

| # | Component | Responsibility | Pkg |
|---|---|---|---|
| C1 | `CommandRunner` + `LocalRunner` | argv-execution seam; default = today's `exec`. | `internal/lifecycle/tmux` |
| C2 | `SSHRunner` + OSAdapter runner-routing | wrap argv as `ssh host -- …`; route all OSAdapter tmux calls. | `tmux` |
| C3 | `workers.yaml` schema + loader | parse `.harmonik/workers.yaml` (mirror `internal/branching`). | `internal/workers` (new) |
| C4 | workers boot-wiring + CLI override | load in `daemon.Start`; flag>file>default. | `daemon` |
| C5 | worker registry + selection + live-disable | hold worker; pick worker-or-local; `enabled` gate. | `daemon`/`workers` |
| C6 | boot health-check | SSH+tmux+claude+repo+no-API-key probe; unhealthy→skip+raise. | `daemon`/`workers` |
| C7 | remote worktree ops | runner-parametrized `CreateWorktree`/`RemoveWorktree`/`resolveWorktreeHEAD`. | `internal/workspace` |
| C8 | code-sync orchestration | fetch-base, push run-branch, box-A fetch-before-merge (DEC-B). | `daemon` |
| C9 | remote liveness probes | `pgrep`/`ps`/`rev-parse` via the run's runner. | `daemon` (pasteinject) |
| C10 | substrate wiring + run-metadata | dispatch a bead through the SSH-backed adapter; stamp `Worker`/`WorkerOS`. | `daemon` |
| C11 | offline detection → recover | SSH/liveness failure → existing `run_stale`/skip + raise; orphan-worktree GC. | `daemon` |
| C12 | ssh-to-localhost e2e (scenario) | full lifecycle proof on one machine. | scenario test |

## Dependency graph (drives dispatch order — same-file beads MUST serialize)

```
B1(C1) ──┬─ B2(C2) ──┬───────────────┐
         ├─ B7(C7) ──┤               │
         └─ B9(C9) ──┤               ├─ B10(C10) ─ B11(C11) ─ B12(C12)
B3(C3) ─ B4(C4) ─ B5(C5) ─ B6(C6) ───┘
```
- **B1 must land first** (defines `CommandRunner`; B2/B7/B9 all build on it).
- **B1→B2→B9→B10 touch overlapping core files** (OSAdapter, pasteinject) → **dispatch SERIALLY**,
  waiting for each merge, to avoid same-file merge-conflict auto-skips (project memory).
- **B3→B4→B5→B6** is a mostly-separate track (new `internal/workers` pkg) — can run alongside the
  runner chain, but B6 needs B2 (SSHRunner) merged first.
- **B12** is a scenario test authored via a worktree sub-agent (not daemon-gated).

## Convergent risks (carry into SPEC) — de-risks
1. **SSH argv quoting** (paths/env w/ spaces/slashes — hk-kuxxl class): one audited shell-escape
   helper in `SSHRunner`; B2 unit test asserts a path-with-spaces + `KEY=VAL` survives; B12 proves
   real quoting end-to-end.
2. **Worker base-SHA staleness** (origin behind): B8 makes `fetch origin <baseSHA>` explicit +
   asserted; box A re-fetches the run branch → mismatch surfaces as a normal merge conflict → auto-skip,
   never silent.
3. **API-key billing leak** (NFR4/D2): B6 health-check fails-closed on `ANTHROPIC_API_KEY` present;
   B10 strips it from the forwarded spawn env.
4. **Latency × SSH chatter** (500ms liveness/commit polls × runtime): mandate + health-check-verify
   `ControlMaster`/`ControlPersist`; coarsen the remote poll interval (runner-aware knob).
5. **Stdin over SSH for `load-buffer -`** (kick-off paste): SSHRunner must forward stdin; B2 asserts
   stdin pass-through; B12 exercises real paste delivery.
