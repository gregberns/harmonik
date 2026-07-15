package sessioncapture

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/substrate"
)

func mustOpen(t *testing.T, cfg Config) *Session {
	t.Helper()
	s, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s
}

func mustClose(t *testing.T, s *Session) {
	t.Helper()
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// TestInputVerbatim asserts the INPUT direction is persisted byte-for-byte
// (HC-028: input is structurally secret-free, no scrub).
func TestInputVerbatim(t *testing.T) {
	ws := t.TempDir()
	s := mustOpen(t, Config{WorkspacePath: ws, SessionID: "sess-in"})

	// A payload that LOOKS like it holds a secret: INPUT must NOT scrub it.
	payload := `{"method":"turn/start","apiKey":"sk-ant-shouldNOTbescrubbed12345"}` + "\n"
	if _, err := s.Input().Write([]byte(payload)); err != nil {
		t.Fatalf("input write: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	got := readFile(t, filepath.Join(s.Dir(), wireInFile))
	if got != payload {
		t.Fatalf("input not verbatim:\n got %q\nwant %q", got, payload)
	}
}

// TestOutputScrub asserts the OUTPUT direction scrubs secret VALUES (HC-032)
// while leaving the surrounding non-secret bytes intact.
func TestOutputScrub(t *testing.T) {
	ws := t.TempDir()
	s := mustOpen(t, Config{WorkspacePath: ws, SessionID: "sess-out"})

	line := `{"event":"log","text":"using key sk-ant-ABCDEFGH12345 for auth","token":"bearerVALUE123456"}` + "\n"
	if _, err := s.Output().Write([]byte(line)); err != nil {
		t.Fatalf("output write: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	got := readFile(t, filepath.Join(s.Dir(), wireOutFile))
	if strings.Contains(got, "sk-ant-ABCDEFGH12345") {
		t.Fatalf("OUTPUT leaked the sk-ant value: %q", got)
	}
	if strings.Contains(got, "bearerVALUE123456") {
		t.Fatalf("OUTPUT leaked the token field value: %q", got)
	}
	if !strings.Contains(got, redactedSentinel) {
		t.Fatalf("OUTPUT missing the redaction sentinel: %q", got)
	}
	// The non-secret surrounding structure survives.
	if !strings.Contains(got, `"event":"log"`) {
		t.Fatalf("OUTPUT over-scrubbed non-secret content: %q", got)
	}
}

// TestOutputScrubSpanningWrites asserts the line-boundary buffer scrubs a
// secret even when the value is split across two Write calls.
func TestOutputScrubSpanningWrites(t *testing.T) {
	ws := t.TempDir()
	s := mustOpen(t, Config{WorkspacePath: ws, SessionID: "sess-span"})

	if _, err := s.Output().Write([]byte(`{"k":"sk-ant-AB`)); err != nil {
		t.Fatalf("write 1: %v", err)
	}
	if _, err := s.Output().Write([]byte(`CDEFGH12345"}` + "\n")); err != nil {
		t.Fatalf("write 2: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	got := readFile(t, filepath.Join(s.Dir(), wireOutFile))
	if strings.Contains(got, "sk-ant-ABCDEFGH12345") {
		t.Fatalf("split secret leaked: %q", got)
	}
}

// TestCaptureLogWritten asserts the mechanical CAPTURE-LOG ledger is ACTUALLY
// written per capture (AIS-014's executed-model requirement).
func TestCaptureLogWritten(t *testing.T) {
	ws := t.TempDir()
	clk := substrate.NewFakeClock(time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC))
	_ = mustOpen(t, Config{WorkspacePath: ws, SessionID: "sess-a", Clock: clk})
	_ = mustOpen(t, Config{WorkspacePath: ws, SessionID: "sess-b", Clock: clk})

	ledger := readFile(t, filepath.Join(ws, ".harmonik", "sessions", captureLogFile))
	if !strings.Contains(ledger, "sess-a") || !strings.Contains(ledger, "sess-b") {
		t.Fatalf("CAPTURE-LOG missing a session row:\n%s", ledger)
	}
	// Header present exactly once (append-not-clobber).
	if n := strings.Count(ledger, "Session capture ledger"); n != 1 {
		t.Fatalf("CAPTURE-LOG header count = %d, want 1", n)
	}
}

// TestRetentionKeepN asserts the keep-N arm prunes older session dirs so the
// corpus does not grow unbounded (must NOT inherit the 89.5 MB events.jsonl
// defect).
func TestRetentionKeepN(t *testing.T) {
	ws := t.TempDir()
	root := filepath.Join(ws, ".harmonik", "sessions")
	clk := substrate.NewFakeClock(time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC))

	// Open 5 sessions with KeepN=2. Stagger mtimes so "recency" is deterministic.
	base := time.Now().Add(-time.Hour)
	for i := 0; i < 5; i++ {
		id := "sess-" + string(rune('0'+i))
		s := mustOpen(t, Config{WorkspacePath: ws, SessionID: id, KeepN: 2, Clock: clk})
		if err := s.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
		// Bump mtime so later sessions are strictly newer.
		mt := base.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(s.Dir(), mt, mt); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
	}
	// The final Open ran retention over the prior dirs; run one more to prune
	// against the freshly-staged mtimes.
	s := mustOpen(t, Config{WorkspacePath: ws, SessionID: "sess-final", KeepN: 2, Clock: clk})
	mustClose(t, s)

	dirs := countDirs(t, root)
	if dirs > 2 {
		t.Fatalf("retention did not bound the corpus: %d session dirs remain, want <= 2", dirs)
	}
}

// TestRetentionAgePrune asserts the age arm removes stale dirs by mtime,
// measured against the injected ClockPort (RS-015 — no wall-clock).
func TestRetentionAgePrune(t *testing.T) {
	ws := t.TempDir()
	root := filepath.Join(ws, ".harmonik", "sessions")
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	clk := substrate.NewFakeClock(now)

	// One old dir (2h stale) and, via a fresh Open with MaxAge=1h, expect prune.
	old := mustOpen(t, Config{WorkspacePath: ws, SessionID: "old", KeepN: 100, Clock: clk})
	mustClose(t, old)
	staleT := now.Add(-2 * time.Hour)
	if err := os.Chtimes(old.Dir(), staleT, staleT); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	s := mustOpen(t, Config{WorkspacePath: ws, SessionID: "fresh", KeepN: 100, MaxAge: time.Hour, Clock: clk})
	mustClose(t, s)

	if _, err := os.Stat(filepath.Join(root, "old")); !os.IsNotExist(err) {
		t.Fatalf("age-prune did not remove the stale dir (err=%v)", err)
	}
	if _, err := os.Stat(filepath.Join(root, "fresh")); err != nil {
		t.Fatalf("age-prune wrongly removed the fresh dir: %v", err)
	}
}

// TestWriteAfterCloseDegrades asserts a Write after Close is a silent no-op
// (degraded), never a panic or a hard error that could bubble to the run.
func TestWriteAfterCloseDegrades(t *testing.T) {
	ws := t.TempDir()
	s := mustOpen(t, Config{WorkspacePath: ws, SessionID: "sess-closed"})
	in := s.Input()
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if n, err := in.Write([]byte("late")); err != nil || n != 4 {
		t.Fatalf("post-close write: n=%d err=%v, want 4,nil", n, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // test path.
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func countDirs(t *testing.T, root string) int {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("readdir %s: %v", root, err)
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() {
			n++
		}
	}
	return n
}
