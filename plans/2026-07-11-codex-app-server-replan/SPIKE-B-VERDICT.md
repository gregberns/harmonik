# Spike B verdict — "can Codex actually orchestrate?"

**Date:** 2026-07-18 · **Crew:** yankee (Codex lane) · **Epic:** hk-q3ovr · **Verdict: 🟢 PASS (base + stretch)**

## Question
Can Codex, driven via the `codex app-server` harness + `internal/codexdriver`, drive a real crew task
end-to-end — receive a mission, take turns, run tools, produce a reviewable commit — headlessly?

## Method
Throwaway harness (`scratchpad/spikeb/harness/main.go`, stdlib-only, modeled on the L3 live test)
drove `codex app-server` (codex-cli 0.144.5) over JSON-RPC NDJSON in an **isolated scratch git repo**
(never the harmonik tree — shared-index race). Handshake `initialize → initialized → thread/start →
turn/start`, headless config `approval_policy=never`. Mission: *"create ORCHESTRATED.md, `git add`,
commit, print hash."* Then a **second `turn/start` on the same thread** to test multi-turn resident context.

## Result — PASS
- **Turn 1** → real shell exec (`item/commandExecution/*` notifications) → commit
  `spike-b: codex-orchestrated commit` (ORCHESTRATED.md +1).
- **Turn 2, same thread** → saw turn-1's file, appended a line, re-committed
  `spike-b: turn-two amend` (clean +1 diff). **Multi-turn resident context works — no re-seed.**
- Working tree clean, no stray branches, exact requested commit messages, reviewable via `git show`.

## Load-bearing finding — the real gate is SANDBOX MODE, not the wire
| sandbox_mode | tool-exec | multi-turn | commit lands? |
|---|---|---|---|
| `workspace-write` | ✅ | ✅ | ❌ — `.git` writes sandboxed out; file written but **untracked**, no commit. Silent no-op output. |
| `danger-full-access` | ✅ | ✅ | ✅ — commit lands. PASS. |

- With `approval_policy=never` the server **never sent** a client approval request, so
  `codexdriver.handleServerRequest`'s `-32601` auto-decline (`session.go:861`) was **not exercised**
  in this path. The **sandbox**, not the approval reply, is what gates real work here.
- Auto-decline still matters under any policy that DOES prompt (untrusted/on-request) — there it would
  block every exec/apply-patch.

## Implication for the product path
To run real crews on `codexdriver`, the spawned app-server MUST be configured
`sandbox_mode=danger-full-access` (external-sandbox assumption) **and** `approval_policy=never`
(or implement approval negotiation). Today `codexdriver.Options.Args` defaults to just `["app-server"]`
(`driver.go:121`) — **no sandbox/approval flags**. A crew launched as-wired would have tools declined
and/or commits sandboxed out. → gap bead below.

## Remaining-work list (for captain → admiral reconciliation)
The originally-named Phase-2 beads (hk-q3ovr / hk-nzzos / hk-l63b9) are CLOSED **administratively**
(2026-07-12 freeze-and-carve), NOT shipped. Genuine gaps, all NEW (no reopen):

1. **Sandbox/approval config in codexdriver** *(new, from Spike B)* — `Options` must set
   `sandbox_mode=danger-full-access` + `approval_policy=never` (or negotiate approval). Without it,
   headless crews produce no reviewable commits. Smallest, highest-leverage next step.
2. **Persistent supervised sidecar** (hk-nzzos residual, NOT built) — codexdriver owns ONE child per
   worker turn; no resident multi-turn client, no reconnect / `thread/resume` / backpressure / watchdog.
   This is the resident-orchestrator gap and the design's "real cost."
3. **Crew-start harness routing** (hk-l63b9 residual, NOT built) — `HARMONIK_SUBSTRATE` is a
   composition-root *worker* switch, not a per-crew resolver. `harmonik crew start` cannot select Codex
   per crew (flag → mission front-matter → per-crew config → default).

