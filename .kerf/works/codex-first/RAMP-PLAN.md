# codex-first — Per-bead `harness:codex` LABELLING PLAN

> **Status: PREP ONLY. LABEL NOTHING.** No production bead carries `harness:codex` as a result of
> this document. Admiral authorizes labelling separately, and only after the assessor's capability
> verdict lands.
>
> Author: juliet, 2026-07-22. Source-only audit (no daemon, no experiment).
> Reported to admiral on `--topic gate`.

---

# THE RAMP'S SAFETY IS A PROPERTY OF THE GRAPH, NOT OF THE LABEL.

Three findings that looked like three separate hazards — review-loop mode (hk-gfamb), the cognition
gate (hk-01vs0), and the reviewer pins on `review`/`qa` — are one property wearing three hats: each
is **safe on the standard DOT graph and unsafe elsewhere**. None of them is a property of the label.

This **inverts** what is worth guarding:

- **Labels are the safe part.** Adding more labelled beads does not raise risk. Blast radius grows
  linearly and every one of them is reverted by removing the label.
- **What breaks the ramp is changing `workflow.dot` or the workflow mode.** A graph edit or a mode
  change silently removes the property that makes every existing label safe — including labels
  applied days earlier by someone who checked correctly at the time.

> ## STANDING RULE — while any ramp is live, THREE things are frozen:
>
> 1. **The workflow MODE is frozen** — `workflow_mode` stays `dot`, globally and per-bead.
> 2. **`workflow.dot`'s CONTENTS are frozen** — no edits to its nodes or their attributes.
> 3. **The SET OF DISPATCHABLE GRAPHS is pinned to those with pinned reviewers** — today that is
>    **`workflow.dot` and `standard-bead.dot` ONLY**.
>
> If any of the three must change, **un-label first**.

**Why part 3 exists, and why parts 1–2 were not enough.** Freezing one graph's *contents* silently
assumed beads would keep using that graph. They need not: `queue.Item.WorkflowRef` /
`resolveWorkflowRef` (`workloop.go:3291-3294`, `:4109-4113`) lets a bead **select a different graph**
per-bead, with no config change. So *which* graph a bead selects is itself part of the safety
surface, not a given. This is not hypothetical — **21 of the 23 reviewer-bearing graphs under
`specs/examples/` have zero reviewer pins** (hk-3dgps), because the pin is opt-in per graph
(`dot_cascade.go:1411`, `effectiveNodeHarness.Valid()`). A labelled bead pointed at any of them gets
a **codex reviewer**.

> **WAVE-1 SELECTION RULE:** a wave-1 bead must carry `WorkflowRef` = the default graph, **or none
> at all**. Any bead carrying a `WorkflowRef` to another graph is disqualified from wave 1.

Everything else in this document is an **instance** of that rule, not an additional precaution:

| Mechanism | Is an instance of |
|---|---|
| §1a `workflow_mode` freeze + two-part check | the **MODE** half of the rule |
| hk-ofm89 dispatch guard (precondition for wave 2) | the **MODE** half, enforced by the daemon instead of by discipline |
| §1b wave-1 precondition (no `type="gate"` node) | the **GRAPH** half |
| §1c unpinned reviewer-class sites | why the **GRAPH** half exists |
| hk-3dgps (21 unpinned corpus graphs) | the **GRAPH-SET** part — precondition for widening past default-graph-only |
| §1d verdict pinning | the same rule across **TIME**: a verdict is only valid for the tree it ran against |

Read them as one rule with several enforcement points. A rule people understand survives; a list of
precautions decays.

---

## §1d. VERDICT PINNING — an assessor verdict is pinned to its SHA and does NOT roll forward

An assessor capability verdict certifies **one tree**, not the project. It is **pinned to the SHA it
ran at** and carries no authority over any later commit.

- Record the SHA on every verdict and every control result. A result whose baseline is "whatever
  HEAD was that night" is unusable a week later.
- **The verdict does not roll forward.** At the epic → main boundary the assessor **RE-GATES at
  actual HEAD**.
