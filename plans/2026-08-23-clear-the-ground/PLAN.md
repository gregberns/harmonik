# Clear the ground — 2026-08-23

Operator-directed. Written short on purpose: two plans written this week were rejected for being
large piles of confident text. If this one grows past a page per workstream, it has the same disease.

**The frame.** We are not building anything new until the rot is out. Every item below is
subtraction, or it is a structural change that makes later subtraction possible.

## What is NOT an input to this plan

`plans/2026-08-21-back-on-track/` and `plans/2026-08-22-decomposition-program/` are rejected by the
operator. Nothing here descends from either. Do not read them for context; do not revive their task
IDs. If a future agent cites `T01`–`T68` or the "back on track" step list, it has picked up something
this plan deliberately dropped.

The one prior document this plan does build on is
`plans/2026-07-27-delete-and-rewrite/reviews/2026-08-22-follow-up-review/` — the operator pointed at
it directly, and its findings are source-verified. Its line-count measurements are one day stale
(the comment cut landed after it). Its structural findings are not.

## Measured starting state, 2026-08-23

| | |
|---|---|
| Go production | 193,655 lines |
| Go tests | 332,828 lines — 1.72× production |
| `internal/daemon` | 33,566 production lines, 110 files, 414 files total in one flat directory |
| `internal/core` | 30,208 production lines across **449 files in one flat directory** |
| `cmd/harmonik` | 24,491 production lines in `package main` |
| `runWorkLoop` | 816 lines, **22 parameters**, cyclomatic complexity 161 |
| Shell scripts | 35,484 lines across 140 files; `scripts/` alone is 30,741 |
| Makefile | 1,712 lines, 149 targets, references 68 scripts |
| Lint exclusion list | 575 pairs (in a 580-line file; five lines are header) — 249 daemon, 106 cmd/harmonik, 37 core; 161 are complexity suppressions, counting `gocognit` 124, `cyclop` 31 and `funlen` 6 |
| Issue ledger | 626 open, 476 bugs (re-counted 2026-08-23 evening; the "none closed since 15 August" line went stale within hours of being written — Charlie closed beads that same evening) |

**Do not measure this program in lines.** The comment cut removed 14,945 production lines from the
daemon overnight and changed no architecture. `runWorkLoop` "shrank" from 1,432 lines to 816 with its
complexity of 161 untouched. Every workstream below states a structural acceptance test instead.

---

## W0 — Measure it, and stop the one thing that wedges restart

Two tasks that the rest of the program leans on. Both were written and given issues, and both were
filed under `W1` — but W1 is the exclusion-list burn-down and neither of these is that. They are
collected here, and `TASKS.md` and their task files now say `W0`.

- **W0.1 — `structural-scoreboard`.** Standing rule 2 below says a task is not done without a
  structural acceptance test. This is the thing that measures one. Until it exists, the rule has no
  instrument and the program cannot tell real progress from a comment deletion. Nothing blocks it.
- **W0.2 — `dispatch-activation-guard`.** Make it impossible to wire the crash-safe dispatch
  producer by accident. Cheap, and the thing it prevents can permanently wedge restart. Nothing
  blocks it.

## W1 — Unblock file moves, then burn the exclusion list down

**Operator ruling, 2026-08-23: change the rule.** The ratchet in `scripts/lint-allow-ratchet.sh`
says the exclusion list may never gain a pair. A pair is keyed `<path> <linter>`, so moving a file
mints a new key and reads as new debt. That is what has blocked every extraction.

A rename-aware fix was attempted and reverted on 2026-08-23 because an adversarial review found three
routes to forge a rename and mint a free exemption. **That attempt left no trace in this repository**
— checked 2026-08-23 evening across the main checkout and every worktree; one commit has ever touched
`scripts/lint-allow-ratchet.sh` and it is unrelated. Nobody can now read the reverted code or the
review that rejected it, so treat the three routes as an unauditable caution. **Do not re-attempt
that fix** — but the reason is the operator's ruling, which stands on its own: change the rule
instead of teaching the ratchet to detect renames.

- **W1.1 — Re-key the exclusion list off file paths.** A tolerated finding should be identified by
  what it is, not where it lives. The requirement is a property: **the key must contain neither a
  file path nor a package name.** A pure move then changes no key and the ratchet never fires. Note
  a package qualifier is still location and does not satisfy this — see the task file, which also
  carries the collision question the implementer has to answer. *Done when:* moving any file between packages with no
  content change leaves the pair set byte-identical, proved by a test that performs a real move.
- **W1.2 — Keep the ratchet's one real job.** It must still refuse a genuinely new tolerated finding.
  *Done when:* a deliberately-introduced new finding is still rejected, proved by mutation.
