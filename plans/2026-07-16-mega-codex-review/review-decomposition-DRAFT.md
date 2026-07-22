# Mega Code Review — System Decomposition into Review Units (DRAFT)

> Purpose: split ~the whole harmonik system into well-scoped **review units** so a mega-review
> can be fanned out across a **Codex lane** and a **Claude lane** in parallel, with no gaps and
> minimal wasteful overlap. Read-only planning artifact — no code changes.
>
> Primary source: `plans/2026-07-12-codebase-census/REPORT.md` (the Keep/Simplify/Rebuild/Delete
> audit) + `PLAN.md` (freeze-and-carve moves). Cross-refs below point back to the census section
> that seeds each unit's risk tier and "places to check."
>
> **As-built caveat (the census is dated 2026-07-12; this map is 2026-07-16).** Since the census:
> - **M3 (run-state-machine) partially landed** — `internal/runexec` (1158 prod LOC) + `internal/mergeq`
>   (146) now exist; the terminal spine + dispatch/launch segments are driven through the machine
>   (COORD c026–c030). BUT `beadRunOne` is still ~2,165 lines and `workloop.go` is **8,098 lines** —
>   the god-function is only partly tamed. Review it as still-critical.
> - **M2 (agent-input substrate) is in flight** — a whole new **codex vertical** exists
>   (`codexdriver`/`codexinput`/`codexreactor`/`codexwire`/`codexdigitaltwin`/`codextest`, ~8k LOC)
>   plus `internal/substrate` (624) and grown `handler`/`handlercontract`. The tmux input stack
>   (`tmuxsubstrate.go` 2769, `pasteinject.go` 2671, `lifecycle/tmux/` 5562) is **NOT yet deleted** —
>   both the new protocol driver AND the old ack-free channel are live and must both be reviewed.
> - **M4 (remote rebuild) has NOT landed** — held behind M2. `remotematerialize.go`, `createworktree.go`,
>   the SSH-string path, and the **92 `runner == nil / != nil` dual-path sites** (verified count) are
>   all still present. The census "remote = Rebuild" verdict stands unchanged.
> - **M1 (delete test-theater) has NOT landed** — `operatornfr` (14.7k LOC) and `specaudit` (37.9k LOC)
>   are still present in full. The "green means nothing" exposure is still open.
> - **M5 extractions landed** — `internal/hook` (365), `internal/policy` (458), `internal/orchestrator`
>   (381) are new small seam packages carved from the daemon.

## Size ledger (ground truth, prod LOC = non-`_test.go`)

Total prod LOC: **internal/ = 168,403**, **cmd/ = 33,987** (~202k). Test LOC dwarfs this in several
packages (daemon test alone = 147k). Biggest prod packages: `daemon` 55,216 · `core` 31,541 ·
`cmd/harmonik` 26,187 · `workspace` 6,465 · `keeper` 7,145 · `lifecycle` 6,717 · `handlercontract`
4,544 · `handler` 3,382 · `queue` 4,467. Biggest single files: `workloop.go` **8,098**,
`tmuxsubstrate.go` 2,769, `pasteinject.go` 2,671, `dot_cascade.go` 2,668, `daemon.go` 2,390,
`reviewloop.go` 2,159, `comms.go` 2,119, `projectconfig.go` 2,027, `keeper/watcher.go` 1,867.

Sizing target per unit: ~3–6k prod LOC for a careful single-agent/Codex pass; Tier-1 units are
kept smaller (or explicitly split) because per-line scrutiny is higher. "Skim" units (generated
type files, test theater) may be much larger because the pass is classification, not line-audit.

---

## Top-level review ordering (by risk, review these first)

1. **RU-01 Daemon workloop / `beadRunOne`** — the treadmill itself; 80% of fix commits land here.
2. **RU-04 Remote / SSH substrate** — ack-free channel #2; 166 fix commits; M4 not started.
3. **RU-05 tmux input channel** — ack-free channel #1; 44 incident beads; both old + new live.
4. **RU-03 run-state-machine seam** — the new spine the whole lifecycle now flows through (parity risk).
5. **RU-09 Queue two-writer path** — live lost-update at `rpc.go`; data-integrity.
6. **RU-08 Substrate/Handler contract** + **RU-07 Codex vertical** — M2 new code, unproven.
7. Everything else by tier below.

---

## Review units

