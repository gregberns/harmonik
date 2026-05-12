# Exploratory Testing Wave Plan

> Produced by a planning agent on 2026-05-12. Intended to be reviewed before
> any testing-wave dispatch. The gate for launching the wave is the row #11
> smoke test (one ready bead → workspace → twin run → bead closed → JSONL
> event) passing green. Until then, this is a design doc.

## 1. Division of Work

**Recommended partitioning: by user-visible journey, not by subsystem.**

The wrong partition is one agent per subsystem (eventbus / handler / daemon /
workspace / brcli). The actual failure modes in a work-loop integration are
cross-subsystem by nature: a crash in `Handler.Launch` manifests in the daemon
loop's error branch, which manifests in `brcli.CloseBead` being called or not
called, which manifests in bead state. Assigning one agent to "test handler"
and another to "test daemon" guarantees both agents exercise the same coupling
seam and neither owns diagnosing it.

The right partition is by **observable scenario**: each agent owns a class of
user-visible outcome. The coupling is tested by every scenario; what differs
is which exit-condition the agent is trying to provoke.

Recommended 6-agent split:

| Agent | Scenario Class | What it probes |
|---|---|---|
| T1 | Cold start / happy path variants | 1-bead success, sequential runs, bead status after close |
| T2 | Subprocess failure modes | non-zero exit, crash, SIGKILL from outside, twin binary refusing |
| T3 | Daemon lifecycle boundary | second-instance attempt (pidfile), Ctrl-C mid-run, orphan sweep on restart |
| T4 | Bead-state edge cases | no ready beads, bead claimed by concurrent `br`, ReopenBead path |
| T5 | Event / JSONL integrity | event ordering, JSONL durability, redaction in emitted events |
| T6 | Scale and shape stress | 10-bead queue drain, large workspace diff, unicode bead body, zero-byte bead |

T1 is the anchor: it must pass before the others are meaningful. T2–T4 cover
the three cross-subsystem failure paths the work loop must handle. T5 is
latent-correctness testing that won't crash the process but can corrupt
operator trust. T6 finds boundaries the unit tests never hit.

## 2. Each Agent's Mandate

**Inputs (every agent receives the same envelope):**

- `BINARY` — absolute path to the built `harmonik` binary (built once before wave launch, path passed in brief).
- `TWIN_BINARY` — absolute path to the pre-built twin binary (see §4). For T2, a second "failing twin" binary that exits non-zero.
- `SCOPE_SHEET` — the agent's row from the table above (verbatim, no paraphrasing).
- `PROJECT_TEMPLATE` — path to a pre-seeded `.beads/` directory with the fixture bead corpus (see §4).
- `REPORT_PATH` — `test/exploratory/findings-T<N>.md`, the file the agent writes.

**Each agent's work loop:**

1. Clone the project template into a fresh temp directory (do not modify the template).
2. Run `harmonik` (or a sub-invocation) against the clone.
3. Observe: exit code, JSONL content, bead status, worktree presence, process table.
4. Record every anomaly as a finding (title, repro command, expected vs actual, severity).
5. Repeat with varied inputs until either the agent is out of ideas within its scope or 30 minutes wall-clock have elapsed.

**Artifacts:**

- `test/exploratory/findings-T<N>.md` — structured findings. Each finding: title, one-line repro command, expected, actual, severity (crash / data-loss / functional / cosmetic).
- `test/exploratory/reproductions/T<N>-<slug>/` — directory per finding with the `.beads/` mutations that triggered the bug.

**Stopping condition:** 30-minute wall-clock OR "no new failure class in the last 5 runs," whichever first. Agents do NOT claim beads or file beads.

**Proving findings are real:** every finding must include a shell-runnable one-liner (only `harmonik`, standard shell, `br`) reproducible from a fresh clone in under 60 seconds. Otherwise filed as `unconfirmed` and excluded from synthesis.

## 3. Aggregation

A synthesizer agent runs AFTER all 6 testing agents complete — not in parallel.

The synthesizer:

1. Reads all 6 `findings-T<N>.md`.
2. Deduplicates by matching on: same exit code + same bead transition + same JSONL event shape. Two findings with identical observable behaviour merge into one entry with both repro commands listed.
3. Produces `test/exploratory/synthesis.md` — deduplicated finding list, ranked by severity, canonical repro command each.
4. Files one bead per confirmed finding via `br create` with `exploratory-finding` label. **Only the synthesizer files beads.**

The orchestrator reads `synthesis.md` as the action list for the next implementation wave. Raw per-agent files are audit trail.

