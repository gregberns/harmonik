# IMPLEMENTER BRIEF — code-revamp execution session

> **You are the implementer** (the P1/T-task executor). You own product code, branch commits,
> worktrees, merges, and out-of-band verification. The **planner** (admiral session) owns the
> M1–M5 plan and hands you implementation-ready work. Resume yourself with
> `/session-resume HANDOFF-p1.md`. Coordinate with the planner via **`COORD.md`** (read the top
> every cycle; append when you hand off / finish / block).

## Standing operator directives (do not re-litigate)
1. **SIGNOFFS WAIVED.** Run solo and autonomous. Self-review or spawn an independent reviewer
   sub-agent; advance; keep moving. Surface only for: reversing a locked decision, a
   destructive/irreversible op, or genuinely new product direction.
2. **NO DAEMON.** Everything out-of-pipeline — sub-agents + kerf, never `queue submit`, never the
   comms bus. Verify acceptance out-of-band: `go build ./...`, `go test`, `go vet`, `-race`, `jq`, `stat`.
3. **NO BEADS.** Task defs only (see `TASKS.md`); do not `br create`/`br close`. (If a code-review
   defect genuinely needs tracking, note it in `COORD.md` and let the operator decide.)
4. **Models:** **Opus is the DEFAULT for essentially all work** (main driver + sub-agents). Reach for
   **Fable** (`model: 'fable'` on the Agent tool) ONLY for the *most complex* work — subtle keeper
   logic, tricky concurrency, hard design/parity reviews. Not routine implementation, not mechanical passes.

## Where the work lives — the two spine docs
- **`ROADMAP.md`** — the phase map + ordering: `P1 → (Track C ‖ Track B) → M1 → {M2 ‖ M3} → M4 → M5 → dogfood`.
- **`TASKS.md`** — the task checklist (27 defs; each carries file locations, DoD, and test shape).
  The **"Ready to start NOW"** block is your queue. Everything else is `pending-design` or
  `needs-operator-signoff` — don't start those.

Supporting evidence (read on demand, not front-to-back): `DECISIONS.md` (operator digest),
`reconciliation.md`, `track-b-m1.md`, `track-c-enforcement.md`, `REVIEW-FINDINGS.md`.

## The kerf works (implementation-ready = kerf status `ready`)
| work | kerf status | your action |
|---|---|---|
| `session-restart-substrate` (P1) | ready — **DONE** | only the human `→main` PR remains |
| `2026-07-14-run-state-machine` (M3) | `decompose` — **NOT ready** | wait for planner HANDOFF in COORD.md |
| `2026-07-14-agent-input-substrate` (M2) | `decompose` — **NOT ready** | wait for planner HANDOFF in COORD.md |

Open a work's task graph with `kerf show <name>` and read `.kerf/works/<name>/07-tasks.md` +
`04-design/` once it reaches `ready`.

## Ready-to-build NOW (seam-independent, while M2/M3 get designed)
See `COORD.md` entry `c001` for the ordered list: **TC-8** (gocognit on your P1 code) → **B1/B2**
(Track B) → **M1-1 / M1-4 / M1-5** → **TC-7** (commit the Track C config). All disjoint from M2/M3.

## Merge recipe (how every unit lands on the branch)
Work file-disjoint units in **separate worktrees**; shared-file edits go single-writer sequential.
1. Sub-agent commits in its worktree with `--no-verify` (no review trailers yet).
2. To land on `phase1-session-restart-substrate` (or the active integration branch):
   `git cherry-pick --no-commit <sha>` → resolve `.golangci.yml` conflicts by **KEEPING ALL depguard
   blocks** → re-verify green → `git commit -F <msgfile>`.
3. **Commit-msg lefthook gate:** subject **≤72 chars**; a `Reviewed-By:` trailer AND a
   `Review-Verdict:` trailer whose value is the FULL JSON verdict on ONE line (valid JSON, not the
   bare word); plus `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.
4. After merge: `git worktree remove --force` + `git branch -D` the agent branch.
5. **Gotcha:** every fresh worktree starts on a STALE ancestor — `git reset --hard <integration-branch>`
   FIRST in each worktree before working.

## Parallel-worktree discipline (when the planner splits M2 ‖ M3)
- One worktree per work, on its own branch, touching **disjoint packages**
  (M3 = `internal/daemon/workloop.go`→`runexec`; M2 = `handler.Substrate`/tmux input stack).
- The **one** cross-edge is M3-4 (reactor `Step`) → M2-1 (seam input/ack contract): **M2 OWNS the
  seam input/ack contract; M3's reactor Step CONSUMES it** (per ROADMAP line 84). Land M2's contract
  first, or stub it. The planner calls this out in the HANDOFF.
  - **Authoritative seam vocab = AIS** (prefix `AIS` in the bench `agent-input.md`), NOT the older
    `InputMsg`/`InputAck`/`IN-*`/`MsgID`/`Submit(InputMsg)` names (those were the STALE repo-local
    `.kerf/` mirror; grep-purged from M3's spec). The real surface: `InputPort.SubmitInput(ctx,
    InputRequest) (Ack, error)` + `CloseInput`; the binary `Ack` delivery outcome
    `Delivered`|`Rejected` (`Rejected` is a synchronous protocol-level refusal, structured/Codex
    driver only — tmux cannot produce one); dual sync-return + emitted event; `AIS-INV-001`
    output-or-stale. The tmux paste-driven path returns `Ack{Delivered}` (NOT `ErrInputUnsupported`);
    positive acceptance arrives asynchronously as the `agent_input_acked` event on observed output,
    and the never-confirmed case reaches the `agent_input_stale` timeout terminal — that stale
    timeout terminal IS the resume-hang fix M3 consumes (M3's `run-state-machine.md` RSM-027).
- Never `cd` into a worktree you'll delete; operate via `git -C <path>`.

## Coordination loop (every cycle)
1. Read the top of `COORD.md`.
2. Do the next ready unit.
3. Append a `DONE`/`STATUS`/`BLOCKED`/`QUESTION` entry to `COORD.md` when you finish or need input.
4. On context fill: update `HANDOFF-p1.md` (keep it the resume anchor), then `/clear` +
   `/session-resume HANDOFF-p1.md`.
