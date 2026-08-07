---
schema_version: 2
assessor_name: assessor-queue-dogfood-readiness
epic_id: "codename:queue-dogfood-readiness"
branch: phase1-session-restart-substrate
gate: deploy
commit: 4480650eaa4b81e353e04760a631038ef382dc40
found_by_sources: [assessor, admiral, fast-follow]
report_path: plans/2026-07-17-assessor-daemon-campaign/runs/readiness-4480650e/ASSESSMENT.md
spawned_by: admiral
---

# Gate: deploy — phase1-session-restart-substrate @ 4480650e

> **RE-PINNED 2026-08-06.** The `commit:` above and the `readiness-4480650e`
> segment of `report_path` name the current branch tip. Both lane branches are
> merged into it: lane bravo's test-honesty lane at `4480650ea` and lane alpha's
> hang-detection fix at `0c0c56d68`. Four branches are still unmerged and each is
> deliberate — `work/alpha-sat32` holds one unharvested refinement, and
> `work/alpha-specrepair`, `work/alpha-step10-stage8` and `work/xray` are work in
> progress. None of them belongs to this candidate.
>
> **RE-PIN AGAIN IF THE BRANCH MOVES BEFORE YOU SPAWN.** Whoever spawns the
> assessor MUST set both fields to the tip being judged and MUST confirm no lane
> that belongs to this candidate is still unmerged. An assessor that judges a
> stale commit returns a verdict about code nobody is shipping.
>
> **The gate-kind disagreement is settled — do not re-litigate it.** The
> readiness-gate amendment to `specs/assessor-handoff-schema.md` is retired at
> `fa6fe25f`, so the spec no longer contradicts itself. `hk-7bfqe` stays open only
> because the daemon owns bead closure and it was down. This mission uses `deploy`
> at `schema_version: 2`, which the schema accepts.

You are the **assessor** for the queue-dogfood-readiness work on branch
**phase1-session-restart-substrate** at commit **4480650e**. Run the **deploy**
gate on an isolated scratch clone. Prove that a small queue run is safe to start
under the queue-only posture. File each confirmed defect as a scoped
`found-by:assessor` bead with `--label codename:queue-dogfood-readiness`. Post a
reasoned `PASS|BLOCK` to **admiral** over `--topic gate`. Then self-terminate.
The admiral holds the decision. You execute and recommend.

## Current State

### 0. The validation record gates everything

Read the validation record before you submit anything to a queue. Refuse the
gate if the record is missing or failing. Read it again as your first action
after any keeper restart. It is the one artifact that decides whether the run
may start.

```
cat <evidence>/validation.json
```

You produce the record yourself in section 3a step 6. Nothing before that step
touches a queue: you clone, you build, you file three beads in the clone's own
ledger, and you measure the host. The moment the record exists, it decides.

Refuse and post `--topic error` when any of the following is true.

1. The file is absent.
2. `accepted` is `false`.
3. `host.daemons_alive` is `0`.

Reason for the third rule. The daemon count is measured with `pgrep`, and a
broken pattern reads as zero. Zero clears the ceiling, so a broken probe and a
clean host look the same. The premise of this gate is that the fleet daemon is
UP, so the honest count before the scratch daemon boots is exactly **1**. A `0`
means the measurement broke. Treat it as a failing record, not a clean host.

The `make queue-dogfood-readiness` target writes both `readiness.json` and
`validation.json` into the `EVIDENCE` directory you name. Run it before you
bring the scratch daemon up. Once the scratch daemon is up the host carries two
daemons, the count is 2, and the validator refuses with
`daemon_count_above_limit`. That refusal is correct and it is not a defect.

### 1. Why this gate is a deploy gate and not a new gate kind

`gate` is `merge` or `deploy`. The schema enforces the pair and nothing here
needs a third. This gate has the deploy gate's exact shape: prove an isolated
end-to-end run on one named commit, and confirm the deploy-readiness
preconditions the mission names (`.harmonik/agents/assessor/operating.md`
§Deploy-gate steps 1 and 2). Section 4 below is those preconditions. The
`commit` field is what pins the candidate. The branch is context.

`specs/assessor-handoff-schema.md` used to carry a late amendment that added a
`readiness` gate at `schema_version: 3`. That amendment was retired on
2026-08-05 and the spec is back to one version. There is no longer a
disagreement to raise, and there is no `readiness` gate. Use `deploy` at
`schema_version: 2`, which is what this mission declares.

