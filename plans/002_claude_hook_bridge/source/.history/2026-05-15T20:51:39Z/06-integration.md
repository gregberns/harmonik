# Pass 6 — Integration check

## Scope

Cross-references and terminology checked across the full spec corpus:
- architecture, execution-model, event-model, handler-contract, control-points (reviewed batch 1)
- workspace-model, process-lifecycle, operator-nfr, reconciliation, beads-integration, scenario-harness (reviewed batch 2)

## Cross-reference checks

### From claude-hook-bridge.md outbound

- `[handler-contract.md §4.2 HC-007, HC-007a, HC-009]` — verified: HC-007 (progress stream over Unix socket), HC-007a (NDJSON framing), HC-009 (handler_capabilities). All exist.
- `[handler-contract.md §4.2 HC-006]` — verified: declares LaunchSpec.claude_session_id field.
- `[handler-contract.md §4.3 HC-016a]` — verified: declares daemon_not_ready retry semantics.
- `[handler-contract.md §4.6 HC-024, HC-026, HC-026a]` — verified.
- `[handler-contract.md §4.10 HC-042, HC-044]` — verified.
- `[handler-contract.md §5 HC-INV-002, HC-INV-004, HC-INV-006, HC-INV-007]` — verified.
- `[handler-contract.md §4.10 HC-045a / HC-045b / HC-045c]` — NEW (added by this kerf as gap-fillers after HC-045, since HC-053 in §6.2 is already occupied); cite forward.
- `[workspace-model.md §4.1 WM-003, §4.3 WM-013e, §4.4 WM-016, §4.7 WM-026, WM-027a]` — verified.
- `[event-model.md §8.1a]` — verified.
- `[process-lifecycle.md §4.1, §4.2 PL-003a, PL-003b, §4.5 PL-014]` — verified.
- `[architecture.md §4.2]` — verified.

### Inbound to claude-hook-bridge.md

- From `handler-contract.md` new HC-045a / HC-045b / HC-045c — cite forward to bridge.
- From `workspace-model.md` new WM-040a — cite forward to bridge (renamed from WM-038 to avoid collision with existing WM-038 interrupt-state writer requirement).
- From `process-lifecycle.md` new PL-017a — cite forward to bridge.
- From `execution-model.md` amended EM-015d — cite forward to bridge (CHB-018, CHB-023) and to HC-045c.

All cite directions are consistent: bridge depends on (cites) HC/WM/EV/PL; the others reference bridge via cite-forward "see also" rather than as a hard dependency.

### From handler-contract.md amendment

- New HC-045a cites `[claude-hook-bridge.md]` — bridge exists in this changeset.
- New HC-045b cites `[HC-007, HC-007a]` for re-use of framing rules; both already in HC.
- New HC-045c cites `[§4.2 HC-006, §4.2 HC-009, [claude-hook-bridge.md §4.2 CHB-007], §4.3 HC-016a, [execution-model.md §4.7 EM-031]]` — all valid.

### From workspace-model.md amendment

- New WM-040a cites `[execution-model.md §4.3]` for agent_type lookup; valid.
- New WM-040a cites `[claude-hook-bridge.md §4.1]` for content ownership; valid.
- New WM-040a cites `[claude-hook-bridge.md §4.1 CHB-004]` for merge rule; valid.

### From process-lifecycle.md amendment

- New PL-017a cites `[claude-hook-bridge.md]`, `[handler-contract.md §4.10 HC-045b]`, `[PL-014, PL-014a, PL-006, HC-007]` — all valid.

### From execution-model.md amendment

- Amended EM-015d cites `[claude-hook-bridge.md §4.7 CHB-018]`, `[claude-hook-bridge.md §4.6 CHB-023]`, `[handler-contract.md §4.10 HC-045c]`, `[§4.5 EM-023a]` — all valid.

## Terminology consistency

