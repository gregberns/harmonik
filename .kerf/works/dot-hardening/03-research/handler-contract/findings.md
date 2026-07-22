# 03 — Research: `specs/handler-contract.md` (HC) grounding for C1–C3

> RESEARCH pass. Grounds the planned HC change (dot-hardening Component C) in current spec + code with
> exact insertion points. Does NOT design new text. Every claim cites `HC-<id>` (spec line) or `file:line`.
> **Current high-water ID: HC-071** (verified: highest numeric id in the file; new clauses continue at HC-072).
> Note: numeric order != document order — HC-054..HC-057, HC-062..HC-068 are interleaved out of sequence.

---

## 1. Current HC clauses in the blast radius (section + line)

| Concern | Clause | Line | Current state (verbatim gist) |
|---|---|---|---|
| Handler selection | **HC-003** | 90 | Handler binary chosen from DOT `handler_ref`/`agent_type` + YAML at **load-time**; no runtime real-vs-twin branch. |
| agent-type / mode resolution | **HC-003a** | 96 | `workflow_mode` is dispatch-level, NOT a handler-selector. **"The adapter surface (§4.3.HC-013) MUST NOT expand"**; watcher stays mode-agnostic. Same handler across all phases; phases differ by LaunchSpec *content* (prompt, skills, freedom). |
| Launch idempotency | HC-004 | 103 | Key = `(run_id,node_id,phase,iteration_count)`; back-edge resume iters are distinct tuples. |
| Shell handler | **HC-063** | 110 | Built-in deterministic `shell` handler for `non-agentic` tool nodes; runs `/bin/sh -c <tool_command>` in-process, exempt from §4.2/§4.9/§4.6. **This is a PRODUCTION tool-node handler — distinct from the test-only `HandlerBinary:"/bin/sh"` executable-swap the harness uses today.** |
| LaunchSpec record | HC-006 / HC-006a | 202 / 218 | HC-006a is the per-phase field table. Its **last paragraph (line 249)** already documents dot-mode `prompt=` overriding the agent-task Body via `agenttask_chb028.go` — the current two-builder behavior C1 collapses. |
| Model argv (claude) | HC-055 | 855 | `--model`/`--effort` appended **only** for claude; from `LaunchSpec.model_preference.{model,effort}`. |
| ModelPreference invariants | **HC-055a** | 902 | Shape-validated (`^[A-Za-z0-9._:/-]+$`, <=128; effort enum). **Value-opacity invariant**: harmonik validates shape not value; handler-side launch failure is the authoritative compat check. "Translation to argv" para is scoped **"For `agent_type = claude-code`"** — no per-tool alias resolution today. |
| context_keys discipline | **HC-062** | 1108 | `context_keys` is a per-workflow `.dot` graph attr; unregistered `Outcome.context_updates` keys warn-and-drop. **Not a blocker for C**; relevant only if declared node I/O reuses the context-key registry (a WG/EM concern, flagged for cross-file). |
| Adapter surface | HC-013 | 410 | Fixed 4 callbacks; adding one needs a foundation amendment. C1/C3 must NOT expand it. |
| Twin parity | HC-035..038 | 655–711 | Twin = real-handler substitute, same Handler iface + wire protocol + tags; §4.8. Carve-out: in-process unit fakes are NOT twins. C3 seams extend this family. |
| Modularity boundary | HC-051..053 | 982–996 | Handler contract IS the deterministic-daemon / execution-shape seam; `Handler`/`LaunchSpec`/taxonomy/event-set are the stable cross-subsystem surface. C1 (transport-only) is a direct restatement of HC-051/052. |
| Input port (paste vs structured) | **HC-069/070/071** | 143/163/182 | §4.1a. **HC-070 "Ack signal source (per driver)" already codifies the claude(tmux-paste) vs structured-driver split as PEERS** — the exact divergence C1 collapses and C3's honesty caveat leans on. HC-071 is the depguard seam-inversion (`internal/handler` !imports `internal/lifecycle/tmux`). |
| §6.1 schemas | — | 1124–1198 | `LaunchSpec` (1171) carries `model_preference`; `ModelPreference` record (1195) = `{model,effort}`. C2 may extend the resolution story; C1 adds no field. |

No launch-spec/argv/resume-delivery clause exists as a standalone HC id today — delivery is described inside
HC-055 (claude argv), HC-006a (per-phase table), and HC-069/070 (input port). **C1 has no single existing home;
it is a new clause.**

---

## 2. Code anchors verified (file:line)

