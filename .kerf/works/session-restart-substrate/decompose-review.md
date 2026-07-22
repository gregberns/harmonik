# Decompose Review — `02-components.md`

> Independent review of Pass 2 (Decompose) for the `session-restart-substrate` spec work.
> Reviewer: independent (autonomous mode). Date: 2026-07-13.
> Method: read 01-problem-space + 02-components; spot-checked every cited anchor against the
> live specs and `internal/core/eventreg_hqwn59.go`; verified `_registry.yaml` prefix freedom;
> swept for omitted-but-affected specs (pi-harness, credential-isolation, cognition-loop).

## Final verdict (2026-07-13, after revision): **Approved**

All five requested changes were applied to `02-components.md` and verified against the live
specs/code. Resolution log:
1. **Collision inventory widened** — lines 26–44 now enumerate all three "substrate" senses
   (process-spawn PL-021b/HC-056/PI-012a/CI-004; cognition CL-015/CL-024; transport/production
   CI §2.2/PI-069); the `replay-substrate`/RS rename is elevated to the leading Change-Design
   option; SB-R14 (105–111) requires three-way disambiguation under the keep-name option.
   pi-harness/credential-isolation recorded as "considered, not edited." **Resolved.**
2. **EV-U5 corrected** — lines 210–219 now frame §8.13 as a *real* collision (code squats §8.13
   which the spec assigns to `epic_completed`) and require the keeper cohort to land at a fresh
   §8.16 plus a code-comment fix; EV-U1 (186–189) pinned to the fresh number as a post-state.
   **Resolved.**
3. **Naming convention** — new EV-U1a (190–195) records the `session_keeper_*` decision; SK-R4
   (142–147) updated to the registered names. **Resolved.**
4. **"Considered, no edit"** for pi-harness/credential-isolation — present (42–44). **Resolved.**
5. **Post-state phrasing** — EV-U1, HC (229–231), ON (249–250) reworded to truths
   ("mutually discoverable"), not pointer-insertion instructions. **Resolved.**

Trivial residual (non-blocking, tidy in Change Design): SK-R5 (line 150) still uses the
`keeper_model_done` shorthand in an illustrative parenthetical; the registered catalog name per
EV-U1a is `session_keeper_model_done`. Prose, not a catalog reference — does not affect the
decomposition.

---

## Original verdict (pre-revision): Changes requested

The decomposition is strong on structure, traceability, and dependency ordering — all six
criteria are substantially met. But three substantive, propagating defects block approval:
(1) the "Substrate" naming-collision inventory is materially incomplete (the term is overloaded
3–4 ways, not one); (2) EV-U5 mischaracterizes a real §8-number collision as a "phantom"
section, which will mislead Research/Design; (3) the four NEW keeper events use a `keeper_*`
naming convention that silently conflicts with the 18 existing `session_keeper_*` types the same
change claims to reconcile into "a consistent catalog." All three are fixable in-place; none is
fatal to the decomposition's shape.

---

## Per-criterion assessment

### 1. Every affected spec has change-line + concrete requirements + dependencies — **PASS**
All seven rows (SB, SK, EV, HC, SH, ON, PL) carry a one-line change description, enumerated
requirements (SB-R1..R14, SK-R1..R11, EV-U1..U5, and pointer-scoped requirements for the four
reference touches), and an explicit dependency clause. Nothing is missing structurally.
Minor: the PL row's dependency is literally "none" and its requirement is "PL itself is not
rewritten" — it is a consistency *constraint on SK*, not an edit to PL. Listing it is fine
(documents the obligation) but it slightly inflates the "affected spec" count; that is honest,
not a defect.

### 2. Every goal 1–6 maps to ≥1 spec area; traceability real — **PASS**
The Goal→Area table is genuine, not hand-waved: each goal cites concrete requirement IDs
(Goal 1→SB-R1..R6/R11/R12; 2→SB-R7 + `internal/codextest`; 3→SK-R1/R2/R3 + SB-R8/R9;
4→EV-U1/U2 + SK-R4; 5→SK-R5/R6/R7/R8 + SB-R6; 6→SB-R10 + SK-R10). Constraints 1–6 are also
mapped. Verified against 01's §Goals — the mapping is accurate.

