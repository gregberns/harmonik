---
id: ops-monitor-ignores-lane-status
title: The fleet-stall wake reads every field in the lane index except the one that says the lane is parked
type: bug
priority: 1
labels: [scripts, ops-monitor, wake-economy, clear-the-ground]
depends_on: []
blocks: []
workstream: W7
batch: 7
---

## Problem

`scripts/ops-monitor-check.sh` Check 9 — the block headed `Check 9: known-ready-lane`, which builds
`_LANE_CANDIDATES`, `KNOWN_READY_LANE`, `KNOWN_READY_LANE_EPIC` and `KNOWN_READY_LANE_COUNT` — picks
the lane it will name in an `[IMMEDIATE]` wake. Its `jq` filter selects on two fields: `.epic_id` and
`.gate`. The word `status` does not appear in it. So a lane the index records as `parked` is a
candidate on exactly the same terms as an active one.

That was harmless for six weeks because the `lanes` array in `.harmonik/context/lanes.json` was
empty. The tier-file repair on 2026-08-24 — the commit titled "the tier files a captain reads as
ground truth were six weeks wrong", `081b38b6e` — re-seeded it with four live lanes, and it is no
longer empty. (The bead credits the neighbouring commit `c99dbeaa8`, which landed fifteen seconds
later and does not touch the file. Measured with `git show --stat`. The date is right and the commit
is not; it changes nothing about the defect.) **Measured, against the live index
on `work/alpha-integration-merge`,** by running Check 9's own `jq` program over the live file:

```
sandbox	hk-scaj0	status=parked
```

One lane comes out, and it is the parked one. The other three lanes (`alpha`, `charlie`, `bravo`)
carry `epic_id: null` and are dropped by the first `select`, so `sandbox` is not one candidate among
several — it is the only one the check can currently name.

**The wake is armed, not firing.** `br ready --parent hk-scaj0 --limit 0 --json` returns `[]` today,
so `KNOWN_READY_LANE` stays empty and the predicate is false. The last snapshot at
`.harmonik/ops-monitor/latest.json`, written `2026-08-24T20:45:24Z` — five minutes after the index
was re-seeded — records `known_ready_lane = ""` and `program_drained_stall = False`. The check has
already run against the re-seeded index. It is one ready bead away from firing.

`lanes.json` says the same thing about itself, in its own `_doc`: the stall check "could never fire
while this array was empty, and now it can. The sandbox lane is a live candidate."

### What fires, and where it lands

When a bead under `hk-scaj0` becomes ready while the fleet is idle with a free slot,
`program_drained_stall` goes true and this string joins `immediate_signals`:

```
program-drained-stall:lane=sandbox,epic=hk-scaj0,ready=1
```

It is sent to the captain on the `ops-monitor` topic, and it re-sends every `IMMEDIATE_COOLDOWN`
(30 minutes) for as long as the condition holds. The check's own header says the `[IMMEDIATE]` tier
exists so "the captain cannot self-score its way out of" it. That is the right design for a real
stall and the wrong one for a lane nobody meant to staff.

**Be accurate about how far it travels, because the bead overstates it.** `program-drained-stall` is
not in `CRITICAL_PREFIXES`, so the escalation block skips it: it never reaches tier 2 or tier 3 and
it never emits to the `ops-CRITICAL` operator-wake topic. Measured by reading the escalation loop —
it `continue`s on any signal outside `CRITICAL_PREFIXES`. So the cost is a repeating captain wake on
a false stall every 30 minutes, not a pager. That is still worth fixing and it is still P1, and a
captain woken with a lane name in hand is meant to act on it, which is the harm. It is not an
operator page.

The epic itself says why the lane is parked, and it is not a stall: `hk-scaj0` is assigned to `lima`,
a crew that has been offline for weeks, and its own bead body records that the container-vs-wrapper
call "belongs to the operator and it gates the design."

### One other consumer already reads `status`, and it agrees with the fix

`DashLane` in `internal/daemon/dashboardtypes.go` decodes the field, and `harmonik dashboard` uses
it: `filterLanesByStatus(snap.Lanes, "active")` in `cmd/harmonik/dashboard_cmd.go` shows only lanes
whose status is exactly `active`. So the dashboard already hides the sandbox lane while Check 9 wakes
the captain about it. Two consumers of one file, opposite answers.

