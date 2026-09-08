#!/usr/bin/env bash
# kernel-vocabulary.sh — the kernel names no domain noun.
#
# The substrate carries opaque byte payloads and never parses them, so no domain
# word belongs in its source. This check greps kernel Go source for any of the
# banned nouns as a WHOLE WORD, case-insensitive, and reports every offending
# line. A whole-word match is on purpose: it catches a planted "run" but not the
# "run" inside "truncate", so a legitimate substrate word never trips the gate.
#
# Scope is Go source only (*.go). go.mod carries "go 1.25" and the linter config
# carries a "run:" key; neither is kernel source, and matching them would be a
# false positive.
#
# Layout of record: plans/2026-09-07-harmonik-restructure/07-segmented-structure-and-day1-standards.md
# §3.2 (the kernel vocabulary test).
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

# The banned nouns. bead/run/session/agent are the substrate's own rule; the
# echo/ping/tmux/claude additions name the tools and harnesses whose words must
# never leak into the kernel.
nouns='bead|run|session|agent|echo|ping|tmux|claude'

if [ ! -d kernel ]; then
	echo "kernel-vocabulary: FAIL — kernel/ directory not found; the check ran against nothing"
	exit 1
fi

hits="$(grep -rniwE "$nouns" kernel --include='*.go' || true)"
if [ -n "$hits" ]; then
	echo "kernel-vocabulary: FAIL — a domain noun appears in kernel/ source:"
	printf '%s\n' "$hits" | sed 's/^/    /'
	exit 1
fi

echo "kernel-vocabulary: ok — no domain noun in kernel/ Go source"
exit 0
