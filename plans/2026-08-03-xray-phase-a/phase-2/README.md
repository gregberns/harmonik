# Phase 2 — areas of focus

**Generated 2026-08-09 against harmonik `d5a12348f`.** Every number here was produced by a command that
is written down next to it. Re-run the commands and you get these numbers back; if you don't, harmonik
moved and the area files need regenerating (see §5).

This directory is **not a plan**. It is the input an agent needs to *write* one. Each area file is a
self-contained brief: what the issue is, why it makes the codebase harder to decompose, the live instance
list, the traps that will make a naive fix wrong, and the gate that proves the fix worked. An agent should
be able to take one area file and produce a task list without reading anything else.

The four phase-A waves that produced `../report.md` and `../report-stage3.md` were all hand-scoped. These
areas are the first ones derived from the detector's full output.

---

## 1. Where the work is, at a glance

`refactor/cmd/detect` finds **2,431 issues** at `d5a12348f`. `refactor/cmd/plan` groups them into **794
operations** totalling **22,244 minutes** of single-agent effort across **399 distinct files**.

| area | issue | findings | agent-ready? | file |
|---|---|---:|---|---|
| 1 | `core.Event.Type` is `string` while its 181-constant enum exists | 358 casts | **yes — highest leverage** | [area-01](area-01-event-type-enum.md) |
| 2 | Seven event types escape the registry and the compat table | 7 | **yes — smallest real win** | [area-02](area-02-unregistered-event-types.md) |
| 3 | A1 — literal duplicates a declared constant | 1,373 | partly; 724 need judgement | [area-03](area-03-a1-literal-duplicates-constant.md) |
| 4 | A2 — repeated literal with no constant | 653 values | **blocked** — see §4 | [area-04](area-04-a2-repeated-literals.md) |
| 5 | A4 — control flow on error message text | 14 | **yes — closed set** | [area-05](area-05-a4-error-string-matching.md) |
| 6 | B4 — wide dependency bundles | 80 | yes, one at a time | [area-06](area-06-b4-wide-bundles.md) |
| 7 | B1/B2 — pure logic welded to effects | 311 | yes, and **never attempted** | [area-07](area-07-b1-b2-pure-and-effects.md) |
| 8 | Toolchain gaps that make generated plans unsafe | 4 | organism repo, not harmonik | [area-08](area-08-toolchain-gaps.md) |

The scheduler's verdict on the whole worklist:

```
794 operations, total serial effort 22244.2 min
  precedence floor    187.0   longest chain of real dependencies
  exclusion floor     279.0   unlimited agents, but no two may touch one file
  parallelism ceiling   399   largest set of ops with no file in common
  BINDING CONSTRAINT: file exclusion
  best achievable speed-up 79.72x
critical path: A2:internal/projectconfig/projectconfig.go (30.0) -> B4:same file (157.0)
```

**Read that as a staffing answer, not a promise.** ~82 agents saturate it; the 83rd idles. The cost model
behind the minutes has exactly one calibrated row (A1), so treat every duration as an ordering signal and
none of them as an estimate.

## 2. Machine-readable instance lists

`data/` holds the finding records, sliced per area, in the schema the detector emits — so a scout agent
that examines code by judgement can append records in the *same* shape and they merge into one worklist.

```
data/area-03-a1-same-package.json        70 findings   the free ones
data/area-03-a1-cross-package.json      579 findings   each creates a package dependency
data/area-03-a1-ambiguous.json          724 findings   two+ packages declare the value; a human picks
data/area-04-a2-repeated-literals.json  653 findings   per site
data/area-04-a2-by-value.json           653 values     per decision, with struct tags flagged
data/area-05-a4-error-strings.json       14 findings   the whole set
data/area-06-b4-wide-bundles.json        80 findings
data/area-07-b1-pure-in-effectful.json  150 findings
data/area-07-b2-inline-effects.json     161 findings
```

Record shape (`refactor/catalog.md` in `codebase-organism` is the authority):

