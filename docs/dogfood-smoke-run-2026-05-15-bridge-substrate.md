# Dogfood Smoke Run — 2026-05-15 — Bridge + Substrate

## Verdict

**GREEN** — End-to-end run against real `claude` CLI inside tmux, with `tmuxSubstrate` wired into the daemon composition root (hk-kqdpf.4) and the bridge fully active. A new tmux window appeared, claude received its task, mutated `marker.txt` to `SMOKE-OK`, committed the work, the Stop hook relayed `outcome_emitted` to the daemon, and the bead closed on verified work (not on EOF alone).

**Date:** 2026-05-15
**HEAD:** `a48e3ef` (docs(handoff): v45)
**Bead:** `hk-kqdpf.5` (re-run dogfood smoke with substrate + bridge wired)
**Smoke bead:** `smoke-az2` (workflow:single)
**Smoke dir:** `/var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.edHDdc9bR7`

---

## Preconditions

- `claude --version`: `2.1.142 (Claude Code)` — PASS
- `tmux -V`: `tmux 3.6a` — PASS
- `br --version`: 0.1.45 — PASS
- `go version`: `go1.26.1 darwin/arm64` — PASS
- `$TMUX` set (operator shell inside session `harmonik`) — PASS
- `go build -o /tmp/hk ./cmd/harmonik`: exit 0 — PASS

---

## Setup

```
SMOKE_DIR=/var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.edHDdc9bR7
BARE=/var/folders/s9/kq5h9q_17t571w_xq_1q9f2r0000gp/T/tmp.edHDdc9bR7.bare.git
BEAD_ID=smoke-az2
```

- `git init`, user.email `smoke@harmonik.local`, user.name `Smoke Runner`
- `README.md` + empty `marker.txt`; initial commit `6f22733`
- Bare local remote + `git push -u origin main`
- `br init --prefix smoke` (writes `.beads/`)
- `br create --title "Add SMOKE-OK marker line to marker.txt and commit" --type task --priority 1 --labels workflow:single --silent`

---

## Run invocation

```bash
/tmp/hk --project "$SMOKE_DIR" --max-concurrent 1 \
  > "$SMOKE_DIR/hk-stdout.txt" 2> "$SMOKE_DIR/hk-stderr.txt" &
```

Operator shell is inside tmux session `harmonik`. Daemon attached to that session via `tmux display-message -p '#S'` per hk-kqdpf.4 wiring.

Within ~3 s a new tmux window appeared (window index 2, pane `%17`) whose name resolves to the worktree path. Daemon stderr confirmed substrate write:

```
2026/05/15 11:42:38 INFO daemon_pane_write
  session_id=019e2cf2-98c6-748f-8537-777e2ef72f1c
  pane_target=%17
  buffer_name=harmonik-019e2cf2-98c6-748f-8537-777e2ef72f1c-task
  purpose=task payload_bytes=47
```

`run_started → bead_closed`: ~20 s.

---

## Event stream

From `.harmonik/events/events.jsonl` (11 lines):

```
1.  daemon_started
2.  daemon_orphan_sweep_completed
3.  run_started               workspace_path=<worktree>
4.  handler_capabilities
5.  session_log_location
6.  skills_provisioned
7.  launch_initiated
8.  agent_ready                          ← SessionStart hook relay PASS
9.  outcome_emitted                       ← Stop hook relay PASS
10. bead_closed
11. run_completed              success=true, summary="auto-close: exit=0"
```

---

## Success-criteria checklist

| # | Check | Result |
|---|---|---|
| 1 | `marker.txt` mutated to contain `SMOKE-OK` | **PASS** — `git show 2b717b2:marker.txt` → `SMOKE-OK` |
| 2 | A real commit with the work exists | **PASS** — `2b717b2 Add SMOKE-OK marker line to marker.txt` on `run/019e2cf2-…` and merged to `main` |
| 3 | `outcome_emitted` event recorded | **PASS** — present in events.jsonl |
| 4 | `agent_ready` event recorded (SessionStart hook relay fired) | **PASS** — present in events.jsonl |
| 5 | `.claude/settings.json` materialized with bridge hooks (CHB-001..003) | **PASS** — handler emitted `skills_provisioned` and `launch_initiated`; relay fired (events 8 & 9) confirms hooks were active in worktree before cleanup |
| 6 | Bead closed with reason `done` | **PASS** — `br show smoke-az2` → `[● P1 · CLOSED] … Closed: 2026-05-15 (done)` |
| 7 | `run_completed.success == true` | **PASS** — `jq` returns `true` |
| 8 | claude subprocess appeared as new tmux window in daemon's session (substrate active) | **PASS** — window 2, pane `%17` appeared in session `harmonik`; pane was inspectable while live |
| 9 | Hook events flowed through bridge to socket | **PASS** — agent_ready + outcome_emitted both relay-sourced events |

All nine checks pass. **GREEN.**

---

## Notes on cleanup behavior

Per PL-021b cleanup, the worktree at `.harmonik/worktrees/019e2cf2-…` is removed on `run_completed`. Direct post-hoc inspection of `marker.txt` and `.claude/settings.json` at the worktree path is therefore impossible after the fact; verification instead comes from (a) the merged commit on `main` (and bare origin) carrying the `SMOKE-OK` content, and (b) the event sequence (`agent_ready` and `outcome_emitted`) which only fires if the bridge hooks materialized correctly.

One benign warning surfaced in stderr — `mergeRunBranchToMain` noted uncommitted changes in the project tree (the very `.beads/`, `.harmonik/`, and hk log files we created in `SMOKE_DIR`); this is expected smoke-fixture noise, not a defect. The merge still succeeded (commit `2b717b2` is on `main` and bare `origin/main`).

---

## Disposition

Substrate wiring (hk-kqdpf.4) + bridge integration (hk-gql20) confirmed end-to-end. hk-kqdpf.5 acceptance met. Per the bead body, close: hk-kqdpf.5, hk-kqdpf (epic, all children resolved), hk-gql20.23 (supersede note), hk-1n0cw (epic — the original RED dogfood smoke whose GREEN target this run satisfies). hk-w5vra and hk-w5vra.7 were already closed 2026-05-15 ahead of this run.
