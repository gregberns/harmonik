#!/usr/bin/env bash
# scripts/stranded-test-seams-test.sh — self-test for stranded-test-seams.sh.
#
# The detector exists to find test code that claims coverage it does not have.
# A detector nobody drove is the same defect one level up, so this drives it
# over a fixture tree with a known answer and checks the answer.
#
# Each case states the claim it defends. A case that stops being able to fail is
# a defect here too.

set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
detector="$here/stranded-test-seams.sh"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

fail=0
check() { # <claim> <expected> <actual>
    if [ "$2" = "$3" ]; then
        printf 'ok   %s\n' "$1"
    else
        printf 'FAIL %s\n       want %q, got %q\n' "$1" "$2" "$3"
        fail=1
    fi
}

# ---------------------------------------------------------------------------
# Fixture. Every seam below is named for what it proves.
# ---------------------------------------------------------------------------
mkdir -p "$work/pkg"

cat > "$work/pkg/prod.go" <<'GO'
package pkg

func secretHelper() string { return "x" }

var tunable = 5
GO

# An empty .go file. The scan must open it and move on. No assertion is tied to
# it on its own — an earlier version of this file carried one, and that
# assertion could not fail, which is the exact defect the detector hunts.
: > "$work/pkg/empty.go"

# The caller sorts BEFORE the file that declares the seam. This is what makes
# the two-pass scan load-bearing: with one interleaved pass the candidate is not
# known yet when its only reference is read, and the seam reads as stranded.
cat > "$work/pkg/aaa_caller_test.go" <<'GO'
package pkg

import "testing"

func TestCallerSortsFirst(t *testing.T) { _ = ExportedDeclaredLate(); _ = t }
GO

cat > "$work/pkg/zzz_export_test.go" <<'GO'
package pkg

func ExportedDeclaredLate() string { return secretHelper() }
GO

cat > "$work/pkg/export_test.go" <<'GO'
package pkg

// ExportedNeverCalled is a seam over secretHelper that nothing calls. It is the
// case the detector exists for. Mentioning ExportedNeverCalled in a comment
// must NOT count as a reference.
func ExportedNeverCalled() string { return secretHelper() }

type (
	// ExportedTypeGroupStranded is declared in a type( group and unreferenced.
	ExportedTypeGroupStranded = string
	// ExportedTypeGroupUsed is declared in a type( group and used below.
	ExportedTypeGroupUsed = string
)

func ExportedTakesGroupedType(v ExportedTypeGroupUsed) string { return v }

// ExportedIsCalled is reached from the sibling test file below.
func ExportedIsCalled() string { return secretHelper() }

// ExportedAliasUsedOnlyInASignature is referenced only by the declaration of
// ExportedReturnsTheAlias, which is itself called. It must NOT be reported.
type ExportedAliasUsedOnlyInASignature = string

func ExportedReturnsTheAlias() ExportedAliasUsedOnlyInASignature { return "y" }

var (
	// ExportedTunablePointer is the grouped-declaration shape.
	ExportedTunablePointer = &tunable
	// ExportedGroupedAndStranded is grouped and unreferenced.
	ExportedGroupedAndStranded = &tunable
)
GO

cat > "$work/pkg/use_test.go" <<'GO'
package pkg

import "testing"

func TestSomething(t *testing.T) {
	_ = ExportedIsCalled()
	_ = ExportedReturnsTheAlias()
	_ = ExportedTakesGroupedType("z")
	// A pointer-seam assignment is a real reference, not a comment.
	*ExportedTunablePointer = 9
	if false {
		t.Fatal("unreachable")
	}
}
GO

# A second package that declares a seam with the SAME name and does call it.
# Reference counting is per directory, so it must not mask the stranded copy in
# pkg/.
mkdir -p "$work/other"
cat > "$work/other/prod.go" <<'GO'
package other

func helper() string { return "x" }
GO
cat > "$work/other/export_test.go" <<'GO'
package other

func ExportedNeverCalled() string { return helper() }
GO
cat > "$work/other/use_test.go" <<'GO'
package other

import "testing"

func TestOther(t *testing.T) { _ = ExportedNeverCalled(); _ = t }
GO

out=$("$detector" "$work" 2>&1) && rc=0 || rc=$?
names=$(printf '%s\n' "$out" | awk '/^STRANDED SEAM/ {print $3}' | sort | tr '\n' ' ')

check "a seam with no caller is reported" \
    "1" "$(printf '%s\n' "$out" | grep -c 'ExportedNeverCalled' || true)"

check "a seam a test calls is not reported" \
    "0" "$(printf '%s\n' "$out" | grep -c 'ExportedIsCalled' || true)"

check "a Test function is never treated as a seam" \
    "0" "$(printf '%s\n' "$out" | grep -c 'TestSomething' || true)"

check "a type used only in a called seam's signature is not reported" \
    "0" "$(printf '%s\n' "$out" | grep -c 'ExportedAliasUsedOnlyInASignature' || true)"

check "a pointer-seam assignment counts as a reference" \
    "0" "$(printf '%s\n' "$out" | grep -c 'ExportedTunablePointer' || true)"

check "a grouped var declaration is scanned" \
    "1" "$(printf '%s\n' "$out" | grep -c 'ExportedGroupedAndStranded' || true)"

check "a grouped type declaration is scanned" \
    "1" "$(printf '%s\n' "$out" | grep -c 'ExportedTypeGroupStranded' || true)"

check "a used grouped type is not reported" \
    "0" "$(printf '%s\n' "$out" | grep -c 'ExportedTypeGroupUsed' || true)"

check "a seam whose only caller is read before its declaration is not reported" \
    "0" "$(printf '%s\n' "$out" | grep -c 'ExportedDeclaredLate' || true)"

check "a called seam in one package does not mask the same name in another" \
    "1" "$(printf '%s\n' "$out" | grep -c 'ExportedNeverCalled' || true)"

check "exactly the three stranded seams are reported" \
    "ExportedGroupedAndStranded ExportedNeverCalled ExportedTypeGroupStranded " "$names"

check "a tree with a stranded seam exits 1" "1" "$rc"

# ---------------------------------------------------------------------------
# A clean tree must exit 0. This is the case that keeps the detector honest:
# without it, a detector that reported everything would still pass above.
# ---------------------------------------------------------------------------
rm "$work/pkg/export_test.go"
cat > "$work/pkg/use_test.go" <<'GO'
package pkg

import "testing"

func TestNothing(t *testing.T) { _ = secretHelper(); _ = t }
GO

cleanout=$("$detector" "$work" 2>&1) && cleanrc=0 || cleanrc=$?
check "a tree with no stranded seam exits 0" "0" "$cleanrc"
check "a clean tree reports none" \
    "0" "$(printf '%s\n' "$cleanout" | grep -c '^STRANDED SEAM' || true)"

if [ "$fail" -ne 0 ]; then
    echo "stranded-test-seams-test: FAILED"
    exit 1
fi
echo "stranded-test-seams-test: all cases pass"
