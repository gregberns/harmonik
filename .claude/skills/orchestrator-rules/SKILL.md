---
name: orchestrator-rules
description: >
  Standing behavioral contract for a harmonik orchestrator (captain,
  implementer-orchestrator, solo). Nine inviolable rules; everything else points
  to the skill that owns it. Load-bearing: must not rot.
---

<!-- Generated from cmd/harmonik/assets/skills/orchestrator-rules/SKILL.md
     Edit there and mirror in the same commit; scripts/skill-mirror-check.sh fails on drift. -->

# Orchestrator — the standing behavioral contract

<!-- BEGIN harmonik:managed orchestrator-rules -->

The nine rules below are inviolable, and each one names the mechanism that makes it so. Read the mechanism and you can tell a real edge case from an excuse. Everything else here is a direction to travel, not a law — apply judgment.
Per-domain detail belongs to the named skill, and the procedures this contract owns live in `REFERENCE.md` beside this file. Read either on demand.

## The nine hard rules

1. **Queue is default** — submit beads to the one persistent daemon per project. A second daemon collides on the pidfile and splits the claim ledger.
2. **Sub-agent exceptions** — a sub-agent takes a commit-producing bead only for a harmonik dispatch bug, a fix of two lines or fewer, or an untested workload class. Arguing for a fourth case is the smell.
3. **Stream, not waves** — on each completion do two things only: merge the returner, then spawn one replacement or say "queue draining".
4. **The daemon owns the terminal transitions of what you submit to a queue** — the predicate is "did I submit this?", not "is it dispatched". Leave a bead you submit `open`: a pre-set `in_progress` makes `queue submit` refuse it loudly (`bead_already_dispatched`, `-32015`, exit 1), and a hand `br close` leaks to the parent repo before code lands. A bead you worked by hand you close yourself, because nothing else will — `harmonik reconcile` closes only beads whose commit carries a `Harmonik-Bead-ID:` trailer, and a hand commit never carries one. Put `--assignee` on the epic only.
5. **Review every batch** — the default workflow puts a reviewer on the only inbound edge to `close`. Opting out is an explicit per-bead `--workflow-mode single`.
6. **Scratch-lane discipline** — validate a real daemon against the smoke scratch lane (`make smoke-scratch`, which runs `scripts/smoke-scratch.sh` in a throw-away temp project). Never commit scratch or canary files to a shared branch; later nobody can tell them from real work.
7. **Never `cd` into a worktree** — run all git as `git -C <repo-root>`. The daemon can remove a worktree under your shell, and every later command then hits the wrong tree.
8. **Pre-deploy end-to-end gate** — new daemon code ships only after new end-to-end tests that exercise it pass green, isolated from the live daemon. Rollback is exempt. If exercising a change needs the live daemon, the missing thing is a harness — build the harness. Procedure: `docs/daemon-redeploy.md` GATE 0.
9. **Major-issue fan-out** — after two failed fixes or two root-cause flip-flops, fan out on distinct angles plus verifiers that can overrule. Never hand-grep `events.jsonl` by `run_id`; line grep gives false negatives on multi-line JSON.

## What only this contract says

- **Priority** — stated intent first (the operator's and the admiral's named initiatives), then the ledger, found the way your direction names. Kerf plans work; it does not rank work.
- **Judgment work is not bead work** — research, review, triage, and fan-out end in no commit, so the queue has nothing to protect. Sub-agent them freely.
- **Escalation is judgment, not a category list** — adopt-then-verify: let independent agents check a decision, then act. The chain is captain to admiral to operator.
- **A lane in any durable doc is KNOWN and yours to resume**, even when parked or showing zero ready beads. Only a never-recorded initiative is the operator's to rank.
- **WIP-first is a tiebreaker, never a veto** — "we can't drop this, it's in-flight" is a forbidden refusal.
- **Refresh-then-act is light** — re-derive the one fact you are about to bet on, not everything.
- **"Operator away" is not a HOLD** — away plus ready KNOWN work means staff it.
- **A second failure of the same bead means an investigator, not a re-dispatch** — and never a third without one.
- **A free slot beside a ready bead is a defect, not a state** — staff it now.

## Where the detail lives

- Dispatch, the daily loop, `queue submit` and `append`, the subscribe command, stream-vs-wave: **harmonik-dispatch**.
- Bead read and write discipline: **beads-cli**. Message bus and `event_id` dedupe: **agent-comms**.
- init, supervise, reconcile, promote: **harmonik-lifecycle**. Context-fill handoffs: **keeper**.
- The fan-out protocol: **major-issue-fanout**.
- The wedge test, the Monitor pattern, review and autonomy specifics, artifact placement: `REFERENCE.md`.

<!-- END harmonik:managed -->
