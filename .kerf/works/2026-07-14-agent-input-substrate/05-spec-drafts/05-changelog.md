# 05 — Changelog: `agent-input-substrate` (M2)

> Pass 5 (Spec Draft). Every drafted spec file, its status, what changed, and the change-design
> that motivated it. Drafts map 1:1 to files in `specs/`. The C5 harness (L0–L3 taxonomy + fault
> matrix + N-consecutive gate) is **DoD, not spec prose** (M2-5 card) — it has no draft file; its
> only normative footprint is AIS-INV-001's conformance obligation, drafted in `agent-input.md` §10.

## Drafted files

| Target spec file | Status | Motivating design | Summary of change |
|---|---|---|---|
| `specs/agent-input.md` | **NEW** | `04-design/agent-input-design.md` (+ 00-decisions D0–D10) | New normative spec, prefix **AIS**. 12-section RS/SK layout. **AIS-000** substrate disambiguation; **AIS-001–017** (InputPort + retirements; Ack class/seq/token; dual sync-return+event; front-stop; structured driver over `substrate.Run[E,A]`; corpus-first + spike gate; billing gate; direct-stdio ownership; StdinDevNull split; observation-only-tmux boundary; deletion-boundary + keeper/CLI carve-out; pipes-only best-effort capture tee; persistence/redaction/ledger/retention; substrate-selection axis + twin-blind; remote seam; WAL-guard adapt). Invariants **AIS-INV-001** (bounded liveness, output-or-stale, never silence) + **AIS-INV-002** (capture never aborts the run). §11 carries 5 open questions (OQ-AIS-001…005) holding the PLANNER-RECONCILE items. |
| `specs/handler-contract.md` | modified (0.5.4 → **0.7.0**) | `04-design/handler-contract-design.md` (C1) + integration F1 | New **§4.1a Session input port**: **HC-069** (InputPort verb; retire the six side-interfaces + no-op adapter; StdinDevNull split), **HC-070** (Ack contents + emitted `agent_input_acked`/`agent_input_stale` event; front-stop composition), **HC-071** (machine-enforced handler↛tmux depguard deny). New invariant **HC-INV-008** (bounded input liveness; machine-checked home = AIS-INV-001). **HC-INV-007 carve-out (integration F1):** the two driver-emitted input-ack events are EXCLUDED from the watcher-is-sole-publisher scope (they are driver-emitted per HC-070, not watcher-published). Amended HC-054 (observation-peer line), HC-056/HC-057 (front-stop cross-ref), §6.1 (InputPort/InputRequest/Ack schemas; removed no-op SendInput), §6.4 (index the two driver-emitted events; registration is event-model's), §10.1/§10.2 (conformance + test obligations), §9.3 (AIS co-ref). **ID-FREEZE reconciliation:** the design's provisional HC-058/059/060 were stale (already live through HC-068) — landed as HC-069/070/071. |
| `specs/event-model.md` | modified (0.7.0 → **0.7.1**) | integration F3 (EV-027) | New **§8.21 "Agent-input acceptance events"** registering two cross-bus events: **8.21.1 `agent_input_acked`** (class O; `run_id`/`class`/`input_seq`/`acceptance_token?`/`session_id?`/`acked_at`) and **8.21.2 `agent_input_stale`** (class O; `run_id`/`input_seq`/`session_id?`/`timed_out_at`/`window`), emitter `daemon-core (input driver)`, consumers run-reactor (M3) / replay-invariant-harness / audit / observability. §8.9(b) input-acceptance-boundary compliance evidence + EV-050 cohort-guard carve-out + Section Axes (idempotent-on-input_seq for acked). §6.3 payload structs `core.AgentInputAckedPayload` / `core.AgentInputStalePayload` with `PayloadCompatEntry{…v1}` (N-1 readable per operator-nfr §4.5). Mirrors the §8.20 keeper-interior-events pattern. |
| `specs/execution-model.md` | modified (0.9.0 → **0.9.2**) | integration F2 | **EM-015d review-loop input delivery MIGRATES from tmux paste-inject to the AIS `InputPort`.** EM-015d-RFD step 2 + EM-015d-RIA intro/step 3: the implementer-resume read instruction and reviewer start instruction are delivered via [agent-input.md AIS-001] `SubmitInput`→`Ack` (AIS-003/004; bounded-liveness AIS-INV-001), superseding the "PL-021b-PASTE" paste-inject for the daemon-run path. Review-loop resume is daemon-run input → it MIGRATES (not a carve-out). WHAT is delivered + control flow + iteration semantics + ordering invariants unchanged; spawn (`tmux new-window`) + `capture-pane` observation remain on tmux. |
| `specs/process-lifecycle.md` | modified (0.5.4 → **0.5.5**) | `04-design/process-lifecycle-design.md` (C3, C6) | **PL-021b** gains an "Observation-only after AIS" subclause (tmux carries no daemon-run input; `capture-pane` MAY survive; §5 READ boundary unchanged; inspectability via AIS capture-tee + optional observation pane). **PL-021d DEMOTED** (not deleted): retitled; demotion clause preserves the `load-buffer`/`paste-buffer`/`send-keys` discipline as NORMATIVE for keeper (SK-002) + interactive-session nudge (`cmd/harmonik/{captain,crew,comms}.go`); daemon-run uses now AIS-owned. C6 deletion-boundary note (post-bake, AIS-INV-001-gated; keeper/CLI verbs + spawn verbs + capture-pane retained). §9.3 AIS co-reference. Every write-verb MUST sentence preserved verbatim. |
| `specs/session-keeper.md` | modified (0.1.0 → **0.2.0**) | `04-design/session-keeper-design.md` (C6 / A11) | SK-002 prose gains the PL-021d **carve-out NOTE** (demoted-for-daemon-run-but-preserved-for-keeper; `PanePort.Inject` unchanged; keeper EXCLUDED from the C6 deletion boundary; keeper's tmux verbs MUST survive). New **§4.10 / SK-021** — deferred keeper-input migration to a session-id-keyed leaf-package port or daemon RPC, carrying the normative MUST-precondition gating any future teardown. §9.1 PL-021d "Depends on" clause (demoted-not-deleted survival); §11 deferred-register pointer. SK-002 interface block untouched; no SK renumbering. |
| `specs/_registry.yaml` | modified | D10 | Reserve prefix **AIS** → `{spec-id: agent-input, reserved: 2026-07-14, status: draft}`, landed in the same commit as `specs/agent-input.md` (registry lint rule). |

## Traceability to change designs / success criteria

- **SC1** (real input method + ack) → AIS-001/003/004 + HC-069/070.
- **SC2** (side-interfaces retired) → AIS-001/002 + HC-069/071.
- **SC3** (tmux observation-only) → AIS-011/012 + PL-021b subclause.
- **SC4** (input stack deleted after bake) → C6 teardown; boundary recorded by AIS-012 + PL-021d C6 note + SK-002 carve-out.
- **SC5** (live capture tee) → AIS-013/014.
- **SC6** (L0–L3 + fault harness + N-consecutive + zero sleeps/scraping) → AIS-INV-001 conformance obligation; harness is DoD (`04-design/harness-acceptance-design.md`), not spec prose.
- **SC7** (abort/rollback + bake window) → C6/C5 gate; escape-hatch-survives boundary recorded by AIS-012 + PL-021d C6 note.

## Validation / acceptance test beads (filed this pass)

- **hk-1cjy5** — `scenario: agent-input.md — structured-driver input ack end-to-end (twin)` (label `scenario-test`, `codename:2026-07-14-agent-input-substrate`).
- **hk-1r5jt** — `explore: agent-input.md — operator drives a daemon run on the structured input driver` (label `exploratory-test`, same codename).
- Both are listed as explicit tasks with dependencies in `07-tasks.md`; neither this work nor its implementation beads may close until these are closed.

## Cross-work / reconciliation flags (see 00-decisions + §11 of agent-input.md)

- **PLANNER-RECONCILE (D0):** repo TASKS.md has M2 OWN the input/ack contract (M3-4/M4 consume) — the planner brief stated the reverse. Drafts follow the repo; the dual sync-return+event ack is direction-agnostic.
- **PLANNER-RECONCILE (D4):** claude `--input-format stream-json` bidirectional stdin is unproven in-repo (T0 spike gates codec freeze); billing (subscription vs API credit) unverified for headless mode.
- **PLANNER-RECONCILE (D5):** "tmux inspectability required" reinterpreted as capture-tee-backed observation (not tmux hosting the process) — operator confirmation needed.
- **PLANNER-RECONCILE (D6):** ROADMAP says "keeper migrated"; SK-002 + keeper architecture push carve-out. Drafts do carve-out + narrowed C6 scope; migrate-vs-carve-out is the planner's call.
- **Note:** handler-contract 0.6.0 label already existed in that spec's history; draft uses 0.7.0 to avoid a duplicate version label.