**Two divergent feedback-delivery paths (C1 collapses these):**
- **claude paste-inject** — `pasteInjectImplementerResume` in `internal/daemon/pasteinject.go`; imports
  `internal/lifecycle/tmux` (`:48`), delivers via `tmux load-buffer`+`paste-buffer`+`send-keys` (`:131`,`:16`).
  The resume brief is the rewritten on-disk `agent-task.md` (phase=implementer-resume); **the feedback
  reference is NOT in argv** — confirmed.
- **pi/codex resume seed** — `implementerResumeSeedPrompt(beadID, priorIteration)`
  `internal/daemon/agentseedprompt.go:44`; template `:33`; builds a **positional seed argv** that names
  `.harmonik/reviewer-feedback.iter-<N-1>.md` (`:35`) and requires a new `Refs:` commit. Argv-faithful.

**Model silently discarded for pi/codex (C2 fixes):**
- `nodeModelForHarness` `dot_cascade.go:1182-1187`: returns `nodeModelAttr` **only** when
  `effHarness == core.AgentTypeClaudeCode`, else `resolvedModel` (empty for pi -> falls to pi config). The
  claude-only guard is explicit at `:1183`. Called at `dot_cascade.go:1351` (`nodeModel := nodeModelForHarness(...)`).
- pi passes `--model rc.model` `pilaunchspec.go:284`; codex passes `--model rc.model` when non-empty
  `codexlaunchspec.go:207`. **Both already read `rc.model`** — so C2's fix = resolve alias->per-tool concrete
  into `rc.model` for ALL harnesses and delete the claude-only guard; no launch-spec change needed.
- pi hard-fails on an empty model at launch (family asymmetry pinned by `crossharness_empty_model_test.go`,
  `codex_empty_model_hkd170r_test.go`) — so a degrade-to-empty is NOT safe for pi (see C2 hazard).

**Test seams that already exist (C3 builds on, does not invent):**
- `LaunchSpecBuilder` DI field: `export_test.go:165` (`func(ctx, claudeRunCtx)(handler.LaunchSpec, claudeRunArtifacts, error)`);
  consumed at `dot_cascade.go:1402` (`specBuilder := deps.launchSpecBuilder`), optionally overridden by
  `pinnedHarnessLaunchSpecBuilder` `:1416`.
- Capture-and-abort stubs: `ExportedMinimalLaunchSpecBuilder` `export_test.go:877`,
  `ExportedCaptureExtraContextBuilder` `:887`, `ExportedCaptureNodePromptBuilder` `:901`.
- `em015FixtureNilStdoutSubstrate` `scenario_reviewloop_em015de_hkintln_test.go:89` — runs the **real argv**
  as a subprocess with **nil stdout** (`:262` comment: "watcher=nil / tmux substrate path"). **Confirms
  ADV-C S1: this substrate is NOT tmux-backed, so `pasteInjectImplementerResume` is a no-op under it.**
- Twin binaries: `cmd/harmonik-twin-{claude,codex,pi,generic,session}`; `TwinBinaryPath()`
  `scenariotest/scenariotest.go:490`.
- **Code/spec disagreement (EM §7.5, for cross-file note):** `dot_cascade.go:875` DOES call
  `WriteReviewerFeedback` in dot mode (hk-wixms); `:825` flags remote dot multi-iteration as unsupported.
  EM owns the reconciliation; HC only needs C1/C3 to treat the feedback file as an *interim* transport.

---

## 3. Per-requirement grounding (C1, C2, C3)

### C1 — Transport-only adapter

**(a) Current state.** No clause states "adapter transports, never re-derives content." The opposite is
implicit: HC-006a §249 documents the dot-mode Body override, and HC-055 / HC-069-070 describe **two
per-tool delivery shapes** (claude on-disk-brief + paste-inject; pi/codex positional seed) as first-class.
Role-specific content is built daemon-side by the two writers in `agenttask_chb028.go` (per findings §2).
HC-051/052 already say the handler is the daemon/execution-shape seam and shapes re-implement the adapter
— C1 is the missing "…and only the *transport*, never the *content*" half.

**(b) Exact HC insertion point.** New clause **HC-072** in **§4.1 (after HC-004, before HC-063)** or as a
lead clause in a new **§4.1b "Brief transport"** paired with §4.1a's input port. It must (i) state the
adapter receives ONE already-assembled brief + chooses only the delivery envelope, (ii) cross-ref HC-051/052
(seam) and HC-013 ("adapter surface MUST NOT expand" — C1 delivers within the fixed 4 callbacks), and (iii)
**amend HC-006a's §249 paragraph** to point at the single-renderer contract in EM (Component B) instead of
the two-builder behavior it currently documents as evidence.

