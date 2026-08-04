package brcli_test

// terminaltransition_claimguard_test.go — the ClaimBead compare-and-set guard.
//
// `br update <bead_id> --claim` is a compare-and-set. It sets assignee=actor
// and status=in_progress together, and it refuses with exit 4 ("already
// assigned to <name>") when the bead already has an assignee. ClaimBead keeps a
// fallback to `br update <bead_id> --status in_progress` for the one legitimate
// refusal (hk-amed0: a crew wrote `br create --assignee <crew>`, so an OPEN
// bead carries a routing assignee). The fallback must never run when another
// party holds the bead — a blind status write there defeats the compare-and-set.
//
// KNOWN LIMIT, pinned by TestClaimBeadReportsSuccessForBeadAlreadyInProgress
// below: the gate never sees a bead at in_progress, which is the state a live
// holder is actually in. terminalTransitionWrite short-circuits that case to a
// nil return before the gate runs, so ClaimBead reports SUCCESS there. No write
// goes out, so nothing is taken, but the caller then dispatches a second run
// onto a live bead. That hole is in the shared idempotency check, not in this
// fallback, and this file does not claim to close it.
//
// These tests drive ClaimBead against a mock `br` that logs every argv it
// receives. They assert on the argv log, so a test fails if the guard lets a
// takeover write reach `br`, and fails if the guard blocks the legitimate case.
//
// Bead ref: hk-amed0.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
)

// claimGuardMock is a mock `br` binary that answers the two commands the claim
// path issues, and appends every invocation's argv to a log file.
type claimGuardMock struct {
	brPath  string
	argvLog string
}

// claimGuardNewMock writes the mock `br` binary.
//
// showJSON is the stdout of `br show <id> --format json`. When showJSON is
// empty the mock makes `br show` fail with exit 1, which models a bead whose
// holder cannot be read.
//
// `br update ... --claim` always exits 4 with the real refusal text.
// `br update ... --status in_progress` always exits 0.
func claimGuardNewMock(t *testing.T, showJSON string) claimGuardMock {
	t.Helper()
	dir := t.TempDir()
	brPath := filepath.Join(dir, "br")
	argvLog := filepath.Join(dir, "argv.log")
	showOut := filepath.Join(dir, "show.json")

	if err := os.WriteFile(showOut, []byte(showJSON), 0o600); err != nil {
		t.Fatalf("claimGuardNewMock: write show fixture: %v", err)
	}

	showBody := fmt.Sprintf("cat %q\n    exit 0\n", showOut)
	if showJSON == "" {
		showBody = "printf '%s' 'br show: unreadable' >&2\n    exit 1\n"
	}

	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" >> %q
case "$1" in
  show)
    %s
    ;;
  update)
    case "$*" in
      *--claim*)
        printf '%%s' "Validation failed: claim: issue hk-guard already assigned to chani" >&2
        exit 4
        ;;
      *)
        exit 0
        ;;
    esac
    ;;
esac
exit 0
`, argvLog, showBody)

	//nolint:gosec // G306: mock binary fixture; permissive mode required for executability
	if err := os.WriteFile(brPath, []byte(script), 0o700); err != nil {
		t.Fatalf("claimGuardNewMock: write mock br: %v", err)
	}
	return claimGuardMock{brPath: brPath, argvLog: argvLog}
}

// invocations returns every argv line the mock recorded, in order.
func (m claimGuardMock) invocations(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(m.argvLog)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("claimGuardMock.invocations: read argv log: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}

// sawStatusWrite reports whether the mock ever received the blind
// `update <id> --status in_progress` fallback write.
func (m claimGuardMock) sawStatusWrite(t *testing.T) bool {
	t.Helper()
	for _, line := range m.invocations(t) {
		if strings.HasPrefix(line, "update ") && strings.Contains(line, "--status in_progress") {
			return true
		}
	}
	return false
}

// claimGuardShowJSON builds a one-element `br show --format json` array with
// the given status and assignee.
func claimGuardShowJSON(status, assignee string) string {
	return fmt.Sprintf(
		`[{"id":"hk-guard","title":"guard fixture","description":"","status":%q,`+
			`"issue_type":"task","labels":[],"dependencies":[],"dependents":[],"assignee":%q}]`,
		status, assignee,
	)
}

// claimGuardCall runs ClaimBead against the mock and returns its error.
func claimGuardCall(t *testing.T, m claimGuardMock) error {
	t.Helper()
	adapter, err := brcli.New(m.brPath)
	if err != nil {
		t.Fatalf("brcli.New: %v", err)
	}
	cfg := brcli.TimeoutConfig{
		WriteTimeout:            5 * time.Second,
		ReadTimeout:             5 * time.Second,
		TerminalWriteMaxRetries: 1,
		TerminalWriteRetryBase:  time.Millisecond,
		TerminalWriteRetryCap:   time.Millisecond,
	}
	return adapter.ClaimBead(
		context.Background(),
		t.TempDir(),
		cfg,
		core.RunID(uuid.Must(uuid.NewV7())),
		core.TransitionID(uuid.Must(uuid.NewV7())),
		core.BeadID("hk-guard"),
	)
}

// TestClaimBeadFallbackRunsForOpenPreAssignedBead pins the hk-amed0 case that
// the fallback exists for: a crew created the bead with `br create --assignee
// <crew>`, so the bead is OPEN but carries an assignee. No run holds it, so
// ClaimBead must take it and the status write must reach `br`.
func TestClaimBeadFallbackRunsForOpenPreAssignedBead(t *testing.T) {
	t.Parallel()
	mock := claimGuardNewMock(t, claimGuardShowJSON("open", "chani"))

	if err := claimGuardCall(t, mock); err != nil {
		t.Fatalf("ClaimBead on an open pre-assigned bead: want nil error, got %v", err)
	}
	if !mock.sawStatusWrite(t) {
		t.Fatalf("ClaimBead did not issue the --status in_progress fallback; argv log: %v", mock.invocations(t))
	}
}

