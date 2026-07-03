# Change-spec delta — handler-contract.md (auto_status v2, C2 marker)

**DRAFT for review. NOT applied to `specs/`. Do not commit.**
Codename: auto-status · Epic: hk-cq1 · Date: 2026-06-13.

## Anchor

Target the captain named "§I (C2 status-derivation protocol)". handler-contract.md has
**no section §I** — its sections are §4.1–§4.13, §5, §6, §7. The best real anchor is
**§4.2a Outcome surface (handler-facing cross-check)** (`specs/handler-contract.md:251`),
home of HC-058/059/060/061. The C2 marker is a new rule **HC-068** appended to §4.2a,
immediately after HC-061.

> ⚠ The C2 artifact `.harmonik/auto_status.json` mirrors `.harmonik/review.json`,
> whose on-disk-bus spec home is **workspace-model.md §4.7 (WM-027a family)** + the
> §6.2 artifact-inventory table — NOT handler-contract.md. HC-068 declares the
> handler-emission discipline and cross-references workspace-model for the canonical
> path/lifecycle/gitignore. A faithful apply also adds a companion
> `.harmonik/auto_status.json` row to workspace-model.md §6.2 and a lifecycle clause
> mirroring WM-027a. Flag for redline (second file touched).

## Proposed rule HC-068 (new) — append to §4.2a after HC-061

#### HC-068 — `.harmonik/auto_status.json` is a daemon-validated INPUT, not an authoritative self-report

An implementer-class `agentic` node carrying `auto_status` (per
[workflow-graph.md §4 WG-053]) MAY write a post-run status-derivation marker artifact
to the canonical path `${workspace_path}/.harmonik/auto_status.json` inside its
worktree. The artifact is OPTIONAL: when absent, the engine derives the node outcome
from its own work-product inspection (C1) per [execution-model.md §7.5 EM-068]; the
artifact NEVER replaces C1, it supplies an additional deny-side signal C1 cannot
natively compute (custom test runners, per-language results).

**The marker is a daemon-validated INPUT, not an authority.** This requirement mirrors
the reviewer-verdict bus of [workspace-model.md §4.7] (`.harmonik/review.json`, read by
`workspace.ReadReviewVerdict`): the daemon reads the file after the implementer's
`agent_completed` event (per §4.3) and before finalizing the node outcome, and
validates it against a deterministic schema+policy. The daemon retains classification
authority per [execution-model.md §4.1 EM-005c] and §4.2a HC-059: a handler-emitted
`failure_class` in the marker is a HINT only; the daemon's classification is
authoritative on disagreement and the disagreement is logged via the HC-059
`failure_class_disagreement` event.

**Schema (v1).** The artifact MUST conform to a schema_version-tagged JSON object:
`{"schema_version": 1, "status": "FAIL", "failure_class": <one of §8's six classes>,
"notes": <freeform string, optional>, "signals": <optional structured object, opaque
to v1>}`. At v1 the ONLY accepted `status` value is `"FAIL"` (deny-side only): a marker
declaring `"SUCCESS"`, `"APPROVE"`, `"BLOCK"`, `"REQUEST_CHANGES"`, or any non-`FAIL`
value MUST be rejected as malformed and ignored (the daemon proceeds as if the marker
were absent and logs the rejection). This enforces D1 at the artifact layer — the
marker can only DENY; it can never confirm SUCCESS or emit a verdict. The `status`
domain is reserved to widen (e.g. to `"REQUEST_CHANGES"`) in a future schema version
paired with the WG-053 `auto_status="<policy-name>"` value-shape; widening is additive.

**Validation discipline (normative).** Before acting on the marker the daemon MUST:
(1) parse the JSON and reject (ignore) on parse failure; (2) reject a marker whose
`status != "FAIL"`; (3) reject a marker whose `failure_class` is not one of §8's six
classes; (4) override a handler-emitted `failure_class = compilation_loop` to
`structural` per HC-059 (daemon-only class). A marker surviving validation contributes
a deny-side `FAIL`+`failure_class` to the engine-side derivation of
[execution-model.md §7.5 EM-068]; a rejected marker is logged and the engine falls
through to C1-only derivation.

**Lifecycle and hygiene.** `.harmonik/auto_status.json` is workflow-control state, not
work product; it MUST be excluded from checkpoint commits via the same `.gitignore`
hygiene set as `.harmonik/review.json` ([workspace-model.md §4.7 WM-013e]). Mid-loop
archival is NOT required at v1 (FAIL is terminal — there is no second iteration to
archive across), distinguishing it from `.harmonik/review.iter-<N>.json`.

**C3 deferred.** A progress-stream `status_marker` NDJSON message (C3) that would let a
handler influence routing *during* the run is explicitly OUT OF SCOPE for v1; the v1
carrier surface is C1 (engine inspection) plus this C2 post-run artifact only.

D-mapping: D2 (adds C2 = `.harmonik/auto_status.json`, mirroring ReadReviewVerdict /
review.json; keeps C1; defers C3), D3 (daemon-VALIDATED input, not authoritative
self-report; cross-refs EM-005c + HC-059), D1 (FAIL-only marker domain; never
SUCCESS/APPROVE/BLOCK), forward-compat (status domain reserved to widen with the
policy-name value-shape).

Tags: mechanism, normative

## Companion edit — workspace-model.md §6.2 artifact inventory

Add a row mirroring the `.harmonik/review.json` row (line ~922):

> | `${workspace_path}/.harmonik/auto_status.json` | implementer agent writes
> (optional); daemon reads + validates per [handler-contract.md §4.2a HC-068] |
> Optional deny-side status-derivation marker for an `auto_status` node; `status`
> domain is `{"FAIL"}` at v1; daemon-validated INPUT (not authoritative) per EM-005c /
> HC-059; excluded from checkpoint commits via WM-013e. |

D-mapping: D2/D3 (the on-disk bus home).
