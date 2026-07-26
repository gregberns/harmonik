# RT16 — EmitterPort conversion: drain the `deps.bus` bypass off the run path

**Status:** READY (after its two dependencies land — see below). No operator decision needed.
**Depends on:** **RT13** (merge-path carve-out → `internal/runmerge`) and **E4c**
(`buildWorkerRegistry` → `internal/workers/bootwire.go`) must both be **committed and building**.
Neither is a logical dependency — RT16 touches no line either of them touches — both are
*write-lock* dependencies: all three edit `internal/daemon/workloop.go`, which is single-writer.
**Size:** ~113 lines changed across **6** Go files (108 call sites + 5 new local declarations),
plus 2 comment rewrites, 1 new gate script (~40 lines), 2 Makefile lines. **No file moves. No new
package. No `.golangci.yml` change. Zero symbols exported.**
**Risk:** **LOW** — `EmitterPort` is a Go *type alias* (`internal/daemon/runports.go:40`), so
`deps.bus` and `rp.Emitter` are the **same static type** holding the **same interface value**; the
conversion cannot change a method set, an interface conversion, a nil check, or an emission string.

---

## 0. Corrections to `E5-dot-runloop.md` (verified against the tree 2026-07-22, post-RT13)

The E5 plan's §3a `deps.bus` row was written **before** RT13 was applied to the tree. Re-measured:

| E5 §3a claim | Measured now | Verdict |
|---|---|---|
| "115 direct reads (workloop ×34, reviewloop ×51, dot_cascade ×23, dot_gate ×7)" | Line counts are **exactly right and RT13 did not change them** — identical 34/51/23/7 at `git show HEAD:` and in the working tree. RT13's cut region (HEAD lines 6397–8011) contained **zero** `deps.bus` sites, because the merge path already took an `EventEmitter` parameter. | **KEPT** |
| "115 reads" | **113 are code; 2 are comments** — `dot_cascade.go:1829` and `:1837` mention `deps.bus` in prose only. | **CORRECTED** |
| "`EmitterPort` reached only at `runports.go:337` and `workloop.go:7762`" | `runports.go:337`→**`:339`** (`Emitter: deps.emitterPort()`); `workloop.go:7762`→**`:6406`** (`emitBeadClosed(ctx, deps.runPorts().Emitter, …)` inside `emitBeadClosedAndMaybeEpic`, an RT13 stay-behind). The plan also missed a **second** in-tree port-access precedent: `dot_gate.go:105` already reads `deps.runPorts().Gate`. | **CORRECTED** |
| (not stated) | **Five more run-path `.bus` reads live on files E5 §1b marks as MOVERS** and were never counted: `runbridge.go:134/:279/:322` (`b.deps.bus`) and `sub_workflow_runner.go:290/:317` (`r.deps.bus`). RT16 takes them — see §1. | **ADDED** |
| "copy-mutation at `dot_cascade.go:215`/`:1254`, `reviewloop.go:222`" | Now `dot_cascade.go:219`/`:1259`, `reviewloop.go:229`, `workloop.go:3179`. The `launchSpecBuilder` smuggle at "`:3967-3979`" is now `:3975-3989`, resolved into `rp.Launch` at **`:3994`**. | **CORRECTED (RT18's problem, not RT16's)** |

**RT16 does not touch any of the copy-mutation sites.** They are RT18's. RT16 reads exactly one
field — `.bus` — which is **never assigned anywhere in the module** (the only `.bus =` in the tree is
`bootstate.go:119`, a different struct). That is what makes this slice behaviour-free.

---

## 1. What changes

`EmitterPort` is already the port. The run path bypasses it by reading `deps.bus` directly. This
slice binds the port **once per function** and points every site at it, so RT18's signature flip
(`deps workLoopDeps` → `(env, ports, shared)`) becomes an **8-line** change instead of a 108-line one.

### 1a. The uniform rule

> **≥2 sites in one function body → declare a local `emit`. Exactly 1 site → use the port expression
> inline.** Nothing else changes on the line.

| Form used | Where | Why |
|---|---|---|
| `emit := rp.Emitter` | `beadRunOne` only | `rp := deps.runPorts()` **already exists** at `workloop.go:3184`; this adds **zero** new `deps.` reads. |
| `emit := deps.emitterPort()` | the other 4 multi-site functions | Narrowest possible binding: reads one field, cannot interact with the `deps.clock` mutation two lines above it. |
| `b.rp.Emitter` inline | `runbridge.go` (3 methods, 1 site each) | `runBridge` **already carries** `rp RunPorts` (`runbridge.go:33`). Strictly removes 3 `b.deps.` reads. |
| `deps.emitterPort()` / `r.deps.emitterPort()` inline | `dispatchDotGateNode`, `dotSubWorkflowRunner.Run`, `dispatchSubWorkflowExpandedNode` | One site each; a single-use local is noise. |

### 1b. Files and sites

