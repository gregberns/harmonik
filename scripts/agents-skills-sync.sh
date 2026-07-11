#!/usr/bin/env bash
# scripts/agents-skills-sync.sh — one skill tree, mechanically enforced (hk-j5yer.14)
#
# .claude/skills/ is CANONICAL. .harmonik/agents/_skills/ is a GENERATED MIRROR of the
# bare-ref skill set derived from .harmonik/agents/*/manifest.yaml at runtime — never
# hand-edited. _skills/boot/ is the one native exception (authored in place, no
# .claude/skills/boot source exists). Spec: drafts/agents/_skills/SYNC.md (this repo's
# plans/2026-07-11-captain-startup-revamp tree) §§2-3.
#
# Usage:
#   scripts/agents-skills-sync.sh            check: completeness + pair-diff + native check
#   scripts/agents-skills-sync.sh --apply    copy .claude/skills -> _skills for the mirror set
#   scripts/agents-skills-sync.sh --rot      state-rot checks only (R1-R7)
#   scripts/agents-skills-sync.sh --all      check + rot
#
# Exit codes: 0 clean, 1 drift/rot found, 2 usage error.

set -uo pipefail

ROOT="$(git rev-parse --show-toplevel)" || exit 2
cd "$ROOT" || exit 2

SRC_DIR=".claude/skills"
MIRROR_DIR=".harmonik/agents/_skills"
NATIVE_REF="boot"
MANIFESTS=(.harmonik/agents/*/manifest.yaml)

fail_count=0

fail() {
    printf 'FAIL: %s\n' "$1" >&2
    fail_count=$((fail_count + 1))
}

usage() {
    sed -n 's/^# \{0,1\}//p' "$0" | sed -n '/^Usage:/,/^Exit codes/p'
}

# ── mirror-set derivation (principle 3: derive from reality, not a snapshot) ────────────
# Bare ref = manifest `ref:` value with no "/" and `as: skill` on the same entry.
mirror_set() {
    grep -h -oE 'ref:[[:space:]]*[A-Za-z0-9_.-]+[[:space:]]*,[[:space:]]*as:[[:space:]]*skill' "${MANIFESTS[@]}" 2>/dev/null \
        | sed -E 's/^ref:[[:space:]]*([A-Za-z0-9_.-]+).*/\1/' \
        | sort -u
}

# ── check: completeness, pair drift, native check ────────────────────────────────────────
check_completeness() {
    local refs orphan_dirs ref
    refs="$(mirror_set)"

    if [[ -z "$refs" ]]; then
        fail "no manifest bare skill refs found via ${MANIFESTS[*]} — mirror set is empty (check manifest parsing)"
        return
    fi

    while IFS= read -r ref; do
        [[ -z "$ref" ]] && continue
        if [[ ! -d "$MIRROR_DIR/$ref" ]]; then
            fail "manifest ref '$ref' has no corresponding $MIRROR_DIR/$ref/ entry — add it or fix the manifest"
        fi
    done <<<"$refs"

    while IFS= read -r orphan_dirs; do
        [[ -z "$orphan_dirs" ]] && continue
        ref="$(basename "$orphan_dirs")"
        if ! grep -qxF "$ref" <<<"$refs"; then
            fail "$MIRROR_DIR/$ref/ has no manifest reference (dead weight or typo'd name) — remove it or add the manifest ref"
        fi
    done < <(find "$MIRROR_DIR" -mindepth 1 -maxdepth 1 -type d)
}

check_pairs() {
    local ref src mirror
    while IFS= read -r ref; do
        [[ -z "$ref" || "$ref" == "$NATIVE_REF" ]] && continue
        src="$SRC_DIR/$ref/SKILL.md"
        mirror="$MIRROR_DIR/$ref/SKILL.md"
        if [[ ! -f "$src" ]]; then
            fail "$src does not exist — canonical source missing for mirrored ref '$ref'"
            continue
        fi
        if [[ ! -f "$mirror" ]]; then
            fail "$mirror does not exist — run --apply to create it"
            continue
        fi
        if ! diff -q "$src" "$mirror" >/dev/null 2>&1; then
            fail "$mirror drifted from $src — run scripts/agents-skills-sync.sh --apply"
        fi
    done < <(mirror_set)
}

