# Quality audit — suppressions removed, 2026-07-22

**What this is.** A measured audit run in a throwaway worktree with every code-quality suppression
turned off, to answer two questions: *what is the tooling currently hiding*, and *which of the bad
areas has no owner in the P2 extraction plan*.

**Method.** Detached worktree at `20cbd18d`. Two full `golangci-lint` runs (v2.3.0, the pinned
`.tools/` binary), no `--new-from-rev`, no issue caps:

1. **base** — config exactly as committed, `//nolint` directives intact.
2. **stripped** — every `//nolint` directive deleted from every `.go` file, and every
   `.golangci.yml` exclusion rule deleted except the SC6 `path-except` block (that one *scopes* a
   two-package ban rather than hiding findings, so removing it would have produced pure noise).

Both runs compiled clean — zero `typecheck` issues — so the delta is real, not a broken build.
Raw artifacts and the stripped worktree are in the session scratchpad
(`base.json`, `stripped.json`, `newly.tsv`, `lintaudit/`).

---

## 1. Headline numbers

| | findings |
|---|---:|
| Merge gate actually sees (`--new-from-rev=origin/main`) | ~dozens, only on changed lines |
| Full run, config as committed | **6,674** |
| Full run, suppressions removed | **8,515** |
| **Hidden by `//nolint` + exclusion rules** | **2,279** |
| …of those, in **production** (non-test) code | **699** |

The 8,515 understates the truth slightly: golangci de-duplicates by line, so where a stripped
`//nolint:errcheck` let errcheck fire, it displaced a `gocritic`/`errchkjson`/`noctx` finding that
was already being reported on that same line. Net direction is unchanged.

## 2. The four suppression mechanisms

**(a) The `--new-from-rev` ratchet — by far the largest.** The merge gate is
`make check-short` → `golangci-lint run --new-from-rev=origin/main` (`Makefile:452`). Full
`golangci-lint run` is deliberately *not* a gate; `Makefile:555` says so, citing "~5666 pre-existing
legacy issues." That number is now **6,674**. This was a deliberate, documented decision
(`plans/2026-07-13-code-revamp/track-c-enforcement.md` §0) and it is the right call for capping new
code — but nothing is draining the grandfathered pile, and it has grown ~18% since the note was
written.

**(b) 2,605 `//nolint` directives** across 219 production files and 610 test files. By linter:
gosec 1,839, errcheck 614. Concentration: `internal/daemon` 869, `internal/lifecycle` 442,
`internal/workspace` 162, `internal/keeper` 150, `cmd/harmonik` 147, `internal/specaudit` 141.

**242 of them are dead** — `nolintlint` reports `directive is unused for linter "gosec"`, meaning
someone applied `//nolint:gosec` to lines gosec never flagged. That is bulk, cargo-cult suppression,
not considered judgement, and it is the strongest evidence that the directives were not individually
reasoned about. (Total `nolintlint` findings: 264.)

**(c) `.golangci.yml` exclusion rules** (`.golangci.yml:45-80`) — complexity ceilings
(`funlen`/`cyclop`/`gocognit`) switched off for all `_test.go`, `internal/scenario/`, and
`internal/specaudit/`; `forbidigo` off for `tools/` and `internal/testhelpers/`. Removing these
revealed 206 gocognit + 58 cyclop + 53 funlen, overwhelmingly in test and harness code. These
exclusions are defensible; they are not where the problem is.

**(d) The coverage gate is barely a gate.** `scripts/coverage-gate.sh` enforces 95% for spec-named
core subsystems, a 90% floor for other `internal/**`, and a 0.3pp regression cap. But:

- it is **not wired into `check-short`** (the merge gate) — `track-c-APPLIED.md` §3 records why:
  wiring it would break every merge, because `internal/substrate` sits at 83.1%;
