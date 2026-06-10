# Captain Retrospective — Friction Analysis (2026-06-10)

> Analyst: READ-ONLY retrospective sub-agent. Sources: HANDOFF.md, comms log (24h),
> captain/crew memory files, SKILL.md + STARTUP.md, docs/orchestrator-rules.md,
> docs/known-workarounds.md. No live-state probing, no repo mutations.

---

## Pattern 1 — Deploy non-ff race: out-of-band push wedges the daemon's shared main ref

**Root cause:** Captain cherry-picks banked commits to a TEMP worktree and pushes to
`origin/main`. The temp worktree's HEAD advances, but `refs/heads/main` in the MAIN
checkout (which the daemon merges onto) stays stale. Every subsequent daemon merge
push non-ff-rejects. One undocumented step (`git -C <repo> merge --ff-only
origin/main`) was missing from the deploy procedure for ~3 deploy cycles before it was
discovered and hard-coded. Before the fix was known, collateral bead failures (hk-kqnay,
hk-yag0s) cost ~15 min of diagnosis per incident. The daemon also has no non-ff
rebase-retry (it just fails the bead).

**Process fix:** the ff-after-push step is now encoded in HANDOFF.md §Deploy and the
captain skill. Keep it; enforce it via a checklist comment in every deploy announcement
on the bus.

**Harmonik embed:** the daemon should fetch+rebase-retry on a non-ff push reject rather
than failing the bead outright. This eliminates the failure entirely regardless of
captain discipline.

**Tag:** HARMONIK-EMBED  
**Proposed bead:** `hk-svieq` — filed, LANDED (`b4858a3c`). Close-out: bead is in
progress; confirm it covers the retry-on-non-ff case fully.

---

## Pattern 2 — Staged-debris confusion: uncommitted index dirt left by merge-overlap

**Root cause:** Two beads (hk-9ztth and hk-fkpb7) edited the same `internal/queue`
files in sequential daemon merges. The merge sequence left staged-but-uncommitted
deletions of hk-fkpb7's `cancel --queue` code in the MAIN working tree (not a worktree).
The captain noticed it, spent one full investigator-agent cycle determining it was
latent-harmless, deferred clearing it across two session restarts, and the staged debris
became a standing WARNING in HANDOFF.md for >2h. The escape-detector's focus on
_commits_ (not staged index) meant it wasn't tripping runs — but any naive `git commit`
in the main tree would have regressed the feature.

**Process fix:** after daemon merges of beads that share a package, the captain should
run a quick `git diff --staged` scan of the main tree and clear debris immediately (via
`git restore --staged` + `git checkout --` on the affected files) while the provenance
is fresh. Don't carry it across restarts.

**Harmonik embed:** the daemon could emit a `merge_index_dirty` event after completing
a bead merge when the main tree's index has unexpected staged changes. This would let
the captain detect the condition via the subscribe feed instead of discovering it from
`git status` observation. Alternatively, the merge path could auto-restore the index to
HEAD after each ff-merge (there is no reason for staged changes to survive a successful
worktree merge).

**Tag:** HARMONIK-EMBED  
**Proposed bead:** `hk-staged-debris-detect` — "emit merge_index_dirty event (or
auto-restore staged index) after daemon bead merge when main tree index is dirty"

---

## Pattern 3 — Crew-bead pre-assign wedge: `br create --assignee <crew>` poisons dispatch

**Root cause:** A crew (chani) created its child release-pipeline beads with
`--assignee chani`. The daemon's `br claim` call silently rejects an already-assigned
bead → `max_attempts_exceeded`, `run_id=null`, group_failure on a fresh queue. No
`run_started`/`launch_initiated` ever fires — impossible to distinguish from
queue-poison without a source-level investigation. Cost: ~5 failed dispatches +
investigator sub-agent + the captain broadcasting a correction rule to all crews.

**Process fix:** "Never pre-assign a dispatchable bead" is now in captain SKILL.md §8
and the crew-launch skill. The directive is load-bearing. Recovery is `br update
<id> --assignee ""`.

**Harmonik embed:** `br claim` should treat "already assigned to the SAME actor" as an
idempotent success (not reject). More broadly, the daemon could detect this failure
class early (on submit or dry-run) and return a helpful error rather than silently
exhausting attempts.

**Tag:** HARMONIK-EMBED  
**Proposed bead:** `hk-amed0` — already filed (P1). Confirm it covers both
"already-assigned-to-daemon-actor" (idempotent claim) AND a helpful dry-run-time warning.

---

## Pattern 4 — DOT-mode reviewer-stall: reviewer submits never arrive; bead strands at iter-2

