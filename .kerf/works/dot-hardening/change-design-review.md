# Change-Design review — dot-hardening (WG / EM / HC)

> Adversarial gate before Spec-Draft. Reviewed: `DECISIONS.md`, the three `04-design/*.md`,
> `03-research/*/findings.md`, `MODEL.md`, `02-components.md`, `ISSUES.md`, plus the live code the
> designs cite. Verdict at the bottom. Every claim carries a file:section anchor.

---

## (a) CONFIRMED defects — must-fix before Spec-Draft

### D1 — The `rubric` (and `task`) VALUE-SOURCE is unspecified; the structural leak fix rests on a separation no clause produces. **[headline]**
The whole redesign's load-bearing claim is "the leak is *unexpressible*" (WG-057; EM-069 posture; goal 1).
That claim holds only if the implementer's `task` input excludes the reviewer's rubric AND the reviewer's
`rubric` input actually carries the rubric. **Neither is specified.** Verified in code: today's rubric is
**hardcoded Go strings inside `buildReviewTargetContent`** (`agenttask_chb028.go:580–608` — the "Coverage
Check", "Spec Field-Name Check" blocks), NOT a bead-body section. EM-069 (B1) says the single renderer
**subsumes/deletes both builders** — so that hardcoded rubric is **orphaned**: no design says where the
`rubric` value now originates (launch var? node prompt? config? extracted from the body?). WG-055 merely
*names* `rubric` in the reviewer default set; EM-069-REV describes reviewer *framing* but not the rubric
*source*. Symmetrically, implementer `task` is never bound to a concrete source, nor guaranteed rubric-free.
- **Consequence 1 (leak):** if `task` ends up = bead body and any bead embeds review criteria (MODEL.md's
  stated leak vector, §"core problem"), the rubric is inside a variable the implementer *does* declare — so
  the "structurally unexpressible" guarantee (WG-057) is hollow.
- **Consequence 2 (regression):** the reviewer default set cannot "reproduce today's behavior" /
  "render byte-identically" (WG A1(iii); EM B1(iii)) — today's bytes include the hardcoded rubric, which
  now must arrive via an unspecified `rubric` input. Delete the builder without a rubric home → reviewer
  loses its checklist. The back-compat claim is **false for the reviewer** until this is closed.
- **Fix direction:** one of the three designs (EM is the natural owner, since it deletes the builder) MUST
  specify the `rubric` value's origin and binding, and MUST state that implementer `task` is
  rubric-free-by-construction. Assign an owner across the WG/EM seam; today it is orphaned. This is the
  single most important thing to fix.

### D2 — The daemon-emitted role→source-keys manifest is load-bearing but has no clause.
The leak oracle (C3 / goal 1) asserts on a **daemon-emitted typed input manifest**, which the research flags
(findings §4.2) as anti-rot-critical: it must be a **production byproduct**, not test-only. HC-074(iv)
correctly states HC does *not* own it and points at EM. But in EM the emitter appears **only as a sentence
in EM-069's cross-file (iv) note** ("keep that emit surface in EM-069") — no clause id, no normative MUST in
the posture body. A load-bearing anti-rot obligation tucked in a cross-file aside will be dropped or
under-drafted. **Fix:** give the manifest emitter its own EM sub-clause (e.g. EM-069-MAN) with an explicit
MUST and the `role→source-keys` shape, so C3's oracle has a real, spec'd surface to assert on.

---

## (b) PLAUSIBLE concerns — should-address

### P1 — "resolution" is overloaded across the EM/HC seam; risks a spec-text contradiction.
EM B5(iv)/B6(iv): "**HC owns** the resolution of an alias→concrete for every tool." HC C2(ii)/(iv):
"**EM owns the resolution ladder + seal** … HC-073 asserts only the tool-facing endpoint." Both are true
under two *different* meanings of "resolution" (EM = tier/precedence ORDER + seal; HC = catalog LOOKUP +
fail-loud + endpoint) — ownership is actually coherent, no gap/double-owner. But the shared word will read
as a contradiction in normative text. **Fix:** pin two distinct terms (e.g. "precedence order" vs
"alias-catalog lookup") in both drafts.

### P2 — `task_context` granularity mismatch feeds the manifest ambiguously.
WG-055 treats `task_context` as ONE input key; EM-069-REV enumerates id/title/**body**/base-head-SHAs as
four items. The leak oracle asserts on *source-keys* — whether `task_context` is one opaque key or four
sub-keys changes what the manifest exposes and what the oracle can distinguish. **Fix:** pin the key
granularity where the manifest is spec'd (ties to D2).

### P3 — Effective-tool resolution owner is unstated.
The seal (EM-070) keys on and stores the node's **effective tool**; WG-059 lets a per-node override carry
`tool`. Who resolves the effective tool, and when (vs handler-selection HC-003 at load)? Not owned by any
of the three. Low-risk (likely existing HC-003 machinery) but name it so the seal write-timing is unambiguous.

### P4 — Verdict↔edge check (WG-060 ch.2) is narrower than "any verdict edge."
The producer=from-node resolution (D-A5) is coherent *because* `outcome.preferred_label` is always the
from-node's output (the cascade evaluates an edge condition against its from-node). So a verdict routed
through an intermediate gate is correctly NOT checked — but that also means the typo-catch only fires on
edges directly off the verdict producer. Acceptable and self-consistent; worth one sentence in the clause so
a drafter doesn't try to "strengthen" it into an unrunnable global check.

---

## (c) Cross-file seam check

| Seam | Verdict | Note |
|---|---|---|
| Declared-I/O vocab: WG defines → EM renderer consumes → HC transports | **PASS (naming)** | WG-055/056 ↔ EM-069 consumes ↔ HC-072 transports. Naming is coherent. The **value-source** gap (D1) sits under this seam but is a production gap, not a naming clash. |
| Verdict enum (WG-058) ↔ EM feedback value ↔ preferred_label (WG-019) | **PASS** | `verdict` = typed name for the `preferred_label` value; no duplicate routing field; WG-019 stands; EM feedback = `verdict`+`notes`. No contradiction. |
| Model seal: WG=override addressing, EM=ladder+seal+replay, HC=concrete-reaches-rc.model+catalog+fail-loud | **PASS (ownership)** | No gap, no double-owner. Flawed only by the P1 terminology overload. Pipeline EM-order → HC-lookup → EM-seal is clean. |
| EM-056 clause 4 (L1610) REPLACED, not duplicated | **PASS** | EM B3 strikes it "in full," replaces with a pointer, and states "the prohibition and the channel MUST NOT both be live." Contradiction (`hk-wixms`) resolved. |

## Hazard-closure audit

- **H1** composite `(node_id, iteration_count)` seal key — **CLOSED** (EM-070, explicit, with the back-edge overwrite rationale).
- **H2** replay reads seal before recompute — **CLOSED** (EM-071 + EM-055 amend; "MUST NOT recompute from live catalog").
- **hazard ii** goal-broadcast leak, structural — **CLOSED for the `goal` vector** (WG-057 + WG-044 amend; renderer handed only declared-input values). BUT the *overall* "leak unexpressible" claim is **contingent on D1** — the `goal` path is sealed while the `task`/`rubric` path is unspecified.
- **H4** reviewer body verbatim — **CLOSED** (EM-069-REV, "id, title, and body verbatim, no truncation").
- **Overclaim / honesty (D-C8)** — **HONEST**: HC-074(v) states all three plainly — claude paste-inject-not-argv (row proves brief-assembly only), twin-proves-plumbing-not-product, daemon-emitted-manifest dependency + known-RED→GREEN sequencing. Not oversold as "end-to-end per handler."
- **Back-compat** — additive **except** the deliberate B5 ladder flip (honestly flagged as not-behavior-preserving → ISSUES #3) and **except** the reviewer-rubric regression exposed by **D1**.

## (d) What's genuinely good

- The EM-056-clause-4 reconciliation (B3) is handled exactly right: one REPLACE, two AMENDs, an explicit
  "MUST NOT both be live" — the code/spec contradiction ends resolved, not duplicated.
- H1/H2 are closed with the *reasoning* (back-edge re-dispatch, hot-catalog race) inline, not just the rule —
  a drafter can't accidentally weaken the composite key.
- The three-way fail/degrade/keep-last-good split (HC-073) with the pi-empty-model fail-loud carve-out
  (D-C4) is precise and prevents "graceful degrade" from being read as "never fail."
- Honesty caveats are load-bearing and unsoftened; the manifest RED→GREEN sequencing is called out so C3
  doesn't land a clause with no reachable GREEN.
- Ownership boundaries are drawn deliberately at every seam and mostly hold (only P1 terminology + D1/D2
  gaps mar them).

## (e) VERDICT: **APPROVE-WITH-FIXES**

The model is faithfully expanded, the four load-bearing hazards (H1/H2/ii/H4) are closed, the honesty limits
are stated plainly, and three of four seams pass cleanly. Two must-fix gaps block Spec-Draft: **D1** (the
rubric/`task` value-source — the linchpin the "structural leak fix" and reviewer back-compat both rest on,
currently orphaned across the WG/EM seam) and **D2** (the manifest emitter needs a real EM clause, not a
cross-file aside). Fix both — assign D1 an owner and specify the rubric's origin + `task`-is-rubric-free
invariant; promote D2 to a named EM sub-clause — and this proceeds to normative text. P1–P4 are cheap
tightenings best folded in at draft.
