# Quality-System — PROGRAM MAP (multi-subsystem test-validation)

*Planning subagent → admiral · 2026-07-06 · answers the operator's "can another crew build the keeper /
comms / and-more test systems in parallel while the daemon-dispatch system is built?"*

> **Bottom line up front.** YES — and it is not a close call. The daemon-dispatch proof (Phase 1, crew
> alia) is the ONE lane that needs the heavy shared substrate (scratch-daemon + digital-twin seam + the
> Layer-0 Docker + the coreloop assertion library). **Keeper and comms are already fully seam-wrapped and
> carry their own LIGHTER, in-repo harnesses that do NOT depend on that substrate at all.** So a second
> crew can start building keeper + comms + several "supporting subsystem" proof-harnesses tonight, entirely
> off the daemon-dispatch critical path. The only things genuinely GATED on the shared substrate are the
> subset of behaviors that are *emergent from a live daemon driving a live agent spawn* — and those are the
> minority.

---

## 1. Subsystem inventory — what needs its own test-validation coverage

Every distinct harmonik subsystem that carries production incidents and therefore earns a dedicated
"prove-this-whole-class-works" harness. Each already has substantial `_test.go` coverage — the gap is not
"zero tests," it is **a repeatable acceptance + incident-corpus regression harness that proves the whole
subsystem contract end to end**, the way `core-loop-proof` does for dispatch.

| # | Subsystem | What it is | Why it needs a dedicated proof harness (real incidents) |
|---|---|---|---|
| **S1** | **Daemon dispatch loop** | bead→queue→worker-select→harness(correct model)→sandbox→edit/commit→DOT verdict-back, ×{claude,codex,pi}×{local,remote} | The whole factory; only ever exercised in production. Corpus gaps C2/C4/C6/C7/C8: the entire pi-model-leak week (hk-pkugu/lfrub/ytzj2), queue-submit field drops (hk-u6zp/y3o51), PR-19 fleet outage. **Phase 1 / crew alia — in flight.** |
| **S2** | **Keeper** | per-session context-fill watcher → handoff→/clear→/session-resume before the pane overflows | Binary-upgrade required-keys landmine, band retune, session_id flip on /clear, restart-now tmux target, smoke fork-bomb, `[1m]` window-size gauge bug. A wrong band or a lost session_id silently strands or fork-bombs a live session. |
| **S3** | **Comms bus** | `harmonik comms` inter-agent messaging (send/recv/log/join/leave/who) over the daemon event bus; at-least-once + dedupe-on-`event_id` (N3) | Zombie presence keys on outbound traffic, subscribe/recv-`--follow` dies on daemon restart, presence-refresh-<120s staleness, `--wake` pane mismatch, dormant-captain presence gap. Silent message loss or false-offline breaks the entire captain/crew coordination layer. |
| **S4** | **Sandbox / srt egress** | network-sandbox wrapper (`networksandbox_{darwin,linux}.go`, `srt`) that lets a harness reach its provider while blocking what it shouldn't | hk-u69my (sandbox blocked loopback model, false-green egress test), hk-ybuts (srt wrongly wrapped tcp:// remote runs), hk-9s5fx (dead-flywheel-ext fork bomb). Egress bugs surface as *silent no-commit*. |
| **S5** | **Remote-worker / runner** | tcp:// substrate, SSHRunner, worker registry, reverse tunnel, agent_ready under concurrent slots | hk-4tjt6/hs7ex (per-queue gate miscounts remote pool), hk-5z1f0 (agent_ready_timeout under 6 slots), hk-5qp7z/lt091 (remote worktree-create / base-fetch races), registry-startup-only. gb-mbp is the throughput critical path and the remote seam repeatedly diverges from local. |
| **S6** | **DOT review-loop** | reviewer agent produces `review.json` verdict; daemon drives resume / feedback-inject / commit-gate | hk-vv10r (ErrMalformed mid-write), hk-thbbv (flagless REQUEST_CHANGES wedge), hk-xkou8/4hso5/up1pk (unbounded sess.Wait idle hang), hk-vbv3b/whru3 (rebase-dropped-commits false reopen), commit-gate-cap salvage. |
| **S7** | **Lifecycle / supervisor / promote** | `supervise` (auto-revive), `reconcile` (close merged in_progress), `promote` (push/PR mode), health-window / last-good | Supervisor health-window false-revert, STAGE-2 watchdog last-good chain, reconcile-close on rebase-drop, promote non-ff race. This is the deploy interlock the 24-hour rule rides on. |
| **S8** | **Beads-integration** | `br` adapter; daemon-owns-terminal-transitions; queue-submit field fidelity | hk-l2xd1 (stranded in_progress claim-flood), daemon-owns-terminal-transition policy, hk-u6zp/y3o51 (queue-submit field drop — shared with S1). Agents writing terminal states corrupts the ledger. |