- `is_internal` is computed from an `internal/` prefix, so **`cmd/**` is outside every gate** —
  that is 26,830 non-test LOC with no coverage floor and no baseline entry;
  > **[Correction 2026-07-23]** No longer true: commit `42010150d` (2026-07-22) added
  > `scripts/cmd-coverage-gate.sh` + `scripts/cmd-coverage.baseline`, wired into tier-2 `make check`.
  > It is a **ratchet** (per-package regression cap), not yet an absolute floor. The remaining gap is
  > *floor + drain*, not *existence*. See `plans/2026-07-21-p2-extraction/cmd-coverage-drain-plan.md`.
- the committed `coverage.baseline` itself records packages far below the 90% floor it claims to
  enforce: `eventbus` 58.4, `daemon` 58.9, `queue/cli` 65.3, `queue` 66.6, `crew` 68.1,
  `keeper` 72.6, `lifecycle` 76.5, `workspace` 80.8, `handler` 83.0.

So the floor is aspirational and the regression cap is the only live rule — and only when someone
runs `make check` by hand.

---

## 3. Where the code is actually terrible

### 3a. The two functions that dominate everything

| function | file | cognitive complexity | status |
|---|---|---:|---|
| `runWorkLoop` | `internal/daemon/workloop.go:1548` | **883** | grandfathered by the ratchet |
| `beadRunOne` | `internal/daemon/workloop.go:3168` | **390** | **suppressed with `//nolint:gocognit,cyclop,funlen`** |

The ceiling is 20. `beadRunOne` is the one to be angry about: `track-c-enforcement.md` §0 explicitly
instructs *"Do not add per-function `//nolint`; do not hand-maintain a suppression list"* — the whole
point of choosing `--new-from-rev` was that no directives would be needed. `workloop.go:3167`
adds one anyway (as does `:1499` and `:6542`). The stated rationale is "grandfathered pre-reactor
guard sequence (M3-D2); the M5 full reactorization decomposes it" — a real plan, but the directive
makes the debt invisible to every measurement rather than merely un-gated.

Next tier, all visible-but-grandfathered: `runReviewLoop` 332, `cmd/harmonik/main.go:run` 254,
`pasteInjectQuitOnCommit` 214, `driveDotWorkflow` 196, `keeper.(*Watcher).Run` 179,
`dispatchDotAgenticNode` 178, `queue.Validate` 172, `runBeadSubcommandIO` 140,
`runCommsRecvFollowIO` 119, `runHarnessWithSigs` 112, `SubscribeHub.HandleSubscribe` 110.

### 3b. Finding density in production code (suppressions removed)

Density, not raw count, is the useful lens — `internal/daemon` looks bad only because it is huge.

| package | prod findings | non-test LOC | per kLOC |
|---|---:|---:|---:|
| `internal/agentmanifest` | 67 | 881 | **76.0** |
| `cmd/harmonik/supervise` | 174 | 2,648 | **65.7** |
| `internal/queue/cli` | 114 | 1,792 | **63.6** |
| `internal/codexwire` | 81 | 1,400 | **57.9** |
| `internal/supervise` | 58 | 1,379 | 42.1 |
| `cmd/harmonik` | 952 | 26,830 | **35.5** |
| `internal/hookrelay` | 21 | 660 | 31.8 |
| `internal/sessiondata` | 20 | 647 | 30.9 |
| `internal/workspace` | 130 | 7,625 | 17.0 |
| `internal/lifecycle` | 94 | 6,832 | 13.8 |
| `internal/daemon` | 665 | 56,581 | 11.8 |
| `internal/keeper` | 86 | 7,819 | 11.0 |

`internal/daemon` is, by this measure, one of the *healthier* packages. The extraction plan's
targets and the quality hot-spots are largely disjoint.

### 3c. Specific defects worth fixing regardless of any refactor

