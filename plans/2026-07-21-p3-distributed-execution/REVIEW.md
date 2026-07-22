# P3 Distributed Execution — Review pass

> Reviewer: lima, 2026-07-22. Status of the plan under review: DRAFT, pre-kerf.
> **Verdict: the design is sound; three of its factual premises are not.** None of the
> findings below are objections to the architecture — C1–C6 hold, the container-as-unit
> model is right, and §2.5's "every ssh wedge was a missing fabric feature reinvented
> badly" is the correct lesson. What follows is what the plan asserts as *true today* and
> is not, because each one changes what can actually be staffed.

Every claim below was checked against the machine or the tree, not inferred from the
document set. Where I checked only one box or one branch, I say so.

---

**STATUS 2026-07-22: R3 and R4 are APPLIED to `_plan.md` — neither needed a decision.** R3: L7 no
longer claims to extend anything; a correction block under §4.2 records all three errors and states
the design rule ("could not verify" must mean unreachable); §5's seam list no longer lists a
fail-closed guard as landed and now flags that ProxyJump presupposes `fleet`. R4: §4.3's spliced
sentence is rewritten and now says why the serialized-first delay is worth accepting. **R1 and R2
remain open and still gate kerf** — they are the two below that are not mine to close.

---

## R1 — BLOCKER. §2.1 "Substrate (real NOW)" does not exist on this machine.

The plan's foundation section states the substrate is real today:

> `gb-mbp` (macOS, daemon host) → lima VM **`fleet`** (Ubuntu) → **incus** containers
> launched inside `fleet`. Golden image **`agent-golden`** already carries pi/claude/codex.
> One container launches by hand today.

**None of that is present.** Verified on `gb-mbp`, as the operator's own user, `LIMA_HOME`
unset:

```
limactl list          -> ONE instance: "test", STATUS = Stopped.  No "fleet".
ls ~/.lima/           -> _config, test.  No fleet directory.
limactl shell fleet   -> fatal: instance "fleet" does not exist
which incus           -> incus not found
```

So there is no `fleet` VM, no incus, no `agent-golden` image, and no container that
"launches by hand today". The stopped `test` instance is the only VM on the box.

**Why this is a blocker and not a nit.** Three separate parts of the plan rest on it:
§5's delivery scaffold is described as "buildable NOW with near-zero new transport code";
§7's addressing answer is "ProxyJump-SSH through `fleet`"; and §2.1 is what makes the
whole plan read as *provisioning-then-integration* rather than *build-the-substrate-first*.
With no substrate, the scaffold whose job is to "keep proof flowing while P1 is built" has
nothing to run on, and standing the substrate up is itself unscoped work that nobody has
sized.

**Scope caveat, stated because it changes who should act.** I checked ONE box. If `fleet`
lives on a different machine, or was torn down deliberately, the fix is a one-line
correction to §2.1 naming where it actually is. If it was never built beyond a
research spike, §2.1 and §5 need rewriting and P3 gains a provisioning workstream. The
plan should not be kerfed until someone says which.

## R2 — BLOCKER. §6.1's "#1 pre-work item" reads CLOSED and is live. Filed as `hk-uaka2`.

§6 is right that this is the top pre-work item and right that "a green container run on
top of an unfixed hk-2hfyt is not a real green". The problem is that anyone acting on that
instruction will check the tracker and conclude it is done:

- `br show hk-2hfyt` → **CLOSED**, 2026-07-12, reason "done".
- At HEAD of `phase1-session-restart-substrate`, `internal/workspace/createworktree.go`
  still sets `emptyHEADRace` at :264 with **no honest probe first** — no
  `rev-parse --verify`, no `test -e <wt>/.git` anywhere in the file — and still carries
  the misleading `"concurrent remote create race"` string at :267 that the fix spec
  explicitly called for removing. **None of the three fix items landed.**

The bead's own last comment (2026-07-12, hawat) already caught this: it closed via
`noChange-subsumed: bead found in main`, where the daemon matched the *string* `hk-2hfyt`
in a **docs** commit rather than any fix content, and the comment says in as many words
"FLEET-DOWN PROBE BUG IS STILL LIVE. Do NOT treat as fixed." The escalation to re-land it
under a clean-ID bead never happened — the only follow-ups in git are docs commits
*parking* the finding.

