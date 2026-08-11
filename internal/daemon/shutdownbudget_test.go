package daemon

import "time"

// daemonExitHangBudget bounds a wait for a shutdown that has already been asked
// for — daemon.Start returning after its context is cancelled, and the like.
//
// It is a HANG DETECTOR, not a rate. The question these waits ask is "does this
// return at all once cancelled", and the wrong answer is NEVER, not SLOW. A
// generous bound therefore proves exactly the same property as a tight one: a
// shutdown that is going to happen happens in milliseconds, and one that is
// broken never happens at all. There is no shutdown that legitimately takes
// forty seconds.
//
// The tight bounds this replaces did not measure the code. They measured the
// machine. `make full` runs 110 package binaries at once, so a five-second
// bound on a correct shutdown fails as a function of how busy the box is, and
// the merge decision returns a different verdict on the same commit. Refs
// hk-vp02y, where TestParallelSmoke_TwoBeadsConcurrent failed this way at
// 19.95s while every assertion about its actual subject passed.
//
// Keep this LARGE. If a future shutdown regression makes a test take thirty
// seconds and still pass, that is the correct trade: the test suite's own
// timeout is the backstop for a true hang, and a bound tight enough to catch
// "slow" is a bound tight enough to fail on a loaded box.
const daemonExitHangBudget = 60 * time.Second

// ExportedDaemonExitHangBudget is the same budget, reachable from the external
// test package. Most of these waits live in package daemon_test, which cannot
// see an unexported identifier declared here, so the alias keeps ONE value
// rather than a second literal that drifts. Same export_*_test.go seam idiom the
// rest of this package uses. Read the comment above for what the budget is for.
const ExportedDaemonExitHangBudget = daemonExitHangBudget
