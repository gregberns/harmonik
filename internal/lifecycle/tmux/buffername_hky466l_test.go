package tmux

// buffername_hky466l_test.go — closes the hk-y466l class: tmux buffer names built
// by raw fmt.Sprintf instead of the shared, validating helper.
//
// The invariant under test is that [BufferName] output ALWAYS satisfies the
// production validator — the same [bufferNameRe] that [OSAdapter.LoadBuffer] and
// [OSAdapter.PasteBuffer] enforce before tmux is invoked — for every input,
// including the degenerate ones that defeat fmt.Sprintf.
//
// One correction to the hk-y466l write-up is pinned here deliberately, because
// it is easy to re-derive wrongly: an EMPTY session id does NOT yield an invalid
// name. "harmonik--captain-boot" MATCHES bufferNameRe, because the character
// class includes '-' and the empty segment is absorbed by the delimiter
// (TestValidBufferName_MatchesProductionValidator asserts this against the live
// regex). Its cost is a missing id segment, not rejection — and the fallback
// fixes only that. Every empty-id launch still shares one buffer name; it is
// just "harmonik-unknown-<purpose>" now instead of "harmonik--<purpose>".
//
// The names that really are rejected carry a character outside [a-z0-9-]:
// uppercase (hk-lckbv's "20060102T150405Z"), an underscore, a dot. Those are
// what BufferName sanitizes away, and what the end-to-end LoadBuffer case below
// proves the retired fmt.Sprintf form would have failed on.
//
// The assertions go through [ValidBufferName] (which delegates to the real
// bufferNameRe) and, for the end-to-end case, through a real LoadBuffer call
// with a recording runner — not a restated regex, which would drift.

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// TestBufferName_AlwaysValid drives BufferName over well-formed and degenerate
// inputs and asserts every result passes the production validator.
func TestBufferName_AlwaysValid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		sessionID string
		purpose   string
		want      string
	}{
		{
			name:      "well-formed uuid session id",
			sessionID: "0198c2f1-4a3b-7c9d-8e01-2f3a4b5c6d7e",
			purpose:   "captain-boot",
			want:      "harmonik-0198c2f1-4a3b-7c9d-8e01-2f3a4b5c6d7e-captain-boot",
		},
		{
			// An empty session id used to yield "harmonik--captain-boot" —
			// valid per the regex, but with no readable id segment. The
			// fallback restores the segment shape; it does NOT make the name
			// unique, since every empty-id launch shares this one too.
			name:      "empty session id falls back",
			sessionID: "",
			purpose:   "captain-boot",
			want:      "harmonik-unknown-captain-boot",
		},
		{
			name:      "empty session id, crew purpose",
			sessionID: "",
			purpose:   "crew-boot",
			want:      "harmonik-unknown-crew-boot",
		},
		{
			// The hk-lckbv trigger: uppercase in the timestamp id.
			name:      "uppercase session id is lowercased",
			sessionID: "20260528T150405Z",
			purpose:   "task",
			want:      "harmonik-20260528t150405z-task",
		},
		{
			name:      "punctuation is mapped to hyphens",
			sessionID: "sess_id.42:9",
			purpose:   "review",
			want:      "harmonik-sess-id-42-9-review",
		},
		{
			name:      "leading and trailing junk is trimmed",
			sessionID: "  ..abc..  ",
			purpose:   "task",
			want:      "harmonik-abc-task",
		},
		{
			name:      "all-punctuation session id falls back",
			sessionID: "///",
			purpose:   "task",
			want:      "harmonik-unknown-task",
		},
		{
			name:      "empty purpose falls back",
			sessionID: "abc123",
			purpose:   "",
			want:      "harmonik-abc123-unknown",
		},
		{
			name:      "both segments degenerate",
			sessionID: "",
			purpose:   "",
			want:      "harmonik-unknown-unknown",
		},
		{
			name:      "keeper injector name",
			sessionID: "keeper",
			purpose:   "inject",
			want:      "harmonik-keeper-inject",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := BufferName(tc.sessionID, tc.purpose)
			if got != tc.want {
				t.Errorf("BufferName(%q, %q) = %q; want %q", tc.sessionID, tc.purpose, got, tc.want)
			}
			// The load-bearing assertion: the production validator accepts it.
			if !ValidBufferName(got) {
				t.Errorf("BufferName(%q, %q) = %q; rejected by bufferNameRe — "+
					"LoadBuffer/PasteBuffer would return ErrStructural (hk-y466l)",
					tc.sessionID, tc.purpose, got)
			}
		})
	}
}

