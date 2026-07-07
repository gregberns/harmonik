# Assessor → remediation loop — system design (2026-07-06)

*Design for the admiral. Operator requirement (2026-07-06): the issues the assessor files —
blocking findings AND assigned known-issues like `hk-lgykq` — must get ROUTED to a crew, TRACKED,
and DRIVEN to closed as a **system**, not by the admiral hand-carrying each bead every gate. Builds
directly on the assigned-known-issue lifecycle in `07-assessor-severity-framework.md` §5 + tail, and
the launcher/query wire-up in `08-assessor-wireup-plan.md`. DESIGN ONLY — no code, no beads, no
manifest edits yet; this becomes the remediation mechanics wired at Phase-1's first epic boundary.*

---

## 1. The problem this closes

Today the assessor *produces* findings (`found-by:assessor` beads) and the severity framework (`07`)
decides which BLOCK and which become tracked known-issues. What is undefined is what happens *after
the verdict*: who picks a finding up, on what queue, who confirms the fix actually fixes it, and how a
blocking finding gets driven to closed so the held epic can finally merge. Left manual, that is the
admiral routing every bead by hand each gate — the exact "manual admiral routing each time" the
operator called out. The remediation loop makes routing, tracking, and drive-to-closed a **standing
system keyed on labels**, so the admiral sets POLICY and adjudicates disputes, and the machinery
carries the rest.

**Design invariant (from the role split):** the admiral does NOT dispatch beads or staff crews (soul.md
"I do NOT dispatch"). So the remediation loop cannot be "admiral assigns crews." It must be a
**labeling contract + a captain standing rule** that turns the found-by bead pool into a first-class
backlog source the captain staffs exactly like feature work — via `kerf next` ranking. The admiral's
only per-gate acts are severity adjudication (`07` §6) and the epic→main PR / deploy hold.

---

## 2. The finding bead IS the unit of remediation (no shadow fix-bead)

One bead per finding, cradle to grave. The `found-by:assessor` (or `found-by:admiral`) bead that the
assessor files is the SAME bead that gets routed, owned, fixed, and closed — there is no separate
"fix bead" mirroring it. This keeps the block query (`08` Gap B), the known-issue ledger (`07` §5),
and the remediation tracker reading **one** pool, partitioned by labels, never drifting out of sync.

`hk-lgykq` is the canonical shape: `found-by:admiral` + `known-issue` + `codename:quality-system-followup`,
P1, and it is BOTH the tracked known-issue AND the thing a crew fixes and closes. That is the pattern
generalized below.

---

## 3. Disposition = a label triad, assigned at file-time

Every finding lands in exactly ONE of three dispositions, encoded by labels the assessor/admiral
attach when the bead is filed. Disposition is a pure function of (severity from `07` §2) × (is-this
critical-for-where-the-project-is-going?):

| Disposition | Labels on the finding bead | Blocks the current epic gate? | On a funded fix track? |
|---|---|---|---|
| **BLOCKING** | `found-by:<src>` + `remediation:blocking` + P0/P1 + `<epic_id>` scope | **YES** — in the `08` Gap-B block set; the epic→main PR is held until it closes | YES — top of the remediation queue |
| **ASSIGNED-KNOWN-ISSUE** | `found-by:<src>` + `known-issue` + `remediation:assigned` + true-fix P-level + `<epic_id>` scope | **NO** — P2+ is below the block threshold; the epic ships on the workaround | YES — funded, but off the current epic's critical path |
| **PASSIVE-KNOWN-ISSUE** | `found-by:<src>` + `known-issue` (no `remediation:*`) + P2/P3/P4 | **NO** | NO — ledger-only, no owner, tolerable indefinitely |

The single new labeling primitive is **`remediation:blocking` / `remediation:assigned`** — the marker
that says "this finding is on a fix track and must reach a crew," distinguishing a *funded* known-issue
from a *passive* one. `07`'s two-partition ledger (passive vs assigned) becomes queryable rather than
prose. The `remediation:*` label is what a routing rule keys on; without it, a P2 known-issue is
correctly invisible to the router (passive, no owner).

