package tmux

import (
	"crypto/sha256"
	"fmt"
	"strconv"

	"github.com/gregberns/harmonik/internal/core"
)

const (
	windowNameMaxBytes   = 64
	hashSuffixLen        = 8
	projectHashPrefixLen = 6
)

// WindowAgent and WindowKeeper name the two windows inside a captain or crew
// session (harmonik-<hash>-captain / harmonik-<hash>-crew-<name>), per the
// tmux-session-organization CONTRACT:
//
//   - WindowAgent ("agent")  holds the LLM pane (claude --remote-control).
//   - WindowKeeper ("keeper") holds the per-agent session-keeper watcher, which
//     injects into the sibling "agent" window's active pane.
//
// A captain/crew restart respawns ONLY the "agent" window so the keeper window
// (and the keeper process) survive the restart. The daemon (slice C) imports
// these constants when constructing the session's window layout.
//
// The keeper package is depguard-isolated from internal/lifecycle and therefore
// cannot import these symbols; it hardcodes the same "agent"/"keeper" literals
// with a "// MUST match tmux.WindowAgent/WindowKeeper" comment.
const (
	WindowAgent  = "agent"
	WindowKeeper = "keeper"
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
// truncated so that the composed name (sentinel prefix + truncated bead_id +
// "~<hash[:8]>" + suffix) fits within 64 bytes, where the hash is an 8-byte
// lowercase-hex prefix of SHA-256(bead_id).
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
//	<bead_id[:budget]> + "~" + lowercase-hex(SHA-256(bead_id))[:8]
//
// where budget = 64 - len(sentinelPrefix) - len(suffix) - len("~") - 8, so the
// composed name never exceeds windowNameMaxBytes. (A fixed 56-byte bead budget
// previously yielded 56+1+8 = 65 bytes even before any prefix/suffix.)
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

	// Budget for the truncated bead_id: total max minus the fixed parts
	// (sentinel prefix, suffix, "~" separator, 8-char hash).
	budget := windowNameMaxBytes - len(sentinelPrefix) - len(suffix) - 1 - hashSuffixLen
	if budget < 0 {
		budget = 0
	}

	// Truncate bead_id to the budget (byte-level truncation, not rune-level;
	// bead IDs are ASCII per Beads convention).
	truncated := raw
	if len(truncated) > budget {
		truncated = raw[:budget]
	}
	return truncated + "~" + hashHex
}