Legend — **Tier:** 1 Critical / 2 High / 3 Medium / 4 Test-theater / 5 Supporting.
**Lane:** C = Claude, X = Codex, **BOTH** = adversarial cross-check (reserved for highest-risk).

| ID | Unit | Scope (packages/files) | ~prod LOC | Tier | Lane | Census §/verdict |
|---|---|---|---|---|---|---|
| RU-01 | Daemon workloop / god-function | `internal/daemon/workloop.go` (8098), `runbridge.go`, `dispatchsegment.go`, `stategather.go` (541), `runshell.go`, `eagerfill_em063.go` (474) | ~10k | 1 | **BOTH** | §2 daemon-workloop **Rebuild** (High); §3 root #1 |
| RU-02 | DOT cascade + review/gate loop | `internal/daemon/dot_cascade.go` (2668), `reviewloop.go` (2159), `dot_gate.go` (715), `verdictexecutor_rc025a.go` (483), `sub_workflow_runner.go` (462) | ~6.5k | 1 | **BOTH** | §2 daemon-workloop **Rebuild**; §4 STEP-0a resume-hang |
| RU-03 | Run-state-machine seam (M3) | `internal/runexec` (1158), `internal/mergeq` (146), `internal/run` (128), `internal/runexectest` (719) | ~2.2k | 1 | **BOTH** | §4 Move 3; COORD c026–c030 (as-built) |
| RU-04 | Remote / SSH substrate | `internal/workspace/remotematerialize.go` (396), `createworktree.go` (288), `reviewverdict.go` (506, ssh), `autostatusmarker.go`, `diffhash.go`; `internal/workers` (2139); the 92 `runner==nil/!=nil` dual-path sites | ~5k | 1 | **BOTH** | §2 remote **Rebuild** (High); §3 root #2; §4 Move 4 |
| RU-05 | tmux input channel (old, still live) | `internal/daemon/tmuxsubstrate.go` (2769), `pasteinject.go` (2671); `internal/lifecycle/tmux/` (5562); `keeper/tmuxresolve.go` | ~11k | 1 | **BOTH** (split) | §2 tmux-io **Rebuild** (High); §3 root #2; §4 Move 2 |
| RU-06 | Daemon god-struct + composition root | `internal/daemon/daemon.go` (2390, 85-field `workLoopDeps`, `mergeMu`), `socket.go` (855), `projectconfig.go` (2027), `subscribe.go` (683), `quiesce.go` (1003), `crewstart.go` (780), `branching.go` (672), `handlerpause_*.go`, `claudelaunchspec.go`, `pilaunchspec.go` | ~11k | 2 | C | §2 daemon-godpackage **Simplify** (High); §3 root #1 |
| RU-07 | Codex substrate vertical (M2 new) | `internal/codexdriver` (1876), `codexinput` (939), `codexreactor` (665), `codexwire` (1593), `codexdigitaltwin` (703), `codextest` (2225) | ~8k | 2 | **X** | §4 Move 2 (structured driver); COORD c024 (M2 design) |
| RU-08 | Substrate / Handler contract seam | `internal/substrate` (624), `internal/handler` (3382), `internal/handlercontract` (4544) + `/lifecycle` (837) | ~9.4k | 2 | **BOTH** | §2 daemon-harness **Simplify**; §4 Move 2 (`handler.Substrate` seam) |
| RU-09 | Queue subsystem | `internal/queue` (4467, focus `rpc.go` two-writer path + `HandlerAdapter`) | ~4.5k | 2 | **BOTH** | §2 queue **Simplify** (High); §3 root #3 |
| RU-09b | Queue CLI | `internal/queue/cli` (3916) | ~3.9k | 3 | X | §2 queue **Simplify** |
| RU-10 | Core event registry surface | `internal/core/eventreg_hqwn59.go` (646), `pertypecompat_hqwn38.go` (388, dead), `eventtype.go` (1426), `DecodePayload`/`ValidateEnvelopeSchemaVersion` surface | ~5k | 2 | C | §2 core-eventreg **Simplify (cut deeper)**; §4 Move 1 |
| RU-10b | Core type / payload defs (skim) | `internal/core` remainder (~26k): `*events_hqwn59.go` payloads, `ids.go`, `tags.go`, `outcome.go` | ~26k | 3 | C (skim) | §2 core-eventreg |
| RU-11 | Event bus | `internal/eventbus` (2174, `busimpl.go` 1516, `jsonlwriter.go` 413) | ~2.2k | 2 | X | §3 root #3 (single-writer); event-model spec |
| RU-12 | Lifecycle sweeps + reconcile | `internal/lifecycle` (6717, `startup_pl005_qm002.go` 987, `orphansweep.go` 966, `orphansweepbeads.go` 740); daemon `reconciliation.go` (594), `reconciliationcadence_rc020a.go`, `orphansweep.go` (1049), `stalewatch.go` (1189), `draindetect.go` (539) | ~11k | 2 | C | §2 lifecycle-reconcile **Simplify** (High); §4 STEP-0b false-close; §3 root #3 |
| RU-13 | Keeper | `internal/keeper` (7145, `watcher.go` 1867, `step.go` 1157, `cycle.go` 890), `keepertest`, `keepertwin` (699), `twinparity` (963) | ~9k | 2 | X | §2 keeper **Simplify** (High) |
| RU-14 | Hook system (M5 seams) | `internal/hook` (365), `internal/hooksystem` (725), `internal/hookrelay` (608), `internal/policy` (458), `internal/orchestrator` (381) | ~2.5k | 3 | X | §2 daemon-harness; M5 extractions |
| RU-15 | Workflow graph engine | `internal/workflow` (1447), `internal/workflow/dot` (2192), `internal/workflowvalidator` (1104), `internal/goalstate` (342) | ~5k | 3 | C | (not census-named; new subsystem — review for over-abstraction) |
| RU-16a | CLI — comms + core commands | `cmd/harmonik/comms.go` (2119), `main.go` (1492), `run.go` (936), `harness.go` (985) + smaller top-level cmds | ~9k | 3 | C | (CLI surface; not census-scored) |
| RU-16b | CLI — keeper/init/assets/lifecycle cmds | `cmd/harmonik/keeper_*.go` (~2400), `init_cmd.go` (921), `sync_assets_cmd.go` (978), `crew`/`captain`/`start` verbs | ~9k | 3 | X | (CLI surface) |
| RU-17 | Supervise + daemon lifecycle | `internal/supervise` (1379), `cmd/harmonik/supervise` (2629), `cmd/harmonik/digest` (364), `internal/lifecycle/pidfile.go`, `daemonpaths.go` | ~4.5k | 3 | X | process-lifecycle spec; harmonik-lifecycle |
| RU-18 | Beads adapter (brcli) | `internal/brcli` (3750, `terminaltransition_bi010.go` 544, `intentlogwrite.go` 319) | ~3.75k | 3 | X | beads-integration; §4 STEP-0b (intent log BI-031) |
| RU-19 | Twin harnesses | `cmd/harmonik-twin-claude` (9237), `-twin-codex` (1499), `-twin-generic` (4624), `-twin-session` (1006) | ~16k (test infra) | 4 | C (skim) | scenario/twin infra |
| RU-20 | Test theater — operatornfr | `internal/operatornfr` (14679, 45 files) | ~14.7k | 4 | X (classify) | §2 test-bloat **Simplify (mostly delete)**; §4 Move 1 |
| RU-21 | Test theater — specaudit | `internal/specaudit` (37913, 133 files) | ~37.9k | 4 | X (classify) | §2 test-bloat **Simplify (mostly delete)**; §4 Move 1 |
| RU-22 | Scenario harness + integration | `internal/scenario` (4792), `internal/daemon/scenariotest` (1502), `internal/workflow/scenario` (7089), `test/` tree | ~13k | 4 | C (classify) | §2 test-bloat (keep ~11 real files); §4 Move 1 |
| RU-23 | Supporting subsystems (grab-bag) | `agentmanifest` (881), `agentlaunch` (333), `apptap` (612), `branching` (619), `cognition` (474), `crew` (732), `dashboard` (393), `digest` (1851), `presence` (577), `release` (922), `replay` (1074), `schedule` (1134), `sentinel` (1724), `sessioncapture`, `sessiondata` (647), `structuredlog` (1104), `usage` (952), `watch` (701), `scratchpad/*`, `t5probe`/`t6probe`, `testhelpers` (2197), `apptap`, `codexreactor`? | ~18k | 5 | split C/X | (not census-scored; review for dead/over-built code) |

