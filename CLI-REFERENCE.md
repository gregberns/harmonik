# harmonik CLI Reference

> **Generated from the binary** (`harmonik <cmd> --help`) on 2026-06-12. The binary is the source of truth; if this doc and the live `--help` disagree, trust `--help`.

harmonik is an agent-driven bead-execution daemon. The canonical pattern is: start **one persistent daemon per project** (queue-only), then submit beads to its queue and watch progress.

```bash
# Start the daemon (queue-only, 4 wide), then submit work and watch:
harmonik --project /path/to/project --no-auto-pull --max-concurrent 4
harmonik queue submit --beads hk-abc123,hk-def456
harmonik subscribe --types run_completed,run_failed --json
```

---

## Exit codes at a glance

These are the documented exit codes per command. `17` (daemon not running) and `2` (transport/unknown verb) recur across the daemon-attached commands.

| Code | Meaning | Commands that document it |
|---|---|---|
| 0 | Success | all |
| 1 | Argument / validation / I/O error | all |
| 2 | Transport/protocol error, unrecognised verb, or usage error | queue, supervise, comms, crew, graph, reconcile, run, smoke |
| 3 | `--wait --timeout` elapsed (comms recv); build-gate failed (promote) | comms recv (3=timeout), promote (3=build gate) |
| 3 | File I/O error | release |
| 4 | Push failed / no last-good binary | promote (4=push), release (4=no last-good) |
| 5 | Push-mode refused (target protected); another instance running | promote (5=protected); run (5=inline-daemon collision); **pidfile-locked = 5** when a second daemon collides on the lock |
| 7 | `.harmonik/` directory not found | digest |
| 16 | No pending verdict for the run_id | confirm-verdict, veto-verdict |
| 17 | Daemon not running (socket absent / ECONNREFUSED) | queue, supervise (start/restart/pause/resume), confirm-verdict, veto-verdict, smoke, subscribe, comms (send/recv/join/leave), crew (start/stop), decisions |
| 25 | Supervisor already running | supervise start |

> **Pidfile-locked = exit 5.** Starting a second daemon for the same project collides on the pidfile lock and exits **5**. `harmonik run` avoids this when a daemon is already up by submitting to the existing daemon's socket instead of colliding (so exit 5 is not returned in that case).

---

## Top-level: `harmonik`

**Purpose:** the daemon itself. Run with no subcommand to start the dispatcher.

**Usage**
```
harmonik [--project DIR] [--max-concurrent N]
harmonik <subcommand> [flags]
```

**Daemon flags (used without a subcommand)**
| Flag | Meaning | Default |
|---|---|---|
| `--project DIR` | Project directory | current working directory |
| `--max-concurrent N` | Max simultaneous beads | 1 |
| `--auto-pull` | Enable br-ready fallback poll (historical topology) | OFF |
| `--no-auto-pull` | No-op alias; queue-only is the default (back-compat) | — |

Branch targeting: `--target-branch` / `--protect-branch` / `--forbid-default-main` keep the daemon from auto-pushing `main` — see [CONFIGURATION.md](CONFIGURATION.md).

**Subcommands listed in the menu:** version, init, run, handler, queue, subscribe, comms, crew, reconcile, confirm-verdict, veto-verdict, graph, promote, release, supervise, keeper, beads-merge, smoke, tmux-start, hook-relay.

**Example**
```bash
harmonik --project /path/to/project --no-auto-pull --max-concurrent 4
```

---

## `harmonik version`

**Purpose:** print semver + commit hash and exit (also `--version`).

**Usage:** `harmonik version`

A release build prints its embedded semantic version and short commit hash (e.g. `harmonik v0.2.0 (commit: a1b2c3d)`). A locally-built or `go install`-ed binary that wasn't stamped at build time prints the placeholder `dev (commit: unknown)` — that is a build-info artifact, **not** a real version number.

**Example**
```bash
$ harmonik version
harmonik dev (commit: unknown)    # locally-built binary; a release shows a real semver + commit
```

---

## `harmonik init`

**Purpose:** bootstrap a new project — create `.harmonik/`, init the beads DB, write configs, render `AGENTS.md`, start the supervisor.

**Usage**
```
harmonik init [--project DIR] [--target-branch BRANCH] [--prefix PREFIX]
              [--doctor] [--force] [--smoke] [--no-supervise]
```

**Flags**
| Flag | Meaning | Default |
|---|---|---|
| `--project DIR` | Project directory | current working directory |
| `--target-branch BRANCH` | Branch harmonik merges completed work into (fail-closed: `init` currently accepts only `main`; configure other target branches via `.harmonik/branching.yaml`) | main |
| `--prefix PREFIX` | Bead ID prefix for `br init` | hk |
| `--doctor` | Run precondition checks only; modify nothing | — |
| `--force` | Overwrite existing config files; reinitialise br DB | — |
| `--smoke` | Run a smoke test after init to verify the setup | — |
| `--no-supervise` | Skip `harmonik supervise start --watch-restart` | — |

