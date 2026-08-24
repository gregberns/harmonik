#!/usr/bin/env bash
# skill-mirror-check.sh — every shipped skill exists in up to three places. Prove they agree.
#
# The three homes, and why this check exists:
#   cmd/harmonik/assets/skills/<name>/   the //go:embed SOURCE — what ships in the binary
#   .claude/skills/<name>/               the working copy the harness reads in this repo
#   .harmonik/agents/_skills/<name>/     resolved FIRST for a bare-name manifest ref, so
#                                        for a manifest-launched agent this copy WINS
#
# There is no reverse sync. Editing one copy and not the others is silent: the agent keeps
# booting the old text and nothing reports an error. That is the failure this check ends.
#
# It also multiplies cost. Every line added to a shipped skill is added three times, so an
# instruction corpus grows three times as fast as anyone writing it believes.
#
# It sweeps EVERY file under the source tree, not only markdown. `//go:embed assets`
# takes the whole tree. Every shipped source file is markdown today; the sweep is not
# limited to markdown so that the first non-markdown asset is guarded on the day it lands.
#
# Usage: scripts/skill-mirror-check.sh   (exit 0 = all copies agree)

set -uo pipefail
cd "$(dirname "$0")/.." || { echo "cannot reach the repo root" >&2; exit 1; }

SRC="cmd/harmonik/assets/skills"
WORK=".claude/skills"
UNDER=".harmonik/agents/_skills"

fail=0
warn=0
checked=0

while IFS= read -r src; do
  rel="${src#"$SRC"/}"
  checked=$((checked + 1))
  # The two mirrors have DIFFERENT completeness contracts, so they cannot share a rule.
  #
  #   .claude/skills/       MUST be complete. `harmonik sync-assets` classifies it as
  #                         Managed and overwrites it wholesale from the embed, so a
  #                         file missing here is a file the next sync resurrects — and
  #                         until then the harness reads a skill set that is not the
  #                         shipped one. Absence is drift.
  #   _skills/              MAY be partial. It carries only the subset that manifests
  #                         reference by bare name. Absence there means "not needed".
  if [[ ! -f "$WORK/$rel" ]]; then
    echo "MISSING: $WORK/$rel does not exist, but its source $src does"
    fail=1
  elif ! cmp -s "$src" "$WORK/$rel"; then
    echo "DRIFT: $WORK/$rel differs from its source $src"
    fail=1
  fi
  if [[ -f "$UNDER/$rel" ]] && ! cmp -s "$src" "$UNDER/$rel"; then
    echo "DRIFT: $UNDER/$rel differs from its source $src"
    fail=1
  fi
done < <(find "$SRC" -type f ! -name '.gitkeep' | sort)

# A file under _skills/ with no source is worse than drift: it is a copy that WINS for a
# manifest-launched agent and that nothing generates, reviews, or ships in the binary.
while IFS= read -r orphan; do
  rel="${orphan#"$UNDER"/}"
  if [[ ! -f "$SRC/$rel" ]]; then
    # A WARNING, not a failure. There is exactly one of these today and giving it a
    # source would make it a shipped skill, which is a product call and not a gate's
    # to make. Tracked as a bead. Drift between copies that DO exist is the failure.
    echo "WARN unguarded: $orphan has no source at $SRC/$rel — it wins for manifest-launched agents and nothing verifies it"
    warn=$((warn + 1))
  fi
done < <(find "$UNDER" -type f ! -name '.gitkeep' 2>/dev/null | sort)

# A sweep that found nothing has not passed — it has lost its input. Without this the
# check goes green when the source tree is renamed or moved, which is precisely when
# you most need it to shout.
if [[ $checked -eq 0 ]]; then
  echo "NO SOURCES: found no files under $SRC — the source tree is missing or moved."
  echo "A mirror check with nothing to check is not a pass."
  exit 1
fi

if [[ $fail -eq 0 ]]; then
  echo "skill mirrors agree ($checked source files checked, $warn unguarded)"
else
  echo ""
  echo "Fix: edit $SRC (the source of truth), then copy byte-for-byte into the other homes"
  echo "in the SAME commit. There is no reverse sync and no automatic repair."
fi
exit $fail