*(S1 is Phase-1/alia and already scoped in `03`/`06`. S2–S8 are the "and more" the operator is asking
about. S4/S5/S6-emergent/S8-field-fidelity overlap S1's live-substrate needs; S2/S3/S6-parsing/S7/S8-adapter
do not.)*

---

## 2. Shared test infrastructure — the common foundation, and who actually needs it

The shared foundation (all confirmed in-tree except Layer-0):

| Foundation piece | Location | Status |
|---|---|---|
| **scratch-daemon** (throwaway daemon, `guard_path`+`assert_not_supervised`, `batch`/`feedback`) | `scripts/scratch-daemon.sh`, `smoke-scratch.sh` | **Exists** (~80% primitive) |
| **Digital-twin agent seam** (script-driven handler over the NDJSON wire; zero `if isTwin`) | `cmd/harmonik-twin-{claude,codex,generic,session}/`; injected via `agent_overrides` (SH-008..011), schema HC-036a | **Exists** (from `remote-test-pyramid`) |
| **Scenario + assertion + corpus harness** (`DriveOrchestration`, `ReadEventLog`, `EvaluateAssertions`, `EventExpectation`, conformance floor) | `internal/scenario/*`; fixtures at repo-root `scenarios/` | **Exists** — event-stream assertion evaluator already lives here |
| **Coreloop assertion library** (the stable, exported assertion package the twin must imitate) | greenfield — **no `internal/coreloop` yet**; alia T2 (hk-1yxhh) lands it | **Being built now** (Phase-1 T2) |
| **Layer-0 Docker substrate** (disposable image, clean-reset, disk/CPU/net/clock dials) | **no Dockerfile anywhere** | **Greenfield** (Phase-2 chunk 3) |

**The crucial finding: the shared substrate is a spectrum, and each subsystem sits at a different tier.**
Keeper and comms have their OWN, lighter seams that predate and sit below the daemon substrate:

- **Keeper → gauge-file injection + tmux-stub.** `internal/keeper/` exposes `CyclerConfig` with ~20
  injectable `*Fn` seams (`ReadGaugeFn`, package-level `tmuxRunFn`, `InjectFn`, `OperatorAttachedFn`,
  `SetManagedSessionFn`, …). The entire warn/act/cycle/session-id/hold/restart-now state machine runs
  against fakes — write a `.ctx` JSON into a temp dir, swap `tmuxRunFn`, done. **No scratch-daemon, no
  twin.** (~45 `_test.go` already do exactly this.) Only the bash `keeper-statusline.sh` window-size
  inference + real bracketed-paste submit race need the `cmd/harmonik-twin-session` twin-in-real-tmux tier
  — which still does **not** need the production daemon.
- **Comms → in-process temp-JSONL + cursor-dir.** The at-least-once + dedupe contract lives entirely in the
  `commscursor` + `commsrecvhandler` pair. The existing N3 scenario test literally documents "no daemon, no
  socket, no live bus" — it runs `HandleCommsRecv` twice across a simulated crash window against a
  `t.TempDir()` events.jsonl. **No substrate.** Only the integration-shaped failures (subscribe-across-
  restart, presence timing, dormant-session gap, `--wake` tmux) need a live daemon/tmux.

Dependency of each subsystem on the shared substrate:

| Subsystem | Depends on shared daemon substrate? | Right harness |
|---|---|---|
| **S1 daemon dispatch** | **YES — this is the substrate's reason to exist** | scratch-daemon + twin + coreloop assertions (Phase 1/2) |
| **S2 keeper** | **NO** (own gauge+tmux-stub seam) — twin-session tier for bash+paste only | Lightweight standalone; twin-session for the bash tier. **New infra: none.** |
| **S3 comms** | **NO** for the core bus (in-process temp-JSONL) | Lightweight standalone; scratch-daemon only for subscribe-across-restart + presence-timing |
| **S4 sandbox** | **SPLIT** — profile/gate logic standalone; real egress needs substrate + real spawn (platform-split) | Lightweight for decision logic; substrate for egress acceptance |
| **S5 remote-worker** | **SPLIT** — registry/health/SSHRunner-construction standalone; agent_ready/tunnel need localhost-ssh substrate | Lightweight unit + a localhost-ssh rig (`scenario_remote_substrate_localhost_dot` is the middle ground) |
| **S6 DOT loop** | **SPLIT** — verdict-parsing + DOT-graph-validation standalone; wedge/salvage/commit-gate emergent need substrate | Lightweight for parsing; substrate for loop emergent behavior |
| **S7 lifecycle/promote** | **MOSTLY NO** — promote/reconcile against temp git repo; supervisor needs a child-process fixture, not a twin | Lightweight (temp git) + process-fixture rig |
| **S8 beads-integration** | **MOSTLY NO** — adapter against real `br`+temp `.beads/`; only queue-submit field-fidelity reaches the daemon | Lightweight standalone; field-fidelity overlaps S1 |