| File | Lines | What | Why |
|---|---|---|---|
| `internal/daemon/workloop.go` | **24 sites** in `beadRunOne` (`:3175`): `3261 3302 3353 3366 3398 3659 3710 3725 3920 3983 4653 4657 4700 4707 4719 4777 4779 4944 4993 5038 5074 5106 5160 5254` | `deps.bus` → `emit`; add `emit := rp.Emitter` at `:3187` (immediately after `mport := rp.Merge`) | `beadRunOne` is E5's biggest mover (`workloop.go` 3175–5397 → `runloop.Run`). These 24 are the run's own emissions. |
| `internal/daemon/workloop.go` | **10 sites LEFT ALONE**: `1883 1893 1949 2001 2013 2203 2425` (`runWorkLoop`, `:1555`), `6112` (`activateFirstPendingGroup`), `6355` (`evaluateGroupAdvanceWithOutcome`), `6477` (`maybeEmitEpicCompleted`) | **no edit** | These are the **outer queue-claim loop**, which `E5-dot-runloop.md` §1c says stays in `internal/daemon` forever. Converting them is churn on the hottest file in the tree for zero extraction value. Same precedent E5 §3a sets for `deps.brAdapter` ("only the ~9 sites inside `beadRunOne` convert"). |
| `internal/daemon/reviewloop.go` | **51 sites**, all in `runReviewLoop` (`:185`): `248 318 458 528 539 619 625 634 704 722 750 764 782 808 825 829 880 915 951 1013 1042 1048 1064 1072 1141 1147 1155 1178 1195 1267 1276 1328 1336 1340 1392 1398 1405 1437 1492 1504 1565 1581 1585 1590 1616 1662 1672 1693 1698 1706 1759` | `deps.bus` → `emit`; add `emit := deps.emitterPort()` at `:232` (after the `if deps.clock == nil` block at `:229-231`) | Whole file is a mover (`→ internal/runloop`). 44 of the 51 are the *identical* line `emitReviewLoopCycleComplete(ctx, deps.bus, runID, state.iterationCount, result.completionReason)`. |
| `internal/daemon/reviewloop.go` | `:458` | `capturedBus := deps.bus` → `capturedBus := emit` | **Type-inference site.** `EmitterPort` is an alias, so the inferred type is byte-identical (`handlercontract.EventEmitter`). Call it out for the reviewer. |
| `internal/daemon/dot_cascade.go` | **6 sites** in `driveDotWorkflow` (`:189`): `432 464 738 746 890 1091` | `deps.bus` → `emit`; add `emit := deps.emitterPort()` at `:222` (after the clock block at `:219-221`) | Mover. |
| `internal/daemon/dot_cascade.go` | **15 sites** in `dispatchDotAgenticNode` (`:1225`): `1450 1641 1659 1670 1721 1759 1809 1812 1821 1840 1880 1940 1987 1998 2035` | same; add `emit := deps.emitterPort()` at `:1262` (after the clock block at `:1259-1261`) | Mover. |
| `internal/daemon/dot_cascade.go` | `:1840` | `hbTarget := deps.bus` → `hbTarget := emit`; the next statement `hbTarget = tap` must still compile | **Second type-inference site.** Alias ⇒ same static type ⇒ `tap` still assigns. Verify by compile, not by eye. |
| `internal/daemon/dot_cascade.go` | `:1829`, `:1837` (**comments**) | reword `deps.bus` → `the run emitter` in the prose, preserving meaning | Required so the §5 exit gate can assert **0**, and because the comment would otherwise name a symbol the code no longer uses. |
| `internal/daemon/dot_gate.go` | **6 sites** in `executeCognitionGate` (`:235`): `356 406 415 428 493 499` | `deps.bus` → `emit`; add `emit := deps.emitterPort()` as the function's first statement (`:258`) | Mover. No clock block in this function. |
| `internal/daemon/dot_gate.go` | **1 site** in `dispatchDotGateNode` (`:74`): `134` | `deps.bus` → `deps.emitterPort()` inline | Single site; matches the existing `deps.runPorts().Gate` idiom already on `:105`. |
| `internal/daemon/runbridge.go` | **3 sites**: `134 279 322` | `b.deps.bus` → `b.rp.Emitter` | `runBridge.rp RunPorts` already exists (`:33`, set at `:79`). Free removal of 3 `b.deps.` reads on a mover. **Missing from E5 §3a's census.** |
| `internal/daemon/sub_workflow_runner.go` | **2 sites**: `290` (`(*dotSubWorkflowRunner).Run`, `:161`), `317` (`dispatchSubWorkflowExpandedNode`, `:307`) | `r.deps.bus` → `r.deps.emitterPort()` | Mover (`→ runloop/dotrun`). Receiver is `*dotSubWorkflowRunner` at both sites, so the pointer-receiver `emitterPort()` resolves. **Missing from E5 §3a's census.** |

**Totals:** 108 call sites converted, 5 local declarations added, 2 comments reworded, 10 sites
deliberately left in the outer loop. `deps.emitterPort()` goes from 1 caller (itself, inside
`runPorts()`) to 6.

### 1c. Explicitly NOT in this slice

- **`deps.clock`, `deps.brAdapter`, `deps.substrate`, `deps.hookStore`, `deps.harnessRegistry`,
  `deps.launchSpecBuilder`.** One port per slice. `clock` carries the copy-mutation idiom RT18 must
  delete; `brAdapter` has a real outer/inner split; the rest need `LaunchPort` *widened* (RT16-Launch
  / RT17 in E5 §4 step 19). Bundling any of them forfeits this slice's "zero behaviour risk" claim.
