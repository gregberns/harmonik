---
id: ratchet-scheme-migration
title: The ratchet must be able to express a key-scheme migration, or it forbids every repair of itself
type: task
priority: 0
labels: [lint, gate, ratchet, clear-the-ground]
depends_on: []
blocks: [lint-rekey-exclusion-list]
workstream: W1
batch: 1
---

> **A `blocks:` line here enforces nothing. Dispatch reads bead edges.** This task is
> `hk-iw11o`. Three beads block on it in the ledger: `hk-lint-rekey-exclusion-list-t9ebz`,
> `hk-e0ybh`, and `hk-h4h78` transitively. If you add a `blocks:` entry, run `br dep add` in the
> same change.

## Problem

`scripts/lint-allow-ratchet.sh` fails on **any added pair** — the allow list may only shrink. A
change to how findings are KEYED re-hashes essentially every row, so every row reads as added.

> **You cannot change a keying scheme under a ratchet that forbids any row from changing.**

Measured: today's ratchet on a faithful re-key reports **934 added pairs, exit 1**. The ratchet runs
in `core`, `fast` and `full` alike — `freeze-gates` calls it, `gate-static-product` calls
`freeze-gates` — so there is no gate it is absent from.

**This is not a theoretical corner. The gate is currently preventing three repairs of itself:**

- **`hk-lint-rekey-exclusion-list-t9ebz`** — the operator ruling of 2026-08-23 directs this programme
  to change the keying rule rather than teach the ratchet to detect renames. **The ratchet forbids
  the change that ruling asks for.**
- **`hk-e0ybh`** — `--uniq-by-line` defaults true and is set nowhere, so the report hides findings.
  Measured at `dd6dc578e`: 971 findings on, 1291 off, 320 masked. The list was SEEDED under the
  masking, so **328 identities are unlisted** and fixing the flag needs 328 new rows. Refused.
- **`hk-h4h78`** — the comparison window, blocked behind the re-key.

**Three rounds of repair to the re-key task file never touched this, because the obstruction was
never in the spec.** A spec defect and a gate defect look identical from inside the task file.

## Scope

`scripts/lint-allow-ratchet.sh` only. **`tools/lintreport`, the Makefile and
`scripts/lint-allow-ratchet-test.sh` are all out of scope and were untouched by the prototype.**

## The design — costed and prototyped 2026-08-25, so do not re-derive it

**Declare the key scheme in a header line in `allow.txt`, and branch on it.**

The header is free. `pairs()` begins `sub(/#.*/, "")` and `readAllow` does `SplitN(line, "#", 2)[0]`,
so a `# key-scheme:` line changes **no compared pair**. Confirmed both sides: 956 pairs, identical
sha with and without the header, at top of file and mid-file. **No tool change is needed to carry
it.**

Then:

| scheme version vs base | path |
|---|---|
| **unchanged** | today's cheap text compare, unmodified, every commit |
| **changed** | the transition check below |

### The transition check — it is sound, not a waiver

**(i) Every current finding's OLD-scheme digest is present in the OLD list.**
Implement it by building `tools/lintreport` **as it stands at the base** — `git archive <base>
tools/lintreport` into a throwaway stdlib-only module, 0.6s — and running it with the base
`allow.txt` (`git show`) against the CURRENT tree's report. **Exit 0 IS (i).** This is why there is no
re-implementation of the old hash for you to get wrong.

**(ii) Old-to-new is an INJECTION WITH A DIRECTION.** Every old row either maps to exactly one new
row, or is DROPPED as stale and reported. No new row exists that no old row maps to. **Shrink allowed
and printed, grow refused** — the same asymmetry the ratchet already has, expressed in finding space
rather than row-text space. `judge()` already computes and prints this as "N allow-list entries are
now clean".

> **DO NOT ASSERT A STRICT BIJECTION.** A row matching no current finding cannot be mapped, and
> stale rows are a legitimate state rather than an attack — there are 22 to 25 of them. A strict
> bijection fails on every one and reads as "this design does not work" when the check is merely
> mis-stated. This is the specific way this task produces a false negative.

**(i) is `-remap` rule 3, and it carries the whole soundness proof.** The refusal that looked like the
obstruction is what refuses a genuinely new tolerated finding: its old-scheme digest was never in the
old list. **Do not relax it.** The answer to it firing is to repair the tree, never to weaken the
rule. Relaxing it would have destroyed the proof before anyone discovered it was needed.

### The precondition this design implies, and it is not optional

