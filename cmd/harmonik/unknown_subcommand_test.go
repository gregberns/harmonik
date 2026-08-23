package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

type capture struct {
	text string
	err  error
}

func runWithArgs(t *testing.T, argv ...string) (exitCode int, stderr string) {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}

	origArgs, origStderr := os.Args, os.Stderr
	os.Args, os.Stderr = argv, w
	t.Cleanup(func() { os.Args, os.Stderr = origArgs, origStderr })

	captured := make(chan capture, 1)
	go func() {
		var buf bytes.Buffer
		_, copyErr := io.Copy(&buf, r)
		captured <- capture{text: buf.String(), err: copyErr}
	}()

	exitCode = run()

	os.Stderr = origStderr
	if closeErr := w.Close(); closeErr != nil {
		t.Fatalf("close stderr pipe writer: %v", closeErr)
	}
	got := <-captured
	if closeErr := r.Close(); closeErr != nil {
		t.Fatalf("close stderr pipe reader: %v", closeErr)
	}
	if got.err != nil {
		t.Fatalf("read stderr: %v", got.err)
	}

	return exitCode, got.text
}

// TestUnknownSubcommandIsRefused drives the real entry point. The verbs are
// words no chain block will ever claim, so a future verb addition cannot turn
// this red for the wrong reason.
func TestUnknownSubcommandIsRefused(t *testing.T) {
	for _, verb := range []string{"statu", "quee", "nonesuch"} {
		t.Run(verb, func(t *testing.T) {
			code, stderr := runWithArgs(t, "harmonik", verb)

			if code != exitUnknownSubcommand {
				t.Errorf("run() = %d, want %d — a mistyped verb must be refused, not run", code, exitUnknownSubcommand)
			}
			if !strings.Contains(stderr, verb) {
				t.Errorf("stderr must name the rejected verb %q; got: %s", verb, stderr)
			}
			if !strings.Contains(stderr, "SUBCOMMANDS") {
				t.Errorf("stderr must carry the subcommand listing so the operator can find the right verb; got: %s", stderr)
			}
		})
	}
}

// TestRefusedSubcommandTouchesNothing is the test that gives the refusal its
// meaning. The original defect was an ordering defect: the process reached the
// disk before it decided the argument was garbage.
func TestRefusedSubcommandTouchesNothing(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	code, _ := runWithArgs(t, "harmonik", "statu")
	if code != exitUnknownSubcommand {
		t.Fatalf("run() = %d, want %d", code, exitUnknownSubcommand)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("a refused verb wrote %v into the working directory; it must write nothing", names)
	}
}

// TestFlagFirstArgvIsNotASubcommand states the division of labour between the
// two refusals. unknownSubcommand only judges POSITIONAL words; a flag-first
// argv is not its business and it must decline all of these.
//
// Declining is no longer the same as permitting. Until
// hk-cli-flag-first-starts-daemon-gjhiy, whatever unknownSubcommand declined
// fell through and started a daemon, so this test read as "these spellings are
// allowed to boot". They are now caught by daemonStartRefusal instead — see
// TestOnlyStartDaemonStartsADaemon, which is the test that carries that claim.
func TestFlagFirstArgvIsNotASubcommand(t *testing.T) {
	for _, argv := range [][]string{
		{"harmonik"},
		{"harmonik", "--project", "/tmp/x"},
		{"harmonik", "-project", "/tmp/x"},
		{"harmonik", "--max-concurrent", "4"},
		{"harmonik", "--no-auto-pull"},
		{"harmonik", "--help"},
		{"harmonik", "-h"},
	} {
		if verb, ok := unknownSubcommand(argv); ok {
			t.Errorf("unknownSubcommand(%q) claimed %q; it must judge positional words only", argv, verb)
		}
	}
}

// TestPositionalArgumentIsASubcommand states the rule the refusal reads by: at
// the end of the chain, a word is a verb that does not exist.
func TestPositionalArgumentIsASubcommand(t *testing.T) {
	for _, arg := range []string{"statu", "quee", "nonesuch"} {
		verb, ok := unknownSubcommand([]string{"harmonik", arg})
		if !ok || verb != arg {
			t.Errorf("unknownSubcommand(harmonik %s) = (%q, %v), want (%q, true)", arg, verb, ok, arg)
		}
	}
}
