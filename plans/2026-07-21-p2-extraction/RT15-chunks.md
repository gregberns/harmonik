# RT15 — `RunEnv` / `SharedHandles` / `beadRunOne` re-signature, cut into 7 chunks

**Status: LANDED 2026-07-22, all seven chunks.** `f33af3a5` (C1) · `7f32b420` (C2) · `d04c0438` (C3) ·
`4a70d2be` (C4) · `e3d014a5` (C5) · `7794ab53` (C6) · `3ea214fb` (C7). `export_test.go` shows a zero
diff across the whole slice, as the exit gate requires. Outcome, corrections and the three things this
slice deliberately left to RT17/RT18 are in `PROGRESS.md` §"RT15". Everything below is the plan as
written; read §"Where this document was wrong" first.

## Where this document was wrong (measured while executing it)

1. **All line numbers are stale.** Fifth consecutive slice. Every symbol was re-located by `grep -n`.
   Counts held for C2/C4/C6/C7; C3's `projectDir` is **24 hits — 20 code + 4 comments**, and six
   further prose comments across C2/C3 name the converted fields and were never listed.
2. **§C6's `shared := deps.sharedHandles()` does not compile.** `shared` is the `harness/shared`
   package, referenced eleven times inside `beadRunOne` below the construction point. The local is
   named **`handles`**.
3. **C5 re-anchors three grandfathered complexity findings** (`funlen`, `gocognit` 398, `cyclop` 223)
   onto the rewritten declaration line under `--new-from-rev`. §"Gate" below says a 19-field `RunEnv`
   travels lint-clean, which is true and is not the issue — the issue is the signature line itself.
   They ship as one justified `//nolint:funlen,gocognit,cyclop`.
4. **C3 re-anchors an `errcheck` finding** on `_ = runpkg.Remove(...)` in the run-registry removal
   defer. Fixed, not suppressed: `ErrNotFound` stays silent, anything else reports to stderr in the
   shape `adoptDeadRunSessions` already uses. This is the slice's one logic delta.

---

**Original status (superseded):** plan only. Not executable until RT13 commits (RT13 currently has
`runports.go` and `workloop.go` dirty; RT15-C1 edits both).

## The one idea that makes this decomposition work

`env` is a **derived local**, not a passed parameter, for chunks 1–4. It is built right beside the
`rp := deps.runPorts()` that is already at `workloop.go:3184`, and it is a read-only projection of
`deps`. `deps` stays the single source of truth for the whole slice — no `workLoopDeps` field is
deleted, and none *can* be (5 of the 8 `RunEnv` fields have readers outside `beadRunOne`).

That is what retires the E5 plan's warning ("do not abandon partway through RT15–RT18: a
half-threaded `RunEnv` leaves the tree in a worse state than not starting"). The warning is about two
dependency-**passing** idioms coexisting across signatures. A derived local is not a second passing
idiom — it is an alias, exactly like the `rp := deps.runPorts()` and `mport := rp.Merge` that already
sit on lines 3184 and 3186 today. Every chunk boundary here is a state the tree can sit at for a year.

## What changed versus the E5 plan's RT15 paragraph

1. **Test blast radius is 7 call sites, not 156 files.** The 157 files that reference
   `ExportedWorkLoopDeps` / `WorkLoopDepsParams` drive `ExportedRunWorkLoop(ctx, deps)`, never
   `beadRunOne`. Because RT15 derives `env` from `deps` and deletes no field, the shim needs no
   compatibility `RunEnv` and those files need zero edits. **`export_test.go` must show a zero diff for
   the whole of RT15** — that is an exit gate, not a preference.
2. **`RunEnv.ProjectCfg` and `SharedHandles.RunRegistry` are not blockers.** Both bundles stay in
   `package daemon` for all seven chunks, so `ProjectCfg ProjectConfig` and `*RunRegistry` type-check
   with zero work. Those questions block THE LIFT, not RT15.
3. **`LaunchPort` no longer leaks E1b private types** — `BuildSpec` now names `shared.LaunchCtx` /
   `shared.LaunchArtifacts` (`runports.go:168`, commit c3fff27d). Red flag retired.