### 2. The launch, and which daemon you may kill

The admiral spawns you **with the fleet daemon UP**.

```
harmonik crew start assessor \
  --queue assessor-queue-dogfood-readiness-q \
  --mission .harmonik/crew/missions/assessor-queue-dogfood-readiness.md
```

The launch needs the fleet daemon. It serves the crew-start call and writes the
registry record that keeps your session off the boot-time orphan sweep. Never
stop it, restart it, or run a bare `harmonik <unknown-subcommand>`, which starts
a daemon.

The daemon you kill is the **scratch** daemon in the throwaway clone. Two guards
in `scripts/scratch-daemon.sh` hold the two apart. `guard_path` resolves symlinks
and refuses the script's own repository root. `assert_not_supervised` refuses any
project that already has a live supervisor session. Drive every scratch action
through the script from your own CWD. Never `cd` into the clone.

The scratch daemon stays down after `scratch-daemon.sh down` because the posture
switches `supervisor_watchdog` off. Confirm that in the boot wiring table the
scratch daemon prints on stderr before you trust the shutdown drain.

### 3. The candidate items

Three items. Each is a path swap in one Markdown file. Each rebuilds zero Go
packages. Each is behavior-free, and a reviewer can approve it on first read.
None of them edits a file under `internal/daemon`. Naming a file in that package
is not editing it.

**Item 1 — the canary.** File `docs/known-workarounds.md`. Replace the one
occurrence of `internal/daemon/dot_cascade.go` with
`internal/daemon/dot_cascade_helpers.go`. The file it names was split into
`dot_cascade_core.go` and `dot_cascade_helpers.go`, and the exact expression the
sentence describes, `append(os.Environ(), env...)`, is now in
`dot_cascade_helpers.go` and nowhere else in the package.

**Item 2.** File `docs/codex-enablement.md`. Replace the one occurrence of
`internal/daemon/codexlaunchspec.go` with `internal/harness/codex/launchspec.go`.
Replace **both** occurrences of the bare `codexbillingguard.go` with
`internal/harness/codex/billingguard.go`. Both files moved to
`internal/harness/codex/`, so both mentions are stale, and naming both keeps the
result the same whichever way the item runs. The same file carries a separate
stale claim about `NewCodexHarness`, which no longer exists. It is out of this
item's scope. Leave it and file it as a finding.

**Item 3.** File `docs/design/crew-harness-select-pi.md`. Replace every
occurrence of `internal/daemon/piharness.go` with `internal/harness/pi/harness.go`
and every occurrence of `internal/daemon/pilaunchspec.go` with
`internal/harness/pi/launchspec.go`. There are two places. Both moved.

Three files, no file shared, so the three can run at the same time and cannot
conflict. Run them at **concurrency 3**. The point of the run is to prove that
several items run at once. The earlier one-item rule is withdrawn, and
`internal/queue/readiness` no longer carries a rejection for a concurrency other
than one. The record states the count and the concurrency, and the stated item
count must equal the number of items you name.

Each item needs its own repeat-safe reason in the record. One sentence covering
three items is two items taken on trust. Use these three reasons.

1. "One path swap in one Markdown doc. It rebuilds no Go package, and a re-run
   from the same pinned commit lands the same one-line change."
2. "One path swap in one Markdown doc. Every occurrence is named, so a re-run
   lands the same file content whatever order it edits them in."
3. "One path swap in one Markdown doc. Both stale names are named, so a re-run
   from the same pinned commit lands the same file content."

### 3a. Where the three items come from, and in which ledger

The mission names the work. It cannot name the bead IDs, because a bead ledger
is machine-local and gitignored, and the clone these items run in does not exist
until you build it. File the three beads yourself, in the **scratch clone**, and
record their IDs before you capture the readiness record. Do not file them in
the fleet ledger. The scratch daemon dispatches from the clone it was started
with, and `scratch-daemon.sh feedback` is the only path that ever writes to the
fleet ledger — you never run it.

Order of operations.

1. `scripts/scratch-daemon.sh init <scratch> --rev <commit>` and `build <scratch>`.
   `--rev` is required and names the commit under test.
