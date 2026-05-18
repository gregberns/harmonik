# Plan 002: claude-hook-bridge

## Objective
Bridge Claude Code's hook system into the harmonik daemon so the daemon observes
agent lifecycle (pre-exec, heartbeat, terminal) via a hook-relay subcommand and
a workspace-materialized `.claude/settings.json`, with a daemon-side socket and
durable `claude_session_id` checkpoint enabling `--resume` correctness.

## Status
mostly-done

Spec corpus is fully landed and reviewed; ~half the implementation beads have
landed code (twin Stop-hook caller, settings.json materialization,
EnsureWorktreeTrust isolation, agent-task.md per-launch write,
CHB-025/026/027 spec additions). Remainder is finishing the
hook-relay subcommand surface (CHB-006 → CHB-024), the three CHB-INV sensors,
and the real-Claude end-to-end test.

## What's done
- Spec corpus finalized: `2c320dc spec(claude-hook-bridge): finalize hook-bridge spec corpus`, `df06fb9 docs(spec-review): claude-hook-bridge corpus review — MINOR_REVISIONS`
- Late spec additions: `b38c441` CHB-025 Stop-hook dedup; `feb6494` CHB-026 concurrent socket serialization; `405a517` CHB-027 + §8 partial-write
- CHB-028 (agent-task.md per-launch artifact): `f6bfe06` spec, `a5c2739` impl, `3ee426c` amendment, `fdee5fa` no-tmux decision
- Workspace/daemon plumbing: `f6cd256` EnsureWorktreeTrust isolation (hk-lj1p9.3), `cec27e6` settings.json permissions.allow + splash-race (hk-53y35, hk-rf4ux), `f79b2a8` SetAgentReadyCallback + pasteInjectOnLaunch wiring (hk-lj1p9.4), `20c7fda` HC-055b worktree auto-trust
- Twin: `bf4d019 feat(twin): settings.json reader + Stop hook caller` (audit items 1+2)
- Closed beads: hk-qo08q.1 (CHB-001 materialize), .2 (atomic write), .3 (5 hooks), .4 (merge user settings), .5 (gitignore), .15 (NDJSON), .23 (durable checkpoint persistence); hk-u5c5i error taxonomy; spec-amendment beads hk-63k6b, hk-rirxa, hk-2ubs8, hk-4woeq
- Sensor partial: `514c0f6 test(specaudit): CHB-INV-003 sensor — mechanism-no-cognition (hk-xlach)` (open bead still listed; verify closure status)
- Triage dispatch document: `76a55be docs(chb-triage): dispatch document for CHB epic hk-qo08q`
- Recent unblock: `9083f55 chore(beads): unblock 3 CHB impl beads — convert 11 spec-text edges`

## What's remaining
Open beads under `codename:claude-hook-bridge` (31 total):
- Epic: hk-qo08q
- Implementation (hook-relay subcommand & handler): hk-lj848, hk-crf9a, hk-pcvw8, hk-02sp0, hk-q7atz, hk-s2vpx, hk-cw56j, hk-ocisx
- CHB-006 → CHB-024 spec-text beads: hk-qo08q.6, .7, .8, .9, .10, .11, .12, .13, .14, .16, .17, .18, .19, .20, .21, .22, .24
- Invariant sensors: hk-gerqr (INV-001 two-contributor), hk-qo96c (INV-002 single terminal), hk-xlach (INV-003 — may already be landed per 514c0f6; verify)
- Failure-path test: hk-pcgms (bridge_dial_failed)
- End-to-end: hk-7uasg (P1, real-Claude review-loop integration test)

## References
- code: `bf4d019`, `cec27e6`, `f6cd256`, `f79b2a8`, `a5c2739`, `20c7fda`, `514c0f6`
- specs: spec corpus landed (commits `2c320dc`, `b38c441`, `feb6494`, `405a517`, `f6bfe06`, `bb42834`)
- beads: 31 open, 12 closed under label `codename:claude-hook-bridge`; epic hk-qo08q
- docs: source kerf bench copied to `source/` — `01-problem-space.md`, `02-components.md`, `03-research/`, `04-design/claude-hook-bridge-design.md`, `05-spec-drafts/claude-hook-bridge.md`, `06-integration.md`, `07-tasks.md` (43 beads), `SESSION.md`, `05-changelog.md`
- chat-context: kerf spec-jig work created 2026-05-12, ready status reached 2026-05-15 after 5 reviewer passes; bead_filter `label: codename:claude-hook-bridge`; migrated to plans/ on 2026-05-18.

## Next steps
- Verify hk-xlach state vs `514c0f6` and close if landed.
- Dispatch hk-lj848 (hook-relay subcommand scaffold) — it gates CHB-010 → CHB-024 implementation beads.
- Then dispatch hk-crf9a + hk-pcvw8 (handler launch-prep + Wait-window) in parallel; CHB-006…CHB-024 spec-text beads follow.
- Schedule hk-7uasg (P1 real-Claude end-to-end) as the closeout sensor.

## Open questions
- None pending user decision. Implementation order is dependency-determined.
