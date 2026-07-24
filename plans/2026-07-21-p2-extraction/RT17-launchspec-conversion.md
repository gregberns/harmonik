> **Verified against the live tree** on 2026-07-23 at HEAD `2cca300cd` (branch `phase1-session-restart-substrate`). `go build ./internal/... ./cmd/...` exits 0; only untracked change is `.harmonik/crew/admiral-initiatives.md` (a doc). Every line number below was re-grepped live; the map/design docs cited HEAD `18c80cf61` and drifted by a few lines — trust this document's coordinates and re-grep before editing anyway (8th consecutive slice with line-drift).

# RT17 — launchSpecBuilder read-seam bind onto LaunchPort

## §0 — Corrections to the parent E5 docs (re-anchored live)

RT17 binds the daemon run path's **launchSpecBuilder READERS** onto a narrow `(*workLoopDeps).launchBuilder()` accessor (the exact `emitterPort()` alias pattern) and freezes the raw field with a new tripwire wired into both CI tiers. It is a pure, sed-provable token substitution with zero behaviour change. It does **not** delete the two copy-mutation assignments — that is RT18.11 (see §0.2, decided on mechanics, not preference).

### §0.1 — Coordinate corrections

| Doc claim (E5-CHUNK-CATALOGUE / PROGRESS / STAFFING) | Measured now (HEAD 2cca300cd) | Verdict |
|---|---|---|
| launchSpecBuilder assignments at workloop.go 3977/3987 (also cited 3973/3983) | `deps.launchSpecBuilder = routedLaunchSpecBuilder(` @ **workloop.go:3933**; `= claude.BuildLaunchSpec` @ **workloop.go:3943**, both under `if deps.launchSpecBuilder == nil` (3931) | CORRECTED |
| run-path READ sites | **reviewloop.go:317** (`implSpecBuilder`), **reviewloop.go:1263** (`revSpecBuilder`), **dot_cascade.go:1438** (`specBuilder`), **dot_gate.go:358** (`specBuilder`) — all `:=` capture form | CORRECTED |
| rp.Launch binding | `rp.Launch = launchPort(deps.launchSpecBuilder)` @ **workloop.go:3950**; single-mode consumer `rp.Launch.BuildSpec(ctx, rc)` further down (comment @ 4301) | CORRECTED |
| exit-gate `deps.` counts (137/88/41; target 0/0/0) | live `grep -c 'deps\.'` = **reviewloop 94 / dot_cascade 74 / dot_gate 43** (exactly RT16's leave-off) | CORRECTED — 137/88/41 baseline and 0/0/0 target are STALE |
| "four clock copy-mutation sites" | **FIVE** live: workloop.go:3106, dot_cascade.go:221, dot_cascade.go:1266, reviewloop.go:231, **AND dot_gate.go:270** (added by RT14-B `cb89e35e2`, never inventoried) | ADDED — RT18's problem, flag for RT18; **out of scope for RT17** |
| runports.go imports "only context" / needs core added | runports.go **already imports** `core`, `handler`, `handlercontract`, `harness/shared`, `substrate`, … (lines 21-34). Only **`github.com/gregberns/harmonik/internal/harness/claude`** is genuinely absent — but the raw-field accessor (§2) does **not** reference `claude`, so **no new import is required at all** | CORRECTED (Design 1/2 stale-import claim rejected) |
| seam symbols | `type LaunchPort interface{BuildSpec…}` @ runports.go:165; `daemonLaunch` value-receiver @ 206/210; `func launchPort(builder…) LaunchPort` @ 217; `RunPorts.Launch LaunchPort` @ 287; `func (deps *workLoopDeps) runPorts()` @ 336 **leaves `Launch` nil** (wires only Ledger/Emitter/Merge/Gate/Clock) | KEPT — the pre-existing, already-bound seam |
| in-code marker workloop.go:3949 "RT8 migrates them" | live: `// DOT sub-drivers still read deps.launchSpecBuilder (RT8 migrates them).` — "RT8" is the old numbering for **this** slice | KEPT (reword during this slice) |

### §0.2 — Scope-ownership contradiction, RESOLVED on mechanics

PROGRESS.md/STAFFING say "RT17 = the two launchSpecBuilder copy-mutation sites"; E5-CHUNK-CATALOGUE says RT17 = hookStore/timeouts/sandbox/delivery and assigns the assignment **deletion** to **RT18.11**. **These reconcile without preference:** the two assignments resolve the builder from per-run **locals** — `emit` (`emit := rp.Emitter`, workloop.go ~3134), `beadRecord` (from `env`), and `env.DefaultHarness` — **none of which are `workLoopDeps` fields**. No `deps`-accessor can reproduce `routedLaunchSpecBuilder(...)`; deleting the assignments requires those locals threaded through `beadRunOne → dispatchDotAgenticNode/executeCognitionGate/runReviewLoop` via a **signature change**, which is RT18's job. **RT17 therefore CANNOT delete the assignments without doing RT18's signature work.** RT17 = install the read-seam (bind the four readers onto the accessor); RT18.11 = delete the two assignments, with this accessor as its now-satisfied precondition. This is the RT16 payoff pattern (RT16's +25 LOC made RT18's re-signature 8 lines not 108).

### §0.3 — Exit-gate restated honestly (**operator-ack gate, see §4 step 0**)

The inherited exit gate ("`deps.` counts 94/74/43 falling further") is **not moved** by this slice and MUST NOT be RT17's acceptance signal. The accessor keeps a `deps.` reference (`deps.launchSpecBuilder` → `deps.launchBuilder()`), so `grep -c 'deps\.'` is **count-neutral** — 94/74/43 stay flat **by design**. The large aggregate drop and 0/0/0 belong to RT18/RT19's signature drop. RT17's **real, measurable** acceptance criteria (§5):
1. raw-field CODE-read count `deps\.launchSpecBuilder` on reviewloop/dot_cascade/dot_gate falls **2/1/1 → 0/0/0**;
2. the mechanical sed-normalization proof (every `-/+` code line collapses to an identical pair);
3. the launchSpecBuilder **seam tests** stay green (byte-identical-arg oracles);
4. the freeze gate is wired into check-fast + check-short and proven RED in four directions.

## §1 — What changes

| # | File | Site (live line) | What | Why |
|---|---|---|---|---|
| 1 | internal/daemon/runports.go | after `emitterPort()` @ 84 | ADD `func (deps *workLoopDeps) launchBuilder() func(context.Context, shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) { return deps.launchSpecBuilder }` | the narrow raw-field accessor — exact `emitterPort()` (`return deps.bus`) analog |
| 2 | internal/daemon/reviewloop.go | 317, 1263 | `:= deps.launchSpecBuilder` → `:= deps.launchBuilder()` (×2) | route the implementer + reviewer readers through the accessor |
| 3 | internal/daemon/dot_cascade.go | 1438 | `:= deps.launchSpecBuilder` → `:= deps.launchBuilder()` | route the DOT work-node reader |
| 4 | internal/daemon/dot_gate.go | 358 | `:= deps.launchSpecBuilder` → `:= deps.launchBuilder()` | route the cognition-gate reader |
| 5 | internal/daemon/workloop.go | 3950 | `launchPort(deps.launchSpecBuilder)` → `launchPort(deps.launchBuilder())` | uniform seam (single-mode binding reads through the accessor too) |
| 6 | internal/daemon/{reviewloop,dot_cascade,dot_gate}.go | reviewloop 258/1247, dot_cascade 1427/1437, dot_gate 333/356 | reword the ~6 prose comments that spell the literal `deps.launchSpecBuilder` so they no longer contain that substring (e.g. "the pre-built launch-spec builder") | the freeze gate matches the bare field and **counts comments** (the live emitter gate's deliberate choice: "a gate that misses a real site is worse than one that objects to prose") — these must reach 0 |
| 7 | scripts/launchbuilder-freeze-gate.sh | new | the tripwire (§4 step 5) | hold the seam for RT18 |
| 8 | Makefile | ~370 / ~579 / ~612 | `.PHONY` + target block; append to check-fast; append to check-short | HARD CI failure in both tiers |
| 9 | plans/2026-07-21-p2-extraction/RT17-launchspec-conversion.md | new | this plan | the RT-slice doc |

### §1c — Explicitly NOT in this slice (bundling any forfeits the zero-behaviour claim)
- The **deletion** of the two `deps.launchSpecBuilder = ` assignments (workloop.go:3933/3943) and dropping `deps` from run-path signatures → **RT18.11** (needs per-run locals threaded; §0.2). The gate freezes them at exactly 2.
- The **FIVE** clock copy-mutation sites (workloop 3106, dot_cascade 221/1266, reviewloop 231, **dot_gate 270**) → **RT18.1/.2** (note: live count is 5, not the docs' 4 — flag for RT18's planner).
- `SharedHandles` bundle, `deps.tidGen`, `RunEnv.BrPath` → RT18.
- **Widening** LaunchPort (hookStore/timeouts/sandbox/delivery/pasteInject; `codexNoWorkDurationFloor`; the `pinnedHarnessLaunchSpecBuilder`/`routedLaunchSpecBuilder` port-routing) — the catalogue's alternate "RT17.0-.17" scope under its numbering scramble → a **different, later** slice. EXCLUDED.
- The PC/ProjectConfig lift → gates RT20, not RT17 (`daemon.ProjectConfig` type-checks fine inside package `daemon`).
- **No** file moves, **no** new export, **no** signature change, **no** LaunchPort widening, `rp.Launch`/`daemonLaunch`/`launchPort()` untouched.

## §2 — The seam it exits behind (pre-existing; nothing new is invented)

| Element | Location | Note |
|---|---|---|
| `LaunchPort interface { BuildSpec(ctx, shared.LaunchCtx) … }` | runports.go:165 | already exists; **not widened** by RT17 |
| `daemonLaunch{builder}` + value-receiver `BuildSpec` | runports.go:206/210 | untouched |
| `func launchPort(builder…) LaunchPort` | runports.go:217 | untouched |
| `RunPorts.Launch LaunchPort` | runports.go:287 | untouched |
| `func (deps *workLoopDeps) emitterPort() EmitterPort { return deps.bus }` | runports.go:84 | the **precedent** the new accessor copies exactly |
| `runPorts()` leaves `Launch` **nil** | runports.go:336 | the narrow-accessor trap (§7 risk 3) — do **not** reach the port via `deps.runPorts().Launch` |

**Why a raw-field accessor, not `rp.Launch.BuildSpec`, and not a default-folding accessor:** the four readers do not merely *call* the builder — they capture it as a reassignable **func value**, conditionally overwrite it per node (`pinnedHarnessLaunchSpecBuilder` / `routedLaunchSpecBuilder`), then apply a load-bearing `if x == nil { x = claude.BuildLaunchSpec }` fallback, then call it. (a) `rp.Launch.BuildSpec` is a value-receiver method value that is **never nil** even when the wrapped builder is nil — it would silently kill the claude fallback and panic on a nil builder; and an interface value cannot be reassigned by handing it a bare func. (b) A default-folding accessor (return field-or-claude) would change the value the local holds, making the proof a *correctness argument* rather than a *mechanical* one. **`launchBuilder()` returning the raw field is a pure alias** — every downstream guard, per-node reassignment, and `specBuilder(ctx, rc)` call stays byte-identical, and the whole diff sed-normalizes to identity. The name has no `deps.launchSpecBuilder` substring, so it never false-matches the gate.

## §3 — Coupling to break

| Metric | Before | After |
|---|---|---|
| raw-field CODE reads `deps\.launchSpecBuilder`: reviewloop / dot_cascade / dot_gate | 2 / 1 / 1 | **0 / 0 / 0** |
| bare-field `deps\.launchSpecBuilder` incl. comments: reviewloop / dot_cascade / dot_gate | 4 / 3 / 3 | **0 / 0 / 0** (reads converted + comments reworded) |
| `deps.launchSpecBuilder = ` assignments (workloop) | 2 | **2 (preserved — RT18.11 deletes)** |
| exit-gate `grep -c 'deps\.'`: reviewloop / dot_cascade / dot_gate | 94 / 74 / 43 | **94 / 74 / 43 (flat by design — §0.3)** |
| new exports | 0 | **0** |
| top-level non-test daemon file count / LOC moved | — | **0 files moved; ~+3 LOC (accessor)** |

## §4 — Step-by-step recipe

**Step 0 — PRECONDITIONS + operator-ack (abort if any red):**
- **Operator/captain sign-off** (the one genuine judgment call, per every reviewer): confirm RT17 = the **read-seam bind** whose acceptance is §0.3's four criteria, and that (i) the aggregate 94/74/43 target is re-attributed to RT18/RT19, (ii) the two assignments are deleted by RT18.11. Do **not** record the task's "convert the two launchSpecBuilder sites" as satisfied by *deletion* — RT17 delivers the read-bind; RT18.11 deletes. File/annotate a bead to reconcile PROGRESS vs CHUNK-CATALOGUE.
- RT14 + RT15 + RT16 landed (git log: `2cca300cd`… includes `4cd9179d6` RT16, RT15 7 chunks, `cb89e35e2` RT14-B). Tree clean. `go build ./internal/... ./cmd/...` exits 0 (**never** `./...` — plans/…/10-zeromq is unconditionally red). `go vet -tags 'scenario e2e_real_claude integration' ./internal/daemon/...` compiles. `df -h` > 10 GiB free (below the 10240 MiB watermark fakes dispatch-timeout failures). Operate from a **clean detached worktree at own HEAD**, never the shared tree; never `cd` into a worktree (use `git -C`). **Re-grep** all coordinates live.

**Step 1 — add the accessor** (runports.go, immediately after `emitterPort()` @ 84). Body is `return deps.launchSpecBuilder` — reads ONLY the one field, never `deps.runPorts()` (nil `Launch`) nor `deps.clock`. Doc-comment: states it returns the FUNC (readers reassign/pin it) and is the narrow analog of `emitterPort()`. **No new import** (the raw field's type uses `context`/`shared`/`handler`, all already imported).

**Step 2 — convert the four reads**, bottom-up by descending line so numbers stay valid: dot_gate.go:358 → dot_cascade.go:1438 → reviewloop.go:1263 → reviewloop.go:317. At each, replace exactly `deps.launchSpecBuilder` with `deps.launchBuilder()`. **Leave byte-identical:** every trailing `if <local> == nil { <local> = claude.BuildLaunchSpec }`, every per-node reassignment (`= pinnedHarnessLaunchSpecBuilder(...)` / `= routedLaunchSpecBuilder(...)`), and every `<local>(ctx, rc)` call.

**Step 3 — convert the single-mode binding** workloop.go:3950 `launchPort(deps.launchSpecBuilder)` → `launchPort(deps.launchBuilder())` (byte-identical: the field is already resolved by the 3931-3945 block above it). Do **not** touch 3933/3943.

**Step 4 — reword the ~6 comments** (reviewloop 258/1247, dot_cascade 1427/1437, dot_gate 333/356) so none contains the substring `deps.launchSpecBuilder`; also update the stale `RT8 migrates them` marker at workloop.go:3949 to name RT17 (workloop is a ceiling file, so this is free). Preserve every comment's meaning.

**Step 5 — author `scripts/launchbuilder-freeze-gate.sh`**, **cloned from the live `scripts/runloop-emitter-gate.sh`** (recursive `find internal/daemon -type f -name '*.go' ! -name '*_test.go'`; `count_matches(){ { grep -oE "$2" "$1" || true; } | wc -l | tr -d ' '; }`; both-direction budgets; missing-file guard). **Four teeth:**
- **(A) Field-read ratchet — BARE field** `FIELD_RE='deps\.launchSpecBuilder'` (matches EVERY read form — `:=`, `= x`, arg-pass, `return`, direct-call — **and comments**; this is the fix for the gate-first design's `:=`-only false-negative hole, adopting the emitter gate's proven bare-field choice). Per-file budgets, measured **post-edit** and hard-coded: **EXACT 0** on reviewloop.go / dot_cascade.go / dot_gate.go; **CEILING** on workloop.go (~8, the `== nil` guard + 2 assignments + its doc comments — set to the measured value), runports.go (**1**, the accessor body), reviewerharness_hkiv748.go (**3**, doc comments). Any unlisted file budgeted 0 (recursive scan defeats the `-maxdepth 1` sub-package blind spot). EXACT budgets asserted **both directions** (a shrink on an EXACT-0 file can't happen; a shrink below workloop's ceiling is allowed — it's a ceiling, so RT18 rewording is not blocked).
- **(B) Assignment ratchet** `ASSIGN_RE='deps\.launchSpecBuilder = '` (space-equals-space excludes `== nil` and comment prose) — **EXACT 2 in workloop.go, 0 everywhere else, both directions.** A 3rd fails (regrowth); a shrink below 2 fails with a message: "RT18.11 may have landed — lower this budget deliberately." This is what catches an inline local-based resolution that adds no accessor call (Design 1's gate hole).
- **(C) Seam pins:** `grep -q 'func (deps \*workLoopDeps) launchBuilder()' runports.go`; `grep -q 'type LaunchPort interface' runports.go`; `grep -qE '^func launchPort\(' runports.go`; `grep -qE '^\s*Launch\s+LaunchPort$' runports.go`.
- **(D) Call-site pins** (defeats "seam exists but every site abandoned it" — the hole RT14's gate had): `count_matches CALL_RE` with `CALL_RE='deps\.launchBuilder\(\)'` ≥ reviewloop.go 2, dot_cascade.go 1, dot_gate.go 1, workloop.go 1. **Documented:** when RT18.11 re-resolves into `rp.Launch` and deletes the assignments, teeth B and D go red and **RT18 updates them deliberately** — the intended re-sign behaviour, exactly as the emitter gate documents for its own PORT_SITES.
- **Documented convention limitation** (inherited from the emitter gate): the regexes bind the receiver name `deps.`; a read through a differently-named receiver is invisible. Acceptable — all `workLoopDeps` methods name their receiver `deps` and all movers take `deps` by value; widen and re-measure if that ever breaks (matching `\.launchBuilder\b` bare would false-fire on unrelated fields). `chmod +x`.

**Step 6 — prove the gate RED before trusting it**, four directions, then revert each: (i) a 3rd `deps.launchSpecBuilder = ` anywhere → tooth B fires; (ii) a shrink of the assignment budget below 2 (premature RT18.11) → tooth B fires; (iii) a new `deps.launchSpecBuilder` read in a daemon **sub-package** (throwaway `internal/daemon/router/x.go`) → tooth A fires (proves recursive scan closes the `-maxdepth 1` blind spot); (iv) two reads on one line → `grep -oE` counts 2 (proves the `grep -c`-counts-lines blind spot is closed). Confirm GREEN at rest. (The `^(func|var|const)` grouped-`var` blind spot is structurally N/A — `launchSpecBuilder` is a struct field, not a top-level decl — but state it.)

**Step 7 — wire the Makefile** in three spots styled identically to the 11 siblings: `.PHONY: launchbuilder-freeze-gate` + target after the runloop-emitter-gate block (~line 370); append `scripts/launchbuilder-freeze-gate.sh` as the last gate line in **check-fast** (after line 579) AND **check-short** (after line 612). HARD failure, not a warning.

**Step 8 — mechanical zero-behaviour proof:** produce the full diff; `sed 's/deps\.launchBuilder()/deps.launchSpecBuilder/g'` over it and assert every `-/+` **code** line collapses to an identical pair. The **only** permitted unpaired additions: the accessor def + doc-comment, the reworded comments, the gate script, the 3 Makefile lines, the plan doc. Any other unpaired line → stop.

**Step 9 — run the verification gate (§5), commit as ONE atomic commit** (`git commit -F`, `Reviewed-By:` + `Review-Verdict:` trailers), independent agent-review APPROVE. The reviewer additionally gives **RT15's seven `Reviewed-By: self` commits** the independent eyes they never got (RT17 edits the same functions RT15 re-signatured: `runReviewLoop`, `dispatchDotAgenticNode`, `executeCognitionGate`, `beadRunOne`). Do **not** abandon partway — every commit boundary here is a valid alias state, but land the whole slice in one sitting; if abandoned, `git revert` whole.

## §5 — Verification gate (oracle)

**The oracle is DIFFERENTIAL-GREEN over a STABLE INTERSECTION, never one-run-vs-one-run** (three runs of one commit gave 123/3/9 failures sharing 1 name — a single before/after pair is not evidence). On a clean detached worktree at own HEAD, `df -h` > 10 GiB first, **serialized** (no parallel agent fan-out/build — that manufactured 5/6 baseline failures):

| # | Command | Pass criterion |
|---|---|---|
| 1 | `go build ./internal/... ./cmd/...` | exit 0 (**never** `./...`) |
| 2 | `go vet ./internal/daemon/...` **and** `go vet -tags 'scenario e2e_real_claude integration' ./internal/daemon/...` | clean (tag-gated daemon files never compile under plain `go test`) |
| 3 | `golangci-lint run --new-from-rev=HEAD` | clean (**never** full — full lights ~2k grandfathered findings) |
| 4 | `scripts/launchbuilder-freeze-gate.sh` | prints `OK`; proven RED in the four directions (step 6) |
| 5 | **seam behavioural oracles** (byte-identical-arg), `-count=10` for stability: `dot_gate_reviewer_harness_hk01vs0_test.go` (gate keeps the field unless it swaps — the capture-replace oracle), `harnessregistry_test.go` (routedLaunchSpecBuilder byte-identical Binary/Args/Env/WorkDir), `dot_model_effort_hkq8nqr`, `dot_node_model_effort_hkmca0b`, `dot_prompt_hksdnzj`, `dot_role_surfacing_hkm5lmo`, `launch_runner_threading_hk3sus`, `launch_substrate_runner_threading_hkfxy9` | all green both runs — these fail loudly if the resolved builder stops propagating to a sub-driver |
| 6 | **differential daemon suite**: `go test ./internal/daemon/... ./internal/harness/... ./internal/runmerge/... -count=1 -timeout 25m` **before AND after**, IDENTICAL package scope (dropping runmerge invalidates it) | extract `--- FAIL` names → `sort -u` both files → `comm -13 before after` (comm requires **sorted** input) MUST be EMPTY (after adds no name absent from before; shrinking fine; moved tests matched by package-qualified name) |
| 7 | **specaudit differential**: `go test -tags specaudit ./internal/specaudit/... -count=1` before/after | same-set (already red at HEAD; `wminv003` allowlists workloop.go only, keys nothing on the touched files; RT17 moves no file) |
| 8 | `go test ./internal/replay/... -count=1` | green (emit-ordering) |
| 9 | `go test ./internal/runexectest/... -count=10` | green |
| 10 | `ubs` on changed files | exit 0 |
| 11 | independent agent-reviewer | APPROVE |

**Per-CLASS flake confirmation before calling any NEW fail a regression** (all three directions — omitting the third mis-diagnoses runmerge):
- **load-sensitive** (`TestWorkLoop_ShutdownDrainsCommittedRun_hkdnrg`, `TestT6_10BeadSequentialDrain`, `TestScenario_ReviewLoop_ResumeSubmitReliable`, `TestWorkLoop_ClaimSemaphore_*`, …) → re-run **in isolation**;
- **isolation-sensitive** (`TestMergeToMain_RealConflict`, in-scope via runmerge) → confirm only in the **FULL suite** (the inverse of the load rule);
- **known-red-at-HEAD** (`TestThroughput_TenBeadsAtMaxFour`, `TestPasteInjectQuitOnCommit_*`, `TestPasteInjectCommitBudget_IdleActivePane_HKukx`) → A/B unmodified, never blocks.

**EXIT GATE (honest, §0.3):** `grep -oE 'deps\.launchSpecBuilder' internal/daemon/{reviewloop,dot_cascade,dot_gate}.go | wc -l` = **0/0/0** (was 2/1/1 code + comments); `deps.launchSpecBuilder = ` in workloop.go = **2** (preserved). `grep -c 'deps\.'` on the three files = **94/74/43 unchanged** — proof RT17 did not touch RT18's territory. The stale 0/0/0 / "falling further" target is explicitly rejected for this slice.

**Runtime proof DEFERRED — daemon down.** Flag it in the close comment.

## §6 — Merge-conflict exposure

workloop.go is the hottest file (~3.7 commits/day, ~331 commits/90d); reviewloop ~98, dot_cascade ~104, dot_gate ~21, runports.go moderate. **ONE atomic slice** — accessor + 4 reads + binding + 6 comments + gate + Makefile in a single commit. Do **not** split by file (a half-converted tree carries two builder-access idioms — strictly worse). **WAIT if the tree is dirty** rather than landing partial. The freeze gate holds the line for RT18. Window: minutes on a clean tree.

## §7 — Risks

1. **Scope contradiction / exit-gate misread.** PROGRESS vs CHUNK-CATALOGUE disagree on who deletes the assignments; the aggregate 94/74/43 does not move. *Mitigation:* §0.2 resolves ownership on mechanics; §0.3 restates the honest acceptance; **operator-ack is a Step-0 precondition**, not a risk note. The plan declares 94/74/43 flat up front so a reviewer does not read it as a miss.
2. **Scope creep into the assignment deletion / the four rebuild sites.** An implementer "finishes the job" by deleting 3933/3943 (routes every codex/pi bead to claude — the by-value `deps` copy is the only thing propagating routed/pinned selection) or by folding the per-node rebuilds. *Mitigation:* §1c; tooth B freezes the assignments at exactly 2; step 2 preserves every reassignment/fallback byte-identical; reviewer confirms the diff touches only the accessor + reads + binding + comments.
3. **Narrow-accessor nil trap (sharper than RT16's clock-nil).** `deps.runPorts().Launch` is structurally nil (per-run assembly); reaching the port via the bundle panics. *Mitigation:* accessor returns the FUNC field directly, never the bundle; tooth C pins the accessor; runPorts() is untouched.
4. **Port-vs-func nil-check regression.** Rebinding a read to `rp.Launch.BuildSpec` makes `<local> == nil` permanently false (value-receiver method value) and panics on a nil builder. *Mitigation:* the accessor returns the raw func (identical nil-comparability + reassignability), NOT the port; every `if <local> == nil` guard preserved.
5. **Freeze-gate false-negative (the gate-first design's defect).** A `:=`-only regex misses `= x`, arg-pass, `return`, direct-call read forms. *Mitigation:* tooth A uses the **bare field** `deps\.launchSpecBuilder` (all read forms, comments included) per the live emitter-gate model — which forces the comment rewording in step 4; plus tooth B (assignment) and tooth D (call-site) close the inline-resolution and abandoned-seam holes.
6. **Comment-trips-its-own-gate.** The bare-field gate counts the ~6 field-naming comments in the three subdriver files. *Mitigation:* step 4 rewords all six to reach EXACT 0; workloop/runports/reviewerharness carry theirs under CEILING budgets.
7. **Differential-oracle false regression** from load, disk < 10 GiB, mismatched scope, or unsorted `comm`. *Mitigation:* serialize, df check, clean detached worktree, identical scope incl. runmerge, `sort -u` before `comm -13`, per-class flake confirmation (all three directions).
8. **Abandon partway.** *Mitigation:* one atomic commit; alias-only boundaries; rollback is a single `git revert`.
9. **Downstream miscounts ride along.** dot_gate.go:270 is a FIFTH, uninventoried clock site (RT14-B); RT15's seven commits carry `Reviewed-By: self`. *Mitigation:* flag the five-clock-site count for RT18's planner **as a note only** (do not rewrite RT18's chunk definitions from an RT17 commit); RT17's reviewer gives RT15's seven commits their independent eyes.

## §8 — Rollback / Documents-to-update

**Rollback:** single `git revert <RT17 commit>` — the accessor, four reads, binding, comments, gate, and Makefile lines are one commit; the tree returns to a valid state.

**Documents to update** (in the RT17 commit or an immediately-following doc commit): `plans/2026-07-21-p2-extraction/PROGRESS.md` (record RT17 landed = read-seam bind; correct 94/74/43-flat framing; note the exit-gate re-attribution), a **correction NOTE** in `E5-CHUNK-CATALOGUE.md` / `E5-dot-runloop.md` flagging (a) the RT17-vs-RT18.11 ownership resolution and (b) the FIVE-site clock count for RT18 — **note only, do not rewrite RT18's chunk bodies**; `00-test-oracle-baseline.md` if any seam-test class shifted. `br sync --flush-only` at session end.