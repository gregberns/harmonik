# Adversarial Review — A1 "Maximal Typed I/O"

> Reviewer stance: attack. Ground truth = the five `research/*/findings.md`, `01-frame.md`,
> and A2 as the competing pole. Verdict at the bottom.

---

## Fatal flaws

### F1 — The dataflow it types does not exist. An LLM node's real output is a git commit, not a value.

A1's thesis is "every node becomes a typed function" emitting typed outputs. But A1 *itself*
concedes the load-bearing exception (§4): **"You cannot statically type an agent's free-text
output."** So the entire typed-output apparatus reduces to the *few* control values that drive
branching — verdict, status, a counter. That is precisely the surface A2 types with a five-word
`enum(...)` annotation and nothing else.

Worse, the flagship typed output — `implement.diff : ref<diff>` — is a **fiction**. The
implementer does not "return" a diff; it mutates a git worktree, and the daemon *derives* the diff
from `review_base_sha`/`review_head_sha` (findings 01 §2, `dot_cascade.go:1257-1286`; findings 05
§5 notes the diff is "already delivered out-of-band … via the git worktree"). The bytes already
have a home the daemon already knows. Declaring `diff` a typed output port and threading it across
an edge (`map="implement.diff -> patch"`) invents a dataflow edge for a value that never travels —
the reviewer reads the worktree, not an edge payload. A1's own §7 exposes this: the
`implement -> commit_gate` edge carries `map="implement.diff -> _artifact"` — a bind to a
throwaway `_artifact` input whose only purpose is to satisfy the model. That is ceremony modeling
a handoff that physically is not a handoff.

**The abstraction leaks at exactly the node type harmonik runs most.** Typing "every node as a
function" fits deterministic nodes and fights agentic ones — and agentic implement/review is the
whole workload.

### F2 — The artifact store is a greenfield subsystem invented to hold bytes that already have homes.

`ref<KIND>` requires a new typed store + artifact layout, local **and SSH-mirrored** for remote
(§1.5, §8.4). What does it hold?

- `ref<diff>` — the diff. Already in git (worktree + base/head SHAs). No new store needed.
- `ref<transcript>` — the session transcript. Already owned by the harness (findings 05 §5).
- `ref<verdict-detail>` / `ref<rubric>` — reviewer findings + rubric. Already delivered as
  `.harmonik/reviewer-feedback.iter-<N-1>.md` and `review-target.md` (findings 01 §2/§4).

Every KIND in A1's closed registry maps to bytes that **already have a durable, replay-correct
home today.** A1 admits (§8.4) this is "the part most likely to leak operational bugs" — GC,
remote mirroring, handle stability across replay, `.gitignore` discipline. This is a large new
determinism surface (the current design seals only the param map into the Run record, findings 01
§6 / findings 02 §2.3) invented to solve a bulk-payload-on-the-bus problem that **A1 has no
evidence harmonik has** (findings 05 §5: "There is no bulk value being wrongly shoved through a
param today. Nothing to split.").

### F3 — It proposes deleting the most-tested, load-bearing path (the review-loop Go path) and rerouting it through the newest, least-tested machinery.

§5.1: `review-loop` mode "desugars into a typed DOT graph … the hardcoded Go path can then be
*deleted*." The frame's actual pain (P3) is that the resume/feedback path is **under-tested and
already caused a no-commit-on-resume loop** (findings 01 §4; commit `8dbe5a17`). A1's response is
to take the one delivery path that works today across both harness families and make it depend on:
a new typed binder, a new artifact store, a new schema registry, and a two-tier validator — all
shipping simultaneously. This maximizes blast radius against the exact invariant the work is
supposed to *harden*. You do not stabilize a fragile resume path by moving it onto four new
subsystems in one schema bump.

### F4 — It does not fix the operator's headline problem (per-bead model override) at all.

Operator problems per frame + memory: per-bead model override, easy control, no-leak feedback.
A1's own mapping table (§6) marks model resolution **"unchanged — deliberately stays node config,
not dataflow."** So on per-bead model control, A1 delivers **exactly what exists today** (Tier-1
`model:<alias>` label; findings 01 §5, §8.2 "No per-queue-item model field") — identical to A2.
On no-leak, A1 and A2 reach the **same observable result** (rubric structurally unreachable from
the implementer brief). On "easy control," A1 makes the `.dot` *harder* to author (F5). Net: A1
buys nothing over A2 on two of three operator axes and is worse on the third — at ~5× the build
cost by its own §8.1 accounting (AST + loader + value/artifact store + assembler + cascade binder
+ every harness adapter).

---

## Serious concerns

### S1 — JSON-in-DOT is unauthorable; A1 admits it and then proposes a *second* grammar to hide it.

§8.5 concedes "JSON-in-DOT-attribute is ugly." The §7 worked example is the proof — this is a
single node, hand-authored:

```dot
implement [
    type="agentic", agent_type="implementer", handler_ref="claude-implementer",
    inputs="{\"task\":{\"type\":\"string\",\"required\":true},
             \"feedback\":{\"type\":\"object(feedback.v1)\",\"required\":false}}",
    outputs="{\"diff\":{\"type\":\"ref<diff>\"}, \"summary\":{\"type\":\"string\"}}"
];
```