## Reproduce
`SPIKE_REPO=<scratch-repo> go run .` from `scratchpad/spikeb/harness/` (set sandbox_mode in `main.go`).

---

# Design (DESIGN-ONLY — 2026-07-18, admiral gate pending; NOT committed)

Captain mode after admiral ruling: hk-5h759 / hk-160yb / hk-f8wtm are **design-only**; the
danger-full-access sandbox posture is a security decision coupled to worktree-confinement that the
admiral will likely loop the operator on. BUILD is held. The hk-5h759 prototype diff is preserved,
uncommitted, at `hk-5h759-prototype.patch` (working tree reverted clean to protect the shared index).

## hk-5h759 — headless sandbox/approval posture — REVISED after independent review (BLOCK)

**The naïve fix does not reach production.** An independent review (agent-reviewer, BLOCK) found:

- `codexdriver.Options.Args` default is consulted **only** when `SubstrateSpawn.Argv` is empty
  (`driver.go:136-142`). Both production spawn sites pass a **non-empty** `Argv`:
  `internal/handler/handler.go:394-403` (bead dispatch) and `internal/daemon/crewstart.go:290-337`
  (crew launch). So editing the default is inert for the daemon.
- More fundamentally: the **live crew/worker dispatch path does not use the app-server driver at all.**
  `buildCodexLaunchSpec` (`internal/daemon/codexlaunchspec.go:170-240`) emits
  `codex exec --json --sandbox workspace-write …` — the **exec** streaming protocol, not app-server
  JSON-RPC. `codexdriver` (app-server) is reachable today only via `HARMONIK_SUBSTRATE=codexdriver`
  on the worker-substrate axis, whose argv also comes from a LaunchSpec, not `Options.Args`.

**Design consequence (dependency order):** headless sandbox posture is only meaningful **once crew
dispatch is actually routed through the app-server driver.** So the real order is:
  1. **hk-f8wtm** (+ sidecar) route crew dispatch to the app-server driver, emitting `app-server` argv
     into the `LaunchSpec` that reaches `SubstrateSpawn.Argv`.
  2. **hk-5h759** then adds `-c approval_policy="never" -c sandbox_mode=<posture>` **to that
     LaunchSpec argv** (in `codexlaunchspec.go` / the app-server harness), NOT to
     `codexdriver.Options.Args`. The `Options.Args` default can stay a safe fallback.

**Safety gap the review surfaced (must be in the operator loop):** `codexWorkerRoutingRunner.Command`
(`substrate_select.go:117-130`) **falls back to `ltmux.LocalRunner{}`** when no worker is
bound/enabled/ssh — i.e. a `danger-full-access` codex would run **directly on the daemon host with no
container**. The "external sandbox" assumption is therefore NOT guaranteed by code. Posture options for
the admiral/operator decision:
  - `danger-full-access` **only** on a remote/containerized runner; **never** on the local-runner
    fallback (scope the flag to the transport), OR
  - `workspace-write` + explicitly widen the writable set to include `.git` (if codex supports it), OR
  - run crews in an externally-enforced sandbox and assert it at launch (fail closed if unconfirmed).

**Approval:** `approval_policy=never` is orthogonal to sandbox and low-risk — it just avoids the
`-32601` auto-decline stall (`session.go:861`); safe to land independent of the sandbox decision.

### Admiral Phase-2 gate ruling (2026-07-18) — hk-5h759 REDESIGN to FAIL CLOSED

The admiral's code-grounded review confirmed the fail-open hazard: `selectSubstrate`
(`substrate_select.go:52`) + `codexWorkerRoutingRunner.Command` (`:117-130`) **silently fall back to
`ltmux.LocalRunner{}`** when no ssh worker is bound. With `danger-full-access` + `approval_policy=never`
that is **unsandboxed FS + net + exec on the daemon host**. A code comment (`driver.go:132-141`) is NOT
enforcement. **hk-5h759 stays design-only** until the operator rules on the fail-closed posture **and a
token ceiling.** Required redesign:

- **FAIL CLOSED, enforced in code:** refuse to spawn a codex crew when the resolved posture is
  `danger-full-access` **and** no worker/container boundary is bound. Never a silent LOCAL fallback.
- **Enforcement layer = admission, not exec.** `codexWorkerRoutingRunner.Command` returns `*exec.Cmd`
  with no error surface, so the check cannot live there cleanly. Put a **pre-spawn admission guard** in
  the codex launch/harness path (where posture is resolved → the `LaunchSpec`): if posture requires an
  external sandbox and the selected transport is not a real boundary (not ssh worker / not container),
  **reject the dispatch with a clear error** (fail closed) before any process starts. The requirement
  is ENFORCED, not documented.
- **Token ceiling:** operator-set cap wired into the codex launch env/config before any live run
  (guards runaway resident turns). Value TBD by operator.

### Topology (admiral): default PER-CREW app-server

Default is **one app-server per crew** (isolation; matches the worktree-per-crew invariant). A
**shared** app-server across crews is NOT assumed — **Spike B did NOT prove multi-crew concurrency.**
Before any shared-server design, run a small **concurrency micro-spike** (N crews, N threads/servers,
prove no cross-talk / turn interleave / thread-id mixups). That micro-spike gates any shared topology.

## hk-160yb — persistent supervised sidecar (BUILD HELD) — design sketch

Gap: `codexdriver` spawns/owns ONE app-server child per worker turn; a resident crew orchestrator needs
a long-lived JSON-RPC client that survives many turns + keeper restarts. Design surface:
- **Resident client:** one `initialize`→`thread/start` per crew session; hold `thread_id`; each wake
  (comms/queue/timer) folds a delta into a `turn/start` on that thread (no re-seed — Spike B proved
  multi-turn resident context holds).
- **Reconnect / restart:** on child death or keeper restart → respawn app-server → `initialize` →
  `thread/resume <thread_id>` → replay comms/queue events since last-seen cursor.
- **Backpressure:** one-turn-in-flight (mirror `codexreactor` I1) + bounded input queue; reject/queue
  wakes arriving mid-turn.
- **Watchdog:** liveness predicate = output-or-stale within a window (reuse AIS-INV-001 shape); ungraceful
  kill handling can reuse `codexwalguard.go`. Supervisor revives like the daemon watchdog.

**Acceptance gate (admiral, mandatory):** when the hk-160yb BUILD is eventually greenlit it MUST ship a
**pre-deploy ISOLATED E2E test** as its acceptance criterion — exercising the hk-5h759 headless flag +
the reconnect / resident-multi-turn paths against a real `codex app-server` in an isolated scratch repo
(the Spike-B harness is the seed for this). No merge without that green.

## hk-f8wtm — crew-start harness routing (design-eligible; don't build ahead of gate) — design sketch

Gap: `HARMONIK_SUBSTRATE` is a composition-root worker switch; there is no **per-crew** harness resolver.
Design: add `Harness` to `CrewStartRequest` (`crewstart.go:~73`); resolve precedence
**flag → mission front-matter → per-crew config → default** in `cmd/harmonik/crew.go:resolveCrewStartArgs`
(~:133); the resolved harness selects the app-server LaunchSpec (feeds hk-5h759's argv). Substrate-neutral
seam — the daemon already routes input via `handler.AsInputPort` with no daemon-side change.

**Track-A fence (admiral):** the **keeper-retirement** aspect of hk-f8wtm (using the resident
server-side context to retire ~70–80% of keeper machinery, per `keeper-verdict-design.md`) stays
**design-only behind crew xray** (Track-A owner). yankee designs the harness-routing seam; the
keeper-retirement decision is not yankee's to build ahead of xray.