> **A SCHEME MIGRATION REQUIRES A TREE THAT IS GREEN UNDER THE OLD SCHEME.**

(i) computes OLD-scheme digests of CURRENT findings, so it **inherits exactly the blindness the new
scheme exists to remove**. It cannot tell "this was tolerated before the code moved" from "this is
new debt", because telling those apart is what the new scheme is for and (i) does not have it yet.

That is not a flaw. Proving soundness against the old scheme is the only thing both sides of the
migration can agree on. But it means **any finding the judge refuses at the base will fail (i) and
block the migration**, and the only fix is to repair the tree — fix the finding, or make it
legitimately tolerated — BEFORE the migration commit.

**Measured 2026-08-25:** this bead landed green, and `hk-lint-rekey-exclusion-list-t9ebz` was still
blocked, because 14 findings were refused at the batch tip. "The judge refuses it" and "its digest is
absent from the list" are the same statement, so those 14 fail (i) by definition. **Do not read that
as (i) being too strict.** It is the gate correctly saying *repair the tree first*.

**This is NOT rename detection and it is inside the 2026-08-23 operator ruling.** It infers nothing
from paths, similarity or history. It recomputes from the tree.

## Done when

1. **A faithful re-key with a bumped scheme version passes.** Exit 0.
2. **A re-key carrying ONE genuinely new tolerated finding FAILS, refused by (i) specifically, and
   the offending finding is NAMED.** Not refused by a count. A check that fails for the wrong reason
   is worth about as much as one that passes for the wrong reason.
3. **Scheme unchanged behaves exactly as today:** nothing added exits 0, a pair added exits 1.
4. **`scripts/lint-allow-ratchet-test.sh` passes UNMODIFIED** — 16 assertions, 0 failures, identical
   to control. You are inserting a branch, not changing existing behaviour.
5. **Three further bypasses are refused:** a fabricated row matching no finding; a header bumped with
   the keying code unchanged; a migration that also edits a function carrying a tolerated finding.
   The third exits 1 under today's shipped ratchet too, so it is a pre-existing constraint rather
   than something you introduced.
6. **Stale rows do not fail the check.** Run it against a tree carrying the real stale rows and show
   they are dropped and reported rather than refused. This is item 2's mirror and it is what the
   bijection warning is about.

## Limits

- **Do not touch `tools/lintreport`, the Makefile, or the self-test.** The prototype needed none of
  them.
- **Behaviour on the cheap path must not change.** That path runs on every commit, so a regression
  there is a regression everywhere. **You may restructure the existing comparison** if branching
  reads better than wrapping — done-when 3 and 4 are what decide whether you got it right, not the
  shape of the diff.

  **An earlier version of this file said "179 lines inserted, zero modified, zero deleted" as a
  Limit. That was a mistake and it is withdrawn.** Those numbers describe how the PROTOTYPE happened
  to come out; they were never the property anyone cared about. The property is the line above. A
  measurement of how one implementation landed is not automatically a constraint on how another
  must, and writing it as one forces a reviewer to infer behaviour from a line count. See the
  "principles, not laws" rule in `AGENTS.md` — this file broke it.
- **Do NOT wire the report-reuse optimisation.** There is an optional ~3-line change reusing an
  existing lint report to cut 6.2s to 2.3s. **It cannot work as things stand** — the ratchet runs
  inside `gate-static`, which fires BEFORE `lint-allow`, so no report exists yet. And honouring an
  ambient-environment `LINT_ALLOW_REPORT` inside a gate is a trusted-input surface that is not worth
  4 seconds. If it is ever wired, honour the variable only from the Makefile.
- **Do not land `hk-ym2nn` first.** It deletes the dead `is_legacy` / `legacy_pairs` branch, which is
  the half-built precedent for this mechanism — the script already branches on which scheme the list
  is written in and lacks only the TRANSITION. **This task likely absorbs it**, because a declared
  version supersedes a format sniff. Deleting the reference implementation before building the thing
  it is a reference for is the wrong order.
- **Do not change the comparison window.** `hk-h4h78` owns it and stays after the re-key.

## Cost, measured — evidence that the change is small, NOT a shape you must reproduce

- **179 lines inserted, zero modified, zero deleted** in the prototype. **Descriptive, not
  prescriptive** — see Limits.
- Expensive path **6.2s wall / 15.1s CPU** standalone, warm.
- **It fires ONLY while the scheme header differs from the base** — the migration edit and the one
  commit that lands it. From the next commit it is the cheap text compare forever. **A one-off, not a
  recurring tax.**