- **The 10 outer-loop `workloop.go` sites** (above).
- **`workloop.go:6477`** (`maybeEmitEpicCompleted`) even though its sibling at `:6406` already uses
  `deps.runPorts().Emitter`. The inconsistency is real and one line wide, but both functions **stay
  in daemon** (RT13 stay-behinds), so fixing it here would be a "while I'm here" edit that weakens
  the pure-mechanical diff. Hand it to RT19b.
- **`workloop_handlerpause_kac8g.go:47` (`if deps.bus == nil`), `diskcheck_hksxlb.go` ×3,
  `eagerfill_em063.go` ×2, `runports.go` ×3.** Non-movers or the port definition itself.
- **The two test-file comments** that mention `deps.bus`
  (`pasteinject_hkemuic_test.go:14`, `pasteinject_hksj6a_test.go:14`). They document a historical
  fix; leave them. The §5 gate is scoped to production files so they cannot trip it.

---

## 2. The seam it exits behind

**Pre-existing. Nothing new is invented. No escalation needed.**

| Seam element | Location | Note |
|---|---|---|
| `type EmitterPort = handlercontract.EventEmitter` | `internal/daemon/runports.go:40` | **A type ALIAS (`=`), not a defined type.** This is the whole safety argument: `deps.bus` (declared `handlercontract.EventEmitter` at `workloop.go:202`) and `rp.Emitter` are the *same type*, so no conversion, no wrapper, no method-set change, no nil-semantics change is possible. |
| `func (deps *workLoopDeps) emitterPort() EmitterPort { return deps.bus }` | `internal/daemon/runports.go:84` | Identity accessor, already written by RT4. |
| `RunPorts.Emitter EmitterPort` | `internal/daemon/runports.go:284` | Bundle field. |
| `func (deps *workLoopDeps) runPorts() RunPorts` | `internal/daemon/runports.go:336`, wires `Emitter: deps.emitterPort()` at `:339` | Already constructed once per run in `beadRunOne` at `workloop.go:3184`. |
| `runBridge.rp RunPorts` | `internal/daemon/runbridge.go:33`, assigned `:79` | Already threaded; `b.rp.Ledger` is used at `:116` and `:350`. |

In-tree precedent for exactly this move, already landed: `beadRunOne` reaches the merge port via
`mport := rp.Merge` (`workloop.go:3186`); `dispatchDotGateNode` reaches the gate port via
`deps.runPorts().Gate` (`dot_gate.go:105`); `emitBeadClosedAndMaybeEpic` already reaches the emitter
via `deps.runPorts().Emitter` (`workloop.go:6406`). RT16 finishes what those three started.

**No `.golangci.yml` change.** This slice creates no package, so there is no depguard block to add
and no allow-list to widen. Section 4 step 8 adds a **ratchet script only** — see §4.

---

## 3. Coupling to break

### 3a. Outbound (run path → daemon internals)

