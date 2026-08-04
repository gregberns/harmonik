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
// The same safety property has a second half one layer up, in
// terminalTransitionWrite. A refused write whose bead already sits at the
// claim's intended post-state (in_progress) used to count as an idempotent
// success, which swallowed the commonest takeover of all before the gate above
// ever ran. That credit is now restricted to beads whose ownership sentinel we
// hold. The paired tests are TestClaimBeadRefusesBeadRunningUnderAnotherActor
// and TestClaimBeadIsIdempotentForBeadWeAlreadyOwn.
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
	brPath     string
	argvLog    string
	projectDir string
}

// ownBead plants the beads-owned ownership sentinel for beadID, which is what
// a previously successful ClaimBead of ours would have left behind.
func (m claimGuardMock) ownBead(t *testing.T, beadID string) {
	t.Helper()
	dir := filepath.Join(m.projectDir, ".harmonik", "beads-owned")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("claimGuardMock.ownBead: mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, beadID), nil, 0o600); err != nil {
		t.Fatalf("claimGuardMock.ownBead: write sentinel: %v", err)
	}
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
  close)
    printf '%%s' "close refused" >&2
    exit 2
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
	return claimGuardMock{brPath: brPath, argvLog: argvLog, projectDir: t.TempDir()}
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
	// NewForProject, not New: the ownership sentinel lives under projectDir, and
	// every production caller builds the adapter this way.
	adapter, err := brcli.NewForProject(m.brPath, m.projectDir)
	if err != nil {
		t.Fatalf("brcli.NewForProject: %v", err)
	}
	cfg := brcli.TimeoutConfig{
		WriteTimeout:            30 * time.Second,
		ReadTimeout:             30 * time.Second,
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

// TestClaimBeadRefusesBeadRunningUnderAnotherActor is the decisive case.
//
// in_progress is the state a live holder is actually in, so this is what an
// operator expects the compare-and-set to protect hardest. It used to be the
// one case that slipped through everything: terminalTransitionWrite saw the
// bead already at the claim's intended post-state and returned nil, so
// ClaimBead reported SUCCESS for a bead another actor was running and the
// caller dispatched a second run onto it.
//
// We do not hold the ownership sentinel for this bead, so it is not ours.
// ClaimBead must return an error and must issue no status write.
func TestClaimBeadRefusesBeadRunningUnderAnotherActor(t *testing.T) {
	t.Parallel()
	mock := claimGuardNewMock(t, claimGuardShowJSON("in_progress", "chani"))
	// Deliberately no mock.ownBead call — no sentinel, so the bead is not ours.

	err := claimGuardCall(t, mock)
	if err == nil {
		t.Fatal("ClaimBead on a bead another actor is running: want error, got nil")
	}
	if !strings.Contains(err.Error(), "already assigned") {
		t.Errorf("ClaimBead error does not carry the original claim refusal: %v", err)
	}
	if mock.sawStatusWrite(t) {
		t.Fatalf("ClaimBead issued the --status in_progress fallback for a bead another actor is running; argv log: %v", mock.invocations(t))
	}
}

// TestClaimBeadIsIdempotentForBeadWeAlreadyOwn is the other half of the pair.
// It stops the sentinel gate from being over-tightened into a wedge.
//
// The bead is at in_progress and we DO hold its ownership sentinel, so a
// previous ClaimBead of ours already succeeded on it. This is a genuine
// self-retry, not a takeover, and it must stay idempotent: ClaimBead returns
// success and issues no second write.
func TestClaimBeadIsIdempotentForBeadWeAlreadyOwn(t *testing.T) {
	t.Parallel()
	mock := claimGuardNewMock(t, claimGuardShowJSON("in_progress", "chani"))
	mock.ownBead(t, "hk-guard")

	if err := claimGuardCall(t, mock); err != nil {
		t.Fatalf("ClaimBead on a bead we already own: want nil, got %v", err)
	}
	if mock.sawStatusWrite(t) {
		t.Fatalf("ClaimBead issued a redundant --status in_progress write for a bead we already own; argv log: %v", mock.invocations(t))
	}
}

// TestCloseBeadIdempotencyIsNotGatedBySentinel pins the SCOPE of the ownership
// gate. Only the claim op is gated.
//
// Claim is special because its post-state, in_progress, is exclusive: it names
// one owner running one bead, so an unexplained in_progress belongs to somebody
// else. Close, reopen and reset drive toward a SHARED end state. Whoever got
// the bead to closed, the caller wanted it closed, so the wave-race idempotency
// credit (hk-cw4sx) is sound for them and must stay unconditional.
//
// Here `br close` is refused but the bead already reads closed, and we hold no
// ownership sentinel. CloseBead must still report success. Extending the
// sentinel gate to every op would break this and re-open hk-cw4sx.
func TestCloseBeadIdempotencyIsNotGatedBySentinel(t *testing.T) {
	t.Parallel()
	mock := claimGuardNewMock(t, claimGuardShowJSON("closed", "chani"))
	// Deliberately no ownBead call — no sentinel for this bead.

	adapter, err := brcli.NewForProject(mock.brPath, mock.projectDir)
	if err != nil {
		t.Fatalf("brcli.NewForProject: %v", err)
	}
	cfg := brcli.TimeoutConfig{
		WriteTimeout:            30 * time.Second,
		ReadTimeout:             30 * time.Second,
		TerminalWriteMaxRetries: 1,
		TerminalWriteRetryBase:  time.Millisecond,
		TerminalWriteRetryCap:   time.Millisecond,
	}
	closeErr := adapter.CloseBead(
		context.Background(),
		t.TempDir(),
		cfg,
		core.RunID(uuid.Must(uuid.NewV7())),
		core.TransitionID(uuid.Must(uuid.NewV7())),
		core.BeadID("hk-guard"),
		false,
	)
	if closeErr != nil {
		t.Fatalf("CloseBead on a bead already closed by another writer: want nil (wave-race idempotency), got %v", closeErr)
	}
}

// TestClaimBeadCreditsLostAcknowledgementWithinOneCall covers the case that
// makes a refusal untrustworthy as proof.
//
// RunWithDBLockedRetry makes several `br` attempts inside ONE call and retries
// a wall-clock-timeout kill. br can commit the claim and then be killed before
// it acknowledges (hk-5dewt / hk-yjsk8). The next attempt reissues `--claim`
// and br refuses it — because OUR OWN earlier attempt landed. So a refusal does
// not prove that nothing of ours landed, and the bead now at in_progress may be
// ours with no sentinel written yet.
//
// Here attempt 1 is KILLED at the wall-clock deadline, and attempt 2 is refused
// with "already assigned to us". ClaimBead must report success. Refusing here
// would turn a lost acknowledgement into a bead nobody can claim: the daemon
// retries, gets the same refusal every time, and the queue item dies at
// max_attempts_exceeded.
//
// The transient class matters and the test must use the RIGHT one. A timeout
// kill is the only class that leaves doubt, because br may have committed
// before it died. A db-locked exit does NOT belong here: br gave up on the
// write lock and wrote nothing, so a refusal after it cannot be our own write.
// An earlier draft of this test used a db-locked exit while its prose narrated
// a timeout kill, which quietly turned it into an assertion that ClaimBead
// succeeds on a takeover. A reviewer caught that. Do not swap the exit code
// back in to make this test faster.
//
// Determinism: ONLY attempt 1 sleeps, gated on the counter file, and it does so
// via `exec` so the deadline signal reaches the sleeping process directly
// instead of a shell that outlives it. Attempt 2 answers immediately and has
// the whole write budget to do it in. An even earlier draft slept on EVERY
// attempt, so under parallel load all of them timed out, the call exhausted to
// the BrUnavailable branch, and the test passed for the wrong reason.
//
// The write budget is deliberately WIDE. It is not a latency assertion. It only
// has to outlast process startup so the mock records its argv before the
// deadline kills it. A 1s budget flaked about one run in four on a loaded box,
// because the shell had not reached its first write when the signal arrived.
// Keep this generous. The test still finishes in about the budget, once.
func TestClaimBeadCreditsLostAcknowledgementWithinOneCall(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	brPath := filepath.Join(dir, "br")
	argvLog := filepath.Join(dir, "argv.log")
	showOut := filepath.Join(dir, "show.json")
	counter := filepath.Join(dir, "claim.count")

	// The bead reads in_progress: attempt 1 landed before it was killed.
	if err := os.WriteFile(showOut, []byte(claimGuardShowJSON("in_progress", "us")), 0o600); err != nil {
		t.Fatalf("write show fixture: %v", err)
	}

	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" >> %q
case "$1" in
  show)
    cat %q
    exit 0
    ;;
  update)
    case "$*" in
      *--claim*)
        n=0
        [ -f %q ] && n=$(cat %q)
        n=$((n+1))
        printf '%%s' "$n" > %q
        if [ "$n" -eq 1 ]; then
          exec sleep 300
        fi
        printf '%%s' "Validation failed: claim: issue hk-guard already assigned to us" >&2
        exit 4
        ;;
      *)
        exit 0
        ;;
    esac
    ;;