**(c) Design notes.** C1 is mostly a *constraint restatement* + a pointer to EM B1 (the renderer lives in
EM, not HC). HC's job: name the two envelopes (claude: brief file + pane kick; pi/codex: positional seed
argv) as the ONLY per-tool variance, and forbid per-role content divergence at the adapter. HC-069/070's
"PEERS not tiers" language is the model to reuse.

**(d) Open questions.** Does C1 land as one new clause, or as an amendment to HC-055/HC-006a + a short seam
clause? Does the interim `reviewer-feedback.iter-N.md` file survive as a claude transport detail, or is it
retired once the brief carries feedback as a rendered input?

**(e) Hazard.** HC-013 forbids expanding the adapter surface without a foundation amendment; C1 must be
expressible within `Launch`/the existing callbacks. If "transport-only" is read as requiring a new adapter
method, it trips HC-013 and needs an amendment it shouldn't need.

### C2 — Per-tool model-alias resolution, fail-loud on explicit escalation

**(a) Current state.** HC-055a value-opacity invariant: harmonik validates model *shape* not *value*;
"Translation to argv" is scoped **only** to `agent_type = claude-code`. HC-055 emits `--model` claude-only.
Code: `nodeModelForHarness` (`dot_cascade.go:1182-1187`) discards a pi/codex node model — the exact bug.

**(b) Exact HC insertion point.** **Amend HC-055a** (the ModelPreference home) + a new clause **HC-073**.
HC-055a today says "future handler types may accept arbitrary model strings" and "handler-side launch
failure is the authoritative compat check" — C2 refines this into a resolution ladder: alias->per-tool
concrete via an operator-owned catalog, plumbed to the value **all** tools read (`rc.model`). Add the
fail-loud vs degrade split as a normative sub-list. §6.1's `ModelPreference` record (line 1195) may gain a
"resolved per-tool concrete" note but needs no new field if resolution happens before the descriptor seals.

**(c) Design notes.** From brainstorm-B §2: three model shapes — bare alias (per-tool catalog resolve;
missing entry -> degrade+warn), namespaced concrete (`pi/…` on claude node -> loud load fail), un-namespaced
concrete (back-compat literal). The catalog is `.harmonik/config.yaml` `models.aliases`, hot-reloadable,
keep-last-good on parse failure. Escalation source (force / bead label) that can't resolve for the target
tool -> fail loud; default-band alias miss -> degrade+warn.

**(d) Open questions.** Does the catalog / alias-resolution contract belong in HC (tool-facing) or EM
(resolution ladder), with HC only asserting "the resolved concrete reaches `rc.model` for every tool"? How
does "fail-loud on explicit escalation" reconcile with the HC-055a value-opacity invariant that currently
says harmonik never fails on a model value — is fail-loud scoped to *resolution* (catalog/namespace), never
to *the concrete's validity* (still handler-side)?

**(e) Hazard.** pi hard-fails on an empty model (findings §2). A "degrade to run-level" that yields empty
for a pi node re-creates a launch failure, not a graceful degrade — the degrade target must be a non-empty
pi-valid concrete, not empty. The claude-only guard's original rationale (a claude literal is meaningless to
pi) is *correct*; C2 must preserve loud-fail for a cross-tool concrete while enabling aliases.

### C3 — Test seams / coverage contract (with honesty caveats)

**(a) Current state.** Twin-parity contract exists (HC-035..038, §4.8). DI seams exist in `_test` code
(findings §2) but are NOT spec'd as a normative coverage contract. No HC clause requires argv-faithfulness
capture or a leak oracle.

**(b) Exact HC insertion point.** New clause **HC-074** in a new **§4.8a "DOT round-trip coverage seams"**
(right after the twin-parity block, §4.8, line ~715), cross-ref'd from HC-035. It should require the seam to
support: keep the REAL launch-spec builder (the HC-055/pi/codex builders), tee the built spec into a
recorder (argv tap), swap only the executable to a handler-faithful twin (HC-035/036), run against real
scratch git — enabling (a) forced REQUEST_CHANGES->resume->APPROVE round-trip, (b) per-handler
argv/binary-matches-declared assertion (the c074 mis-route guard), (c) the leak oracle.

**(c) Design notes.** Reuse, don't invent: `LaunchSpecBuilder` DI (`export_test.go:165`), the capture stubs,
`em015FixtureNilStdoutSubstrate`, the twin binaries, `TwinBinaryPath()`. ADV-C S4 splits the tiers: the c074
guard + leak oracle + reviewer-harness-override are provable at **unit speed** from the built LaunchSpec on
the FIRST dispatch (upgrade one capture stub to record the whole spec); only the multi-iteration back-edge
needs the full subprocess+git+`//go:build scenario` twin harness.

