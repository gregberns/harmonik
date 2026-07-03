# Spec draft — Pi + OpenRouter Harness

> Codename pilot · Epic hk-ag97p · Normative build contract (draft, pre-review).
> Companion design: `04-design/pi-harness-design.md`. Requirement IDs use the `PI-` prefix.
> NORMATIVE language: MUST / MUST NOT / SHOULD / MAY per RFC 2119.

## §0 Scope & phasing

- **PI-001** The work MUST land in three independently-shippable phases, each gated on the prior
  proving out: Phase 0 (per-bead harness), Phase 1 (one crew via a resident Pi harness — NOT a thin
  "shim"; spike-gated), Phase 2 (crew-launch provider abstraction). Phase N+1 MUST NOT begin until
  Phase N is proven on real beads.
- **PI-002** Phase 0 MUST NOT modify the harness-blind shared loop except at the two declared seam
  points (registry/launchSpec lookup; the `Completion()` gate). Any third branch is a spec violation.

## §1 Phase 0 — harness conformance

- **PI-010** A `PiHarness` MUST implement all 8 `handlercontract.Harness` methods. `AgentType()` MUST
  return `core.AgentTypePi`. It MUST register in `newHarnessRegistry`; a `PiAdapter` MUST register at
  startup (mirroring `RegisterCodex`).
- **PI-011** `Completion()` MUST return `CompletionProcessExit`.
- **PI-012** `SessionIDPolicy()` MUST return `SessionIDCaptured`. The harness MUST capture the session
  id from the `id` field of Pi's first NDJSON line `{"type":"session",…}` and pass it on the resume
  turn as `--session <id>`. The `session`/`agent_end` event shapes are asserted from docs and carry a
  **confirm-by-test obligation** in Phase 0 (findings.md §2).
- **PI-012a (forced-exec substrate — load-bearing)** Pi MUST inherit codex's `SessionIDCaptured`
  forced-exec posture: the launch path MUST force `implSpec.Substrate = nil` (`reviewloop.go:367–383`)
  so the NDJSON `StdoutWrapper` is actually invoked. If Pi runs on the tmux substrate (`Stdout()==nil`)
  both session-id capture (PI-012) and the `agent_end` watcher (PI-014) silently no-op.
- **PI-013** `DetectReady` MUST return `false` for `launch_initiated` (HC-041) and MUST NOT synthesize
  ready-state from any non-`agent_ready` signal.
