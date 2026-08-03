# Integration — Declared Substrate Capability Contract

## Scope checked

The integration check scanned all 44 files in `specs/`. It reviewed the direct
term matches and contract anchors in `process-lifecycle.md`,
`handler-contract.md`, `execution-model.md`, `agent-input.md`,
`beads-integration.md`, and `run-state-machine.md`.

The drafted `process-lifecycle.md` is a complete copy of the current target.
Its diff contains only the version change, PL-021b item 6a, the PL-028b
cross-reference, the conformance test obligation, and the revision entry.

## Cross-reference checks

| Check | Result |
|---|---|
| `PL-021b` item 6a to the composition-root rule | Consistent. `internal/daemon` remains the composition root. |
| `PL-021b` item 6a to `handler-contract.md` HC-069 and HC-071 | Consistent. The amendment does not add public `Substrate`, `Session`, or `InputPort` methods. |
| `PL-021b` item 6a to `execution-model.md` EM-012a | Consistent. Construction checks occur before dispatch. Run-plan operation checks do not change the sealed workflow. |
| `PL-021b` item 6a to `agent-input.md` AIS input and acknowledgment rules | Consistent. Capture is observation only. `InputPort` remains the tmux/Claude delivery seam and AIS supplies positive acceptance. |
| `PL-021b` item 6a to PL-021b session resolution | Consistent. The three session outcomes remain unchanged. |
| `PL-028b` item 4 | Consistent. Startup announces hosting loss. Selected-mode capability failure remains before dispatch. Optional daemon-private capabilities do not gain a boot-announcement obligation. |
| Existing cross-spec file references in the full draft | Valid. All named target files exist in `specs/`. |

## Resolved integration findings

- **Remote scope.** This work does not make remote hosting a selected public
  mode. `runnerSwapper` is an operation requirement when a run has chosen a
  remote worker. It fails structurally before worker tmux calls. The optional
  `sessionEnsurer` call stays conditional. Its absence is not a new preflight
  guarantee. A later remote work owns that policy.
- **Paste callers.** The daemon design now records a caller and harness matrix.
  The tmux/Claude daemon-run input contract stays on `InputPort`; the AIS async
  event remains its positive acceptance. Launch seed, cognition-gate seed, and
  reviewer re-seed have explicit compatibility results when paste is absent.
  Crew mission paste remains a best-effort non-run path.
- **Check timing.** PL-021b item 6a distinguishes construction requirements
  from run-plan and post-spawn operation requirements. This prevents a pane
  target or remote-worker choice from being falsely described as known before
  `run_started`.

## Run-session and buffer closure

The selected change removes the unreachable independent run-session interface,
concrete creation method, state, branch, run-only tests, and documentation. It
does not remove tmux window hosting.

`perRunSubstrate.inputBufferName` is used by its run input method. Preserved
crew, gate, and legacy paste callers use `bufferName` directly with their
existing session identifiers. Therefore the run-only pane-target uniqueness
rule does not amend the retained PL-021d buffer-name contract. The
implementation task must prove two shared-session runs with distinct captured
pane targets use distinct valid input buffer names.

## Serialized composition

The specification draft does not schedule an edit to
`bootState.wireWatchersAndObservers`. The implementation plan retains the
accepted order: Step 12 lands its subsystem-switch base first. Alpha then
makes one integrated edit that replaces the quiesce-adapter and diagnostic-hook
discovery while retaining every switch guard and disabled-subsystem result.

## Changelog and terminology

`05-changelog.md` names the one modified spec and the three no-draft
dispositions. It preserves the full capability table as a daemon design
artifact, not a public Go interface list. The terms *construction requirement*,
*operation requirement*, *optional capability*, and *degraded result* match
the approved design and the new PL-021b item 6a.

## Assessment

The drafted amendment is coherent with the existing specification corpus. No
additional cross-spec change is needed. The work is ready for integration
re-review.
