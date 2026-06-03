<!-- PP-TRIAL:v2 2026-06-02 main @7049f53b 0/0 origin — CLEAN, nothing blocking. THREE concurrent agent threads share ONE daemon: named-queues (daemon/queues/scenario), flywheel (ledger/kerf), controlpoints (control-points/infra). Per-thread detail in HANDOFF-<role>.md. Today: overnight daemon-reliability cluster + agent-comms bus landed; afternoon dispatch-deadlock fix (hk-z0pmi) + scenario-test uplift (tagged suite GREEN @6ecfb017) + flywheel bead-reassessment (46→13 open). Daemon UP -c6 under a SINGLE supervisor. -->

Read order (per CLAUDE.md): AGENT_INDEX.md → STATUS.md → TASKS.md. Cross-project: `~/.claude/CLAUDE.md`. Dispatch loop: skill `harmonik-dispatch`.

ROLE: orchestrator. Delegate via the daemon queue / sub-agents; main thread stays minimal. Failed-twice → investigator, don't re-dispatch.

# Where we are (2026-06-02) — CLEAN. Main `7049f53b`, 0/0 origin, daemon UP `-c6`.

## DETERMINE YOUR IDENTITY BY LANE (a `/clear` can mis-ID you — it did today)
Three threads share one daemon: **daemon+queues+scenario** = `named-queues`; **ledger+kerf hygiene** = `flywheel`; **control-points/infra** = `controlpoints`. Read YOUR `HANDOFF-<role>.md`, not just this file. Bus identity must match your lane.

## What's done recently (all landed + on origin)
- **Overnight:** `set-concurrency` (hk-ohiaf), 6-fix daemon-reliability cluster, **agent-comms BUS** (epic hk-uxm0j, T1–T13).
- **Afternoon:** dispatch-deadlock fix (**hk-z0pmi**, QM-002b Class A' — a stuck-`dispatched` item wedged `main` and blocked ALL submits); **scenario-test coverage uplift** — 6 new `//go:build scenario` tests, full tagged suite **GREEN @6ecfb017**; friction-batch closes (hk-i2ie5/yyso7/1k5as/x6j6r).
- **Flywheel:** full open-bead **REASSESSMENT** — 46→13 open, closed 33 stale/landed (10 spec-parent epics + 16 kerf-upstream routed + backlog) with commit+code proof; audit trail `docs/bead-reassessment-2026-06-02/`; kerf baseline acked.

## STANDING DIRECTIVES
- Comms = the **`harmonik comms` BUS** (.md outboxes RETIRED). Monitor: `harmonik comms recv --agent <you> --follow --json`.
- **ONE supervisor:** tmux `hk-daemon-supervise` (`/tmp/hk-daemon-supervise.sh`, `-c6`). Do NOT start a second — the old `/tmp/hk-keeper.sh` dueling it caused a pidfile crash-loop (resolved). **Ping over comms before any daemon restart.**
- Deploy = `go install ./cmd/harmonik` then `pkill -f "harmonik --project"`; the supervisor auto-revives on the new binary. Don't manual-`tmux`-restart.
- Route work to your OWN `--queue <name>`, not shared `main`. Don't write tracked files into `main` while peer beads RUN (escape-detector; hk-77q8e softened it for gitignored/untracked).

## Open / next (13 open beads — small, honest backlog)
Operator decisions (not auto-dispatch): **hk-12ke1** (spec-drift audit — validates the 33 closes; the one residual risk), **hk-ymav1** (auto-tune concurrency), **hk-ulp7v** (1-line rename). Otherwise: `kerf next`.

## Files to open first
`HANDOFF-<your-role>.md` · `docs/bead-reassessment-2026-06-02/` · STATUS.md · `kerf next`.

# Translations
named-queues/flywheel/controlpoints = the 3 concurrent orchestrator threads (ID by LANE) · agent-comms bus = `harmonik comms` · hk-z0pmi = dispatch-deadlock fix · hk-12ke1 = spec-drift audit (validates the 33 closes) · "suite GREEN @6ecfb017" = scenario-test uplift done · supervisor = `/tmp/hk-daemon-supervise.sh` (`-c6`, single) · the-33-closes = flywheel's reassessment bead closes.

# No hard blockers. Daemon healthy, bus live, backlog reassessed. Next: operator decisions above or `kerf next`.