- The baseline has already drifted: **4d308f3b → 87b0e3ca** (lima's hk-qx065 fix). Any verdict or
  control quoted without its SHA is already ambiguous between those two trees.

This is the same invariant as the graph/mode freeze, applied across time rather than across
configuration: a safety result is only valid for the exact thing it was measured against.


---

**This is a LABELLING plan, not a deploy plan.** The wholesale substrate flip
(`HARMONIK_SUBSTRATE=codexdriver`) is dead: the assessor proved it is argv-byte-identical to the
per-bead label but with strictly more blast radius, since it hands every claude-resolved bead a
codex-JSON-RPC-locked substrate (hk-3eso9 silent hang). The per-bead label is the only path forward.

Consequences that shape everything below:

- **No daemon restart, no binary swap, no config change** is required to label a bead.
- **Blast radius is exactly the set of labelled beads** — nothing else changes behaviour.
- **The abort action is "remove the label."** Not "roll back a binary," not "restart the daemon."

---

## 0. What is and is not established

Two separate claims. Only the first is settled.

| Claim | Status | Basis |
|---|---|---|
| A per-bead `harness:codex` label routes **implement** to codex while **review** + **qa** stay claude, in DOT cascade | **PROVEN** (source) | §1 below |
| Codex is **capable** of completing real harmonik beads under the real commit gate | **OPEN** | assessor's verdict, not this document |

The ramp shape is safe. Whether codex can do the work is a different question, and ramping labelled
beads in production to find out would be testing in production. Do not conflate them.

### Economics (N=2, india strength test 2026-07-22): review is the bottleneck, not implementation

Measured on a real bead (`br-r91`, run `019f8855`, terminal close under the unpatched production
commit gate):

- **implement (codex) leg ≈ 6% of wall-clock and 0 fresh Claude tokens.**
- **the two claude reviewer nodes (review + qa) ≈ 83% of wall-clock and 535,369 fresh Claude tokens**
  (32.4M incl. cache reads).

Therefore **codex-first alone does NOT buy the originally-modeled runway.** Codex-first removes the
*cheap* leg; the expensive half is review, which stays on claude. Anyone modeling "codex-first
extends our life" as a large saving is **wrong by roughly an order of magnitude** on this evidence —
say so wherever the ramp is discussed.

**Consequence — `hk-pisrf` is PROMOTED to ACTIVE** (de-hard-code the reviewer: DOT nodes take
model + harness per-node from config). It is the lever that actually converts codex-first into
runway, because it is the only thing that lets a reviewer-class node run a cheaper model/harness.
This is **NOT** a conclusion to put reviewers on codex — review quality is load-bearing; `hk-pisrf`
only buys the ability to *tune* per-node cost. Ramp remains UNAUTHORIZED at N=2.

---

## 1. CONSTRAINT — abort-criterion grade, applies to every ramp bead

> **The per-bead `harness:codex` label may be applied ONLY to beads dispatched through the DOT
> CASCADE.**
>
> In **REVIEW-LOOP (non-DOT) mode** the reviewer inherits the implementer's resolved harness at
> tier 3 with **NO claude pin** (`internal/daemon/reviewloop.go:1244-1251`), so a `harness:codex`
> bead there sends **BOTH implementer AND reviewer** to codex. Tier-1 is neutralized in that path by
> an empty `core.BeadRecord{}`, so this is **inheritance, not label-beats-pin**. Review-loop mode has
> no DOT node to carry a `harness=` attribute at all (`reviewloop.go:1240-1242`).
>
> The **DOT-cascade pins are what make the label safe** (`internal/daemon/dot_cascade.go:1411-1421`).

Violating this constraint manufactures a **false red**: a codex reviewer emits no verdict, and the
run fails in a way that looks like a product defect rather than a harness-routing mistake.

### Why the cascade pins hold (citations)

- `implement` carries **no** `harness=` attribute — `workflow.dot:81-87` — so the tier-1 label governs it.
- `review` pinned `harness="claude-code"` — `workflow.dot:129`.
- `qa` pinned `harness="claude-code"` — `workflow.dot:146`.
- The pin is **enforced, not merely declared**: `dot_cascade.go:1411-1421` substitutes
  `pinnedHarnessLaunchSpecBuilder`, which **bypasses `resolveHarness` entirely**
  (`harnessregistry.go:166-181`), so a tier-1 label cannot reach it.
- The comment at `dot_cascade.go:1412-1415` documents this exact failure mode as the **reason**
  hk-2jxqg introduced the pinned builder. Previously found and fixed.

### Why the nil-registry caveat cannot bite in production

- `newHarnessRegistry` never returns `(nil, nil)` — only `(reg, nil)` or `(nil, err)`:
  `harnessregistry.go:47-68` (built `:48`, returned `:67`). Codex registered at `:52`.
- Production `newWorkLoopDeps` calls it at `workloop.go:1103` and **fails daemon start** on error
  (`workloop.go:1104-1106`). Assigned at `workloop.go:1148`. No path proceeds with a nil registry.
- The nil case is **test-only**: `export_test.go:546` passes `p.HarnessRegistry`, nil unless supplied.
- Were it ever nil, the `dot_cascade.go:1411` guard fails, `specBuilder` falls back to
  `deps.launchSpecBuilder` — which production sets to `routedLaunchSpecBuilder`
  (`workloop.go:3795`) — and the tier-1 label **would** override the reviewer pin. That is the
  predicted false-red, gated shut by the fail-fast constructor above.

---

## 1a. GUARDED INVARIANT — the safety property is one config line deep, and one label deep

The constraint in §1 is only *enforceable* because production dispatches through DOT cascade. That
is not a law of the system; it is a **setting**, and it can be defeated **two** ways — both silent.

### Vector 1 — the global config line

- Production sets it in exactly one place: `.harmonik/config.yaml:22` → `workflow_mode: dot`.
- The flag default agrees: `cmd/harmonik/main.go:988` defaults `--workflow-mode` to
  `core.WorkflowModeDot`, and the config value is applied when the flag is not passed explicitly
  (`cmd/harmonik/main.go:1168`).
- Dispatch branches on the resolved mode at `internal/daemon/workloop.go:3926` (dot) vs `:4100`
  (single / review-loop fallthrough).

**If anyone edits that line while labelled beads exist, the §1 safety property is gone silently.**
Every labelled bead then sends implementer *and* reviewer to codex, and the resulting false red
looks like a codex capability failure.

### Vector 2 — a per-bead `workflow:` label (worse, and not in the original brief)

`resolveWorkflowMode` takes a **tier-1 per-bead `workflow:<mode>` label**
(`internal/daemon/moderesolve.go:59-71`) that overrides the global setting **for that bead alone**.

So a single bead carrying **both** `harness:codex` and `workflow:review-loop` (or `workflow:single`)
defeats the DOT-cascade boundary by itself — with the global config untouched and looking correct.
This vector is worse than Vector 1 because it is invisible in config review and scoped to one bead.

### The check — run BEFORE labelling, and again before each wave

```bash
# 1. Global mode must be dot.
grep -E '^\s*workflow_mode:' .harmonik/config.yaml     # MUST print: workflow_mode: dot

# 2. No labelled bead may also carry a workflow: label (Vector 2).
for b in $(br list --status=open --json 2>/dev/null | jq -r \
      '.[] | select(.labels // [] | index("harness:codex")) | .id'); do
  echo "== $b"; br show "$b" | grep -E '^Labels:'      # MUST NOT contain any workflow:<mode>
done
```

**Both must hold. If either fails, remove the `harness:codex` labels before proceeding** — do not
"fix it later," because in the interim every labelled bead is producing false evidence about codex.

**Standing rule:** `workflow_mode` is frozen for the duration of any labelled bead. Changing it is a
change to the ramp, not an unrelated config edit. Anyone who needs it changed must un-label first.

> **Filed as hk-ofm89 (P1) and required before wave 2.** This §1a check is a *procedural* control
> over a *silent* failure, and those get violated eventually. hk-ofm89 refuses to dispatch a
> `harness:codex` bead when its **resolved** mode is not dot (keying on the resolved value, so it
> catches Vector 2 and not merely the global config), emitting a loud event. Wave 1 runs on the
> manual check; wave 2 does not start until the guard is landed. See §3, §6.

---

## 1b. OPEN WAVE-1 GATING QUESTION — hk-01vs0 (NOT resolved in our favour)

**Status: OPEN.** Recorded open per admiral (08:51Z) pending india's cross-check. My source read is
below as evidence, not as a ruling.

`internal/daemon/dot_gate.go:296` builds the cognition-gate launch spec from
`deps.launchSpecBuilder` **unconditionally** — no node-harness pin, no reviewer-harness resolution —
while passing `phase: handlercontract.ReviewLoopPhaseReviewer` (`dot_gate.go:280`). It is a
reviewer-class site with no protection.

**Does a tier-1 `harness:codex` label reach it? From source: YES.** Production leaves
`deps.launchSpecBuilder` nil and builds it at `workloop.go:3793-3800` as
`routedLaunchSpecBuilder(harnessRegistry, beadRecord, …)` — passing the **real bead record**, with
the code comment stating "build from harnessRegistry + beadRecord (tier-1 labels)". So the builder
handed to `dot_gate.go:296` is label-sensitive. This is **not** a site that only ever sees the
global default.

**Why that is the dangerous shape:** the gate would resolve to codex *from the label alone*, while
the global default is still claude and `workflow_mode` is still dot — so **every written control in
§1a reads as satisfied** while a reviewer-class phase runs on codex.

**Mitigating fact, which is why this may not block wave 1:** the site is only reached for graphs
containing a `NodeTypeGate` node (`dot_cascade.go:1013`, `case core.NodeTypeGate`). The standard
`workflow.dot` has **no gate node** — its only node types are 3× `agentic` and 4× `non-agentic`, and
`commit_gate` is `type="non-agentic"` / `handler_ref="shell"` (`workflow.dot:96-98`), not a gate
node. So a ramp bead on the standard graph should never execute `dot_gate.go:296`.

**Therefore, treated as a wave-1 PRECONDITION rather than assumed safe:**

> Before labelling any wave-1 bead, confirm the graph it will dispatch under contains **no
> `type="gate"` node**, **and that no sub-workflow it invokes contains one either** — `dot_gate` has
> two callers, `dot_cascade.go:1013` and `sub_workflow_runner.go:351` (mike). If it does — or if the
> bead carries a `workflow_ref` selecting a non-standard graph — **do not label it** until hk-01vs0
> is fixed.

Do not downgrade this to a fast-follow on the strength of the standard graph alone: the protection
is "the standard graph happens to have no gate node," which is a property of a *file*, not a guard.
Anyone adding a cognition gate to `workflow.dot` silently re-arms it.

---

## 1c. Reviewer-class launch sites — full source enumeration

Requested by admiral (08:51Z) because two unpinned reviewer-class sites were found by accident, so
the question became *how many exist*, not *is this one pinned*. Source read, not a live test.

There are exactly **four** places that select a launch-spec builder (every `deps.launchSpecBuilder`
consumer); three are reviewer-class.

| # | Site | Class | Harness behaviour | Label-sensitive? |
|---|---|---|---|---|
| 1 | `dot_cascade.go:1402-1426` | reviewer-class when the node is a reviewer (phase set `:1294`) | **PINNED** — `pinnedHarnessLaunchSpecBuilder` when `node.Harness` is valid **and** registry non-nil (`:1411-1421`); bypasses `resolveHarness` (`harnessregistry.go:166-181`) | **No**, while pinned. Falls back to `deps.launchSpecBuilder` (label-sensitive) if the node has no `harness=` attr |
| 2 | `dot_gate.go:296-300` | reviewer-class (`phase` `:280`) | **UNPINNED** — `deps.launchSpecBuilder` unconditionally | **YES** — hk-01vs0, see §1b |
| 3 | `reviewloop.go:1244-1251` | reviewer-class | **INHERITS** implementer's resolved harness as tier-3; tier-1 neutralized via empty `core.BeadRecord{}` | No (tier-1 blocked) but **follows the implementer onto codex** — §1 |
| 4 | `reviewloop.go:305` | implementer (not reviewer-class) | `deps.launchSpecBuilder` | Yes — correct by design; this is where the label is *supposed* to act |

`buildLaunchSpecReviewer` (`launchspecbuild.go:119-134`) is **not** a fifth site: it only decorates
an already-built base spec with phase/mode/iteration fields and never selects a harness.

**What the table says about the safety story:** reviewer-class protection is not uniform. Exactly
one of the three reviewer-class sites (#1) is genuinely pinned, and only when the node carries a
`harness=` attribute. #2 is unprotected and label-reachable; #3 has no pin available at all. The
ramp is safe today because standard-graph beads reach only #1 — with review and qa both carrying
`harness="claude-code"` — and never #2 or #3. That is a narrower guarantee than "reviewers are
pinned to claude," and it should be stated that way.

### Corroboration and refinements (mike's independent adversarial pass)

Two independent source reads now agree on the count and on all three reviewer-class sites. Mike's
refinements, all of which sharpen the **GRAPH** half of the top-line rule:

- **(a) #3 is codex-reviewer *by construction*.** `reviewloop.go:1246` blanks tier-1 and sets tier-3
  to the implementer's harness, so codex-implementer → codex-reviewer is not a risk to be avoided in
  that mode, it is the guaranteed outcome.
- **(b) The #1 pin has a SILENT HOLE.** The parser accepts `agent_runtime=` as an alias for
  `harness=`, but the consumer reads only `node.Harness` — so a reviewer node written with
  `agent_runtime=` falls through to the label-sensitive builder while *looking* pinned to a graph
  author. **Latent only:** `workflow.dot` uses `agent_runtime` zero times today. Mike is filing a
  bead. This is the sharpest possible example of the top-line rule: the safety lives in how the
  graph is written, and a graph edit can remove it while the diff looks correct.
- **(c) "Pinned" ≠ "pinned to claude."** An implementer node carrying `reviewer_harness="codex"`
  pins the reviewer *to codex* via the override branch at `dot_cascade.go:1403-1406`. The pin
  mechanism is neutral; only the graph's chosen *value* makes it safe.
- **(d) hk-01vs0's reachability is DOUBLE.** `dot_gate` has two callers — `dot_cascade.go:1013` and
  `sub_workflow_runner.go:351` — so §1b's precondition must consider sub-workflows too, not just
  gate nodes in the top-level graph.

Every one of these is a **graph-authoring** risk, not a labelling risk. They are evidence for the
top-line invariant, not exceptions to it: none can be triggered by adding another labelled bead, and
all can be triggered by editing the graph.

---

## 2. Candidate first ramp beads

Selection criteria, in priority order:

1. **DOT cascade** dispatch (constraint §1 — disqualifying if not met).
2. **Objective pass/fail oracle** — a test or gate decides, not a judgement call.
3. **Low blast radius** — failure cannot wedge a queue, corrupt shared state, or block another lane.
4. **Small, self-contained diff** — ideally one package.
5. **Not on the critical path** — a failure costs a retry, not a schedule.

**EXCLUDED: hk-pina9** — india is using it as the strength test. Do not label it.

Because the whole point is a bounded first step, the first ramp beads should be picked *at ramp
time* from the then-current ready set against the criteria above, not frozen now and gone stale.
The criteria are the durable part. Concretely, prefer in this order:

- **Tier 1 (best):** a bead whose acceptance is a single new/changed unit test in one package —
  the commit gate's build + vet + scoped tests is a complete oracle.
- **Tier 2:** a mechanical refactor with no behaviour change (rename, dead-code removal) where
  `go build` + `go vet` + existing tests fully decide correctness.
- **Tier 3:** a docs/comment-only bead — proves the harness round-trips and commits, though it
  exercises little capability.

**Do NOT pick for the first wave:** anything touching `internal/daemon` dispatch, anything with a
known-red test in its package (see the known-red list on hk-04q2j), anything whose verification is
"a human reads it", or anything another crew is concurrently editing.

---

## 3. Order and concurrency

| Wave | Count | Gate before proceeding |
|---|---|---|
| 1 | **1 bead** | Full manual inspection: correct harness routed, reviewer stayed claude, verdict emitted, commit gate ran, merge clean. §1a check run by hand. |
| 2 | **2 beads**, disjoint packages | **hk-ofm89 (the §1a guard) MUST be landed first** — admiral's call. Plus: both wave-1 criteria met with no new failure mode. |
| 3 | **3–4 beads** | Steady state; only then treat labelling as routine. |

**Wave 2 is gated on the guard, deliberately.** Wave 1 runs on the procedural §1a check because one
bead under full manual inspection is exactly the condition where a human check is reliable. Wave 2
is where the check starts being skipped — it has worked once and begins to feel like ceremony — and
where two concurrent beads mean nobody is watching both. That is the point at which the invariant
must be enforced by the daemon rather than by discipline.

**Never raise concurrency and introduce a new bead class in the same wave** — a failure then has two
candidate causes and costs a bisect.

Wave 1 is one bead specifically so that any failure is unambiguous. Resist the urge to "save time"
by starting at three.

---

## 4. ABORT CRITERIA — decided now, while nobody is invested in success

Stop the ramp and revert to claude **immediately** on any of the following. These are not judgement
calls; if one triggers, un-label and reassess.

**A. Routing violations (abort on first occurrence — these mean the model is wrong):**
- A reviewer or qa node runs on anything other than claude-code.
- A `harness_selected` event reports tier 1 for a **reviewer** node.
- Any `bead_label_conflict` event on a ramp bead.
- A ramp bead turns out to have dispatched through review-loop mode rather than DOT cascade
  (constraint §1 violated — this is our error, not codex's; un-label, do not count it as a codex
  capability data point either way).

**B. Capability failures (abort on the second occurrence, or the first that is not clearly
environmental):**
- Bead completes but the commit gate fails on work the implementer claimed done.
- No verdict emitted / run ends without reaching a terminal state.
- Agent never reaches `agent_ready`.
- Codex commits nothing despite reporting success.

**C. Blast-radius breaches (abort on first occurrence):**
- A ramp bead wedges its queue or requires manual daemon intervention.
- A ramp bead's failure blocks an unrelated lane.
- Shared state (target branch, shared config) is left dirty.

**Explicitly NOT abort criteria** — expected noise, do not panic-revert on:
- A test that is already on the known-red baseline failing again. **Check the right package:** the
  two fleet-known-red tests (`TestSelectSubstrate_RequireIsolationBoundary_HK5H759`,
  `TestCodexSpawnSeam_ProductionRunner_RemoteCwd_czb11`) live in **`cmd/harmonik/`** —
  `cmd/harmonik/substrate_select_router_hkm4c3_test.go` and
  `cmd/harmonik/substrate_select_spawn_seam_czb11_test.go` — **not** `internal/daemon/`. Grepping
  `internal/daemon` finds nothing and tempts the false conclusion that your tree is clean. Note also
  that `scenario-gate.sh` scopes to changed package dirs, so a daemon-only change never runs them
  and a green daemon-only gate says nothing about them either way. The seven additional known-red
  `internal/daemon` tests are recorded on hk-04q2j — **UNVERIFIED / under live re-classification
  (2026-07-22):** three independent agents (india, the hk-pkxju reviewer, a qa node) flagged the
  standing already-red/known-red crew guidance as unreliable. Do not treat the "seven" as settled or
  chase individual entries as known-broken: juliet flagged 4 of the 7 as suspect-flaky (load flake,
  not deterministic breakage) and an independent pristine-tree measurement (hk-rn4i4) found only 3
  RED. `internal/daemon` RED status is under live classification via **hk-rn4i4** (full-package
  `-p 1` run). (Note: the two by-name cmd/harmonik tests above are verified present and correctly
  attributed — they exist; the unreliable part of the old guidance was pointing crews at the wrong
  package, already corrected here.)
- A `could not import` / `build failed` run — that is hk-gjbpp (cache reap); re-run with
  `GOCACHE=$(mktemp -d)` before drawing **any** conclusion.
- One slow run under host load.

> **Bias note, deliberate:** the criteria above are strict on routing and lenient on infrastructure
> noise. That asymmetry is intentional. A routing fault silently produces false evidence about
> codex's capability, which is worse than a loud infrastructure failure we can recognise.

---

## 5. Mechanics — label and un-label

**Label one bead:**
```bash
br update <bead-id> --add-label "harness:codex"
br show <bead-id>            # VERIFY the label is present and spelled exactly harness:codex
```

**Pre-flight, before dispatch — all three must hold:**
1. The bead dispatches through **DOT cascade** (constraint §1). If unsure, do not label it.
2. Exactly **one** `harness:` label. Multiple labels are treated as *absent* and the walk falls
   through to tier 2 (`harnessresolve.go:82-85`) — the bead silently runs on claude and the ramp
   learns nothing.
3. The agent-type value is valid; an invalid value is likewise treated as absent
   (`harnessresolve.go:79-81`).

**Un-label (the revert):**
```bash
br update <bead-id> --remove-label "harness:codex"
br show <bead-id>            # VERIFY removed
```

Un-labelling is a **complete** revert of the routing decision: with no `harness:` label, tier 1 is
absent, the walk falls to tier 2/3/4, and the bead runs on the default harness exactly as before.
No daemon restart, no config change, no binary swap. That is the property that makes a per-bead
label the right ramp shape rather than a substrate flip.

**Verifying a ramp bead actually routed as intended:** check the `harness_selected` events for the
run — the implement node should report tier 1, and review/qa should report the node pin, not tier 1.

---

## 6. Open items not settled by this plan

- **Codex capability** — assessor's verdict. This plan is inert until then.
- Whether the review-loop inheritance in §1 should be *fixed* (give review-loop mode a claude pin)
  rather than merely avoided. Currently avoided by constraint. Worth a bead if labelling becomes
  routine, because "remember not to label those beads" is a weak control that will eventually be
  violated.
- **Make §1a self-enforcing.** Today the DOT-only boundary is protected by a procedure. A real guard
  — refuse to dispatch a bead carrying `harness:codex` when its *resolved* workflow mode is not dot,
  and emit a loud event — would close both Vector 1 and Vector 2 at once and remove the need for the
  manual check entirely. This is the durable fix; the §1a check is the interim control.
