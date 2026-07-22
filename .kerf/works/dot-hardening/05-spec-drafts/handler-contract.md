# SPEC-DRAFT — `specs/handler-contract.md` (HC), Component C

> Normative spec text for the dot-hardening HC change. On `kerf finalize` this text lands in
> `specs/handler-contract.md` at the insertion points named in each block header. Voice, clause-id
> style (`HC-###`), section style (`§4.x`), and the MUST/SHOULD/MAY register match the live spec.
> Terminology is pinned per DECISIONS P1: HC says **"alias-catalog lookup"** (catalog resolve +
> fail-loud + tool-facing endpoint); EM owns **"precedence order"** (ladder/tier + seal). The bare
> word "resolution" is never used for the catalog step in HC text.
>
> **ID budget:** continues from the HC-071 high-water → **HC-072, HC-073, HC-074**. Adapter surface
> (§4.3 HC-013, L410) is NOT expanded — no new callback (D-C7). No foundation amendment.

---

## 1. NEW — HC-072 (new §4.1b "Brief transport", inserted after §4.1a HC-071, before §4.2)

### 4.1b Brief transport

> This section is the transport peer of §4.1a's input port. §4.1a governs how a *running* session
> receives a typed input; this section governs how the *initial* dispatch brief reaches the agent.
> The brief itself is assembled ONCE, upstream, by the single renderer of [execution-model.md §7.5
> EM-069]; this section constrains what the adapter MAY do with it. It stays at seam-contract
> altitude — the renderer contract (what content the brief carries, per role) is EM's, not HC's.

#### HC-072 — Adapter transports the assembled brief; it does not build content

The tool adapter MUST receive ONE already-assembled brief — the single-renderer output per
[execution-model.md §7.5 EM-069] — and MUST NOT re-derive, re-frame, or role-specialize its content.
The adapter MAY choose ONLY the **delivery envelope** by which that one brief reaches the agent. Two
delivery envelopes exist at v1, and they are **PEERS, not tiers** (reusing the §4.1a HC-070 "Ack
signal source (per driver)" precedent that codifies the claude-vs-structured split as peers):

- **claude** — write the brief to the on-disk task artifact (`<workspace_path>/agent-task.md` per
  §4.2 HC-006a) and kick the tmux pane. For an `implementer-resume` dispatch, the resume feedback is
  delivered by **paste-inject referencing an on-disk file** (the interim
  `.harmonik/reviewer-feedback.iter-<N-1>.md` artifact), NOT by argv — this file is a **claude
  transport detail** and is retained solely as claude's delivery mechanism per [execution-model.md
  §7.5 EM-056] (the canonical feedback is the reviewer's produced value on the back-edge; the file is
  transport, not the channel of record).
- **pi / codex** — carry the brief's seed as the **positional seed argv**.

The delivery envelope is the **ONLY** permitted per-tool variance. Per-role content divergence at the
adapter is **FORBIDDEN**: the adapter MUST NOT carry a reviewer-vs-implementer (or any other
per-role) content branch. Role framing, declared-input selection, and prompt/goal composition are
selected ONCE, upstream, by the single renderer (EM-069); the adapter transports the result verbatim.

This requirement is a constraint on the EXISTING `Launch` path and the fixed 4 adapter callbacks of
§4.3 HC-013 — it introduces **no new adapter method** and requires no foundation amendment. It is a
direct restatement of the modularity boundary of §4.12 HC-051 / HC-052 (the handler contract is the
daemon/execution-shape seam; a future execution shape re-implements the *transport* adapter, not the
*content* renderer).

**Honesty caveat (per driver).** "Transport-only" unifies **content**, NOT **delivery mechanism**.
The two envelopes are not interchangeable: claude's resume-feedback reference lives in the on-disk
artifact and NEVER in `LaunchSpec.Args`, whereas pi/codex carry their seed in real argv. State this
as the peer split, not as a capability tier — the same discipline §4.1a HC-070 applies to input acks.

Cross-refs: §4.2 HC-006a (`agent-task.md` path/content; per-phase field table), §4.3 HC-013 (fixed
adapter surface — not expanded), §4.1a HC-069/HC-070 (input-port peer; the "per driver" precedent),
§4.12 HC-051/HC-052 (the daemon/execution-shape seam), [execution-model.md §7.5 EM-069] (the single
renderer — the content authority), [execution-model.md §7.5 EM-056] (feedback as a back-edge value;
the `reviewer-feedback.iter-N.md` file as claude transport detail).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