---

## Per-unit "places to check" (concrete concerns, seeded from census)

- **RU-01 Daemon workloop.** 85-field `workLoopDeps` passed **by value** (race-safety by convention).
  `beadRunOne` still ~2,165 lines even post-M3; check the imperative guard sequence RT7 "explicitly
  kept imperative" (COORD c030) — the un-migrated tail. 598 `hk-` annotations in `workloop.go`. Verify
  the M3 `runbridge`/`spineArgs` hooks are genuine parity, not divergence. Mutable closure flags,
  `*bool` out-params (mostly deleted per c030 — confirm none remain).
- **RU-02 DOT cascade.** STEP-0a **resume-hang**: relaunch-on-gate-fail goes dead-silent
  (`model_selected → skills_provisioned → SILENCE`), no `run_stale`. QA-execution-gate workflow
  (~`0adb6551`). Check every relaunch/resume path emits a terminal signal within a liveness bound.
- **RU-03 run-state-machine.** New spine — **parity is the risk**. Cross-check the terminal-spine
  unification (4 daemon terminal blocks → single RT6 tail) is byte-identical to pre-M3. `mergeMu` →
  merge-queue (`mergeq`) migration: confirm no mutex held across git push / build / SSH. Coverage:
  runexec 95.4%, beadRunOne 63.7% (COORD c029) — flag under-covered branches.