4. **`brPath` has zero readers inside `beadRunOne`.** `RunEnv.BrPath` is populated by the constructor
   but consumed only by `runbridge.go` — RT18 surface, not RT15 surface. No chunk owns it.
5. **`SharedHandles` gets no parameter in RT15.** See "Rejected" below.

## The trap

`workloop.go:3312` — `itemWorkflowRef = resolveWorkflowRef(beadRecord, itemWorkflowRef)`. This is the
**only** one of `beadRunOne`'s eleven item parameters that is reassigned inside the body (verified by
regex over 3176–5404: every other param scores 0). The resolved value is read at `:3951` and `:4131`.
Replacing those readers with `env.ItemWorkflowRef` silently drops EM-012a tier-0/tier-1 workflow_ref
resolution with a green build.

**Resolution: `ItemWorkflowRef` never goes on the env-read path.** It stays a local, aliased in C5's
preamble. Gate: `grep -c 'env\.ItemWorkflowRef' internal/daemon/workloop.go` must be exactly `1` (the
preamble line) after C5, and `0` before it.

## Do not touch

- The copy-mutation sites at `:3180` (`deps.clock = substrate.SystemClock{}`), `:3977` and `:3987`
  (`deps.launchSpecBuilder = …`). They sit inside RT15's edit region and are tempting. Deleting them
  needs LaunchPort/ClockPort widening that only exists after RT16. **RT16 owns them.** (Full run-path
  list, current line numbers: workloop.go 3180 / 3977 / 3987, dot_cascade.go 220 / 1260,
  reviewloop.go 230 — six, not the plan's four.)
- The 14-parameter dispatch closure at `:3124`. Its explicit parameter list is a loop-variable-capture
  guard. Build the `RunEnv` inside the goroutine body; do not collapse the closure.
- `deps.tidGen` (10 reads in `beadRunOne`). `SharedHandles` has no TIDGen field and adding one widens a
  bundle beyond its declaration. RSM-011 says it must stay shared by reference; that is RT18's call to
  make and justify. RT15 leaves `tidGen` on `deps`.
- `export_test.go:43-395`.

## Chunks

Order: reads first (C1–C4, zero risk), then the signature (C5, the actual deliverable that unblocks
RT18), then SharedHandles (C6–C7, a **detachable tail** — independent of C1–C5, deferrable
indefinitely if RT16 wants the file).

| # | What | Sites | Files | ~LOC |
|---|------|-------|-------|------|
| C1 | `runEnv()` constructor + `env :=` + tail group (`allowedRepos` 1, `workflowModeDefault` 1) | 2 | runports.go, workloop.go | 35 |
| C2 | `targetBranch` 3 + `protectBranches` 2 | 5 | workloop.go | 10 |
| C3 | `projectDir` | 20 | workloop.go | 40 |
| C4 | `defaultHarness` 2 + `projectCfg` 3 | 5 | workloop.go | 10 |
| C5 | **signature**: 11 params → `env RunEnv` + alias preamble | 7 call sites | workloop.go + 4 test files | 45 |
| C6 | `sharedHandles()` constructor + `localInFlight` 4, `agentSpawnSem` 3, `budgetPort` 1 | 8 | runports.go, workloop.go | 25 |
| C7 | `runRegistry` 6 + `workerRegistry` 8 via `shared` | 14 | workloop.go | 28 |

### C1 — constructor + tail group

`func (deps *workLoopDeps) runEnv(runID core.RunID, beadRecord core.BeadRecord, queueName string,
queueID *string, queueGroupIndex *int, queueItemIndex int, itemWorkflowMode, itemWorkflowRef string,
itemTemplateParams map[string]string, itemLocalOnly bool, itemWorkerTarget string) RunEnv` next to
`runPorts()` at `runports.go:336`. Populates **all 19 fields** — a partially-populated `RunEnv` is a
trap for whoever reads `env.RunID` next.

`env := deps.runEnv(runID, beadRecord, queueName, queueID, queueGroupIndex, queueItemIndex,
itemWorkflowMode, itemWorkflowRef, itemTemplateParams, itemLocalOnly, itemWorkerTarget)` on the line
after `:3184`. All 16 parameters are in scope there; no restructuring.

Then `:3302` → `env.WorkflowModeDefault`, `:3424` → `env.AllowedRepos`.

C1 cannot be smaller: an unused local is a **compile error** in Go, so the constructor cannot land
without at least one reader.

Note `env.ItemWorkflowRef` holds the value as passed — *before* `:3312`'s resolution. That is correct
and matches what C5's call-site construction will produce. Nothing may read it.

### C2 / C3 / C4 — mechanical read swaps

`:3457`, `:3482`, `:3509` → `env.TargetBranch`; `:3445`, `:3486` → `env.ProtectBranches`. (C2)

`:3288, 3417, 3423, 3425, 3436, 3446, 3485, 3645, 3745, 3818, 3880, 3881, 3952, 4126, 4131, 4289,
4431, 4454, 4518, 4538` → `env.ProjectDir`. (C3)

`:3346`, `:3982` → `env.DefaultHarness`; `:3352`, `:3366`, `:3393` → `env.ProjectCfg`. (C4)

Nothing else. If the diff names any file other than `workloop.go`, the chunk has scope-crept.

### C5 — the signature (one atom)

```go
func beadRunOne(ctx context.Context, deps workLoopDeps, env RunEnv, extraContext string,
	preSelectedWorker *workers.Worker, localSlotHeld bool) (succeeded bool)
```

Delete the `env := deps.runEnv(...)` line; add the alias preamble in its place:

```go
runID, beadRecord := env.RunID, env.BeadRecord
queueName, queueID := env.QueueName, env.QueueID
queueGroupIndex, queueItemIndex := env.QueueGroupIndex, env.QueueItemIndex
itemWorkflowMode, itemWorkflowRef := env.ItemWorkflowMode, env.ItemWorkflowRef
itemTemplateParams, itemLocalOnly := env.ItemTemplateParams, env.ItemLocalOnly
itemWorkerTarget := env.ItemWorkerTarget
```

The preamble is why this is a ~45-line diff and not a ~200-line one: the 79 `runID` reads, the 19
`beadRecord` reads, and the `:3312` reassignment are all **untouched**, so behavior preservation is
visually obvious. Inlining the aliases is a later, optional, purely cosmetic exercise nobody has to do.

Production call site: move `deps.runEnv(...)` into the goroutine body at `:3129`, immediately before
the call. Value-identical to constructing at `:3184` — the inputs are `deps` config fields (never
mutated in `beadRunOne`) and the 11 params (`itemWorkflowRef`'s reassignment is at `:3312`, 128 lines
*after* the old construction point).

Six test call sites (`hk3hozm_slot_leak_test.go:193`, `pi_provider_selected_hk8ziid2_test.go:173/245/306`,
`pi_unknown_profile_refuse_test.go:165`, `workloop_gate_n5md3_test.go:351`) each shrink from a 3-line
positional call to one line: `beadRunOne(ctx, deps, deps.runEnv(runID, beadRecord, "", nil, nil, 0,
"", "", nil, false, ""), "", preSelectedWorker, false)`. All six are in-package `package daemon`; no
`export_test.go` shim exists or is needed.

### C6 / C7 — SharedHandles (detachable tail)

`func (deps *workLoopDeps) sharedHandles() SharedHandles` in `runports.go`, populating all five fields.
`shared := deps.sharedHandles()` beside `env`. C6 converts `:3197, 3198, 3587, 3588` (`LocalInFlight`),
`:4671, 4673, 4681` (`AgentSpawnSem`), `:4084` (`Budget`). C7 converts `:3235, 3395, 3593, 4357, 4725,
5057` (`RunRegistry`) and `:3215, 3218, 3562, 3565, 3567, 3582, 3726, 3727` (`Workers`).

Splitting the reads across two chunks is safe *because* C6's constructor populates all five fields —
the struct is never partially built. Partial read conversion is not a hazard while `deps` remains the
source of truth.

All 22 reads are inside `beadRunOne` and nowhere else on the run path (`dot_cascade.go`,
`reviewloop.go`, `dot_gate.go`, `runbridge.go`, `sub_workflow_runner.go`: zero hits for all five
names). This is the most self-contained work in the stream.

## Rejected

- **A `shared SharedHandles` parameter in RT15.** Three of the five fields have readers outside
  `beadRunOne` (`runRegistry` also in diskcheck/eagerfill/wiringlog/bootworkloop; `workerRegistry` in
  bootworkloop; `localInFlight` likewise), so those `deps` fields cannot be deleted — which is enough
  to carry the decision. The other two do not: after C6/C7 the only production reference to
  `agentSpawnSem` or `budgetPort` is `sharedHandles()` itself. An earlier draft of this section said
  all five had outside readers; that was wrong for those two, and an independent review of the RT15
  chunks caught it. The conclusion is unaffected. Adding the parameter while `deps` is still passed is **pure duplication across a signature
  boundary** — the exact two-idiom hazard this decomposition exists to avoid — and it buys nothing:
  RT18 is editing those 7 call sites anyway when it drops `deps`. **RT18 owns the `shared` parameter.**
- **Splitting C5 into "add `env` alongside the 11 params" then "delete the 11".** Step 1 would leave
  every value reachable through two parameters at once, and doubles call-site churn in the hot file
  for zero interruption-safety gain (Go arity makes the single-step version compile-enforced anyway).
- **Converting param reads to `env.X` before C5** to shrink the alias preamble. Legal (env holds the
  same values from C1 onward) but it puts churn in the hot file to save a cosmetic 11 lines, and for
  `itemWorkflowRef` it is the silent-regression trap.

## Gate

RT15 creates no package. No depguard block (so the plan's "the `internal/runloop` block must allow
policy/workflow/workflow-dot/brcli" does not bite here), no freeze-gate script, no `wminv003` path
allowlist, no `go list -deps` boundary test. `.golangci.yml:86` disables gocritic `hugeParam` /
`rangeValCopy`, so a 19-field `RunEnv` and a 5-field `SharedHandles` travel by value lint-clean with no
`nolint`.

Fast inner loop (seconds): `go build ./internal/... ./cmd/... && go vet ./internal/...`

Per-chunk gate, serialized — **never run the full suite concurrently with agent fan-out**:

```bash
go build ./internal/... ./cmd/... &&
go vet ./internal/... && go vet -tags=scenario ./internal/daemon/ &&
golangci-lint run &&
go test ./internal/runexectest/... -count=10 &&
go test ./internal/replay/... -count=1 &&
go test ./internal/daemon/... ./internal/harness/... -count=1 -timeout 25m 2>&1 \
  | grep -E '^--- FAIL' | sort -u > after-failures.txt
comm -13 before-failures.txt after-failures.txt   # must be EMPTY
```

`go vet -tags=scenario` is not optional: 28 scenario-tagged files, 12 of which drive the run-loop
exports, never compile under plain `go test`. `runexectest -count=10` is the real behavior gate.

Scope assertions, per chunk:

```bash
# C2,C3,C4,C7 — nothing but the one hot file
test -z "$(git diff --name-only HEAD~1 -- . ':!internal/daemon/workloop.go')"
# C1,C6 — nothing but the hot file and the cold one
test -z "$(git diff --name-only HEAD~1 -- . ':!internal/daemon/workloop.go' ':!internal/daemon/runports.go')"
# every chunk — the 157-file shim is frozen
test -z "$(git diff --name-only HEAD~1 -- internal/daemon/export_test.go)"
# C1..C4 — the trap stays shut
test "$(grep -c 'env\.ItemWorkflowRef' internal/daemon/workloop.go)" = 0
# C5 — exactly one reader, the alias line
test "$(grep -c 'env\.ItemWorkflowRef' internal/daemon/workloop.go)" = 1
```

## Conflict window

`workloop.go` is 331 commits / 90d ≈ one commit every ~6.5 wall-clock hours. `runports.go` is **6 /
90d** — effectively cold, which is why C1 and C6 put their constructors there and only the small
`workloop.go` half sits in the contested zone.

Editing time is minutes; the gate is the long pole (~40–60 min serialized). Hold `workloop.go` for
roughly one gate per chunk and commit immediately after. C2/C3/C4/C7 are pure single-token
substitutions in one function, so an upstream commit landing mid-gate rebases trivially unless it
touched the same lines.

**Hard serialization:** RT13 has `runports.go` and `workloop.go` dirty right now (`MergePort.Submit`
returns `runmerge.Submit`). RT15-C1 edits both. RT15 cannot start until RT13 commits.
