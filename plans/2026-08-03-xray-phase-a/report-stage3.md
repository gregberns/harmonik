# Report — xray phase-A, stage 3

Branch `work/xray`, worktree `/Users/gb/github/harmonik-wt/xray`, base for this stage
`c96d1efbb`. Instruction set: `_stage3.md` (written before any source edit, as required).
Stage 3 complete. No hard stop was hit.

---

## What was done

Two commits, in plan order.

| commit | subject | diff |
|---|---|---|
| `fe5406774` | `refactor(core): type PayloadCompatEntry.TypeName as EventType` | 4 files, +16 / -16 |
| `85a2a12ef` | `refactor(core): name compat-table event types by constant, not by literal` | 1 file, +175 / -175 |

**Stage 3.1** changed `PayloadCompatEntry.TypeName` from `string` to `EventType` and
`LookupPayloadCompatEntry` to take `EventType`. Four files moved, exactly the four the
plan derived:

- `internal/core/pertypecompat_hqwn38.go` — field type, function signature (+2/-2).
- `internal/core/runstartedread.go:28` — `LookupPayloadCompatEntry(string(EventTypeRunStarted))`
  -> `LookupPayloadCompatEntry(EventTypeRunStarted)`. The pre-existing `string()`
  conversion became a compile error in the opposite direction.
- `internal/replay/replay.go:247` — `core.LookupPayloadCompatEntry(ev.Type)` ->
  `core.LookupPayloadCompatEntry(core.EventType(ev.Type))`, because `core.Event.Type` is
  still `string`.
- `internal/core/pertypecompat_hqwn38_test.go` — 12 sites needed `string(e.TypeName)`
  (map keys, `t.Run` names, and the `LookupTypeSchemaVersion` argument). +24/-24 across
  12 lines.

The 180 table rows kept their string literals through this commit and still compiled — an
untyped string constant is assignable to `EventType`. That is what made the two stages
separable.

**Stage 3.2** replaced 175 of the 180 literals with the `eventtype.go` constant holding
that exact value. Five remain, all on the derived deny list, at lines 179
(`agent_message`), 180 (`agent_presence`), 301 (`session_keeper_config_rejected`), 318
(`agent_input_acked`), 319 (`agent_input_stale`) — the same five names `_plan.md` carved
out of the registry in stage 2. Comments untouched.

Counts reconciled exactly as planned: **182 constants in `eventtype.go`, 0 duplicate
values within it, 180 table rows, 0 duplicate rows, 175 mapped + 5 denied = 180.**

Post-conditions, all met:

```
go build ./...                                                     clean
go vet ./internal/core/ ./internal/replay/                         clean
go test -count=1 <the four gate packages>                          all ok (both stages)
gofmt -l internal/core/ internal/replay/                           empty
git diff --stat, 3.1                                               exactly 4 files
git diff --stat, 3.2                                               exactly 1 file
grep -cE '\{TypeName: "' pertypecompat_hqwn38.go                   5
grep -c '<decoy const types>' pertypecompat_hqwn38.go              0
git log --oneline c96d1efbb..HEAD                                  exactly 2 commits
git status --porcelain                                             only ?? plans/
```

---

## Measurement

### Edge delta — `edgecheck.py`, stage-2 graph as baseline

Raw output, `--graph /tmp/xr3/after_symbols.json --before /tmp/xr/after_symbols.json`:

```
internal/core/pertypecompat_hqwn38.go  <->  internal/core/eventtype.go
  before: 4 decls <-> 184 decls | 0 edges (0 refs) {}
  after : 4 decls <-> 184 decls | 177 edges (177 refs) {'TYPEREF': 2, 'VOCAB': 175}
  delta : +177 edges {'VOCAB': 175, 'TYPEREF': 2}

internal/core/eventreg_hqwn59.go  <->  internal/core/pertypecompat_hqwn38.go
  before: 23 decls <-> 4 decls | 0 edges (0 refs) {}
  after : 23 decls <-> 4 decls | 0 edges (0 refs) {}
  delta : +0 edges {}

internal/replay/replay.go  <->  internal/core/pertypecompat_hqwn38.go
  before: 22 decls <-> 4 decls | 1 edges (1 refs) {'CALL': 1}
  after : 22 decls <-> 4 decls | 1 edges (1 refs) {'CALL': 1}
  delta : +0 edges {}

internal/core/runstartedread.go  <->  internal/core/pertypecompat_hqwn38.go
  before: 5 decls <-> 4 decls | 1 edges (1 refs) {'CALL': 1}
  after : 5 decls <-> 4 decls | 1 edges (1 refs) {'CALL': 1}
  delta : +0 edges {}
```

