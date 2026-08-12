package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestEnsureIntegrationBranchCreatesOnceAndConverges(t *testing.T) {
	repo := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = repo
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "--initial-branch=integration")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	if err := os.WriteFile(filepath.Join(repo, "README"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "README")
	run("commit", "-m", "base")
	base := run("rev-parse", "integration")

	const branch = "harmonik/integration/hk-parent"
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- EnsureIntegrationBranch(context.Background(), repo, branch, "integration")
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("EnsureIntegrationBranch: %v", err)
		}
	}
	if got := run("rev-parse", "refs/heads/"+branch); got != base {
		t.Fatalf("derived branch tip = %q; want %q", got, base)
	}
}
