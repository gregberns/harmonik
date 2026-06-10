# Harness Contract

```yaml
---
title: Harness Contract
spec-id: harness-contract
requirement-prefix: HN
status: draft
spec-category: foundation-cross-cutting
spec-shape: requirements-first
version: 0.1.0
spec-template-version: 1.1
owner: codex-harness-author
last-updated: 2026-06-10
depends-on:
  - architecture
  - handler-contract
  - claude-launchspec
  - credential-isolation
  - execution-model
  - event-model
  - process-lifecycle
  - queue-model
---
```

## 1. Purpose

harmonik dispatches a bead's work to an **implementer harness** — a CLI agent launched in a managed
git worktree. Today the only harness is Claude Code; the harness identity has been an implicit
assumption baked into the dispatch path. This spec names the **Harness contract**: the interface and
the normative properties that any implementer harness MUST satisfy so a second harness (OpenAI
**codex**) — and any future harness — can be selected per run without forking the shared
dispatch, worktree, merge, or review-loop machinery.

The contract is the cross-harness seam. It defines *what* a harness is and *what* the shared
infrastructure may assume of every harness; it does NOT define how any one harness (claude, codex)
implements those operations. Per-harness implementation detail (codex's `exec` argv, claude's
`/quit` sequence) lives in the per-harness adapter specs and is out of scope here.

This is a separate spec from `handler-contract.md` because handler-contract owns the handler runtime
boundary (LaunchSpec shape, Outcome record, error taxonomy, skill provisioning) that is harness-blind
by design; harness-contract owns the *parameterized-by-harness* surface — the five operations and two
policies that vary between claude and codex — and the selection precedence that resolves which harness
runs a given bead.

## 2. Scope

### 2.1 In scope

- The `Harness` interface: the five harness-varying operations (`LaunchSpec`, `Seed`, `Retask`,
  `Teardown`, `DetectReady`) plus the two harness policies (`SessionIDPolicy`, `Completion`) and the
  identity accessor (`AgentType`).
- The five normative properties every harness MUST satisfy (credential guard, completion-governs-
  liveness, session-id policy, completion-via-git, shared-infra-off-limits).
- The two declared seam points the shared infrastructure exposes for harness dispatch, and the
  prohibition on branching anywhere else.
- Harness selection: the four-tier precedence resolver, the claude default (the N-1 anchor), and the
  fail-closed error rules.
- The reviewer-harness resolution rule (default = implementer; optional independent override).
- The completion-mode liveness contract: which liveness mechanism the shared loop applies per
  completion mode.
- The auth/billing boundary every harness MUST present (the credential-strip obligation), and codex's
  subscription-path billing posture as a normative instance.
- Back-compat: the additive-only rule and the byte-identical-claude guarantee.

### 2.2 Out of scope

