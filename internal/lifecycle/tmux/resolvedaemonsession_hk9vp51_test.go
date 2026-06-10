package tmux

// resolvedaemonsession_hk9vp51_test.go — unit tests for ResolveDaemonSpawnSession,
// the hk-9vp51 fix-forward of sub-fix #3 (sessionName resolution).
//
// Background: the original sub-fix #3 ALWAYS replaced the live ambient session
// with a boot-time deterministic name (DefaultSessionName) created via
// EnsureSession. Under the real supervisor (daemon runs inside a tmux session),
// that boot-created session did NOT persist to dispatch time, so every
// SpawnWindow failed in 0.6s ("tmux: session does not exist") → P0 dispatch
// regression, reverted fe94e0b1.
//
// The fix-forward (option (a), LOW RISK) keeps dispatch-time live-session
// resolution — which always returns an existing session — and excludes ONLY the
// supervisor's own session, falling back to the deterministic daemon session
// (ensured by the caller) in that one case.
//
// Invariant under test: ResolveDaemonSpawnSession
//   (1) NEVER returns the supervisor session name;
//   (2) NEVER returns empty;
//   (3) returns the live session verbatim (needEnsure=false) for any normal
//       ambient session — guaranteeing the spawn target already exists at
//       dispatch time without an EnsureSession round-trip;
//   (4) returns the deterministic DefaultSessionName (needEnsure=true) ONLY when
//       the live session is the supervisor's or is empty.
//
// NOT covered here (requires live tmux under the supervisor — the orchestrator's
// live-smoke catches it): that EnsureSession actually creates a persistent
// session, that display-message under nested tmux returns the expected name, and
// that the #4 reaper does not subsequently kill the fallback session.

import "testing"

func TestResolveDaemonSpawnSession_NormalSessionUsedVerbatim(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	cases := []string{
		"harmonik",                      // ambient session the daemon ran inside (live evidence)
		"harmonik-abc123def456-default", // operator's hk tmux-start session
		"my-custom-session",             // operator launched the daemon by hand
		"  harmonik  ",                  // leading/trailing whitespace is trimmed
	}
	for _, live := range cases {
		live := live
		t.Run(live, func(t *testing.T) {
			t.Parallel()
			got, needEnsure := ResolveDaemonSpawnSession(projectDir, live)
			want := trimForTest(live)
			if got != want {
				t.Errorf("ResolveDaemonSpawnSession(%q) session = %q, want %q (live session must be used verbatim)", live, got, want)
			}
			if needEnsure {
				t.Errorf("ResolveDaemonSpawnSession(%q) needEnsure = true, want false (live session already exists)", live)
			}
			if got == SupervisorSessionName {
				t.Errorf("ResolveDaemonSpawnSession(%q) returned the supervisor session — INVARIANT VIOLATION", live)
			}
			if got == "" {
				t.Errorf("ResolveDaemonSpawnSession(%q) returned empty — INVARIANT VIOLATION", live)
			}
		})
	}
}

func TestResolveDaemonSpawnSession_SupervisorSessionExcluded(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	want := DefaultSessionName(projectDir)

	// The supervisor's own session must NEVER be the spawn target — fall back to
	// the deterministic daemon session and require the caller to EnsureSession it.
	for _, live := range []string{SupervisorSessionName, "  " + SupervisorSessionName + "  "} {
		live := live
		t.Run(live, func(t *testing.T) {
			t.Parallel()
			got, needEnsure := ResolveDaemonSpawnSession(projectDir, live)
			if got == SupervisorSessionName {
				t.Fatalf("ResolveDaemonSpawnSession(%q) returned the supervisor session — this is the exact P0 leak the bead fixes", live)
			}
			if got != want {
				t.Errorf("ResolveDaemonSpawnSession(%q) session = %q, want %q (deterministic fallback)", live, got, want)
			}
			if !needEnsure {
				t.Errorf("ResolveDaemonSpawnSession(%q) needEnsure = false, want true (fallback session must be ensured)", live)
			}
		})
	}
}

func TestResolveDaemonSpawnSession_EmptyLiveFallsBack(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	want := DefaultSessionName(projectDir)

	// display-message failure → empty live session → deterministic fallback.
	for _, live := range []string{"", "   ", "\t\n"} {
		got, needEnsure := ResolveDaemonSpawnSession(projectDir, live)
		if got != want {
			t.Errorf("ResolveDaemonSpawnSession(empty=%q) session = %q, want %q", live, got, want)
		}
		if !needEnsure {
			t.Errorf("ResolveDaemonSpawnSession(empty=%q) needEnsure = false, want true", live)
		}
		if got == "" {
			t.Errorf("ResolveDaemonSpawnSession(empty=%q) returned empty — INVARIANT VIOLATION", live)
		}
	}
}

// TestResolveDaemonSpawnSession_FallbackIsNeverSupervisor proves the fallback
// name (DefaultSessionName) can never collide with the supervisor session name,
// closing the loop on invariant (1) for the fallback branch.
func TestResolveDaemonSpawnSession_FallbackIsNeverSupervisor(t *testing.T) {
	t.Parallel()

	if DefaultSessionName(t.TempDir()) == SupervisorSessionName {
		t.Fatal("DefaultSessionName collides with SupervisorSessionName — fallback could spawn into the supervisor session")
	}
}

// trimForTest mirrors strings.TrimSpace for the verbatim-use assertion without
// importing strings (kept tiny and local to this test file).
func trimForTest(s string) string {
	start := 0
	end := len(s)
	for start < end && isSpaceForTest(s[start]) {
		start++
	}
	for end > start && isSpaceForTest(s[end-1]) {
		end--
	}
	return s[start:end]
}

func isSpaceForTest(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\v' || b == '\f'
}
