# Twin-Binary Gap-Fill — Tracking Beads (2026-05-05)

> Audit + new-bead pass that closes the "no tasks to actually build the Claude
> twin binary" gap surfaced by the user on 2026-05-05 ("We didn't create tasks
> to generate the claude twin? S07 or whatever didn't do that?"). Source of
> truth for the gap: [phase-1-readiness-gap-analysis.md §B1
> (BLOCKER)](../foundation/phase-1-readiness-gap-analysis.md). Authored by the
> twin-binary task gap-filler agent.

## TL;DR

- Existing twin-related corpus beads: **15** (audited below). All declare
  *contracts* about twins; none authored the `cmd/harmonik-twin-claude/` Go
  program that those contracts apply to.
- New beads filed: **9** under a new mini-epic `hk-ahvq.48` ("Twin-binary
  scaffolding mini-epic"), all `scope:bootstrap`, all dispatchable (no parked
  / awaiting-review status).
- Dependency edges added: **17**. `br dep cycles` clean before and after.
- One spec gap surfaced (twin script-file format unspecified) — filed as
  `hk-ahvq.48.9` for follow-up after working code grounds the schema.

## 1. Existing twin-related corpus (audit)

Found via `br list --title-contains "twin"` plus
`br list --title-contains "claude"` plus
`br list -l scope:bootstrap | grep -i twin`. Classification per the mission
brief:

- **Contract** — declares behavior the twin must exhibit; doesn't build.
- **Implementation** — would build something concrete (file, binary).
- **Test** — exercises the twin via the harness.

| Bead | Title | Class | Source spec |
|---|---|---|---|
| hk-8i31.42 | Twins implement the same Handler interface | Contract | HC-035 |
| hk-8i31.43 | Twin subprocesses honor the same wire protocol | Contract | HC-036 |
| hk-8i31.44 | Twins carry identical boundary-classification tags | Contract | HC-037 |
| hk-8i31.45 | Twin conformance drift detection scoped to S07 | Contract | HC-038 |
| hk-8i31.47 | Twins MUST emit agent_ready identically | Contract | HC-040 |
| hk-8i31.53 | Twin binaries obey same launch rules | Contract | HC-045 |
| hk-8i31.59 | Twin-parity for skill provisioning is wire-only | Contract | HC-049a |
| hk-8i31.65 | Sensor: twin handlers indistinguishable from real handlers | Contract (sensor) | HC-INV-002 |
| hk-8i31.77 | Canonical twin handler binary for scenario-harness tests | Contract (declarative) | HC-035..038 |
| hk-i0tw.8 | Twin substitution is a handler-config override, not a runtime branch | Contract | SH-008 |
| hk-i0tw.9 | Twin-binary discovery uses scenario path or PATH prefix | Contract | SH-009 |
| hk-i0tw.10 | Missing twin binary fails the scenario with `twin-binary-not-found` | Contract | SH-010 |
| hk-i0tw.11 | Twin parity is the parity surface declared in HC §4.8 | Contract | SH-011 |
| hk-i0tw.39 | Sensor: harness operates only on twin binaries | Contract (sensor) | SH-INV-003 |
| hk-8mup.50 | Pidfile + socket twin-driven fixture | Test | PL-001..PL-003b |

**Result of audit:** zero `Implementation`-class beads. The closest is
`hk-8i31.77`, which the readiness analysis (§B1) explicitly calls out as
"abstract — describes the binary's contract but not its construction." There
is no bead authoring `cmd/harmonik-twin-claude/main.go`, no bead authoring the
twin's wire-protocol emission loop, no bead authoring the script-driver, no
bead authoring the build artifact placement, and no bead authoring the first
SH §10.1 conformance scenario YAML. This matches the readiness analysis's
v0.1 finding exactly.

## 2. Gaps identified

The minimum-viable twin-binary work to support [scenario-harness.md §10.1
conformance set](../../specs/scenario-harness.md):