**Root cause:** Under `--workflow-mode dot`, the daemon's reviewer session spawned,
worked, but its verdict submission silently stalled. Dozens of commits accumulated on
`run/` branches un-reviewed across a ~4h window. Three successive "fixes" (hk-bqf1q
no-verdict retry, hk-76n5g seed-race fix, a seed-submit race patch) were each deployed
and validated on light beads — yet the reviewer-stall survived on heavy daemon-code
beads. Resolution only came when crew chani hypothesized "DOT-mode-specific" and the
captain ran a review-loop canary (hk-c73fs), confirmed it worked, and switched the
daemon to `--workflow-mode review-loop`. Total cost: ~6h of captain context, multiple
bead strandings, and a near-invocation of the 10–15-agent major-issue-fanout. The root
cause was never fully isolated at the code level — the mode-switch was the fix.

**Process fix:** DOT-mode reviewer-submit reliability is a prerequisite for using DOT
as the default workflow. Until the reviewer-submit path is validated on heavy beads
under concurrency, keep `--workflow-mode review-loop` as the default (already done via
supervisor pin). Validate DOT with the concurrent-heavy-bead smoke (not a trivial
1-bead smoke) before re-pinning to dot.

**Harmonik embed:** (a) add a `reviewer_submitted` typed event (distinct from
`reviewer_launched` and `reviewer_verdict`) so the captain can detect "reviewer is
working" vs "reviewer spawned but went silent" without pane-inspection; (b) add a
`--workflow-mode` flag to `queue status` output so the captain can confirm the daemon's
live mode without a restart; (c) gate `--workflow-mode dot` on a concurrent-heavy-bead
smoke CI check before it can become the default.

**Tag:** HARMONIK-EMBED  
**Proposed bead:** `hk-dot-reviewer-telemetry` — "emit reviewer_submitted event on
verdict file write; surface workflow_mode in queue status output"

---

## Pattern 5 — Seed-not-submitted hang (hk-76n5g): iter-N implementer/crew boot seed pasted but Enter not sent

**Root cause:** After pastes for crew-boot and review-loop iter-N seeds, the daemon
pasted the text but did NOT press Enter. Sessions sat with the mission or re-task seed
visible but unsubmitted. Effect: a crew boot appears to "launch" (exit-0 from
`crew start`) but never calls `comms join`; an iter-2 implementer holds a daemon slot
for ~30–48 min until the no-progress detector fires. The captain spent significant time
manually sending Enter to stuck panes (`tmux send-keys Enter`) and filed the issue as
a P1 bead (hk-76n5g). Multiple crew reboots and bead re-dispatches were caused by this.

**Process fix:** the captain now manually Enter-unsticks crew-start boots (the hard-wedge
case — no no-progress safety net) but avoids unsticking LOOPING beads (which have a
safety net and Enter just prolongs the loop). Documented in HANDOFF.md §Interim
mitigations.

**Harmonik embed:** the paste-inject path should unconditionally send Enter after
pasting ANY seed (crew-boot OR review-loop iter-N). This is the definition of the bug
and its fix; hk-76n5g is the filed bead. Confirm it covers BOTH crew-boot AND every
review-loop iter.

**Tag:** HARMONIK-EMBED  
**Proposed bead:** `hk-76n5g` — filed P1, LANDED (`1ed53a6d`). Verify the fix covers
the crew-boot path in addition to the review-loop iter-N path.

---

## Pattern 6 — Context exhaustion → manual crew restart toil: no automated context-clear/reseed

**Root cause:** Crews (liet at ~200k tokens, the captain itself at ~76%) have no
automated context-management. When a crew hits high context it stops accepting
keystrokes and must be manually restarted via `harmonik crew stop` + `crew start` with
a fresh mission file. The captain spent visible effort on 3 crew restarts + its own
handoff-restart cycle this session. The session-keeper (hk-ekap1, Phase 2) is
DESIGNED and REVIEWED but NOT deployed — it would automate the `/session-handoff →
/clear → /session-resume` cycle. Without it, the captain must monitor crew context
levels, recognize the 200k wedge symptom, kill and reseed manually.

**Process fix:** deploy session-keeper (hk-ekap1) as a crew-context manager. The epic
is closed, all code is landed except one minor fix (hk-2ojne, banked). The only
remaining step is the operator's live `claude --remote-control` round-trip validation
and the deploy greenlight. This is the single highest-ROI undeployed improvement.

**Harmonik embed:** `harmonik crew start` (or a new `harmonik keeper attach <crew>`)
should accept a `--context-ceiling` parameter that auto-injects a handoff+clear+resume
cycle at the threshold. The standalone `harmonik keeper` command already exists; wiring
it to crew sessions is the remaining gap.

**Tag:** HARMONIK-EMBED  
**Proposed bead:** `hk-ekap1-deploy` — "deploy session-keeper for crew context
auto-clear/reseed; operator round-trip validation + greenlight" (unblocks all three
current context-restart incidents per session)

