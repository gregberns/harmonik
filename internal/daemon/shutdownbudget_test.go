package daemon

import "time"

const daemonExitHangBudget = 60 * time.Second

// ExportedDaemonExitHangBudget is the same budget, reachable from the external
// test package. Most of these waits live in package daemon_test, which cannot
// see an unexported identifier declared here, so the alias keeps ONE value
// rather than a second literal that drifts. Same export_*_test.go seam idiom the
// rest of this package uses. Read the comment above for what the budget is for.
const ExportedDaemonExitHangBudget = daemonExitHangBudget
