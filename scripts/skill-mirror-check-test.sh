#!/usr/bin/env bash
# skill-mirror-check-test.sh — self-test for the skill-mirror gate.
#
# A gate with no test is the failure it exists to prevent: it can go green for the
# wrong reason and nothing reports it. Two of the cases below (NO SOURCES, MISSING)
# are ones the gate originally got WRONG — it passed on both — so they are regression
# tests, not hypotheticals.
#
# Every case runs against a throwaway fixture tree, never against this repo, because
# the script under test resolves its own root from its own path.

set -uo pipefail

SCRIPT="$(cd "$(dirname "$0")" && pwd)/skill-mirror-check.sh"
fails=0

# fixture <dir> — a minimal repo with one shipped skill mirrored to both homes,
# one _skills-only file (the unguarded case), and one non-markdown asset.
fixture() {
  local d="$1"
  mkdir -p "$d/scripts" "$d/cmd/harmonik/assets/skills/demo" "$d/.claude/skills/demo" "$d/.harmonik/agents/_skills/demo"
  cp "$SCRIPT" "$d/scripts/skill-mirror-check.sh"
  printf 'body\n'  > "$d/cmd/harmonik/assets/skills/demo/SKILL.md"
  printf 'body\n'  > "$d/.claude/skills/demo/SKILL.md"
  printf 'body\n'  > "$d/.harmonik/agents/_skills/demo/SKILL.md"
  printf '#!/bin/sh\n' > "$d/cmd/harmonik/assets/skills/demo/run"
  printf '#!/bin/sh\n' > "$d/.claude/skills/demo/run"
  mkdir -p "$d/.harmonik/agents/_skills/orphan"
  printf 'no source\n' > "$d/.harmonik/agents/_skills/orphan/SKILL.md"
}

# expect <name> <want_exit> <want_substring|-> <mutation...>
expect() {
  local name="$1" want_rc="$2" want_txt="$3"; shift 3
  local d; d="$(mktemp -d)"
  fixture "$d"
  ( cd "$d" && "$@" ) >/dev/null 2>&1
  local out rc
  out="$(bash "$d/scripts/skill-mirror-check.sh" 2>&1)"; rc=$?
  local ok=1
  [[ $rc -eq $want_rc ]] || ok=0
  if [[ "$want_txt" != "-" ]] && ! grep -qF "$want_txt" <<<"$out"; then ok=0; fi
  if [[ $ok -eq 1 ]]; then
    printf 'ok   %s\n' "$name"
  else
    printf 'FAIL %s — want exit %s + %s, got exit %s:\n%s\n' "$name" "$want_rc" "$want_txt" "$rc" "$out"
    fails=$((fails + 1))
  fi
  rm -rf "$d"
}

nothing() { :; }

expect "clean tree passes"                      0 "skill mirrors agree"  nothing
expect "unguarded _skills file warns, not fails" 0 "WARN unguarded"      nothing

expect "drift under .claude fails"              1 "DRIFT" \
  sh -c 'printf "changed\n" > .claude/skills/demo/SKILL.md'

expect "drift under _skills fails"              1 "DRIFT" \
  sh -c 'printf "changed\n" > .harmonik/agents/_skills/demo/SKILL.md'

# The gate originally passed here. .claude/skills is overwritten wholesale by
# sync-assets, so a file missing there is drift, not an allowed partial mirror.
expect "a DELETED .claude mirror fails"         1 "MISSING" \
  sh -c 'rm .claude/skills/demo/SKILL.md'

# _skills carries only what manifests name by bare name, so absence is legitimate there.
expect "an absent _skills mirror is allowed"    0 "skill mirrors agree" \
  sh -c 'rm .harmonik/agents/_skills/demo/SKILL.md'

# The gate originally passed here too, which is the worst kind: green because it had
# nothing to check.
expect "a missing source tree fails"            1 "NO SOURCES" \
  sh -c 'rm -rf cmd/harmonik/assets/skills'

# Skills ship executables next to their markdown, and //go:embed takes the whole tree.
expect "drift in a non-markdown asset fails"    1 "DRIFT" \
  sh -c 'printf "#!/bin/bash\n" > .claude/skills/demo/run'

if [[ $fails -eq 0 ]]; then
  echo "skill-mirror-check: all cases pass"
  exit 0
fi
echo "skill-mirror-check: $fails case(s) failed"
exit 1
