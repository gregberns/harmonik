> **Verified against the live tree** on 2026-07-23 at HEAD `30147430e` (branch `phase1-session-restart-substrate`). `go build ./internal/... ./cmd/...` exits 0; tree clean. Every coordinate below was re-grepped live by a six-agent recon fan-out + one adversarial verifier (all PASS). The E5-CHUNK-CATALOGUE RT18 rows drifted +40–70 lines and — more importantly — rest on a **precondition that never landed** (see §0). Trust this document's coordinates and re-grep before editing anyway (10th consecutive slice with line-drift).

# RT18 — drop `deps workLoopDeps` from the run-path signatures

## §0 — The decisive discovery: the catalogue's RT18 is built on a phantom precondition

The E5-CHUNK-CATALOGUE frames RT18 as ~12 small mechanical chunks that "drop `deps`, add `(env, ports, shared)`," on the stated premise that **"the read conversions belong to RT16/RT17 and are not RT18's"** (catalogue §RT18) and that **"the resolution already lives in RT17.0's `launchPortFull()`"** (RT18.11).

Both are false against the tree that actually exists:

- **`launchPortFull()` does not exist.** `grep -rn launchPortFull internal/daemon` → nothing. RT17 as landed (`d71e79749`) built the *narrow* `(*workLoopDeps).launchBuilder()` raw-field accessor and **explicitly EXCLUDED** the LaunchPort widening (its §1c: the "RT17.0–.17" hookStore/timeouts/sandbox/delivery scope is "a different, later slice. EXCLUDED"). That widening slice was **never planned or staffed.**
- **The run-path files still carry ~15 homeless field-families.** Live `grep -c 'deps\.'`: `reviewloop 92 / dot_cascade 72 / dot_gate 41` — not the `0/0/0` the catalogue's RT18 table assumes. `executeCognitionGate`, `dispatchDotAgenticNode`, `runReviewLoop`, `driveDotWorkflow`, `dotSubWorkflowRunner`, and `beadRunOne` all read fields that **have no home on any bundle**: `harnessRegistry`, `hookStore`, `substrate`, `reviewerSubstrate`, `adapterRegistry`, `handlerBinary`, `handlerArgs`, `handlerEnv`, `daemonBinaryPath`, `agentReadyTimeout`, `remoteAgentReadyTimeout`, `postAgentReadyHangTimeout`, `sandboxCfg`, `codexNoWorkDurationFloor`, `brTimeoutCfg`, `intentLogDir`, `brAdapter`, `runner`, `worktreeFactory`, `worktreeCreateMu`, plus the raw `launchBuilder()` func and `emittedEpics`/`emittedEpicsMu`.

**You cannot drop `deps` from a function that reads homeless fields — it does not compile.** This is exactly §3.1's own lesson ("an in-place read conversion is only real if the destination field already exists"), now biting the catalogue's *surviving* RT18 signature-drop table. RT18.3–.11 are **unwritable as catalogued.**

### §0.1 — What is actually writable, and the revised RT18 shape

| Phase | What | Writable now? |
|---|---|---|
| **RT18-C** (was RT18.0–.2) | `clockOrSystem()` accessor + fold `runPorts().Clock` default + convert 48 `deps.clock` reads + delete the 5 copy-mutation guards | **YES** — `RunPorts.Clock` already exists |
| **RT18-W** (the missing slice) | **Widen the existing bundles** to home the ~15 field-families, converting each family's reads to a derived-local bundle alias (`handles := deps.sharedHandles()` etc.) with **no signature change** — the RT15/RT16 payoff pattern | **YES** — see §0.2 authority ruling |
| **RT18-S** (was RT18.3–.10) | The leaf-first signature drops become an ~8-line change each once RT18-W drains the reads | after RT18-W |
| **RT18.11** | Delete the `launchSpecBuilder` smuggle, reconciled with RT17-as-landed | after RT18-S sub-drivers convert |

### §0.2 — Authority ruling: widening an existing bundle is NOT a new seam

The recon correctly flagged the field-homing as touching `_plan.md`'s no-new-seam rule. Traced to source, the rule is:

- `_plan.md:29` — *"moving implementations out of the god packages behind seams that **ALREADY EXIST** — not by inventing new seams."*
- `_plan.md:148` — *"extraction must NOT invent a new seam — it exits behind a pre-existing one."*
- `_plan.md:45` — non-goal: *"no new abstraction layers."*

`RunEnv` / `RunPorts` / `SharedHandles` are **existing seams** (RT4 seam-ified the run path; RT15 built RunEnv/SharedHandles for exactly this purpose). Adding a **field** to one — a byte-identical straight copy of a `deps` field, populated in the existing `runEnv()`/`sharedHandles()` constructor — is *using the existing seam more fully*, not inventing a new one. The `sharedHandles()` doc already pre-sanctions this: it says `deps.tidGen`'s omission is "not **this** slice's call to make (RSM-011)" — deferring the widening **to a future slice. RT18 is that slice.** The catalogue's own recommended resolution for `tidGen` is `SharedHandles.TIDGen` (a field), precisely because it is *not* a new seam — whereas `RunPorts.TIDGen` as an **8th port interface** *would* be.

