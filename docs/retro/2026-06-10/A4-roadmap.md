# Harmonik Roadmap Analysis — 2026-06-10

## Initiative Status Table

| Initiative | Kerf work | Status | Open beads | Assessment |
|---|---|---|---|---|
| **Captain & Crew** | `captain` | kerf `ready`; 15/15 beads merged | 2 open (hk-4adgn crew-epic, hk-njetn doc) | **Functionally DONE.** Feature complete (57c6fd94). Live fleet deployed. Epic open pending one operator round-trip (keeper cycle on a real crew). |
| **Codex Harness** | `codex-harness` | kerf `ready`; 10/20 beads merged | 1 crew-epic (hk-w4tmz; owns ~10 remaining) | **Mid-flight — actively in progress.** T9+T11 banked reviewed-not-pushed. T12 (registers CodexHarness in DOT cascade) is the keystone; T13–T18 blocked behind it. |
| **Named Queues** | `named-queues` | kerf `tasks`; 10/10 impl beads merged | 2 open (epic hk-tigaf + .11 per-queue budget) | **Effectively DONE.** Core feature + scenario tests merged. hk-tigaf.11 (per-queue budget caps) is P3 deferred. |
| **Session Keeper** | `session-keeper` | kerf `ready`; Phase 1+2 both merged | 1 open (hk-ekap1 epic) | **Functionally DONE.** Phase 1 (warn) + Phase 2 (handoff/clear/resume cycle) both dogfooded and merged. Epic open pending one live `claude --remote-control` round-trip watched by operator. |
| **Flywheel** | `flywheel` | kerf `ready`; 20/21 beads merged | 1 open (hk-m8zqv integration smoke) | **Design + infra DONE; one smoke test pending.** All sub-beads merged. Final bead = 4h unattended run + CL conformance scenarios. |
| **Validation Net** | `validation-net` | kerf `ready`; 4 load-bearing beads merged | 3 open (hk-tijaj scenario, hk-i0hor test-harness, hk-d5twq hook-bridge stub) | **Core DONE; 3 infra/scenario beads remain.** hk-d5twq (hook-bridge socket stub) and hk-i0hor block the full substrate-path E2E. hk-tijaj (merge-conflict-skip scenario) is P1. |
| **Release Pipeline** | `codename:release-pipeline` (no kerf work) | **Mid-flight.** 4/8 beads merged today | 4 open (hk-jdesv CI workflow, hk-o4j13 validate, hk-ya51z rollback, hk-vem4j changelog) | **Blocked at 4/8.** hk-jdesv + hk-o4j13 gated on OAuth `workflow` scope (operator decision). hk-ya51z (rollback) held for sandboxed session. |
| **Productization** | `codename:productization` | Core gates DONE 2026-06-03 | 7 code beads transferred to named-queues lane | **User-onboarding DONE.** Remaining 7 beads (standard-bead.dot, queue-submit workflow_mode stamp, harmonik promote) transferred and queued. One open risk: `br`/`kerf` install docs not yet pinned. |
| **Logmine** | `logmine` | kerf `research`; 22/24 beads closed | 2 open (hk-mhmaw recurring pipeline, hk-4mten CI reds) | **Acute fixes DONE; the RECURRING pipeline is the remaining open work.** hk-mhmaw (the self-improvement loop itself) and hk-4mten (unblock CI Tier-2/3) are the live items. |
| **Phase-3 DOT** | `phase-3-dot` | kerf `ready`; 64/64 beads closed (old) | 0 open in kerf | **Design DONE; implementation in progress** via `codex-harness` T12 (the first DOT-cascade consumer). `standard-bead-dot` kerf work is at spec-draft pass — the per-bead DOT process is still being authored. |
| **Spec Drift** | (no kerf work) | 17 open spec-drift beads | 17 open (label `kind:spec-drift`) | **Ongoing hygiene.** All P2. Concentrations in HC, EV, ON, PL. No kerf work owning them — needs to be wired or batch-dispatched. |
| **CI / Test restoration** | crew epic hk-kjkbw | Active crew lane (liet) | 1 P1 crew epic + hk-4mten CI reds | **Active.** Liet crew owns CI green + quarantined E2E restoration. OAuth `workflow` scope is the current blocker for pushing `.github/workflows/`. |
| **Daemon / Infra stability** | crew epic hk-3js5m | Active crew lane (stilgar) | 1 P1 crew epic + hk-7rgqs reviewer-wedge | **Active.** Stilgar owns spawn-wedge (hk-4l7zs), commit-gate, Linux build, flaky tests. hk-7rgqs (reviewer-wedge at launch_initiated) is P1. |

---

## Current Completion State — Summary

**DONE or nearly DONE (close the loop only):**
- Named Queues — close epic, dispatch hk-tigaf.11 optionally as P3
- Captain & Crew — one operator round-trip closes hk-ekap1
- Session Keeper — same: one operator round-trip closes epic
- Flywheel — one integration smoke run closes hk-m8zqv
- Productization (onboarding) — pin `br`/`kerf` install docs to close the last open risk

**Mid-flight (active work in progress):**
- Codex Harness — ~10 beads remaining; keystone is T12 (CodexHarness registration)
- Release Pipeline — 4/8 beads blocked on OAuth scope; unblocks on operator granting `workflow` scope
- Validation Net — 3 infra/scenario beads; hk-d5twq is a significant seam build
- CI/Test Restoration (liet crew) — CI reds + quarantined E2E
- Daemon/Infra Stability (stilgar crew) — spawn-wedge, commit-gate, Linux build

