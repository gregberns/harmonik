package keeper_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/keeper"
)

func scrubHandoffFile(t *testing.T) func(string) error {
	t.Helper()
	return keeper.ScrubHandoffFileForTest
}

func TestHandoffScrubKeepsEveryByteThatIsNotAKeeperMarker(t *testing.T) {
	t.Parallel()

	const handoff = "# Handoff — crew paul\n" +
		"\n" +
		"Decision: the daemon owns terminal transitions.\n" +
		"<!-- KEEPER:cyc-20260805-01 -->\n" +
		"Next: land the scrub test.\n"
	const want = "# Handoff — crew paul\n" +
		"\n" +
		"Decision: the daemon owns terminal transitions.\n" +
		"Next: land the scrub test.\n"

	got := keeper.StripNonceMarkersForTest(handoff)
	if got != want {
		t.Fatalf("the scrub did not preserve the crew's prose\n got: %q\nwant: %q", got, want)
	}
	if strings.Contains(want, "<!-- KEEPER:") {
		t.Fatal("the wanted output still holds a keeper marker; this test asks the scrub for nothing")
	}
}

func TestHandoffScrubLeavesNoBlankLineWhereAWholeLineMarkerStood(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handoff string
		want    string
	}{
		{
			name:    "marker alone on its line takes the line's newline with it",
			handoff: "before\n<!-- KEEPER:cyc-1 -->\nafter\n",
			want:    "before\nafter\n",
		},
		{
			name:    "indented marker takes its leading whitespace too",
			handoff: "before\n\t  <!-- KEEPER:cyc-1 -->\nafter\n",
			want:    "before\nafter\n",
		},
		{
			name:    "marker on the last line without a trailing newline leaves no empty tail line",
			handoff: "before\n<!-- KEEPER:cyc-1 -->",
			want:    "before\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := keeper.StripNonceMarkersForTest(tc.handoff); got != tc.want {
				t.Fatalf("scrub left the wrong bytes\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

func TestHandoffScrubCutsAnEmbeddedMarkerOutOfItsLineAndKeepsTheRestOfTheLine(t *testing.T) {
	t.Parallel()

	const handoff = "Status: green <!-- KEEPER:cyc-2 --> and holding.\n"
	const want = "Status: green  and holding.\n"

	if got := keeper.StripNonceMarkersForTest(handoff); got != want {
		t.Fatalf("scrub damaged the prose around an embedded marker\n got: %q\nwant: %q", got, want)
	}
}

func TestHandoffScrubKeepsProseThatOpensAKeeperMarkerAndNeverClosesIt(t *testing.T) {
	t.Parallel()

	const handoff = "The keeper writes <!-- KEEPER: plus a cycle id into this file.\n"

	if got := keeper.StripNonceMarkersForTest(handoff); got != handoff {
		t.Fatalf("scrub ate prose that only mentions the marker prefix\n got: %q\nwant: %q", got, handoff)
	}
}

func TestHandoffScrubStillRemovesAMarkerThatFollowsAnUnclosedPrefix(t *testing.T) {
	t.Parallel()

	const handoff = "We document <!-- KEEPER: in prose here.\n" +
		"Decision: keep the crew's prose.\n" +
		"<!-- KEEPER:cyc-3 -->\n" +
		"Next: verify.\n"
	const want = "We document <!-- KEEPER: in prose here.\n" +
		"Decision: keep the crew's prose.\n" +
		"Next: verify.\n"

	got := keeper.StripNonceMarkersForTest(handoff)
	if got != want {
		t.Fatalf("scrub swallowed prose between an unclosed prefix and a later marker\n got: %q\nwant: %q", got, want)
	}
	if strings.Contains(got, "cyc-3") {
		t.Fatal("scrub left the stale marker behind after skipping the unclosed prefix")
	}
}

func TestHandoffScrubRemovesEveryMarkerWhenSeveralAreLeftBehind(t *testing.T) {
	t.Parallel()

	const handoff = "<!-- KEEPER:cyc-1 -->\n" +
		"# Handoff\n" +
		"Mid <!-- KEEPER:cyc-2 --> line.\n" +
		"<!-- KEEPER:cyc-3 -->\n" +
		"End.\n"
	const want = "# Handoff\n" +
		"Mid  line.\n" +
		"End.\n"

	got := keeper.StripNonceMarkersForTest(handoff)
	if got != want {
		t.Fatalf("scrub did not clear every stale marker\n got: %q\nwant: %q", got, want)
	}
	if strings.Contains(got, "<!-- KEEPER:") {
		t.Fatalf("a keeper marker survived the scrub: %q", got)
	}
}

func TestHandoffScrubReturnsContentWithoutMarkersUnchanged(t *testing.T) {
	t.Parallel()

	const handoff = "# Handoff\n\nDecision: none today.\nNext: rest.\n"

	if got := keeper.StripNonceMarkersForTest(handoff); got != handoff {
		t.Fatalf("scrub changed content that holds no marker\n got: %q\nwant: %q", got, handoff)
	}
}

func TestHandoffFileScrubKeepsTheCrewsProseAndThePermissionBits(t *testing.T) {
	t.Parallel()

	const before = "# Handoff — crew paul\n" +
		"\n" +
		"Decision: land the scrub test first.\n" +
		"<!-- KEEPER:cyc-20260805-02 -->\n" +
		"Next: report to the lane.\n"
	const after = "# Handoff — crew paul\n" +
		"\n" +
		"Decision: land the scrub test first.\n" +
		"Next: report to the lane.\n"

	path := filepath.Join(t.TempDir(), "HANDOFF.md")
	// 0o640 is the subject of this test, not an oversight: the assertion below
	// is that the scrub PRESERVES the permission bits, and a mode gosec is happy
	// with is also the mode a rewrite would land on by accident. Tightening this
	// to 0o600 makes the assertion unable to fail.
	//nolint:gosec // G306: the permissive mode is what this test measures
	if err := os.WriteFile(path, []byte(before), 0o640); err != nil {
		t.Fatalf("seed handoff file: %v", err)
	}

	if err := scrubHandoffFile(t)(path); err != nil {
		t.Fatalf("scrub returned an error: %v", err)
	}

	got, err := os.ReadFile(path) //nolint:gosec // G304: path is t.TempDir-derived
	if err != nil {
		t.Fatalf("read handoff file after scrub: %v", err)
	}
	if string(got) != after {
		t.Fatalf("scrub destroyed part of the handoff file\n got: %q\nwant: %q", string(got), after)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat handoff file after scrub: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o640 {
		t.Fatalf("scrub changed the file permission bits: got %04o, want %04o", perm, 0o640)
	}
}

func TestHandoffFileScrubLeavesAFileWithNoMarkerCompletelyAlone(t *testing.T) {
	t.Parallel()

	const content = "# Handoff\n\nDecision: none.\nNext: rest.\n"

	path := filepath.Join(t.TempDir(), "HANDOFF.md")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("seed handoff file: %v", err)
	}
	past := time.Now().Add(-90 * time.Minute).Truncate(time.Second)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatalf("age the handoff file: %v", err)
	}
	stale, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat handoff file before scrub: %v", err)
	}

	if err := scrubHandoffFile(t)(path); err != nil {
		t.Fatalf("scrub returned an error: %v", err)
	}

	fresh, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat handoff file after scrub: %v", err)
	}
	if !fresh.ModTime().Equal(stale.ModTime()) {
		t.Fatalf("scrub rewrote a file it changed nothing in and moved the mtime: before %v, after %v",
			stale.ModTime(), fresh.ModTime())
	}
	got, err := os.ReadFile(path) //nolint:gosec // G304: path is t.TempDir-derived
	if err != nil {
		t.Fatalf("read handoff file after scrub: %v", err)
	}
	if string(got) != content {
		t.Fatalf("scrub changed a file that holds no marker\n got: %q\nwant: %q", string(got), content)
	}
}

func TestHandoffFileScrubTreatsAMissingFileAsNothingToDo(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "HANDOFF.md")

	if err := scrubHandoffFile(t)(path); err != nil {
		t.Fatalf("scrub of a missing handoff file returned an error: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("scrub created a handoff file that was not there: stat err = %v", err)
	}
}
