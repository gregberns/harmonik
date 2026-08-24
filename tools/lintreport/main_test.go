package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestFindingIdentitySurvivesCrossPackageMove(t *testing.T) {
	root := t.TempDir()
	oldPath := filepath.Join(root, "oldpkg", "worker.go")
	newPath := filepath.Join(root, "newpkg", "worker.go")
	source := []byte("package worker\n\nfunc Run() {\n\tprintln(\"work\")\n}\n")
	if err := os.MkdirAll(filepath.Dir(oldPath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldPath, source, 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, root, "init", "--quiet")
	git(t, root, "add", ".")
	git(t, root, "-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "--quiet", "-m", "seed")
	before := writeReport(t, root, oldPath)
	oldFindings, err := readFindings(before)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(newPath), 0o750); err != nil {
		t.Fatal(err)
	}
	git(t, root, "mv", oldPath, newPath)
	after := writeReport(t, root, newPath)
	newFindings, err := readFindings(after)
	if err != nil {
		t.Fatal(err)
	}
	oldKey := onlyKey(t, oldFindings)
	newKey := onlyKey(t, newFindings)
	if oldKey != newKey {
		t.Fatalf("move changed identity: %#v != %#v", oldKey, newKey)
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func TestFindingIdentityChangesForNewFinding(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "worker.go")
	if err := os.WriteFile(path, []byte("package worker\nfunc Run() { println(\"work\") }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := readFindings(writeReport(t, root, path))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package worker\nfunc Run() { println(\"new work\") }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := readFindings(writeReport(t, root, path))
	if err != nil {
		t.Fatal(err)
	}
	if onlyKey(t, first) == onlyKey(t, second) {
		t.Fatal("changed finding body retained its identity")
	}
}

func writeReport(t *testing.T, root, source string) string {
	t.Helper()
	var r report
	var i issue
	i.FromLinter = "gocognit"
	i.Text = "cognitive complexity 31 of func Run is high (> 30)"
	i.Pos.Filename = source
	i.Pos.Line = 2
	r.Issues = append(r.Issues, i)
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, filepath.Base(filepath.Dir(source))+".json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func onlyKey(t *testing.T, findings map[key][]finding) key {
	t.Helper()
	if len(findings) != 1 {
		t.Fatalf("got %d findings, want 1", len(findings))
	}
	for k := range findings {
		return k
	}
	return key{}
}
