# Phase 1 Readiness — Gap Analysis

**Date:** 2026-05-05
**Purpose:** identify what's missing for Harmonik to enter Phase 1
**Operational criterion:** a basic version we can use to further extend the system

> Reframe applied throughout: Phase 1 entry = **"all tasks in place AND validated AND able to start building itself."** Stricter than the existing definition ("bootstrap subset identified, dependency-closed"). The bootstrap subset is necessary but not sufficient — readiness also requires the infrastructure substrate the bead corpus assumes, the meta-layer the project runs on (review pipeline, branching, skill registry), and a defensible validation criterion that confirms the loaded set actually executes.

## Executive summary

- **Bead corpus is ~75% labelled.** 823 total beads; 291 `scope:bootstrap`; only 5 `post-mvh`; **527 beads carry neither tag** (untagged middle ground). Either the bootstrap-subset analysis missed those or they are post-MVH but not labelled — the implicit gap is large enough to warrant a labelling pass before declaring Phase 1 ready.
- **SH (scenario-harness) spec landed at v0.2.0 reviewed and the SH pilot v0.1.0 is drafted (54 beads), but neither the SH pilot review pass nor the SH bead load has happened.** Until SH beads land in `.beads/`, the ~291 bootstrap subset is provably **insufficient for the §1 working-definition acceptance test** (which requires the harness to drive the round-trip per `bootstrap-subset.md` §7).
- **The infrastructure substrate the corpus assumes does not exist as code or as beads.** Twin binaries, `make check-fast/check/check-full` Makefile + `lefthook.yml`, `.golangci.yml` with the depguard component matrix, the `agent-reviewer` skill, the `beads-cli` skill, and the `internal/core` Go scaffold are all named normatively in `docs/foundation/project-level/*.md` and `subsystem-organization.md` but are neither code nor labelled bootstrap beads.
- **No "readiness workflow" exists in the corpus.** Per project memory ("loaded beads must not auto-start; parked state + readiness workflow"), there should be a workflow that promotes parked beads to dispatchable. There is no bead, no spec, and no pilot for it.
- **No first self-build cycle slice has been authored.** "A basic version that can extend the system" requires a concrete first input — pick a spec slice (e.g., a single SH-021 assertion-vocabulary increment, or a CP single-gate landing) — and walk it end-to-end. That walk is the unblock for declaring Phase 1 ready.

**Recommended Phase 1 entry gate:** SH pilot reviewed + loaded; corpus labelling reconciled (every bead is `scope:bootstrap` OR `post-mvh`); a thin "readiness workflow" beaded; the agent-reviewer skill + Makefile scaffold landed; one first-self-build slice picked + dry-walked through the bootstrap subset to confirm dependency-closure under operational use, not just static `br dep cycles`.

## Current state snapshot

- **Specs:** 11 reviewed (10 foundation + SH). SH at v0.2.0 reviewed; landed 2026-05-05. (`specs/scenario-harness.md`.)
- **Beads in corpus:** 823 (live `br --json list --limit 0` count). Pre-SH count was ~639 + epic envelopes; SH's 54 beads not yet loaded.
- **Beads tagged `scope:bootstrap`:** **291** (verified live; matches `bootstrap-subset.md` §2). When SH loads + is labelled, target rises ~291 + ~50 SH-bootstrap = ~340.
- **Beads tagged `post-mvh`:** **5.** (`hk-sx9r.60` distributed-tracing, `.59` metrics-exposition, `.58` multi-tenancy, `.8` binary-signing; `hk-hqwn.59.56` `bead_terminal_transition_recovered`.)
- **Beads with neither tag:** **527** (823 − 291 − 5 = 527). This is the implicit-deferred middle ground. Cluster reports under `docs/decompose-to-tasks/bootstrap-subset/{pl,wm,em,hc,ev,bi}-bootstrap.md` enumerate per-cluster excludes (~517 beads in §5 narrative); the gap is the difference between "narrative-deferred" and "labelled."
- **Discipline:** v0.9 (per `STATUS.md` and `HANDOFF.md`); v0.10 patch batch of 13 findings still queued.
- **Phase 0 outstanding:** `hk-ahvq.39` forward-zero (S07-caveated), `hk-ahvq.42` milestone close (gated on S07).
- **Live corpus state:** `br dep cycles` clean. `epic status` shows 0/N children closed for every spec epic (no implementation has begun). All beads remain in `draft` status.

## A. Bead-corpus completeness — gaps

### A1. SH beads not loaded — **BLOCKER**

