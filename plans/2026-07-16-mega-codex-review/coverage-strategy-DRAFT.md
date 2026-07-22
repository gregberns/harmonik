# Mega-Review — Testing & Coverage Strategy (DRAFT)

> **Purpose.** The operator's mega code-review "needs to cover testing as well, to make sure we have
> good coverage in the right places." This doc is the coverage arm of the mega-review: where coverage
> is strong / weak / theater / missing today, where it MUST be strong given risk, how each reviewer
> verifies "good coverage in the right places," how this composes with the M6 controlled-testing
> harness (does NOT duplicate it), and the ranked coverage-improvement targets.
>
> **Method.** Statement-coverage measured live (`go test -cover`, 2026-07-16) + three parallel
> read-throughs of the actual test bodies (daemon/run seams; ack-free IO + remote; theater +
> hook/policy). Builds on the 2026-07-12 census (`plans/2026-07-12-codebase-census/REPORT.md`) and the
> code-revamp milestones (`plans/2026-07-13-code-revamp/`).
>
> **DRAFT — for adversarial review.** Coverage % are statement-level and are an input, not the verdict;
> the load-bearing claims are the *quality* classifications (does the test exercise real product
> behavior at the right seam) with file evidence.

---

## 0. The one thing to internalize first

**The problem is NOT low coverage numbers — it is coverage *quality* and *placement*.** Statement
coverage is high almost everywhere measured today: core 93.1%, keeper 83.6%, lifecycle 77.8%, handler
81.9%, and the rebuilt seams 87–98%. The census's finding — "I don't know if what it's fixing is real"
— was never about a red suite. It was about two failure modes that a green % hides:

1. **Theater** — tests that assert on hand-transcribed constants or spec-prose regex and never `exec`
   the product (operatornfr, specaudit). A high % here is meaningless.
2. **Fake-anchored coverage at ack-free boundaries** — the orchestration/decision logic around tmux
   paste and remote SSH is well-tested *against fakes that always succeed*, so it cannot reproduce the
   exact failure the code exists to handle (paste lands in a not-ready TUI; ssh exit-255 mid-op). The
   byte-crossing itself is untested product code.

So the review's coverage question is not "what's the number" but **"does the green here mean the
product behaves, or does it mean a fake behaved?"** Every review unit below is scored on that axis.

The review-unit spine is the census's Keep/Simplify/Rebuild/Delete areas
(`plans/2026-07-12-codebase-census/REPORT.md` §2). The mega-review decomposition should inherit those
names verbatim so this coverage map plugs straight in; each item below is tagged with its unit.

---

## 1. Coverage map — per review unit

Legend: **STRONG** = tests drive real product behavior at the right seam · **WEAK** = thin or
wrong-altitude · **THEATER** = asserts on static/spec data, never execs product · **MISSING** = no
meaningful test · **FAKE-ANCHORED** = drives real logic but only against a fake that can't reproduce
the real failure mode.

### RU: `daemon-workloop` + run seams  — census **Rebuild** (M3 extraction largely landed)