**Who assigns disposition:** the assessor proposes it from the `07` rubric at file-time; the admiral
adjudicates disputes (`07` §6) and — this is the operator's rule — makes the **"critical-for-direction"
call** that promotes a would-be passive P2 into an `remediation:assigned` funded fix (even at a fix
P-level above P2, as `hk-lgykq` is P1 while the gate ALLOWED the deploy). That judgment is admiral-owned
and per-finding; everything downstream is mechanical.

---

## 4. Routing — the captain pulls remediation like any backlog, ranked

No admiral hand-routing. The finding beads are ordinary beads with priority and labels, so **`kerf
next` already ranks them into the captain's feed.** The system = one captain standing rule + one
ranking bias:

1. **Remediation beads are a first-class backlog source.** The captain treats any open bead with
   `remediation:blocking` or `remediation:assigned` exactly as it treats ready feature work: staff a
   crew/queue slot to it per the normal lane model (STARTUP Step 5 / §4 re-task). No special path — the
   whole point is that remediation flows through the *same* dispatch machinery.
2. **Ranking bias (the only ordering rule):**
   - `remediation:blocking` on an epic that is at/near its gate boundary **outranks new feature work on
     that same epic** — the epic cannot merge until it closes, so it is on the critical path by
     construction.
   - `remediation:assigned` ranks at its **true filed P-level** among the general backlog — it is
     funded but explicitly NOT ahead of the current epic (that is what "does not block" means). It gets
     drained on its own track as slots free, same as `hk-lgykq` is sequenced *after* T1 lands.
3. **Owner discipline:** when the captain staffs a remediation bead it mirrors `--assignee <crew>` (the
   Gap-1 attribution rule) so the tracker (§5) shows owner + progress without round-trips. `remediation:*`
   with no assignee for >1 gate cycle is a routing miss the tracker flags.
4. **Substrate routing for daemon-core fixes:** many remediation beads are daemon-core Go (like
   `hk-lgykq`) — the captain routes those to a codex/claude crew per the token-budget rule, NOT the pi
   path. That is a per-bead harness choice already in the captain's toolkit; the design just notes
   daemon-core findings default off the pi path.

So the admiral does zero routing. It sets the labels' *meaning* (this policy doc) and the captain's
standing rule consumes them. New finding → filed with disposition labels → appears in `kerf next` →
captain staffs it → done, no admiral in the routing loop.

---

## 5. Tracking — one query, the remediation tracker

The tracker is not a new artifact; it is a **saved query over the found-by pool** plus the deploy-readiness
report's existing Known-Issues section (`07` §5). Two views:

**A. Per-epic gate view (what the deploy-readiness report already surfaces).** At gate-time, partitioned
by the `08` Gap-B branch-scoped query:

```
# BLOCKING set (holds the gate) — scoped to the epic:
br list --status open --priority 0 --priority 1 \
  --label-any found-by:assessor --label-any found-by:admiral --label-any found-by:fast-follow \
  --label <epic_id> --json
# ASSIGNED known-issues (funded, not blocking, must show owner):
br list --status open --label remediation:assigned --label <epic_id> --json
# PASSIVE known-issues (ledger-only):
br list --status open --label known-issue --label <epic_id> --json   # minus the remediation:* rows
```

**B. Fleet remediation health view (standing, not per-gate).** The admiral's hourly audit and the
ops-monitor watch this to catch remediation debt accumulating across epics:

```
br list --status open --label-any remediation:blocking,remediation:assigned --json
# For each: P-level, epic scope, assignee (blank = routing miss), age.
```

The deploy-readiness report (`07` §5) gains one line per assigned known-issue showing **owner + fix
state**, so "assigned" is provably distinct from "parked":

```
Known issues (P2+, open, ACCEPTED residual risk):
  Passive:   hk-xxxxx (P3): <one-line> · workaround:<...>          # no owner, tolerable
  Assigned:  hk-lgykq (P1): per-bead integration-branch targeting  # owner:hawat · fix on integration/… · REDs T10 hk-xke2i
```

Storage stays the bead DB; the tracker is queries, so it can never drift from the block set — the same
invariant as `07` §5 ("block set and ledger are the same pool partitioned by P-level"), extended with
the `remediation:*` partition.

---

## 6. Drive-to-closed — the loop, with a dogfood gate and a bound

A remediation bead is not closed when a crew says "done" — it is closed when the fix **proves itself
against the harness the gap concerns** and the daemon closes it on the merge. The loop:

