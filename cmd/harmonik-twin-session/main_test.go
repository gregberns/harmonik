package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newTestState builds a twinState with a deterministic seed session_id and an
// isolated HANDOFF path under t.TempDir() — no exec, no tmux, no real scripts.
func newTestState(t *testing.T) (*twinState, string) {
	t.Helper()
	handoff := filepath.Join(t.TempDir(), "HANDOFF-twin.md")
	return &twinState{
		tokens:      10_000,
		sessionID:   "00000000-0000-4000-8000-000000000000", // valid v4 seed
		window:      1_000_000,
		model:       "claude-opus-4-8 [1m]",
		startTokens: 10_000,
		handoffPath: handoff,
		seen:        make(map[string]bool),
	}, handoff
}

// readHandoff returns the trimmed contents of the handoff file, or "" if absent.
func readHandoff(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatalf("read handoff: %v", err)
	}
	return string(b)
}

func TestHandleLine_HandoffWritesNonce(t *testing.T) {
	st, handoff := newTestState(t)
	const nonce = "<!-- KEEPER:cyc-123 -->"
	line := "/session-handoff " + handoff + "\n\nIMPORTANT: include exactly this line verbatim: " + nonce

	if changed := st.handleLine(line); changed {
		t.Fatalf("handoff should not report a token/session change")
	}

	got := readHandoff(t, handoff)
	if !contains(got, nonce) {
		t.Fatalf("handoff file missing nonce line.\nwant substring: %q\ngot: %q", nonce, got)
	}
}

// TestHandleLine_MultiLineHandoffWritesNonce proves the twin extracts the nonce
// from the REAL production directive shape, where the nonce is on a LATER line
// than the "/session-handoff" trigger. cycle.go:553-556 emits exactly:
//
//	/session-handoff <path>\n\nIMPORTANT: include exactly this line verbatim in the handoff file: <nonce>
//
// keeper.InjectText pastes that as one bracketed paste; a real Claude REPL sees
// it as one prompt, but the twin's bufio.Scanner splits stdin on '\n', so the
// trigger and the nonce arrive as SEPARATE handleLine calls (see the
// twin_paste_probe in the bead notes). The twin must arm on the trigger and
// scan the continuation lines for the nonce — this is the hk-fan fix.
func TestHandleLine_MultiLineHandoffWritesNonce(t *testing.T) {
	st, handoff := newTestState(t)
	const nonce = "<!-- KEEPER:cyc-20260612T010203-000001 -->"

	// The exact lines a line-by-line stdin reader sees when keeper.InjectText
	// pastes the production directive (cycle.go fmt.Sprintf). Line 1 carries the
	// trigger (no nonce), line 2 is the blank "\n\n" gap, line 3 carries the
	// nonce (no trigger).
	lines := []string{
		"/session-handoff " + handoff,
		"",
		"IMPORTANT: include exactly this line verbatim in the handoff file: " + nonce,
	}
	for _, l := range lines {
		if changed := st.handleLine(l); changed {
			t.Fatalf("handoff lines should not report a token/session change (line %q)", l)
		}
	}

	got := readHandoff(t, handoff)
	if !contains(got, nonce) {
		t.Fatalf("multi-line handoff did not land the nonce.\nwant substring: %q\ngot: %q", nonce, got)
	}
}

// TestHandleLine_MultiLineHandoffIdempotent proves a redelivered multi-line
// directive (the injector's settle+retry Enters can double-deliver the whole
// paste) does NOT rewrite the handoff file once the nonce is seen.
func TestHandleLine_MultiLineHandoffIdempotent(t *testing.T) {
	st, handoff := newTestState(t)
	const nonce = "<!-- KEEPER:cyc-dup -->"
	deliver := func() {
		for _, l := range []string{
			"/session-handoff " + handoff,
			"",
			"IMPORTANT: include exactly this line verbatim in the handoff file: " + nonce,
		} {
			st.handleLine(l)
		}
	}
	deliver()
	if !contains(readHandoff(t, handoff), nonce) {
		t.Fatalf("first multi-line delivery did not land the nonce")
	}
	// Corrupt the file, redeliver the SAME directive; idempotency means the twin
	// recognizes the already-seen nonce and does NOT rewrite.
	if err := os.WriteFile(handoff, []byte("CORRUPTED"), 0o600); err != nil {
		t.Fatalf("seed corruption: %v", err)
	}
	deliver()
	if got := readHandoff(t, handoff); got != "CORRUPTED" {
		t.Fatalf("redelivered multi-line handoff rewrote the file: %q", got)
	}
}