- **10 `nilerr` in production** — an error is non-nil and the function returns `nil` anyway.
  Five are clustered in the orphan sweeper: `internal/lifecycle/orphansweep.go:59,865`,
  `orphansweepbeads.go:279,304,321`. Others: `internal/daemon/commscursor.go:246`,
  `internal/daemon/pasteinject.go:2267`, `internal/lifecycle/tmux/osadapter.go:95`,
  `internal/supervise/reap_osadapter.go:39`, `internal/workspace/claudetrust_wm040b.go:616`.
  These are swallowed failures in reaping and trust-repair paths — exactly the code whose silent
  failure is hardest to notice.
- **`internal/core` breaks its own layering.** `core/policydocument.go:8` imports `gopkg.in/yaml.v3`;
  `core/policyexprevaluator.go:9,10` import `github.com/expr-lang/expr`. depguard flags all three.
  `core` is meant to be the stdlib-ish shared kernel every package may import; a YAML parser and an
  expression VM in it are a real edge, not a config bug.
- **The `queue` depguard rule is misconfigured** and emits ~30 phantom findings — the allow-list
  omits the package's own import path (needed for external `_test` packages) and
  `github.com/google/uuid`, which `queue/rpc.go:33` and `queue/state.go:24` genuinely use. Likewise
  the `core` rule omits `pgregory.net/rapid`, producing 20 more phantoms in property tests. Net
  effect: depguard's 57 findings are ~52 noise and ~5 signal, so nobody reads them — which is how
  the `core` breach above stays unnoticed.
- **`internal/daemon/reviewloop.go:773`** — `deferInLoop`: `defer` inside a `for` loop, a genuine
  resource leak shape.
- **`contextcheck` cluster** in `internal/codexdriver` (`driver.go:250,252,278`,
  `session.go:216,217,598,609`) and the DOT loop (`dot_cascade.go:1849,1868,1898`,
  `reviewloop.go:653,731,770,1412,1464,1496`) — contexts not threaded, so cancellation does not
  propagate through teardown and timer paths.
- **`gosec` under the suppressions**, production code only: G304 (file inclusion via variable path)
  211, G204 (subprocess with variable args) 128, G301/G306/G302 (permissive dir/file modes) 154.
  Most are probably fine for a local orchestrator — but 172 G304 and 74 G204 of these were sitting
  under `//nolint`, so no one has triaged which ones are not.
- **errcheck in production**, unsuppressed top files: `workloop.go` 69, `cmd/harmonik/handler.go` 67,
  `cmd/harmonik/init_cmd.go` 65, `internal/agentmanifest/brief.go` 55,
  `cmd/harmonik/keeper_enable_doctor_cmd.go` 54, `cmd/harmonik/sync_assets_cmd.go` 44,
  `cmd/harmonik/smoke.go` 39.

---

## 4. Cross-reference against the P2 extraction plan

The P2 plan is thorough about what it covers, and several things I expected to be gaps turned out to
be handled explicitly and well — `tmuxsubstrate.go` is assigned to E4, `pasteinject.go` is argued
into E5 with the reasoning pre-written for the review, `crewstart.go`'s unexported-interface blocker
is identified down to the line. What follows is only what no unit owns.

### Covered — no action needed here

`workloop.go` / `beadRunOne` / `runWorkLoop` / `reviewloop.go` / `dot_cascade.go` (E5),
`pasteinject.go` (deferred into E5, deliberately), `tmuxsubstrate.go` (E4),
`crewstart.go` + crew wiring (E2a/E2b), queue wiring (E3a/E3b), `reversetunnel.go` (E4a, landed),
`internal/core` split (E6, parked by design).

### Gaps — nothing in P1, P2 or P3 owns these

1. **`cmd/harmonik` — 26,830 non-test LOC across 63 files, 952 production findings, 35.5/kLOC.**
   This is the second god package and the single largest hole. It appears in the P2 documents only
   as call-site edits (`E2-crew.md:283`, `E3-queue-wiring.md:214`), never as a subject. It also has
   **no coverage gate at all** (§2d) and no `coverage.baseline` entry. Its giants —
   `main.go:run` (254), `runBeadSubcommandIO` (140), `runCommsRecvFollowIO` (119),
   `runHarnessWithSigs` (112), `runKeeperDoctor` (66) — are exactly the "extract me" shape E5 exists
   to address in the daemon, with no equivalent plan.
   > **[Correction 2026-07-23]** cmd/harmonik now measures **45.9%** coverage (not 0/ungated) and IS
   > gated as of `42010150d` (a ratchet — `scripts/cmd-coverage-gate.sh`). Drain plan with a 9-chunk
   > parallelizable decomposition and a floor proposal: `cmd-coverage-drain-plan.md`.

