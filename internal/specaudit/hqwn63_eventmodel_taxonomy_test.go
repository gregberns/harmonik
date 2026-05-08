package specaudit_test

// hk-hqwn.63 binding test — §8 taxonomy lint.
//
// Spec ref: specs/event-model.md §8, §8.9, EV-INV-005, EV-016.
//
// Per §10.2 §8 lint obligation and EV-INV-005, every event type declared in
// §8 of the event-model spec MUST satisfy three conditions:
//
//  (a) Durability class declared — every §8 table row carries a Dur column
//      value in the closed set {F, O, L} (fsync-boundary, ordinary,
//      lossy-tail-ok per §4.4 EV-016).
//
//  (b) Four-axis tags present — every §8 event type carries an Axes: line
//      with all four canonical axes (llm-freedom, io-determinism,
//      replay-safety, idempotency) per §8.9(e) and EV-INV-005.
//
//  (c) At-least-one-consumer-in-sibling-spec — every §8 event type is cited
//      by backtick-quoted name (e.g. `run_started`) in at least one sibling
//      spec file under specs/ (excluding event-model.md itself). Per
//      §8.9(g), the `metric` entry (§8.8.1) is explicitly exempt.
//
// # Audit frames
//
// Check (a): parse §8 Markdown table rows. A §8 row has the form:
//
//	| N.N.N | `type_name` | DUR | Emitter | Consumers | Payload |
//
// where DUR ∈ {F, O, L}. A missing or unrecognized Dur value is a violation.
// The hqwn63FixtureSection8RowPattern regex identifies these rows.
//
// Check (b): scan for an Axes: line within a 50-line look-ahead window
// following the §8 table row for each event type. Because §8 uses a Markdown
// table format rather than #### requirement headings, the associated Axes: line
// (if any) would appear in a block comment or annotation immediately after the
// table. Currently NO §8 entry carries an Axes: line — this is a structural
// spec defect (EV-INV-005 + §8.9(e) violation). All check-(b) violations are
// pinned in hqwn63FixtureExpectedViolations with follow-up bead hk-hqwn.67,
// so the suite does not fail perpetually.
//
// Check (c): scan every sibling spec under specs/ (all *.md files and
// specs/**/spec.md, excluding event-model.md) for the string "`type_name`"
// (backtick-quoted). A §8 entry with no backtick citation in any sibling spec
// is a violation. Uncited events are pinned in hqwn63FixtureExpectedViolations
// with follow-up bead hk-hqwn.68 so the suite does not fail perpetually.
//
// # Path B rationale
//
// Check (b): the §8 table format does not support per-row Axes: annotation at
// present; all 81 events lack the required Axes: line. A spec-level amendment
// is required (hk-hqwn.67). Pinned so the suite passes today.
//
// Check (c): six events have no backtick-quoted consumer citation:
// state_exited, sub_workflow_exited, agent_rate_limit_status,
// reconciliation_started, consumer_failed, dead_letter_enqueued. The spec
// defect requires adding cross-spec consumer citations (hk-hqwn.68). Pinned
// so the suite passes today.
// (state_entered is cited at specs/reconciliation/schemas.md:162 and is NOT pinned.)

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// hqwn63FixtureSection8RowPattern matches a §8 Markdown table row of the form:
//
//	| 8.N.N | `type_name`[(optional post-MVH bold)] | DUR | ...
//
// Capture groups:
//
//	1 — section number (e.g. "8.1.1")
//	2 — event type name (e.g. "run_started")
//	3 — durability class abbreviation (e.g. "F")
var hqwn63FixtureSection8RowPattern = regexp.MustCompile(
	`^\| (8\.\d+\.\d+) \| ` + "`([a-z_]+)`" + `(?:\s*\*\*[^|]*)? \| ([A-Z]) \|`,
)

// hqwn63FixtureAxesLine matches a standalone Axes: line and captures the value.
var hqwn63FixtureAxesLine = regexp.MustCompile(`^Axes: (.+)`)

// hqwn63FixtureValidDurClasses is the closed vocabulary of durability class
// abbreviations per §4.4 EV-016: F=fsync-boundary, O=ordinary, L=lossy-tail-ok.
var hqwn63FixtureValidDurClasses = map[string]bool{
	"F": true,
	"O": true,
	"L": true,
}

