package workers_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gregberns/harmonik/internal/workers"
)

func writeFile(t *testing.T, dir, content string) {
	t.Helper()
	d := filepath.Join(dir, ".harmonik")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	p := filepath.Join(d, "workers.yaml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestLoad_FileAbsent(t *testing.T) {
	dir := t.TempDir()
	got, err := workers.Load(dir)
	if err != nil {
		t.Fatalf("Load with absent file: unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, workers.Config{}) {
		t.Fatalf("Load with absent file: expected zero Config, got %+v", got)
	}
}

func TestLoad_GoldenFixture(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, `
version: 1
workers:
  - name: mac-studio
    transport: ssh
    host: mac-studio.local
    os: darwin
    repo_path: /Users/deploy/harmonik
    max_slots: 4
    enabled: true
`)
	got, err := workers.Load(dir)
	if err != nil {
		t.Fatalf("Load golden fixture: unexpected error: %v", err)
	}
	want := workers.Config{
		Version: 1,
		Workers: []workers.Worker{
			{
				Name:      "mac-studio",
				Transport: "ssh",
				Host:      "mac-studio.local",
				OS:        "darwin",
				RepoPath:  "/Users/deploy/harmonik",
				MaxSlots:  4,
				Enabled:   true,
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load golden fixture:\n  got  %+v\n  want %+v", got, want)
	}
}

func TestLoad_MalformedYAML(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "version: [not a number")
	_, err := workers.Load(dir)
	if err == nil {
		t.Fatal("Load with malformed YAML: expected error, got nil")
	}
	var target *workers.ErrMalformedYAML
	if !errors.As(err, &target) {
		t.Fatalf("Load with malformed YAML: expected *ErrMalformedYAML, got %T: %v", err, err)
	}
}

func TestLoad_BadVersion(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, `
version: 2
workers:
  - name: remote-1
    transport: ssh
    host: host.example.com
    os: linux
    repo_path: /home/deploy/repo
    max_slots: 2
    enabled: true
`)
	_, err := workers.Load(dir)
	if err == nil {
		t.Fatal("Load with version=2: expected error, got nil")
	}
	var target *workers.ErrUnsupportedVersion
	if !errors.As(err, &target) {
		t.Fatalf("Load with version=2: expected *ErrUnsupportedVersion, got %T: %v", err, err)
	}
	if target.Version != 2 {
		t.Fatalf("ErrUnsupportedVersion.Version: got %d, want 2", target.Version)
	}
}

func TestLoad_TwoWorkers(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, `
version: 1
workers:
  - name: worker-a
    transport: ssh
    host: host-a.example.com
    os: linux
    repo_path: /home/a/repo
    max_slots: 2
    enabled: true
  - name: worker-b
    transport: ssh
    host: host-b.example.com
    os: linux
    repo_path: /home/b/repo
    max_slots: 2
    enabled: true
`)
	_, err := workers.Load(dir)
	if err == nil {
		t.Fatal("Load with two workers: expected error, got nil")
	}
	var target *workers.ErrTooManyWorkers
	if !errors.As(err, &target) {
		t.Fatalf("Load with two workers: expected *ErrTooManyWorkers, got %T: %v", err, err)
	}
	if target.Count != 2 {
		t.Fatalf("ErrTooManyWorkers.Count: got %d, want 2", target.Count)
	}
}
