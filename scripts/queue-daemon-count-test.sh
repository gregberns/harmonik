#!/usr/bin/env bash
# queue-daemon-count-test.sh — proves scripts/queue-daemon-count.sh refuses
# rather than reporting a daemon count of zero it cannot stand behind.
#
# THE FAILURE THIS GUARDS. `make queue-dogfood-readiness` hands the count to
# `harmonik queue readiness validate --daemons-alive`, and the validator refuses
# a run when the count is above --max-daemons. A count that is wrongly zero can
# never reach the ceiling, so a wrong zero turns the ceiling off and the run
# starts on a host that already has daemons on it. The count has read a wrong
# zero twice from a hard-coded pgrep pattern that stopped matching. A reviewer
# then found a third way: `pgrep | wc -l` cannot tell exit 1 (found nothing)
# from exit 2 or more (could not read the process table), so an error became a
# zero.
#
# The pattern half of the repair got a Go test. This file is the shell half.
#
# WHAT THE COUNT USED TO BE. Do not read the extraction as a move of working
# code. Before this change the whole count was one unconditional line in the
# make recipe:
#
#     daemons="$(pgrep -f 'harmonik --project' 2>/dev/null | wc -l | tr -d ' ')"
#
# That line could not refuse. It could not refuse, because there was nothing in
# it that a failure could reach: the pattern was written by hand and matched no
# live daemon after the start verb was named, `2>/dev/null` threw away whatever
# pgrep had to say, and the pipe into `wc -l` turned every pgrep exit code into a
# line count. Each of those on its own produces a zero, a zero clears the
# --max-daemons ceiling, and the line reported that zero as a measurement. One
# unconditional line was the defect.
#
# WHY THE DERIVATION IS A SCRIPT. A make recipe line cannot be driven by a test.
# So the derivation became scripts/queue-daemon-count.sh, and the recipe now
# calls it. Every refusal in that script is NEW work, not a move: the target can
# now stop where before it always continued. That is the intended change, and it
# is why this file exists. The last cases below read the expanded recipe and
# prove the call is still there and still carries the refusal.
#
# HOW THE CASES ARE BUILT. Each case runs the REAL script with a PATH that holds
# only the tools that case wants, and with a stub `harmonik` that prints one
# canned `supervise ps --json` answer. Leaving jq, pgrep, or wc off that PATH is
# how the "tool is absent" cases are built. A stub pgrep with a chosen exit code
# is how the fail-open case is built.
#
# Every refusal the script has gets a case here: no binary argument, no project
# argument, no jq, no pgrep, a failed `supervise ps`, no project_dir, no daemon
# signature, a signature that does not end in the project dir, a pgrep that
# could not look, and a count that came out empty. Add a refusal to the script
# and add its case here.
#
# The happy path uses harmless `sleep` processes that carry a daemon-shaped
# argv. It never starts a daemon. The signature in that case carries a token
# unique to this run, so a real fleet daemon on this host cannot change the
# count and this test cannot count one.

set -uo pipefail

repo_root=$(git rev-parse --show-toplevel) || {
	echo "queue-daemon-count-test: not inside a git worktree" >&2
	exit 1
}
cd "$repo_root" || exit 1

script="$repo_root/scripts/queue-daemon-count.sh"
test -x "$script" || {
	echo "queue-daemon-count-test: $script is missing or not executable" >&2
	exit 1
}

work=$(mktemp -d "${TMPDIR:-/tmp}/queue-daemon-count-test-XXXXXX")
kids=""
cleanup() {
	for kid in $kids; do
		kill "$kid" 2>/dev/null
	done
	rm -rf "$work"
}
trap cleanup EXIT INT TERM

failures=0
assertions=0

fail() {
	printf 'queue-daemon-count-test: FAIL: %s\n' "$*" >&2
	failures=$((failures + 1))
}

pass() {
	printf 'queue-daemon-count-test: ok: %s\n' "$*"
}

# ---------------------------------------------------------------------------
# make_sandbox <name> <tool>...
#
# Builds a directory whose bin holds symlinks to exactly the named host tools
# and nothing else. Sets SANDBOX. `bash` must be one of them, because the script
# runs through its own `#!/usr/bin/env bash` line and env resolves bash on the
# PATH it is given.
# ---------------------------------------------------------------------------
make_sandbox() {
	SANDBOX="$work/$1"
	shift
	mkdir -p "$SANDBOX/bin"
	local tool path
	for tool in "$@"; do
		path=$(command -v "$tool") || {
			echo "queue-daemon-count-test: this host has no $tool, so nothing can be measured" >&2
			exit 1
		}
		ln -sf "$path" "$SANDBOX/bin/$tool"
	done
}