**(d) Open questions.** Does HC own the *coverage contract* (what must be assertable) while
`scenario-harness.md` (S07, per HC-038) owns the *harness mechanics*? What is the exact shape + owner of the
typed input manifest (see §4)? Does the leak-oracle clause land now as "known-RED until the renderer lands"
or only after Component B?

**(e) Hazard.** ADV-C S6: the twin validates *output* (NDJSON parity) not the *argv it received*, so argv
drift falls entirely on hand-maintained `argvSig` assertions — the rot surface moves, it isn't eliminated.
A C3 clause that claims "twin proves faithfulness" overstates; it must say "the *recorder* asserts argv,
the twin scripts *outcomes*."

---

## 4. HONESTY LIMITS the spec must state plainly (from ADV-C, code-verified)

1. **claude resume feedback is tmux paste-inject, NOT argv.** `pasteInjectImplementerResume`
   (`pasteinject.go`, imports `internal/lifecycle/tmux`) delivers via `tmux load-buffer/paste-buffer/send-keys`;
   the feedback reference lives in on-disk `agent-task.md`, never in `LaunchSpec.Args`. The argv recorder
   **cannot** capture it. Further, `em015FixtureNilStdoutSubstrate` (the twin substrate) is **not
   tmux-backed** (`scenario_reviewloop_em015de_hkintln_test.go:262`), so `pasteInjectImplementerResume` is a
   **no-op under the twin**. Therefore: the **claude row proves only "daemon assembled/wrote the brief"**;
   **pi/codex rows are argv-faithful** (positional seed in real argv). The spec must state this split
   plainly, not sell "end-to-end per handler." HC-070's existing "Ack signal source (per driver)" language
   is the precedent for saying it honestly.

2. **The leak oracle asserts on a daemon-emitted TYPED INPUT MANIFEST (role->source keys), not on markdown
   substrings** — so tests survive the renderer prose change. Two hard dependencies (ADV-C S5): (i) the
   **daemon** (not test code) must emit the manifest as a production byproduct of the real routing path, else
   it's the test's *model* of the daemon and rots silently; (ii) the manifest only becomes a real leak oracle
   **after** per-role source keys exist (i.e. after the renderer change / Component B) — today the same full
   body renders into both briefs, so there are no distinct keys to assert on. State the dependency; do not
   present the manifest as an independent, already-available test-layer deliverable.

3. **C2 degradation is a three-way split, not "graceful degrade everywhere":** fail-loud on an explicit
   escalation (force / bead label / namespaced concrete) that can't resolve for the target tool; degrade+warn
   only on a **default-band** alias that misses the catalog; keep-last-good on catalog **parse** failure.
   This must be stated as three distinct behaviors so "graceful degradation" is not read as "never fail."

---

## Open questions for change-design

1. **C1 shape:** one new seam clause (HC-072) vs. amendments to HC-055 + HC-006a §249 + a short pointer?
   Does the interim `reviewer-feedback.iter-N.md` file stay as a claude transport detail or retire?
2. **C2 home:** does the alias-catalog / resolution ladder live in HC (tool-facing) or EM (ladder), with HC
   asserting only "resolved concrete reaches `rc.model` for every tool"?
3. **C2 vs value-opacity:** how does "fail-loud on unresolvable explicit escalation" coexist with HC-055a's
   invariant that harmonik never hard-fails on a model *value* — is fail-loud scoped strictly to *resolution*
   (catalog/namespace author error), leaving *concrete validity* handler-side as today?
4. **C2 degrade target for pi:** the degrade fallback must resolve to a non-empty pi-valid concrete (pi
   hard-fails on empty model) — is the fallback the run-level resolved model, and is that guaranteed non-empty
   for pi?
5. **C3 ownership boundary:** HC owns the coverage *contract*; does `scenario-harness.md` (S07, per HC-038)
   own the harness *mechanics*? Where does the typed input manifest's **emitter** get specified (HC vs EM vs
   event-model), given it must be daemon-emitted to have anti-rot value?
6. **C3 sequencing:** does the leak-oracle clause land now as "known-RED pending renderer," or only after
   Component B lands the per-role source keys?
7. **Adapter-surface budget (HC-013):** confirm C1 + C3 are expressible without adding an adapter callback
   (which would force a foundation amendment).
8. **HC-062 relevance:** does declared node I/O (WG Component A) reuse the `context_keys` registry, pulling
   HC-062 into scope, or is node I/O a separate namespace? (cross-file confirm with WG/EM research.)
