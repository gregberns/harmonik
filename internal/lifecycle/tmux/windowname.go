package tmux

import (
	"crypto/sha256"
	"fmt"
	"strconv"

	"github.com/gregberns/harmonik/internal/core"
)

const (
	windowNameMaxBytes   = 64
	beadIDMaxBytes       = 56
	hashSuffixLen        = 8
	projectHashPrefixLen = 6
)

// WindowName derives the deterministic tmux window name for a given agent
// session. The function is pure: identical inputs produce identical outputs
// across invocations.
//
// Rules (WM-002a):
//   - phase=single        → bead_id
//   - phase=implementer-* → bead_id + "/i" + dec(iteration)
//   - phase=reviewer      → bead_id + "/r" + dec(iteration)
//
// When ownsSession=false ($TMUX-reuse mode), the name is prefixed with
// "hk-<hash6>-" where hash6 is the first 6 hex chars of projectHash.
//
// Truncation: if len(name) > 64 bytes after construction, bead_id is
// truncated to its leading 56 bytes and an 8-byte lowercase-hex prefix of
// SHA-256(bead_id) is appended as "~<hash[:8]>" before any suffix.
// The suffix ("/i<n>", "/r<n>") and the sentinel prefix are not truncated.
//
// Spec ref: workspace-model.md §4.1 WM-002a.
func WindowName(beadID core.BeadID, phase Phase, iteration int, projectHash core.ProjectHash, ownsSession bool) string {
	suffix := windowNameSuffix(phase, iteration)
	sentinelPrefix := windowNameSentinelPrefix(projectHash, ownsSession)
	beadPart := windowNameBeadPart(beadID, sentinelPrefix, suffix)
	return sentinelPrefix + beadPart + suffix
}

// windowNameSuffix returns the phase-dependent suffix appended to bead_id.
// Returns "" for single-mode, "/i<n>" for implementer, "/r<n>" for reviewer.
func windowNameSuffix(phase Phase, iteration int) string {
	n := strconv.Itoa(iteration)
	switch phase {
	case PhaseSingle:
		return ""
	case PhaseImplementerInitial, PhaseImplementerResume:
		return "/i" + n
	case PhaseReviewer:
		return "/r" + n
	default:
		// Unknown phase: treat as single-mode (no suffix).
		return ""
	}
}

// windowNameSentinelPrefix returns "hk-<hash6>-" when ownsSession is false,
// or "" when ownsSession is true.
// hash6 is the first 6 hex chars of projectHash (which is a 12-char hex string).
func windowNameSentinelPrefix(projectHash core.ProjectHash, ownsSession bool) string {
	if ownsSession {
		return ""
	}
	// ProjectHash is a 12-char lowercase-hex string per core.ProjectHash.
	// WM-002a: first 6 hex chars.
	h := string(projectHash)
	if len(h) >= projectHashPrefixLen {
		return "hk-" + h[:projectHashPrefixLen] + "-"
	}
	// Defensive: should never happen for a valid ProjectHash (validated at
	// construction time by core.ProjectHash.UnmarshalText). Use the full hash.
	return "hk-" + h + "-"
}

// windowNameBeadPart returns the bead_id portion of the window name, applying
// truncation when sentinelPrefix+beadID+suffix would exceed windowNameMaxBytes.
//
// Truncation rule (WM-002a): replace bead_id with
//
//	<bead_id[:56]> + "~" + lowercase-hex(SHA-256(bead_id))[:8]
//
// The sentinel prefix and suffix are excluded from truncation.
func windowNameBeadPart(beadID core.BeadID, sentinelPrefix, suffix string) string {
	raw := string(beadID)

	// Fast path: no truncation needed.
	if len(sentinelPrefix)+len(raw)+len(suffix) <= windowNameMaxBytes {
		return raw
	}

	// Compute the 8-char SHA-256 hash of the original bead_id.
	sum := sha256.Sum256([]byte(raw))
	hashHex := fmt.Sprintf("%x", sum[:4]) // 4 bytes → 8 lowercase hex chars

	// Truncate bead_id to 56 bytes (byte-level truncation, not rune-level;
	// bead IDs are ASCII per Beads convention).
	truncated := raw
	if len(truncated) > beadIDMaxBytes {
		truncated = raw[:beadIDMaxBytes]
	}
	return truncated + "~" + hashHex
}
