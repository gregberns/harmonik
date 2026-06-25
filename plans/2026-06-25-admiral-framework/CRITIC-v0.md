# CRITIC v0 — adversarial review of STRAWMAN-v0 (Admiral / Captain Operating Framework)

**Reviewer stance:** independent skeptic. Goal = find where this FAILS to prevent the 2h stall, not praise it.

---

## VERDICT: **NEEDS-REWORK**

The strawman correctly *diagnoses* the classification bug and writes excellent principle-text for it. But on the operator's #1 stated worry — the **forcing function** — it does not close the gap, and there is direct evidence in the live files that more principle-text is exactly what already failed. Two of its three artifacts also re-litigate distinctions that already exist in the files, which is doc-churn, not a fix. It is one structural change away from SOUND.

---

## The single most important finding (the forcing-function gap is NOT closed)

**The mechanisms the strawman proposes already exist in the live files, fired during the stall, and returned the wrong answer.** Adding better-worded versions of them will not change the outcome, because the failure was never that the rule was missing — it was that the rule *was consulted and mis-answered*.

Concretely, walking the actual files:

1. The admiral mission **already has a forcing function**: `/loop 1h` (admiral.md:50) re-fires the audit every hour, and it survives `/clear` because it is re-armed on every restart (admiral.md:142-144, "re-read this on-disk mission … re-arm the `/loop 1h`").
2. That hourly audit **already asks the exact stall-detecting question**: admiral.md:115 audit-question **C3** — *"Is the captain idling with ready work AND a free crew/queue slot (missed staffing)?"*
3. The captain STARTUP **already encodes the stall as a hard failure**: STARTUP.md:596-601 — *"a monitoring cycle is FAILED if … (c) `br ready --limit 0` shows ready beads AND a free crew/queue slot exists AND the captain did not staff them. A quiet all-green ops-monitor digest while (c) holds is … a MISSED STAFFING FAILURE."*
4. The captain STARTUP **already forbids the exact behavior**: anti-pattern **G** (STARTUP.md:664-668) — *"Holding while ready work exists, punting a decidable question to the operator, or treating a satisfied past request as a standing blocker are all FAILURES."*

So the system already had: a surviving forcing function (`/loop 1h`), the precise detector (C3 / FAILED-(c)), and an explicit prohibition (anti-pattern G). **All three were live on 2026-06-25 and the admiral still scored the idle fleet "ALIGNED" at every fire** (ANALYSIS.md:40,43,46). The strawman's 2.1 ("idle fleet … is itself a problem"), 2.3 (self-authorization), and the item-6 "aligned detector" are *re-statements of C3 / FAILED-(c) / anti-pattern G in nicer words.* The retro's own thesis convicts the strawman: "adding principle-text to a skill file did NOT make admiral re-read the priority list today." The strawman is, in the main, more principle-text.

**Why the existing mechanism failed is the thing the strawman must fix and doesn't:** C3 is a *self-scored judgment question* the agent answers from its own current frame. When the agent's frame was "parked = operator-gated," it answered C3 "no missed staffing — the lane is correctly gated," and FAILED-(c)'s "free slot" predicate read false because the agent believed the lane was not a legitimate staffing target. A judgment question filtered through a wrong frame returns the wrong answer no matter how it is worded. The fix is to remove the judgment from the trigger — make the detector **deterministic and external** (something the ops-monitor or a script computes and *posts as an IMMEDIATE wake*, the way audit conflict-5 proposes), so the wake is not gated on the admiral's own classification. The strawman never proposes a deterministic, agent-external trigger; it leaves the stall-detection inside the same self-scored audit that already mis-scored it.

---

## Top risks, ranked, each with a concrete fix

### Risk 1 — (HIGHEST) No new forcing function; the proposed mechanisms are the ones that already failed
*Stress-test #1 + #2.* As above: 2.1 / 2.3 / item-6 restate C3, FAILED-(c), anti-pattern G — all of which were live and mis-answered. REFRESH-THEN-ACT (2.2) is also pure principle-text with no trigger: nothing *makes* a fresh `/clear` admiral run it; it's a sentence it has to choose to obey, which is the exact failure class. **Would the strawman as written have prevented the stall? No** — the admiral would have read the new self-authorization principle, then still self-scored "this parked lane is the gated kind, principle doesn't apply," exactly as it self-scored C3.

