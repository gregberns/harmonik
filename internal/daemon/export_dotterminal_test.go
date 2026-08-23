package daemon

import (
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/runloop"
)

// ExportedDotNodeTerminalFailure exposes dotNodeTerminalFailure for tests in
// package daemon_test. It takes the production types verbatim rather than a
// mirror struct, so a field added to runloop.ExitInfo is visible to the table
// the moment it exists.
func ExportedDotNodeTerminalFailure(
	sessionID string,
	exit runloop.ExitInfo,
	socketOutcome *handler.ExportedOutcomeEmittedPayload,
	watcherErr error,
) (string, bool) {
	return dotNodeTerminalFailure(sessionID, exit, socketOutcome, watcherErr)
}

// ExportedAnnouncementIsCleanExit exposes announcementIsCleanExit for tests in
// package daemon_test. The composition it performs is unreachable end to end:
// a run fixture can drive an agent that announces and is killed for it, and
// cannot drive one that announces AND trips a watchdog in the same window.
func ExportedAnnouncementIsCleanExit(announcedEnd, killedForFailure bool) bool {
	return announcementIsCleanExit(announcedEnd, killedForFailure)
}
