# 2 — Adversarial panel verdict (2026-07-13)

> Five adversarial reviewers were run against the generative-system direction (the CAPTURE +
> charter-draft + emergent-systems framing + the factory-simulation idea). Each attacked one
> angle and was told to default skeptical. This doc is the synthesis. Bottom line: the
> operator's own two doubts — "this is yak-shaving" and "locality won't hold" — are UPHELD.
> The grand program deflates to a small, concrete bet run in parallel with direct fixes.

## The five verdicts (all "salvage-minimal", none "proceed as-is")
1. **Yak-shaving / ROI:** Kill the simulation. The census already named every fix; the charter
   re-narrates known findings in boids/slime vocabulary — zero new information about what to
   fix. Meta-work pays ONLY when the direct fix is blocked on not-knowing-which-principle-works;
   here we're not blocked, we're avoiding. Do the charter's Step B and nothing else meta.
2. **Locality breaks:** Locality is right as *context-economy* (don't make 1000 agents read one
   manual) and WRONG as *sufficiency*. Software's hardest concerns — dedup across components,
   dependency cycles, cross-module spec conformance, architecture — are irreducibly non-local
   (O(N²) / whole-graph). The natural exemplars have NO global invariants, which is why locality
   suffices for them and not for us. Concrete failure: two crews independently write the same
   retry helper; every local principle passes green; result is divergent duplication no
   boid-rule catches. Fix: push global invariants into CI gates (dup index, cycle check,
   spec-conformance) + ownership boundaries behind one interface. Honest reframe: **"emergent
   execution over a centrally-designed invariant surface"** — NOT leaderless emergence.
3. **Analogy transfer is hollow:** Every exemplar (boids/ants/slime/Kuramoto/immune) is
   gradient-descent on a low-dim *sensable* field. Software correctness is not scalar, not
   continuous, not spatially local — so once you drop "truth-density" (already conceded
   unmeasurable), the analogies have nothing left but vocabulary. The factory sim is worst: it
   rewards local reinforcement = **a machine for manufacturing the god-function**, and would
   report "success" on the exact topology that is the disease. The ONE mechanism with genuine,
   gradient-free, *measurable* transfer: **decay-by-default** (age-since-last-firing is a real
   counter). Selection survives weakly (count recurrences, cap the set). Everything else is
   decoration to drop.
4. **Just-fix-it:** Every stated principle maps to an existing lint / gate / reviewer-prompt
   clause — none needs a "system." This week, no charter: fix STEP-0a resume-hang, 0b
   false-close, queue two-writer (rpc.go:1016); add a `funlen`/complexity lint so a 2300-line
   function CANNOT merge; add one `agent-reviewer` clause ("BLOCK any func >N lines, struct >20
   fields, mutex held across IO"). The "no agent flagged it" gap closes for the price of a
   `.golangci.yml` entry. Keep from the vision only the anti-accretion sentence, as one
   paragraph in orchestrator-rules + agent-reviewer.
5. **Simulation validity:** Bed-1 (scripted actors) assumes the conclusion — demote to
   whiteboard math. A hard gate refusing over-limit code proves the *lint* works, not that any
   agent *reasoned*. The only trustworthy test: a bounded task with a *latent, ungated*
   structural temptation, blind-scored on a pre-registered rubric, A/B principle-present vs
   absent, ~15–20 runs/arm (Fisher's exact). It can conclude "encoding P shifts the ungated bad
   choice by Δ"; it CANNOT conclude anything about scale, accretion, or 1000-session emergence.

## What survives (the consensus)
- **KILL the simulation / factory substrate** as a predictive or validation instrument (4 of 5).
- **Keep exactly two things from the grand vision:** (a) **decay-by-default / anti-accretion** as
  an encoded rule — the one real transfer; (b) the honest reframe **"emergent execution over a
  centrally-designed invariant surface"** — global coherence is bought by gates + ownership
  boundaries, a little frozen central authority, not pure emergence.
- **Most "principles" are CI rules**, not a system: bounded-size lint, coverage-floor gate,
  no-mutex-across-IO lint, reviewer-prompt clauses.
- **The freeze has a live, compounding cost:** resume-hang wedged 5/5 recent runs; false-close
  corrupted bead status. Every hour "abstracting up" is an hour these stay broken.

## Recommended path (deflated, concrete)
1. **Lift the freeze for the direct fixes NOW** (independent, single-writer-safe): STEP-0a
   resume-hang, STEP-0b false-close, queue two-writer.
2. **Land the cheap structural encodings this week:** funlen/complexity lint + agent-reviewer
   prompt clause. This is the actual proof the vision wanted — for the price of a lint.
3. **Encode ONE real behavioral rule:** decay-by-default / anti-accretion (orchestrator-rules +
   agent-reviewer paragraph).
4. **OPTIONAL, non-blocking:** one blind pre-registered A/B on an *ungated* judgment to test
   whether encoding a principle changes agent *reasoning* (per adversary 5's design). Nice-to-know,
   not the path.
5. **Do NOT** build the sim, the metabolism engine, or the recursion doctrine until step 3 has
   demonstrably changed one real carve. Build-the-builder only after one encoded principle earns it.

## Status
Panel complete. This deflates the grand program to a small bet + direct fixes. Awaiting operator
decision on lifting the freeze for STEP-0 + the lint/reviewer encodings. Fleet still frozen.
