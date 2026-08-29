---
id: pasteinject-never-skip-enter
title: A failed seed verification must not skip the Enter that submits the prompt
type: task
priority: 0
labels: [daemon, pasteinject, fleet, clear-the-ground]
depends_on: []
blocks: []
workstream: W1
batch: 2
---

## Why this is P0 and why it goes before everything else

**Every bead this programme dispatches pays this tax.** On days with real dispatch volume the wedge
rate is **10–40%**, and it has been since July. **113 distinct runs** have hit it.

**It fires AFTER the implementer commits.** So the failure is not "a run did not start", which anyone
would notice. It is "a run did its work, committed it, and then hung holding a worker slot until the
daemon judged it stalled and failed it" — with the bead reopening and the commit stranded on a run
branch nothing merges. On 2026-08-25 that cost four runs in one evening; all four commits were
recovered by hand, and would have been lost to anyone not watching for `pasteinject_failed`.

## What is proven, from source — do not re-derive this

All line numbers are from `internal/daemon/pasteinject.go`; **find the symbol, not the line.**

- **The paste is not failing, and the marker is always in the payload.** The implementer-initial
  payload is exactly `"Please read .harmonik/agent-task.md and begin.\n"`. The resume payload begins
  with that same sentence. The reviewer path uses `"review-target.md"` against its own seed. So
  `seed marker "agent-task.md" absent from pane` **never means the text did not arrive**, and captured
  wedged panes visibly held the complete, correctly formatted prompt.
- **`injectAndVerifySeed` does not verify delivery.** Its check is
  `strings.Contains(pane, marker)` against a tmux capture of the **visible pane**. It verifies that a
  string is on screen at the instant it looks.
- **A failed verification returns a non-empty reason, and the caller returns early on it** — before
  the `enterSender` block that calls `sendSubmitEnterWithRetry`. **That skipped Enter is the whole
  damage.** The prompt sits complete and unsubmitted; one Enter releases it.

## The fix is TWO items and the first does not depend on the second

### Item 1 — never skip the Enter. This is the whole operational fix.

**A failed verification must log, emit its event, and SUBMIT ANYWAY.** Nothing else. This is
independent of ever understanding why the check misfires, and it alone would have saved all four runs
on 2026-08-25.

**This is smaller than it looks, because the function already fails open.** When pane capture fails on
every attempt, the existing code prints *"pane capture failed on all N attempts but every paste write
succeeded; trusting the write"* and returns success. **So "we could not see it, so trust the write" is
already the design.** What the code does not currently accept is "we looked, and the marker was not
there" — and it treats that as worse, when it is the same uncertainty with a different shape. Item 1
extends a principle already in the file; it does not introduce one.

**Keep the failure visible.** Still log it, still emit `pasteinject_failed`. The event is the only
signal that anything is off, and item 2 needs it.

### Item 2 — find out why the verifier misfires. Separable, and NOT a blocker for item 1.

**Instrument what `CaptureLastPane` actually returns on each failed attempt.** Settle it by
measurement, not by reasoning — every mechanism proposed so far has died to the phase counts below.

## The mechanism is NOT known, and one plausible story is already disproved

A published explanation held that a long resume payload pushes the marker line out of the visible
capture. **Counting all 113 events by phase kills it:**

| phase | wedges |
|---|---|
| `reviewer` | 76 |
| `implementer-initial` | 23 |
| `implementer-resume` | 14 |

**`implementer-initial` pastes ONE LINE and wedges 23 times**, so payload length cannot be the cause.
And `implementer-resume` — the phase the story was built around — is the **rarest** of the three. It
looked causal because four runs in one evening happened to be resumes. That was sampling, not cause.

**All three phases share the verifier and the retry loop, and all three fail.** So the defect is in
the VERIFY step, not in any payload. Failing across both short and long payloads reads as a timing or
capture race — the check asking whether the render has happened yet. **That is a hypothesis. Treat it
as one.**

**One more fact for whoever instruments it:** `submitSeedInput` is called INSIDE the retry loop, so a
run that fails verification three times has had the payload **submitted three times**. Whatever the
capture returns, account for that.

## Done when

1. **A failed seed verification still sends the Enter.** Prove it with a test that forces verification
   to fail and asserts the Enter is sent anyway.
2. **The failure is still logged and `pasteinject_failed` is still emitted.** A test that asserts the
   event fires on the failing path.
3. **The existing capture-failed fail-open path still behaves as it does today.** You are extending
   it, not replacing it.
4. **Item 2 is either delivered or filed as its own bead with the instrumentation findings attached.**
   Say which. Landing item 1 alone is an acceptable outcome and is the point of the split.

## Limits

- **Do not delete the verification.** It is the only signal that anything is off, and item 2 needs the
  data it produces. Removing it would hide the defect rather than fix it.
- **Do not move the marker to the end of the payload.** That treats a symptom of a mechanism the
  phase counts above already disprove.
- **Do not make item 1 wait on item 2.** If the race turns out to be hard, the fleet still stops
  losing completed work.
- **Do not change what the payloads say.** The text is correct and arrives intact; the bug is
  downstream of it.