**What it does:** checks preconditions → creates `.harmonik/` → `br init` → writes `config.yaml`, `branching.yaml`, `.gitignore` → renders `AGENTS.md` + `CLAUDE.md` symlink → starts supervisor → (`--smoke`) sanity checks. Idempotent: each step skips if its artifact exists; `--force` overwrites.

**Exit codes:** 0 success · 1 argument/precondition/I/O error.

**Example**
```bash
harmonik init --force --smoke
```

---

## `harmonik run`

**Purpose:** legacy/solo-bootstrap bead execution. **Not the primary dispatcher.** Submits to a running daemon if one exists, else runs the beads inline and exits on completion.

**Usage**
```
harmonik run <bead-id> [flags]
harmonik run --beads id1,id2,... [flags]
```

**Flags**
| Flag | Meaning | Default |
|---|---|---|
| `--beads id1,id2,...` | Comma-separated bead IDs (mutually exclusive with positional `<bead-id>`) | — |
| `--max-concurrent N` | Max simultaneous beads | 1 |
| `--context TEXT` | Free-form extra context injected into each agent task | — |
| `--context @FILE` | Same, but read context from a file | — |
| `--workflow-mode MODE` | Dispatch shape: `builtin`, `single`, `review-loop`, `dot` | builtin |
| `--workflow-ref PATH` | Path to `.dot` workflow file (required with `--workflow-mode dot`) | — |
| `--no-review-loop` | Opt out of review-loop; beads run single-node | review on |
| `--review-loop` | Deprecated no-op (review-loop is now default) | — |
| `--notify-stream` | One line per bead completion to stdout (auto-on for multi-bead runs) | — |
| `--notify-stream=PATH` | Same, but write to a FIFO or file | — |
| `--no-notify-stream` | Disable per-bead completion lines | — |
| `--wave` | Use wave-mode queue (no mid-flight appends) | stream |
| `--param KEY=VALUE` | Template substitution param for `.dot` workflows (repeatable; replaces `__KEY__`) | — |
| `--dry-run` | Print intended spawns without launching claude or mutating state | — |
| `--plan-only` | Alias for `--dry-run` | — |
| `--project DIR` | Project directory | current working directory |

**Exit codes:** 0 all beads succeeded (or `--dry-run` plan printed) · 1 a bead failed or arg/validation error · 2 unexpected queue state (inline-daemon path only) · 5 another harmonik instance is already running (inline-daemon path only; when a daemon is detected via `daemon.sock`, beads are submitted to it instead — so exit 5 is *not* returned in that case).

**Example**
```bash
harmonik run --beads hk-abc123,hk-def456 --max-concurrent 2
```

---

## `harmonik handler`

**Purpose:** inspect or resume a paused handler.

**Usage:** `harmonik handler <verb> [flags]`

**Verbs**
| Verb | Meaning |
|---|---|
| `status` | Show handler pause state (no daemon required) |
| `resume` | Resume a paused handler |

**Flags (status):** `--type AGENT-TYPE` (filter to one handler type, e.g. `claude-code`) · `--format json|text` (default text) · `--json` (shorthand for `--format json`) · `--project DIR`.

**Flags (resume):** `--type AGENT-TYPE` (required) · `--force` (no-op if already live, instead of error) · `--project DIR`.

**Example**
```bash
harmonik handler status --type claude-code --format json
```

---

## `harmonik queue`

**Purpose:** submit or inspect the bead queue. Most verbs require the daemon running.

**Usage:** `harmonik queue <verb> [flags]`

**Verbs**
| Verb | Meaning | Daemon required? |
|---|---|---|
| `submit` | Submit a new bead to the queue | yes |
| `append` | Append a bead to an existing queue run | yes |
| `status` | Show current queue state and bead statuses | yes |
| `list` | List all active queues with status and worker counts | yes |
| `pause` | Pause a named queue | yes |
| `resume` | Resume a paused named queue | yes |
| `dry-run` | Validate a submission without executing | yes |
| `cancel` | Archive a stale `queue.json` (e.g. left by a killed daemon) | **no** |
| `set-concurrency <n>` | **Set the daemon's concurrent-dispatch ceiling live.** `n` must be an integer ≥ 1. | yes |

**Notes:** queues are created automatically on first submit to a new name (`--queue` flag); absent `--queue` defaults to the `main` queue. `--queue-id <uuid>` targets a specific run for `append`. Exit 17 means the daemon is not running (socket absent / ECONNREFUSED).

**Verb `--help`:** `harmonik queue submit --help`, `harmonik queue dry-run --help`, and `harmonik queue append --help` now print the `harmonik queue` help and exit 0 — `--help` is intercepted as the first sub-arg before the value is read as a queue-file path. (Earlier builds swallowed `--help` as a filename and errored with `cannot read "--help"`; that is fixed — documented from source, takes effect after the next daemon rebuild if your installed binary predates the fix.) `harmonik queue status --help` ignores the arg and just runs (`(no queue active)`, exit 0).