- Per-harness adapter implementation detail (codex `exec` argv/flags, the codex JSONL event parser,
  claude's `/quit`+grace+kill sequence) — owned by the per-harness adapter specs.
- The handler runtime boundary (the LaunchSpec record fields, the Outcome record, the handler error
  taxonomy `ErrTransient`/`ErrDeterministic`, skill provisioning) — owned by [handler-contract.md].
- The credential-strip *mechanism* (allowlist env construction, empty-override emission against the
  tmux additive-env path) — owned by [credential-isolation.md]; this spec names the *obligation* and
  the per-harness credential-var set.
- The named-queue model and per-queue config field shape — owned by [queue-model.md]; this spec names
  the per-queue tier as one selection tier only.
- The DOT workflow grammar and node/edge attribute parsing — owned by [execution-model.md]; this spec
  names the `harness` and `reviewer_harness` attributes as selection inputs only.
- A third+ harness or a dynamic plugin marketplace; per-model routing inside a single harness;
  cost/quality benchmarking of one harness versus another.

## 3. Glossary

- **Harness** — a CLI agent type harmonik can launch into a managed worktree to implement (or review)
  a bead. Today: `claude` (Claude Code) and `codex` (OpenAI codex). (see §4.1)
- **Implementer harness** — the harness selected to do a bead's implementation work. (see §4.4)
- **Reviewer harness** — the harness selected to run the review-loop verdict for a bead; defaults to
  the implementer harness. (see §4.5)
- **Adapter registry** — the in-process table mapping `core.AgentType` to its registered `Harness`
  implementation. (see §4.1)
- **Session-id policy** — `Minted` (caller assigns a UUID up front) or `Captured` (the harness
  reports its own session id after launch). (see §4.3)
- **Completion mode** — `EventStreamThenQuit` (the harness runs an event stream and is quit by
  harmonik) or `ProcessExit` (the harness self-terminates and completion is the process exit). (see
  §4.6)
- **Commit-via-git** — the shared, harness-blind rule that "done with work" is decided by the git
  layer (worktree HEAD ≠ parent and a `Refs:<beadID>` commit trailer), never by harness internals.
  (see §4.7)
- **Seam point** — one of the two declared insertion sites where the shared dispatch path consults the
  resolved harness; the only two places per-harness branching is permitted. (see §4.8)
- **N-1 anchor** — the property that a run with no harness selector resolves to `claude` and produces
  byte-identical behavior to the pre-contract dispatch path. (see §4.4, §4.10)

## 4. Normative requirements

### 4.1 The Harness interface

#### HN-001 — A harness is a single typed interface

A harness MUST be a single Go interface (`Harness`) implemented once per `core.AgentType` and
registered in the adapter registry keyed by that `AgentType`. The interface comprises eight members:
the identity accessor `AgentType`, the five harness-varying operations (`LaunchSpec`, `Seed`,
`Retask`, `Teardown`, `DetectReady`), and the two harness policies (`SessionIDPolicy`, `Completion`).
No harness-varying behavior may live outside these eight members; conversely, no shared
infrastructure may live inside them (see §4.8).

```
AgentType() core.AgentType                       -- identity; registered in the adapter registry
LaunchSpec(rc RunCtx) (LaunchSpec, error)        -- binary + argv + env + cwd for ONE spawn
Seed(sess Session, rc RunCtx) error              -- deliver the first-turn task to a fresh session
Retask(sess Session, feedback String, rc RunCtx) error  -- deliver review feedback for iteration >= 2
Teardown(sess Session) error                     -- end the session so the shared loop's sess.Wait returns
DetectReady(ev Event) bool                       -- map a harness event to harmonik's agent_ready
SessionIDPolicy() SessionIDPolicy                -- {Minted | Captured}: how the run's session id is obtained
Completion() CompletionMode                      -- {EventStreamThenQuit | ProcessExit}: how the run signals "done"
```

Tags: mechanism

#### HN-002 — The two harness policies are first-class enums

`SessionIDPolicy` MUST be one of `{Minted, Captured}` and `Completion` MUST be one of
`{EventStreamThenQuit, ProcessExit}`. These two policies are first-class (separate accessors, not
derived) because they are exactly the two structural points at which codex differs from claude: claude
is `(Minted, EventStreamThenQuit)`; codex is `(Captured, ProcessExit)`. The shared loop branches on
these accessors per §4.3 and §4.6 and on no other harness-internal property.

Tags: mechanism

#### HN-003 — `LaunchSpec` returns a pure spawn descriptor

`LaunchSpec(rc)` MUST return the binary, argv, env, and cwd for exactly ONE spawn, as a pure value —
it MUST NOT perform shared-scaffolding side effects (worktree-trust seeding, `agent-task.md` write,
settings materialization, pre-exec messages). Those side effects belong to the shared caller per §4.8
so every harness reuses them identically. The pure-return scoping is what makes a harness's launch
behavior golden-testable independently of the shared scaffolding.

Tags: mechanism

#### HN-004 — `Seed` and `Retask` split first-turn from iteration

`Seed(sess, rc)` MUST deliver the first-turn task to a fresh session. `Retask(sess, feedback, rc)`
MUST deliver review feedback for iteration ≥ 2. The two are separate operations because a harness MAY
deliver the first turn and a subsequent turn differently (claude live-injects into a persistent
session; codex spawns a fresh `exec resume` process per turn). The shared review-loop calls `Seed`
once per run and `Retask` once per review iteration; it makes no assumption about whether the same OS
process services both.

Tags: mechanism

#### HN-005 — `DetectReady` maps a harness event to `agent_ready`

`DetectReady(ev)` MUST return `true` when the supplied harness event signals that the agent session is
ready for its task, and `false` otherwise. The shared loop emits harmonik's `agent_ready` signal on
the first `true`. The mapping is the harness's only obligation to translate its native event stream
into the shared liveness model; the shared loop MUST NOT inspect harness-native events directly.

Tags: mechanism

### 4.2 The five normative properties

#### HN-006 — N1: env credential guard

A harness's `LaunchSpec.env` MUST strip that harness's billing-credential environment variables and
re-emit them as empty overrides, so no live API credential reaches the child via the substrate's
additive env injection. The credential-var set is per-harness:

| Harness | Stripped credential vars |
|---|---|
| `claude`  | `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN`, `CLAUDE_CODE_OAUTH*` |
| `codex`   | `OPENAI_API_KEY`, `CODEX_API_KEY` |

The strip-and-empty-override *mechanism* (allowlist env construction; empty-override emission that
defeats the tmux server's additive `-e` injection) is owned by [credential-isolation.md]; this spec
names the obligation and the per-harness var set. A new harness MUST declare its credential-var set and
apply the same strip-and-override discipline.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### HN-007 — N2: completion governs liveness

The harness's `Completion()` mode governs which liveness mechanism the shared loop applies:

- When `Completion() == ProcessExit`, the shared loop MUST NOT run the heartbeat-staleness kill path
  (the `pasteInjectQuitOnCommit` machinery). It relies on process exit (`sess.Wait`) plus the absolute
  commit hard ceiling (90 minutes per [process-lifecycle.md] / [claude-launchspec.md]).
- When `Completion() == EventStreamThenQuit`, the shared loop MUST run the existing `/quit` + grace +
  kill path (the heartbeat-staleness machinery).

The branch is consulted at the seam point of §4.8.HN-013 and nowhere else. This property is
load-bearing: a `ProcessExit` harness that ran the heartbeat-staleness kill path would be killed
mid-run; an `EventStreamThenQuit` harness that skipped the `/quit` path would hang on `sess.Wait`.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### HN-008 — N3: session-id policy

A harness's `SessionIDPolicy()` governs how the run obtains its session id:

- A `Minted` harness MUST receive a caller-minted UUIDv7 before launch; the caller owns the id and
  passes it into `LaunchSpec` (claude).
- A `Captured` harness MUST obtain its session id from the harness after launch — the run reads it
  from the harness's first session-established event (e.g. codex's `thread_id` from the first
  `thread.started` JSONL line) — and the run MUST record it durably so `Retask` (iteration ≥ 2) can
  resume the correct session.

A `Captured` harness that exits before emitting a session id MUST fail the run closed (it cannot be
resumed without an id); silent fallback to a minted id is forbidden.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

#### HN-009 — N4: completion via git, not harness internals

"Done with work" MUST be decided by the SHARED git layer: the worktree HEAD differs from its parent
AND the new commit carries a `Refs:<beadID>` trailer. A harness MUST ensure its commit carries the
trailer by (a) instructing the agent to commit with the trailer, (b) verifying the trailer after the
session ends, and (c) applying a deterministic commit-after-exit fallback when the harness edited the
worktree but did not commit (or committed without the trailer). When the harness made no edits, the
shared noChange path fires unchanged.

No harness-internal completion inspection (reading the harness's own "I finished" signal as proof of
work) is permitted. The git layer is the single completion authority; a harness's process exit or
terminal event refines the *outcome* (success vs. failure) but never substitutes for the git check.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### HN-010 — N5: shared infrastructure is off-limits to harness code

The shared, harness-blind infrastructure MUST NOT be branched per harness except at the two declared
seam points of §4.8. The off-limits surface is: the tmux substrate; worktree create / merge / remove;
commit-detection; merge-one-at-a-time; queue and dispatch; and the DOT cascade and review-loop control
flow. Harness code interacts with this surface only through opaque argv/env/cwd (the `LaunchSpec`
return) and the shared `Session` handle; it MUST NOT reach into the substrate, the worktree manager,
or the merge path. Any `if <harness>` branch outside the two seam points is a structural violation of
this contract.

Tags: mechanism

### 4.3 Session-id handling

#### HN-011 — `Minted` is the N-1 anchor; `Captured` is recorded for resume

The default harness (`claude`) is `Minted`: the caller mints the UUIDv7 and the run id maps directly to
it. A `Captured` harness's session id MUST be recorded in run state keyed by the harmonik run id (the
run-uuid → harness-session-id mapping) at the moment it is first observed, before any `Retask` is
attempted. The mapping is part of the run's durable state so a daemon restart mid-run can resume the
correct harness session. This requirement refines HN-008 with the durability obligation.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### 4.4 Harness selection

