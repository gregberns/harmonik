---
id: ops-monitor-check-to-go
title: Move the fleet health check into Go, where a test can run it instead of grepping its text
type: task
priority: 2
labels: [scripts, shell-to-go, daemon, clear-the-ground]
depends_on: []
blocks: []
workstream: unassigned
batch: 6
---

> **Workstream unassigned.** These shell-to-Go conversions do not belong to `PLAN.md` §W6,
> which is the `crew-cleanup` skill. Whether they form a workstream of their own is an open
> operator decision, so this field reads `unassigned` rather than carrying a number that is
> already taken. Do not invent one. This task is parked until that ruling lands.

## Problem

`scripts/ops-monitor-check.sh` is 2,058 lines — the largest shell file in `scripts/` — and
`test/exploratory/ops_monitor_check_test.sh` is another 2,848. Together they are 4,906 lines, more
than any other shell subject in the repository.

It is live. `cmd/harmonik/ops_monitor_cmd.go` builds a launchd LaunchAgent whose payload is this
script, `cmd/harmonik/schedule.go` names it in the scheduled-command example, and the shipped `watch`
skill (`cmd/harmonik/assets/skills/watch/SKILL.md`) tells the watch tier to read its output. Its own
header lists at least ten health checks: daemon up, supervisor up, paused queues, single-mode
throughput, crew staleness, ready-but-unstaffed, idle fleet, review-gate bypass, and more. Several
carry an issue reference for a real incident, including a seven-hour eleven-minute gap where both the
daemon and the supervisor were down and the fleet had no self-healing path.

**The decisive fact is how it is tested.** Four Go tests in `cmd/harmonik` —
`watch_zombie_soak3_test.go` and `watch_escalation_we_soak2_test.go` — build a path to the script and
then assert on its **source text**. One of them fails with "ops-monitor-check.sh does not contain
'_dedup_key'". Another asserts the script "does NOT gate watch_present on watch_info['online']".
These are greps for an implementation detail wearing the shape of a behavioural test. They pass when
the string is present and say nothing about whether the check works. Rename an internal helper and a
green test goes red for no reason; change what the helper does while keeping its name and the test
stays green while the behaviour is gone.

That is not a testing mistake anyone chose. It is what is available when the subject is 2,000 lines
of shell that shells out to `harmonik` and parses JSON: there is no seam to call. In Go, each check
is a function over the daemon and supervisor status the CLI already returns as structured data, and
a table test can call it.

Nothing in `make fast`, `make core` or `make full` runs it, so this is **not** gate-critical — it is
the largest and least testable live shell body, which is a different reason to convert it.

## Scope

- `scripts/ops-monitor-check.sh` — every check in its header list, the deduplication key, the
  severity levels including `[IMMEDIATE]`, and the output format the `watch` skill reads.
- `test/exploratory/ops_monitor_check_test.sh` — the case corpus to port. It is the assessor-owned
  failure corpus, so it is evidence about real incidents, not scaffolding.
- `cmd/harmonik/ops_monitor_cmd.go` — the two `filepath.Join(projectDir, "scripts", ...)` call sites
  and the LaunchAgent payload.
- `cmd/harmonik/schedule.go` — the documented example command.
- `cmd/harmonik/watch_zombie_soak3_test.go` and `cmd/harmonik/watch_escalation_we_soak2_test.go` —
  the source-text assertions to replace.
- The shipped `watch` skill, which names `scripts/ops-monitor-check.sh` by path in its
  staffing-readiness paragraph. **It has two copies, not three.** Measured 2026-08-24:
  `cmd/harmonik/assets/skills/watch/SKILL.md` (the source of truth, embedded in the binary) and
  `.claude/skills/watch/SKILL.md` (generated output), and they are byte-identical today. There is no
  `.harmonik/agents/_skills/watch/` — that folder holds only `agent-comms`, `beads-cli`, `boot`,
  `crew-launch` and `harmonik-dispatch`, and `.harmonik/agents/watch/manifest.yaml` does not ask for
  a `watch` skill, so no third copy is needed. Re-count with
  `ls -d cmd/harmonik/assets/skills/watch .claude/skills/watch .harmonik/agents/_skills/watch`
  before you edit, because another lane may add one.

## Done when

1. The health check runs as Go — a `harmonik` subcommand, since `harmonik ops-monitor` already owns
   this surface and a LaunchAgent needs one binary to invoke.
2. `cmd/harmonik/ops_monitor_cmd.go` installs a LaunchAgent that runs the binary, not a shell script,
   and no Go source builds a path into `scripts/`.
3. `scripts/ops-monitor-check.sh` is gone.
4. Every source-text assertion in `watch_zombie_soak3_test.go` and `watch_escalation_we_soak2_test.go`
   is replaced by a test that **calls** the check with a synthesized fleet state and asserts the
   verdict. No test in the repository asserts on the contents of the deleted file.
5. Each check in the header list has a Go test with both directions: a state that must raise it and a
   state that must not. Name in the commit body any check ported without a raising case, and why.
6. The deduplication key and the escalation-suppression behaviour have tests that call them —
   including the `watch_present` case the current test can only describe in a comment.
7. **Every copy of the `watch` skill that exists is updated in the same commit, and they stay
   byte-identical.** The skill names the script by path, so deleting the script forces an edit even
   if the output format never changes. Today that is two files —
   `cmd/harmonik/assets/skills/watch/SKILL.md` and `.claude/skills/watch/SKILL.md`. Edit the
   `cmd/harmonik/assets/skills/` copy first, then mirror it. Prove it with
   `diff cmd/harmonik/assets/skills/watch/SKILL.md .claude/skills/watch/SKILL.md`, which must print
   nothing. If a `.harmonik/agents/_skills/watch/` copy exists by the time you run, it is a third
   file and the same rule covers it.
8. `make fast` and `make full` are green.

## Limits

- **Do not change which conditions raise an alert or at what severity.** The `[IMMEDIATE]` level and
  the suppression windows the header records (crew staleness above 150 s, suppressed if the crew
  posted within 900 s) are tuned against real incidents. Converting must not retune them. If a
  threshold looks wrong, record it and leave it.
- **Do not drop a check because the shell version looks broken.** Port it, add the failing-direction
  test that shows it broken, and file the finding. A check silently lost during conversion is a
  monitor that reports healthy for a fleet that is not.
- **Do not delete `test/exploratory/ops_monitor_check_test.sh` until its cases exist in Go.** It is
  the assessor's live failure corpus; the assessor owns that directory.
- **Do not convert and delete in one landing if that leaves an installed LaunchAgent pointing at a
  file that no longer exists.** A monitor that fails to start is silent in exactly the way a monitor
  must not be. Land the Go path first, then remove the script.
- **Do not widen `tools/lintreport/allow.txt`.**
- W4 is shrinking `cmd/harmonik`. This subcommand's logic belongs in an `internal/` package with a
  thin `cmd/harmonik` entry point, not as 2,000 more lines in `package main`.
