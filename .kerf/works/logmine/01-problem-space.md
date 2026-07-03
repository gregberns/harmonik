# logmine — Problem Space

## Summary

Harmonik produces a large, rich operational trail every day — daemon events, the
comms bus, the daemon/supervisor log, ephemeral sub-agent transcripts, and QA
scratch notes. That trail is where every wedge, false-fail, mis-diagnosis, and
hours-long detour leaves fingerprints. Today nothing systematically mines it: a
pattern is only noticed when a human happens to trip over it twice. This work
stands up a **dedicated crew that runs a recurring log-mining → document →
investigate → prioritize → improve pipeline** over the last day's logs, turning
the operational trail into documented findings and then into landed fixes.

## Goals

1. **Harvest + Document (Wave 1).** Fan out many read-only sub-agents across every
   log source for the last ~24h. Each surfaces recurring patterns, anomalies, and
   issues (wedges, repeated failure classes, false-fails, slow paths, mis-routes,
   tooling friction). Consolidate into a **durable, deduplicated findings document**
   committed to this kerf work. Documentation comes FIRST — before any fixing.
2. **Investigate + Prioritize (Wave 2).** A second wave of sub-agents each takes a
   documented finding, establishes root cause from durable artifacts (file:line,
   events.jsonl entries — NOT ephemeral pane scrapes), assesses blast radius/
   frequency, and assigns a priority. Output: a prioritized issue register, each
   item filed as a bead labeled `codename:logmine`.
3. **Improve (Wave 3).** The prioritized beads are dispatched (daemon queue, normal
   review-loop) to actually land fixes/improvements.

## Method — three waves, heavy fan-out

- Wave 1 sub-agents are **read-only** and **must NOT be worktree-isolated** (they
  write findings the crew collects via their return reports; worktree-isolated
  agents lose gitignored bench files — see [[reference_worktree_agent_bench_artifact_loss]]).
- Each sub-agent returns its findings IN ITS REPORT (the durable copy). The crew
  writes the consolidated doc.
- Dedup aggressively — many sub-agents will independently surface the same wedge.
- Anchor every claim to a durable artifact. Triangulate ≥2 independent signals
  before declaring a root cause (false negatives from hand-grepping events.jsonl
  by run_id are a known trap).

## Log sources (last ~24h)

- `.harmonik/events/events.jsonl` — typed daemon events (~1900 lines today, ~1250 yesterday).
- `harmonik comms log --since 24h` — the inter-agent bus (~485 lines).
- `/tmp/hk-daemon.log` — daemon/supervisor stdout (~270K).
- `/tmp/hk-daemon-supervise.sh` behavior + restart history.
- Sub-agent transcripts under `/private/tmp/claude-*/-Users-gb-github-harmonik/*/tasks/`.
- `docs/qa-scratch/` notes.
- `git log --since="1 day ago"` — what actually landed vs. what churned.

## Non-goals

- NOT a one-shot triage — this is meant to become a **repeatable** pipeline the
  crew can re-run each day.
- NOT fixing things blind: no fix lands without a documented finding + a root cause.
- NOT replacing the per-bead reviewer gate; logmine FEEDS the queue, it doesn't bypass review.
- NOT mining for secrets/PII — operational signal only.

## Success criteria

1. A committed, deduplicated **findings document** in this kerf work enumerating the
   patterns/issues found in the last day's logs, each with evidence.
2. A **prioritized issue register**, every item a bead labeled `codename:logmine`
   with a root-cause note and priority.
3. At least the top-priority findings **dispatched and landed** (or explicitly
   deferred with rationale).
4. The pipeline is documented well enough that a fresh crew could re-run it next day.

## Kerf-pass mapping

- problem-space (this doc) → analyze/research = Wave 1 harvest+document →
  decompose/tasks = Wave 2 investigate+prioritize+file-beads → (fixes dispatched as Wave 3).

## Owning crew

Executed by the dedicated **logmine crew** (Track B), bound to its own named queue.
The crew drives the waves with sub-agent fan-out; the captain assigns the lane and verifies.
