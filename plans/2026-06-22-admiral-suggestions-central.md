# Admiral Suggestions — Central Reference
> Session cf35c324 · 2026-06-21 16:39Z → 2026-06-22 ~18:20Z
> Compiled from: comms history (all messages), admiral-playbook.md, admiral-retro.md, admiral mission.md
> Purpose: single review artifact for the operator

---

## A. Operator-gated decisions admiral flagged (pending your input)

These were surfaced by the admiral as correctly held or approaching — none auto-proceeded.

| # | What it is | Status | Admiral's stated next-action |
|---|---|---|---|
| A1 | **Flywheel live ACT-mode flip** — turning the governor from observe-only to actually intervening on the live fleet | Pending — operator-gated throughout session | Operator signs off; captain dispatched as CD3/CD4 |
| A2 | **leanfleet greenlight (hk-itoc)** — expanding the fleet beyond the current scale-out posture | Pending — operator-gated throughout session | Operator confirms or lets scale-out window lapse (see A6) |
| A3 | **v0.2.0 GPG-signed release push** — the release tag is cut but held for your GPG-signed push | Pending — last mentioned 07:17 | Operator runs the signed push; everything else is green |
| A4 | **C2-governor ownership decision** — who owns the flywheel governor in the production fleet | Pending — surfaced from MORNING-NOTE.md | Two options surfaced by the captain; awaiting operator choice |
| A5 | **C2-keeper-rearm decision** — whether crew keepers auto-rearm or escalate to operator on restart | Pending — surfaced from MORNING-NOTE.md | Two options surfaced; awaiting operator choice |
| A6 | **3-day scale-out window expiry** — the "fill every lane" posture set 2026-06-19 may be superseded by the new token-efficiency direction | Decision-needed — mentioned 18:20 | Re-confirm posture or let it lapse; captain still posturing on scale-out |
| A7 | **C3 remote-box un-park** — remote-substrate Box A campaign was parked on stale failures; those failures are now fixed | Pending — mentioned 06:17, 07:17, 08:17, 12:17 | Operator un-parks the lane; it is now file-disjoint + buildable |
| A8 | **OperatorNFR config-knob prioritization** — deciding which keeper/config knobs are surfaced as first-class operator controls | Decision-needed — first mentioned 12:17 | Operator ranks the knob list; captain implements |
| A9 | **Operator-queue-of-4 concurrency cap** — the "align to 4" directive | Effectively decided — admiral tracked this hourly | Captain transiently bumped to 5 (see D1); admiral noted captain's handoff says "back to 4" |

---

## B. Process / oversight improvements admiral surfaced (retro + playbook)

Items the admiral flagged as improvements for future admiral instances and fleet operation generally.

| # | What it is | Status | Concrete change proposed |
|---|---|---|---|
| B1 | **Ground-truth verification per audit** — every audit relayed captain self-report on trust; no independent git/queue spot-check | Process gap (playbook Rule 2) | Each audit: `git log origin/main \| grep <sha>` for one claimed-landed fix; `harmonik queue list` for actual concurrency vs stated |
| B2 | **Track operator-set knobs hour-over-hour** — kept a stale "4/4 saturated" frame while concurrency was actually 5 | Process gap (playbook Rule 3) | Maintain a standing knob-list (concurrency · DOT model · 300k token cap · file-disjoint lanes) and check each against live state every audit |
| B3 | **Say/do-gap scan as the #1 catch** — the session's only confirmed behavior change came from cross-referencing a captain claim against the absence of a matching operator message | High-value pattern (playbook Rule 1) | Actively look for: captain claim of "told the operator X" with no matching `--to operator` message; operator gate about to be bypassed by automation |
| B4 | **One artifact per audit, then stop** — every audit emitted three restatements (internal score + operator post + "going quiet" recap) | Waste (playbook Rule 8) | Score silently as an internal checklist; emit exactly ONE operator-facing artifact; no closing recap |
| B5 | **Don't narrate the captain's wins** — admiral re-posted at least one milestone the operator already had (Audit 4) | Waste (playbook Rule 9) | If the captain already posted the milestone, stay silent |
| B6 | **Investigate unrecognized sessions touching infra immediately** — 206KB of `named-queues`/`nq-resume` session broadcasts that were redeploying the daemon went unassessed until session end | Missed catch (playbook Rule 7) | Any session in `comms who` that isn't commissioned fleet → assess who/what/authorized; surface to operator if uncoordinated |
| B7 | **Surface throughput slack as a dial, not as drift** — idle crew + free slots + ready backlog = present to operator with the burn-vs-progress tradeoff; do NOT direct the captain to fill every slot | Good pattern to preserve (playbook Rule 5) | Already codified; keep this framing for next admiral |
| B8 | **Synthesize cross-hour patterns** — the cache-reaper "two-incident trouble-spot" and the iter-2-thrash as "dominant constraint" were the two sharpest non-corrective contributions | High-value pattern (playbook Rule 4) | Hold a running memory of recurring trouble-spots and standing paused lanes; react to the trend, not just the hour's snapshot |
| B9 | **Distinguish tactical call from drift; surface process questions** — correctly declined to override fan-out timing, but dropped the process question it raised | Partial credit (playbook Rule 6) | When a tactic is the captain's call, don't override — but surface the underlying process question (e.g. "should fan-out auto-fire at 2 refutations?") to the operator |
| B10 | **Wrong escalation when critical path stalls — suspect lost message before wedge** | Near-miss (playbook Rule 11) | Before recommending a captain restart: (a) direct crew to re-send the verdict; (b) re-ping; (c) only escalate "wedge" if re-sent message gets no response over a real window. "Alive-per-gauge AND daemon auto-merging other lanes" = delivery gap, not dead session |
| B11 | **Scenario tests can't go through the daemon pipeline** — the 30-min commit-budget wall + the thrash bug made the normal daemon path untenable for scenario-test beads | Structural gap (identified during session) | Sanctioned path: worktree-author + independent reviewer + cherry-pick + `harmonik promote`; now institutionalized as a captain memory |

