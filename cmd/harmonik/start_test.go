package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

// captureDispatch records which downstream launcher was called and with what
// argv, so the parser can be asserted without spawning tmux or a daemon RPC.
type captureDispatch struct {
	captainCalled bool
	captainArgv   []string
	crewCalled    bool
	crewArgv      []string
}

func (c *captureDispatch) dispatch() startDispatch {
	return startDispatch{
		captain: func(subArgs []string) int {
			c.captainCalled = true
			c.captainArgv = subArgs
			return 0
		},
		crew: func(subArgs []string) int {
			c.crewCalled = true
			c.crewArgv = subArgs
			return 0
		},
	}
}

func TestRunStart_Parser(t *testing.T) {
	tests := []struct {
		name string
		args []string

		wantExit int
		// which downstream is expected (one of "captain", "crew", "none").
		wantRole string
		wantArgv []string
		// substring expected in stderr for error cases.
		wantErrSubstr string
	}{
		// ---- SIMPLE form ----
		{
			name:     "simple captain: bare, no name, no flags",
			args:     []string{"captain"},
			wantExit: 0,
			wantRole: "captain",
			wantArgv: nil,
		},
		{
			name:     "simple crew: one bare positional name",
			args:     []string{"crew", "paul"},
			wantExit: 0,
			wantRole: "crew",
			wantArgv: []string{"start", "paul"},
		},

		// ---- ADVANCED form (all-named, no positional) ----
		{
			name:     "advanced captain: all named",
			args:     []string{"captain", "--name", "captain", "--tmux", "captain"},
			wantExit: 0,
			wantRole: "captain",
			wantArgv: []string{"--name", "captain", "--tmux", "captain"},
		},
		{
			name:     "advanced crew: all named, name via --name",
			args:     []string{"crew", "--name", "paul", "--queue", "paul-q", "--mission", "m.md"},
			wantExit: 0,
			wantRole: "crew",
			// no positional synthesised; flags forwarded verbatim after the verb.
			wantArgv: []string{"start", "--name", "paul", "--queue", "paul-q", "--mission", "m.md"},
		},
		{
			name:     "advanced crew: --flag=value form still counts as a flag",
			args:     []string{"crew", "--name=paul", "--queue=paul-q"},
			wantExit: 0,
			wantRole: "crew",
			wantArgv: []string{"start", "--name=paul", "--queue=paul-q"},
		},

		// ---- MIXING errors (positional name + any flag) ----
		{
			name:          "mixing error: crew bare name + flag",
			args:          []string{"crew", "paul", "--queue", "paul-q"},
			wantExit:      2,
			wantRole:      "none",
			wantErrSubstr: "positional name not allowed alongside flags — use --name paul",
		},
		// A bare token AFTER a flag is flag-land (ambiguous flag-value), so the
		// thin role-agnostic pre-parser forwards it verbatim; the downstream crew
		// launcher is responsible for rejecting a stray positional. The XOR rule
		// only guards a LEADING bare name combined with flags (the case above).
		{
			name:     "trailing bare token after a flag is forwarded (flag-land)",
			args:     []string{"crew", "--queue", "paul-q", "paul"},
			wantExit: 0,
			wantRole: "crew",
			wantArgv: []string{"start", "--queue", "paul-q", "paul"},
		},
		{
			name:          "mixing error: captain bare positional + flag",
			args:          []string{"captain", "skip", "--tmux", "captain"},
			wantExit:      2,
			wantRole:      "none",
			wantErrSubstr: "positional name not allowed alongside flags",
		},

		// ---- captain takes NO positional name ----
		{
			name:          "captain rejects a bare positional (simple form)",
			args:          []string{"captain", "skip"},
			wantExit:      2,
			wantRole:      "none",
			wantErrSubstr: "takes no positional argument",
		},

		// ---- too many positionals ----
		{
			name:          "crew rejects two bare positionals",
			args:          []string{"crew", "paul", "alpha"},
			wantExit:      2,
			wantRole:      "none",
			wantErrSubstr: "at most one positional name",
		},

		// ---- role errors ----
		{
			name:          "unknown role",
			args:          []string{"keeper"},
			wantExit:      2,
			wantRole:      "none",
			wantErrSubstr: `unknown role "keeper"`,
		},
		{
			name:          "missing role",
			args:          []string{},
			wantExit:      2,
			wantRole:      "none",
			wantErrSubstr: "a role is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cap := &captureDispatch{}
			var stdout, stderr bytes.Buffer

			got := runStartWith(tt.args, cap.dispatch(), &stdout, &stderr)
			if got != tt.wantExit {
				t.Fatalf("exit = %d, want %d (stderr=%q)", got, tt.wantExit, stderr.String())
			}

			switch tt.wantRole {
			case "captain":
				if !cap.captainCalled {
					t.Fatalf("expected captain dispatch, got none")
				}
				if cap.crewCalled {
					t.Fatalf("unexpected crew dispatch")
				}
				if !reflect.DeepEqual(cap.captainArgv, tt.wantArgv) {
					t.Fatalf("captain argv = %#v, want %#v", cap.captainArgv, tt.wantArgv)
				}
			case "crew":
				if !cap.crewCalled {
					t.Fatalf("expected crew dispatch, got none")
				}
				if cap.captainCalled {
					t.Fatalf("unexpected captain dispatch")
				}
				if !reflect.DeepEqual(cap.crewArgv, tt.wantArgv) {
					t.Fatalf("crew argv = %#v, want %#v", cap.crewArgv, tt.wantArgv)
				}
			case "none":
				if cap.captainCalled || cap.crewCalled {
					t.Fatalf("expected NO dispatch on error, got captain=%v crew=%v", cap.captainCalled, cap.crewCalled)
				}
			}

			if tt.wantErrSubstr != "" && !strings.Contains(stderr.String(), tt.wantErrSubstr) {
				t.Fatalf("stderr = %q, want substring %q", stderr.String(), tt.wantErrSubstr)
			}
		})
	}
}

// TestRunStart_HelpForwarding: a --help token in the role args is treated as a
// flag (no positional name) and forwarded so the downstream launcher prints its
// own full flag listing.
func TestRunStart_HelpForwarding(t *testing.T) {
	cap := &captureDispatch{}
	var stdout, stderr bytes.Buffer

	if got := runStartWith([]string{"crew", "--help"}, cap.dispatch(), &stdout, &stderr); got != 0 {
		t.Fatalf("exit = %d, want 0", got)
	}
	if !cap.crewCalled {
		t.Fatalf("expected crew dispatch for --help forwarding")
	}
	if !reflect.DeepEqual(cap.crewArgv, []string{"start", "--help"}) {
		t.Fatalf("crew argv = %#v, want [start --help]", cap.crewArgv)
	}
}

// TestRunStart_TopLevelHelp: `start --help` prints umbrella usage and exits 0
// WITHOUT dispatching to a role.
func TestRunStart_TopLevelHelp(t *testing.T) {
	cap := &captureDispatch{}
	var stdout, stderr bytes.Buffer

	if got := runStartWith([]string{"--help"}, cap.dispatch(), &stdout, &stderr); got != 0 {
		t.Fatalf("exit = %d, want 0", got)
	}
	if cap.captainCalled || cap.crewCalled {
		t.Fatalf("top-level --help must not dispatch a role")
	}
	if !strings.Contains(stdout.String(), "harmonik start") {
		t.Fatalf("expected umbrella usage on stdout, got %q", stdout.String())
	}
}
