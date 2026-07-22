# 06 — Integration: `agent-input-substrate` (M2)

> Pass 6 (Integration). Cross-reference + contradiction sweep across ALL drafted spec changes and
> the existing system-spec corpus (not just modified files). Performed by an independent
> fresh-context sweep (2026-07-14), then resolved by the parent writer. Every finding F1–F11 below
> is either FIXED in-drafts this pass or carried as an explicit Tasks/deferred item.

## Cross-reference checks performed

Read all six drafts (agent-input NEW; handler-contract, process-lifecycle, session-keeper,
event-model, execution-model modified) + `_registry.yaml` + changelog. Read the referenced existing
specs: event-model (§8.20 keeper precedent, EV-027/EV-050), execution-model (EM-015d-RIA/RFD),
claude-hook-bridge (CHB-028, §11 stream-json), replay-substrate (RS-004…023), workspace-model
(§4.7/WM-026), claude-launchspec (StdinDevNull), handler-pause. Verified every `[file §ID]` link in
the drafts resolves and means what the draft claims; verified terminology consistency of
InputPort / Ack / acked-vs-stale / front-stop / bounded-liveness / `agent_input_*` across all drafts.

## Findings and resolutions

| ID | Class | Detail | Resolution |
|---|---|---|---|
| **F1** | CONTRADICTION | HC-INV-007 declares the watcher the SOLE publisher of every §6.4 event, but `agent_input_acked`/`agent_input_stale` are driver-emitted (HC-070). | **FIXED in handler-contract.md draft:** added an "Input-ack events carve-out (HC-070)" paragraph to HC-INV-007 excluding exactly those two driver-emitted types from the sole-publisher scope (they stay subject to redaction §4.7 and event-model registration; their ordering guarantee is HC-INV-008/AIS-INV-001, not HC-INV-004). |
| **F2** | GAP/CONTRADICTION | EM-015d-RIA/RFD review-loop paste-injects are daemon-run input that cite the demoted paste path; not migrated/carved-out; C6 deletes the stack. | **FIXED — new execution-model.md draft:** EM-015d-RFD step 2 + EM-015d-RIA intro/step 3 migrated to `[agent-input.md AIS-001] SubmitInput→Ack`; PL-021b-PASTE superseded for the daemon-run path; review-loop resume migrates (NOT a carve-out). Step intent/control-flow/ordering unchanged. |
| **F3** | GAP (major, blocking) | Two NEW cross-bus event types deferred to event-model §8/§6.3; no amendment existed; EV-027 mandates registration. | **FIXED — new event-model.md draft:** §8.21 taxonomy rows + §6.3 payload structs + `mustRegister`/`PayloadCompatEntry` + EV-050 cohort-guard carve-out + §8.9(b) evidence, mirroring the §8.20 keeper pattern. Version 0.7.0→0.7.1. |
| **F4** | GAP | agent-input `depends-on` omitted `event-model` and `workspace-model`. | **FIXED:** both added to agent-input.md front-matter `depends-on`. |
| **F5** | GAP | AIS-014 writes capture corpus + CAPTURE-LOG into the WM-owned `…/.harmonik/sessions/${session_id}/` dir without a WM citation/amendment. | **Partly FIXED + TASKS item:** agent-input.md now depends-on workspace-model and AIS-014 relies on WM §4.7's existing session dir (read-only reliance). A WM §4.7 layout amendment enumerating the capture-corpus files is carried as a **Tasks item** (not a blocking spec hole — AIS relies on the existing dir, it doesn't require WM to list every file). |
| **F6** | TERMINOLOGY | AIS cited event-model §8; HC cited §6.3/§6.4 for the same events. | **FIXED:** agent-input.md now cites "[event-model.md §8 (registration + taxonomy) + §6.3 (payloads)]" uniformly; matches the keeper precedent (both sections) and HC's §6.4-index / event-model-registration split. |
| **F7** | TERMINOLOGY | Ack field names diverged: AIS §6.2 `Class`/`Seq`/`Token` vs HC §6.1 `class`/`input_seq`/`acceptance_token`, despite a "verbatim" claim. | **FIXED:** added a note to agent-input.md §6.2 — `Ack` is the in-process Go type (`Class`/`Seq`/`Token`, single owner); the serialized event-payload field names (`class`/`input_seq`/`acceptance_token`) are the event-model §6.3 wire form; one contract, two surfaces. |
| **F8** | BROKEN-XREF | RS-021 mislabeled "type-alias seam-instantiation discipline" (RS-021's subject is codex-stays-green). | **FIXED:** both agent-input.md citations relabeled to "the type-alias re-instantiation clause of RS-021 (codex-scoped in RS-021's body; applied here to the codex (structured-driver) instantiation)." |
| **F9** | BROKEN-XREF | agent-input §9.1 cited "session-keeper §4.9 SK-021"; SK-021 is §4.10. | **FIXED:** corrected to §4.10. |
| **F10** | BROKEN-XREF | "[agent-input.md §AIS]" — AIS is a prefix, not a section anchor (process-lifecycle + execution-model). | **FIXED:** `[agent-input.md §AIS]` → `[agent-input.md]`; prose `§AIS` → `(AIS)`. Zero `§AIS` anchors remain in any draft. |
| **F11** | GAP (defer) | CHB-028 frames agent-task.md task-delivery "under the tmux substrate per PL-021b"; AIS makes PL-021b observation-only. | **DEFERRED to M3/structured-input-driver (Tasks item):** CHB-028's `agent-task.md` ARTIFACT contract survives; only its delivery instruction changes. Reconciling CHB-028's delivery wording with the structured input path is a claude-hook-bridge amendment best done alongside the M3 consumer, not blocking M2. Carried as a Tasks item. |

