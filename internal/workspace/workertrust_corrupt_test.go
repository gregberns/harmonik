package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/handlercontract"
)

func runWorkerTrustProgram(t *testing.T, cfgPath, worktree string) ([]byte, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "python3", "-", worktree)
	cmd.Stdin = strings.NewReader(workerTrustUpsertProgram(cfgPath, time.Second))
	cmd.Env = append(os.Environ(), "HOME="+t.TempDir())
	out, err := cmd.CombinedOutput()
	return out, err
}

func assertConfigUntouched(t *testing.T, cfgPath string, want []byte) {
	t.Helper()

	//nolint:gosec // G304: cfgPath is inside this test's t.TempDir fixture.
	got, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("the config is gone after the program ran; it was supposed to be left alone: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("the config was rewritten. Every key that was in it and is not in the new one is lost.\n"+
			"before: %s\nafter:  %s", want, got)
	}
}

func TestWorkerTrustUpsert_UnparseableConfigIsLeftAlone(t *testing.T) {
	worktree := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), ".claude.json")

	corrupt := []byte(`{"projects": {"/some/other/worktree": {"hasTrustDialogAccepted": true}}, "oauth`)
	if err := os.WriteFile(cfgPath, corrupt, 0o600); err != nil {
		t.Fatalf("seed the corrupt config: %v", err)
	}

	out, err := runWorkerTrustProgram(t, cfgPath, worktree)

	if err == nil {
		t.Fatalf("the program exited 0 on a config it could not parse. It either wrote nothing and "+
			"claimed success, or it rebuilt the file.\noutput: %s", out)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != workerConfigUnparseableExit {
		t.Fatalf("the program failed with %v, not with the unparseable-config status %d, so the Go side "+
			"cannot tell this apart from any other non-zero exit.\noutput: %s",
			err, workerConfigUnparseableExit, out)
	}
	assertConfigUntouched(t, cfgPath, corrupt)
	if !strings.Contains(string(out), cfgPath) {
		t.Errorf("the failure message does not name the config file %s, so nobody can find it.\noutput: %s",
			cfgPath, out)
	}
}

func TestWorkerTrustUpsert_NonObjectConfigIsLeftAlone(t *testing.T) {
	worktree := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), ".claude.json")

	notAnObject := []byte(`["this is not a claude config"]`)
	if err := os.WriteFile(cfgPath, notAnObject, 0o600); err != nil {
		t.Fatalf("seed the non-object config: %v", err)
	}

	out, err := runWorkerTrustProgram(t, cfgPath, worktree)

	if err == nil {
		t.Fatalf("the program exited 0 on a config that is not a JSON object.\noutput: %s", out)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != workerConfigUnparseableExit {
		t.Fatalf("the program failed with %v, not with the unparseable-config status %d.\noutput: %s",
			err, workerConfigUnparseableExit, out)
	}
	assertConfigUntouched(t, cfgPath, notAnObject)
}

// TestWorkerTrustUpsert_MissingConfigIsStillCreated pins the case the fix must
// NOT break. "Cannot read it" and "it is not there" are different, and only the
// second one is safe to answer with a fresh file. Every first launch against a
// new worker depends on this staying true.
func TestWorkerTrustUpsert_MissingConfigIsStillCreated(t *testing.T) {
	worktree := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), ".claude.json")

	out, err := runWorkerTrustProgram(t, cfgPath, worktree)
	if err != nil {
		t.Fatalf("the program failed on an absent config, which is the one case it must handle by "+
			"creating the file: %v\noutput: %s", err, out)
	}

	//nolint:gosec // G304: cfgPath is inside this test's t.TempDir fixture.
	got, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("the config was not created: %v", err)
	}
	if !strings.Contains(string(got), "hasTrustDialogAccepted") {
		t.Errorf("the created config carries no trust entry, so the launch it was seeding would park on "+
			"the trust dialog.\nconfig: %s", got)
	}
}

// TestEnsureWorktreeTrustVia_UnparseableConfigIsStructural pins the Go half of
// the contract. A launch that cannot seed trust MUST NOT exec claude — an
// un-trusted session parks on the trust dialog before SessionStart, no
// agent_ready is ever synthesized, and the run dies at its ready deadline with
// nothing saying why. Structural is what makes the dispatch path report it.
func TestEnsureWorktreeTrustVia_UnparseableConfigIsStructural(t *testing.T) {
	runner := fixedResultRunner{script: fmt.Sprintf("echo 'not valid JSON' >&2; exit %d", workerConfigUnparseableExit)}

	err := EnsureWorktreeTrustVia(t.Context(), runner, t.TempDir())

	if err == nil {
		t.Fatal("EnsureWorktreeTrustVia returned nil although the worker refused to touch the config")
	}
	if !errors.Is(err, ErrTrustConfigUnparseable) {
		t.Errorf("error is not ErrTrustConfigUnparseable, so a caller cannot tell this apart from a "+
			"generic worker failure: %v", err)
	}
	if !errors.Is(err, handlercontract.ErrStructural) {
		t.Errorf("error is not structural, so the launch would be retried against a config that will "+
			"fail exactly the same way: %v", err)
	}
}

// TestWorkerTrustUpsert_NullConfigIsTreatedAsEmpty pins PARITY with the
// in-process path rather than a safety property. readClaudeConfigMap has an
// explicit branch for a literal "null" body and substitutes an empty map,
// because null holds nothing and overwriting it discards nothing. The remote
// program makes the same exception. Without this test the two paths could drift
// apart on the one input where "not an object" and "not any data" disagree.
func TestWorkerTrustUpsert_NullConfigIsTreatedAsEmpty(t *testing.T) {
	worktree := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), ".claude.json")

	if err := os.WriteFile(cfgPath, []byte("null"), 0o600); err != nil {
		t.Fatalf("seed the null config: %v", err)
	}

	out, err := runWorkerTrustProgram(t, cfgPath, worktree)
	if err != nil {
		t.Fatalf("the program refused a null config, where the in-process path treats it as empty and "+
			"writes. The two paths now disagree on this input: %v\noutput: %s", err, out)
	}

	//nolint:gosec // G304: cfgPath is inside this test's t.TempDir fixture.
	got, readErr := os.ReadFile(cfgPath)
	if readErr != nil {
		t.Fatalf("read the config back: %v", readErr)
	}
	if !strings.Contains(string(got), "hasTrustDialogAccepted") {
		t.Errorf("the trust entry was not written over the null body.\nconfig: %s", got)
	}
}
