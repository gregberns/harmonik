# P2 — The EXTRACTION (steady-stream god-package carve-out)

**Status:** DRAFT for review (pre-kerf). Daemon/comms DOWN — this is design only.
**Frame:** platform-architecture P2 (DECISIONS.md). Companion threads: P1 fabric, P3 distributed execution.
**Grounding:** A4 coupling survey; A5 §4b/§5 R3; live package read + git log (2026-07-21).

---

## 0. Starting point (git ground truth, not doc claim)

A **"giant-retirement" refactor stream is already live** and is the momentum this plan continues — it is the template, not a new invention:

- `bootconfig` slices **B1–B6** (`refactor(daemon): giant-retirement boot-config …`) → extracted `internal/daemon/bootconfig/` (config-resolution seam, bus+subscribers, socket listener, work-loop shell).
- `socket-router` slices **SR-1–SR-3** → extracted `internal/daemon/router/` (pure value-in/value-out dispatch table; depguard-fenced from importing `daemon` back — `.golangci.yml:207-222`).
- run-state-machine **RT1–RT9** (`refactor(daemon): RT4 RunPorts/GatePort/MergePort/LedgerPort/EmitterPort seam` …) → seam-ified the DOT run path with injected ports.

**The established pattern** (what every P2 unit follows): scaffold a package → move value-in/value-out logic into it → daemon threads effectful closures/ports IN → add a depguard rule denying the new package from importing `daemon` back → tests move with the code → release as one reviewable unit.

**Measured current state** (`internal/daemon`, 2026-07-21):
- 631 non-test `.go` files, **56,583 non-test LOC**. `internal/core`: 501 files (234 non-test), **56 event/payload family files**.
- `workloop.go` = **8,207 LOC**, `dot_cascade.go` = 2,683, `reviewloop.go` = 2,172 — the DOT run-loop is the mass and the heat.
- **939 commits touched `internal/daemon` in 60 days** — R3 (merge-conflict warfare) is real, and drives ordering.
- **Churn is wildly uneven** (90-day commit counts): `workloop.go` 316, `dot_cascade.go` 94; but `codexlaunchspec.go` 15, `codexcommit.go` 3, `claudelaunchspec.go` 20, `harnessregistry.go` 15. **The harness impls are COLD; the DOT loop is HOT.** Extract cold-first.

---

## 1. Goal

Reduce coupling by **moving implementations out of the two god packages behind seams that ALREADY EXIST** — not by inventing new seams. The seams and their owners:

| Existing seam | Owner pkg | What exits behind it |
|---|---|---|
| `handlercontract.HarnessRegistry` / `AdapterRegistry` | `handlercontract` (core-only leaf) | claude / codex / pi harness impls |
| `handler.Substrate` (one method `SpawnWindow`) | `handler` | subprocess-hosting (tmux/ssh) |
| `lifecycle/tmux.CommandRunner` | `lifecycle` | local-vs-ssh execution transport |
| `workers.Registry` + tunnel/health payloads | `workers` | remote worker addressing |
| `internal/queue` RPC API (`QueueSetter`/`LockedQueueView`) | `queue` (core-only leaf) | queue wiring/persistence/dispatch |
| `internal/eventbus` | `eventbus` (core-only leaf) | event emission from all of the above |
| `internal/crew` registry (depguard-fenced leaf) | `crew` | crew spawn/launchspec/reap wiring |
| depguard v2 matrix (`.golangci.yml`) | CI | the freeze-then-strangle tripwires |
| RunPorts/GatePort/MergePort/LedgerPort (RT seams) | `daemon` (in-progress) | DOT run-loop |

**Success = the god packages shrink, measurably, one releasable unit at a time**, with zero behavior change per unit and a depguard edge locking each extraction so the concern cannot leak back.

