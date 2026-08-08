---
schema_version: 2
assessor_name: assessor-alpha-bravo-merge
epic_id: hk-s1cvx
branch: work/alpha-integration-merge
gate: merge
commit: TIP-OF-BRANCH   # resolve with: git rev-parse work/alpha-integration-merge
found_by_sources: [assessor, operator]
report_path: .harmonik/reports/alpha-bravo-merge-gate.md
spawned_by: operator
---

# Gate: merge — lanes alpha and bravo, the candidate for `main`

You are the **assessor** for **hk-s1cvx**. Run the **merge** gate on an isolated scratch clone you
own, file findings as `found-by:assessor` beads scoped with `hk-s1cvx`, post a reasoned
**PASS|BLOCK** to the **operator** over `--topic gate`, and self-terminate.

**Every question the previous handoffs told you to settle first is settled below. Do not re-open
them. Start at §Step 1.**

## Current State

### What you are gating

Two lanes finished work on the same base commit `3ca2c85a8`. Together they are the candidate for
`main`. Both are pushed to origin, so nothing is at risk.

| Lane | Branch | Commit | Content |
|---|---|---|---|
| alpha | `work/alpha-integration-merge` | branch tip | the core queue data race fix `a01fbcec`, a gate-document correction `4ecab4f35`, and this mission file. **`a01fbcec` is the only code change.** |
| bravo | `work/bravo-reachability` | `16efbb28b` | 5 commits: close-on-exec check, sleep-marker reorder, socket-path guard, queue candidate-file leak, claim-failure revert |

**There is no merge commit, and you are not waiting for one.** The merge onto a shared branch is an
operator step and it has not happened. `git merge-tree` reports **0 conflicts** between the two tips
(verified 2026-08-08). Your contract §Merge-gate step 4b already tells you to gate the **merge
result**, so you build it yourself, inside your own scratch clone, where it is throwaway. §Step 2
below gives the exact commands.

### The independence question is DECIDED — read this before you plan the CR leg

You built bravo's five commits. Your contract §Bounds bars you from grading work you helped build.
The operator has decided: **you are the assessor, and you recuse on your own five commits.**

That means, concretely:

- **You grade alpha's two commits and the merge result** yourself. You did not build them, so your
  independence over that surface is intact.
- **Bravo's five commits get a FRESH sub-agent** for the cold-code-review leg — one with no history
  in either lane. Contract §Delegation model already binds every sub-agent to the same independence
  rule, so this is that rule applied, not an exception to it.
- **You relay that sub-agent's findings on those five commits. You do not overrule them.** You may
  ask it for more evidence. You may not soften a finding against your own work.
- **Your verdict states the carve-out on its face** — which commits you graded and which you did
  not. A partial verdict that says so is honest. A full verdict that hides the conflict is not.

This is a deliberate operator decision. Record it in the verdict as such.

### Both assessor-blockers are CLEARED for the tree you launch from

Two P0 issues were marked closed against a tree that did not have their fixes. Re-verified
2026-08-08:

- **The contract named a build target that does not exist.** `make check-short` is not in the
  Makefile. The corrected `operating.md` names `make core` and `make full` instead, and says in
  place that the target must not be re-added. **Do not go looking for `check-short`.**
- **The scratch-daemon script could audit the wrong commit and report it green.** The fixed script
  requires `--rev`, forces the tree to it, reads HEAD back, and names the revision in every
  subcommand.

**Your one-command safety test on any tree before you gate from it:**

```
grep -c -- '--rev' scripts/scratch-daemon.sh    # 22 = safe. 0 = do not gate from this tree.
```

Both fixes are present on `work/alpha-integration-merge` and on `work/bravo-reachability`. They were
absent from `/Users/gb/github/harmonik` at `de4b9baeb`. The operator fast-forwards that checkout to
the **tip of `work/alpha-integration-merge`** before starting you — that is what puts this mission
file there too. Run the grep anyway and refuse if it answers 0.

**No commit hash for the alpha tip is written down anywhere in this file, on purpose.** An earlier
draft hardcoded one, and every edit to this document moved the tip and made the hardcoded value
wrong. Resolve it yourself in §Step 1 and quote what you resolved. Only `a01fbcec` carries code on
this lane; the rest are documents.

## Step 1 — stand up the scratch clone, pinned

```
REV=$(git -C $HARMONIK_PROJECT rev-parse work/alpha-integration-merge)
scripts/scratch-daemon.sh init   <scratch-path> --rev "$REV" --source $HARMONIK_PROJECT
scripts/scratch-daemon.sh build  <scratch-path>
scripts/scratch-daemon.sh up     <scratch-path>
scripts/scratch-daemon.sh status <scratch-path>
```

`status` must print a **bare commit hash**. `NOT PINNED`, `DRIFTED`, `MODIFIED`, or any
`+local-edits` suffix means no result from that tree is an audit of that commit. Rebuild from a
clean tree. Never fold a `+local-edits` result into a PASS.

## Step 2 — build the merge result inside the scratch clone

Do this in the scratch clone only. Never on a shared branch.

