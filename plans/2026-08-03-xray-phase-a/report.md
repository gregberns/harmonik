# Report — xray phase-A

Branch `work/xray`, worktree `/Users/gb/github/harmonik-wt/xray`, base `dee6286d5`.
Stages 1 and 2 complete. Stage 3 not started.

---

## What was done

Two commits, one file each, in plan order. Both touch only
`internal/core/eventreg_hqwn59.go`.

| commit | subject | diff |
|---|---|---|
| `1e700ea4f` | `refactor(core): type the event registration helpers as EventType` | 1 file, +6 / -6 |
| `c96d1efbb` | `refactor(core): register event types by constant, not by literal` | 1 file, +174 / -174 |

Stage 1 changed the two unexported helpers `mustRegister` and
`mustRegisterAtVersion` to take `EventType` and convert once at the boundary.
Zero call sites needed a change, exactly as the plan predicted, including the
one in `eventreg_wkzlc.go`. `RegisterEventType` and `RegisterEventTypeAtVersion`
keep their `string` signatures.

Stage 2 replaced 174 of the 179 registration-call literals with the constant
declared in `eventtype.go` that holds that exact value. Five literals stay. The
five are the five the plan named, at the lines the plan named (247, 250, 532,
572, 573). Comments were not touched.

Post-conditions, all met:

```
go build ./...                                                    clean
go test -count=1 <the four gate packages>                          all ok
git diff --stat (each commit)                                      exactly 1 file
grep -cE 'mustRegister(AtVersion)?\("' .../eventreg_hqwn59.go      5
grep -n 'func mustRegister' .../eventreg_hqwn59.go                 both take EventType
git log --oneline dee6286d5..HEAD                                  exactly 2 commits
```

The mapping was derived from source, not from naming, and came to exactly
174 + 5. No literal mapped to more than one constant. No two constants in
`eventtype.go` share a value. No literal appears twice at a call site.

### Value-set proof

```
constants available: 182
before: 179 calls (179 literal, 0 constant)
after : 179 calls (5 literal, 174 constant)
PASS — the registered value set is unchanged
EXIT=0
```

---

## Measurement

`edgecheck.py`, after-graph against `/tmp/xr/base/all_symbols.json`:

```
internal/core/eventreg_hqwn59.go  <->  internal/core/eventtype.go
  before: 23 decls <-> 184 decls | 0 edges (0 refs) {}
  after : 23 decls <-> 184 decls | 176 edges (176 refs) {'VOCAB': 174, 'TYPEREF': 2}
  delta : +176 edges {'VOCAB': 174, 'TYPEREF': 2}
```

**The prediction holds.** Zero edges became 176. The decomposition is exact and
attributable: 174 VOCAB edges, one per substitution, plus 2 TYPEREF edges — the
two helper signatures from stage 1. Nothing is unaccounted for, and the two
stages are separately visible in the result, which is what the two-commit split
bought.

The other two pairs were also measured, and both are unchanged, confirming the
change did not leak into stage 3's territory:

```
internal/core/pertypecompat_hqwn38.go  <->  internal/core/eventtype.go
  delta : +0 edges {}

internal/core/eventreg_hqwn59.go  <->  internal/core/pertypecompat_hqwn38.go
  delta : +0 edges {}
```

So after phase A, one of the three invisible relationships is now visible and two
still are not. The registry is joined to the taxonomy. The compat table remains
joined to neither.

Whole-repo graph after the change: 866 files, 8985 nodes, 21570 edges, 0 load
errors.

---

## Effort

Roughly 25 minutes wall clock, of which maybe 8 was the actual change.

| stage | time | mechanical vs judgement |
|---|---|---|
| 0 — baseline confirm | ~2 min | fully mechanical. The gate is ~41s wall, not 8s — see Surprises. |
| 1 — type the helpers | ~3 min | fully mechanical. One edit, two signatures. |
| 2 — derive the mapping | ~3 min | mechanical, but *only because it was scripted*. The judgement was in deciding to derive from values rather than names, which the plan already made for me. |
| 2 — apply 174 substitutions | ~1 min | fully mechanical, one regex pass keyed on the derived value→name map. |
| 2 — verification + measurement | ~6 min | mechanical. Typegraph rebuild and run is the long pole (~2 min). |
| unplanned — chasing the cache anomaly | ~7 min | pure judgement, and not in the plan. See Surprises. |
| report | ~5 min | judgement. |