// TestHandleLine_ClearDisarmsPendingHandoff proves a /clear that arrives while a
// handoff is armed (trigger seen, nonce not yet) ends the scan, so a later
// stray nonce-shaped line does not retroactively write a handoff. In production
// the keeper only injects /clear AFTER the nonce confirms, so an armed-but-
// unconfirmed handoff at /clear time is stale and must not capture later prose.
func TestHandleLine_ClearDisarmsPendingHandoff(t *testing.T) {
	st, handoff := newTestState(t)
	st.tokens = 900_000 // so /clear actually fires
	const nonce = "<!-- KEEPER:cyc-stale -->"

	// Arm with a nonce-less trigger, then /clear before any nonce arrives.
	if st.handleLine("/session-handoff " + handoff) {
		t.Fatalf("nonce-less trigger should not report a change")
	}
	if !st.handleLine("/clear") {
		t.Fatalf("/clear should report a state change")
	}
	// A stray nonce-shaped line AFTER the /clear must NOT write a handoff: the
	// scan was disarmed.
	st.handleLine("IMPORTANT: include exactly this line verbatim: " + nonce)
	if got := readHandoff(t, handoff); got != "" {
		t.Fatalf("stale post-/clear line wrote a handoff: %q", got)
	}
}

func TestHandleLine_HandoffWithoutNonceIsNoop(t *testing.T) {
	st, handoff := newTestState(t)
	if st.handleLine("/session-handoff " + handoff) {
		t.Fatalf("nonce-less handoff should be a no-op")
	}
	if readHandoff(t, handoff) != "" {
		t.Fatalf("nonce-less handoff must not write the file")
	}
}

func TestHandleLine_ClearRotatesToValidUUIDv4(t *testing.T) {
	st, _ := newTestState(t)
	// Drive tokens up so the reset is observable.
	st.tokens = 950_000
	seed := st.sessionID

	if changed := st.handleLine("/clear"); !changed {
		t.Fatalf("/clear must report a state change")
	}

	if st.sessionID == seed {
		t.Fatalf("/clear did not rotate the session_id (still %q)", seed)
	}
	if !isValidUUIDv4(st.sessionID) {
		t.Fatalf("rotated session_id %q is not a valid UUIDv4", st.sessionID)
	}
	// Version nibble at index 14 MUST be '4', never '7' (keeper rejects v7).
	if st.sessionID[14] != '4' {
		t.Fatalf("session_id version nibble = %q, want '4' (got %q)", string(st.sessionID[14]), st.sessionID)
	}
	if st.tokens != st.startTokens {
		t.Fatalf("/clear did not reset tokens: got %d, want %d", st.tokens, st.startTokens)
	}
}

func TestHandleLine_ResumeHoldsState(t *testing.T) {
	st, _ := newTestState(t)
	st.tokens = 12_345
	sid := st.sessionID

	if changed := st.handleLine("/session-resume " + st.handoffPath); changed {
		t.Fatalf("/session-resume must not mutate state")
	}
	if st.tokens != 12_345 || st.sessionID != sid {
		t.Fatalf("/session-resume changed state: tokens=%d sid=%q", st.tokens, st.sessionID)
	}
}

func TestHandleLine_DuplicateHandoffIsIdempotent(t *testing.T) {
	st, handoff := newTestState(t)
	const nonce = "<!-- KEEPER:cyc-abc -->"
	line := "/session-handoff " + handoff + " include verbatim: " + nonce

	st.handleLine(line)
	first := readHandoff(t, handoff)

	// Corrupt the file, then re-deliver the SAME line; idempotency means the
	// twin recognizes the nonce as already-seen and does NOT rewrite it.
	if err := os.WriteFile(handoff, []byte("CORRUPTED"), 0o600); err != nil {
		t.Fatalf("seed corruption: %v", err)
	}
	if st.handleLine(line) {
		t.Fatalf("duplicate handoff should be a no-op")
	}
	if got := readHandoff(t, handoff); got != "CORRUPTED" {
		t.Fatalf("duplicate handoff rewrote the file: %q (first write was %q)", got, first)
	}
}

