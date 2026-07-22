# 04 — Change Design: `specs/handler-contract.md` (HC), Component C

> CHANGE-DESIGN pass. For each requirement C1–C3: the concrete clause new/amended, its SHAPE (the
> normative posture, not final text), how it satisfies the requirement, back-compat, cross-file
> dependency, and the honesty caveat it must carry. One step before Spec-Draft.
> Expands DECISIONS D-C1..D-C8. All ids/lines cite `specs/handler-contract.md` unless noted.
> **ID budget:** new clauses continue from HC-071 (current high-water) → HC-072, HC-073, HC-074.
> Adapter surface (HC-013, L410) is NOT expanded — no new callback (D-C7).

---

## C1 — Transport-only adapter (D-C1, D-C7)

### (i) Clauses touched
- **NEW `HC-072` — "Adapter transports the assembled brief; it does not build content"** — lands as
  the lead clause of a **new §4.1b "Brief transport"**, inserted after HC-004 (L103) and before the
  `shell` handler HC-063 (L110), pairing structurally with §4.1a's input port (HC-069/070/071,
  L143/163/182).
- **AMEND HC-006a's dot-mode override paragraph (L249)** — the "`agent-task.md` Body for a `dot`-mode
  … MAY be sourced from the node's `prompt`" paragraph. Re-point it at the EM single-renderer contract
  (Component B, B1) as the authority for *what* content the artifact carries; HC-006a keeps only the
  per-phase *field/transport* table, not the two-builder content story.

### (ii) Normative posture (shape, 1–3 sentences)
HC-072 states: the tool adapter receives **ONE already-assembled brief** (the EM single-renderer
output) and MAY choose only the **delivery envelope** — claude: write the brief file + kick the pane;
pi/codex: carry the positional seed argv — and MUST NOT re-derive, re-frame, or role-specialize the
brief content. The delivery envelope is the **only** permitted per-tool variance; per-role content
divergence at the adapter is forbidden. This is expressed as a constraint on the existing `Launch`
callbacks (D-C7) — no new adapter method — and is a direct restatement of HC-051/052 (L982/L988: the
handler is the daemon/execution-shape seam; shapes re-implement the adapter, not the content).

### (iii) Back-compat
Strict restatement + pointer; no field, no wire change, no argv change. The interim
`.harmonik/reviewer-feedback.iter-N.md` file **survives as claude's transport detail** (D-B3): claude
delivers feedback by paste-inject referencing that on-disk file, not by argv. HC-006a's per-phase
field table (L228 rows) is untouched; only its trailing two-builder *content* paragraph (L249) is
re-pointed. Every existing `.dot` and every live claude/pi/codex launch is unaffected.

### (iv) Cross-file dependency
Depends on **EM B1** (single renderer) landing the brief that HC-072 says the adapter merely
transports — HC-072 must cite `[execution-model.md]` B1 as the content authority and must NOT restate
the renderer contract (EM owns it). HC-006a L249 amendment likewise points at EM B1 (replacing today's
`WG-040`/`§7.5` two-builder pointer).

### (v) Honesty caveat it carries
Name the two envelopes as **PEERS, not tiers**, reusing HC-069/070's existing "Ack signal source (per
driver)" language (L163): claude = brief-file + tmux pane-kick; pi/codex = positional-seed argv. State
plainly that "transport-only" does not make the two envelopes interchangeable — claude's feedback
reference lives in the on-disk artifact and NEVER in `LaunchSpec.Args` (D-C8 limit 1). The honesty is
that C1 unifies *content*, not *delivery mechanism*.

---

## C2 — Per-tool model-alias resolution, fail-loud on explicit escalation (D-C2, D-C3, D-C4)