1. **Fix lands** on the crew's own integration branch (C-model — the fix does not self-merge to main).
2. **Dogfood assertion goes green.** For anything with a matching harness cell, the fix must flip the
   known-RED cell to green. Canonical: `hk-lgykq`'s fix must make the branch-targeting assertion
   **`hk-xke2i` (T10)** — which REDs today because the `LandsOn` path is dead code — turn GREEN. That
   green IS the closure evidence; a "fixed" `hk-lgykq` while T10 still REDs is not fixed.
3. **Corpus scenario is mandatory for every real-bug closure** (`07` §5.5): the closed finding gets a
   permanent regression scenario at repo-root `scenarios/<group>/` so it can never silently return as a
   MAJOR regression. No corpus scenario → the finding does not close. This is what makes the loop
   *ratchet* instead of oscillate.
4. **Re-gate.** For a BLOCKING finding, the assessor re-runs the epic gate; the block query (§5-A) now
   returns one fewer row. When the blocking set empties, the epic→main PR is unheld (admiral authority).
   For an ASSIGNED known-issue, closing it just clears the ledger row — no epic was held on it.
5. **Daemon owns the terminal close** (locked decision — the assessor/crew never `br close`).

**Bound against ping-pong (new — the loop must terminate):** if a remediation fix **fails its dogfood
gate or re-opens the same finding twice** (2 fix→re-gate cycles that don't green the cell), it stops
being a mechanical retry and **escalates to the admiral** as a severity/approach dispute — the fix
approach may be wrong, the severity may be mis-scored, or the substrate may be broken (`07` §6). The
admiral then decides: re-scope the fix, adjust severity, or (if it's blocking an epic indefinitely)
surface to the operator. This mirrors the daemon's max-iters guard — remediation is not allowed to
wedge a held epic forever with silent retries.

---

## 7. What is genuinely new vs reused

**Reused unchanged:** the found-by bead pool + P-level block (`07` §3, `08` Gap B); the severity rubric
and dispute adjudication (`07` §2/§6); the deploy-readiness Known-Issues section (`07` §5); the captain's
existing dispatch/lane/`--assignee` machinery (STARTUP Step 5); `kerf next` ranking; daemon-owned close.

**Genuinely new (the minimal additions this design introduces):**
1. The **`remediation:blocking` / `remediation:assigned`** label pair — the only new primitive; turns
   `07`'s prose "assigned vs passive" partition into a queryable, routable marker.
2. The **captain standing rule** "remediation beads are first-class backlog, blocking-on-gating-epic
   outranks feature work on that epic" — so routing is automatic, not admiral-hand-carried.
3. The **dogfood-green + mandatory-corpus closure gate** — a remediation bead closes only when it flips
   its known-RED harness cell and adds a regression scenario, making the loop a ratchet.
4. The **2-cycle escalation bound** — remediation can't silently wedge a held epic.

All four are policy/label/rule, not code — same as `07`/`08`. They wire in at Phase-1's first epic
boundary alongside the deterministic block and the severity matrix.

---

## 8. Where it plugs into the manifests (carry-in, not now)

Before the first gate (same window as `08` Gaps A+B):
- **assessor `operating.md` §Merge-gate step 5 (file findings):** add the `remediation:*` disposition
  label + `<epic_id>` scope label to the file rule; add the disposition decision (from §3 here) to the
  rubric it already applies.
- **captain skill / STARTUP:** add the §4 standing rule (remediation beads = first-class backlog; the
  ranking bias) so the captain drains them without admiral routing.
- **admiral soul.md / hourly audit:** add the §5-B fleet remediation-health query to the audit (catch
  routing misses + accumulating debt); the "critical-for-direction" promotion call (§3) is already
  admiral-owned per the `07` tail.
- **deploy-readiness report template (`07` §5):** add the owner + fix-state line for assigned
  known-issues (§5 here).

---

*Ends. This is the system the operator asked for: assessor files → disposition labels route it into the
captain's normal backlog → the captain staffs it like any lane → the fix proves itself against the very
harness the gap concerns and adds a corpus scenario → the daemon closes it → the gate re-runs. The
admiral sets the label policy and adjudicates severity; it does not hand-route a single bead. `hk-lgykq`
is the first real trip through the whole loop — trace it end-to-end as the acceptance case.*