**Parked / awaiting design:**
- Logmine recurring pipeline (hk-mhmaw) — P1 epic, design not started
- Standard-bead-dot — spec-draft pass in progress; no beads yet; kerf work unwired
- Spec-drift 17 beads — unwired to any kerf work; just sitting

---

## Ranked "Next Phase" Recommendations

### 1. Flush the current winding-down batch (KNOWN — no operator decision needed)
**What:** Drain the 4 mid-flight lanes that are 80%+ done — Codex Harness (T9→T11→T12→T13–T18 chain via duncan crew), Validation Net (hk-tijaj + hk-d5twq + hk-i0hor), Release Pipeline (re-dispatch jdesv+o4j13 once OAuth scope granted), Flywheel smoke (hk-m8zqv).

**Rationale:** All four have a clear remaining task list. Completing them eliminates the open-bead tail that creates ongoing context overhead. Codex Harness T12 is also the gate for `standard-bead-dot` becoming real.

**Size:** Medium — ~15–20 beads, mostly routine dispatch. OAuth scope is the one operator action needed.

**Class:** KNOWN work already in the queue.

---

### 2. Standard-bead-dot — make per-bead DOT workflows the default (KNOWN — bead hk-p0kum + kerf work `standard-bead-dot` at spec-draft)
**What:** Finish authoring the `standard-bead-dot` spec, wire its beads, and dispatch them. This makes the per-bead workflow graph (implementer → reviewer → optional fix-up → merge) a first-class artifact rather than a hard-coded path. Enables the "DOT-defined bead process" vision (Phase 3 per the north-star).

**Rationale:** This is the most structurally significant next step in the north-star trajectory. It decouples review policy from daemon code, makes workflows composable, and is what enables future phases (multi-tier review, AlphaGo-style re-plan nodes). Captain & Crew + named-queues + codex-harness are all already providing the substrate for composable dispatch; standard-bead-dot is the workflow grammar on top.

**Size:** Medium-large. The kerf spec-draft is partially done; beads hk-p0kum/hk-30vlb/hk-n7fw3 give a start. Full cycle likely 8–15 beads.

**Class:** KNOWN work (kerf work exists, beads transferred). Just needs the spec-draft completed and beads wired.

---

### 3. Logmine recurring self-improvement pipeline (NEEDS-OPERATOR-RANKING — hk-mhmaw)
**What:** Implement the recurring logmine loop: daily log ingestion → pattern mining → issue filing → prioritized dispatch. This is what makes harmonik self-improving rather than requiring a human to periodically read logs and file issues.

**Rationale:** The acute-fix beads (logmine F1–F21) are all closed, which means the infrastructure for high-quality issue filing from logs now exists. The next step is automating the cycle. This is directly connected to the north-star Goal G04 (Learning and Improvement Loops). Without it, the only feedback mechanism is manual human review.

**Size:** Medium. hk-mhmaw is a P1 epic with an attached crew (liet). The kerf `logmine` work is still at `research` pass — it would need to advance through the spec-design cycle before beads are dispatchable.

**Class:** NEEDS-OPERATOR-RANKING. The kerf work needs passes 5–7 before beads are ready. Operator needs to decide whether to advance the logmine kerf work now, or park it until the CI/infra lanes are stable.

---

### 4. Flywheel first slice — minimal unattended cognition loop (NEEDS-OPERATOR-RANKING — hk-m8zqv + Phase 2)
**What:** Run the 4h unattended flywheel smoke (hk-m8zqv), then — if it passes — activate the flywheel as the persistent captain replacing manual session-by-session orchestration. The design is complete (kerf `ready`); the gap is the commitment to run unattended and observe.

**Rationale:** This is the most direct path to the north-star Phase 2 ("substrate replaces sub-agents"). With named-queues, captain & crew, session-keeper, and flywheel infra all landed, the machinery for autonomous multi-session orchestration is present. What's missing is the operator's decision to run it and wire the real keeper opt-in for the captain session.

**Size:** Small implementation, large commitment. hk-m8zqv is one bead (the smoke). The larger size is the "enable keeper for captain" step and the monitoring/safety-check discipline around it.

**Class:** NEEDS-OPERATOR-RANKING. Operator needs to decide: run the 4h smoke now, and if it passes, create the `.harmonik/keeper/captain.managed` marker. This is the highest-risk step (a `/clear` on a live captain session has no undo) — requires informed operator consent.

---

### 5. Spec drift remediation + harness-contract spec finalization (KNOWN — minor, but accruing)
**What:** Batch-dispatch the 17 `kind:spec-drift` beads (all P2) and finalize the `harness-contract.md` spec (already on disk at `specs/harness-contract.md` via T18). Wire the spec-drift cluster to a kerf work or dispatch directly.

**Rationale:** 17 open spec-drift beads in HC, EV, ON, PL represent growing spec-code divergence. Each one is small but as a cluster they represent a correctness debt in the normative contracts. Batch dispatch is cheap and restores spec authority.

**Size:** Small. 17 discrete beads, likely all independently dispatchable. Zero design work needed.

**Class:** KNOWN work. No operator decision needed.

---

## Key Operator Decisions Required

| Decision | Impact | Urgency |
|---|---|---|
| Grant OAuth `workflow` scope to daemon/captain tokens | Unblocks release-pipeline 4/8 + CI tier re-block | High — release pipeline parked |
| Confirm willingness to run flywheel 4h smoke + keeper opt-in | Unblocks north-star Phase 2 | Medium — machinery ready |
| Advance logmine kerf work to spec-draft/tasks | Enables recurring self-improvement pipeline | Medium — no blocker except priority |
| Operator round-trip: watch keeper `/clear`→`/session-resume` on a real crew | Closes session-keeper epic | Low — one manual observation |