check_native() {
    local boot="$MIRROR_DIR/$NATIVE_REF/SKILL.md"
    if [[ ! -f "$boot" ]]; then
        fail "$boot does not exist — _skills/boot/ is native (authored in place), not mirrored, but must exist"
        return
    fi
    local lines
    lines=$(wc -l <"$boot" | tr -d ' ')
    if (( lines < 10 )); then
        fail "$boot is a stub (${lines} lines, need >=10) — _skills/boot/ is exempt from mirroring but not from content"
    fi
}

run_check() {
    check_completeness
    check_pairs
    check_native
}

# ── --apply: pure-git blob compare hand-edit guard, then copy ───────────────────────────
mirror_matches_some_source_blob() {
    # $1 = mirror file, $2 = source path (repo-relative)
    local mirror="$1" src="$2" mirror_hash src_hash commit
    mirror_hash="$(git hash-object "$mirror")" || return 1

    src_hash="$(git hash-object "$src" 2>/dev/null)"
    if [[ -n "$src_hash" && "$src_hash" == "$mirror_hash" ]]; then
        return 0
    fi

    while IFS= read -r commit; do
        [[ -z "$commit" ]] && continue
        src_hash="$(git rev-parse "${commit}:${src}" 2>/dev/null)"
        if [[ -n "$src_hash" && "$src_hash" == "$mirror_hash" ]]; then
            return 0
        fi
    done < <(git log --format=%H -- "$src" 2>/dev/null)

    return 1
}

run_apply() {
    local ref src mirror mirror_dir
    while IFS= read -r ref; do
        [[ -z "$ref" || "$ref" == "$NATIVE_REF" ]] && continue
        src="$SRC_DIR/$ref/SKILL.md"
        mirror="$MIRROR_DIR/$ref/SKILL.md"
        mirror_dir="$MIRROR_DIR/$ref"

        if [[ ! -f "$src" ]]; then
            fail "$src does not exist — cannot sync ref '$ref'"
            continue
        fi

        if [[ -f "$mirror" ]] && diff -q "$src" "$mirror" >/dev/null 2>&1; then
            continue
        fi

        if [[ -f "$mirror" ]] && ! mirror_matches_some_source_blob "$mirror" "$src"; then
            fail "hand-edit detected in mirror ($mirror); reconcile into .claude/skills first"
            continue
        fi

        mkdir -p "$mirror_dir"
        cp "$src" "$mirror"
        printf 'synced %s\n' "$mirror"
    done < <(mirror_set)
}

# ── --rot: state-rot checks R1-R7 (admiral audit owns it) ────────────────────────────────
to_epoch() {
    # $1 = RFC3339 timestamp; tries GNU date, falls back to BSD date.
    date -u -d "$1" +%s 2>/dev/null && return 0
    date -j -u -f "%Y-%m-%dT%H:%M:%SZ" "$1" +%s 2>/dev/null
}

rot_r1() {
    local file cap lines
    file=".harmonik/context/captain-lanes.md"
    [[ -f "$file" ]] || return
    cap="$(grep -m1 -oE 'max-lines:[[:space:]]*[0-9]+' "$file" | grep -oE '[0-9]+')"
    cap="${cap:-60}"
    lines=$(wc -l <"$file" | tr -d ' ')
    if (( lines > cap )); then
        fail "R1: $file:1: ${lines} lines exceeds declared cap ${cap}"
    fi
}

rot_r2() {
    local file lines entries
    file=".harmonik/context/direction-log.md"
    [[ -f "$file" ]] || return
    lines=$(wc -l <"$file" | tr -d ' ')
    entries=$(grep -c '^## ' "$file")
    if (( lines > 60 )); then
        fail "R2: $file:1: ${lines} lines exceeds cap 60"
    fi
    if (( entries > 10 )); then
        fail "R2: $file:1: ${entries} entries exceeds cap 10"
    fi
}

rot_r3() {
    local now_epoch line file lineno content content_rest val exp_epoch
    now_epoch=$(date -u +%s)
    local targets=(.harmonik/context/*.md)
    [[ -f lanes.json ]] && targets+=(lanes.json)

    while IFS= read -r line; do
        [[ -z "$line" ]] && continue
        file="${line%%:*}"
        content_rest="${line#*:}"
        lineno="${content_rest%%:*}"
        content="${content_rest#*:}"

        val="$(grep -oE 'expires:[[:space:]]*[0-9]{4}-[0-9]{2}-[0-9]{2}([T][0-9:]+Z)?' <<<"$content" | head -1 | sed -E 's/^expires:[[:space:]]*//')"
        [[ -z "$val" ]] && continue

        if [[ "$val" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}$ ]]; then
            fail "R3: $file:$lineno: expires: $val is date-only (must be RFC3339 with time)"
            continue
        fi

        exp_epoch="$(to_epoch "$val")"
        if [[ -z "$exp_epoch" ]]; then
            fail "R3: $file:$lineno: expires: $val is unparseable"
            continue
        fi
        if (( exp_epoch < now_epoch )); then
            fail "R3: $file:$lineno: expires: $val is in the past"
        fi
    done < <(grep -rnE 'expires:' "${targets[@]}" 2>/dev/null)
}

