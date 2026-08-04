// Package core holds shared types that cross subsystem boundaries.
// No imports from any internal subsystem are permitted (see internal/core depguard rule).
package core

import "strings"

// RemoteEndpoint records the remote worker a run executed on
// (execution-model.md §6.1 RECORD RemoteEndpoint).
//
// The endpoint is present on a ReleaseClaim only for a remote run. It exists so
// that restart recovery synchronizes the run branch from the same worker that
// produced it. Recovery MUST use these recorded values. It MUST NOT select a
// worker again, because a second selection can pick a different machine and the
// committed work lives on one machine only (execution-model.md §4.7 EM-031b).
type RemoteEndpoint struct {
	// WorkerName is the stable worker identity chosen for the run.
	WorkerName string

	// Host is the SSH host or equivalent remote host identity.
	Host string

	// RepoPath is the absolute repository path on that worker.
	RepoPath string
}

// Valid reports whether every endpoint field carries a non-empty value.
//
// All three fields are required. A partly-filled endpoint is worse than an
// absent one: recovery would reach for a field that is not there and would then
// have to guess, which EM-031b forbids.
func (e RemoteEndpoint) Valid() bool {
	return e.WorkerName != "" && e.Host != "" && e.RepoPath != ""
}

// ReleaseClaim is the immutable record of the inputs a release needs
// (execution-model.md §6.1 RECORD ReleaseClaim, §4.7 EM-031b).
//
// # Why the claim exists
//
// A committed DOT run holds durable work on its task branch. Between that
// commit and the merge, close, or reopen that settles it, the daemon can stop.
// Nothing in git alone then says which target the release chose, or which
// worker holds the branch. The claim turns those inputs into git evidence, so
// restart recovery has one authority.
//
// # Immutability (EM-031b, §6.1)
//
// The claim's only durable representation is the transition-record sibling file
// inside the checkpoint commit. A later transition MUST NOT rewrite, replace,
// or reinterpret a prior claim. Immutability is structural: every transition
// owns a distinct record path derived from its own transition_id per EM-018, so
// a later transition writes a different file. WriteReleaseClaimCheckpoint adds
// an enforced guard on top of the structural one — it refuses to write over a
// record that already exists.
//
// # Reading a claim back
//
// Recovery MUST read the claim from git and read the current bead state, and
// use only those two sources. It MUST NOT read the JSONL event log, a
// daemon-local registry, or reconstructed process memory to supply, replace, or
// infer any claim field (EM-031b).
type ReleaseClaim struct {
	// DispatchHeadSHA is the task-branch head resolved at dispatch, before the
	// handler starts work. A branch tip ahead of this SHA on a non-terminal bead
	// is the evidence of an unfinished release (EM-031b).
	DispatchHeadSHA string

	// MergeTargetRef is the fully-qualified target ref selected for this
	// release, such as "refs/heads/main". It is fully qualified so that
	// recovery resolves the same ref the release chose, with no branch-name
	// lookup and no default in the middle.
	MergeTargetRef string

	// MergeTargetSHA is the SHA that MergeTargetRef resolved to when this claim
	// was written. It is durable audit evidence. It is NOT a precondition for a
	// later merge: the target moves while other runs land, and recovery merges
	// against the target as it stands then (execution-model.md §6.1).
	MergeTargetSHA string

	// RemoteEndpoint is nil for local work. For a remote run it names the
	// worker, host and repository path the release synchronization must use.
	RemoteEndpoint *RemoteEndpoint
}

// Valid reports whether the claim carries the values EM-031b requires.
//
// A ReleaseClaim is valid iff:
//   - DispatchHeadSHA is non-empty
//   - MergeTargetRef is non-empty and fully qualified (it starts with "refs/")
//   - MergeTargetSHA is non-empty
//   - RemoteEndpoint is nil, or dereferences to a valid RemoteEndpoint
//
// The "refs/" prefix is checked, not assumed. A bare branch name such as "main"
// resolves through git's ref-search rules, which can pick a tag or a remote
// tracking ref instead of the branch the release meant. §6.1 declares the field
// fully qualified for that reason.
func (c ReleaseClaim) Valid() bool {
	if c.DispatchHeadSHA == "" {
		return false
	}
	if c.MergeTargetRef == "" || !strings.HasPrefix(c.MergeTargetRef, "refs/") {
		return false
	}
	if c.MergeTargetSHA == "" {
		return false
	}
	if c.RemoteEndpoint != nil && !c.RemoteEndpoint.Valid() {
		return false
	}
	return true
}

// IsLocal reports whether the claim describes local work.
//
// A local claim carries no endpoint. A remote claim carries all three endpoint
// fields (§6.1).
func (c ReleaseClaim) IsLocal() bool {
	return c.RemoteEndpoint == nil
}
