# Cases — live run lifecycle under failure

Format and rules: [`README.md`](README.md).

**This file holds the `protocol` cases, and they are the ones that earn their keep.** Every
expensive finding lane bravo has produced came from one of these, not from a probe. A protocol
is a way of looking, with a question in hand. It does not reduce to an exit code, and it is
supposed to be run by someone who is paying attention.

---

## LP-010 — protocol: ask every health surface the same question and compare the answers

Class: protocol
Exercises: the whole run lifecycle, and every surface that claims to report on it
Bead: found `hk-stop-hook-failure-wedges-run-dc5z6` and two others
Status: the specific defect is FIXED at `2361a2c0d` + `04351ed60`; **the protocol stays open
forever** — it is a method, not a case that closes

**The question:** every health surface says this run is fine. Is it?

Preconditions: a scratch daemon with one real bead dispatched to a real agent. Not a twin —
the point is to observe a system nobody scripted.

Method. At intervals through the run, and especially when nothing seems to be happening, ask
EVERY surface independently and write down all the answers side by side:

    harmonik queue list --project "$SCRATCH"        # workers, status
    harmonik queue status --project "$SCRATCH"      # the operator-facing summary
    harmonik subscribe --project "$SCRATCH" --json  # the event stream, structured
    tail .harmonik/events/events.jsonl              # what was actually written
    git -C <worktree> log --oneline                 # ground truth: did work land?
    ps / tmux ls                                    # is the agent process alive at all?

Then ask the question the surfaces cannot ask themselves: **when did a real event last
arrive?** Not a heartbeat — a real one. Heartbeats continue after the work stops.

Expect: the surfaces agree with each other AND with git.

Failure signature — the shape to recognise, which is more general than any one bug:

> The surfaces agree with each other and disagree with reality.

The instance that produced the bead: an implementer finished, its completion signal failed to
reach the daemon, and for 79 minutes there was no `agent_failed`, no `run_stale`, nothing in
the daemon log, `agent_heartbeat` arriving on cadence, and `queue list` showing
`status=active workers=1`. Last real event at 04:24:54Z. At 05:43:49Z a budget backstop fired
and the run moved on recording `commit_landed: false`.

Why it matters: three separate findings fell out of one sitting with this protocol, and all
three pointed the same way. **A run that is dead and a run that is quiet look identical on
every surface this system offers.** That is the finding; the individual bugs are instances.

Two traps this protocol has already caught in its own results:

- **The recovery can name the wrong cause.** The backstop that fired reported
  `implementer_budget_exceeded`, so a later reader sees an agent that ran out of budget rather
  than one whose completion signal could not be delivered. Check that the recovery names the
  cause, not just that a recovery happened. Mislabels like this cost the project twelve days
  once already, on the pi endpoint probe.
- **A bounded wedge is not the same as no wedge, and it is also not a P1.** This was first
  filed as an unbounded wedge and that was wrong — a backstop existed and it fired. Find the
  backstop before you set the severity.

---

## LP-011 — protocol: run the same bead on every harness and compare what the daemon believes

Class: protocol
Exercises: the completion contract across pi, codex and claude
Bead: found `hk-quit-instruction-not-portable-ms55w`, `hk-codex-success-retried-and-lost-rqxz3`
Status: both FIXED at `b49210d67` and `5450d1cfc`; protocol stays open

**The question:** the agent did the work and committed it. Does the daemon agree?

Method: dispatch one trivial, unambiguous bead — append a dated line to a file — to each
harness in turn. For each run, record separately:

1. Did the agent do the task? (`git log` on `run/<run_id>`, read the diff)
2. Did the agent exit cleanly? (exit code, terminal event)
3. What did the daemon record? (`run_completed` / `run_failed`, `sub_reason`, `commit_landed`)
4. Did the work land anywhere a human would find it?

Expect: (1) and (3) agree.

Failure signature: **(1) is yes and (3) is no.** Both instances found had this shape and each
had a different mechanism:

- **pi** did the task, committed it, then could not end its own session. It was told to run
  `/quit` — a Claude REPL command — and having only a shell, it ran `echo "/quit" | pbcopy`
  and copied the string to the clipboard. Killed on the budget at 158s, recorded as a crash.
  Commit `b55dacb76` real and stranded on `run/<id>`.
- **codex** did the task, committed it, and exited correctly with `commit_landed=true`,
  `exit_code=0`, a clean `terminate_complete` — and was retried anyway. The retry failed on a
  bad thread id and the retry's failure replaced the success. Commit `7482bfc0a` stranded the
  same way.

Why it matters: a harness that does the work correctly and is scored as a failure is the most
expensive defect class in this system. It burns the tokens, produces the commit, and throws it
away — and the record blames the agent, so the investigation starts in the wrong place.

**Always look at the commit, not the verdict.** Both of these read as harness failures in
every summary the daemon produced.

---

## LP-012 — protocol: break the environment the gate runs in, not the code it checks

Class: protocol
Exercises: commit gate failure classification
Bead: `hk-gate-env-failure-blamed-on-agent-g4w5q`
Status: FIXED at `c4d61aa4e` + `f92b9a5df`; protocol stays open

**The question:** when the gate cannot run, who does the daemon blame?

Method: stand up a scratch clone the way the documented assessor sequence does, dispatch any
trivial bead, and watch what the commit gate does. The interesting condition is a gate that
**cannot execute**, as distinct from a gate that executes and finds a problem.

Expect: a gate that could not run is classified as an environment failure and is never routed
back to the implementer to fix.

Failure signature: `gofumpt: No such file or directory`, exit 127 — command not found. The
gate could not run and therefore found nothing wrong. The daemon classified that as a
deterministic failure and resumed the implementer to fix it. The implementer then spent about
twenty minutes of Opus time installing five build tools and running the whole merge-decision
suite, for a one-line documentation change.

Root cause worth remembering: `.tools/` is gitignored and the scratch-daemon script had no
`make tools` step, so a fresh clone never had the toolchain.

Why it matters: **a gate that cannot run reports nothing, and nothing reads as approval or as
the agent's fault depending on which way the code leans.** This one contaminated a whole
three-harness result matrix before it was found — the live-verify leg stands its scratch up
through the same scripts, so every cell in that matrix was red for this reason.

Before concluding anything from a red matrix, check that the gate could execute at all.

---

## LP-013 — protocol: check the gate can pass BEFORE you read anything into a run failure

Class: protocol
Exercises: the whole dispatch loop's dependence on repo health
Bead: `hk-4sp1z`
Status: OPEN at `daf396b41` — the gate cannot currently pass

**The question:** is this run red because of the run, or because the tree is red?

Method: before dispatching anything, run the commit gate by hand on the tree you are about to
test from. The standard workflow's `commit_gate` node is `make full`, fail-closed, and costs
roughly 22 minutes per pass.

    out=$(make full 2>&1); rc=$?; echo "rc=$rc"

Expect: exit 0 before you dispatch anything.

Failure signature at `daf396b41` — two independent blockers, and the second is hidden behind
the first:

1. The reachability gate exits 2 on 20 `internal/keeper` names left behind by a port-split
   refactor. `make fast` and `make full` share every static step, so this stops both.
2. Behind it, `make full` tests EVERY package, and the root module contains `evaltasks/`,
   where `eval-bugfix-rate-limiter` holds a bug **on purpose** — grading an agent on fixing it
   is the entire point of the fixture. Its own test catches that bug and fails. The other
   twelve fixtures pass.

Why it matters: with the gate red, every dispatched run fails at the gate for a reason that
has nothing to do with the agent's work, gets routed back to the implementer, and burns ~22
minutes plus Opus tokens per pass. **Any conclusion drawn about the daemon from such a run is
worthless.**

The sequencing trap: fixing (1) does not turn the gate green, it reveals (2) — and (2) fails
with output that looks exactly like an ordinary broken test in a package nobody touched.