// TestValidBufferName_MatchesProductionValidator pins ValidBufferName to
// bufferNameRe itself, including the exact malformed names the retired
// fmt.Sprintf call sites could emit.
func TestValidBufferName_MatchesProductionValidator(t *testing.T) {
	t.Parallel()

	valid := []string{
		"harmonik-abc-task",
		"harmonik-0198c2f1-4a3b-captain-boot",
		"harmonik-keeper-inject",
		// Pinned as VALID on purpose: the hk-y466l write-up asserts an empty
		// session id fails the regex. It does not — '-' is inside the character
		// class. If this line ever starts failing, bufferNameRe was tightened
		// and the empty-id sites became genuinely broken, not merely ambiguous.
		"harmonik--captain-boot",
		"harmonik--crew-boot",
	}
	for _, name := range valid {
		if !ValidBufferName(name) {
			t.Errorf("ValidBufferName(%q) = false; want true", name)
		}
		if !bufferNameRe.MatchString(name) {
			t.Errorf("bufferNameRe rejects %q but the fixture claims it is valid", name)
		}
	}

	invalid := []string{
		"hk-keeper-inject",               // retired keeper literal: wrong prefix
		"hk-comms-wake",                  // retired comms-wake literal: same class (hk-o0j47)
		"harmonik-ABC-task",              // uppercase session id (hk-lckbv)
		"harmonik-20260528T150405Z-task", // the literal hk-lckbv name
		"harmonik-sess_id-captain-boot",  // underscore
		"harmonik-input",                 // no purpose segment (hk-9hvr0)
		"",
	}
	for _, name := range invalid {
		if ValidBufferName(name) {
			t.Errorf("ValidBufferName(%q) = true; want false", name)
		}
		if bufferNameRe.MatchString(name) {
			t.Errorf("bufferNameRe accepts %q but the fixture claims it is invalid", name)
		}
	}
}

// TestBufferName_KeeperInjectorDuplicate is one half of a cross-package pin.
//
// internal/keeper is depguard-isolated and may NOT import this package
// (hk-ekap1 / hk-fzzc6), so its injector hardcodes the literal
// injectBufferName = "harmonik-keeper-inject" instead of calling BufferName.
// This test asserts the helper really does produce that literal and that the
// REAL validator accepts it — the check the keeper package cannot perform. The
// other half (keeper's TestInjectBufferName_NotTheRetiredLiteral) asserts the
// injector uses it. Change either side and the other fails.
func TestBufferName_KeeperInjectorDuplicate(t *testing.T) {
	t.Parallel()

	const keeperInjectBuffer = "harmonik-keeper-inject"

	if got := BufferName("keeper", "inject"); got != keeperInjectBuffer {
		t.Errorf("BufferName(\"keeper\", \"inject\") = %q; want %q — "+
			"internal/keeper/injector.go injectBufferName duplicates this literal "+
			"and must be updated in the same change (hk-y466l)", got, keeperInjectBuffer)
	}
	if !ValidBufferName(keeperInjectBuffer) {
		t.Errorf("ValidBufferName(%q) = false; the keeper's duplicated literal is "+
			"not a valid buffer name", keeperInjectBuffer)
	}
	if ValidBufferName("hk-keeper-inject") {
		t.Error("ValidBufferName(\"hk-keeper-inject\") = true; the retired keeper " +
			"literal is supposed to be invalid — that is why it was replaced")
	}
}

// TestBufferName_AcceptedByLoadBuffer is the end-to-end guard, run against the
// REAL enforcement point rather than a re-derived predicate: a name the helper
// built from a hostile session id reaches tmux, while the same id formatted the
// retired fmt.Sprintf way is rejected with ErrStructural before any tmux
// invocation.
func TestBufferName_AcceptedByLoadBuffer(t *testing.T) {
	t.Parallel()

	// The hk-lckbv session-id shape: uppercase letters the regex forbids.
	const hostileID = "20260528T150405Z"

	rr := &RecordingRunner{
		// "true" is a no-op binary present on every POSIX box, so LoadBuffer
		// succeeds without a live tmux server.
		CmdFunc: func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "true")
		},
	}
	a := OSAdapter{}.WithRunner(rr)

	good := BufferName(hostileID, "captain-boot")
	if err := a.LoadBuffer(context.Background(), good, []byte("seed")); err != nil {
		t.Fatalf("LoadBuffer(%q) = %v; want nil — the helper's output must clear "+
			"the structural gate (hk-y466l)", good, err)
	}
	if len(rr.Calls) != 1 {
		t.Fatalf("recorded %d runner calls; want 1", len(rr.Calls))
	}
	if args := strings.Join(rr.Calls[0].Args, " "); !strings.Contains(args, good) {
		t.Errorf("load-buffer argv %q does not carry buffer name %q", args, good)
	}

	// The same id through the retired construction: rejected before tmux runs,
	// so the boot seed would never be delivered.
	bad := "harmonik-" + hostileID + "-captain-boot"
	err := a.LoadBuffer(context.Background(), bad, []byte("seed"))
	if !errors.Is(err, ErrStructural) {
		t.Fatalf("LoadBuffer(%q) = %v; want ErrStructural — this is the failure "+
			"the helper exists to prevent", bad, err)
	}
	if len(rr.Calls) != 1 {
		t.Errorf("rejected name still reached the runner: %d calls; want 1", len(rr.Calls))
	}
}
