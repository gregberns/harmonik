# Plan: xray — phase-A enrichment of the event-type vocabulary

## Objective

Replace the string literals in `internal/core/eventreg_hqwn59.go` with the `EventType`
constants they already duplicate, so that the dependency between the event registry and
the event-type taxonomy becomes something the compiler and a static symbol graph can both
see — and measure whether that actually happens.

## Status

active — stage 1 and stage 2 are ready to execute; stage 3 is gated on a measurement.

---

## Why this change, and why it is an experiment

`internal/core` currently maintains **one vocabulary of 179 event-type names in four
places**:

| # | file | how it spells a type name | maintained by |
|---|---|---|---|
| 1 | `eventtype.go` | `EventTypeRunStarted EventType = "run_started"` | the constant |
| 2 | `eventreg_hqwn59.go` | `mustRegister("run_started", ...)` | a string literal |
| 3 | `pertypecompat_hqwn38.go` | `{TypeName: "run_started", ...}` | a string literal |
| 4 | `eventtype_coverage_gjyks_test.go` | `{EventTypeRunStarted, ctor}` | a hand-kept table |

Nothing connects 1, 2 and 3. The compiler sees no relationship at all; a symbol graph
built from the type checker records **zero edges** between any pair of those files. What
holds them together is (4): a test table whose own comment says *"The list mirrors
`eventreg_hqwn59.go`'s mustRegister calls. Keep the two in sync."*

That is the shape this experiment is about. The files behave as one unit — git history has
them changing in the same commit up to **53 times** — while every tool that reads the code
statically reports three unrelated files. A test was written to catch the drift that a type
would have prevented for free.

**The prediction being tested.** If the literals in file 2 are replaced by references to the
constants in file 1, real edges appear between them, and the coupling that was previously
visible only in git history becomes visible to static analysis. If that does not happen, a
core assumption behind the whole `codebase-organism` phase ordering is wrong, which is worth
knowing cheaply.

Measured baseline at `dee6286d5`, on this worktree, before any change:

```
internal/core/eventreg_hqwn59.go   <-> internal/core/eventtype.go          0 edges
internal/core/pertypecompat_hqwn38.go <-> internal/core/eventtype.go       0 edges
internal/core/eventreg_hqwn59.go   <-> internal/core/pertypecompat_hqwn38.go  0 edges
```

Full baseline, with the commands that produced it:
`/Users/gb/github/codebase-organism/refactor/01_xray-baseline_2026-08-03.md`.

---

## Stage 0 — confirm the starting state (do this first)

```sh
cd /Users/gb/github/harmonik-wt/xray
git status --porcelain          # expect: only untracked plans/2026-08-03-xray-phase-a/
git rev-parse --short HEAD      # expect: dee6286d5
go build ./...                  # expect: clean, no output

# THE GATE — 8 seconds, green at baseline. This is what you run after every stage.
go test ./internal/core/... ./internal/replay/... ./internal/eventbus/... ./internal/handlercontract/...
```

**Do not use `go test ./...` as your gate.** It takes ~10 minutes on this machine and is
**not green at baseline**: five packages and about thirty named tests fail before you touch
anything, and most are timing- or environment-sensitive integration tests (`internal/daemon`
cold-start tokens with 30-second waits, `internal/keeper` cooldown timing, `internal/workers`
sweep-rate assertions, plus two unrelated failures in `cmd/harmonik` and a deliberately-broken
eval fixture in `evaltasks/`). None of them are yours and none of them will tell you anything
about this change.

The gate above is scoped to the actual blast radius: the package you are editing plus the
three that consume its event registry. Stages 1 and 2 are pure compile-time type changes —
the value handed to `RegisterEventType` is byte-identical before and after — so a green build
plus these four packages is the honest check, and stage 2 adds a value-set proof on top.

**Known environment flake.** This machine's Go build cache is being cleared underneath
running builds. If you see errors of the form:

```
could not import fmt (open .../Library/Caches/go-build/xx/xxxx-d: no such file or directory)
```

that is the machine, not your change. Re-run the same command. Never conclude anything
from a single such failure.

---

## Stage 1 — type the two registration helpers

One commit. Nothing else.

In `internal/core/eventreg_hqwn59.go`, change the two helpers at the bottom of the file so
they take `EventType` rather than `string`, converting once at the boundary to the
registry's own API:

```go
func mustRegister(typeName EventType, ctor func() EventPayload) {
	if err := RegisterEventType(string(typeName), ctor); err != nil {
		panic("core: mustRegister: " + string(typeName) + ": " + err.Error())
	}
}

func mustRegisterAtVersion(typeName EventType, ctor func() EventPayload, version int) {
	if err := RegisterEventTypeAtVersion(string(typeName), ctor, version); err != nil {
		panic("core: mustRegisterAtVersion: " + string(typeName) + ": " + err.Error())
	}
}
```

