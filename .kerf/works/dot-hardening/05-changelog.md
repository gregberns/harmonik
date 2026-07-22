# 05 — Changelog (consolidated)

> Consolidates the three per-file changelog fragments (end of each `05-spec-drafts/*.md`) for
> `kerf finalize`. All changes are **additive** except two honestly-flagged non-additive changes (EM).
> Cross-references between drafts verified aligned (EM §7.5 EM-069 renderer, EM-069-MAN manifest,
> EM-056 replacement, EM-012b precedence — WG and HC point at these; EM lands them).

| Target spec file | Status | Version | Summary |
|---|---|---|---|
| `specs/workflow-graph.md` | modified | 0.3.1 → 0.4.0 | Declared per-node I/O + structural visibility + verdict typing + override addressing |
| `specs/execution-model.md` | modified | → v0.10.0 | Single renderer + task/rubric value-source + feedback-on-back-edge (replaces dot-mode prohibition) + per-(node,iteration) model seal + replay-reads-seal |
| `specs/handler-contract.md` | modified | modified | Transport-only adapter + per-tool alias-catalog lookup (reaches rc.model for every tool) + DOT round-trip coverage seams |
| `specs/examples/standard-bead.dot` (+ sidecar) | modified (exemplar) | — | Gains declared I/O so the default graph is the leak-proof path (D-A9) |

## workflow-graph.md (0.4.0)
New: WG-055 declared `inputs` (distinct from context_keys and template params; optional-with-role-default),
WG-056 declared `outputs` (producer->consumer; reviewer produces verdict+notes; dangling = warning),
WG-057 structural per-node visibility (task/rubric distinct; implementer never declares rubric; leak
unexpressible — D-FIX-1), WG-058 output types (verdict enum = typed name for the preferred_label value),
WG-059 override node-id addressing + model_locked (task config, not version pinning; serialization → ISSUES #1),
WG-060 load checks (unbound-required-input; verdict<->edge only on from-node=producer edges — P4; back-edge cap
= ref to WG-028; override-names-real-node). Amended: WG-040 (prompt = renderer input; reviewer prompt = rubric
source), WG-044 (goal = default-visible declared input, not broadcast), WG-014/015/025 (preferred_label
enum-constrained when verdict enum declared), WG-019 (cross-ref, routing unchanged), WG-002, WG-031, §16.1.

## execution-model.md (v0.10.0)
New: EM-069 single renderer (replaces L1584–1590 B<->E contract), EM-069-REV reviewer framing (body verbatim +
SHAs + rubric), EM-069-SRC task/rubric value-source (D-FIX-1), EM-069-MAN daemon-emitted input manifest (D-FIX-2),
EM-069-FB feedback value channel, EM-069-ITER iter binding, EM-070 per-(node,iteration) seal (H1), EM-071 replay
reads the seal (H2). New §6.1: Node.role + ENUM Role, Run.node_model_seal.
NON-ADDITIVE (flagged): REPLACE EM-056 clause 4 (dot-mode feedback prohibition struck; resolves hk-wixms
code/spec contradiction); REWRITE EM-012b-NODE + precedence block (ladder flip). Amended: EM-055 step 6 +
resume semantics (seal-read branch), EM-015d-RFD/RIA, §6.1 Workflow.goal note.

## handler-contract.md (modified)
New: HC-072 transport-only adapter (§4.1b; no new callback — HC-013 budget held), HC-073 per-tool
alias-catalog lookup (§4.10; catalog at .harmonik/config.yaml models.aliases, hot-reload, keep-last-good;
reaches rc.model for every tool; three-way fail-loud/degrade+warn/keep-last-good + pi non-empty floor),
HC-074 DOT round-trip coverage seams (§4.8a; contract-only, mechanics in scenario-harness.md S07; three
honesty limits stated). Amended: HC-006a §249 (re-pointed to EM-069), HC-055a (argv generalized to every
tool; lookup-vs-validity scope split preserves value-opacity).

## Open reconciliations for `kerf finalize`
- WG front-matter `version:` reads 0.1.0 vs table 0.3.1; drafts followed the table (→0.4.0). Correct at finalize.
- Cross-ref sync: WG/HC point at EM §7.5 EM-069 / EM-069-MAN; verified aligned. Re-verify if any EM id shifts.
- Role-enum openness: EM draft resolved unknown role -> fallback to agent_type framing (not fail-load). Confirm
  at finalize (ADV-B S2 alternative: hard-fail on unknown role).