- **W1.3 — Burn down the centre. NOT YET A TASK FILE AND NOT YET AN ISSUE** (noted 2026-08-23
  evening). It is named in the order below and nothing tracks it, so as written it will silently
  never start. It is also the largest single piece of W1, and it wants splitting per linter before
  anyone picks it up. 392 of 575 pairs sit in `daemon`, `cmd/harmonik` and `core`. Fix
  the findings; do not re-tolerate them. Order: `gosec` first — these are security findings we are
  currently ignoring — then `errcheck`, then `unused`. **Scoped to the three centre packages the
  counts are 70, 40 and 27**, not the 106, 45 and 36 an earlier draft gave; those are whole-tree
  figures and they oversize the work by about 40%. *Done when:* each is zero in the three centre
  packages.
- **W1.4 — Leave the complexity suppressions for last, on purpose.** All 161 of them — 124
  `gocognit`, 31 `cyclop`, 6 `funlen` — are a symptom of W2 and W3. They should disappear because
  the functions got smaller, never because the finding got fixed in place.

W1.1 blocks W3 and W4. Nothing else waits on it.

## W2 — Chop `runWorkLoop` completely

**Nobody is doing this.** Charlie is on the instruction corpus, not the run machine. This is the
work item the operator named first and it currently has no owner.

The target is not line count. It is: 816 lines, 22 parameters on one signature, complexity 161,
driving the whole dispatch loop.

- **W2.1 — Collapse the 22 parameters into named state.** Thirteen of them are already `*Port`
  structs of function fields (`loopLifecyclePort`, `schedulePort`, `diskReclaimPort`, …) declared
  across seven files. They are one thing. *Done when:* the signature is under 6 parameters and the
  loop's mutable state is a named type, not a stack frame.
- **W2.2 — Extract one pure decision at a time.** Each extraction must come with a table test that a
  deliberate mutation proves observes the production path. *Done when:* `runWorkLoop` is under all three
  ceilings that apply to it — `cyclop` 15, `gocognit` 20, `funlen` 100 lines and 60 statements —
  and each extracted decision has a test that fails when the decision changes. It measures 161, 505
  and 384 statements today. See the task file: the numbers are invisible until the `//nolint`
  directive above the function is stripped.
- **W2.3 — Same treatment for the next four.** `run() int` (770 lines), `runBeadSubcommandIO` (540),
  `beadRunOne` (501), `driveDotWorkflow` (495). Serialize these behind one owner — they touch the
  same files and parallel lanes will collide.

**Acceptance for the whole workstream is the three complexity ceilings, not the line count.**
`funlen` counts statements as well as lines, so deleting comments moves none of them.

## W3 — Break `internal/core` into real packages

449 files averaging 126 lines in one flat directory. It holds 26% of all remaining comment lines
(20,529) — the comment cut took only 2,074 from it while taking 16,008 from the daemon.

- **W3.1 — Name the clusters before moving anything.** Event types, daemon events, agent events and
  reconciliation events are already visible as separate concerns in the file names.
- **W3.2 — Move by cluster, one package per landing.** Each landing carries its own tests.
- **W3.3 — Then re-examine the comments.** Much of the density is doc comments on exported event
  types, which is legitimate. `eventtype.go` is 68% comment; that is worth reading before cutting.

Blocked by W1.1.

## W4 — Make `cmd/harmonik` a thin command-line tool

24,491 production lines in `package main`, the repo's lowest test ratio (0.85). It is the CLI: it
should parse arguments, call a package, and print. Almost none of that volume should be there.

- **W4.1 — Assess first.** Report what is in it, which parts are genuine CLI plumbing and which are
  business logic that belongs in a reusable package. **No moves until this lands.**
- **W4.2 — Move logic out behind the assessment's boundaries.** Largest first: `comms.go` (1,897),
  `keeper_enable_doctor_cmd.go` (1,207), `main.go` (991), `keeper_cmd.go` (904), `harness.go` (898).
- **W4.3 — `run() int` at 770 lines is a W2 item, not a W4 item.** Do not do it twice.

Blocked by W1.1. W4.1 is not.

## W5 — Can we still change this code?

332,828 test lines against 193,655 production lines. The operator's stated fear: if the test mass
stops us changing code, the program fails. The delete-and-rewrite already removed 225,000 lines and
the ratio is still 1.72:1.

- **W5.1 — Measure the real cost of a change, not the line count.** Take three representative small
  production changes and record how many test files each forces open and why. *Done when:* we can
  say which tests pin behaviour and which pin structure, with a number.
- **W5.2 — Report before proposing.** No deletion campaign until W5.1 says what the mass is made of.
  The last sweep took ten behavioural tests with it and they had to be restored (`11c058b95`).

## W6 — The `crew-cleanup` skill

Operator's design: a skill that knows the places old junk accumulates, lists what it found, and
**asks before deleting**. Not an automated reaper — a checklist with a human gate.