Note the shape of that precedent before you copy it. The dashboard uses an allow-list — it shows
`active` and hides everything else, including a lane whose status is missing or a word nobody has
used yet. Check 9 must use a deny-list instead: only the literal `parked` suppresses, and anything
else fires. **Hiding a row is cheap and muting a wake is not**, so an unrecognised status should
leave the alarm armed rather than silence it. That asymmetry is the reason the two look different,
and it is worth one sentence in the comment so the next reader does not "fix" the inconsistency.

## The disagreement, which is the real reason this is a P1 and not a one-line patch

Three texts already say something about a parked lane, and they do not agree. Read all three before
touching the `jq`, because the obvious patch reverses the check's founding case.

1. **The index's own rule.** `.harmonik/context/lanes.json` `_doc`: "A lane is KNOWN/resumable when
   gate is null or expired; GATED only when a non-null unexpired gate object is present." Status is
   not in that predicate. **The file and the code agree today.**
2. **The check's founding incident.** The comment block above `LANES_FILE` says Check 9 exists
   because a 2026-06-25 stall happened when "a known, **parked**, already-ranked lane" was
   mis-classified as needing an operator ruling. On its own wording, a parked lane with ready work is
   precisely the case the check was built to catch.
3. **The standing rule for orchestrators.** The `orchestrator-rules` skill (`REFERENCE.md`,
   §Self-authorization) and `captain/SKILL.md` both say a parked lane is KNOWN and resumable on the
   orchestrator's own authority, and that "**PARKED is a fact, not a hold**." The hold mechanism is a
   named, dated, owned, expiring gate — not the status field.

So `status == "parked" → skip` is the wrong rule stated broadly. Applied to every lane it turns the
status field into an unowned, undated, never-expiring mute, which is the thing texts 2 and 3 exist to
prevent, and it silently reinstates a hold that an operator let expire.

**The rule below threads all three.** It changes exactly one lane shape — parked with no gate at all
— and leaves every gated shape deciding by its gate, as all three texts say it should.

## The rule to implement

A lane is a candidate when its `epic_id` is non-null AND:

- **it carries a gate object** — the existing expiry logic decides, unchanged, and `status` is not
  consulted. A future expiry excludes the lane; an expired, missing or malformed expiry lapses it
  back to candidate, which is the documented autonomous default. An operator who let a hold expire
  gets the lane back whatever its status says.
- **it carries no gate at all** — it is a candidate only when its status is not `parked`.

`status` is absent-tolerant: a missing, null or unrecognised status reads as **not parked**, so an
older index that predates the field behaves exactly as it does now. Only the exact string `parked`
suppresses.

## The obvious edit does not work, and it fails silently

**Do not** add `and ((.status // "active") != "parked")` to the existing `(.gate == null)` disjunct.
Measured: with that edit the live index still emits `sandbox status=parked`.

The reason is a `jq` behaviour that is easy to miss. Indexing `null` yields `null` rather than an
error, so for a lane with no gate the second disjunct `((.gate.expires // null) == null)` evaluates
to **true** and re-admits the lane the new clause just excluded. The three disjuncts silently assume
a non-null gate, so the status test cannot live beside them — it has to live on the other side of a
branch on `.gate`.

This form is measured green against the live index and against every existing fixture:

```jq
| select(
    if .gate == null then
      ((.status // "active") != "parked")
    else
      ((.gate.expires // null) == null)
      or ( ( ((.gate.expires + "T00:00:00Z") | fromdateiso8601?)
             // (.gate.expires | fromdateiso8601?)
             // 0 ) < $now )
    end
  )
```

Measured results of that filter, one row per case:

| lane shape | before | after |
|---|---|---|
| `sandbox` — epic set, gate null, status `parked` (live index) | candidate | **skipped** |
| `alpha` / `charlie` / `bravo` — `epic_id: null` | skipped | skipped |
| `wake-economy` — epic set, gate null, status `active` (Test 36) | candidate | candidate |
| `gated-lane` — future gate, date-only and RFC3339 (Test 39a/39b) | skipped | skipped |
| `lapsed-lane` — past gate, status `parked` (Test 39c) | candidate | candidate |
| epic set, gate null, **no `status` key at all** | candidate | candidate |

## An existing test pins the shape next door. It must stay green.

`test/exploratory/ops_monitor_check_test.sh` **Test 39c** drives a lane with `status: "parked"` and a
gate expiring `2000-01-01`, and asserts the wake **does** fire and names `lapsed-lane`. Under a
blanket parked-skip it goes red.

