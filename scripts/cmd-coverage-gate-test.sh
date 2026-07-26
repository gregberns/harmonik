#!/usr/bin/env bash

set -euo pipefail

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
subject=$script_dir/cmd-coverage-gate.sh
tmp=$(mktemp -d "${TMPDIR:-/tmp}/cmd-coverage-gate-test.XXXXXX")
trap 'rm -rf "$tmp"' EXIT

mkdir -p "$tmp/bin"
cat >"$tmp/bin/go" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
case $1 in
    list)
        [[ ${FAKE_LIST_FAIL:-0} != 1 ]] || exit 1
        printf '%s\n' \
            example.test/project/cmd/alpha \
            example.test/project/cmd/alpha/sub \
            example.test/project/cmd/beta
        ;;
    test)
        [[ ${FAKE_TEST_FAIL:-0} != 1 ]] || exit 1
        profile=
        for arg in "$@"; do
            case $arg in -coverprofile=*) profile=${arg#*=} ;; esac
        done
        [[ " $* " == *' -count=1 '* ]]
        [[ " $* " == *' -covermode=atomic '* ]]
        [[ ${FAKE_NO_PROFILE:-0} != 1 ]] || exit 0
        cp "$FAKE_PROFILE" "$profile"
        ;;
    *) exit 2 ;;
esac
EOF
chmod +x "$tmp/bin/go"

profile=$tmp/profile
baseline=$tmp/baseline
cat >"$profile" <<'EOF'
mode: atomic
example.test/project/cmd/alpha/main.go:1.1,2.1 10 1
example.test/project/cmd/alpha/main.go:3.1,4.1 10 0
example.test/project/cmd/alpha/sub/main.go:1.1,2.1 10 1
example.test/project/cmd/beta/main.go:1.1,2.1 4 1
EOF

run_gate() {
    GO_BIN="$tmp/bin/go" FAKE_PROFILE="$profile" \
        CMD_COVERAGE_BASELINE="${CMD_COVERAGE_BASELINE:-$baseline}" \
        FAKE_LIST_FAIL="${FAKE_LIST_FAIL:-0}" FAKE_TEST_FAIL="${FAKE_TEST_FAIL:-0}" \
        FAKE_NO_PROFILE="${FAKE_NO_PROFILE:-0}" \
        "$subject" "$@"
}

if output=$(FAKE_LIST_FAIL=1 run_gate 2>&1); then
    echo "expected go list failure to fail closed" >&2
    exit 1
fi
grep -q 'go list ./cmd/... failed' <<<"$output"

if output=$(FAKE_TEST_FAIL=1 run_gate 2>&1); then
    echo "expected go test failure to fail closed" >&2
    exit 1
fi
grep -q 'tests failed; coverage was not evaluated' <<<"$output"

if output=$(FAKE_NO_PROFILE=1 run_gate 2>&1); then
    echo "expected a missing profile to fail" >&2
    exit 1
fi
grep -q 'did not produce an atomic coverage profile' <<<"$output"

printf 'mode: count\n' >"$profile"
if output=$(run_gate 2>&1); then
    echo "expected a non-atomic profile to fail" >&2
    exit 1
fi
grep -q 'did not produce an atomic coverage profile' <<<"$output"

printf 'mode: atomic\n' >"$profile"
if output=$(run_gate 2>&1); then
    echo "expected packages without profile statements to fail" >&2
    exit 1
fi
grep -q 'profile has no statements for' <<<"$output"

cat >"$profile" <<'EOF'
mode: atomic
example.test/project/cmd/alpha/main.go:1.1,2.1 10 1
example.test/project/cmd/alpha/main.go:3.1,4.1 10 0
example.test/project/cmd/alpha/sub/main.go:1.1,2.1 10 1
example.test/project/cmd/beta/main.go:1.1,2.1 4 1
EOF

run_gate --write-baseline >/dev/null
grep -q '^example.test/project/cmd/alpha 50.0$' "$baseline"
grep -q '^example.test/project/cmd/alpha/sub 100.0$' "$baseline"
grep -q '^example.test/project/cmd/beta 100.0$' "$baseline"
run_gate >/dev/null

sed -i.bak 's/cmd\/alpha 50.0/cmd\/alpha 50.2/' "$baseline"
rm -f "$baseline.bak"
run_gate >/dev/null

sed -i.bak 's/cmd\/alpha 50.2/cmd\/alpha 50.3/' "$baseline"
rm -f "$baseline.bak"
if output=$(run_gate 2>&1); then
    echo "expected an exactly 0.3pp regression to fail" >&2
    exit 1
fi
grep -q 'regression=0.3pp' <<<"$output"

sed -i.bak 's/cmd\/alpha 50.3/cmd\/alpha 49.7/' "$baseline"
rm -f "$baseline.bak"
output=$(run_gate)
grep -q 'NOTICE .* improved 0.3pp' <<<"$output"

grep -v '/cmd/beta ' "$baseline" >"$tmp/missing"
mv "$tmp/missing" "$baseline"
if output=$(run_gate 2>&1); then
    echo "expected a missing package baseline to fail" >&2
    exit 1
fi
grep -q 'missing: example.test/project/cmd/beta' <<<"$output"

echo 'example.test/project/cmd/stale 1.0' >>"$baseline"
if output=$(run_gate 2>&1); then
    echo "expected missing and stale package baselines to fail" >&2
    exit 1
fi
grep -q 'stale:   example.test/project/cmd/stale' <<<"$output"

run_gate --write-baseline >/dev/null
echo 'example.test/project/cmd/beta 100.0' >>"$baseline"
if output=$(run_gate 2>&1); then
    echo "expected duplicate baseline entries to fail" >&2
    exit 1
fi
grep -q 'malformed or duplicate' <<<"$output"

run_gate --write-baseline >/dev/null
sed -i.bak 's/cmd\/beta 100.0/cmd\/beta 100.1/' "$baseline"
rm -f "$baseline.bak"
if output=$(run_gate 2>&1); then
    echo "expected an out-of-range baseline percentage to fail" >&2
    exit 1
fi
grep -q 'malformed or duplicate' <<<"$output"

run_gate --write-baseline >/dev/null
sed -i.bak 's/cmd\/beta 100.0/cmd\/beta nope/' "$baseline"
rm -f "$baseline.bak"
if output=$(run_gate 2>&1); then
    echo "expected a non-numeric baseline percentage to fail" >&2
    exit 1
fi
grep -q 'malformed or duplicate' <<<"$output"

# A failed measurement must leave an existing baseline byte-for-byte intact.
printf 'sentinel baseline\n' >"$baseline"
if output=$(FAKE_TEST_FAIL=1 run_gate --write-baseline 2>&1); then
    echo "expected baseline generation with failed tests to fail" >&2
    exit 1
fi
grep -q '^sentinel baseline$' "$baseline"

# Baseline replacement uses a same-directory temporary and leaves no debris.
run_gate --write-baseline >/dev/null
if find "$(dirname "$baseline")" -name ".$(basename "$baseline").tmp.*" | grep -q .; then
    echo "atomic baseline write left a temporary file behind" >&2
    exit 1
fi

missing_dir_baseline=$tmp/does-not-exist/baseline
if output=$(CMD_COVERAGE_BASELINE="$missing_dir_baseline" run_gate --write-baseline 2>&1); then
    echo "expected a missing baseline directory to fail" >&2
    exit 1
fi
grep -q 'baseline directory does not exist' <<<"$output"

echo 'cmd-coverage-gate-test: PASS'
