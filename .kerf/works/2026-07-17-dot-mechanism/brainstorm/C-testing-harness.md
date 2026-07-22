# Brainstorm C — In-process DOT integration test harness (agent-twin + git seam)

> Status: design proposal for the "robust testing" thread of the DOT-mechanism hardening spike.
> Addresses P2, P3, P6, P7 and gap6 (round-trip determinism). Constraints OFF — we may add
> first-class exported seams / a fuller harness helper rather than working only through capture stubs.

## TL;DR

The in-process cascade driver is **already reachable**: `daemon.ExportedDriveDotWorkflow(…, graph)` and
`ExportedDriveDotWorkflowFull(…, extraContext)` drive the REAL `driveDotWorkflow` in package `daemon_test`,
against a **real ephemeral scratch git worktree**, with a scripted handler. Roughly 30 `dot_*_test.go` files
already exercise verdict edges, traversal_cap, param substitution, role surfacing, and even the ①
resume/feedback back-edge (`dot_resume_feedback_hkwixms_test.go`, cases A/B/C).

**The gap is not "no harness" — it is that today's harness swaps the whole executable for `/bin/sh`
(`HandlerBinary:"/bin/sh"`), so the daemon's REAL per-handler argv/launchspec is never built or asserted.**
That is exactly why P3 (①) and P7 (handler matrix) and the c074 mis-route are still open: the shell handler
proves daemon-side file writing and cascade routing, but it can never prove that the argv the daemon builds
for claude/pi/codex actually points *that* agent at *its* brief (agent-task.md vs review-target.md vs
reviewer-feedback.iter-N.md), nor that a bead routes to the handler it declared.

**This proposal:** promote the harness from "swap the binary" to "keep the REAL launchspec, swap only the
terminal executable to a scripted, argv-recording, handler-faithful twin." That single change makes the
argv the object-under-test, unlocks the deterministic round-trip the real model cannot give (P2/gap6),
proves ① end-to-end per handler (P3), and gives the c074 mis-route a loud failure (P7).

---

## 1. The current seam map (what already exists — build on it, do not reinvent)

The DOT dispatch path has **three distinct injection points**, all already present:

| Seam | Where | Shape | What it controls | Used today by |
|---|---|---|---|---|
| **A. `launchSpecBuilder`** | `WorkLoopDepsParams.LaunchSpecBuilder`, consumed at `dot_cascade.go:1402` (`specBuilder := deps.launchSpecBuilder`) | `func(ctx, claudeRunCtx) (handler.LaunchSpec, claudeRunArtifacts, error)` | **Builds the REAL argv** (claude→`buildClaudeLaunchSpec`; pi/codex→`routedLaunchSpecBuilder`/`pinnedHarnessLaunchSpecBuilder`→harness registry). The `claudeRunCtx` carries `nodePrompt`, `extraContext`, `runner`, `handlerBinary`, resolved model/effort, phase (implementer vs implementer-resume vs reviewer). | Capture-and-abort stubs: `ExportedCaptureExtraContextBuilder`, `ExportedCaptureNodePromptBuilder`, `ExportedCaptureRunnerBuilder`, `ExportedMinimalLaunchSpecBuilder`. They read one field then return an error to short-circuit — they never let the run finish. |
| **B. `handler.Substrate`** | `WorkLoopDepsParams.Substrate`; `Substrate.SpawnWindow(ctx, SubstrateSpawn{Argv, Cwd, Env})` (`handler.go:399`) | interface: `SpawnWindow → SubstrateSession{Wait/Kill/Stdout/Outcome/PID}` | **Executes the built argv.** This is where the argv is faithful and where a twin process actually runs. `Stdout()==nil` selects the tmux (watcher-less) path. | `em015FixtureNilStdoutSubstrate` (runs real argv as subprocess, nil stdout); `fakeSubstrate` (records call). |
| **C. `HandlerBinary`/`HandlerArgs`** | `WorkLoopDepsParams.HandlerBinary` (fed into `claudeRunCtx.handlerBinary` → `buildClaudeLaunchSpec`) | `string` + `[]string` | **Replaces the executable wholesale.** Setting `/bin/sh` + a script path is the current shortcut — it makes the argv `[/bin/sh <script>]`, discarding the real claude/pi/codex argv shape. | Every `dot_*_test.go` and `scenario_reviewloop_*` today. |

**Git seam (already chosen):** real ephemeral scratch repo + worktree via `rlcFixtureSetup` /
`rlFixtureWorktree` — `git init`, real commits with `-c user.email=… --no-gpg-sign`, real `git worktree`.
The implementer "commits" by writing a file and running real `git add/commit`; HEAD-advance, diff-hash
no-progress, and commit-gate `test -f` all run against real git.