**Test 39c going red means the rule was implemented too broadly, not that the test is stale.** Do not
update it, do not delete it, do not weaken its assertions. It is the regression guard for the
lapse-to-autonomous default, and the rule above is written the way it is so that this case keeps
passing. Treat a red 39c as your own signal to go back to the `jq`.

The same applies to **Test 36**, whose `wake-economy` fixture is `status: "active"` with a null gate
and which asserts the wake fires and names the lane. It must stay green.

**No existing case covers a parked, gateless, epic-bearing lane** — which is why this shipped. Yours
is the first.

## Scope

- `scripts/ops-monitor-check.sh`, the Check 9 block only: the `_LANE_CANDIDATES` `jq` program, and
  the comment block above `LANES_FILE` that states the candidate rule in prose. That comment must
  state the new rule, including the `gate`-versus-`status` split and the absent-status default. Do
  not touch the Python analysis, `program_drained_stall`, the `checks` map, or any other check.
- `test/exploratory/ops_monitor_check_test.sh`: one new case, plus its line in the `DONE-CHECK` list
  at the top of the file — that list is how this harness records coverage and it is kept current.
- `.harmonik/context/lanes.json`: the `_doc` sentence "A lane is KNOWN/resumable when gate is null or
  expired" is now false and must gain the status clause. It is tracked in git, so this is a real
  commit and not a local-only edit. **The `lanes` array itself is not yours** — do not add a gate to
  the sandbox lane, do not change any lane's status, do not add or remove a lane.

## Done when

1. **A new case in `test/exploratory/ops_monitor_check_test.sh` fails first, then passes.** Add it as
   Test 46 — 45 is the highest number in the file. Fixture: a lane with a non-null `epic_id`,
   `gate: null` and `status: "parked"`, `--br-parent-json` giving that epic at least one ready bead,
   the same idle-fleet event and `max_concurrent: 4` queue stub that Test 36 uses. Copy Test 36's
   `setup_fixture` call and change the lane index — do not invent a new fixture shape. Assert
   `program_drained_stall` is false, `known_ready_lane` is empty, `program-drained-stall` is absent
   from stdout, and no comms was sent.
   Run it against the unfixed script first and **paste the observed RED output into the commit
   body.** A test that was never seen red proves nothing here.
   Add a second case beside it — 46b — with the same fixture and a status of `quiesced`, a word this
   project does not use. It asserts the wake **does** fire and names the lane. That is what pins the
   deny-list, and without it the next author can quietly turn the rule into an allow-list and break
   nothing visible.
2. **The whole harness runs, and it introduces no failure that your own baseline did not already have.**
   `bash test/exploratory/ops_monitor_check_test.sh`. **It is NOT green today and that is not your
   doing.** Measured baseline on `work/alpha-integration-merge`, whole file, before any change:

   ```
   Results: 314 passed, 2 failed
     - 27e: release-due should be suppressed at 6 min (30-min IMMEDIATE_COOLDOWN, not 5-min critical)
     - 27e: no release-due comms should be sent within 30-min cooldown
   ```

   Both are in Test 27e, they are about the release-due cooldown, and they have nothing to do with
   Check 9. **Do not fix them and do not fold them in** — a second defect in the same file is a
   separate bead, `hk-release-due-cooldown-chujb`, and it is open.

   **The criterion is that no NEW failure appears — not that the count is exactly two.** That bead
   may land before yours. If it does, the two 27e failures go away and a fully green run is the
   correct result. This baseline has already drifted once, from 313 to 314. So take your own
   baseline on the tip you branch from, before you change anything, and compare against that rather
   than against the block above. What must be true at the end: every failure in your run also
   appears in your own baseline, and the passed count rose by your new assertions.

   The harness exits 1 while any failure stands, so the exit code proves nothing here; read the
   summary block.
   Nothing in `make fast`, `make full` or CI runs this harness — checked — so running it by hand is
   the only evidence that exists.
3. **Test 39c and Test 36 pass inside that run.** Both pass today; the measured lines are
   `PASS: 39c past gate: known_ready_lane == lapsed-lane` and
   `PASS: SD4+ immediate_signals names wake-economy`. Quote your own observed lines in the commit
   body, because these are the two cases a too-broad fix breaks.
4. **The live index produces no candidate.** Run Check 9's `jq` program, as patched, against
   `.harmonik/context/lanes.json` with `--argjson now "$(date +%s)"`. It prints nothing and exits 0.
   Before the fix the same command prints `sandbox`.