#### HN-012 — Selection precedence is a four-tier fail-closed resolver

A run's harness MUST be resolved by a single pure resolver (`ResolveHarness`) with strict precedence,
highest first:

```
per-bead label (harness:<x>)  >  per-queue default  >  per-node DOT attr (harness=<x>)  >  global default
```

- **Default (the N-1 anchor):** absent all four tiers, the resolver MUST return `claude`
  (`AgentTypeClaudeCode`). A no-selection run resolves to claude.
- The global tier is fed by `Config.DefaultHarness` (default `"claude"`); a `--default-harness` flag
  overrides it.
- An unknown harness string at ANY tier MUST cause a resolve-time error (fail closed), and the error
  MUST name the offending value. The error fires at resolve time, not at launch time.
- Duplicate or conflicting selectors at the SAME tier (e.g. two `harness:<x>` bead labels) MUST cause
  a resolve-time error (fail closed).
- The resolver MUST NOT fall back to claude on an unknown or conflicting selector — fail-closed is the
  rule; only the *absence* of all selectors yields the claude default.
- The per-queue tier reads the per-queue `harness` default from [queue-model.md]; when the named-queue
  field is absent, the resolver MUST skip that tier without error and consult the lower tiers.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### HN-013 — Selection resolves once per run, then drives the registry lookup