```
git -C <scratch-path> fetch $HARMONIK_PROJECT work/bravo-reachability
git -C <scratch-path> merge --no-ff FETCH_HEAD -m "assessor scratch: alpha+bravo candidate"
git -C <scratch-path> rev-parse HEAD          # quote THIS hash in the verdict
```

Expect no conflicts. **If you get one, that is a finding — stop and report it**, because it
contradicts a measurement this mission is built on.

Run `make core` and `make full` against **both** trees: the pinned alpha commit alone, and the merge
result. A clean branch that breaks only after merge is exactly what this leg exists to catch.

## Step 3 — the four legs

Per contract: **LT** (drive the real work loop on the scratch daemon), **XT** (adversarial
break-testing fan-out), **CR** (cold diff review — see the recusal above), **MG** (`make core` and
`make full` on the pinned commits).

- **`make core` is the hard gate. A red `core` is a BLOCK.**
- **`make full` is the merge decision and what CI runs.** A red `full` beside a green `core` is a
  real answer, not a contradiction. Report each failure and weigh it. Out-of-core failures are not a
  BLOCK on their own.
- Read the **NOT RUN** section of both test reports. A disabled test is an unproven claim.
- Separate **branch-introduced** failures from **inherited debt**. Inherited debt goes to the
  operator as a separate main-health finding and does not hold the branch.

### What `make full` is expected to do, so you can tell news from noise

`make full` was reported red at the scenario tier all week and **was not reaching that tier** — it
died two stages earlier. Four of five stages now pass, including the whole-tree tests and
`lint-allow`. `test-scenario` is red, and its 19 failures are already isolated into three groups:

- **One real product defect** — the core queue data race, `hk-e7y44`. **Alpha fixed it in
  `a01fbcec`, which is in your candidate.** 4 detector reports in 8 isolated runs before, 0 in 12
  after. **Confirm it drops out of the failure set.** That is a specific thing to check, not a thing
  to assume.
- **One deterministically broken test** — asks for a retired workflow mode, then fails on a sensor
  that reads other runs' events.
- **Fifteen structural under-budgets** (`hk-scenario-budgets-structural-udd1t`). Four cannot fail on
  their named property and two are duplicates (`hk-scenario-tests-cannot-fail-zlu0f`).

**Those 15 are inherited debt, not branch-introduced.** Do not BLOCK the candidate on them. Do
report whether the count moved.

## Step 4 — verdict

Your verdict is **your reasoned judgment, not a bead tally**. An empty finding list never by itself
means PASS. Your first-class duty is reconciling claimed-done against reality: for every item this
mission claims complete, confirm it against the actual commits, diffs and test results. Beads drift.
Artifacts do not.

**The verdict names the commits it graded** — the pinned alpha commit, the bravo tip, and the scratch
merge hash. A verdict that cannot name its revision is not a verdict.

File findings: `br --db /Users/gb/github/harmonik/.beads/beads.db create ... --labels found-by:assessor,hk-s1cvx`.
Leave every finding UNASSIGNED. **Never `close`, `claim` or `reopen`** — the daemon owns terminal
transitions. Post the verdict to the operator over `--topic gate`, then self-terminate. This is one
verdict, not a standing loop.

## Traps that have already cost this project time

- **`br` inside a worktree reads a decoy database and answers with silence, not an error.** It
  reported 0 open issues while the live ledger held 3,651. Always pass
  `--db /Users/gb/github/harmonik/.beads/beads.db`. There are three database files in that
  directory: `beads.db` is live, `harmonik.db` is a mirror that lags it, `issues.db` is stale since
  May. Filed as `hk-7zzk7`.
- **Never pipe `make core` or `make full` into `tail` or `grep`** — a pipeline returns the LAST
  command's exit status. `make core > log 2>&1; echo exit=$?` also always reports 0, for the same
  reason. Record the exit code inside the log and grep the file.
- **Do not run a gate while sub-agents work in the same tree.** A reviewer's mutation compiled into
  a gate run once and read as a real red. Give each sub-agent its own worktree, or run in sequence,
  and diff `git status --porcelain` across the run.
- **Run `make leakcheck` before any timing measurement, not `uptime`.** A gate run leaked a test
  binary at 100% CPU for 76 minutes, so every "quiet box" claim after it was wrong.
  `hk-gate-leaks-spinning-test-binary-6iqal`.
- **Check `df -g /` before and after any timing run.** The daemon pauses dispatch below 10 GiB free
  and it reads as a flaky test.
- **A status is a claim.** Six of 27 issues were fixed and still open. Two `assessor-blocker` P0s
  were closed against a tree without their fixes. Re-derive before you trust.
- **An absence is not evidence.** A test asserting a file is gone passes identically if it was never
  created. Prove the machinery ran.
- **Never `cd` into a worktree or the scratch clone.** Operate from `$HARMONIK_PROJECT` via
  `git -C <absolute path>` and the scratch-daemon script.

## Open items that are NOT yours and must not hold this gate

`hk-fabricated-review-trailer-zbkqf` (review trailers are load-bearing and unverifiable) ·
`hk-pd732` (the no-reviewer escape hatch) · three commits already ancestors of the integration
branch that claim no reviewer was reachable when one was. All three need an operator, not an
assessor. Note them in the verdict as context. Do not gate on them.