5. **A gated lane still decides by its gate.** Pipe the three Test 39 fixtures through the patched
   filter by hand: future date-only → nothing, future RFC3339 → nothing, past gate → `lapsed-lane`.
   Paste the three results.
6. **The four Go tests that read this script's source text still pass:**
   `go test ./cmd/harmonik/ -run 'TestOpsMonitor|TestWatchZombie|TestWatchSkill|TestWatchDownInFlapRetain' -count=1`.
   They assert on strings such as `_dedup_key` in `scripts/ops-monitor-check.sh` and they are
   unrelated to Check 9, so they are a cheap proof that nothing else in the file moved.
7. **`.harmonik/context/lanes.json` `_doc` and the Check 9 comment state the same rule**, in the same
   words, and both mention what an absent `status` means. Two texts that disagree about this field
   are what produced the defect.
8. **`make full` is green.** It is the merge decision.

## Limits

- **Do not skip every parked lane.** The rule is in "The rule to implement" and it is deliberately
  narrower than the one-line reading. A gated lane keeps deciding by its gate.
- **Do not edit `.claude/skills/orchestrator-rules/`, `cmd/harmonik/assets/skills/`, or
  `.harmonik/agents/_skills/`.** "PARKED is a fact, not a hold" stays true and this change does not
  contradict it: that rule is about what a captain **may do on its own authority**, and this task is
  about what a script **shouts about unasked**. A captain may still un-park the sandbox lane whenever
  it judges the work is ready. Those skills have three copies that must stay byte-identical, so an
  edit here is expensive and it is not needed.
- **Do not change the lane data to fix the code.** Adding a gate to the sandbox lane would suppress
  the wake and would also be a false statement — nobody has held that lane with a dated, owned gate.
- **Do not change any Go code.** `filterLanesByStatus` and `DashLane` in
  `cmd/harmonik/dashboard_cmd.go` and `internal/daemon/dashboardtypes.go` are cited as evidence, not
  as scope. The dashboard's allow-list is correct for the dashboard. Leave it alone.
- **Do not touch any other check in the script.** `ops-monitor-never-flags-a-stopped-queue` owns the
  `paused-queues` block in this same file and is a separate task. If both are in flight, this one
  edits Check 9 and that one edits Check 2, and they do not overlap.
- **Do not port anything to Go.** `ops-monitor-check-to-go` is parked on an open operator ruling
  about whether shell-to-Go is a workstream at all. This is a behavioural bug and it does not wait
  for that. If the port lands first, the port carries this rule and this test.
- Do not add a `nolint`, and do not widen `tools/lintreport/allow.txt`.

## Traps

- **The daemon schedule for this check is disabled and the launchd agent is not installed** —
  measured: `harmonik schedule list --json` shows `ops-monitor` with `"enabled": false` and a last
  fire of 2026-07-09, and `harmonik ops-monitor status` reports the plist "not installed". Yet
  `latest.json` was written today, so something runs it by hand on a captain's health tick. Do not
  conclude from the disabled schedule that the check is dead, and do not try to reproduce this
  through the live fleet. The whole defect and the whole fix are observable from the repository with
  `jq` and the test harness, which is how every measured claim above was taken.
- **The harness is slow and it is already red.** Every case shells out to a stubbed binary, so a
  full run costs minutes, not seconds. The one measurement taken here: tests 1 through 17 — 23 of
  the 75 `run_check` calls, 132 assertions — took 2m42s on this box at load average ~19, which
  extrapolates to roughly 9 minutes for the whole file. Treat that as an order of magnitude and not
  a promise; it was extrapolated rather than timed end to end, and it moves with machine load. Do
  not run a subset and call it green. It ends red before you touch anything, and it exits 1 — read
  the summary block rather than the exit code, and see done-when 2 for how to judge the failures.
- **Four run worktrees under `.harmonik/worktrees/` hold their own copies of
  `scripts/ops-monitor-check.sh`.** They are other runs' checkouts. Edit the one at the repository
  root and leave the rest alone.
- **`.harmonik/context/lanes.json` is live operational state as well as a tracked file.** A captain
  rewrites it whenever staffing changes, so your worktree's copy can be behind the one at the
  repository root within the same session. Take the evidence for the live-index check from the file
  at the repository root, re-read the file immediately before you edit the `_doc`, and change nothing
  but that one sentence — a whole-file rewrite would discard a staffing change somebody else made
  while you worked.