---

## 2. AMEND — HC-006a trailing paragraph (specs/handler-contract.md L249)

> Replace the current L249 paragraph (the "two-builder" content story — the `agent-task.md` Body for a
> `dot`-mode node MAY be sourced from `prompt`, plus the `goal`/`prompt` ExtraContext sentence) with
> the paragraph below. The per-phase field TABLE (L228–247) and every other part of HC-006a are
> UNCHANGED. Only the trailing content-provenance paragraph is re-pointed: HC-006a keeps the
> field/transport table; it stops narrating *what content* the artifact carries and defers that to the
> EM single renderer.

**Replacement paragraph (L249):**

The **content** of the `agent-task.md` artifact — for every phase and for both `dot` and `review-loop`
modes — is authored by the single renderer per [execution-model.md §7.5 EM-069], which composes the
brief from the node's declared inputs, `prompt`, `role`, and `goal` and selects role framing. HC-006a
governs only the per-phase **field/transport** shape (the table above) and the artifact PATH; it does
NOT define the artifact's content provenance. A `dot`-mode implementer-class node's Body, the
reviewer's rubric framing, and the `goal` extra-context section are ALL products of the single
renderer, which is the sole authority on which declared inputs each role sees; the adapter transports
the rendered artifact per §4.1b HC-072 without re-deriving it. No LaunchSpec required field (HC-006),
no wire-protocol obligation, and no Outcome obligation changes — content provenance moving to EM-069 is
a rendering-side contract, not a transport-field change.

---

## 3. AMEND — HC-055a (specs/handler-contract.md L902), two edits

> Two edits to the existing HC-055a clause. Edit A generalizes the "Translation to argv" paragraph
> (L914) beyond `agent_type = claude-code`. Edit B is a new "Alias-catalog lookup vs. value validity"
> paragraph inserted immediately after the existing "Value opacity invariant" paragraph (after L910),
> pinning the fail-loud scope. The "Shape invariants" and "LaunchSpec field" paragraphs are UNCHANGED,
> and the "Value opacity invariant" paragraph is PRESERVED VERBATIM (Edit B is additive, next to it).

**Edit A — replace the "Translation to argv" paragraph (L914):**

**Translation to argv:** The handler receives `ModelPreference` from LaunchSpec and translates it to
handler-tool-specific argv. The resolved per-tool concrete in `LaunchSpec.model_preference.model` MUST
reach the value **every** tool reads — `rc.model` — regardless of `agent_type`: claude appends
`--model <model>` / `--effort <effort>` per HC-055; pi appends `--model <rc.model>`; codex appends
`--model <rc.model>` when non-empty. There is NO claude-only translation path — the prior behavior that
discarded a non-claude node model (the `effHarness == claude-code` guard in `nodeModelForHarness`) is
removed; a pi/codex node model reaches `rc.model` on the same footing as a claude node model. If the
tool rejects the value at exec (non-zero exit, tool-side error message), the handler MUST surface the
failure as `ErrStructural` with sub-reason `model_rejected_by_tool`.

**Edit B — insert after the "Value opacity invariant" paragraph (after L910):**

**Alias-catalog lookup vs. value validity (normative scope split):** The value-opacity invariant above
is PRESERVED in full — harmonik validates the model **shape**, never its **value**, and handler-side
launch failure (subprocess non-zero exit or a typed adapter error before `agent_ready`) remains the
authoritative compatibility check for whether a resolved concrete names a real, accepted model. The
fail-loud behavior introduced by §4.10 HC-073 is scoped STRICTLY to **alias-catalog lookup and tool
namespace** (an author error the daemon can detect at load: an explicit escalation whose alias has no
catalog entry for the target tool, or a tool-namespaced concrete pinned to the wrong tool). It is NOT
scoped to the concrete's **validity** — harmonik still never fails a run because a resolved concrete
"is not a real model." Ownership split: EM owns the **precedence order** (the tier ladder + the sealed
per-`(node_id, iteration_count)` concrete, per [execution-model.md §4.3 EM-012b] and the seal clause);
HC owns the **alias-catalog lookup** (catalog location + fail-loud/degrade scope) and the tool-facing
**endpoint** (`rc.model` reached for every tool). HC-073 asserts only the endpoint and the catalog; it
does NOT restate the precedence order.

