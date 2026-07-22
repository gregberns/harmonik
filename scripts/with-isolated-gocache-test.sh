#!/usr/bin/env bash

set -euo pipefail

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
subject="$script_dir/with-isolated-gocache.sh"
test_root=$(mktemp -d)
trap 'rm -rf "$test_root"' EXIT

tmp_parent="$test_root/temporary files"
mkdir -p "$tmp_parent"

cache_record="$test_root/cache path"
argument_record="$test_root/argument"
expected_argument='value with spaces'

# shellcheck disable=SC2016 # Expanded by the child bash, not this shell.
TMPDIR="$tmp_parent" GOCACHE="$test_root/shared-cache" "$subject" \
    bash -c 'printf "%s" "$GOCACHE" > "$1"; printf "%s" "$2" > "$3"' \
    bash "$cache_record" "$expected_argument" "$argument_record"

cache_path=$(cat "$cache_record")
[[ "$cache_path" == "$tmp_parent"/harmonik-gocache.* ]]
[[ "$cache_path" != "$test_root/shared-cache" ]]
[[ $(cat "$argument_record") == "$expected_argument" ]]
if [[ -e "$cache_path" ]]; then
    echo "isolated cache was not removed after success: $cache_path" >&2
    exit 1
fi

failure_record="$test_root/failure cache path"
set +e
# shellcheck disable=SC2016 # Expanded by the child bash, not this shell.
TMPDIR="$tmp_parent" "$subject" bash -c \
    'printf "%s" "$GOCACHE" > "$1"; mkdir -p "$GOCACHE/nested"; exit 37' \
    bash "$failure_record"
status=$?
set -e

if [[ $status -ne 37 ]]; then
    echo "expected wrapped exit status 37, got $status" >&2
    exit 1
fi
failure_cache_path=$(cat "$failure_record")
if [[ -e "$failure_cache_path" ]]; then
    echo "isolated cache was not removed after failure: $failure_cache_path" >&2
    exit 1
fi

run_signal_test() {
    local signal=$1
    local expected_status=$2
    local cache_record="$test_root/$signal cache path"
    local observation="$test_root/$signal observation"
    local ready="$test_root/$signal ready"
    local status
    local cache_path

    set +e
    (
        wrapper_pid=$BASHPID
        (
            for _ in {1..100}; do
                [[ -f "$ready" ]] && break
                sleep 0.02
            done
            if [[ ! -f "$ready" ]]; then
                echo "$signal test child did not become ready" >&2
                kill -TERM "$wrapper_pid" 2>/dev/null || true
                exit 1
            fi
            kill -s "$signal" "$wrapper_pid"
        ) &

        # Keep the wrapper in the foreground of this subshell. Starting it as
        # an asynchronous shell command would itself give it an ignored INT
        # disposition before its script has a chance to install traps.
        # shellcheck disable=SC2016 # Expanded by child bash and signal traps.
        exec env TMPDIR="$tmp_parent" "$subject" bash -c '
            printf "%s" "$GOCACHE" > "$1"
            trap '\''if [[ -d "$GOCACHE" ]]; then printf TERM:cache-present > "$2"; else printf TERM:cache-missing > "$2"; fi; exit 0'\'' TERM
            trap '\''if [[ -d "$GOCACHE" ]]; then printf INT:cache-present > "$2"; else printf INT:cache-missing > "$2"; fi; exit 0'\'' INT
            : > "$3"
            while :; do sleep 1; done
        ' bash "$cache_record" "$observation" "$ready"
    )
    status=$?
    set -e
    if [[ $status -ne $expected_status ]]; then
        echo "expected $signal status $expected_status, got $status" >&2
        return 1
    fi
    if [[ $(cat "$observation") != "$signal:cache-present" ]]; then
        echo "child did not handle $signal while its cache was present" >&2
        return 1
    fi
    cache_path=$(cat "$cache_record")
    if [[ -e "$cache_path" ]]; then
        echo "isolated cache was not removed after $signal: $cache_path" >&2
        return 1
    fi
}

run_signal_test TERM 143
run_signal_test INT 130

set +e
usage_output=$($subject 2>&1)
status=$?
set -e
if [[ $status -ne 2 ]] || [[ $usage_output != usage:* ]]; then
    echo "missing-command usage contract failed" >&2
    exit 1
fi

echo "with-isolated-gocache-test: PASS"
