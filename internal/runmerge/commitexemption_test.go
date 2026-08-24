package runmerge

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/commitmsg"
	"github.com/gregberns/harmonik/internal/core"
)

// TestMergePathCommitsCarryTheTrivialExemption asserts that every commit the
// daemon merge path writes for itself lands, and that the `Trivial: true`
// exemption is what makes it land.
//
// WHY BOTH HALVES. The predecessor of this test asserted only the first half —
// "commitmsg.Validate returns no problems" — and named itself after the SUBJECT
// line. That test could not fail. `Validate` short-circuits on the exemption
// before any rule runs, and no rule reads the subject anyway, so a
// hundred-character garbage subject passed it just as well as the real one. It
// was green for a reason unrelated to anything it claimed to check.
//
// The negative control is what makes the positive assertion mean something. The
// merge path writes no `Reviewed-By:` and no `Review-Verdict:`, because these
// commits are daemon plumbing and no reviewer ever sees one. That is only legal
// under the exemption. Strip the exemption line and the gate MUST refuse the
// same message — if it does not, the exemption has stopped being load-bearing
// and the first half is passing by accident again.
//
// The messages come from the production builders, not from copies. A copy is
// how the first half rots: the daemon could start writing `Trivial: yes` and a
// test holding its own string literals would never notice.
//
// Options is left at its zero value ON PURPOSE. The exemption is decided before
// any reviewer name or cleanup mode is read, so passing this repository's real
// reviewer set would make the test depend on git state it does not care about.
func TestMergePathCommitsCarryTheTrivialExemption(t *testing.T) {
	t.Parallel()

	const sampleRunID = "019f555e-bd64-7ebd-bf9e-37155c93c095"
	const sampleBeadID = "hk-r1v2n"

	cases := []struct {
		name string // the production site that writes it
		msg  string
	}{
		{name: "CommitResidualDelta", msg: residualDeltaCommitMessage(core.RunID(uuid.MustParse(sampleRunID)))},
		{name: "StripRunContextFromMerge", msg: stripRunContextCommitMessage},
		{name: "commitFmtChanges", msg: fmtGateCommitMessage(core.BeadID(sampleBeadID))},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if problems := commitmsg.Validate(tc.msg, commitmsg.Options{}); len(problems) > 0 {
				t.Errorf("the commit-message gate refuses the message %s writes:\n%s\n--- message ---\n%s",
					tc.name, commitmsg.Render(problems), tc.msg)
			}

			withoutExemption := dropExemptionLine(tc.msg)
			if withoutExemption == tc.msg {
				t.Fatalf("%s does not write the exemption as its own exact line %q. "+
					"The gate matches that line verbatim, so this message is landing for some "+
					"other reason and the assertion above proves nothing.\n--- message ---\n%s",
					tc.name, exemptionLine, tc.msg)
			}
			if problems := commitmsg.Validate(withoutExemption, commitmsg.Options{}); len(problems) == 0 {
				t.Errorf("the gate accepts %s's message with the %q line removed, so the exemption "+
					"is not what lets it land and this test no longer checks anything.\n--- message ---\n%s",
					tc.name, exemptionLine, withoutExemption)
			}
		})
	}
}

// exemptionLine is the exact line commitmsg treats as the trivial exemption. It
// is matched verbatim: an indented copy, a different case, or a trailing space
// is NOT the exemption.
const exemptionLine = "Trivial: true"

// dropExemptionLine removes every exact exemption line, and returns the message
// unchanged when there is none.
func dropExemptionLine(msg string) string {
	lines := strings.Split(msg, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if line == exemptionLine {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}
