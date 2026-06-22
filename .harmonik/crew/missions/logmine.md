---
schema_version: 1
crew_name: logmine
queue: logmine-q
epic_id: hk-mhmaw
goal: "logmine iter-11 harvest: mine the 06-18→now window (incl. the 06-20 135-commit burst); file/route new defect beads; digest daemon-lane to captain."
captain_name: captain
---

## ⚡ iter-11 harvest 2026-06-21 ~17:28Z (captain staffing into a free slot)

Your last run was **iter-10 on 2026-06-18**. The unmined window **06-18 → now** is
HIGH-VALUE: it spans the entire **2026-06-20 nine-initiative burst (~135 commits —
the biggest single-day landing in the project)**: keeper-redesign, captain-economy,
tmux-session-organization, doc-instruction-audit, easy-start native launchers,
rc-prefix. Much of that shipped as code but was **never exercised live** — exactly
the surface where mined defects are most likely.

Run the normal frozen playbook over that window. READ-ONLY; stay OFF daemon-package
files (paul owns them right now). Deliver a digest to captain; report DRAIN when done.

# Mission: daily logmine harvest (epic hk-mhmaw)

You are crew member **logmine** on queue **logmine-q**. Report to **captain**
(`harmonik comms send --from logmine --to captain --topic status -- ...`). Follow
`crew-launch` for boot.

## THE PLAYBOOK (authoritative)
Your procedure is **`.kerf/works/logmine/pipeline.md`** — the frozen, twice-validated
6-slice harvest method. READ IT FIRST and follow it. **Resume from the LATEST
high-water cursor footer** in `.kerf/works/logmine/` (iter-10's findings doc holds the
newest one; fall back to iter-7 `019eceae` only if no newer footer exists). Mirror
`br update hk-mhmaw --assignee logmine` on boot.

Summary of the playbook:
- **Boot:** `harmonik comms join`; `br update hk-mhmaw --assignee logmine`; arm a
  persistent `harmonik comms recv --follow --json` inbox; post a boot status to captain
  + the hk-mhmaw journal.
- **Wave 1 — Harvest+Document:** fan out ~6 READ-ONLY sub-agents (one per slice:
  failures, comms, daemon log, sub-agent transcripts, git churn, …) over the window
  since your cursor. Worktree-isolated agents lose gitignored bench writes, so do NOT
  worktree-isolate them. Classify each prior `Fxx` FIXED-confirm vs RECURRING.
- **Wave 2 — Investigate+Prioritize:** dedup vs `br list --status=open --limit 0`;
  ENRICH an existing bead with a comment rather than filing a duplicate; file genuine
  new findings as `codename:logmine` beads.
- **Wave 3 — Improve:** deliver a prioritized digest to captain; **route by lane** —
  daemon-lane defects (internal/daemon, dispatch reliability, false-positive strands)
  get DIGESTED to the captain by id+title (paul's lane owns them; do NOT dispatch them
  yourself); only SAFE, NON-TOKEN logmine-q items (process/skill/CI/docs) may dispatch
  on logmine-q (captain confirms).

## Bounds / scale-out caveats (set 2026-06-19)
- READ-ONLY harvest; the only writes are br beads/comments + the dated findings doc.
- **Stay OFF daemon-package files** — paul owns daemon internals; don't dispatch
  daemon-edit fixes to logmine-q.
- Internet is FLAKY (gh/WebFetch/package fetches may fail) — mine local `events.jsonl`.
- Every dispatchable code bead goes through DOT = the **sonnet triple-review** graph.
- Stay under **300k tokens**; you are keeper-watched — restart-now at a clean
  checkpoint on a WARN. Keep concurrent sub-agents ≤6 (the daemon shares the box).
- **Do NOT install the recurring trigger** (launchd plist / daily script) — gated on
  operator T0 sign-off, out of scope. You run the HARVEST only.
- CLI gotchas: `br create --labels` (plural) vs `br list --label` (singular);
  `br list --json` caps at 50 unless `--limit 0`; `comms log` is NDJSON (jq per-line).
- When the digest is delivered + safe fixes dispatched, report DRAIN and stand by for
  the self-terminate sequence below.

## Self-terminate on drain

Logmine runs ~once/day. After its daily run the crew must tear itself down rather than
lingering idle and holding a slot.

**When ALL of the following are true:**

1. logmine-q is genuinely drained — `br ready --limit 0 --label codename:logmine`
   returns empty AND no bead is in-flight on your queue. (`codename:logmine` is
   the label logmine-filed beads carry; `hk-mhmaw` is the epic ID used in comms
   and journal entries, but the bead label filter is `codename:logmine`.) Never
   treat an empty `br ready` as drained without the `--limit 0` check; bare
   `br ready` caps at 20 and can falsely appear empty.
2. No re-task is pending — your `comms recv --follow` inbox has no unprocessed
   `topic == assign` message assigning a new epic.
3. The final daily digest has been posted to captain on BOTH surfaces:
   - `harmonik comms send --from logmine --to captain --topic status -- "<digest>"`, AND
   - `br comments add hk-mhmaw "<drain summary>"`.

**Execute the self-terminate sequence:**

```bash
# Step 1 — announce departure on the bus
harmonik comms leave --name logmine

# Step 2 — stop the crew session
harmonik crew stop logmine
```

`harmonik crew stop logmine` signals the daemon to tear down the logmine pane and
keeper. The crew does NOT need to (and cannot) run `/quit` manually after this — the
stop command terminates the session.
