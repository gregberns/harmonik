#!/bin/sh
# ─────────────────────────────────────────────────────────────────────────────
# ubs.sh — repo-local wrapper that runs Ultimate Bug Scanner (ubs) under a
#          modern bash (>= 4.0).
#
# WHY THIS EXISTS
#   `ubs` is a bash script with shebang `#!/usr/bin/env bash` and a hard
#   `BASH_VERSINFO[0] >= 4` guard. On macOS, `env bash` resolves to the
#   system /bin/bash 3.2.57 (frozen at GPLv2 for licensing reasons) in
#   non-interactive contexts — scripts, git hooks (lefthook), Makefile
#   recipes, CI — because /bin precedes /opt/homebrew/bin on the default
#   non-interactive PATH. So `ubs` aborts with a version error there even
#   though Homebrew's bash 5.x is installed.
#
#   This wrapper resolves a bash >= 4 explicitly and execs the real `ubs`
#   under it, forwarding all arguments. Every caller (Makefile, lefthook,
#   CI) should invoke `scripts/ubs.sh ...` instead of `ubs ...`.
#
# NOTE: this wrapper itself is POSIX /bin/sh — it must run even when only
#       the ancient shells are on PATH.
# ─────────────────────────────────────────────────────────────────────────────
set -eu

# --- 1. Locate a bash >= 4.0 -------------------------------------------------
# Prefer the well-known Homebrew locations, then probe anything named `bash`
# on PATH and accept the first whose major version is >= 4.
bash_major() {
  # Prints the major version integer of the bash at $1, or nothing on failure.
  "$1" -c 'echo "${BASH_VERSINFO[0]}"' 2>/dev/null
}

BASH_BIN=""
for cand in \
  /opt/homebrew/bin/bash \
  /usr/local/bin/bash \
  "$(command -v bash 2>/dev/null || true)"
do
  [ -n "$cand" ] || continue
  [ -x "$cand" ] || continue
  major="$(bash_major "$cand")"
  case "$major" in
    ''|*[!0-9]*) continue ;;  # not a number
  esac
  if [ "$major" -ge 4 ]; then
    BASH_BIN="$cand"
    break
  fi
done

# Last-resort: scan PATH entries by hand in case `command -v` returned the
# 3.2 system bash but a newer one exists elsewhere on PATH.
if [ -z "$BASH_BIN" ]; then
  IFS_SAVE=$IFS
  IFS=:
  for dir in $PATH; do
    cand="$dir/bash"
    [ -x "$cand" ] || continue
    major="$(bash_major "$cand")"
    case "$major" in
      ''|*[!0-9]*) continue ;;
    esac
    if [ "$major" -ge 4 ]; then
      BASH_BIN="$cand"
      break
    fi
  done
  IFS=$IFS_SAVE
fi

if [ -z "$BASH_BIN" ]; then
  echo "ERROR: scripts/ubs.sh could not find a bash >= 4.0 on this system." >&2
  echo "       macOS ships bash 3.2 at /bin/bash; ubs needs >= 4.0." >&2
  echo "       Install one with:  brew install bash" >&2
  echo "       (expected at /opt/homebrew/bin/bash on Apple Silicon)." >&2
  exit 1
fi

# --- 2. Locate the real ubs --------------------------------------------------
# Prefer PATH; fall back to the known install location.
UBS_BIN="$(command -v ubs 2>/dev/null || true)"
if [ -z "$UBS_BIN" ]; then
  for cand in \
    "$HOME/.local/bin/ubs" \
    /Users/gb/.local/bin/ubs
  do
    if [ -x "$cand" ]; then
      UBS_BIN="$cand"
      break
    fi
  done
fi

if [ -z "$UBS_BIN" ]; then
  echo "ERROR: scripts/ubs.sh could not find the 'ubs' executable." >&2
  echo "       Looked on PATH and at ~/.local/bin/ubs." >&2
  echo "       Install it:  curl -sSL https://raw.githubusercontent.com/Dicklesworthstone/ultimate_bug_scanner/main/install.sh | bash" >&2
  exit 1
fi

# --- 3. Default the scan-size guard if the caller did not set it -------------
# ubs refuses directories larger than UBS_MAX_DIR_SIZE_MB (default 1000).
# This repo's working tree plus build/cache artifacts can exceed that; give a
# generous default so a bare `scripts/ubs.sh .` does not bail on size. Callers
# may still override (including UBS_MAX_DIR_SIZE_MB=0 to disable).
: "${UBS_MAX_DIR_SIZE_MB:=5000}"
export UBS_MAX_DIR_SIZE_MB

# --- 4. Exec the real ubs under the modern bash ------------------------------
exec "$BASH_BIN" "$UBS_BIN" "$@"
