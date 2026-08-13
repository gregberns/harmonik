#!/usr/bin/env bash
#
# pipefail-grepq-gate-test.sh — assertions for scripts/pipefail-grepq-gate.sh.
#
# The gate this tests is a NEGATIVE assertion: it exists to go red. That is the
# class of gate this repo keeps finding broken, because a gate that never fires
# looks exactly like a clean tree. So the first case here is a real negative
# control — it builds a file that holds the banned shape and watches the gate go
# red on it. Nothing else in this file means anything if Case 1 does not fail.
#
# Every fixture is assembled from pieces (see mk_bad below) so that THIS file
# never contains the banned text itself. If it did, the gate would flag its own
# test, which is the same class of mistake as a gate that flags its own source.
set -uo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
gate="$repo_root/scripts/pipefail-grepq-gate.sh"

assertions=0
failures=0

pass() {
	assertions=$((assertions + 1))
	echo "pipefail-grepq-gate-test: ok: $1"
}

fail() {
	assertions=$((assertions + 1))
	failures=$((failures + 1))
	echo "pipefail-grepq-gate-test: FAIL: $1"
}

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# The pipe character and the consumer names live in variables, so the banned
# shape never appears literally in this file's source.
P='|'
G='grep'
H='head'

mk_dir() { d="$work/$1"; rm -rf "$d"; mkdir -p "$d"; echo "$d"; }

# A script that turns pipefail on and sends a large producer into a quiet grep.
mk_bad() {
	{
		echo '#!/usr/bin/env bash'
		echo 'set -uo pipefail'
		# The pattern matches the FIRST line, so grep leaves while seq still has
		# 199,999 lines to write. Match the LAST line instead and grep reads the whole
		# stream, the producer finishes, and the hazard does not reproduce — which is
		# exactly why the bug survives small inputs.
		#
		# It prints FOUND or MISSED rather than just exiting, because `if` swallows a
		# pipeline's status. Reading the script's own exit code here would show 0 for
		# both spellings and prove nothing.
		printf 'if seq 1 200000 %s %s -q "^1$"; then echo FOUND; else echo MISSED; fi\n' "$P" "$G"
	} >"$1"
}

# The same assertion written safely: capture, then match the capture.
mk_good() {
	{
		echo '#!/usr/bin/env bash'
		echo 'set -uo pipefail'
		echo 'out="$(seq 1 200000)"'
		printf 'if %s -q "^1$" <<<"$out"; then echo FOUND; else echo MISSED; fi\n' "$G"
	} >"$1"
}