### (i) Clauses touched
- **AMEND HC-055a — ModelPreference descriptor invariants (L902)**: two edits.
  1. Generalize the "Translation to argv" paragraph (currently scoped **"For `agent_type =
     claude-code`"**, L~925): the **resolved per-tool concrete** MUST reach the value ALL tools read
     (`rc.model` → pi `--model` `pilaunchspec.go:284`; codex `--model` when non-empty
     `codexlaunchspec.go:207`; claude `--model` HC-055 L855). This is the spec obligation whose code
     counterpart is **deleting the claude-only guard `nodeModelForHarness` at `dot_cascade.go:1182`**
     (the `effHarness == AgentTypeClaudeCode` gate at :1183 that discards a pi/codex node model).
  2. **PRESERVE the value-opacity invariant** verbatim (L~915): harmonik still validates model *shape*
     (`^[A-Za-z0-9._:/-]+$`, ≤128), never *value*; handler-side launch failure stays the authoritative
     compatibility check. C2 changes *where the concrete comes from*, not *who validates it*.
- **NEW `HC-073` — "Alias-catalog location + resolution fail-loud scope"** — lands in §4.10 right after
  HC-055a (L~930), the ModelPreference home.

### (ii) Normative posture (shape)
**HC-055a amendment:** every tool's resolved concrete reaches `rc.model`; the claude-only path is
deleted; value-opacity preserved (fail-loud is RESOLUTION-scoped, never VALIDITY-scoped — D-C3).
**HC-073** declares: (a) the alias catalog lives at `.harmonik/config.yaml` `models.aliases`,
**operator-owned, hot-reloadable, keep-last-good on parse failure**; (b) **EM owns the resolution
ladder + seal** (D-B5/B8/B9) — HC-073 asserts only the tool-facing endpoint "resolved concrete reaches
`rc.model` for EVERY tool"; (c) the **three-way fail/degrade split** as a normative sub-list:
- **fail-loud** — an **explicit escalation** (per-run force / per-bead label) whose alias misses the
  catalog for the target tool, OR a **tool-namespaced concrete on the wrong tool** (`pi/…` on a claude
  node) = author error, caught at load;
- **degrade + warn** — a **default-band** alias that misses the catalog → fall back to the run-level
  resolved concrete + emit a warning (drift-safe);
- **keep-last-good** — a catalog **parse** failure → retain the last successfully-loaded catalog.

**Degrade target for pi (D-C4):** the degrade fallback MUST resolve to a **non-empty pi-valid**
concrete. pi hard-fails on an empty model (`crossharness_empty_model_test.go`,
`codex_empty_model_hkd170r_test.go`); if the default-band degrade would yield empty for a pi node, that
is itself a **fail-loud** — never dispatch an empty-model pi run.

### (iii) Back-compat
An **un-namespaced concrete literal** (e.g. `claude-sonnet-4-6`) stays a pass-through under
value-opacity — no catalog lookup, no behavior change. The catalog is **additive**: absent
`models.aliases` → no aliases defined → a bare token that isn't a catalog key is treated as a literal
(today's behavior). The deletion of the `nodeModelForHarness` guard **changes pi/codex behavior by
design** (they previously silently dropped a node model) — this is the bug fix, flagged in ISSUES #3
as low blast radius (beads OFF this phase).

### (iv) Cross-file dependency
**EM owns the ladder + seal** (D-B5 EM-012b-NODE L295 flip; D-B8 `node_model_seal`; D-B9 replay reads
the seal, EM-055 L1592). HC-073 must NOT restate the ladder — it cites EM and asserts only the
tool-facing endpoint + the catalog location + the fail-loud scope. §6.1 `ModelPreference` (L1195) needs
**no new field** if resolution completes before the descriptor seals (D-C2).

### (v) Honesty caveat it carries
State the **three-way split explicitly** (D-C8 limit 3) so "graceful degradation" is never read as
"never fail": fail-loud (explicit/namespaced) / degrade+warn (default-band) / keep-last-good (parse).
And state the value-opacity boundary plainly: harmonik still never checks that a resolved concrete
*names a real model* — that stays handler-side. Fail-loud is about *resolution* (catalog miss on
escalation, cross-tool namespace), not about *the concrete's validity*.

---

## C3 — DOT round-trip coverage seams / coverage contract (D-C5, D-C6, D-C8)

### (i) Clauses touched
- **NEW `HC-074` — "DOT round-trip coverage seams"** — lands in a **new §4.8a "DOT round-trip coverage
  seams"**, inserted **after the twin-parity block §4.8** (HC-038 ends L716) and **before §4.9
  Ready-state detection (L717)**, cross-referenced from HC-035 (L655) as an extension of the twin
  family. HC-074 owns the coverage **CONTRACT**; `[scenario-harness.md]` (S07, per HC-038 L711) owns
  the harness **MECHANICS**.

### (ii) Normative posture (shape)
HC-074 requires the seam to be constructed so a harness can: **keep the REAL launch-spec builder**
(HC-055 claude / pi / codex builders — the `LaunchSpecBuilder` DI at `export_test.go:165`, consumed
`dot_cascade.go:1402`), **tee the built spec into a recorder** (the argv-faithfulness tap), **swap ONLY
the executable to a handler-faithful twin** (HC-035/036 binary; `Args` untouched), and **run against
real scratch git**. On that seam, three assertions MUST be expressible:
- **(a) forced round-trip** — a deterministic `REQUEST_CHANGES → resume → APPROVE` cycle, driven by a
  role×iteration twin script that ignores brief content by fiat, so the back-edge fires regardless of
  the leak;
- **(b) argv/binary-matches-declared** — the built `LaunchSpec.Binary` basename matches the
  handler the node/bead declared (the **c074 mis-route guard**); asserted from the recorder, at unit
  speed, on the FIRST dispatch;
- **(c) leak oracle** — the implementer's brief does NOT carry the reviewer's rubric source-key.

HC-074 must state the **cost/tier split** (ADV-C S4): (b) + (c) + reviewer-harness-override are
provable at **unit speed** from the built `LaunchSpec` + on-disk briefs on the first dispatch (upgrade
one capture-and-abort stub — `ExportedCaptureExtraContextBuilder` `export_test.go:887` — to record the
whole spec); only (a) the multi-iteration back-edge needs the full `//go:build scenario` subprocess +
git + twin harness.

### (iii) Back-compat
Purely additive test-contract surface — no production code obligation beyond the two already covered by
C1/C2 (the single renderer + the manifest emitter, both EM-side). Reuses existing seams — does NOT
invent: `LaunchSpecBuilder` DI, the capture stubs, `em015FixtureNilStdoutSubstrate`
(`scenario_reviewloop_em015de_hkintln_test.go:89`), the `cmd/harmonik-twin-{claude,codex,pi}` binaries,
`TwinBinaryPath()` (`scenariotest.go:490`). No existing `dot_*_test.go` is invalidated.

### (iv) Cross-file dependency
- **`[scenario-harness.md]` (S07)** owns harness mechanics per HC-038 (L711); HC-074 cites it and stays
  at contract altitude.
- **Leak oracle (c) depends on EM**: it asserts on a **daemon-emitted typed input manifest**
  (`role → source-keys`), NOT on rendered markdown substrings. The manifest **EMITTER is daemon-side,
  spec'd in EM** (the renderer emits it as a production byproduct of the real routing path) — HC-074
  states the dependency, does NOT define the emitter. The oracle only becomes REAL **after per-role
  source keys exist** (post-renderer, Component B): it lands now as **known-RED, GREEN after B** (D-C6).
- Forced round-trip (a) depends on **EM B3/B4** (feedback as a value on the back-edge; iteration-1
  unbound vs resume bound) — there must be a real feedback value to round-trip.

### (v) Honesty caveats it carries (D-C8 — state plainly, do NOT soften)
1. **claude row proves less than pi/codex.** claude resume feedback is **tmux paste-inject, NOT argv**
   (`pasteInjectImplementerResume`, `pasteinject.go`, imports `internal/lifecycle/tmux`) — the recorder
   **cannot** capture it; and the twin substrate (`em015FixtureNilStdoutSubstrate`,
   `…hkintln_test.go:262`) is **not tmux-backed**, so paste-inject is a **no-op under the twin**. So the
   **claude row proves only "the daemon assembled/wrote the brief"** (its delivery is proven by existing
   unit tests + live); **pi/codex rows are argv-faithful** (positional seed in real argv,
   `agentseedprompt.go:44`). Reuse HC-070's per-driver "Ack signal source" precedent to say this
   honestly. Do NOT frame the harness as "end-to-end per handler."
2. **The twin fakes agent consumption by fiat** (ADV-C S2/S3). In NO handler does the harness prove the
   agent *reads and acts on* the feedback — the twin commits per its script regardless of brief content.
   So the forced round-trip (a) proves **PLUMBING** (the daemon builds + wires the resume path), NOT the
   **PRODUCT** (that a real model round-trips once the leak is fixed). It passes identically whether the
   leak is fixed or not — the leak oracle (c) is the separate behavioral check; the round-trip is a
   plumbing regression test. Name what it proves and what it does not; do not claim it substitutes for
   live e2e.
3. **The recorder asserts argv; the twin scripts outcomes** (ADV-C S6). The twin validates *output*
   (NDJSON parity), not the *argv it received*, so argv-drift detection rests entirely on the
   recorder + hand-maintained per-handler signature assertions — the rot surface is **relocated to those
   assertions, not eliminated**. HC-074 must say "the recorder asserts argv, the twin scripts outcomes,"
   never "the twin proves faithfulness."

---

## Summary — clause ledger

| Req | Clause | Kind | Home / insertion | Cites (cross-file) |
|---|---|---|---|---|
| C1 | **HC-072** | new | §4.1b "Brief transport", after HC-004 (L103) | EM B1 (renderer) |
| C1 | HC-006a §249 (L249) | amend | re-point to EM B1 | EM B1 |
| C2 | HC-055a (L902) | amend | generalize argv-translation; PRESERVE value-opacity | EM B5/B8/B9 |
| C2 | **HC-073** | new | §4.10 after HC-055a (L~930) | EM (ladder/seal) |
| C3 | **HC-074** | new | §4.8a, after HC-038 (L716), before §4.9 (L717) | scenario-harness S07; EM (manifest emitter, B3/B4) |

**Adapter-surface budget (D-C7 / HC-013 L410):** all three requirements are expressible within the
fixed 4 callbacks — **no new adapter method, no foundation amendment**. C1 is a constraint on `Launch`;
C2 changes the value in `rc.model` (already read by every builder); C3 is a test-contract on existing
DI seams.

## Under-specified decisions (carry to Spec-Draft / ISSUES)
- **HC-006a §249 amendment scope:** whether the interim `reviewer-feedback.iter-N.md` file is *named*
  in HC-072 as claude's transport detail, or only referenced via EM B3. Recommend naming it in HC-072
  (it is a claude-adapter transport fact, not an EM renderer fact) — confirm at draft.
- **HC-073 catalog schema shape:** `models.aliases` is stated as `.harmonik/config.yaml` with
  per-tool concrete values, but the exact YAML shape (flat `alias: {tool: concrete}` vs nested band)
  is EM's config concern — HC-073 should assert location + semantics only and defer shape to EM/config.
  Flagged so Spec-Draft does not over-specify the tool-facing clause.
- **Leak-oracle RED→GREEN sequencing (D-C6):** HC-074 lands the oracle now as known-RED; the Tasks pass
  must sequence its flip after EM's renderer + manifest emitter land (Component B), else the clause has
  no GREEN state to reach.