## 4. Test Infrastructure Required Before the Wave

Discrete prerequisites, each a bead to file before the wave runs:

- **P1 — Fixture bead corpus generator** (`test/exploratory/fixtures/seed.sh`). Creates a `.beads/` SQLite DB populated with N ready beads of varied shapes (1-liner, 500-word body, unicode title, ASCII title, shell-metacharacter body). Idempotent; accepts `--count N` and `--dest DIR`.
- **P2 — Failing twin binary** (`test/twins/fail-immediately/main.go`). Same CLI surface as the smoke-test twin; exits 1 immediately. Needed by T2.
- **P3 — Hanging twin binary** (`test/twins/hang/main.go`). Starts cleanly, writes nothing, hangs until SIGKILL. Needed by T2 and T3.
- **P4 — Reset script** (`test/exploratory/reset.sh`). Given a project dir, removes `.harmonik/`, `.beads/`, harmonik-created worktrees, then re-seeds. No network.
- **P5 — Known-good smoke run** (row #11 itself). The wave does NOT launch until `go test ./internal/daemon/...` (or wherever the smoke test lands) is green. Not a new bead; the gate condition.

P1–P4 can each be one XS implementer dispatch. File them with `blocks: [<wave-dispatch bead>]` so `br ready` surfaces the gate.

**Twin pattern note:** `internal/handler/twinlaunch.go` + `VerifyTwinLaunch` already support repo-relative resolution with hash-pinning. P2/P3 binaries must be built with `-ldflags "-X main.commitHash=<sha>"` so `VerifyTwinLaunch` accepts them; P1's fixtures must encode the pinned hash. Row #11 demonstrates the pattern.

## 5. Dispatch Shape

Brief template, parameterized per T1–T6:

```
WORKTREE: /Users/gb/github/harmonik/.claude/worktrees/agent-<id>
BRANCH:   worktree-agent-<id>
SCOPE:    Exploratory testing — <scenario class from §1, verbatim>
BINARY:   <abs path to pre-built harmonik binary>
TWIN_OK:  <abs path to smoke-test twin>
TWIN_FAIL: <abs path to fail-immediately twin (P2)>
TWIN_HANG: <abs path to hanging twin (P3)>
FIXTURE_CORPUS: <abs path to pre-seeded .beads/ snapshot dir>
RESET_SCRIPT: test/exploratory/reset.sh
REPORT:   test/exploratory/findings-T<N>.md
TIME_LIMIT: 30 minutes wall clock
PROTOCOL: read .claude/implementer-protocol.md — authoritative.
DO NOT file beads. DO NOT commit code changes. Write findings to REPORT only.
```

The two `DO NOT` lines are the only additions over the standard brief. Testing agents are observers, not implementers; the synthesizer owns the transition into the bead corpus.

## 6. Anti-Patterns to Avoid

- **Do not skip the twin binary.** Real Claude Code invocations burn API credits and add network latency that swamps signal. Every scenario uses a twin. If row #10's work loop hardcodes `claude` rather than accepting a configurable binary path, that itself is the first finding — a structural defect, not a test gap.
- **Do not let testing agents file beads directly.** Two agents tripping on the same bug from different angles will file duplicates with different titles and repros. Funnel through the synthesizer.
- **Do not launch the wave until row #11 is green.** Six agents all hitting the same root cause is parallel reproduction, not exploration.
- **Do not share a project directory across agents.** SQLite serializes writes but does not stop one agent consuming a bead another was about to use. Each agent gets its own clone of the fixture corpus.
- **Do not treat the 30-min limit as a hard kill.** Soft limit with instruction to write findings before exit. Hard kill produces truncated reports.
- **Do not run T5 before T1 returns findings.** If JSONL wiring is incomplete (hk-8mup.63 status is the marker), T5 will report every event as missing — a corpus gap, not a finding.
- **Do not route the synthesizer's `br create` calls through a testing-agent worktree.** Synthesizer is its own post-wave implementer dispatch with its own worktree.

## Critical files for the wave

- `internal/handler/handler.go` — `Handler.Launch`; testing agents exercise this surface via the work loop.
- `internal/handler/twinlaunch.go` — `TwinLaunchConfig` / `VerifyTwinLaunch`; P2/P3 must satisfy this contract.
- `internal/daemon/daemon.go` — `Start`; the work loop (row #10) lands here.
- `internal/brcli/adapter.go` — bead-state error taxonomy every finding depends on.
- `.claude/implementer-protocol.md` — the brief template testing agents inherit, with two addendum lines.
