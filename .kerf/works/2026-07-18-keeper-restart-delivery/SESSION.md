# SESSION — 2026-07-18-keeper-restart-delivery

**Title:** Keeper restart timing & delivery — leader-session comms nudge + self-restart safety net
**Status:** pass 8 "ready" — square-clean; heading to finalize.

## What this work delivers

For leader sessions (captain/admiral) the keeper stops writing the context-fill nudge into the
operator's pane and instead delivers it over comms (K1) when the target is reachable
(presence-Online read in-process; terminal fallback otherwise — never a silent no-op). The
nudge carries a four-element deferral template with a good-stopping-point self-test (K2), whose
default payload is the agent-run `harmonik keeper restart-now --agent <name>` command, made
self-restart-safe by a net-new carry-for-audit `--nonce` flag that closes the T+301s
late-handoff gap (K3). All wording lives in `.harmonik/config.yaml keeper.warn_messages`,
editable on the fly via an mtime-gated per-tick re-read, structure-normative / prose-tunable
(K4). The situational read is sharpened best-effort (in-cycle operator-attached TOCTOU re-check;
reachability pre-check feeds the decision; hook-bridge keystroke signal named external/out of
scope) (K5). Test coverage rides the keeper's session-twin integration tier, not the pane-less
wire twin (K6). Crew keeper-message disposition (K7) is designed but deferred — NOT a research
cluster, not required by square.

## Artifact inventory (passes 1–8)

1. `01-problem-space.md`  2. `02-components.md`  3. `03-research/{delivery-reachability,
message-cluster,testing}/findings.md`  4. `04-design/` (session-keeper, agent-input,
scenario-harness, crew-disposition + cluster bridges delivery-reachability/message-cluster/
testing)  5. `05-spec-drafts/` (session-keeper, agent-input, scenario-harness, park-resume-
protocol amendments + cluster indexes delivery-reachability/message-cluster/testing)
6. `06-integration.md`  7. `07-tasks.md`  8. `05-changelog.md`, `spec.yaml` (status: ready),
`SESSION.md`.

## Guardrails

- **Zero threshold changes (NG1/SC-9):** SK-016, the bands, and the FORCE-ACT / hard-ceiling
  gate ladder are untouched.
- **Retry-Enter loop preserved (NG3):** the terminal fallback keeps the 750ms-settle +
  retry-Enter loop in full; never a naive single-Enter send.
- **restart-now upholds SK-INV-001 (NG5):** no `/clear` without a confirmed fresh handoff; the
  agent-run command enforces the same ordering, it does not bypass it.
- Delivery totality invariant SK-INV-006: every fired leader warn tick resolves to comms or
  terminal fallback.

## Next step

`kerf finalize --branch`, then create 10 beads T1–T10 labeled `codename:keeper-restart-delivery`
(matching `spec.yaml bead_filter`).
