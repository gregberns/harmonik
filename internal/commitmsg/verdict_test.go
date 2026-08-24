package commitmsg

// The adversarial verdict payloads.
//
// These are the payloads the shell validator's parser-equivalence battery drove
// at jq and at its Python fallback, plus the truncated document it compared on
// exit status alone. That battery existed because the shell validator had TWO
// parsers that had to be kept byte-identical by hand, and eight of these
// payloads are ones they disagreed about. Go has one parser, so the battery's
// subject is gone — but the payloads are not, and each still names a way a
// trailer can lie.
//
// WHICH GUARDS THE SHELL NEEDED AND THIS PACKAGE DOES NOT. The number-literal
// whitelist and its leading-zero rule existed only because jq reads 1e0, 01, +1
// and nan and renders each as the integer 1, so a jq-side APPROVE went green on
// all four. encoding/json refuses 01, +1 and nan outright as syntax errors, and
// renders 1e0, 1.0 and 1e3 as what the author wrote, so none can reach an
// accepted APPROVE by any route. Those guards are dropped. The refusals below
// say so: where the shell named a number literal, this package names a wrong
// schema_version or an unparseable document.
//
// The byte-order-mark guard is KEPT, for its wording alone: encoding/json
// refuses a leading mark on its own, with a message naming a character the
// author cannot see.
//
// The control-byte refusals are KEPT, and their reason has changed. In the
// shell they were load-bearing — the parser handed four fields back as one
// tab-separated record, so a raw control byte in a free-text field moved the
// column boundaries and handed a later check a value the JSON never said.
// There is no record here and nothing to shift: a NUL inside "APPROVE" simply
// is not the word APPROVE and falls to the unknown-verdict arm anyway. What
// the guard still buys is a legible refusal — the alternative prints raw
// control bytes at the author's terminal — and one that names a class rather
// than reading like a typo.

// bomLiteral is U+FEFF, written as an escape so the next reader can see it and
// a future edit cannot delete it by accident.
const bomLiteral = "\ufeff"