Selection MUST resolve exactly once per run. The resolved `core.AgentType` MUST drive the adapter
registry lookup (`AdapterRegistry.ForAgent(agentType)`) that yields the run's `Harness`. A lookup for
an unregistered `AgentType` MUST return a typed error and fail the run closed. There MUST be a single
dispatch site that performs this lookup — there is NO parallel per-harness dispatch path (see §4.8).

Tags: mechanism

### 4.5 Reviewer-harness resolution

#### HN-014 — Reviewer harness defaults to the implementer harness

The reviewer's harness MUST default to the implementer's resolved harness. The review-loop reaches its
reviewer harness through the SAME registry lookup as the implementer; harness selection is inherited by
the review-loop without a parallel resolution path. Both the implementer node and the reviewer node
fetch `LaunchSpec` / `Seed` / `Retask` / `Teardown` from the run's resolved harness; only the
cascade's existing phase split (implementer vs. reviewer) differs.

Tags: mechanism

#### HN-015 — An optional `reviewer_harness` override MAY pin a different reviewer

An optional `reviewer_harness` selector (a DOT review-node attribute, or a `Config.ReviewerHarness`
global) MAY pin a reviewer harness distinct from the implementer's — e.g. an always-claude reviewer
while a new harness's structured-verdict reliability is unproven. When the override resolves to a
`Minted` harness on a `Captured` implementer run (or vice versa), the reviewer launches via its own
harness with its own session-id policy applied independently (a `Minted` reviewer always mints a fresh
id). The existing verdict-parsing path (the reviewer verdict record and `reviewer_verdict` event) is
reused unchanged regardless of which harness produced the verdict.