- **PI-014 (agent_end watcher — load-bearing)** Because Pi's process exit is unreliable
  (upstream #4303/#161/#4942), the Pi run path MUST observe the run's NDJSON event stream and, on the
  terminal **`agent_end`** event, invoke `Teardown`→Kill. The run's completion MUST NOT depend on Pi
  self-exiting; the 90-minute `commitHardCeiling` is a backstop only. The watcher MUST **extend the
  existing per-harness `StdoutWrapper`/`SessionIDInterceptor`** assigned in shared launch code
  (`reviewloop.go:430`, gated on `implIsSessionIDCaptured`) — it MUST NOT add a *new* shared-loop
  branch, and MUST NOT be described as "outside the shared loop" (it rides the SessionIDCaptured hook
  codex established). Depends on PI-012a.
- **PI-015** The harness MUST NOT pass a `--sandbox` flag (Pi is unsandboxed). The seed prompt MUST
  instruct Pi to read `.harmonik/agent-task.md`, implement, and commit with a `Refs: <bead-id>`
  trailer.

## §2 Launch spec & environment

- **PI-020** Initial argv MUST be `pi --mode json --provider <prov> --model <prov/id> "<seed>"`;
  resume argv MUST be `pi --mode json --session <id> "<feedback>"`. WorkDir MUST be the run worktree.
  `StdinDevNull` MUST be `true`. The key MUST NOT be passed as `pi --api-key <value>` (ps/argv leak) —
  env injection only.
- **PI-021 (allowlist strip)** `buildPiEnv` MUST empty-override (`KEY=`) **every** provider credential
  env var matching a maintained provider-key table / `*_API_KEY` pattern **except** the selected
  `api_key_env` (an enumerated denylist is incomplete against Pi's open provider set — review B1), and
  MUST inject **only** the selected provider's key. The key VALUE MUST come from the operator
  environment, never from config. A single shared key-resolution helper MUST feed both `buildPiEnv`
  and the guard (PI-040) so they cannot disagree.

## §3 Commit fallback

- **PI-030** `ensurePiRefsTrailer` MUST guarantee a `Refs: <bead-id>` trailer using the codex decision
  table (no-op / amend / commit / no-change) and MUST NOT fabricate a commit on a clean worktree.
- **PI-031 (remote-safe)** Every git operation in the fallback MUST route through the run's `runner`
  when non-nil (and fall back to local `exec` when nil), so the remote SSH substrate works. It MUST be
  gated at the existing `Completion()==ProcessExit` seam.

## §4 Billing/auth guard (fail-closed)

- **PI-040** Before launch, the guard MUST assert the env var named by the resolved `api_key_env` is
  present and non-empty; if absent/empty, it MUST refuse to launch (return a typed error before
  `agent_ready`). It MUST NOT fall back to any default provider/model/key. Guard events and error
  `Reason` strings MUST name the env-var **name, never its value**. The `skipBillingGuard` test-escape
  MUST be `false` in production, asserted by a wiring test.
- **PI-041** The guard MUST strip all non-selected provider keys (per PI-021) so a mis-set env cannot
  bill a provider other than the configured one.
- **PI-042 (on-disk credential)** The guard MUST either (a) establish-and-cite that Pi persists **no**
  on-disk credential surviving the env strip, or (b) add a disk-state assertion mirroring codex's
  `authIndicatesAPIKeyLogin`. Until (a) is confirmed (findings.md §4, UNCONFIRMED), (b) is required.
- **PI-043 (no silent claude fallback)** A Pi config/guard failure MUST yield `run_failed` + bead
  reopen, NEVER a silent re-route to claude. A `harness:pi` label resolves hard at tier-1, so the
  tier-4 claude fallback (`harnessresolve.go:109–117`) cannot fire on a Pi config failure.

## §5 Configuration (no hardcoded defaults)

- **PI-050** Config MUST add a top-level `harnesses.pi` block with REQUIRED `provider`, `model`,
  `api_key_env` and OPTIONAL `fallback{provider,model,api_key_env}`. The product MUST NOT bake any
  default provider, model, or key.
- **PI-051** `ResolvePiConfig` MUST aggregate **all** missing required keys into one error, refuse to
  start, name the dotted yaml paths, and point at `harmonik pi config --example` (mirroring
  `ResolveKeeperConfig`).
- **PI-052** `model` MUST be validated by shape only (HC-055a: `^[A-Za-z0-9._:/-]+$`, ≤128 chars),
  never against a curated value enum; Pi's full provider/model range MUST be selectable.

## §6 Selection & fence

- **PI-060** A `harness:pi` label (tier-1) MUST select the Pi harness; tier-4 `daemon.default_harness:
  pi` MUST select it globally. An unregistered/unknown selector MUST hard-error (no silent claude
  fallback).
- **PI-061** Operator discipline (NOT code) MUST fence Pi to mechanical, deterministically-checkable
  beads via a dedicated `pi` queue/lane; the DOT test+review gate MUST remain the correctness check.

## §7 Rate-limit & failure handling (paid-first)

- **PI-069 (production substrate = paid)** The unattended-fleet production substrate MUST be a PAID
  provider/model (operator config). **Free OpenRouter is an explicitly-labelled, hand-attended
  *experiment* lane only** — the design MUST NOT hinge on free-tier viability. A worker/concurrency
  cap MUST NOT be claimed to bound per-request rate (harmonik has no per-request throttle); an in-run
  token-bucket throttle is OUT of V1 scope and MUST be named as such if referenced.
- **PI-070 (fail-loud queue cap)** Pi beads MUST run on a dedicated named queue with an **explicit**
  `Workers` cap. If `Workers` is unset the queue silently inherits global `max_concurrent`
  (`DefaultWorkers`, `queue/rpc.go`) → multiplied request rate; the Pi queue therefore MUST require an
  explicit cap and **fail loud if absent**. Pi MUST NOT share the high-concurrency main path.
- **PI-071** `adapter_pi.DetectRateLimit` MUST read the 429/404 signal from Pi's NDJSON
  (`auto_retry_*`/error events — UNCONFIRMED channel, MUST be confirmed before implementing,
  findings.md §7). It MUST classify a 429 by retry-after magnitude: a **minute-window** 429 → backoff
  + retry with delay coupled to retry-after (NOT immediate); a **day-window** 429 (retry-after = hours)
  → fail the run fast, MUST NOT idle to the 90m ceiling → escalate. **If a retry-after magnitude is
  unavailable** (Pi swallows the status; the inference path recovers only "a 429 happened," not the
  window), the classifier MUST safely degrade: treat ALL 429s as fail-fast → escalate. 404/"no
  endpoints" → transient → re-submit once → escalate. A bead failing twice MUST NOT be re-dispatched
  without captain escalation.
- **PI-072 (honest fallback)** The `fallback:` config block MUST exist, but **V1 has NO automatic
  fallback** — on free-cap exhaustion the operator flips the lane to the paid provider; the free lane
  therefore strands work until a human acts and is hand-attended only. The spec MUST state this plainly
  (not "mandatory paid fallback"). Auto-fallback is a named follow-on, OUT of V1.
- **PI-073 (global-tuner isolation — load-bearing)** Pi's rate-limit signal MUST be isolated from the
  global `bandwidthtuner` (`NotifyRateLimit()` snaps global `max_concurrent` to 1). A free-tier Pi 429
  MUST NOT throttle the paid Claude fleet; Pi rate-limit handling MUST use per-queue backoff only.

## §8 Phase 1 — crew shim — **DESIGN SPIKE REQUIRED (PI-080/081 are GOALS, not yet normative)**

> Per the crew-shim review, Phase 1's premise is unverified and three leaned-on subsystems break on a
> non-claude pane. PI-080/081 below are **goals** held until a Phase-1 design spike resolves the named
> unknowns (PI-085). They MUST NOT be treated as a build contract until the spike completes.

- **PI-080 (goal)** The crew binary accepts `--dangerously-skip-permissions --remote-control <label>
  (--session-id <uuid> | --resume <uuid>)` + OPTIONAL `--model <m>`, and is a **resident interactive
  harness** (one process across many turns) — NOT a per-turn exec, and NOT a thin "shim" (it is a new
  ~Claude-Code-equivalent interactive program; the `--remote-control` protocol is the easy part).
- **PI-081 (goal)** It seeds from the bracketed-paste mission line, runs the full crew operating loop
  (claim/mirror-assignee/dispatch/monitor/status-cadence/triage/re-hydrate) driving `br`/`comms`/
  `queue` via Pi's `bash` tool, and honors comms `park` + pane-wake.
- **PI-082** The Phase-1 pilot MUST be a single narrow mechanical lane (gurney; 2 deterministic
  beads), understood as a **spike datapoint** — it proves "weak model + short queue," NOT "weak model
  can run a crew" (the orchestration-judgment risk is not de-risked by bead-mechanicalness). The
  captain and all judgment/design lanes (paul, stilgar, admiral, irulan) MUST remain Claude.
- **PI-085 (spike unknowns — MUST resolve before PI-080/081 become normative)** The spike MUST resolve:
  (1) whether `pi --mode rpc` is a resident multi-turn server (else per-turn `--mode json --session`
  with state rebuild — UNVERIFIED, findings.md); (2) the keystroke→Pi→pane translator design;
  (3) a context-fill trigger WITHOUT a Claude gauge — the keeper is **blind on a non-claude pane**
  (its hooks read Claude's context %; its restart pastes `/clear`+`/session-resume`), and
  `probeKeeperLiveness` (`crewstart.go:608`) masks the blindness by checking only the flock — so the
  harness needs its own token-tracking + self-restart and `/clear`/`/session-resume` handling;
  (4) a post-spawn **shim-liveness probe** (crew-start returns success with no readiness check);
  (5) `HandlerBinary` config-wiring (`daemon.go:116–123`) — Phase 1 depends on this Phase-2 capability,
  resolved via a narrow global-binary override or by moving the wiring earlier.

## §9 Phase 2 — crew-launch provider abstraction (gated on Phase 1)

- **PI-090** Crew launch MUST be routed through a provider/harness abstraction resolved by the same
  tier mechanism as the per-bead path, replacing the hard-coded `claude` at `crewlaunchspec.go:100`
  and `captain.go:208`. The binary MUST be selectable per-crew/per-lane (not a global swap). The
  captain MUST remain Claude unconditionally.

## §10 Tests (acceptance)

- **PI-100** Unit tests MUST cover: argv (initial/resume); env strip+inject; HC-041 DetectReady;
  session-id capture; the `agent_end` watcher firing Teardown under a simulated non-exit hang;
  `ensurePiRefsTrailer` incl. the runner-routed remote path; `pibillingguard` fail-closed on missing
  key; `ResolvePiConfig` aggregating all missing keys.
- **PI-101** A prose conformance scenario (old-bench house style) MUST exist for: "Pi claims a
  mechanical bead, implements, commits with a `Refs:` trailer, the DOT gate passes, the daemon
  merges and closes the bead — with the configured provider and a fail-closed guard."
</content>