Filed as **`hk-uaka2`** (P1) carrying the full mechanism and the original apply-spec, so
the next agent does not have to excavate it a third time.

**Second-order defect, worth its own bead:** the daemon's noChange-subsumption closes a
bead when its ID appears as a *string* in any commit, including a docs commit that merely
mentions it. That is how a P1 bug closed itself. Every bead ID written into
`captain-lanes.md` or a handoff is exposed to the same auto-close.

## R3 — CORRECTION. L7 and §5 lean on a fail-closed guard that does not hold.

Both §5 ("reuse the landed seams: … the fail-closed spawn guard (`hk-5h759`)") and control
**L7** ("spawn-time reachability fail-closed … extends hk-5h759 fail-closed guard") treat
that guard as existing and load-bearing. Two problems:

1. **The citation is wrong.** `hk-5h759` is titled *"codexdriver: set
   `sandbox_mode=danger-full-access` + `approval_policy=never` for headless crew
   orchestration"* — it is the **posture**, not a guard. Whatever fail-closed behaviour
   the plan means, that bead is not it.
2. **The actual fail-closed codex isolation fence was deliberately removed**, hours before
   this review, as the operator-directed point of `hk-tckw3.1` Step 1. That removal is
   intentional and is not a regression — but it means a plan written against "the
   fail-closed guard exists" is describing a world that no longer holds.
3. **The nearest real guard is itself failing open.** `hk-y81iv` (**OPEN, P1**) records
   that `verifySandboxEngaged` **fails open** when srt never runs: "srt failed + canary
   absent" is read as ENGAGED.

L7 is a *good* control and should stay. But it must be specified as **new work that builds
the fail-closed check**, not as an extension of something already in place — and it should
be written knowing `hk-y81iv` is the shape of mistake to avoid, since a container
reachability check has exactly the same "absence of evidence read as success" hazard.

## R4 — Minor. §4.3's opening sentence is unfinished.

> "v1 keeps the registry as "one logical container-transport worker" is the OLD framing but
> P3's whole point is to lift `ErrTooManyWorkers`."

Two clauses are spliced and the sentence does not parse. The *intent* is clear from what
follows (start serialized, measure, then add concurrency) and I agree with it; the sentence
just needs rewriting before this becomes a spec anyone implements from.

---

## What I am NOT flagging

So the above is not read as broader doubt: §2.3's codex-sidecar finding (driver stays home,
NDJSON in-band over stdout, readiness in-band, so the entire `agent_ready` / reverse-tunnel
failure class does not gate codex) is the strongest part of the plan and I found nothing
against it. §2.4's git-branch-home is correctly identified as the one thing the scrapped ssh
model got right. The L1–L9 liveness enumeration is genuinely thorough and L5 (never strand
`in_progress`) is correctly marked non-skippable. The demolition date on the scaffold (§5)
is exactly the discipline whose absence created the ssh model.

## Recommended disposition

**Do not kerf yet.** R1 and R2 are premise failures, not design flaws, and both change
staffing:

1. **R1** — operator or captain to say where `fleet` is, or accept that P3 v1 includes a
   substrate-provisioning workstream. This determines whether §5's scaffold is days or
   weeks away.
2. **R2** — `hk-uaka2` should be scheduled as real pre-work, since the plan itself makes it
   a gate on trusting any container E2E.
3. **R3** — rewrite L7 and the §5 seam list as new work; drop the `hk-5h759` citation.
4. **R4** — one sentence.

With R1 answered and R2 scheduled, the plan is ready for kerf. The design does not need
rework; the ground it stands on needs correcting.

> **Fragility note.** This file, and the entire `plans/2026-07-21-p3-distributed-execution/`
> directory, is **untracked** — it exists only in this machine's working directory and in no
> commit. That is `hk-uefon` (223 untracked planning files across 19 dated plan dirs,
> including the `DECISIONS.md` holding the locked C1–C6 this review is measured against).
> This review is as easy to lose as the plan it reviews.