The important cost observation: **the 174 substitutions were the cheapest part of
the whole exercise.** Applying them took one scripted pass. Everything expensive
was verification, measurement, and one anomaly investigation. A cost model that
prices this kind of work per-substitution will be wrong. Price it per-file, plus
a fixed verification cost, plus whatever the anomalies cost — and the anomalies
are the variable that is hard to predict.

The five exceptions cost almost nothing to honour, because the plan's rule
("the constant declared in `eventtype.go` whose value is exactly this") is
*self-enforcing*: none of the five has a constant in that file, so a script
implementing the literal rule leaves all five alone without knowing they are
special. That is a well-designed rule. It would not have protected me if one of
the five had also existed in `eventtype.go`.

---

## Surprises

**1. The gate as written can report green without running your change.** This is
the one that matters. Immediately after the stage-1 edit, the plan's gate command
printed:

```
ok  github.com/gregberns/harmonik/internal/core  (cached)
```

for the package I had just edited. Re-running the identical command with
`-count=1` ran it fresh, and it was green — so the change was fine. But the gate
had told me "green" without executing anything. I used `-count=1` for every gate
run thereafter.

I probed this and could not fully explain it. Appending a bare comment to the
file also yields `(cached)`, which is legitimate Go behaviour — a comment does
not change the compiled output, so the cached result genuinely applies. But the
stage-1 edit did change the compiled output (see finding 2), and it still
reported cached. I am reporting the observation and not inventing a mechanism.

**The actionable fix is one flag.** The plan's stage-0 gate should read
`go test -count=1 ./internal/core/... ...`. Without it, post-condition 2
("the stage-0 gate is green") is not reliably a check. This is exactly the class
of thing the experiment was meant to surface: the plan's author read the file
carefully, but a gate is something you only learn by running twice.

**2. Stage 1 and stage 2 compile to byte-identical binaries.** I built the
`internal/core` test binary at all three states:

```
base       (dee6286d5)  d875a9e4ab6993cba9c5c8d5ac95417c0daf2dea4c5a40c5e00c218e2f786975
stage 1    (1e700ea4f)  7c5e2fa92d6e1f5b6892d02326a8a375407c22c2201bab52d7d9c4adf3518904
stage 2    (c96d1efbb)  7c5e2fa92d6e1f5b6892d02326a8a375407c22c2201bab52d7d9c4adf3518904
```

Stage 2 — the 174-substitution "measured change" — produced a **bit-for-bit
identical binary** to stage 1. This is a stronger statement than
`verify_vocab_swap.py` makes. That script proves the registered value *set* is
unchanged; the hash proves the entire compiled package is unchanged, which
subsumes it. It is worth adding as a cheap extra post-condition for any future
literal→constant stage, because it is a two-command check that catches value
substitution errors, ordering errors, and anything else that touches codegen:

```sh
go test -c -o /tmp/a.test ./internal/core/   # before
go test -c -o /tmp/b.test ./internal/core/   # after
shasum -a256 /tmp/a.test /tmp/b.test         # must match
```

Note the asymmetry: base → stage 1 *did* change the binary, even though that
change is also semantically inert. The `EventType` parameter type shows up in the
binary. So this check works for stage 2's shape (substituting constants for their
own values) but not for stage 1's shape (changing a signature type).

**3. The gate is ~41 seconds, not 8.** The plan and the handoff both say 8
seconds. The reported per-package test times do sum to about 8 seconds, so the
number is not wrong — it is just not the number you experience. Wall clock at
baseline was 41s, because compiling the test binaries dominates. Not a problem,
but if a future plan uses "8 seconds" to argue the gate is cheap enough to run
constantly, the real figure is five times that on a cold cache.

**4. Nothing else in the plan was wrong.** Every count, every line number, every
claim about the five exceptions, and the "zero call sites change" prediction all
held exactly. The five exceptions are at lines 247, 250, 532, 572, 573 as
written. `internal/core` does not import `internal/codexinput`.
`eventTypeAgentMessage` is at `internal/digest/resolver.go:162` and is
unexported, as described. For a plan written without editing the file, that is a
high hit rate — the two real surprises are both about the *process* (the gate,
the verification), not about the code.

---

## Findings not acted on