| Coupling | Before | After |
|---|---|---|
| `beadRunOne` → `workLoopDeps.bus` | 24 direct field reads | **1** (`emit := rp.Emitter`, itself a port read — 0 `deps.` reads added) |
| `runReviewLoop` → `workLoopDeps.bus` | 51 | **1** (`emit := deps.emitterPort()`) |
| `driveDotWorkflow` → `workLoopDeps.bus` | 6 | **1** |
| `dispatchDotAgenticNode` → `workLoopDeps.bus` | 15 | **1** |
| `executeCognitionGate` → `workLoopDeps.bus` | 6 | **1** |
| `dispatchDotGateNode` → `workLoopDeps.bus` | 1 | **1** (inline `deps.emitterPort()`) |
| `runBridge` → `b.deps.bus` | 3 | **0** (`b.rp.Emitter`) |
| `dotSubWorkflowRunner` → `r.deps.bus` | 2 | **2** (inline `r.deps.emitterPort()`; struct still holds `deps`, RT18's problem) |
| **Total mover-side `.bus` field reads** | **108** | **8** |

`grep -c 'deps\.' internal/daemon/{reviewloop,dot_cascade,dot_gate}.go` — RT17's declared exit gate —
moves from **137 / 88 / 41** (E5 §4 step 19) to **86 / 67 / 34**. RT16 alone retires ~37% of
`reviewloop.go`'s total `deps.` surface. That is the largest single-slice reduction available in the
RT stream, and it is the only one with no semantic content at all.

### 3b. Inbound (daemon → this slice)

**None.** Nothing moves packages, so nothing needs exporting.

### 3c. Total export count

**0.** No symbol is renamed, exported, or given a new signature. `export_test.go` (229 commits/90d —
the second-hottest file in the tree) is **not touched**, which is the single biggest reason this
slice is cheap where RT15 is expensive.

---

## 4. Step-by-step recipe

Run everything from `/Users/gb/github/harmonik` with absolute paths. **Never `cd` into a worktree.**

### Preconditions (do not start until all four hold)

```bash
cd /Users/gb/github/harmonik
git log --oneline -3                       # RT13 and E4c must both appear as landed commits
git status --porcelain -- internal/daemon  # MUST be empty — no other writer mid-edit
go build ./internal/... ./cmd/...          # MUST exit 0 (NOT ./... — see §5 trap 1)
df -h /Users/gb | tail -1                  # MUST show ≥10 GiB free (§5 trap 3)
```

If `git status -- internal/daemon` is dirty, **wait**. Do not split the slice to work around it
(§6 explains why splitting makes the exposure worse, not better).

**1. Capture the differential baseline at the parent commit, in the exact scope §5 will re-run.**

```bash
cd /Users/gb/github/harmonik
git rev-parse HEAD > /tmp/rt16-parent.txt
go test ./internal/daemon/... ./internal/harness/... ./internal/runmerge/... \
    -count=1 -timeout 25m 2>&1 \
  | grep -E '^--- FAIL' | sort -u > /tmp/rt16-before-failures.txt
wc -l /tmp/rt16-before-failures.txt
```

`./internal/runmerge/...` is in the scope **because RT13 put it there**. A baseline captured before
RT13 landed is invalid for this slice — `00-test-oracle-baseline.md` §"Lesson for the differential
oracle" records that mismatched package scope is what invalidated the last differential run.

**2. Record the pre-edit site counts, so the exit gate has a before-number.**

```bash
cd /Users/gb/github/harmonik
for f in workloop reviewloop dot_cascade dot_gate runbridge sub_workflow_runner; do
  printf "%-22s %s\n" "$f.go" "$(grep -c 'deps\.bus' internal/daemon/$f.go)"
done | tee /tmp/rt16-before-counts.txt
# expect: workloop 34 · reviewloop 51 · dot_cascade 23 · dot_gate 7 · runbridge 3 · sub_workflow_runner 2
```

**3. `workloop.go` — `beadRunOne` only.** Insert one line after `mport := rp.Merge` (`:3186`):

```go
	// RSM-010: the run's EmitterPort (identity over deps.bus, runports.go:40 —
	// EmitterPort is a type ALIAS, so this is the same value and the same type).
	emit := rp.Emitter
```

Then rewrite `deps.bus` → `emit` on **exactly** these 24 lines and no others:
`3261 3302 3353 3366 3398 3659 3710 3725 3920 3983 4653 4657 4700 4707 4719 4777 4779 4944 4993 5038 5074 5106 5160 5254`.
Line numbers shift by +3 after the insertion; work **bottom-up** (highest line first) so earlier
numbers stay valid.

**Do not touch** `1883 1893 1949 2001 2013 2203 2425 6112 6355 6477`.

**4. `reviewloop.go`.** Insert after the clock block that ends at `:231`:

```go
	// RSM-010: the run's EmitterPort (identity over deps.bus; runports.go:84).
	emit := deps.emitterPort()
```

Rewrite all 51 `deps.bus` → `emit`, bottom-up. Line `:458` becomes `capturedBus := emit`.

**5. `dot_cascade.go`.** Two insertions and 21 rewrites, bottom-up:

- `dispatchDotAgenticNode`: insert `emit := deps.emitterPort()` (same comment) after the clock block
  ending at `:1261`; rewrite the 15 sites `1450 1641 1659 1670 1721 1759 1809 1812 1821 1840 1880 1940 1987 1998 2035`.
  `:1840` becomes `hbTarget := emit`.
- Reword the two comments at `:1829` and `:1837`: `deps.bus` → `the run emitter`. Change no other
  word — the hk-sj6a / hk-e7n76 rationale must survive verbatim.
- `driveDotWorkflow`: insert after the clock block ending at `:221`; rewrite `432 464 738 746 890 1091`.

**6. `dot_gate.go`.**

- `executeCognitionGate` (`:235`): insert `emit := deps.emitterPort()` as the first statement of the
  body (before the `verdictPath := …` line at `:258`); rewrite `356 406 415 428 493 499`.
- `dispatchDotGateNode` (`:74`): rewrite `:134` `deps.bus` → `deps.emitterPort()` inline. No local.

**7. `runbridge.go` and `sub_workflow_runner.go`.**

- `runbridge.go` `:134 :279 :322`: `b.deps.bus` → `b.rp.Emitter`. No local, no new field.
- `sub_workflow_runner.go` `:290 :317`: `r.deps.bus` → `r.deps.emitterPort()`. No local, no new field.

**8. Add the ratchet.** `workloop.go` takes ~3.7 commits/day; without a gate the count silently
regrows before RT17/RT18 land. Per the settled decision (`README.md` §2 row 2) the tripwire is a
**hard CI failure**. Write `/Users/gb/github/harmonik/scripts/runloop-emitter-gate.sh`, modelled on
`scripts/transport-freeze-gate.sh`:

```bash
#!/usr/bin/env bash
# runloop-emitter-gate.sh — P2 E5 RT16 ratchet.
#
# The DOT run path reaches its event bus through EmitterPort (runports.go:40), not
# through the workLoopDeps.bus field. RT16 converted 108 direct field reads to 8
# port reads so RT18 can flip beadRunOne / runReviewLoop / driveDotWorkflow /
# dispatchDotAgenticNode to (RunEnv, RunPorts, SharedHandles) in 8 lines instead of
# 108. A new deps.bus read on a MOVER file un-does that and is a hard failure.
#
# workloop.go keeps EXACTLY 10 reads on purpose: runWorkLoop (the outer
# queue-claim loop) x7, activateFirstPendingGroup, evaluateGroupAdvanceWithOutcome,
# maybeEmitEpicCompleted. Those stay in internal/daemon forever
# (E5-dot-runloop.md §1c), so they are budgeted, not forbidden.
#
# Rationale: plans/2026-07-21-p2-extraction/RT16-emitterport-conversion.md
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

fail=0
check() { # file expected
  local f="internal/daemon/$1" want="$2" got
  got=$(grep -c 'deps\.bus' "$f" || true)
  if [ "$got" -gt "$want" ]; then
    echo "RT16 RATCHET VIOLATION: $f has $got 'deps.bus' reads, budget is $want." >&2
    grep -n 'deps\.bus' "$f" >&2
    fail=1
  fi
}
check workloop.go            10
check reviewloop.go           0
check dot_cascade.go          0
check dot_gate.go             0
check runbridge.go            0
check sub_workflow_runner.go  0

if [ "$fail" -ne 0 ]; then
  echo "" >&2
  echo "The run path reaches the bus through EmitterPort. In a function that already" >&2
  echo "binds it, use 'emit'. Otherwise use deps.emitterPort() / rp.Emitter /" >&2
  echo "b.rp.Emitter. If this read genuinely belongs to the OUTER loop (runWorkLoop)," >&2
  echo "raise workloop.go's budget in this script and say why in the commit body." >&2
  exit 1
fi
echo "runloop-emitter-gate: OK"
```

```bash
chmod +x /Users/gb/github/harmonik/scripts/runloop-emitter-gate.sh
```

Wire it into the Makefile exactly as the six existing gates are wired — one `scripts/…` line in
`check-fast` (after `Makefile:494`, alongside `harnesspi-freeze-gate.sh`) and the matching line in
`check-short`, plus a `.PHONY` target near `Makefile:300` for parity with the others.

**9. Format, then prove the diff is mechanical** (this is the pure-move review of §5.1, done by
machine rather than by eye):

```bash
cd /Users/gb/github/harmonik
gofumpt -l internal/daemon/                # must print nothing
git diff -U0 -- internal/daemon \
  | grep -E '^[-+]' | grep -vE '^(\+\+\+|---)' > /tmp/rt16-diff-lines.txt
# Normalise the six known substitutions on BOTH sides; every -/+ pair must collapse
# to an identical string. Anything that does not is either a declaration line
# (expect exactly 5) or a comment reword (expect exactly 2) — or a bug.
sed -e 's/\bemit\b/deps.bus/g' \
    -e 's/rp\.Emitter/deps.bus/g'   -e 's/b\.rp\.Emitter/b.deps.bus/g' \
    -e 's/deps\.emitterPort()/deps.bus/g' -e 's/r\.deps\.emitterPort()/r.deps.bus/g' \
    /tmp/rt16-diff-lines.txt | sed 's/^[-+]//' | sort | uniq -c \
  | awk '$1 % 2 == 1' > /tmp/rt16-unpaired.txt
cat /tmp/rt16-unpaired.txt   # expect ONLY the 5 declarations + 2 comment rewrites
```

**10.** Run the full §5 gate. Commit as one bead, one release. Do not leave the tree half-converted
overnight (`README.md` §3: "never leave a half-renamed tree overnight").

---

## 5. Verification gate

Run in this order from `/Users/gb/github/harmonik`. **Serialize the test runs** — no agent fan-out,
no parallel build (`00-test-oracle-baseline.md`: that is what manufactured five of the six baseline
failures).

| # | Command | Pass criterion |
|---|---|---|
| 1 | `go build ./internal/... ./cmd/...` | exit 0. **Scoped deliberately.** `go build ./...` is RED at the repo root regardless of this slice — `plans/2026-07-15-agent-substrate-v2/investigate/10-zeromq-experiments/*.go` import unvendored `zmq4`/`mangos`. Never make an unscoped root build a gate. |
| 2 | `go vet ./internal/... ./cmd/...` | exit 0 |
| 3 | `go vet -tags=scenario ./internal/daemon/` <br> `go vet -tags=e2e_real_claude ./internal/daemon/` <br> `go vet -tags=integration ./internal/daemon/` | exit 0 each. **Required.** 28 daemon test files sit behind `//go:build scenario` and 12 of them drive the run-loop exports; plain `go test ./...` never compiles them. This slice changes lines inside functions those files exercise. |
| 4 | `make specaudit-lint` | **Differential**, not zero — `specaudit` is already red at HEAD with 7 top-level failures (`00-test-oracle-baseline.md` addendum §1). The one to watch is `TestWMINV003PartBCorpusCheck`: `wminv003_task_branch_append_only_test.go` allowlists `internal/daemon/workloop.go` **by literal path** and scans for `git rebase` / `--amend` execs. RT16 adds no exec and moves no file, so it must be **unchanged** — a change here means the edit was not mechanical. |
| 5 | `.tools/golangci-lint run --new-from-rev=HEAD` | exit 0. **Use `--new-from-rev`, NOT the full run `README.md` §4 prescribes.** That instruction is for slices that *move files* (100% new lines). Here a full `golangci-lint run ./internal/daemon/...` would light up all **104** functions grandfathered past the `funlen: 100` ceiling (`.golangci.yml:88-90` says so explicitly) and tell you nothing. The changed lines here are all inside already-grandfathered giants; the linter reports funlen/cyclop/gocognit at the *declaration* line, which this slice does not touch. |
| 6 | `bash scripts/runloop-emitter-gate.sh` | prints `runloop-emitter-gate: OK` |
| 7 | `go test ./internal/replay/... -count=1` | exit 0. **The sharpest oracle for this slice.** The RT10 run-keyed replay checkers detect event-stream divergence — a reordered, renamed, or dropped emission. RT16 touches nothing *but* emission plumbing, so this is the gate that would actually catch a mistake. |
| 8 | `go test ./internal/runexectest/... -count=10` | exit 0. RT11 fault-matrix + N=10 relaunch oracle. `-count=1` is not sufficient. |
| 9 | `go test ./internal/daemon/... ./internal/harness/... ./internal/runmerge/... -count=1 -timeout 25m` | **Differential green.** Same three-package scope as step 1 of §4 — mismatched scope invalidates the comparison (`PROGRESS.md` §"Phase 5 status"). |
| 10 | `comm -13 /tmp/rt16-before-failures.txt /tmp/rt16-after-failures.txt` | **MUST be empty.** The gate is "**no NEW failure names**", never zero — `internal/daemon` is already red at HEAD. |
| 11 | `make test-scenario` | differential, same rule |
| 12 | `make fmt-check` | exit 0. **Known environmental trap:** this currently fails on 4–5-day-abandoned `.claude/worktrees/agent-*` checkouts inside the repo (`PROGRESS.md` §5b). Confirm any failure names a path under `internal/daemon`, not a stale worktree, before treating it as yours. |
| 13 | `ubs $(git diff --name-only HEAD \| grep '\.go$')` | exit 0 |
| 14 | `make agent-review` | APPROVE required to commit |

### Traps this gate is built around (from `00-test-oracle-baseline.md`)

1. **Root build is red.** Never `go build ./...` / `go test ./...` as a gate — scope to
   `./internal/... ./cmd/...`.
2. **Identical package scope, before and after.** Steps §4.1 and §5.9 use the *same* three-package
   list. `./internal/runmerge/...` must be in both (RT13 created it). Testing 72 packages vs. 1
   changes the disk-churn profile enough to change the failure set on its own.
3. **The disk watermark fakes failures.** Below 10 GiB free the daemon logs
   `disk-check: available=… watermark=10240MiB — dispatch paused` and every dispatch-dependent test
   times out. Check `df -h` before *and* after the run. (Measured 2026-07-22: 22 GiB free — currently
   above the line, but another session's scratchpads have crossed it twice this week.)
4. **Run against a clean detached worktree at your own HEAD, never the shared tree.** A second agent
   holds ~60 dirty files right now; `go test` compiles theirs too.
5. **Tagged tiers are invisible to `go test`.** Steps 3, 4 and 11 exist for that reason.
6. **The known-flaky allowlist has two opposite confirmation procedures.**
   `TestThroughput_TenBeadsAtMaxFour` (hard, pre-existing) and the five load-sensitive flakes are
   confirmed by re-running **in isolation** (isolated pass ⇒ load artifact). But
   `TestMergeToMain_RealConflictWithBeadsLedger_Escalates` is **isolation-sensitive** and must be
   re-run **in the full suite**. Applying the wrong procedure inverts the answer.
7. **`internal/harness/codex`'s three `LaunchSpec` tests are non-deterministic under parallel load.**
   Expect them in the after-set occasionally; they are on the allowlist.

### Measurement for the close comment

This slice **moves nothing**, so the daemon file/LOC count is unchanged — say so explicitly rather
than reporting a shrink of 0 as if it were a miss. Report instead:

```bash
cd /Users/gb/github/harmonik
for f in workloop reviewloop dot_cascade dot_gate runbridge sub_workflow_runner; do
  printf "%-22s deps.bus=%-3s deps.=%s\n" "$f.go" \
    "$(grep -c 'deps\.bus' internal/daemon/$f.go)" \
    "$(grep -c 'deps\.'    internal/daemon/$f.go)"
done
```

Expected after: `workloop 10`, `reviewloop 0`, `dot_cascade 0`, `dot_gate 0`, `runbridge 0`,
`sub_workflow_runner 0`. And `deps.` totals in the three RT17-gated files falling from
**137 / 88 / 41** to roughly **86 / 67 / 34**.

The close comment **must also state** that the `_plan.md` §5.4 runtime proof is **DEFERRED because
the daemon is down** — `E5-dot-runloop.md` §7.11 requires every slice to flag this rather than omit
it. The mechanical substitutes actually run are §5 steps 7 and 8.

---

## 6. Merge-conflict exposure — and why this is ONE slice, not four

### The churn (measured 2026-07-22, 90-day commit counts)

| File | commits/90d | ≈/day |
|---|---:|---:|
| `workloop.go` | **331** | 3.7 |
| `dot_cascade.go` | 104 | 1.2 |
| `reviewloop.go` | 98 | 1.1 |
| `dot_gate.go` | 21 | 0.23 |
| `sub_workflow_runner.go` | 10 | 0.11 |
| `runbridge.go` | 3 | 0.03 |
| **total across the six** | **567** | **6.3** |
| *(`export_test.go`, for scale — NOT touched by this slice)* | *229* | *2.5* |

### Window

Mechanical edit ~60–90 min (108 sites, bottom-up, no judgement calls) + fast gate ~5 min +
differential suite ~25–35 min (`internal/daemon` alone is ~605 s) + scenario tier ~10 min.
**≈2–2.5 hours end to end.** Land it in one sitting, same day.

### Recommendation: **ONE atomic slice.** Do not split by file.

1. **Splitting multiplies the window, it does not shrink it.** The edit is ~90 minutes; the *gate* is
   the cost, and every sub-slice pays the full gate (differential suite + scenario tier + replay +
   `runexectest -count=10`). Four slices ≈ 4 × ~45 min of serialized verification for an identical
   end state — roughly **three extra hours of exposure** to buy nothing.
2. **The write-lock is exclusive either way.** `PROGRESS.md` §3 declares `internal/daemon/**` held by
   the P2 stream, and `E5-dot-runloop.md` §7 risk 6 declares these RT slices strictly sequential
   single-writer. There is no concurrent writer for a smaller window to dodge. The 6.3 commits/day
   figure is the *historical* rate, not the rate during an exclusive hold.
3. **Splitting manufactures the exact failure mode §8.4 warns about.** `E5-dot-runloop.md` §8.4:
   "a half-threaded `RunEnv` leaves the tree with two parallel dependency-passing idioms, which is
   strictly worse than the single ugly one it has today." A tree where `reviewloop.go` reads `emit`
   and `dot_cascade.go` reads `deps.bus` is precisely that, and every reviewer of the next slice has
   to hold both idioms in their head.
4. **The gate only means something whole.** `scripts/runloop-emitter-gate.sh` asserts a per-file
   budget of 0. It cannot be armed until every file is converted, so a split leaves the ratchet
   unarmed across the intermediate commits — on the hottest files in the tree.
5. **Conflict resolution here is trivial by construction.** Every hunk is a one-token substitution on
   a line whose surrounding text is unchanged. If a concurrent commit does touch one of these lines,
   `git rebase` conflicts on exactly that line and the resolution is "keep their line, apply the
   substitution." Small windows buy nothing when the resolution is mechanical.

**Fallback if the tree is dirty when you start: WAIT, do not split.** The right response to a
concurrent writer is to let them land, not to interleave 108 token edits with their work.

### Must NOT run concurrently with RT16

- **RT13** (merge-path carve-out) and **E4c** (`buildWorkerRegistry`) — both edit `workloop.go`.
  These are RT16's stated dependencies; confirm both are *committed*, not just "in flight."
- **E4d** — parked into E5, but it targets `beadRunOne`'s remote branch, which is where 3 of RT16's
  24 workloop sites live (`:3659 :3710 :3725`).
- **RT14** (`waitAgentReady` → `dispatchSegment`) — rewrites `workloop.go:4897`-neighbourhood and
  `dot_gate.go:474`, both inside RT16's edit ranges.
- **RT17 / RT18** — by definition; they are RT16's successors on the same functions.
- **The quality audit's rec 5** (replace `beadRunOne`'s `//nolint:gocognit,cyclop,funlen`, now at
  `workloop.go:3174`) — `PROGRESS.md` §3 already defers it; it sits 13 lines above RT16's insertion
  point.
- **Any `internal/harness/*` residue** touching `dot_cascade.go`'s `codex.EmitImplementerNoWorkSuspected`
  call site (`:2035`) or `workloop.go:5160`.

---

## 7. Risks

1. **Scope creep into the outer loop.** The 10 `runWorkLoop` / `activateFirstPendingGroup` /
   `evaluateGroupAdvanceWithOutcome` / `maybeEmitEpicCompleted` sites *look* identical to the 108 and
   an implementer will be tempted by a global `sed`. Converting them is churn on the tree's hottest
   file for zero extraction value, and it would make the ratchet's `workloop.go` budget wrong.
   **Mitigation:** §1b lists the 10 excluded line numbers explicitly; `scripts/runloop-emitter-gate.sh`
   budgets `workloop.go` at `>10`, not `>0`, so a global sed shows up as a *shrink* the reviewer must
   justify, and §4 step 3 says to work bottom-up from an explicit line list, never by pattern.

2. **Type inference at the two capture sites.** `reviewloop.go:458 capturedBus := deps.bus` and
   `dot_cascade.go:1840 hbTarget := deps.bus` (followed by `hbTarget = tap`) infer their type from
   the RHS. **Mitigation:** `EmitterPort` is a type *alias* (`runports.go:40`), so the inferred type
   is literally `handlercontract.EventEmitter` in both the before and after — the `hbTarget = tap`
   assignment on the next line is the compile-time proof, and §5 step 1 catches any error instantly.

3. **`emit` shadows something.** Verified: the only `emit` binding in these six files is
   `var emit workers.EmitFunc` at `workloop.go:1307`, inside `buildWorkerRegistryWithRunner` — a
   different function, and one **E4c moves out of the file entirely**. Every other `emit`/`emitter`
   hit in all six files is comment prose. **Mitigation:** `go build` is definitive; if E4c has not
   landed, that is a second reason to wait for it.

4. **`deps.bus` mutated between the binding and a use.** Would break the hoist. Verified impossible:
   the only `.bus =` assignment in the module is `bootstate.go:119`, on `*bootState`, not
   `workLoopDeps`. **Mitigation:** stated here so the reviewer checks the claim rather than assuming
   it; re-verify with `grep -rn '\.bus\s*=' internal/daemon/*.go | grep -v _test`.

5. **Placement of the `emit` binding relative to the `deps.clock` default.** `emitterPort()` reads
   only `.bus` and never touches `.clock`, so placement is behaviour-neutral — but placing
   `deps.runPorts()` (which *does* read `.clock`) before the default would silently produce a nil
   `Clock`. **Mitigation:** RT16 deliberately uses the narrow `emitterPort()` in the four functions
   that carry the clock-mutation idiom, and reuses the *already-correctly-placed* `rp` in
   `beadRunOne`. Do not "simplify" the four to `deps.runPorts().Emitter`.

6. **The lint gate is inverted relative to `README.md` §4.** That block says "FULL run, not
   `--new-from-rev`: moved files are 100% new lines." Nothing moves here, and a full
   `golangci-lint run ./internal/daemon/...` trips all 104 functions grandfathered past
   `funlen: 100`. **Mitigation:** §5 step 5 pins `--new-from-rev=HEAD` and says why. Do not accept a
   "lint is red" report that used the full run.

7. **The differential baseline is captured against the wrong scope.** RT13 added
   `internal/runmerge`; a baseline taken from the E5 plan's older two-package command silently
   compares different universes. **Mitigation:** §4 step 1 and §5 step 9 use the identical
   three-package list, and `PROGRESS.md` §"Phase 5 status" records that this exact error invalidated
   the previous differential run.

8. **"This is a no-op, why did we do it?"** A reviewer can legitimately ask what 113 changed lines
   bought, since the binary behaves identically. **Mitigation:** state the number in the commit body
   — 108 mover-side `deps.bus` field reads → 8 port reads, which turns RT18's re-signature of
   `beadRunOne` / `runReviewLoop` / `driveDotWorkflow` / `dispatchDotAgenticNode` from a 108-line
   rewrite into an 8-line one, and drops `reviewloop.go`'s `deps.` surface (RT17's declared exit
   gate) by ~37% at zero behavioural risk. This is the cheapest coupling removal remaining in the
   entire RT stream; every other item in E5 §3a carries real semantics.

