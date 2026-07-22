# ADV-C — Adversarial review of the in-process DOT testing-harness design (`brainstorm/C-testing-harness.md`)

Reviewer stance: attack. Every load-bearing code claim was checked against
`/Users/gb/github/harmonik` (Grep/Read on `internal/daemon`). Verdict at bottom.

---

## Code-claim verification (the "is it even true" pass)

**No FALSE claims found. Every load-bearing claim verifies:**

| Claim | Status | Evidence |
|---|---|---|
| `ExportedDriveDotWorkflow` / `…Full` exist, drive real `driveDotWorkflow` against real scratch git | ✅ TRUE | `export_test.go:849`; used by ~30 `dot_*_test.go` (`dot_reviewer_no_verdict`, `dot_no_progress_guard`, `dot_resume_feedback_hkwixms`, `dot_commit_gate_cap_salvage`, …). |
| Today's tests swap the executable for `/bin/sh` (`HandlerBinary:"/bin/sh"`), so real per-handler argv is never asserted | ✅ TRUE | `HandlerBinary:"/bin/sh"` pervasive across `dot_*_test.go`; the ① back-edge test **`dot_resume_feedback_hkwixms_test.go` is itself `/bin/sh`** (lines 205/303/465) — confirming the exact gap the doc names. |
| `LaunchSpecBuilder` is a real DI seam in `WorkLoopDepsParams`, consumed in the cascade | ✅ TRUE | Field at `export_test.go:165`; consumed at `dot_cascade.go:1402` `specBuilder := deps.launchSpecBuilder` (then optionally overridden by `pinnedHarnessLaunchSpecBuilder`, `:1416`). |
| Capture-and-abort stubs read one field then error out | ✅ TRUE | `ExportedCaptureExtraContextBuilder/NodePrompt/Runner` (`export_test.go:887/901/917`) tee `rc.<field>` then `return …, error("capture-only stub: stopping dispatch")`. `ExportedMinimalLaunchSpecBuilder` returns `Binary:"/bin/true"`. |
| Twin binaries exist & parity-gated; `TwinBinaryPath()`; `pi_twin_parser_drive_test.go` | ✅ TRUE | `cmd/harmonik-twin-{claude,codex,pi,generic,session}`; `scenariotest.TwinBinaryPath()`; `pi_twin_parser_drive_test.go` present. |
| `em015FixtureNilStdoutSubstrate` runs real argv as subprocess (nil stdout) | ✅ TRUE | `scenario_reviewloop_em015de_hkintln_test.go:89` — `SpawnWindow` runs argv as a real subprocess. |

So the design is built on a **correctly-mapped** substrate. Credit where due: the seam
inventory (A `launchSpecBuilder` / B `Substrate` / C `HandlerBinary`) and the "replace C
with disciplined A+B" reframing are accurate and buildable on existing infra.

---

## FATAL flaws

**None that invalidate the approach.** No false code claim; the seam is real; git-real is
correct. The problems below are serious enough to force a rescope and a rename, not to
reject the A+B seam.

---

## SERIOUS concerns

### S1 — "Prove ① end-to-end per handler" is FALSE for claude, and overclaimed for all (axes 1 & 2)
The seam-A recorder taps the built `LaunchSpec`. For **pi/codex** the resume feedback IS a
positional argv (`implementerResumeSeedPrompt`, `agentseedprompt.go:44`) — so the recorder
genuinely captures the ① fix. **For claude it is not in the argv at all.** Claude's resume
feedback is delivered by **paste-inject** (`pasteInjectImplementerResume` → tmux
`load-buffer`/`paste-buffer`/`send-keys`, `pasteinject.go`), and the reference lives in the
on-disk `agent-task.md` (phase=implementer-resume), never in `LaunchSpec.Args`. Two consequences:
- The proposed **subprocess / nil-stdout substrate is NOT tmux-backed**, so
  `pasteInjectImplementerResume` (which requires the `pasteInjecter`/`enterSender`
  tmux-substrate interfaces) is a **no-op** under the twin. The single mechanism ① fixed
  *for claude* is therefore **not exercised at all** by this harness.
