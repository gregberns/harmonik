<!-- PP-TRIAL:v2 2026-06-09 main @a0987bb6 — CAPTAIN handoff. Both session asks DELIVERED. Daemon hardened; consolidated at a reviewer-stall reliability layer. Crews stood down. You are the CAPTAIN: monitor + verify + unblock; delegate, don't implement. -->

<!-- ORCHESTRATION DIRECTIVES — DO NOT EDIT -->
1. ALWAYS have a next-step trigger — comms watcher armed (filtered: `comms recv --agent captain --follow --json 2>/dev/null | grep '"event_id"'` — avoids gap-flood).
2. Root-cause discipline: trust CONCRETE ARTIFACTS (git ancestry, daemon log, live tmux PANE) over claims & over events.jsonl-by-run_id. `run_stale`/`launch_initiated`/`no-agent_ready` are FALSE-NEGATIVES for in-progress work — verify the pane (cmd=claude + live token counter) before EVER recommending a kill. (6 false "spawn-wedge" alarms this session; all were working beads.)
3. Throttle footprint; shared box. Knee ~4–5 wide; daemon at -c4. Watch disk (go-build cache hit 15G → ENOSPC risk; `go clean -cache` frees it).
4. Delegate daemon code to an OFF-DAEMON worktree sub-agent (the daemon can't run its own fix); captain reviews the diff + deploys. Never hand-edit daemon code (skill-doc typos excepted).
<!-- END DIRECTIVES -->

# BOTH SESSION ASKS DELIVERED
- **Track A** (gather→organize→deploy crews): 4-crew fleet deployed; **captain feature 15/15 COMPLETE**; codex **T12 landed** (556d648b). Codex T13–T18 + infra batch authored but strand-blocked (see #1).
- **Track B** (4th crew logmine = mine logs→document→investigate→prioritize→improve): **COMPLETE** — kerf work `logmine` (epic hk-mhmaw). 21 findings → `.kerf/works/logmine/04-research/findings.md`; 18 beads filed; **5 fixes landed** incl. the reusable `major-issue-fanout` skill; daemon defects routed to infra.

# STATE
- **main `@a0987bb6`** buildable. Daemon **PID 98748**, `--workflow-mode dot`, **-c4** (reverts to -c6 on supervisor revive → re-run `harmonik queue set-concurrency 4`).
- **Daemon fixes DEPLOYED + live** (the session's hardening): hk-togxq (no_progress=HEAD-advance), hk-jzpqo (crew-seed), hk-az4fd (activity-aware launch ceiling), 9bfc0d13 (queue-poison), 9b38f012 (gate retry-on-flaky + orphan-salvage), **hk-8ps7q (a0987bb6 — iter-N APPROVE-and-done completes instead of strand)**.
- All 4 crews STOOD DOWN (lanes complete or consolidated). daemon idle.

# ⛔ #1 NEXT-TASK — hk-bqf1q: reviewer-NO-verdict strand (the dominant blocker)
On HEAVY daemon-code beads the reviewer node times out / never emits `reviewer_verdict` → the cascade advances to iter-2 → no_progress fires (HEAD unchanged since the impl commit) → run_failed → the valid commit STRANDS un-reviewed. hk-8ps7q only completes when `priorVerdict==APPROVE`, so a MISSING verdict still strands. **Systematic on heavy beads** (confirmed 3/3: hk-qxfj0, T13, hk-tcenh). FIX: handle reviewer-no-verdict — re-launch the reviewer, or treat committed-but-unreviewed as needs-review (not no_progress-fail). Author OFF-DAEMON, deploy via restart (respawn crews after — hk-mmlqt). Until fixed, heavy beads don't merge.

# 🔖 SALVAGE CANDIDATES — valid commits preserved on run/ branches, UN-REVIEWED (next pass: review → merge; do NOT delete these branches)
- `18f6012c` hk-tcenh — "defense-in-depth force-cancel for pre-launch stalls" → run/019eaf09-c026 (real spawn-wedge-fix; supersedes the older 1956aa4f/03f92980).
- `f2c02e20` hk-o90sl (T13) — "gate pasteInjectQuitOnCommit on Completion policy" → run/019eaf04-ce91.
- `c1aebb50` hk-iv748 (T14, skeleton) → its run/ branch.
- `66f1cb86` hk-qxfj0 — "queue dry-run shows resolved harness per item" → run/019eaf08-ee2a.
All four beads left OPEN. (Worktrees may be gone; the run/ branch refs preserve the commits.)

# NO-COMMIT beads (need the worktree-sub-agent authoring path, NOT the daemon)
- `hk-6hzci` (restore 21 E2E tests) + `hk-1o0cc` (flaky -short tests) — test-heavy reproduce-before-fix beads boot real daemons/run tests and exceed the implementer commit budget before a fix lands → no_commit (nothing to salvage). Author via worktree sub-agent + cherry-pick next pass.

# FILED DAEMON-RELIABILITY BACKLOG (the focused next pass; `br list --label crew:stilgar --status open`)
hk-bqf1q (P1, reviewer-no-verdict — DO FIRST) · hk-mmlqt (P1, daemon restart kills all crews — they're tmux windows in the daemon session) · hk-vlkh4 (P2, SIGTERM shutdown hangs ~45s → needs SIGKILL) · hk-vrnh3 (P1, crew-start writes empty queue stub → -32010) · hk-672di (P1, crews launch w/o --dangerously-skip-permissions → wedge on prompts) · hk-ue0u2 (P2, ceiling should count pane-output for read-heavy beads; workaround = early WIP commit) · hk-i09r9 (P1, queue pause/resume flag-leak — use POSITIONAL `queue pause <name>`, not `--queue`) · + logmine F1–F12 (hk-m3ydd/ly0hg/hggxx/qkahq/c73fs/z0f02/mptxw/m1wqp/cizvu/5xuvc/kqnay). Plus a crew-liveness-tooling gap (crews repeatedly mis-read events.jsonl as wedges).

# SALVAGED + CLOSED this session (no work lost)
T9 hk-bpxci, T11 hk-tu48u, T13-captain hk-bejpi, hk-x342d, hk-u6m4l (9bfc0d13), hk-8b35c (9b38f012), T12 hk-xhawy (556d648b), hk-5s6re, hk-az4fd, hk-8ps7q.

# RESUME PLAN (next pass)
1. Fix hk-bqf1q off-daemon → deploy (restart per runbook below) → respawn+reseed crews.
2. Review+merge the 4 SALVAGE CANDIDATES (or just re-dispatch the beads — they'll complete clean once hk-bqf1q lands).
3. Work the rest of the daemon-reliability backlog; then release duncan/stilgar to finish their lanes.

# CREWS — Captain & Crew system VALIDATED (crew start/stop/list, missions, comms, progress feed all work)
Missions: `.harmonik/crew/missions/{stilgar,duncan,chani,liet}.md` (schema `specs/crew-handoff-schema.md`). **Crew launch is MANUAL-SEED** (launch bug): `harmonik crew start <name> --queue <q> --mission <path>`, then `tmux send-keys -t harmonik-<hash>-default:hk-crew-<name> "You are crew <name> (NOT captain). Read <path> and /session-resume on it, then resume."` + `Enter`; clear queue stubs (`mv .harmonik/queues/<q>.json /tmp/`).

# DEPLOY/RESTART RUNBOOK (it bites)
ff main → `go install ./cmd/harmonik` → **TARGETED** `kill <daemon-pid>` (NOT broad pkill — isolated test daemons exist) → if it hasn't exited in ~60s `kill -9` (hk-vlkh4) → wait for supervisor revive (~15-90s; "(no socket)" is expected, don't pile on kills) → `harmonik queue set-concurrency 4` → **respawn+reseed crews** (hk-mmlqt kills them on restart). Keep main tree clean of `.beads/issues.jsonl` churn (`git checkout -- .beads/issues.jsonl`; DB is authoritative).

# Files first
This → `.kerf/works/logmine/04-research/findings.md` (Track B) → `.harmonik/crew/missions/*.md` → `docs/orchestrator-rules.md` → AGENT_INDEX.md.

# Translations
crew = persistent `claude --remote-control` session on a named queue · iter-N-strand = daemon false-fails a committed bead at iter-N (HEAD didn't advance) → strands commit; hk-8ps7q fixed the APPROVE-and-done case, hk-bqf1q (reviewer-no-verdict) is the remaining systematic one · salvage candidate = valid commit on a run/ branch, un-reviewed, preserved for next-pass review+merge · daemon = `harmonik --project` dispatcher (PID 98748, -c4 dot, all session fixes live).