---

## Pattern 7 — events.jsonl false-negative causes erroneous wedge alarms from crews

**Root cause:** Multiple crews (stilgar, duncan) repeatedly raised "wedge/slot-leak"
alarms based on `run_stale` and `launch_initiated` events read from `events.jsonl`
filtered by `run_id`. The captain repeatedly had to verify by pane-inspection that
beads were actively running (cmd=claude, live token counter). A "run_stale at
launch_initiated" is NOT a wedge for a progressing bead; it is a false-negative from
the events path. The captain broadcast a "STANDING RULE: no more wedge escalations from
events.jsonl" message and had to re-explain pane-truth vs event-truth at least 5 times
this session. The cost was crew throughput (killed work, stalled dispatches) and captain
context.

**Process fix:** the crew-launch skill already includes a dedupe rule for `run_stale`
false-positives. Harden it: add an explicit "wedge classification" decision tree —
(a) `run_stale` alone = not a wedge signal; (b) wedge = pane shows NO claude process
OR token counter frozen across two 60s checks; (c) events.jsonl grep-by-run_id has
known false-negatives — always verify via pane first.

**Harmonik embed:** the daemon should emit a `bead_progressing` event on a periodic
basis (heartbeat-like, e.g. every 5 min) for any run that has an active tmux pane with
a live claude process. This gives crews a positive "still working" signal on the event
bus, eliminating the false-negative gap. The current model only emits sparse typed
events (launch, commit, verdict, etc.); a gap between them looks identical to a wedge.

**Tag:** HARMONIK-EMBED  
**Proposed bead:** `hk-bead-progress-heartbeat` — "daemon emits periodic
bead_progressing event (every 5 min) for runs with an active implementer/reviewer pane;
crews can distinguish slow-but-live from wedged without pane inspection"

---

## Pattern 8 — Attribution round-trips: "whose bead is this?" answered by asking, not by br show

**Root cause:** On `run_failed` and `run_stale` events, crews (and the captain itself,
early-session) sometimes asked "which crew owns this bead?" over comms or surfaced it
ambiguously instead of reading `br show <bead_id> --format json → parent_id →
br show <epic_id> → assignee`. This attribution-by-ask is documented as having
caused ≥4 ownership round-trips in prior sessions (logmine F13). The captain skill now
encodes the "attribution-first rule" (Gap 1) but compliance requires conscious effort
each turn — it is not structural.

**Process fix:** attribution-first is now load-bearing in captain SKILL.md §9 (Gap 1
reinforcement). Apply it consistently. A crew that dispatches a `run_failed` to the
captain should include the bead's parent epic assignee in its message (crews read
`br show <bead> --format json | jq '.parent_id'` and then `br show <epic> | jq
'.assignee'` before sending).

**Harmonik embed:** `harmonik subscribe --json` run events should include the owning
crew name in the event payload (read from the epic's `--assignee` field at emit time).
If the daemon already knows which epic a bead belongs to, it can denormalize `assignee`
into `run_failed`, `run_stale`, and `run_completed` events — eliminating every
attribution round-trip structurally.

**Tag:** HARMONIK-EMBED  
**Proposed bead:** `hk-run-event-attribution` — "daemon denormalizes epic.assignee
into run_failed/run_stale/run_completed/run_started event payloads, so crews and the
captain never need a round-trip br show to attribute a run event"

---

## Summary table

| # | Pattern | Tag | Harmonik bead (new or existing) |
|---|---|---|---|
| 1 | Deploy non-ff race wedges daemon's local main | HARMONIK-EMBED | hk-svieq (landed; verify retry coverage) |
| 2 | Staged-debris left by merge-overlap, discovered late | HARMONIK-EMBED | hk-staged-debris-detect (new) |
| 3 | Pre-assigned bead poisons dispatch silently | HARMONIK-EMBED | hk-amed0 (filed P1) |
| 4 | DOT-mode reviewer-stall; stealthy on heavy beads | HARMONIK-EMBED | hk-dot-reviewer-telemetry (new) |
| 5 | Seed-not-submitted Enter hang on crew-boot + iter-N | HARMONIK-EMBED | hk-76n5g (landed; verify crew-boot path) |
| 6 | Manual crew context-restart toil; no auto-clear | HARMONIK-EMBED | hk-ekap1-deploy (new bead, code complete) |
| 7 | events.jsonl false-negatives trigger erroneous wedge alarms | HARMONIK-EMBED + PROCESS | hk-bead-progress-heartbeat (new) |
| 8 | Attribution round-trips on run events | HARMONIK-EMBED + PROCESS | hk-run-event-attribution (new) |

All 8 patterns are HARMONIK-EMBED candidates; #7 and #8 also have a PROCESS component
that can be improved independently.