**Twin binaries (already built & parity-gated):** `cmd/harmonik-twin-{claude,codex,pi,generic,session}`.
`pi_twin_parser_drive_test.go` proves the pi twin's NDJSON drives the REAL pi parser
(`piSessionIDInterceptor`, `capturePiUsage`) to the same session-id / agent_end / token result a live pi
session would — and pins a byte-identical committed fixture. `TwinBinaryPath()`
(`scenariotest.go:490`) resolves the built twin or signals skip.

**Conclusion:** we are not starting from zero. We are replacing seam **C** (whole-binary swap) with a
disciplined **A+B** combination (real builder + twin-as-executable), and adding a thin harness helper +
per-handler twin-outcome scripting on top.

---

## 2. The agent-twin seam — design

### 2.1 Where the seam lives

A new test-only helper in `internal/daemon/scenariotest` (NOT behind a build tag; it is `_test`-package
support code, and `//go:build scenario` gates the slow tests that use it — same convention as
`scenario_reviewloop_*`). It composes the two existing production seams:

1. **Keep the real builder (seam A).** Inject a `LaunchSpecBuilder` that is a thin *wrapper*, not a stub:
   it calls the genuine builder for the run's resolved harness (`buildClaudeLaunchSpec` for claude;
   `routedLaunchSpecBuilder(piCfg, registry)`-equivalent for pi/codex), gets back the **real
   `handler.LaunchSpec` (real Binary, real Args, real seed prompt)**, then:
   - tees a copy of the built `LaunchSpec` (+ the `claudeRunCtx.phase`, node id, resolved harness) into a
     thread-safe recorder the test reads afterward — this is the **argv-assertion tap**;
   - rewrites **only `LaunchSpec.Binary`** to the twin binary for that harness family (claude→
     `harmonik-twin-claude`, pi→`harmonik-twin-pi`, codex→`harmonik-twin-codex`), leaving **`Args`
     untouched** so the seed-prompt argv, `--model`/`--effort`/`--provider` flags, and file references
     stay byte-identical to production.
2. **Run it through the real Substrate (seam B).** Provide a substrate that executes the (now
   twin-Binary) argv as a real subprocess in the worktree — `em015FixtureNilStdoutSubstrate` already does
   exactly this; reuse it (nil stdout = tmux path) or a stdout-carrying variant when we want the real
   NDJSON→parser path (pi/codex session-id capture).

The twin is thus invoked **with the daemon's real argv**, in the **real worktree**, reading the **real
on-disk briefs** the daemon wrote. Faithfulness is structural, not asserted-by-convention: the argv is the
production argv because the production builder produced it.

### 2.2 How the twin returns scripted outcomes

The twin is deliberately **dumb**; the *test* supplies a script. The twin's only inputs are (a) its argv,
(b) the files on disk, (c) a small **outcome table** the harness materializes into the worktree before the
run (e.g. `.harmonik/twin-script.json`). The table is keyed by **(node-role, iteration)** and each row says:

```
implementer, iter 1  → { commit: ["LINE-A.txt"], commitMsg: "…" }
reviewer,    iter 1  → { verdict: "REQUEST_CHANGES", flags:[…], notes:"…MARKER…" }
implementer, iter 2  → { assert_feedback_file_present: true, commit: ["LINE-B.txt"] }
reviewer,    iter 2  → { verdict: "APPROVE" }
```

On each launch the twin: reads its role from argv/brief, reads the current iteration from a counter it
maintains in the worktree (mirrors the existing `CNT_FILE` idiom), looks up its row, performs the action
(implementer: real `git add/commit`; reviewer: write `.harmonik/review.json` via the same schema the real
reviewer uses — ideally by shelling `harmonik write-review-verdict` so that path is exercised too),
records what it *saw* (argv + brief digest + feedback-file presence) into a **twin-observation log** the
test reads, then emits handler-faithful NDJSON on stdout and exits.

**Why this beats a real model (the P2/gap6 core):** because the implementer body leaks the reviewer rubric
(P1), a capable real model does everything up front and the reviewer approves on pass 1 — the back-edge
never fires. The twin **ignores the leaked rubric and does exactly what its table row says**, so a
REQUEST_CHANGES → resume → APPROVE round-trip is forced deterministically regardless of what the body
contains. Determinism is the whole point of the twin.

### 2.3 Injection mechanism — NOT a build tag

