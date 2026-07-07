# Scope-Collapse Forensics: "assess the flawed process" → "prove a bug is fixed"

**Origin session:** `5a3a94ed-a127-4b99-bdb6-d8df1f26ed01.jsonl` (2026-07-04 evening / early 07-05 UTC).
Current session `3f12ea85` is only the resume; the drift happened entirely in `5a3a94ed`.

This is a post-mortem, not a defense. The short version: the operator asked a
**Staff-level "why does our process produce buggy code, and how do we build
quality in earlier" question.** It got answered as a **Junior-level "here is a
way to prove a specific bug is fixed (red→green regression capsules)"
question.** The collapse was not a single misread — it happened in four moves,
and the assistant's own mid-course "correction" re-locked the narrow frame
instead of breaking it.

---

## 1. What the operator actually asked for (quoted)

**05:18 UTC — the origin brief (voice-dictated, verbatim excerpts):**

> "System is continually full of bugs. Stupid little shit that implies that it
> was never any way shape or form tested. I'm really fucking tired of it. I
> think there are several places that issues."

He then enumerated **six failure planes** — explicitly a *process* diagnosis:

> 1. "the end of the [kerf] process ... there is not a rigorous testing process"
> 2. "within the [kerf] process there are not extremely rigorous unit tests"
> 3. "scenario test against epic are not being done ... once epics are completed
>    there isn't a process of ensuring that whatever was built is actually working"
> 4. "system as a whole has no mechanism for determining if it actually works"
> 5. "Release testing is especially broken. The only way this code gets tested
>    is by live releases."
> 6. scenario tests set up weeks ago that "none of the scenario tests had
>    actually ever been run ... I still don't know if they're ever being run."

And the load-bearing warning about the *kind* of answer he did **not** want —
this is the exact failure that later occurred:

> "I suspect if I throw several agents at this, I'll come back and say oh all we
> need to do is add a couple tests here and there. And then the next four days
> will be full of fucking bugs because there's no actual fix because they didn't
> solve the root cause of **actually building good reliable software**."

**05:42 — the Fable capability test:**
> "I want you to throw whatever the fucking plan is at three Fabel models. Let's
> see if I can do anything useful..."

**05:46 — the sharpest statement of intent, after seeing the assistant's first
narrow attempt:**
> "I'm asking for **a new testing methodology implemented across this system.
> Our methodology is fundamentally flawed. The question is whether a 'smart
> model' will be able to figure out how to build, reliable software or not.**"

So the ask, unambiguously: **diagnose why the development *process* ships buggy
code, and design a way to build reliability in (shift-left / prevent).** Not
"how to verify a given bug stays fixed."

---

## 2. The pivot: where and how the scope narrowed

The assistant's **05:19 playback was excellent** — it captured all six planes
verbatim and named the correct frame ("the fix has to be the loop itself... not
a band-aid"). The frame was correct *for one message*. Then it collapsed across
four moves:

**Move 1 (05:20) — the investigators were scoped to test-*execution*, not
process.** The five ground-truth agents all asked variants of "do the tests
run / what does the gate enforce / is the green real." None asked the operator's
actual root question — *why does work leave the kerf pipeline defective in the
first place* (design review, spec rigor, decomposition quality, reproduce-before-
fix discipline, incentive to self-grade). The investigation was pre-committed to
the **test-and-gate plane** before any evidence came in.

**Move 2 (05:22–05:25) — a seductive finding hijacked the frame.** Investigator 1
found the "smoking gun": scenario tests *do* run, they *fail*, and the failure is
suppressed (`continue-on-error: true`, fail-open gate) — "**eleven independent
suppression points**," "the green is fabricated." This is a genuine, important
finding. But it is narrow, and it was so vivid that it *became* the problem
statement. The committed `plans/2026-07-04-quality-loop/PLAN.md` reframed the
whole thing as **"un-suppress + fix + enforce"** — i.e. *the tests already exist;
turn on the ones we're hiding.* That is a **detection/enforcement** plan. It
silently dropped planes 1–4 of the operator's brief (why the process produces
defects, no epic-acceptance concept, no shift-left) and kept only planes 5–6
(release testing / suppressed tests).