# write_ps_stub <exit-code> <json> — the stub `harmonik` for the current sandbox.
write_ps_stub() {
	local rc="$1" json="$2"
	cat >"$SANDBOX/bin/harmonik" <<STUB
#!/bin/sh
# Stands in for the real binary. It prints one canned answer.
printf '%s\n' '$json'
exit $rc
STUB
	chmod +x "$SANDBOX/bin/harmonik"
}

# write_pgrep_stub <exit-code> [line-to-print] — a pgrep that exits as told and
# prints the given line, if any. A case that needs the count to run through `wc`
# gives it a pid to print.
write_pgrep_stub() {
	local rc="$1" line="${2:-}"
	rm -f "$SANDBOX/bin/pgrep"
	cat >"$SANDBOX/bin/pgrep" <<STUB
#!/bin/sh
test -z '$line' || printf '%s\n' '$line'
exit $rc
STUB
	chmod +x "$SANDBOX/bin/pgrep"
}

# ---------------------------------------------------------------------------
# run_count_args <argument>...
#
# Runs the real script with only the sandbox tools on PATH and with the exact
# arguments given. The status goes to a file and comes back from that file,
# because a status read from a later shell can be the status of something else.
#
# run_count is the ordinary two-argument form. The argument-refusal cases call
# run_count_args instead, because what they drive is the argument list itself.
# ---------------------------------------------------------------------------
run_count_args() {
	env -i PATH="$SANDBOX/bin" "$script" "$@" \
		>"$SANDBOX/out" 2>"$SANDBOX/err"
	printf '%s' "$?" >"$SANDBOX/rc"
	RC=$(cat "$SANDBOX/rc")
	OUT=$(cat "$SANDBOX/out")
	ERR=$(cat "$SANDBOX/err")
}

# run_count <project-dir>
run_count() {
	run_count_args "$SANDBOX/bin/harmonik" "$1"
}

# assert_refuses <label> <expected-message-fragment>
#
# Two halves, and both are required. The step must exit 2, and it must NOT have
# printed a count. An exit 2 with a "0" on stdout would still feed a zero to a
# caller that reads the number and ignores the status.
assert_refuses() {
	local label="$1" want="$2"
	assertions=$((assertions + 1))
	if [ "$RC" != "2" ]; then
		fail "$label: exited $RC, and only exit 2 refuses"
		printf '%s\n' "$ERR" >&2
	elif [ -n "$OUT" ]; then
		fail "$label: refused (exit 2) but still printed '$OUT' as the count"
	elif ! printf '%s' "$ERR" | grep -qF "$want"; then
		fail "$label: refused (exit 2) but said nothing about '$want'"
		printf '%s\n' "$ERR" >&2
	else
		pass "$label: refused with exit 2 and said why"
	fi
}

canonical_json() {
	printf '%s' '{"schema_version":1,"project_dir":"/srv/proj","process_signatures":[{"name":"supervisor-shim","pattern":"harmonik supervise _shim /srv/proj"},{"name":"daemon","pattern":"harmonik start daemon --project /srv/proj"},{"name":"keeper-fallback","pattern":"hk-keeper.sh /srv/proj"}]}'
}

# ---------------------------------------------------------------------------
# HAPPY — the derived host-wide pattern is the signature minus the project dir.
#
# The pattern must come out as exactly `harmonik start daemon --project`. The
# closing quote in the message is the proof that no part of the project dir
# survived the strip.
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
make_sandbox pattern bash jq pgrep wc tr
write_ps_stub 0 "$(canonical_json)"
write_pgrep_stub 1
run_count /srv/proj
if [ "$RC" != "0" ]; then
	fail "pattern derivation: a well-formed 'supervise ps' still exited $RC"
	printf '%s\n' "$ERR" >&2
elif ! printf '%s' "$ERR" | grep -qF "with the pattern 'harmonik start daemon --project'"; then
	fail "pattern derivation: the host-wide pattern is not 'harmonik start daemon --project'"
	printf '%s\n' "$ERR" >&2
else
	pass "pattern derivation: the project dir is stripped and the three words stay adjacent"
fi