esac
exit 0
`, argvLog, showOut, counter, counter, counter)

	//nolint:gosec // G306: mock binary fixture; permissive mode required for executability
	if err := os.WriteFile(brPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write mock br: %v", err)
	}

	mock := claimGuardMock{brPath: brPath, argvLog: argvLog, projectDir: t.TempDir()}
	// Deliberately no ownBead call: attempt 1 never returned success, so
	// ClaimBead never got to write the sentinel.

	adapter, err := brcli.NewForProject(brPath, mock.projectDir)
	if err != nil {
		t.Fatalf("brcli.NewForProject: %v", err)
	}
	cfg := brcli.TimeoutConfig{
		WriteTimeout:            10 * time.Second,
		ReadTimeout:             30 * time.Second,
		TerminalWriteMaxRetries: 3,
		TerminalWriteRetryBase:  time.Millisecond,
		TerminalWriteRetryCap:   5 * time.Millisecond,
	}
	claimErr := adapter.ClaimBead(
		context.Background(),
		t.TempDir(),
		cfg,
		core.RunID(uuid.Must(uuid.NewV7())),
		core.TransitionID(uuid.Must(uuid.NewV7())),
		core.BeadID("hk-guard"),
	)
	if claimErr != nil {
		t.Fatalf("ClaimBead after a lost acknowledgement within one call: want nil, got %v", claimErr)
	}
	if mock.sawStatusWrite(t) {
		t.Fatalf("ClaimBead issued a blind status write; argv log: %v", mock.invocations(t))
	}
	// Guard the test itself: the call must have reached the REFUSED-write branch,
	// not exhausted its retries to the BrUnavailable branch. Exactly two claim
	// attempts means attempt 2 answered with the refusal.
	claims := 0
	for _, line := range mock.invocations(t) {
		if strings.Contains(line, "--claim") {
			claims++
		}
	}
	if claims != 2 {
		t.Skipf("the machine could not run the mock br inside its write budget: want exactly 2 claim attempts "+
			"(timeout kill then refusal), got %d; argv log: %v", claims, mock.invocations(t))
	}
}

// TestClaimBeadRefusesTakeoverAfterDbLockedRetry is the counterexample a
// reviewer built against an earlier version of this gate, kept as a test.
//
// The earlier gate credited the claim whenever the call made more than one
// attempt. That conflated two transient classes with opposite meaning. Here
// attempt 1 exits 3, a locked database: br gave up on the write lock and wrote
// NOTHING. Attempt 2 is refused because "chani" holds the bead and is running
// it. Under the attempt-count gate this returned nil SUCCESS — the exact
// takeover this file exists to stop, reachable under the ordinary SQLite
// contention the retry loop is built to absorb.
//
// A db-locked retry must NOT credit the claim. ClaimBead must return an error
// and issue no status write.
func TestClaimBeadRefusesTakeoverAfterDbLockedRetry(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	brPath := filepath.Join(dir, "br")
	argvLog := filepath.Join(dir, "argv.log")
	showOut := filepath.Join(dir, "show.json")
	counter := filepath.Join(dir, "claim.count")

	// Another actor holds the bead and is running it.
	if err := os.WriteFile(showOut, []byte(claimGuardShowJSON("in_progress", "chani")), 0o600); err != nil {
		t.Fatalf("write show fixture: %v", err)
	}

	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" >> %q
case "$1" in
  show)
    cat %q
    exit 0
    ;;
  update)
    case "$*" in
      *--claim*)
        n=0
        [ -f %q ] && n=$(cat %q)
        n=$((n+1))
        printf '%%s' "$n" > %q
        if [ "$n" -eq 1 ]; then
          printf '%%s' "database is locked" >&2
          exit 3
        fi
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
`, argvLog, showOut, counter, counter, counter)

	//nolint:gosec // G306: mock binary fixture; permissive mode required for executability
	if err := os.WriteFile(brPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write mock br: %v", err)
	}

	mock := claimGuardMock{brPath: brPath, argvLog: argvLog, projectDir: t.TempDir()}
	// No sentinel: we have never claimed this bead.

	adapter, err := brcli.NewForProject(brPath, mock.projectDir)
	if err != nil {
		t.Fatalf("brcli.NewForProject: %v", err)
	}
	cfg := brcli.TimeoutConfig{
		WriteTimeout:            30 * time.Second,
		ReadTimeout:             30 * time.Second,
		TerminalWriteMaxRetries: 3,
		TerminalWriteRetryBase:  time.Millisecond,
		TerminalWriteRetryCap:   5 * time.Millisecond,
	}
	claimErr := adapter.ClaimBead(
		context.Background(),
		t.TempDir(),
		cfg,
		core.RunID(uuid.Must(uuid.NewV7())),
		core.TransitionID(uuid.Must(uuid.NewV7())),
		core.BeadID("hk-guard"),
	)
	if claimErr == nil {
		t.Fatal("ClaimBead took a bead another actor runs after a db-locked retry: want error, got nil")
	}
	if !strings.Contains(claimErr.Error(), "already assigned") {
		t.Errorf("ClaimBead error does not carry the original claim refusal: %v", claimErr)
	}
	if mock.sawStatusWrite(t) {
		t.Fatalf("ClaimBead issued the --status in_progress fallback; argv log: %v", mock.invocations(t))
	}
}

