# Adversarial Review — Seam Integrity (reviewer 1)

> Read-only adversarial pass, 2026-07-16, against LIVE source at branch
> `phase1-session-restart-substrate`. Every line claim below re-verified against
> `internal/daemon/daemon.go`, `workloop.go`, `branching/branching.go`, and
> `daemon_branchprotection_sul12_test.go` — NOT trusted from the design doc.

## Verdict: **APPROVE-WITH-CHANGES**

The core thesis holds: the pure config-resolution seam IS genuinely
import-isolatable to `$gostd + internal/core`, and the honest split
("thin pure subsystem + effect phase-helpers") is accurate. I verified the
island touches nothing in `branching`/`queue`/`workspace`/`eventbus` — only
`cfg` primitives + `core.WorkflowMode.Valid()`. The depguard edge is
achievable as drawn. BUT three concrete correctness traps must be fixed or
explicitly flagged before implementation — two of them are lifetime/ordering
regressions the design's own honesty caveats UNDERSTATE, and one is a
mislabeled API parameter that invites a real behavior bug.

---

## Findings (numbered, with severity + file:line evidence)

### 1. [HIGH — understated regression] The `jsonlWriter.Close()` defer has the SAME lifetime trap as pidfile, but B3 extracts it and the design does NOT flag it.
`internal/daemon/daemon.go:966` — `defer func() { _ = jsonlWriter.Close() }()`
lives inside the P4 block (Registry + bus construction, 950-1035), which **B3
(`wireBusAndSubscribers`) extracts wholesale**. A `defer` inside the extracted
helper fires when the helper *returns* — i.e. the event-log JSONL writer would
be **closed immediately after wiring, before the daemon ever runs its work
loop**. This is the identical class of bug the design correctly calls out for
`pidfile.Release()` (§8 risk #2), yet §8 flags ONLY the pidfile. There are
exactly two lifetime-spanning defers in the function (verified: 807 pidfile,
966 jsonlWriter; the 2044/2263 defers are goroutine-local and safe). B3 must
either keep `jsonlWriter` open+deferred in the outer shell, or return the
writer/closer so `startWithHooks` owns the `defer`. **This is a boot-breaking
omission, not a nit** — surface it in §8 alongside pidfile with equal severity.

### 2. [MEDIUM — observable behavior change] The umbrella `Resolve()` reorders workflow-mode validation to run AFTER `branching.Load` I/O.
Current order: workflow-mode fail-closed check at `daemon.go:820-825` runs
**before** `branching.Load` at `daemon.go:837-848`. The design's proposed call
site (§3) does `branching.Load` FIRST, then `bootconfig.Resolve` (which
validates mode as its internal step 1). Net effect: if `WorkflowModeDefault`
is empty/invalid AND `.harmonik/branching.yaml` is malformed, **current code
returns the workflow-mode error; the redesign returns the branching-load error
first.** The empty-mode misconfig (`hk-81n9r`) is common and currently
short-circuits before ANY I/O. Design §8 item 3 frames resolve/validate order
as *within-Resolve* only — it misses this daemon-side reorder of
`branching.Load` vs mode-validation. Fix: call
`bootconfig.ValidateWorkflowMode(cfg.WorkflowModeDefault)` **before**
`branching.Load` in the daemon, then call `Resolve` for the rest; or explicitly
document the precedence change as accepted. As written, "the daemon calls
[Resolve] once" (§2) forces the inversion.

### 3. [MEDIUM — API trap invites a real bug] `ValidateBranchProtection`'s `flagTarget` parameter is mislabeled; case (1) requires the POST-MERGE target, not the flag value.
Signature (§3): `ValidateBranchProtection(forbidDefault bool, flagTarget,
resolvedTarget string, protect []string) error`. But the live case-(1) check at
`daemon.go:865` is `cfg.ForbidUnprotectedDefault && cfg.TargetBranch == ""`
where `cfg.TargetBranch` is the value **after** the YAML merge at 842-843. If an
implementer reads "flagTarget" literally and passes `in.FlagTargetBranch` (the
pre-merge flag value), then the scenario "flag empty, YAML supplies `lands_on`,
`ForbidUnprotectedDefault=true`" would **falsely error** — current code sees a
non-empty merged target and passes. The design's "byte-exact / verbatim error
text" claim (§2, §8 item 3) does not call out that case (1) and case (2) read
*different* forms of the target (case 1: merged-but-unresolved, can still be
`""`; case 2: `resolveTargetBranch(...)`, `""→"main"`). Rename the param
(`mergedTarget`) and state the contract, or the "byte-identical precedence"
guarantee is not actually pinned by the signature.

### 4. [LOW — factual error in a "verified" doc] §9 open-question 4 claims in-daemon `resolveTargetBranch` callers at `daemon.go:1649` and `:2302`. Neither exists.
`grep -n resolveTargetBranch` across the package: the only `daemon.go` call is
`:864` (the one being moved). Real remaining callers are `workloop.go:1180` and
`export_test.go:509`. The `:1649`/`:2302` caller set is wrong — it *overstates*
churn (fewer callers to rewire than claimed), so it's low-risk, but it
undercuts the doc's "every file:line verified" framing. Correct the caller list.

### 5. [LOW — mischaracterized coverage, not a defect] §6 frames the branching-precedence (P2b flag>yaml merge) table cases as "migrated from the daemon-boot tests."
No existing test sets `ProjectDir` + a `branching.yaml` through the
`startWithHooks` P2b merge — the sul12 tests all leave `ProjectDir` unset
(`daemon_branchprotection_sul12_test.go:41-108`), so `branching.Load` (837) is
never hit by them. The precedence-merge table coverage is **net-new**, which is
a genuine win — just not "migrated." Harmless, but the design should not imply
pre-existing coverage is being preserved when it is being *created*.

---

## Verification of the brief's four specific questions

1. **Pure seam import-isolatable to `$gostd + core`?** YES — confirmed by
   reading the actual inline code (820-872). The four funcs touch only `cfg`
   primitives/slices and `core.WorkflowMode.Valid()`. Nothing reaches
   `branching`/`queue`/`workspace`/live handles. `branching.Load` (I/O) stays
   daemon-side; its result is projected to `string`/`[]string`. Edge holds.
2. **Load-bearing boot invariants preserved?**
   (a) `defer pidfile.Release()` — CORRECTLY flagged; must stay in outer scope.
   VERIFIED at `:807`. **But the jsonlWriter defer at `:966` is the second such
   trap and is NOT flagged (Finding 1).**
   (b) Phase ordering — the slice boundaries as drawn preserve the real
   invariants: orphan sweep (`:1496`) before `loadStartupQueues` (`:1740`);
   `sleepBootBackoff` (`:2059`) after socket bind (`:2047`); `daemon_started`
   (`:1297`) before `supervisor_revival` (`:1321`); all `Subscribe` before
   `bus.Seal()` (`:1271`). B2's `bootBackoffDelay` must be threaded from
   `runBootPreflights` return through to the outer `sleepBootBackoff` — design
   notes this. The socket goroutines (`:2045-2049`) are self-contained/drained
   within B5's block, safe to extract.
   (c) Branch-protection fail-closed — semantics preserved ONLY if Finding 3 is
   fixed (case-1 uses post-merge target).
3. **Precedence merge byte-exact in `Resolve()`?** Reproducible (fill-only-zero
   at 842-848 maps cleanly to `MergeBranchingDefaults`), and slice-aliasing
   behavior is preserved. The one edge that is NOT byte-exact as designed is the
   mode-vs-Load ordering (Finding 2) and the case-1 target form (Finding 3).
4. **Test migration holds?** YES, with a caveat: cases 1/2/3 assert
   `daemon.Start` surfaces a validation *error*; the retained daemon-side
   integration test MUST be an error case (to prove Resolve-error propagates
   through boot), not the happy-path case 4 (`TestDaemonStart_EmitsDaemonConfig`,
   returns nil). §6 intends this ("proves resolve-error propagates"), but B1's
   prose ("retain ONE ... integration test") is loose — pin it to an error case
   explicitly or the "propagates through boot" coverage is lost.

## Where the design's own honesty caveats UNDERSTATE risk
- §8 risk #2 flags pidfile but omits the **identical jsonlWriter defer** trap
  (Finding 1) — the more dangerous omission because B3, not B2, is where it bites
  and B3 is described as a clean "cut-and-call."
- §8 item 3 and §2 claim byte-exact ordering/precedence but the umbrella
  Resolve() **inverts mode-vs-`branching.Load`** (Finding 2) and the
  `flagTarget` label **misstates case-1's target form** (Finding 3). "Byte-exact"
  is not yet true as specified.