- **RU-04 Remote.** Fresh `ssh -- '<string>'` per op through the remote **login shell**; ControlMaster
  deliberately disabled; **box-A mutexes owning worker-side state** (`mergeMu` held over network fetch);
  embedded **Python `fcntl.flock` script** as a Go string (`remotematerialize.go` ~:293–329); **92
  `runner==nil/!=nil` dual-path sites** (both arms). STEP-0c honest-probe guard in `createworktree.go`
  (`resolveWorktreeHEADViaRunner`) — check the missing-`.git` gap is closed.
- **RU-05 tmux input.** Ack-free paste into a TUI: exit 0 ≠ input accepted; 750ms sleeps, blind Enter
  retries, `capture-pane` screen-scraping; 44 incident beads; 4 workaround generations. **Both the old
  stack AND the new codex driver are live** — check the seam boundary and that nothing new re-imports
  the sleep/scrape pattern.
- **RU-06 Daemon god-struct.** `daemon.go:619` `mergeMu`; the composition-root wiring; `handlerpause_*`
  caulks; whether ≥8 coherent subsystems are still trapped in one namespace (extraction = relocation).
- **RU-07 Codex vertical.** NEW M2 code — no incident history yet, so review for correctness not scar
  tissue. Check the ack/liveness contract (`Ack{Delivered|Rejected,Seq}`, AIS-003/004), JSON-RPC
  request-id → ack mapping, the app-server child-stdin ownership. This is where the resume-hang could
  be **re-imported** on a substrate whose escape hatch (tmux) will be deleted.
- **RU-08 Substrate/Handler.** Census: "claude bypasses its own interface" and "codex WAL guard is 380
  lines of symptom-treatment." Check the `handler.Substrate` seam, `LaunchSpec.Substrate`, and whether
  the new `InputPort` (M2-1) is cleanly declared.
- **RU-09 Queue.** **Live lost-update at `rpc.go:~1016`** (two writer paths to `queue.json`);
  `HandlerAdapter` grab-bag being colonized by daemon knobs. Otherwise the census's "one mutex-free,
  spec-pinned, well-tested island" — verify that's still true.
- **RU-10 Core registry.** `pertypecompat_hqwn38.go` (388 lines, **all-vacuous, zero consumers**);
  `DecodePayload`/`ValidateEnvelopeSchemaVersion` reached only via `DispatchObservational/Synchronous`
  (zero non-test callers). Confirm dead before deletion recommendation.
- **RU-12 Lifecycle/reconcile.** STEP-0b **false-close**: `noChange`-subsumption closed `hk-2hfyt` on a
  bead-ID **mention** in an unrelated docs commit — close path can **fabricate done-status**. Class A–D
  matrix compensating for dual-source-of-truth; Class B fires ~83×/session. Check intent-log (BI-031)
  is extended not rebuilt.
- **RU-13 Keeper.** Root cause (multi-writer gauge) already fixed; remaining is two god-files
  (`watcher.go` 1867, `step.go` 1157) needing flattening. Review for residual multi-writer patterns.
- **RU-20/21 Test theater.** operatornfr asserts **its own constants** (fixture-mirror tautologies);
  specaudit is markdown-regex over spec prose — neither `exec`s the product. Classify keep vs delete:
  **keep** operatornfr `commandcodes.go`/`exitcode.go`/`securitypolicy_on006_on026.go`/
  `sandboxinvariant_on024.go` + their real tests; specaudit → one CI lint script outside `go test`.
