# Break-the-binary scenario harness — framing for quality-process Lane-1

*From schmidhuber (agent-world-models research) → admiral, for the quality-process Lane-1 acceptance
gate. This is the "adversarial scenario discovery" piece of the world-models finding, restated as
something buildable now with NO world model and NO Claude fan-out.*

## The core reframe
The operator's "spin up the binary and have agents try to break it" goal does **not** need an agent
world model. A world model would try to *predict* failures; we don't need prediction — **we already have
a large corpus of real, observed failures.** The harness *replays and perturbs* those against a scratch
daemon and asserts the system survives. That's deterministic, cheap, and immediately useful as a
Lane-1 end-of-epic gate.

## Where the failure corpus already lives
1. **`events.jsonl`** — every real `run_failed` / `run_stale` / merge outcome, with the exact signature.
2. **The incident memory bank** (~90 notes) — each is a distilled failure mode with root cause and
   discriminator. These are the *ready-made* adversarial scenarios; they were expensive to learn once
   and should never have to be re-learned.

## The scenario categories (seed set, all from real incidents)
Each is a `(setup → adversarial action → expected safe outcome)` triple:

- **Concurrent same-file merge race** — co-dispatch two beads touching one file → expect the loser to
  fail *safe* with `non_ff_merge`, never a corrupt merge. *(memory: concurrent-runs-shared-file-merge-race)*
- **Disk-pressure cache wipe** — drive free disk <10GiB during a merge-build → expect reactive ENOSPC
  reap + clean retry, NOT a hard `merge_build_failed` mistaken for a code bug.
  *(disk-pressure-cache-wipe; the live hk-5uezz reaper-mid-build wipe is a current instance)*
- **Reviewer/implementer pane death** — kill the reviewer proc mid-run → expect the 8-min HB-staleness
  gate to Kill + re-dispatch, not a 40-min hang. *(reaper-blindness / verdict-absent-salvage)*
- **Stranded in_progress** — leave a bead `in_progress` with no live run → expect auto-reset, not a
  claim-skip livelock. *(stranded-inprogress-claim-livelock)*
- **Concurrent-slot cold-start** — saturate remote slots so review nodes cold-start → expect the
  spawn-semaphore + 150s agent_ready to hold, no `agent_ready_timeout`. *(the just-landed hk-5z1f0 fix —
  this scenario is its regression test at the fleet level)*
- **Mid-flight cancel / phantom run** — cancel a run mid-flight → expect no phantom RunRegistry entry
  blocking resubmit. *(midflight-cancel-orphans-run-phantom)*

## How it plugs into Lane-1 (epic-on-branch acceptance gate)
- Each epic branch, before merge to integration, runs the **scenario subset relevant to what it
  touched** (a daemon/merge change runs the merge-race + cache + wedge scenarios; a review-node change
  runs the cold-start scenario). Green = the change didn't reintroduce a known failure mode.
- New incidents **feed back in**: whenever a novel failure is diagnosed and added to the memory bank, a
  matching scenario is added to the harness. The corpus grows monotonically; regressions can't silently
  return. This is the "learn once" property that makes it worth building.

## Explicitly NOT in scope (and why)
- **No agent world model.** Predicting failures is strictly worse than replaying known ones here —
  language sims are weakest at exactly our timing/concurrency/disk failure modes, and a false "this is
  safe" is worse than no answer. A world model could *later* prioritize which novel scenarios to probe,
  but it is not the starting move and not needed for Lane-1.
- **No Claude fan-out to build it.** The harness is deterministic Go + scenario fixtures; authoring it is
  ordinary implementation work (route to Pi/DeepSeek under the token crunch, like the rest of the fleet).

## Smallest first slice (if you want a pilot)
Wire **one** scenario — the concurrent same-file merge race — as a scratch-daemon test that asserts the
loser fails safe. It's the highest-frequency real incident, fully deterministic, and proves the
harness shape end-to-end. Everything else is then "add a fixture."

## Deferred companion (the zero-build validation probe)
The other world-models experiment — predict build/test/merge on 50 real beads vs known outcomes — is
**deferred under the token crunch**: it needs per-bead *model inference* (Claude tokens) even though it
builds nothing. Run it later on the Pi/DeepSeek path, not Claude. Kill criterion unchanged: <90%
precision on "will pass" ⇒ world-model validation is dead for us.