---

## C. Token / cost observations and recommendations

| # | What it is | Status | Admiral's recommendation |
|---|---|---|---|
| C1 | **Paul crew at ~669k tokens** — the Sonnet context-watchdog (meant to catch over-cap sessions) had been dead for ~36h, leaving paul unmonitored and over the 300k cap | Discovered 04:17; 5-agent fan-out launched | Keeper-coverage bead (hk-u5tgh) opened; root cause found (watchdog death); durable fix dispatched to stilgar |
| C2 | **Admiral's own session over-cap** — admiral ran to ~399k (13:17) and then ~457k (18:20), over the explicit 300k cap, keeper-less, with the watchdog unable to restart it | Self-flagged at 13:17 and 18:20 | "Restart me if you want, else I keep auditing" — live data point for the keeper-coverage gap; operator's call |
| C3 | **Token-efficiency direction may supersede scale-out posture** — admiral noted at 18:20 that the new token-efficiency direction likely supersedes the 2026-06-19 "fill every lane" scale-out posture | Decision-needed | Operator re-confirms scale-out posture or replaces it with the token-efficiency framing; captain currently still posturing on scale-out |
| C4 | **~107 ready beads available at 08:17 with fleet running lean** — fleet at 1 active lane; captain was staging ahead of the daemon restart the thrash fixes needed rather than filling slots | Surfaced as operator dial, not drift | Fill with design/research backlog if you want more parallelism now; otherwise lean-staging matches your "don't blast the fleet" directive |

---

## D. Self-flagged issues by the admiral

| # | What it is | Status | Admiral's action |
|---|---|---|---|
| D1 | **Missed the captain's concurrency 4→5 deviation** — kept reporting "4/4 saturated" while actual concurrency was 5; the captain had bumped it to unstick a wedged critical-path bead | Caught late | Flagged at 03:17 ("MINOR: captain transiently bumped concurrency 4→5"); admiral noted captain's handoff says "back to your 4" |
| D2 | **No independent ground-truth verification throughout** — relayed "fix LIVE fleet-wide" and "concurrency back at 4" on the captain's word without a single git or queue spot-check | Self-diagnosed in retro | Codified as the mandatory ground-truth Rule 2 in admiral-playbook.md |
| D3 | **Output redundancy — three restatements per audit** — internal ①–⑤ scoring block + operator post + "going quiet" recap; the recap added ~0 value | Self-diagnosed in retro | Codified as Rule 8 (one artifact, then stop) in admiral-playbook.md |
| D4 | **206KB of unrecognized session broadcasts unassessed** — `named-queues`/`nq-resume` sessions were redeploying the daemon; admiral noticed at session end, couldn't assess | Self-diagnosed in retro | Codified as Rule 7 (investigate stranger sessions NOW) in admiral-playbook.md |
| D5 | **Throughput slack surfaced one cycle late** — the precursor was visible an audit earlier; the persistent `leto-fw paused-by-failure` lane was never tracked proactively across audits | Self-diagnosed in retro | Addressed in playbook Rule 4 (hold a running memory of recurring trouble-spots) |
| D6 | **Over-escalated the FIX3 stall as "captain wedged"** — actual cause was a verdict lost across a keeper-restart; the fix was "re-send the verdict," not a captain restart | Self-corrected at 17:17 | Playbook Rule 11: cheapest-hypothesis-first ordering before recommending restart |

---

## E. Alignment corrections the admiral made to the captain

Concrete directives the admiral sent to the captain (via `--to captain --topic directive`).