- **Injection = dependency injection through `WorkLoopDepsParams`** (`LaunchSpecBuilder` + `Substrate`),
  which already exist. No build tag on the production path; no conditional compilation of daemon code.
- **`//go:build scenario`** gates the *tests* (they spawn subprocesses and touch real git — slow), exactly
  like `scenario_reviewloop_em015de_hkintln_test.go`.
- **Twin discovery** reuses `TwinBinaryPath()` (env `HARMONIK_TWIN_*` override → repo-root prebuilt binary
  → skip). Extend it to resolve all three families.
- We SHOULD add a first-class exported constructor, e.g.
  `scenariotest.NewTwinHarness(t, HarnessKind, outcomeTable) → (WorkLoopDepsParams patch, *Recorder)`,
  so a test is ~10 lines and every test asserts through the same recorder API. This is the anti-rot
  surface (see §5).

---

## 3. The git seam — recommendation: **ephemeral scratch git, not a mock**

**Recommend ephemeral scratch git** (already the de-facto choice). Rationale:

- **Fidelity dominates here.** The cascade's correctness *is* git semantics: HEAD-advance detection,
  diff-hash no-progress (EM-015e), `non_committing` (WG-041), commit-gate `test -f`/shell exit, the
  `Refs:` trailer requirement on resume, review base/head SHA ranges. A git mock would have to
  re-implement all of that; you would then be testing the mock, and the mock is exactly where the subtle
  bugs the harness must catch would hide.
- **Speed cost is small and bounded.** `git init` + a couple of commits is millisecond-scale; the real
  time cost is the per-node subprocess + the `stopHookGrace` wait on the watcher-less path (~3 s/node,
  already accepted in the reviewloop scenarios). That is a `scenario`-tagged / non-`-short` cost, not a
  unit-test cost.
- **It is already proven.** Every `dot_*_test.go` uses it; no new infrastructure risk.

Where a mock *does* belong: the narrow, already-mocked collaborators — `stubBeadLedger`,
`stubEventCollector`, the sealed adapter registry. Keep those stubbed; keep git real.

(One speed lever if the matrix gets expensive: a single `git init` template repo cloned per case, and
run the twin via the nil-stdout substrate to skip NDJSON parsing when the test only asserts routing.)

---

## 4. What the harness must be able to assert (the coverage contract)

Each item below maps to a concrete tap the design already exposes.

| # | Assertion | How the harness proves it |
|---|---|---|
| 1 | **Per-role input routing — implementer brief ≠ reviewer brief, NO leak** | Twin records the brief it read (`agent-task.md` for implementer, `review-target.md` for reviewer). Harness asserts (a) the reviewer's distinctive rubric token appears in the reviewer's brief, and (b) does **NOT** appear in the implementer's brief. *Today this assertion FAILS (P1 leak) — that is correct and desirable: the harness must be able to encode the current leak as known-RED and flip to GREEN when per-role routing lands.* |
| 2 | **Param substitution (WG-045/046)** | Twin greps its brief for the substituted value and for any residual `__TOKEN__`. Already covered by `dot_param_subst_goal_hk4bn9o_test.go`; the twin variant additionally proves the value survives into the **real argv/seed**, not just `agent-task.md`. |
| 3 | **Verdict edges (WG-010/019)** | Twin reviewer emits scripted APPROVE/REQUEST_CHANGES/BLOCK; harness asserts `result.TerminalNodeID` (APPROVE→`close`, BLOCK→`close-needs-attention`) and that REQUEST_CHANGES routes the back-edge to the implementer node. |
| 4 | **Resume/feedback back-edge — ① end-to-end (P3), PER HANDLER** | Twin implementer on iteration ≥2 records: (a) it received the **resume** argv/seed (`implementerResumeSeedPrompt` for pi/codex; rewritten `agent-task.md` phase=implementer-resume for claude), and (b) `reviewer-feedback.iter-<N-1>.md` exists and carries the reviewer's notes. This is the assertion the `/bin/sh` harness structurally *cannot* make, because it never builds the real seed. Run it for claude AND pi AND codex → closes P3 across the fleet. |
| 5 | **Terminal-node selection (WG-021/022)** | Assert `result.TerminalNodeID` ∈ declared terminals; assert `close` has exactly one inbound (the review-floor WG-050) by construction of the fixtures. |
| 6 | **traversal_cap enforcement (WG-028/EM-043)** | Twin reviewer emits REQUEST_CHANGES ×3 → assert cap-hit → `close-needs-attention`, `needsAttention=true`, and `iteration_cap_hit` event (mirror `TestScenario_EM015d_CapHit`). |
| 7 | **Handler-family faithfulness / c074 guard (P7)** | From the **recorder** (seam A tap): assert the built `LaunchSpec.Binary` basename matches the handler the node/bead declared. A codex-labelled bead whose real argv is `harmonik-twin-claude` fails loudly — the exact c074 mis-route. |

