package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindFilesHonoursScopeAndExclusions(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	write := func(rel string) {
		abs := filepath.Join(repo, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte("package p\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, rel := range []string{
		"internal/a/a.go",
		"internal/a/testdata/fixture.go",
		"internal/a/.hidden/h.go",
		"cmd/b/b.go",
		"tools/c/c.go",
		"test/d/d.go",
		"evaltasks/e/e.go",
		"docs/f/f.go",
		"internal/a/a.txt",
	} {
		write(rel)
	}
	got, err := findFiles(repo, nil, nil, nil)
	if err != nil {
		t.Fatalf("findFiles: %v", err)
	}
	want := []string{"cmd/b/b.go", "internal/a/a.go", "test/d/d.go", "tools/c/c.go"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %q, want %q", got, want)
	}

	filtered, err := findFiles(repo, nil, []string{"cmd/"}, nil)
	if err != nil {
		t.Fatalf("findFiles: %v", err)
	}
	if len(filtered) != 1 || filtered[0] != "cmd/b/b.go" {
		t.Fatalf("path filter got %q", filtered)
	}

	if _, err := findFiles(repo, []string{"nope"}, nil, nil); err == nil {
		t.Fatal("expected an error for a missing scope root")
	}
}

func TestPackagesOfDeduplicates(t *testing.T) {
	t.Parallel()
	got := packagesOf([]string{"internal/a/x.go", "internal/a/y.go", "cmd/b/z.go"})
	want := []string{"./cmd/b", "./internal/a"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestModeClassSetsAreStated(t *testing.T) {
	t.Parallel()
	safe, err := lookupMode("safe")
	if err != nil {
		t.Fatal(err)
	}
	if len(safe.Classes) != 1 || safe.Classes[0] != AttachFuncBodyFloating {
		t.Fatalf("safe mode cuts %v, want only %s", safe.Classes, AttachFuncBodyFloating)
	}
	// Every mode must be a superset of the one before it, or "aggressive" and
	// "max" stop meaning what their names say.
	for i := 1; i < len(modes); i++ {
		prev := map[Attach]bool{}
		for _, c := range modes[i-1].Classes {
			prev[c] = true
		}
		have := map[Attach]bool{}
		for _, c := range modes[i].Classes {
			have[c] = true
		}
		for c := range prev {
			if !have[c] {
				t.Errorf("mode %s drops class %s that mode %s cuts", modes[i].Name, c, modes[i-1].Name)
			}
		}
	}
	// No mode may ever release a protected class.
	protected := []Attach{
		AttachPkgDoc, AttachDocExported, AttachStructFieldDoc, AttachStructFieldTrail,
		AttachIfaceMemberDoc, AttachIfaceMemberTrail, AttachSpecTrail, AttachTrailingOther,
	}
	for _, m := range modes {
		for _, c := range m.Classes {
			for _, p := range protected {
				if c == p {
					t.Errorf("mode %s cuts protected class %s", m.Name, p)
				}
			}
		}
		if !strings.Contains(m.Describe(), "classes cut:") {
			t.Errorf("mode %s does not print its class set", m.Name)
		}
	}
	if _, err := lookupMode("nope"); err == nil {
		t.Fatal("expected an error for an unknown mode")
	}
}
