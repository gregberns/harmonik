package cli

import (
	"io"
	"strings"
	"testing"
)

// hk-snjr — KEEPER parser-parity SPLIT C (queue helpers.go). These tests pin the
// contract that parseQueueFlagsExtra — the shared parser behind all nine queue
// verbs — keeps every recognized flag (--project / --json / --format plus any
// verb-specific extra flag), preserves genuine positionals, and REJECTS an
// UNRECOGNIZED leading-dash token loudly (ok=false → exit 2) instead of silently
// consuming it as a positional. This mirrors the keeper subcommand parser parity
// landed under hk-t1wd.

// queueExtraFlagFn is a representative verb-specific extra-flag handler (it
// recognizes --queue / --queue=) used to prove ENUMERATE-AND-KEEP: a verb's own
// flags are still consumed, and only truly-unrecognized dash tokens are rejected.
func queueExtraFlagFn(captured *string) func(args []string, i int) (int, bool) {
	return func(args []string, i int) (int, bool) {
		switch {
		case args[i] == "--queue" && i+1 < len(args):
			*captured = args[i+1]
			return i + 2, true
		case strings.HasPrefix(args[i], "--queue="):
			*captured = strings.TrimPrefix(args[i], "--queue=")
			return i + 1, true
		}
		return i, false
	}
}

func TestParseQueueFlagsExtra_RecognizedFlagsAndPositionalsKept(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		args           []string
		wantOK         bool
		wantJSON       bool
		wantQueue      string
		wantPositional []string
	}{
		{"project-flag", []string{"--project", "/tmp/x", "pos"}, true, false, "", []string{"pos"}},
		{"project-equals", []string{"--project=/tmp/x", "pos"}, true, false, "", []string{"pos"}},
		{"json-shorthand", []string{"--json", "pos"}, true, true, "", []string{"pos"}},
		{"format-json", []string{"--format", "json", "pos"}, true, true, "", []string{"pos"}},
		{"format-equals-json", []string{"--format=json"}, true, true, "", nil},
		{"extra-flag-kept", []string{"--queue", "investigate", "pos"}, true, false, "investigate", []string{"pos"}},
		{"extra-flag-equals-kept", []string{"--queue=flywheel"}, true, false, "flywheel", nil},
		{"bead-id-positional-not-rejected", []string{"hk-abc", "hk-def"}, true, false, "", []string{"hk-abc", "hk-def"}},
		{"all-recognized-flags-mixed", []string{"--project", "/tmp/x", "--json", "--queue", "q", "pos"}, true, true, "q", []string{"pos"}},
		// Reject paths: an unrecognized leading-dash token → ok=false.
		{"leading-dash-bogus", []string{"--bogus"}, false, false, "", nil},
		{"trailing-dash-bogus", []string{"pos", "--bogus"}, false, false, "", nil},
		{"single-dash-unknown", []string{"-x"}, false, false, "", nil},
		{"dangling-project-no-value", []string{"--project"}, false, false, "", nil},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var capturedQueue string
			_, positional, outputJSON, ok := parseQueueFlagsExtra(tc.args, io.Discard, queueExtraFlagFn(&capturedQueue))
			if ok != tc.wantOK {
				t.Fatalf("args %v: ok=%v, want %v", tc.args, ok, tc.wantOK)
			}
			if !tc.wantOK {
				return // reject path: remaining fields are zero-valued
			}
			if outputJSON != tc.wantJSON {
				t.Errorf("args %v: outputJSON=%v, want %v", tc.args, outputJSON, tc.wantJSON)
			}
			if capturedQueue != tc.wantQueue {
				t.Errorf("args %v: extra-flag captured=%q, want %q", tc.args, capturedQueue, tc.wantQueue)
			}
			if !equalStringSlices(positional, tc.wantPositional) {
				t.Errorf("args %v: positional=%v, want %v", tc.args, positional, tc.wantPositional)
			}
		})
	}
}

// TestParseQueueFlags_RejectsBogusFlag pins the no-extra-flag path (used by
// `queue list` and `queue set-concurrency`): an unrecognized dash token is
// rejected, but a genuine positional (e.g. a concurrency integer) is kept.
func TestParseQueueFlags_RejectsBogusFlag(t *testing.T) {
	t.Parallel()

	if _, _, _, ok := parseQueueFlags([]string{"--bogus"}, io.Discard); ok {
		t.Errorf("parseQueueFlags([--bogus]): ok=true, want false (loud reject)")
	}
	_, positional, _, ok := parseQueueFlags([]string{"4"}, io.Discard)
	if !ok {
		t.Fatalf("parseQueueFlags([4]): ok=false, want true (numeric positional kept)")
	}
	if len(positional) != 1 || positional[0] != "4" {
		t.Errorf("parseQueueFlags([4]): positional=%v, want [4]", positional)
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
