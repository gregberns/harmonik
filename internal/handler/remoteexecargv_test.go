package handler_test

// remoteexecargv_test.go — hk-qxvc2/hk-okqyx: RemoteExecArgv rewrites a launch into
// an `env KEY=VAL … binary args` argv so env survives an ssh login-shell exec.

import (
	"reflect"
	"testing"

	"github.com/gregberns/harmonik/internal/handler"
)

func TestRemoteExecArgv_ForwardsSurvivingEnv(t *testing.T) {
	t.Parallel()

	env := []string{
		"CLAUDE_CONFIG_DIR=/wt/.harmonik/claude-config", // kept
		"CLAUDE_CODE_OAUTH_TOKEN=",                      // dropped (empty deny-list override)
		"PATH=/box-a/bin",                               // dropped (box-A specific)
		"HOME=/box-a/home",                              // dropped (box-A specific)
		"FOO=bar",                                       // kept
	}
	name, argv := handler.RemoteExecArgv(env, "claude", []string{"--flag", "x"})

	if name != "env" {
		t.Fatalf("name = %q; want \"env\"", name)
	}
	want := []string{
		"CLAUDE_CONFIG_DIR=/wt/.harmonik/claude-config",
		"FOO=bar",
		"claude",
		"--flag",
		"x",
	}
	if !reflect.DeepEqual(argv, want) {
		t.Fatalf("argv = %#v; want %#v", argv, want)
	}
}

func TestRemoteExecArgv_PreservesOrder(t *testing.T) {
	t.Parallel()

	env := []string{"A=1", "B=2", "C=3"}
	name, argv := handler.RemoteExecArgv(env, "bin", []string{"arg"})
	if name != "env" {
		t.Fatalf("name = %q; want \"env\"", name)
	}
	want := []string{"A=1", "B=2", "C=3", "bin", "arg"}
	if !reflect.DeepEqual(argv, want) {
		t.Fatalf("argv = %#v; want %#v", argv, want)
	}
}

func TestRemoteExecArgv_NilEnvUnchanged(t *testing.T) {
	t.Parallel()

	args := []string{"a", "b"}
	name, argv := handler.RemoteExecArgv(nil, "bin", args)
	if name != "bin" {
		t.Fatalf("name = %q; want \"bin\" (unchanged)", name)
	}
	if !reflect.DeepEqual(argv, args) {
		t.Fatalf("argv = %#v; want %#v (unchanged)", argv, args)
	}
}

func TestRemoteExecArgv_AllFilteredUnchanged(t *testing.T) {
	t.Parallel()

	// Every entry drops out: empty value, PATH, HOME, and a non-assignment token.
	env := []string{"CLAUDE_CODE_OAUTH_TOKEN=", "PATH=/x", "HOME=/y", "novalue"}
	args := []string{"app-server"}
	name, argv := handler.RemoteExecArgv(env, "codex", args)
	if name != "codex" {
		t.Fatalf("name = %q; want \"codex\" (unchanged — nothing survived)", name)
	}
	if !reflect.DeepEqual(argv, args) {
		t.Fatalf("argv = %#v; want %#v (unchanged)", argv, args)
	}
}

func TestRemoteExecArgv_BinaryAfterPrefixBeforeArgs(t *testing.T) {
	t.Parallel()

	name, argv := handler.RemoteExecArgv([]string{"K=V"}, "binary", []string{"arg1", "arg2"})
	if name != "env" {
		t.Fatalf("name = %q; want \"env\"", name)
	}
	// binary must sit AFTER all KEY=VAL prefixes and BEFORE args.
	binIdx, kvIdx, argIdx := -1, -1, -1
	for i, tok := range argv {
		switch tok {
		case "binary":
			binIdx = i
		case "K=V":
			kvIdx = i
		case "arg1":
			argIdx = i
		}
	}
	if kvIdx >= binIdx || binIdx >= argIdx {
		t.Fatalf("positions kv=%d binary=%d arg1=%d; want kv < binary < arg1 in %#v", kvIdx, binIdx, argIdx, argv)
	}
}
