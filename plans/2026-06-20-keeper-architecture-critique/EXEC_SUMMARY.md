# Keeper Architecture Critique — Executive Synthesis (FINAL)

**Date:** 2026-06-20 · 11 independent critics + 2 adversarial verifiers · all reports in this folder.
**Method:** distinct-lens fan-out (`01`–`11`) → draft synthesis → 2 adversaries that could overrule it (`12`, `13`) → one ground-truth git check of the disputed commit → this final, with both verifiers folded in.

---

## The operator's three questions, answered

**1. Are there architectural issues?** — YES, but *localized*, not pervasive rot.
There is **one** fragile design decision, named independently by all 10 code lenses:

> The keeper **infers** facts it should be **told** (session identity, "/clear happened," operator-busy) and **acts blindly** through a ~9-file multi-writer state ABI + an **open-loop tmux paste** — success is *inferred*, never *confirmed*, and there is no liveness alarm when it dies silently.

Everything else (the triplicated gates, the 12-dimension implicit state machine, the silent fail-open) is *downstream* of that one decision. **Adversary-A's correction stands:** the team's own problem-space doc says *"The architecture is SOUND … a SIGNIFICANT REFACTOR, NOT a replacement."* So the honest title is **"one fragile mechanism + accreted fix-scar tissue,"** not "architecturally flawed beyond repair."

**2. Are there complexity issues that cause failures?** — YES, real and measured.
Accreted-fix complexity: nearly every gate cites 2–4 bead IDs working around a prior failure; the "should I clear?" sequence is hand-copied across **4 entry points**; a load-bearing invariant is enforced *only by call-site ordering in a comment*. The design makes each edge case a new **branch**, not a new **state**, so the failure surface grows with every patch. This is reducible — `89852bb3` already deletes **~539 lines** of it.

**3. Untestability → consistent failure?** — **REFUTED as literally stated** (the one dissent, report 04).
The code is *well-seamed* (27 injectable `…Fn` fields, deterministic 4s suite, 74% coverage); the architecture critic agrees the seams are clean. The real defect is sharper: **the tests mock the exact mechanism that fails in production** — real tmux paste `InjectText` 10.5% covered, submit-race `sendEnter` 0%, and the restart-now test *certifies the production no-op as success*. So: **testable-but-untested-where-it-matters**, with tests that validate the bug — not an imperative tangle that can't be tested.

---

## What the evidence actually supports (after adversarial review)

| Claim | Status after verifiers + git check |
|---|---|
| Single-root diagnosis (infer-and-act-blindly) | **UPHELD** — one root seen by 10 lenses (not 10 separate defects; Adversary-A) |
| "Architecturally flawed / needs replacement" | **DOWNGRADED** — team's own source says architecture is sound; it's a refactor |
| ~44% of keeper-code commits are `fix(keeper)` | **UPHELD** (reproduced ~46/95 ≈ 48%) |
| "69% fix-of-fix regression rate" as *the* clincher | **WEAKENED** — self-reported assertion, not a measurement |
| `89852bb3` is the stranded cure | **PARTIALLY** — see below |
| "Native compaction loses fleet intent" (keeper's reason to exist) | **UNVERIFIED** — no logged incident found; it is design-time opinion |

### The stranded commit `89852bb3` — ground truth (settles the A/B conflict)
- **Genuinely unmerged** (not an ancestor of main; `restartnow.go` absent from main).
- **Real −539-line simplification** of the **manual `restart-now` path** + identity collapse in that path + flag-only args + **loud (non-zero) exit instead of silent no-op**. Worth landing.
- **BUT** (Adversary-B, confirmed by reading the diff): the "ACK handshake" read-back (`capture-pane`/scrollback) exists **only in the doc/comment, not as automated code** — it does **not** close the loop. And it **does not touch the automatic ACT-loop** (`MaybeRun`/`runCycle`) — the diff comment itself says restart-now is "*Unlike MaybeRun*." **The worst live recurring failure (the automatic looping / handoff-truncated-to-0 path) is outside this fix's blast radius.**

So: landing `89852bb3` is good housekeeping (simpler, louder, identity collapsed in one path) but will **NOT** resolve the symptom the operator actually feels. Adversary-A's "fixes most symptoms" was based on a secondary table; the diff says otherwise.

---

## The real fork (operator decision)

Both adversaries, plus reports 01/07/11, converge on this being the actual question — and it must be decided **before** any spec work (speccing first would entrench the wrong shape):

**Is the in-place `/clear`-and-resume-via-tmux-paste cycle the right mechanism at all?**

- **Option L — Land & close-the-loop (keep the keeper).** Land `89852bb3`; then apply the same treatment to the *automatic* cycle: a real automated read-back (`capture-pane` for the ACK nonce) so success is confirmed not inferred, collapse identity to one authoritative source, fold the 4 copied gate paths into one `evaluateGates(mode)`, collapse 5 bash hooks into thin shims over one tested subcommand + a loud liveness alarm. Higher effort; preserves the current capability.
- **Option D — Delete-to-checkpoint (shrink the keeper).** Delete the in-place `/clear` cycle entirely; route context overflow through the **`respawn.go` restart-from-HANDOFF that operators already do manually** (`known-workarounds.md:57` — crew stop/start). Keeper degrades to warn-only + respawn safety net (~30-line watchdog residue). Adversary-B found **no fatal flaw**; the only blocker is the unverified "native compaction loses intent" premise.

### Recommendation (sequenced, throughput-first)
1. **Land `89852bb3` now**, correctly-billed — cheap, reversible, real −539 lines, makes failures *loud*. Do this regardless of the fork.
2. **Run the one experiment that resolves the fork:** does restart-from-HANDOFF (respawn) preserve fleet intent as well as the in-place cycle, and/or does native compaction actually lose it? This is a single smoke-run, and it converts the load-bearing *opinion* into *evidence*. **This is the highest-value next action** — the entire L-vs-D decision hinges on it.
3. **Then** either close-the-loop on the automatic cycle (Option L) or delete it (Option D) — chosen by the experiment's result, not by argument.
4. **Then** fix test aim (cover `InjectText`/`sendEnter`; stop certifying the no-op as success) and finalize **one** spec into `specs/` with RED-tested identity + threshold invariants. Spec last, not first.

---

## Reading order for the full critique
Start here, then: **07** (failure data) → **02** (architecture) → **05** (identity) → **06** (open-loop) for the diagnosis; **12** + **13** (adversaries) for the corrections; **11** for Option D's design; **04** for the testability nuance.