| Sub-unit | Coverage | Quality | Evidence |
|---|---|---|---|
| `internal/runexec` (pure Run/Dispatch reactor) | 95.6% | **STRONG** | `run_test.go` drives the real terminal spine: `HappyCloseSpine`, `PreLaunchFailureReopenSpine`, `EscapeReopenEmitsEscape`, `MergeRetryThenExhaustedReopen`, `TotalityNoPanic`, `CloseLadderByteIdentical`. `dispatch_test.go`: `LaunchTimeoutNotSilent`, `WorkingStallKills`, `TerminalExclusivity`, `DuplicateAckDropped`. |
| `internal/runexectest` (fault matrix) | STRONG | **STRONG** | Replays `substrate.Twin` NDJSON into the real reactors under `FakeClock`; 102/102 cells assert never-silence (RSM-INV-001/002); N=10 clean-resume oracle. Real behavior, no theater. |
| `internal/mergeq` (merge exclusion) | 87.0% | **STRONG** | `TestFIFOOrderUnderConcurrentSubmits` (N=25, `-race`, non-overlap counter). The "no mutex across network/build IO" hazard is eliminated by design (single owner-goroutine over a channel) and the test verifies it. |
| `internal/orchestrator` (M5 queue-selection) | 89.9% | **STRONG** | Tests call the real `SelectNextQueue`/`EagerFillTarget`/`FirstPendingGroupIndex`; wired into the live loop at `workloop.go:1437,1499`. |
| `beadRunOne` (workloop.go:3072) | **63.7%** | **WEAK** | Covered integration-only via the daemon E2E families; ~36% unexercised. No fast unit harness. |
| `runWorkLoop` (workloop.go:1508) | 71.9% | acceptable | Integration-covered by the workloop_* families (wave, drain, semaphore-race, stranded-inprogress). |
| **`runbridge.go` (RT7 shell↔reactor bridge)** | **~via beadRunOne only** | **MISSING** | **No `runbridge_test.go` exists.** The glue that maps shell-classified events → machine events → `br` close/reopen — the terminal done/reopen *decision* — has zero dedicated test; it rides entirely on `beadRunOne` at 63.7%. **Single highest-value gap in this unit.** |
| `runshell.go::fireOnCancel` | 0% | MISSING | Shutdown-cancel timer flush; flagged as an RT12 follow-on in `RT11-coverage-record.md`. |

Recorded floors (durable): `M1-5-coverage-baseline.md` (daemon 73.5% @ `32791808`),
`RT11-coverage-record.md` (runexec 95.4 / mergeq 87.0 / daemon 74.3 post-refactor).

### RU: `tmux-io` (pasteinject/tmuxsubstrate)  — census **Rebuild (input channel only)**

**Split verdict — the defining pattern of this whole review.**

- **Watchdog/retry orchestration — STRONG but FAKE-ANCHORED.** ~37 `pasteinject_*`/`tmuxsubstrate_*`
  tests drive the failure-handling logic (seed-verify land loop, stale-pane fast-fail, heartbeat-kill,
  reseed-Enter, budget ceilings) including simulated dropped-Enter (`hk76n5g`), dead-pane (`hk1too`),
  heartbeat-never (`hk7srrd`). **But every one drives through interface stubs**
  (`quitSender`/`enterSender`/`paneCapturer`/`paneLivenessChecker` doubles, `fakeTmuxAdapter`). They
  test the decision logic around IO, never the IO.
- **The real paste primitive — the load-bearing gap.** Byte delivery lives in
  `internal/lifecycle/tmux/osadapter.go` (`LoadBuffer`/`PasteBuffer`/`SendKeys*`/`CapturePane`).
  `osadapter_test.go` exercises argv-construction + exec + `ErrTmuxFailure` against a **fake `tmux`
  shell script on PATH that succeeds by exit code**. This *structurally cannot reproduce the ack-free
  hazard* — `tmux paste-buffer` returns exit-0 the instant it hands the buffer to the pane, whether or
  not the not-yet-ready TUI absorbed it. A fake returning 0 proves the daemon *called* paste, never
  that it *landed*. **`SendKeysEnter`, `SendKeysQuit`, `CapturePane`, `WriteToPane` have NO
  adapter-level test at all.**
- **The only real paste-into-real-Claude test** is `internal/daemon/e2e_real_claude_single_test.go`:
  double-gated `//go:build e2e_real_claude` AND `t.Skip` on missing binary/key/tmux. Never runs in a
  default `go test ./...` or default CI.

### RU: `remote` (SSHRunner, reversetunnel, code-sync)  — census **Rebuild** (M4 in flight, T1 unproven)

**STRONG contract/argv coverage / MISSING real-transport coverage — same shape as tmux-io.**