// TestClaimBeadRefusesTakeoverWhenEveryAttemptIsDbLocked is a reviewer's
// counterexample against the retry-exhaustion branch, kept as a test.
//
// The retry loop wraps BrUnavailable around BOTH of its escalations, including
// the one where every attempt exited 3 with a locked database. So
// "BrUnavailable" does not mean "a timeout happened". While that branch was
// ungated, a call whose attempts ALL exited 3 — br holding no write lock and
// writing nothing, ever — credited itself with a bead another actor was
// running, and ClaimBead then planted the ownership sentinel, which made every
// later claim on that bead sail through.
//
// Every attempt exits 3, the bead reads in_progress under "chani", and we hold
// no sentinel. ClaimBead must return an error, issue no status write, and leave
// no sentinel behind.
func TestClaimBeadRefusesTakeoverWhenEveryAttemptIsDbLocked(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	brPath := filepath.Join(dir, "br")
	argvLog := filepath.Join(dir, "argv.log")
	showOut := filepath.Join(dir, "show.json")

	if err := os.WriteFile(showOut, []byte(claimGuardShowJSON("in_progress", "chani")), 0o600); err != nil {
		t.Fatalf("write show fixture: %v", err)
	}

	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" >> %q
case "$1" in
  show)
    cat %q
    exit 0
    ;;
  update)
    case "$*" in
      *--claim*)
        printf '%%s' "database is locked" >&2
        exit 3
        ;;
      *)
        exit 0
        ;;
    esac
    ;;
