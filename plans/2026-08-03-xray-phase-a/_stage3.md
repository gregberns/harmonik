# Stage 3 — `pertypecompat_hqwn38.go`: type the field, then swap the literals

Companion to `_plan.md`. That plan's stages 0–2 are done and committed (`1e700ea4f`,
`c96d1efbb`). This is the instruction set for stage 3, which `_plan.md` deliberately held
as a sketch pending stage 2's measurement.

**Base for this stage: `c96d1efbb`.** Worktree `/Users/gb/github/harmonik-wt/xray`,
branch `work/xray`. Work nowhere else.

Everything in `_plan.md` that is not restated here still applies: the known build-cache
flake (retry once before believing it), the ban on `go test ./...` as a gate, the
"report the number you get, whatever it is" rule for the measurement, and the
out-of-scope list.

---

## Objective

`internal/core/pertypecompat_hqwn38.go` is the third of the four places that spell the
event-type vocabulary. Stage 2 joined the second (the registry) to the first (the
taxonomy) and the measurement confirmed the compat table is still joined to neither:

```
internal/core/pertypecompat_hqwn38.go  <->  internal/core/eventtype.go
  graph : 4 decls <-> 184 decls | 0 edges (0 refs) {}
internal/core/eventreg_hqwn59.go  <->  internal/core/pertypecompat_hqwn38.go
  graph : 23 decls <-> 4 decls | 0 edges (0 refs) {}
```
*(measured at `c96d1efbb` against `/tmp/xr/after_symbols.json`, the stage-2 graph.)*

Join the third. Same prediction, same falsifiability: VOCAB edges appear, or they do not
and we report that.

---

## Why this stage is not stage 2 again