**A. Eight declared event-type constants are never registered in
`eventreg_hqwn59.go`.** `eventtype.go` declares 182 constants; only 174 are
consumed by the registry. The other eight split into two groups:

- Five are registered from `internal/workers`, by direct `core.RegisterEventType`
  calls with **string literals**, in `telemetry.go`, `tunnelfailed.go`,
  `offline.go`, `health.go`, `breach.go` (`worker_report`,
  `worker_tunnel_failed`, `worker_offline`, `worker_unhealthy`,
  `resource_breach`). These are the same duplication this change removed, just
  in a different package. The plan explicitly scopes them out. They are five
  one-line changes and would need `RegisterEventType`'s signature widened, which
  the plan rules out for good reason.
- Three appear to be registered nowhere at all: `governor_signal`,
  `loop_observed_phantom_done`, `run_stale`. Each has a declared constant and one
  or more non-test usages, but no registration call anywhere in the repo. I did
  not investigate whether that is a real gap or whether these types are emitted
  through a path that does not need registration. Worth someone's ten minutes.

**B. The two missing constants remain missing.** `agent_presence` and
`session_keeper_config_rejected` are registered event types with payload types
and no entry in the `EventType` enum. Not invented, per the plan.

**C. `agent_message` is declared privately in the wrong direction.**
`internal/digest/resolver.go:162` declares `const eventTypeAgentMessage =
"agent_message"` — a package-private re-declaration of a name `core` registers
but never gave a constant. The fix is to add the constant to `core` and have
`digest` use it, not to touch `core`'s registration. Left alone.

**D. `Event.Type` is still `string`.** `internal/core/event.go:47`, with the
stale comment saying the enum lives in a separate bead and this field uses
`string` "until that enum lands". The enum landed. This is the root cause of the
whole pattern — because the carrier is untyped, nothing downstream is ever forced
to hold an `EventType`, so every new site drifts back to a literal. ~200 sites.
Operator decision, recorded not acted on.

**E. Two of the four vocabulary copies are still joined by nothing.** The compat
table and the coverage test table are unchanged, and the measurement above
confirms both still have zero edges to the taxonomy. Phase A fixed one of three
invisible relationships.

---

## What you would tell the next agent

1. **Use `-count=1` on the gate, always.** It is the single most important thing
   in this report. The gate lied to me once and I nearly took it at face value.
   If you inherit a plan whose post-condition is "tests green", make sure the
   command can actually fail.

2. **Add the binary-hash check to stage 3.** For a pure literal→constant
   substitution, `shasum` on the compiled test binary before and after is a
   complete proof and takes two commands. Use it *alongside*
   `verify_vocab_swap.py`, not instead — the script is the one that tells you
   *which* value moved when it fails, and it works across signature changes where
   the hash check does not.

3. **Script the substitution, do not hand-edit it.** Derive the value→constant
   map by parsing both files, then do one regex pass over the call sites keyed on
   that map. This makes the five exceptions fall out automatically — they have no
   constant in `eventtype.go`, so the script skips them without needing to know
   they are special. Hand-editing 174 lines invites exactly the silent
   wrong-constant error that the whole verification apparatus exists to catch.

4. **The exceptions table was worth reading first, and cost nothing to honour.**
   But be aware the protection was structural, not vigilance-based. If stage 3
   has an exception that *does* have a matching constant in the source file, the
   self-enforcing rule will not save you and you will need a real deny-list.

5. **Stage 3 has a shape stage 2 did not.** Stage 2 was a leaf change — 179 call
   sites in one file, zero ripple. Stage 3 changes an exported struct field's
   type (`PayloadCompatEntry.TypeName`) and an exported function signature
   (`LookupPayloadCompatEntry`), which means the "byte-identical binary" check
   will *not* apply, the ripple is real (`internal/replay/replay.go:247` needs an
   explicit conversion), and the gate becomes load-bearing in a way it was not
   here. Budget more for it than the 174-vs-178 line counts suggest.

6. **The measurement is worth the two-commit discipline.** Because stage 1 and
   stage 2 were separate commits, the +176 decomposed cleanly into 2 TYPEREF
   (stage 1) + 174 VOCAB (stage 2), and every edge is attributable to a specific
   line of work. If they had been squashed the result would have been a single
   number with no internal structure. Keep doing this.
