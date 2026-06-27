package workflow

// shellquote.go — canonical POSIX single-quote primitive (WG-045 security).
//
// ShellQuote is the single source of truth for shell-quoting a value that will be
// concatenated into a `/bin/sh -c` (local) or `/bin/sh -lc` (remote login-shell)
// command string. It is SECURITY-LOAD-BEARING: substituted template-param values
// bound for a node `tool_command` are wrapped with ShellQuote so they become one
// inert shell word that cannot inject a command separator, subshell, redirect, or
// quote break-out (WG-045 / WG-046). The daemon's remote gate path delegates to
// this same function so there is exactly one quoting implementation to audit.
//
// Tags: mechanism, normative

import "strings"

// ShellQuote wraps s in single quotes for safe interpolation into a POSIX shell
// command string, escaping any embedded single quote via the standard '\” idiom.
//
// Inside a single-quoted string every byte is literal — `"`, `$`, backtick, `;`,
// `|`, `&`, `\`, `>`, `(`, `)`, glob, whitespace, and newline all lose their
// special meaning — so the result is a single shell word equal to s. The empty
// string maps to ” (an explicit empty argument). Used for both mid-token
// concatenation (e.g. `--url=http://x/'VALUE'`, which the shell joins into one
// word) and standalone arguments.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