**The deterministic round-trip test (the headline):** a two-line fixture
(`implement → review [RC→implement cap3][APPROVE→close]`) + a twin table
`{impl@1:commit A}{rev@1:REQUEST_CHANGES}{impl@2:assert feedback present, commit B}{rev@2:APPROVE}`
drives implement→review→**back-edge**→resume→review→close with **zero reliance on model behavior**. This
is gap6, finally forced.

---

## 5. Handler-matrix coverage (P7) — how to parameterize

The matrix is a table of **handler descriptors**, and the SAME wiring assertions (§4 items 1,3,4,7) run for
every row:

```go
type handlerCase struct {
    name        string            // "claude" | "pi" | "codex"
    nodeAttrs   map[string]string // harness= pin OR bead label the run carries
    twinBinary  string            // expected basename: harmonik-twin-<x>
    argvSig     func(t, LaunchSpec) // family signature assertions
}
```

- **`argvSig` encodes each family's known argv shape** (this is where the fleet asymmetries live and must
  stay locked — see `crossharness_empty_model_test.go`): claude → carries `--model`/`--effort` +
  agent-task.md file ref; pi → `--provider <p> --model <p/id>`, empty model is a hard error; codex →
  omits `--model` on the account-default path, seed prompt is positional. Reuse the exported
  `ExportedBuild{Codex,Pi}LaunchSpec` seams for the pure-argv rows and the full cascade for the wiring rows.
- **The matrix must drive the REAL harness-resolution path** (`resolveHarnessAgentTypeQuiet` → node
  `harness=` pin / bead `harness:` label / `harnessRegistry`), not a test shortcut — otherwise it cannot
  catch a mis-route. So pi/codex rows need a **sealed harness registry populated with pi/codex config**
  (extend `NewSealedAdapterRegistryForTest` / add `NewTwinHarnessRegistry(t, kinds…)`).
- **Parameterization:** `for _, hc := range handlerCases { t.Run(hc.name, func(t){ … }) }`. Each subtest
  builds the same DOT topology, swaps in `hc.nodeAttrs`, runs the twin harness for `hc`, asserts the
  seven-point contract, and asserts `recorder.Binary(node) == hc.twinBinary`.
- **`reviewer_harness` axis:** add a column so a claude-implementer + codex-reviewer graph is a row —
  the review-loop `reviewer_harness` override (`dot_cascade.go` effective-harness precedence) is itself a
  mis-route surface.

Net: adding a handler later = **one row**, and the c074 class of bug is caught for every handler on every
wiring assertion.

---

## 6. How the harness must EVOLVE with the typed-param / feedback redesign (anti-rot)

The harness has to be the thing that lets us safely extend DOT (the "dictionary" / typed per-role inputs,
WG-045 growth, and the DOT-mode feedback-delivery gap). Four abstractions keep it from rotting:

1. **Assert on a typed "twin input manifest," not on rendered markdown substrings.** Today assertions grep
   `agent-task.md` for a marker — that couples every test to the current prose format, so the input-model
   redesign would break dozens of tests for no semantic reason. Instead have the daemon (or the harness
   wrapper) emit a small **typed record of what each role was given** — `{role, iteration, brief_source_keys,
   feedback_path, params{…}, node_prompt_used}` — and let the twin/recorder assert on *keys*. When inputs
   become a typed dictionary, the manifest gains keys and existing assertions still hold. **This is the
   single most important decision** and directly serves the per-role-routing redesign: `brief_source_keys`
   for the implementer must not include the reviewer's key = the leak oracle, format-independent.
2. **Role×iteration outcome table as the twin's ONLY script surface.** New node types (future gate/
   sub-workflow agentic variants) add rows, not twin logic. The twin never grows a special case.
3. **A single handler-descriptor registry (§5)** — one place maps harness → {twin binary, argv signature,
   resume-seed expectation}. New handler = one row; no assertion duplicated across files.
4. **Keep the git seam real** so new deterministic semantics (auto_status WG-053 probes, non_committing,
   future diff/coverage gates) are exercised by real git for free, with no harness change.