Whole-repo graph after: 100 packages, 139,244 LOC, 0 load errors.

**All three stated predictions held, including the negative one.**

- `pertypecompat <-> eventtype`: 0 -> 177, decomposing **exactly** into 175 VOCAB (one per
  substitution, commit 3.2) + 2 TYPEREF (the `TypeName` field and the
  `LookupPayloadCompatEntry` parameter, commit 3.1). Nothing unaccounted for. This is the
  same clean decomposition stage 2 produced (174 + 2), and again it is only readable
  because the plan forced two commits.
- `eventreg <-> pertypecompat` stayed **0**. Both files now reference `eventtype.go`, but
  neither references the other, and the graph correctly does not invent a tie from a
  shared neighbour. Worth stating plainly: **sharing a vocabulary does not create an edge
  between the two consumers.** After two waves, the registry and the compat table are each
  joined to the taxonomy and still not to each other.
- The two `CALL` edges are unchanged.

So all three of `_plan.md`'s originally-invisible relationships have now been measured.
Two of the three (registry->taxonomy, compat->taxonomy) are now visible. The third
(registry<->compat) is not, and phase A as designed cannot make it so — that pair is joined
only through the constants they now both reference, which is a two-hop path, not an edge.

### Binary identity

```
base   (c96d1efbb)  7c5e2fa92d6e1f5b6892d02326a8a375407c22c2201bab52d7d9c4adf3518904
3.1    (fe5406774)  bbf88c753ce674d251789b308ab0d495e18deeddfa828035bbbecdfb854dc6cc
3.2    (85a2a12ef)  bbf88c753ce674d251789b308ab0d495e18deeddfa828035bbbecdfb854dc6cc
```

`go test -c -o ... ./internal/core/`, then `shasum -a 256`. Commit 3.2 is **byte-identical**
to 3.1, as a pure literal->constant substitution must be. Commit 3.1 legitimately changed
the binary (a struct field type and an exported signature are in it), which is why the
check is scoped to 3.2 only. The base hash matches the one stage 2 recorded, confirming
continuity with the previous wave.

### Value-set proof — `verify_vocab_swap.py`

The script expects `call(...)` syntax; this file uses `{TypeName: ...}` struct-literal
syntax. Both sides were rewritten with `sed 's/{TypeName: /compatRow(/'` into a synthetic
call form before running. Raw output:

```
constants available: 182
before: 180 calls (180 literal, 0 constant)
after : 180 calls (5 literal, 175 constant)
PASS — the registered value set is unchanged
EXIT=0
```

**Read this PASS narrowly.** `02_xray-result` warned the script's inverse map has the
collapse bug. The precise situation is slightly different from that description and worth
recording, because the distinction decides whether the tool can be trusted on the next
wave:

- The script resolves **name -> value**, which does not collapse. The collapse hazard is in
  the *other* direction and lives in `detect`, not here.
- The script's real weakness is that it compares **value sets**. A decoy constant holding
  the identical value preserves the set, so a wrong-type substitution is invisible to a
  set comparison.
- It is saved here only by `--consts` being `eventtype.go` alone: a decoy is then absent
  from its table and comes back `UNRESOLVED`, which fails the run. Had `--consts` been
  pointed at the whole package, all five collision rows could have been substituted with
  the wrong constant and the script would still have printed PASS.

**So the PASS is necessary, not sufficient, and it is exactly as strong as the `--consts`
argument.** What actually proves the right constant was chosen is the typed field
(a decoy would not compile) plus the explicit grep for decoy type names returning 0.

---

## Effort

Roughly 35 minutes wall clock, of which about 6 was editing source.