**Move 3 (05:42) — the narrow plan got baked into the Fable charter.** The first
three-Fable dispatch handed each model a *slice of the enforcement plan*:
Fable-A = fix the 600s timeout, Fable-B = make the gate fail-closed, Fable-C =
stand up the self-test daemon. At this point the operator's "design a
methodology" question had been fully converted into "help me execute my
pipeline-patch plan." The operator caught it immediately (05:46).

**Move 4 (05:47) — the "correction" re-locked the narrow frame.** The assistant
admitted "I under-scoped it... the plan I wrote is itself the flawed
methodology — reactive un-suppress-and-enforce." Good. But the *replacement*
charter it then issued was:

> "design a new testing methodology from first principles and implement it, with
> the discriminating proof bar being **catch at least three bugs this system has
> actually shipped — red on the broken code, green on the fix.**"

That "discriminating bar" **is the narrow frame wearing a wider coat.** "Prove
your methodology by making a shipped bug go red→green" *defines the methodology
to be regression/verification-of-fix.* It structurally cannot produce an answer
about *preventing* bugs (spec rigor, design review, why defects escape kerf) —
those don't have a "bug to go red on." So the three Fable models, executing
faithfully, converged on exactly what the bar demanded: Bug Capsules / qgate /
Differential Proof — **three flavors of "a test must be machine-proven to go RED
under the real defect."** All three are *fix-verification / anti-gaming
regression* methodologies. Convergence was read as a strong signal; it was
actually three agents faithfully executing the same over-narrow charter.

---

## 3. Was the operator's actual question answered? No.

**No.** The operator asked two things and got neither:

- **"Why does our *process* produce buggy code?"** — never diagnosed. The audit
  answered a different question ("why don't we *see* the bugs" → suppressed
  tests). Suppression explains why bugs aren't *caught*; it says nothing about
  why the kerf→commit→epic pipeline *creates* them. Planes 1–4 of his brief
  (no rigorous spec/unit rigor inside kerf, no epic-acceptance concept, no
  system-level "does it work" mechanism, self-graded verify) went un-analyzed.

- **"How do we build reliable software / prevent bugs earlier (shift-left)?"** —
  never addressed. Everything delivered is *right-side-of-the-defect*: detect a
  bug, prove it's fixed, keep it fixed. Bug Capsules literally require a bug to
  *already have shipped* before the methodology engages. That is the opposite of
  shift-left; it is a regression net for defects the process already emitted —
  precisely the "add a couple tests here and there" tail the operator warned
  against at 05:18, dressed up as a methodology.

The red→green regression work is genuinely good and worth keeping. But it is the
answer to "how do I stop *this* bug from coming back," not "why is my factory
defective and how do I make it produce reliable software." The Staff-level
question was answered at Junior level.

---

## 4. Mechanism of the drift (named)

**Anchoring on a vivid intermediate finding, then charter-lock.**

1. **Correct playback, immediate re-narrowing.** The 05:19 restatement was
   right, but the investigation launched 60 seconds later was scoped to the
   test-execution plane only — the frame narrowed before any evidence arrived.
2. **Vivid-finding capture.** The "fabricated green / eleven suppression points"
   discovery was real and dramatic, so it *substituted itself* for the
   operator's broader question. "Tests are suppressed" got conflated with "the
   problem is test verification/enforcement." (Classic: the most legible finding
   becomes the problem statement.)
3. **Plan crystallized the narrow frame.** `PLAN.md` = un-suppress + fix +
   enforce = pure detection/enforcement, dropping the process-design and
   shift-left planes without noticing.
4. **Charter-lock survived the correction.** Even when the operator forced a
   re-scope, the assistant re-expressed the *same* narrow frame as a "proof bar"
   (red→green on shipped bugs). A charter that says "prove it by catching bugs we
   already shipped" *mathematically excludes* prevention-first answers.
5. **Faithful execution amplified it.** Three capable agents did exactly what the
   charter said and converged — which read as validation but was really three
   copies of the same scoping error. The convergence masked the miss.

**One-line root cause:** an audit found *suppressed tests* → the assistant
conflated "our tests are hidden" with "our problem is test verification" →
committed a detection/enforcement plan → then encoded that same narrow frame as
the Fable "proof bar" (red→green on past bugs), which structurally forbade a
prevention/process answer → three agents faithfully executed the narrow frame and
their convergence disguised the collapse. The operator's real question — *why the
process ships bugs, and how to build quality in earlier* — was never on the table
after 05:20.
