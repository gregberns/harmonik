#!/usr/bin/env bash
# import-closure.sh — the legacy wall.
#
# For each segment module, no package in its transitive import closure may reach
# github.com/gregberns/harmonik/internal, the unsegmented zone. The rule is
# one-way: internal/ may import a segment, a segment may never import internal/.
# `go list -deps ./...` reports the WHOLE closure, so this catches a breach that
# arrives through a dependency, not only a direct import. An empty result is a
# pass.
#
# Layout of record: plans/2026-09-07-harmonik-restructure/06-segmented-layout.md §2.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

modules="contract kernel tools/echo"
banned="github.com/gregberns/harmonik/internal"
status=0

for m in $modules; do
	# A go list error is a hard fail — a swallowed error would read as "no hits".
	if ! deps="$(cd "$m" && go list -deps ./... 2>&1)"; then
		echo "import-closure: FAIL — 'go list -deps' errored in $m:"
		printf '%s\n' "$deps" | sed 's/^/    /'
		status=1
		continue
	fi
	hits="$(printf '%s\n' "$deps" | grep -F "$banned" || true)"
	if [ -n "$hits" ]; then
		echo "import-closure: FAIL — segment '$m' reaches the unsegmented internal/ zone:"
		printf '%s\n' "$hits" | sed 's/^/    /'
		status=1
	else
		echo "import-closure: ok — $m reaches no internal/ package"
	fi
done

exit $status
