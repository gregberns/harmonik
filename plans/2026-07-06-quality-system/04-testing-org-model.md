# Testing / Quality Operating Model — Who Owns the Gate

**Date:** 2026-07-06 · **Status:** RECOMMENDATION for operator review
**Companion to:** `plans/2026-07-05-quality-process/02-shift-left-and-fast-follow-design.md` (the AT/CR/LT/XT
acceptance block + Lane-1/Lane-2 routing this org must enforce).

---

## TL;DR recommendation

**A NEW dedicated agent-manifest — `assessor` (the gate executor) — spawned per epic at the gate boundary,
DIRECTED by the admiral, structurally independent of the captain.** Not admiral-runs-tests, not
captain-owns-both.

The operator's instinct ("make the admiral responsible for testing") is *directionally correct* — validation
must sit in a chain **independent of the builder** — but the admiral must not *run* the tests itself. Its
whole soul is "I direct; the captain acts… I do NOT dispatch, spawn, or touch repo files." Bolting a
break-testing harness onto the admiral breaks its altitude and its once-hourly cadence. So we keep the
operator's *independence* principle and split it into two roles:

- **admiral = gate AUTHORITY** — owns the mandate, decides when a gate is required, receives verdicts,
  blocks/clears the merge, escalates. (Extends its existing audit role; no new "act" behavior.)
- **assessor = gate EXECUTOR** — a new crew-tier manifest that actually stands up the scratch daemon, runs
  LT, fans out the XT break-testers, commissions the CR reviewer, and lands a structured verdict.
- **captain = builder** — unchanged: organizes lanes, staffs building crews, drives beads to the *epic
  branch*. Never validates its own lane.

---

## 1. Ownership: the three alternatives

### The governing constraint (quality-process P2)
> the reviewer must be a DIFFERENT context than the author.

At harmonik's altitude "author" is not one agent — it's the **captain + its building crew**, who share an
attribution incentive: the captain *wants its lane to pass the gate* (a cleared epic is its scorecard). So
"different context than the author" means: **the validating chain must not descend from the captain that
staffed the building crew.**