| stage | wall clock | mechanical vs judgement |
|---|---|---|
| reading `_plan.md`, `report.md`, `02_xray-result` | ~4 min | mechanical, but load-bearing — the trap warning came from here |
| deriving counts, deny list, collision audit | ~6 min | **judgement**, and the highest-value spend of the whole stage. One script, but deciding *what* to audit for (values declared by more than one constant, anywhere in `internal/core`) is what found the two extra collisions |
| writing `_stage3.md` | ~9 min | judgement. The longest single item |
| 3.0 — gate + blast-radius re-derivation | ~2 min | fully mechanical. Gate is 5.5 s warm |
| 3.1 — type change, 4 files | ~4 min | mostly mechanical; the test-file conversion set was derived by compiling, not by guessing |
| 3.2 — derive + apply 175 substitutions | ~1 min | fully mechanical, one scripted pass with assertions |
| 3.2 — verification (gate, hashes, value proof, greps) | ~4 min | mechanical |
| 3.3 — typegraph rebuild + measure | ~3 min | mechanical. Typegraph run is the long pole (~90 s) |
| report | ~7 min | judgement |

**This is the second data point for the cost-model claim in `02_xray-result` finding 3,
and it confirms the shape.** The 175 substitutions took one scripted pass, about a minute
— the same as stage 2's 174. Doubling the instance count would not have moved the total.
What actually cost time: **planning (~15 min), verification (~6 min), reporting (~7 min).**
Price a phase-A op as *fixed planning + fixed verification + fixed reporting + ~0 per
instance*. A per-substitution or per-LOC model prices the free part.

One correction to stage 2's cost note, though: it observed that "everything expensive was
verification, measurement, and one anomaly investigation." This stage had **no anomaly at
all** — no cache flake, no retry needed, nothing unexplained — and it still took longer,
because the planning was genuinely harder. Stage 2's cost was dominated by an unpredictable
variable; stage 3's was dominated by a predictable one. **Both stages were cheap in the
part a naive model measures and expensive in a different part each time.**

---

## Surprises

**1. The trap is bigger than reported: five collisions, not three.** `02_xray-result` named
`iteration_cap_hit`, `infrastructure_unavailable` and `merge_conflict_escalation`. A
systematic audit — every string-valued constant declared anywhere in `internal/core`,
grouped by value — found two more, both in the compat table:

| line | value | the `EventType` constant | the decoys |
|---|---|---|---|
| 117 | `iteration_cap_hit` | `EventTypeIterationCapHit` | `DecisionRequiredReasonIterationCapHit` |
| **129** | **`gate_escalated`** | `EventTypeGateEscalated` | **`OperatorEscalationReasonGateEscalated`** (`reconciliationevents_hqwn59.go`) |
| **195** | **`budget_exhausted`** | `EventTypeBudgetExhausted` | **`OperatorEscalationReasonBudgetExhausted`** *and* **`FailureClassBudgetExhausted`** (`failureclass.go`) — a three-way collision |
| 238 | `infrastructure_unavailable` | `EventTypeInfrastructureUnavailable` | `DaemonDegradedReasonInfrastructureUnavailable` |
| 240 | `merge_conflict_escalation` | `EventTypeMergeConflictEscalation` | `DecisionRequiredReasonMergeConflictEscalation` |

The lesson is not that the previous analysis was sloppy — it found the class correctly. It
is that **the previous analysis found the collisions incidentally, while checking something
else, and an incidental sweep is not a census.** The instruction that mattered was
"verify this claim yourself before relying on it." Doing so cost about ninety seconds and
changed the size of the known hazard by 67%. Any plan that hands a subagent a list of
known-bad cases should say explicitly whether the list is exhaustive or illustrative.

**2. The guard rail did not catch a wrong substitution — and that is the correct
result, not a disappointment.** Asked directly: **the typed field caught nothing during
the substitution.** The script never proposed a decoy, because the mitigation that actually
did the work was the *other* one — building the constant table from `eventtype.go` alone.
With that table, a decoy is simply not a candidate; there is nothing for the compiler to
reject.

The honest reading is that the two mitigations are not redundant, they are **layered, and
they fire at different times**:

- Building from `eventtype.go` alone is *preventive*. It makes the wrong substitution
  unrepresentable in the tool that generates the edit. This is what prevented all five.