// TestClaimBeadRefusesTakeoverOfBeadHeldByAnotherActor is the defect test.
// The bead is assigned to another actor and has already moved past open, so
// the "already assigned" refusal guards a real holder. ClaimBead must return
// the claim error and must issue NO status write.
//
// Without the guard ClaimBead swallows the refusal and writes
// `update hk-guard --status in_progress`, taking the bead from its holder.
func TestClaimBeadRefusesTakeoverOfBeadHeldByAnotherActor(t *testing.T) {
	t.Parallel()
	for _, status := range []string{"blocked", "closed"} {
		t.Run(status, func(t *testing.T) {
			t.Parallel()
			mock := claimGuardNewMock(t, claimGuardShowJSON(status, "chani"))

			err := claimGuardCall(t, mock)
			if err == nil {
				t.Fatalf("ClaimBead on a bead held by another actor at status %s: want error, got nil", status)
			}
			if !strings.Contains(err.Error(), "already assigned") {
				t.Errorf("ClaimBead error does not carry the original claim refusal: %v", err)
			}
			if !strings.Contains(err.Error(), "chani") {
				t.Errorf("ClaimBead error does not name the holder: %v", err)
			}
			if mock.sawStatusWrite(t) {
				t.Fatalf("ClaimBead issued the --status in_progress fallback and took a bead held by another actor; argv log: %v", mock.invocations(t))
			}
		})
	}
}

// TestClaimBeadReportsSuccessForBeadAlreadyInProgress pins the KNOWN LIMIT
// described in the file header. It records what ClaimBead does today, not what
// it should do.
//
// in_progress is the state a live holder is actually in, so this is the case an
// operator would expect the compare-and-set to protect hardest. It is also the
// one case the guard never sees. terminalTransitionWrite checks the bead after
// the refusal and returns nil when the bead already sits at the intended
// post-state, and in_progress IS the intended post-state of a claim. ClaimBead
// therefore returns SUCCESS for a bead another actor is running.
//
// The guard's own promise still holds: no status write reaches `br`, so nothing
// is taken. The harm is downstream — the caller reads success and dispatches a
// second run onto a live bead. Closing that needs a change to the shared
// idempotency check, which is out of scope for the claim fallback.
//
// If this test ever fails because ClaimBead started returning an error here,
// that is a fix, not a regression. Update the test and drop the KNOWN LIMIT
// notes in terminaltransition_bi010.go.
func TestClaimBeadReportsSuccessForBeadAlreadyInProgress(t *testing.T) {
	t.Parallel()
	mock := claimGuardNewMock(t, claimGuardShowJSON("in_progress", "chani"))

	err := claimGuardCall(t, mock)
	if err != nil {
		t.Fatalf("ClaimBead on a bead already in_progress: want nil (documented current behaviour), got %v", err)
	}
	if mock.sawStatusWrite(t) {
		t.Fatalf("ClaimBead issued the --status in_progress fallback for a bead another actor is running; argv log: %v", mock.invocations(t))
	}
}

// TestClaimBeadRefusesTakeoverWhenHolderIsUnreadable covers the fail-closed
// branch. `br show` fails, so the adapter cannot see who holds the bead. An
// unknown holder counts as a different holder: ClaimBead must return the claim
// error and issue no status write.
func TestClaimBeadRefusesTakeoverWhenHolderIsUnreadable(t *testing.T) {
	t.Parallel()
	mock := claimGuardNewMock(t, "")

	err := claimGuardCall(t, mock)
	if err == nil {
		t.Fatal("ClaimBead with an unreadable holder: want error, got nil")
	}
	if !strings.Contains(err.Error(), "already assigned") {
		t.Errorf("ClaimBead error does not carry the original claim refusal: %v", err)
	}
	if mock.sawStatusWrite(t) {
		t.Fatalf("ClaimBead issued the --status in_progress fallback with an unreadable holder; argv log: %v", mock.invocations(t))
	}
}
