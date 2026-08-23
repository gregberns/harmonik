package twinparity

import (
	"fmt"
	"sort"
	"testing"
)

// EquivOptions configures AssertStreamEquivalent.
type EquivOptions struct {
	// Kinds is the ordered spine that must appear as a subsequence in the
	// real stream. Empty → TerminalKinds.
	Kinds []string
	// PayloadFields overrides the per-kind stable-field whitelist compared
	// for equality. Empty/absent kind → the package default whitelist.
	PayloadFields map[string][]string
	// AllowExtra tolerates extra events interleaved between spine kinds in
	// the real stream. Defaults to true.
	AllowExtra bool
}

type divergence struct {
	kind     string
	position int    // spine index (or real position for field mismatch)
	field    string // empty for a missing/ordering divergence
	expected string
	got      string
	reason   string
}

func (d divergence) String() string {
	if d.field != "" {
		return fmt.Sprintf("first divergence at spine kind %q (spine index %d), field %q: expected %q, got %q",
			d.kind, d.position, d.field, d.expected, d.got)
	}
	return fmt.Sprintf("first divergence at spine index %d, kind %q: %s (expected %q, got %q)",
		d.position, d.kind, d.reason, d.expected, d.got)
}

// AssertStreamEquivalent asserts that the twin stream's spine (opts.Kinds, or
// TerminalKinds by default) appears as an ordered subsequence in the real
// stream, with per-kind stable payload fields equal at each matched spine
// event. On the first divergence it reports which kind/field diverged, expected
// vs got, and the position, via t.Errorf.
func AssertStreamEquivalent(t testing.TB, twin, realStream Stream, opts EquivOptions) {
	t.Helper()

	spine := opts.Kinds
	if len(spine) == 0 {
		spine = TerminalKinds
	}

	if div, ok := checkEquivalent(twin, realStream, spine, opts.PayloadFields); !ok {
		t.Errorf("twinparity: streams not equivalent: %s", div.String())
	}
}

func checkEquivalent(twin, realStream Stream, spine []string, fieldOverrides map[string][]string) (divergence, bool) {
	twinSpine, div, ok := locateSpine(twin, spine)
	if !ok {
		return div, false
	}

	si := 0 // spine index
	for _, rev := range realStream.Events {
		if si >= len(spine) {
			break
		}
		if rev.Kind != spine[si] {
			continue // extra event — tolerated
		}
		if d, fieldOK := compareFields(spine[si], si, twinSpine[si], rev, fieldOverrides); !fieldOK {
			return d, false
		}
		si++
	}

	if si < len(spine) {
		return divergence{
			kind:     spine[si],
			position: si,
			reason:   "spine kind not found as ordered subsequence in real stream",
			expected: spine[si],
			got:      "<absent>",
		}, false
	}
	return divergence{}, true
}

func locateSpine(twin Stream, spine []string) ([]CanonEvent, divergence, bool) {
	out := make([]CanonEvent, len(spine))
	si := 0
	for _, ev := range twin.Events {
		if si >= len(spine) {
			break
		}
		if ev.Kind == spine[si] {
			out[si] = ev
			si++
		}
	}
	if si < len(spine) {
		return nil, divergence{
			kind:     spine[si],
			position: si,
			reason:   "spine kind absent from TWIN stream (assertion would be vacuous)",
			expected: spine[si],
			got:      "<absent in twin>",
		}, false
	}
	return out, divergence{}, true
}

func compareFields(kind string, spineIdx int, twinEv, realEv CanonEvent, overrides map[string][]string) (divergence, bool) {
	fields := fieldsFor(kind, overrides, twinEv, realEv)
	for _, f := range fields {
		tv := twinEv.Fields[f]
		rv := realEv.Fields[f]
		if tv != rv {
			return divergence{
				kind:     kind,
				position: spineIdx,
				field:    f,
				expected: tv,
				got:      rv,
			}, false
		}
	}
	return divergence{}, true
}

func fieldsFor(kind string, overrides map[string][]string, twinEv, realEv CanonEvent) []string {
	if overrides != nil {
		if fs, ok := overrides[kind]; ok {
			return fs
		}
	}
	if fs, ok := stablePayloadFields[kind]; ok {
		return fs
	}
	set := map[string]struct{}{}
	for k := range twinEv.Fields {
		set[k] = struct{}{}
	}
	for k := range realEv.Fields {
		set[k] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
