#!/usr/bin/env bash
# check-report.sh — quiet, unified reporter for the make check gauntlet.
#
# PROBLEM: `make check-fast|check-short|check` dump hundreds-to-thousands of
# lines of per-test INFO/WARN/`daemon:` log noise even on a fully green run. A
# green `check-short` is 1000+ lines, ~4 of which are signal.
#
# THIS SCRIPT changes ONLY how the output is presented — never what runs:
#   * ALL GREEN  -> one `✓ <step>` line per step that ran (~10 lines total).
#   * ANY RED    -> a header per failing step and, under it, ONLY that step's
#                   failing lines (the `--- FAIL:` blocks / lint findings / gate
#                   error). Passing steps still collapse to one `✓` line.
#   * exit non-zero iff any GATING step failed (the full-tier legacy lint is
#     explicitly NON-GATING — surfaced distinctly, never flips the exit code).
#
# FAITHFULNESS / SELF-VERIFICATION: the step list is DERIVED at runtime from
# `make -n <target>` — i.e. straight from the Makefile tier definition, with
# sub-makes flattened and variables expanded. It is not a hand-maintained copy,
# so a recipe line added to a tier is automatically run and shown here. Any
# command this script cannot map to a friendly label still RUNS and is still
# SHOWN (with a generic label) plus a loud NOTE — a new step can never be
# silently omitted. `--list` prints the derived model without running it, and
# `--verify` exits non-zero if any command is unlabeled (a CI meta-check).
#
# Usage:
#   scripts/check-report.sh [fast|short|full]   # default: short
#   scripts/check-report.sh short --list         # print derived steps, don't run
#   scripts/check-report.sh full  --verify       # assert every step is labeled
#
# Raw per-step logs are kept on disk (path printed) for drill-down.

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT" || exit 2

# ---- args ------------------------------------------------------------------
TIER="short"
MODE="run"   # run | list | verify
for arg in "$@"; do
    case "$arg" in
        fast|short|full) TIER="$arg" ;;
        --list)   MODE="list" ;;
        --verify) MODE="verify" ;;
        -h|--help)
            grep '^#' "$0" | sed 's/^# \{0,1\}//' | sed -n '1,40p'
            exit 0 ;;
        *) echo "check-report: unknown arg '$arg' (want fast|short|full [--list|--verify])" >&2; exit 2 ;;
    esac
done

case "$TIER" in
    fast)  TARGET="check-fast" ;;
    short) TARGET="check-short" ;;
    full)  TARGET="check" ;;
    *)     echo "check-report: unknown tier '$TIER'" >&2; exit 2 ;;
esac

# ---- colors (only when stdout is a tty) ------------------------------------
if [ -t 1 ]; then
    C_GRN=$'\033[32m'; C_RED=$'\033[31m'; C_YEL=$'\033[33m'
    C_DIM=$'\033[2m';  C_BLD=$'\033[1m';  C_RST=$'\033[0m'
else
    C_GRN=""; C_RED=""; C_YEL=""; C_DIM=""; C_BLD=""; C_RST=""
fi

