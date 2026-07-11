#!/usr/bin/env bash
# check-hooks.sh — assert installed .git/hooks/* match lefthook.yml declarations.
# Exits 0 if all hooks are present and invoke lefthook; exits 1 with a clear
# message for each missing or stale hook.
# Run via: make check-hooks (or directly after `make install-hooks`).
set -euo pipefail

HOOKS_DIR="$(git rev-parse --git-common-dir)/hooks"

# All git hook names that lefthook can manage.
GIT_HOOK_NAMES=(
  "pre-commit" "prepare-commit-msg" "commit-msg" "post-commit"
  "pre-rebase" "post-checkout" "post-merge" "pre-push"
  "pre-receive" "update" "post-receive" "post-update"
  "push-to-checkout" "pre-auto-gc" "post-rewrite"
)

# Parse lefthook.yml for top-level keys that are valid git hook names.
LEFTHOOK_YML="${1:-lefthook.yml}"
if [[ ! -f "$LEFTHOOK_YML" ]]; then
  echo "ERROR: $LEFTHOOK_YML not found" >&2
  exit 1
fi

declare -a EXPECTED=()
for name in "${GIT_HOOK_NAMES[@]}"; do
  if grep -qE "^${name}:" "$LEFTHOOK_YML"; then
    EXPECTED+=("$name")
  fi
done

if [[ ${#EXPECTED[@]} -eq 0 ]]; then
  echo "ERROR: no git hook sections found in $LEFTHOOK_YML" >&2
  exit 1
fi

exit_code=0
for hook in "${EXPECTED[@]}"; do
  hook_file="$HOOKS_DIR/$hook"
  if [[ ! -f "$hook_file" ]]; then
    echo "MISSING  $hook  ($hook_file not found; run: make install-hooks)" >&2
    exit_code=1
  elif ! grep -q "lefthook" "$hook_file"; then
    echo "STALE    $hook  ($hook_file does not invoke lefthook; run: make install-hooks)" >&2
    exit_code=1
  else
    echo "OK       $hook"
  fi
done

# A hook file existing and invoking lefthook is not enough: if the lefthook
# binary itself can't be resolved at commit time, the auto-generated shim
# just echoes "Can't find lefthook in PATH" and exits 0 — a silent fail-open
# that lets every gate (trailer validation included) pass unchecked.
# (hk-x2spu: 0 reviewer-verdict trailers across 35 overnight commits despite
# hooks being installed, because lefthook wasn't on PATH.)
if command -v lefthook >/dev/null 2>&1; then
  echo "OK       lefthook resolvable via PATH ($(command -v lefthook))"
else
  fallback="$(grep -oE '(^|[[:space:]])"?/[^[:space:]"]*/lefthook"?([[:space:]]|$)' "$HOOKS_DIR/pre-commit" 2>/dev/null | tr -d '" ' | head -1 || true)"
  if [[ -n "$fallback" && -x "$fallback" ]]; then
    echo "OK       lefthook resolvable via baked-in fallback ($fallback)"
  else
    echo "MISSING  lefthook binary not resolvable via PATH or hook fallback — hooks fail open (echo + exit 0)" >&2
    echo "Fix: brew install lefthook  (or: make tools && make install-hooks; then: lefthook install --force)" >&2
    exit_code=1
  fi
fi

if [[ $exit_code -ne 0 ]]; then
  echo ""
  echo "Git hooks are not in sync with $LEFTHOOK_YML." >&2
  echo "Fix: make install-hooks" >&2
fi
exit $exit_code