- **RU-22 Scenario.** Keep the harness + ~11 behavioral files that `exec` real code (`asserteval`,
  `crashrecovery`, conformance-corpus harness); prune the rest. This is the coverage that M3 leans on —
  verify before pruning (the M1→M3 coverage-audit gate).

---

## Lane split rationale

- **BOTH (adversarial cross-check)** — reserved for the 6 highest-risk units where the census verdict
  is Rebuild or a live data-integrity bug and getting it wrong is expensive: **RU-01, RU-02, RU-03,
  RU-04, RU-05, RU-08, RU-09**. Two independent reads on ack-free channels and the god-function.
- **Codex lane (X)** — favors self-contained, protocol/mechanical, or newly-written code where Codex's
  strength (systematic per-file coverage) pays off: **RU-07 codex vertical, RU-09b queue CLI, RU-11
  event bus, RU-13 keeper, RU-14 hooks, RU-16b/17/18 CLI+lifecycle+beads, RU-20/21 test-theater
  classification** (large, mechanical keep/delete).
- **Claude lane (C)** — favors structural / architectural judgment, cross-file reasoning, and
  "is this over-built / is this real" calls: **RU-06 god-struct, RU-10/10b core, RU-12 lifecycle,
  RU-15 workflow engine, RU-16a CLI comms, RU-19 twins skim, RU-22 scenario classification.**
- **RU-23** grab-bag is split across whichever lane has free capacity; each sub-package is independent.

---

## Coverage check — is anything NOT in a unit? (no silent gaps)

Every `internal/*` and `cmd/*` package maps to a unit. Explicit accounting of easy-to-miss corners:

- **`internal/testhelpers`, `internal/scratchpad/{canary,evalvol}`, `internal/t5probe`, `internal/t6probe`**
  → RU-23 (supporting/test infra).
- **`internal/apptap`, `internal/sessioncapture`, `internal/structuredlog`, `internal/usage`,
  `internal/presence`, `internal/dashboard`** → RU-23.
- **`internal/codexreactor`** listed under RU-07 (codex vertical) — confirm it isn't double-counted in
  RU-23 (it is NOT; RU-23's trailing `codexreactor?` note is a reminder to exclude it).
- **`cmd/harmonik/assets/*`** (skills, scaffolds, templates, scripts) — these are **embedded non-Go
  assets** (agent skill markdown, shell scripts). NOT covered by a Go-code review unit. **GAP FLAG:** if
  the review is meant to include the shipped agent-skill/scaffold content, add a **RU-24 (assets/skills
  audit)** — currently out of scope as non-code.
- **Repo-root docs** (`AGENT_INDEX.md`, `STATUS.md`, `ROADMAP.md`, `docs/`, `specs/`) — NOT code; not a
  review unit. `specs/` is the normative source reviewers should read AS INPUT, not audit. **If spec-vs-code
  drift is in scope**, that is a distinct cross-cutting pass (call it RU-25) not a per-package unit.
- **`scenarios/`, `testdata/`, `twins/`, `refs/`, `research/`, `evaltasks/`, `tools/`, `scripts/`** (repo
  root) — fixtures/data/tooling, not product Go. Out of scope unless explicitly requested.
- **Generated/large event-payload files in `core`** — deliberately routed to a **skim** unit (RU-10b),
  not a line-audit, to avoid burning a Codex/agent context on mechanical type definitions.

## Open calibration questions for the reviewers of THIS map

1. **RU-05 and RU-21 exceed the 6k target** (11k / 37.9k). RU-05 should likely split into
   RU-05a (`daemon/tmuxsubstrate`+`pasteinject`) and RU-05b (`lifecycle/tmux/`). RU-21 (specaudit) is
   37.9k but a pure classification pass — acceptable as one mechanical unit, or split by file-glob.
2. **RU-01 at ~10k** is above target but `workloop.go` (8098) resists splitting mid-function. Consider a
   two-agent read of the same file with divided line ranges rather than a package split.
3. **Assets/skills (RU-24) and spec-drift (RU-25)** are flagged as gaps — confirm whether they are in
   scope for "the whole system" or explicitly excluded as non-code.
4. **BOTH-lane budget:** 7 units get two reads. If lane capacity is tight, demote RU-08 or RU-09 to a
   single lane (they are Simplify, not Rebuild) and keep BOTH only on the 5 Rebuild-verdict units.