---

## 3. Dependency graph + parallelization strategy

Three tiers by substrate dependency:

```
                    ┌─────────────────────── CRITICAL PATH ───────────────────────┐
 Phase-1 S1 core-loop-proof (alia)  ──►  coreloop assertion lib (T2/hk-1yxhh)  ──► Phase-2 twin+Docker substrate
   (scratch-daemon, live matrix)          [GATE for everything substrate-bound]      (scripted-twin ‖ scratch-substrate → adversarial → chaos)
                                                        │
        ┌───────────────────────────────────────────────┴──────────── gated on substrate ──────────┐
        ▼                        ▼                          ▼                        ▼
   S4 egress acceptance   S5 agent_ready/tunnel      S6 loop emergent          S8 field-fidelity
        │                        │                          │                        │
        └── build ONLY after Phase-1 assertion lib is stable + (for most) Layer-0 dials land in Phase-2 ──┘

  ┌─────────────────── OFF THE CRITICAL PATH — build in parallel NOW (no shared substrate) ───────────────────┐
  │  S2 keeper harness (gauge+tmux-stub)          S3 comms harness (in-process temp-JSONL + cursor)            │
  │  S6-parsing (review.json + DOT-graph)         S7 promote/reconcile (temp git)   S8 br-adapter (temp .beads)│
  └────────────────────────────────────────────────────────────────────────────────────────────────────────┘

  DESIGN + kerf for ALL of S1–S8 needs NO substrate — can be authored immediately, in parallel with everything.
```

- **Critical path (unchanged):** `core-loop-proof` merged → coreloop assertion library stable+green at a
  named path → Phase-2 scripted-twin + Docker substrate → adversarial/chaos. Nothing shortens this; it is
  the daemon-dispatch spine and alia owns it.
- **What can be DESIGNED now, fully in parallel:** every subsystem's proof-harness (S2–S8) — design needs
  no substrate. Kerf problem-space/design passes for keeper, comms, and the "supporting-subsystem" batch
  can all run tonight.
