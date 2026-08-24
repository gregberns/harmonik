---
id: dead-shell-sweep
title: Delete the 25 shell scripts nothing anywhere calls
type: chore
priority: 2
labels: [scripts, cleanup, clear-the-ground]
depends_on: []
blocks: []
workstream: W6
batch: 5
---

## Problem

The repo holds **140 shell scripts totalling 35,484 lines**; `scripts/` alone is 111 files and 30,741
lines. For scale: that is 92% the size of `internal/daemon`'s entire production body. The Makefile is
1,712 lines with 149 targets and references 68 distinct scripts.

**25 of those scripts, about 2,600 lines, have no caller anywhere** — not in the Makefile, not in CI,
not in Go source, not in a skill, not in a doc that instructs anyone to run them. Audited 2026-08-23.

Several are self-tests that were dropped from `make script-tests` and now never run. An unrun
self-test is precisely the defect `scripts/stranded-test-seams.sh` was written to detect — and that
script is itself one of the 25 with no caller.

Also found: `Makefile:741` names `scripts/scenario-gate.sh`, which does not exist. It is inside a
comment so nothing breaks, but the Makefile documents a script that was deleted.

## Scope

Delete, after re-verifying each has no caller at the time of deletion:

**Frozen run artifacts** — `plans/2026-07-17-assessor-daemon-campaign/runs/*/scenarios/*.sh` (6 files,
three near-identical copies), `plans/2026-07-17-prod-readiness-watch/*.sh` (4 files).

**Untracked scratch** — `.harmonik/reports/assessor-archive-jul19/{h2,regate-watcher}.sh`,
`.harmonik/tmp/assessor-follow.sh`. These have hardcoded `/tmp` paths and a July commit pin.

**Shipped-vertical measurement rigs** — `scripts/keeper-{metrics,oracle-n10,coverage-gate}.sh`,
`scripts/codex-{nudge,keeper-watch,keeper-notify}.sh`, `scripts/test-codex-keeper-pilot.sh`.

**One-off operator tools** — `scripts/{hk-tag-version,hk-version-report,ubs,eval-grade,agent-session-watchdog}.sh`.

**`scripts/run-st5-scenario.sh`** — wraps a single `go test -run`; `make test-scenario` covers it.

**Orphaned self-tests** — `scripts/{cmd-coverage-gate-test,with-isolated-gocache-test,queue-status-writer-ratchet-test,readywait-freeze-gate-test,runloop-emitter-gate-test,lint-full-count-concurrency-test}.sh`.
For each: either re-add it to `make script-tests` or delete it. **Do not leave it in the third state.**

And fix the stale `Makefile:741` reference.

## Done when

1. Each listed file is deleted or, for the orphaned self-tests, wired into `make script-tests`.
2. `make full` is green afterwards.
3. The commit body records, per file, the check that proved it had no caller.

## Limits

- **Re-verify before deleting.** The audit is dated 2026-08-23; a caller may have appeared. A grep
  over the Makefile, CI workflows, Go source, skills and docs, per file.
- **Do not delete anything with a caller**, however ugly. Porting a live script to Go is separate work
  with its own tasks.
- Do not touch `scripts/scratch-daemon.sh` or the keeper hook scripts. Both are live and both are
  legitimately shell.
- `scripts/stranded-test-seams.sh` has no caller but detects a real defect class. Decide whether to
  wire it up or drop it — do not leave it sitting unrun.