## Hook-sourced-ack addendum (COORD c021, 2026-07-14 — post-integration)

Applied after this sweep, per `plans/2026-07-13-code-revamp/M2-RESCOPE-hook-sourced-ack.md`:

- **Ack-signal SOURCE made explicit** — the tmux/Claude path's positive ack (AIS-003, HC-070) is the
  Claude-hook-bridge event (`outcome_emitted` on `Stop`, `agent_ready` on `SessionStart` start/resume;
  verified against `internal/hookrelay/hookrelay.go`), NOT a `capture-pane` scrape and NOT a Claude
  wire protocol. `agent-input.md` gains `claude-hook-bridge` in `depends-on` + a normative §9.1
  cross-ref; event-model §8.21 prose and PL-021b/EM-015d input-delivery clauses aligned.
- **`Degraded`/`Accepted` ack-class residue purged from the HC + EM drafts** — `handler-contract.md`
  HC-069/070/HC-INV-008/§6.1/§6.4/§10 and `execution-model.md` EM-015d still carried the retired
  three-valued `{Accepted, Rejected, Degraded}` class; converted to the binary `{Delivered, Rejected}`
  delivery-outcome + async `agent_input_acked`/`agent_input_stale` model that `agent-input.md` already
  used. (The daemon `degraded` status and keeper `degraded?` senses are unrelated and untouched.)
- **New AIS-018 handoff done-gate** (`outcome_emitted` + expected-artifact-present; the
  `buildStopMessage`→`.harmonik/review.json` precedent) added, with the completed-vs-pending-question
  discrimination flagged OPEN (OQ-AIS-006); session-keeper SK-014 gains a cross-ref note; a new Tasks
  item T14 scopes it. This is a NEW deferred consistency item, not a contradiction fix.
- **"Observation-only tmux" scoped to the structured (Codex) path** (AIS-011, PL-021b) — the Claude
  path keeps tmux paste as first-class input; `capture-pane` is a human observation window only.

## Consistency confirmations (no change needed)

- **PL-021d demotion ↔ SK-002 carve-out ↔ AIS-012 ↔ SK-021** tell one coherent story: demoted for the
  daemon-run path, preserved (normative) for keeper + interactive CLI nudge; keeper excluded from the
  C6 deletion boundary; keeper verbs survive.
- **PL-021b READ boundary ↔ AIS-011**: `capture-pane` observation retained, §5 `pipe-pane` prohibition
  unchanged, "inspectability" reinterpreted as tail-the-capture-tee.
- **AIS-INV-001 ↔ HC-INV-008**: same window (`InputAckTimeout` + overhead), same ClockPort, same terminal set.
- **claude-launchspec**: `StdinDevNull` is a handler-contract `SubstrateSpawn` concept, not a CLS concept —
  AIS-010 + HC-069 stdin split are internally consistent; no CLS requirement stranded.
- **handler-pause**: no input/paste/InputPort reference; "does SubmitInput honor handler-pause?" is an
  M3-consumer behavioral question, noted only.

## Changelog / registry accuracy

- Version bumps verified against draft front-matter: HC 0.7.0, PL 0.5.5, SK 0.2.0, event-model 0.7.1,
  execution-model 0.9.2, agent-input 0.1.0 (new), AIS reserved in `_registry.yaml`.
- The HC 0.6.0-skip and the HC-058/059/060→069/070/071 ID-FREEZE are documented.

## Final coherence assessment

The change-set is **internally consistent and safe to advance to Tasks.** The port/ack/liveness
contract is consistent across AIS ↔ HC ↔ SK ↔ PL ↔ event-model ↔ execution-model; the
demotion/carve-out story is clean; the three blocking/contradiction findings (F1 HC-INV-007, F2
EM-015d, F3 event-model registration) are resolved in-drafts this pass; the xref/terminology polish
(F4/F6/F7/F8/F9/F10) is fixed inline; F5 (WM layout enumeration) and F11 (CHB-028 delivery wording)
are carried as explicit Tasks items with a documented rationale for deferral. Affected-spec set grew
from 4 to 6 (event-model + execution-model added) — a decompose under-scope that this pass corrected.

## Items carried to the Tasks pass

- **T-WM (F5):** amend workspace-model §4.7 session-dir layout to enumerate the capture-corpus files + CAPTURE-LOG (non-blocking; AIS relies on the existing dir).
- **T-CHB (F11):** reconcile claude-hook-bridge CHB-028 task-delivery wording with the AIS structured input path (deferred to the M3/structured-input-driver motion).
- The two validation beads (hk-1cjy5 scenario, hk-1r5jt exploratory) become explicit test tasks.
