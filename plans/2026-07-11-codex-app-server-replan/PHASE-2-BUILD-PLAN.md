# Codex Phase-2 — BUILD PLAN (yankee, GREEN-LIT 2026-07-18)

Operator decisions in; BUILD is GO. Un-held: **hk-160yb** (persistent supervised sidecar) +
**hk-5h759** (fail-closed headless posture). Acceptance gate on BOTH = **hk-g0ror.4** (isolated E2E).

## Binding acceptance posture (operator, non-negotiable)
1. **Sandboxed AND commits land.** Codex runs INSIDE a real isolation boundary (worker/container **is**
   the sandbox); its commits land INSIDE that boundary. Never unsandboxed naked-host exec.
2. **FAIL-CLOSED.** Refuse to spawn a codex crew when no worker/container boundary is bound. Never the
   `substrate_select.go:52` LOCAL fallback under a permissive sandbox posture. Enforced in code.
3. **Token ceiling: DROPPED** (operator on subscription).
4. **Topology: PER-CREW app-server** default. A concurrency micro-spike gates any shared-server idea.
5. **hk-f8wtm keeper-retirement:** design-only behind crew **xray** (Track-A).

## Grounding (what's really there)
- The live codex WORKER path is `buildCodexLaunchSpec` → `codex exec --json --sandbox workspace-write`
  (`codexlaunchspec.go:170-245`) — single-turn, NOT the app-server sidecar.
- The **app-server** path is `codexdriver` (`codex app-server`, JSON-RPC), worker-routed via
  `codexWorkerRoutingRunner` (`substrate_select.go:88-131`), which **falls back to LocalRunner** when no
  enabled ssh worker is bound → the fail-open hazard.
- **Boundary predicate (exists today):** `reg.WorkerSnapshot()` returns a worker with
  `Enabled && Transport=="ssh"`. That enabled ssh worker IS the isolation boundary. No worker ⇒ no boundary.
- **Fail-closed idiom (reuse):** `runCodexBillingGuard` already does exactly this shape — a pre-flight
  assert that returns an error and refuses to return a LaunchSpec (`codexlaunchspec.go:~232`). The
  sandbox-boundary guard mirrors it.

## Build sequence

### C1 — hk-5h759: fail-closed sandbox-boundary guard + headless posture *(foundational, first)*
- **Boundary guard (admission):** before spawning a codex crew (app-server sidecar) with the permissive
  sandbox posture, assert an isolation boundary is bound (enabled ssh worker / container). If not →
  return an error, refuse to spawn. Mirror `runCodexBillingGuard`'s fail-closed return. NEVER fall
  through to LocalRunner under the permissive posture.
- **Posture flags:** on the app-server LaunchSpec/Options for a crew: `approval_policy="never"`
  (avoids the `-32601` auto-decline stall) + the sandbox mode that lets commits land *inside the
  boundary* (`danger-full-access`, safe ONLY because the boundary is the sandbox — guaranteed by the
  guard above). `approval_policy=never` is orthogonal + safe.
- **Acceptance criterion (explicit):** "runs sandboxed AND commits land" + "fail-closed refusal with no
  bound boundary" — both asserted by hk-g0ror.4.
- Files: guard in the codex crew launch/harness admission path; posture wired onto the app-server argv
  (NOT `codexdriver.Options.Args`, which production never reads — see SPIKE-B-VERDICT §hk-5h759).

### C2 — hk-160yb: persistent supervised sidecar *(the bulk)*
- **Resident client:** one `initialize`→`thread/start` per crew; hold `thread_id`; each wake folds a
  delta into `turn/start` on that thread (Spike B proved multi-turn resident context).
- **Reconnect / restart:** child death or keeper restart → respawn → `initialize` →
  `thread/resume <thread_id>` → replay comms/queue events since last-seen cursor.
- **Backpressure:** one-turn-in-flight (mirror codexreactor I1) + bounded input queue.
- **Watchdog:** output-or-stale liveness (AIS-INV-001 shape); ungraceful-kill handling reuses
  `codexwalguard.go`; supervisor revives like the daemon watchdog.

### C3 — hk-g0ror.4: pre-deploy ISOLATED E2E acceptance test *(the gate on C1+C2)*
- Isolated scratch repo (seed from `scratchpad/spikeb/harness/`). Asserts: (a) codex runs sandboxed AND
  a commit lands inside the boundary; (b) fail-closed refusal when no boundary bound; (c) reconnect /
  resident-multi-turn paths. No merge of hk-5h759 or hk-160yb without this green.

## Proposed partition (for a 2nd Codex crew, if captain adds one)
Clean seam between security-admission and sidecar-mechanics:
- **yankee:** C2 sidecar core (resident client, thread lifecycle, reconnect/resume, backpressure,
  watchdog) + C3 E2E integration.
- **2nd crew (proposed):** C1 fail-closed guard + posture (self-contained, security-critical, testable
  in isolation) — no overlap with the sidecar internals; they meet only at the launch-spec seam.
- Will tell captain when the C2 interface is stable enough to hand C1 off cleanly.

## Process
INLINE (daemon worktree-dispatch broken). Per committed change: independent reviewer sub-agent →
captain gates → commit EXPLICIT PATHS only (never `-A`/`.`), bead id in subject. Daemon owns terminal
bead transitions. Post progress + first commits to captain on `--topic status`.
