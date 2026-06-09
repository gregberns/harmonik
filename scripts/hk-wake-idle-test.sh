#!/usr/bin/env bash
# hk-wake-idle-test.sh — fixture tests for pane_is_idle() in hk-wake.sh.
#
# Each fixture is a captured-pane snapshot; we stub `tmux capture-pane` to emit
# it, source pane_is_idle from hk-wake.sh, and assert the IDLE/BUSY verdict.
# Run:  bash scripts/hk-wake-idle-test.sh
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="${HERE}/hk-wake.sh"

# --- extract pane_is_idle() from the script without running its main loop ----
# The function depends on $TARGET and $CAPTURE_LINES; we provide both, and a
# `tmux` shim that returns the current fixture for `capture-pane`.
TARGET="fixture"
CAPTURE_LINES="${HK_WAKE_CAPTURE_LINES:-12}"
FIXTURE=""

tmux() {
  case "$1" in
    capture-pane) printf '%s\n' "$FIXTURE" ;;
    *) return 0 ;;
  esac
}

# Pull the pane_is_idle function body out of hk-wake.sh and eval it (avoids
# running the script's trailing while-true loop).
eval "$(awk '/^pane_is_idle\(\) \{/{f=1} f{print} /^\}/{if(f){f=0}}' "$SCRIPT")"

PASS=0; FAIL=0
check() {
  local name="$1" want="$2"      # want = IDLE | BUSY
  FIXTURE="$3"
  local got
  if pane_is_idle; then got="IDLE"; else got="BUSY"; fi
  if [[ "$got" == "$want" ]]; then
    printf 'PASS  %-40s -> %s\n' "$name" "$got"; PASS=$((PASS+1))
  else
    printf 'FAIL  %-40s -> got %s, want %s\n' "$name" "$got" "$want"; FAIL=$((FAIL+1))
  fi
}

FOOTER='⏵⏵ bypass permissions on · ? for shortcuts · esc to interrupt · ctrl+t to hide'

# (a) permission dialog: selection caret "❯ 1. Yes" => BUSY
check "permission-dialog (❯ 1. Yes / 2. No)" BUSY "$(cat <<EOF
 Bash command
   rm -rf /important/path

 Do you want to proceed?
 ❯ 1. Yes
   2. No

$FOOTER
EOF
)"

# (b) pre-typed text on the input row => BUSY
check "pre-typed prompt (❯ do something)" BUSY "$(cat <<EOF
 some earlier output here

❯ do something

$FOOTER
EOF
)"

# (c) empty prompt => IDLE. The real Claude layout renders the "❯" input row
# bracketed by box-rule lines, with the footer hint bar BELOW it (so the prompt
# is NOT the last content row).
RULE='──────────────────────────────────────────────────────────────'
check "empty prompt (input box, footer below)" IDLE "$(cat <<EOF
 some earlier output here

$RULE
❯
$RULE
  $FOOTER
EOF
)"

# (d) tall pane idle: empty prompt + footer box, then many blank rows below =>
# the trailing-blank-strip must keep the prompt+footer in the scan window.
check "tall-pane idle (prompt above blanks)" IDLE "$(printf ' previous turn output\n\n%s\n❯ \n%s\n  %s\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n' "$RULE" "$RULE" "$FOOTER")"

# (e) spinner / esc-to-interrupt running line => BUSY
check "spinner running-line (esc to interrupt)" BUSY "$(cat <<EOF
 working on it

✶ Cogitating… (5s · esc to interrupt)

$FOOTER
EOF
)"

# extra: bare elapsed running-line "(1m 16s ·" => BUSY
check "elapsed running-line (1m 16s ·)" BUSY "$(cat <<EOF
 ✻ Worked for 1m 16s

 (1m 16s · ↓ 40.4k tokens · esc to interrupt)

$FOOTER
EOF
)"

# extra: numbered-menu option row without caret => BUSY
check "numbered menu (1. / 2. options)" BUSY "$(cat <<EOF
 Select an option:
   1. Yes
   2. No

$FOOTER
EOF
)"

# extra: empty prompt + ONLY the static footer's esc-to-interrupt => IDLE
check "empty prompt + static footer only" IDLE "$(cat <<EOF
 done with the previous turn

$RULE
❯
$RULE
  $FOOTER
EOF
)"

# extra: dead/empty pane => BUSY (can't confirm idle)
check "empty pane" BUSY ""

echo "----"
echo "idle-fixtures: PASS=$PASS FAIL=$FAIL"
[[ "$FAIL" -eq 0 ]]