- Typing the field is *detective*. It only fires if the preventive layer is bypassed —
  a hand-edit, a different script, a future maintainer adding a row by hand.

So the guard rail's value here is **prospective, not retrospective**. It caught nothing
today; it will catch the next person who types `{TypeName: DecisionRequiredReasonIterationCapHit}`
into this table by hand, and before this commit that person would have shipped it green.
That is worth the commit — but a report claiming "the guard rail saved us" would be false,
and I want the cost model to record that this stage's safety came from the cheap
mitigation, not the expensive one.

**One thing the type change did catch immediately**, which is the closest it came to
earning its keep on the day: `runstartedread.go:28` had `LookupPayloadCompatEntry(string(EventTypeRunStarted))`
— code that already *had* the right constant and threw the type away to satisfy a `string`
parameter. The compiler flagged it the instant the signature changed. That is a real,
if small, catch: it is the pattern of a developer knowing the constant exists and being
forced by an untyped API to launder it back into a string.

**3. The plan's own row count was wrong before I counted: 180, not the "~178" in
`_plan.md`'s sketch.** Small, but it is the second time a sketched number has been off
(stage 2's sketch also predated counting). The rule that worked both times: **derive every
count by running a command, and treat any number inherited from a sketch as a hypothesis.**
Both wrong numbers were harmless only because the plan required reconciliation before
substituting.

**4. The test file needed 12 conversion sites, not the "three files" the sketch
implied.** `_plan.md`'s stage-3 sketch named three files and framed the ripple as "one
call site at `replay.go:247`". The actual ripple was four files and the test file was the
largest single piece of it — `e.TypeName` was used as a `map[string]` key three times, as
a `t.Run` name eight times, and as an argument to `LookupTypeSchemaVersion` once. None of
it was hard, but it was 24 of the commit's 32 changed lines. **A field-type change ripples
into tests roughly in proportion to how often the field is used as a map key or a
subtest name**, and those uses are invisible to a grep for the function signature. Deriving
the site list by compiling rather than by reading was the right call and took one iteration.

**5. `verify_vocab_swap.py` needed a syntax adapter to run at all,** because it assumes
call syntax and this target is struct-literal syntax. The `sed` rewrite is three words
long and touches no real source, so this is a footnote rather than a defect — but the
script is now two-for-two on needing the caller to know its assumptions. If a third wave
targets a third syntax, the `--call` regex should probably become a `--pattern` that
matches the whole first-argument context.

**6. Nothing else went wrong.** No build-cache flake, no retry needed, no unexplained
behaviour, gate green on the first run at every checkpoint, `gofmt` clean without
intervention. `-count=1` was used on every invocation and never disagreed with a cached
run — but that is not evidence the flag is unnecessary; it is one clean sample against
stage 2's one dirty one.

---

## Findings not acted on

**A. `core.Event.Type` is still `string`** — `internal/core/event.go:47`, with the stale
comment saying the `EventType` enum lives in a separate bead and this field uses `string`
"until that enum lands". It landed. ~200 sites. **Operator decision, recorded not acted
on**, per the plan.

This stage produced fresh evidence for it. `internal/replay/replay.go:247` now reads
`core.LookupPayloadCompatEntry(core.EventType(ev.Type))`. That conversion exists for no
reason other than the carrier field being untyped — the value in `ev.Type` *is* an event
type, the parameter *is* an `EventType`, and a cast sits between them purely as ceremony.
Every consumer that typing an API touches will grow one of these until the envelope field
is typed. **Phase A can keep making the vocabulary visible, but each wave now adds a small
tax at the boundary, and the tax is the root cause billing itself in instalments.**

**B. Seven `EventType` constants have no compat-table row.** Derived:
`EventTypeGovernorSignal`, `EventTypeLoopObservedPhantomDone`, `EventTypeResourceBreach`,
`EventTypeWorkerOffline`, `EventTypeWorkerReport`, `EventTypeWorkerTunnelFailed`,
`EventTypeWorkerUnhealthy`. Five are the `internal/workers` types registered by literal
through the exported `core.RegisterEventType`. The other two are the pair
`02_xray-result` flagged as registered nowhere — and `governor_signal` is actively
emitted (`internal/daemon/movementgovernor.go:346`, with the error discarded). **Not my
call, but note the shape:** these seven are exactly the types that escape both the
registry table and the compat table, and `TestEV029_CompatTableCoversAllRegisteredTypes`
cannot see them because it iterates registered types, and they are not registered.

**C. The five deny-list rows are unchanged and still correct.** `agent_message` (only an
unexported constant, in `internal/digest/resolver.go:162` — the fix points outward, give
`core` the constant); `agent_presence` and `session_keeper_config_rejected` (no constant
anywhere, and inventing one has consequences in `eventtype_coverage_gjyks_test.go`'s
`session_keeper_*` carve-outs); `agent_input_acked` / `agent_input_stale` (constants exist
only in `internal/codexinput`, and `core` must not import it — 48 dependents, zero
dependencies).

**D. `LookupTypeSchemaVersion`, `RegisterEventType` and `RegisterEventTypeAtVersion` still
take `string`.** Deliberately not widened; different blast radius, `internal/workers`
calls them with literals. But note these three are now the remaining untyped surface of
the event registry, and widening them is the natural next wave *if* the goal is a typed
registry API rather than more edges.

**E. Sharing a vocabulary does not create an edge — measured, not assumed.** The
`eventreg <-> pertypecompat` pair is still 0 after both files were joined to
`eventtype.go`. If the research goal is to make co-changing files visible to a clustering
algorithm, **phase A joins consumers to a shared definition but leaves the consumers
mutually invisible.** This is the graph behaving correctly; it is the *metric* that may
need a two-hop or shared-neighbour notion. Combined with `02_xray-result`'s finding that
VOCAB is priced at 0.2 and cannot move a partition at any sane weight, the honest position
after two waves is: **phase A demonstrably improves graph fidelity and has not yet been
shown to improve any plan.**

---

## What you would tell the next agent

1. **Verify the hazard list yourself; assume it is illustrative, not exhaustive.** The
   handed-down list of collisions had three entries and the real number was five. Ninety
   seconds of systematic audit. If a plan gives you known-bad cases, the first question is
   whether anyone enumerated them or merely encountered them.

2. **The cheap mitigation did the work; the expensive one is insurance.** Building the
   substitution table from the single authoritative file prevented every wrong substitution.
   Typing the field caught nothing today. **Do both anyway** — but budget them honestly:
   the preventive one is free and mandatory, the detective one costs a commit and a
   test-file ripple and pays out later. Do not let a plan justify the type change on the
   grounds that it will catch something during the wave. It probably will not.

3. **Derive the ripple by compiling, not by grepping.** The test-file conversion set was
   12 sites and most were `map[string]` keys and `t.Run` names — invisible to any search
   for the changed signature. Make the type change, read the compiler errors, fix, repeat.
   One iteration. Reading the file first and predicting the set would have been slower and
   wrong.

4. **Keep the two-commit split.** Third wave running, third clean decomposition: +177 =
   175 VOCAB (substitutions) + 2 TYPEREF (signature). Every edge attributable to a named
   piece of work. A squashed commit yields one number and no structure, and the binary-hash
   post-condition becomes inapplicable because it only holds across the substitution
   commit.

5. **`-count=1` on every `go test`, and the binary hash on every pure substitution.** Both
   inherited from stage 2, both used, both cheap. The hash is the strongest check available
   for a literal->constant swap and it is two commands.

6. **Do not report a bare PASS from `verify_vocab_swap.py`.** It compares value *sets*, so
   it is blind to a same-valued decoy unless `--consts` happens to exclude the decoy. State
   the `--consts` argument alongside the PASS, or the reader cannot tell how strong the
   check was.

7. **If the next wave's goal is to move a plan rather than to add edges, phase A is
   probably the wrong instrument.** Two waves have now produced 353 new edges between
   files in the *same package*, at a tie price the clustering step is configured to
   disregard. The registry and compat table are still in different communities from the
   taxonomy, and still have zero edges to each other. The obvious next test is a
   *cross-package* A1 instance, where a package-level metric like `mq` is at least
   structurally capable of seeing the change.