run_gate() {  # run_gate <root> [allow-file] -> sets GATE_RC and GATE_OUT
	GATE_OUT="$work/gate.out"
	if [ $# -ge 2 ]; then
		"$gate" --root "$1" --allow "$2" >"$GATE_OUT" 2>&1
	else
		"$gate" --root "$1" >"$GATE_OUT" 2>&1
	fi
	GATE_RC=$?
}

# ---------------------------------------------------------------------------
# Case 0 — the defect is real. Run the banned shape and the safe shape on the
# same large payload and record both exit codes. If this stops reproducing, the
# gate is guarding a hazard that no longer exists and should be re-argued rather
# than kept on faith.
# ---------------------------------------------------------------------------
probe="$work/probe.sh"
mk_bad "$probe"
chmod +x "$probe"
bad_says="$(bash "$probe" 2>/dev/null)"
mk_good "$probe"
good_says="$(bash "$probe" 2>/dev/null)"
if [ "$bad_says" = "MISSED" ] && [ "$good_says" = "FOUND" ]; then
	pass "the hazard reproduces: same payload, same pattern — banned shape says MISSED, here-string says FOUND"
else
	fail "the hazard did not reproduce: banned shape said '$bad_says', here-string said '$good_says' (expected MISSED then FOUND)"
fi

# ---------------------------------------------------------------------------
# Case 1 — NEGATIVE CONTROL. A file with pipefail on and the banned shape in it
# must turn the gate red and must name the file.
# ---------------------------------------------------------------------------
d="$(mk_dir case1)"
mk_bad "$d/offender.sh"
run_gate "$d"
if [ "$GATE_RC" -eq 1 ] && grep -q 'offender.sh' "$GATE_OUT"; then
	pass "negative control: the gate goes red on the banned shape and names offender.sh"
else
	fail "negative control: expected exit 1 naming offender.sh, got exit $GATE_RC"
	sed 's/^/    /' "$GATE_OUT"
fi

# ---------------------------------------------------------------------------
# Case 2 — the fixed spelling passes. Otherwise the gate would have no cure to
# point at and every fix would still be red.
# ---------------------------------------------------------------------------
d="$(mk_dir case2)"
mk_good "$d/clean.sh"
run_gate "$d"
if [ "$GATE_RC" -eq 0 ]; then
	pass "the here-string spelling passes"
else
	fail "the here-string spelling was rejected (exit $GATE_RC)"
	sed 's/^/    /' "$GATE_OUT"
fi

# ---------------------------------------------------------------------------
# Case 3 — scope. Without pipefail the pipeline reports the CONSUMER's status,
# so the shape is harmless and the gate must stay quiet. A gate that fires on
# harmless code gets switched off, and then it guards nothing.
# ---------------------------------------------------------------------------
d="$(mk_dir case3)"
{
	echo '#!/usr/bin/env bash'
	echo 'set -u'
	printf 'if seq 1 200000 %s %s -q "^1$"; then echo FOUND; else echo MISSED; fi\n' "$P" "$G"
} >"$d/nopipefail.sh"
run_gate "$d"
if [ "$GATE_RC" -eq 0 ]; then
	pass "scope: a script without pipefail is not flagged"
else
	fail "scope: a script without pipefail was flagged (exit $GATE_RC)"
	sed 's/^/    /' "$GATE_OUT"
fi

# ---------------------------------------------------------------------------
# Case 4 — a slow streaming producer sent into head. This is the shape that had
# both boot digests reporting an abort trap instead of the backlog.
# ---------------------------------------------------------------------------
d="$(mk_dir case4)"
{
	echo '#!/usr/bin/env bash'
	echo 'set -uo pipefail'
	printf 'br ready --limit 0 %s %s -40\n' "$P" "$H"
} >"$d/digest.sh"
run_gate "$d"
if [ "$GATE_RC" -eq 1 ] && grep -q 'digest.sh' "$GATE_OUT"; then
	pass "a streaming producer sent into head is flagged"
else
	fail "a streaming producer sent into head was not flagged (exit $GATE_RC)"
	sed 's/^/    /' "$GATE_OUT"
fi

# ---------------------------------------------------------------------------
# Case 5 — a commented-out line does not execute, so it cannot bite. Several
# scripts in this repo describe the hazard in a comment; flagging those would
# punish the only files that document it.
# ---------------------------------------------------------------------------
d="$(mk_dir case5)"
{
	echo '#!/usr/bin/env bash'
	echo 'set -uo pipefail'
	printf '# never write: seq 1 200000 %s %s -q "^1$"\n' "$P" "$G"
	echo 'echo ok'
} >"$d/documented.sh"
run_gate "$d"
if [ "$GATE_RC" -eq 0 ]; then
	pass "a commented-out occurrence is not flagged"
else
	fail "a commented-out occurrence was flagged (exit $GATE_RC)"
	sed 's/^/    /' "$GATE_OUT"
fi

# ---------------------------------------------------------------------------
# Case 6 — the allow list actually suppresses a site.
# ---------------------------------------------------------------------------
d="$(mk_dir case6)"
mk_bad "$d/offender.sh"
allow="$work/case6.allow"
printf '# judged harmless for the test\noffender.sh::seq 1 200000\n' >"$allow"
run_gate "$d" "$allow"
if [ "$GATE_RC" -eq 0 ]; then
	pass "an allow entry suppresses the site it names"
else
	fail "an allow entry did not suppress its site (exit $GATE_RC)"
	sed 's/^/    /' "$GATE_OUT"
fi

# ---------------------------------------------------------------------------
# Case 7 — the allow list is not a blanket. An entry for another path, or an
# anchor that does not appear on the line, must leave the site flagged.
# ---------------------------------------------------------------------------
d="$(mk_dir case7)"
mk_bad "$d/offender.sh"
allow="$work/case7.allow"
printf 'someone-else.sh::seq 1 200000\noffender.sh::a phrase that is not on the line\n' >"$allow"
run_gate "$d" "$allow"
if [ "$GATE_RC" -eq 1 ]; then
	pass "an allow entry that does not match leaves the site flagged"
else
	fail "a non-matching allow entry suppressed the site anyway (exit $GATE_RC)"
	sed 's/^/    /' "$GATE_OUT"
fi

# ---------------------------------------------------------------------------
# Case 8 — fail closed. A scan that finds no shell at all has proved nothing,
# and reporting that as a pass is the false-green this repo keeps hitting.
# ---------------------------------------------------------------------------
d="$(mk_dir case8)"
echo 'not a shell script' >"$d/readme.txt"
run_gate "$d"
if [ "$GATE_RC" -ne 0 ]; then
	pass "fail closed: a scan that finds no shell files does not report success"
else
	fail "a scan that found no shell files reported success"
fi

# ---------------------------------------------------------------------------
# Case 9 — a missing allow list is an error, not an empty allow list. Silently
# treating an unreadable list as empty would turn a typo into a gate that no
# longer exempts anything, and the noise gets the gate switched off.
# ---------------------------------------------------------------------------
d="$(mk_dir case9)"
mk_good "$d/clean.sh"
run_gate "$d" "$work/there-is-no-such-file.allow"
if [ "$GATE_RC" -ne 0 ]; then
	pass "a missing allow list is an error"
else
	fail "a missing allow list was treated as empty"
fi

# ---------------------------------------------------------------------------
# Case 10 — the gate does not flag its own source, and neither does this test.
# Both turn pipefail on and both talk about the banned shape at length, so this
# is the obvious way for the gate to land broken. Scanning copies of the two
# files proves it directly, without depending on the state of the rest of the
# repo.
# ---------------------------------------------------------------------------
d="$(mk_dir case10)"
cp "$gate" "$d/copy-of-gate.sh"
cp "${BASH_SOURCE[0]}" "$d/copy-of-test.sh"
run_gate "$d"
if [ "$GATE_RC" -eq 0 ]; then
	pass "the gate flags neither its own source nor this test's source"
else
	fail "the gate flags its own source or this test's source (exit $GATE_RC)"
	sed 's/^/    /' "$GATE_OUT"
fi

# ---------------------------------------------------------------------------
# Case 11 — the real repo is clean, with its real allow list. This is the
# assertion that makes the gate mean something day to day.
# ---------------------------------------------------------------------------
GATE_OUT="$work/repo.out"
"$gate" >"$GATE_OUT" 2>&1
repo_rc=$?
if [ "$repo_rc" -eq 0 ]; then
	pass "this checkout is clean: $(tail -1 "$GATE_OUT")"
else
	fail "this checkout has unguarded sites, or a stale allow entry"
	sed 's/^/    /' "$GATE_OUT"
fi

echo "pipefail-grepq-gate-test: $assertions assertions, $failures failures"
[ "$failures" -eq 0 ] || exit 1