**Exit codes:** 0 success (JSON to stdout) · 1 validation error (JSON error body) · 2 transport/protocol error or unrecognised verb (also: usage error from `set-concurrency`) · 17 daemon not running.

**Examples**
```bash
harmonik queue submit --beads hk-abc123
harmonik queue submit --queue investigate --beads hk-abc,hk-def
harmonik queue submit /tmp/batch.json
harmonik queue dry-run --beads hk-abc123
harmonik queue append --queue-id <uuid> 0 hk-abc123
harmonik queue status
harmonik queue list
harmonik queue pause investigate
harmonik queue resume investigate
harmonik queue cancel
harmonik queue set-concurrency 4      # → "max_concurrent: 4 → 4"
```

---

## `harmonik reconcile`

**Purpose:** close `in_progress` beads whose implementation has merged to the target branch.

**Usage:** `harmonik reconcile [--project DIR] [--target-branch BRANCH] [--run RUN_ID]`

**Flags**
| Flag | Meaning | Default |
|---|---|---|
| `--project DIR` | Project directory | current working directory |
| `--target-branch BRANCH` | Git branch to scan for merge commits | main |
| `--run RUN_ID` | Scope the scan to a single in-flight run ID | — |

**Exit codes:** 0 all subsumed beads closed (zero matches is also success) · 1 argument/adapter error · 2 at least one bead close failed (partial reconciliation).

**Example**
```bash
harmonik reconcile --target-branch develop
```

---

## `harmonik confirm-verdict`

**Purpose:** confirm a pending reconciliation verdict (daemon must be running).

**Usage:** `harmonik confirm-verdict <run_id> [--project DIR]`

**Argument:** `<run_id>` — the reconciliation run whose verdict to confirm; the daemon must have a pending-confirmation entry for it (see `harmonik status` to list pending verdicts).

**Flags:** `--project DIR`.

**Notes:** only meaningful when the workflow's YAML policy sets `confirm_required: true`. When false (the default), the daemon executes verdicts automatically.

**Exit codes:** 0 success (daemon proceeds with execution) · 1 arg/flag error · 16 no pending verdict for the run_id · 17 daemon not running.

**Example**
```bash
harmonik confirm-verdict run-abc123
```

---

## `harmonik veto-verdict`

**Purpose:** veto a pending reconciliation verdict (daemon must be running).

**Usage:** `harmonik veto-verdict <run_id> [--promote-to escalate-to-human] [--project DIR]`

**Argument:** `<run_id>` — the reconciliation run whose verdict to veto.

**Flags**
| Flag | Meaning |
|---|---|
| `--promote-to escalate-to-human` | After vetoing, substitute `escalate-to-human` and execute it (emits `operator_escalation_required`; run awaits manual resolution). Currently the only valid promotion target. |
| `--project DIR` | Project directory |

**Notes:** without `--promote-to`, a veto discards the verdict without executing any action (run unchanged).

**Exit codes:** 0 success (verdict discarded or promoted) · 1 arg/flag error · 16 no pending verdict · 17 daemon not running.

**Example**
```bash
harmonik veto-verdict run-abc123 --promote-to escalate-to-human
```

---

## `harmonik graph`

**Purpose:** workflow graph utilities.

**Usage:** `harmonik graph <verb> [flags]`

**Verbs**
| Verb | Meaning |
|---|---|
| `validate` | Validate a `.dot` workflow file (pre-run checks) |

### `harmonik graph validate`

**Usage:** `harmonik graph validate [--json] <path>`

**Argument:** `<path>` — path to a `.dot` workflow file.

**Flags:** `--json` (emit diagnostics as a JSON array instead of plain text).

**Exit codes:** 0 valid (no diagnostics) · 1 invalid (≥1 diagnostic) · 2 usage error (bad flags / missing path).

**Example**
```bash
harmonik graph validate --json workflow.dot
```

---

## `harmonik promote`

**Purpose:** promote work toward the target branch — cherry-pick banked SHA(s) with a build gate + push, or open a PR.

**Usage**
```
harmonik promote <sha>...      # push-mode: cherry-pick SHA(s) -> target, build-gate, push
harmonik promote --pr          # PR-mode:   open a PR from --from to target via gh pr create
```

**Flags**
| Flag | Meaning | Default |
|---|---|---|
| `--project <dir>` | Project root | cwd or `$HARMONIK_PROJECT` |
| `--target <branch>` | Target branch | `branching.yaml` `lands_on`, else `main` |
| `--pr` | PR-mode; mutually exclusive with positional SHA args | — |
| `--from <branch>` | PR-mode: head branch for the PR | integration |
| `--title <text>` | PR-mode: PR title (passthrough to `gh pr create`) | — |
| `--body <text>` | PR-mode: PR body (passthrough to `gh pr create`) | — |
| `--dry-run` | Print planned actions without mutating anything | — |

**Protection gate:** if the resolved target is in the project's `protect_branches` list, push-mode is refused fail-closed (exit 5). Use `--pr` instead.

**Exit codes:** 0 success · 1 arg/flag/config error · 2 cherry-pick conflict · 3 build gate failed · 4 push failed (retries exhausted) · 5 push-mode refused (target protected).