**No call site needs to change.** Every existing call passes an untyped string constant,
which converts to `EventType` implicitly. That includes the one call in
`internal/core/eventreg_wkzlc.go`. This is why the stage is separable: it is a type
change with a zero-line ripple, so if anything does break, the cause is unambiguous.

Do **not** change the exported `RegisterEventType` / `RegisterEventTypeAtVersion`
signatures. They are called from `internal/workers` with literals, and widening the
change there is a different decision on a different blast radius.

**Post-conditions (all must hold before committing):**

1. `go build ./...` clean.
2. The stage-0 gate is green.
3. `git diff --stat` names exactly one file, `internal/core/eventreg_hqwn59.go`.

Commit message: `refactor(core): type the event registration helpers as EventType`

---

## Stage 2 — replace the literals with the constants

One commit. This is the measured change.

**The rule.** For every `mustRegister("x", ...)` and `mustRegisterAtVersion("x", ..., n)`
call in `internal/core/eventreg_hqwn59.go`, replace `"x"` with the identifier of the constant
**declared in `internal/core/eventtype.go`** whose declared value is exactly `"x"`.

Derive the mapping from the source, not from the constant's name — the naming is regular but
you must not rely on that:

```sh
# every literal at a registration call site
grep -oE 'mustRegister(AtVersion)?\("[^"]+"' internal/core/eventreg_hqwn59.go \
  | grep -oE '"[^"]+"' | sort -u        # expect 179 distinct

# the constant table: name -> value
grep -oE 'EventType[A-Za-z0-9_]+ +EventType += +"[^"]+"' internal/core/eventtype.go
```

**Expected counts, and they are load-bearing: 174 substitutions, 5 left alone.**

If your derived mapping does not come to 174 + 5, stop and report rather than proceeding.

### The five exceptions, and why each is different

This matters more than the 174. A scanner reported all of these as "literal duplicates a
declared constant". They are four different situations and only one of them is work.

| line | literal | what the "duplicate" actually is | what to do |
|---|---|---|---|
| 247 | `agent_message` | `eventTypeAgentMessage` — **unexported**, declared in `internal/digest/resolver.go` | **Leave it.** It is not reachable from `core`. The finding is real but points the other way: `digest` privately re-declared a name that `core` never gave a constant. Report it. |
| 250 | `agent_presence` | nothing — no constant exists anywhere | **Leave it.** Report the gap. |
| 532 | `session_keeper_config_rejected` | nothing — no constant exists anywhere | **Leave it.** Report the gap. |
| 572 | `agent_input_acked` | `codexinput.EmitInputAcked`, type `codexinput.EmitType`, **another package** | **Leave it. Substituting is actively harmful** — see below. |
| 573 | `agent_input_stale` | `codexinput.EmitInputStale`, same | **Leave it.** Same reason. |

**Why lines 572–573 are a trap.** `internal/core` does not import `internal/codexinput` and
must not start. `core` has 48 dependents and zero dependencies; adding an import to a leaf
package would invert the dependency direction of the most-depended-on package in the repo to
save two string literals. The constants happen to hold the same text because the two packages
describe the same wire event from opposite ends — that is a coincidence of value, not a shared
declaration. This is the clearest available example of *a correct detection whose fix would be
wrong*, and it is sitting in the file you are editing.

**Do not invent the two missing constants** (250, 532). Both are registered event types with
payload types and compat-table rows that were never added to the `EventType` enum. Adding one
has consequences in `eventtype_coverage_gjyks_test.go`, whose cohort table has explicit
carve-out rules for the `session_keeper_*` family. That gap is a finding for the report.

**Comments.** Leave the surrounding comments alone. Many name the event in prose
(`// agent_presence (hk-djqc9, ...)`) and the §8 durability tables reference the wire names.
Those are documentation of the wire format and should keep saying the wire name.

**Post-conditions (all must hold before committing):**

1. `go build ./...` clean.
2. The stage-0 gate is green.
3. Exactly **five** string literals remain as the first argument of a registration call:
   ```sh
   grep -cE 'mustRegister(AtVersion)?\("' internal/core/eventreg_hqwn59.go   # expect 5
   ```
4. `git diff --stat` names exactly one file.
5. **No value changed.** The compiler cannot catch the one dangerous error here —
   substituting `EventTypeRunCompleted` where `EventTypeRunFailed` belonged compiles fine and
   silently re-registers an event under the wrong name. Prove it did not happen:
   ```sh
   cd /Users/gb/github/harmonik-wt/xray
   git show dee6286d5:internal/core/eventreg_hqwn59.go > /tmp/xr/eventreg-before.go
   python3 /Users/gb/github/codebase-organism/refactor/verify_vocab_swap.py \
     --before /tmp/xr/eventreg-before.go \
     --after  internal/core/eventreg_hqwn59.go \
     --consts internal/core/eventtype.go \
     --call 'mustRegister(?:AtVersion)?'
   ```
   Must print `PASS — the registered value set is unchanged` and exit 0. It has been dry-run
   against a deliberately mis-substituted copy and catches it. **This post-condition is not
   optional and is not satisfied by the tests passing.**
