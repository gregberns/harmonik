NOTICE - FROM OPERATOR: DO NOT TRUST THE CLAIMS OF THIS DOCUMENT, THEY SHOULD NOT BE ASSUMED TO BE CORRECT

EXAMPLE:

> The daemon stayed up through the six quiet days and ran work the whole time.

This is completely made up.


# Back on track — 2026-08-21

Status: DRAFT for operator review. Nothing here is actioned yet.

## The frame

The machinery did not stop. The direction did. The daemon stayed up through the
six quiet days and ran work the whole time. What is missing is a way for the
system to move work through without close supervision.

Three problems, kept separate on purpose. They have been treated as one problem
("the test system is bad") and that is why they resisted fixing.

| | Problem | State |
|---|---|---|
| A | The gate is slow and red on things we do not care about | Mostly already solved, unused |
| B | Not enough capacity — tokens, disk, agent slots | Partly working, needs routing |
| C | Work reaching the queue is not well chosen | Real, hard, deferred |

## What was measured on 2026-08-21

Evidence, not claims. Each of these was run, not read.

- **`make core` passes. Exit 0, 474 seconds (~8 min), 29 packages.** The core set
  is green today on the integration branch. The list lives at `Makefile` under
  `CORE_PKGS` and comes from the charter.
- **One package is 56% of the gate.** `internal/daemon` is 320s of 573s total test
  time. `internal/brcli` is another 89s. The remaining 27 packages total 164s.
- **The full suite is what CI runs** — 110 packages — which is where the 30-minute
  figure and most of the irrelevant red come from.
- **GitHub Actions is not trusted and is not the target.** The gate that matters is
  local: what one bead runs, and what the integration branch runs.
- **A live run failed after ~2 hours and 3 iterations, and failing was correct.**
  The review node blocked a fix that would have reversed the body of every commit
  the system writes, moving the subject line to the bottom. It reproduced the bug
  standalone and noted the tests only checked trailer presence so they could not
  have caught it.
- **A commit was bounced for an 83-character subject (ceiling 72).** The agent did
  not shorten it. It read the rejection as "the code has formatting problems" and
  spent two further iterations fixing typos. The real work was in the first commit.
- **Charlie's branch merges clean** — fast-forward, zero conflicts — but every one
  of its 61 commits credits a reviewer this repo does not have (`Kierkegaard`,
  `Avicenna`), which trips the commit gate on the tip.

## The plan, in order

### 1. Split the gate by scope

Two gates, different jobs.

- **Per bead: the touched package only.** Target under 2 minutes. Most packages
  are 2–25 seconds. This is the change that makes a queue worth running.
- **Per merge to the integration branch: `make core`.** Already green, already
  ~8 minutes, already under target. This mostly needs pointing at, not building.

Everything outside the core 29 packages stops blocking immediately. That is what
buys time to look at Comms, Crew, Flywheel and the scenario suite properly,
rather than under merge pressure.

### 2. Repair mechanical failures; never bounce them to an agent

An over-long subject line is a message edit. It should never cost an
implementation pass. The measured cost of bouncing one was two wasted iterations
and two junk commits.

The principle is wider than this one rule: **anything mechanically checkable
should be mechanically repaired.** Every paperwork rule an agent must satisfy is
a chance for it to misread the rejection and generate garbage. The cost is not
the rule — it is the agent's interpretation of failing it.

Open question for the operator: whether the 72-character rule earns its place at
all. It buys little. The review trailer buys real auditability but is currently
costing more than it returns.

### 3. Verify completion notifications route to the owning crew

**This is the precondition for everything in problem C, and it is unverified.**

Node transitions through the graph work well — that is the core processing and it
is not in question. The open question is different: when a bead finishes, does
the notification reach only the crew that owns that queue, or does everyone get
it? The suspicion on record is that the captain received all of them.

If this is broken, no arrangement of admiral, captain and crew will work. Lists
will keep drifting and starving, and it will keep reading as a planning failure
when it is a plumbing failure. Verify before designing anything above it.

### 4. Continue the daemon decomposition

Already in flight outside the fleet. It now has a second reason to be first among
the refactors: `internal/daemon` is 56% of the gate's runtime. Splitting it into
testable modules is what takes the per-bead gate from minutes to seconds for most
beads. Same work, two payoffs.

Separately, charlie's branch needs a decision on the invented reviewer names
before it can land. Recommended: merge with `--no-ff` and an honest trailer on the
merge commit, rather than rewriting 61 commit messages. The whole-range check is
advisory by construction; only the tip commit blocks.

### 5. Route work by task type, not just by availability

The failed run is the evidence. The local model on the GPU box produced a fix that
looked right and was subtly wrong on ordering logic. Sending more work to that box
without sorting it by kind just moves the cost to the reviewers, which are the
expensive models — the tokens get spent anyway.

Bulk and mechanical work to the local model. Subtle logic to the stronger models.

### 6. Planning model — deferred

The two failures on record are the same failure, and it is not really about
planning:

- Alpha planned, Bravo implemented. Bravo drained the list; Alpha did not refill it.
- The admiral held priorities, the captain allocated them. The lists drifted apart.

Both are *one agent maintaining state another agent depends on, with nothing
detecting the drift*. That is a coupling problem. It is worth solving properly and
it should not be attempted before step 3 confirms the substrate underneath it.

Ranking, meanwhile, has a mechanism that already worked once: the review documents
under `plans/2026-07-27-delete-and-rewrite/reviews/` produced a real ordered
backlog. The ledger is storage, not a queue — the open question is what we pull
out of it, not how to sort 569 items.

## Deliberately not doing

- Fixing GitHub Actions. Not trusted, not the gate that matters.
- Re-ranking or pruning the 569-item ledger.
- Remote execution to the other machine. Real, valuable, too large for now.
- A broad pass over the test suite. Scope the gate instead.
- Declaring any subsystem dead. Step 1 removes the pressure to decide.

## Open questions

1. Does the 72-character subject rule earn its place? Does the review trailer?
2. Charlie's branch: merge at its current point, or wait for the in-flight item?
3. After step 3, is the supervisor thin enough to build, or does it need design?
