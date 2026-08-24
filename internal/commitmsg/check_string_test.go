package commitmsg

// String names a Check for a test failure message. It lives in a test file
// because production never renders a check name — Render prints the problem
// lines, not the rule that produced them. Keeping it here means the
// reachability gate does not have to carry an unreachable exported method,
// and a reader looking for it in production does not find one.
func (c Check) String() string {
	switch c {
	case CheckReviewedByPresent:
		return "reviewed-by-present"
	case CheckReviewVerdictPresent:
		return "review-verdict-present"
	case CheckVerdictJSON:
		return "verdict-json"
	case CheckSchemaVersion:
		return "schema-version"
	case CheckNotes:
		return "notes"
	case CheckVerdictValue:
		return "verdict-value"
	case CheckApprovalIdentity:
		return "approval-identity"
	case CheckApprovalFlags:
		return "approval-flags"
	case CheckSelfAuthored:
		return "self-authored"
	case CheckNoReviewerNamed:
		return "no-reviewer-named"
	case CheckBlockNeverCommitted:
		return "block-never-committed"
	default:
		return "unknown"
	}
}
