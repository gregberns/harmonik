package runmerge

import "strings"

// IsRetryablePushRejection reports whether a failed `git push` was refused for
// a reason worth re-preparing for, rather than one that will refuse again
// however many times it is tried.
//
// hk-lhdqo. This used to test for "non-fast-forward" or "[rejected]" only, and
// it therefore missed the most common case in production — a LOST CONCURRENT
// PUSH, which happens whenever two runs finish close together and both push the
// target branch. Git refuses the loser with:
//
//	remote: error: cannot lock ref 'refs/heads/main':
//	  is at 11198a81... but expected ade9d761...
//	! [remote rejected] main -> main (failed to update ref)
//
// "[remote rejected]" does NOT contain the substring "[rejected]" — there is a
// "remote " between the bracket and the word — so the old test called a
// recoverable race terminal on attempt 1, and a routine race permanently failed
// the bead. Measured 2 in 357 runs on a 2-bead fixture and 1 in 60 at
// max_concurrent 6; the rate scales with concurrency, as a push race should.
// The wording above was reproduced twice at the git level, once by holding a
// stale ref lock and once by racing two clones against a bare remote.
//
// The set is deliberately NOT the bare "[remote rejected]". That token also
// carries refusals that will never clear — a declined pre-receive hook, a
// shallow update, a prohibited deletion — and re-preparing for those spends the
// budget without changing the answer.
//
// It is not a clean partition, and the imprecision is priced rather than
// overlooked. "failed to update ref" is git's generic ref-transaction bucket
// and also fires on a permanent directory/file refname conflict; "cannot lock
// ref" also fires on a stale lock file that no re-fetch clears. Both cost the
// 3-attempt budget and then fail with git's real output. The alternative is a
// predicate that separates a lost race from a stale lock by wording, which is
// likelier to misclassify the case that actually matters. See
// staleRefLockOutput in pushrejectclassifier_test.go, which pins the overlap so
// a later reader does not remove it without knowing it was a choice.
//
// One recoverable case is still missed, and is currently unreachable:
// gitPushOrigin passes no --atomic, and an --atomic push that loses a ref lock
// reports "atomic transaction failed", which matches nothing here.
func IsRetryablePushRejection(pushOut string) bool {
	for _, token := range []string{
		"non-fast-forward",
		"[rejected]",
		"failed to update ref",
		"cannot lock ref",
		"fetch first",
	} {
		if strings.Contains(pushOut, token) {
			return true
		}
	}
	return false
}