2. Give the clone its own ledger if it has none. `br init --db
   <scratch>/.beads/beads.db`. Every `br` call below carries the same `--db`,
   which is how you stay out of the fleet ledger without leaving your own CWD.
3. `br create --db <scratch>/.beads/beads.db` one bead per item. Each bead's
   body is the item text from section 3, quoted whole, including the exact
   replacement strings. A reviewer must be able to approve it on first read
   without opening anything else.
4. Confirm the split. `br list --db <scratch>/.beads/beads.db` must show exactly
   your three beads. The fleet ledger must not have grown.
5. Record the three IDs.
6. Run `make queue-dogfood-readiness` with `QDR_PROJECT` pointed at the clone,
   because the capture reads every selected item from the live ledger through
   one `br show` per item, and the fleet ledger does not hold these three.

```
make queue-dogfood-readiness \
  QDR_PROJECT=<scratch> \
  SCRATCH=<scratch> \
  EVIDENCE=<evidence dir> \
  CONCURRENCY=3 \
  BEADS='<id1>=<reason 1>;<id2>=<reason 2>;<id3>=<reason 3>'
```

`QDR_PROJECT` defaults to the current directory, which is the fleet checkout.
Leaving the default would read the wrong ledger and the capture would fail on
three unknown beads. If it instead succeeds, stop — that means three beads with
those IDs exist in the fleet ledger, and you are about to dispatch fleet work.

### 4. The operational test for "repeat-safe"

Run both checks. The first is cheap and comes first.

**Check A — the edit is idempotent.** Apply the stated substitution to a fresh
clone by hand. Apply it a second time. The second application must leave
`git -C <scratch> diff` empty. Record the two diffs.

**Check B — two runs land the same tree.** Each item states its exact final
text, so two runs of the same item must produce the same file content.

1. Build clone A at the pinned commit. Run the batch. Record the landing
   commit's tree with `git -C <scratchA> log -1 --format=%T <branch>`.
2. `scratch-daemon.sh down <scratchA>`. Delete clone A.
3. Build clone B at the same pinned commit. Run the same batch. Record its tree
   the same way.
4. Repeat-safe holds when the two tree hashes match, each run touched the same
   one file per item, and nothing outside the clones changed.

Prove the last part rather than assert it. `git -C <fleet checkout> status
--porcelain` must be empty before and after. `find <fleet checkout> -newermt
<start time>` must name nothing. A survival claim on its own is not evidence.

### 5. The host preconditions, with the numbers

The validator compares the measured host against these three limits. They are
`internal/queue/readiness` `DefaultHostLimits` and the `QDR_*` defaults in the
`Makefile`. Do not guess and do not change them.

| Limit | Value | Refusal |
|---|---|---|
| Load average per CPU | at or below **1.0** — that is, load at or below the CPU count | `host_load_above_limit` |
| Free disk on the scratch path | at or above **10 GB** | `host_free_disk_below_limit` |
| Live daemons | at most **1**, and exactly **1** in practice | `daemon_count_above_limit` |

Measure the CPU count with `getconf _NPROCESSORS_ONLN`. The target measures load,
CPUs, free disk and the daemon count for you and prints them on one line.

The one-daemon rule is the fleet daemon and nothing else. Capture and validate
before `scratch-daemon.sh up`. See section 0 for why a count of `0` is a refusal
and not a pass.

The run plan is also refused for a Pi harness, a named remote worker, a
cross-repository target, a wave queue, an unstated queue kind, an unstated item
count, an unstated concurrency, and `feedback`. Set `--harness claude`,
`--queue-kind stream`, and no remote worker. Never run
`scratch-daemon.sh feedback`. It is the one subcommand that writes to the fleet
ledger.

A result from a loaded box is not evidence, green or red. If the host drifts
above a limit mid-run, say so and re-run on a quiet box. Do not report a load
flake as a product failure. Do not report it as a pass either.

### 6. Waived legs, each with its waiver

Four legs are mandatory at a merge gate: LT, XT, CR and MG. This is a deploy
gate. Two are waived, one is narrowed, and one stands in full.

Read the waivers as what they are. `good-enough-principles.md` §2 says all five
requirements must hold for a PASS, and §4 applies the same five to a deploy
gate. That bar authorizes no waiver, so these three come from the admiral's gate
authority and not from your judgment. You may not widen them and you may not add
one. If the evidence tells you a waived leg was needed, say so and BLOCK. Name
every waiver in the report, and say plainly that this gate did not meet all five
requirements, so nobody later reads this PASS as a full merge-gate PASS.