- **What can be BUILT now, fully in parallel (the operator's actual question):** **S2 keeper and S3 comms
  proof-harnesses** — their seams are in-tree and substrate-free. Plus the lightweight halves of S6/S7/S8.
  A second crew is not blocked on alia at all.
- **What is GATED on the shared substrate (must NOT be pulled forward):** S4 egress acceptance, S5
  agent_ready/tunnel, S6 loop-emergent (wedge/salvage/commit-gate), S8 queue-submit field-fidelity. These
  consume the coreloop assertion library and (for the timing/disk/net cases) the Phase-2 Docker dials.
  Building them before the assertion contract is pinned = building against a moving target.

---

## 4. Recommended lane / phase structure for the next ~9 hours

The operator wants max parallelism. The unlock is that **keeper + comms + the lightweight subsystem halves
do not touch the daemon substrate**, so they parallelize the *build*, not just the *design*. Token posture:
Claude weekly cap ~98%; **pi harness is BLOCKED (hk-4ir08); prefer the codex path** for all build work.
These harnesses are Go test code + shell — codex-buildable.

### Stand up NOW (hand to the captain immediately)

**Lane 1 — S1 daemon-dispatch (IN FLIGHT, critical path).** Crew **alia** on `integration/core-loop-proof`.
No change. Its T2 assertion library (hk-1yxhh) is the gate for the substrate-bound lanes — flag the moment
it lands green.

**Lane 2 — S2 keeper proof-harness (NEW, build now, off critical path).** New crew on
`integration/keeper-proof`. Produces: a repeatable acceptance harness driving the `CyclerConfig.*Fn`
gauge+tmux-stub seams that proves the full warn/act/cycle/session-id/hold/restart-now contract, PLUS ports
the keeper incident corpus (required-keys, band, session_id-flip, restart-now-target, fork-bomb, `[1m]`
gauge) as durable regression scenarios. Reserve the `cmd/harmonik-twin-session`-in-real-tmux tier for the
bash `keeper-statusline.sh` + paste-race cases. **No substrate dependency — starts immediately.**

**Lane 3 — S3 comms proof-harness (NEW, build now, off critical path).** Same new crew or a second one, on
`integration/comms-proof`. Produces: an in-process bus harness (temp events.jsonl + temp cursor dir) that
proves at-least-once + dedupe-on-`event_id` across a simulated crash window, PLUS the presence/subscribe
integration tier (subscribe-across-restart, presence-refresh-<120s, dormant-session gap, `--wake` pane) —
the integration tier borrows the scratch-daemon but NOT the twin/coreloop, so it is independent of alia.
**Starts immediately.**

> Lanes 2 and 3 are small, disjoint surfaces (keeper package vs comms handlers) and both codex-friendly —
> one crew can carry both sequentially, or two crews run them concurrently if slots allow (local ≤4).

**Design/kerf track (admiral-owned, parallel, no crew build slot):** author kerf design passes for S4–S8
proof-harnesses now so they are ready when the substrate lands. Concretely: pin what each subsystem's
acceptance harness asserts + which incidents port as regressions. This is pure design — no substrate, no
tokens-of-note — and keeps the pipeline full so no crew ever waits.

### Stage (release to the captain when the gate opens)

**Lane 4 — supporting-subsystem lightweight batch (S6-parsing, S7 promote/reconcile, S8 br-adapter).**
These are substrate-free too, but lower-incident-density than keeper/comms — stage them as the next pickup
for whichever crew drains Lane 2/3 first. Branch `integration/subsystem-proofs`.

**Lane 5 — substrate-bound acceptance (S4 egress, S5 agent_ready/tunnel, S6 loop-emergent, S8
field-fidelity).** GATED: do not release until (a) the coreloop assertion library (alia T2) is stable+green
at its named path, and (b) for the timing/disk/net cases, the Phase-2 Layer-0 Docker dials exist. This is
effectively folded into the existing Phase-2/Phase-3 plan (`06`) — S4/S5/S6/S8 acceptance rows become
adversarial-corpus scenarios once the twin+substrate land.

### Ordering summary

1. **Now:** alia continues S1 (Lane 1). Spin **Lane 2 (keeper)** and **Lane 3 (comms)** on a new crew —
   these are the "another crew builds in parallel" the operator asked for. Admiral authors S4–S8 design in
   parallel.
2. **On alia T2 green:** release Phase-2 substrate build (scripted-twin ‖ scratch-substrate) per `06`, and
   unblock Lane 5's substrate-bound acceptance rows.
3. **As Lane 2/3 drain:** re-task that crew onto Lane 4 (supporting-subsystem lightweight batch).
4. **Every epic boundary:** assessor gate fires (per `04`/`08`) — keeper-proof and comms-proof each get
   the same independent CR+LT+XT gate before their `integration/*→main` PR.

---

## 5. Executive summary (for the admiral — the lane structure + what to hand the captain first)

- **Answer to the operator: YES, a second crew can build the keeper and comms test-validation systems in
  parallel with the daemon-dispatch system right now.** It is not a subsequent phase.
- **Why it works:** keeper and comms are already fully seam-wrapped and carry their own *lighter* harnesses
  in-tree — keeper via `CyclerConfig`'s ~20 gauge/tmux-stub `*Fn` seams, comms via an in-process
  temp-JSONL + cursor-dir rig (the N3 test already runs "no daemon, no socket"). Neither needs the
  scratch-daemon + digital-twin + Docker substrate that the daemon-dispatch proof (alia) is building.
- **What IS gated on the shared substrate:** only the *emergent-from-a-live-spawn* behaviors — sandbox real
  egress, remote agent_ready/tunnel, DOT loop wedge/salvage/commit-gate, queue-submit field-fidelity.
  Those wait for alia's coreloop assertion library (T2) to stabilize + the Phase-2 Docker dials. Do not
  pull them forward.
- **Hand the captain immediately:** (Lane 1) alia stays on core-loop-proof; (Lane 2) a NEW crew on
  `integration/keeper-proof`; (Lane 3) the same or a second crew on `integration/comms-proof`. All three
  disjoint, codex-path (Claude cap ~98%, pi blocked). Keeper/comms each = acceptance-harness + incident-
  corpus regression port, gated by the assessor at their epic boundary.
- **Stage, don't hand yet:** (Lane 4) lightweight supporting-subsystem batch — DOT verdict-parsing,
  promote/reconcile against temp git, br-adapter against temp `.beads/` — as the drain-pickup for Lane 2/3.
  (Lane 5) the substrate-bound acceptance rows — release on alia-T2-green, folded into Phase-2/3.
- **Admiral-owned parallel track (no build slot):** author S4–S8 proof-harness kerf design now so the queue
  never starves when the substrate lands.
- **Critical path is unchanged and singular:** core-loop-proof → coreloop assertion library → Phase-2
  twin+Docker substrate → adversarial/chaos. Keeper, comms, and the lightweight subsystem halves run
  entirely off it — that is the parallelism win.
</content>
</invoke>
