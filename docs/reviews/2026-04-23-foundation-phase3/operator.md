# Operator Review — Foundation 2026-04-21

## Summary verdict

The foundation is architecturally coherent and impressively rigorous about its invariants (three-store authority, lease-by-run, reconciliation-as-workflow), but **it is not yet operable in production**. The document specifies *what* the operator does (pause/stop/upgrade command names, state machine states) but leaves critical *how-does-it-feel-at-3am* questions unanswered: what the operator sees during a stuck reconciliation, how to override an investigator verdict, what happens when a child process silently hangs (not crashes), how multi-daemon machines are coordinated, what exit codes mean, what `harmonik upgrade` actually does, and what the recovery procedure is when git, Beads, or the pidfile are corrupt. Several spec sections mention "operator-configurable" cadences, thresholds, and timeouts without saying where they live or how to change them. An operator handed this spec today could not confidently run a single project through a crash-and-recover cycle without a runbook that does not yet exist.

## Critical findings (block running this in practice)

- **No specified failure mode for daemon startup preconditions beyond "another daemon running."** `02-components.md §8.2` step 1 covers the pidfile-lock case. It does not cover: (a) `.harmonik/` directory missing or unwritable, (b) git repo in detached-HEAD or mid-rebase state, (c) Beads SQLite database corrupt or schema-version newer than daemon expects (§7.4 says "unsupported versions cause startup failure with a specific error code" but the codes are not enumerated), (d) a checkpoint commit whose `Harmonik-Schema-Version` trailer exceeds the daemon's supported N-1 window. Each of these is a 3am-page scenario. Operator needs: structured error catalog, exit-code table, and a recovery procedure per class.

- **`harmonik upgrade` is named but not contracted.** §8.3 lists the command; §7.3 says upgrade transitions `running → pausing → paused → upgrading → running (new binary)`; §7.2 says "commit-hash check (the to-be-installed binary's source-commit hash must match the operator-supplied expected hash)" — but nothing specifies: how does the operator supply that expected hash? Where does the new binary come from (path? flag? inbox directory)? What happens to the socket during the exec-replacement — do client CLI commands retry? If the new binary rejects the queue format, does the old binary resume or does the daemon stay `paused`? Without these answers, upgrade is a theoretical state transition, not an operator capability.

- **No contract for silent-hang detection.** §8.5 says "agent-subprocess failure (crash, hang, policy violation) is observed by the daemon per handler-contract.md §4.6," but §4.6 only defines crash-via-Wait and ctx-canceled paths. A handler that is still alive, still connected to the socket, but producing no `agent_output_chunk` events for an arbitrary period has no described detector. §7.1 mentions heartbeats "on a defined cadence" with no cadence specified and no action path beyond "degraded classification." Scenario: a Claude Code session is waiting on an infinite backend spin. The daemon has no automatic remedy.

- **Reconciliation has no operator override or interruption contract.** §7.3's "Reconciliation carve-out" says pause is *queued* during `reconciling`, i.e., the operator cannot interrupt reconciliation. §9.5 lists the verdict enum but does not give the operator a path to: (a) see the investigator's reasoning before the daemon executes the verdict, (b) veto `reopen-bead` or `reset-to-checkpoint`, (c) promote `resume-here` to `escalate-to-human` manually. For Cat 6 cases that are the highest-stakes, the operator is a passive observer. This is likely wrong for production — a human running a first release will want a "pause before executing verdict" flag on investigator workflows.