**LT — the core-loop matrix is waived. The canary batch replaces it.**
`make core-loop-lt` measures harness coverage across pi, codex and claude cells.
This gate measures whether a queue run is safe to start under the queue-only
posture, which is a different question. The matrix also needs its pi cells, and
the readiness validator refuses a Pi harness outright, so those cells cannot be
part of a run this record can accept. The live-verify leg here is the three-item
batch through `scratch-daemon.sh batch`, driven by the real task-processing loop
on the scratch daemon. Report its `BATCH_SUMMARY` line and the per-item verdicts.

**XT — waived. Card T12 owns it.** Exploratory break-testing for this work is a
separate card with eight named test beads, run after the scenario pass. Running
an adversarial fan-out inside this gate would repeat that work and would not
change this gate's question. The blast radius here is bounded by construction:
the clone is throwaway, `isolate_push_target` repoints origin at a local bare
repository, the batch is one named queue of a known item count, and the daemon
comes down when the batch ends. Say in the report that XT was waived and that
T12 still owes it. Do not let the waiver read as coverage.

**CR — narrowed, not waived.** A cold review of the whole branch diff is not what
this gate decides, and every commit on the branch already passed a per-commit
review gate. What you must still do is reconcile claimed-done against reality for
the four cards this gate rests on, which is the assessor's first-class duty and is
never waived. For each of T8a, T8c, T9 and T9a, confirm the claim against the
actual commit, the diff and a test.

- **T8a** claims a queue-only run posture. The artifact is the `subsystems:`
  block in `scripts/scratch-config-overlay.yaml`. Confirm the scratch daemon
  boots under it, and read the boot wiring table it prints on stderr to confirm
  each switched-off subsystem was never constructed. Do not trust the config.
- **T8c** claims the supervisor watchdog is switchable. The symbols are
  `internal/projectconfig` `SubsystemSupervisorWatchdog` and `cmd/harmonik`
  `startSupervisorWatchdogIfEnabled`. Confirm you can kill the scratch daemon and
  watch the drain reach a terminal result with nothing respawning under it.
- **T9** claims `harmonik queue readiness capture|validate` and
  `make queue-dogfood-readiness`. You exercise all three in section 0.
- **T9a** claims a retained event capture. **This bullet used to say it looked
  unlanded. That was read on 2026-08-04 and it is now wrong** — the work landed
  afterwards, and a re-read on 2026-08-06 found both halves in
  `scripts/scratch-daemon.sh`. The capture is retained at
  `<scratch>/.harmonik/batch-<name>.events.ndjson`, not a `mktemp` file removed
  on exit, and `SCRATCH_BATCH_EVENT_TYPES` carries six types rather than three:
  the three run terminals plus `bead_closed`, `outcome_emitted` and
  `bead_ledger_recovered`. Confirm it against a real batch anyway, because a
  variable holding the right names does not prove the file survives teardown.
  Two recovery events are deliberately absent and the script says why beside the
  list: `bead_terminal_transition_recovered` has no emitter at all, and
  `queue_item_reconciled` fires before the socket binds, so a live-only
  subscriber can never see it. Neither absence is a defect. If an ordering test
  bead needs `queue_item_reconciled`, it needs a cursor-seeded read of
  `events.jsonl` — say so rather than filing the filter.

**MG — required in full, and budget for it.** Run **`make full`** on the pinned
commit in the scratch clone. `make full` is the merge decision and it is what CI
runs. It tests every package, it never scopes by what changed, and it never
approves on a timeout, an OOM, a compile failure or a passing retry. A PASS is
impossible while any step of it is red on a branch-introduced issue.
Pre-existing debt is a separate main-health finding for the admiral and is never
charged against this branch.

**`make full` is red at the pinned commit, on one test, and it is pre-existing.**
Measured on the fleet checkout on 2026-08-06: 106 of 107 packages pass, 74577
tests, and the single failure is
`TestColdStartToken_ALocalRunTakesNoColdStartToken` in `internal/daemon`. Expect
to meet it. It is filed as `hk-ziouf` with two independent measurements two days
apart, and the earlier one measured it failing MORE at the base commit
`e0c6d1304` than after the merges, so it is not branch-introduced. Under the rule
above it is a main-health finding for the admiral, not a charge against this
candidate.