Tags: mechanism

### 4.6 Completion-mode liveness

#### HN-016 — The completion-mode branch lands at the single declared seam site

The completion-mode branch of HN-007 MUST be applied at the seam point of §4.8.HN-019 — the launch
site of the heartbeat-staleness machinery — and at the analogous reviewer launch site, and nowhere
else. The bare `sess.Wait` in the workloop is NOT a branch site: it consumes whichever liveness signal
the gated path produces and needs no per-harness edit. Placing the branch in the workloop instead of at
the declared seam site is a structural violation.

Tags: mechanism

#### HN-017 — `ProcessExit` completion relies on `sess.Wait` plus the hard ceiling only

For a `ProcessExit` harness, run completion MUST be governed by the substrate session's process exit
(`sess.Wait`) plus the absolute commit hard ceiling (90 minutes). The heartbeat-staleness kill, the
`/quit` injection, and the post-quit grace-then-kill machinery MUST all be skipped. A terminal harness
event (e.g. a success/failure event on the JSONL stream) MAY refine the outcome status but MUST NOT be
the completion authority — that remains the git layer per HN-009.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

#### HN-018 — `Teardown` is harness-defined and may be a no-op

`Teardown(sess)` MUST end the harness's session so the shared loop's `sess.Wait` returns. For an
`EventStreamThenQuit` harness this is the `/quit` + grace + kill sequence; for a `ProcessExit` harness
this MUST be a no-op (the process self-terminates). The shared loop calls `Teardown` unconditionally at
the end of a run; a `ProcessExit` harness's no-op `Teardown` MUST be safe to call after the process has
already exited.

Tags: mechanism

### 4.7 The two seam points

#### HN-019 — Exactly two seam points exist; no third branch is permitted

The shared dispatch path MUST expose exactly two seam points for harness dispatch, and per-harness
branching is permitted ONLY at these two:

1. **The launch-spec / adapter-registry lookup.** The cascade resolves the harness (`ResolveHarness`,
   §4.4) and obtains its launch-spec builder and adapter from the registry
   (`AdapterRegistry.ForAgent`, §4.1). This replaces the always-claude launch-spec-builder default with
   a harness-dispatched lookup. The implementer and reviewer both route through this single resolved
   harness (§4.5).
2. **The completion-mode gate at the heartbeat-staleness launch site.** The launch of the
   `pasteInjectQuitOnCommit` machinery is gated on `Completion()`: launched for `EventStreamThenQuit`,
   skipped for `ProcessExit` (§4.6). The same gate applies at the analogous reviewer launch site.

Every other surface (tmux substrate, worktree mgmt, commit-detection, merge, queue/dispatch, the DOT
cascade and review-loop control flow) MUST remain shared and harness-blind per HN-010. A grep for
harness dispatch MUST show one launch-dispatch site and one completion-gate site — never an
`if <harness>` branch in shared infrastructure.

Tags: mechanism

#### HN-020 — Shared scaffolding stays in the shared caller

The shared scaffolding side effects — worktree-trust seeding, the `agent-task.md` write, harness
settings materialization, and pre-exec messages — MUST fire from the shared caller, NOT from any
harness's `LaunchSpec` (HN-003). This is what lets a new harness reuse the scaffolding identically
without re-implementing it. A claude run's scaffolding side effects MUST be byte-for-byte unchanged
from the pre-contract behavior (side-effect parity).

Tags: mechanism

### 4.8 Auth / billing boundary

#### HN-021 — Every harness presents a credential-isolated billing boundary

