package commitmsg

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

// The sentinels the field readers return for the ways a value can be absent or
// unusable. They are values a JSON document cannot produce by accident, and
// they appear verbatim in a refusal so the author is told WHICH of the several
// ways they got it wrong.
// byteOrderMark is U+FEFF, written as an escape so the next reader can see it.
const byteOrderMark = "\ufeff"

const (
	sentinelAbsent  = "__ABSENT__"
	sentinelEmpty   = "__EMPTY__"
	sentinelNull    = "__NULL__"
	sentinelControl = "__CONTROL__"
)

// The verdict words schema v1 allows.
const (
	VerdictApprove        = "APPROVE"
	VerdictRequestChanges = "REQUEST_CHANGES"
	VerdictBlock          = "BLOCK"
	VerdictNotReviewed    = "NOT_REVIEWED"
	VerdictClean          = "CLEAN"
	VerdictDriftMinor     = "DRIFT_MINOR"
	VerdictDriftMajor     = "DRIFT_MAJOR"
)

// verdictProblems reads the `Review-Verdict:` trailer and applies every rule
// that depends on what it says.
func verdictProblems(reviewedByLines []string, reviewedBy, verdictLine string, opts Options) []Problem {
	raw := strings.TrimPrefix(verdictLine, "Review-Verdict: ")
	raw = strings.TrimPrefix(raw, "Review-Verdict:") // the no-space variant
	got := "  Got: " + raw

	fields, parseProblems := decodeVerdict(raw, got)
	if parseProblems != nil {
		return parseProblems
	}

	var problems []Problem
	problems = schemaVersionProblems(problems, fields, got)
	problems = notesProblems(problems, fields, got)
	return verdictValueProblems(problems, reviewedByLines, reviewedBy, fields, got, opts)
}

// decodeVerdict turns the trailer text into its fields, or into the one
// refusal that says why it could not.
//
// This is one `encoding/json` pass. The shell validator ran a jq program and a
// hand-mirrored Python fallback and had to keep the two byte-identical, plus
// text pre-guards that existed only to paper over the places the two disagreed
// about numbers. Go has one parser, so the guards that arbitrated between two
// are gone; see the package tests for the payloads each used to cover.
func decodeVerdict(raw, got string) (map[string]json.RawMessage, []Problem) {
	refuse := func(headline, detail string) []Problem {
		lines := []string{headline}
		if detail != "" {
			lines = append(lines, detail)
		}
		return []Problem{{Check: CheckVerdictJSON, Lines: append(lines, got)}}
	}

	// A leading byte-order mark. encoding/json refuses it on its own, with a
	// message naming a character the author cannot see. This guard is kept for
	// the wording alone: a refusal that misnames the cause sends the author to
	// the wrong part of their trailer.
	if strings.HasPrefix(strings.TrimLeft(raw, " \t\r\n"), byteOrderMark) {
		return nil, refuse(
			"Review-Verdict trailer begins with a byte-order mark.",
			"  Every reader has to agree on what this trailer says, and they do not agree on this.",
		)
	}

	// Read the whole trailer, not the first document on it. An APPROVE followed
	// by a BLOCK used to exit 0 with the BLOCK read by nothing, and the tail is
	// the half a human reader sees first.
	dec := json.NewDecoder(strings.NewReader(raw))
	var docs []json.RawMessage
	for {
		var doc json.RawMessage
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, refuse(
				"Review-Verdict trailer is not valid JSON.",
				"  Parse error: "+err.Error(),
			)
		}
		docs = append(docs, doc)
	}

	switch {
	case len(docs) == 0:
		return nil, refuse("the Review-Verdict trailer carries no JSON at all", "")
	case len(docs) > 1:
		return nil, refuse(
			"the Review-Verdict trailer holds more than one JSON document; only the first would be read", "")
	}

	// A bare word, a number or an array carries no schema_version, no verdict
	// and no notes, so every field check below would read the same empty value
	// and say nothing useful. Refuse it once, by shape.
	body := bytes.TrimSpace(docs[0])
	if len(body) == 0 || body[0] != '{' {
		return nil, refuse("Review-Verdict is valid JSON but not a JSON object", "")
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, refuse(
			"Review-Verdict trailer is not valid JSON.",
			"  Parse error: "+err.Error(),
		)
	}
	return fields, nil
}

