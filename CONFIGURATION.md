# Configuration

This is the single reference for every knob you can set when running harmonik: the two YAML files it reads, the flags you pass to the daemon, and the environment variables that change its behavior. For the full list of every CLI subcommand and flag, see [CLI-REFERENCE.md](CLI-REFERENCE.md). For how to deploy against an integration branch, see [OPERATING-GUIDE.md](OPERATING-GUIDE.md).

## How settings combine (precedence)

Higher-priority sources win over lower ones. From highest to lowest:

1. **Command-line flags and environment variables** supplied when you start the daemon — these beat everything.
2. **`.harmonik/config.yaml` and `.harmonik/branching.yaml`** — project-level files committed to the repo.
3. **Built-in defaults** shipped with harmonik.

Both YAML files are read **once at daemon startup** and cached. To change them, edit the file and restart the daemon. (`branching.yaml` is also re-read if its file timestamp changes; `config.yaml` requires a restart.) If a YAML file is absent, harmonik falls back cleanly to the built-in defaults — the files are optional. A malformed YAML file or an unsupported `version`/`schema_version` makes the daemon **refuse to start**, by design.

> **Billing / credential guard, read this first.** The daemon and every `claude` it launches must run on your Claude **subscription**, not the metered API. harmonik actively strips `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN`, and any `CLAUDE_CODE_OAUTH*` variable out of the environment of every agent it spawns, so a stray key in your shell cannot silently route work to API-key billing. Do **not** rely on setting `ANTHROPIC_API_KEY` to authenticate the daemon — it will be removed. The one place a credential is read is `harmonik supervise start` (the optional cognition/holder process), which reads it from an explicit env export or a gitignored repo-root `.env`; see the supervise docs.

---

## A. `.harmonik/config.yaml` — per-project agent defaults

Optional file at the repo root. It sets the default model and effort level for each kind of agent harmonik spawns. A per-bead `model:` label overrides whatever is set here.

```yaml
schema_version: 1
agents:
  claude-code:
    model: sonnet
    effort: medium
  claude-twin:
    model: sonnet
    effort: medium
```

| Name | Where set | Default | Plain-English meaning |
|---|---|---|---|
| `schema_version` | config.yaml (top level) | none — required if file is non-empty | File format version. Must be `1`. Any other value stops the daemon from starting. |
| `agents.<agent-type>.model` | config.yaml, under each agent block | unset → falls back to the daemon's built-in baseline (Sonnet) | Model alias for that agent type (e.g. `sonnet`, `haiku`, `opus`). Omit to defer to the built-in default. |
| `agents.<agent-type>.effort` | config.yaml, under each agent block | unset → falls back to built-in default | Reasoning/effort level for that agent type (e.g. `medium`). Omit to defer. |

Notes: `<agent-type>` is a key like `claude-code` or `claude-twin`. Unknown agent keys are ignored (so the file stays forward-compatible). An empty file behaves the same as no file.

---

## B. `.harmonik/branching.yaml` — branch targets and protection

Optional file at the repo root, meant to be committed so the branching convention travels with the team. It controls which branch completed work lands on and which branches the daemon must never touch. This is the recommended way to target an integration branch and keep `main` protected. The CLI flags in section C override these per run.

```yaml
version: 1
defaults:
  lands_on: harmonik-integration
  protect_branches:
    - main
```

| Name | Where set | Default | Plain-English meaning |
|---|---|---|---|
| `version` | branching.yaml (top level) | none — required if file is non-empty | File format version. Must be `1`. Any other value stops the daemon from starting. |
| `defaults.start_from` | branching.yaml | `main` | Branch each worktree is cut from when a bead starts. |
| `defaults.lands_on` | branching.yaml | `main` | Branch that completed bead branches are merged into. Set this to your integration branch to keep work off `main`. |
| `defaults.landing_strategy` | branching.yaml | `squash` | How a finished bead branch is merged: `squash` or `cherry-pick`. Any other value stops the daemon from starting. |
| `defaults.protect_branches` | branching.yaml | none (empty list) | List of branch names the daemon may **never** merge into or overwrite. Put `main` here so the daemon fail-closes and will not push `main` under any condition. |

---

## C. Daemon command-line flags

Passed when you start the daemon (`harmonik [flags]`, no subcommand). These override the YAML files for that run. Full flag detail across all subcommands: [CLI-REFERENCE.md](CLI-REFERENCE.md).

