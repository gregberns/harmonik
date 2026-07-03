# Change-spec delta — execution-model.md (auto_status v2, engine FAIL-only derivation)

**DRAFT for review. NOT applied to `specs/`. Do not commit.**
Codename: auto-status · Epic: hk-cq1 · Date: 2026-06-13.

## Anchor

Target the captain named "§7 (engine-side FAIL-only derivation)". The best real anchor
is **§7.5 — Workflow Mode: `dot` (BINDING DOCUMENT)** (`specs/execution-model.md:1554`),
the home of the `dot`-mode dispatch contract. The C1+C2 derivation runs in exactly that
path (`internal/daemon/dot_cascade.go:1115`, right after the HEAD-advance check and
before `SUCCESS` is finalized). The delta is a new rule **EM-068** inside §7.5 (a new
§7.5.6 sub-part, after §7.5.5). A cross-reference clause is also added to the Outcome
spine **EM-027** (`:589`) so the derivation is named in the integrated-flow spine.

> ⚠ The C1 engine inspection already SHIPPED (hk-oo4, 5c5b15ef,
> `runAutoStatusInspection`, dot_cascade.go:1138) with ZERO spec coverage. EM-068 is
> both the v2 spec AND the catch-up for the shipped v1 FAIL-axis. Flag for redline.

## Proposed rule EM-068 (new) — §7.5.6, after §7.5.5

#### EM-068 — `auto_status` deny-side engine derivation (`dot` mode, FAIL-only)

When an implementer-class `agentic` node carries `auto_status` per
[workflow-graph.md §4 WG-053], the daemon MUST run a **deterministic, daemon-
authoritative deny-side outcome gate** in the `dot` dispatch path at the
post-implementer-exit / pre-`SUCCESS`-finalization point — AFTER the HEAD-advance check
of §7.5 and BEFORE the node's `SUCCESS` Outcome is returned to the cascade of §4.10
EM-041. The gate evaluates two carriers:

1. **C1 — work-product inspection (primary, daemon-authoritative).** The daemon
   inspects deterministic work-product signals it already computes against the
   implementer's worktree (at v1: `go build ./...` and `go vet ./...`, gated on a
   `go.mod` being present; a non-Go worktree passes through). A non-zero result yields
   `Outcome{status = FAIL, failure_class = deterministic}` per §4.1 EM-005 / EM-005c.
2. **C2 — handler-supplied marker (validated input).** When present and surviving the
   daemon's validation per [handler-contract.md §4.2a HC-068], a
   `${workspace_path}/.harmonik/auto_status.json` marker contributes a deny-side
   `FAIL`+`failure_class`. The marker is a daemon-VALIDATED INPUT, not an authoritative
   self-report: the daemon retains classification authority per §4.1 EM-005c; a
   handler-emitted `failure_class` is a HINT overridable per [handler-contract.md
   §4.2a HC-059].

**Deny-side only (D1).** The gate MAY emit `FAIL`+`failure_class` OR leave the
pre-existing `SUCCESS` derivation untouched. It MUST NOT emit `APPROVE`, `BLOCK`, or
any verdict, and it MUST NOT mark `SUCCESS` as authoritative or bypass any downstream
reviewer — the LLM reviewer remains the sole authority for `APPROVE`/`BLOCK` per the
review-loop lifecycle of §4.3 EM-015d.

**No reviewer-loop re-entry (D4).** A derived `FAIL` is **terminal** for the node: it
routes through the cascade exactly as any other `FAIL` Outcome (failure-class-
conditional edges per [workflow-graph.md §7 WG-018]) and does NOT re-enter the review
loop and does NOT bounce to `REQUEST_CHANGES`. `REQUEST_CHANGES`-via-policy re-entry is
DEFERRED to a future version paired with the `auto_status="<policy-name>"` value-shape
of WG-053; v1 derives `FAIL`+`failure_class` ONLY.

**No new Outcome enum value.** The derivation reuses the existing
`FAIL`+`failure_class` shape of §4.1 EM-005 / EM-005c; no §4.1 status enum or
`FailureClass` enum member is added.

**AR-006 mechanism tag (LLM-free).** This is a mechanism-tagged evaluation point per
§4.9 EM-039: the entire C1+C2 derivation MUST be deterministic — exit-code evaluation,
schema-validated artifact parsing, and git-state comparison ONLY, with ZERO LLM calls.
Any future signal added to the gate MUST remain deterministic and LLM-free
(consistent with the AR-006 sensor `internal/specaudit/ar006_mechanism_no_llm_test.go`).

**Default unchanged.** A node without `auto_status` is identical to prior behavior; the
gate does not run. `auto_status` is orthogonal to `non_committing` (§7.5 / WG-041): the
gate runs after the (relaxed-or-strict) HEAD-advance check, so a `non_committing`
`auto_status` node is gated on its work product even when HEAD did not advance.

D-mapping: D1 (deny-side only; never auto-APPROVE/BLOCK; reviewer stays sole APPROVE
authority), D2 (C1 retained + C2 marker read & validated), D3 (C2 is a validated input;
daemon retains authority per EM-005c/HC-059), D4 (FAIL-only; no reviewer-loop re-entry;
no REQUEST_CHANGES in v1), forward-compat (policy-name value-shape reserved).

Tags: mechanism, normative

## Amend EM-027 (Outcome spine, line 589) — additive sentence

Append to EM-027:

> When an `agentic` node carries `auto_status` per [workflow-graph.md §4 WG-053], a
> deterministic deny-side derivation gate (§7.5 EM-068) sits in this spine at the
> post-handler-outcome / pre-cascade point: it MAY substitute a `FAIL`+`failure_class`
> Outcome for the node's `SUCCESS`, consuming the handler's typed output and producing
> the cascade's typed input without bypassing any spine segment.

D-mapping: D1/D2/D4 (locates the gate in the integrated outcome flow).
