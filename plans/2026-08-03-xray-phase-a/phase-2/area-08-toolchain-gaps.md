# Area 8 — the four toolchain gaps that make a generated plan unsafe to hand to agents

**This area is work in `codebase-organism`, not in harmonik.** It is here because it is the answer to
"what stops us pointing agents at the generated worklist today", and because three of the four items are
small enough to be one task each.

Ordered by how much they block.

---

## 1. `Finding` has no `files []string` — 226 operations under-declare their locks

**Blocks: area 4 entirely.**

An A2 finding is reported once per *value* across all its sites, deliberately, because the fix is one
decision rather than N conflicting tasks. The record keeps only the first site's file. **550 of 653 A2
findings (84%) name more than one file in their evidence; one names 68.**

So `plan` emits 226 operations that each declare one file lock and will in fact edit up to sixty-eight, and
`opdag` will schedule two of them concurrently on a file both are rewriting. The exclusion half of the
model — the half that *binds*, per the scheduler's own output — is being fed a number the detector already
knows to be wrong and has no field in which to state it.

**Fix:** add `files []string` to `Finding` alongside `file`; populate from A2's site list; have `plan` use
`files` for `Op.Files` when present. One field, three call sites. **Verify:** an A2 operation's declared
lock set equals the set of files its evidence names, and `opdag`'s ceiling drops to the honest number.

---

## 2. Phase C cannot enter a plan, so no generated plan can ever widen

**Blocks: the entire economic argument the scheduler exists to make.**

`funcseam` and `seam_finder` compute extraction candidates — catalog C1 and C2 are marked built — but they
emit their own schemas. **Nothing converts them into `Finding` records.** `plan`'s mapping also has no row
for `Creates`.

The consequence is structural, not cosmetic. Every operation a generated plan contains is an edit to an
existing file, so the concurrency profile starts at its maximum and decays monotonically:
`399 → 363 → 342 → … → 3 → 1`. The profile the scheduler was built to model is the opposite shape —
`1×3 → 3 → 5 → 3 → 1`, narrow at the front, **widening as cuts create files** — and the headline it exists
to demonstrate is that *a serial cut is not a cost to minimise, it is a purchase*. What it buys is exclusion
zones, and the return is however much phase-E work then runs in parallel on the files it created.

**None of that is reachable through the current pipeline.** The one phase predicted to be *narrow* is the
one phase the pipeline cannot produce.

**Fix:** emit `funcseam` (C1) and `seam_finder` (C2) output as `Finding` records carrying `creates`, and
add the `Creates` row to `plan`'s mapping. **Verify:** a generated plan's concurrency profile widens
somewhere.

---

## 3. No cross-file precedence edge is derivable, so the precedence floor is decoration

**Blocks: expressing "port first, then extract" — the one precedence relation confirmed twice.**

`plan` derives precedence from the `phase` field at file granularity: 372 edges on harmonik at
`d5a12348f`. **Every one of them is intra-file, and two operations on one file already exclude each other**,
so every derived chain is a subset of the serial work that file forces anyway. Measured: precedence floor
187.0, exclusion floor 279.0, and the 187 is a *sub-sum* of the 279 on the same file
(`internal/projectconfig/projectconfig.go`). This is a theorem, not a coincidence, and there is a test
guarding it.

The phase field says *when in the loop*, not *what depends on what*. It cannot produce the edge that
matters: **a `create-port` operation in one file that an extraction in another file depends on.** A
cross-file edge is the first edge capable of binding, and none exists.

**Fix:** derive one real cross-file edge — the port/extraction relation — and re-run. **Verify:** the
precedence floor exceeds the exclusion floor for at least one plan, i.e. the constraint finally binds.

---

## 4. The A2 detector counts struct tags, inflating every A2 number by ~36%

**Found 2026-08-09 while writing area 4. Not previously recorded anywhere.**

`json:"run_id"` is a string literal in the AST, so the A2 detector reports it as a repeated literal with no
constant — 151 sites across 68 files, **the single largest A2 finding in harmonik.** It is not fixable: Go
struct tags must be compile-time literals in the tag position and cannot reference a constant.

Measured at `d5a12348f`: **233 of 653 A2 values (1,913 of 4,895 sites) are struct tags.** Every A2 count
this project has published is inflated by roughly a third, including the totals in this directory's README.

**Fix:** skip literals appearing in a `StructTag` position. Exact, cheap, no threshold. **Verify:** A2
falls from 653 values to ~420, and `json:` no longer appears in the output.

*Secondary, and a judgement call rather than a defect:* 67 more values (728 sites) are CLI flag strings —
`--project=`, `--help`, `--json`. A flag name is arguably already a name. Consider a reported sub-class
rather than a suppression, so the operator decides.

---

## Not a defect, but the largest gap in the loop

**No scout agent has ever been run.** Step 3 of the wave protocol — agents examining a segment and
returning findings in *the same schema the detector emits*, so mechanical and judgement findings land in
one worklist and are triaged together — is a design that has never been exercised. The shared schema is the
whole trick and it is entirely untested.

Area 3's ambiguous bucket is the natural first exercise: 724 findings that the detector explicitly refuses
to resolve, each needing exactly the judgement a scout is for, and each producing a record that must merge
with the detector's own output. If the schema composes, that is a result. If it does not, better to find
out on 724 records than after building a wave around the assumption.