- So `TestDotTwin_ResumeSeedReferencesFeedback_claude` can only assert "daemon **wrote**
  `agent-task.md` referencing the feedback file" — not that claude was kicked to read it.
  The claude row proves strictly **less** than pi/codex and skips exactly the fragile,
  harness-divergent delivery path (the c073/① lineage). The doc half-admits this (assertion
  #4 asserts `agent-task.md` for claude) but the TL;DR and test names sell "end-to-end per
  handler." **Rename** to "daemon-side seed/brief construction proven (pi/codex delivery
  faithful; claude delivery bypassed); agent consumption still un-exercised."

### S2 — In no handler does it prove the agent READS/ACTS on feedback — the twin fakes that by fiat (axis 2)
The twin does its scripted commit **regardless of brief content**. So even for pi/codex, ①
is proven as "daemon builds+delivers a seed that references the file," never "agent reads the
file and acts." That's an acceptable scope *if named honestly*. As written ("proves ① end-to-end")
it conflates construction with consumption.

### S3 — The headline round-trip proves PLUMBING, not the PRODUCT (axis 3)
The motivating defect is P1: the leak makes a **real** implementer one-shot, so the back-edge
never fires. A twin that **ignores the body by fiat** forces REQUEST_CHANGES→resume→APPROVE
deterministically — but it **passes identically whether P1 is fixed or not**. Test #1 is a
plumbing **regression** test; it says nothing about the defect that launched the spike. The
actual product question ("does the fixed input-model cause a round-trip with a *real* model?")
remains only answerable in live e2e. "gap6 finally forced" = the *plumbing* is forced, not the
*behavior*. Keep the test, drop the "headline / product proven" framing; the leak oracle is
entirely offloaded to test #4 (correctly), which means test #1's product value is ~nil.

### S4 — Scope: three cost tiers bundled under one expensive harness (axis 5)
The single-dispatch argv assertions need **neither a subprocess, nor real git, nor
`//go:build scenario`**:
- **#3 binary-matches-declared (c074 guard)**, **#7 reviewer-harness-override**, **#4
  no-rubric-leak**, and the implementer/reviewer brief-routing half of **#1** are all
  provable from the **built `LaunchSpec` + on-disk briefs on the FIRST dispatch**.
- The existing capture-and-abort stubs already tee a field and short-circuit *before spawn*.
  Upgrading one to "call the real builder → record the **whole** spec → abort" delivers
  #3/#4/#7 at **unit speed** — this is a tiny extension of `ExportedCaptureExtraContextBuilder`.
- **#6 cap-hit** is already covered by `TestScenario_EM015d_CapHit` (shell) — the twin adds nothing.
- **#2's seed CONTENT** is already covered by the 4 existing `8dbe5a17`
  `implementerResumeSeedPrompt` unit tests; the *only* new thing the cascade adds for #2 is
  proving the **wiring reaches the resume builder at iter≥2** with the right prior state.

Net: the genuine justification for the full 3-binary subprocess+git+scenario harness collapses
to **test #1 (one round-trip) + the #2 wiring check** — ~1–2 scenario tests, not 7. The doc's
own §8 ("don't re-cover the 80%") argues for this narrowing; the §9 build-order list violates it.

### S5 — The "typed input manifest" anti-rot linchpin has two holes (axis 4)
The doc calls this "the single most important decision," yet:
1. **Who emits it?** The doc waffles: "the daemon (**or the harness wrapper**)." If the
   *wrapper* (test code) emits it, the manifest is the test's **model** of the daemon, not the
   daemon's real output — it can agree with a buggy daemon and rot silently. It only has
   anti-rot value if the **daemon** emits it as a production byproduct of the real routing path
   — which is **new production surface** the spike hasn't scoped or costed.
2. **Chicken-and-egg leak oracle.** `brief_source_keys` "must not include the reviewer's key"
   presupposes per-role **source keys already exist**. Today (P1) the *same full body* is
   rendered into both briefs — there are no distinct keys to assert on. The manifest can't be
   the leak oracle until the redesign it's meant to guard already exists. It's a dependency on
   the redesign, not an independent test-layer deliverable.

### S6 — The twin is NOT "handler-faithful" on its INPUT (axes 1 & 4)
The twin reads role + an iteration counter and **ignores the rest of its argv**, emitting
scripted NDJSON. Its parity is on **output** (NDJSON, gated by `pi_twin_parser_drive_test`),
not on **validating the argv it received**. So the twin itself **cannot catch argv drift**; that
falls entirely on the hand-maintained per-handler `argvSig` assertions (§5) — exactly the rot
surface the doc claims to have solved. "Faithfulness is structural" is true of the argv being
production-*built* but not production-*validated*: a wrong-but-runnable argv yields **green**
unless `argvSig` happens to cover that axis. Rot risk is **relocated to `argvSig`, not eliminated.**

---

## What it gets RIGHT

- **Diagnosis is correct and verified:** the `/bin/sh` swap means the per-handler argv/seed is
  never the object under test; that is precisely why P3/P7/c074 stay open. Confirmed in code
  (even the ① test `dot_resume_feedback_hkwixms` is shell-only).
- **Seam inventory is accurate** and the A+B-vs-C reframing is sound and buildable on existing DI.
- **Keep git real** is well-argued and correct — cascade correctness *is* git semantics
  (HEAD-advance, diff-hash no-progress, commit-gate `test -f`); a mock would be the mock under test.
- **pi/codex ① coverage is genuinely strong:** the resume seed is a real positional argv, so the
  seam-A recorder faithfully closes the unit-only gap **for those two harnesses**. This is the
  most defensible part of the proposal.
- **The c074/P7 binary-matches-declared guard is a real, high-value win** (though cheap — see S4).
- **Honest §8 risk list** (twin drift, Args-rewrite hazard, skip-is-green prebuilt dependency,
  positional-seed compat) — the failure modes are named even where the body oversells.

---

## VERDICT

**ACCEPT the diagnosis and the A+B seam; REJECT the scope and the "end-to-end per handler" framing.**

Concretely, split the work:
1. **Cheap capture-recorder unit layer** (no subprocess, no git, no scenario tag): upgrade one
   capture stub to record the full built `LaunchSpec`, and assert #3 (c074 guard), #4
   (no-rubric-leak), #7 (reviewer-harness override), and first-dispatch brief-routing. This is
   where ~80% of the *new* value lands at unit speed.
2. **ONE scenario-tagged twin+git round-trip** for the genuine multi-iteration back-edge (test
   #1) plus the #2 wiring check — reserve the expensive harness for what only it can do.
3. **Rename ① claims** to "daemon-side seed/brief **construction** proven": faithful delivery for
   pi/codex, **paste-inject bypassed for claude**, agent **consumption faked** in all rows. Do
   not claim the harness substitutes for live e2e on the actual product behavior (S3).
4. **Decide the manifest's emitter up front** (daemon-emitted or it's worthless as anti-rot), and
   note it can't be the leak oracle until per-role source keys exist (S5).

The proposal is a good *foundation* oversold as a *closer*. Trimmed to the two things only it can
do — the multi-iteration round-trip and per-handler argv assertions — it is worth building.
