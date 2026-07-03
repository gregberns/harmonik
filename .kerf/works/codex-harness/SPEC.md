# SPEC — Harness contract (codex as a second implementer harness)

> Normative spec. On `kerf finalize` this is copied to `specs/harness-contract.md` (project
> convention: normative specs live in `specs/`). harmonik is spec-first — code is updated to match
> this spec. Status: draft for the `codex-harness` plan work. Supersedes the implicit assumption
> that the implementer harness is always Claude Code.

## 1. Purpose

harmonik dispatches a bead's work to an **implementer harness** — a CLI agent launched in a managed
git worktree. Today the only harness is Claude Code. This spec defines a `Harness` contract so a
second harness (OpenAI **codex**) can be selected per run, with Claude remaining the default and all
existing beads/queues/workflows unchanged (N-1 safe).

## 2. The Harness contract (normative)

A harness MUST implement:

```
AgentType() core.AgentType                 // identity; registered in the AdapterRegistry
LaunchSpec(rc RunCtx) (LaunchSpec, error)  // binary + argv + env + cwd for ONE spawn
Seed(sess, rc) error                       // deliver the first-turn task to a fresh session
Retask(sess, feedback, rc) error           // deliver review feedback for iteration >= 2
Teardown(sess) error                       // end the session so the shared loop's sess.Wait returns
DetectReady(ev Event) bool                 // map a harness event to harmonik's agent_ready
SessionIDPolicy() {Minted | Captured}      // how the run's session id is obtained
Completion() {EventStreamThenQuit | ProcessExit}  // how the run signals "done"
```

**Normative properties:**

- **N1 — env credential guard.** `LaunchSpec.env` MUST strip the harness's billing-credential env
  vars and re-emit them as empty overrides, so no live API credential reaches the child via the
  substrate's additive env injection. Claude: `ANTHROPIC_API_KEY`/`ANTHROPIC_AUTH_TOKEN`/
  `CLAUDE_CODE_OAUTH*`. Codex: `OPENAI_API_KEY`/`CODEX_API_KEY`.
- **N2 — completion governs liveness.** When `Completion()==ProcessExit`, the shared loop MUST NOT
  run the heartbeat-staleness kill path (`pasteInjectQuitOnCommit`, launched at `dot_cascade.go:643`);
  it relies on process exit (`sess.Wait`) plus the absolute `commitHardCeiling` (90m). When
  `Completion()==EventStreamThenQuit`, the existing `/quit`+grace+kill path runs.
- **N3 — session-id policy.** `Minted` harnesses receive a caller-minted UUIDv7 up front (Claude).
  `Captured` harnesses obtain their session id from the harness after launch (codex `thread_id` from
  the first `thread.started` JSONL event); the run MUST record it for `Retask`.
- **N4 — completion via git, not harness internals.** "Done with work" is decided by the SHARED git
  layer: worktree HEAD ≠ parent and a `Refs:<beadID>` commit trailer. A harness MUST ensure its
  commit carries the trailer (instruct + verify, with a deterministic commit-after-exit fallback if
  the harness edits but does not commit). No harness-internal completion inspection is permitted.
- **N5 — shared infrastructure is off-limits to harness code.** The tmux substrate, worktree
  create/merge/remove, commit-detection, merge-one-at-a-time, queue/dispatch, and DOT cascade are
  harness-blind and MUST NOT be branched per harness except at the two declared seam points
  (`launchSpecBuilder`/registry lookup, and the `Completion()` gate at `dot_cascade.go:643`).

## 3. Harness selection (normative)

A run's harness is resolved by `ResolveHarness` with strict precedence:

```
per-bead label (harness:<x>)  >  per-queue default  >  per-node DOT attr (harness=<x>)  >  global default
```

- **Default:** absent all four tiers → `claude` (`AgentTypeClaudeCode`). This is the N-1 anchor.
- `Config.DefaultHarness` feeds the global tier (default `"claude"`); `--default-harness` overrides.
- Unknown harness string at any tier → error at resolve time (fail closed), naming the bad value.
- Duplicate/conflicting selectors at the same tier (e.g. two `harness:<x>` bead labels) → error at
  resolve time (fail closed).
- The reviewer's harness defaults to the implementer's; an optional `reviewer_harness` override MAY
  pin a different reviewer harness (e.g. always-claude).

## 4. Codex billing posture (normative)

Codex MUST run on the **ChatGPT subscription** path, never the API credit pool, by default:

- **B1.** Strip `OPENAI_API_KEY` and `CODEX_API_KEY` from the codex child env (N1).
- **B2.** Materialize/verify `forced_login_method = "chatgpt"` in `$CODEX_HOME/config.toml`
  (idempotent; preserve other keys).
- **B3.** Pre-flight `assertChatGPTPlan` (run `codex login status` BEFORE the first task turn); fail
  the run closed (`codex_billing_guard` event) if it does not report a ChatGPT plan.
- **B4.** Set `$CODEX_HOME` deterministically to a stable writable path; do not trust an inherited
  value (B3 is the backstop for a logged-out inherited home).
- **B5 (operational, MUST-TEST before enabling codex in production):** verify on the pinned codex
  version that `codex exec` honors B1/B2; audit the OpenAI org for a "Codex CLI (auto-generated)"
  key (#2000); verify the codex reviewer reliably emits the structured verdict.

## 5. Codex launch shape (informative — see C2 spec for detail)

`codex exec --json --sandbox workspace-write -a never -C <worktree>` for the initial turn;
`codex exec resume <thread_id> --json ...` for iterations ≥2 (spawn-per-turn, not live-inject).
Completion = process exit + terminal `turn.completed`/`turn.failed` JSON event. `Teardown` is a
no-op (exec self-terminates). This is structurally identical to claude's iteration ≥2, which is
already a fresh `claude --resume` process — so no shared cross-iteration-process assumption breaks.

## 6. Back-compat (normative)

All additions (the `Harness` interface, `AgentTypeCodex`, the `harness`/`reviewer_harness` DOT attrs,
the `harness:<x>` bead label, the per-queue `harness` field, `Config.DefaultHarness`/`CodexBinary`)
are **additive**. No field is renamed or removed. `Config.HandlerBinary` keeps its claude semantics.
A no-selection run resolves to `claude` and produces byte-identical launch behavior (verified by a
golden test on the pure `LaunchSpec` return + a side-effect-parity test on the shared scaffolding).

## 7. Out of scope

A third+ harness or a dynamic plugin marketplace; per-model routing inside a harness; replacing any
shared infrastructure; cost/quality benchmarking of codex vs claude.