- **Source:** `HANDOFF.md` (S07 blocking question resolved by authoring SH spec + pilot); `bootstrap-subset.md` §7 ("the 291 IDs labelled here form the **non-S07 bootstrap subset** — necessary, but not sufficient for the §1 working-definition acceptance test"); live corpus query `br list -l spec:scenario-harness --limit 0` returns **0 beads.**
- **What's missing:** The SH pilot at `docs/decompose-to-tasks/sh-pilot.md` v0.1.0 enumerates 54 beads + 133 edges (including 38 cross-spec edges, 0 forward-deferred — first pilot in the corpus with zero forward-deferred). Pilot has not been through the 3-reviewer protocol per `pilot-review-protocol.md`, has not been loaded via `scripts/load-pilot.py`, and is not labelled `scope:bootstrap`.
- **Severity:** BLOCKER. The §1 working-definition includes scenario-driven validation (twin handler, checkpoint commit, merge, scenario assertion). Without SH beads, the acceptance test for "system can build itself" has no implementation track.

### A2. 527 beads have neither `scope:bootstrap` nor `post-mvh` — **MAJOR**

- **Source:** Live `br --json list --limit 0` (823 total; 291 bootstrap; 5 post-mvh; 527 untagged for scope).
- **What's missing:** Discipline v0.7 §3.1 and the bootstrap-subset analysis carved out the bootstrap subset in narrative form (`bootstrap-subset.md` §5). No corresponding `post-mvh` labelling pass has occurred. Effects:
  - Cannot answer "is bead X in MVH?" via a single `br list` query.
  - Cannot run a closed-set validation (e.g., `br list -l scope:bootstrap` ∪ `-l post-mvh` should equal corpus minus epic-parents, but currently equals 296 / 823).
  - When SH loads, ~50 of its beads will be bootstrap and ~4 will be post-MVH, but there is no convention being followed for the rest.
- **Severity:** MAJOR. Not a blocker for first-self-build (the closure-check confirms dependency-closure for the 291), but is a **defect in the validation surface** — see §E.
- **Recommendation:** label-application pass. Either (a) the 527 are all implicit `post-mvh`, label them as such (one `br update --add-label` chunk); or (b) re-run the per-cluster carveouts with explicit per-bead labelling. Option (a) is cheap and correct given the cluster-report enumerations.

### A3. No readiness workflow in the corpus — **BLOCKER**

- **Source:** Project memory `project_harmonik_task_ingestion.md` ("loaded beads must not auto-start (parked state + readiness workflow)"); live corpus search for `parked` returns 0 beads, `readiness` returns 2 (one is `hk-8mup.18` Ready-protocol surface — about daemon `ready` state, not bead readiness; the other is `hk-ahvq.42` Phase 0 milestone).
- **What's missing:** A workflow definition (DOT or equivalent) and a small bead family (3-6 beads) that defines: (a) bead loaded → enters `parked` state; (b) readiness check workflow runs (validates upstream deps closed, validates twin available, validates skill-set resolvable per HC §4.11); (c) on pass, bead transitions to `ready`/dispatchable; (d) on fail, bead emits `readiness_failed` event and stays parked.
- **Severity:** BLOCKER. Without the readiness workflow, the operational criterion "system can start building itself" devolves to "operator manually flips beads to ready" — that's not self-building, it's hand-driving.
- **Recommendation:** author a thin "readiness" pilot (3-6 beads) under a new meta-epic or under BI/EM. Not a full normative spec at MVH; one workflow + 3 beads (ParkedState, ReadinessCheckNode, ReadinessGate) is enough.

### A4. Meta-beads for review pipeline + skill registry are absent — **MAJOR**

- **Source:** `docs/foundation/project-level/build-practices.md` ("agent-reviewer-every-commit"), `agent-configuration.md` §Skills (8 named load-bearing skills); live corpus search for `reviewer` in bootstrap returns 0; `skill` in bootstrap returns 6 beads (all are HC §4.11 surface bits or BI's beads-cli wire — none is a meta-bead for "the agent-reviewer skill exists at `.claude/skills/agent-reviewer/`").
- **What's missing:**
  - Meta-bead for `agent-reviewer` skill (the JSON-verdict reviewer that gates every non-trivial commit).
  - Meta-bead for `beads-cli` skill (named in HC §4.11 + BI as a foundation skill but not present as a corpus task).
  - Meta-bead for the JSON-verdict schema versioning.
  - Meta-bead for `agent-config-reviewer` (Tier 2 review subagent).
- **Severity:** MAJOR (not BLOCKER). The build-practices spec is normative, but the skills exist outside the corpus model — they live in `.claude/skills/`. They should be tracked as discrete tasks because they're load-bearing per `quality-checks.md §Agent-enforceability` ("agent-reviewer skill is load-bearing and must not rot").
- **Recommendation:** meta-epic for "operational skills + review pipeline." 5-8 beads. Either a new top-level epic or under `hk-ahvq` (Phase 0) → carries forward into Phase 1.

### A5. Policy engine bypass-ability is implicit, not specified — **MINOR**

- **Source:** `bootstrap-subset.md` §1 ("Out of scope for v0… policy-engine guards"); CP fully deferred (0 of 85 beads in bootstrap); `core-scope.md` §10 framing.
- **What's missing:** A clear statement that the bootstrap workflow has zero policy/guard/gate touchpoints AND that the orchestrator code path must be coded such that "no policy engine" is a first-class operating mode (not a `policyEngine == nil` branch — that violates SH-018's "no test-mode branches in production"). The discipline is implied but not corpus-tracked.
- **Severity:** MINOR. The orchestrator implementer will hit this during the first cluster-A build.
- **Recommendation:** one sentence in `bootstrap.md` (or a 2-line bead under EM) clarifying "MVH composition root wires a no-op PolicyEngine as the production interface; no branch."

