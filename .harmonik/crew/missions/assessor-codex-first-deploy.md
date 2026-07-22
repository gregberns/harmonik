---
schema_version: 2
assessor_name: assessor-codex-first-deploy
epic_id: hk-tckw3
branch: phase1-session-restart-substrate
gate: deploy
commit: d59d5d325ab55e2b4e293f9ac0f49bda042e86bc
found_by_sources: [assessor, admiral, fast-follow]
report_path: .harmonik/reports/codex-first-deploy-gate.md
spawned_by: admiral
---

# Gate: deploy — phase1-session-restart-substrate @ d59d5d32

You are the **assessor** for epic **hk-tckw3** (codex-first) on branch
**phase1-session-restart-substrate** at commit **d59d5d32**. Run the **deploy** gate on an
isolated scratch clone with your OWN daemon, file findings as scoped `found-by:assessor`
beads labelled `hk-tckw3`, post a reasoned `PASS|BLOCK` to **admiral** over `--topic gate`,
and self-terminate. The admiral holds the release decision; you execute and recommend.

## Current State

**Why this gate exists.** Tonight the fleet swapped this commit into the production daemon
without any assessor gate, watched it die twice, and rolled production back 77 commits to
`894e2856` (2026-07-18, which predates the PR#31 main merge). The operator has restated the
standing rule: **the assessor validates ALL releases**, and a daemon binary swap IS a release.
Nothing goes back into the production daemon until you have proven this commit on your own
isolated daemon. Do not use the production daemon for any part of this. Do not restart it.

**What this commit claims to deliver** (both are needed for the codex-first proof):
- `2b921fde` (hk-9hvr0) — the daemon's input-paste call site at
  `internal/daemon/tmuxsubstrate.go:2246` used the legacy constant
  `inputBufferName="harmonik-input"`, which violates the tmux buffer-name invariant added on
  this branch at `internal/lifecycle/tmux/osadapter.go:382` (requires
  `harmonik-<session-id>-<purpose>`). Result was `pasteinject_failed`: workers reached
  `agent_ready` and then idled forever, never receiving their task prompt. The fix constructs
  a valid per-run buffer name at the call site.
- `d59d5d32` (hk-tckw3.1) — codex-first Step 1: drops the fail-closed isolation fence that
  forbade launching Codex without an enabled remote ssh worker, and sets the exec path to
  `danger-full-access`. Per locked decisions D1 and D3
  (`plans/2026-07-20-codex-strategy-realignment/DECISIONS.md`) the fence was agent-invented,
  not an operator mandate, and native harness sandboxes are OFF across all harnesses — Codex
  runs the same host posture Claude already runs.

**THE PRIMARY QUESTION — the unexplained exits (hk-45pm7, P0).** This is the gate's central
concern and the reason the rollback happened. From `.harmonik/events/events.jsonl`
(all times UTC 2026-07-22):

    00:21:53  daemon_started       binary_commit_hash=d59d5d32  pid=70809
    00:28:46  supervisor_revival   cause=unexpected_exit  prior_pid=70809  (~7 min uptime)
    00:28:45  daemon_started       binary_commit_hash=d59d5d32  pid=77053
    02:01:33  supervisor_revival   cause=unexpected_exit  prior_pid=77053  (~93 min uptime)
    02:01:32  daemon_started       binary_commit_hash=894e2856   <- the rollback

Two clean starts that ran for minutes and then exited on their own. **No cause was ever
named.** Note explicitly what this is NOT: an earlier finding in this session was that `cp`
of the Go binary on macOS invalidates its code signature and the kernel SIGKILLs it (exit
137), requiring `codesign --force` after the copy. That explains a binary that never execs
at all. It does not explain a process that starts cleanly, serves for 7 or 93 minutes, and
then exits. Treat the signature story as a separate, already-understood runbook gap.

Your job on this leg: stand `d59d5d32` on your isolated daemon **with stdout and stderr
captured to a file**, keep it up long enough to cover both observed failure windows (well
past 93 minutes of uptime), and either (a) reproduce the exit and NAME the cause — panic,
OOM, unhandled signal, supervisor watchdog kill, socket or pidfile contention, goroutine
leak, something else — or (b) demonstrate it is stable in isolation, which would point the
cause at the production environment rather than the code. Either answer is a valid finding;
"could not reproduce, cause unknown" is not, unless you say so explicitly and rank the
residual risk. The captain has also flagged a suspected "work-loop / crew-launch regression"
on this branch worth bisecting — treat that as a lead, not an established fact, and confirm
or refute it. Incident notes: `docs/incidents/2026-07-21-daemon-wedge-rollback.md`.

**THE SECOND LEG — does Codex actually run a bead?** On your isolated daemon at this commit,
with `HARMONIK_SUBSTRATE=codexdriver`, dispatch a trivial brand-new-file chore (no shared-file
race) and watch four signals: (1) the IMPLEMENT node launches `codex exec` on LocalRunner,
(2) no EPERM / sandbox refusal, (3) the commit lands carrying `Refs:<bead>`, (4) the bead
advances through review to close. The reviewer node is deliberately still Claude — that split
is intended, not a defect.

**Known confound, do not misattribute:** the Claude reviewer node runs `claude:local`, which
has a pre-existing wedge — `hk-8juwz`, a theme-modal that makes `agent_ready` time out. It is
a FALSE CLOSE of `hk-oga33`. If Codex implements and commits cleanly but the REVIEW node times
out on `agent_ready`, that is `hk-8juwz` and is NOT a codex-first failure. Report precisely
WHICH node wedges.

**Also verify the paste fix directly:** on this commit a dispatched worker must actually
receive its prompt. `pasteinject_failed` must not appear. A worker reaching `agent_ready` and
then idling with no prompt is the exact regression `2b921fde` claims to fix.

**Scope discipline.** Two things are explicitly out of scope and must not consume your gate:
per locked decision D4, ssh-per-node remote execution is SCRAPPED — `hk-qxvc2`, `hk-daegv`,
the reverse-tunnel and env-forward work are MOOT. And per D3 the native sandbox questions are
a separate parallel workstream, not this gate's business.

**Bead state you will encounter:** `hk-9hvr0` and `hk-tckw3.1` are both still OPEN even though
their code is committed at branch tip — the rollback orphaned their terminal transitions. That
is a ledger artifact, not evidence the work is undone. Do not let it skew the
claimed-done-vs-reality reconciliation; note it and move on.

**What a PASS means here.** Not "the tests are green." It means: the unexplained exit is
either fixed or affirmatively understood and judged acceptable, and Codex demonstrably runs a
real bead end to end on this commit in isolation. Weigh it against
`.harmonik/agents/assessor/good-enough-principles.md`. If you BLOCK, say precisely what would
clear the block. I may probe you on `--topic gate` before I make the call.