GraphViz DOT string attributes are single-line-oriented and quote-delimited; embedding JSON means
`\"`-escaping every key and value, and the multi-line form above is already outside comfortable
DOT. Machine-generation is brittle (double-escaping), hand-editing is error-prone, and a
mis-escaped brace is a parse failure with a bad diagnostic. A1's fix (§8.5) is to add a *compact
mini-syntax* (`inputs="task:string!, feedback:feedback.v1?"`) that a new parser expands — i.e.
**invent a second grammar to paper over the first.** That is more surface, not less. Contrast A2's
entire type surface: `params="TASK, RUBRIC, MAX_ROUNDS=3, VERDICT:enum(A,B,C)"` — one attribute,
comma-separated, already proven in attractor-pi-dev (findings 05 §4).

### S2 — The type safety is opt-in and the default path stays leaky — the guarantee undermines itself.

§5.2 back-compat: a node with no `inputs`/`outputs` gets implicit-default ports, and the
implicit implementer `task` still binds the bead body. §8.2 then admits: "authors under-declare
and lean on implicit defaults, **partially re-exposing P1** for un-migrated graphs." So the leak
A1 claims to make "impossible to express" (§2.1) is only closed for graphs that pay the full
port-declaration tax. The default experience is still leaky. A2 closes the leak on the *default*
path (reviewer brief sourced from the reviewer node's own prompt) with no per-graph opt-in.

### S3 — Two-tier validation adds a new post-spend failure mode.

§4 + §8.3: the structural-check-at-crossing "can *reject a completed agent's output* (tokens
already spent) if it doesn't match the declared type." This is a failure class A2 does not
have. A1's mitigation ("keep declared outputs minimal") is an admission that the maximal typing is
counterproductive — the safe move is to type almost nothing, which is A2.

### S4 — Load-time behavioral tightenings that can break existing graphs.

A1 upgrades `context_keys` from a bare list to a typed declaration and **turns HC-062
warn-and-drop into a hard error** (§1.6, §6 table). It also makes edge-condition LHS type-checked
(today explicitly NOT validated — OQ-WG-002 is OPEN, findings 02 §2/§7b; findings 03 flags this as
"a solution waiting for a routing bug that hasn't happened"). These are not additive — they can
convert a currently-loading graph into a load error. Claiming this closes OQ-WG-002/D8 as a "win"
inverts the research consensus that type-pinning should wait for an actual routing bug.

### S5 — "Corollary of one model" is marketing, not economy.

A1's central pitch (§0, §9) is that ports collapse P1/P4/P5/§6-feedback into "corollaries of one
model." But the four problems are *already* nearly-independent one-line fixes in A2 (un-defer
reviewer prompt; params carry bead text; thread reviewer prompt into the review brief; lift the
feedback file out of review-loop-only scope). Unifying four cheap fixes under one expensive model
is not a saving — it is bundling four $1 repairs into a $25 platform and calling the platform free
because the repairs come with it.

---

## What it gets right (steelman — genuinely better than A2)

1. **The single port-driven brief assembler.** A1's collapse of the two divergent builders
   (`buildAgentTaskContent` + `buildReviewTargetContent`, findings 01 §2) into **one assembler
   that renders a brief from declared inputs + role framing** (§2.3) attacks the P3/P7 harness- and
   role-divergence at its structural root. A2 keeps two builders and two resume paths reading one
   file; A1 makes the harness adapter a pure *transport* detail over one representation. This
   factoring is correct and worth extracting **independent of the rest of A1** — it reduces the
   exact drift surface (findings 01 §8.6) that caused the no-commit-on-resume bug. If harmonik ever
   grows the node-role diversity the frame gestures at (planner/validator/debugger), "declare a
   role, get an assembled brief, zero new builders" is the right architecture and A2's per-node
   prompt convention would sprawl into N bespoke builders.

2. **Leak-as-a-load-error rather than leak-by-convention.** A1's explicit edge binding makes the
   leak *impossible to express* and *statically checkable*, where A2 makes it *conventionally
   avoided* (correct only if the author wires params right). If the product ever exposes
   user-authored multi-role graphs as a surface, A1's structural guarantee is the safer contract.

**When A1 becomes the correct choice:** the day harmonik actually runs *graph diversity* — multiple
producer→consumer pairs, fan-in/parallel, genuinely agent-to-agent bulk artifacts that are **not**
git-derivable, or an open product surface of arbitrary user-authored multi-role graphs. Under that
future the type lattice, edge binder, and artifact store earn their keep. Today the dominant shape
is one linear implement→review loop with one back-edge (findings 02 §1 standard-bead; A2 §0), and
none of that generality is exercised.

---

## Verdict — REJECT as the base design; ADOPT-PARTIALLY two ideas into A2.

**Reject** as the shipping design: the type lattice, `ref<KIND>` artifact store, edge `map`
binder, schema registry, typed-context hard-error tightening, two-tier crossing validator, and
especially the **deletion of the review-loop Go path** in the same schema bump. This is speculative
generality (findings 05 §5 through-line) priced for graph diversity harmonik does not run, it does
not improve the operator's model-override problem over the status quo, it makes authoring worse
(S1), and it stakes the already-fragile resume path (P3) on four new subsystems at once (F3).

**Adopt into A2 (the recommended base):**
1. **The single port/role-driven brief assembler** — collapse the two builders into one
   role-parameterized assembler with one feedback representation across harnesses. Do this even
   under A2; it is the real fix for P3/P7 harness divergence and stands alone.
2. **The `feedback.v1` shape as a documented struct** — *if and only if* a concrete routing need
   appears, borrow A1's feedback object as the content schema of the existing markdown file /
   `context.last_verdict`, **without** the ref store or the edge binder. The verdict label is
   already structured (`outcome.preferred_label`); the findings are inherently free text (A2 §3.3).

Ship A2's schema bump + ~4 spec deltas. Keep the typed edge-dataflow layer on the shelf and let it
earn its way in the day a second producer/consumer pair actually exists.