## B. Infrastructure outside the bead corpus — gaps

### B1. Twin binaries — **BLOCKER**

- **Source:** `bootstrap.md` §5 step 4 ("Claude twin binary"); HC bootstrap subset includes `hk-8i31.77` (canonical twin handler binary) — but that's the **handler-side reception** of the twin, not the twin binary itself. `subsystem-organization.md` §Go module layout names `cmd/harmonik-twin-claude/` but the directory does not exist.
- **What's missing:** The twin Go program(s) that emit the parity surface declared at `[handler-contract.md §4.8]` (HC-035 through HC-040). A scenario test cannot be authored before the binary it asserts against exists.
- **Severity:** BLOCKER for the §1 acceptance test. The first slice of self-build cycles is conventionally "harmonik builds harmonik" — but the FIRST cycle must be hand-built, including the twin.
- **Recommendation:** carve the twin binary out as Phase 1's **first hand-build task**, before the orchestrator code itself. Author a 5-bead meta-epic ("twin-binary scaffolding" → 1 main loop, 1 wire-protocol parity, 1 process-tree contract, 1 hash-verification check, 1 conformance test against HC-035..HC-040). Not the same thing as HC-077-style abstract beads — these are concrete code-existence beads.

### B2. Build/test scaffolding (`Makefile`, `.golangci.yml`, `lefthook.yml`) — **BLOCKER**

- **Source:** `quality-checks.md` §Three-tier identical gauntlet; `build-practices.md` §Agent review on every commit. Both are normative.
- **What's missing:** None of these files exist in the repo (verified: `ls /Users/gb/github/harmonik/Makefile` etc. return absent). The depguard component matrix in `subsystem-organization.md` is a normative reference but has no `.golangci.yml` to reference.
- **Severity:** BLOCKER. The agent-reviewer-every-commit decision is a CONSTITUTION.md-level commitment; it requires `make check-full` to run before commit. Without the Makefile, every "agent declared-done" claim is unverifiable. This must land before the first hand-built code.
- **Recommendation:** scaffold-landing pass before Phase 1 entry. ~6 beads: Makefile (3 targets), `lefthook.yml`, `.golangci.yml` (depguard component matrix), `tools/go-linters/forbid-import.go` stub, `scripts/coverage-gate.sh`, `.github/workflows/ci.yml`. None depends on the bead corpus; all are infrastructure.

### B3. Runtime config schema (`.harmonik/`) — **MINOR**

- **Source:** PL §4.1 fields (pidfile, socket, JSONL), WM §4.1 (worktree-prefix), various spec sections name `.harmonik/` files.
- **What's missing:** No consolidated schema for the `.harmonik/` directory layout. The `bootstrap.md` §6 risk row "regression baseline shape?" alludes to this.
- **Severity:** MINOR. Each spec's normative section is the source of truth; an aggregator doc is convenient but not required.
- **Recommendation:** defer. Will fall out naturally when the first daemon-startup beads (cluster A — 37 beads) implement the file scaffolding.

### B4. CI: the agent-reviewer pipeline itself — **MAJOR**

- **Source:** `build-practices.md` §Commit conventions ("`Reviewed-By: agent-reviewer`" + JSON-structured verdict trailer); `agent-configuration.md` §Skills (the skill is `.claude/skills/agent-reviewer/`).
- **What's missing:**
  - The skill itself (no `.claude/skills/` directory exists).
  - The pre-commit hook that invokes the reviewer.
  - The CI job that runs the same gauntlet.
  - The structured-JSON verdict schema (named in `build-practices.md` but not pinned).
- **Severity:** MAJOR. CI gating without the reviewer is workable for the first few commits; long-term, "the reviewer skill must not rot" (`quality-checks.md`) is load-bearing.
- **Recommendation:** include in the same scaffolding pass as B2.

### B5. Coverage tooling — **MINOR**

- **Source:** `quality-checks.md` §Coverage thresholds (deferred); STATUS.md "Aggressive coverage targets (95% core / 90% floor / <0.3% regression gate)" but quality-checks.md says "wait until testing methodology settles."
- **What's missing:** the targets are decided but not implemented. `scripts/coverage-gate.sh` is named but absent.
- **Severity:** MINOR. Coverage is a Tier 2 check; doesn't block the first self-build cycle.
- **Recommendation:** defer to first-self-build cycle scope, not Phase 1 entry.