// renderValue is the text a JSON value reads as in a refusal: a string is
// itself, and anything else is its compact JSON spelling.
func renderValue(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err == nil {
			return s
		}
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, trimmed); err != nil {
		return string(trimmed)
	}
	return buf.String()
}

// freeText marks the two ways a rendered value cannot be read as written.
func freeText(s string) string {
	switch {
	case s == "":
		return sentinelEmpty
	case hasControlRune(s):
		return sentinelControl
	}
	return s
}

func hasControlRune(s string) bool {
	for _, r := range s {
		if r < 32 || r == 127 {
			return true
		}
	}
	return false
}

// schemaVersionProblems holds `schema_version` to exactly 1.
//
// The version may be written as the number 1 or as the string "1"; both say the
// same thing and both are accepted. `1.0`, `1e0` and `1e3` are not the integer
// 1 and are refused by name — encoding/json renders each as what the author
// wrote, so the refusal quotes it back.
func schemaVersionProblems(problems []Problem, fields map[string]json.RawMessage, got string) []Problem {
	raw, present := fields["schema_version"]
	value := sentinelAbsent
	if present {
		value = freeText(renderValue(raw))
	}

	switch {
	case value == sentinelControl:
		return append(problems, Problem{Check: CheckSchemaVersion, Lines: []string{
			"Review-Verdict 'schema_version' holds a control character.",
			"  A schema version is a number. A control byte in it is not read the same",
			"  way by every reader, so it is refused rather than guessed at.",
			got,
		}})
	case value != "1":
		return append(problems, Problem{Check: CheckSchemaVersion, Lines: []string{
			"Review-Verdict JSON missing or wrong 'schema_version' (expected 1, got '" + value + "').",
			got,
		}})
	}
	return problems
}