| Model | Who runs the gate | Failure mode it risks |
|---|---|---|
| **A. Captain-owns-both** | The same captain that staffed the building crew also spawns the testers | **Same-frame reviewer blind spot.** The captain has a standing incentive to clear its own lane; it also runs a tight event-driven monitor loop and is the wrong altitude to orchestrate a 20-min break-test batch. Marking your own homework — the exact P2 violation. |
| **B. Admiral-runs-validation** (operator's literal proposal) | Admiral spawns staff, plans, and validates | **Altitude collapse + cadence mismatch.** Admiral is a *once-hourly, read-assess-correct-STOP, never-dispatch* auditor. Making it stand up scratch daemons and fan out 3–5 adversarial agents turns the oversight tier into an implementer tier — it can no longer be the neutral escalation backstop, and gate work doesn't fit an hourly tick. |
| **C. NEW `assessor` manifest, admiral-directed** ✅ | A dedicated per-epic validator, spawned into its own worktree/scratch clone, reporting up the admiral chain | **Cost: one more manifest to maintain + a spawn-coordination seam.** Mitigated because the *work* (scratch-daemon, batch, feedback, fanout) is already-built primitives; the manifest is a thin operating contract over them. |

### Why C wins
1. **Independence is structural, not behavioral.** The assessor is spawned by / reports to the **admiral**,
   a different chain than the captain that owns building. No agent grades its own lane. This is the cleanest
   satisfaction of P2 at the fleet altitude.
2. **Separation of throughput vs. assurance.** Captain optimizes flow (staff every free slot, re-task drained
   lanes); assessor optimizes finding defects (actively try to break it). Those are opposed objectives — a
   single agent holding both under-serves whichever is inconvenient this cycle. Splitting them lets each soul
   be single-minded, matching the existing captain/admiral/crew/watch decomposition.
3. **The gate is bursty and epic-scoped, not continuous.** It fires at an epic boundary, runs a heavy
   isolated batch, emits a verdict, and dies — exactly the `cardinality: {min:0, max:n}`, per-work,
   self-terminating shape the **crew** manifest already models. It does *not* fit the always-on admiral
   (hourly) or watch (event stream) shapes.
4. **Keeps the operator's win.** Operator wanted testing owned *away from the builder*. C delivers that — the
   admiral owns the gate authority — without overloading the admiral with execution it was explicitly built
   NOT to do.

---

## 2. The new `assessor` manifest (sketch)

**soul one-liner:**
> **I am `assessor`** — the epic-gate validator: I take ONE epic branch that a crew has finished, prove its
> expected outcomes on an isolated scratch daemon, actively try to break it, and land a binding
> PASS/BLOCK verdict before it can reach `main`.

**operating one-liner:**
> On a `gate_requested` for `epic/<codename>`: stand up a scratch daemon on that branch, run LT (verify the
> AT checklist), commission CR (fresh-context reviewer) + XT (3–5 adversarial break-testers via
> `major-issue-fanout`), collect findings as `found-by:*` beads, emit a structured verdict to the admiral,
> then terminate.

**What it owns (the AT/CR/LT/XT block from the shift-left design):**
- **LT** — runs `scripts/epic-livetest.sh` / `scratch-daemon.sh batch` against the epic branch; GREEN iff
  every AT assertion is observed in the event stream.
- **XT** — fans out 3–5 adversarial agents (concurrency, malformed input, lifecycle interruption, boundary,
  operator-surface abuse) at the *same* scratch daemon; files every anomaly as a `found-by:exploratory` bead.
- **CR** — commissions ONE worktree-isolated, fresh-context reviewer agent (the existing `agent-reviewer` /
  `code-review` machinery) over the whole epic diff. The assessor does not review inline — it *commissions*,
  preserving reviewer independence even from itself.
- **Verdict** — aggregates: `PASS` iff (AT closed ∧ CR=APPROVE ∧ LT green ∧ no open P0/P1 XT finding), else
  `BLOCK` with the blocking beads enumerated.

**What it does NOT do:** rank priorities, staff building crews, edit code, or decide *product* direction. It
finds and reports; the admiral rules on the verdict; the captain re-dispatches any fix beads.

**Cardinality / harness:** `type: assessor · cardinality {min:0, max:n} · harness: claude · parent_intent:
admiral · lifecycle.self_restart: true` (keeper-re-hydratable like a crew — a gate batch can outlive a
context window). Reuses skills: `major-issue-fanout`, `harmonik-dispatch`, `beads-cli`, `agent-comms`, plus a
new `epic-gate` skill owning the scratch-daemon runbook.

**Isolation:** ALWAYS operates on a scratch clone (`scratch-daemon.sh`, `guard_path` + `assert_not_supervised`
already hard-refuse the fleet checkout) — never the fleet daemon, honoring the 24-hour rule (a new build
replaces the live daemon only *after* passing this gate).

**Directed by / reports to:** spawned and directed by the **admiral**; reports its verdict to the admiral;
copies the captain on the outcome (so the captain can re-task). This is the independence seam.

---

## 3. Hand-off mechanics (end to end)

```
BUILD (captain chain)                    GATE (admiral chain)                       MERGE
────────────────────                     ────────────────────                       ─────
crew drives epic/<codename> beads        admiral spawns assessor for the epic       human PR
   │ all children CLOSED on branch           │                                      epic/<cn>→main
   ▼                                          ▼                                          ▲
crew posts epic_ready ──► captain ──► admiral ──► assessor runs LT+XT+CR ──► verdict ─┘
   (comms --topic gate)   verifies   (gate       (scratch daemon, isolated)   to admiral
                          branch      authority)                              +captain
```

1. **"Epic ready for gate" signal.** When the crew's epic has all children closed on `epic/<codename>`, the
   crew posts `comms send --from <crew> --to captain --topic gate -- "epic <codename> ready; branch
   epic/<codename> @ <sha>; AT beads <ids>"`. (New `gate` topic; parallels the existing `status`/`error`
   feed. The crew already owns a mandatory progress feed — this is one more trigger on it.) The daemon should
   also emit an `epic_completed` for the branch; the crew's comms post is the human-legible companion.
2. **Captain relays, does not gate.** The captain — which already subscribes to `epic_completed` — confirms
   the branch is real (not a zombie) and forwards to the admiral: `--to admiral --topic gate`. The captain
   then re-tasks the freed building crew to the next lane. It does **not** decide pass/fail.
3. **Admiral triggers validation.** On the `gate` topic the admiral (its existing bus subscription) spawns
   the assessor: `harmonik start assessor --epic <codename> --branch epic/<codename>`. This is the admiral's
   one *new* verb; it fits its "direct the fleet" role (it directs a validator, still doesn't touch a diff).
4. **Who runs the break-testers.** The **assessor** runs XT — it fans out the 3–5 adversarial agents via
   `major-issue-fanout` against its own scratch daemon. Neither the captain nor the admiral spawns testers
   directly; the assessor is the single orchestrator of the gate batch, keeping the break-test blast radius
   inside one isolated clone.
5. **Where the verdict lands.** Three places, one source of truth:
   - **Beads (ledger, binding):** every finding is a `found-by:{live,exploratory,review}` bead on the epic;
     the verdict is recorded as a comment on the epic bead. **The set of open P0/P1 `found-by:*` beads IS the
     block** — deterministic, greppable, not a prose verdict a reviewer can hand-wave (per
     `feedback_deterministic_donecheck_beats_reviewer`).
   - **Comms (notification):** `assessor → admiral --topic gate-verdict -- "PASS|BLOCK <codename>; open
     P0/P1: <ids>"`, cc captain.
   - **Verdict artifact:** structured JSON (reuse the `agent-reviewer` schema-v1 shape) in the epic's kerf
     bench, for audit.
6. **What blocks a merge/deploy.** The `epic/<codename> → main` PR is the single enforcement point
   (unchanged from the shift-left design). The gate is: **AT closed ∧ CR=APPROVE ∧ LT green ∧ zero open
   P0/P1 `found-by:*` bead.** The admiral holds the merge until the assessor posts `PASS`; a `BLOCK` bounces
   the epic back to the captain, who dispatches the blocking beads onto the *same epic branch* for another
   build→gate cycle. The human still performs the final PR (integration→main stays a human step per
   lifecycle policy) — but only ever on a PASS.

### The RULE-EXCEPTION and 24-hour rule fit cleanly
- **Testing-framework crew exception:** the framework crew builds+tests in its own worktree + integration
  branch and merges to *that* branch then to main — i.e. it is its *own* assessor for its *own* branch (the
  tools it's building don't yet exist to gate it). Model as an assessor spawned into the framework crew's own
  chain rather than the admiral chain; it's the one case where builder==validator is unavoidable and
  acceptable (bootstrapping the gate machinery).
- **24-hour rule / GATE-0:** the assessor's LT *is* the "new isolated E2E reproducing the changed behavior"
  precondition for a daemon deploy. No `daemon-YYYYMMDD-NN` swap ships until an assessor PASS exists for the
  change — the admiral, as gate authority, is the natural holder of that deploy interlock.

### Lane 2 (small merges) stays agent-light
Fast-follow is a *scheduled script* (`every@15m` → `fast-follow.sh`), not an agent role — no assessor spawned.
Its reds land as `found-by:fast-follow` P1 beads + a captain `quality` ping. The assessor exists **only** for
Lane-1 epic gates. If a Lane-2 change's fast-follow keeps going red (≥2×), that's the mis-routing signal — the
captain promotes it to an epic, and it then earns an assessor gate.

---

## Summary — the seam that makes it work

The captain and the assessor **never share a chain**: captain descends from operator→admiral→captain for
*building*; assessor descends from admiral for *validating*. The admiral is the shared parent but does neither
job itself — it authorizes and adjudicates. That is the structural, not merely behavioral, guarantee that
"the reviewer is a different context than the author," applied at fleet altitude.
