package main

import (
	"strings"
	"testing"
)

func streamDefaultFixtureResolveKind(t *testing.T, extraArgs []string) string {
	t.Helper()

	kind := resolveGroupKind(extraArgs)
	return string(kind)
}

// TestRunStreamDefaultKindIsStream verifies that omitting --wave produces
// GroupKindStream (the new default introduced by hk-7nbey).
func TestRunStreamDefaultKindIsStream(t *testing.T) {
	t.Parallel()
	got := streamDefaultFixtureResolveKind(t, []string{})
	if got != "stream" {
		t.Errorf("expected default group kind %q, got %q", "stream", got)
	}
}

// TestRunStreamWaveFlagProducesWave verifies that --wave opts back into
// GroupKindWave.
func TestRunStreamWaveFlagProducesWave(t *testing.T) {
	t.Parallel()
	got := streamDefaultFixtureResolveKind(t, []string{"--wave"})
	if got != "wave" {
		t.Errorf("expected group kind %q with --wave, got %q", "wave", got)
	}
}

// TestRunStreamHelpMentionsWave verifies that --wave is documented in the
// usage output so operators can discover the opt-out.
func TestRunStreamHelpMentionsWave(t *testing.T) {
	t.Parallel()

	out := streamDefaultFixtureCaptureUsage(t)
	if !strings.Contains(out, "--wave") {
		t.Errorf("runUsage() output does not mention --wave; got:\n%s", out)
	}
}

func streamDefaultFixtureCaptureUsage(t *testing.T) string {
	t.Helper()
	var buf strings.Builder
	if err := runUsage(&buf); err != nil {
		t.Fatalf("runUsage: %v", err)
	}
	return buf.String()
}
