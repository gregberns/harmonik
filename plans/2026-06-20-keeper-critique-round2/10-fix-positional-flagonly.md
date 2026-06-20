# hk-nbft — Make every `harmonik keeper` subcommand flag-only

**Goal:** every keeper subcommand must reject positional args with a clear,
uniform message and a non-zero exit (2). The agent is named ONLY via `--agent`.

## Mechanism (reused, not reinvented)

`restart-now` / `await-ack` already rejected positionals via the shared helper
`resolveKeeperAgent` (cmd/harmonik/keeper_cmd.go), which checks `fs.NArg() > 0`
and prints:

```
<label>: unexpected positional argument(s) "<args>" — this command is flag-only; use --agent <name>
```

and returns exit 2. The fix applies this SAME message + exit-2 contract to the
two stragglers (`enable`, `doctor`), which used hand-rolled parsers that accepted
a bare positional as the agent name.

## Per-subcommand before/after

| Subcommand            | Parser path                       | Before                                              | After                          |
|-----------------------|-----------------------------------|-----------------------------------------------------|--------------------------------|
| keeper (bare watcher) | resolveKeeperAgent                | positional rejected, exit 2                         | unchanged (already correct)    |
| set-dispatching       | parseKeeperMarkerArgs→resolve…    | positional rejected, exit 2                         | unchanged                      |
| clear-dispatching     | parseKeeperMarkerArgs→resolve…    | positional rejected, exit 2                         | unchanged                      |
| restart-now           | parseKeeperMarkerArgs→resolve…    | positional rejected, exit 2 (the model)             | unchanged                      |
| ping                  | resolveKeeperAgent                | positional rejected, exit 2                         | unchanged                      |
| await-ack             | resolveKeeperAgent                | positional rejected, exit 2                         | unchanged                      |
| **enable**            | parseKeeperEnableArgs (hand-roll) | **positional ACCEPTED as agent** (parsed `captain`, then failed downstream on scripts-dir) | **positional rejected, exit 2**; `--agent` now required |
| **doctor**            | parseKeeperDoctorArgs (hand-roll) | **positional ACCEPTED as agent, exit 0 false-green** at keeper boot | **positional rejected, exit 2**; `--agent` now required |

Only `enable` and `doctor` changed behavior; the other six were already
flag-only. The operator's report matches exactly: doctor exited 0 on a
positional, enable parsed the positional then failed later.

## Changes

- `cmd/harmonik/keeper_enable_doctor_cmd.go`
  - `parseKeeperEnableArgs`: removed the `rest[0]→agentName` fallback; any
    leftover positional → exit 2 with the shared flag-only message. `--agent`
    now required (exit 1 if absent).
  - `parseKeeperDoctorArgs`: same.
  - Usage strings (`keeperEnableUsage`, `keeperDoctorUsage`) rewritten to the
    flag-only form (`--agent <name>`), documented exit code 2.
- `cmd/harmonik/keeper_positional_reject_hknbft_test.go` (NEW): table-driven
  regression guard — for EVERY keeper subcommand, asserts a positional arg →
  exit 2 + the `"flag-only"` / `"--agent"` message substrings. Plus a positive
  companion asserting `--agent` does NOT hit the flag-only reject. Captures
  os.Stderr at the FD level (several run-entries write directly to os.Stderr).
- `cmd/harmonik/keeper_enable_doctor_cmd_test.go`: updated the pre-existing
  parser-parity tests that encoded the OLD positional-accepting contract
  (PreservesYesDestructive, AllKnownFlagsPreserved, ProjectFlagPreserved →
  `--agent` form; AgentFlagWinsPositional → now RejectsPositional asserting
  exit 2; doctor RejectsPathTraversal → RejectsPositionalAgent asserting exit 2).

## Safety

All subcommands reject the positional at the argument-PARSE stage, BEFORE any
tmux pane resolution or lockfile acquisition — so the new test does no live
tmux/process work. Verified: the new test run x3 added zero tmux sessions
(`tmux ls | wc -l` stable). No fork-bomb risk.

## Verification

- `go build ./...` — clean
- `go vet ./cmd/harmonik/... ./internal/keeper/...` — clean
- `go test ./cmd/harmonik/... ./internal/keeper/...` — all `ok`
