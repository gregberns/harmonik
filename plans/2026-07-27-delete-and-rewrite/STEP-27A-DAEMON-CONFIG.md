# Step 27a — daemon configuration construction

Status: planned source analysis on 2026-08-02. No production code changed.

## Purpose

Give the live daemon command paths one checked construction boundary. The work
does not create a second composition root. `cmd/harmonik/run.go` is a dead
daemon-start path today because it leaves `Config.WorkflowModeDefault` empty.
`daemon.resolveBootConfig` then calls `bootconfig.ValidateWorkflowMode`, which
rejects that value before boot can continue.

`internal/daemon` remains the composition root. The command package resolves
operator input and passes values into its public constructor.

## Re-measured scope

`daemon.Config` has 40 exported fields. It has four non-test assembly sites:

| Site | Current role | Fields written |
|---|---|---:|
| `cmd/harmonik/main.go` | live `harmonik daemon` boot | 21 |
| `cmd/harmonik/run.go` | inline queue run | 17 |
| `internal/scenario/orchdrive.go` | scenario harness | 14 |
| `internal/daemon/scenariotest/concurrent_merge.go` | daemon integration fixture | 12 |

Only the first two are operator boot paths. The other two need a valid
constructor contract, but they must keep their short-lived test settings.

The old list of nine omissions must not become a required-field list. Source
shows that most have intentional zero-value meanings:

- `WorkflowModeDefault` is required. `bootconfig.ValidateWorkflowMode` rejects
  empty and unknown values.
- `DefaultHarness` falls back to `core.AgentTypeClaudeCode` in
  `resolveHarness`.
- `AgentReadyTimeout` and `RemoteAgentReadyTimeout` use effective defaults in
  `runlaunch.EffectiveAgentReadyTimeout`.
- An empty `KerfPath` disables eager refill in `eagerRefillEval`.
- An empty `workers.Config` selects local execution in `workers.BuildRegistry`.
- A non-positive `SubscriptionTokenCeiling` disables the bandwidth tuner in
  `bootState.startBandwidthTunerIfEnabled`.
- `NoAutoPull=false` is the published compatibility behavior.
- `CodexBinary` has no production reader in `internal/daemon`. It is a separate
  unwired-path defect, not a constructor requirement.

`ProjectDir` is required for the two operator paths. `daemon.Start` must still
allow its empty test mode. The constructor needs a production and harness
policy rather than changing `Start` to reject every existing unit fixture.

## Design decision to make in Kerf

Create a new spec work named `daemon-config-construction` before implementation.
It is a medium-risk change across the public daemon boundary and two command
paths. No existing Kerf work covers this contract. Do not add it to
`event-payload-ownership`, which has a separate event-model purpose.

The work must settle this narrow contract:

1. Add an exported `daemon.NewConfig` constructor with a named input type. It
   returns `(Config, error)`.
2. The constructor validates only values that cannot be omitted for the caller
   category. It must call the existing `bootconfig.ValidateWorkflowMode` rather
   than repeat its workflow-mode rules.
3. Provide an explicit production input category. It requires a non-empty
   project directory and an explicit valid workflow mode.
4. Provide an explicit harness input category, or a documented constructor
   option, for `orchdrive` and `scenariotest`. It admits their project roots,
   explicit mode, short timeout, queue-only flag, and skip-preflight values.
5. Keep `Config` usable by unit tests and keep `daemon.Start` validation as the
   defense for direct callers. The constructor does not make every test literal
   change at once.

The named input type must hold the configuration as values. It must not hide
the check behind a mutable global or a late `Start` mutation. The command paths
must not retain direct `daemon.Config{...}` literals after the change.

## Implementation order

1. Create and complete the Kerf design for `daemon-config-construction`.
   Record the caller categories and the exact error surface.
2. Add the constructor, typed input, and typed constructor error in
   `internal/daemon`. Keep `Config` as the runtime value consumed by `Start`.
3. Convert `cmd/harmonik/main.go` to construct its live configuration through
   the constructor.
4. Convert `cmd/harmonik/run.go` through the same constructor. Set its daemon
   default explicitly to `core.WorkflowModeDot`. Keep `--workflow-mode` as the
   per-item override that it already writes into the queue.
5. Convert `internal/scenario/orchdrive.go` and
   `internal/daemon/scenariotest/concurrent_merge.go` through the admitted
   harness path.
6. Add a small command-local pure builder only if it is needed to test the
   command inputs without booting tmux. Do not move flag parsing or create a
   general command-framework package.
7. Re-run the source inventory. There must be no non-test
   `daemon.Config{...}` literal outside the constructor implementation.

Step 10 is complete at `9f52d7f5d`, so its technical dependency is met. Step
13 is likely to touch `internal/daemon`. Serialize the Step 27a implementation
after that merge, or use a dedicated worktree and rebase before review. This is
an operational collision guard, not a new technical dependency.

## Tests and acceptance

Add focused tests with these claims:

1. A production input with an empty workflow mode fails from `NewConfig` and
   names `WorkflowModeDefault`.
2. An unknown or retired workflow mode fails from `NewConfig` with the same
   validation class as `daemon.Start`.
3. The live-daemon and inline-run builders both produce a valid explicit daemon
   workflow default. The inline run must not reach `daemon.Start` with an empty
   mode.
4. The scenario and concurrent-merge shapes construct successfully with their
   intentional test values.
5. Direct `daemon.Start` with an invalid literal remains rejected. This keeps
   the public boundary safe for callers that do not yet use the constructor.
6. A source ratchet checks that production sites no longer use raw
   `daemon.Config{...}` literals. Put it in an existing checked test or a gate
   that is wired into a check tier.

During implementation, temporarily remove the constructor mode check and
confirm the first test fails. Restore it before committing.

Run, at minimum:

```sh
go test -short -count=1 ./internal/daemon ./cmd/harmonik ./internal/scenario ./internal/daemon/scenariotest
go test -short -count=1 ./internal/daemon
```

The second command is the required Alpha package gate. Run any established
composition and freeze gates that touch the changed paths after the focused
tests pass.

## Exclusions

- Do not make zero-valued optional settings fail at construction.
- Do not wire `CodexBinary`, `CPRegistry`, or other known unwired surfaces as
  part of this step.
- Do not change workflow resolution, queue item overrides, timeout defaults,
  worker behavior, eager refill, or bandwidth-tuner policy.
- Do not migrate the large body of direct test literals in this step.
