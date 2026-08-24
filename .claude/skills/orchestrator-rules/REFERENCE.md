<!-- SOURCE OF TRUTH: cmd/harmonik/assets/skills/orchestrator-rules/REFERENCE.md (Go //go:embed).
     The copy at .claude/skills/orchestrator-rules/REFERENCE.md is GENERATED OUTPUT — `harmonik sync-assets`
     overwrites it from the embed and there is NO reverse sync, so an edit made only there silently
     drifts and is eventually reverted. Edit the cmd/harmonik/assets/ copy, then mirror it
     byte-for-byte into .claude/skills/ in the SAME commit. -->

<!-- TIER: B (behavioral contract — changes only on a deliberate rule change)
     LOADED BY: on demand, from SKILL.md. Not part of the boot load.
     OWNER: orchestrator-rules; mirrored to .claude/skills/orchestrator-rules/REFERENCE.md
     DO NOT PUT HERE: operational state (→ .harmonik/context/ + HANDOFF.md); per-domain detail (→ the named domain skill) -->

# Orchestrator rules — reference

`SKILL.md` is the contract. This file holds the procedures the contract owns, for the reader who needs one. Read a section, not the file.

<!-- BEGIN harmonik:managed orchestrator-rules-reference -->

## Run liveness — tell slow from stuck by evidence, not impatience

**Principle: killing or re-dispatching a live run costs more than waiting out a slow one, so get evidence from the durable event trace before you intervene.** Different silences mean different things. A slow run and a wedged crew look alike and need opposite responses.

**This section is the canonical wedge test.** Other role files describe when to run it. They do not redefine it.

**`run_stale` is not a wedge signal.** Before flagging a run as wedged or re-dispatching it:

1. **Wait for the slow-recovery ceiling.** `run_stale` fires at about 10 minutes of silence, well before the real recovery window closes. The real ceiling is **about 30 minutes from the relevant phase launch** — implementer launch, reviewer launch, or commit_gate entry — not from the first `run_stale`. A silent implementer grinding a long node, a thinking reviewer, and the commit_gate working the merge sequence are all legitimate.
2. **Ground-truth via the durable per-`run_id` event trace**, not the subscribe heartbeat's `last_event_id`, which is a global cursor:
   ```bash
   jq 'select(.run_id == "<run_id>")' .harmonik/events/events.jsonl | tail -20
   ```
   Inspect the last event TYPE and node. For example `node_dispatch_requested` for `commit_gate` means the run is grinding the merge gate, which is normal.
3. **A genuine wedge needs durable evidence past launch plus 30 minutes, and it needs all three signs at once:** the worktree is pristine, so the implementer wrote nothing; AND no live tmux session exists for the run; AND the daemon has emitted `run_stale` at least twice (`emit_count` of 2 or more) instead of promoting it to `run_failed`. That combination is the signature of a run stuck waiting on a dead session. Any one sign alone is not evidence — a pristine worktree is normal early, and a repeated `run_stale` is normal for a long node. Only with all three is a reap warranted: rebuild and restart in a lull, and announce the hold and the return to green.

A `run_stale` during legitimate slow recovery is not a wedge.

**Slow-but-live RUN versus SILENT-WEDGED CREW — opposite responses.** A slow run emits `run_stale` and is grinding, so wait the 30-minute ceiling above. A **silent-wedged crew** emits NOTHING — a submit-wedge, where a directive typed into the crew pane never submitted, or a dead wake-trigger, where an in-flight bead was closed out-of-band so its `run_completed` never fires — and it waits forever, so INTERVENE and re-drive the pane. Discriminator: a healthy crew shows an active spinner OR an empty `❯ ` input box. Stable non-empty input with no spinner means wedged. Detail and recovery: the crew process-liveness sweep in `.claude/skills/captain/STARTUP.md`.

## Monitor pattern

**Principle: from submit to completion you are blind unless something is watching, so watch the event stream rather than polling the beads.**

Use `harmonik subscribe` in a Monitor tool call. The **harmonik-dispatch** skill owns the exact command line, including the type list and the heartbeat flag. Four things about it belong here:

- **Filter by event TYPE, not `bead_id`.** `run_completed` is keyed by `run_id` only, so grepping a subscribe stream by `bead_id` silently drops completions.
- **Fallback**, only if subscribe is unavailable: `tail -F .harmonik/events/events.jsonl | grep -E "run_completed|run_failed|run_stale|queue_paused|merge_conflict|reviewer_verdict"`. There is no `daemon.log` and no per-run output file to tail.
- **Keep `queue_paused` in whichever list you use.** Every other type reports one run. `queue_paused` reports that the queue itself stopped. It arrives once and then the queue is silent, so a watcher without it reads a stopped queue as an idle one. What to do about one: the **harmonik-dispatch** skill, § Restart a queue that stopped.
- **Captain exception.** Run-level subscribe is for an orchestrator watching its OWN submitted batches. A booted captain arms only its two watchers — the comms feed and `epic_completed`, per its operating doc — because run-level telemetry is the crews' to watch. Subscribing the captain to it is the context-burn pattern the two-watcher rule exists to prevent.

`subscribe` attaches to the running daemon, so ONE Monitor sees every bead the daemon dispatches whichever agent submitted it. Re-arm it if it hits the Monitor timeout.

## On batch failure — failures are signal, not retry fodder

**Principle: a repeated failure means your model of the failure is wrong.** Re-dispatching without new understanding just burns the bead again. Classify first, investigate early, and retry only when you have a reason to expect a different outcome.

When a submitted batch returns failures (a group reaches complete-with-failures, or `harmonik subscribe` reports `run_failed`):

1. Read the failure class from `.harmonik/events/events.jsonl` (`no_commit`, `context_cancelled`, and so on).
2. If the **same bead failed twice** this session, dispatch an investigator sub-agent. Do not re-dispatch the bead.
3. If this is a **new failure class**, file a bead and dispatch an investigator.
4. Treat a third dispatch of the same bead without an investigation in between as the point to stop and think.
5. Reopen any beads incorrectly closed by implementers.

**Investigation dispatch template:** anchor the investigator to **durable artifacts** (file paths, symbol names, `events.jsonl` entries), NOT ephemeral state (tmux pane contents, live process output): "Start with `<file>` `<symbol>`, read the code and comments there, then check `<specific durable artifact>`. Report root cause in under 200 words."

## Bead lifecycle — the mechanism behind hard rule 4

- **The retired opt-out spelling.** Hard rule 5 says the per-bead opt-out is `--workflow-mode single`. The old `--no-review-loop` spelling is retired and now exits 1.
- **Neither hazard is silent, and the one that is silent is a third thing.** A pre-set `in_progress` is rejected at the door: `queue submit` returns `bead_already_dispatched` (JSON-RPC `-32015`), prints it, and exits 1. A hand `br close` from inside a worktree is visible in the ledger the moment anyone looks. The failure nothing reports is claiming a bead by hand and then never submitting it — no run exists, so no watcher, no timeout and no sweep has anything to notice. If you claim, either submit it or work it through.
- **Ledger-honesty reconciles — the two writes that stay yours on a submitted lane.** Hard rule 4 gives the daemon the terminal transitions of the work you submit and leaves you every bead you did not. Two writes stay yours even on a submitted lane. Reopen any bead marked closed-without-commit (`br update <id> --status=open`) — implementers sometimes `br close` and then exit without producing code. Run `br close <id>` the moment you merge code that satisfies a bead. Both are the ledger catching up to reality, not the orchestrator driving lifecycle.
- **Implementer commit and push discipline.** Implementer briefs must end with "COMMIT EXPLICITLY" and `git push origin HEAD`, and the orchestrator verifies the commit landed. `.claude/implementer-protocol.md` is authoritative for the implementer lifecycle.
- **Throwaway-canary — a recommended process, not a hard rule.** When you are probing whether the system can spawn a worker at all — a health check, not real work — use a fake throwaway task as the canary. A real bead that fails mid-probe is wasted effort and muddies that bead's history. Pattern: `br create --title="canary: throwaway smoke probe" --type=chore --priority=4`, then close it after the probe.

## Priority — the detail behind the one-liner

`harmonik-dispatch` owns the ranking procedure. Three things about priority live only here:

- **Stated intent outranks any ranking a tool produces.** It lives wherever the operator and the admiral wrote it down. A flagship that no ranking surfaces is a signal to re-float it, never a reason to work past it onto grab-bag churn.
- **Friction gets priority, because we are dogfooding.** If the agents do not fix their own painpoints, nobody will. File any friction bead at P1 minimum and let it jump ahead of substantive feature work. If you are about to rank a friction bead below feature work, say out loud why this one is the exception.
- **`kerf next` is still wired into the daemon, and that is known.** The daemon's eager refill pulls its candidate beads from `kerf next` and keeps that order when it appends them to a stream queue. `specs/execution-model.md` EM-062 and EM-063 specify that path, and `internal/daemon/eagerfill_em063.go` implements it. The spec is normative, so it stays as written until the code changes with it, and that work has its own bead. Read the mismatch as known and tracked. It does not make `kerf next` a priority source for you.

## Review and quality — independent eyes, and verify claims against ground truth

**Principle: work is not done when someone says it is done. It is done when independent eyes or ground truth confirm it.** This applies to merges (a reviewer), to bead counts (the ledger lies), and to plans (observable acceptance criteria). The normative statement of the commit-time review gate is `docs/foundation/project-level/build-practices.md`; what follows is the dispatch-side detail.

- **Reviewers miss composition-root wiring.** Reviewer briefs should include a "find the production call site" check.
- **Anti-fake mutation tests must use a real isolation boundary.** To prove that a regression test fails when only the production fix is removed, use a detached worktree (`git worktree add --detach <path> <base>`), a real clone, or read-only `git show` and `git diff`. **Never `cp -R` a Git worktree and run Git mutations in the copy.** A linked worktree's `.git` is a pointer file. Copying it preserves the pointer to the original worktree's index and HEAD, so `checkout`, `revert`, `stash`, or `reset` in the supposed copy mutates the live worktree. To audit an existing scratch area, find pointer files with `find <scratch> -maxdepth 2 -name .git -type f` and cross-check each path against `git worktree list`.
- **Trust `br ready`, but verify.** Cross-check `br stats`, and run `br ready --limit 0` before declaring a lane empty. The default 20-row page hides ready beads.
- **Subsumed beads are common.** Dispatch an audit-then-sweep before assuming an open-count is real.
- **Plans have "done means...".** Every `_plan.md` needs observable acceptance criteria.
- **Dispatch shape.** Implementers: `model=sonnet`, `effort=high`, `isolation=worktree`, `run_in_background=true`. Reviewers: `model=sonnet`, `effort=high`, no isolation — but isolate any reviewer that does a `git checkout`, because a non-isolated reviewer mutates the main repo's branch. When you dispatch two or more sub-agents in parallel, pass `run_in_background: true` on each.

## CWD and commit discipline — the working tree is shared with a live daemon

**Principle: your working tree and its worktrees are SHARED with a live daemon that checks out, reverts, merges, and removes files under you. Never act as if you own them.** The never-`cd`-into-a-worktree rule is hard rule 7 in `SKILL.md`. Two corollaries live here:

- **Operate from the repo root for the whole session.** The merge dance runs from the repo root. If a worktree is gone, cherry-pick from the reflog.
- **Stage specific paths, not `git add -A`.** Because the tree is shared, stage exactly the paths you changed (`git add <path> ...`). A blanket add once committed a source tree the daemon had reverted mid-operation. Corollary for lane-state docs such as `captain-lanes.md` and `lanes.json`: stage the specific path AND commit in the same action as the edit, because an uncommitted replace-in-place rewrite is the only copy of current truth until it lands.

## Autonomy — the KNOWN-vs-brand-new definition

**This section is the canonical home of the KNOWN-vs-brand-new definition.** Every role file — captain, admiral, watch — carries a one-line pointer back here. `SKILL.md` carries the one-line summaries. These are principles: each names the intent and the tiebreaker, and trusts the agent. They dissolve the stall class where "resume a known, parked, already-ranked lane" gets mis-classified as "rank a brand-new initiative" (the operator-only class) and the fleet sits idle on standing authority.

**Self-authorization.** A lane recorded in **any durable doc** — or **ever ranked in any feed** — is a **KNOWN** lane. Resuming it, un-parking it, or re-staffing it is the orchestrator's own call, whether captain or admiral, *even when it is currently parked or shows zero ready beads in the live feed this instant.* Only a **never-before-recorded** initiative is the operator's to rank. A lane is **GATED only when a named, dated, owned, expiring gate is present.** Absence of a live named gate means KNOWN and resumable. If you are unsure and the lane appears in any durable doc, **treat it as KNOWN and act.**

**WIP-first is a tiebreaker, never a veto.** Default to advancing started work before unstarted epics, for "all else equal" only. The operator can reprioritize anything, anytime, and WIP-first never overrides a fresh operator directive. **No agent may EVER cite started-work as a reason it "can't" reshuffle priorities.** Catching yourself about to refuse a reprioritization on WIP grounds IS the signal that you turned a tiebreaker into a veto.

**Refresh-then-act is LIGHT.** Re-derive the one fact you are about to act on, not everything. Act on the boot digest's live numbers for fleet condition, never on a claim carried in a doc or a handoff — that holds for ALL durable docs, not only HANDOFF. The digest carries no backlog, so it is not the answer to what to work on. For a one-off in-loop action between boots, re-derive only the single fact you are betting on, for example `br ready --parent <epic> --limit 0` for the lane you are about to staff. A glance, not a re-audit.

**"Operator away" is NOT a HOLD trigger.** Away plus ready KNOWN work means staff it, autonomously. "Lean" means do not speculatively spin up NEW crews for empty-backlog lanes. It does not mean leaving ready, already-ranked work unstaffed. This is a project-level correction of the `feedback_captain_lean_while_operator_away` memory note's over-read ("away → HOLD ready work"). Do NOT amend the cross-project `~/.claude/CLAUDE.md`.

**Daemon restart and redeploy is self-authorized, never operator-gated.** The captain and admiral restart and redeploy the daemon on their OWN authority. It is routine work, NOT a "destructive op" and NOT a "surface-and-await" item. They coordinate but never ask permission: announce over comms, especially during an active fan-out; pick a true lull so an in-flight bead is not stranded; if the supervisor is actively reviving, let it win; if the supervisor is confirmed dead, restart the daemon yourself; disable the operator's in-use worker box first only if the redeploy touches that shared machine. These are timing conditions, not a gate. The operator-only destructive class is force-push, `branch -D` on a shared ref, `rm -rf`, and `--no-verify` on shared history. A daemon restart is not one of them.

**Do not ask — execute.** On `/session-resume` with no hard blocker, execute. Do not close a say-back with an A/B question.

**Push autonomy.** The orchestrator pushes its own work without asking for a per-push confirmation. Only the operator-only destructive class above needs a human.

**Active dispatch — do not park the stream.** When the critical path is serialized, pull from the broader queue rather than idling behind it. The lane-level twin of this rule is: never sequence the whole fleet behind one lane.

**Queue a decision with context.** Do not queue minor work to the operator. When you queue a real decision, include a plain-English description, why you queued it, and concrete options with their consequences. Before any operator-facing message, apply the pre-send check in the global `~/.claude/CLAUDE.md`: tool terminology is fine, a private tracking identifier used as the handle for a thing is not. Agent-to-agent comms and bead and commit text keep their codes.

**Read to route, delegate to solve.** The main thread exists to dispatch, and its context window is the scarcest resource in the fleet. Reading enough to know who should fix a thing is part of dispatching. Open the stack trace, the event trace, and the one file the error names. What burns the main thread is the second and third file, the reproduction you run yourself, the fix you start drafting. Treat "I am opening my third file" as the signal to stop, file a bead, hand a sub-agent what you have already learned, and go back to dispatching.

**Fail fast and loud.** When the system says something already exists or is already claimed — a name or queue collision, a lock, a duplicate — that is INFORMATION. Stop and diagnose. Never auto-rename or auto-retry around it. A crew-start collision almost always means the lane is already staffed, and relaunching under a new name double-staffs the epic.

**A stopped queue is the inverse trap, and the harder one.** Learn which refusals exist, because the missing one is the point. A submit is refused with `queue_already_active` (`-32010`) when the named queue is `active` or `paused-by-drain`. An append is refused with `queue_not_advancing` (`-32012`) when the queue is `paused-by-failure` or `paused-by-drain`. A submit to a queue at `paused-by-failure` is NOT refused. `Validate` in `internal/queue/validation.go` excludes that status from `-32010` deliberately. So a lane a failed bead stopped meets no closed door at all: the submit is accepted, a fresh queue starts, and the parked failed items are left behind. Nothing tells you. Read the `status` field from `harmonik queue list --json` rather than waiting for an error, and restart the queue you own — the **harmonik-dispatch** skill, § Restart a queue that stopped.

## Environment facts

These are not principles. They are properties of this project's environment, checked rather than assumed.

- **CI exists and is merge-blocking.** `.github/workflows/ci.yml` runs `make full` on every push and every pull request. There is no CI-only tier. Three more workflows run beside it: nightly race, release, and scenario. Never add `continue-on-error` to a check job — it silences the run, step, and check-run conclusions at once, so every REST surface reports success after a real failure.
- **Sub-agents can write markdown.** Nothing in `.claude/settings.json` or `.claude/settings.local.json` blocks it. Have the sub-agent write its own files. Pulling a document back through the main thread so the orchestrator can persist it spends the one context you exist to protect.

## Planning artifact placement

**Principle: every artifact lives at the home that matches its time horizon**, so short-lived state never fossilizes inside a durable doc, and durable rules never hide inside a session file. `AGENTS.md` owns the placement of specs and kerf artifacts; this table covers the orchestration tiers.

| Horizon | File or location | Belongs there |
|---|---|---|
| Current session / current run | `HANDOFF.md` | Immediate state: in-flight work, monitor state, blockers, next action, recovery notes. |
| Days / active lanes | `.harmonik/context/captain-lanes.md` | Lane and epic registry, crew-to-queue mapping, parked work, dated operator directives. |
| Weeks / durable guardrails | `.harmonik/context/project.yaml` | Phase, locked decisions, forbidden actions, durable project guardrails. |
| Months / milestone history | `ROADMAP.md` | Long-horizon progress, completed campaigns, milestone narrative. |

Do not use `AGENTS.md` or this skill as operational state. They are routing and behavioral contracts.

<!-- END harmonik:managed -->
