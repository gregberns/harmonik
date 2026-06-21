# Embedding the clear-communication pattern — mechanism & what landed

**Date:** 2026-06-20
**Brief:** `PLAN.md` (the failure + the pivot). **Constraint:** a rule already existed and didn't fire — so the task is "make the behavior happen by default," not "write another rule."

## Diagnosis (why the existing rule didn't fire)

The rules in this ecosystem that *do* fire share a trait the failed one lacks:

| Fires | Doesn't fire |
|---|---|
| session-handoff "include a one-line translations glossary" — concrete required *form*, bound to a concrete *trigger* (writing the handoff), with an example | "Plain English, always" — abstract *principle*, no trigger, no required form, buried mid-file |

So the winning pattern, demonstrated in this very repo, is **a trigger-bound ritual with a concrete required form + a baked-in exemplar.** Principles lose to task focus; triggered rituals with examples survive.

**Second finding — a global message-scanning hook is the WRONG primary lever.** The whole fleet (captains, crews, daemon-spawned implementers) runs under the same global `settings.json`. Those agents legitimately speak in bead IDs to each other and to the daemon. A Stop hook that flags "untranslated codes" would misfire constantly in the headless fleet and could wedge dispatch. The discipline is about **human-facing** messages; a global hook can't distinguish "talking to the operator" from "talking to the daemon." (`AskUserQuestion` is already denied globally — one context-free-question path is already shut.)

## Refinement (operator feedback, round 2) — the rule is NOT "strip codes"

The first cut said "strip every internal code." The operator corrected the substance:

- **Tool terminology is good — keep it, don't dumb it down.** *daemon, worktree, stream/wave, agent_ready, review-loop* are the real names of real things; the operator built the tool and lives in it. When the operator says "don't use jargon," the model overcorrects to "explain it as if the operator has never seen the tool" — that is its own failure. The middle ground: speak to a peer who knows the tool but not your private bookkeeping.
- **The real failure is *partial information*, not vocabulary.** It's a **private tracking identifier** (commit SHA, bead ID, `ES`/`D` code, "Tranche", kerf codename) used as the *handle* for a thing. The operator can't dereference it, so the sentence presupposes knowledge they don't have. Fix = give the **content**, not the pointer. e.g. ❌ "the bug `hk-3vbc` supposedly fixed" → ✅ "the silent remote worktree-create bug we thought we'd fixed."
- **No tooling.** The optional warn-only linter is rejected outright — not just deferred. It is important that agents talk in code to *each other*; the discipline is operator-facing only.

The embedded rule (global `~/.claude/CLAUDE.md` + orchestrator-rules) now encodes this two-kinds / partial-information framing, with the remote-run message above as a second exemplar.

## Mechanism (ranked by leverage)

1. **PRIMARY — trigger-bound pre-send check + literal before/after exemplar, at the TOP of global `~/.claude/CLAUDE.md`.** The "every agent" lever; mirrors the pattern that demonstrably works here. **LANDED.**
2. **REINFORCE — wire the same check into the surfaces that gate the danger-zone moments.** session-resume say-back (sharpened: options-with-consequences, never context-free go/no-go; apply the question-test before asking) and orchestrator-rules "Autonomy and flow" (new PRE-SEND CHECK rule, operator-facing only). **LANDED** (orchestrator-rules synced to embedded assets; embed-sync test green).
3. **OPTIONAL backstop / verifier — a warn-only clarity-linter Stop hook, scoped to interactive operator sessions only.** Detects the specific failure signature (trailing context-free "go/no-go" question; code-dense prose outside fenced blocks not present in the user's recent input) and logs it for a measurable hit-rate; flip to blocking only after warn-only proves precision. **NOT built — pending operator decision** (false-positive risk + fleet-scoping fragility = why it's last, not first).

## What changed

- `~/.claude/CLAUDE.md` — new first section "Before any status update or question — run this check" (frame check / question test / end-on-action + the real before→after exemplar).
- `~/.claude/skills/session-resume/SKILL.md` — pause clause sharpened with the question-test + options-with-consequences.
- `.claude/skills/orchestrator-rules/SKILL.md` (+ embedded mirror) — new "PRE-SEND CHECK ON EVERY OPERATOR-FACING MESSAGE" rule; scoped operator-facing only.

## Verification — how we'd know it's working

The honest answer: the only deterministic verifier is the optional Layer-3 linter, run in **warn-only** mode, logging each hit so the hit-rate over time is observable. Without it, verification is behavioral/anecdotal (does the operator stop getting code-dumped). That tradeoff is the substance of the open decision below.

## Decision (resolved 2026-06-20)

**No tooling.** Operator rejected the linter outright (not deferred): "It is important that agents talk in code to one another." The mechanism is the embedded ritual (Layers 1–2) only; verification stays behavioral.