- **Multi-daemon coordination is effectively un-specified.** §7.10 says "multi-tenancy reduces to 'run more daemons, one per project' — an OS-process-isolation concern, not a harmonik-code concern." That is fine as a philosophy; operationally, an operator with 10 projects needs: a way to list all running daemons across the machine (there is no `harmonik list` command in §8.3), global resource-budget enforcement (if 10 daemons each start 5 agents, that's 50 Claude subprocesses — the spec does not mention a machine-level ceiling), cross-daemon event aggregation for "what's happening on this box?", and a clean-shutdown-all procedure. `harmonik stop` operates on one daemon, identified how — by cwd? by socket path flag? The command surface in §8.3 does not say.

## Important findings (missing UX, incomplete failure handling)

- **Exit code taxonomy missing.** §7.1 says "operator-observable exit codes: defined per operator command; non-zero exit codes are structured (category → code mapping specified)" — but no mapping is given in these 924 lines. Operators script against exit codes; this has to be nailed down.

- **"Operator-configurable" without a config surface.** §3.4 ("timer-flush cadence is operator-configurable"), §7.7 ("drain timeout: operator-configurable"), §6.9 ("warning threshold: at 80% of budget, configurable") all mention operator configurability without saying where these knobs live. §6.8 describes precedence but not inventory. An operator cannot tune what they cannot find.

- **Disk-full / resource-exhaustion during checkpoint commit is undefined.** §2.1a requires a checkpoint before every durable state transition. If disk is full mid-commit, the run cannot advance past the transition. Is this a Cat 6 integrity violation? A `budget_exhausted` variant for disk? A new failure class? Not answered.

- **Malformed Beads records / `br` CLI returns non-JSON or times out.** §10.8 names a `br`-CLI adapter as the integration seam, but does not specify the adapter's behavior when `br` returns garbage, hangs, or exits non-zero during startup (§8.2 step 3) vs during steady-state claim. The daemon's resilience to a broken `br` is unstated.

- **tmux-daemon death while the harmonik daemon is alive.** §8.7 uses ntm for tmux spawning. If the tmux server dies (OOM-killed, kernel panic survivor), every agent subprocess becomes unreachable. Is this Cat 6? Is there a detector? §9.3 Cat 6 mentions "workspace path referenced by in-flight bead does not exist on disk" but not "tmux session gone."

- **`harmonik attach` failure modes.** §8.3 says multiple attaches are supported. Not specified: what happens if the attach process crashes while holding a socket read? Does it back-pressure the daemon? Does the attach get a snapshot on connect or only live events? Can an operator get a *historical* view of what happened 10 minutes ago without re-reading JSONL?

- **Stale-pidfile detection.** §8.8 says "by checking the PID is no longer a live process." On shared machines, PIDs are reused. Without a secondary check (start-time, process name, socket-file presence + listener check) this is race-prone.

- **"What happened to bead X?" traceability is implied but not assembled.** The artifacts exist (Beads audit log, git commit trailer with `Harmonik-Bead-ID`, events with optional `bead_id` payload, session logs with bead-ID metadata). The spec does not say an operator has a single command or doc path that joins these into one view. Without it, 3am debugging means hand-joining four stores.

- **`harmonik status` output shape is unspecified.** It appears in §8.3 as a listed command only. Operators need to know: can it answer "which runs are currently in reconciliation?" "What are the last 10 events?" "What is the resident memory and subprocess count?" Not said.

## Questions I'd need answered before I'd run this in prod

- **Scenario: bead X's run crashed at 3am, I'm paged at 9am.** What's the exact operator sequence? `harmonik status` to see current state → `harmonik attach` to watch → wait for reconciliation verdict → ??? If the verdict was `escalate-to-human`, what do I read to understand why?
- If reconciliation has been running for 5 minutes and is not progressing, how do I tell (a) it is healthy-slow, (b) the investigator agent is hung, (c) the detector itself is looping? §7.8's "degraded state that reports `reconciling` with progress markers" — what do the progress markers look like?
- If I `harmonik stop --immediate` (§7.7 lists it but does not list the flag), then restart, what guarantees exist about agent subprocesses being actually dead and not orphaned as zombies under a different PPID?
- How does `harmonik upgrade` interact with an in-flight reconciliation? Is upgrade queued behind reconciliation the same way pause is?
- What is the minimum privilege required to run a daemon? Can it run as non-root? What filesystem permissions on `.harmonik/daemon.sock` — `0600`? `0660`? Who can `harmonik attach`?
- If dead-letter queue (§3.7) grows unboundedly because an async consumer is permanently broken, is there a cap? A rotation policy? An operator procedure to drain/discard?
- Upgrade-across-schema-break (§7.5 references "migration release") — what is the migration command? Is it inside `harmonik upgrade` or a separate `harmonik migrate`? Does it run against a paused daemon or a stopped one?
- What does the operator see in the attach UI during a Cat 3 store divergence — the raw disagreement, the investigator's reasoning, both?

## Affirmations

- **Reconciliation-as-workflow is operationally smart.** Because investigator runs are themselves normal workflows, operators get the same observability (checkpoint commits, events, session logs) for recovery as for production work. This is a big win; the spec should lean into it by specifying exactly what the reconciliation-workflow artifacts look like in each store.
- **Per-project daemon isolation is the right default.** Blast radius is bounded; one project's corrupt Beads does not take down another.
- **Three-store authority with "git wins on completion" is operator-legible.** I can grep `git log --grep Harmonik-Bead-ID` and get ground truth; that's a good property.
- **§8.6 daemon-vs-orchestrator-agent distinction is load-bearing and correct.** It prevents the worst class of debugging headache (an LLM silently influenced a dispatch decision).
- **§10.4 "terminal transitions only" write rule to Beads** is a principled way to avoid thrashing a shared ledger; it also means Beads's audit log is high-signal for operators.
- **Reconciliation category taxonomy (§9.2) is the right level of abstraction.** Six categories is tractable; the granularity-bias ("prefer more categories") is correct for investigator-agent simplicity.

The spec is a strong architectural foundation. It needs an explicit "operator runbook" pass — exit codes, config inventory, failure-mode matrix, multi-daemon commands, upgrade contract, silent-hang detection — before an on-call engineer can own it.