1. **Binary directory + entry-point.** No bead authors `cmd/harmonik-twin-claude/main.go`. The `subsystem-organization.md §Go module layout` names the path; it does not exist.
2. **Wire-protocol parity loop.** No bead authors the NDJSON-over-Unix-socket emitter that satisfies HC-007/HC-007a/HC-008/HC-040 in the twin process.
3. **Script-driver.** No bead authors the loop that reads a per-scenario script file and emits the declared message stream. (HC-036's "the subprocess script drives output instead of an LLM" assumes such a loop exists.)
4. **Commit-hash stamp.** No bead embeds the source commit-hash in the twin binary so that the daemon's HC-043 pre-launch check passes. (`hk-sx9r.7` is the daemon-side stamp; no twin-side counterpart.)
5. **Build artifact placement.** No bead wires the twin into a Makefile target whose output lands at `<repo-root>/twins/claude-twin` so SH-009's default search path resolves it.
6. **First conformance scenario.** No bead authors `scenarios/smoke/twin-launch-and-ready.yaml` (the floor scenario for §1 acceptance).
7. **Second conformance scenario.** No bead authors `scenarios/smoke/checkpoint-and-merge.yaml`.
8. **Third conformance scenario.** No bead authors `scenarios/regression/twin-failure-classification.yaml`.
9. **Spec-gap.** Neither HC nor SH defines the on-disk schema for the twin's scripted-input file (referenced by HC §4.6 scenario-mode carve-out and HC-036 but never schema'd). Logged for spec-edit follow-up; not blocking the code beads since the schema can be established in code first.

## 3. New beads filed

All filed under new mini-epic `hk-ahvq.48` (parent: `hk-ahvq` Phase-0 meta).
All carry `scope:bootstrap` plus appropriate `kind:`/`phase:`/`spec:` labels.
All are dispatchable (status `open`, no `parked`/`awaiting-review`).

| Bead | Title | Parent | Labels | Addresses gap |
|---|---|---|---|---|
| hk-ahvq.48 | Twin-binary scaffolding mini-epic (cmd/harmonik-twin-claude/ + first scenarios) | hk-ahvq | kind:meta-parent, phase:0, scope:bootstrap, tag:meta | — (epic) |
| hk-ahvq.48.1 | Author cmd/harmonik-twin-claude/main.go scaffold + Go module entry-point | hk-ahvq.48 | kind:scaffold, phase:1, scope:bootstrap, spec:handler-contract, tag:mechanism | 1 |
| hk-ahvq.48.2 | Twin wire-protocol parity loop: NDJSON over Unix socket per HC-007/HC-007a/HC-008 | hk-ahvq.48 | kind:scaffold, phase:1, scope:bootstrap, spec:handler-contract, tag:mechanism | 2 |
| hk-ahvq.48.3 | Twin script-driver: read fixture file + emit scripted message stream | hk-ahvq.48 | kind:scaffold, phase:1, scope:bootstrap, spec:handler-contract, tag:mechanism | 3 |
| hk-ahvq.48.4 | Twin binary commit-hash stamp via build-time ldflags (HC-043 satisfaction) | hk-ahvq.48 | kind:scaffold, phase:1, scope:bootstrap, spec:handler-contract, tag:mechanism | 4 |
| hk-ahvq.48.5 | Makefile target + build artifact placement for twins/ search-path | hk-ahvq.48 | kind:scaffold, phase:1, scope:bootstrap, spec:scenario-harness, tag:mechanism | 5 |
| hk-ahvq.48.6 | Author scenarios/smoke/twin-launch-and-ready.yaml + twin script | hk-ahvq.48 | kind:scaffold, phase:1, scope:bootstrap, spec:scenario-harness, tag:mechanism | 6 |
| hk-ahvq.48.7 | Author scenarios/smoke/checkpoint-and-merge.yaml + twin script | hk-ahvq.48 | kind:scaffold, phase:1, scope:bootstrap, spec:scenario-harness, tag:mechanism | 7 |
| hk-ahvq.48.8 | Author scenarios/regression/twin-failure-classification.yaml | hk-ahvq.48 | kind:scaffold, phase:1, scope:bootstrap, spec:scenario-harness, tag:mechanism | 8 |
| hk-ahvq.48.9 | SPEC-EDIT: define twin script-file format normatively in HC §4.8 or SH §4.3 | hk-ahvq.48 | kind:spec-edit, phase:0, scope:bootstrap, spec:handler-contract, tag:meta | 9 |

Each bead's description cites the specific spec section / readiness-analysis
recommendation that motivates it. Two of the beads (hk-ahvq.48.1 and
hk-ahvq.48.6) match the readiness analysis's named candidates
`p1-twin-claude-binary-scaffold` and `p1-twin-conformance-scenarios`; the
analysis recommended one bead per slot, and this pass split them into the
finer-grained 9-bead set so each concrete deliverable is its own dispatchable
unit.

## 4. Dependency edges added

Internal edges within the mini-epic (`<bead> -> <depends-on>`, "blocks" type):