# ---- classify a command -> group|label|filter|gating -----------------------
# Order matters: more-specific patterns first (vet-tagged before vet;
# cmd-coverage before coverage; delta-lint before full-lint).
# gating: "gate" (flips exit code) | "nongate" (surfaced, never gates).
classify() {
    local c="$1"
    case "$c" in
        *go-format.sh\ check*)                 echo "fmtcheck|fmt-check|generic|gate" ;;
        *"go vet -tags"*)                       echo "vettagged|vet-tagged|vet|gate" ;;
        "go vet "*|*" go vet "*)                echo "govet|go vet|vet|gate" ;;
        "go build "*|*" go build "*)            echo "gobuild|go build|build|gate" ;;
        *golangci-lint\ run\ --new-from-rev*)   echo "lintdelta|lint (delta)|lint|gate" ;;
        *golangci-lint\ run*)                   echo "lintfull|lint (full, legacy)|lintfull|nongate" ;;
        *-freeze-gate.sh*|*emitter-gate.sh*)    echo "gates|gates|gate|gate" ;;
        *cmd-coverage-gate.sh*|*with-isolated-gocache*) echo "cmdcov|cmd coverage|generic|gate" ;;
        *coverage-gate.sh*)                     echo "coverage|coverage|generic|gate" ;;
        *forbid-import*)                        echo "forbid|forbid-import|generic|gate" ;;
        *govulncheck*)                          echo "govuln|govulncheck|generic|gate" ;;
        *go\ test*)                             echo "tests|tests|gotest|gate" ;;
        *go\ mod\ tidy*|*go.mod.check*|*go.sum.check*|*cp\ go.mod*|*cp\ go.sum*) echo "modtidy|go mod tidy|generic|gate" ;;
        *)                                      echo "UNLABELED|${c:0:48}|generic|gate" ;;
    esac
}

# ---- flatten `make -n <target>` into a command list ------------------------
# Skips the recursive `make <subtarget>` wrapper lines (their commands are
# already flattened + printed by make -n), comment lines, and blanks. Joins
# backslash-continued recipe lines into a single command.
mapfile -t RAW < <(make -n "$TARGET" 2>&1)
if [ "${#RAW[@]}" -eq 0 ]; then
    echo "check-report: 'make -n $TARGET' produced nothing (is make on PATH?)" >&2
    exit 2
fi

