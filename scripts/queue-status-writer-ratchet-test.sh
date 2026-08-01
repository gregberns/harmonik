#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
ratchet="$repo_root/scripts/queue-status-writer-ratchet.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

run_ratchet() {
    QUEUE_STATUS_RATCHET_ROOT="$1" bash "$ratchet" 2>&1
}

new_fixture() {
    fixture="$(mktemp -d "$tmp/fixture.XXXXXX")"
    mkdir -p "$fixture"
    cp -R "$repo_root/internal" "$fixture/internal"
    cp -R "$repo_root/cmd" "$fixture/cmd"
    printf '%s\n' "$fixture"
}

output="$(run_ratchet "$repo_root")"
grep -q 'baseline direct assignments bravo=16 daemon=14' <<<"$output"
grep -q 'construction-path writes=18' <<<"$output"
grep -q 'queue-status-writer-ratchet: OK' <<<"$output"

fixture="$(new_fixture)"
printf 'package main\nimport "github.com/gregberns/harmonik/internal/queue"\nfunc bypass(item queue.Item) { item.Status = queue.ItemStatusPending }\n' >"$fixture/cmd/harmonik/queue_status_bypass.go"
if failed_output="$(run_ratchet "$fixture")"; then
    echo "queue-status-writer-ratchet test: expected command bypass to fail" >&2
    exit 1
fi
grep -q 'direct queue status write outside the transition owner' <<<"$failed_output"

fixture="$(new_fixture)"
printf 'func ownerBypass(item *Item) { item.Status = ItemStatusPending }\n' >>"$fixture/internal/queue/status_transition.go"
if failed_output="$(run_ratchet "$fixture")"; then
    echo "queue-status-writer-ratchet test: expected owner growth to fail" >&2
    exit 1
fi
grep -q 'transition owner grew from 16 to 17' <<<"$failed_output"

fixture="$(new_fixture)"
printf 'package daemon\nimport "github.com/gregberns/harmonik/internal/queue"\nfunc daemonBypass(item queue.Item) { item.Status = queue.ItemStatusPending }\n' >"$fixture/internal/daemon/queue_status_bypass.go"
if failed_output="$(run_ratchet "$fixture")"; then
    echo "queue-status-writer-ratchet test: expected daemon growth to fail" >&2
    exit 1
fi
grep -q 'daemon baseline grew from 14 to 15' <<<"$failed_output"

fixture="$(new_fixture)"
printf 'package queue\nfunc constructionBypass() { _ = Item{Status: ItemStatusPending} }\n' >"$fixture/internal/queue/queue_status_construction.go"
if failed_output="$(run_ratchet "$fixture")"; then
    echo "queue-status-writer-ratchet test: expected construction growth to fail" >&2
    exit 1
fi
grep -q 'construction surface grew from 18 to 19' <<<"$failed_output"

echo "queue-status-writer-ratchet test: OK"
