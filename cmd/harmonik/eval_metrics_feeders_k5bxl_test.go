package main

// eval_metrics_feeders_k5bxl_test.go
// Sensors for `harmonik eval metrics` (WS3b, bead hk-eval-prog-quality-feeders-k5bxl).
//
// Covers the pure feeder functions plus an integration path through
// evalComputeMetrics against a scratch git repo with a minimal evaltask package.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ── Pure-function unit tests ──────────────────────────────────────────────────

func TestEvalDeriveTaskID(t *testing.T) {
	cases := []struct{ in, want string }{
		{"hk-eval-fizzbuzz-avjjr", "eval-fizzbuzz"},
		{"hk-eval-lru-cache-xxxxx", "eval-lru-cache"},
		{"hk-eval-string-reverse-k5bxl", "eval-string-reverse"},
		{"hk-abc", "abc"},
	}
	for _, tc := range cases {
		if got := evalDeriveTaskID(tc.in); got != tc.want {
			t.Errorf("evalDeriveTaskID(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestEvalCountDiffMarkers_None(t *testing.T) {
	diff := `diff --git a/foo.go b/foo.go
+++ b/foo.go
+func Foo() string { return "bar" }
`
	todo, fixme, stub := evalCountDiffMarkers(diff)
	if todo != 0 || fixme != 0 || stub != 0 {
		t.Errorf("got todo=%d fixme=%d stub=%d, want all 0", todo, fixme, stub)
	}
}

func TestEvalCountDiffMarkers_All(t *testing.T) {
	diff := `+// TODO: fix this later
+// FIXME: broken
+	panic("not implemented")
+// context line without marker
`
	todo, fixme, stub := evalCountDiffMarkers(diff)
	if todo != 1 {
		t.Errorf("todo = %d, want 1", todo)
	}
	if fixme != 1 {
		t.Errorf("fixme = %d, want 1", fixme)
	}
	if stub != 1 {
		t.Errorf("stub = %d, want 1", stub)
	}
}

func TestEvalCountDiffMarkers_SkipsContextAndRemoved(t *testing.T) {
	diff := ` // TODO: this is a context line (no leading +)
-// FIXME: removed line
+// actual added line
`
	todo, fixme, stub := evalCountDiffMarkers(diff)
	if todo != 0 || fixme != 0 || stub != 0 {
		t.Errorf("context/removed lines must not be counted: todo=%d fixme=%d stub=%d", todo, fixme, stub)
	}
}

func TestEvalCountDiffMarkers_SkipsPlusHeader(t *testing.T) {
	diff := `+++ b/foo.go
+// actual added line
`
	todo, fixme, stub := evalCountDiffMarkers(diff)
	if todo != 0 || fixme != 0 || stub != 0 {
		t.Errorf("'+++ b/foo.go' must not be counted: got todo=%d fixme=%d stub=%d", todo, fixme, stub)
	}
}

func TestEvalCountDiffAddedLines_ExcludesTestFiles(t *testing.T) {
	diff := `diff --git a/lru.go b/lru.go
+++ b/lru.go
+func Foo() {}
+func Bar() {}
diff --git a/lru_test.go b/lru_test.go
+++ b/lru_test.go
+func TestFoo(t *testing.T) {}
`
	n := evalCountDiffAddedLines(diff)
	if n != 2 {
		t.Errorf("added lines = %d, want 2 (test file lines excluded)", n)
	}
}

func TestEvalCountDiffAddedLines_ExcludesPlusHeader(t *testing.T) {
	diff := `diff --git a/x.go b/x.go
+++ b/x.go
+line1
+line2
`
	n := evalCountDiffAddedLines(diff)
	if n != 2 {
		t.Errorf("added lines = %d, want 2", n)
	}
}

func TestEvalReadBeadIDFromTask(t *testing.T) {
	dir := t.TempDir()
	hDir := filepath.Join(dir, ".harmonik")
	if err := os.Mkdir(hDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "bead_id: hk-eval-fizzbuzz-avjjr\ntitle: some title\n"
	if err := os.WriteFile(filepath.Join(hDir, "agent-task.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := evalReadBeadIDFromTask(dir)
	if err != nil {
		t.Fatalf("evalReadBeadIDFromTask: %v", err)
	}
	if got != "hk-eval-fizzbuzz-avjjr" {
		t.Errorf("got %q, want hk-eval-fizzbuzz-avjjr", got)
	}
}

func TestEvalReadBeadIDFromTask_Missing(t *testing.T) {
	_, err := evalReadBeadIDFromTask(t.TempDir())
	if err == nil {
		t.Error("expected error for missing agent-task.md")
	}
}

func TestEvalReadBeadIDFromTask_NoBead(t *testing.T) {
	dir := t.TempDir()
	hDir := filepath.Join(dir, ".harmonik")
	if err := os.Mkdir(hDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hDir, "agent-task.md"), []byte("title: no bead_id here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := evalReadBeadIDFromTask(dir)
	if err == nil {
		t.Error("expected error when bead_id line absent")
	}
}

// ── gofmt feeder ─────────────────────────────────────────────────────────────

func TestEvalGofmtCheck_Clean(t *testing.T) {
	dir := t.TempDir()
	// Well-formatted Go file.
	src := "package foo\n\nfunc Foo() {}\n"
	f := filepath.Join(dir, "foo.go")
	if err := os.WriteFile(f, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	clean, unformatted := evalGofmtCheck(dir, []string{"foo.go"})
	if !clean {
		t.Errorf("clean = false, want true; unformatted=%v", unformatted)
	}
	if len(unformatted) != 0 {
		t.Errorf("unformatted = %v, want []", unformatted)
	}
}

func TestEvalGofmtCheck_Unformatted(t *testing.T) {
	dir := t.TempDir()
	// Intentionally unformatted (extra space before {).
	src := "package foo\n\nfunc Foo()  { }\n"
	f := filepath.Join(dir, "bar.go")
	if err := os.WriteFile(f, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	clean, unformatted := evalGofmtCheck(dir, []string{"bar.go"})
	if clean {
		t.Error("clean = true, want false for unformatted file")
	}
	if len(unformatted) == 0 {
		t.Error("unformatted list is empty, want at least one entry")
	}
}

func TestEvalGofmtCheck_NoFiles(t *testing.T) {
	clean, unformatted := evalGofmtCheck(t.TempDir(), nil)
	if !clean {
		t.Error("clean = false for empty file list, want true")
	}
	if len(unformatted) != 0 {
		t.Errorf("unformatted = %v, want []", unformatted)
	}
}

// ── Integration: evalComputeMetrics against a scratch git repo ───────────────

// metricsTestRepo sets up a minimal git repo with an evaltask package and
// .harmonik/agent-task.md so evalComputeMetrics can run.  Returns the repo dir.
func metricsTestRepo(t *testing.T, beadID, taskID string, goSrc string) string {
	t.Helper()
	dir := t.TempDir()

	// git init
	cmds := [][]string{
		{"git", "-C", dir, "init"},
		{"git", "-C", dir, "config", "user.email", "test@test.com"},
		{"git", "-C", dir, "config", "user.name", "Test"},
	}
	for _, c := range cmds {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", c, err, out)
		}
	}

	// .harmonik/agent-task.md
	hDir := filepath.Join(dir, ".harmonik")
	if err := os.MkdirAll(hDir, 0o755); err != nil {
		t.Fatal(err)
	}
	agentTask := "bead_id: " + beadID + "\ntitle: test task\n"
	if err := os.WriteFile(filepath.Join(hDir, "agent-task.md"), []byte(agentTask), 0o644); err != nil {
		t.Fatal(err)
	}

	// evaltask package
	pkgDir := filepath.Join(dir, "evaltasks", taskID)
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, taskID+".go"), []byte(goSrc), 0o644); err != nil {
		t.Fatal(err)
	}

	// go.mod so go vet can run
	modContent := "module example.com/evaltest\n\ngo 1.21\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(modContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// commit everything
	addCmds := [][]string{
		{"git", "-C", dir, "add", "."},
		{"git", "-C", dir, "commit", "-m", "initial"},
	}
	for _, c := range addCmds {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", c, err, out)
		}
	}

	return dir
}

func TestEvalComputeMetrics_BasicPass(t *testing.T) {
	goSrc := "package evaltest\n\nfunc Add(a, b int) int { return a + b }\n"
	dir := metricsTestRepo(t, "hk-eval-test-xxxxx", "eval-test", goSrc)

	rec, err := evalComputeMetrics(dir)
	if err != nil {
		t.Fatalf("evalComputeMetrics: %v", err)
	}

	if rec.SchemaVersion != 1 {
		t.Errorf("schema_version = %d, want 1", rec.SchemaVersion)
	}
	if !rec.GofmtClean {
		t.Errorf("gofmt_clean = false, want true for well-formatted source")
	}
	if !rec.VetClean {
		t.Errorf("vet_clean = false, want true for valid source; issues=%v", rec.VetIssues)
	}
	if rec.TodoCount != 0 || rec.FixmeCount != 0 || rec.StubCount != 0 {
		t.Errorf("marker counts = todo:%d fixme:%d stub:%d, want all 0", rec.TodoCount, rec.FixmeCount, rec.StubCount)
	}
	if rec.DiffAddedLines == 0 {
		t.Error("diff_added_lines = 0, want > 0 for a non-empty commit")
	}
	// Fields with no bead labels must be nil.
	if rec.ExpectedBigO != nil {
		t.Errorf("expected_big_o = %v, want nil (no label)", rec.ExpectedBigO)
	}
	if rec.ReferenceLineBudget != nil {
		t.Errorf("reference_line_budget = %v, want nil (no label)", rec.ReferenceLineBudget)
	}
	// HiddenTestPass must be nil (no hidden_test.go).
	if rec.HiddenTestPass != nil {
		t.Errorf("hidden_test_pass = %v, want nil (no hidden_test.go)", rec.HiddenTestPass)
	}
	// UnusedSymbols must be a non-nil slice.
	if rec.UnusedSymbols == nil {
		t.Error("unused_symbols is nil, want []")
	}
	// GofmtUnformatted and VetIssues must be non-nil slices.
	if rec.GofmtUnformatted == nil {
		t.Error("gofmt_unformatted is nil, want []")
	}
	if rec.VetIssues == nil {
		t.Error("vet_issues is nil, want []")
	}
}

func TestEvalComputeMetrics_WithMarkers(t *testing.T) {
	goSrc := "package evaltest\n\nfunc Foo() {\n\t// TODO: implement\n\t// FIXME: broken\n\tpanic(\"not implemented\")\n}\n"
	dir := metricsTestRepo(t, "hk-eval-test-yyyyy", "eval-test", goSrc)

	rec, err := evalComputeMetrics(dir)
	if err != nil {
		t.Fatalf("evalComputeMetrics: %v", err)
	}
	if rec.TodoCount != 1 {
		t.Errorf("todo_count = %d, want 1", rec.TodoCount)
	}
	if rec.FixmeCount != 1 {
		t.Errorf("fixme_count = %d, want 1", rec.FixmeCount)
	}
	if rec.StubCount != 1 {
		t.Errorf("stub_count = %d, want 1", rec.StubCount)
	}
}

func TestRunEvalMetrics_WritesFile(t *testing.T) {
	goSrc := "package evaltest\n\nfunc Sub(a, b int) int { return a - b }\n"
	dir := metricsTestRepo(t, "hk-eval-test-zzzzz", "eval-test", goSrc)

	var stdout, stderr strings.Builder
	code := runEvalMetrics(
		[]string{"--workdir", dir},
		&stdout, &stderr,
		func() (string, error) { return dir, nil },
	)
	if code != 0 {
		t.Fatalf("runEvalMetrics exit %d: stderr=%s", code, stderr.String())
	}

	outPath := filepath.Join(dir, ".harmonik", "metrics.json")
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read metrics.json: %v", err)
	}
	var rec evalMetricsRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("unmarshal metrics.json: %v", err)
	}
	if rec.SchemaVersion != 1 {
		t.Errorf("schema_version = %d, want 1", rec.SchemaVersion)
	}
}

func TestRunEvalMetrics_MissingAgentTask(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik"), 0o755); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	code := runEvalMetrics(
		[]string{"--workdir", dir},
		&stdout, &stderr,
		func() (string, error) { return dir, nil },
	)
	if code == 0 {
		t.Error("expected non-zero exit when agent-task.md is missing")
	}
}

func TestRunEvalCmd_MetricsHelp(t *testing.T) {
	var stdout, stderr strings.Builder
	// "metrics --help" should exit 0 and mention workdir.
	code := runEvalCmd([]string{"metrics", "--help"}, &stdout, &stderr)
	if code != 0 {
		t.Errorf("exit %d, want 0; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "workdir") {
		t.Error("help output should mention --workdir flag")
	}
}
