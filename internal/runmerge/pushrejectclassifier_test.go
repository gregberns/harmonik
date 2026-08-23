package runmerge

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const lostPushRaceOutput = `remote: error: cannot lock ref 'refs/heads/main': is at 11198a81bd5c9a4f70cd39718da1807e97f5a82c but expected ade9d761fa0df4e7dc92598d6bcbe8a106c66c03        
To /tmp/remote.git
 ! [remote rejected] main -> main (failed to update ref)
error: failed to push some refs to '/tmp/remote.git'
`

const staleRefLockOutput = `remote: error: cannot lock ref 'refs/heads/main': Unable to create '/tmp/remote.git/./refs/heads/main.lock': File exists.        
remote: 
remote: Another git process seems to be running in this repository, e.g.        
remote: an editor opened by 'git commit'. Please make sure all processes        
remote: are terminated then try again.        
To ../remote.git
 ! [remote rejected] main -> main (failed to update ref)
error: failed to push some refs to '../remote.git'
`

// TestIsRetryablePushRejection pins which push refusals route into the Phase-D
// re-fetch recovery. The negative rows matter as much as the positive ones: a
// permanent refusal retried three times buys nothing and reports the wrong
// reason.
func TestIsRetryablePushRejection(t *testing.T) {
	for _, tc := range []struct {
		name    string
		pushOut string
		want    bool
	}{
		{"lost push race, verbatim git output", lostPushRaceOutput, true},
		{"stale remote ref lock (accepted overlap)", staleRefLockOutput, true},
		{"stale local ref", " ! [rejected]        main -> main (fetch first)\n", true},
		{"non-fast-forward", " ! [rejected]        main -> main (non-fast-forward)\n", true},
		{"failed to update ref alone", " ! [remote rejected] main -> main (failed to update ref)\n", true},
		{"pre-receive hook declined", " ! [remote rejected] main -> main (pre-receive hook declined)\n", false},
		{"shallow update not allowed", " ! [remote rejected] main -> main (shallow update not allowed)\n", false},
		{"auth failure", "fatal: could not read Username for 'https://example.com': terminal prompts disabled\n", false},
		{"no network", "fatal: unable to access 'https://example.com/': Could not resolve host\n", false},
		{"empty", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsRetryablePushRejection(tc.pushOut); got != tc.want {
				t.Fatalf("IsRetryablePushRejection(%q) = %v, want %v", tc.pushOut, got, tc.want)
			}
		})
	}
}

// TestCommitHandlePushFailureRoutesLostRaceToRecovery checks the routing, not
// just the predicate: a lost push race must NOT come back as a terminal
// "push_failed:", which is what permanently failed the bead in hk-lhdqo.
//
// projectDir is a non-repo temp dir on purpose. The CAS rollback probe fails
// there and is skipped, and the recovery's `git fetch` also fails — so the
// retryable branch is identified by its distinct "push_failed_fetch" reason.
// That is enough to tell the two branches apart without a live remote; the full
// recovery is covered end-to-end by
// TestMergeToMain_ForcedLostPushRace_ReEntersAndRetries in internal/daemon.
func TestCommitHandlePushFailureRoutesLostRaceToRecovery(t *testing.T) {
	const maxPushAttempts = 3
	dir := t.TempDir()
	pushErr := errors.New("exit status 1")

	for _, tc := range []struct {
		name          string
		pushOut       string
		attempt       int
		wantsRecovery bool
	}{
		{"lost race, budget left", lostPushRaceOutput, 1, true},
		{"lost race, budget exhausted", lostPushRaceOutput, maxPushAttempts, false},
		{"hook decline is terminal at once", " ! [remote rejected] main -> main (pre-receive hook declined)\n", 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			co := commitHandlePushFailure(context.Background(), dir, "main", "prior", "advanced",
				[]byte(tc.pushOut), pushErr, tc.attempt, maxPushAttempts)
			if co.done == nil {
				t.Fatalf("want a terminal outcome from the non-repo dir, got retryKind=%v", co.retryKind)
			}
			gotRecovery := strings.HasPrefix(co.done.Reason, "push_failed_fetch")
			if gotRecovery != tc.wantsRecovery {
				t.Fatalf("reached recovery = %v, want %v; reason: %s", gotRecovery, tc.wantsRecovery, co.done.Reason)
			}
			if !tc.wantsRecovery && !strings.HasPrefix(co.done.Reason, "push_failed:") {
				t.Fatalf("want a terminal push_failed:, got: %s", co.done.Reason)
			}
		})
	}
}