func verdictPayloadCases() []validateCase {
	return []validateCase{{
		name: "a tab inside schema_version cannot hide a BLOCK verdict",
		msg:  payloadMsg(`{"schema_version": "1\tAPPROVE\tok\tarray\n", "verdict": "BLOCK", "notes": "Fix the data race.", "flags": ["idiom-violation"]}`),
		want: []Check{CheckSchemaVersion, CheckBlockNeverCommitted},
		text: "BLOCK verdict must not be committed",
	}, {
		name: "a tab inside the verdict word is refused as a control byte",
		msg:  payloadMsg(`{"schema_version": 1, "verdict": "APPROVE\tok\tarray", "notes": "n", "flags": []}`),
		want: []Check{CheckVerdictValue},
		text: "'verdict' holds a control character",
	}, {
		name: "a newline inside the verdict word is refused as a control byte",
		msg:  payloadMsg(`{"schema_version": 1, "verdict": "APPROVE\nok\narray", "notes": "n", "flags": []}`),
		want: []Check{CheckVerdictValue},
	}, {
		name: "a carriage return inside the verdict word is refused as a control byte",
		msg:  payloadMsg(`{"schema_version": 1, "verdict": "APPROVE\rok\rarray", "notes": "n", "flags": []}`),
		want: []Check{CheckVerdictValue},
	}, {
		// An ESCAPED backslash is ordinary text, not a control byte, so this one
		// falls to the enum arm. It is the control that tells a rule about
		// control BYTES from a rule about backslashes.
		name: "an escaped backslash in the verdict word is ordinary text and fails the enum",
		msg:  payloadMsg(`{"schema_version": 1, "verdict": "APPROVE\\tok", "notes": "n", "flags": []}`),
		want: []Check{CheckVerdictValue},
		text: "unknown verdict value",
	}, {
		name: "a backslash beside a real control byte is still refused as a control byte",
		msg:  payloadMsg(`{"schema_version": 1, "verdict": "a\\\tb\\\nc", "notes": "n", "flags": []}`),
		want: []Check{CheckVerdictValue},
		text: "holds a control character",
	}, {
		name: "a bare tab as the schema_version is refused as a control byte",
		msg:  payloadMsg(`{"schema_version": "\t", "verdict": "APPROVE", "notes": "n", "flags": []}`),
		want: []Check{CheckSchemaVersion},
		text: "'schema_version' holds a control character",
	}, {
		// The version may be written as the number 1 or as the string "1". Both
		// say the same thing, so the refusal here is the empty verdict alone.
		name: "the schema version may be the string 1, so only the empty verdict is refused",
		msg:  payloadMsg(`{"schema_version": "1", "verdict": "", "notes": "n", "flags": []}`),
		want: []Check{CheckVerdictValue},
		text: "'verdict' is an empty string",
	}, {
		name: "an empty-string schema_version is refused, and named as empty",
		msg:  payloadMsg(`{"schema_version": "", "verdict": "APPROVE", "notes": "n", "flags": []}`),
		want: []Check{CheckSchemaVersion},
		text: "expected 1, got '__EMPTY__'",
	}, {
		// One missing key must produce ONE complaint. The shell read its four
		// fields out of a tab-separated record, and an empty first column
		// vanished rather than reading back empty, so every field shifted one
		// place left and the validator named three fields that were all fine.
		name: "an absent schema_version is named as absent and shifts nothing",
		msg:  payloadMsg(`{"verdict": "APPROVE", "notes": "A real review.", "flags": []}`),
		want: []Check{CheckSchemaVersion},
		text: "expected 1, got '__ABSENT__'",
	}, {
		name: "an absent verdict is refused as missing",
		msg:  payloadMsg(`{"schema_version": 1, "notes": "n", "flags": []}`),
		want: []Check{CheckVerdictValue},
		text: "missing the 'verdict' field",
	}, {
		// A null verdict is the honest intent written in a shape that cannot be
		// told apart from a trailer cut off just after "verdict":.
		name: "a null verdict is refused as null, not as an unknown enum value",
		msg:  payloadMsg(`{"schema_version": 1, "verdict": null, "notes": "n", "flags": []}`),
		want: []Check{CheckVerdictValue},
		text: "Review-Verdict 'verdict' is null",
	}, {
		name: "a null schema_version is quoted back as null",
		msg:  payloadMsg(`{"schema_version": null, "verdict": "APPROVE", "notes": "n", "flags": []}`),
		want: []Check{CheckSchemaVersion},
		text: "expected 1, got 'null'",
	}, {
		name: "boolean fields are refused on their own terms",
		msg:  payloadMsg(`{"schema_version": true, "verdict": false, "notes": "n", "flags": []}`),
		want: []Check{CheckSchemaVersion, CheckVerdictValue},
		text: "unknown verdict value 'false'",
	}, {
		name: "structured fields are quoted back in their compact spelling",
		msg:  payloadMsg(`{"schema_version": {"a":1}, "verdict": [1,2], "notes": "n", "flags": []}`),
		want: []Check{CheckSchemaVersion, CheckVerdictValue},
		text: "unknown verdict value '[1,2]'",
	}, {
		name: "a numeric verdict fails the enum",
		msg:  payloadMsg(`{"schema_version": 1, "verdict": 1, "notes": "n", "flags": []}`),
		want: []Check{CheckVerdictValue},
	}, {
		name: "an accented verdict word fails the enum",
		msg:  payloadMsg(`{"schema_version": 1, "verdict": "APPROV\u00c9\u00e9", "notes": "n", "flags": []}`),
		want: []Check{CheckVerdictValue},
	}, {
		// Whitespace is not the empty string, so this is an unknown word rather
		// than an absent value, and the author is told which.
		name: "a whitespace-only verdict fails the enum rather than reading as empty",
		msg:  payloadMsg(`{"schema_version": 1, "verdict": "   ", "notes": "n", "flags": []}`),
		want: []Check{CheckVerdictValue},
		text: "unknown verdict value",
	}, {
		name: "a padded schema version string is not the version 1",
		msg:  payloadMsg(`{"schema_version": " 1 ", "verdict": "APPROVE", "notes": "n", "flags": []}`),
		want: []Check{CheckSchemaVersion},
		text: "expected 1, got ' 1 '",
	}, {
		// notes is FREE TEXT. Tabs, newlines and backslashes in it are the
		// reviewer's prose and decide nothing.
		name: "control characters inside notes are prose and are accepted",
		msg:  payloadMsg(`{"schema_version": 1, "verdict": "APPROVE", "notes": "a\tb\nc\\d", "flags": []}`),
	}, {
		name: "a flags value that is not an array is refused",
		msg:  payloadMsg(`{"schema_version": 1, "verdict": "APPROVE", "notes": "n", "flags": "oops"}`),
		want: []Check{CheckApprovalFlags},
		text: "'flags' must be an array",
	}, {
		// A null flags marshals back as no flags, so it counts as the key being
		// there. A null notes does not: notes has to be a string that says
		// something.
		name: "null notes is refused by type while null flags counts as present",
		msg:  payloadMsg(`{"schema_version": 1, "verdict": "APPROVE", "notes": null, "flags": null}`),
		want: []Check{CheckNotes},
		text: "'notes' must be a string",
	}, {
		name: "a JSON array where the verdict object belongs is refused by shape",
		msg:  payloadMsg(`[1,2]`),
		want: []Check{CheckVerdictJSON},
		text: "valid JSON but not a JSON object",
	}, {
		name: "a JSON string where the verdict object belongs is refused by shape",
		msg:  payloadMsg(`"APPROVE"`),
		want: []Check{CheckVerdictJSON},
		text: "valid JSON but not a JSON object",
	}, {
		name: "a JSON null where the verdict object belongs is refused by shape",
		msg:  payloadMsg(`null`),
		want: []Check{CheckVerdictJSON},
		text: "valid JSON but not a JSON object",
	}, {
		// 1.0, 1e0 and 1e3 are valid JSON and are not the integer 1. The shell
		// needed a text whitelist to say so because jq could not tell 1e0 from
		// 1; here the literal is quoted straight back at the author.
		name: "a schema_version of 1.0 is not the integer 1",
		msg:  payloadMsg(`{"schema_version": 1.0, "verdict": "APPROVE", "notes": "n", "flags": []}`),
		want: []Check{CheckSchemaVersion},
		text: "expected 1, got '1.0'",
	}, {
		name: "a schema_version of 1e0 is not the integer 1",
		msg:  payloadMsg(`{"schema_version": 1e0, "verdict": "APPROVE", "notes": "n", "flags": []}`),
		want: []Check{CheckSchemaVersion},
		text: "expected 1, got '1e0'",
	}, {
		name: "a schema_version of 1e3 is not the integer 1",
		msg:  payloadMsg(`{"schema_version": 1e3, "verdict": "APPROVE", "notes": "n", "flags": []}`),
		want: []Check{CheckSchemaVersion},
		text: "expected 1, got '1e3'",
	}, {
		name: "a schema_version too large for an integer is still not 1",
		msg:  payloadMsg(`{"schema_version": 100000000000000000000, "verdict": "APPROVE", "notes": "n", "flags": []}`),
		want: []Check{CheckSchemaVersion},
	}, {
		// RFC 8259 has no leading zero, no leading plus and no nan. jq read all
		// three and rendered each as the integer 1, so all three were accepted
		// APPROVEs on the jq path. encoding/json refuses them as syntax errors,
		// which is why the whitelist that used to catch them is gone.
		name: "a leading zero is not JSON and the document does not parse",
		msg:  payloadMsg(`{"schema_version": 01, "verdict": "APPROVE", "notes": "n", "flags": []}`),
		want: []Check{CheckVerdictJSON},
		text: "not valid JSON",
	}, {
		name: "a leading plus is not JSON and the document does not parse",
		msg:  payloadMsg(`{"schema_version": +1, "verdict": "APPROVE", "notes": "n", "flags": []}`),
		want: []Check{CheckVerdictJSON},
		text: "not valid JSON",
	}, {
		name: "nan is not JSON and the document does not parse",
		msg:  payloadMsg(`{"schema_version": nan, "verdict": "APPROVE", "notes": "n", "flags": []}`),
		want: []Check{CheckVerdictJSON},
		text: "not valid JSON",
	}, {
		// The control that keeps the version rule from becoming a rule about the
		// digit zero. A number that merely is not 1 is refused as a version, in
		// words that say so.
		name: "a schema_version of 100 is refused as a version, not as a literal",
		msg:  payloadMsg(`{"schema_version": 100, "verdict": "APPROVE", "notes": "n", "flags": []}`),
		want: []Check{CheckSchemaVersion},
		text: "expected 1, got '100'",
	}, {
		name: "a NUL in the verdict and in the schema version is refused in both places",
		msg:  payloadMsg(`{"schema_version":"1\u0000","verdict":"APPROVE\u0000","notes":"n","flags":[]}`),
		want: []Check{CheckSchemaVersion, CheckVerdictValue},
		text: "'verdict' holds a control character",
	}, {
		// The class, not the member. A NUL is the one control byte a shell
		// drops, so a fix that added NUL to an escape list would pass the case
		// above and leave every other control byte reaching the record.
		name: "a control byte other than NUL is refused too",
		msg:  payloadMsg(`{"schema_version":1,"verdict":"APPROVE\u0001","notes":"n","flags":[]}`),
		want: []Check{CheckVerdictValue},
		text: "'verdict' holds a control character",
	}, {
		name: "a leading byte-order mark is refused, and named",
		msg:  payloadMsg(bomLiteral + `{"schema_version": 1, "verdict": "APPROVE", "notes": "n", "flags": []}`),
		want: []Check{CheckVerdictJSON},
		text: "begins with a byte-order mark",
	}, {
		// Only a LEADING mark is refused. A U+FEFF inside the notes string is
		// ordinary text, and refusing it would invent a rule this repo does not
		// have.
		name: "a byte-order mark inside notes is ordinary text",
		msg:  payloadMsg(`{"schema_version": 1, "verdict": "APPROVE", "notes": "a` + bomLiteral + `b", "flags": []}`),
	}, {
		// Two documents on one trailer line. Reading only the first meant an
		// APPROVE followed by a BLOCK exited 0 with the BLOCK read by nothing,
		// and the tail is the half a human reader sees first.
		name: "two JSON documents on the trailer line are refused",
		msg:  payloadMsg(`{"schema_version": 1, "verdict": "APPROVE", "notes": "n", "flags": []} {"schema_version": 1, "verdict": "BLOCK", "notes": "n", "flags": []}`),
		want: []Check{CheckVerdictJSON},
		text: "more than one JSON document",
	}, {
		name: "a truncated JSON trailer is refused",
		msg:  payloadMsg(`{"schema_version": 1, "verdict": "APPROVE", "notes": "n", "flags": [] `),
		want: []Check{CheckVerdictJSON},
		text: "not valid JSON",
	}, {
		name: "a Review-Verdict key with nothing after it is named for carrying no JSON",
		msg: "feat(cli): add the promote subcommand\n\n" +
			"Reviewed-By: agent-reviewer\n" +
			"Review-Verdict:",
		want: []Check{CheckVerdictJSON},
		text: "carries no JSON at all",
	}, {
		name: "a Review-Verdict key with only whitespace after it carries no JSON either",
		msg: "feat(cli): add the promote subcommand\n\n" +
			"Reviewed-By: agent-reviewer\n" +
			"Review-Verdict:   ",
		want: []Check{CheckVerdictJSON},
		text: "carries no JSON at all",
	}, {
		name: "a verdict written with no space after the trailer key still parses",
		msg: "feat(cli): add the promote subcommand\n\n" +
			"Reviewed-By: agent-reviewer\n" +
			`Review-Verdict:{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"A real review."}`,
	}, {
		name: "a missing notes field is refused",
		msg: "feat(cli): add the promote subcommand\n\n" +
			"Reviewed-By: agent-reviewer\n" +
			`Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[]}`,
		want: []Check{CheckNotes},
		text: "missing the required 'notes' field",
	}, {
		name: "a whitespace-only notes field is refused",
		msg: "feat(cli): add the promote subcommand\n\n" +
			"Reviewed-By: agent-reviewer\n" +
			`Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"   "}`,
		want: []Check{CheckNotes},
		text: "empty 'notes' field",
	}, {
		name: "a schema_version of 2 is refused",
		msg: "feat(queue): chain the claim through\n\n" +
			"Reviewed-By: agent-reviewer\n" +
			`Review-Verdict: {"schema_version":2,"verdict":"APPROVE","flags":[],"notes":"A real review."}`,
		want: []Check{CheckSchemaVersion},
		text: "missing or wrong 'schema_version'",
	}, {
		name: "a schema_version that is not a number at all is refused",
		msg: "feat(queue): chain the claim through\n\n" +
			"Reviewed-By: agent-reviewer\n" +
			`Review-Verdict: {"schema_version":"banana","verdict":"APPROVE","flags":[],"notes":"A real review."}`,
		want: []Check{CheckSchemaVersion},
		text: "expected 1, got 'banana'",
	}, {
		// The control for the number rules. Without it a validator that refused
		// every schema_version would keep all of them green.
		name: "a schema_version of 1 is accepted",
		msg: "feat(queue): chain the claim through\n\n" +
			"Reviewed-By: agent-reviewer\n" +
			`Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"A real review."}`,
	}, {
		// The control for the byte-order-mark and number guards together: they
		// read raw text, so a validator that read the STRINGS too would refuse
		// every notes value containing a full stop, which is every real one.
		name: "ordinary prose in notes is not read as a number",
		msg: "fix(gate): ordinary prose in the notes field\n\n" +
			"Reviewed-By: agent-reviewer\n" +
			`Review-Verdict: {"schema_version": 1, "verdict": "APPROVE", "notes": "Checked the queue path. Version 1.0 of the spec agrees. E.g. this sentence.", "flags": []}`,
	}}
}
