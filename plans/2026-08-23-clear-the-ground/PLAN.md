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
| `runWorkLoop` | 816 lines, **22 parameters**, cyclomatic complexity 158 |
| Shell scripts | 35,484 lines across 140 files; `scripts/` alone is 30,741 |
| Makefile | 1,712 lines, 149 targets, references 68 scripts |
| Lint exclusion list | 580 pairs — 249 daemon, 106 cmd/harmonik, 37 core; 155 are complexity suppressions |
| Issue ledger | 606 open, 477 bugs, **none closed since 15 August** |

**Do not measure this program in lines.** The comment cut removed 14,945 production lines from the
daemon overnight and changed no architecture. `runWorkLoop` "shrank" from 1,432 lines to 816 with its
complexity of 158 untouched. Every workstream below states a structural acceptance test instead.

---

## W1 — Unblock file moves, then burn the exclusion list down

**Operator ruling, 2026-08-23: change the rule.** The ratchet in `scripts/lint-allow-ratchet.sh`
says the exclusion list may never gain a pair. A pair is keyed `<path> <linter>`, so moving a file
mints a new key and reads as new debt. That is what has blocked every extraction.

A rename-aware fix was attempted and reverted on 2026-08-23 because an adversarial review found three
routes to forge a rename and mint a free exemption. **Do not re-attempt that fix.** The operator's
ruling makes it unnecessary: change the rule instead of teaching the ratchet to detect renames.

- **W1.1 — Re-key the exclusion list off file paths.** A tolerated finding should be identified by
  what it is, not where it lives. Key on `<linter> <symbol-or-finding-identity>`; a pure move then
  changes no key and the ratchet never fires. *Done when:* moving any file between packages with no
  content change leaves the pair set byte-identical, proved by a test that performs a real move.
- **W1.2 — Keep the ratchet's one real job.** It must still refuse a genuinely new tolerated finding.
  *Done when:* a deliberately-introduced new finding is still rejected, proved by mutation.
- **W1.3 — Burn down the centre.** 392 of 580 pairs sit in `daemon`, `cmd/harmonik` and `core`. Fix
  the findings; do not re-tolerate them. Order: `gosec` (106 — these are security findings we are
  currently ignoring), then `errcheck` (45), then `unused` (36). *Done when:* each is zero in the
  three centre packages.
- **W1.4 — Leave the complexity suppressions for last, on purpose.** The 155 `gocognit`/`cyclop`
  entries are a symptom of W2 and W3. They should disappear because the functions got smaller, never
  because the finding got fixed in place.

W1.1 blocks W3 and W4. Nothing else waits on it.

## W2 — Chop `runWorkLoop` completely

**Nobody is doing this.** Charlie is on the instruction corpus, not the run machine. This is the
work item the operator named first and it currently has no owner.

The target is not line count. It is: 816 lines, 22 parameters on one signature, complexity 158,
driving the whole dispatch loop.

- **W2.1 — Collapse the 22 parameters into named state.** Thirteen of them are already `*Port`
  structs of function fields (`loopLifecyclePort`, `schedulePort`, `diskReclaimPort`, …) declared
  across seven files. They are one thing. *Done when:* the signature is under 6 parameters and the
  loop's mutable state is a named type, not a stack frame.
- **W2.2 — Extract one pure decision at a time.** Each extraction must come with a table test that a
  deliberate mutation proves observes the production path. *Done when:* complexity is under 30 and
  each extracted decision has a test that fails when the decision changes.
- **W2.3 — Same treatment for the next four.** `run() int` (770 lines), `runBeadSubcommandIO` (540),
  `beadRunOne` (501), `driveDotWorkflow` (495). Serialize these behind one owner — they touch the
  same files and parallel lanes will collide.

**Acceptance for the whole workstream is the complexity number, not the line count.**

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
- **W7.2 — Check it is any good, and check it travels.** The operator's concern: other projects will
  need their own reviewer, so harmonik's protocols must not be baked into it. Report which parts are
  general review judgement and which are harmonik-specific, and whether the split is clean enough
  that another project could take the general half.
- **W7.3 — Govern the copies.** Four physical copies of the shipped skill set exist
  (`cmd/harmonik/assets/skills/`, `.claude/skills/`, `.harmonik/agents/_skills/`, `.claude/agents/`).
  Only the first pair is covered by any rule, and one of the ungoverned pair is what drifted.

## W8 — Instruction corpus

Charlie has this in flight — 13 skill files modified and uncommitted in its worktree. **Do not touch
those files from another lane.**

- **W8.1 — Land Charlie's work.**
- **W8.2 — Document the three structural causes** so the re-measure has something to check against:
  (1) four physical copies, only two governed; (2) `AGENTS.md`'s load map and the agent manifests are
  two independent, disagreeing specifications of what each role loads — neither is a superset of the
  other; (3) one rule restated in many places (the daemon-owns-closing rule appears in 21 files;
  `br ready --limit 0` in 24).
- **W8.3 — Re-measure after it lands.** Baseline before Charlie: ~531,000 tokens reachable, captain
  cold boot 51–67K.

---

## Order

**Start now, no dependencies, parallel:** W1.1+W1.2 (one owner), W2 (one owner), W4.1, W5.1, W6, W7.

**After W1.1 lands:** W3, W4.2, W1.3.

**Charlie continues on W8 alone.** Nothing else touches `.claude/skills/` or `.harmonik/agents/`.

**Serialize W2 under a single owner.** It and W4.3 touch the same functions.

## Staffing

Queues are not being used; agents work tasks directly. This plan assumes hand-run lanes and
sub-agents, not queue dispatch. Fixing the queue is not in this plan.

## Standing rules for this program

1. **Never widen the exclusion list.** W1 changes how it is keyed, not what it tolerates.
2. **A structural acceptance test, or the task is not done.** Complexity, parameter count, package
   boundary, caller count — never a line count.
3. **Deletion needs no permission; moves need W1.1.** If a thing has no caller and no reference,
   removing it is not a decision that needs a gate.
4. **Do not resurrect the two rejected plans.**
