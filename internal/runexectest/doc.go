// Package runexectest hosts the run-state-machine L2 replay/fault tiers
// (RT11, codename:run-state-machine; run-state-machine.md §11 RSM-030;
// liveness-parity-design §6 Oracle). It mirrors internal/keepertest 1:1 and
// contains NO production code — every test lives in the external package
// runexectest_test.
//
// The tiers (all zero-token, all VIRTUAL time — substrate.FakeClock only):
//
//   - The fault matrix (fault_matrix_test.go): substrate.Twin over the
//     schedules the replay.StimulusSynthesizer derives from the recorded run
//     corpus (testdata/daemon-runs/baseline-2026-07-14), across the substrate
//     fault modes (DropAfter / Stall / Truncate / Dup) at every stimulus
//     position. Every cell must reach a Run terminal (closed or reopened) or
//     an explicitly asserted entry-foreclosed shape — silence is FORBIDDEN
//     (RSM-INV-001/002). The headline cell is FaultStall-after-resume.
//
//   - The N=10 clean relaunch oracle (relaunch10_test.go): ten consecutive
//     clean resumed-relaunch cycles through the Dispatch + Run machines on
//     one FakeClock, all green.
//
// The shell-sim harness (harness_test.go) plays runshell's role: it maps
// ArmTimer/CancelTimer actions onto virtual deadlines, answers Run-machine
// port actions with clean substrate results, models the shell-side frozen
// commit watchdog (M3-D3: EvHeartbeatStale is shell-fed, not a reactor
// timer), and bridges Dispatch terminals into Run mode outcomes.
package runexectest
