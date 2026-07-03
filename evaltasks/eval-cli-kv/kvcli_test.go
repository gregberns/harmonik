package kvcli_test

// kvcli_test.go — tamper-proof acceptance test for the eval-cli-kv task.
// Invokes the CLI in-process via kvcli.Run (same argv contract as os/exec, no binary build).
// TestCLI covers the required sequence: set→get→del→get-miss(exit 1)→list.
// TestCLI_CorruptStore covers the malformed-store error path.
// TestCLI_Idempotent covers idempotent del (missing key, exit 0) and double-set.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	kvcli "github.com/gregberns/harmonik/evaltasks/eval-cli-kv"
)

// invoke runs the CLI in-process with the given store path and subcommand args.
func invoke(t *testing.T, storePath string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	all := append([]string{"--store", storePath}, args...)
	var outBuf, errBuf bytes.Buffer
	code = kvcli.Run(all, &outBuf, &errBuf)
	return outBuf.String(), errBuf.String(), code
}

// TestCLI is the primary acceptance gate.
// Sequence: set → get → del → get-miss (exit 1) → list.
func TestCLI(t *testing.T) {
	store := filepath.Join(t.TempDir(), "kv.json")

	// ── set ──────────────────────────────────────────────────────────────────
	out, _, code := invoke(t, store, "set", "name", "alice")
	if code != 0 {
		t.Fatalf("set: want exit 0, got %d", code)
	}
	if out != "set name\n" {
		t.Errorf("set stdout: got %q, want %q", out, "set name\n")
	}

	// ── get ──────────────────────────────────────────────────────────────────
	out, _, code = invoke(t, store, "get", "name")
	if code != 0 {
		t.Fatalf("get: want exit 0, got %d", code)
	}
	if out != "alice\n" {
		t.Errorf("get stdout: got %q, want %q", out, "alice\n")
	}

	// ── del ──────────────────────────────────────────────────────────────────
	out, _, code = invoke(t, store, "del", "name")
	if code != 0 {
		t.Fatalf("del: want exit 0, got %d", code)
	}
	if out != "del name\n" {
		t.Errorf("del stdout: got %q, want %q", out, "del name\n")
	}

	// ── get-miss: key removed → must exit 1 ──────────────────────────────────
	_, errOut, code := invoke(t, store, "get", "name")
	if code != 1 {
		t.Errorf("get-miss: want exit 1, got %d", code)
	}
	if !strings.Contains(errOut, "not found") {
		t.Errorf("get-miss stderr: got %q, want mention of 'not found'", errOut)
	}

	// ── populate then list ────────────────────────────────────────────────────
	invoke(t, store, "set", "b", "2") //nolint:errcheck
	invoke(t, store, "set", "a", "1") //nolint:errcheck
	invoke(t, store, "set", "c", "3") //nolint:errcheck

	out, _, code = invoke(t, store, "list")
	if code != 0 {
		t.Fatalf("list: want exit 0, got %d", code)
	}
	want := "a\t1\nb\t2\nc\t3\n"
	if out != want {
		t.Errorf("list stdout:\ngot  %q\nwant %q", out, want)
	}
}

// TestCLI_CorruptStore confirms exit 1 and a "malformed" error for invalid JSON.
func TestCLI_CorruptStore(t *testing.T) {
	store := filepath.Join(t.TempDir(), "kv.json")
	if err := os.WriteFile(store, []byte("{bad json"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, errOut, code := invoke(t, store, "get", "anykey")
	if code != 1 {
		t.Errorf("corrupt store get: want exit 1, got %d", code)
	}
	if !strings.Contains(errOut, "malformed") {
		t.Errorf("corrupt store stderr: got %q, want mention of 'malformed'", errOut)
	}

	// set also fails on a corrupt store
	_, errOut, code = invoke(t, store, "set", "k", "v")
	if code != 1 {
		t.Errorf("corrupt store set: want exit 1, got %d", code)
	}
	if !strings.Contains(errOut, "malformed") {
		t.Errorf("corrupt store set stderr: got %q, want mention of 'malformed'", errOut)
	}
}

// TestCLI_Idempotent covers del on a missing key (must exit 0) and double-set.
func TestCLI_Idempotent(t *testing.T) {
	store := filepath.Join(t.TempDir(), "kv.json")

	// del on a key that was never set is idempotent → exit 0
	_, _, code := invoke(t, store, "del", "ghost")
	if code != 0 {
		t.Errorf("del nonexistent: want exit 0, got %d", code)
	}

	// double set overwrites silently → get returns the latest value
	invoke(t, store, "set", "x", "first")  //nolint:errcheck
	invoke(t, store, "set", "x", "second") //nolint:errcheck
	out, _, code := invoke(t, store, "get", "x")
	if code != 0 {
		t.Fatalf("get after double-set: want exit 0, got %d", code)
	}
	if out != "second\n" {
		t.Errorf("get after double-set: got %q, want %q", out, "second\n")
	}
}