esac
exit 0
`, argvLog, showOut)

	//nolint:gosec // G306: mock binary fixture; permissive mode required for executability
	if err := os.WriteFile(brPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write mock br: %v", err)
	}

	mock := claimGuardMock{brPath: brPath, argvLog: argvLog, projectDir: t.TempDir()}
	adapter, err := brcli.NewForProject(brPath, mock.projectDir)
	if err != nil {
		t.Fatalf("brcli.NewForProject: %v", err)
	}
	cfg := brcli.TimeoutConfig{
		WriteTimeout:            30 * time.Second,
		ReadTimeout:             30 * time.Second,
		TerminalWriteMaxRetries: 2,
		TerminalWriteRetryBase:  time.Millisecond,
		TerminalWriteRetryCap:   time.Millisecond,
	}
	claimErr := adapter.ClaimBead(
		context.Background(),
		t.TempDir(),
		cfg,
		core.RunID(uuid.Must(uuid.NewV7())),
		core.TransitionID(uuid.Must(uuid.NewV7())),
		core.BeadID("hk-guard"),
	)
	if claimErr == nil {
		t.Fatal("ClaimBead took a bead another actor runs after exhausting on db-locked: want error, got nil")
	}
	if mock.sawStatusWrite(t) {
		t.Fatalf("ClaimBead issued the --status in_progress fallback; argv log: %v", mock.invocations(t))
	}
	// A wrong credit is self-confirming, because ClaimBead plants the sentinel
	// after a nil return. Prove none was planted.
	sentinel := filepath.Join(mock.projectDir, ".harmonik", "beads-owned", "hk-guard")
	if _, statErr := os.Stat(sentinel); !os.IsNotExist(statErr) {
		t.Fatalf("ClaimBead planted an ownership sentinel for a bead it did not claim (stat err = %v)", statErr)
	}
}

// TestReissueTerminalTransitionKeepsSentinelInStep covers the crash-recovery
// re-drive path.
//
// ReissueTerminalTransition re-issues a terminal write at boot from a stale
// intent file. It re-drives the same ops ClaimBead, CloseBead, ReopenBead and
// ResetBead issue, so it has to do the same ownership-sentinel bookkeeping. If
// it did not, a re-driven close would strand a sentinel and let a later claim
// credit itself with it, and a re-driven claim would leave no sentinel and let
// a later self-retry be refused.
func TestReissueTerminalTransitionKeepsSentinelInStep(t *testing.T) {
	t.Parallel()

	newAdapter := func(t *testing.T) (*brcli.Adapter, string) {
		t.Helper()
		brPath := filepath.Join(t.TempDir(), "br")
		//nolint:gosec // G306: mock binary fixture; permissive mode required for executability
		if err := os.WriteFile(brPath, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
			t.Fatalf("write mock br: %v", err)
		}
		projectDir := t.TempDir()
		adapter, err := brcli.NewForProject(brPath, projectDir)
		if err != nil {
			t.Fatalf("brcli.NewForProject: %v", err)
		}
		return adapter, projectDir
	}

	// plantIntent writes the stale intent file that a crashed prior run would
	// have left on disk, and returns it with the directory holding it.
	// ReissueTerminalTransition starts at BI-030 step 5 and expects steps 1-4 to
	// have happened already.
	plantIntent := func(t *testing.T, op core.TerminalOp, post core.CoarseStatus) (core.IntentLogEntry, string) {
		t.Helper()
		runID := core.RunID(uuid.Must(uuid.NewV7()))
		tid := core.TransitionID(uuid.Must(uuid.NewV7()))
		entry := core.IntentLogEntry{
			IdempotencyKey:    core.IdempotencyKey(runID, tid, op),
			RunID:             runID,
			TransitionID:      tid,
			Op:                op,
			BeadID:            core.BeadID("hk-guard"),
			IntendedPostState: post,
			RequestedAt:       time.Now().UTC(),
			SchemaVersion:     brcli.IntentLogEntrySchemaVersion,
		}
		intentDir := t.TempDir()
		tmpPath, err := brcli.WriteIntentLogTmp(intentDir, entry)
		if err != nil {
			t.Fatalf("WriteIntentLogTmp: %v", err)
		}
		if _, err := brcli.RenameIntentLogTmpToFinal(tmpPath, intentDir, entry.IdempotencyKey); err != nil {
			t.Fatalf("RenameIntentLogTmpToFinal: %v", err)
		}
		return entry, intentDir
	}

	t.Run("claim writes the sentinel", func(t *testing.T) {
		t.Parallel()
		adapter, projectDir := newAdapter(t)
		entry, intentDir := plantIntent(t, core.TerminalOpClaim, core.CoarseStatusInProgress)
		if err := adapter.ReissueTerminalTransition(
			context.Background(), intentDir, brcli.TimeoutConfig{}, entry,
		); err != nil {
			t.Fatalf("ReissueTerminalTransition claim: %v", err)
		}
		sentinel := filepath.Join(projectDir, ".harmonik", "beads-owned", "hk-guard")
		if _, statErr := os.Stat(sentinel); statErr != nil {
			t.Fatalf("re-driven claim did not write the ownership sentinel: %v; a later self-retry would be refused", statErr)
		}
	})

	for _, tc := range []struct {
		name string
		op   core.TerminalOp
		post core.CoarseStatus
	}{
		{"close", core.TerminalOpClose, core.CoarseStatusClosed},
		{"reopen", core.TerminalOpReopen, core.CoarseStatusOpen},
		{"reset", core.TerminalOpReset, core.CoarseStatusOpen},
	} {
		t.Run(tc.name+" clears the sentinel", func(t *testing.T) {
			t.Parallel()
			adapter, projectDir := newAdapter(t)
			ownedDir := filepath.Join(projectDir, ".harmonik", "beads-owned")
			if err := os.MkdirAll(ownedDir, 0o700); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			sentinel := filepath.Join(ownedDir, "hk-guard")
			if err := os.WriteFile(sentinel, nil, 0o600); err != nil {
				t.Fatalf("plant sentinel: %v", err)
			}
			entry, intentDir := plantIntent(t, tc.op, tc.post)
			if err := adapter.ReissueTerminalTransition(
				context.Background(), intentDir, brcli.TimeoutConfig{}, entry,
			); err != nil {
				t.Fatalf("ReissueTerminalTransition %s: %v", tc.name, err)
			}
			if _, statErr := os.Stat(sentinel); !os.IsNotExist(statErr) {
				t.Fatalf("re-driven %s left the ownership sentinel behind (stat err = %v); a later claim would credit itself with it", tc.name, statErr)
			}
		})
	}
}

// TestSweepCloseBeadDeletesOwnershipSentinel closes a stale-sentinel hazard.
//
// SweepCloseBead is the Cat 3c auto-close path. It closes a bead, so our
// ownership ends there exactly as it does in CloseBead. If it left the sentinel
// behind, a later claim of the same bead after a reopen would find our stale
// marker, credit itself through postStateIsOurs, and take a bead another actor
// holds — the very defect this file exists to prevent.
func TestSweepCloseBeadDeletesOwnershipSentinel(t *testing.T) {
	t.Parallel()
	mock := claimGuardNewMock(t, claimGuardShowJSON("closed", ""))
	mock.ownBead(t, "hk-guard")

	sentinel := filepath.Join(mock.projectDir, ".harmonik", "beads-owned", "hk-guard")
	if _, statErr := os.Stat(sentinel); statErr != nil {
		t.Fatalf("sentinel fixture missing before sweep: %v", statErr)
	}

	// The shared mock refuses `close` with exit 2, so use a mock that accepts it.
	brPath := filepath.Join(t.TempDir(), "br")
	//nolint:gosec // G306: mock binary fixture; permissive mode required for executability
	if err := os.WriteFile(brPath, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("write mock br: %v", err)
	}
	adapter, err := brcli.NewForProject(brPath, mock.projectDir)
	if err != nil {
		t.Fatalf("brcli.NewForProject: %v", err)
	}

	if sweepErr := adapter.SweepCloseBead(
		context.Background(),
		brcli.TimeoutConfig{WriteTimeout: 30 * time.Second, ReadTimeout: 30 * time.Second},
		core.BeadID("hk-guard"),
	); sweepErr != nil {
		t.Fatalf("SweepCloseBead: %v", sweepErr)
	}

	if _, statErr := os.Stat(sentinel); !os.IsNotExist(statErr) {
		t.Fatalf("SweepCloseBead left the ownership sentinel behind (stat err = %v); a later claim would credit itself with it", statErr)
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
