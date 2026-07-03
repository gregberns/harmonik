# logmine — Re-run Playbook (daily pipeline)

This is the repeatable procedure for crew **liet** to mine harmonik's operational logs. Output of
each run: a dated findings doc + a batch of `codename:logmine` beads + a captain digest. References:
`04-research/findings.md` (iter-1, 2026-06-09) and `04-research/findings-iter2.md` (iter-2, 2026-06-11).

> **Iter-2 refinements (validated):** use **6 slices, not 8** (worktree transcripts auto-clean → fold
> that slice into failures); every slice MUST classify each prior `Fxx` as FIXED-confirm/RECURRING
> (the fixed-delta is the payoff of recurrence); **pre-screen `br list --status=open` and enrich the
> existing bead with a comment** rather than filing duplicates; CLI gotchas: `br create --labels`
> (plural) vs `br list --label` (singular), `br list --json` caps at `--limit 50` (pass `--limit 0`).
> The path to a *recurring* (non-manual) pipeline is specified in `05-specs/recurring-pipeline-spec.md`.

## Boot (crew-launch)
1. `$HARMONIK_AGENT` must be `liet`. `harmonik comms join`; `br update hk-mhmaw --assignee liet`.
2. Arm a persistent comms-inbox monitor: `harmonik comms recv --follow --json`.
3. Post boot status to captain (`comms send --from liet --to captain --topic status`) AND the
   epic journal (`br comments add hk-mhmaw ...`).

## Wave 1 — Harvest + Document (fan-out, READ-ONLY, NOT worktree-isolated)
Fan out ~8 sub-agents (cap ~8; daemon shares the box at -c6), one per distinct slice. Each
RETURNS findings in its report (the durable copy — worktree-isolated agents lose gitignored
bench writes). Window = the two most recent dates (e.g. `2026-06-0[89]T`).

Proven slice split (1 agent each):
1. events.jsonl — run failures & wedges (`run_failed`/`run_stale`/`launch_stall_detected`/`no_progress_detected`); triangulate false-fails via `git log --all --grep "Refs: <bead>"`.
2. events.jsonl — reconciliation & ledger-dep (`reconciliation_*`, `queue_item_deferred_for_ledger_dep`, `queue_item_reconciled`); check deferrals whose blocker already closed.
3. events.jsonl — review loop (`reviewer_launched`/`reviewer_verdict`/`review_loop_cycle_complete`); detect merges with NO verdict (unreviewed-merge class).
4. events.jsonl — daemon lifecycle & keeper (`daemon_started`/`daemon_config`/`daemon_orphan_sweep_completed`/`session_keeper_*`/`operator_pause_status`/`queue_*`).
5. comms bus — `harmonik comms log --since 24h [--json]`; coordination friction, mis-routes, identity, restart collisions.
6. daemon stdout — `/tmp/hk-daemon.log` + `/tmp/hk-daemon-supervise.sh`; panics/backoff/socket/pidfile/ENOSPC.
7. sub-agent transcripts — `~/.claude/projects/-Users-gb-github-harmonik--harmonik-worktrees-*/...jsonl` (NOT `/private/tmp/.../tasks/`). DON'T read whole transcripts; grep + `jq` filter `is_error==true`; same-`Refs:` re-dispatch counts. Raw keyword greps are polluted by spec-doc prose — trust `is_error` + path-miss signals.
8. git churn + qa-scratch — `git log --since="1 day ago"` reverts/fixup-chains/high-churn files; `docs/qa-scratch/`.

> **Slice-6 pollution caveat (iter-8):** the daemon-stdout/git-churn slice tends to grep the WHOLE
> `events.jsonl` (no daemon log file exists; output goes to events.jsonl), so its failure-class counts
> and event cites drift OUT of the frozen window (iter-8 slice-6 cited a 2026-06-10 scenario-wedge,
> non-ff races, and merge_fmt_failed that were ZERO in-window). ALWAYS re-verify any slice-6 failure
> claim against `/tmp/logmine-window.jsonl` before filing. Live `df`/`du`/`worktree list` measurements
> are window-independent and fine as-is.

