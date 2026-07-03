# Integration Review — `pilot` (Pi-driven dispatch & control plane)

Cross-spec coherence pass over the four drafted spec changes (`05-spec-drafts/{execution-model,cognition-loop,operator-nfr,queue-model}.md`) against each other AND against the full unchanged system-spec corpus under `specs/`. This pass also records the resolution of the finalize critical-review must-fix items (EM-067 coherence; the design notes' deferred-to-Integration items).

## Scope of the change bundle (recap)

| Spec | New IDs | Nature |
|---|---|---|
| `execution-model.md` | EM-066, EM-067 | quiet-daemon topology + operator-pause fallback binding/gate (§4.11, §7.4 pseudocode, §10.1/§10.2) |
| `operator-nfr.md` | ON-056, ON-057 | agent-callable pause/resume verb + production `operator_pause_status` producer (§4.3, §7.1 table) |
| `cognition-loop.md` | none (CL-051/071/080/030 amended in place + §7 scenarios) | two-phase done, Pi dispatch surface, verb confirmation, recycle digest |
| `queue-model.md` | none (informative annotations) | stream-for-curation note (§2.4), QM-054 producer note (§8.5), submit-as-start note (§8.1) |

All four are amendments to existing specs (A1–A4 per 02-components.md). No new spec files; no requirement IDs renumbered or retired in any spec.

---

## Part 1 — Must-fix resolutions from the finalize critical review

### EM-067 coherence resolution (must-fix #1) — RESOLVED

**The flaw as raised.** The §7.4 main-loop pseudocode keeps a loop-top gate `IF should_pause_between_runs(): wait_for_resume(); CONTINUE` ABOVE the `queue IS None` branch. EM-067 then adds a SECOND operator-pause gate INSIDE that branch (`IF operator_pause_state() IN {pausing,paused}: idle_wait(); CONTINUE`). The reviewer observed that `should_pause_between_runs()` is annotated `[operator-nfr.md §4.3] pause-between-runs` — and §4.3.ON-008 (the between-task drain gate) is precisely what drives the daemon to operator-control `pausing`/`paused`. Therefore, once the ON-056/ON-057 producer lands and makes the loop-top check effective for operator-pause, the loop would `CONTINUE` at the top BEFORE control ever reached the `queue IS None` branch — making EM-067's inline gate unreachable/dead. The draft was internally ambiguous about whether EM-067 was load-bearing or dead.

**Evidence inspected.**
- `specs/execution-model.md:1391` — the loop-top check `IF should_pause_between_runs(): -- [operator-nfr.md §4.3] pause-between-runs` is the EXISTING, unchanged hook; its annotation points at ON §4.3.
- `specs/operator-nfr.md:211` (ON-008) — the between-task invariant: a `pause` transitions `running → pausing`, allows in-flight runs to reach their next durable checkpoint, runs the ON-027 drain, and gates `pausing → paused` on drain-completion. This is the operator-control pause; the loop-top hook IS its dispatch-side enforcement.
- `specs/operator-nfr.md:239` (ON-011) — operator-control states `running/pausing/paused/resuming/stopped/upgrading`.

**Conclusion: resolution (b).** `should_pause_between_runs()` is NOT narrower than operator-control `pausing`/`paused`; it is exactly the ON-008 operator-control pause hook. So the loop-top check already covers operator-pause on ALL dispatch paths, including the `br ready` fallback. EM-067's inline branch gate is therefore NOT the load-bearing primary protection; it is a belt-and-suspenders assertion.

**What EM-067 normatively contributes (genuinely load-bearing), so it is not dead text:** the **binding of the fallback path's pause behavior to the single source of pause truth** (ON-056/ON-057 `operator_pause_status` — the same signal that drives the queue-level QM-054 transition). Without EM-067, nothing in the spec corpus states that the fallback dispatch branch and the active-queue dispatch branch (gated on `Queue.status` by §7.4) must honor ONE pause concept rather than two. EM-067 names which pause state the fallback path is bound to and asserts there is no parallel pause-truth source. The inline branch gate is retained as a defense-in-depth re-assert (vacuous in a conforming implementation where the loop-top check covers the full operator-control state; non-vacuous only if an implementation scoped `should_pause_between_runs()` to handler-pause-only and bypassed the loop-top operator-pause gate).

**Edits applied (this pass):**
- `05-spec-drafts/execution-model.md` EM-067 (§4.11) — retitled "Operator-pause binding and defense-in-depth gate on the `br ready` fallback path"; rewritten into three normative blocks: (1) relationship-to-loop-top clarification naming the loop-top ON-008 check as the PRIMARY gate; (2) load-bearing single-source-of-pause-truth binding; (3) inline branch gate framed as defense-in-depth, with the conditions under which it is reachable made explicit.
- `05-spec-drafts/execution-model.md` §7.4 pseudocode (line ~1391, ~1418-1420) — comments updated: loop-top check labeled PRIMARY operator-pause gate covering all paths; inline branch labeled defense-in-depth re-assert, with a comment noting it is reachable under operator pause only if `should_pause_between_runs()` was scoped narrower than ON-008, vacuous otherwise.
- `05-spec-drafts/execution-model.md` §10.2 test obligation — reframed the pause-gate test as an OBSERVABLE-outcome conformance test (no `run_started` while paused, regardless of which gate enforces it) and added a single-source-of-truth test (the fallback gate observes the same `operator_pause_status` value as QM-054).
- `05-spec-drafts/execution-model.md` front-matter changelog (v0.8.1 row) and `05-changelog.md` EM-067 entry — updated to the reframed semantics.

**Cross-spec consistency after the edit.** `operator-nfr.md` ON-057 (draft line 307) already describes the producer as observed by "the execution-model daemon dispatch path, including the optional `br ready` fallback path, which MUST NOT dispatch while...paused per EM-067" — it does NOT claim the fallback inline gate is the primary gate, so it is consistent with the reframing with no edit required. `queue-model.md` §8.5 QM-054 INFORMATIVE note (draft line 710) names the same `operator_pause_status` as "the single source of pause truth observed by both this queue transition and the execution-model br-ready fallback gate (EM-067)" — consistent.

### Design-notes deferred-to-Integration items — RESOLVED

The four design docs (`04-design/*`) explicitly deferred three coherence questions to this pass. Each is resolved below.

**(i) CL-051's `[reconciliation/spec.md §4.4]` Tier-2-routing resolution.** CL-051 Condition-2-only window (a `Refs:` trailer present on `origin/main` with NO terminal event) emits `loop_observed_phantom_done{bead_id}` and routes to "Tier-2 reconciliation ([reconciliation/spec.md §4.4]); MUST NOT act directly" (cognition-loop draft line 123).
- `specs/reconciliation/spec.md:452` — §4.4 is the **Investigator-agent contract** (RC-015, RC-015a, RC-016, the SnapshotToken-bounded investigator). This is the investigator-required-category path (Cat 2 / Cat 3 generic per §8) — the agent-investigation route, NOT a deterministic auto-resolver.
- A trailer-present-but-no-terminal-event divergence is a git-vs-events store divergence (`store_divergence_detected` family per §4.3 / RC-019a corroboration). Store-divergence cases that are not one of the deterministic auto-resolver categories route to an investigator per §4.4. **§4.4 is therefore the correct anchor.**
- **Terminology bridge (recorded):** "Tier-2 reconciliation" is harness-internal cognition-loop terminology for "route to the investigator-required-category path of reconciliation" — it is NOT a literal term in `reconciliation/spec.md` (which uses "investigator-required category" / "Cat 2 / Cat 3 generic"). The cross-reference is semantically valid; the §4.4 target (investigator-agent contract) is where that routing lands. No edit to CL-051 required; the cognition-loop spec already correctly scopes the loop to "route...MUST NOT act directly" (CL-050 forbids the loop taking reconciliation locks or writing trailers), so the loop's only obligation is to emit the warning and let the daemon's reconciliation path pick it up — consistent with the daemon owning §4.4.

**(ii) EM-062..065 line-range re-verification.** The cognition-loop draft cites the daemon-side mirror at `[execution-model.md §4.13 EM-062/EM-063]` and `[execution-model.md §4.14 EM-064/EM-065]` (draft lines 157, 160).
- Draft `execution-model.md`: EM-062/EM-063 live in §4.13 (draft lines 968, 993); EM-064/EM-065 live in §4.14 (draft lines 1013, 1027). Current `specs/execution-model.md`: same section homes (lines 945, 970, 990, 1004).
- The pilot edits inserted EM-066/EM-067 into §4.11 (before §4.13/§4.14), shifting absolute LINE numbers down ~23 lines, but the §4.13/§4.14 SECTION numbers and the EM-062..065 requirement IDs are unchanged. **Because every cross-reference is by section-number + requirement-ID (never by line number), all EM-062..065 cross-references remain valid.** No edit required.

**(iii) ON-013 `drain_summary?` EV cross-spec coordination.** ON-013 (operator-nfr draft line 259) states the `status=paused` emission MUST include a `drain_summary` field and records a "Cross-spec coordination request to EV: extend §8.7.6 payload to carry `drain_summary?` as an optional field." ON-057's conformance clause (d) (draft line 312) depends on this field ("`operator_pause_status{status=paused}` carrying `drain_summary`").
- This is an unchanged, pre-existing ON-013 obligation (the pilot bundle adds ON-056/ON-057 which CONSUME it; they do not introduce it). The coordination request to EV (event-model.md §8.7.6) is already recorded in ON-013 and is carried unchanged by this bundle.
- **Resolution:** the request stands as a tracked cross-spec coordination item owned by operator-nfr → event-model; the pilot bundle adds no new EV-payload obligation beyond re-using `drain_summary?`. Recorded here so the EV §8.7.6 extension is not lost. No new edit required in the pilot drafts; it is surfaced as a coordination note (not a pilot task) below.

---

## Part 2 — Cross-reference checks performed

Every `[spec.md §x ID]` link in the four drafts was resolved against the target spec. Outbound links checked (representative; all verified present):

**execution-model.md draft →**
- `[operator-nfr.md §4.3 ON-011]` → present (`operator-nfr.md:239`). VALID.
- `[operator-nfr.md §4.3 ON-008]` (loop-top primary gate) → present (`operator-nfr.md:211`). VALID.
- `[operator-nfr.md §4.3 ON-056/ON-057]` → present (operator-nfr draft 292/303). VALID.
- `[operator-nfr.md §7.1]` (resuming transition) → present (operator-nfr draft §7.1 table, lines 798/801). VALID.
- `[queue-model.md §8.5 QM-054]` → present (queue-model draft 699). VALID.
- `[queue-model.md §3 QM-002]` → present. VALID.
- `[process-lifecycle.md §4.1]` (`--no-auto-pull` flag surface) / `[§4.9]` (supervised topology launch) → present in `specs/process-lifecycle.md`. VALID.
- EM-066/EM-067 self-references to §4.11, §7.4, §10.1, §10.2 → all present in the draft. VALID.

**operator-nfr.md draft →**
- `[cognition-loop.md §4.10 CL-080]` (agent-callable loop) → present (cognition-loop draft 170, CL-080). VALID.
- `[execution-model.md §7.4 EM-067]` → present (draft §4.11 EM-067; the §7.4 pseudocode references EM-067; ON-057 cites the EM-067 ID). VALID — EM-067 is reachable as both a §4.11 requirement and a §7.4 pseudocode obligation.
- `[queue-model.md §8.5 QM-054]` → present. VALID.
- `[process-lifecycle.md §4.1 PL-003a]` (JSON-RPC transport) → present in `specs/process-lifecycle.md`. VALID.
- `[event-model.md §8.7.6]` (operator_pause_status payload), `[event-model.md §6.3]` (structured fields) → present in `specs/event-model.md`. VALID. (The `drain_summary?` extension is a coordination request, not yet a landed EV field — see Part 1 (iii).)
- ON-056/ON-057 self-references to ON-008/ON-010/ON-011/ON-013/ON-013a/ON-013c/ON-027/ON-030a → all present. VALID.

**cognition-loop.md draft →**
- `[reconciliation/spec.md §4.4]` → present (`reconciliation/spec.md:452`, Investigator-agent contract). VALID (see Part 1 (i)).
- `[execution-model.md §4.12 EM-052/EM-053]` (terminal-multi-step window) → present in `specs/execution-model.md` §4.12. VALID. (Corrected this pass from a prior §4.4 mis-cite per the CL-051 amendment changelog.)
- `[execution-model.md §4.13 EM-062/EM-063]`, `[§4.14 EM-064/EM-065]` → present (see Part 1 (ii)). VALID.
- `[queue-model.md §2.4, §7 QM-040]` (stream-only append target) → present (queue-model draft §2.4 line 98; §7.1 QM-040 line 614). VALID.
- `[queue-model.md §8.1 QM-050]` (submit-as-start) → **FIXED this pass.** Was `[queue-model.md §9]`, which is "Concurrency" (QM-060+), the WRONG target; the submit-as-start confirmation lives at §8.1 QM-050. Corrected to `[queue-model.md §8.1 QM-050]`.
- `[operator-nfr.md §4.3 ON-056/ON-057]` (CL-080 producer/agent-callable) → present. VALID.

**queue-model.md draft →**
- `[operator-nfr.md §4.3 ON-056/ON-057]` (QM-054 producer note) → present. VALID.
- `[operator-nfr.md §4.7 ON-027]` (drain ordering) → present in `specs/operator-nfr.md`. VALID.
- `[execution-model.md §7.4 EM-067]` (single-source-of-truth note) → present. VALID.
- `[execution-model.md §4.13 EM-062]`, `[§4.11 EM-NOTE-STREAM-CONCURRENCY / EM-NOTE-WAKE]` (§2.4 stream note) → present. VALID.
- `[handler-pause.md §6 HP-025]` (QM-052a orthogonality) → present in `specs/handler-pause.md`. VALID.

## Part 3 — Contradictions checked (changed AND unchanged specs)

1. **Operator-pause vs. handler-pause (two distinct pause concepts).** `queue-model.md` §8.3a QM-052a (draft 680) makes handler-pause ORTHOGONAL to queue-level pause: handler-pause does NOT transition `Queue.status` and manifests only as a submission gate + per-item eligibility hold. EM-067 gates the `br ready` fallback on OPERATOR-pause (ON-056/057), which DOES drive `active → paused-by-drain` (QM-054). The assessment doc (S9) notes the pre-existing fallback path "has no operator-pause gate (only handler-pause)." **No contradiction:** EM-067 adds the operator-pause gate ALONGSIDE the existing handler-pause gate on the fallback path; the two are orthogonal (handler-pause = per-agent-type eligibility; operator-pause = global drain). Recorded so future readers don't conflate them.

2. **`should_pause_between_runs()` (loop-top) vs. EM-067 inline gate.** Resolved in Part 1 (must-fix #1): no contradiction; loop-top is the primary gate, EM-067 is the binding + belt-and-suspenders re-assert.

3. **"No new control surface" (ON-INV-006) vs. ON-056 adding `pause`/`resume` verbs.** `operator-nfr.md` already owns the operator-control commands; ON-056 adds no NEW control concept — it exposes the EXISTING §7.1 `running → pausing → paused` transitions over the EXISTING PL-003a transport, agent-callable. `subscribe` carries an explicit ON-INV-006 carve-out (operator-nfr:575) as a non-mutating surface; `pause`/`resume` are mutating but are the canonical operator-control verbs the state machine already defines, so they are not a "new" surface. **No contradiction.** (ON-056 draft 294 explicitly frames the verbs as the entry point to existing transitions, "adds the command entry point and the agent-callable obligation only.")

4. **`harmonik run --beads` wave default vs. Pi-curated stream dispatch.** `queue-model.md` §2.4 note (draft 101) and CL-071 (draft 155-156) both state the curation path uses a `stream` group and that the `harmonik run --beads` `wave` default MUST NOT be changed to obtain appendability. **No contradiction:** the two entry points coexist (wave = closed batch; stream = incremental curation). Consistent with the assessment doc S10.

5. **Submit-as-start vs. a separate "start" verb.** `queue-model.md` §8.1 QM-050 note (draft 818), CL-071 (draft 156), and the assessment doc S6 all agree: `queue-submit` returning `status: active` IS the start semantics; there is no separate start method. **Consistent across all three.**

6. **Two-phase done (CL-051) vs. daemon terminal-multi-step window (EM-052/EM-053).** CL-051 Condition-1-only (event without trailer on `origin/main`) is explicitly mapped to the daemon's terminal-multi-step window (push-after-merge may have failed); the loop treats it as in-flight and re-polls. **No contradiction:** the loop is a second consumer (CL-050) and does not own run-state; it defers to the daemon's window. Consistent.

No contradictions remain unresolved.

## Part 4 — Terminology consistency

- **`operator_pause_status`** — used identically across operator-nfr (producer, ON-013/ON-057), queue-model (consumer, QM-054), execution-model (consumer, EM-067). Single event type, `status ∈ {pausing, paused}`, `pause_reason ∈ {operator, improvement}`. Consistent; the drafts uniformly note `operator_pausing`/`operator_paused` do NOT exist as separate Go EventTypes.
- **"single source of pause truth"** — the exact same phrase is used in operator-nfr ON-057, queue-model QM-054 note, and execution-model EM-067, all naming the SAME `operator_pause_status` value. Consistent.
- **"Tier-2 reconciliation"** — cognition-loop-internal term; bridged to reconciliation/spec.md's "investigator-required category" / §4.4 investigator-agent contract (Part 1 (i)). Bridge recorded.
- **`stream` / `wave` GroupKind** — used consistently across queue-model (§2.4), cognition-loop (CL-071), execution-model (EM-NOTE-STREAM-CONCURRENCY). Consistent.
- **`harmonik supervise pause/resume`** — the single canonical verb form across cognition-loop CL-080 (draft 170), operator-nfr ON-056 (draft 294); bare `pause`/`resume` are the RPC `CommandName` wire values only. Consistent.
- **"task" vs `run`** — the drafts respect the operator-nfr §4.4 rule (human surfaces say "task", specs/wire say `run`). No drift introduced.

## Part 5 — Changelog verification

`05-changelog.md` checked row-by-row against the actual drafts:
- operator-nfr ON-056/ON-057 + §7.1 table annotation + conformance obligation → matches drafts (lines 292-312, 798/801). ✓
- execution-model EM-066/EM-067 + §7.4 reconcile + §10.1/§10.2 → matches drafts; the EM-067 row was UPDATED this pass to the reframed semantics (binding + defense-in-depth, not a new primary gate). ✓
- cognition-loop CL-051/071/080/030 in-place amendments + §7 scenarios 6/7 → matches drafts. ✓ (The §9→§8.1 QM-050 cross-ref fix in CL-071 does not change the changelog's description, which already says "submit-as-start, no separate start verb.")
- queue-model §2.4/§8.5/§8.1 informative notes, no new IDs → matches drafts. ✓
- Net-new-IDs table (ON-056, ON-057, EM-066, EM-067; none renumbered/retired) → matches. ✓
- The cross-reference-integrity and bead-coverage tables in the changelog → consistent with this pass's findings.

The changelog is complete and accurate against the drafts (with the EM-067 entry now reflecting the reframed semantics applied this pass).

## Part 6 — Final assessment

The four-spec bundle is internally coherent and consistent with the unchanged system-spec corpus. The one substantive coherence flaw flagged by the finalize critical review (EM-067's load-bearing-vs-dead ambiguity) is resolved by reframing EM-067 as (a) the normative single-source-of-pause-truth binding for the fallback path and (b) a defense-in-depth re-assert beneath the primary loop-top ON-008 gate — with §7.4 pseudocode, §10.2 tests, and both changelogs updated to match. The producer→consumer→gate→obligation chain (ON-056/057 → QM-054 + EM-067 + CL-080) is bidirectionally consistent: one `operator_pause_status` producer, two consumers (queue transition, fallback gate), one agent-callable obligation. Cross-references resolve in both directions after fixing one drifted link (CL-071's `§9` → `§8.1 QM-050`). No requirement IDs were renumbered or retired; every change is additive. Two cross-spec coordination items remain tracked but out-of-bundle: the EV §8.7.6 `drain_summary?` extension (owned by operator-nfr → event-model, pre-existing ON-013 request) and the EV event additions already requested by reconciliation/spec.md — neither blocks this bundle. Overall spec coherence: **APPROVED for advance to the Tasks pass.**

## Integration-review verdict

Recorded per jig-system.md §Review Pattern. Findings applied in a single round (the finalize must-fix items were the review inputs; all were resolvable by spec-text edits + this integration record). No contradictions remain. Cross-references valid in both directions. Terminology consistent. Changelog matches drafts. **Verdict: APPROVE — advance to `tasks`.**