| # | When | What the admiral directed | Captain's response | Outcome |
|---|---|---|---|---|
| E1 | 17:17 (Audit 2) | **"Nail the model first" weigh-in gap** — captain told stilgar it was surfacing the system-state model to the operator, but no operator-addressed message had gone out; spec was in triple-review set to auto-merge. Directed: (1) send operator the model shape NOW; (2) park the merge if review finishes before operator replies | "ACK + acted on your realignment" — sent the operator the model shape + parked the merge | **DONE — the session's only confirmed behavior change** |
| E2 | 04:17 (post-retro) | **DOT triple-review mandate violated on captain's own lanes** — two captain-spawned queues went out as `workflow_mode=review-loop` (single reviewer): `captain-research` (handler-pause, still active) and `captain-fixes` (remote-substrate fix hk-zsn7, already landed on main single-reviewed). Directed: (1) stop captain-research and re-dispatch via triple-review DOT; (2) run an independent triple-review pass on the already-landed commit or surface to operator; (3) going forward all captain-spawned queues use triple-review | Captain dispatched C1 (workflow-mode-in-config) as the permanent fix; triple-review enforced from 05:17 onward | **Resolved — C1 landed at 07:17 (e5977386), permanently removes the root cause (submit.go no longer hardcodes review-loop)** |
| E3 | 05:18 | **Overnight-priority drift** — operator's overnight brief says build C1 (workflow-mode-config fix) FIRST ("operator's #1 frustration, design done, buildable now") but C1 was not dispatched; free slot + parked crew leto available; noted C1 is file-disjoint from paul/stilgar/logmine | Captain dispatched C1 within 3 min | **DONE — acted on within 3 min** |
| E4 | 09:17 | **Meta-risk: thrash bug eating its own fix pipeline** — FIX1 of the thrash-fix cluster just died on the exact thrash signature; the 4 serialized fix beads were running on the daemon (the same buggy commit-gate path) and would thrash until FIX1+FIX2 land. Directed: use the proven worktree-author + isolated-reviewer + `harmonik promote` escape (off the daemon path) rather than re-dispatching onto the daemon | Captain confirmed the escape; used worktree-author pattern for fix beads | **Resolved — FIX1 landed via salvage+independent-review (72ef26d9) at 11:17** |
| E5 | 15:17 | **FIX3 promote stall** — FIX3 (3rd thrash fix) was salvage-APPROVED ~40 min prior but not on main; captain appeared non-responsive to paul's ping; directed captain to promote 2bbc94b8 now per FIX2 playbook | Captain did not respond to the directive; re-escalated at 16:17 with operator options | **Resolved at 17:17 — actual cause was a lost verdict across keeper-restart, not a wedge; paul re-sent, captain promoted in 10 min (5e5a0da0); admiral's over-diagnosis acknowledged** |

---

## F. Patterns the admiral named across the session (no-action items, for the record)

These are cross-hour syntheses the admiral surfaced as informational, not as corrections.

- **Cache-reaper = two-incident trouble-spot** (Audit 8, 23:17): the cache-cleanup subsystem caused two incidents in the same session (fleet-wide build failures). Stopgap durable-reverted to main; real fix (cache-cleanup-vs-dispatch locking race mutex) prioritized as P1.
- **Iter-2-thrash as the dominant throughput constraint** (01:17): after the reviewer requests changes, the re-run implementer was thrashing 60–90 min, exhausting budget, committing nothing, and failing — seen across 6 beads; framed correctly as a systemic bug, not a staffing/priority issue.
- **Major-issue fan-out paying off** (02:17): the thrash root-cause was found via 8-agent fan-out; adversarial verifiers correctly overturned a wrong intermediate conclusion before it reached the operator.
- **Daemon-first incident response working** (19:17, 20:17): captain-coordinated daemon restarts cleared frozen slots; supervisor-death detector landed and immediately caught a real event the same session.
- **Stilgar spec-conformance pipeline surfaced real work** (12:17, 14:17): what the admiral initially categorized as hygiene (auditing 11 already-implemented epics) turned out to uncover 6 real spec-evolution gaps and 15 scenario-harness gaps including an entirely unbuilt execution layer.

---

## Quick-reference: pending decisions for the operator

| Decision | Admiral's last mention |
|---|---|
| Flywheel live ACT-mode flip (CD3/CD4) | Throughout session |
| leanfleet greenlight (hk-itoc) | Throughout session |
| v0.2.0 GPG-signed release push | 07:17 |
| C2 governor ownership | 07:17, 08:17, 12:17 |
| C2 keeper-rearm auto-vs-escalate | 07:17, 08:17, 12:17 |
| C3 remote-box un-park | 07:17, 08:17, 12:17 |
| OperatorNFR config-knob prioritization | 12:17 onward |
| 3-day scale-out window expiry / token-efficiency posture | 18:20 |
| Admiral session restart (over-cap, keeper-less) | 13:17, 18:20 |
