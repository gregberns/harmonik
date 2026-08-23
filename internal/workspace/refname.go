package workspace

import (
	"context"
	"encoding/hex"
	"fmt"
	"os/exec"
	"strings"
)

// BeadIDToRefSafe transforms proposedBeadID into a git-ref-safe component
// suitable for embedding in a branch name (e.g., the <parent_bead_id_refsafe>
// slot of harmonik/integration/<parent_bead_id_refsafe>).
//
// Procedure per workspace-model.md §4.2 WM-006a:
//
//  1. Construct the proposed full branch name by embedding proposedBeadID
//     verbatim as "harmonik/integration/<proposedBeadID>".
//  2. Invoke "git check-ref-format refs/heads/<proposed>".
//     Zero exit → return proposedBeadID unchanged (verbatim accepted).
//  3. Non-zero exit → apply the canonical fallback transformation:
//     (i)  hex-encode every byte NOT in [a-zA-Z0-9/_-] as %HH (uppercase).
//     (ii) collapse every run of '/' longer than one into a single '/'.
//     (iii) reject and return [ErrRefNameInvalid] if the result is empty,
//     is the bare "@" character, is a single ".", or still fails a second
//     "git check-ref-format" invocation.
//  4. Re-validate the fallback form; if it passes, return the transformed
//     bead ID. Otherwise return [ErrRefNameInvalid].
//
// git check-ref-format is the single source of truth per WM-006a; this
// function MUST NOT cache or independently encode git's accepted set.
//
// ErrRefNameInvalid is returned when the bead ID cannot be made ref-safe
// after the canonical fallback (workspace-model.md §8).
func BeadIDToRefSafe(ctx context.Context, proposedBeadID string) (string, error) {
	const integrationPrefix = "harmonik/integration/"
	proposed := integrationPrefix + proposedBeadID

	if refNameCheckRefFormat(ctx, proposed) {
		return proposedBeadID, nil
	}

	fallback := refNameHexEncodeFallback(proposedBeadID)

	if fallback == "" || fallback == "@" || fallback == "." {
		return "", fmt.Errorf("%w: bead ID %q produces empty/bare ref component after fallback",
			ErrRefNameInvalid, proposedBeadID)
	}

	fallbackFull := integrationPrefix + fallback
	if !refNameCheckRefFormat(ctx, fallbackFull) {
		return "", fmt.Errorf("%w: bead ID %q still fails git check-ref-format after fallback (encoded: %q)",
			ErrRefNameInvalid, proposedBeadID, fallback)
	}

	return fallback, nil
}

func refNameCheckRefFormat(ctx context.Context, branch string) bool {
	refPath := "refs/heads/" + branch
	//nolint:gosec // G204: refPath is constructed from internal constants + bead ID; git is a fixed binary
	cmd := exec.CommandContext(ctx, "git", "check-ref-format", refPath)
	return cmd.Run() == nil
}

func refNameHexEncodeFallback(beadID string) string {
	var sb strings.Builder
	for i := 0; i < len(beadID); i++ {
		b := beadID[i]
		switch {
		case (b >= 'a' && b <= 'z') ||
			(b >= 'A' && b <= 'Z') ||
			(b >= '0' && b <= '9') ||
			b == '/' || b == '_' || b == '-':
			sb.WriteByte(b)
		default:
			sb.WriteString("%" + strings.ToUpper(hex.EncodeToString([]byte{b})))
		}
	}
	result := sb.String()
	for strings.Contains(result, "//") {
		result = strings.ReplaceAll(result, "//", "/")
	}
	return result
}
