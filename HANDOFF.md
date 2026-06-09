<!-- PP-TRIAL:v2 2026-06-09 main @74045214 — canonical CAPTAIN handoff. Fleet UP (4 crews). You are the CAPTAIN: monitor + verify + unblock; delegate, don't implement. -->

<!-- ORCHESTRATION DIRECTIVES — DO NOT EDIT -->
1. ALWAYS have a next-step trigger — keep the comms watcher (Monitor: `comms recv --agent captain --follow --json`) armed; re-arm if it drops.
2. Root-cause discipline: trust CONCRETE ARTIFACTS (git ancestry, events.jsonl, file:line) over claims. VERIFY crew reports before acting — lane discovery agents misread "landed vs banked" against a stale local checkout this session.
3. Throttle your own footprint — shared box; daemon at -c4 + 4 crews + liet's sub-agent fan-out. Knee ~4–5 wide.
4. Delegate daemon/code work to crews; captain coordinates + verifies, never edits daemon code (skill-doc typo fixes are the only inline exception).
<!-- END DIRECTIVES -->

# STATE (2026-06-09 ~22:00Z)
- **main `@74045214`**, buildable. P0 daemon fix DEPLOYED (hk-togxq no_progress + hk-jzpqo crew-seed); daemon healthy `--workflow-mode dot`, **live cap dialed to -c4** (`harmonik queue set-concurrency 4`; reverts to -c6 on supervisor revive). Auto-merge restored; **bypass-SOP retired**.
- Concurrent-spawn wedges hk-jgxqc + hk-4l7zs are FIXED+on-main (the old `-c1` serial pin is SUPERSEDED). Only hk-tcenh (intermittent edge wedge) remains, in stilgar's lane.

# FLEET — 4 crews live (monitor via comms; verify reports)
| crew | queue | epic | lane | progress |
|---|---|---|---|---|
| **stilgar** | stilgar-q | hk-3js5m | daemon/infra stability | dispatching hk-8b35c/hk-tcenh/hk-x342d/hk-6hzci + P2s; owns hk-vrnh3 + hk-672di |
| **duncan** | duncan-q2 (duncan-q was stub-wedged) | hk-w4tmz | codex-harness | T12 hk-xhawy dispatched; T13–T18 chain next |
| **chani** | chani-q | hk-4adgn | captain feature | ✅ T10+T15 DONE (closed); T14 hk-zi4ej (E2E scenario) dispatched to a worktree sub-agent |
| **liet** | liet-q | hk-mhmaw | **logmine** (Track B) | ✅ Wave 1 done — 21 findings in `.kerf/works/logmine/04-research/findings.md`; Wave 2 (investigate+prioritize+file `codename:logmine` beads) next; Wave 3 = fixes via liet-q |

# GOTCHAS / WORKAROUNDS (this session — load-bearing)
- **Crew launch needs MANUAL SEED.** `harmonik crew start <name> --queue <q> --mission <file>` creates the session but does NOT paste/submit the mission (hk-jzpqo notwithstanding). Seed by hand: `tmux send-keys -t harmonik-<hash>-default:hk-crew-<name> "You are crew <name> (NOT captain). Read .harmonik/crew/missions/<name>.md and run /session-resume on it, then begin your operating loop."` then `tmux send-keys ... Enter`. Verify via `comms who`.
- **Crew queues poison on start (hk-vrnh3, P1).** crew start writes an empty stub `{"queue_id":"","groups":null}` → every `queue submit` returns `queue_already_active (-32010)`, and `queue cancel` ignores it. FIX (captain ops): `mv .harmonik/queues/<q>.json /tmp/<q>.bak` then the crew can submit fresh. stilgar owns the code fix.
- **Crews wedge on permission prompts (hk-672di, P1).** crew sessions launch WITHOUT `--dangerously-skip-permissions`, so a non-allowlisted command (e.g. `python3 -u -c` monitor) stalls the crew at a Yes/No prompt. Unstick: `tmux send-keys -t <target> 2` (yes, don't-ask-again) + `Enter`. Real fix = pass the flag in crew start.
- **`br comments add` is POSITIONAL** (`br comments add <id> "text"`), NOT `--body` (fixed in crew-launch skill, hk-0r4m7).
- **Daemon restart: TARGETED kill only.** chani's T14 boots isolated temp-dir test daemons — use the live PID/pidfile, NOT broad `pkill -f "harmonik --project"`, or you flake them.

# CAPTAIN POSTURE NOW
- Watch the comms watcher. On crew `--topic error`/blocker → clear the operational blocker (ops) and/or route the code fix to a crew; keep crews moving. On manual/smoke beads done → captain `br close` after verifying (daemon only closes daemon-merged work). Pass `--from captain` on every send.
- Pending hygiene: ~23 merged `worktree-agent-*` branches deletable (+ now-flushed cp-bank-t13 / a10a70661fcf97789 / a7ccad559138189d4). Correct stale memory: supervisor pins `--workflow-mode dot` (NOT review-loop).

# Files first
This → `.harmonik/crew/missions/{stilgar,duncan,chani,liet}.md` → `.kerf/works/logmine/` (Track B) → `docs/orchestrator-rules.md` → AGENT_INDEX.md.

# Translations
crew = persistent `claude --remote-control` session on a named queue · stilgar=infra, duncan=codex-harness, chani=captain-feature, liet=logmine · logmine = daily log-mining→document→investigate→prioritize→improve pipeline (kerf `logmine`, epic hk-mhmaw) · hk-vrnh3=crew-queue stub bug · hk-672di=crew skip-permissions bug · daemon = persistent `harmonik --project` dispatcher (healthy, -c4 dot).
