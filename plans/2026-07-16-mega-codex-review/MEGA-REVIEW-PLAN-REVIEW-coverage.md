# Adversarial Review — Coverage & Completeness Lens

> Reviewer role: adversarial. Lens: does the ~24-unit decomposition actually cover **the whole
> system** (operator's word), is the M6 boundary correct, is the ADD-NOW/DEFER split right, and is
> the risk-tier ordering sound? Target artifacts: `MEGA-REVIEW-PLAN.md`,
> `review-decomposition-DRAFT.md`, `coverage-strategy-DRAFT.md`. Read-only.

## Verdict: **APPROVE-WITH-CHANGES**

The plan is strong on the loud, scarred subsystems (daemon workloop, tmux, remote, run-machine) and
its M6 boundary logic is mostly right. But it makes a **"no silent gaps"** claim
(`review-decomposition-DRAFT.md` §"Coverage check": *"Every `internal/*` and `cmd/*` package maps to a
unit"*) that is **demonstrably false** for `internal/workspace` and under-specified for
`cmd/harmonik`. One of those gaps (workspace merge/conflict/lease core, ~4.4k LOC) is the exact
category the correctness leg exists to catch (git concurrency + resource lifecycle), so it must be
closed **before Wave 1 fires**, not discovered mid-sweep. Not a BLOCK only because the plan is staged
("armed now, fires after giant-retirement") — there is a window to add the missing unit before fire.

---

## Fix-list (numbered, severity-tagged)

### 1. [CRITICAL] `internal/workspace` merge/conflict/lease core is in NO review unit — ~4.4k LOC hole

RU-04 ("Remote / SSH substrate") names only the *remote* files of `workspace`:
`remotematerialize.go`, `createworktree.go`, `reviewverdict.go`, `autostatusmarker.go`, `diffhash.go`.
But `workspace` is **6,465 prod LOC / ~31 files**. Enumerating what RU-04 does **not** name (verified
by file-by-file count):

```
 649  agenttask_chb028.go            353  conflictresolution_wm024.go
 425  claudetrust_wm040b.go          253  orphansweep.go        (workspace's own)
 404  claudesettings_wm040a.go       253  leaselock.go
 246  workspace.go                   218  sessionmetadatasidecar_wm063.go
 217  discoverworktrees.go           215  gitignorehygiene.go
 208  mergedispatch_wm018a.go        176  interruptstate_wm040.go
 149  implementerref_wm022.go        136  conflictescalation_wm023.go
 132  wipcapture_rc019.go            120  crashevidence.go
  92  lookupworkspace.go              65  conflictresolution_wm022a.go
  45  integrationbranch.go            26  taskbranch.go
                                          ── TOTAL unassigned: ~4,382 LOC
```

This is **not** peripheral code. It is worktree lifecycle (`createworktree` is in RU-04 but
`discoverworktrees`/`worktreepath`/`taskbranch`/`integrationbranch` are not), **merge dispatch**
(`mergedispatch_wm018a.go`), **three conflict-resolution modules** + escalation, **lease locking**
(`leaselock.go` — a cross-process mutex primitive), **WIP capture**, **interrupt state**, and **crash
evidence**. That is precisely the concurrency + git-resource-lifecycle + terminal-decision surface the
correctness leg is chartered to audit. The census's RU-04 lens (SSH/ack-free transport) will **not**
look at `mergedispatch`/`conflictresolution`/`leaselock` — different failure class, different reviewer
attention.

The plan's own §"Coverage check — is anything NOT in a unit?" asserts full package coverage and lists
only *easy-to-miss corners* (testhelpers, probes, assets). It never notices that a top-9 prod package
(workspace, 6,465 LOC — larger than keeper or lifecycle) is only ~30% assigned.

**Change:** add **RU-04b "Workspace lifecycle — merge/conflict/lease/worktree"** covering the ~4.4k
LOC above, Tier 2 (git-mutation + cross-process lock is data-integrity-adjacent), lane C or BOTH for
`mergedispatch`/`leaselock`/`conflictresolution` (terminal git decisions). Do this **before Wave 1
fires**. Reconcile the "every package maps to a unit" sentence with the file-level truth.

### 2. [HIGH] `cmd/harmonik` file→RU mapping is unspecified for ~3–4k LOC of coherent CLI subsystems

`cmd/harmonik` is 26,187 prod LOC. RU-16a names four files (`comms/main/run/harness` = ~5.5k) plus
"+ smaller top-level cmds"; RU-16b names keeper/init/assets/crew/captain/start; RU-17 takes supervise.
The LOC *buckets* roughly add up, but the **assignment of specific files is left to "+smaller cmds"**,
which means whole coherent CLI subsystems have **no named owner**:

- **Eval-harness CLI** — `eval_cmd.go` (448) + `eval_metrics_cmd.go` (387) + `eval_report_cmd.go`
  (236) + `eval_guardrails_lygpp.go` (93) ≈ **1.2k LOC**, named in no RU.
- **Gate / verdict CLI** — `decisions.go` (851) + `decisions_k4.go` (538) + `confirm_verdict.go`
  (229) + `veto_verdict.go` (172) + `write_review_verdict_cmd.go` (154) + `greenlight_cmd.go` (125) +
  `goalkeeper_cmd.go` (216) ≈ **2.3k LOC** — this is the review-gate control surface (directly
  relevant to the plan's own review-gate concerns), named in no RU.
- Others adrift: `handler.go` (770), `smoke.go` (601), `schedule.go` (509), `sleepwake.go` (392),
  `sentinel_cmd.go` (392), `ops_monitor_cmd.go` (386), `dashboard_cmd.go` (358), `state_cmd.go`
  (197), `graph.go` (210), `substrate_select.go` (182), `reconcile.go` (286),
  `migrate_rc_prefix_cmd.go` (263), `branch_reap_cmd.go` (216).

A reviewer working RU-16a/16b off the current text can plausibly finish "their" named files and never
touch `decisions.go` — a silent gap. **Change:** replace "+ smaller top-level cmds" with an **explicit
file manifest** per RU-16a/16b (a `ls cmd/harmonik/*.go` diff'd against the two unit scopes at fire
time), so every top-level `.go` lands in exactly one RU. Flag the eval-harness and gate/verdict CLI as
their own sub-buckets — they are coherent subsystems, not stragglers.

### 3. [MEDIUM] The reconcile/brcli **false-close** seam is a live fabricated-done-status bug buried at Tier 2/3, single-lane — its sibling (runbridge) is Tier 1 BOTH

The plan correctly elevates `runbridge.go` (fabricated done-status; hk-2hfyt closed with fix absent)
to Wave 1 / Tier 1 / BOTH and makes it ADD-NOW #1. But the **same class of bug** — a close path that
can *fabricate done-status* — is documented for the **reconcile** path: `review-decomposition-DRAFT.md`
RU-12 "places to check" says *"`noChange`-subsumption closed `hk-2hfyt` on a bead-ID mention in an
unrelated docs commit — close path can fabricate done-status. Class B fires ~83×/session."* That seam
spans **RU-12** (lifecycle/reconcile, Tier 2, **Claude-only**) and **RU-18** (`brcli`
`terminaltransition_bi010.go` + `intentlogwrite.go`, Tier 3, **Codex-only**). Two different single
lanes, two different waves, for a **live data-integrity bug of the same severity** the plan cross-
checks runbridge for.

**Change:** either (a) pull the reconcile-close / brcli-terminal-transition seam forward into a Tier-1/2
sub-unit with a BOTH-lane cross-check, or (b) explicitly state in §3 why the reconcile false-close is
lower-severity than the runbridge one. As written the ordering is internally inconsistent about "who
fabricates done-status."

### 4. [MEDIUM] `twinparity` is bucketed under RU-13 (keeper/Codex) but is M6 WS3-F1 foundation code needing the M6-verification lens, not a keeper review

RU-13's scope pulls in `twinparity` (963) and `keepertwin`. But `internal/twinparity` is **brand-new
M6 code** (WS3-F1, commit `d01e27f8`, landed 2026-07-16 — it is the twin↔real parity *equivalence
library*, not keeper logic). The plan's own §5 defines the correct job for it: *"Verify the parity
harness's equivalence library actually **fails on a mutated stream**."* COORD c054 records that WS3-F1's
round-1 review caught a **load-bearing false-negative** (the terminal spine had order inversions +
non-journaled kinds, so it would have *accepted* mutated streams) — exactly the failure the review must
re-check. Filing twinparity as a generic "keeper, Codex-lane, Tier 2" review misframes what to look
for. **Change:** move `twinparity` out of RU-13 into the §5 M6-verification track (or tag it inside
RU-13 with the explicit "does the equivalence library reject a mutated stream?" mandate). Same for the
`internal/scenario` prune in RU-22 — see #5.

### 5. [MEDIUM] RU-22 "prune the scenario corpus" now collides with the *already-landed* M6 WS1.2/1.3 gate

RU-22 says to keep the harness + ~11 real files and **prune the ~37 structural-corpus files**. But M6
WS1.2/1.3 **already landed** (COORD c054, commit `4caa9822`): `check-full` now delegates its scenario
line to `test-scenario`, which **includes `./internal/daemon/...`** and makes the remote-substrate E2E
a real gate. Pruning scenario files that the newly-wired `check-full` now compiles/runs would be a
regression the plan does not warn about. **Change:** RU-22's prune list must be diff'd against what
`test-scenario` / `check-full` now pull in **at fire time**; the plan already says "re-check which WS
merged" (§5 note) but does not connect that to RU-22's deletion recommendation specifically.

### 6. [MEDIUM] Take a position on Q9 — assets/skills (RU-24) and spec-drift (RU-25). Both belong in "whole system", at least partially.

The operator said **the whole system**. The plan defers this to Open Question Q9. My position:

- **RU-24 (assets) — PARTIALLY IN SCOPE, don't blanket-exclude.** `cmd/harmonik/assets/` ships **4
  shell scripts (~395 LOC) + 4 `.tmpl` + 15 skill `.md`** embedded in the binary. The **shell scripts
  and templates are executable product code** — a bug in a scaffold script is a real runtime bug on a
  real machine, squarely in the correctness leg's charter. **Include the scripts/templates in a review
  unit** (fold into RU-16b's asset verbs, or a light RU-24). The markdown skills are agent-behavioral
  contract — a lighter "does the skill still match the code it documents" pass, lower priority, is
  defensible to defer; but the executable assets should not be excluded as "non-code."
- **RU-25 (spec-vs-code drift) — SHOULD be a cross-cutting pass, not merely per-unit.** The project's
  whole premise is spec-first (CLAUDE.md: *"the spec is always right, code is expected to match it"*;
  ten locked decisions). The plan already lists "spec drift" as an in-scope target in the per-unit
  Codex/Claude prompts — but per-unit spec-drift only catches drift **inside a reviewed file**. It
  cannot catch **an orphaned spec with no implementing code**, or a normative `MUST` that was never
  built. That inventory-direction check (walk `specs/`, confirm each normative clause has a live
  implementation) is the single highest-leverage cross-cutting pass for a spec-first repo and is not
  covered by reading `specs/` "as input." **Add a light RU-25 spec-coverage pass.** Excluding it is a
  legitimate operator call, but the plan should force that call, not leave it as "maybe."

### 7. [LOW] §4.5 mislabels hook/policy as "M6-covered" and creates an apparent RU-14 contradiction

§4.5 ("M6-covered areas — DO NOT DUPLICATE") leads with hook/policy ("Leave alone"). But hook/policy
coverage is **pre-existing STRONG unit coverage, not M6-harness coverage** — M6 is the controlled
harness (WS1–WS5: scenario gate, twin parity, core-loop-proof, assessor, risk-tiering). Meanwhile §3
Wave 3 assigns **RU-14 (hook system) to the Codex lane**. A reader reconciling the two sees a
contradiction ("leave alone" vs "review it"). The intent is fine — the *coverage leg* says don't add
tests, the *correctness leg* still reads the code — but say so. **Change:** retitle §4.5 to separate
"already-STRONG, don't add coverage" from "M6-harness territory," and note RU-14 still does a
correctness read even where coverage is left alone.

---

## What the plan gets RIGHT (so the fix-list isn't mistaken for a rewrite)

- **Both top census hazards are in Wave 1.** `runbridge.go` (fabricated done-status) is inside RU-01
  (Wave 1, Tier 1, BOTH, and ADD-NOW #1). tmux paste **landing** is RU-05 (Wave 1, Tier 1, BOTH,
  split a/b). Neither top hazard is buried — the risk-tier ordering is correct on the axis the task
  flagged. (The one ordering inconsistency is the *reconcile* false-close sibling — fix #3.)
- **The ADD-NOW vs DEFER-TO-M6 split is sound.** All six ADD-NOW items are genuinely pure-logic
  (runbridge via `substrate.Twin`+`FakeClock`; tmux **argv** tests via fake-binary, explicitly
  disclaiming landing; `EmitWorkerOfflineEvent` real-body via recording bus; `fireOnCancel`; queue
  two-writer). All four DEFER items genuinely require a real agent/transport/tmux. The only borderline
  is ADD-NOW #5 `SpawnKeeperWindow` — its body spawns a tmux window, so a "body test" needs the same
  fake-`tmux`-binary technique as #2; it's still legitimately ADD-NOW but the plan should note the
  fake-binary dependency rather than calling it a plain body test.
- **The M6 non-duplication boundary rule is correct** ("controlled-harness gap → M6 verifies;
  pure-logic gap → review calls for a unit test now"), and §5's "verify M6's own tests aren't
  themselves fake-anchored" is the right adversarial posture. The four DEFER→M6 mappings are accurate.
- **The BOTH-lane budget escape hatch** (Q10: demote RU-08/RU-09 to single-lane if capacity tight,
  keep BOTH only on the 5 Rebuild-verdict units) is a sensible pressure valve.

---

## Bottom line

Fire-blocking before Wave 1: **#1 (workspace ~4.4k-LOC hole)** and **#2 (cmd/harmonik file manifest)**
— these two defeat the "no silent gaps / whole system" claim as written. #3–#7 are refinements that
can be folded in during the fire-time re-validation the plan already schedules against the post-
giant-retirement tree. With #1 and #2 closed, the decomposition genuinely covers the system.