CMD_TEXT=()   # each element = one command (possibly multi-line)
i=0
n=${#RAW[@]}
while [ "$i" -lt "$n" ]; do
    line="${RAW[$i]}"
    # skip blanks
    if [ -z "${line//[[:space:]]/}" ]; then i=$((i+1)); continue; fi
    # skip comment lines
    if [[ "$line" =~ ^[[:space:]]*# ]]; then i=$((i+1)); continue; fi
    # skip recursive-make wrapper lines (first token's basename == make)
    first="${line%% *}"; base="${first##*/}"
    if [ "$base" = "make" ] || [ "$base" = "gmake" ]; then i=$((i+1)); continue; fi
    # accumulate backslash continuations into one command
    cmd="$line"
    while [[ "$cmd" == *\\ ]] && [ "$((i+1))" -lt "$n" ]; do
        i=$((i+1))
        cmd="${cmd%\\}"$'\n'"${RAW[$i]}"
    done
    i=$((i+1))
    CMD_TEXT+=("$cmd")
done

if [ "${#CMD_TEXT[@]}" -eq 0 ]; then
    echo "check-report: no runnable commands parsed from '$TARGET'" >&2
    exit 2
fi

# ---- collapse consecutive same-group commands into logical steps -----------
STEP_GROUP=(); STEP_LABEL=(); STEP_FILTER=(); STEP_GATING=(); STEP_CMDIDX=()
UNLABELED_CMDS=()
prev_group=""
for k in "${!CMD_TEXT[@]}"; do
    IFS='|' read -r g label filt gating <<<"$(classify "${CMD_TEXT[$k]}")"
    [ "$g" = "UNLABELED" ] && UNLABELED_CMDS+=("${CMD_TEXT[$k]}")
    # UNLABELED never collapses (unique group per command)
    if [ "$g" = "UNLABELED" ] || [ "$g" != "$prev_group" ]; then
        STEP_GROUP+=("$g"); STEP_LABEL+=("$label"); STEP_FILTER+=("$filt"); STEP_GATING+=("$gating")
        STEP_CMDIDX+=("$k")
        prev_group="$g"
    else
        s=$(( ${#STEP_GROUP[@]} - 1 ))
        STEP_CMDIDX[$s]="${STEP_CMDIDX[$s]} $k"
    fi
done

# ---- --list / --verify modes ----------------------------------------------
if [ "$MODE" = "list" ]; then
    echo "${C_BLD}check-report [$TIER] -> make $TARGET — derived step model${C_RST}"
    for s in "${!STEP_GROUP[@]}"; do
        gate_tag=""; [ "${STEP_GATING[$s]}" = "nongate" ] && gate_tag=" ${C_YEL}(non-gating)${C_RST}"
        printf '  %2d. %-22s [%s]%s\n' "$((s+1))" "${STEP_LABEL[$s]}" "${STEP_FILTER[$s]}" "$gate_tag"
        for k in ${STEP_CMDIDX[$s]}; do
            printf '        %s%s%s\n' "$C_DIM" "${CMD_TEXT[$k]//$'\n'/ }" "$C_RST"
        done
    done
    exit 0
fi
if [ "$MODE" = "verify" ]; then
    if [ "${#UNLABELED_CMDS[@]}" -gt 0 ]; then
        echo "${C_RED}check-report --verify: ${#UNLABELED_CMDS[@]} unlabeled command(s) in '$TARGET' — add to classify() in scripts/check-report.sh:${C_RST}" >&2
        for c in "${UNLABELED_CMDS[@]}"; do echo "  - ${c//$'\n'/ }" >&2; done
        exit 1
    fi
    echo "${C_GRN}check-report --verify: every step of '$TARGET' maps to a labeled reporter step.${C_RST}"
    exit 0
fi

# ---- per-step failing-line filters -----------------------------------------
# Each reads a raw log on stdin and prints ONLY the load-bearing failing lines.
filter_gotest() {
    awk '
        /^--- FAIL/            { inblk=1; print; next }
        inblk && /^[ \t]/      { print; next }
        inblk                  { inblk=0 }
        /^FAIL[ \t]/           { print; next }   # FAIL<tab>pkg summary
        /^# /                  { print; next }   # build/vet pkg error header
        /[^ ].*\.go:[0-9]+:[0-9]+:/ { print; next }  # compile / vet errors
        /^panic:/              { print; next }
        /\[build failed\]/     { print; next }
    '
}
filter_lint() {
    local out
    out="$(grep -E ':[0-9]+:[0-9]+:|^level=(error|warning)|^ERRO|could not|typecheck' 2>/dev/null || true)"
    if [ -n "$out" ]; then printf '%s\n' "$out"; else cat; fi
}
filter_generic() { grep -v '^[[:space:]]*$' || true; }  # everything non-blank
# Freeze-gate scripts print "<gate>: OK — ..." on pass and FORBIDDEN/FAIL on
# fail. Drop the passing-gate chatter; keep everything else (the failing gate).
filter_gate() {
    local all pruned
    all="$(cat)"
    pruned="$(printf '%s\n' "$all" | grep -vE '[[:space:]]OK[[:space:]]' | grep -v '^[[:space:]]*$' || true)"
    if [ -n "$pruned" ]; then printf '%s\n' "$pruned"; else printf '%s\n' "$all" | grep -v '^[[:space:]]*$'; fi
}

apply_filter() {
    local filt="$1"
    case "$filt" in
        gotest) filter_gotest ;;
        lint)   filter_lint ;;
        gate)   filter_gate ;;
        *)      filter_generic ;;   # vet, build, generic
    esac
}

# ---- run --------------------------------------------------------------------
LOGDIR="$(mktemp -d "${TMPDIR:-/tmp}/check-report-XXXXXX")"
overall_start=$SECONDS
declare -a STEP_OK STEP_LOG STEP_NOTE
gating_failed=0
last_fail_code=0

for s in "${!STEP_GROUP[@]}"; do
    logf="$LOGDIR/$(printf '%02d' "$((s+1))")-${STEP_GROUP[$s]}.log"
    STEP_LOG[$s]="$logf"
    : >"$logf"
    step_rc=0
    for k in ${STEP_CMDIDX[$s]}; do
        bash -c "${CMD_TEXT[$k]}" >>"$logf" 2>&1
        rc=$?
        if [ "$rc" -ne 0 ]; then step_rc=$rc; fi
    done
    if [ "$step_rc" -eq 0 ]; then
        STEP_OK[$s]=1
    else
        STEP_OK[$s]=0
        if [ "${STEP_GATING[$s]}" = "gate" ]; then
            gating_failed=1; last_fail_code=$step_rc
        fi
    fi
done
elapsed=$(( SECONDS - overall_start ))

# ---- count helpers for friendly green labels -------------------------------
label_for() {
    local s="$1" base="${STEP_LABEL[$s]}"
    case "${STEP_GROUP[$s]}" in
        gates)
            local n=0; for _ in ${STEP_CMDIDX[$s]}; do n=$((n+1)); done
            echo "gates ($n)" ;;
        tests)
            local pk; pk=$(grep -cE '^(ok|FAIL|\?)[[:space:]]+' "${STEP_LOG[$s]}" 2>/dev/null); pk=${pk:-0}
            if [ "$pk" -eq 0 ] && grep -q 'skipping go test' "${STEP_LOG[$s]}" 2>/dev/null; then
                echo "tests (skipped: no changed pkgs)"
            else
                echo "tests ($pk pkgs)"
            fi ;;
        lintfull)
            local nf; nf=$(grep -cE ':[0-9]+:[0-9]+:' "${STEP_LOG[$s]}" 2>/dev/null); nf=${nf:-0}
            echo "lint (full: $nf legacy findings, non-gating)" ;;
        *) echo "$base" ;;
    esac
}

