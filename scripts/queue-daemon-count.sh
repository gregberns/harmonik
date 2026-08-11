#!/usr/bin/env bash
# queue-daemon-count.sh — count the live harmonik daemons on this host.
#
# `make queue-dogfood-readiness` hands this number to
# `harmonik queue readiness validate --daemons-alive`. The validator refuses a
# dogfood run when the number is above --max-daemons. A number that is wrongly
# zero can never reach that ceiling, so a wrong zero turns the check off. That
# is why every step here refuses with exit 2 instead of guessing.
#
# A live daemon is `<bin> start daemon --project <dir>`. `start daemon` is the
# only spelling that starts one, which is why scripts/scratch-daemon.sh warns
# that `pkill -f "harmonik start daemon --project"` would take the fleet daemon
# down. The three words must stay next to each other. `harmonik queue submit
# --project X` is a CLI call and not a daemon, and the adjacency is what
# excludes it.
#
# This script does not write that pattern. It reads it from
# `harmonik supervise ps --json`, the verb whose job is to print the canonical
# signature, and then drops the trailing project directory to count daemons for
# every project on this host. A hard-coded pattern is how this check failed
# twice: 'harmonik daemon' never matched any process, and 'harmonik --project'
# stopped matching when the start verb was named. Both times the count read zero
# on a busy host and the ceiling in internal/queue/readiness checkHost could
# never fire — the fail-open direction that check exists to refuse.
#
# The count reads pgrep's exit status instead of piping straight to `wc -l`.
# pgrep exits 1 when it finds no process and 2 or more when it could not look. A
# pipe hides the difference and turns the second case into a zero, which is the
# fail-open again in a new place. Only exit 1 becomes a zero here.
#
# Usage: queue-daemon-count.sh <harmonik-binary> <project-dir>
#   Prints the count on stdout and exits 0.
#   Prints a refusal on stderr and exits 2 when it cannot measure the count.
#
# scripts/queue-daemon-count-test.sh holds this script's assertions. It runs
# inside script-tests, so it runs in both `make fast` and `make full`.

set -u

# Every message keeps the make target's prefix, because the operator reads them
# in that target's output.
say() {
	printf 'queue-dogfood-readiness: %s\n' "$*" >&2
}

bin="${1:-}"
project="${2:-}"

test -n "$bin" || {
	say "the daemon count needs the harmonik binary as its first argument"
	exit 2
}
test -n "$project" || {
	say "the daemon count needs the project directory as its second argument"
	exit 2
}

command -v jq >/dev/null 2>&1 || {
	say "jq is required to read the canonical daemon signature"
	exit 2
}
command -v pgrep >/dev/null 2>&1 || {
	say "pgrep is required to count live daemons"
	exit 2
}

ps_json="$("$bin" supervise ps --project "$project" --json)" || {
	say "'harmonik supervise ps' failed, so the daemon count cannot be trusted"
	exit 2
}
real_dir="$(printf '%s' "$ps_json" | jq -er '.project_dir')" || {
	say "'supervise ps' printed no project_dir"
	exit 2
}
daemon_sig="$(printf '%s' "$ps_json" | jq -er '.process_signatures[] | select(.name == "daemon") | .pattern')" || {
	say "'supervise ps' printed no daemon signature"
	exit 2
}

daemon_pat="${daemon_sig% $real_dir}"
test "$daemon_pat" != "$daemon_sig" || {
	say "the daemon signature '$daemon_sig' does not end in the project dir '$real_dir'; refusing to guess a host-wide pattern"
	exit 2
}

# Say the pattern out loud. It is derived, so the operator cannot read it from
# this file, and a wrong pattern is the failure this whole script guards.
say "counting daemons with the pattern '$daemon_pat'"

if daemon_pids="$(pgrep -f "$daemon_pat")"; then
	daemons="$(printf '%s\n' "$daemon_pids" | wc -l | tr -d ' ')"
else
	pgrep_rc=$?
	test "$pgrep_rc" = 1 || {
		say "pgrep exited $pgrep_rc, so the daemon count cannot be trusted; refusing rather than reporting zero"
		exit 2
	}
	daemons=0
fi

# The count above runs through `wc` and `tr`. Neither is guarded by name, so a
# host without one of them leaves the count empty and this script would exit 0
# on an empty line. The caller does catch that today, because --daemons-alive
# parses as an int and an empty value is a parse error. That is the caller
# saving us. The guarantee belongs here with the other refusals, so that it
# holds whatever the caller does with the number.
test -n "$daemons" || {
	say "the daemon count came out empty, so it cannot be trusted. Check that wc and tr are on PATH."
	exit 2
}

printf '%s\n' "$daemons"