**The boundary, stated crisply:**
- **In-charter (implementer's call — proceed):** adding a **field** to an existing bundle, carrying an existing concrete type (a value, or a by-reference pointer/channel/func), copied straight from `deps` in the constructor.
- **A new-seam escalation (operator's call — do NOT do):** inventing a new **port interface type** / abstraction (e.g. a `TIDGenPort`, or E2b's `crewSpawner`).

None of the RT18-W homings introduce an interface — they are struct fields on `RunEnv`/`SharedHandles`/`RunPorts` carrying the concrete types the `deps` fields already hold. **The live safety net is the per-commit `agent-reviewer` unwanted-abstraction check:** if a reviewer reads a field-homing as an unwanted seam, it says so and we reconsider. That is the real gate — not a phantom freeze that would shrink RT18 to its smallest writable fragment.

## §1 — RT18-C: eliminate the clock copy-mutation idiom (WRITABLE — land first)

Combines catalogue RT18.0/.1/.2 into one atomic, sed-provable, behaviour-preserving commit (single-writer, daemon down — the per-chunk split was for a racing-writers world).

**Foundation facts** (the whole safety story): `clock substrate.ClockPort` field @ `workloop.go:504`; prod is **never nil** — `newWorkLoopDeps` wires `clock: substrate.SystemClock{}` @ `workloop.go:1164`; the 5 `if deps.clock == nil` guards protect **only** struct-literal test deps. `substrate` already imported in `runports.go` — no new import. `deps` is by value, so the guard-mutation of the copy never escaped.

1. **Add the accessor** after `launchBuilder()` @ `runports.go:95`:
   ```go
   // clockOrSystem returns the run shell's ClockPort, folding a nil field to the
   // production SystemClock at the READ site (a pure read — unlike the deleted
   // `if deps.clock == nil { deps.clock = … }` copy-mutation, it never writes).
   // The default-folding analog of emitterPort() (RT18).
   func (deps *workLoopDeps) clockOrSystem() substrate.ClockPort {
       if deps.clock != nil {
           return deps.clock
       }
       return substrate.SystemClock{}
   }
   ```
2. **Fold the default into `runPorts()`** so the eventual `ports.Clock` reads (after RT18-S) are never nil: `runports.go:351` `Clock: deps.clock,` → `Clock: deps.clockOrSystem(),`. (Load-bearing per the verifier: `ports.Clock` is raw today; the drop would re-introduce a nil-clock panic for struct-literal test deps without this fold.)
3. **RT18.0 — delete the proven-dead default** in `dispatchDotAgenticNode` (`dot_cascade.go:1263–1267`, comment + 3-line guard). Deadness proven: every path reaching it (`driveDotWorkflow:906` after its own default @221; `sub_workflow_runner.go:323` via `newDotSubWorkflowRunner`; `swMakeRunner` fixtures use only non-agentic nodes) arrives non-nil.
4. **Convert all 48 `deps.clock` reads** → `deps.clockOrSystem()` (and `b.deps.clock` → `b.deps.clockOrSystem()`, and `Clock: deps.clock,` struct-literal fields). Live read sites: `workloop.go` 12 (3191,3216,4407,4648,4696,4734,4741,4854,4901,4921,5062,5120), `reviewloop.go` 16 (557,586,625,631,688,709,728,741,796,815,987,1368,1441,1456,1474,1517), `dot_cascade.go` 12 (255,438,1707,1744,1768,1783,1789,1833,1836,1895,1946,1960), `dot_gate.go` 5 (454,514,516,526,580), `runbridge.go` 2 (84,142), **`runports.go` 1 (351 — the fold above)**. `//nolint` comments on those lines (e.g. dot_cascade 1895 `contextcheck`) are preserved verbatim.
5. **RT18.2 — delete the remaining 4 guard-blocks** (each `if deps.clock == nil { deps.clock = substrate.SystemClock{} }` + its 2–3 line lead comment): `driveDotWorkflow` (dot_cascade 217–222), `beadRunOne` (workloop 3105–3107 + lead), `runReviewLoop` (reviewloop 230–232 + lead), `executeCognitionGate` (dot_gate 269–271 + lead). Re-grep each lead comment live (2 vs 3 lines). Reword the RT17 clock comments (dot_cascade ~222/1268: "…must not bypass the default set just above…") — stale once the default is gone.

**Green:** `grep -rn 'deps\.clock = substrate' internal/daemon` → 0; `grep -rn 'deps\.clock == nil' internal/daemon` → 0; `grep -rn 'deps\.clock' internal/daemon/*.go | grep -v _test` → exactly 2 (the accessor body); `grep -rn 'b\.deps\.clock' internal/daemon` → 0; `go build ./internal/... ./cmd/...` exit 0; sed-normalize `s/deps\.clockOrSystem()/deps.clock/g` (+`b.deps` form) collapses every ±code line to an identical pair (only unpaired add = the accessor). Trips **no** freeze-gate tooth (clock has none). Daemon differential suite adds no new `--- FAIL` name.

## §2 — RT18-W: widen the bundles (the real body of RT18)

Each field-family is one commit: add the bundle field(s) → populate in the constructor (`runEnv()`/`sharedHandles()`) → in each reading function derive the bundle local (`handles := deps.sharedHandles()` / `env` is already present) → convert `deps.X` reads to `handles.X`/`env.X` (byte-identical alias, RT16 `emit :=` pattern) → **no signature change**. Green criterion per commit: build 0, sed-normalizes to identity, differential suite same-set, agent-reviewer APPROVE (its unwanted-abstraction check is the seam-boundary gate).

**Field → home table** (home chosen per each bundle's charter; verify each field's concrete type live before adding):

| Field(s) | Home | Rationale (charter fit) |
|---|---|---|
| `harnessRegistry`, `adapterRegistry`, `hookStore`, `substrate`, `reviewerSubstrate`, `brAdapter`, `runner`, `worktreeFactory`, `worktreeCreateMu`, `tidGen`, `emittedEpics`+`emittedEpicsMu` | **SharedHandles** | cross-goroutine handles/registries/mutexes shared **by reference** — verbatim the SharedHandles charter (like `RunRegistry`/`Workers` already there) |
| `handlerBinary`, `handlerArgs`, `handlerEnv`, `daemonBinaryPath`, `intentLogDir`, `agentReadyTimeout`, `remoteAgentReadyTimeout`, `postAgentReadyHangTimeout`, `codexNoWorkDurationFloor`, `sandboxCfg`, `brTimeoutCfg` | **RunEnv** | immutable per-run daemon **config** — verbatim the RunEnv charter (like `ProjectDir`/`TargetBranch`/`DefaultHarness` already there) |
| resolved launch builder (raw func) | **RunPorts.LaunchBuilder** | the reassignable-func propagation channel that replaces the by-value smuggle (§4); a func field, not a port interface |

Notes: `worktreeFactory` feeds the per-run `RunPorts.Worktree` assembly (RT7) — confirm whether it wants a SharedHandles home or stays inside the Worktree-port build. `runner` also arrives as a per-run **param** to the dot/review functions (SSH-vs-local); the `deps.runner` reads are the *default/factory* — verify it is genuinely shared before homing. `tidGen` and `emittedEpics`/`emittedEpicsMu` are the RT18.9 preconditions (§3).

## §3 — RT18.9 precondition: `SharedHandles.TIDGen` (+ `emittedEpics`)

`tidGen *core.TransitionIDGenerator` (`workloop.go:265`) is a **pointer**; `SharedHandles.TIDGen: deps.tidGen` shares it by reference exactly like the 5 existing handles, so `shared.TIDGen` and the retained `deps.tidGen` (outer-loop KEEP sites `runWorkLoop:2801`, `adoptLiveRunSession:6553`) dereference the **same generator** — monotonicity (EM-018a) preserved. Cite `runports.go:330-332` in the commit body. Also update the deliberate-omission comment (`runports.go:414-416`).

Census (13 reads): `runbridge.go:112` (RT18.9), 10 in `beadRunOne` (RT18.10 → `shared.TIDGen`), 2 permanent KEEP. `runBridge`'s second blocker: `closeHook` calls `emitBeadClosedAndMaybeEpic(b.deps, …)` which forwards into `maybeEmitEpicCompleted` reading `deps.emittedEpics`/`emittedEpicsMu` (`workloop.go:441-442`, homeless). Resolution: fold `emittedEpics`+`emittedEpicsMu` into SharedHandles (same by-reference argument as TIDGen) and re-sign `emitBeadClosedAndMaybeEpic`/`maybeEmitEpicCompleted` onto the bundle — **or** the minimal option (a): keep a single `deps` field on the bridge for that one call, flagged explicitly so review does not read the surviving field as an incomplete drop. Prefer folding (kills the smell RT18 exists to remove).

## §4 — RT18.11: delete the `launchSpecBuilder` smuggle (reconciled with RT17-as-landed)

The routed resolution still lives inline in `beadRunOne` at `workloop.go:3931-3945` (guard `:3931`, `harnessRegistry` guard `:3932`, routed assign `:3933`, claude assign `:3943`), rebound `rp.Launch = launchPort(deps.launchBuilder())` `:3950`, consumed single-mode at `:4302`. The four sub-drivers (`reviewloop 317/1263`, `dot_cascade 1438`, `dot_gate 358`) read `deps.launchBuilder()` as a **reassignable func**, per-node swap it via `routedLaunchSpecBuilder(deps.harnessRegistry,…)`/`pinnedHarnessLaunchSpecBuilder(…)`, then `if x==nil { x=claude.BuildLaunchSpec }`.

**The routing trap (load-bearing).** The by-value `deps.launchSpecBuilder` copy is the **sole** channel propagating routed/pinned selection to the sub-drivers. Deleting the mutation before threading a bundle channel → `deps.launchBuilder()` nil in prod → every codex/pi bead silently falls to `claude.BuildLaunchSpec` **with a green build**. Correct sequence:

1. **Precondition P (before RT18-S sub-drivers convert):** add `RunPorts.LaunchBuilder func(context.Context, shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error)` (raw func, not a port interface).
2. **Relocate the resolver to the goroutine caller** (`workloop.go:3052`, where `deps` is live), preserving the test carve-out byte-for-byte (`builder := deps.launchSpecBuilder; if builder == nil { …routed… / claude }`), then `rp.LaunchBuilder = builder; rp.Launch = launchPort(builder)` and pass `rp` into `beadRunOne`. `harnessRegistry` is reachable here because `deps` is still in scope (it is homed on SharedHandles by RT18-W anyway).
3. **In the sub-drivers:** `x := deps.launchBuilder()` → `x := ports.LaunchBuilder`; every reassignment / fallback / call stays byte-identical.
4. **Delete** the `:3931-3945` block + comments from `beadRunOne`.

## §5 — Freeze-gate choreography

- **`runloop-emitter-gate.sh` (RT16):** `PORT_RE` lacks `ports.Emitter`. On the FIRST re-signature (RT18-S), add `\bports\.Emitter\b` to `PORT_RE` in the same commit; the per-file PORT_SITES counts (`workloop 2, reviewloop 1, dot_cascade 2, dot_gate 2, runbridge 3, sub_workflow_runner 2`) stay satisfied by the 1-for-1 spelling swap — **do not lower them.**
- **`launchbuilder-freeze-gate.sh` (RT17):** RT18.11 lowers Tooth B (`deps.launchSpecBuilder = ` EXACT 2 → 0) and Tooth D (`deps.launchBuilder()` call sites → 0) deliberately; once the field, assignments, and accessor are all gone there is nothing to freeze — **retire the script** (delete it + its two Makefile lines in check-fast/check-short) and delete the now-dead `launchBuilder()` accessor, in the RT18.11 commit, saying so in the body.

## §6 — Sequencing (leaf-first; the DAG)

`RT18-C (clock)` → `RT18-W (widen; each field-family independent, land highest-read-count first: harnessRegistry, hookStore, substrate…)` → then the signature drops, leaf-first: `RT18.3 (executeCognitionGate+buildCognitionGateEval)` → `RT18.4 (dispatchDotGateNode)`; `{.4,.5} → RT18.6 (dotSubWorkflowRunner)`; `{.4,.5,.6} → RT18.7 (driveDotWorkflow)`; `RT18.8 (runReviewLoop)` independent; `RT18.9 (runBridge)`; `{.7,.8,.9} → RT18.10 (beadRunOne)` — **`beadRunOne` param must be named `handles`, not `shared`** (the `harness/shared` package is used pervasively in the body). RT18.10 & RT18.11 are **one atom** (the smuggle assignments live inside beadRunOne's body; dropping `deps` and deleting them cannot separate). **REPORTING RULE:** RT18 is INCOMPLETE until RT18.11 lands.

## §7 — Corrections to the E5 catalogue (for whoever refreshes it — note only, do not rewrite chunk bodies from here)

1. `launchPortFull()` never existed; RT17 landed `launchBuilder()`. Every RT18.11 sentence resting on it is void.
2. RT18.3–.11 are NOT writable as `(env, ports)` drops — ~15 homeless field-families block them; the "reads already converted by RT16/RT17" premise is false (`deps.` = 92/72/41, not 0/0/0). The missing work is RT18-W (§2).
3. The `deps.tidGen` escalation is incomplete: `harnessRegistry` (and the whole §3.1 list) are co-equal homeless blockers.
4. All RT18 line numbers stale +40–70 (decls, assignments, call sites — see the coordinate tables above).
5. RT18.5's "`//nolint` gocognit 196" mis-attributes `driveDotWorkflow`'s finding to `dispatchDotAgenticNode`; re-measure live per chunk.
6. RT18.10's "+the //nolint for gocognit 389" is already present at `workloop.go:3100` (RT15.5) — preserved/re-anchored, not added.
7. "Stop at RT18.10 = coherent green tree" is false — .10/.11 are inseparable.
