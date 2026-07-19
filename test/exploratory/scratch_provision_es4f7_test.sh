#!/usr/bin/env bash
# scratch_provision_es4f7_test.sh — regression test for hk-es4f7.
#
# `harmonik init` now ships a DEFAULT harnesses.pi block (provider openrouter).
# scratch-daemon.sh provision_matrix_config used to SKIP appending the ornith
# matrix overlay whenever any `harnesses:` block was already present — so the
# ornith/DGX pi config never landed and every pi cell red-failed the forced
# core-loop-lt gate. The fix strips init's default harnesses:/codex: blocks and
# appends the overlay (idempotently). This test proves the override + idempotency
# + YAML validity against an init-shaped fixture.
#
# Usage: bash test/exploratory/scratch_provision_es4f7_test.sh
set -eo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
SCRATCH_DAEMON="$REPO_ROOT/scripts/scratch-daemon.sh"

TMP="$(mktemp -d)"
# The neutered script copy must live INSIDE the repo tree: provision_matrix_config
# derives repo_root from `dirname ${BASH_SOURCE[0]}` → `git rev-parse --show-toplevel`
# to locate the overlay, so a copy under $TMP (not a git repo) would fail exit-128.
NEUTERED="$REPO_ROOT/.scratch-daemon-es4f7-test.$$.sh"
trap 'rm -rf "$TMP"; rm -f "$NEUTERED"' EXIT

fail() { echo "FAIL: $1"; exit 1; }
pass() { echo "PASS: $1"; }

SCRATCH="$TMP/scratch"
mkdir -p "$SCRATCH/.harmonik"

# Fixture: a config.yaml shaped like `harmonik init` output — a base + keeper block
# + the DEFAULT openrouter harnesses.pi block (comment-heavy, as init writes it).
cat > "$SCRATCH/.harmonik/config.yaml" <<'YAML'
version: 1
daemon:
  target_branch: main
sentinel:
  enabled: true
keeper:
  warn_pct: 80
harnesses:
  pi:
    # Pi implementer harness config. All three fields are REQUIRED — no defaults.
    provider: openrouter
    model: openrouter/qwen/qwen3-coder
    api_key_env: OPENROUTER_API_KEY
    # api_key_file: ~/.config/harmonik/openrouter.key
    # base_url: http://dgx.local:8551/v1
YAML

# scratch-daemon.sh runs `main "$@"` at the bottom; neuter it so we can source the
# file and call the internal function directly. Written inside the repo (see above).
sed 's/^main "\$@"$/: # main neutered for test/' "$SCRATCH_DAEMON" > "$NEUTERED"
# shellcheck disable=SC1090
source "$NEUTERED"
# scratch-daemon.sh sets `set -euo pipefail` at its top, which the source inherits.
# Relax nounset: provision_matrix_config reads ${BASH_SOURCE[0]} to locate the repo,
# which is unset-sensitive under -u when the function is called (not sourced) here.
set +u

CFG="$SCRATCH/.harmonik/config.yaml"

echo "--- provision run 1 (override init default) ---"
provision_matrix_config "$SCRATCH" >/dev/null 2>&1

grep -q 'provider: ornith' "$CFG"        || fail "ornith harnesses.pi did not land"
pass "ornith harnesses.pi landed"
! grep -q 'provider: openrouter' "$CFG"  || fail "init default openrouter block was not stripped"
pass "init default openrouter block stripped"
grep -q 'stale_wal_max_bytes' "$CFG"     || fail "codex.stale_wal_max_bytes did not land"
pass "codex.stale_wal_max_bytes landed"
grep -q 'warn_pct: 80' "$CFG"            || fail "keeper block was lost during strip"
pass "keeper block preserved"
n="$(grep -cE '^harnesses:' "$CFG")"
[ "$n" = "1" ] || fail "expected exactly one top-level harnesses: key, got $n (duplicate-key YAML)"
pass "exactly one top-level harnesses: key (no duplicate)"

echo "--- provision run 2 (idempotency) ---"
provision_matrix_config "$SCRATCH" >/dev/null 2>&1
n2="$(grep -cE '^harnesses:' "$CFG")"
[ "$n2" = "1" ] || fail "re-run produced $n2 harnesses: keys (not idempotent)"
pass "idempotent: still exactly one harnesses: key after re-run"
grep -q 'provider: ornith' "$CFG" || fail "re-run lost the ornith config"
pass "idempotent: ornith config preserved on re-run"

# YAML must parse and resolve to the ornith provider + codex threshold.
if command -v python3 >/dev/null 2>&1; then
    python3 - "$CFG" <<'PY' || fail "config.yaml is not valid YAML after provisioning"
import sys, yaml
d = yaml.safe_load(open(sys.argv[1]))
assert d["harnesses"]["pi"]["provider"] == "ornith", d["harnesses"]["pi"]
assert d.get("codex", {}).get("stale_wal_max_bytes") == 1048576, d.get("codex")
print("PASS: valid YAML, harnesses.pi.provider=ornith, codex.stale_wal_max_bytes=1048576")
PY
else
    echo "SKIP: python3 unavailable — YAML-parse assertion skipped"
fi

echo "ALL PASS: scratch_provision_es4f7_test.sh"
