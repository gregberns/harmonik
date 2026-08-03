# Spec-draft changelog — Declared Substrate Capability Contract

## Drafts

| Target spec file | Status | Change | Design source |
|---|---|---|---|
| `specs/process-lifecycle.md` | modified | Add PL-021b item 6a. The daemon composition root resolves selected host-control capabilities before dispatch. A missing required capability refuses the selected mode. A missing optional capability has an explicit tested degraded result. The amendment keeps the handler seams narrow, preserves session resolution and sealed workflow selection, and removes the unreachable independent run-session path. PL-028b item 4 cross-references the selected-mode rule. | `04-design/process-lifecycle-design.md`, `04-design/daemon-contract-design.md`, `04-design/step12-serialization.md` |

## No-draft dispositions

- `specs/handler-contract.md` is unchanged. `Substrate`, `Session`, and
  `InputPort` remain narrow public seams.
- `specs/execution-model.md` is unchanged. The selected workflow remains
  sealed before `run_started`.
- `specs/agent-input.md` is unchanged. Pane capture remains observation only.
  This work does not add paste or input semantics.

## Integration constraints

- The full capability decision table remains in
  `04-design/daemon-contract-design.md`. The process-lifecycle amendment does
  not publish private interface names.
- `bootState.wireWatchersAndObservers` remains serialized behind the Step 12
  base change. Its later integrated edit keeps all subsystem-switch guards.
- The removal of the independent run-session branch is not a removal of tmux
  hosting. `PL-021b` still requires the selected tmux host to create windows.
