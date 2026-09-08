#!/usr/bin/env bash
# tool-isolation.sh — a tool is a plugin, not a library other tools link.
#
# No tools/* module may reach another tools/* module or the kernel module. A
# tool links the contract module (and third-party code) only; it reaches the
# substrate and every other tool through the socket, not by import.
#
# The rule has TWO ways to be broken, and this check must close both:
#   1. a `require` edge in go.mod (a declared dependency), and
#   2. an `import` in Go source with NO require line — which, under go.work,
#      compiles anyway because the workspace stitches the modules together.
# A go.mod grep alone is fail-OPEN against (2): the stray import is exactly how
# such a breach arrives, and it leaves go.mod bare. So the load-bearing check is
# the transitive import closure (`go list -deps ./...`), the same instrument
# import-closure.sh uses; the go.mod grep is kept as a cheap second signal.
#
# Layout of record: plans/2026-09-07-harmonik-restructure/06-segmented-layout.md §2,
# check 3 (the tool-isolation check).
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

status=0
found_any=0

for gomod in tools/*/go.mod; do
	[ -e "$gomod" ] || continue
	found_any=1
	tool_dir="$(dirname "$gomod")"
	self="$(awk '/^module /{print $2; exit}' "$gomod")"
	# The forbidden targets for THIS tool: the kernel module, and any tools/*
	# module other than itself.
	banned='github.com/gregberns/harmonik/(kernel|tools/[A-Za-z0-9_-]+)'

	# (1) The transitive import closure — the load-bearing check. A go list error
	# is a hard fail; a swallowed error would read as "no hits".
	if ! deps="$(cd "$tool_dir" && go list -deps ./... 2>&1)"; then
		echo "tool-isolation: FAIL — 'go list -deps' errored in $tool_dir:"
		printf '%s\n' "$deps" | sed 's/^/    /'
		status=1
		continue
	fi
	# A tool's own subpackages (e.g. tools/echo/cmd/echo) are self, not a
	# sibling: exempt anything under self's own path, not just an exact match.
	import_hits="$(printf '%s\n' "$deps" | grep -E "$banned" | grep -vE "^${self}(/|$)" || true)"

	# (2) The go.mod require edges — a cheap second signal.
	mod_hits="$(grep -oE "$banned" "$gomod" | grep -vE "^${self}(/|$)" || true)"

	hits="$(printf '%s\n%s\n' "$import_hits" "$mod_hits" | grep -v '^$' | sort -u || true)"
	if [ -n "$hits" ]; then
		echo "tool-isolation: FAIL — $tool_dir reaches a sibling tool or the kernel:"
		printf '%s\n' "$hits" | sed 's/^/    /'
		status=1
	else
		echo "tool-isolation: ok — $tool_dir (module $self)"
	fi
done

if [ "$found_any" = 0 ]; then
	echo "tool-isolation: FAIL — no tools/*/go.mod found; the check ran against nothing"
	exit 1
fi

exit $status