```
.48.2 -> .48.1   (wire-protocol needs main.go scaffold)
.48.3 -> .48.1
.48.3 -> .48.2   (script-driver needs wire to emit onto)
.48.4 -> .48.1
.48.5 -> .48.1
.48.6 -> .48.2 + .48.3 + .48.4 + .48.5   (first scenario needs full binary)
.48.7 -> .48.6   (second scenario builds on first's pattern)
.48.8 -> .48.3 + .48.6   (third scenario needs script-driver's "exit_unexpectedly")
.48.9 -> .48.3   (spec-edit waits for working schema)
```

Cross-spec edges to the existing corpus:

```
.48.6 -> hk-i0tw.9    (first scenario relies on twin-binary discovery)
.48.6 -> hk-i0tw.10   (first scenario must surface twin-binary-not-found cleanly)
hk-8i31.77 -> .48.1   (canonical-twin-bead's existence requirement satisfied by main.go scaffold)
hk-8i31.77 -> .48.2   (... + wire protocol)
hk-8i31.77 -> .48.3   (... + script-driver)
hk-8i31.77 -> .48.4   (... + commit-hash)
```

Total added: **17 dep edges**. `br dep cycles` after additions: **clean**.

Edges considered and removed:
- `hk-8i31.50 -> .48.4` — daemon-side hash check and twin-side stamp are
  orthogonal; daemon check matches whatever stamp exists, so this isn't a
  blocking edge. Removed.
- `.48.4 -> hk-sx9r.7` — sibling pattern (same -ldflags mechanism), not
  blocking; daemon and twin can stamp independently. Removed.

## 5. Spec-gap follow-up

`hk-ahvq.48.9` records a real spec gap discovered during the bead-filing
pass: HC §4.6 scenario-mode carve-out and HC-036 reference "a scenario script"
that drives the twin's output, and SH §4.3 references `agent_overrides`
(which selects *which* twin) — but **no spec defines the on-disk schema /
filename / path-resolution rule for the script-file the twin reads to
generate its output stream**. The recommended approach: hk-ahvq.48.3
establishes a working schema in code (suggested:
`<fixture-root>/<scenario>/twin-scripts/<role>.yaml`); then hk-ahvq.48.9
lifts that schema into a normative §6.1 record (likely SH-side, since the
script is a scenario fixture, not a handler-contract artifact). Don't block
.48.3 on the spec edit.

## 6. Recommended next-step actions

For the agent that picks up this work after Phase 0 exit (or sooner if the
user prioritizes the first conformance scenario):

1. **First**, land the build-scaffolding mini-epic (Makefile,
   `.golangci.yml`, lefthook) per readiness-analysis §B2 — without `make
   check-full`, the twin-binary work is unverifiable.
2. **Then** dispatch hk-ahvq.48.1 (main.go scaffold) — it's the leaf-most
   blocker; everything else in .48 depends on it transitively.
3. **In parallel** with .48.1, draft a one-page proposal for the twin script
   schema (referenced by .48.3) so the script-driver and the first scenario
   can be authored against an agreed shape.
4. **After** .48.6 lands and runs green, treat hk-ahvq.48.9 (spec-edit) as a
   ready bead — the working schema grounds the spec language.
5. **Don't** open a separate spec-edit pass for HC's twin-script schema until
   .48.3 has a working implementation; the readiness-analysis warns against
   speculative spec changes.

## 7. Verification trail

Commands used:

```
br list --title-contains "twin"
br list --title-contains "claude"
br list -l scope:bootstrap | grep -i twin
br show hk-8i31.77   # canonical twin bead per readiness analysis
br show hk-i0tw.9 / .10 / .39
br show hk-8i31.42 / .43 / .47 / .65 / .53 / .59 / .44 / .45
br show hk-sx9r.6   # daemon-side commit-hash gate
br show hk-i0tw.8 / .11
br show hk-ahvq     # Phase-0 meta epic
br dep cycles       # baseline (clean)
br create ...       # 9 new beads
br dep add ...      # 17 edges (2 removed)
br dep cycles       # post (clean)
```

Source documents inspected:
- `specs/scenario-harness.md` v0.2.0 (§4.3, §10.1, §10.2)
- `specs/handler-contract.md` v0.3.3 (§4.6, §4.8, §4.10, §6.1)
- `docs/concepts/digital-twins.md` (referenced; not contradicted by this pass)
- `docs/bootstrap.md` (§5 step 4)
- `docs/foundation/phase-1-readiness-gap-analysis.md` (§B1 BLOCKER, §candidate beads)
- `docs/foundation/project-level/subsystem-organization.md` (§Go module layout, decision #6)
- `docs/decompose-to-tasks/hc-pilot.md` + `hc-pilot-data.yaml` (twin-bead pattern reference)