// notesProblems requires `notes`, as a string, saying something.
//
// It is a required field of schema v1 in every statement of it — the
// agent-reviewer skill, the config-reviewer skill, and the Go reader in
// internal/workspace, which rejects a verdict file with absent or empty notes.
// Requiring the key alone would be satisfied by "notes": "".
func notesProblems(problems []Problem, fields map[string]json.RawMessage, got string) []Problem {
	raw, present := fields["notes"]
	if !present {
		return append(problems, Problem{Check: CheckNotes, Lines: []string{
			"Review-Verdict JSON is missing the required 'notes' field.",
			"  schema v1 requires: schema_version, verdict, notes.",
			got,
		}})
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '"' {
		return append(problems, Problem{Check: CheckNotes, Lines: []string{
			"Review-Verdict JSON field 'notes' must be a string.",
			got,
		}})
	}
	var notes string
	if err := json.Unmarshal(trimmed, &notes); err != nil {
		return append(problems, Problem{Check: CheckNotes, Lines: []string{
			"Review-Verdict JSON field 'notes' must be a string.",
			got,
		}})
	}
	if strings.TrimSpace(notes) == "" {
		return append(problems, Problem{Check: CheckNotes, Lines: []string{
			"Review-Verdict JSON has an empty 'notes' field.",
			"  notes carries the reason for the verdict; it must say something.",
			got,
		}})
	}
	return problems
}

// flagsState reports how the `flags` key reads: absent, an array (a JSON null
// counts, since it marshals back as no flags), or something else.
func flagsState(fields map[string]json.RawMessage) string {
	raw, present := fields["flags"]
	if !present {
		return "absent"
	}
	trimmed := bytes.TrimSpace(raw)
	if string(trimmed) == "null" || (len(trimmed) > 0 && trimmed[0] == '[') {
		return "array"
	}
	return "notarray"
}

// verdictValueProblems reads the verdict word and applies the bar that word
// carries.
func verdictValueProblems(
	problems []Problem,
	reviewedByLines []string,
	reviewedBy string,
	fields map[string]json.RawMessage,
	got string,
	opts Options,
) []Problem {
	raw, present := fields["verdict"]
	value := sentinelAbsent
	if present {
		if string(bytes.TrimSpace(raw)) == "null" {
			value = sentinelNull
		} else {
			value = freeText(renderValue(raw))
		}
	}

	switch value {
	case sentinelAbsent:
		return append(problems, Problem{Check: CheckVerdictValue, Lines: []string{
			"Review-Verdict JSON is missing the 'verdict' field.",
			got,
		}})
	case sentinelEmpty:
		// The key is present and says nothing. That gets its own refusal rather
		// than reading as "missing", because the author has to be told which of
		// the two they wrote.
		return append(problems, Problem{Check: CheckVerdictValue, Lines: []string{
			"Review-Verdict 'verdict' is an empty string.",
			"  Write the verdict the reviewer gave.",
			`  If no reviewer was reached, say so: "verdict": "NOT_REVIEWED".`,
			got,
		}})
	case sentinelControl:
		return append(problems, Problem{Check: CheckVerdictValue, Lines: []string{
			"Review-Verdict 'verdict' holds a control character.",
			"  A verdict is a plain word. A control byte in it is not read the same",
			"  way by every reader, so it is refused rather than guessed at.",
			got,
		}})
	case sentinelNull:
		// A null verdict is the honest intent written in a shape that cannot be
		// told apart from a trailer that was cut off just after `"verdict":`.
		// Say the absence in words instead.
		return append(problems, Problem{Check: CheckVerdictValue, Lines: []string{
			"Review-Verdict 'verdict' is null.",
			"  A null verdict cannot be told apart from a truncated trailer.",
			`  If no reviewer was reached, say so: "verdict": "NOT_REVIEWED".`,
			got,
		}})
	}

	switch value {
	case VerdictApprove, VerdictClean:
		// An approval is held to more than the rest, because it is the only
		// verdict that claims the change was found good.
		problems = approvalIdentity(problems, value, reviewedBy, opts)
		return approvalFlagsProblems(problems, value, fields, got)

	case VerdictNotReviewed:
		// This verdict lands, and it is meant to. The one thing it may not do
		// is name a reviewer on the line above it.
		return noReviewerNamed(problems, reviewedByLines, opts)

	case VerdictRequestChanges, VerdictDriftMinor, VerdictDriftMajor:
		// These land, and they may name a reviewer this repo does not ship:
		// with a REQUEST_CHANGES or a DRIFT verdict, `Reviewed-By: <anyone>` is
		// a true statement about who declined. One thing is still checked, and
		// the verdict value cannot excuse it — the author may not be the
		// reviewer.
		problems, _ = notSelfAuthored(problems, value, reviewedBy)
		return problems

	case VerdictBlock:
		return append(problems, Problem{Check: CheckBlockNeverCommitted, Lines: []string{
			"BLOCK verdict must not be committed (fix first).",
			"  Review-Verdict: " + strings.TrimSpace(strings.TrimPrefix(got, "  Got:")),
		}})

	default:
		return append(problems, Problem{Check: CheckVerdictValue, Lines: []string{
			"unknown verdict value '" + value + "'.",
			"  Allowed (agent-reviewer): APPROVE, REQUEST_CHANGES",
			"  Allowed (config-reviewer): CLEAN, DRIFT_MINOR, DRIFT_MAJOR",
			"  Allowed when no reviewer was reached: NOT_REVIEWED",
			"  BLOCK = fix before committing, never in a commit.",
			got,
		}})
	}
}

// approvalFlagsProblems requires the `flags` key an approval's reviewer skill
// always emits. It is demanded of an approval and NOT of the honest verdicts,
// on purpose: every rule that makes the truthful trailer more expensive to
// write pushes the author back toward APPROVE.
func approvalFlagsProblems(problems []Problem, verdict string, fields map[string]json.RawMessage, got string) []Problem {
	switch flagsState(fields) {
	case "absent":
		return append(problems, Problem{Check: CheckApprovalFlags, Lines: []string{
			"the " + verdict + " verdict must carry the 'flags' key that the reviewer skill emits.",
			`  Use "flags": [] when the reviewer raised nothing.`,
			got,
		}})
	case "notarray":
		return append(problems, Problem{Check: CheckApprovalFlags, Lines: []string{
			"Review-Verdict JSON field 'flags' must be an array.",
			got,
		}})
	}
	return problems
}
