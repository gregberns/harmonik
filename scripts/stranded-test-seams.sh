#!/usr/bin/env bash
# scripts/stranded-test-seams.sh — find exported test seams that nothing calls.
#
# WHY THIS EXISTS. A test seam is an exported identifier declared in a _test.go
# file so that an external test package can reach an unexported production
# symbol. The seam is test code, so the Go compiler does not report it when it
# becomes unused, and `go vet` does not either. A seam whose caller was deleted
# therefore stays in the tree and reads as coverage. It is not coverage. The
# production symbol behind it can go to zero executions and no test turns red.
#
# This is not a style check. Every seam this script prints is a claim of test
# coverage that the test suite does not honor.
#
# WHAT IT SCANS. Exported identifiers declared in a _test.go file:
#   - top-level `func`, `type`, `var` and `const` declarations, and
#   - entries inside a top-level `var (`, `const (` or `type (` group.
# The four go-test entry-point prefixes (Test, Benchmark, Example, Fuzz) are not
# seams and are excluded. Methods are also out of scope: a method on a test-only
# type is reached through its receiver, so an unused one is a different defect
# and needs a different check. There are about 800 of them in this tree.
#
# WHAT IT REPORTS. A scanned name is stranded when no line in its own directory
# refers to it, apart from its own declaration. Comment lines are not
# references. A `*ExportedName = value` line IS a reference — that is how a seam
# over a package-level variable is used, so the comment filter must not eat it.
#
# WHY THE DIRECTORY IS THE SCOPE. An identifier declared in a _test.go file is
# compiled only into that one package's test binary. Every legal reference to it
# is therefore a file in the same directory. Counting references per directory
# is not an approximation — it is the language rule, and it stops two packages
# that use the same seam name from masking each other.
#
# WHAT IT DOES NOT CATCH. A seam reached only through reflection, or through a
# name built at run time, reads as stranded here.
#
# EXIT CODES
#   0  no stranded seam
#   1  at least one stranded seam (names printed on stdout)
#
# THIS IS A REPORT, NOT A GATE. The tree holds pre-existing stranded seams.
# To make this a merge gate, first clean those, or add an allow list in the
# style of tools/lintreport/allow.txt. A gate that is red the day it lands gets
# switched off rather than obeyed.
#
# The fix for a finding is one of two things, and both are improvements: call
# the seam from a test, or delete the seam and the claim to coverage with it.
#
# Self-test: scripts/stranded-test-seams-test.sh

set -euo pipefail

# Scans the repo root by default. The optional argument names a different tree,
# which is how the self-test drives it over a fixture.
cd "${1:-$(dirname "$0")/..}"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# Sorted, so the order two files are scanned in does not depend on the
# filesystem. The self-test's ordering case needs a caller to be read before the
# file that declares the seam, and readdir order differs between APFS and ext4.
find . -name '*.go' -not -path './.git/*' | sort > "$work/files"

# awk opens each path itself, from the list file. No path is ever put on a
# command line, so a path that holds a space is safe and ARG_MAX does not apply.
# Two full passes: the candidate set must be complete before references are
# counted, or a seam whose only caller sorts before its declaration reads as
# stranded.
awk -v list="$work/files" '
    function dirOf(p,   i) {
        i = length(p)
        while (i > 0 && substr(p, i, 1) != "/") i--
        return substr(p, 1, i)
    }
    function isComment(line) {
        # A comment line, or a block-comment continuation. "* " needs the space
        # so that a `*ExportedName = v` assignment is NOT read as a comment.
        return line ~ /^[ \t]*\/\// || line ~ /^[ \t]*\/\*/ || line ~ /^[ \t]*\* /
    }
    function scan(path, pass,   line, inGroup, name, isDecl, f, n, toks, i, key, d) {
        inGroup = 0
        d = dirOf(path)
        while ((getline line < path) > 0) {
            if (line ~ /^(var|const|type) \(/) { inGroup = 1; continue }
            if (inGroup && line ~ /^\)/)       { inGroup = 0; continue }

            name = ""; isDecl = 0
            if (line ~ /^(func|type|var|const) [A-Z][A-Za-z0-9_]*/) {
                split(line, f, /[ \t(]+/); name = f[2]; isDecl = 1
            } else if (inGroup && line ~ /^[ \t]+[A-Z][A-Za-z0-9_]*[ \t]/) {
                split(line, f, /[ \t]+/); name = f[2]; isDecl = 1
            }

            if (pass == 1) {
                if (isDecl && path ~ /_test\.go$/ && name != "" &&
                    name !~ /^(Test|Benchmark|Example|Fuzz)/) {
                    key = d SUBSEP name
                    if (!(key in candidate)) { declLoc[key] = path; nameOf[key] = name }
                    candidate[key] = 1
                }
                continue
            }

            # Pass 2. A comment is not a reference. A declaration line is not a
            # reference to the name it declares — but it IS a reference to every
            # other name on it, because a seam over a type is often used only in
            # the signature of the seam that returns it.
            if (isComment(line)) continue
            n = split(line, toks, /[^A-Za-z0-9_]+/)
            for (i = 1; i <= n; i++) {
                if (isDecl && toks[i] == name) continue
                key = d SUBSEP toks[i]
                if (key in candidate) refs[key] = 1
            }
        }
        close(path)
    }
    BEGIN {
        while ((getline path < list) > 0) files[++nf] = path
        close(list)
        for (i = 1; i <= nf; i++) scan(files[i], 1)
        for (i = 1; i <= nf; i++) scan(files[i], 2)
        for (key in candidate) {
            total++
            if (!(key in refs)) {
                printf "STRANDED SEAM  %-52s %s\n", nameOf[key], declLoc[key]
                stranded++
            }
        }
        printf "%d %d\n", stranded + 0, total + 0 > "/dev/stderr"
    }
' > "$work/out" 2> "$work/counts"

sort "$work/out"

read -r stranded total < "$work/counts"

if [ "$stranded" -eq 0 ]; then
    echo "stranded-test-seams: clean (${total} seams, all called)"
    exit 0
fi

echo
echo "stranded-test-seams: ${stranded} of ${total} exported test seams have no caller."
echo "Each one names a production symbol that no test reaches. Call it or delete it."
exit 1
