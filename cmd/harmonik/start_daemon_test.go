package main

// start_daemon_test.go — a daemon starts because somebody named it, never
// because an argv ran out of verbs to match.
//
// The defect: the daemon was what run() did when nothing else claimed the
// arguments. Three spellings therefore started one by accident —
//
//	harmonik                        (no arguments at all)
//	harmonik --project DIR          (flags, no verb)
//	harmonik --project DIR status   (there is no `status` subcommand)
//
// — because unknownSubcommand declines anything beginning with "-" and anything
// with no argument, and everything it declined fell through to flag.Parse.
//
// The third is the one that cost real time. During a daemon redeploy an operator
// poll-checking `harmonik --project X status` started a SECOND daemon that
// contended with the one being revived; it hung a poll loop and probably killed
// an early revive attempt during the 2026-06-30 deploy. The runbook then warned
// readers off the SAFE spelling and left the hazardous one unmarked, so a careful
// reader was steered into it.
//
// Bead ref: hk-cli-flag-first-starts-daemon-gjhiy.
//
// These tests call run in this process, which is safe for the same reason
// unknown_subcommand_test.go documents: the refusal returns before run registers
// its flags and reads the working directory. A "flag redefined: project" panic
// from this file means somebody moved the refusal below the daemon setup — which
// would also mean the process reached the disk before deciding not to boot.

import (
	"os"
	"strings"
	"testing"
)

// TestOnlyStartDaemonStartsADaemon is the load-bearing test. Each argv is one
// that used to boot a daemon; all must now be refused.
//
// Measured under mutation on 2026-08-10: disabling the refusal in run() does not
// turn this red, it makes it HANG until the test binary's timeout. That is the
// defect stated as plainly as it can be — with the refusal gone, `harmonik
// --project /tmp/x` starts a daemon inside the test process and never returns.
// So a timeout panic here is not flakiness; read it as the refusal being gone.
func TestOnlyStartDaemonStartsADaemon(t *testing.T) {
	for _, tc := range []struct {
		name string
		argv []string
	}{
		{"bare", []string{"harmonik"}},
		{"flags only", []string{"harmonik", "--project", "/tmp/x"}},
		{"single dash", []string{"harmonik", "-project", "/tmp/x"}},
		{"concurrency only", []string{"harmonik", "--max-concurrent", "4"}},
		{"trailing status", []string{"harmonik", "--project", "/tmp/x", "status"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, stderr := runWithArgs(t, tc.argv...)

			if code != exitUnknownSubcommand {
				t.Errorf("run(%q) = %d, want %d — this spelling must not start a daemon", tc.argv, code, exitUnknownSubcommand)
			}
			if !strings.Contains(stderr, "start daemon") {
				t.Errorf("the refusal must name the one verb that starts a daemon; got: %s", stderr)
			}
			if !strings.Contains(stderr, "SUBCOMMANDS") {
				t.Errorf("the refusal must carry the subcommand listing; got: %s", stderr)
			}
		})
	}
}

// TestRefusedDaemonStartTouchesNothing is what gives the refusal its meaning.
// The original defect wrote .harmonik/ into whatever directory the operator
// stood in and then spawned a supervisor to revive itself, so a refusal that
// happens after the daemon has already reached the disk is no refusal at all.
func TestRefusedDaemonStartTouchesNothing(t *testing.T) {
	for _, argv := range [][]string{
		{"harmonik"},
		{"harmonik", "--no-auto-pull"},
		{"harmonik", "--project", ".", "status"},
	} {
		dir := t.TempDir()
		t.Chdir(dir)

		if code, _ := runWithArgs(t, argv...); code != exitUnknownSubcommand {
			t.Fatalf("run(%q) = %d, want %d", argv, code, exitUnknownSubcommand)
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
			t.Errorf("refused argv %q wrote %v into the working directory; it must write nothing", argv, names)
		}
	}
}

// TestRefusalNamesTheIgnoredWord covers the case that caused the incident. The
// operator typed something that reads like a status check; the word was silently
// dropped and a daemon started. Naming the dropped word is what turns a
// confusing refusal into an obvious one.
//
// It deliberately does NOT echo the word back as a suggested command: `status`
// is not a subcommand, so `harmonik status …` would recommend something that
// does not exist.
func TestRefusalNamesTheIgnoredWord(t *testing.T) {
	got := daemonStartRefusal([]string{"harmonik", "--project", "/tmp/x", "status"})

	if !strings.Contains(got, `"status" was ignored`) {
		t.Errorf("the refusal must name the word it dropped; got: %s", got)
	}
	if strings.Contains(got, "harmonik status") {
		t.Errorf("the refusal must not suggest `harmonik status`, which is not a subcommand; got: %s", got)
	}
	if !strings.Contains(got, "start daemon") {
		t.Errorf("the refusal must name the verb that does start a daemon; got: %s", got)
	}
}

// TestTrailingPositionalIgnoresFlagValues guards the fail-noisy direction. A
// flag's VALUE is a bare word too, so a hint that cannot tell them apart would
// report `harmonik --project /tmp/x` as having dropped "/tmp/x" and send the
// reader after a word that was used correctly.
func TestTrailingPositionalIgnoresFlagValues(t *testing.T) {
	for _, tc := range []struct {
		name string
		argv []string
		want string
	}{
		{"flag value is not a dropped word", []string{"harmonik", "--project", "/tmp/x"}, ""},
		{"no arguments", []string{"harmonik"}, ""},
		{"trailing flag", []string{"harmonik", "--project", "/tmp/x", "--no-auto-pull"}, ""},
		{"real trailing word", []string{"harmonik", "--project", "/tmp/x", "status"}, "status"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := trailingPositional(tc.argv); got != tc.want {
				t.Errorf("trailingPositional(%q) = %q, want %q", tc.argv, got, tc.want)
			}
		})
	}
}

// TestStartDaemonIsARecognisedRole stops `start daemon` from regressing into the
// unknown-role arm. run() intercepts it before the role dispatch, so the arm in
// runStart is unreachable in production — but if the interception is ever
// removed, the operator must not be told that `daemon` is not a thing.
func TestStartDaemonIsARecognisedRole(t *testing.T) {
	var stdout, stderr strings.Builder

	code := runStartWith([]string{"daemon"}, startDispatch{}, &stdout, &stderr)

	if code == 0 {
		t.Fatal("runStart(daemon) returned 0; the role launcher does not start daemons — run() intercepts first")
	}
	if strings.Contains(stderr.String(), "unknown role") {
		t.Errorf("`start daemon` must never read as an unknown role; got: %s", stderr.String())
	}
}

// TestStartUsageNamesDaemon keeps the help honest. The refusal points the
// operator at `harmonik start daemon`, so `harmonik start --help` must list it.
func TestStartUsageNamesDaemon(t *testing.T) {
	var b strings.Builder
	if err := startUsage(&b); err != nil {
		t.Fatalf("startUsage: %v", err)
	}
	if !strings.Contains(b.String(), "start daemon") {
		t.Errorf("`harmonik start --help` must list the daemon role; got: %s", b.String())
	}
}