**Fix (the missing piece):** add ONE deterministic, agent-external trigger and make it the load-bearing change; demote the principles to support text.
- Adopt audit **Conflict-5's carve-out** (conflicts.md:267-276): the **ops-monitor** (a deterministic bash check, already running every 5m and already wired to post `[IMMEDIATE]` comms — STARTUP.md:530-556) computes `program_drained AND br_ready_in_known_lane AND free_slot` and posts an **IMMEDIATE escalation that names the specific ready lane**. This is not a judgment the admiral makes — it is a fact a script computes and *pushes*, waking the captain with the lane named. The strawman's `[OPEN-Q]` set never mentions this; it is the actual fix and must be promoted from the audit doc into the framework as Part-0.
- Make the admiral C3 answer **machine-checkable**: replace "Is the captain idling with ready work?" (judgment) with "run `br ready --limit 0` per known lane; if any known lane has ready beads + a free slot and is unstaffed, that is a FINDING — you may not score ALIGNED" (computation). The strawman's item-6 gestures at this ("'aligned' is NOT available … when there is ready known work + a free slot + an idle crew") but leaves it as a verdict-availability *rule* the agent still self-applies through its frame; bind it to the `br ready` command output instead.

### Risk 2 — Direction-log will rot exactly like the docs it's meant to replace
*Stress-test #3.* The strawman's defense ("tiny, append-only, read-mostly, near-zero maintenance") is the **same defense that every rotted doc had.** `admiral-initiatives.md` is *also* "one line per initiative, reconciled each audit," and the audit (conflicts.md:25) found it carried a stale `PARKED (deliberately held, has a gate)` reading that actively *caused* the mis-classification — i.e. the existing append-mostly registry rotted INTO the bug. The captain-lanes.md dated-directive block (the strawman's own Conflict-2 evidence) is the canonical rot: it expired 2026-06-22, nobody struck it, and its lapse silently flipped the posture. A hand-maintained log written "only on a major sequencing decision" depends on the agent (a) recognizing a moment as major and (b) choosing to write — both judgment calls the stalled agent demonstrably gets wrong. **What makes it different this time? Nothing is proposed that does.**

**Fix:** the direction-log only avoids rot if a write is *forced* and a read is *forced*.
- **Forced write:** the same operator-directive-change or admiral re-sequencing event that the log records is *already* a comms message. Don't ask the agent to separately remember to append — derive the log entry from the comms event (or make "write the RETURN-PATH entry" a non-optional step of the directive-issuance procedure, checked by the next audit: "a directive block exists with no matching direction-log entry = a FINDING").
- **Forced read with a freshness gate:** give every direction-log entry the same `expires:` + on-expiry-default the strawman (correctly) gives dated directives (Part 1b). An un-renewed RETURN-PATH past its expiry LAPSES to "resume standing autonomous posture," and the audit must flag an expired-but-present entry. Without this, the log becomes a museum of stale RETURN-PATHs that a fresh `/clear` reads as live — the precise mechanism by which "holding for operator" survived five resets.

### Risk 3 — Fragmentation: 2 of the 3 artifacts re-litigate distinctions the files already carry
*Stress-test #5 + #6.* The strawman's own Part-1(a)/(b) admit the EPICS set and PRIORITY-ORDER "already have a home; nothing structural changes." So those two "artifacts" are really *edits to vocabulary in existing files* — and the edits land the SAME `parked-known vs gated` sentence into `admiral-initiatives.md` status vocab, captain STARTUP §0 + §8 + LAZY-BOOT, admiral.md, watch SKILL.md, AND orchestrator-rules (Part-3 items 1-8). That is the same sentence in **6+ files**, which is the reconciliation burden the project's own AGENTS.md precedence rule exists to prevent, and which `[OPEN-Q 7]` openly worries about. A `/clear` agent that reads one file and not another gets an inconsistent contract — and the audit shows the agent *already* reads these inconsistently (it trusted captain-lanes' stale block over the live feed).

**Fix:** resolve `[OPEN-Q 7]` toward **one canonical definition, pointed-to, not copied** — consistent with the project's existing "orchestrator-rules states the rule once; other files point" pattern (AGENTS.md "AGENTS.md is a ROUTER … does not restate a contract"). Put the parked-known-vs-brand-new definition in **orchestrator-rules** (already the single canonical standing-rules file, already loaded by the captain at STARTUP Step 1.3) as ONE normative sentence keyed on "ever ranked/recorded." Every other file gets a one-line *pointer* ("KNOWN-vs-brand-new: see orchestrator-rules §Autonomy"), not a re-statement. This drops the 6-file edit to 1 definition + 5 pointers and removes the stale-copy risk. The audit's "single highest-leverage fix" (conflicts.md:280-297) explicitly recommends this single-sentence-mirrored approach — but "mirrored verbatim into 5 files" IS the fragmentation; "defined once, pointed-to" is the safer read of the same intent.