# ---- report -----------------------------------------------------------------
total=${#STEP_GROUP[@]}
nfail=0
for s in "${!STEP_GROUP[@]}"; do
    [ "${STEP_OK[$s]}" -eq 0 ] && [ "${STEP_GATING[$s]}" = "gate" ] && nfail=$((nfail+1))
done

if [ "$gating_failed" -eq 0 ]; then
    echo "${C_GRN}${C_BLD}check-report [$TIER]  —  all green${C_RST}  ${C_DIM}($total steps, ${elapsed}s)${C_RST}"
else
    echo "${C_RED}${C_BLD}check-report [$TIER]  —  FAILED${C_RST}  ${C_DIM}($nfail of $total steps failed, ${elapsed}s)${C_RST}"
fi

# compact checklist
for s in "${!STEP_GROUP[@]}"; do
    lbl="$(label_for "$s")"
    if [ "${STEP_OK[$s]}" -eq 1 ]; then
        echo "  ${C_GRN}✓${C_RST} $lbl"
    elif [ "${STEP_GATING[$s]}" = "nongate" ]; then
        echo "  ${C_YEL}●${C_RST} $lbl"
    else
        echo "  ${C_RED}✗${C_RST} $lbl"
    fi
done

# per-failing-step detail (gating failures only get the drill-down block)
for s in "${!STEP_GROUP[@]}"; do
    [ "${STEP_OK[$s]}" -eq 1 ] && continue
    [ "${STEP_GATING[$s]}" = "gate" ] || continue
    echo
    echo "${C_RED}${C_BLD}━━━ ✗ ${STEP_LABEL[$s]} ━━━${C_RST}"
    apply_filter "${STEP_FILTER[$s]}" <"${STEP_LOG[$s]}" | sed 's/^/  /'
    echo "  ${C_DIM}(full log: ${STEP_LOG[$s]})${C_RST}"
done

# unlabeled-step note (a new Makefile step ran but has no friendly label yet)
if [ "${#UNLABELED_CMDS[@]}" -gt 0 ]; then
    echo
    echo "${C_YEL}NOTE: ${#UNLABELED_CMDS[@]} step(s) had no friendly label — they RAN and are shown above; add to classify() in scripts/check-report.sh:${C_RST}" >&2
    for c in "${UNLABELED_CMDS[@]}"; do echo "  - ${c//$'\n'/ }" >&2; done
fi

echo "${C_DIM}raw logs: $LOGDIR${C_RST}"

# exit non-zero iff a gating step failed
if [ "$gating_failed" -ne 0 ]; then
    exit "${last_fail_code:-1}"
fi
exit 0
