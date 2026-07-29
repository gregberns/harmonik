#!/usr/bin/env bash

set -euo pipefail

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
subject="$script_dir/go-format.sh"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

repo="$tmp/repo"
mkdir -p "$repo/bin" "$repo/ignored/worktree"
git -C "$tmp" init -q repo
git -C "$repo" config user.email test@example.com
git -C "$repo" config user.name test
printf 'module example.test/format\n\ngo 1.23\n' > "$repo/go.mod"
printf 'ignored/\n' > "$repo/.gitignore"
printf 'package tracked\n' > "$repo/tracked.go"
printf 'package deleted\n' > "$repo/deleted.go"
git -C "$repo" add go.mod .gitignore tracked.go deleted.go
git -C "$repo" commit -qm base
rm "$repo/deleted.go"
printf 'package fresh // BADFMT\n' > "$repo/fresh.go"
printf 'package spaced // BADFMT\n' > "$repo/space name.go"
printf 'package ignored // BADFMT\n' > "$repo/ignored/worktree/stale.go"

cat > "$repo/bin/gofumpt" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
mode=$1
shift
[[ ${1:-} == -- ]] && shift
for file in "$@"; do
    case "$mode" in
        -l) grep -q BADFMT "$file" && echo "$file" || true ;;
        -d) grep -q BADFMT "$file" && echo "diff $file" || true ;;
        -w) sed -i.bak 's/ BADFMT//' "$file"; rm -f "$file.bak" ;;
        *) exit 2 ;;
    esac
done
EOF
cat > "$repo/bin/gci" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
# Mirror gci's real stdin behaviour: its diff/print subcommands treat stdin as
# an additional Go source file whenever stdin is not a character device, which
# is exactly what a CI build step hands a subprocess.
if [[ $1 == diff && ! -c /dev/fd/0 ]]; then
    echo "Error: StdIn:1:1: expected 'package', found 'EOF'" >&2
    exit 1
fi
mode=$1
shift
while [[ $# -gt 0 && $1 != -- ]]; do shift; done
[[ ${1:-} == -- ]] && shift
for file in "$@"; do
    case "$mode" in
        diff) grep -q BADIMPORT "$file" && echo "diff $file" || true ;;
        write) sed -i.bak 's/ BADIMPORT//' "$file"; rm -f "$file.bak" ;;
        *) exit 2 ;;
    esac
done
EOF
chmod +x "$repo/bin/gofumpt" "$repo/bin/gci"

# Every invocation runs with a pipe on stdin, never a terminal. That is what a
# CI build step provides, and it is the condition under which gci treats stdin
# as an extra (empty) Go source file. Running the subject only from an
# interactive shell hid a week of red CI.
run_subject() {
    printf '' | (cd "$repo" && GOFUMPT="$repo/bin/gofumpt" GCI="$repo/bin/gci" "$subject" "$@")
}

if output=$(run_subject check 2>&1); then
    echo "expected an unformatted non-ignored untracked file to fail" >&2
    exit 1
fi
grep -q '^fresh.go$' <<<"$output"
grep -q '^space name.go$' <<<"$output"
if grep -q 'ignored/worktree/stale.go' <<<"$output"; then
    echo "ignored worktree file leaked into formatter input" >&2
    exit 1
fi

run_subject write
run_subject check
grep -q BADFMT "$repo/ignored/worktree/stale.go"
if grep -q BADFMT "$repo/fresh.go"; then
    echo "write mode did not format the non-ignored untracked file" >&2
    exit 1
fi
if grep -q BADFMT "$repo/space name.go"; then
    echo "write mode did not safely handle a path containing spaces" >&2
    exit 1
fi

printf 'package tracked // BADIMPORT\n' > "$repo/tracked.go"
if output=$(run_subject check 2>&1); then
    echo "expected tracked import drift to fail" >&2
    exit 1
fi
grep -q 'diff tracked.go' <<<"$output"

echo "go-format-test: PASS"