- SSH argv quoting (`runner_ssh_quote_hkfxy9_test.go` + `_adversarial`) — **STRONG** unit.
- Remote ack conformance (`remote_ack_conformance_m4c2_test.go`) — **STRONG invariants / fake
  transport**: asserts `Delivered` never synthesizes acceptance, acceptance only via async
  `agent_input_acked`, ssh-disconnect → `agent_input_stale`-class within bound — but over a
  `remoteAckFixtureAdapter` and a synthesized `sh -c "exit 255"`, not a real ssh drop.
- Code-sync (`codesync_rs_b8_test.go`) — **WEAK/argv-only**: verifies git call *order* with **git
  fully mocked** (the scenario file's own doc comment says so).
- `worker_offline` emission (`remote_substrate_b11_test.go`) — **THEATER**:
  `TestRSB11_WorkerOfflinePayload_Fields` builds a `WorkerOfflinePayload{...}` literal and asserts each
  field equals what it just assigned (field-echo). `EmitWorkerOfflineEvent`'s real body never runs.
- **The localhost-SSH E2E skips-to-green by default.** `scenario_remote_substrate_localhost_test.go`
  is `//go:build scenario` (armed-only) and `rsb12RequireSSHOrSkip` (line 250) probes `ssh localhost
  true` — **on failure it `t.Skipf`s green (line 260)**; only `HARMONIK_REQUIRE_REMOTE_E2E=1` flips it
  to `t.Fatalf`. When it *does* run it is genuinely strong (real ssh round-trips, "worker commit lands
  on box-A main" assertions). `scenario_remote_substrate_t4_claude_test.go` is further gated behind
  `HARMONIK_T4_WORKER=<host>`.

### RU: input-ack seam (M2 rebuild — `handler/input_port`, `codexdriver`, `codexreactor`)

**STRONG invariants, but anchored only to a self-authored twin.**

- The ack-seam invariants (`agent_input_stale` bounded terminal, `Rejected`, async `agent_input_acked`)
  are genuinely and rigorously tested across `core/agentinputevents_roundtrip_test.go`,
  `codexinput/reactor_test.go`, `codexdriver/driver_test.go` (86.1%), `codexreactor` (94.1%),
  `codextest/l2_fault_matrix`. `input_port.go`'s "MUST block until terminal; MUST NOT synthesize
  acceptance" contract is verified.
- **Systemic risk: no real-codex anchor on the INPUT direction.** The codex input path is tested almost
  entirely against `internal/codexdigitaltwin` (replays a captured JSONL corpus — real recorded bytes,
  not fiction, but cannot catch protocol drift in a newer codex). `codextest/l3_live` is
  `CODEX_LIVE=1`-gated and the only live turn (`l3_live_hkoe86p`) is **output-direction only**. The
  input driver has no real-codex behavioral anchor.

### RU: hook/policy seams (rebuilt) — census-adjacent, high risk

**ALL STRONG — no new coverage needed here.** `hook` 95.5% (`NewSessionStore`/`Dispatch` driven),
`policy` 98.5% (real decisions: `StepRateLimit`, `SleepVeto`, `ClassifyDrain`, `ParseGateVerdict`,
`GateEvalFailureOutcome`, `BudgetExhaustedTrips`, `BackoffDuration`), `hookrelay` 86.6% (`Run` driven
48× incl. TCP endpoint), `hooksystem` 82.4% (`NewDispatcher`/`Emit`/`PersistHookVerdict` at the seam).
The event/decision behavior at the seam is genuinely exercised.

### RU: `test-bloat` (operatornfr/specaudit/scenario) — census **Simplify (mostly delete)**

- **`internal/operatornfr` — ~65-70% THEATER, STILL LIVE IN THE DEFAULT SUITE.** 37 test files / ~13.6k
  test LOC over ~1.06k product LOC; **zero build tags** — it all runs in `go test ./...`. Signature:
  each test defines its own fixture constant hand-transcribed from spec prose, then asserts on it
  (`TestON047_DefaultsTableHasFiveRows` checks `len(fixture) != 5`). "17 of 37 import a product
  package" is a red herring — the import is usually a token `LookupExitCode(...)` call after which the
  body asserts on transcribed fixtures. **~24-27 of 37 files are theater (~9-10k LOC).** Genuine
  carve-outs to KEEP: `exitcode.go`, `commandcodes.go`, `securitypolicy_on006_on026.go`,
  `sandboxinvariant_on024.go`, and the real secrets **integration** tests (`oninv003_sx9r70_test.go`
  drives `bus.Emit`/`Seal` + `ScanRegisteredPayloadsForSecretFields` and asserts real redaction).
  **M1-2/M1-3 (the deletions) are PARKED/operator-gated** (`COORD.md` c035 step 3) — this theater is
  the one that still inflates the live green suite.
- **`internal/specaudit` — 37k LOC THEATER but correctly NEUTRALIZED (M1-1 landed).** 129 of 132 files
  carry `//go:build specaudit` (commit `32791808`) — out of the default suite. Dormant, not deleted.
  **3 stragglers still untagged** and run by default: `ar025_agent_type_regex_test.go`,
  `hqwn57_eventbus_interface_test.go`, `sh_inv005_declarative_loadable_test.go` (these do byte-identity
  vs a real product constant, more defensible, but stragglers if the intent was zero-in-default).
- **`internal/scenario` (42 files) — MIXED, mostly scaffolding.** Only 5 drive a real `daemon.Start`;
  **37 are structural corpus checks** (build `core.*` struct literals / parse the corpus and assert —
  `conformancecorpus_test.go` says so: "structural corpus check, NOT an execution check"). The package
  *does* carry real harness product code (`asserteval.go`), so those 37 test the harness, not product
  behavior.
- **REAL scenario coverage lives elsewhere:** `test/scenario` (5 files — STRONG, drives `daemon.Start`
  end-to-end) and `internal/workflow/scenario` (~11 files — STRONG, drives `DecideNextNode` 577× over
  real `.dot` files).

### RU: `queue` — census **Simplify** ("the one well-tested island")

**Reputation vs reality gap worth flagging: 68.8% statement coverage** — the lowest of the "keep"
islands, below its "genuinely tested" billing. The tests it has are real (spec-pinned), but the
number says there is untested surface here; a reviewer should confirm the two-writer path (B1 fix,
`COORD.md` c007) and `HandlerAdapter` are covered.

### RU: other Simplify units (context for completeness)

- `keeper` 83.6%, `lifecycle` 77.8%, `handler` 81.9%, `workspace`, `brcli` — statement coverage
  healthy; not deep-audited here (out of the three-agent scope). `workers` is **STRONG** (real
  OK→ARMING→BREACHED transitions over injected clock; real health-probe→disable emit) **except**
  `worker_offline` (theater, above) and `agentlaunch/SpawnKeeperWindow` (untested body).
- `core` 93.1% — but the census flagged the event-registry decode/validate surface + 388-line
  `pertypecompat` table as **production-dead**. High % here partly measures *tests exercising dead
  code* — a reviewer should not read core's 93% as "well-protected product"; confirm the covered code
  has live consumers.

---

## 2. The "right places" — where coverage MUST be strong, and where it is NOT

Ranked by the census's risk model (blast radius × incident history × ack-free-ness):

| Right place (must be strong) | Why | Status today |
|---|---|---|
| **Run terminal spine / state machine** (runexec, mergeq, runexectest) | The 63-fix/20d treadmill; false-close/resume-hang live exemplars | ✅ **STRONG** — the M3 extraction is the model. Keep the floor. |
| **`runbridge.go` — shell↔reactor terminal close/reopen** | The exact seam where the census caught a *fabricated* done-status (hk-2hfyt closed with fix absent) | ❌ **MISSING** dedicated test — only transitive via beadRunOne 63.7%. **#1 target.** |
| **tmux paste *landing*** (the ack-free input channel) | 44 incident beads, "paste, assume success, hang forever" | ❌ **FAKE-ANCHORED** — real primitive only tested vs exit-0 fake; Enter/Quit/Capture untested. Real test is gated-off. |
| **Remote SSH transport failure modes** (exit-255, offline, tunnel-bind, misfire) | 166 fix commits; box-A mutex state; M4 T1 unproven | ❌ **MISSING real transport** — all synthesized/argv-only; localhost E2E skips-to-green. |
| **Input-ack contract on a real codex** | The whole point of AIS-INV-001 (output-or-stale) | ⚠️ invariants STRONG but **twin-only anchor**; no live input-direction test. |
| **hook/policy decisions** (gate verdict, drain, rate-limit) | Governs every run's control flow | ✅ **STRONG** — leave alone. |
| **The green suite meaning anything** | "green was partly theater" | ⚠️ specaudit neutralized; **operatornfr theater still live** in default suite. |

**The through-line:** every place that is *ack-free or terminal-decision* (runbridge, tmux landing,
ssh transport, live input-ack) is exactly where coverage is fake-anchored or missing — because those
are precisely the paths a unit test with a fake *cannot* prove. That is not an accident to patch with
more unit tests; it is the case for M6's controlled harness (§4).

---

## 3. How the mega-review VERIFIES coverage adequacy (per-reviewer checklist)

Each review unit gets a reviewer. Coverage is not "is there a test file" — it is these checks, in
order. A unit **fails the coverage gate** if any MUST check fails.

**For every review unit, the reviewer MUST:**
1. **Seam check.** Name the highest-risk behavior in this unit (the terminal decision, the byte
   delivery, the failure classification). Point to the test that drives *that behavior through product
   code*. If the only test drives a fake/stub in its place → **FAKE-ANCHORED, flag it.**
2. **Theater check.** Open 3 random test files. If a test asserts on a constant/fixture it defined in
   the same file, or greps spec markdown, and never calls a product function that does work →
   **THEATER, flag for deletion (not repair).**
3. **Skip-to-green check.** Grep the unit's tests for `t.Skip`/`t.Skipf`/`//go:build`. For each: does
   it skip on a *missing external prerequisite* (acceptable) or does it mask a path that would
   otherwise be exercised (false gate)? Name every armed-only/env-gated test and state what runs it.
4. **High-risk-path check.** For the risk paths in §2 that map to this unit: is the path covered, and
   is the coverage real (check 1) not fake (check 1)? A high % with all-fake anchors is a **fail**.
5. **Number sanity.** Record the statement % but do NOT treat it as the verdict. Reconcile it: does a
   93% unit have dead code inflating it (core/eventreg)? Does a 63.7% entry-point have an untested
   *decision*?

**Reviewer output per unit:** one line per §1 sub-unit — `{STRONG|WEAK|THEATER|FAKE-ANCHORED|MISSING}
· evidence file:line · MUST-fix? y/n`. Folds into the assessor's CR leg (§4).

---

## 4. How this composes with M6 (do NOT duplicate)

`plans/2026-07-13-code-revamp/M6-PLAN.md` is the controlled-testing harness milestone. **The
fake-anchored/missing gaps in §2 are exactly what M6 is built to close — this review does not rebuild
them, it points at M6 and verifies M6's own coverage is real.** Explicit mapping:

| §2 gap | M6 workstream that closes it | Review's job |
|---|---|---|
| tmux paste *landing* untestable by unit | **WS3 twin↔real parity** (Claude-A/B/C/D) + WS3 audit's "2 irreducibly-real-only stages: splash-dismiss + physical-Enter" | Verify the parity harness's equivalence library (WS3-F1) actually fails on a mutated stream; do NOT write yet-another fake tmux test. |
| ssh transport / localhost skip-to-green | **WS1.3** (`HARMONIK_REQUIRE_REMOTE_E2E=1` → `t.Fatalf`) + **WS2** dockerized remote E2E (host-independent) | Confirm WS1.3 is wired and WS2 replaces the skip-to-green tier; the review flags the skip, M6 fixes it. |
| live core-loop (real agent bead→terminal) | **WS4** revived `core-loop-proof` (forced LT leg, zero-PENDING gate) | Confirm the forced command exists and is loud-PENDING (never false-green) — do not build a parallel E2E. |
| operatornfr/scenario theater still green | **WS5-1** (assessor: reasoned judgment, NOT bead-count) + this doc's deletion targets | The review supplies the deletion list; M6 supplies the assessor that stops trusting the green count. |
| coverage-vs-risk tiering | **WS1.5** risk-tiering rule (Tier-0/1/2 path-glob floor) | This doc's §2 risk table should seed WS1.5's tiering; keep them consistent. |

**Boundary rule for the review:** if a gap is a *controlled-harness* gap (real agent, real transport,
real tmux), it belongs to M6 — the review's deliverable is to *verify M6 closes it and that M6's own
new tests are not themselves fake-anchored*. If a gap is a *pure-logic* gap (runbridge decision,
untested adapter argv path), the review can call for a direct unit test now (§5, targets 1–3).

---

## 5. Concrete coverage-improvement targets, ranked by risk

Split into **ADD-A-TEST-NOW** (pure logic, no harness needed) and **DEFER-TO-M6** (needs the
controlled harness — do not hand-roll).

**ADD NOW (pure-logic, high value, low cost):**
1. **`runbridge.go` — dedicated bridge test.** RU `daemon-workloop`. The shell-event → machine-event →
   `br` close/reopen mapping, reject-reason resolution, drain handling. This is the seam where the
   census caught a fabricated done-status. **Highest value.** Fast unit test with the existing
   `substrate.Twin` + `FakeClock`.
2. **tmux adapter argv/exec tests for `SendKeysEnter`, `SendKeysQuit`, `CapturePane`, `WriteToPane`.**
   RU `tmux-io`. Same fake-binary technique already in `osadapter_test.go` — closes an untested-product
   gap even though it can't prove *landing* (landing is M6/WS3). Cheap, worth it.
3. **`EmitWorkerOfflineEvent` real-body test.** RU `remote`. Replace the field-echo theater in
   `remote_substrate_b11_test.go` with a test that drives the real emit body (nil-guard, marshal, emit)
   via a recording bus. Cheap; removes a theater test masquerading as coverage.
4. **`runshell.go::fireOnCancel` (0%)** + the daemon 0-25% funcs that touch terminal/merge behavior
   (`maybeEmitEpicCompleted` 25%, `runMergeFmtCheck` 21%). RU `daemon-workloop`. Medium value.
5. **`agentlaunch/SpawnKeeperWindow` body** (untested). RU `keeper`-adjacent. Low-medium.
6. **queue two-writer path + `HandlerAdapter`** — confirm/raise from 68.8%. RU `queue`.

**DEFER TO M6 (do NOT hand-roll a fake for these):**
- tmux paste *landing* on a not-ready TUI → **WS3-Claude parity** + real-agent WS4.
- real ssh transport failure modes → **WS2 docker E2E** + **WS1.3** required-mode.
- live codex **input-direction** anchor → **WS3-codex** live re-capture (`CODEX_LIVE`).
- live core-loop bead→terminal on real agents → **WS4**.

**DELETE, don't cover (see §6):**
- operatornfr theater (~24-27 files) → M1-2/M1-3 (operator-gated).
- specaudit 3 untagged stragglers → tag them or fold into the specaudit lint.
- internal/scenario 37 structural-corpus files → collapse to the harness + the 5 real ones.

---

## 6. Where "add tests" is the WRONG move

The census marks four units **Rebuild** or **Delete**. Adding coverage to code that is about to be
deleted or rebuilt is waste — worse, it raises the sunk-cost bar against the rebuild.

- **`test-bloat` (Delete):** operatornfr theater, specaudit, the 37 scenario corpus files. **Do NOT
  add tests; delete/neutralize.** operatornfr M1-2/M1-3 are the sanctioned deletions (operator-gated).
  The green they produce is the problem, not a thing to extend.
- **`tmux-io` input channel (Rebuild):** do not invest in new *fake-anchored* paste tests trying to
  simulate landing — the ack-free channel "can never be made reliable, only re-caulked" (census).
  Cover the adapter argv gap (§5 target 2, cheap) but route *landing* confidence to M6/WS3, and let the
  structured-input rebuild retire pasteinject rather than grow its test mass. (T11 deletion of
  pasteinject is already parked pending zero-callers — `COORD.md` c035.)
- **`remote` (Rebuild → M4 in flight):** do not deepen the mocked-git / synthesized-255 unit tests as
  the confidence story. M4 T1 is empirical (real Claude on gb-mbp) and is the alignment gate; the real
  coverage is WS2 docker E2E + the T1 proof, not more box-A-mutex unit mocks.
- **`core-eventreg` dead surface (Simplify, cut deeper):** the production-dead decode/validate +
  `pertypecompat` table should be **deleted, and its coverage with it** — do not "improve" coverage of
  code with zero live consumers (census §"cut deeper", M1-4 territory).

---

## 7. Review-unit index (for the decomposition)

Every §1 item, keyed to the census review unit so it plugs into the mega-review decomposition:

- **`daemon-workloop`** → runexec ✅, mergeq ✅, orchestrator ✅, runexectest ✅ · beadRunOne WEAK ·
  **runbridge MISSING (top target)** · fireOnCancel/peripheral funcs MISSING.
- **`daemon-godpackage`** → (structure/extraction, not a coverage unit per se; inherits workloop tests).
- **`daemon-harness`** → codexdriver ✅ / codexreactor ✅ / handler ✅ · live input anchor ⚠️ (M6).
- **`tmux-io`** → watchdog orchestration STRONG-but-FAKE-ANCHORED · real primitive/adapter MISSING ·
  landing → M6/WS3.
- **`remote`** → argv/ack-invariant STRONG · transport MISSING · localhost E2E skip-to-green → M6/WS1.3+WS2.
- **`keeper`** → 83.6%, healthy (not deep-audited) · SpawnKeeperWindow body MISSING.
- **`core-eventreg`** → high % partly on dead code; **delete, don't cover**.
- **`lifecycle-reconcile`** → 77.8%, not deep-audited (candidate for a dedicated reviewer).
- **`queue`** → 68.8%, real but thinner than reputation; confirm two-writer/HandlerAdapter.
- **`test-bloat`** → operatornfr ~65-70% THEATER (live), specaudit neutralized (3 stragglers),
  scenario 37/42 corpus-only · **delete, don't cover**.
- **hook/policy** (cross-cuts harness/godpackage) → ALL STRONG, leave alone.

---

## 8. Open questions for adversarial review

1. Is the review-unit spine (census areas) the same decomposition the rest of the mega-review will
   use? If the decomposition names differ, remap §7.
2. `beadRunOne` at 63.7% — is the untested ~36% dead/peripheral, or is a real path uncovered? A
   reviewer should diff the covered vs uncovered branches before accepting the floor as adequate.
3. Should the 3 untagged specaudit stragglers stay in the default suite (they check real product
   constants) or move behind the tag for a truly theater-free green? (Judgment call — flag, don't
   assume.)
4. Is there value in a `HARMONIK_REQUIRE_REMOTE_E2E=1` CI lane on a sshd-capable runner *before* WS2
   docker lands, to kill the skip-to-green immediately? (M6 WS1.3 vs WS2 sequencing.)
5. queue 68.8% — is the gap the retired/dead HandlerAdapter grab-bag (delete) or live queue logic
   (cover)? Determines whether it's a §5 or §6 item.
