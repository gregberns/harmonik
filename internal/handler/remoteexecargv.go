package handler

import "strings"

// RemoteExecArgv rewrites a (binary, args) launch into an `env KEY=VAL … binary args`
// argv so that env vars survive delivery over an ssh login-shell exec (RemoteCwdRunner.
// CommandInDir emits `exec <name> <args>`; ssh does NOT forward client env and the login
// shell carries no env prefix — hk-qxvc2/hk-okqyx). It returns the rewritten (name, argv):
//   - ("env", [filtered KEY=VAL…, binary, args…]) when any env survives the filter
//   - (binary, args) unchanged when nothing survives (byte-identical to the pre-fix path)
//
// Filter — only vars that must AUGMENT the worker's ambient login env are forwarded:
//   - DROP empty-valued entries. LaunchSpec.Env carries deliberate empty credential
//     overrides (e.g. CLAUDE_CODE_OAUTH_TOKEN=) — the tmux-substrate deny-list mechanism
//     that zeroes a leaked tmux-server credential. On the ssh path there is no tmux server;
//     the remote agent authenticates via the worker's AMBIENT login env (OAUTH from
//     ~/.zshenv). Forwarding an empty value would CLOBBER that ambient credential and break
//     auth. The deny-list targets a threat model that does not exist on this path.
//   - DROP PATH and HOME. These are box-A(daemon-host)-specific; the worker's login shell
//     provides its own. Overriding them would point the remote agent at nonexistent paths.
func RemoteExecArgv(env []string, binary string, args []string) (execName string, execArgv []string) {
	prefix := make([]string, 0, len(env))
	for _, kv := range env {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			// No '=' at all: not a KEY=VAL assignment; skip it.
			continue
		}
		key := kv[:eq]
		val := kv[eq+1:]
		if val == "" {
			continue
		}
		if key == "PATH" || key == "HOME" {
			continue
		}
		prefix = append(prefix, kv)
	}
	if len(prefix) == 0 {
		return binary, args
	}
	argv := make([]string, 0, len(prefix)+1+len(args))
	argv = append(argv, prefix...)
	argv = append(argv, binary)
	argv = append(argv, args...)
	return "env", argv
}