---

## 4. NEW — HC-073 (§4.10, inserted immediately after HC-055a, ~L930)

#### HC-073 — Alias-catalog location and lookup fail-loud scope

The **alias catalog** — the operator-owned mapping from a model alias to a per-tool concrete model
string — MUST live at `.harmonik/config.yaml` under `models.aliases`. The catalog MUST be
**hot-reloadable**: the daemon MUST pick up an edited catalog without a restart. On a catalog **parse**
failure the daemon MUST **keep the last successfully-loaded catalog** (keep-last-good) and MUST NOT
fail a run or crash on the parse error; it emits a warning and continues with the prior good catalog.
The catalog's exact YAML shape (flat vs. per-tool-nested band) is an EM/config concern and is not
pinned here; HC-073 asserts the **location** and the **lookup semantics** only.

**Tool-facing endpoint.** The alias-catalog lookup MUST resolve, for the node's **effective tool**, a
per-tool concrete that reaches `rc.model` for EVERY tool per §4.10 HC-055a (Edit A). The effective tool
is resolved by the existing handler-selection path (§4.1 HC-003) at dispatch, BEFORE the run-record
seal is written (per [execution-model.md §4.3 EM-012b] / the seal clause); the alias-catalog lookup
produces the concrete that the seal then freezes. HC-073 does NOT own the precedence order or the seal
— it owns the catalog and the endpoint (per HC-055a Edit B).