**Example**
```bash
harmonik promote --pr --from integration --title "Sprint 23"
```

---

## `harmonik release`

**Purpose:** release ledger management.

**Usage:** `harmonik release <verb> [flags]`

**Verbs**
| Verb | Meaning |
|---|---|
| `ledger` | List all release ledger entries |
| `record-create <semver>` | Record a CREATE-stage pre-release entry in the release ledger |
| `certify <semver>` | Certify a pre-release (flip `prerelease:false`, stamp `certified_at`) |
| `yank <semver>` | Yank a certified release (requires `--reason`) |
| `rollback` | Restore the last-good binary (supervisor last-good pin) |

**Flags:** `--project DIR` (all verbs) · `--commit SHA` and `--tag TAG` (for `record-create`) · `--reason TEXT` (required for `yank`) · `--bin PATH` (for `rollback`; default current executable).

**Exit codes:** 0 success · 1 arg/flag error · 2 ledger invariant violation (already certified/yanked, not found, etc.) · 3 file I/O error · 4 no last-good binary recorded.

**Example**
```bash
harmonik release yank v0.2.0 --reason "critical regression in merge logic"
```

---

## `harmonik supervise`

**Purpose:** manage the supervisor (cognition / flywheel) process.

**Usage:** `harmonik supervise <verb> [flags]`

**Verbs**
| Verb | Meaning |
|---|---|
| `start` | Launch the supervisor in a tmux session |
| `stop` | Terminate the supervisor |
| `status` | Show supervisor process state (file-surface, no daemon required) |
| `attach` | Attach terminal to the flywheel tmux session |
| `restart` | Stop and restart the supervisor (re-reads `config.json`) |
| `logs` | Capture recent flywheel pane output |
| `pause` | Pause daemon dispatch (no new beads; in-flight complete) |
| `resume` | Resume daemon dispatch after a pause |

**Common flags seen in examples:** `--watch-restart` (start), `--json` (status), `--lines N` (logs).

**Exit codes:** 0 success · 1 arg/operational error · 2 unrecognised verb · 17 daemon not running (start/restart/pause/resume) · 25 supervisor already running (start).

**Example**
```bash
harmonik supervise start --watch-restart
harmonik supervise logs --lines 500
```

---

## `harmonik keeper`

**Purpose:** context watcher for a managed agent pane (session-keeper, Phase-1). Warns at a context-use threshold and, when managed, runs an intent-preserving handoff→/clear→resume cycle.

**Usage**
```
harmonik keeper --agent <name> [--tmux <target>] [--warn-pct N] [--act-pct N]
                [--warn-abs-tokens N] [--act-abs-tokens N]
harmonik keeper enable <agent> [--project DIR] [--scripts-dir DIR] [--tmux TARGET] [--yes-destructive]
harmonik keeper doctor <agent> [--project DIR]
harmonik keeper set-dispatching <agent> [--project DIR]
harmonik keeper clear-dispatching <agent> [--project DIR]
```

**Verbs**
| Verb | Meaning |
|---|---|
| `enable` | Wire statusLine + Stop + PreCompact stanzas into `~/.claude/settings.json` (idempotent, JSON-aware, backs up first). Seeds `HANDOFF-<agent>.md`, validates tmux pane, prints the run command. `.managed` creation needs `--yes-destructive`. |
| `doctor` | Read-only drift validator (binary currency, all 3 hooks present, gauge freshness, `.idle`/`.managed` markers, `ANTHROPIC_API_KEY` risk). Non-zero on any gap. Also runs at keeper boot. |
| `set-dispatching` | Write `.dispatching` marker (HoldingDispatch → true). Call **before** submitting a batch so the keeper defers handoff while queue work is in flight. |
| `clear-dispatching` | Remove `.dispatching` marker (HoldingDispatch → false). Call when in-flight queue work completes. Idempotent. |

**Flags (watcher mode)**
| Flag | Meaning | Default |
|---|---|---|
| `--agent <name>` | Agent name (required); identifies lockfile + `.managed` marker | — |
| `--tmux <target>` | tmux pane target (injected on warn/act crossing) | — |
| `--warn-pct N` | Context-use % that triggers a warning | 80 |
| `--act-pct N` | Context-use % that triggers handoff action (`.managed`-gated) | 90 |
| `--warn-abs-tokens N` | Absolute-token warn threshold; effective = `min(warn-abs-tokens, warn-pct% * window)` | 240000 |
| `--act-abs-tokens N` | Absolute-token act threshold; effective = `min(act-abs-tokens, act-pct% * window)` | 300000 |
| `--respawn-cmd <cmd>` | Shell command to re-launch the agent when it exits (supervised respawn). After the gauge is stale 20s and the pane is idle, runs `sh -c <cmd>`. Requires `--tmux`; 90s cooldown. | — |

**Exit codes (watcher mode):** 0 success (no-op or clean signal shutdown) · 1 arg/I/O error · 2 lock held by another live keeper. **(set/clear-dispatching):** 0 success · 1 arg/validation/I/O error.