func TestHandleLine_DuplicateClearDoesNotDoubleRotate(t *testing.T) {
	st, _ := newTestState(t)
	st.tokens = 900_000

	if !st.handleLine("/clear") {
		t.Fatalf("first /clear should change state")
	}
	afterFirst := st.sessionID

	// A redelivered /clear for the SAME (already-cleared) session must be a
	// no-op — no second rotation (the injector's retry Enters can double-deliver).
	if st.handleLine("/clear") {
		t.Fatalf("duplicate /clear should be a no-op (no double rotation)")
	}
	if st.sessionID != afterFirst {
		t.Fatalf("duplicate /clear rotated the session_id again: %q -> %q", afterFirst, st.sessionID)
	}
}

func TestHandleLine_BlankLineIgnored(t *testing.T) {
	st, _ := newTestState(t)
	sid := st.sessionID
	for _, blank := range []string{"", "   ", "\t", "  \r"} {
		if st.handleLine(blank) {
			t.Fatalf("blank line %q should be ignored", blank)
		}
	}
	if st.sessionID != sid {
		t.Fatalf("blank lines mutated state")
	}
}

func TestBuildStatusJSON_WindowZeroOmitsContextWindowSize(t *testing.T) {
	snap := statusSnapshot{tokens: 50_000, sessionID: "sid-x", window: 0, model: "claude-opus-4-8 [1m]"}
	raw := marshalStatusJSON(snap)

	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatalf("unmarshal: %v\njson: %s", err, raw)
	}
	// Top-level context_window_size MUST be absent (the [1m] quirk).
	if _, ok := generic["context_window_size"]; ok {
		t.Fatalf("window==0 must OMIT top-level context_window_size; json: %s", raw)
	}
	cw, ok := generic["context_window"].(map[string]any)
	if !ok {
		t.Fatalf("context_window object missing; json: %s", raw)
	}
	if _, ok := cw["context_window_size"]; ok {
		t.Fatalf("window==0 must OMIT nested context_window.context_window_size; json: %s", raw)
	}
	// The fields the script reads must still be present.
	if _, ok := cw["used_percentage"]; !ok {
		t.Fatalf("missing .context_window.used_percentage; json: %s", raw)
	}
	if tok, ok := cw["total_input_tokens"].(float64); !ok || int64(tok) != 50_000 {
		t.Fatalf("missing/wrong .context_window.total_input_tokens; json: %s", raw)
	}
	if generic["session_id"] != "sid-x" {
		t.Fatalf("missing/wrong .session_id; json: %s", raw)
	}
	if generic["model"] != "claude-opus-4-8 [1m]" {
		t.Fatalf("missing/wrong .model; json: %s", raw)
	}
}

func TestBuildStatusJSON_WindowSetPresentAndCorrect(t *testing.T) {
	snap := statusSnapshot{tokens: 300_000, sessionID: "sid-y", window: 1_000_000, model: "m"}
	raw := marshalStatusJSON(snap)

	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatalf("unmarshal: %v\njson: %s", err, raw)
	}
	// Top-level path (script tries .context_window_size first).
	top, ok := generic["context_window_size"].(float64)
	if !ok || int64(top) != 1_000_000 {
		t.Fatalf(".context_window_size missing/wrong; json: %s", raw)
	}
	cw := generic["context_window"].(map[string]any)
	// Nested fallback path (.context_window.context_window_size).
	nested, ok := cw["context_window_size"].(float64)
	if !ok || int64(nested) != 1_000_000 {
		t.Fatalf(".context_window.context_window_size missing/wrong; json: %s", raw)
	}
	// used_percentage derived from tokens/window: 300000/1000000 = 30.
	if pct, ok := cw["used_percentage"].(float64); !ok || pct != 30.0 {
		t.Fatalf("used_percentage = %v, want 30.0; json: %s", cw["used_percentage"], raw)
	}
}

// TestBuildStatusJSON_FieldPathsMatchScript pins the exact JSON keys the script
// reads, so a rename here is caught even if the struct tags drift.
func TestBuildStatusJSON_FieldPathsMatchScript(t *testing.T) {
	st, _ := newTestState(t)
	var top map[string]json.RawMessage
	if err := json.Unmarshal(st.buildStatusJSON(), &top); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"context_window", "context_window_size", "session_id", "model"} {
		if _, ok := top[key]; !ok {
			t.Fatalf("top-level key %q missing (window=%d)", key, st.window)
		}
	}
	var cw map[string]json.RawMessage
	if err := json.Unmarshal(top["context_window"], &cw); err != nil {
		t.Fatalf("unmarshal context_window: %v", err)
	}
	for _, key := range []string{"used_percentage", "total_input_tokens", "context_window_size"} {
		if _, ok := cw[key]; !ok {
			t.Fatalf("context_window key %q missing", key)
		}
	}
}