Stage 2 was protected by a *structural* accident: its rule ("the constant in
`eventtype.go` whose value is exactly this") skipped all five exceptions automatically,
because none of the five had a constant in that file. `02_xray-result`'s finding 4 warned
that the protection stops the moment an exception has a matching constant. **Stage 3 is
worse than that case.** Here the hazard is not an exception with a constant — it is a
*correct* substitution target that has **two or more** constants holding the same value,
declared with **different types**, in **different files**.

Derived, not assumed. Five values in `internal/core` are declared by more than one
constant, and in every case exactly one of them is the `EventType`:

| row | value | the right constant (`eventtype.go`) | the decoys, and where they live |
|---|---|---|---|
| 117 | `iteration_cap_hit` | `EventTypeIterationCapHit` | `DecisionRequiredReasonIterationCapHit` — `eventpayloads_u3q6o.go:144` |
| 129 | `gate_escalated` | `EventTypeGateEscalated` | `OperatorEscalationReasonGateEscalated` — `reconciliationevents_hqwn59.go` |
| 195 | `budget_exhausted` | `EventTypeBudgetExhausted` | `OperatorEscalationReasonBudgetExhausted` — `reconciliationevents_hqwn59.go`; **and** `FailureClassBudgetExhausted` — `failureclass.go` |
| 238 | `infrastructure_unavailable` | `EventTypeInfrastructureUnavailable` | `DaemonDegradedReasonInfrastructureUnavailable` — `daemondegradedreason.go:33` |
| 240 | `merge_conflict_escalation` | `EventTypeMergeConflictEscalation` | `DecisionRequiredReasonMergeConflictEscalation` — `eventpayloads_u3q6o.go:148` |

`02_xray-result` named three of these. There are five; `gate_escalated` and
`budget_exhausted` were not previously identified, and `budget_exhausted` is a three-way
collision. Reproduce the list yourself with the derivation script in §2 before trusting
this table. `detect` reports the wrong constant for the ones it flags — it resolves by
value and value alone.

**Two mitigations. Both are mandatory and neither is optional-if-careful.**

1. **Build the constant table from `eventtype.go` alone**, and assert no two constants
   *within that file* share a value. (Verified true today: 182 constants, 0 duplicate
   values. If that ever stops holding, stop.) A value→name inverse map built over all of
   `internal/core` collapses the five rows above and silently picks a decoy.

2. **Type the field first, in its own commit.** `PayloadCompatEntry.TypeName` becomes
   `EventType` before any literal is touched. This is the guard rail: with the field typed,
   `{TypeName: DecisionRequiredReasonIterationCapHit}` is a **compile error**, not a
   working program with the wrong provenance. With the field left as `string` it compiles
   and every test passes.

---

## Stage 3.0 — the gate, and confirming the start state

```sh
cd /Users/gb/github/harmonik-wt/xray
git status --porcelain          # expect: only ?? plans/2026-08-03-xray-phase-a/
git rev-parse --short HEAD      # expect: c96d1efbb
go build ./...                  # expect: clean
```

**THE GATE.** Note `-count=1`. `_plan.md`'s gate omitted it and reported `ok … (cached)`
for a package that had just been edited; the post-condition "the gate is green" was
vacuous for one run of stage 1. Never run it without the flag.

```sh
go test -count=1 ./internal/core/... ./internal/replay/... \
        ./internal/eventbus/... ./internal/handlercontract/...
```

**The blast radius adds no packages to `_plan.md`'s gate.** Derived: the only symbols
leaving this file are `PayloadCompatEntry`, `LookupPayloadCompatEntry` and
`AllPayloadCompatEntries`, and the complete set of files mentioning any of them is

```
internal/core/pertypecompat_hqwn38.go        the file itself
internal/core/pertypecompat_hqwn38_test.go   the table's tests
internal/core/runstartedread.go              LookupPayloadCompatEntry(string(EventTypeRunStarted))
internal/replay/replay.go:247                core.LookupPayloadCompatEntry(ev.Type)
```

`internal/core` and `internal/replay` are both already in the gate. Re-derive this before
you start:

```sh
grep -rn --include='*.go' -E 'PayloadCompatEntry|AllPayloadCompatEntries' . | grep -v '^./internal/core/pertypecompat'
```

If that turns up a file outside `internal/core` and `internal/replay`, **that is hard stop
1** — the ripple is wider than planned. Stop and report.

Baseline test binary, for reference (this is *not* the hash the post-condition compares —
see stage 3.2):

```sh
go test -c -o /tmp/xr3/core-base.test ./internal/core/ && shasum -a 256 /tmp/xr3/core-base.test
```

---

## Stage 3.1 — type the field and the lookup

**One commit. No literal in the compat table changes in this commit.**

The table rows keep their string literals throughout stage 3.1 and still compile: an
untyped string constant is assignable to `EventType`. That is what makes the two stages
separable, and it is why the resulting edge delta can be attributed.

### The edits

`internal/core/pertypecompat_hqwn38.go`:

```go
type PayloadCompatEntry struct {
	// TypeName is the §8 event type name (e.g. "run_started").
	TypeName EventType        // was: string
	...
}

func LookupPayloadCompatEntry(typeName EventType) (PayloadCompatEntry, bool) {   // was: string
```

`internal/core/runstartedread.go:28` — the existing `string(...)` conversion becomes wrong
in the other direction. `string(EventTypeRunStarted)` is a *typed* `string` constant, and
`string` is not assignable to `EventType`. Drop the conversion:

```go
entry, ok := LookupPayloadCompatEntry(EventTypeRunStarted)
```

`internal/replay/replay.go:247` — `ev.Type` is `string` because `core.Event.Type` is still
`string` (see "out of scope"). Convert explicitly at the boundary:

```go
if entry, ok := core.LookupPayloadCompatEntry(core.EventType(ev.Type)); ok && !entry.CompatWindowHolds {
```

`internal/core/pertypecompat_hqwn38_test.go` — **the sketch in `_plan.md` said three files
and did not anticipate how much of the test file moves.** `e.TypeName` is used as a
`map[string]` key, as a `t.Run` name, and as an argument to `LookupTypeSchemaVersion`
(which keeps its `string` signature — do not widen it, it is the registry's API and has
other callers). Every such use needs `string(e.TypeName)`. Derive the exact set rather
than working from this list:

```sh
grep -n 'e\.TypeName\|got\.TypeName' internal/core/pertypecompat_hqwn38_test.go
```

Expected shape of the change — confirm each against the compiler, do not pattern-match
blindly:

| line | current | why it breaks | fix |
|---|---|---|---|
| 60 | `declaredByName[e.TypeName] = e` | `map[string]PayloadCompatEntry` key | `string(e.TypeName)` |
| 80 | `t.Run("declared/"+e.TypeName, …)` | concat yields `EventType`; `t.Run` takes `string` | `string(e.TypeName)` |
| 82, 84 | `registeredNames[e.TypeName]` | `map[string]bool` key | `string(e.TypeName)` |
| 103, 136, 157, 177, 221, 241, 270 | `t.Run(e.TypeName, …)` | `t.Run` takes `string` | `string(e.TypeName)` |
| 179 | `LookupTypeSchemaVersion(e.TypeName)` | takes `string` | `string(e.TypeName)` |
| 202 | `seen[e.TypeName]++` | `map[string]int` key | `string(e.TypeName)` |
| 276–280 | `got.TypeName != e.TypeName`, `%q` args | fine — same type; `%q` handles a named string type | no change |
| 291 | `LookupPayloadCompatEntry("not_a_real_…")` | untyped constant | no change |

**Do not** change the `TypeName` doc comment's `(e.g. "run_started")`. It documents the
wire name, same reasoning as `_plan.md`'s comment rule.

**Do not** widen `LookupTypeSchemaVersion`, `RegisterEventType`, or
`RegisterEventTypeAtVersion`. Different blast radius, different decision.

### Post-conditions for 3.1 (all before committing)

1. `go build ./...` clean.
2. The gate (with `-count=1`) green.
3. `git diff --stat` names **exactly four** files: `pertypecompat_hqwn38.go`,
   `pertypecompat_hqwn38_test.go`, `runstartedread.go`, `internal/replay/replay.go`.
   More than four → hard stop 1.
4. Zero literal substitutions happened:
   ```sh
   grep -cE '\{TypeName: "' internal/core/pertypecompat_hqwn38.go   # expect 180, unchanged
   ```
5. The field and signature are typed:
   ```sh
   grep -n 'TypeName EventType' internal/core/pertypecompat_hqwn38.go
   grep -n 'func LookupPayloadCompatEntry' internal/core/pertypecompat_hqwn38.go
   ```

The compiled binary **will** change in this commit (a struct field type and an exported
signature are in it). That is correct and expected; the hash check does not apply here.

Commit message: `refactor(core): type PayloadCompatEntry.TypeName as EventType`

---

## Stage 3.2 — replace the literals with the constants

**One commit. This is the measured change.**

### The rule

For every row `{TypeName: "x", …}` in `allPayloadCompatEntries`, replace `"x"` with the
identifier of the constant **declared in `internal/core/eventtype.go`** whose declared
value is exactly `"x"`. If no such constant exists in that file, leave the literal alone.
Never resolve a value against any other file.

**Script it. Do not hand-edit 175 lines.** `_plan.md`'s stage 2 report is emphatic on this
and it applies with more force here, because five of the rows have decoys.

### Derived counts — load-bearing

```
compat rows with a literal TypeName            180
EventType constants declared in eventtype.go   182
duplicate VALUES within eventtype.go             0     <- assert this; stop if nonzero
duplicate literals within the compat table       0
rows that map to an eventtype.go constant      175     <- substitutions
rows that map to nothing there                   5     <- the deny list
```

**175 + 5 = 180.** If your derivation does not reconcile to exactly this, **hard stop 2**.
Note this is not the "~178" the `_plan.md` sketch guessed; the sketch was estimated, this
is counted.

Derivation, run it yourself:

```sh
cd /Users/gb/github/harmonik-wt/xray
python3 - <<'PY'
import re, sys
from collections import Counter
ct = open('internal/core/eventtype.go').read()
decls = re.findall(r'\b(EventType[A-Za-z0-9_]*)\s+EventType\s*=\s*"([^"]*)"', ct)
name2val = dict(decls)
assert len(decls) == len(name2val), "duplicate constant NAMES in eventtype.go"
dup = {v:c for v,c in Counter(v for _,v in decls).items() if c > 1}
if dup: sys.exit(f"STOP: duplicate values within eventtype.go: {dup}")
val2name = {v:n for n,v in decls}
lits = re.findall(r'\{TypeName: "([^"]*)"', open('internal/core/pertypecompat_hqwn38.go').read())
dl = [l for l,c in Counter(lits).items() if c>1]
if dl: sys.exit(f"STOP: duplicate rows: {dl}")
mapped   = [l for l in lits if l in val2name]
unmapped = [l for l in lits if l not in val2name]
print(f"constants {len(decls)} | rows {len(lits)} | mapped {len(mapped)} | denied {len(unmapped)}")
print("deny list:", unmapped)
PY
```

### The deny list — 5 rows, and why each stays a literal

These are the same five names `_plan.md` carved out of the registry, appearing again in
the compat table. The rule above skips them structurally, but they are written down anyway
because the structural protection is an accident of this codebase and must not be relied
on as vigilance.

| line | literal | what exists instead | why it stays |
|---|---|---|---|
| 179 | `agent_message` | `eventTypeAgentMessage`, **unexported**, `internal/digest/resolver.go:162` | Not reachable from `core`. The gap points outward: `core` should own this constant and `digest` should use it. Not this stage's call. |
| 180 | `agent_presence` | nothing, anywhere | No constant to reference. **Do not invent one** — adding to the `EventType` enum has consequences in `eventtype_coverage_gjyks_test.go`. |
| 301 | `session_keeper_config_rejected` | nothing, anywhere | Same. The coverage test has explicit carve-outs for the `session_keeper_*` family. |
| 318 | `agent_input_acked` | `codexinput.EmitInputAcked`, type `codexinput.EmitType`, **another package** | Substituting requires `core` to import `internal/codexinput`. `core` has 48 dependents and zero dependencies; inverting that to save a string literal is the wrong trade. Same text, different declaration — a coincidence of value. |
| 319 | `agent_input_stale` | `codexinput.EmitInputStale`, same | Same. |

If your derivation's deny list is not exactly these five strings, **hard stop 2**.

### Post-conditions for 3.2 (all before committing)

1. `go build ./...` clean.
2. The gate (with `-count=1`) green.
3. Exactly five literals remain:
   ```sh
   grep -cE '\{TypeName: "' internal/core/pertypecompat_hqwn38.go   # expect 5
   ```
   and they are the five above:
   ```sh
   grep -nE '\{TypeName: "' internal/core/pertypecompat_hqwn38.go
   ```
4. `git diff --stat` names **exactly one** file, `internal/core/pertypecompat_hqwn38.go`.
   If the test file needs a change in this commit, something went wrong in 3.1.
5. **The five collision rows name the `EventType` constant.** Machine-checkable, and this
   is the specific thing this stage exists to get right:
   ```sh
   grep -nE '\{TypeName: (EventTypeIterationCapHit|EventTypeGateEscalated|EventTypeBudgetExhausted|EventTypeInfrastructureUnavailable|EventTypeMergeConflictEscalation),' internal/core/pertypecompat_hqwn38.go | wc -l   # expect 5
   grep -cE 'DecisionRequiredReason|DaemonDegradedReason|OperatorEscalationReason|FailureClass' internal/core/pertypecompat_hqwn38.go   # expect 0
   ```
6. **Binary identity.** A constant substituted for its own value is a no-op after constant
   folding, so the compiled package must be byte-identical across this commit *only*:
   ```sh
   git stash            # or build at HEAD before applying
   go test -c -o /tmp/xr3/core-3.1.test ./internal/core/
   git stash pop
   go test -c -o /tmp/xr3/core-3.2.test ./internal/core/
   shasum -a 256 /tmp/xr3/core-3.1.test /tmp/xr3/core-3.2.test   # MUST match
   ```
   A mismatch means the substitution changed a value or otherwise altered codegen —
   **hard stop 3**. This does not apply across 3.1, which legitimately changes the binary.
7. **Value-set proof.** `verify_vocab_swap.py` expects `call(...)` syntax and this file
   uses `{TypeName: …}` struct-literal syntax, so rewrite both sides into a synthetic call
   form first — the rewrite is mechanical and touches no real source:
   ```sh
   mkdir -p /tmp/xr3
   git show c96d1efbb:internal/core/pertypecompat_hqwn38.go \
     | sed 's/{TypeName: /compatRow(/' > /tmp/xr3/compat-before.go
   sed 's/{TypeName: /compatRow(/' internal/core/pertypecompat_hqwn38.go > /tmp/xr3/compat-after.go
   python3 /Users/gb/github/codebase-organism/refactor/verify_vocab_swap.py \
     --before /tmp/xr3/compat-before.go --after /tmp/xr3/compat-after.go \
     --consts internal/core/eventtype.go --call 'compatRow'
   ```
   Must print `PASS` and exit 0.

   **Read this result correctly.** `02_xray-result` says the script's inverse map has the
   collapse bug. Precisely: the script resolves *name → value*, which does not collapse;
   the weakness is that it compares **value sets**, so a decoy constant holding the same
   value would preserve the set. It is saved here only by `--consts` being `eventtype.go`
   alone — a decoy is then not in the table and comes back `UNRESOLVED`, which fails.
   **So: PASS is necessary, not sufficient, and it is only as strong as the `--consts`
   argument.** Post-condition 5 and the typed field are what actually prove the right
   constant was chosen. Say all of this in the report; do not report a bare PASS.

Commit message: `refactor(core): name compat-table event types by constant, not by literal`

---

## Stage 3.3 — measure

Run from the research repo. Paste raw output into the report; **report the number you get,
whatever it is.** Zero is a valid result and must not be worked around.

```sh
cd /Users/gb/github/codebase-organism
go build -o /tmp/xr3/typegraph ./typegraph/cmd/typegraph
/tmp/xr3/typegraph -repo /Users/gb/github/harmonik-wt/xray -out /tmp/xr3/after ./...
python3 refactor/edgecheck.py \
  --graph /tmp/xr3/after_symbols.json --before /tmp/xr/after_symbols.json \
  --pair internal/core/pertypecompat_hqwn38.go internal/core/eventtype.go \
  --pair internal/core/eventreg_hqwn59.go internal/core/pertypecompat_hqwn38.go \
  --pair internal/replay/replay.go internal/core/pertypecompat_hqwn38.go \
  --pair internal/core/runstartedread.go internal/core/pertypecompat_hqwn38.go
```

`--before /tmp/xr/after_symbols.json` is the stage-2 graph and is the correct baseline for
this stage. Recorded there:

```
pertypecompat <-> eventtype                    0 edges
eventreg      <-> pertypecompat                0 edges
replay.go     <-> pertypecompat                1 edge  {'CALL': 1}
runstartedread<-> pertypecompat                1 edge  {'CALL': 1}
```

Predictions, stated in advance so they can be wrong:

- `pertypecompat <-> eventtype` goes from 0 to roughly 175 VOCAB plus a small number of
  TYPEREF (the `TypeName` field and the `LookupPayloadCompatEntry` parameter). The
  two-commit split is what lets those be told apart — the VOCAB count belongs to 3.2, the
  TYPEREF to 3.1.
- `eventreg <-> pertypecompat` stays **0**. Both files now reference `eventtype.go`, but
  neither references the other. If this moves, the graph is counting something other than
  a direct reference and that is worth more than the rest of the measurement.
- The two `CALL` edges are unchanged.

---

## Hard stops — stop, do not route around, write the report

1. **The ripple exceeds the four planned files**, or reaches a package outside
   `internal/core` and `internal/replay`.
2. **The mapping does not reconcile.** Every row must map to exactly one constant in
   `eventtype.go` or appear on the five-row deny list. Not 175 + 5 → stop.
3. **The gate is red, or the 3.1→3.2 binary hashes differ**, or `verify_vocab_swap.py`
   does not print PASS, or post-condition 5's decoy grep is nonzero.
4. **Two constants in `eventtype.go` share a value.** The whole substitution rule is
   unsound if this ever becomes true.
5. **You want to change `eventtype.go` or `event.go`.** Out of scope, both of them.
   Also out of scope: `LookupTypeSchemaVersion`, `RegisterEventType`,
   `RegisterEventTypeAtVersion`, and inventing the two missing constants.

A hard stop is information. The report is the deliverable either way.

---

## Explicitly out of scope

**`core.Event.Type` is still `string`** — `internal/core/event.go:47`, with a comment
saying the enum lives in a separate bead and this field uses `string` until it lands. It
landed. This is the root cause of the whole four-copies pattern: because the carrier field
is untyped, nothing downstream is ever forced to hold an `EventType`, so every new site
drifts back to a literal. It is visible in this very stage — `internal/replay/replay.go`
needs an explicit `core.EventType(ev.Type)` conversion for no reason other than this.
~200 sites. **Operator decision. Record it, do not act on it.**

Also out of scope: the five deny-list rows; the two missing constants (`agent_presence`,
`session_keeper_config_rejected`); `internal/workers`' direct `core.RegisterEventType`
literal calls; the `allEventTypeCohort` coverage test table; the seven `EventType`
constants with no compat-table row (`EventTypeGovernorSignal`,
`EventTypeLoopObservedPhantomDone`, and the five worker/resource types registered from
`internal/workers`); and every test failing at baseline outside the gate.

---

## Done means...

1. Exactly **two** commits above `c96d1efbb` on `work/xray`. 3.1 touches four files, 3.2
   touches one. Verified by `git log --oneline --stat c96d1efbb..HEAD`.
2. `PayloadCompatEntry.TypeName` is `EventType` and `LookupPayloadCompatEntry` takes
   `EventType`. Verified by grep.
3. Exactly five string literals remain in the compat table, and they are the five named.
   Verified by `grep -nE '\{TypeName: "' internal/core/pertypecompat_hqwn38.go`.
4. No decoy constant appears in the file. Verified by post-condition 5's second grep
   returning 0.
5. `go build ./...` clean; the gate green **with `-count=1`**; 3.1→3.2 test binaries
   byte-identical; `verify_vocab_swap.py` PASS with its limitation stated.
6. The edge delta for all four pairs is measured and pasted raw into the report.
7. `plans/2026-08-03-xray-phase-a/report-stage3.md` exists, uncommitted, with the headings
   below.

---

## The report — `report-stage3.md`, uncommitted

- **What was done** — the two commits, files, line counts.
- **Measurement** — `edgecheck.py` output pasted verbatim, all four pairs, plus the
  binary hashes and the `verify_vocab_swap.py` output.
- **Effort** — per stage, wall clock, mechanical vs judgement. This calibrates a cost
  model that is currently invented, so be precise; `02_xray-result` finding 3 argues the
  model is wrong in *shape* (per-file plus fixed verification, not per-substitution) and
  this is the second data point.
- **Surprises** — the most valuable section. What this plan got wrong. Where a rule was
  stated but judgement was needed. **Specifically: did the typed-field guard rail actually
  catch anything?** If it caught nothing, say so plainly — that is a real result about
  whether the guard rail was necessary or merely reassuring.
- **Findings not acted on.**
- **What you would tell the next agent.**
