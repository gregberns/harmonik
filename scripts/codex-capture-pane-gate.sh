#!/usr/bin/env bash
set -euo pipefail

# codex-capture-pane-gate.sh — SC6 gate #3 (T9; harness-acceptance-design
# §"SC6 enforcement"). The structured Codex input driver owns the child stdio
# directly (AIS-009); it must NEVER observe via a tmux `capture-pane` scrape.
# `capture-pane` is an exec-arg string, not an import, so a grep ratchet over the
# driver packages is the cheapest enforcement (the forbidigo `time.*` ban and the
# depguard driver→tmux deny — the other two-thirds of the SC6 trio — live in
# .golangci.yml).
#
# Scope: the structured input-driver production packages only (their _test.go
# files are excluded — a test may legitimately mention the string, e.g. to assert
# its ABSENCE, and the pre-M2 output codextest tier references tmux fixtures).
#
# Exit 0: clean. Exit 1: a `capture-pane` string found in a scoped source file.

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

SCOPE=(internal/codexinput internal/codexdriver)

HITS=0
for dir in "${SCOPE[@]}"; do
    [ -d "$dir" ] || continue
    while IFS= read -r f; do
        case "$f" in *_test.go) continue ;; esac
        if grep -nF 'capture-pane' "$f" >/dev/null 2>&1; then
            echo "codex-capture-pane-gate: FORBIDDEN capture-pane in $f:" >&2
            grep -nF 'capture-pane' "$f" >&2
            HITS=$((HITS + 1))
        fi
    done < <(find "$dir" -name '*.go' -type f)
done

if [ "$HITS" -ne 0 ]; then
    echo "codex-capture-pane-gate: FAIL — the structured input driver must not scrape a tmux pane (AIS-009)" >&2
    exit 1
fi
echo "codex-capture-pane-gate: OK — no capture-pane in ${SCOPE[*]}"