```jsonc
{ "issue": "A1", "phase": "A", "node": "internal/core.registerRunLifecycle",
  "file": "...", "line": 71, "evidence": "...", "fix": "...", "verify": "...",
  "decl_pkg": "...", "decl_name": "EventTypeRunStarted", "same_pkg": false, "ambiguous": false }
```

`node` is the join key back into the symbol graph. `evidence` is what the detector saw; it is not a
mandate. `fix` and `verify` are the catalog's generic prescription — the area file overrides them where
harmonik's specifics differ.

## 3. Rules every area inherits

These come from four executed waves and each one was learned the expensive way.

- **harmonik's `go test ./...` is not green at baseline** — roughly 30 timing- and environment-sensitive
  failures, ~10 minutes. It is not a gate. **Derive the gate from the wave's blast radius while planning**:
  the edited package plus its direct consumers, and confirm it is green *before* the change.
- **`-count=1`, always.** Go will report `ok (cached)` for the package just edited, which makes a
  post-condition vacuous. This machine also clears the build cache underneath running builds.
- **Prefer a post-condition the change's own shape makes exact.** A literal→constant substitution is a
  no-op after constant folding, so `go test -c` + `shasum` must be byte-identical. That subsumes the test
  run. Ask per class: *what does this change provably not alter?*
- **One issue class per wave.** If a wave changes literals *and* extracts functions, a regression cannot be
  attributed to either.
- **Findings are candidates, not tasks.** Every class below has a triage section, and for A1 and A2 the
  honest answer for the majority of instances is *leave it*.
- **Two agents never hold one file.** Every area file states its lock unit.

## 4. What is blocked, and by what

**Area 4 (A2) is unsafe to fan out.** The `Finding` schema has a single `file` field, but an A2 finding is
reported once per *value* across all its sites — 84% of them name more than one file, and one names 69. So
an A2 operation declares one lock and edits up to sixty-nine, and the scheduler will happily run two of
them concurrently on a file both are rewriting. Either fix the schema (area 8, item 1 — a one-line change)
or run A2 strictly one operation at a time.

**Phase C cannot enter a plan at all.** `funcseam` and `seam_finder` compute extraction candidates but emit
their own schemas; nothing converts them to `Finding` records. So the generated plan has no operation that
*creates* a file, which means its concurrency profile can only decay — the entire "a serial cut is a
purchase" economics is unreachable through the current pipeline. Area 8, item 2.

**No agent-scouted finding has ever been produced.** The shared-schema claim in §2 is a design that has
never been exercised. The first agent to append hand-written records to one of these files is testing it.

## 5. Regenerating this directory

From a `codebase-organism` checkout, against any harmonik revision:

```sh
go run ./refactor/cmd/detect -repo <harmonik> -out findings.json
go run ./refactor/cmd/plan   -in findings.json -out plan.json      # add -select triage.json once triaged
go run ./scheduler/cmd/opdag -plan plan.json -agents 8 -sweep=false
```

Detect is read-only and takes ~2.3 s on harmonik. Prefer a detached worktree at a named revision over the
working checkout, so the numbers are reproducible and other agents' edits cannot land mid-scan.

## 6. Honest limits of this directory

- **Nothing here has been executed.** Areas 1, 2 and 5 are the only ones whose instance lists were
  hand-verified end to end; areas 3, 4, 6 and 7 are detector output with a triage rule applied.
- **The minutes are a model, not a measurement.** One calibrated row (A1, from one agent's self-report, on
  one file). B-class marginals are invented, and phase B has never been executed at all — which is exactly
  why area 7 is worth more as an experiment than as cleanup.
- **The A2 struct-tag result in area 4 was found while writing this** and has not been fed back into the
  detector. Until it is, every A2 count published anywhere in this project is inflated by ~36%.
- **The triage decisions in areas 3 and 4 are rules, not judgements.** They partition the findings into
  buckets that *deserve* judgement; they do not supply it. `plan` will stamp `"triaged": false` on anything
  built from these files until a real selection document exists.