### Risk 4 — "PARKED-known vs GATED" split is a new bright-line rule that will bite the next unanticipated case
*Stress-test #4.* The strawman's whole thesis is "principles not rules," yet its central mechanism (Part-1a, item-3, item-4, `[OPEN-Q 1]`) is a **new two-valued enum** — every parked item must be tagged PARKED-known XOR GATED. That is a bright line, and the retro's lesson is that bright lines meet unanticipated cases and the agent picks the conservative side. Concretely: a lane that is *mostly* resumable but has one genuinely operator-gated sub-decision (e.g. wake-economy's leanfleet sub-lane, which the playbook lists as "operator-gated") — which tag? The agent will tag the whole lane GATED to be safe, and you have rebuilt the stall one level down. The enum also requires someone to *maintain* the tag correctly forever (Risk 2 rot applies).

**Fix:** don't add an enum. Make GATED require a **named, dated gate with an owner and an expiry** (the Part-1b discipline) — and make "no named live gate present" deterministically mean KNOWN/resumable. Then there is no judgment tag to mis-set: the *absence* of a live named gate IS the KNOWN signal, computed from the file, not classified by the agent. "PARKED" stops being an enum value and becomes "has zero ready beads right now" (a fact), fully decoupled from "is gated" (which requires an explicit, expiring gate object). This is the strawman's own item-3 intent, but it must DELETE the PARKED-known label rather than add it — the label is the bright line.

### Risk 5 — REFRESH-THEN-ACT is simultaneously too vague to guide and untriggered
*Stress-test #4 (the "vague principle = agency without direction" half) + #1.* 2.2's test — "is the fact I'm about to act on one I last saw before a `/clear` or more than an audit-cycle ago?" — requires the agent to *know which fact it is betting on* and *remember when it last saw it across a context wipe.* A fresh `/clear` agent has no memory of "when I last saw" anything; that's the whole point of `/clear`. So the test is unanswerable by the agent it most needs to govern. And `[OPEN-Q 5]` ("is this light enough?") concedes the author isn't sure it's actionable. It is the kind of principle that reads well and changes nothing.

**Fix:** replace the introspective test with a **mechanical default**: the boot digest (STARTUP.md:130, `captain-boot-digest.sh`) and the LEAN-resume already re-derive live state once per boot/restart. State the rule as "act on the boot-digest's live numbers, never on a claim carried in a doc or handoff" (which STARTUP.md:104 already says for HANDOFF — just generalize it to *all* durable docs). That requires no introspection: the digest output is the fresh fact, by construction. Drop the "when did I last see it" framing entirely.

---

## What's RIGHT (so the rework keeps it)

- The **root-cause diagnosis is correct and sharp**: parked-known mis-read as brand-new, keyed on "live feed now" instead of "ever ranked." Keep this framing verbatim.
- **Part-1b (dated-directive `expires:` + on-expiry-default = revert-to-autonomous, NOT hold + admiral-audit-owns-flagging)** is the single best concrete mechanism in the doc — it is deterministic, owner-assigned, and directly prevents the Conflict-2 silent-lean-park. Promote it; apply the same `expires:` discipline to the direction-log (Risk 2).
- **WIP-first as an explicit tiebreaker-never-a-veto with the "forbidden sentence" framing (2.4)** is genuinely good rule-design and should survive untouched.
- Item-8's "operator away is NOT a HOLD trigger" reframe is correct and load-bearing.

## Minimum-viable version (answer to stress-test #6)

The stall is fixed by **two** changes, not a framework:
1. **One deterministic external trigger** (Risk-1 fix): ops-monitor posts an IMMEDIATE naming the ready known lane when a program drains and leaves a free slot. This is the only thing that would have actually woken the stalled fleet, because it doesn't route through the admiral's mis-classifying audit.
2. **One canonical sentence, defined once in orchestrator-rules, pointed-to elsewhere** (Risk-3 fix): KNOWN = ever-ranked/recorded; GATED requires a named live expiring gate; everything else resumes autonomously.

Everything else in the strawman — the direction-log, REFRESH-THEN-ACT, the PARKED-known enum, the 6-file softening pass — is either rot-prone, untriggered, or duplicative. The direction-log is the one *new* idea worth keeping, but ONLY with forced write + expiry (Risk 2); without that it is a 4th doc that rots into the next bug.
