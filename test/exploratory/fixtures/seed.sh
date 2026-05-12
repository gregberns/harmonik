#!/usr/bin/env bash
# seed.sh — fixture bead corpus generator for the exploratory testing wave
#
# Creates a fresh .beads/ SQLite workspace in DEST pre-seeded with N ready
# beads of varied shapes, suitable for use as a fixture corpus with reset.sh.
#
# Usage: bash seed.sh --count N --dest DIR
#   --count N   Number of beads to seed (default: 5)
#   --dest DIR  Destination directory (required); will be created if absent
#
# Validation:
#   bash seed.sh --count 3 --dest /tmp/myfix
#   (cd /tmp/myfix && br ready --limit 0)   # → shows 3 entries
#
# Idempotent: re-running with the same dest overwrites prior contents.
set -euo pipefail

usage() {
    echo "Usage: $0 --dest DIR [--count N]" >&2
    echo "" >&2
    echo "  --dest DIR    Destination directory for the .beads/ workspace (required)" >&2
    echo "  --count N     Number of beads to seed (default: 5)" >&2
    exit 2
}

COUNT=5
DEST=""

while [[ $# -gt 0 ]]; do
    case "$1" in
        --count)
            [[ $# -ge 2 ]] || usage
            COUNT="$2"
            shift 2
            ;;
        --dest)
            [[ $# -ge 2 ]] || usage
            DEST="$2"
            shift 2
            ;;
        *)
            echo "Unknown argument: $1" >&2
            usage
            ;;
    esac
done

if [[ -z "$DEST" ]]; then
    echo "Error: --dest is required" >&2
    usage
fi

if ! [[ "$COUNT" =~ ^[0-9]+$ ]] || [[ "$COUNT" -lt 1 ]]; then
    echo "Error: --count must be a positive integer, got: $COUNT" >&2
    exit 2
fi

# --- Fixture body variants (cycle through these for N beads) ---

# Shape 1: ASCII single-line body
BODY_1="Single-line ASCII body for fixture testing."

# Shape 2: Multi-paragraph body (~500 words of filler text)
BODY_2="Lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat.

Duis aute irure dolor in reprehenderit in voluptate velit esse cillum dolore eu fugiat nulla pariatur. Excepteur sint occaecat cupidatat non proident, sunt in culpa qui officia deserunt mollit anim id est laborum. Sed ut perspiciatis unde omnis iste natus error sit voluptatem accusantium doloremque laudantium.

Totam rem aperiam, eaque ipsa quae ab illo inventore veritatis et quasi architecto beatae vitae dicta sunt explicabo. Nemo enim ipsam voluptatem quia voluptas sit aspernatur aut odit aut fugit, sed quia consequuntur magni dolores eos qui ratione voluptatem sequi nesciunt.

Neque porro quisquam est, qui dolorem ipsum quia dolor sit amet, consectetur, adipisci velit, sed quia non numquam eius modi tempora incidunt ut labore et dolore magnam aliquam quaerat voluptatem. Ut enim ad minima veniam, quis nostrum exercitationem ullam corporis suscipit laboriosam.

Quis autem vel eum iure reprehenderit qui in ea voluptate velit esse quam nihil molestiae consequatur, vel illum qui dolorem eum fugiat quo voluptas nulla pariatur? At vero eos et accusamus et iusto odio dignissimos ducimus qui blanditiis praesentium voluptatum deleniti atque corrupti quos dolores."

# Shape 3: Unicode title + body (CJK characters, emoji)
TITLE_3="テスト用ビード 🧪 — 探索的テスト"
BODY_3="このビードはUnicodeタイトルと本文を含む。絵文字もテストする: 🎯 ✅ 🔍。中文：这是一个测试条目，用于验证Unicode处理。한국어: 유니코드 테스트 항목입니다."

# Shape 4: Body with typical punctuation
BODY_4="It's a body with \"quotation marks\", apostrophes (like don't and won't), parentheses (nested (deeply)), and semi-colons; plus colons: here. Also: hyphens-in-words, ellipses... and a trailing period."

# Shape 5: Empty / near-empty body
BODY_5=""

# --- Prepare destination ---
mkdir -p "$DEST"

# Remove any existing .beads/ so init is idempotent
if [[ -d "$DEST/.beads" ]]; then
    rm -rf "$DEST/.beads"
fi

# Initialize a fresh beads workspace in DEST
(cd "$DEST" && br init --prefix fx 2>&1)

# --- Seed N beads cycling through the 5 shapes ---
for i in $(seq 1 "$COUNT"); do
    # 1-based modulo (1..5)
    shape=$(( ((i - 1) % 5) + 1 ))
    case "$shape" in
        1)
            title="Fixture bead $i — ASCII single-line"
            body="$BODY_1"
            ;;
        2)
            title="Fixture bead $i — multi-paragraph"
            body="$BODY_2"
            ;;
        3)
            title="$TITLE_3 $i"
            body="$BODY_3"
            ;;
        4)
            title="Fixture bead $i — punctuation body"
            body="$BODY_4"
            ;;
        5)
            title="Fixture bead $i — empty body"
            body="$BODY_5"
            ;;
    esac

    if [[ -n "$body" ]]; then
        (cd "$DEST" && br create "$title" --body "$body" --silent 2>&1)
    else
        (cd "$DEST" && br create "$title" --silent 2>&1)
    fi
done

echo "Seeded $COUNT bead(s) in $DEST"