Two things about it will mislead you if nobody says them first.

- **It passes when you re-run it, and that is not evidence.** The ratio is about
  1 in 6. A single green re-run means nothing. To confirm the state yourself use
  `-count=6` and expect roughly one failure per batch.
- **Its own assertion message names the wrong cause.** It says a local run took a
  cold-start token it must skip. What the test actually observes is only that no
  agent appeared within 30 seconds. A passing run takes 3.5 seconds and a failing
  one takes exactly the 30 second limit with nothing in between, which is a run
  that never launched rather than a slow one. Read the bead before you spend time
  on the cold-start gate.

Report it as a confirmed pre-existing failure and do not let it alone decide the
gate. If a SECOND package is red in the scratch clone, that is new information —
the fleet checkout had exactly one — and it is worth stopping for.

Do not run `make check-short`. That target is retired and the repo has two gate
targets only: `make fast` while you work, `make full` before anyone accepts the
work. Any budget quoted from a `check-short` run describes a gate that no longer
exists.

Your own role documents still order it. `.harmonik/agents/assessor/operating.md`
step 4b and `.harmonik/agents/assessor/good-enough-principles.md` §2.5 both name
`make check-short` and both say a PASS is impossible while one of its checks is
red. The target they name is gone, so the order cannot be carried out as
written. Run `make full` in its place. The intent of §2.5 is that your gate is a
superset of CI, and `make full` is what CI now runs, so `make full` serves that
intent and `check-short` no longer can. Report the stale order to the admiral as
a documentation finding. Do not treat the missing target as a red check.

One way the swap is narrower, so say so in the report. §2.5 names `go test
-short -race`. `make full` runs the race detector over the scenario tier and not
over every package, so the race coverage of the retired target is wider than
what you run. Name that gap in the residual-risk section.

Budget the time before you start. `make full` runs `go test -short -count=1
./...` under `GATE_GO_TIMEOUT`, which is **25 minutes**, and then adds the
whole-tree lint allow list, the scenario tier, the crash tier and module
hygiene. The Makefile's own note on `full` puts `internal/daemon` at about five
minutes, which matches the roughly 308 seconds the card that scoped this gate
quotes. That is why the canary items must not touch that package. The batch has
its own separate ceiling: `SCRATCH_BATCH_TIMEOUT` defaults to **1800 seconds**.
A timeout is not a pass. Report it as a timeout.

### 7. Where the evidence goes

`.harmonik/reports/` is gitignored, so a report written there cannot travel with
the candidate. Everything durable goes in the campaign's tracked run directory
instead, which is why `report_path` points there.

```
plans/2026-07-17-assessor-daemon-campaign/runs/readiness-4480650e/
  ASSESSMENT.md                  the verdict — this is report_path
  RUN-LOG.md                     append-only, one block per command
  CANDIDATE.md                   branch, pinned commit, and the clone's detached HEAD
  evidence/readiness.json        copied from the EVIDENCE directory
  evidence/validation.json       copied from the EVIDENCE directory
  evidence/batch.json            copied from <scratch>/.harmonik/batch-<name>-<qid>.json
  evidence/events.jsonl          copied from <scratch>/.harmonik/events/events.jsonl
  evidence/scratch-daemon.log    copied from <scratch>/.harmonik/scratch-daemon.log
  evidence/make-full.log         the MG leg output
```

Copy every file out of the clone before teardown. The clone is deleted and
nothing inside it survives.

The directory name carries the pinned commit's first eight characters. If the
admiral re-pins the candidate, the directory changes with it, and a verdict under
an old directory is stale and must be re-run.

### 8. What a PASS means here

Not "the tests are green". It means the three items ran together on the scratch
daemon under the queue-only posture, the run was repeat-safe by both checks in
section 4, the host cleared every limit in section 5 with a real measurement, the
CI merge gate was green on branch-introduced checks, and the four cards in
section 6 reconcile against real commits and tests.

Weigh it against `.harmonik/agents/assessor/good-enough-principles.md`. A PASS
authorizes one thing: this batch, at this concurrency, on this pinned commit, in
a throwaway clone. It authorizes no fleet run. If you BLOCK, say exactly what
would clear the block. Post the verdict to admiral over `--topic gate` and
self-terminate.