## C. Process / meta-layer — gaps

### C1. Branching model is documented; no scripts/hooks/CI gates implement it — **MAJOR**

- **Source:** Memory `project_harmonik_branching_model.md` ("3-level: node commits → task branch → integration branch → main"); `build-practices.md` §Branch model ("direct-to-main"); WM §5.8 (the runtime 3-level model — DIFFERENT from project-level direct-to-main).
- **What's missing:** No git hooks enforce the trailer set on workflow-run commits (`Harmonik-Run-ID` etc.); no CI distinguishes project-level commits from workflow-run commits; no `harmonik/integration` branch exists. These are runtime concerns, not project-build concerns — which is fine for Phase 1 entry.
- **Severity:** MAJOR for the first self-build cycle (the workflow run that implements something must produce the right trailers); MINOR for Phase 1 entry per se.
- **Recommendation:** confirm the branching enforcement falls within the bootstrap subset's WM/EM cluster (45 + 65 beads). It does — `hk-8mwo.19` (squash-merge contract), `hk-b3f.86`/`.87` etc. are all bootstrap. So the gap is "does the operational scaffolding for the project-level commits exist" → see B2.

### C2. Commit cadence + reviewer prompt — **MAJOR**

- See A4 + B4. Same gap, three angles. The skill, the JSON schema, and the example-prompt-for-the-reviewer are all named normatively but absent.

### C3. Kerf is paused; status of "Phase 1 needs kerf back?" undecided — **MINOR**

- **Source:** `STATUS.md` ("No kerf use in this session. User paused kerf: 'disregard kerf for now; come back when something's working.'"); `CLAUDE.md` ("Phase 0 (plan refinement → spec drafting) is active. Code begins after the bootstrap subset of tasks is identified.")
- **What's missing:** No statement on whether implementation work in Phase 1 needs kerf for spec-drafting beyond what's already in `specs/`. Cluster-report cluster reports indicate the bootstrap subset is implementation-ready against the existing specs; therefore kerf MAY remain paused through Phase 1. Worth surfacing for explicit user decision.
- **Severity:** MINOR. Doesn't block Phase 1 entry; one-line clarification.
- **Recommendation:** explicit user statement either way. Default: kerf stays paused; reactivate when (i) an implementation cycle surfaces a real spec gap, or (ii) the post-MVH specs (CP gates, S09 improvement) start being authored.

### C4. CASS / Memory layer pointed at a session-log dir — **MINOR**

- **Source:** `bootstrap.md` step 9 ("Memory Layer (S08): CASS pointed at the session-log dir"); project memory: "MVH = just CASS." `core-scope.md` §"Section 2" §"durability rules" — JSONL events.
- **What's missing:** No bead in bootstrap subset for "S08 directory wiring." The bootstrap-subset analysis explicitly excludes S08 (per `bootstrap-subset.md` §1: "Out of scope for v0… CASS/memory"). But the operational criterion says "ready for Phase 2 with reasonable observability" requires S08 (per `bootstrap.md` step 9: "required soon after").
- **Severity:** MINOR for **Phase 1 entry**; first self-build cycle can run without CASS. Becomes MAJOR before second self-build cycle.
- **Recommendation:** stays out of Phase 1 entry. Add to Phase 1 exit criterion (i.e., "before declaring Phase 1 done, S08 wiring lands") in a separate doc.

## D. First self-build cycle — analysis

### D1. Candidate first slice

The cleanest first slice is a **trivial non-agentic single-node workflow** that exercises the full surface end-to-end:

```
Input:  one bead (e.g., a hand-authored "hello" bead) marked ready
Workflow: a 1-node DOT with kind:non-agentic that just emits an event + commits
Expected: one git checkpoint commit + one terminal event in JSONL + bead closed
```

This avoids the agentic-handler complexity (Claude twin) for the first walk-through but is still a full daemon round-trip. **Per `bootstrap-subset.md` §1 working-definition item 2.** The §1 item 3 (twin handler subprocess) is a follow-up cycle, not the very first.

### D2. What needs to be working

For the trivial slice to flow:

1. **Daemon up:** PL cluster A 37 beads' implementations.
2. **Bead in queue:** BI cluster's 36 beads — specifically `hk-872.5` (read), `.6` (write), `.7` (status enum), and the Cat 0 `br --version` check (`hk-872.26`).
3. **Workspace lease:** WM cluster B-WM 45 beads — at minimum `hk-8mwo.1` (worktree primitive), `.10` (lease-by-run), `.19` (squash-merge).
4. **Workflow execution:** EM cluster B-EM+F 65 beads — minimum the EM-001 envelope through EM-046b non-agentic execution path.
5. **Event capture:** EV cluster D 47 beads — bus + JSONL + 5 of the 78 §8 row types (`run_started`, `run_completed`, `node_dispatch_requested`, `state_entered`, `state_exited`).
6. **Checkpoint commit:** WM-019 (squash-merge) + EM trailer set (`Harmonik-Run-ID`, etc.).
7. **No reconciliation needed (Cat 0 path)** — RC cluster's 4 beads handle no-op resume.