Every harness MUST present a credential-isolated billing boundary: the credential guard of HN-006
strips the harness's API-credit-pool credentials so the default billing path is the subscription /
managed path, never an inherited API key. A harness MUST NOT silently bill an API credit pool when a
subscription path is available. The strip is the floor; a harness MAY add belt-and-suspenders config
and pre-flight guards (HN-022 is codex's normative instance).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### HN-022 — Codex MUST run the ChatGPT-subscription billing path

Codex MUST run on the ChatGPT-subscription path, never the OpenAI API credit pool, by default.
Because `codex exec` env-var precedence is undocumented and version-variable, codex MUST apply
defense-in-depth — all of:

- **B1.** Strip `OPENAI_API_KEY` and `CODEX_API_KEY` from the codex child env (the HN-006 instance).
- **B2.** Materialize or verify `forced_login_method = "chatgpt"` in `$CODEX_HOME/config.toml`
  (idempotent; other keys preserved).
- **B3.** Pre-flight assert: run `codex login status` BEFORE the first task turn and fail the run
  closed (emitting a `codex_billing_guard` event) if it does not report a ChatGPT plan.
- **B4.** Set `$CODEX_HOME` deterministically to a stable writable path; do not trust an inherited
  value (B3 is the backstop for a logged-out inherited home).

`codex login status` being unparseable, codex not being installed, or `$CODEX_HOME` not being writable
MUST each fail the run closed (a `codex_billing_guard` / `codex_not_available` event) rather than
proceed on the assumption of a subscription.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

> INFORMATIVE: B1–B4 are the guards harmonik can enforce in code. They do NOT by themselves prove that
> a given codex version honors them; the empirical confirmations (does the pinned `codex exec` honor
> the env strip and `forced_login_method`? is there a "Codex CLI (auto-generated)" org key billing the
> API pool? does the codex reviewer reliably emit the structured verdict?) are MUST-TEST items that
> gate enabling codex in production and live in the codex adapter and migration specs, not here.

### 4.9 Back-compat

#### HN-023 — All harness-contract additions are additive

Every addition this contract introduces — the `Harness` interface, `core.AgentTypeCodex`, the
`harness` / `reviewer_harness` DOT attributes, the `harness:<x>` bead label, the per-queue `harness`
field, and the `Config.DefaultHarness` / `Config.CodexBinary` config keys — MUST be additive. No
existing field is renamed or removed. `Config.HandlerBinary` keeps its claude semantics (it continues
to name the claude binary); a codex binary path is a codex-adapter concern (default `codex`,
overridable via `Config.CodexBinary`).

Tags: mechanism

#### HN-024 — A no-selection run is byte-identical to the pre-contract claude path

A run with no harness selector MUST resolve to `claude` and produce byte-identical launch behavior to
the pre-contract dispatch path. This MUST be verifiable by (a) a golden test on the pure `LaunchSpec`
return — the claude harness's `(argv, env, cwd)` byte-identical to the pre-refactor builder — and (b) a
side-effect-parity test on the shared scaffolding (HN-020). The claude harness's completion mode MUST
be `EventStreamThenQuit` and its session-id policy MUST be `Minted` (the N-1 anchor of §4.4).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

## 5. Invariants

#### HN-INV-001 — The git layer is the sole completion authority

Across all harnesses and all completion modes, "done with work" MUST be decided by the shared git layer
(worktree HEAD ≠ parent AND a `Refs:<beadID>` trailer) per HN-009. No harness-internal signal — process
exit, a terminal JSONL event, a `/quit` acknowledgement — may substitute for the git check. Any path
that marks a run done without the git check violates this invariant.

Tags: mechanism

#### HN-INV-002 — Per-harness branching exists only at the two seam points

Every per-harness behavioral difference MUST be expressed through one of the eight `Harness` interface
members or consulted at one of the two seam points of §4.7. No `if <harness>` branch may appear in the
shared substrate, worktree, merge, queue, cascade, or review-loop code. A private per-harness branch
anywhere in shared infrastructure is a structural invariant violation.

Tags: mechanism

#### HN-INV-003 — No live API credential reaches a harness child by default

For every harness, the resolved child env MUST NOT carry a live API-credit-pool credential by default;
the credential guard of HN-006 strips it and re-emits an empty override. Any launch path that lets an
inherited API key reach the child (via the substrate's additive env injection or any other path)
violates this invariant.

Tags: mechanism

## 6. Schemas and data shapes

### 6.1 The Harness interface (Go shape)

```
INTERFACE Harness:
    AgentType()        : core.AgentType                          -- identity; registry key
    LaunchSpec(rc)     : (LaunchSpec, error)                     -- pure spawn descriptor (HN-003)
    Seed(sess, rc)     : error                                   -- first-turn task (HN-004)
    Retask(sess, fb, rc) : error                                 -- iteration >= 2 feedback (HN-004)
    Teardown(sess)     : error                                   -- end session (HN-018)
    DetectReady(ev)    : bool                                    -- harness event -> agent_ready (HN-005)
    SessionIDPolicy()  : SessionIDPolicy                         -- {Minted, Captured} (HN-008)
    Completion()       : CompletionMode                          -- {EventStreamThenQuit, ProcessExit} (HN-007)
```

```
ENUM SessionIDPolicy:
    Minted     -- caller mints a UUIDv7 before launch (claude)
    Captured   -- harness reports its own session id after launch (codex thread_id)
```

```
ENUM CompletionMode:
    EventStreamThenQuit  -- harmonik quits the harness; /quit + grace + kill liveness (claude)
    ProcessExit          -- harness self-terminates; sess.Wait + commit hard ceiling liveness (codex)
```

### 6.2 Harness-selection inputs

```
RECORD HarnessSelectors:
    BeadLabel     : String   -- the bead's `harness:<x>` label (highest precedence; "" if absent)
    QueueDefault  : String   -- the per-queue `harness` default ("" if absent or tier unsupported)
    NodeAttr      : String   -- the DOT node `harness` attribute ("" if absent)
    GlobalDefault : String   -- Config.DefaultHarness (default "claude")
```

`ResolveHarness(HarnessSelectors) -> (core.AgentType, error)` applies the §4.4 precedence and the
fail-closed error rules; absent all four tiers it returns `claude`.

### 6.3 Per-harness contract instances

| Property | `claude` (`AgentTypeClaudeCode`) | `codex` (`AgentTypeCodex`) |
|---|---|---|
| `SessionIDPolicy()` | `Minted` (caller-minted UUIDv7) | `Captured` (`thread_id` from first `thread.started`) |
| `Completion()` | `EventStreamThenQuit` | `ProcessExit` |
| Stripped credential vars | `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN`, `CLAUDE_CODE_OAUTH*` | `OPENAI_API_KEY`, `CODEX_API_KEY` |
| `Teardown` | `/quit` + grace + kill | no-op (process self-terminates) |
| Iteration ≥ 2 process | fresh `claude --resume` process | fresh `codex exec resume <thread_id>` process |
| Billing boundary | subscription / managed (credential-stripped) | ChatGPT subscription (HN-022 defense-in-depth) |

The per-harness launch shape (argv, flags, JSONL event grammar) is owned by the per-harness adapter
specs and is out of scope here.

## 7. Conformance

A `Harness` implementation conforms to this contract when:

1. It implements all eight interface members of §4.1 and registers under a unique `core.AgentType`
   (HN-001, HN-013).
2. Its `LaunchSpec` is a pure spawn descriptor with no shared-scaffolding side effects (HN-003), and
   its `env` strips the harness's credential-var set with empty overrides (HN-006, HN-INV-003).
3. Its `SessionIDPolicy()` and `Completion()` are honored by the shared loop at the two seam points
   only (HN-002, HN-007, HN-008, HN-INV-002).
4. Completion is decided by the shared git layer, never by harness internals (HN-009, HN-INV-001).
5. It introduces no per-harness branch in shared infrastructure outside the two seam points (HN-010,
   HN-019, HN-INV-002).
6. For the default (`claude`) harness, a no-selection run is byte-identical to the pre-contract path —
   a golden `LaunchSpec` test and a side-effect-parity test both pass (HN-024).

A new harness additionally MUST declare its credential-var set (HN-006), its session-id and completion
policies (HN-002), and — if it bills against an API credit pool — a subscription-path billing posture
analogous to codex's HN-022.
