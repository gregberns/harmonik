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
		return ""
	}
}

func windowNameSentinelPrefix(projectHash core.ProjectHash, ownsSession bool) string {
	if ownsSession {
		return ""
	}
	h := string(projectHash)
	if len(h) >= projectHashPrefixLen {
		return "hk-" + h[:projectHashPrefixLen] + "-"
	}
	return "hk-" + h + "-"
}

func windowNameBeadPart(beadID core.BeadID, sentinelPrefix, suffix string) string {
	raw := string(beadID)

	if len(sentinelPrefix)+len(raw)+len(suffix) <= windowNameMaxBytes {
		return raw
	}

	sum := sha256.Sum256([]byte(raw))
	hashHex := fmt.Sprintf("%x", sum[:hashSuffixLen/2]) // N bytes → hashSuffixLen lowercase hex chars

	budget := windowNameMaxBytes - len(sentinelPrefix) - len(suffix) - 1 - hashSuffixLen
	if budget < 0 {
		budget = 0
	}

	truncated := raw
	if len(truncated) > budget {
		truncated = raw[:budget]
	}
	return truncated + "~" + hashHex
}