**Example**
```bash
harmonik keeper --agent flywheel --tmux harmonik:0 --warn-pct 80
harmonik keeper set-dispatching orchestrator
```

---

## `harmonik beads-merge`

**Purpose:** custom git merge-driver for `.beads/issues.jsonl` (union-by-bead-ID, last-writer-wins on `updated_at`; labels/deps union-merged). **Invoked automatically by git** — not for direct operator use.

**Usage:** `harmonik beads-merge %O %A %B %P` (base / current / other / working-tree path).

**Exit codes:** 0 merge succeeded (`%A` holds the result) · 1 argument or file-parse error. Unresolvable conflicts append to `.beads/merge-conflicts.log`.

**Example (testing only)**
```bash
harmonik beads-merge /tmp/git-merge-base /tmp/git-merge-a /tmp/git-merge-b .beads/issues.jsonl
```

---

## `harmonik smoke`

**Purpose:** end-to-end verification of a live daemon — dispatches a smoke bead and asserts it flows all the way to closed, checking five signals along the way (see below).

**Usage:** `harmonik smoke [flags]`

**Flags**
| Flag | Meaning | Default |
|---|---|---|
| `--project DIR` | Project directory | current working directory |
| `--branch BRANCH` | Target branch to verify the commit on | from `branching.yaml` or `main` |
| `--queue NAME` | Queue to submit the smoke bead to | main |
| `--timeout DUR` | Max time to wait for all signals | 20m |
| `--bead-id ID` | Reuse an existing bead instead of creating one | — |

**Signals verified:** (1) `run_started` (2) `run_completed` (3) commit with `Refs: <bead-id>` on the target branch (4) `reviewer_verdict` (5) `bead_closed`.

**Exit codes:** 0 all 5 signals observed within the timeout · 1 argument/setup/assertion failure · 2 timeout (a signal not observed) · 17 daemon not running.

**Example**
```bash
harmonik smoke --timeout 30m --branch integration
```

---

## `harmonik tmux-start`

**Purpose:** bootstrap a tmux session and start the daemon inside it.

**Usage:** `harmonik tmux-start [--session-name NAME] [--project DIR]`

**Flags:** `--session-name NAME` (default `harmonik`) · `--project DIR` (default cwd).

**Example**
```bash
harmonik tmux-start --project /path/to/project --session-name my-project
```

---

## `harmonik hook-relay`

**Purpose:** forward a Claude hook event to the daemon. **Internal use** — meant for Claude Code hook configs, not direct operator invocation. The daemon must be running to receive events.

**Usage:** `harmonik hook-relay <event-kind>` (e.g. `PreToolUse`, `PostToolUse`, `Stop`).

**Example**
```bash
harmonik hook-relay Stop
```

---

## `harmonik subscribe`

**Purpose:** stream daemon events (run_completed/run_failed/run_stale/heartbeat) on the Unix socket as NDJSON to stdout, with a server-side heartbeat.

**Usage:** `harmonik subscribe [flags]`

**Flags**
| Flag | Meaning | Default |
|---|---|---|
| `--types t1,t2,...` | Comma-separated event-type filter | all |
| `--heartbeat DUR` | Idle heartbeat cadence (clamped 10s..600s) | 60s |
| `--since-event-id ID` | Replay cursor: replay events strictly after this `event_id` before the live stream | — |
| `--to NAME` | Agent-message filter: only `agent_message` addressed to NAME or `*` | — |
| `--from NAME` | Agent-message filter: only `agent_message` sent by NAME | — |
| `--topic TOPIC` | Agent-message filter: matching topic | — |
| `--socket PATH` | Override socket path | `<project>/.harmonik/daemon.sock` |
| `--project DIR` | Project directory | cwd |
| `--json` | No-op alias; output is already NDJSON | — |

**Exit codes:** 0 stream closed cleanly · 1 argument error or stream write failure · 17 daemon not running.

**Example**
```bash
harmonik subscribe --types run_completed,run_failed --heartbeat 30s
```

---

## `harmonik digest`

**Purpose:** print a cognition/supervisor status sheet — a deterministic digest of recent activity (active runs, recent events, open notes) computed from the durable files under `.harmonik/`. No LLM and, in snapshot mode, no daemon connection required.

**Usage:** `harmonik digest [--project DIR] [--json] [--since EVENT_ID] [--full]`

**Flags**
| Flag | Meaning | Default |
|---|---|---|
| `--project DIR` | Project directory | current working directory |
| `--json` | Emit one schema-versioned NDJSON object to stdout (instead of the human-readable sheet) | — |
| `--since EVENT_ID` | Restrict the events window to those after this `event_id` (a UUIDv7 watermark) | — |
| `--full` | Disable size caps — include all active runs, events, and notes | — |

> A `--watch` flag also exists (live view polling at ~1s cadence; Ctrl-C to exit).

**Exit codes:** 0 success · 1 argument/flag error · **7 `.harmonik/` directory not found**.

**Example**
```bash
harmonik digest --json
```

