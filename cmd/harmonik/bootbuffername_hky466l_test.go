package main

// bootbuffername_hky466l_test.go — hk-y466l sites 2 and 3.
//
// captainBootBufferName / crewBootBufferName replace the raw
// fmt.Sprintf("harmonik-%s-captain-boot", sessionID) constructions that built
// the boot-seed buffer name without passing through the shared, validating
// helper. Both are pasted with ltmux.OSAdapter.WriteToPane, which enforces
// bufferNameRe before invoking tmux: an invalid name means ErrStructural, which
// PasteSeedToAgentPane / pasteCrewBriefSeedViaTmux only WARN about, so the boot
// seed is silently dropped and the agent never runs `harmonik agent brief`.
//
// One nuance the bead write-up gets wrong and this file pins: an EMPTY session
// id does NOT produce an invalid name — "harmonik--captain-boot" matches
// bufferNameRe because the character class contains '-'. The real rejection
// shape is a session id carrying a character outside [a-z0-9-] (the hk-lckbv
// uppercase-timestamp case). Both are covered below; the empty case is asserted
// for segment SHAPE — the fallback keeps an id segment present, it does NOT make
// the name unique — and the hostile case for validity.
//
// Assertions go through ltmux.ValidBufferName, which delegates to the real
// bufferNameRe — not a restated regex.

import (
	"strings"
	"testing"

	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// TestBootBufferNames_AlwaysValid checks both launcher construction sites over
// well-formed and degenerate session ids.
func TestBootBufferNames_AlwaysValid(t *testing.T) {
	t.Parallel()

	sessionIDs := []struct {
		name string
		sid  string
	}{
		{"minted uuidv4", "3f2504e0-4f89-41d3-9a0c-0305e82c3301"},
		{"empty", ""},
		{"uppercase timestamp (hk-lckbv shape)", "20260528T150405Z"},
		{"underscored", "sess_id_42"},
		{"all punctuation", "///"},
	}

	for _, tc := range sessionIDs {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for label, got := range map[string]string{
				"captainBootBufferName": captainBootBufferName(tc.sid),
				"crewBootBufferName":    crewBootBufferName(tc.sid),
			} {
				if !ltmux.ValidBufferName(got) {
					t.Errorf("%s(%q) = %q; rejected by the tmux buffer-name validator — "+
						"WriteToPane would return ErrStructural and the boot seed would be "+
						"dropped (hk-y466l)", label, tc.sid, got)
				}
			}
		})
	}
}

// TestBootBufferNames_EmptySessionIDIsNotBare is the specific regression the
// bead asks for: an empty session id must no longer collapse into the bare
// "harmonik--<purpose>" form, which carries no readable id segment in
// `tmux list-buffers` and is accepted by the validator only because '-' happens
// to be inside its character class.
//
// This is NOT a collision guard. "harmonik-unknown-captain-boot" is shared by
// every empty-id launch exactly as "harmonik--captain-boot" was; ltmux.BufferName
// is a pure function and has no unique suffix to offer. What is pinned here is
// shape: an id segment exists, and the name still clears the real validator.
func TestBootBufferNames_EmptySessionIDIsNotBare(t *testing.T) {
	t.Parallel()

	cases := []struct {
		label   string
		got     string
		retired string
	}{
		{"captainBootBufferName", captainBootBufferName(""), "harmonik--captain-boot"},
		{"crewBootBufferName", crewBootBufferName(""), "harmonik--crew-boot"},
	}
	for _, tc := range cases {
		if tc.got == tc.retired {
			t.Errorf("%s(\"\") = %q; still the bare fmt.Sprintf form (hk-y466l)", tc.label, tc.got)
		}
		if !ltmux.ValidBufferName(tc.got) {
			t.Errorf("%s(\"\") = %q; not a valid tmux buffer name", tc.label, tc.got)
		}
		if strings.Contains(tc.got, "--") {
			t.Errorf("%s(\"\") = %q; contains an empty segment", tc.label, tc.got)
		}
	}
}

// TestBootBufferNames_DistinctPurposes guards against the two launcher paths
// converging on one name, which would let a captain and a crew booting with the
// same session id overwrite each other's seed buffer.
func TestBootBufferNames_DistinctPurposes(t *testing.T) {
	t.Parallel()

	const sid = "3f2504e0-4f89-41d3-9a0c-0305e82c3301"
	capBuf, crewBuf := captainBootBufferName(sid), crewBootBufferName(sid)
	if capBuf == crewBuf {
		t.Fatalf("captain and crew boot buffers collide: both %q", capBuf)
	}
	if !strings.HasSuffix(capBuf, "-captain-boot") {
		t.Errorf("captainBootBufferName(%q) = %q; want a -captain-boot suffix", sid, capBuf)
	}
	if !strings.HasSuffix(crewBuf, "-crew-boot") {
		t.Errorf("crewBootBufferName(%q) = %q; want a -crew-boot suffix", sid, crewBuf)
	}
	// The well-formed path must be byte-identical to the retired construction,
	// so this change is a no-op for every real launch today.
	if want := "harmonik-" + sid + "-captain-boot"; capBuf != want {
		t.Errorf("captainBootBufferName(%q) = %q; want %q (unchanged for a well-formed id)", sid, capBuf, want)
	}
	if want := "harmonik-" + sid + "-crew-boot"; crewBuf != want {
		t.Errorf("crewBootBufferName(%q) = %q; want %q (unchanged for a well-formed id)", sid, crewBuf, want)
	}
}