- **claude_session_id** — defined in event-model.md §3 glossary (added in v0.4.0 workflow-modes kerf); used consistently across HC-006, HC-045c, CHB, WM-040a, EM-015d. Verified single definition, multiple consistent uses.
- **hook-relay subprocess** — newly defined in CHB §3 glossary. Cross-referenced in HC-045b, PL-017a, agent-runner.md, hook-system.md.
- **handler subprocess** vs **handler-process** — CHB uses "harmonik handler-process" to distinguish the long-lived OS process from "handler subprocess" (Claude itself in CHB's vocabulary). This is potentially confusing; **adjust CHB §3 glossary to use "handler subprocess" consistently with HC vocabulary**, and rename "harmonik handler-process" → "harmonik handler subprocess" everywhere in CHB.
  - **RESOLVED**: defer wording adjustment to spec-draft-review. Marked as MINOR finding.
- **two-contributor model** — CHB-specific informative term; appears once in CHB §3 glossary, used in CHB-INV-001. No conflict with corpus terminology.
- **bridge** — CHB-specific. Doc-amendments cite "the bridge"; consistent.

## Contradictions found

### C1. HC-007 phrase "the sole bidirectional channel"

HC-007 says: *"The progress stream is the sole bidirectional channel between the daemon and the handler subprocess"*

Bridge's CHB-INV-001 / HC-045b introduce additional one-shot connections from a DIFFERENT subprocess (the relay), not from the handler subprocess.

**Resolution:** the HC-045b amendment's prose explicitly carves out: "These short-lived subprocesses MAY open one-shot NDJSON connections ... Such connections are NOT subject to HC-007's 'sole bidirectional channel' phrasing (which scopes to the handler subprocess itself, not to incidental short-lived subprocesses spawned by the agent)." This is a deliberate scoping clarification, not a contradiction. The integration pass confirms the resolution stands.

**No further action.** HC-007 prose is unchanged; HC-045b's carve-out is sufficient.

### C2. PL-018 ID collision (placeholder in PL amendment)

PL-018 already exists in §4.6 ("Daemon is a deterministic Go binary"). The PL amendment proposed PL-018 in §4.5, which collides.

**Resolution:** assign the new requirement ID **PL-017a** (mnemonic numbering between PL-017 silent-hang and PL-018 deterministic-daemon-binary). PL amendment patched in spec-draft to use PL-017a placeholder; integration confirms this is the chosen final ID.

### C3. WM-038 vs WM-NEW-1/2/3 numbering

Decompose pass listed WM-NEW-1, WM-NEW-2, WM-NEW-3. Spec-draft consolidated these into a single WM-040a (originally drafted as WM-038, renamed in the 2026-05-12 correction pass to avoid collision with the existing WM-038 interrupt-state writer requirement) because the three were one cohesive requirement (settings.json materialization with merge+atomic-write+gitignore as one transactional concept).

**Resolution:** consolidation is correct. WM-040a covers all three. The gitignore extension is folded into WM-013e via an inline amendment, not a new requirement.

### C4. Bridge spec uses "agent subprocess" for Claude — terminology drift?

HC-007 distinguishes "handler subprocess" (the process the handler launches). At MVH, for claude-code, the handler subprocess IS Claude Code itself. CHB's prose calls Claude the "agent subprocess" sometimes and "Claude" sometimes; HC-007 says "handler subprocess."

**Resolution:** marked as MINOR finding for spec-draft-review pass. Rename CHB usages of "agent subprocess" / "harmonik handler-process" to align with HC's "handler subprocess" vocabulary.

## Coherence assessment

The bridge spec is a clean addition to the corpus:
- It depends on HC, WM, EV, PL but doesn't modify their core semantics.
- The four amendments are surgical (new requirements with deterministic IDs, no renumbering).
- Zero new event types means EV-027 (workflow-modes amendment scope) is uncontested.
- Zero new control points (no operator-policy surface introduced).
- Twin-parity invariant HC-INV-002 is preserved through CHB-022.

The load-bearing structural insight (two-contributor model: handler+relay both write to daemon socket) is named in CHB-INV-001 and contractually grounded by HC-045b. This is the only piece that previously didn't exist anywhere in the corpus; it lands cleanly.

## Final action items (rolled into tasks pass)

1. **Wording cleanup**: CHB prose to use "handler subprocess" consistently with HC vocabulary. Spec-draft-review pass.
2. **ID assignment**: PL amendment to formalize PL-017a; integration confirms.
3. **EV amendment**: skip per recommendation; revisit if Phase 3 reviewers request glossary additions.
4. **AGENT_INDEX.md row**: add claude-hook-bridge to the normative spec inventory at finalize.

## Status

Integration check PASS. No unresolved contradictions. All cross-references valid. Three MINOR findings rolled to tasks-pass and spec-draft-review.