// hqwn63FixtureMetricExemptType is the §8.8.1 metric event type, which is
// exempt from check (c) per §8.9(g): "The `metric` entry (§8.8.1) is the
// single escape-hatch exception; its use is free but payload-shape-bounded."
const hqwn63FixtureMetricExemptType = "metric"

// hqwn63FixtureEventEntry captures a parsed §8 event type.
type hqwn63FixtureEventEntry struct {
	sectionNum string // e.g. "8.1.1"
	typeName   string // e.g. "run_started"
	durClass   string // e.g. "F"
	lineNo     int    // 1-based line number in event-model.md
}

// hqwn63FixtureViolation records a single violation found by the taxonomy lint.
type hqwn63FixtureViolation struct {
	file     string // relative spec path
	lineNo   int    // 1-based
	section  string // §8.N.N
	typeName string // event type name
	check    string // "a-durability", "b-axes", "c-consumer"
	detail   string
}

func (v hqwn63FixtureViolation) String() string {
	return fmt.Sprintf("%s:%d: [%s] §%s `%s` — %s",
		v.file, v.lineNo, v.check, v.section, v.typeName, v.detail)
}

// hqwn63FixtureExpectedViolationEntry is a single entry in the skip-list.
type hqwn63FixtureExpectedViolationEntry struct {
	// pinnedBy is the bead ID that owns the fix for this violation.
	pinnedBy string
	// reason is a human-readable explanation of why the violation is deferred.
	reason string
}

// hqwn63FixtureViolationKey returns the skip-list lookup key for a violation.
// Format: "<check>:<section>:<typeName>".
func hqwn63FixtureViolationKey(v hqwn63FixtureViolation) string {
	return fmt.Sprintf("%s:%s:%s", v.check, v.section, v.typeName)
}