---

## `harmonik decisions`

**Purpose:** the agent→human decision surface. An agent raises a question the operator must answer; the agent blocks until it receives the answer. Verbs are split into **agent side** (raise / wait / withdraw) and **operator side** (list / show / answer).

**Usage:** `harmonik decisions <verb> [flags]`

**Exit codes (all verbs):** 0 success · 1 argument error or op rejected · 2 unrecognised verb · 17 daemon not running (socket missing / ECONNREFUSED).

---

### Agent-side verbs

#### `raise`

Emit a `decision_needed` event and print the minted `decision_id`. With `--wait`, also block (§4 N8 arm-then-check) until the decision is answered and print the chosen option.

```
harmonik decisions raise --question "..." --option A --option B [--option ...] [flags]
```

| Flag | Default | Description |
|---|---|---|
| `--question TEXT` | — | The question the human must answer. **Required.** |
| `--option VALUE` | — | An enumerated choice. Repeatable; at least one required. |
| `--context LINK` | — | Free-form context pointer (bead id, codename, run_id). |
| `--from NAME` | `$HARMONIK_AGENT` | Emitting (blocked) agent name. |
| `--wait` | off | After raising, block until answered and print the chosen option. |
| `--socket PATH` | `<project>/.harmonik/daemon.sock` | Override socket path. |
| `--project DIR` | cwd | Project directory. |

**Output:** the minted `decision_id` (one line). With `--wait`: the `decision_id` first, then the chosen option (or `withdrawn: <reason>`) once answered.

**Exit codes:** 0 success · 1 argument error or daemon rejected the op · 17 daemon not running.

#### `wait`

Block until a specific decision's terminal arrives. Holds an open subscribe stream with the N8 arm-then-check ordering: arm first, then re-project the durable log; if already terminal, return immediately. Prints the chosen option on resolve, or `withdrawn: <reason>` on withdrawal. Dedupes on `event_id`; applies the first terminal (first-writer-wins).

```
harmonik decisions wait <decision_id> [--socket PATH] [--project DIR]
```

**Exit codes:** 0 terminal arrived (or stream closed cleanly) · 1 argument error or read failure · 17 daemon not running.

#### `withdraw`

Cancel your own open decision. Emits `decision_withdrawn` with reason `self_obsoleted` (default). Prints the emitted `event_id`.

```
harmonik decisions withdraw <decision_id> [--reason self_obsoleted] [--from NAME] [flags]
```

| Flag | Default | Description |
|---|---|---|
| `--reason REASON` | `self_obsoleted` | Withdrawal reason: `self_obsoleted` or `orphaned`. Agents use `self_obsoleted`; the keeper is the sole emitter of `orphaned`. |
| `--from NAME` | `$HARMONIK_AGENT` | Agent name recorded as the withdrawer. |
| `--socket PATH` | `<project>/.harmonik/daemon.sock` | Override socket path. |
| `--project DIR` | cwd | Project directory. |

**Exit codes:** 0 success · 1 argument error or daemon rejected the op · 17 daemon not running.

---

### Operator-side verbs

#### `list`

Show every open decision across all agents — the "what-needs-me" queue. Renders each decision as:

```
question · options · blocked_agent · context_link · decision_id
```

An open decision whose `blocked_agent` is Offline (past the ~10-minute presence cutoff, not merely Stale) is flagged `[orphaned-pending]` in the output — display only; no event is emitted. (The keeper tick reaps it.) This is a pure read; no aggregator process need be running.

```
harmonik decisions list [--json] [--socket PATH] [--project DIR]
```

`--json` emits a machine-readable JSON array. Output is sorted by `decision_id` for stable diffing.

**Exit codes:** 0 success · 1 argument error or daemon rejected the op · 17 daemon not running.

#### `show`

Show one open decision by id. Equivalent to `decisions list` filtered to a single `decision_id`, with the same orphaned-pending flag. Exits 1 if no open decision has that id.

```
harmonik decisions show <decision_id> [--json] [--socket PATH] [--project DIR]
```

**Exit codes:** 0 success · 1 argument error, unknown id, or daemon rejected the op · 17 daemon not running.

#### `answer`

Resolve an open decision. Emits `decision_resolved` for `<decision_id>` with the chosen `<option>`, which must be one of that decision's options (rejected otherwise). Resolving an unknown or already-answered `decision_id` is a **no-op** (exit 0, no event) — first-writer-wins. Prints the emitted `event_id` on success, or a `no-op:` note on a no-op.

```
harmonik decisions answer <decision_id> <option> [--value <text>] [--resolver <name>] [flags]
```

| Flag | Default | Description |
|---|---|---|
| `--value TEXT` | — | Optional free-text answer (v1.1 hook; parsed but not acted on in v1). |
| `--resolver NAME` | `operator` | Who answered. |
| `--socket PATH` | `<project>/.harmonik/daemon.sock` | Override socket path. |
| `--project DIR` | cwd | Project directory. |