rot_r4() {
    local file count
    file=".harmonik/context/captain-lanes.md"
    [[ -f "$file" ]] || return
    count=$(grep -c 'CURRENT TRUTH' "$file")
    if (( count > 1 )); then
        fail "R4: $file: ${count} 'CURRENT TRUTH' headings found (must be 1)"
        grep -n 'CURRENT TRUTH' "$file" | while IFS= read -r hit; do
            printf '  %s\n' "$hit" >&2
        done
    fi
}

rot_r5() {
    local file
    for file in .harmonik/crew/missions/*.md; do
        [[ -f "$file" ]] || continue
        case "$(basename "$file")" in
            _TEMPLATE-*) continue ;;
        esac
        if grep -q 'SUPERSEDED' "$file"; then
            while IFS= read -r hit; do
                fail "R5: $hit — SUPERSEDED in a mission file; missions are overwrite-only, git is the archive"
            done < <(grep -n 'SUPERSEDED' "$file")
        fi
    done
}

rot_r6() {
    local cl ai cl_updated ai_updated cl_flag ai_flag
    cl=".harmonik/context/captain-lanes.md"
    ai=".harmonik/crew/admiral-initiatives.md"
    [[ -f "$cl" && -f "$ai" ]] || return

    cl_updated="$(grep -m1 -oE 'updated:[[:space:]]*[^[:space:]]+' "$cl")"
    ai_updated="$(grep -m1 -oE 'updated:[[:space:]]*[^[:space:]]+' "$ai")"
    cl_flag="$(grep -m1 -oE 'flagship:[[:space:]]*[^[:space:]]+' "$cl")"
    ai_flag="$(grep -m1 -oE 'flagship:[[:space:]]*[^[:space:]]+' "$ai")"

    if [[ -z "$cl_updated" || -z "$cl_flag" ]]; then
        fail "R6: $cl is missing an 'updated:' or 'flagship:' header line"
    fi
    if [[ -z "$ai_updated" || -z "$ai_flag" ]]; then
        fail "R6: $ai is missing an 'updated:' or 'flagship:' header line"
    fi
    if [[ -n "$cl_flag" && -n "$ai_flag" ]]; then
        local cl_tok="${cl_flag#flagship:}" ai_tok="${ai_flag#flagship:}"
        cl_tok="${cl_tok// /}"
        ai_tok="${ai_tok// /}"
        if [[ "$cl_tok" != "$ai_tok" ]]; then
            fail "R6: $cl flagship:${cl_tok} disagrees with $ai flagship:${ai_tok}"
        fi
    fi
}

rot_r7() {
    local file
    for file in AGENTS.md AGENT_INDEX.md; do
        [[ -f "$file" ]] || continue
        while IFS= read -r hit; do
            local lineno content
            lineno="${hit%%:*}"
            content="${hit#*:}"
            if grep -qiE 'do not read[^.]*STARTUP\.md' <<<"$content"; then
                continue
            fi
            fail "R7: $file:$lineno: re-teaches the old boot — $content"
        done < <(grep -niE 'STARTUP\.md|AGENT_INDEX(\.md)?[[:space:]]*(→|->)[[:space:]]*STATUS|reading order' "$file")
    done
}

run_rot() {
    rot_r1
    rot_r2
    rot_r3
    rot_r4
    rot_r5
    rot_r6
    rot_r7
}

# ── main ──────────────────────────────────────────────────────────────────────────────
mode="check"
case "${1:-}" in
    "") mode="check" ;;
    --apply) mode="apply" ;;
    --rot) mode="rot" ;;
    --all) mode="all" ;;
    -h|--help) usage; exit 0 ;;
    *)
        printf 'agents-skills-sync.sh: unknown argument: %s\n' "$1" >&2
        usage >&2
        exit 2
        ;;
esac

case "$mode" in
    check) run_check ;;
    apply) run_apply ;;
    rot) run_rot ;;
    all) run_check; run_rot ;;
esac

if (( fail_count > 0 )); then
    exit 1
fi
exit 0
