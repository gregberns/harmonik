// strip_nonce_markers_hk4tjyj_test.go — direct, character-level unit tests for
// the handoff nonce scrub and the injected-command shell quoting. Bead: hk-4tjyj.

package keeper_test

import (
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/keeper"
)

// TestStripNonceMarkers pins every behavioral claim the scrub makes. The scrub
// runs against the crew's REAL handoff on every cycle that carries a stale
// nonce, so any byte it removes beyond the marker itself is lost work — the
// exact failure hk-4tjyj was filed for. Driving it through a full cycle proves
// only the happy path; this table drives the pure function directly.
//
// The load-bearing case is "unclosed prefix followed by a well-formed marker".
// An earlier revision searched for the closing "-->" across the WHOLE remainder
// of the file, so a bare `<!-- KEEPER:` in prose swallowed everything from
// itself through the next real marker's closer — a crew writing a handoff ABOUT
// the keeper protocol (routine in this repo) would have had its decisions and
// next steps silently deleted. That case fails on the unbounded search.
func TestStripNonceMarkers(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "no markers at all is byte-identical",
			in:   "# HANDOFF-crew\n\nDECISION: hold the gate.\nNEXT: drain.\n",
			want: "# HANDOFF-crew\n\nDECISION: hold the gate.\nNEXT: drain.\n",
		},
		{
			name: "empty content",
			in:   "",
			want: "",
		},
		{
			name: "whole-line marker takes its own line, prose untouched",
			in:   "# H\n\nprose\n<!-- KEEPER:cyc-1 -->\nmore prose\n",
			want: "# H\n\nprose\nmore prose\n",
		},
		{
			name: "whole-line marker with leading indent and trailing spaces",
			in:   "prose\n  <!-- KEEPER:cyc-1 -->   \nmore\n",
			want: "prose\nmore\n",
		},
		{
			name: "marker at EOF with no trailing newline",
			in:   "prose\n<!-- KEEPER:cyc-1 -->",
			want: "prose\n",
		},
		{
			name: "CRLF line endings",
			in:   "prose\r\n<!-- KEEPER:cyc-1 -->\r\nmore\r\n",
			want: "prose\r\nmore\r\n",
		},
		{
			name: "inline marker is excised in place, line survives",
			in:   "see <!-- KEEPER:cyc-1 --> here\n",
			want: "see  here\n",
		},
		{
			name: "multiple markers on separate lines",
			in:   "a\n<!-- KEEPER:cyc-1 -->\nb\n<!-- KEEPER:cyc-2 -->\nc\n",
			want: "a\nb\nc\n",
		},
		{
			name: "adjacent markers on one line",
			in:   "a\n<!-- KEEPER:cyc-1 --><!-- KEEPER:cyc-2 -->\nb\n",
			want: "a\nb\n",
		},
		{
			name: "unclosed prefix with NO later marker is left untouched",
			in:   "# H\n\n<!-- KEEPER: talking about the protocol\nDECISION: locked.\n",
			want: "# H\n\n<!-- KEEPER: talking about the protocol\nDECISION: locked.\n",
		},
		{
			// The regression. Reviewer's exact counterexample.
			name: "unclosed prefix followed by a well-formed marker keeps ALL prose",
			in: "# H\n\n<!-- KEEPER: unclosed prose\nDECISION: locked the gate on hk-xyz.\n" +
				"NEXT: drain the queue.\n<!-- KEEPER:cyc-999 -->\n",
			want: "# H\n\n<!-- KEEPER: unclosed prose\nDECISION: locked the gate on hk-xyz.\n" +
				"NEXT: drain the queue.\n",
		},
		{
			name: "closer on a LATER line never closes an earlier prefix",
			in:   "<!-- KEEPER:no-closer-here\nkeep me\n-->\ntail\n",
			want: "<!-- KEEPER:no-closer-here\nkeep me\n-->\ntail\n",
		},
		{
			name: "marker is the entire file",
			in:   "<!-- KEEPER:cyc-1 -->\n",
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := keeper.StripNonceMarkersForTest(tc.in)
			if got != tc.want {
				t.Errorf("stripNonceMarkers mismatch.\n in:   %q\n want: %q\n got:  %q", tc.in, tc.want, got)
			}
			// Whatever survives must never still carry a well-formed marker that
			// could pre-satisfy the nonce poll (the DEFECT-2 purpose the scrub
			// inherited from the old truncate).
			for _, line := range strings.Split(got, "\n") {
				if idx := strings.Index(line, "<!-- KEEPER:"); idx >= 0 &&
					strings.Contains(line[idx:], "-->") {
					t.Errorf("a well-formed marker survived the scrub on line %q (input %q)", line, tc.in)
				}
			}
		})
	}
}

