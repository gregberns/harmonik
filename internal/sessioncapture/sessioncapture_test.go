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
	if !strings.Contains(got, `"event":"log"`) {
		t.Fatalf("OUTPUT over-scrubbed non-secret content: %q", got)
	}
}

// TestOutputScrubEscapedQuoteInValue asserts a secret whose value contains a
// JSON-escaped quote (\") is scrubbed in full — the value terminates at the
// first UNescaped quote, so the real tail after the escape must not survive.
// Regression for hk-13ff4 (keyValuePattern value group stopped at the escaped
// quote, leaking the suffix verbatim).
func TestOutputScrubEscapedQuoteInValue(t *testing.T) {
	ws := t.TempDir()
	s := mustOpen(t, Config{WorkspacePath: ws, SessionID: "sess-esc"})

	line := `{"event":"log","token":"abc\"def"}` + "\n"
	if _, err := s.Output().Write([]byte(line)); err != nil {
		t.Fatalf("output write: %v", err)
	}
	mustClose(t, s)

	got := readFile(t, filepath.Join(s.Dir(), wireOutFile))
	if strings.Contains(got, "def") {
		t.Fatalf("OUTPUT leaked the secret tail past the escaped quote: %q", got)
	}
	if !strings.Contains(got, redactedSentinel) {
		t.Fatalf("OUTPUT missing the redaction sentinel: %q", got)
	}
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

	base := time.Now().Add(-time.Hour)
	for i := 0; i < 5; i++ {
		id := "sess-" + string(rune('0'+i))
		s := mustOpen(t, Config{WorkspacePath: ws, SessionID: id, KeepN: 2, Clock: clk})
		if err := s.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
		mt := base.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(s.Dir(), mt, mt); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
	}
	s := mustOpen(t, Config{WorkspacePath: ws, SessionID: "sess-final", KeepN: 2, Clock: clk})
	mustClose(t, s)

	dirs := countDirs(t, root)
	if dirs > 2 {
		t.Fatalf("retention did not bound the corpus: %d session dirs remain, want <= 2", dirs)
	}
}

func setModTime(t *testing.T, dir string, want time.Time) time.Time {
	t.Helper()
	if err := os.Chtimes(dir, want, want); err != nil {
		t.Fatalf("chtimes %s: %v", dir, err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat %s: %v", dir, err)
	}
	return info.ModTime()
}

// TestRetentionAgePrune asserts the age arm removes stale dirs by mtime,
// measured against the injected ClockPort (RS-015 — no wall-clock).
//
// WHY THE VIRTUAL NOW IS DERIVED FROM A STAT'D MOD-TIME. The age arm computes
// Clock.Now().Sub(mtime): an INJECTED clock against a REAL filesystem mtime. A
// fake clock set to an arbitrary calendar date is not commensurate with that
// mtime. This test used to pin now at 2026-07-15 while the dir that stands for
// "fresh" kept a real mtime of today, so its age came out about MINUS 28 days,
// no threshold could read true, and the assertion that the fresh dir survived
// was satisfied for free. It was also calendar-dependent: run before that date
// it computed a large POSITIVE age and deleted the very dir it exists to
// protect. Anchoring virtual now to a mod-time the test itself planted makes
// every age exact and removes the calendar from the fixture. Refs: hk-3ty39,
// and the same idiom in internal/keeper/watcher_test.go
// driveWatcherFakeClockFrom.
//
// WHY THE FRESH DIR IS FIVE MINUTES OLD RATHER THAN BRAND NEW. An age of zero
// survives any positive limit, so a zero-age fixture cannot tell a working age
// arm from a disabled one. At five minutes it survives the one-hour limit and
// is removed the moment the limit drops below five minutes — the negative
// control that proves this test can fail. Run that control by lowering the
// trigger Open's MaxAge, NOT the maxAge const: the const also feeds the two
// fixture guards above, so lowering it trips a guard and never reaches the
// assertion the control is aimed at.
func TestRetentionAgePrune(t *testing.T) {
	const (
		freshAge = 5 * time.Minute
		staleAge = 2 * time.Hour
		maxAge   = time.Hour
	)

	ws := t.TempDir()
	root := filepath.Join(ws, ".harmonik", "sessions")

	oldSess := mustOpen(t, Config{WorkspacePath: ws, SessionID: "old", KeepN: 100})
	mustClose(t, oldSess)
	freshSess := mustOpen(t, Config{WorkspacePath: ws, SessionID: "fresh", KeepN: 100})
	mustClose(t, freshSess)

	freshMT := setModTime(t, freshSess.Dir(), time.Now().Add(-freshAge))
	now := freshMT.Add(freshAge)
	oldMT := setModTime(t, oldSess.Dir(), now.Add(-staleAge))
	clk := substrate.NewFakeClock(now)

	if got := now.Sub(oldMT); got <= maxAge {
		t.Fatalf("fixture: stale dir age %v is not past the %v limit", got, maxAge)
	}
	if got := now.Sub(freshMT); got <= 0 || got >= maxAge {
		t.Fatalf("fixture: fresh dir age %v is not inside (0, %v)", got, maxAge)
	}

	trigger := mustOpen(t, Config{WorkspacePath: ws, SessionID: "trigger", KeepN: 100, MaxAge: maxAge, Clock: clk})
	mustClose(t, trigger)

	if _, err := os.Stat(filepath.Join(root, "old")); !os.IsNotExist(err) {
		t.Fatalf("age-prune did not remove the stale dir (err=%v)", err)
	}
	if _, err := os.Stat(filepath.Join(root, "fresh")); err != nil {
		t.Fatalf("age-prune wrongly removed the fresh dir (age %v, limit %v): %v",
			now.Sub(freshMT), maxAge, err)
	}
	if _, err := os.Stat(filepath.Join(root, "trigger")); err != nil {
		t.Fatalf("age-prune removed the dir it had just created — the injected clock is not commensurate with the filesystem: %v", err)
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