### D3. What blocks this first cycle that ISN'T in scope:bootstrap

- **Twin binary (B1).** Not needed for trivial non-agentic slice; needed for second cycle. **Defer to second cycle.**
- **Readiness workflow (A3).** Need to manually mark the input bead `ready` for the first cycle. Acceptable; flag as known constraint.
- **Make scaffolding (B2).** Implementer cannot validate their own work without `make check-full`. **Block.**
- **Agent-reviewer (B4).** Optional for first cycle (one human-reviewed commit is OK) but normative for cycle N+1. **Block-soft.**
- **Untagged 527 beads (A2).** No operational impact for first cycle if the 291 are in fact dependency-closed (per closure-check). The risk is a missing dep that's in the 527 — closure-check ran but its 6 PULL_INs were AR / RC / EV declarative; the cluster reports' bullet enumerations may have introduced more silent assumptions. **Soft-block; mitigated by the §E validation step.**

### D4. Is there a first scenario authored?

- **Source:** `specs/scenario-harness.md` §10.1 ("smoke/twin-launch-and-ready.yaml", "smoke/checkpoint-and-merge.yaml", "regression/twin-failure-classification.yaml" — the named conformance set). `sh-pilot.md` §6 ("SH does NOT mint test-infra beads at this draft").
- **Status:** scenario file paths are NAMED in the spec; **no scenario YAML files exist yet** under `scenarios/` (verified: directory absent).
- **Severity:** MAJOR. The §1 acceptance test names a 3-scenario conformance floor. None is authored.
- **Recommendation:** authoring the first scenario is a Phase 1 task, not a Phase 1 entry blocker. **But:** before declaring "Phase 1 ready," the trivial-slice walkthrough (§D1) should be authored either as a scenario file or as a hand-driven dry-run script — call it a "smoke 0" — to confirm the bootstrap subset is operationally closed.

## E. Validation criterion recommendation

The user's reframe puts validation in the entry gate: "tasks in place AND validated AND able to start building itself."

**Recommended validation step:** **A dry-run (paper) walkthrough of the trivial slice (§D1) against the 291 + ~50 SH-bootstrap beads, asserting every step has at least one bead that owns the runtime surface.**

This is cheaper than:

- **Scenario-harness regression** (the harness doesn't exist yet — circular).
- **A live dry-run** (no daemon yet — circular).
- **`br dep cycles`** (already done; only catches one bug class).

The walkthrough is a doc, not code: it tabulates the trivial slice's runtime steps (~25 atomic operations from receive-bead through bead-closed), maps each operation to one or more bootstrap beads, and surfaces operations with no bead owner. Operations with no bead owner are gaps the bootstrap subset analysis missed.

**This is what "validated" means at the boundary of "tasks in place" → "able to start building itself."**

It also addresses A2's softer concern: an operation that traces to an untagged-bead exposes the labelling oversight; an operation that traces to NO bead exposes a corpus gap.

## Recommended Phase 1 entry gate

Concrete checklist. Items marked **ENTRY** must complete before declaring Phase 1; items marked **PARALLEL** can land in early Phase 1 alongside first-cycle work.

### ENTRY (must precede Phase 1)

1. **SH pilot reviewed + loaded.** Run `pilot-review-protocol.md` 3-reviewer pass on `sh-pilot.md`; apply BLOCKER/MAJOR findings; load via `scripts/load-pilot.py`; verify `br dep cycles` still clean across union; label SH-bootstrap beads (~50 of 54) with `scope:bootstrap`. Source: HANDOFF.md, sh-pilot.md.
2. **Corpus labelling reconciled (A2).** Either label the 527 untagged beads as `post-mvh`, or re-evaluate per cluster. After this, `br list -l scope:bootstrap` ∪ `br list -l post-mvh` should equal the corpus minus epic envelopes.
3. **Forward-zero (`hk-ahvq.39`) verification re-run.** With SH loaded, the S07-pending caveat lifts. Confirm zero forward-deferred edges.
4. **Trivial-slice paper walkthrough (§E).** Author `docs/foundation/trivial-slice-walkthrough.md` (or kerf-scoped) mapping the 25-op walk to the bootstrap beads. Address any "no owner" findings either by labelling or by filing new beads.
5. **Build/test scaffolding (B2).** Land `Makefile`, `.golangci.yml` with depguard component matrix, `lefthook.yml`, `.github/workflows/ci.yml`. Phase 0-tail meta-epic, ~6 beads.
6. **Readiness workflow authored (A3).** A 3-6 bead pilot. Workflow definition file (DOT) under `workflows/readiness.dot`. Beads can be `parked` and a readiness check can flip them to `ready`.
7. **Phase 0 milestone close (`hk-ahvq.42`).** All Phase 0 exit conditions met; `STATUS.md` flipped to "Phase 1 active."

### PARALLEL (early Phase 1, non-blocking for entry)

8. **Twin binary scaffolding (B1).** ~5 hand-built tasks. Required before second-cycle `claude-twin` agentic slice; not for first non-agentic cycle.
9. **Agent-reviewer skill + JSON-verdict schema (A4 + B4).** Required before commit cadence locks in. First few hand-commits can be human-reviewed.
10. **First conformance scenario YAML (`smoke/checkpoint-and-merge.yaml`).** Authored once SH §6.1 schema beads are implemented — early Phase 1, not entry.
11. **Beads-CLI skill bead (A4).** Trivial bead; can land any time.
12. **CASS / S08 wiring (C4).** Phase 1 exit criterion, not entry.

## Recommended new tracking beads

Each is a candidate for a new bead under an existing or new epic. Names are mnemonic; Beads will assign IDs. Suggested labels per `discipline.md` §2.9.

### Under a new "Phase-1-entry-gate" meta-epic (parent: `hk-ahvq` or new top-level)

- **`p1-readiness-workflow-definition`** — author `workflows/readiness.dot` (1-2 nodes: validate-deps, transition-to-ready). Labels: `phase:0`, `tag:meta`, `kind:workflow`.
- **`p1-readiness-parked-state`** — implementation bead: bead loaded → `parked` status; readiness workflow flips to `ready`. Labels: `phase:0`, `tag:meta`, `scope:bootstrap`.
- **`p1-readiness-gate-bead`** — gate that runs the readiness workflow on every bead-load. Labels: `phase:0`, `tag:meta`, `scope:bootstrap`.

### Under an "operational-skills" meta-epic (new)

- **`p1-skill-agent-reviewer`** — author `.claude/skills/agent-reviewer/` skill + JSON-verdict schema v1. Labels: `phase:0`, `tag:meta`, `scope:bootstrap`.
- **`p1-skill-beads-cli`** — author `.claude/skills/beads-cli/` skill. Labels: `phase:0`, `tag:meta`, `scope:bootstrap`. (Hooked into HC §4.11; thin file.)
- **`p1-skill-agent-config-reviewer`** — author Tier 2 reviewer skill. Labels: `phase:0`, `tag:meta`. (Not bootstrap; runs at session boundary.)
- **`p1-skill-go-subsystem-add`** — author the package-scaffold skill. Labels: `phase:0`, `tag:meta`. (Not bootstrap; runs at first add.)

### Under a "build-scaffolding" meta-epic (new, all `scope:bootstrap`)

- **`p1-build-makefile`** — author `Makefile` with `check-fast`, `check`, `check-full` targets. Labels: `phase:0`, `tag:meta`, `scope:bootstrap`.
- **`p1-build-golangci-yml`** — author `.golangci.yml` with depguard component matrix per `subsystem-organization.md`. Labels: `phase:0`, `tag:meta`, `scope:bootstrap`.
- **`p1-build-lefthook-yml`** — author `lefthook.yml` wiring pre-commit + pre-push to make targets. Labels: `phase:0`, `tag:meta`, `scope:bootstrap`.
- **`p1-build-ci-workflow`** — author `.github/workflows/ci.yml` running same gauntlet. Labels: `phase:0`, `tag:meta`, `scope:bootstrap`.
- **`p1-build-coverage-gate`** — author `scripts/coverage-gate.sh` per `quality-checks.md`. Labels: `phase:0`, `tag:meta`. (Optional at MVH; can be stub.)
- **`p1-build-forbid-import`** — author `tools/go-linters/forbid-import.go`. Labels: `phase:0`, `tag:meta`. (Optional.)

### Under HC implementation epic (`hk-8i31`, all `scope:bootstrap`)

- **`p1-twin-claude-binary-scaffold`** — `cmd/harmonik-twin-claude/main.go` + parity loop per HC-035..HC-040. Labels: `tag:mechanism`, `scope:bootstrap`, `spec:handler-contract`. (Companion to `hk-8i31.77` which is the abstract bead; this is the concrete code task.)
- **`p1-twin-conformance-scenarios`** — author the first 3 scenario YAML files per SH §10.1. Labels: `tag:mechanism`, `scope:bootstrap`, `spec:scenario-harness`. (Pending SH load.)

### Under EM implementation epic (`hk-b3f`)

- **`p1-policy-engine-noop-mode`** — explicit declarative bead: composition root wires no-op PolicyEngine as production interface for MVH. Labels: `tag:mechanism`, `scope:bootstrap`, `spec:execution-model`. (Resolves §A5.)

### Under "Phase-1-validation" (new sub-epic)

- **`p1-trivial-slice-walkthrough`** — author the paper walkthrough doc per §D1+§E. Labels: `phase:0`, `tag:meta`. (Validation artifact.)
- **`p1-corpus-label-reconciliation`** — apply `post-mvh` label to the 527 currently-untagged beads (or per-cluster as cluster reports specify). Labels: `phase:0`, `tag:meta`. (Resolves §A2.)

**Total recommended new beads: ~17.** All small (1-3 sentence descriptions; no decomposition needed). User/follow-up agent decides which to file; this analysis lists candidates.

## Out of scope for this analysis

These are clearly post-MVH and are NOT considered gaps for Phase 1 entry:

- **Pi handler + Pi twin** (`bootstrap.md` step 10). User-resolved Q2 = OUT in opening pass.
- **CP gates / freedom profiles / policy-engine guards.** Per `bootstrap-subset.md` §1 explicit out-of-scope.
- **S09 Improvement loop.** Phase-2 capability per `bootstrap.md` §2.
- **Revision-loop cap configuration.** Per `bootstrap.md` §3 — open decision; not a Phase-1 entry blocker (conservative default suffices).
- **Operator pause/upgrade controls (`harmonik stop`, `harmonik upgrade`).** Per `bootstrap-subset.md` §1 deferred.
- **Multi-run concurrency.** SH §4.6 declares sequential at MVH.
- **Reconciliation Cat 1-6.** Only Cat 0 + Cat 5 in bootstrap.
- **Adze, agent-mail.** Per `core-scope.md` §"Ground rules": not in foundation.
- **Coverage tooling enforcement (95% targets).** Quality-checks.md §Coverage thresholds explicitly defers.
- **Secrets registry / redaction sophistication.** Per `bootstrap-subset.md` §5 explicit deferred.
- **Improvement loop, conformance suite cadence, full crash-recovery suite.** Per `methodology/TESTING.md`'s "during bootstrap" framing — ladder-up after first cycle.

## Surprises / corrections to existing claims

- **STATUS.md says "639 + 54 SH = 693."** Live count is **823.** Discrepancy is ~130 beads — probably the meta-epic envelopes (`hk-ahvq.*`) and the spec-parent epics. STATUS.md's tally is corpus-children-only; the live count includes all entities. Worth noting: the user-facing snapshot in the mission ("~639 + ~54 SH = ~693") undercounts by ~130 if "all beads" is the metric.
- **HANDOFF.md says SH beads "will join `scope:bootstrap` on load,"** but `br list -l spec:scenario-harness --limit 0` returns 0 — SH has not yet loaded as of 2026-05-05. The pilot is drafted; the pilot review + load have not run.
- **`bootstrap-subset.md` §2 reports 291 verified.** Live count: 291. ✓ Matches.
- **`bootstrap-subset.md` §6 IGNORE log lists 61 forward-deferred references** that closure-check classified as IGNORE per global rationales. These IGNOREs are not the same as the 527 untagged beads — they're the 61 outbound dependencies from bootstrap that point at non-bootstrap targets. Distinct gap surfaces.

## §Z. User clarifications (2026-05-05)

This section captures three user-driven amendments that landed the same day as the v0.1 analysis. The original gap items above are preserved verbatim for historical context; this addendum is authoritative when it conflicts with the body.

### Z.1. CI clarification — local agent-reviewer runs, no GitHub Actions pipeline

**User direction (verbatim, paraphrased):** "We don't need CI — but we do need to make sure we can compile and run tests quickly and easily."

The build/test scaffolding gap from §B2 remains real. What does NOT remain in scope:

- A `.github/workflows/ci.yml` pipeline (named in §B2's recommendation list and in candidate bead `p1-build-ci-workflow`).
- A separate "post-push CI runs the identical gauntlet" surface; the `build-practices.md §Agent-declared-done` "local pass predicts CI pass" framing collapses to "local pass IS the gate."

What stays:

- `Makefile` with `check-fast`, `check`, `check-full` targets (per `quality-checks.md`'s three-tier gauntlet).
- `.golangci.yml` with the depguard component matrix (per `subsystem-organization.md`).
- `lefthook.yml` wiring pre-commit and pre-push to the make targets.
- The `agent-reviewer` skill + JSON-verdict schema (per §B4 / §A4).
- Agent-reviewer-every-commit (the `Reviewed-By: agent-reviewer` trailer + structured verdict per `build-practices.md`).

**Effect on the candidate bead list (§"Recommended new tracking beads"):** the `p1-build-ci-workflow` candidate is voided. The `p1-build-makefile`, `p1-build-golangci-yml`, `p1-build-lefthook-yml`, and `p1-build-coverage-gate` candidates are unchanged.

### Z.2. Parked-state rule withdrawn — loaded beads are agent-dispatchable

**User direction (verbatim):** "If there are instructions you read as 'I can't do anything until the person does something' then those need to be changed or they were written in a different context and need to be ignored. The objective now is to get all of the tasks into a state where we can start a new agent and it can just start churning hard through all the tasks."

This withdraws the "loaded beads must not auto-start; parked state + readiness workflow" rule that previously lived in project memory (now updated). Concretely:

- **§A3 (No readiness workflow in the corpus — BLOCKER) is VOIDED.** No readiness workflow is needed; no `parked` bead state is needed. Loaded beads transition directly to a dispatchable status (`open`, per Beads's native `Status.enum`) so agents can claim them via `br ready` without intermediate operator approval.
- **The candidate beads `p1-readiness-workflow-definition`, `p1-readiness-parked-state`, and `p1-readiness-gate-bead` are voided.** Do not file them.
- **Phase 1 entry gate item 6 ("Readiness workflow authored (A3)") is VOIDED.** The remaining entry-gate items (1, 2, 3, 4, 5, 7) are unchanged.

What this does NOT remove:

- **Operator queue-level controls (`harmonik stop`, `harmonik pause`, `harmonik upgrade`).** These remain per `docs/bootstrap.md §4` and per the operator-nfr spec's pause/stop semantics. They operate at the QUEUE level (between tasks); they are not bead-lifecycle approval. The reframe specifically targets bead-lifecycle approval, not runtime safety.
- **The `gated-by-spec-edit`, `post-mvh`, and `gated-by-corpus-scale` transient tags** (per discipline §2.8 / §2.11 / §2.5). These tags continue to mark beads whose dispatch should be deferred for substantive reasons (unresolved spec edits, post-MVH delivery scope, corpus-scale degeneracy). Their effect is now expressed as agent-side or loader-side filtering of the dispatchable set, not as a workflow gate.

**Source-of-record updates pending elsewhere in the corpus** (catalogued by the parked-state cleanup pass 2026-05-05):

- `docs/decompose-to-tasks/discipline.md` v0.9 carries "readiness workflow" prose at §2.8 / §2.9 / §2.11(c). v0.10 is the active patch wave; the parked rule's removal is a v0.11 candidate (do not silently apply to v0.9; revision-history entry + version bump required).
- `specs/operator-nfr.md` v0.4.0 (`reviewed`) §3 glossary entry for `in_flight(run)` references a "parked lifecycle position" excluded from the predicate. The reference is structurally inert (excluding an empty/deprecated state is harmless), but a v0.4.x patch should rewrite the prose to drop the term — flagged for follow-up.
- Pilot-narrative references (`bi-pilot.md`, `ev-pilot.md`, `bi-smoke-load-findings.md`) and review artifacts (`ar-pilot-r1/decomposition-r1.md`, `operator-nfr-r2/skeptic.md`) are historical — they are NOT edited; they capture the state at their authoring time.

### Z.3. Twin-binary tasks must exist as beads — separate gap, parallel pass

**User direction (paraphrased):** S07 didn't generate twin-binary build tasks. A separate twin-binary-gap pass is filing those beads now.

This is concretely the §B1 gap (Twin binaries — BLOCKER). The §B1 recommendation (a 5-bead meta-epic for "twin-binary scaffolding") is being executed by a parallel beads-filing pass independent of this addendum. The candidate bead `p1-twin-claude-binary-scaffold` (under HC implementation epic) is being expanded to ~5 concrete code-existence beads per §B1's enumeration (main loop, wire-protocol parity, process-tree contract, hash-verification check, conformance test against HC-035..HC-040).

**Effect:** §B1 stays as a gap until those beads land; the addendum acknowledges the parallel pass and sets the expectation that §B1 closes via that pass, not via this analysis.

### Cross-cutting note on §A and §B closure

§A's bead-corpus gaps (A1 SH load, A2 corpus labelling, A3 readiness workflow [now VOID], A4 meta-beads, A5 policy-engine bypass) and §B's infrastructure gaps (B1 twin binaries, B2 build/test scaffolding, B3 runtime config, B4 agent-reviewer pipeline, B5 coverage tooling) are largely being addressed by parallel beads-filing agents working off the §"Recommended new tracking beads" list. §A3 is voided per Z.2; §B's CI surface is reduced per Z.1; the rest stand.

## Revision history

- **v0.1 (2026-05-05).** Initial gap analysis. 5 bootstrap-corpus gaps (1 BLOCKER, 1 BLOCKER, 1 BLOCKER, 1 MAJOR, 1 MINOR), 5 infrastructure gaps (2 BLOCKER, 1 MAJOR, 2 MINOR), 4 process gaps (2 MAJOR, 2 MINOR), one validation criterion recommendation, 17 candidate new beads.
- **v0.2 (2026-05-05).** Added §Z user-clarifications addendum: (Z.1) CI scope reduced to local agent-reviewer runs, no GitHub Actions; (Z.2) parked-state rule withdrawn — §A3 BLOCKER voided, candidate beads `p1-readiness-*` voided, entry-gate item 6 voided; (Z.3) twin-binary gap (§B1) being closed by a parallel beads-filing pass. Body §§A–E preserved verbatim; the addendum is authoritative on conflicts.
