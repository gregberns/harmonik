<!-- PP-TRIAL:v2 2026-06-09 main @556d648b — CAPTAIN handoff. Both session asks DELIVERED. Consolidated at a daemon-reliability inflection. You are the CAPTAIN: monitor + verify + unblock; delegate, don't implement. -->

<!-- ORCHESTRATION DIRECTIVES — DO NOT EDIT -->
1. ALWAYS have a next-step trigger — keep a comms watcher armed (filtered: `comms recv --agent captain --follow --json 2>/dev/null | grep '"event_id"'` to avoid gap-flood).
2. Root-cause discipline: trust CONCRETE ARTIFACTS (git ancestry, daemon log, live tmux panes) over claims & over events.jsonl-by-run_id (false negatives). VERIFY before acting — inspecting the live implementer pane this session prevented a destructive restart.
3. Throttle footprint; shared box. Knee ~4–5 wide; daemon at -c4.
4. Delegate daemon code to an OFF-DAEMON worktree sub-agent (the daemon can't run its own fix); captain reviews the diff + deploys. Captain never hand-edits daemon code (skill-doc typos excepted).
<!-- END DIRECTIVES -->

# BOTH SESSION ASKS DELIVERED
- **Track A** (gather→organize→deploy crews): 3 lanes crewed; **captain feature 15/15 COMPLETE**; codex **T12 (gating bead) landed** (556d648b); infra lane authored (work salvaged).
- **Track B** (4th crew = log-mining→document→investigate→prioritize→improve): **COMPLETE** — kerf work `logmine` (epic hk-mhmaw). 21 findings → `.kerf/works/logmine/04-research/findings.md`; 18 beads filed; **5 self-improvements landed** incl. the reusable `major-issue-fanout` skill; 13 daemon/CI beads routed to infra.

# STATE
- **main `@556d648b`** buildable. Daemon **PID 10488**, `--workflow-mode dot`, **-c4** (reverts to -c6 on supervisor revive → re-run `harmonik queue set-concurrency 4`).
- **Daemon fixes DEPLOYED + live this session** (the hardening pass): hk-togxq (no_progress=HEAD-advance), hk-jzpqo (crew-seed), **hk-az4fd** (activity-aware launch ceiling — worktree-progress defers to 90m, no more 12m false-kills of editing beads), **9bfc0d13** (queue-poison/drainCancelledQueue), **9b38f012** (gate retry-on-flaky + orphan-salvage).

# ⛔ #1 NEXT-TASK — the iter-N-strand PREVENTION fix (the linchpin)
The daemon **false-fails APPROVED single-iteration beads at iter-N** with "no-progress at iter-N: HEAD did not advance" (commit_gate-retry path, no review node). The work is committed + reviewer-APPROVED in iter-1, so iter-N has nothing to do → HEAD doesn't advance → false no_progress → run_failed → commit STRANDED. **orphan-salvage (9b38f012) does NOT rescue these** (T12 proved it: 556d648b stranded, had to be hand-salvaged). hk-togxq's "no_progress=HEAD-advance" fix is what introduced this.
**FIX NEEDED:** don't fire no_progress at iter-N when the bead already committed + the reviewer APPROVED (legitimately nothing to do) — complete/merge instead. Author OFF-DAEMON (delicate dot_cascade/review-loop code), review, deploy via restart, respawn crews. **Until this lands, every new bead strands + needs manual salvage — do NOT mass-dispatch.** (File this bead; relates to logmine F9 hk-m1wqp.)

# FILED DAEMON BUGS (the backlog the logmine crew + this session surfaced)
- **iter-N-strand prevention** (above) — FILE + do FIRST.
- `hk-mmlqt` (P1) — daemon restart KILLS all crews (they're tmux windows in the daemon's session). Respawn after every restart.
- `hk-vlkh4` (P2) — SIGTERM shutdown hangs ~45s+ (blocks supervisor revive; needed SIGKILL/long wait).
- `hk-vrnh3` (P1) — crew start writes empty queue stub → `queue_already_active (-32010)`.
- `hk-672di` (P1) — crews launch without `--dangerously-skip-permissions` → wedge on prompts.
- `hk-ue0u2` (P2) — activity-aware ceiling should also count pane-output (read-heavy beads that plan >12m before first edit still false-die; workaround = early WIP commit).
- `hk-tcenh` — pre-spawn wedge backstop; stranded commits 1956aa4f/03f92980 MAY conflict with hk-az4fd (review before salvaging; possibly subsumed by the ceiling fix).

# CREWS (Captain & Crew system works — validated this session)
- **liet** (logmine) + **chani** (captain feature) — lanes COMPLETE; idle/down.
- **duncan** (codex, epic hk-w4tmz) + **stilgar** (infra, epic hk-3js5m) — HELD; their remaining work (codex T13–T18; infra batch + the 12 logmine daemon beads) is BLOCKED on the iter-N prevention fix. Respawn + re-seed after the next daemon restart.
- **Crew launch is MANUAL-SEED** (hk-vrnh3/launch bug): `harmonik crew start <name> --queue <q> --mission .harmonik/crew/missions/<name>.md`, then `tmux send-keys -t harmonik-<hash>-default:hk-crew-<name> "You are crew <name> (NOT captain). Read <mission> and run /session-resume on it, then resume."` + `Enter`. Clear queue stubs (`mv .harmonik/queues/<q>.json /tmp/`). Crew missions: `.harmonik/crew/missions/{stilgar,duncan,chani,liet}.md` (schema: `specs/crew-handoff-schema.md`).

# SALVAGED + CLOSED this session (no work lost)
T9 hk-bpxci, T11 hk-tu48u, T13 hk-bejpi, hk-x342d (Linux build), hk-u6m4l (9bfc0d13), hk-8b35c (9b38f012), T12 hk-xhawy (556d648b), hk-5s6re. Pending: stilgar's hk-tcenh strands (review-then-salvage-or-close).

# DEPLOY/RESTART RUNBOOK (it bites — do it right)
ff main → `go install ./cmd/harmonik` → **TARGETED** `kill <daemon-pid>` (NOT broad pkill; isolated test daemons exist) → wait 45–90s for supervisor revive (backoff; "(no socket)" is expected, don't re-kill; if it hangs >2m, the process may need SIGKILL per hk-vlkh4) → `harmonik queue set-concurrency 4` → **respawn+reseed crews** (hk-mmlqt). Keep main working tree CLEAN of `.beads/issues.jsonl` churn or it warns/interferes with merges (`git checkout -- .beads/issues.jsonl`; DB is authoritative, daemon re-flushes).

# Files first
This → `.kerf/works/logmine/04-research/findings.md` (Track B output) → `.harmonik/crew/missions/*.md` → `docs/orchestrator-rules.md` → AGENT_INDEX.md.

# Translations
crew = persistent `claude --remote-control` session on a named queue · iter-N-strand = daemon false-fails an APPROVED single-iter bead at iter-N (HEAD didn't advance) + strands its commit · orphan-salvage = 9b38f012 mitigation (does NOT cover the commit_gate-retry strand) · daemon = `harmonik --project` dispatcher (PID 10488, -c4 dot, all session fixes live).