# ---------------------------------------------------------------------------
# HAPPY — a daemon shape is counted and a `queue submit` shape is not.
#
# Two harmless `sleep` processes carry the two argv shapes. The signature uses a
# token unique to this run, so a real daemon on this host is out of reach of the
# pattern and this test can assert an exact count.
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
token="hkselftest$$"
make_sandbox counting bash jq pgrep wc tr
proj="$work/proj"
mkdir -p "$proj"
cat >"$work/argv-shim.sh" <<'SHIM'
#!/bin/sh
# Holds an argv shape for the counter to find. The arguments are the whole
# point. This process sleeps and does nothing else.
sleep 120
SHIM
chmod +x "$work/argv-shim.sh"

"$work/argv-shim.sh" "$token" start daemon --project "$proj" &
kids="$kids $!"
"$work/argv-shim.sh" "$token" queue submit --project "$proj" &
kids="$kids $!"

for _ in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do
	if pgrep -f "$token start daemon --project" >/dev/null 2>&1 &&
		pgrep -f "$token queue submit --project" >/dev/null 2>&1; then
		break
	fi
	sleep 0.2
done

write_ps_stub 0 "{\"schema_version\":1,\"project_dir\":\"$proj\",\"process_signatures\":[{\"name\":\"daemon\",\"pattern\":\"$token start daemon --project $proj\"}]}"
run_count "$proj"
if [ "$RC" != "0" ]; then
	fail "counting: a well-formed 'supervise ps' still exited $RC"
	printf '%s\n' "$ERR" >&2
elif [ "$OUT" != "1" ]; then
	fail "counting: expected 1 daemon-shaped process, got '$OUT'; a 'queue submit' shape is not a daemon"
	printf '%s\n' "$ERR" >&2
else
	pass "counting: the daemon shape counts once and the 'queue submit' shape counts not at all"
fi

# ---------------------------------------------------------------------------
# HAPPY — pgrep exit 1 means no daemon, and that is the one zero allowed.
#
# Without this case every assertion here could be satisfied by a script that
# refuses at all times, and such a script measures nothing.
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
make_sandbox none bash jq pgrep wc tr
write_ps_stub 0 "$(canonical_json)"
write_pgrep_stub 1
run_count /srv/proj
if [ "$RC" != "0" ] || [ "$OUT" != "0" ]; then
	fail "no daemon: pgrep exit 1 must give a count of 0 and exit 0, got count '$OUT' and exit $RC"
	printf '%s\n' "$ERR" >&2
else
	pass "no daemon: pgrep exit 1 gives a count of 0"
fi

# ---------------------------------------------------------------------------
# REFUSAL — pgrep could not look.
#
# THE REVIEWER'S CASE, and the reason this file exists. pgrep exits 2 or more
# when it could not read the process table. Piping it into `wc -l` turns that
# into a zero, and a zero clears the ceiling. The count must refuse instead.
# ---------------------------------------------------------------------------
make_sandbox pgrepfail bash jq pgrep wc tr
write_ps_stub 0 "$(canonical_json)"
write_pgrep_stub 2
run_count /srv/proj
assert_refuses "pgrep exited 2" "pgrep exited 2"

# ---------------------------------------------------------------------------
# REFUSAL — the count came out empty.
#
# `wc` and `tr` carry the count, and neither is checked by name. Drop `wc` and
# the count is an empty string. The script must refuse rather than print an
# empty line and exit 0. Today the caller would catch it, because
# --daemons-alive parses as an int. This case holds the guarantee here instead
# of leaving it to how the caller parses.
#
# pgrep must FIND something for this case to reach `wc` at all, so the stub
# prints a pid.
# ---------------------------------------------------------------------------
make_sandbox nowc bash jq pgrep tr
write_ps_stub 0 "$(canonical_json)"
write_pgrep_stub 0 4242
run_count /srv/proj
assert_refuses "the count came out empty" "came out empty"

# ---------------------------------------------------------------------------
# REFUSAL — the arguments are not there.
#
# The project argument is the live one. QDR_PROJECT can expand to nothing, and
# an empty project reaches this refusal rather than a `supervise ps` on the
# wrong directory.
# ---------------------------------------------------------------------------
make_sandbox noargs bash jq pgrep wc tr
write_ps_stub 0 "$(canonical_json)"
run_count_args
assert_refuses "no binary argument" "needs the harmonik binary as its first argument"