Known accumulation sites: `.harmonik/crew/` (17 of 29 mission files have neither a live crew nor any
reference — ~120 KB), `.harmonik/comms/cursors/`, `.harmonik/comms/cursors-live/`,
`.harmonik/cognition/`, `.harmonik/beads-owned/`. Also in scope: 25 shell scripts with no caller
anywhere (~2,600 lines), four root handoff files nothing references, and a 44 KB handoff archive.

*Done when:* running it on this repo lists every site with counts and ages, proposes a deletion set,
and deletes nothing without confirmation.

**This is a distraction if it grows.** One skill, one pass, no new subsystem.

## W7 — The reviewer definition

Two separate things, both operator-raised.

- **W7.1 — Fix the live drift.** `9e70fbe4b` landed the "a Go comment is not a normative doc"
  exclusion in `.claude/skills/agent-reviewer/SKILL.md` only. `.claude/agents/agent-reviewer.md` is
  1,936 bytes short and still enforces the retired rule, so every spawned reviewer sub-agent is
  currently manufacturing comment-only commits. *One file.*

  **Overlaps W8, checked 2026-08-23 evening.** Charlie's branch edits this same file, but only to
  swap a script path in the `APPROVE` paragraph; it does not touch the comment rule. So this is real
  work, it is not already done, and the two edits sit in different parts of the file. Whichever lands
  second resolves the overlap. This task is dispatched on `charlie-q`.
- **W7.2 — Check it is any good, and check it travels.** The operator's concern: other projects will
  need their own reviewer, so harmonik's protocols must not be baked into it. Report which parts are
  general review judgement and which are harmonik-specific, and whether the split is clean enough
  that another project could take the general half.
- **W7.3 — Govern the copies.** Four physical copies of the shipped skill set exist
  (`cmd/harmonik/assets/skills/`, `.claude/skills/`, `.harmonik/agents/_skills/`, `.claude/agents/`).
  Only the first pair is covered by any rule, and one of the ungoverned pair is what drifted.

## W8 — Instruction corpus

**Corrected 2026-08-23 evening. Charlie's work is committed, not in flight.** It is 4 commits on the
branch `work/charlie-file-diet`, 129 files, +9,088/-16,718, and the worktree is clean. W8.1 is a
merge someone has to perform, not a lane anyone is waiting on.

- **W8.1 — Merge `work/charlie-file-diet` into the integration branch. DONE 2026-08-23 evening**,
  as `208d95fec`. It was not a fast-forward — both branches had moved since they parted at
  `aa423dcc0` — and exactly one file conflicted, `.harmonik/crew/missions/charlie.md`. Charlie's
  side edited the July root-cause mission this branch had already replaced with the queue-manager
  mission, so the resolution kept this branch's file and folded in the one measured correction
  charlie's side carried. The merged tree compiles. **It is not yet pushed: the `make full` decision
  is still outstanding**, and the repo rule is that a timeout never approves. Charlie's whole
  `internal/daemon` diff is one path-helper swap and a comment, so it is very unlikely to be the
  cause of a red gate, but that has to be measured rather than argued.
- **W8.2 — Document the three structural causes** so the re-measure has something to check against:
  (1) four physical copies, only two governed; (2) `AGENTS.md`'s load map and the agent manifests are
  two independent, disagreeing specifications of what each role loads — neither is a superset of the
  other; (3) one rule restated in many places (the daemon-owns-closing rule appears in 21 files;
  `br ready --limit 0` in 24).
- **W8.3 — Re-measure after it lands.** Baseline before Charlie: ~531,000 tokens reachable, captain
  cold boot 51–67K.

---

## Order

**Start now, no dependencies, parallel:** W0.1, W0.2, W1.1+W1.2 (one owner), W2 (one owner), W4.1, W5.1, W6, W7.

**After W1.1 lands:** W3, W4.2, W1.3 — and W1.3 needs writing up first; see above.

**W8 is a merge, not a lane.** Charlie's instruction-corpus work is already committed on its own
branch and waits only on the merge-gate ruling. The "nothing else touches `.claude/skills/` or
`.harmonik/agents/`" freeze is lifted — there is no uncommitted work left to protect. W7.1 edits a
file that branch also edits; see W7.1 for why that is safe.

**Serialize W2 under a single owner.** It and W4.3 touch the same functions.

## Staffing

**Corrected 2026-08-23 evening: the queue is in use.** Charlie now runs as a queue manager and is
dispatching these issues through `charlie-q`. Tasks must therefore be self-contained enough for an
implementer that reads the task file and nothing else. If a task file is ambiguous, that is a defect
in the task file, not a question for the implementer.

## Standing rules for this program

1. **Never widen the exclusion list.** W1 changes how it is keyed, not what it tolerates.
2. **A structural acceptance test, or the task is not done.** Complexity, parameter count, package
   boundary, caller count — never a line count.
3. **Deletion needs no permission; moves need W1.1.** If a thing has no caller and no reference,
   removing it is not a decision that needs a gate.
4. **Do not resurrect the two rejected plans.**
