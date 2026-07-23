package tmux

import "strings"

// bufferNamePrefix is the mandatory leading component of every PL-021d tmux
// buffer name. It is the literal that [bufferNameRe] anchors on.
const bufferNamePrefix = "harmonik-"

// bufferSegmentFallback is substituted for a segment that sanitizes to the empty
// string (an empty, all-punctuation, or all-hyphen input).
//
// It does NOT make the name unique. "harmonik-unknown-captain-boot" is shared
// by every empty-id launch exactly as "harmonik--captain-boot" was, so two
// concurrent empty-id boots still overwrite each other's payload. [BufferName]
// is a pure function of its arguments and deliberately stays that way: a unique
// suffix would have to come from a clock or a random source, and the name has to
// be reproducible by the caller that pastes and deletes the buffer afterwards.
//
// What it buys is SHAPE. An empty segment is not rejected today —
// "harmonik--captain-boot" does satisfy [bufferNameRe], because the character
// class includes '-' and the empty segment is absorbed by the neighbouring
// delimiter (TestValidBufferName_MatchesProductionValidator pins that against
// the live regex). But that name carries no readable id in `tmux list-buffers`,
// and it is valid only by that accident of the character class: tightening
// [bufferNameRe] to forbid empty segments would turn every such launch into
// [ErrStructural]. The literal keeps the "harmonik-<id>-<purpose>" shape intact
// under both.
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

// bufferSegment sanitizes one segment and substitutes the fallback when nothing
// usable survives.
func bufferSegment(s string) string {
	if seg := sanitizeBufferSegment(s); seg != "" {
		return seg
	}
	return bufferSegmentFallback
}

// sanitizeBufferSegment lowercases s and maps every character outside [a-z0-9]
// to '-', so the result is safe to embed as a segment of a [bufferNameRe]-valid
// buffer name. Leading and trailing hyphens are trimmed so the segment never
// collapses the "harmonik-<id>-<purpose>" delimiters. Returns "" when s has no
// usable characters.
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