**Non-goals:** no new abstraction layers; no big-bang; no kernel/fabric work (that's P1); no per-harness transport reinvention.

---

## 2. Extraction backlog + ordering

Ordering rule (from A4 + churn data): **highest-value-first AND coldest-first** — they coincide. Harness impls are both the highest leverage (they unblock P3's "minimal harmonik in a container") and the lowest risk (cold files, tiny blast radius, seam already wired). The DOT loop is highest-risk/hottest → last of the daemon units. `core` splits last of all.

### Unit E1 — Harness implementations (claude / codex / pi) → `internal/harness/{claude,codex,pi}` [FIRST]

- **What it is:** the concrete `handlercontract.Harness` impls + their private run-context types + support code. ~**4,766 LOC across 16 files**: `{claude,codex,pi}harness.go`, `{claude,codex,pi}launchspec.go` (which own the private `claudeRunCtx`/`codexRunCtx`/`piRunCtx`/`claudeRunArtifacts` structs), `codexcommit.go`/`picommit.go`, `codex/pi jsonlparser.go`, `codexwalguard.go`, `{codex,pi}billingguard.go`, `claudeheartbeat.go`, `claudeworktreesweep.go`, `pi_profile_resolve.go` + their `_test.go` siblings.
- **Seam it exits behind:** `handlercontract.HarnessRegistry` — **already the wiring** (`daemon/harnessregistry.go` `newHarnessRegistry` registers all three via `reg.Register(core.AgentType…, New*Harness())`; `routedLaunchSpecBuilder` already routes through `HarnessRegistry.ForAgent`). Nothing new to build; move the impls, keep the registry-assembly as a thin daemon composition file (or move it to `internal/harness/registry`).
- **Blast radius = SMALL, verified:** the launchspec builders import only `core` (7 refs), `handler` (8 refs), `workspace` (1 ref) — all clean seam packages, **zero deep-daemon-internal calls**. Cross-harness refs in `codexharness.go` are comments only; each harness's private run-ctx is self-contained in its own launchspec file.
- **Size/risk:** ~4.8k LOC, **LOW risk** (cold files, mechanical move, seam pre-exists). Split into 3 sub-releases: **E1a codex** (hottest of the three, and P3/Codex-first priority), **E1b claude**, **E1c pi**.
- **Watch-item:** any run helper genuinely shared across two harnesses goes to a tiny `internal/harness/shared` leaf (core/handler only) — do NOT leave it as a daemon back-edge.

### Unit E2 — Crew wiring → `internal/crewrun` (or `internal/daemon/crewrun` sub-pkg) [SECOND]

- **What it is:** daemon-side crew spawn/launchspec/reap: `crewstart.go` (849), `crewlaunchspec.go` (185), `crewidlereap.go` (297) + tests. ~1.3k LOC.
- **Seam it exits behind:** `internal/crew` registry (already a depguard-fenced leaf, `.golangci.yml:157-171`) + `HarnessRegistry` (crew-start already routes through harness selection — commit `9adc9c1a feat(crew): route crew-start through harness selection`). Depends on E1 landing first (crew launchspec resolves a harness).
- **Size/risk:** ~1.3k LOC, **LOW-MEDIUM** — mostly self-contained; the registry leaf exists.

### Unit E3 — Queue wiring → behind `internal/queue` RPC API [THIRD]

- **What it is:** the daemon-side queue *wiring* (the engine is already a clean leaf): `queuestore_hkj808w.go`, `queueledger_bridge.go`, `dispatchsegment.go`, `perqueuespendmeter_tigaf11.go`, `queue_operatoreventconsumer_7urls.go`, `socketdispatch.go` + tests.
- **Seam it exits behind:** `internal/queue` typed RPC surface (`QueueSetter`/`LockedQueueView`/`MutationLocker`, `queue/rpc.go:57-84`) + `eventbus`. The engine does not reach back; only the wiring is trapped.
- **Size/risk:** **MEDIUM** — some of this is entangled with dispatch on the run path; carve the store/ledger-bridge/spend-meter first (leaf-ward), leave `dispatchsegment`/`socketdispatch` for the tail where it abuts E5.

### Unit E4 — ssh / transport → behind `CommandRunner` + `workers.Registry` [FOURTH — P3-critical]

- **What it is:** `reversetunnel.go` (361) + the remote-run bits embedded in `workloop.go` (reverse-tunnel readiness gating `:422-430`, per-worker cold-start bound, `RemoteAgentReadyTimeout` `:523-524`, the `CommandRunner` threaded into the DOT spawn path `:757-764`).
- **Seam it exits behind:** `lifecycle/tmux.CommandRunner` (local-vs-ssh execution seam, `runner.go:16`) + `workers.Registry` (`registry.go:10`, tunnel/health payloads). These exist; the work is pulling the remote-orchestration logic OUT of `workloop` into a transport-owning package that the run-loop calls through the seam.
- **Size/risk:** **MEDIUM-HIGH** — the logic is embedded in the hot `workloop.go`; must be teased out carefully. **This is the unit that most directly unblocks P3** (see §4): P3's container dispatch builds on a clean transport seam instead of forking workloop.
- **Sequencing note:** partially gated by E5's RT seam maturing (the remote bits live inside the run machine). May interleave the leaf part (`reversetunnel.go` → `internal/transport/tunnel`) early and the workloop-embedded part with E5.

### Unit E5 — DOT run-loop → ride the RT ports seam [FIFTH — hardest, last daemon unit]

- **What it is:** the mass and heat of the daemon: `workloop.go` (8,207), `reviewloop.go` (2,172), `dot_cascade.go` (2,683), `dot_gate.go` (735), `runregistry.go`, `runshell.go`, `runbridge.go`, `runports.go`, `runinflightreconcile`, `run_session_adoption.go`.
- **Seam it exits behind:** the **RT ports already being cut in-flight** (RunPorts/GatePort/MergePort/LedgerPort/EmitterPort/Worktree/Launch/Budget — commits `9df61a32`, `c23ffba5`, `a70082bc`, `c22ccc11`, `38668b8c`). Extraction = finish threading the run machine through these ports, then lift the machine into `internal/runloop` with the ports injected by daemon.
- **Size/risk:** **HIGH** — hottest files in the tree (316 commits/90d on workloop). Do LAST, only after E1–E4 have drained the surrounding concerns out of the package (each prior extraction shrinks workloop's neighbor-set and reduces conflict surface). This unit is itself a multi-slice stream (continue the RT numbering), never one PR.

### Unit E6 — `internal/core` split by type-family → sibling leaves [LAST OF ALL]

- **What it is:** the 501-file / 56-payload-family shared kernel. Split **by type-family, one family per PR, never a rename storm** (A5 §5 R3).
- **Candidate families (from file-prefix survey):** `budget*` (budgetpayload/counterstate/dispatchcheck/exhaustion/warning/scope/ref — ~12 files), `gate*` (gateaction/gatepayload/gatedecision/gateverdict/gateref/gatesubtype), `hook*` (hookname/hookpayload/hookverdict/hooktrigger/hookevents), `cp*`+`guard*` (control-point + guard registry), `agent*`+`lifecycle*` (agenttype/agentevents/agentinput/agentlifecycle), **event-infra** (eventid/eventtype/eventreg/eventenvelope/eventdispatch/eventpattern — the bus vocabulary).
- **Seam it exits behind:** none needed — `core` is a leaf; a family moves to `internal/core<family>` (or a `core/<family>` sub-leaf) and the 35 fan-in importers update their import path. Pure mechanical, but 35-importer blast radius per family → **do rarely, and only a family whose churn justifies it**.
- **Size/risk:** **MEDIUM per family, HIGH if rushed.** Gate: only split a family when it has become an independent hot-spot; otherwise leave it. Never split more than one family per release.

**Ordering summary:** E1(a/b/c) → E2 → E3 → E4 → E5 → E6. E1 first because value ∧ safety ∧ P3-unblock all point at it. E6 last because A4/A5 say so and the blast radius (fan-in 35) is unforgiving.

---

## 3. Steady-stream discipline

**Each unit is an independently testable + releasable slice — component by component, test+release as each separates.** (DECISIONS.md P2 execution style.)

- **No big-bang:** never move two subsystems in one PR; never touch E5 and E1 together. Each release moves ONE unit (or one sub-slice of a large unit).
- **No trickle:** a unit is not dribbled file-by-file across weeks — the whole self-contained unit (impl + its private types + its tests) moves in one reviewable PR behind the pre-existing seam, then releases. E1a = "all of codex, in one move." Large units (E5, E6) are the only ones sliced, and even then each slice is a complete, green, releasable RT-numbered step — not a half-wired intermediate.

### Freeze-then-strangle (the tripwire — A5 §5 R3)

For each extracted concern, the **same PR that extracts it adds the depguard edge that forbids it coming back** — this is the mechanical brake, exactly as SR-1 fenced `router` and the keeper rule fences keeper:

1. **New package deny-back edge:** `internal/harness/**` (etc.) gets an `allow: [$gostd, core, handler, handlercontract, workspace, self]` + `deny: internal/daemon "harness impls MUST NOT import daemon back"`. Machine-checked in CI.
2. **"No NEW files in daemon for an extracted concern" tripwire:** after E1, a CI grep-guard (or a depguard file-glob deny) fails the build if a new `internal/daemon/*harness*.go` / `*launchspec*.go` appears. Extracted concern = closed door. This is the literal freeze: **P3 and anyone else cannot reopen the god package for that concern.**
3. **Core-addition rule:** a new file in `internal/core` requires a **named type-family** in its header + review sign-off; ad-hoc "misc types" additions are rejected at the review gate. (Prevents core re-bloating while E6 is deferred.)
4. **Boundary test as pre-agreed ground rule** (guardrail from DECISIONS.md §Guardrail + A5 §4e): each unit ships with a "does not import daemon back" test + the depguard edge, agreed BEFORE the extraction. Scope disputes ("is this in the unit or not?") are settled by that test, not by argument.

---

## 4. Coordination with P1 / P3

**Hard rule (A5 §1.3 / C4, load-bearing):** *nothing new for P3 lands in `internal/daemon`. The execution path is born OUTSIDE the god package or not at all.* The freeze tripwire (§3.2) is the enforcement — after E1/E4 land, P3's container/dispatch code physically cannot add files to the extracted concerns in daemon; it builds on the new leaf packages.

**Where P2 UNBLOCKS P3 (the extractions are the forcing function A5 §2/§4d names):**

- **E1 (harness impls) IS "minimal harmonik in a container."** Option-3's "minimal harmonik" cannot be minimal while it links `internal/daemon` (drags the 56k-LOC monolith into every container). E1 makes `internal/harness/codex` a linkable unit with a core/handler/workspace-only closure → the container links the harness, not the daemon. **E1 = the single most P3-enabling P2 unit.** Codex-first (E1a) aligns with PRIORITY-0.
- **E4 (transport) gives P3 a clean dispatch substrate.** P3's remote container dispatch builds on `CommandRunner` + `workers.Registry` instead of forking `workloop`'s embedded reverse-tunnel logic — which is exactly the "bespoke pipe reinvented per-path" failure P1/A5 §1 diagnoses.
- **E3 (queue) gives P3's dispatch plugin a clean queue API** (`queue` RPC surface) to hand beads through, rather than reaching into daemon queue wiring.

**Where P2 does NOT depend on P1:** P2 needs no fabric, no kernel, no new transport. It runs on its own clock (A5 §4b) and every unit reduces total system risk regardless of P1/P3 progress. P2's contribution to P1/P3 is **negative space**: it empties the god package so the new execution path has somewhere clean to be born.

**Crew staffing (DECISIONS.md):** P2 is one of ~4-5 parallel crews; run it on the **Codex harness** once E1a proves Codex operational (conserve Claude tokens per PRIORITY-0). P2 extraction work is well-suited to Codex — mechanical moves with a hard behavior-preservation contract and a green-tests oracle.

---

## 5. Testing / release cadence (proving behavior-preservation per unit)

Each unit clears this gate before release; the gate is the same for every unit so it's a rote, delegable checklist:

1. **Pure-move review:** the diff is a `git mv` + package rename + import-path fixups with **no logic change** — reviewed as a mechanical move. Any logic delta is called out explicitly and justified, else rejected. (`agent-reviewer` unwanted-abstraction check: extraction must NOT invent a new seam — it exits behind a pre-existing one.)
2. **depguard green:** the new package's allow/deny edges + the daemon deny-back edge + the freeze tripwire all pass `golangci-lint`. This is the boundary test (§3.4).
3. **Full test suite green:** the unit's `_test.go` files move WITH it and pass in the new package; the whole-repo `go test ./...` is green (no import cycle, no broken fan-in).
4. **Runtime proof (units with a run surface — E1, E4, E5):** drive the real path end-to-end, not just tests. E1: run one codex bead + one claude bead through DOT (impl→review→merge) and confirm identical outcome vs pre-extraction. E4: run one remote bead. Use the `verify` skill. (Daemon is DOWN now; this gate applies at execution time, not design time.)
5. **`ubs` on changed files + `agent-reviewer` verdict** on the commit (trailers), per build-practices.
6. **Release = merge the unit to target branch as its own bead branch.** The daemon merges completed bead branches into `$TARGET_BRANCH` (harmonik-lifecycle). One unit = one bead = one release. Measure success by the **daemon non-test LOC count dropping** and the **file count leaving `internal/daemon`** per release — publish that number in each unit's close comment so the stream's progress is legible.

**Cadence target:** one unit (or one large-unit slice) per release cycle, continuously — a steady stream, not a batch. E1a/E1b/E1c are three consecutive releases; E5/E6 are multi-slice streams each releasing per slice.

---

## 6. Open questions (resolved)

- **Where do run-ctx types go?** RESOLVED: each harness's private run-context struct (`codexRunCtx` etc.) lives in that harness's own launchspec file and is self-contained → moves with the harness. Verified no cross-harness code coupling (only comments).
- **What about the registry assembly (`newHarnessRegistry`)?** RESOLVED: keep it as a thin daemon composition file (daemon is the composition root — assembling plugins is its correct job) OR move to `internal/harness/registry`; either satisfies the freeze rule as long as the *impls* leave. Prefer keeping assembly in daemon (composition root) to avoid a gratuitous package.
- **E5 vs the in-flight RT refactor?** RESOLVED (as design intent): E5 does NOT restart the RT work — it **continues the RT numbering** and lifts the machine only once the ports are fully threaded. Extraction rides the existing seam; it does not compete with it.
- **Shared harness helpers?** RESOLVED: if two harnesses share a helper, it goes to a small `internal/harness/shared` leaf (core/handler only), never a daemon back-edge.

---

## Questions for operator

1. **Package home for harness impls: top-level `internal/harness/{claude,codex,pi}` vs daemon sub-package `internal/daemon/harness/…` (like `router`/`bootconfig`)?** The P3 goal — a container that links the harness but NOT the daemon monolith — argues strongly for **top-level** `internal/harness/*` (a daemon sub-package still lives under the daemon import path and risks dragging siblings). Recommend top-level. This is an architecture call; confirm.
2. **Freeze tripwire severity: hard CI failure vs review-gate warning** for "new file in daemon for an extracted concern"? Recommend **hard CI failure** (a warning erodes — 939 commits/60d will walk right past it). Confirm you want the build to break.
3. **Timing of E6 (core split):** A4/A5 say split core LAST and only by hot type-family. Do you want E6 in this plan's scope at all now, or explicitly parked until E1–E5 land and a core type-family proves itself a hot-spot? Recommend **park E6** (name the families, don't cut them yet).
4. **DECISIONS.md guardrail ratification:** adopt substrate-v2's kill-criteria + boundary tests as the shared, pre-agreed ground rules for P2 scope disputes (this plan assumes yes in §3.4). Confirm ratification.
5. **Crew harness for P2:** run the P2 extraction crew on Codex (per PRIORITY-0 token conservation) once E1a proves Codex operational — with Claude reserved for the pure-move review gate (§5.1)? Confirm the Codex-for-P2 routing.