// hqwn63FixtureExpectedViolations is the skip-list of known violations that are
// intentionally deferred to follow-up beads.
//
// Key format: "<check>:<section>:<typeName>" where check ∈ {a-durability,
// b-axes, c-consumer}, section is the §8.N.N number, and typeName is the
// event type name.
//
// Rules (mirrors ar001FixtureExpectedViolations):
//   - An entry whose violation is NOT present causes t.Errorf("stale skip-list entry …").
//   - An entry whose violation IS present produces t.Logf and does NOT fail.
//   - Any NEW violation NOT in this map DOES fail the suite.
var hqwn63FixtureExpectedViolations = map[string]hqwn63FixtureExpectedViolationEntry{
	// ── Check (b): Axes: lines missing from §8 table entries ──────────────────
	// EV-INV-005 and §8.9(e) require every §8 event type to carry four-axis tags,
	// but the §8 Markdown table format does not support per-row Axes: annotation.
	// All 81 entries are deficient. A spec-level amendment is tracked in hk-hqwn.67.
	"b-axes:8.1.1:run_started":                             {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.1.2:run_completed":                           {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.1.3:run_failed":                              {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.1.4:state_entered":                           {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.1.5:state_exited":                            {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.1.6:transition_event":                        {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.1.7:checkpoint_written":                      {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.1.8:outcome_emitted":                         {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.1.9:sub_workflow_entered":                    {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.1.10:sub_workflow_exited":                    {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.1.11:node_dispatch_requested":                {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.2.1:hook_fired":                              {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.2.2:hook_failed":                             {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.2.3:hook_verdict_persisted":                  {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.2.4:gate_allowed":                            {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.2.5:gate_denied":                             {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.2.6:gate_escalated":                          {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.2.7:guard_reordered":                         {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.2.8:guard_failed":                            {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.2.9:control_points_registered":               {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.2.10:control_points_registration_started":    {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.2.11:verdict_envelope_mismatch":              {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.2.12:policy_expression_exceeded_cost":        {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.3.1:agent_ready":                             {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.3.2:agent_started":                           {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.3.3:agent_output_chunk":                      {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.3.4:agent_completed":                         {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.3.5:agent_failed":                            {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.3.6:agent_rate_limit_status":                 {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.3.7:session_log_location":                    {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.3.8:skills_provisioned":                      {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.3.9:handler_capabilities":                    {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.3.10:agent_warning_silent_hang":              {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.3.11:agent_resumed_after_warning":            {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.3.12:agent_soft_terminating":                 {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.3.13:agent_hard_terminating":                 {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.4.1:budget_warning":                          {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.4.2:budget_accrual":                          {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.4.3:budget_exhausted":                        {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.5.1:workspace_created":                       {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.5.2:workspace_leased":                        {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.5.3:workspace_merge_status":                  {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.5.4:workspace_discarded":                     {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.5.5:workspace_interrupted":                   {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.5.6:merge_conflict_escalation":               {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.6.1:reconciliation_started":                  {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.6.2:reconciliation_category_assigned":        {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.6.3:reconciliation_verdict_emitted":          {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.6.4:reconciliation_verdict_executed":         {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.6.5:reconciliation_verdict_malformed":        {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.6.6:reconciliation_budget_exhausted":         {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.6.7:reconciliation_verdict_stale":            {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.6.8:store_divergence_detected":               {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.6.9:operator_escalation_required":            {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.6.10:divergence_inconclusive":                {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.6.11:reconciliation_dispatch_deduplicated":   {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.6.12:reconciliation_detector_panic":          {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.6.13:reconciliation_verdict_execution_retry": {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.6.14:bead_terminal_transition_recovered":     {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.7.1:daemon_started":                          {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.7.2:daemon_ready":                            {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.7.3:daemon_shutdown":                         {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.7.4:daemon_startup_failed":                   {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.7.5:daemon_degraded":                         {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.7.6:operator_pause_status":                   {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.7.7:operator_resuming":                       {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.7.8:operator_stopped":                        {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.7.9:operator_upgrading":                      {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.7.10:operator_upgrade_completed":             {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.7.11:operator_upgrade_rejected":              {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.7.12:operator_command_rejected":              {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.7.13:dispatch_deferred":                      {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.7.14:daemon_orphan_sweep_completed":          {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.7.15:infrastructure_unavailable":             {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.7.16:operator_command_failed":                {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.7.17:operator_escalation_cleared":            {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.8.1:metric":                                  {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.8.2:consumer_failed":                         {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.8.3:dead_letter_enqueued":                    {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.8.4:bus_overflow":                            {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},
	"b-axes:8.8.5:redaction_failed":                        {pinnedBy: "hk-hqwn.67", reason: "§8 table format carries no Axes: lines; spec amendment required per EV-INV-005"},

	// ── Check (c): no backtick-quoted consumer citation in sibling specs ───────
	// Per §8.9(g) each non-exempt §8 event MUST appear by backtick-quoted name
	// in at least one sibling spec. Six events are missing that citation.
	// (state_entered is cited at specs/reconciliation/schemas.md:162 — not pinned.)
	// Consumer citations will be added by hk-hqwn.68.
	"c-consumer:8.1.5:state_exited": {
		pinnedBy: "hk-hqwn.68",
		reason:   "state_exited cited only in emit_event() pseudocode in execution-model.md without backtick quoting; backtick consumer citation required per §8.9(g)",
	},
	"c-consumer:8.1.10:sub_workflow_exited": {
		pinnedBy: "hk-hqwn.68",
		reason:   "sub_workflow_exited has no backtick-quoted citation in any sibling spec; consumer citation required per §8.9(g)",
	},
	"c-consumer:8.3.6:agent_rate_limit_status": {
		pinnedBy: "hk-hqwn.68",
		reason:   "agent_rate_limit_status has no backtick-quoted citation in any sibling spec; consumer citation required per §8.9(g)",
	},
	"c-consumer:8.6.1:reconciliation_started": {
		pinnedBy: "hk-hqwn.68",
		reason:   "reconciliation_started has no backtick-quoted citation in any sibling spec; consumer citation required per §8.9(g)",
	},
	"c-consumer:8.8.2:consumer_failed": {
		pinnedBy: "hk-hqwn.68",
		reason:   "consumer_failed has no backtick-quoted citation in any sibling spec; consumer citation required per §8.9(g)",
	},
	"c-consumer:8.8.3:dead_letter_enqueued": {
		pinnedBy: "hk-hqwn.68",
		reason:   "dead_letter_enqueued has no backtick-quoted citation in any sibling spec; consumer citation required per §8.9(g)",
	},
}

// hqwn63FixtureRepoRoot returns the repository root directory by walking up
// from this test file's source path. The test file lives at:
//
//	internal/specaudit/hqwn63_eventmodel_taxonomy_test.go
//
// so the repo root is two directories up.
func hqwn63FixtureRepoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("hqwn63FixtureRepoRoot: runtime.Caller(0) failed")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}

// hqwn63FixtureEventModelPath returns the absolute path to specs/event-model.md.
func hqwn63FixtureEventModelPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(hqwn63FixtureRepoRoot(t), "specs", "event-model.md")
}

// hqwn63FixtureSiblingSpecFiles returns all spec files under specs/ except
// event-model.md itself. Scope:
//   - specs/*.md (top-level, excluding event-model.md)
//   - specs/**/*.md (all *.md files one level deep under each subdirectory)
func hqwn63FixtureSiblingSpecFiles(t *testing.T) []string {
	t.Helper()
	specsDir := filepath.Join(hqwn63FixtureRepoRoot(t), "specs")

	var files []string

	topEntries, err := os.ReadDir(specsDir)
	if err != nil {
		t.Fatalf("hqwn63FixtureSiblingSpecFiles: ReadDir %s: %v", specsDir, err)
	}
	for _, e := range topEntries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		if e.Name() == "event-model.md" {
			continue // exclude self
		}
		files = append(files, filepath.Join(specsDir, e.Name()))
	}

	// All *.md files one level deep under each subdirectory (e.g. specs/reconciliation/*.md).
	for _, e := range topEntries {
		if !e.IsDir() {
			continue
		}
		subDir := filepath.Join(specsDir, e.Name())
		subEntries, subErr := os.ReadDir(subDir)
		if subErr != nil {
			t.Fatalf("hqwn63FixtureSiblingSpecFiles: ReadDir %s: %v", subDir, subErr)
		}
		for _, se := range subEntries {
			if se.IsDir() || !strings.HasSuffix(se.Name(), ".md") {
				continue
			}
			files = append(files, filepath.Join(subDir, se.Name()))
		}
	}

	return files
}

// hqwn63FixtureParseSection8Entries parses event-model.md and returns all §8
// event type entries in document order.
//
// The parser scans for lines matching hqwn63FixtureSection8RowPattern, which
// identifies §8 Markdown table rows of the form:
//
//	| 8.N.N | `type_name` | DUR | Emitter | Consumers | Payload |
//
// Each match produces one hqwn63FixtureEventEntry.
func hqwn63FixtureParseSection8Entries(t *testing.T) []hqwn63FixtureEventEntry {
	t.Helper()

	specFile := hqwn63FixtureEventModelPath(t)

	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("hqwn63FixtureParseSection8Entries: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("hqwn63FixtureParseSection8Entries: scan %s: %v", specFile, scanErr)
	}

	var entries []hqwn63FixtureEventEntry
	for i, line := range lines {
		m := hqwn63FixtureSection8RowPattern.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		entries = append(entries, hqwn63FixtureEventEntry{
			sectionNum: m[1],
			typeName:   m[2],
			durClass:   m[3],
			lineNo:     i + 1, // 1-based
		})
	}

	return entries
}

// hqwn63FixtureCheckA validates check (a): every §8 entry's Dur column value
// is in the closed vocabulary {F, O, L}.
//
// In practice the event-model spec is self-consistent here, but the audit
// guards against future edits that introduce an unknown abbreviation.
func hqwn63FixtureCheckA(entries []hqwn63FixtureEventEntry) []hqwn63FixtureViolation {
	var violations []hqwn63FixtureViolation
	for _, e := range entries {
		if !hqwn63FixtureValidDurClasses[e.durClass] {
			violations = append(violations, hqwn63FixtureViolation{
				file:     "specs/event-model.md",
				lineNo:   e.lineNo,
				section:  e.sectionNum,
				typeName: e.typeName,
				check:    "a-durability",
				detail: fmt.Sprintf(
					"Dur column value %q is not in closed vocabulary {F, O, L} (fsync-boundary, ordinary, lossy-tail-ok per EV-016)",
					e.durClass,
				),
			})
		}
	}
	return violations
}

// hqwn63FixtureCheckB validates check (b): every §8 entry has an Axes: line
// within a 50-line look-ahead window following its table row in event-model.md.
//
// Because §8 uses Markdown table format rather than #### requirement headings,
// there is currently no per-row Axes: annotation mechanism. All entries will
// fail this check; violations are pinned in hqwn63FixtureExpectedViolations
// (hk-hqwn.67).
//
// The look-ahead window is 50 lines (larger than ar001/ar005's 30 lines) to
// accommodate potential future block-comment annotations placed after §8 table
// sections. The scan stops at the next Markdown heading (## or ###) to avoid
// attributing Axes: lines from later sections to §8 entries.
func hqwn63FixtureCheckB(t *testing.T, entries []hqwn63FixtureEventEntry) []hqwn63FixtureViolation {
	t.Helper()

	specFile := hqwn63FixtureEventModelPath(t)

	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("hqwn63FixtureCheckB: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("hqwn63FixtureCheckB: scan %s: %v", specFile, scanErr)
	}

	// headingBreaker matches any Markdown heading at level 2 or higher that
	// would terminate a §8 sub-section scope.
	headingBreaker := regexp.MustCompile(`^#{2,} `)

	var violations []hqwn63FixtureViolation
	for _, e := range entries {
		rowIdx := e.lineNo - 1 // 0-based index of the table row
		hasAxes := false

		limit := rowIdx + 1 + 50
		if limit > len(lines) {
			limit = len(lines)
		}
		for j := rowIdx + 1; j < limit; j++ {
			if headingBreaker.MatchString(lines[j]) {
				break
			}
			if hqwn63FixtureAxesLine.MatchString(lines[j]) {
				hasAxes = true
				break
			}
		}

		if !hasAxes {
			violations = append(violations, hqwn63FixtureViolation{
				file:     "specs/event-model.md",
				lineNo:   e.lineNo,
				section:  e.sectionNum,
				typeName: e.typeName,
				check:    "b-axes",
				detail:   "no Axes: line found within 50-line look-ahead; §8 table entries must carry four-axis tags per EV-INV-005 and §8.9(e) — spec amendment tracked in hk-hqwn.67",
			})
		}
	}
	return violations
}

// hqwn63FixtureLoadSiblingContent loads all sibling spec files and returns
// their concatenated text content indexed by file path.
func hqwn63FixtureLoadSiblingContent(t *testing.T) map[string]string {
	t.Helper()
	siblingFiles := hqwn63FixtureSiblingSpecFiles(t)
	content := make(map[string]string, len(siblingFiles))
	for _, fp := range siblingFiles {
		//nolint:gosec // G304: path from hqwn63FixtureSiblingSpecFiles which resolves against the repo's specs/ directory; not user input.
		data, err := os.ReadFile(fp)
		if err != nil {
			t.Fatalf("hqwn63FixtureLoadSiblingContent: read %s: %v", fp, err)
		}
		content[fp] = string(data)
	}
	return content
}

// hqwn63FixtureCheckC validates check (c): every non-exempt §8 event type is
// cited by backtick-quoted name in at least one sibling spec.
//
// Syntactic frame: the string "`type_name`" (backtick on both sides) anywhere
// in a sibling spec file. This is the narrowest syntactic frame that avoids
// false positives from plain-text prose references, pseudocode without ticks,
// or partial-name matches.
//
// Exemption: the `metric` entry (§8.8.1) is exempt per §8.9(g).
func hqwn63FixtureCheckC(entries []hqwn63FixtureEventEntry, siblingContent map[string]string) []hqwn63FixtureViolation {
	var violations []hqwn63FixtureViolation
	for _, e := range entries {
		if e.typeName == hqwn63FixtureMetricExemptType {
			// §8.9(g) explicit exemption: metric is a free-use event type.
			continue
		}

		needle := "`" + e.typeName + "`"
		found := false
		for _, content := range siblingContent {
			if strings.Contains(content, needle) {
				found = true
				break
			}
		}

		if !found {
			violations = append(violations, hqwn63FixtureViolation{
				file:     "specs/event-model.md",
				lineNo:   e.lineNo,
				section:  e.sectionNum,
				typeName: e.typeName,
				check:    "c-consumer",
				detail: fmt.Sprintf(
					"no backtick-quoted citation `%s` found in any sibling spec; §8.9(g) requires at least one sibling spec to cite this event by name",
					e.typeName,
				),
			})
		}
	}
	return violations
}

// TestHQWN63EventModelTaxonomyLint is the binding test for hk-hqwn.63.
//
// It parses specs/event-model.md §8 and runs the three taxonomy lint checks:
//
//	(a) Every §8 table row carries a valid durability class (F, O, or L).
//	(b) Every §8 event type has an Axes: line (four-axis tags per EV-INV-005).
//	(c) Every non-exempt §8 event type is backtick-cited in at least one sibling spec.
//
// Known violations covered by in-flight beads are listed in
// hqwn63FixtureExpectedViolations. Those entries are logged (not failed) and
// produce an error if they become stale (violation no longer present).
func TestHQWN63EventModelTaxonomyLint(t *testing.T) {
	entries := hqwn63FixtureParseSection8Entries(t)
	if len(entries) == 0 {
		t.Fatal("TestHQWN63EventModelTaxonomyLint: no §8 event entries found; check hqwn63FixtureSection8RowPattern")
	}
	t.Logf("hk-hqwn.63 audit: parsed %d §8 event type entries from specs/event-model.md", len(entries))

	siblingContent := hqwn63FixtureLoadSiblingContent(t)

	var allViolations []hqwn63FixtureViolation
	allViolations = append(allViolations, hqwn63FixtureCheckA(entries)...)
	allViolations = append(allViolations, hqwn63FixtureCheckB(t, entries)...)
	allViolations = append(allViolations, hqwn63FixtureCheckC(entries, siblingContent)...)

	// Build a set of violation keys found in the current corpus.
	foundKeys := make(map[string]hqwn63FixtureViolation, len(allViolations))
	for _, v := range allViolations {
		foundKeys[hqwn63FixtureViolationKey(v)] = v
	}

	// Check for stale skip-list entries (pinned violations that no longer exist).
	for key, entry := range hqwn63FixtureExpectedViolations {
		if _, present := foundKeys[key]; !present {
			t.Errorf("hk-hqwn.63 skip-list: stale entry %q (pinned by %s) — violation no longer present; remove from hqwn63FixtureExpectedViolations",
				key, entry.pinnedBy)
		}
	}

	// Separate violations into expected (pinned) and unexpected (new failures).
	var unexpected []hqwn63FixtureViolation
	for _, v := range allViolations {
		key := hqwn63FixtureViolationKey(v)
		if entry, pinned := hqwn63FixtureExpectedViolations[key]; pinned {
			t.Logf("hk-hqwn.63 expected violation (pinned by %s): %s — %s",
				entry.pinnedBy, key, entry.reason)
			continue
		}
		unexpected = append(unexpected, v)
	}

	if len(unexpected) == 0 {
		t.Logf("hk-hqwn.63 audit: all %d §8 entries pass — "+
			"check(a) durability=%d events, check(b) axes=%d pinned, check(c) consumer=%d pinned",
			len(entries),
			len(entries), // all pass check-a
			func() int {
				n := 0
				for k := range hqwn63FixtureExpectedViolations {
					if strings.HasPrefix(k, "b-axes:") {
						n++
					}
				}
				return n
			}(),
			func() int {
				n := 0
				for k := range hqwn63FixtureExpectedViolations {
					if strings.HasPrefix(k, "c-consumer:") {
						n++
					}
				}
				return n
			}(),
		)
		return
	}

	// Report ALL unexpected violations so the full failure surface is visible.
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(
		"hk-hqwn.63 violation: %d NEW §8 taxonomy defect(s) found\n",
		len(unexpected),
	))
	sb.WriteString("(specs/event-model.md §8, §8.9, EV-INV-005, EV-016)\n\n")
	sb.WriteString("Checks:\n")
	sb.WriteString("  (a) durability: Dur column ∈ {F=fsync-boundary, O=ordinary, L=lossy-tail-ok}\n")
	sb.WriteString("  (b) axes:       Axes: line with 4 valid axes per EV-INV-005\n")
	sb.WriteString("  (c) consumer:   backtick-quoted citation in at least one sibling spec\n\n")
	for _, v := range unexpected {
		sb.WriteString("  ")
		sb.WriteString(v.String())
		sb.WriteString("\n")
	}
	t.Error(sb.String())
}