2. **`internal/codexwire` — 1,400 LOC in a single file, 57.9/kLOC, of which 74 are `revive`.**
   Zero mentions anywhere in `plans/2026-07-21-p2-extraction/`. Given the Codex-first priority, a
   1,400-line single-file wire protocol with that much style/doc-comment debt deserves an owner.

3. **`internal/keeper` — 7,819 non-test LOC across 19 files**, `watcher.go` 2,110, `step.go` 1,187,
   `cycle.go` 935; `Watcher.Run` at cognitive complexity 179; 150 `//nolint` directives; coverage
   baseline 72.6%. Referenced in six P2 documents, owned by none.

4. **`internal/lifecycle` — 6,832 LOC**, `orphansweep.go` 1,005 + `orphansweepbeads.go` 796 +
   `startup_pl005_qm002.go` 955 (`reconcileThreeWay` at 92); **442 `//nolint` directives, the second
   heaviest in the tree**; 5 of the 10 production `nilerr` bugs. No unit.

5. **`internal/workspace` — 7,625 LOC, 130 production findings**, `CreateWorktree` at 30,
   `ensureWorktreeTrustAt` at 21, `noctx` on three `exec.Command` calls in
   `mergedispatch_wm018a.go:144,166,193`. No unit.

6. **`internal/eventbus/busimpl.go` — 1,632 LOC, coverage 58.4%** (the lowest recorded in the repo,
   and it is on the 95%-required list). No unit.

7. **The small dense packages**: `agentmanifest` (76/kLOC), `cmd/harmonik/supervise` (65.7),
   `queue/cli` (63.6), `internal/supervise` (42.1), `hookrelay`, `sessiondata`. Individually small
   enough that any one is a day's work; collectively the worst-quality code in the repo by density.

8. **The suppression debt itself has no owner** — 2,605 directives, 242 of them provably dead, the
   grandfathered pile grown from 5,666 to 6,674, and the coverage gate unwired. Nothing schedules
   the drain.

9. **The `core` layering breach** (yaml + expr, §3c) falls in E6's territory, and E6 is parked. It
   should not wait for a package split.

---

## 5. Suggested next moves

Cheap and high-value first — none of these compete with the extraction stream:

1. **Delete the 242 dead `//nolint:gosec` directives.** Mechanical, zero risk, and it stops the
   directives reading as considered judgement.
2. **Fix the `queue` and `core` depguard allow-lists** (add `internal/queue` self-import + `uuid`;
   add `pgregory.net/rapid` to `core`). Removes ~52 phantom findings and makes depguard's real
   signal — including the `core` → yaml/expr breach — legible again.
3. **Triage the 10 production `nilerr`.** Small, and they are live swallowed-error bugs in reap and
   trust-repair paths.
4. **Give `cmd/harmonik` an owner** — either a P2 unit or a separate stream. Bring it inside
   `coverage-gate.sh` (widen `is_internal`, or add a `cmd/` arm) and add baseline entries so it stops
   being invisible.
5. **Replace `beadRunOne`'s `//nolint:gocognit,cyclop,funlen`** with the same grandfathering
   everything else gets. Restoring the finding costs nothing (the ratchet already hides it from the
   merge gate) and puts a 390-complexity function back on the books where E5 can see it.
6. **Publish the number.** `_plan.md` §5 already asks each unit to report daemon LOC dropped; add
   "full-run finding count" alongside it, so the grandfathered pile has a visible trend instead of
   surfacing once a quarter as a surprise.