### 3. No unjustified spec area — **PASS**
Every listed area traces to a goal or to a consistency requirement of SB/SK. The four reference
touches (HC/SH/ON/PL) are each justified by a real anchor that SB or SK must stay consistent
with (HC-035 carve-out, SH-018 no-test-branch, ON-059 fragment, PL-021d write discipline) — all
confirmed present in the live specs.

### 4. Requirements state what is TRUE, not prose edits — **MOSTLY PASS**
SB-R* / SK-R* are correctly phrased as contract truths ("Defines…", "Requires…", "Encodes SR4
as a testable ordering invariant"). Two edges lean toward text-editing instructions and should
be reworded in Change Design (not blocking):
- **EV-U1** — "a new §8 sub-section following the §8.9 pattern" prescribes document structure
  and location rather than a truth about the catalog. State the truth ("the four types are
  registered as a numbered cohort satisfying every §8.9 criterion") and let Change Design pick
  the section number.
- **ON / HC touches** — "ON-059 gains a pointer to SK" / "HC-035 MUST reference SB" are
  pointer-insertion instructions. Acceptable for reference touches, but phrase as the post-state
  ("ON-059 and SK are mutually discoverable"; "the surface HC-035 disclaims is governed by SB").

### 5. All relevant specs accounted for — **FAIL (the main lever)**
Two genuine omissions, both tied to the overloaded word "substrate":

- **The naming-collision inventory is incomplete.** The artifact (lines 26–30, SB-R14) treats
  "Substrate" as colliding with exactly one prior concept — the tmux/subprocess **process-spawn
  seam** (`internal/handler.Substrate`, PL-021b, HC-056, PI-012a, CI-004). Verified accurate for
  that family. But "substrate" is used in **at least two further, distinct senses** that the
  analysis misses:
  - `cognition-loop.md` **CL-015 / CL-024** — "**substrate teardown**" = the flywheel *session*
    substrate (fresh-start recycle). A third meaning.
  - `credential-isolation.md §2.2 / line 44` — "the **LLM transport substrate** (raw Messages
    API vs pi-agent-core)"; `pi-harness.md` **PI-069** — "production **substrate** = paid." A
    fourth, transport/deployment meaning.
  Naming a bare top-level `specs/substrate.md` (prefix SB) into a namespace where the word
  already carries 3–4 normative meanings under-addresses the collision. SB-R14 as written only
  disambiguates from the process-spawn seam. **This materially strengthens the case for the
  deferred `replay-substrate`/RS rename** — the fallback should be elevated from "only if SB-R14
  proves insufficient" to a leading option, and if the bare name is kept, SB-R14 MUST disambiguate
  from *all* prior senses, not just PL-021b.

- **pi-harness / credential-isolation are cited but their "not touched" decision is unstated.**
  PI-012a and CI-004 both use "Substrate" normatively (verified). The artifact names them in the
  collision note but does not list them among the reference touches, nor state that they were
  considered and deliberately left untouched. Add an explicit "considered, no edit — SB-R14 owns
  disambiguation centrally" line so the omission is a decision on the record, not a gap.
  (cognition-loop's keeper coupling was checked: it consumes decision/HITL events but does not
  house the restart cycle — correctly excluded on *that* axis; it matters only for the
  substrate-naming axis above.)

### 6. Dependencies correctly identified — **PASS**
The Dependency Map is coherent and correct: registry-prefix reservation → {SB, EV}; SB + EV → SK
(finalize last); SK → {HC/SH/ON/PL} pointer touches. The split is right — HC/SH depend on "SB
exists," ON/PL on "SK exists." EV-U5 (drift reconcile) is correctly called a prerequisite
*within* the EV change. SB's soft-dependence on EV's `ScanAfter`/typed-decode landing (SB-R4/R6)
is accurately characterized. No cycle, no missing edge.

---

## Contested-call rulings

**(a) SB/SK "new spec" vs fold-into-existing — DEFENSIBLE, concur.**
- **SK**: confirmed by grep that no keeper spec exists and `SR3/SR4/SR6/SR7/SR9` and the 7/11-step
  cycle have *no* normative home anywhere in `specs/`; the only adjacent fragment is ON-059 §4.13
  (a restart-*now* gate ladder, not the auto-cycle). Folding into operator-nfr would bury a
  load-bearing vertical inside an NFR spec. A dedicated SK spec is the right call.
- **SB**: the generic record→replay seam is genuinely new material. `scenario-harness.md` is a
  subprocess-level E2E rig (SH-018 verified) and is not the record→replay seam — the artifact
  correctly keeps SH a pointer, not a merge. Defensible. The only caveat is the *name*, not the
  *existence* (see 5b / contested call b).

**(b) "Substrate" naming-collision handling — INADEQUATE.**
As detailed under criterion 5: the collision is broader than the single family analyzed, so the
"disambiguate-by-requirement, don't rename" resolution rests on an incomplete inventory. The
deferral of the rename to Change Design is the right escape hatch, but the decompose should not
present disambiguation as the primary path while under-counting the collisions it must
disambiguate against.

---

## Additional finding outside the six criteria (must fix — factual/consistency)

**EV-U5 mischaracterizes a live section-number collision as a "phantom §8.13."**
Verified in `internal/core/eventreg_hqwn59.go`: 18 `session_keeper_*` types are registered, and
their comments explicitly claim **§8.13** (`registerKeeperEvents registers §8.13 session-keeper
event payload constructors`, lines 478–489). But in `event-model.md`, **§8.13 is the
Epic-completion lifecycle** (`epic_completed`), and the highest keeper-free section is §8.15.
So this is not a "phantom" section — it is a **real section-number collision**: the code has
squatted §8.13, which the spec already assigns to a different cohort. The reconciliation is not
merely "add the missing rows"; it must (i) assign the keeper cohort a *fresh, un-colliding*
number (next free is §8.16), and (ii) correct the code comments' §8.13 citation. EV-U5 as
written would lead the Research/Design pass to look for keeper events at §8.13 and find
Epic-completion. Fix the characterization.

**Naming-convention conflict between the 4 new events and the 18 existing.** The change adds
`keeper_handoff_written` / `keeper_model_done` / `keeper_clear_sent` / `keeper_new_session_up`
(`keeper_*`), while every one of the 18 already-registered types is `session_keeper_*`. EV-U5
explicitly wants the 4 new events added "into a consistent catalog, not on top of undocumented
drift" — but the proposed names *are* an inconsistency. The decompose must flag the
`keeper_*` vs `session_keeper_*` convention as a decision (recommend `session_keeper_*` for
catalog consistency, or state why the new interior-event cohort deliberately diverges).

---

## Changes requested (actionable, ordered)

1. **Widen the naming-collision inventory (SB-R14 / lines 26–30).** Add the `cognition-loop`
   CL-015/CL-024 "substrate teardown" (session substrate) and the `credential-isolation §2.2` /
   `pi-harness PI-069` "transport/production substrate" senses. Either (a) elevate the
   `replay-substrate`/RS rename to the leading option, or (b) rewrite SB-R14 to require
   disambiguation from *all* prior senses, naming each.

2. **Correct EV-U5.** Replace "phantom §8.13" with the true finding: §8.13 is already
   Epic-completion; the 18 `session_keeper_*` code registrations collide on that number. Require
   the reconciliation to (i) assign the keeper cohort a fresh section (§8.16) and (ii) fix the
   code-comment citation. This also affects EV-U1's "new §8 sub-section" — pin it to the fresh
   number, not §8.13.

3. **Resolve the `keeper_*` vs `session_keeper_*` naming.** Add an explicit convention decision
   for the four new interior events so they land consistently with the existing 18.

4. **Record the pi-harness / credential-isolation "not touched" decision.** Add a one-line
   "considered, no edit — SB-R14 owns disambiguation centrally" so the omission is on the record.

5. **(Non-blocking) Reword EV-U1 and the HC/ON pointer requirements** from document-structure /
   pointer-insertion instructions to post-state truths (criterion 4).

---

## What is already good (keep)
- Traceability table is real and ID-level accurate (criterion 2).
- Every cited existing-spec anchor checks out against the live specs (HC-035, HC-056/PL-021b,
  SH-018/SH-INV-001, ON-059 §4.13, PL-021d, EV-038/032/033/028/029, §8.6 `reconciliation_run_id`).
- SB and SK prefixes are genuinely free in `_registry.yaml`.
- Dependency ordering (registry → SB/EV → SK → pointer touches) is sound.
- The SK "new spec" call is well-evidenced (no keeper spec, no SR-invariant home — grep-verified).