| Name | Where set | Default | Plain-English meaning |
|---|---|---|---|
| `--project DIR` | daemon flag | current working directory | The project directory harmonik operates on. |
| `--max-concurrent N` | daemon flag | `1` | Maximum number of beads dispatched at the same time. The practical knee is ~4–5 on a 10-core box; higher oversubscribes CPU and can fill the disk. |
| `--workflow-mode MODE` | daemon flag | `builtin` | Workflow dispatch shape applied to each bead: `builtin` (the default implement → review → merge path, with the review-loop on), `single` (implement only, no review), `review-loop`, or `dot` (an author-defined graph; requires `--workflow-ref`, an early/emerging capability — not the everyday path). A per-bead `workflow:` label overrides this. |
| `--auto-pull` | daemon flag | off (queue-only) | Opt in to the historical behavior of draining `br ready` automatically. Off by default — the daemon only runs work submitted to its queue. Leaving this off is the safe billing posture. |
| `--no-auto-pull` | daemon flag | (no-op alias) | Accepted for backward compatibility; does nothing because queue-only is already the default. |
| `--target-branch BRANCH` | daemon flag | `main` | Branch the daemon merges and pushes completed work to. Overrides `defaults.lands_on`. Use this for integration-branch deployments. |
| `--protect-branch BRANCH` | daemon flag (repeatable) | none | Branch the daemon must never merge into or overwrite. Repeat the flag for several branches. Overrides/augments `defaults.protect_branches`. |
| `--forbid-default-main` | daemon flag | off | Refuse to start unless the repo's default branch (`main`/`master`) is in the protected set. A safety latch for product repos that must never auto-push `main`. |
| `--subscription-token-ceiling N` | daemon flag | `0` (disabled) | Per-5-hour token ceiling for the shared Claude subscription. When non-zero, harmonik auto-tunes `--max-concurrent` to stay under it. Start conservative and raise until you see a rate-limit (429). |
| `--default-harness NAME` | daemon flag | `claude-code` (built-in) | Global default agent harness when a bead/queue/node does not specify one: `claude-code` or `codex`. |
| `--codex-binary PATH` | daemon flag | `codex` resolved via `PATH` | Path to the `codex` executable, used when the resolved harness is `codex`. |

Note: there are **no** CLI flags for spend/run budgets or model tiers — those are environment variables (section D). The per-queue daily spend cap is set as a `spend_cap_usd` field inside the queue-submit JSON, not as a flag or YAML key.

---

## D. Environment variables

Set in the environment of the daemon process (or, where noted, the supervise/keeper process). Variables marked **guard** change safety/billing behavior rather than tuning a value.

| Name | Where set | Default | Plain-English meaning |
|---|---|---|---|
| `ANTHROPIC_API_KEY` | environment | (none) | **Guard.** If present, harmonik **strips it** from every agent it spawns so work bills the subscription, not the metered API. Do not depend on it to authenticate the daemon — it is removed by design. |
| `ANTHROPIC_AUTH_TOKEN` | environment | (none) | **Guard.** Same treatment as `ANTHROPIC_API_KEY` — stripped from spawned agents. |
| `CLAUDE_CODE_OAUTH_TOKEN` (and any `CLAUDE_CODE_OAUTH*`) | environment | (none) | **Guard.** Any variable starting with `CLAUDE_CODE_OAUTH` is stripped from spawned agents for the same billing-isolation reason. |
| `FLYWHEEL_BUDGET_USD_PER_DAY` | environment | `20.0` | Per-day USD spend cap covering both the cognition (Pi) side and daemon-spawned `claude` cost. When the cap is hit, work pauses. Resets at the day boundary. |
| `HARMONIK_MAX_RUNS_PER_DAY` | environment | `200` | Per-day ceiling on the number of bead runs the daemon will start, as a loss-proof backstop alongside the USD cap. Must be a positive integer (`unlimited` is not accepted). |
| `HARMONIK_MAX_CONCURRENT_SESSIONS` | environment | `--max-concurrent × 2` | Hard ceiling on concurrently-spawned agent sessions (an implementer + a reviewer per bead, hence ×2). Set to `0` to disable the cap entirely. |
| `FLYWHEEL_MODEL_TIER1` | environment | a Haiku model (unconfirmed) | Model ID the cognition loop uses for its cheapest/fastest tier. The exact default is set on the cognition (Pi) side, not a compiled harmonik constant — treat the value as approximate. |
| `FLYWHEEL_MODEL_TIER2` | environment | a Sonnet model (unconfirmed) | Model ID for the mid tier. Default set Pi-side; treat as approximate. |
| `FLYWHEEL_MODEL_TIER3` | environment | a Sonnet model (unconfirmed) | Model ID for the judgment/expensive tier. Set to an Opus model to opt in to Opus; the cost-posture default keeps it on Sonnet. Default set Pi-side; treat as approximate. |
| `HARMONIK_PROJECT` | environment | (none) | Fallback project directory for some subcommands when `--project` is not passed. |
| `HARMONIK_AGENT` | environment | (none) | The agent's own name on the `harmonik comms` bus; the keeper sets this in a managed pane so the agent can self-identify when sending messages. |
| `HARMONIK_TWIN_CLAUDE` | environment | (none; auto-discovered) | Path to the twin (test-double) claude binary. Used by tests/CI to point at an explicit twin instead of locating one relative to the repo. |
| `CLAUDE_CONFIG_HOME` | environment | (Claude default) | Directory harmonik trusts as the Claude config home when preparing a worktree's Claude trust. |
| `HARMONIK_CLAUDE_CONFIG_PATH` | environment | (none) | Explicit override for the Claude config file path used when establishing worktree trust. |

The following are **set by the daemon for the agent it spawns** (per-run context), not knobs you configure: `HARMONIK_RUN_ID`, `HARMONIK_SESSION_ID`, `HARMONIK_BEAD_ID`, `HARMONIK_PHASE`, `HARMONIK_WORKFLOW_MODE`. Listed here only so you recognize them if you see them inside an agent's environment.

---

## See also

- [OVERVIEW.md](OVERVIEW.md) — overview and the branch-protection quickstart.
- [INSTALL.md](INSTALL.md) / [QUICKSTART.md](QUICKSTART.md) — getting harmonik running.
- [CLI-REFERENCE.md](CLI-REFERENCE.md) — every subcommand and flag.
- [OPERATING-GUIDE.md](OPERATING-GUIDE.md) — running the daemon day to day, including integration-branch deployment.
- [CONCEPTS.md](CONCEPTS.md) — what beads, the queue, and workflows are.
