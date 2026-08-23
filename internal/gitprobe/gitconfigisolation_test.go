package gitprobe_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestGitConfigIsIsolatedFromTheHost fails if the package TestMain stops
// redirecting the global gitconfig.
//
// It asks git what git resolved rather than reading the generated file, so it
// still holds if git changes how it merges its config sources — and so it also
// catches an outer GIT_CONFIG_GLOBAL that points somewhere without these values.
func TestGitConfigIsIsolatedFromTheHost(t *testing.T) {
	t.Parallel()

	if os.Getenv("GIT_CONFIG_GLOBAL") == "" {
		t.Fatal("GIT_CONFIG_GLOBAL is unset, so every git fixture in this package reads the " +
			"operator's ~/.gitconfig; this package needs a TestMain calling hermetic.Main")
	}

	for _, want := range []struct{ key, value string }{
		{"commit.gpgsign", "false"},
		{"tag.gpgsign", "false"},
		{"user.name", "Harmonik Test"},
	} {
		//nolint:gosec // G204: the key comes from the literal list above, not from input.
		out, err := exec.CommandContext(t.Context(), "git", "config", "--get", want.key).Output()
		if err != nil {
			t.Errorf("git config --get %s: %v (a host without this value fails every fixture here)", want.key, err)
			continue
		}
		if got := strings.TrimSpace(string(out)); got != want.value {
			t.Errorf("git resolved %s = %q, want %q", want.key, got, want.value)
		}
	}
}