**Exit codes:** 0 success or no-op on unknown/answered id · 1 argument error, bad option (not in the decision's options), or op rejected · 17 daemon not running.

---

**Examples**
```bash
# Agent: raise and block until answered
harmonik decisions raise --question "Ship v2?" --option ship --option hold --wait

# Agent: raise without blocking, store the id, wait later
DECISION=$(harmonik decisions raise --question "Proceed with migration?" --option yes --option no)
harmonik decisions wait "$DECISION"

# Operator: see what needs answering
harmonik decisions list
harmonik decisions show 0192f5a1-...

# Operator: answer a pending decision
harmonik decisions answer 0192f5a1-... ship
harmonik decisions answer 0192f5a1-... ship --resolver alice
```

---

## `harmonik comms`

**Purpose:** agent-to-agent messaging bus (send/recv/who/log/join/leave). Delivery is at-least-once — **dedupe on `event_id`**.

**Usage:** `harmonik comms <verb> [flags]`

**Verbs**
| Verb | Meaning | Daemon required? |
|---|---|---|
| `send` | Send an `agent_message` to a named agent or broadcast | yes |
| `recv` | Receive unread `agent_message`s from the durable cursor | yes |
| `log` | Read-only operator view of recent `agent_message` events | **no** |
| `join` | Emit `agent_presence{online, reason:"join"}` | yes |
| `leave` | Emit `agent_presence{offline, reason:"leave"}` | yes |
| `who` | List currently-online agents (presence registry) | **no** |

**Exit codes:** 0 success · 1 argument error or op rejected · 2 unrecognised verb · 17 daemon not running (send/recv/join/leave). `recv --wait --timeout` adds **3** = timeout with no matching message.

### `comms send`
**Usage:** `harmonik comms send (--to NAME | --broadcast) [--from NAME] [--topic T] [--reply-to ID] [--wake] [--] <body>`
- `--to NAME` directed recipient (mutually exclusive with `--broadcast`) · `--broadcast` to all (`to:"*"`) · `--from NAME` sender identity (default `$HARMONIK_AGENT`; **required**) · `--topic T` filter key · `--reply-to ID` threading hint · `--wake` nudge recipient's tmux pane (requires `--to`; best-effort) · `--socket PATH` · `--project DIR` · `--` end of flags · `<body>` trailing args or `-` for stdin.

### `comms recv`
**Usage:** `harmonik comms recv [--agent NAME] [--from NAME] [--topic T] [--follow] [--json] [--socket PATH] [--project DIR]`
- Without `--follow`/`--wait`: drains backlog once and exits. `--follow` drains then tails live (until signal). `--wait` blocks for exactly one matching message, advances cursor, exits (mutually exclusive with `--follow`); `--timeout DUR` makes it exit 3 on timeout.
- `--agent NAME` identity (default `$HARMONIK_AGENT`; required) · `--from`/`--topic` filters · `--json` NDJSON.

### `comms log`
**Usage:** `harmonik comms log [--since <event_id|duration>] [--to NAME] [--from NAME] [--topic T] [--json] [--project DIR]` — read-only, no daemon; `--since` accepts an `event_id` or a duration (`30m`, `1h`). Exit 0/1 only.

### `comms who`
**Usage:** `harmonik comms who [--json] [--project DIR]` — online = latest presence beat `online` within ~120s. No daemon. Exit 0/1 only.

### `comms join` / `comms leave`
**Usage:** `harmonik comms join [--name NAME] [--socket PATH] [--project DIR]` (and `leave`). `--name` default `$HARMONIK_AGENT`, required. Exit 0/1/17.

**Example**
```bash
harmonik comms send --to crew-alpha --from captain --wake -- New task for you
harmonik comms recv --agent captain --follow --json
harmonik comms who --json | jq -s '.'
```

---

## `harmonik crew`

**Purpose:** captain & crew session management (start/stop/list).

**Usage:** `harmonik crew <verb> [flags]`

**Verbs**
| Verb | Meaning | Daemon required? |
|---|---|---|
| `start` | Launch a persistent crew session, bind it to a named queue | yes |
| `stop` | Stop a crew session and clean up its registry record | yes |
| `list` | List registered crew members (reads `.harmonik/crew/*.json`) | **no** |

**Exit codes:** 0 success · 1 argument error or op rejected · 2 unrecognised verb · 17 daemon not running (start/stop).

### `crew start`
**Usage:** `harmonik crew start <name> --queue <q> --mission <handoff-path> [--socket PATH] [--project DIR]`
- `<name>` charset `[a-z0-9-]`, 1–64 chars. `--queue` and `--mission` both required. The daemon mints a `session_id` (printed to stdout), writes `.harmonik/crew/<name>.json`, ensures the queue exists, launches an interactive `claude --remote-control` session, and pastes the mission seed.

### `crew stop`
**Usage:** `harmonik crew stop <name> [--pause-queue] [--socket PATH] [--project DIR]`
- `--pause-queue` halts dispatch on the crew's queue after teardown (workers → 0). Registry/tmux teardown is synchronous, but the `claude --remote-control` process may take ~10s to fully exit (not a leak).

### `crew list`
**Usage:** `harmonik crew list [--json] [--project DIR]` — no daemon; sorted by name; absent dir → empty list. `--json` is NDJSON (one object per line — pipe to `jq -s` for an array).

**Example**
```bash
harmonik crew start alpha --queue alpha-q --mission /tmp/alpha-handoff.md
harmonik crew stop alpha --pause-queue
harmonik crew list --json | jq -s '.'
```

---

## `harmonik schedule`

> **(new in hk-0es; documented from source — not yet in the live `--help`; runs after the next daemon rebuild.)** The installed binary that produced the rest of this reference predates this command, so `schedule` does **not yet appear in the live `--help` menu** (it is absent from the subcommand list above) and `harmonik schedule --help` does not run on the installed binary. This section is documented directly from `cmd/harmonik/schedule.go`; it becomes live after the next daemon rebuild.

**Purpose:** the generic recurring-job primitive — register jobs the daemon fires on a daily clock. Each job pairs a schedule (when) with an action (what): run a shell **command**, or **spawn a crew**.

**Usage:** `harmonik schedule <verb> [flags]`

All verbs read and write `.harmonik/schedules.json` directly, so they work **whether or not the daemon is running** — there is no daemon connection and therefore **no exit-17 path**. A running daemon notices the file change and picks it up on its next poll tick; when no daemon is up the change takes effect on next boot.

**Verbs**
| Verb | Meaning |
|---|---|
| `add` | Add a scheduled job |
| `list` | List scheduled jobs (id, enabled, next-fire, last-fire, action summary) |
| `remove <id>` | Remove a job by id |
| `enable <id>` | Enable a job by id |
| `disable <id>` | Disable a job by id |
| `run-now <id>` | Fire a job on the daemon's next tick (honours overlap policy); when the daemon is down it fires on next boot |

### `schedule add`

**Usage:** `harmonik schedule add --id <id> --schedule "daily@HH:MM [tz]" --action <command|spawn-crew> [action flags]`

**Flags**
| Flag | Meaning | Default |
|---|---|---|
| `--id <id>` | Unique job id (**required**) | — |
| `--schedule "daily@HH:MM [tz]"` | Daily fire time (**required**); `tz` is `local` or an IANA zone (e.g. `America/New_York`). v1 supports the `daily` kind only. | tz = `local` |
| `--action <command\|spawn-crew>` | Action kind (**required**) | — |
| `--crew <c>` | Crew name (**required** for `spawn-crew`) | — |
| `--queue <q>` | Named queue the crew binds to (**required** for `spawn-crew`) | — |
| `--mission <path>` | Mission/handoff path the crew seeds from (`spawn-crew`, optional) | — |
| `-- <argv...>` | Command and its args (**required** for `command`; must be last, after `--`) | — |
| `--overlap-policy <skip\|allow>` | Skip a fire while the prior run is still active, or always fire | `skip` |
| `--catchup <coalesce-within-window\|off>` | Fire one coalesced catch-up for a recent missed fire, or never catch up | `coalesce-within-window` |
| `--catchup-window <dur>` | Bound catch-up eligibility (Go duration, e.g. `24h`) | schedule interval (24h for daily) |
| `--project DIR` | Project directory | current working directory |

### `schedule list`

**Usage:** `harmonik schedule list [--json] [--project DIR]` — plain text shows one row per job (id, enabled/disabled, next-fire, last-fire, action summary), sorted by id; `--json` emits one JSON object per line (NDJSON).

### `schedule remove | enable | disable | run-now`

**Usage:** `harmonik schedule <verb> <id> [--project DIR]` — each takes the job id as a positional argument.

**Exit codes:** 0 success · 1 argument error / job not found / persistence failure · 2 unrecognised verb. (No exit-17 — these verbs never connect to the daemon.)

**Examples**
```bash
harmonik schedule add --id nightly-crew --schedule "daily@02:00 America/New_York" \
  --action spawn-crew --crew nightowl --queue night --mission /tmp/mission.md
harmonik schedule add --id rotate-logs --schedule "daily@00:30" \
  --action command -- /usr/bin/logrotate /etc/logrotate.conf
harmonik schedule list --json
harmonik schedule disable nightly-crew
harmonik schedule run-now rotate-logs
harmonik schedule remove rotate-logs
```

---

## See also

- [README.md](README.md) — project intro and the documentation map.
- [OVERVIEW.md](OVERVIEW.md) — what harmonik is, who it's for, and its limits.
- [CONCEPTS.md](CONCEPTS.md) — the vocabulary (daemon, bead, queue, worktree, review-loop, crew, captain, comms bus, keeper).
- [INSTALL.md](INSTALL.md) — prerequisites and setup.
- [QUICKSTART.md](QUICKSTART.md) — run your first bead end to end.
- [OPERATING-GUIDE.md](OPERATING-GUIDE.md) — day-2 operations and troubleshooting.
- [CONFIGURATION.md](CONFIGURATION.md) — every config key, daemon flag, and environment variable.