Then: consolidate + **deduplicate** across slices into `04-research/findings.md`. Anchor every
finding to a durable artifact (event_id / file:line / sha). Mark **[T]** when ≥2 slices triangulate.
Add a prioritized register table. P1 minimum for any friction/recurring-wedge.

**Cross-run cursor (REQUIRED, recurring-pipeline) — window by EVENT_ID / LINE POSITION, never by a
timestamp string (F41, iter-4):** the window start = the prior findings doc's high-water mark. Read it
from the latest `findings-iterN.md` footer line `> high-water: <event_id>`. **RESOLVE that event_id to
its line number and slice forward** — events.jsonl is append-ordered, so append order IS chronological
order:

```bash
L=$(grep -n "<high-water-event_id>" .harmonik/events/events.jsonl | head -1 | cut -d: -f1)
sed -n "$((L+1)),\$p" .harmonik/events/events.jsonl > /tmp/logmine-window.jsonl   # whole file = the window
```

Hand every slice that **frozen snapshot** and tell it the WHOLE file is the window (no per-slice filter).
**Do NOT filter by `.timestamp_wall > "<Z-string>"`.** events.jsonl stamps `timestamp_wall` in MIXED
formats — `session_keeper_*` events use UTC-`Z`, all daemon-core events use local offset (`-07:00`) — so
string comparison sorts by local-clock digits and silently drops a UTC-offset-sized band (in iter-4 that
was ~7h / ~90% of the window: 1 of 30 runs, 1 of 31 reviews, 2 of 9 reconciliations seen). **First run**
(no footer yet) → take the last 24h by resolving the earliest line whose event falls inside 24h, never a
full-log scan by timestamp string. Each run MUST end its findings doc with its own `> high-water:
<event_id>` footer (the last event_id it processed), so the next daily run resumes exactly where this one
stopped. (Root-cause of the mixed-format serialization is filed as the **F41b** harmonik defect,
`crew:stilgar` — until it lands, line-anchoring is the only safe window.)

## Wave 2 — Investigate + Prioritize → file beads
For each distinct, actionable finding, `br create --labels codename:logmine,crew:<lane> --actor liet`.
Do NOT `--parent` under hk-mhmaw (open-epic children get ledger-dep-gated). Lane rule:
- Fix edits `internal/daemon/**` or comms-bus code → `crew:stilgar` (digest to captain, do NOT dispatch from liet-q — avoids colliding with the daemon lane).
- codex/harness code → `crew:duncan`.
- docs / skills / launch-context / orchestrator-rules / CI-config → `crew:liet` (dispatchable).
Record the finding→bead map in `findings.md`. `br create` auto-flushes to JSONL.

## Wave 3 — Improve
- Dispatch `crew:liet` beads to **liet-q** (NEVER main): `harmonik queue submit --queue liet-q --beads <ids>` (dry-run first). Hold beads whose real fix crosses into the daemon lane.
- Arm `harmonik subscribe --types run_completed,run_failed,run_stale,heartbeat --heartbeat 300s --json`.
  NB: the subscribe monitor sees ALL daemon runs — filter mentally to YOUR queue's run_ids.
- For `crew:stilgar`/`crew:duncan` beads: digest to captain (`--topic findings`), don't fix yourself.

## Cadence & hygiene
- Status to captain `--topic status` + `br comments add hk-mhmaw` on each wave boundary, on bead-close, and ≤10-min while active.
- Do NOT `br close` (daemon owns terminal transitions). `br create`/`br comments`/metadata-only `br update` are fine.
- Never `cd` into a worktree; operate from repo root with `git -C`.
- Known lag: idle daemon doesn't wake on submit (hk-24xn1) — beads sit pending until the next workloop tick.

## Cross-run dedup
Before filing, check prior findings docs and open `codename:logmine` beads so a recurring pattern
updates the existing bead instead of spawning a duplicate. `br list --json | grep logmine`.
</content>