9. **Emission ordering silently changes.** The theoretical worst case for any emission-plumbing edit.
   **Mitigation:** `internal/replay`'s run-keyed checkers (§5 step 7) exist precisely to detect a
   "pure move" that reordered or renamed an emission, and §4 step 9's normalized-diff check proves
   mechanically that no statement moved.

10. **`sub_workflow_runner.go` keeps 2 `deps.` reads after conversion.** `r.deps.emitterPort()` still
    reaches through the struct's `deps workLoopDeps` field. This is deliberate — replacing that field
    is RT18's job — but it means the file's `deps.` count does not go to 0. **Mitigation:** the
    ratchet budgets `sub_workflow_runner.go` on `deps.bus` (0), not on `deps.` (2), so the gate says
    what is true.

---

## 8. Rollback

Trivially reversible; nothing moves, nothing is exported, no runtime state is created.

1. **Mid-flight, uncommitted** — restore only this slice's own files, never `git reset --hard`
   (`PROGRESS.md`'s 08:00 near-collision):
   ```bash
   cd /Users/gb/github/harmonik
   git checkout HEAD -- internal/daemon/workloop.go internal/daemon/reviewloop.go \
     internal/daemon/dot_cascade.go internal/daemon/dot_gate.go \
     internal/daemon/runbridge.go internal/daemon/sub_workflow_runner.go Makefile
   rm -f scripts/runloop-emitter-gate.sh
   go build ./internal/... ./cmd/...
   ```
2. **Committed, not released:** one commit, no moves — `git revert <sha>` restores all six files and
   the Makefile atomically. Nothing depends on the new binding, so a revert cannot orphan a caller.
   Verify with the §5 gate, not by eye.
3. **Released, regression surfaces later:** revert, then re-run §5 steps 7–9. If the regression
   *reproduces after the revert*, it was not RT16 — check the known-flaky allowlist in
   `00-test-oracle-baseline.md` before re-opening, applying the right confirmation procedure per
   §5 trap 6.
4. **Abandoning the E5 stream entirely:** **keep RT16.** It is net-positive standalone — it removes
   100 direct field reads from the four hottest files in the tree and arms a ratchet that stops them
   coming back, whether or not the run machine is ever lifted.