// TestStripNonceMarkers_IsIdempotent: the scrub runs on every cycle that sees a
// stale nonce, so a second pass over already-scrubbed content must be a no-op.
// A non-idempotent scrub would erode the handoff a little on every restart.
func TestStripNonceMarkers_IsIdempotent(t *testing.T) {
	t.Parallel()

	inputs := []string{
		"# H\n\nprose\n<!-- KEEPER:cyc-1 -->\ntail\n",
		"<!-- KEEPER: unclosed\nprose\n<!-- KEEPER:cyc-2 -->\n",
		"see <!-- KEEPER:cyc-3 --> here\n",
	}
	for _, in := range inputs {
		once := keeper.StripNonceMarkersForTest(in)
		twice := keeper.StripNonceMarkersForTest(once)
		if once != twice {
			t.Errorf("scrub is not idempotent for %q:\n first:  %q\n second: %q", in, once, twice)
		}
	}
}

// TestShellQuoteIfNeeded pins the ALLOWLIST quoting rule for the injected reboot
// command. That command string is pasted into a live pane and executed, so a
// denylist ("quote only if it has a space or a quote") is a command-injection
// surface: `$`, backtick, `\`, `;`, `&`, `|` and glob metacharacters all pass a
// denylist untouched and are all live in a shell. Every case below with a
// metacharacter fails against a denylist implementation.
func TestShellQuoteIfNeeded(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"ordinary path stays bare", "/Users/gb/github/harmonik", "/Users/gb/github/harmonik"},
		{"agent name stays bare", "crew-kilo_1", "crew-kilo_1"},
		{"allowlisted punctuation stays bare", "a.b-c_d/e:f,g+h=i@j%k", "a.b-c_d/e:f,g+h=i@j%k"},
		{"empty becomes explicit empty arg", "", "''"},
		{"space is quoted", "/tmp/my project", "'/tmp/my project'"},
		{"tab is quoted", "/tmp/a\tb", "'/tmp/a\tb'"},
		{"newline is quoted", "/tmp/a\nb", "'/tmp/a\nb'"},
		{"dollar is quoted", "/tmp/$HOME/x", "'/tmp/$HOME/x'"},
		{"backtick is quoted", "/tmp/`whoami`", "'/tmp/`whoami`'"},
		{"semicolon is quoted", "/tmp/a;rm -rf /", "'/tmp/a;rm -rf /'"},
		{"backslash is quoted", `/tmp/a\b`, `'/tmp/a\b'`},
		{"ampersand is quoted", "/tmp/a&b", "'/tmp/a&b'"},
		{"pipe is quoted", "/tmp/a|b", "'/tmp/a|b'"},
		{"glob is quoted", "/tmp/*", "'/tmp/*'"},
		{"question mark is quoted", "/tmp/a?b", "'/tmp/a?b'"},
		{"parens are quoted", "/tmp/a(b)c", "'/tmp/a(b)c'"},
		{"double quote is quoted", `/tmp/a"b`, `'/tmp/a"b'`},
		{"non-ascii is quoted", "/tmp/café", "'/tmp/café'"},
		{"single quote is closed-escaped-reopened", "/tmp/it's", `'/tmp/it'\''s'`},
		{
			"the compound injection attempt",
			"/tmp/x`id`;$(id)&",
			"'/tmp/x`id`;$(id)&'",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := keeper.ShellQuoteIfNeededForTest(tc.in); got != tc.want {
				t.Errorf("shellQuoteIfNeeded(%q) = %q; want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestShellQuoteIfNeeded_MetacharsNeverEscapeQuoting is a property check over the
// full set of shell metacharacters: none may ever appear OUTSIDE single quotes in
// the result. This catches a future edit that adds a character to the allowlist
// without thinking about the shell.
func TestShellQuoteIfNeeded_MetacharsNeverEscapeQuoting(t *testing.T) {
	t.Parallel()

	const metachars = "$`\\;&|<>()[]{}*?!#~^\"' \t\n"
	for _, r := range metachars {
		in := "/tmp/a" + string(r) + "b"
		got := keeper.ShellQuoteIfNeededForTest(in)
		if !strings.HasPrefix(got, "'") || !strings.HasSuffix(got, "'") {
			t.Errorf("shellQuoteIfNeeded(%q) = %q; a value containing %q must be single-quoted", in, got, r)
		}
	}
}