6. **The graph moved.** Run from the research repo and paste the output into the report:
   ```sh
   cd /Users/gb/github/codebase-organism
   go build -o /tmp/xr/typegraph ./typegraph/cmd/typegraph
   /tmp/xr/typegraph -repo /Users/gb/github/harmonik-wt/xray -out /tmp/xr/after ./...
   python3 refactor/edgecheck.py \
     --graph /tmp/xr/after_symbols.json --before /tmp/xr/base/all_symbols.json \
     --pair internal/core/eventreg_hqwn59.go internal/core/eventtype.go
   ```
   Expected: VOCAB edges appear where there were none. The prediction is directional, not a
   specific count — **report the number you get, whatever it is.** A result of zero is a
   valid and valuable outcome and must be reported as such, not worked around.

Commit message: `refactor(core): register event types by constant, not by literal`

**Then stop.** Do not begin stage 3.

---

## Stage 3 — `pertypecompat_hqwn38.go` (gated — do not start)

Held deliberately. Stage 2's measurement decides whether this happens and in what form.

Sketch, for the record: `PayloadCompatEntry.TypeName` becomes `EventType`,
`LookupPayloadCompatEntry` takes `EventType`, and the 178 literals in the table become
constant references. Known blast radius is three files —
`pertypecompat_hqwn38.go`, `pertypecompat_hqwn38_test.go`, and one call site at
`internal/replay/replay.go:247` which passes `ev.Type` and would need an explicit
conversion.

---

## Hard stops — stop and write the report instead of continuing

1. The change ripples outside `internal/core`. It should not; stage 1 is designed so it
   cannot. If it does, that is information, not an obstacle to route around.
2. A test in the stage-0 gate starts failing.
3. Your derived literal→constant mapping does not come to 174 + 5.
4. Any literal maps to more than one constant in `eventtype.go`, or two constants there share
   a value.
5. `verify_vocab_swap.py` does not print PASS.
5. You find yourself wanting to change `eventtype.go`, `pertypecompat_hqwn38.go`,
   `event.go`, or any test file. All are out of scope for stages 1–2.

---

## Explicitly out of scope

**`Event.Type` is the root cause and it is not being fixed here.** `internal/core/event.go:47`
declares the central envelope field as `Type string`, with the comment:

> *"The EventType enum is declared in a separate bead (hk-hqwn.59); this field uses string
> until that enum lands (non-breaking hoist)."*

The enum landed. The hoist never happened. Because the carrier field is `string`, nothing
downstream is ever forced to hold an `EventType`, which is why registration, the compat
table and comparison sites all drift back to literals. Changing it touches ~200 sites
across the repo and is a separate decision for the operator — record it in the report,
do not act on it.

Also out of scope: all five exceptions in the stage-2 table; the `allEventTypeCohort` test
table, which becomes partly redundant once registration is typed but still checks
constructors; `internal/workers`' direct `core.RegisterEventType` calls; and every test
failing at baseline outside the stage-0 gate.

---

## Done means...

1. `internal/core/eventreg_hqwn59.go` contains exactly five string literals in registration
   calls, all five named in the stage-2 exceptions table. Verified by
   `grep -cE 'mustRegister(AtVersion)?\("' internal/core/eventreg_hqwn59.go` returning `5`.
2. `mustRegister` and `mustRegisterAtVersion` take `EventType`. Verified by
   `grep -n 'func mustRegister' internal/core/eventreg_hqwn59.go`.
3. `go build ./...` is clean and the stage-0 gate is green. Verified by running both.
3a. The registered value set is provably unchanged. Verified by
   `verify_vocab_swap.py` printing PASS.
4. Exactly two commits exist on `work/xray` above `dee6286d5`, one per stage, each touching
   one file. Verified by `git log --oneline --stat dee6286d5..HEAD`.
5. The edge count between `eventreg_hqwn59.go` and `eventtype.go` has been measured after
   the change and recorded in the report — **whatever the number is.** Verified by the
   `edgecheck.py` output being pasted into `report.md`.
6. `plans/2026-08-03-xray-phase-a/report.md` exists and covers every heading listed below.

---

## The report — write this, it is half the deliverable

Create `plans/2026-08-03-xray-phase-a/report.md`. It is the channel back; the point of this
exercise is as much what you learned as what you changed. Headings:

- **What was done** — the two commits, files, line counts.
- **Measurement** — the `edgecheck.py` before/after output, pasted verbatim.
- **Effort** — roughly how long each stage took and how much of it was mechanical versus
  judgement. This calibrates a cost model that is currently invented.
- **Surprises** — anything the plan got wrong, anything the counts did not match, anything
  that took judgement where the plan implied a rule. **This is the most valuable section.**
  The plan was written by someone who read the file but did not edit it.
- **Findings not acted on** — things you noticed and correctly left alone.
- **What you would tell the next agent.**

Do not commit `report.md` unless asked; leave it in the working tree.