Plus one **product** lever the harness unlocks: the DOT-mode feedback-delivery gap (§6 of the specs
findings — `reviewer-feedback.iter-N.md` is "review-loop-mode only" per EM §7.5, yet `dot_cascade.go`
already writes it, hkwixms). The harness is where that contradiction gets pinned as a decision: once the
spec is reconciled, the round-trip test is its executable proof.

---

## 7. What stays REAL vs stubbed

| Component | Real | Stubbed / twinned | Why |
|---|---|---|---|
| `driveDotWorkflow` + edge cascade + `DecideNextNode` | ✅ real | | The system under test. |
| Launchspec builders (claude/pi/codex argv + seed) | ✅ real | wrapper tees + rewrites Binary only | Argv faithfulness is the whole point (P3/P7). |
| Git (repo, worktree, commit, diff-hash, gate) | ✅ real (ephemeral scratch) | | Cascade correctness == git semantics (§3). |
| Agent executable | | 🔁 handler-faithful twin | Deterministic scripted outcomes (P2). |
| Harness/NDJSON parser (pi session-id, usage) | ✅ real (stdout-carrying substrate) | | Proven pattern (`pi_twin_parser_drive_test`). |
| Bead ledger, event bus, adapter registry | | 🔁 stub/sealed | Not under test; already stubbed. |
| Model / API | | 🔁 none needed | Twin replaces it. |

---

## 8. Costs & risks

- **Runtime:** real subprocess + real git + `stopHookGrace` (~3 s/node on the watcher-less path) → a
  full round-trip is ~10–20 s; the handler matrix multiplies by 3. Mitigation: `//go:build scenario`,
  keep out of `-short`, run in CI; use the nil-stdout substrate for routing-only rows to skip NDJSON.
- **Twin↔real drift:** a twin could diverge from the real harness's parse contract. Mitigation: the
  existing **twin-parity gate + committed NDJSON fixtures** already guard this; extend the same discipline
  to codex/claude twins if not already covered.
- **Argv-faithfulness bug in the wrapper:** if the wrapper rewrote `Args` (not just `Binary`), fidelity is
  silently lost. Mitigation: assert in the harness itself that `recorder.Args == realBuilder.Args` for one
  golden case; rewrite Binary ONLY.
- **Positional-seed compatibility:** codex/pi deliver the task as a positional seed argv; claude via
  `agent-task.md`. The twin must honor each family's convention (read the file for claude; read the
  positional seed for pi/codex). The existing twins already model this — verify before matrixing.
- **Prebuilt-binary dependency:** `TwinBinaryPath()` skips when the twin isn't built → a green run that
  silently tested nothing. Mitigation: a Makefile/CI target that builds all twins before the scenario
  suite, and a harness that **fails (not skips)** when `HARMONIK_TWIN_STRICT=1`.
- **Scope risk:** ~80% of the driver is already covered by shell-handler tests. The twin harness should
  NOT re-cover them — it should target precisely the argv-level assertions (§4 items 1,4,7) the shell
  handler cannot make, so we add value, not duplication.

---

## 9. Concrete first test list (build order)

1. **`TestDotTwin_DeterministicRoundTrip_claude`** — the headline. implement→review[RC]→resume→review[APPROVE]→close,
   twin-scripted; asserts back-edge fired, `reviewer-feedback.iter-1.md` seen at resume, `close` terminal. (gap6, P2)
2. **`TestDotTwin_ResumeSeedReferencesFeedback_{claude,pi,codex}`** — ① end-to-end per handler: assert the
   real resume argv/seed references `reviewer-feedback.iter-<N-1>.md`. (P3, closes the 8dbe5a17 unit-only gap)
3. **`TestDotTwin_HandlerMatrix_BinaryMatchesDeclared`** — the c074 guard: for each of {claude,pi,codex},
   assert `recorder.Binary(implementer_node)` == expected twin binary; a mislabeled bead fails loudly. (P7)
4. **`TestDotTwin_NoRubricLeakToImplementer`** — encodes P1 as known-RED now; flips GREEN when per-role
   routing lands. The redesign's acceptance oracle.
5. **`TestDotTwin_TraversalCapHit`** — REQUEST_CHANGES ×3 → `close-needs-attention` + `iteration_cap_hit`.
6. **`TestDotTwin_ParamSubstReachesArgv`** — WG-045 value survives into the real seed argv, not just agent-task.md.
7. **`TestDotTwin_ReviewerHarnessOverride`** — claude implementer + codex reviewer graph routes each role
   to its declared twin (reviewer_harness precedence).

Items 1–3 are the load-bearing three; 4 is the redesign oracle; 5–7 harden the surface.