**Three-way lookup behavior (normative — these are three DISTINCT behaviors, not one "graceful
degrade").** The daemon MUST distinguish:

1. **Fail-loud** — when the miss is an **author error**: (a) an **EXPLICIT escalation** (a per-run
   `--force-model` / a per-bead escalation label) whose alias has no catalog entry that resolves for
   the target tool; OR (b) a **tool-namespaced concrete on the wrong tool** (e.g. a `pi/…`-namespaced
   concrete on a claude node) that cannot resolve for the target tool. The daemon MUST refuse the
   dispatch with a load-time structural error naming the alias/namespace and the target tool. Fail-loud
   is scoped to the alias-catalog lookup and namespace only (per HC-055a Edit B), NEVER to concrete
   validity.
2. **Degrade + warn** — when a **DEFAULT-band** alias (one NOT carried by an explicit escalation) misses
   the catalog: the daemon MUST fall back to the **run-level resolved concrete** and MUST emit a warning
   recording the alias, the target tool, and the fallback concrete. This is the drift-safe path; a
   default-band miss MUST NOT fail the run.
3. **Keep-last-good** — when the **catalog file itself fails to parse**: the daemon MUST retain the
   last-good catalog (above) and MUST NOT treat the parse failure as a per-run fault.

**pi degrade floor (non-empty guarantee).** The degrade-and-warn fallback of behavior (2) MUST resolve
to a **non-empty** concrete for any tool that hard-fails on an empty model. pi hard-fails on an empty
`--model`; therefore, if the default-band degrade would yield an EMPTY concrete for a pi node, that
case is itself a **fail-loud** per behavior (1) — the daemon MUST NOT dispatch an empty-model pi run.
The degrade target for pi MUST be a non-empty pi-valid concrete or the dispatch fails loudly; there is
no silent empty-model pi launch.

**Back-compat.** An **un-namespaced concrete literal** (e.g. `claude-sonnet-4-6`) that is not a catalog
key is treated as a pass-through literal under the value-opacity invariant (HC-055a) — no catalog
lookup, no behavior change. The catalog is **additive**: an absent `models.aliases` section means no
aliases are defined, and every bare token is a literal, reproducing today's behavior.

Cross-refs: §4.10 HC-055a (value opacity; the lookup-vs-validity scope split; the `rc.model` endpoint),
§4.10 HC-055 (claude `--model`/`--effort` argv), §4.1 HC-003 (effective-tool selection precedes the
seal), [execution-model.md §4.3 EM-012b] (the precedence order + the sealed per-`(node_id,
iteration_count)` concrete — EM owns it).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

---

## 5. NEW — HC-074 (new §4.8a "DOT round-trip coverage seams", after §4.8 HC-038 / L716, before §4.9)

### 4.8a DOT round-trip coverage seams

> This section is CONTRACT-ONLY. It states what a harness MUST be able to assert about the DOT
> review-loop round-trip; the harness MECHANICS (fixture roots, subprocess build tags, git scratch
> setup) are owned by [scenario-harness.md] (S07) per §4.8 HC-038, exactly as the twin-parity block
> above defers drift-detection mechanics to S07. It extends the twin family of §4.8 (HC-035/HC-036):
> the round-trip runs against a handler-faithful twin, not a real agent.

#### HC-074 — DOT round-trip coverage seams

The DOT review-loop coverage seam MUST be constructed so a harness can exercise the real dispatch path
while substituting a scripted agent. The seam MUST:

- **keep the REAL launch-spec builder** — the same claude / pi / codex `LaunchSpec` construction path
  used in production (§4.10 HC-055 and the pi/codex builders); the harness MUST NOT hand-fabricate a
  `LaunchSpec`;
- **tee the built spec into a recorder** — an argv/binary tap that captures the `LaunchSpec` as built,
  before launch (the argv-faithfulness tap);
- **swap ONLY the executable to a handler-faithful twin** — the twin binary of §4.8 HC-035/HC-036,
  with `LaunchSpec.Args` UNTOUCHED (only `Binary` points at the twin);
- **run against real scratch git** — a real worktree/checkpoint trail, not a mocked VCS.

On that seam, three assertions MUST be expressible:

- **(a) forced round-trip** — a deterministic `REQUEST_CHANGES → resume → APPROVE` cycle, driven by a
  role×iteration twin script that commits per its script REGARDLESS of brief content, so the back-edge
  fires and the resume path is exercised whether or not the leak is present;
- **(b) argv/binary-matches-declared** — the built `LaunchSpec.Binary` basename MUST match the handler
  the node/bead declared (the mis-route guard); asserted from the recorder, at unit speed, on the FIRST
  dispatch;
- **(c) leak oracle** — the string `rubric` MUST NOT appear in the implementer's `source_keys` in the
  daemon-emitted typed input manifest per [execution-model.md §7.5 EM-069-MAN] (the manifest names, per
  node dispatch, the declared inputs the brief was assembled from). The oracle asserts on the
  daemon-emitted manifest, NOT on rendered-markdown substrings.

Assertions (b) and (c) are provable at **unit speed** from the built `LaunchSpec` and the daemon-emitted
manifest on the first dispatch; only assertion (a), the multi-iteration back-edge, requires the full
subprocess + git + twin harness. The mechanics of both tiers live in [scenario-harness.md] (S07).

**Honesty limits (normative — state plainly; do NOT soften).** This seam proves less than "end-to-end
per handler." Three limits hold, stated in the manner of §4.1a HC-070's "Ack signal source (per
driver)" precedent:

1. **The claude row proves less than the pi/codex rows.** claude resume feedback is delivered by **tmux
   paste-inject, NOT argv** (per §4.1b HC-072); the recorder taps `LaunchSpec.Args` and therefore
   CANNOT capture it, and the twin substrate is NOT tmux-backed, so paste-inject is a **no-op under the
   twin**. Accordingly, the **claude row proves only that "the daemon assembled and wrote the brief"** —
   its delivery is proven separately by existing unit tests and by live operation, not by the round-trip
   recorder. The **pi/codex rows are argv-faithful** (the resume seed is a positional argv the recorder
   captures). The harness MUST NOT be described as "end-to-end per handler."

2. **The round-trip proves PLUMBING, not a real model round-tripping.** The twin fakes agent consumption
   by fiat — it commits per its script regardless of what the brief contains. Assertion (a) therefore
   proves that the daemon **builds and wires** the `REQUEST_CHANGES → resume → APPROVE` path (edges,
   resume, iteration cap), NOT that a real model reads and acts on the feedback. It passes identically
   whether the cross-role leak is fixed or not; the leak oracle (c) is the SEPARATE behavioral check.
   The oracle is **known-RED until the single renderer (EM-069) and the manifest emitter (EM-069-MAN)
   land** — there is no reachable GREEN state before then, because today's path renders the same full
   body into every role's brief and emits no distinct per-role `source_keys` to assert on. The Tasks
   pass MUST sequence the oracle's flip to GREEN after EM-069 / EM-069-MAN.

3. **The recorder asserts argv; the twin scripts outcomes.** The twin validates its OUTPUT (NDJSON
   progress-stream parity per §4.8 HC-036), NOT the argv it received. Argv-drift detection therefore
   rests entirely on the recorder plus hand-maintained per-handler signature assertions — the rot
   surface is RELOCATED to those assertions, not eliminated. State this as "the recorder asserts argv,
   the twin scripts outcomes"; never claim "the twin proves faithfulness."

Cross-refs: §4.8 HC-035/HC-036/HC-038 (twin parity; S07 owns drift/mechanics), §4.1b HC-072 (claude
paste-inject vs pi/codex argv — why the claude row is not argv-faithful), §4.1a HC-070 (the "per
driver" honesty precedent), [scenario-harness.md] (S07 — harness mechanics), [execution-model.md §7.5
EM-069] (single renderer — assertion (a)/(c) depend on it), [execution-model.md §7.5 EM-069-MAN]
(daemon-emitted typed input manifest — the leak oracle's substrate).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

---

## Changelog fragment (HC)

**Target file:** `specs/handler-contract.md`
**Status:** modified
**Motivating design:** dot-hardening Component C (`.kerf/works/dot-hardening/04-design/handler-contract-design.md`, DECISIONS D-C1..D-C8 + change-design review fixes D-FIX-1/2, P1–P4).

**What changed:**

- **NEW HC-072** (new §4.1b "Brief transport", after §4.1a): the adapter transports ONE
  already-assembled brief (the EM-069 single-renderer output) and chooses ONLY the delivery envelope —
  claude = brief file + pane kick (feedback via paste-inject to the interim
  `reviewer-feedback.iter-N.md` file); pi/codex = positional seed argv — as the sole per-tool variance.
  Per-role content divergence at the adapter is forbidden. Expressed within the fixed 4 callbacks (HC-013
  not expanded); a restatement of the HC-051/052 seam. Peers-not-tiers honesty per HC-070.
- **AMEND HC-006a** (trailing L249 paragraph): re-pointed from the two-builder content story to the
  EM-069 single-renderer contract as the authority for artifact content provenance; the per-phase
  field/transport table is unchanged.
- **AMEND HC-055a** (L902): (A) generalized "Translation to argv" so the resolved per-tool concrete
  reaches `rc.model` for EVERY tool (claude-only guard removed); (B) new scope-split paragraph — the
  value-opacity invariant is preserved (shape not value; validity stays handler-side), and HC-073
  fail-loud is scoped to alias-catalog lookup/namespace (author error), never to concrete validity. EM
  owns precedence order + seal; HC owns lookup + endpoint.
- **NEW HC-073** (§4.10, after HC-055a): alias catalog at `.harmonik/config.yaml models.aliases`,
  hot-reloadable, keep-last-good on parse failure; three-way behavior (fail-loud on explicit
  escalation / wrong-tool namespace; degrade+warn on default-band miss; keep-last-good on parse
  failure); pi degrade floor is non-empty-or-fail-loud.
- **NEW HC-074** (new §4.8a "DOT round-trip coverage seams", after §4.8): contract-only coverage seam
  (mechanics owned by scenario-harness S07 per HC-038) — real launch-spec builder, argv recorder tap,
  executable-only twin swap, real scratch git; assertions (a) forced round-trip, (b)
  argv/binary-matches-declared, (c) leak oracle (`rubric` ∉ implementer `source_keys` in the EM-069-MAN
  manifest). Three honesty limits stated plainly: claude row proves brief-assembly only (paste-inject
  not argv); round-trip proves plumbing not a real model (leak oracle known-RED until EM-069/EM-069-MAN);
  recorder asserts argv, twin scripts outcomes.

**Counts:** 3 new clauses (HC-072, HC-073, HC-074); 2 amendments (HC-006a L249, HC-055a two edits). No
new adapter method; no HC-013 foundation amendment.
