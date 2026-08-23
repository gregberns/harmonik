package tmux

import "strings"

const bufferNamePrefix = "harmonik-"

const bufferSegmentFallback = "unknown"

// BufferName builds the PL-021d buffer name "harmonik-<sessionID>-<purpose>",
// sanitizing both segments so the result ALWAYS satisfies [bufferNameRe] and is
// therefore always accepted by [Adapter.LoadBuffer], [Adapter.PasteBuffer], and
// [OSAdapter.WriteToPane].
//
// This is the one construction site every caller should use. What raw
// fmt.Sprintf cannot guarantee is the CHARACTER CLASS: any session id carrying
// an uppercase letter, an underscore, or a dot yields a name the validator
// rejects with [ErrStructural] before tmux is ever invoked, so the payload is
// dropped at write time rather than at construction time. That is exactly how
// hk-lckbv wedged the daemon (a "20060102T150405Z" timestamp id) and how
// hk-9hvr0 wedged the tmux substrate (a hardcoded "harmonik-input" with no
// purpose segment). Sanitizing here makes the class unreachable instead of
// re-litigating it per call site.
//
// Segments that carry no usable characters fall back to [bufferSegmentFallback]
// rather than collapsing the "harmonik-<id>-<purpose>" delimiters.
//
// Spec ref: process-lifecycle.md §4.7 PL-021d — buffer-name discipline.
// Beads: hk-y466l (this helper), hk-lckbv / hk-9hvr0 (the failures it prevents).
func BufferName(sessionID, purpose string) string {
	return bufferNamePrefix + bufferSegment(sessionID) + "-" + bufferSegment(purpose)
}

// ValidBufferName reports whether name satisfies the PL-021d buffer-name
// invariant that [Adapter.LoadBuffer] and [Adapter.PasteBuffer] enforce.
//
// Exported so callers and their tests can assert against the REAL validator
// instead of restating the regex — a restated copy drifts from the original and
// then proves nothing.
func ValidBufferName(name string) bool {
	return bufferNameRe.MatchString(name)
}

// SanitizeBufferSegment lowercases s and maps every character outside [a-z0-9]
// to '-', returning "" when s carries nothing usable. It is the segment-level
// half of [BufferName], exported for the one caller that needs the EMPTY answer
// rather than the fallback: internal/daemon's perRunSubstrate.inputBufferName
// chains run-session-id → pane-target → a literal, and has to know which link
// sanitized away to nothing.
//
// Exported for the same reason as [ValidBufferName]: a restated copy in another
// package drifts from this one and then proves nothing. There WAS such a copy,
// byte-identical, in internal/daemon/tmuxsubstrate.go.
//
// Prefer [BufferName] for constructing a name. Reach for this only when the
// empty case is load-bearing.
func SanitizeBufferSegment(s string) string {
	return sanitizeBufferSegment(s)
}

func bufferSegment(s string) string {
	if seg := sanitizeBufferSegment(s); seg != "" {
		return seg
	}
	return bufferSegmentFallback
}

func sanitizeBufferSegment(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r - 'A' + 'a')
		default:
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