// TestBuildStatusJSON_EmitNA proves --emit-na emits a NON-numeric
// used_percentage ("NA") under the same field path, so the real
// keeper-statusline.sh numeric guard (script line ~69) skips the .ctx write.
// The other fields the script reads stay present and well-formed.
func TestBuildStatusJSON_EmitNA(t *testing.T) {
	snap := statusSnapshot{tokens: 300_000, sessionID: "sid-na", window: 1_000_000, model: "m", emitNA: true}
	raw := marshalStatusJSON(snap)

	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatalf("unmarshal: %v\njson: %s", err, raw)
	}
	var cw map[string]json.RawMessage
	if err := json.Unmarshal(top["context_window"], &cw); err != nil {
		t.Fatalf("unmarshal context_window: %v\njson: %s", err, raw)
	}
	// used_percentage must be the JSON string "NA" — NOT a number. A numeric
	// value would defeat the script's skip guard.
	var pct any
	if err := json.Unmarshal(cw["used_percentage"], &pct); err != nil {
		t.Fatalf("unmarshal used_percentage: %v\njson: %s", err, raw)
	}
	if s, ok := pct.(string); !ok || s != "NA" {
		t.Fatalf("emit-na used_percentage = %#v; want string \"NA\"; json: %s", pct, raw)
	}
	// session_id / model still present (faithful full statusLine shape).
	if string(top["session_id"]) != `"sid-na"` {
		t.Fatalf("missing/wrong session_id; json: %s", raw)
	}
	if _, ok := top["model"]; !ok {
		t.Fatalf("missing model; json: %s", raw)
	}
}

// TestStatuslineSuppressed covers the pure suppression gate behind
// --suppress-statusline-after: a zero deadline never suppresses; a set deadline
// suppresses only at/after the instant.
func TestStatuslineSuppressed(t *testing.T) {
	base := time.Date(2026, 6, 16, 0, 0, 0, 0, time.UTC)
	// Zero deadline (flag unset / 0): never suppressed.
	if statuslineSuppressed(time.Time{}, base) {
		t.Fatal("zero deadline must never suppress")
	}
	deadline := base.Add(1 * time.Second)
	if statuslineSuppressed(deadline, base) {
		t.Fatal("before the deadline must not suppress")
	}
	if !statuslineSuppressed(deadline, deadline) {
		t.Fatal("at the deadline must suppress")
	}
	if !statuslineSuppressed(deadline, deadline.Add(time.Millisecond)) {
		t.Fatal("after the deadline must suppress")
	}
}

func TestNewUUIDv4_IsV4AndUnique(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id := newUUIDv4()
		if !isValidUUIDv4(id) {
			t.Fatalf("newUUIDv4 produced invalid v4: %q", id)
		}
		if id[14] != '4' {
			t.Fatalf("version nibble %q != '4' in %q", string(id[14]), id)
		}
		if isUUIDv7Local(id) {
			t.Fatalf("newUUIDv4 produced a v7-shaped id: %q", id)
		}
		if seen[id] {
			t.Fatalf("duplicate UUID minted: %q", id)
		}
		seen[id] = true
	}
}

// --- test-local helpers (no production dependency) ---

// isValidUUIDv4 checks the canonical 8-4-4-4-12 layout, version nibble '4' at
// index 14, and RFC-4122 variant (8/9/a/b) at index 19.
func isValidUUIDv4(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !isHex(r) {
				return false
			}
		}
	}
	if s[14] != '4' {
		return false
	}
	switch s[19] {
	case '8', '9', 'a', 'b', 'A', 'B':
		return true
	}
	return false
}

// isUUIDv7Local mirrors internal/keeper.isUUIDv7 (sid[14]=='7') for the test.
func isUUIDv7Local(s string) bool { return len(s) == 36 && s[14] == '7' }

func isHex(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && indexOfTest(haystack, needle) >= 0)
}

func indexOfTest(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}