make_sandbox noproject bash jq pgrep wc tr
write_ps_stub 0 "$(canonical_json)"
run_count_args "$SANDBOX/bin/harmonik" ""
assert_refuses "no project argument" "needs the project directory as its second argument"

# ---------------------------------------------------------------------------
# REFUSAL — the tools are not here.
# ---------------------------------------------------------------------------
make_sandbox nojq bash pgrep wc tr
write_ps_stub 0 "$(canonical_json)"
run_count /srv/proj
assert_refuses "jq is absent" "jq is required"

make_sandbox nopgrep bash jq wc tr
write_ps_stub 0 "$(canonical_json)"
run_count /srv/proj
assert_refuses "pgrep is absent" "pgrep is required"

# ---------------------------------------------------------------------------
# REFUSAL — the signature cannot be read.
# ---------------------------------------------------------------------------
make_sandbox psfail bash jq pgrep wc tr
write_ps_stub 1 ""
run_count /srv/proj
assert_refuses "'supervise ps' failed" "'harmonik supervise ps' failed"

make_sandbox nodir bash jq pgrep wc tr
write_ps_stub 0 '{"schema_version":1,"process_signatures":[{"name":"daemon","pattern":"harmonik start daemon --project /srv/proj"}]}'
run_count /srv/proj
assert_refuses "no project_dir" "printed no project_dir"

make_sandbox nosig bash jq pgrep wc tr
write_ps_stub 0 '{"schema_version":1,"project_dir":"/srv/proj","process_signatures":[{"name":"keeper-fallback","pattern":"hk-keeper.sh /srv/proj"}]}'
run_count /srv/proj
assert_refuses "no daemon signature" "printed no daemon signature"

# ---------------------------------------------------------------------------
# REFUSAL — the signature does not end in the project dir, so the host-wide
# pattern cannot be derived by stripping the suffix. Guessing here is how a
# pattern that matches nothing gets written, and that reads as a zero.
# ---------------------------------------------------------------------------
make_sandbox badsuffix bash jq pgrep wc tr
write_ps_stub 0 '{"schema_version":1,"project_dir":"/srv/proj","process_signatures":[{"name":"daemon","pattern":"harmonik start daemon --project /somewhere/else"}]}'
run_count /srv/proj
assert_refuses "the signature does not end in the project dir" "refusing to guess a host-wide pattern"

# ---------------------------------------------------------------------------
# STRUCTURAL — the make target still routes through this script, and it still
# refuses when the script refuses.
#
# A script that refuses correctly is worth nothing if the recipe stopped calling
# it or started tolerating its status. `make -n` expands the recipe, so this
# reads the real step and not the Makefile text.
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
steps=$(make -n queue-dogfood-readiness SCRATCH=/dev/null EVIDENCE=/dev/null BEADS=x=y CONCURRENCY=1 2>/dev/null)
if [ -z "$steps" ]; then
	fail "structural: 'make -n queue-dogfood-readiness' produced nothing, so nothing was checked"
elif ! printf '%s' "$steps" | grep -q 'scripts/queue-daemon-count\.sh'; then
	fail "structural: the make target no longer calls scripts/queue-daemon-count.sh"
elif ! printf '%s' "$steps" | grep -E 'scripts/queue-daemon-count\.sh' | grep -q 'exit 2'; then
	fail "structural: the make target calls the count but does not carry its refusal"
else
	pass "structural: the make target calls the count script and exits 2 when it refuses"
fi

# The derivation must live in one place. A second copy in the recipe is how the
# tested version and the running version drift apart.
assertions=$((assertions + 1))
recipe=$(sed -n '/^queue-dogfood-readiness:/,/^$/p' "$repo_root/Makefile")
if [ -z "$recipe" ]; then
	fail "structural: the queue-dogfood-readiness recipe could not be read"
elif printf '%s' "$recipe" | grep -vE '^[[:space:]]*@?#' | grep -q 'pgrep'; then
	fail "structural: the recipe holds a pgrep of its own, so the tested count is not the only count"
else
	pass "structural: the recipe holds no second copy of the derivation"
fi

# ---------------------------------------------------------------------------
printf 'queue-daemon-count-test: %d assertions, %d failed\n' "$assertions" "$failures"
if [ "$assertions" -lt 15 ]; then
	printf 'queue-daemon-count-test: only %d assertions ran; this file expects 15\n' "$assertions" >&2
	exit 1
fi
[ "$failures" -eq 0 ]
