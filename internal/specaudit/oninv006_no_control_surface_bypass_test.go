//go:build specaudit

package specaudit_test

// hk-sx9r.72 binding test — ON-INV-006 sensor: no subsystem introduces a
// control surface bypassing the between-task invariant.
//
// Spec ref: specs/operator-nfr.md §5 ON-INV-006.
//
// ON-INV-006 states: "No subsystem MAY introduce a new control surface (a CLI
// command, an API endpoint, a signal handler, a socket protocol action) that
// aborts an in-flight run without routing through `stop --immediate` per
// §4.3.ON-009 OR the drain-gated pause/upgrade path of §4.3.ON-008. Subsystems
// MUST NOT add local escape-hatches (e.g., `kill-run`, `abandon-run`,
// `skip-reconciliation`) that would bypass the drain gate or the reconciliation
// carve-out."
//
// # Audit frame
//
// This test is a code-corpus sensor with three mechanical scans plus one
// spec-text binding check:
//
//  1. Spec-text binding: ON-INV-006 heading and the normative "MAY introduce"
//     obligation sentence are present in specs/operator-nfr.md so the rule
//     cannot be silently eroded by spec edits.
//
//  2. CLI subcommand scan: every top-level verb dispatched in
//     cmd/harmonik/main.go must appear in the declared allowlist. Any new
//     subcommand not on the list is flagged as a potential ON-INV-006 violation
//     requiring explicit review and allowlist update.
//
//  3. Socket-op scan: every `case "..."` in the socket-request switch in
//     internal/daemon/socket.go must appear in the declared allowlist. Any new
//     op code not on the list is flagged as a potential ON-INV-006 violation.
//
//  4. Signal-handler scan: every signal argument to signal.NotifyContext in
//     cmd/harmonik/main.go must appear in the declared allowlist. Any new
//     signal registration not on the list is flagged as a potential ON-INV-006
//     violation.
//
// # Allowlist discipline
//
// The allowlists below record control-surface entries that predate local
// authorization annotations. New CLI subcommands, socket ops, and daemon
// signals MAY instead carry an immediately-preceding
// `// ON-INV-006-AUTH: ...` comment at the dispatch site. The comment must cite
// the requirement that authorizes the surface and explain why it cannot abort
// an in-flight run. This keeps new-surface review local to the file that adds
// the surface, avoiding concurrent merge conflicts in this central sensor.
//
// # Failure modes
//
//   - spec-text-binding: ON-INV-006 heading or key obligation phrase absent.
//   - cli-unlisted-verb: a top-level subcommand in main.go is neither in the
//     CLI allowlist nor locally annotated; it may be a local escape-hatch
//     bypassing the invariant.
//   - socket-unlisted-op: a case label in the socket op switch is neither in
//     the socket-op allowlist nor locally annotated; it may expose an
//     undocumented run-abort path.
//   - signal-unlisted: a signal registered via signal.NotifyContext in main.go
//     is neither in the signal allowlist nor locally annotated; a new signal
//     may bypass the drain gate.
//
// # Helper prefix
//
// All package-level identifiers in this file use the oninv006Fixture prefix per
// the implementer-protocol.md helper-prefix discipline.

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

// oninv006FixtureRepoRoot returns the absolute path to the repository root
// by walking up two directories from this file's location.
func oninv006FixtureRepoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("oninv006FixtureRepoRoot: runtime.Caller(0) failed")
	}
	// thisFile is .../internal/specaudit/oninv006_no_control_surface_bypass_test.go
	// internal/specaudit/ → internal/ → repo root
	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}

// oninv006FixtureLoadLines opens the file at path and returns all lines.
func oninv006FixtureLoadLines(t *testing.T, path string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known repo directory; not user input.
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("oninv006FixtureLoadLines: open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if scanErr := sc.Err(); scanErr != nil {
		t.Fatalf("oninv006FixtureLoadLines: scan %s: %v", path, scanErr)
	}
	return lines
}

// oninv006FixtureHeading matches the ON-INV-006 level-4 requirement heading.
var oninv006FixtureHeading = regexp.MustCompile(`^#### ON-INV-006 —`)

// oninv006FixtureAnySectionHeading matches any Markdown heading line (level 1–4).
var oninv006FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// oninv006FixtureBodyWindow is the maximum number of lines after the heading to
// scan for requirement-body content before the next section begins.
const oninv006FixtureBodyWindow = 20

// oninv006FixtureBodyContains reports whether any line in body contains substr
// (case-sensitive substring match).
func oninv006FixtureBodyContains(body []string, substr string) bool {
	for _, line := range body {
		if strings.Contains(line, substr) {
			return true
		}
	}
	return false
}

// oninv006FixtureONINV006Body returns the body lines of ON-INV-006 in specFile.
func oninv006FixtureONINV006Body(t *testing.T, lines []string) (body []string, headingLineNo int) {
	t.Helper()
	idx := -1
	for i, line := range lines {
		if oninv006FixtureHeading.MatchString(line) {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("oninv006FixtureONINV006Body: ON-INV-006 heading not found in specs/operator-nfr.md")
	}
	limit := idx + 1 + oninv006FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}
	for i := idx + 1; i < limit; i++ {
		if oninv006FixtureAnySectionHeading.MatchString(lines[i]) {
			break
		}
		body = append(body, lines[i])
	}
	return body, idx + 1
}

// ─── Allowlists ─────────────────────────────────────────────────────────────
//
// MAINTENANCE: when adding a new CLI subcommand, socket op, or daemon signal,
// cite the requirement that authorizes the new control surface either in the
// relevant legacy allowlist below or in an immediately-preceding
// `// ON-INV-006-AUTH: ...` comment at the dispatch site. Keep the citation
// precise; it is the audit trail.

// oninv006FixtureCLIAllowlist is the exhaustive set of top-level subcommands
// declared in cmd/harmonik/main.go as of the commit introducing this sensor.
//
// Authorisation:
//   - "tmux-start": process-lifecycle.md §4.10 PL-028 refinement; operator
//     session bootstrap — does not affect in-flight runs.
//   - "hook-relay": specs/claude-hook-bridge.md §4.4 CHB-010..017; hook delivery
//     path — does not affect in-flight runs.
//
// New entries may instead use a local ON-INV-006-AUTH comment at the dispatch
// site when that avoids central allowlist churn.
var oninv006FixtureCLIAllowlist = map[string]string{
	"tmux-start": "process-lifecycle.md §4.10 PL-028; bootstrap-only, no run impact",
	"hook-relay": "claude-hook-bridge.md §4.4 CHB-010..017; hook delivery, no run abort",
	// hk-eblue: queue verbs submit/append/status/dry-run route through the
	// queue-model.md §8 state machine and are gated by ON-008/ON-009; the
	// socket listener serialises all mutations through the QueueStore write lock
	// (QM-060) which is ON-008-drain-safe.
	"queue": "queue-model.md §8; drain-safe via QM-060 single-writer; ON-008 compliant",
	// hk-xjbvi: worker enable/disable flips the remote worker's enabled flag in
	// the live registry via socket RPC. It gates FUTURE remote dispatch only —
	// SelectWorker skips a disabled worker; in-flight remote runs complete and
	// release their slot normally. Cannot abort an in-flight run. Drain-safe ON-008.
	"worker": "operator-nfr.md §4.3 ON-008; live remote-worker dispatch toggle, gates future dispatch only, no in-flight run abort",
	// hk-icecw: harmonik run <bead-id> submits a single-item queue and starts
	// the daemon in-process; the daemon exit is driven by queue drain
	// (CompleteAndUnlink + cancelOnQueueDrain), not by operator abort; fully
	// ON-008 drain-gated (no in-flight run impact).
	"run": "hk-icecw; single-bead queue submission; exits on drain, ON-008 compliant",
	// hk-6ynv4: read-only observation CLI; opens daemon socket and streams
	// NDJSON envelopes to stdout. No daemon-state mutation, no run impact.
	// Authorised by operator-nfr.md §4.9 ON-055.
	"subscribe": "operator-nfr.md §4.9 ON-055; read-only event stream, no run impact",
	// hk-y4e96: usage text printed to stdout and immediate exit; no daemon
	// connection, no state mutation, no in-flight run impact.
	"--help": "operator-nfr.md §4.9 ON-055; help text to stdout, no run impact",
	// hk-y171w: project-bootstrap subcommand; scaffolds .harmonik/ on a fresh
	// repo before any daemon exists. Cannot abort an in-flight run (there is none).
	"init": "operator-nfr.md §4.9 ON-055; pre-daemon project bootstrap, no run impact",
	// hk-lgtq2: Cat-3c auto-reconciler; detects and closes IN_PROGRESS beads whose
	// implementation already merged. Routes through the reconciliation carve-out
	// (§4.3 ON-010); does not abort in-flight runs.
	"reconcile": "operator-nfr.md §4.3 ON-010; reconciliation carve-out, no mid-run abort",
	// hk-63oh.39: operator confirms a PENDING reconciliation verdict so the daemon
	// proceeds with verdict execution; pre-execution verdict pause per ON-014. The
	// run is already drain-parked awaiting the verdict — confirm does not abort it.
	"confirm-verdict": "operator-nfr.md §4.3 ON-014; pre-execution verdict confirm, no abort",
	// hk-63oh.39: operator vetoes a PENDING reconciliation verdict (optionally
	// substituting escalate-to-human); pre-execution verdict pause per ON-014.
	// Acts before verdict execution; does not abort an in-flight run.
	"veto-verdict": "operator-nfr.md §4.3 ON-014; pre-execution verdict veto, no in-flight abort",
	// hk-jon6r: git merge-driver for .beads/issues.jsonl invoked by git during
	// merge (harmonik beads-merge %O %A %B %P). A file-merge helper; not a daemon
	// control surface and cannot abort runs.
	"beads-merge": "operator-nfr.md §4.9 ON-055; git merge-driver helper, no run impact",
	// hk-0f35x: one-time dedup of .beads/issues.jsonl; reads and rewrites the
	// JSONL file in-place keeping the newest record per bead ID. No daemon
	// interaction, no run-state mutation, cannot abort runs.
	"beads-dedup": "operator-nfr.md §4.9 ON-055; JSONL file dedup helper, no run impact",
	// hk-39ryh: read-only handler-pause status surface; reads
	// .harmonik/handler-state.json directly (no daemon). No state mutation.
	"handler": "operator-nfr.md §4.9 ON-055; read-only handler status, no run impact",
	// hk-ekap1 (session-keeper): context-watcher and .dispatching-marker control;
	// set/clear-dispatching write an agent-scoped marker file only. No daemon
	// state mutation, no in-flight run impact.
	"keeper": "operator-nfr.md §4.9 ON-055; agent dispatching-marker, no run abort",
	// hk-qx702: supervisor/cognition process management per PL-028d; start/stop/
	// status/attach/restart/logs act on the supervisor process, not on in-flight
	// runs. SIGTERM of the daemon still routes through the ON-027 drain steps.
	"supervise": "process-lifecycle.md §4.10 PL-028d; supervisor process mgmt, ON-027 drain on stop",
	// hk-4rkrg: end-to-end smoke verification; creates a smoke bead and submits it
	// through the normal queue path (ON-008 drain-safe). Asserts on events; does
	// not abort any other run.
	"smoke": "queue-model.md §8; submits a smoke bead via the queue, ON-008 compliant",
	// hk-cnjhx/hk-onn1x: agent-to-agent messaging surface; send/recv/log/who write
	// and read comms events only. No daemon run-state mutation, no run abort.
	"comms": "operator-nfr.md §4.9 ON-055; agent messaging events, no run impact",
	// hk-yj2j6 (captain/crew): crew session management (start/stop/list). Manages
	// crew sub-sessions, not in-flight harmonik runs; cannot abort the between-task
	// invariant.
	"crew": "operator-nfr.md §4.9 ON-055; crew session mgmt, no in-flight run abort",
	// hk-ly0n (captain launcher): bare launcher that mints a UUIDv4 --session-id
	// and brings up `claude --remote-control` in a tmux session. A bootstrap
	// launcher like tmux-start — it never opens the daemon socket, never acquires
	// the pidfile lock, and cannot abort an in-flight run.
	"captain": "operator-nfr.md §4.9 ON-055; captain session launcher, no daemon connection, no run abort",
	// hk-ly0n: `harmonik start captain` is the alias prefix for the captain
	// launcher above; same bootstrap-only authorisation (the verb after `start`
	// routes to runCaptainSubcommand). No daemon connection, no in-flight run impact.
	"start": "operator-nfr.md §4.9 ON-055; alias prefix for captain launcher, no run abort",
	// hk-voyf4: workflow-graph utilities (graph validate <path>); reads files
	// directly, no daemon, no state mutation.
	"graph": "operator-nfr.md §4.9 ON-055; offline graph validation, no run impact",
	// hk-1qrty: status-sheet builder; snapshot mode reads .harmonik/ with no
	// daemon. Read-only reporting, no run impact.
	"digest": "operator-nfr.md §4.9 ON-055; read-only status sheet, no run impact",
	// hk-ww7ee: prints semver + commit hash to stdout and exits 0. No daemon
	// connection, no state mutation, no in-flight run impact.
	// Spec ref: release-pipeline.md §2.3.
	"version": "release-pipeline.md §2.3; version info to stdout, no run impact",
	// hk-n7ofb: release ledger management (ledger list, certify, yank). Operates
	// on the ledger JSON file directly; no daemon required, no in-flight run impact.
	// Spec ref: release-pipeline.md §4, §6, §7.1.
	"release": "release-pipeline.md §4/§6/§7.1; offline ledger management, no run impact",
	// hk-0es (codename:schedule): generic recurring-job config (add/list/remove/
	// enable/disable/run-now). Mutates .harmonik/schedules.json directly with no
	// daemon connection; it does NOT abort or mutate in-flight harmonik runs. The
	// daemon's schedule tick fires actions through the already-governed crew-start
	// and command-exec paths — the §7.1 between-task invariant is unaffected.
	"schedule": "operator-nfr.md §4.9 ON-055; offline recurring-job config, no in-flight run abort",
	// hitl-decisions SPEC §2 (hk-xz9/hk-kba): agent→human decision surface.
	// raise/withdraw/list/answer emit or query decision events on events.jsonl;
	// none of these ops abort an in-flight run or mutate daemon run-state.
	// Authorised by operator-nfr.md §4.9 ON-055 (observation/event surface).
	"decisions": "operator-nfr.md §4.9 ON-055; agent→human decision event surface, no run abort",
	// specs/promote.md (hk-pk3p1): git cherry-pick-to-target with build gate
	// (push-mode) or gh pr create (PR-mode). Operates in a temp worktree with no
	// daemon connection; cannot abort an in-flight run.
	"promote": "specs/promote.md §2; offline git cherry-pick or PR opener, no run abort",
	// specs/process-lifecycle.md §4.2 PL-031 (hk-dmw): read-only SHA-256 hash
	// printer — prints the first 12 hex chars of SHA-256(realpath(project_root))
	// and exits 0. No daemon connection, no state mutation, no run impact.
	"project-hash": "process-lifecycle.md §4.2 PL-031; read-only project hash, no run impact",
	// flywheel V6 (hk-owz1): ephemeral goal-keeper that reads operator comms
	// since the last_event_id cursor and rewrites .harmonik/intent/goal-state.json.
	// No daemon connection, no run abort, no in-flight state mutation. Fired
	// on idle-triggered realign by the Pi dispatcher (NOT a clock timer).
	// Authorised by operator-nfr.md §4.9 ON-055 (offline state file mutation,
	// observation-only impact on in-flight runs).
	"goal-keeper": "operator-nfr.md §4.9 ON-055; offline goal-state update, no in-flight run abort",
	// flywheel V4 (hk-9mr2): sentinel governor-trip exception writer. `emit-trip`
	// writes a decision_required record to .harmonik/decision_acks/ — a file-only
	// operation with no daemon connection and no in-flight run abort. The adversary
	// crew calls this LLM-free command after reviewing evidence; the projector (not
	// this CLI) gates the all-clear. Authorised by operator-nfr.md §4.9 ON-055.
	"sentinel": "operator-nfr.md §4.9 ON-055; offline decision_acks writer, no in-flight run abort",
	// AC2 (hk-lacr): captain approval for staged deploy+verify follow-up beads.
	// Removes the "needs-greenlight" label via `br label remove` so the daemon's
	// dispatch loop can claim the bead. Label-only change; no daemon connection
	// required, no in-flight run affected. Operator-nfr.md §4.2 ON-007 (operator
	// controls dispatch gate explicitly; greenlight IS the authorisation).
	"greenlight": "operator-nfr.md §4.2 ON-007; label-only br write, no daemon connection, no in-flight run abort (AC2 hk-lacr)",
	// hk-s5v3 (M4 of hk-rl4b / codename:sleep-wake): LLM-initiated quiesce of
	// captain+crew sessions. Non-force consults the SS-INV-005 veto guard:
	// daemon refuses if in-flight runs exist or dispatchable work is pending.
	// --force overrides the veto; in-flight runs proceed to completion unaffected
	// (the between-task invariant is NOT violated because sleep parks LLM
	// *sessions*, NOT harmonik *runs*; per §4.3.ON-008).
	"sleep": "operator-nfr.md §4.3 ON-008; LLM-initiated quiesce; daemon refuses if work stranded (SS-INV-005 veto); --force overrides veto, parks sessions only, no in-flight run abort",
	// hk-s5v3 (M4 of hk-rl4b / codename:sleep-wake): LLM-initiated wake of
	// sleeping captain/crew sessions. Resumes orchestrator sessions only;
	// no harmonik run state is mutated and no in-flight run is affected. --all and
	// --agent variants both route through QuiesceArbiter.HandleDaemonWake.
	// Equivalent in scope to operator-resume (ON-010) but at the session layer.
	"wake": "operator-nfr.md §4.3 ON-010; resumes sleeping LLM sessions only, no in-flight run abort",
	// hk-i7i3 (codename:asset-sync): reconciles on-disk project instruction files
	// against the binary's embedded asset bundle via a class-aware 3-way merge.
	// --dry-run is the default and has zero side effects. --apply is gated on a
	// daemon-lull guard (no active dispatching) — it does NOT abort in-flight runs;
	// it only proceeds when no dispatch is in progress, preserving ON-008 safety.
	// Exit-3 is returned (not abort) when the lull gate refuses. Operator-nfr.md
	// §4.9 ON-055 (observation/admin surface, lull-gated write, no run abort).
	"sync-assets": "operator-nfr.md §4.9 ON-055; lull-gated asset reconcile, --dry-run default, no in-flight run abort",
	// hk-igpg: read-only printer for the per-project Claude RC label prefix
	// (daemon.remote_control_prefix). Reads project config directly; no daemon
	// socket connection, no state mutation, side-effect-free. Mirror of
	// project-hash (same bootstrap-only authorisation).
	"remote-control-prefix": "operator-nfr.md §4.9 ON-055; read-only RC-prefix printer, no daemon connection, no run impact",
	// hk-f4w7: interactive one-shot migration that adds daemon.remote_control_prefix
	// to an existing project's .harmonik/config.yaml. Reads config + beads prefix,
	// prompts operator, writes config file. No daemon socket connection; no in-flight
	// run abort; side-effects limited to config.yaml. Authorised as an operator
	// admin/bootstrap surface (same class as init, sync-assets).
	"migrate-rc-prefix": "operator-nfr.md §4.9 ON-055; one-shot config migration, no daemon connection, no in-flight run abort",
	// hk-gv04 (P2-a): typed StateSnapshot aggregator (specs/system-state.md
	// §4 SS-001..SS-015). Daemon-up: live socket RPC ("state" op, read-only
	// snapshot); daemon-down: disk fallback. No state mutation, no in-flight
	// run abort. Authorised by operator-nfr.md §4.9 ON-055 (read-only
	// observation surface).
	"state": "specs/system-state.md §4 SS-001; read-only state snapshot aggregator, no in-flight run abort",
	// hk-nwqa0 (SH-032): scenario harness runner. Operates against per-suite
	// ephemeral fixture roots (SH-016a synthetic project roots) that are
	// entirely separate from any operator project's .harmonik/ tree. No daemon
	// socket connection; no in-flight run state is read, mutated, or aborted.
	// Authorised by operator-nfr.md §4.9 ON-055 (standalone test tool, no
	// control-surface impact on any in-flight harmonik run).
	"harness": "operator-nfr.md §4.9 ON-055; standalone scenario harness, synthetic fixture roots (SH-016a), no daemon connection, no in-flight run abort",
	// hk-b89kk (Phase-0 token-usage join): reads ~/.claude/projects/.../session.jsonl
	// transcripts and .harmonik/events/events.jsonl; no daemon socket connection,
	// no state mutation, no in-flight run impact. Offline cost-analysis surface.
	"usage": "operator-nfr.md §4.9 ON-055; offline transcript+event cost analysis, no daemon connection, no in-flight run abort",
	// hk-qpzsv: installs/uninstalls/shows a macOS launchd LaunchAgent plist that
	// runs scripts/ops-monitor-check.sh every 5m independent of any Claude session.
	// Writes to ~/Library/LaunchAgents/ + calls launchctl; no daemon socket
	// connection, no harmonik run-state mutation, no in-flight run abort.
	"ops-monitor": "operator-nfr.md §4.9 ON-055; offline launchd LaunchAgent install, no daemon connection, no in-flight run abort",
}

// oninv006FixtureSocketOpAllowlist is the exhaustive set of op codes handled
// in the SocketRequest switch in internal/daemon/socket.go.
//
// Authorisation:
//   - "emit-outcome": MVH_ROADMAP row #5; agent reports a completed run outcome.
//     Routes through the run-completion path of §4.3 — does not abort in-flight.
//   - "claim-next": MVH_ROADMAP row #5; agent requests the next ready bead.
//     Reads queue state — does not affect in-flight runs.
//
// New entries may instead use a local ON-INV-006-AUTH comment at the dispatch
// site when that avoids central allowlist churn.
var oninv006FixtureSocketOpAllowlist = map[string]string{
	"emit-outcome": "MVH_ROADMAP row #5; run-completion report, no mid-run abort",
	"claim-next":   "MVH_ROADMAP row #5; queue read, no run impact",
	// hk-eblue: queue JSON-RPC ops route through the queue-model.md §8 state
	// machine, serialised by QM-060 single-writer; they do not abort in-flight
	// runs (submit creates a new queue; append/status/dry-run are queue reads or
	// pre-dispatch mutations; all are drain-safe per ON-008).
	"queue-submit":  "queue-model.md §8; new queue creation, no in-flight run abort",
	"queue-append":  "queue-model.md §8; append to pending group, drain-safe ON-008",
	"queue-status":  "queue-model.md §9; read-only status query, no run impact",
	"queue-dry-run": "queue-model.md §8; validation-only, no state mutation, ON-008",
	// hk-tigaf.8: read-only enumeration of named queues; no state mutation.
	"queue-list": "queue-model.md §9; read-only queue enumeration, no run impact",
	// hk-6ynv4: read-only observation surface; streams NDJSON envelopes on
	// the connection until close. Cannot mutate daemon state, cannot abort
	// in-flight runs. Authorised by operator-nfr.md §4.9 ON-055.
	"subscribe": "operator-nfr.md §4.9 ON-055; read-only observation, no run impact",
	// hk-tigaf.6: per-queue or global pause/resume routes through QueueOperatorEventConsumer
	// → Queue.Status transition (paused-by-drain). Does not abort in-flight runs
	// (in-flight items complete; new dispatches are gated). ON-007/ON-010.
	"operator-pause":  "operator-nfr.md §4.3 ON-007; drain-gated pause, no mid-run abort",
	"operator-resume": "operator-nfr.md §4.3 ON-010; resume from paused-by-drain, ON-010",
	// hk-tigaf: adjusts the daemon's concurrency ceiling (HandleQueueSetConcurrency).
	// Raising/lowering the ceiling gates future dispatch only — in-flight runs
	// complete normally; it cannot abort an in-flight run. Drain-safe per ON-008.
	"queue-set-concurrency": "operator-nfr.md §4.3 ON-008; concurrency-ceiling gate, no mid-run abort",
	// hk-xjbvi: worker-set-enabled flips the remote worker's enabled flag in the
	// live registry (HandleWorkerSetEnabled → registry.SetEnabledByName). Gates
	// future remote dispatch only — SelectWorker skips a disabled worker; in-flight
	// runs complete and release their slot. It cannot abort an in-flight run. ON-008.
	"worker-set-enabled": "operator-nfr.md §4.3 ON-008; live remote-worker dispatch toggle, no mid-run abort",
	// hk-nbrmf/hk-7t27s: agent-comms ops route through the comms event handlers
	// only; they write/read agent_message + presence events and never touch daemon
	// run state. No in-flight run abort.
	"comms-send":     "operator-nfr.md §4.9 ON-055; agent message write, no run impact",
	"comms-recv":     "operator-nfr.md §4.9 ON-055; agent message read, no run impact",
	"comms-presence": "operator-nfr.md §4.9 ON-055; presence read, no run impact",
	// hk-5tg5o (captain/crew): crew-start/stop manage crew sub-sessions per
	// c2-spec.md §3.1–§3.5; they spawn/teardown crew sessions, not harmonik runs,
	// and cannot abort the between-task invariant.
	"crew-start": "operator-nfr.md §4.9 ON-055; crew session spawn, no in-flight run abort",
	"crew-stop":  "operator-nfr.md §4.9 ON-055; crew session teardown, no in-flight run abort",
	// hitl-decisions SPEC §2 (hk-xz9 K2): agent-side emit ops. decisions-raise
	// and decisions-withdraw write decision_needed / decision_withdrawn events to
	// events.jsonl via DecisionsHandler; they do not mutate run state or abort
	// in-flight runs. Authorised by operator-nfr.md §4.9 ON-055.
	"decisions-raise":    "operator-nfr.md §4.9 ON-055; agent-side decision emit, no run abort",
	"decisions-withdraw": "operator-nfr.md §4.9 ON-055; agent-side decision withdraw, no run abort",
	// hitl-decisions SPEC §2 (hk-kba K4): operator-side ops. decisions-list is a
	// read-only projection over events.jsonl (S6); decisions-answer emits a
	// decision_resolved event (N7 option check, N3 no-op on duplicate). Neither
	// op aborts an in-flight run. Authorised by operator-nfr.md §4.9 ON-055.
	"decisions-list":   "operator-nfr.md §4.9 ON-055; read-only open-decision projection, no run abort",
	"decisions-answer": "operator-nfr.md §4.9 ON-055; operator decision resolve event, no run abort",
	// hk-s5v3 (M4 of hk-rl4b / codename:sleep-wake): LLM-initiated quiesce of
	// captain/crew sessions. Routes to QuiesceArbiter.HandleDaemonSleep; non-force
	// consults the SS-INV-005 veto guard (refuses if work stranded). force=true
	// parks orchestrator sessions immediately — the between-task invariant is NOT
	// violated because this parks LLM *sessions* (captain/crew), NOT harmonik
	// *runs*; in-flight runs proceed to completion (per §4.3.ON-008).
	"daemon-sleep": "operator-nfr.md §4.3 ON-008; LLM-initiated quiesce; daemon refuses if work stranded (SS-INV-005 veto); force overrides veto, parks sessions only, no in-flight run abort",
	// hk-s5v3 (M4 of hk-rl4b / codename:sleep-wake): LLM-initiated wake of
	// sleeping captain/crew sessions. Routes to QuiesceArbiter.HandleDaemonWake;
	// clears sleep markers and nudges captain/crew tmux panes. No harmonik run
	// state mutated; no in-flight run abort.
	// Equivalent scope to operator-resume (ON-010) at the LLM session layer.
	"daemon-wake": "operator-nfr.md §4.3 ON-010; resumes sleeping LLM sessions, no run state mutation, no in-flight run abort",
	// hk-gv04 (P2-a): read-only StateSnapshot RPC. HandleState snapshots
	// RunRegistry + QueueStore and computes the §4.2 fold — pure reads,
	// no state mutation, no in-flight run abort. Authorised by
	// specs/system-state.md §4 SS-001 (read-only snapshot via live socket).
	"state": "specs/system-state.md §4 SS-001; read-only state snapshot RPC, no state mutation, no in-flight run abort",
}

// oninv006FixtureSignalAllowlist is the exhaustive set of signals registered
// with signal.NotifyContext in cmd/harmonik/main.go.
//
// Authorisation:
//   - "syscall.SIGINT":  operator keyboard interrupt (Ctrl-C); routes through
//     context cancellation → drain path per ON-027 (drain-gated graceful stop).
//   - "syscall.SIGTERM": standard termination signal; routes identically to
//     SIGINT via signal.NotifyContext context cancellation → drain path.
//
// New entries may instead use a local ON-INV-006-AUTH comment at the dispatch
// site when that avoids central allowlist churn.
var oninv006FixtureSignalAllowlist = map[string]string{
	"syscall.SIGINT":  "operator interrupt; routes via context cancel → ON-027 drain",
	"syscall.SIGTERM": "termination signal; routes via context cancel → ON-027 drain",
}

// ─── Matchers ───────────────────────────────────────────────────────────────

const (
	oninv006FixtureAuthPrefix     = "ON-INV-006-AUTH:"
	oninv006FixtureLocalAuthDepth = 8
)

// oninv006FixtureCLIVerbLine matches a line in cmd/harmonik/main.go that
// dispatches a top-level subcommand by checking os.Args[1].
// Examples:
//
//	if len(os.Args) >= 2 && os.Args[1] == "tmux-start" {
var oninv006FixtureCLIVerbLine = regexp.MustCompile(`os\.Args\[1\]\s*==\s*"([^"]+)"`)

// oninv006FixtureSocketOpLine matches a case label in the socket op switch.
// Examples:
//
//	case "emit-outcome":
//	case "claim-next":
var oninv006FixtureSocketOpLine = regexp.MustCompile(`^\s*case\s+"([^"]+)":`)

// oninv006FixtureSignalNotifyLine matches a signal argument inside a
// signal.NotifyContext call. Because multiple signals may appear on a single
// line we extract all syscall.SIG* tokens from any line containing
// "NotifyContext".
var (
	oninv006FixtureSignalNotifyLine = regexp.MustCompile(`signal\.NotifyContext\b`)
	oninv006FixtureSyscallSigToken  = regexp.MustCompile(`\bsyscall\.(SIG[A-Z]+)\b`)
)

// oninv006FixtureHasAuthorization reports whether a discovered control surface
// is authorized by the legacy central allowlist or by a local dispatch-site
// `// ON-INV-006-AUTH: ...` comment. Local comments let new surfaces satisfy
// ON-INV-006 without forcing unrelated concurrent beads to edit this file.
func oninv006FixtureHasAuthorization(allowlist map[string]string, key string, lines []string, lineIdx int) bool {
	if _, ok := allowlist[key]; ok {
		return true
	}
	return oninv006FixtureLocalAuthorization(lines, lineIdx) != ""
}

// oninv006FixtureLocalAuthorization finds the nearest immediately-preceding
// ON-INV-006 authorization comment for the dispatch site at lineIdx. It scans
// only contiguous `//` comments and blank spacer lines directly above the site;
// encountering code or a block comment stops the search.
func oninv006FixtureLocalAuthorization(lines []string, lineIdx int) string {
	start := lineIdx - oninv006FixtureLocalAuthDepth
	if start < 0 {
		start = 0
	}
	for i := lineIdx - 1; i >= start; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, "//") {
			break
		}
		comment := strings.TrimSpace(strings.TrimPrefix(trimmed, "//"))
		if strings.HasPrefix(comment, oninv006FixtureAuthPrefix) {
			return strings.TrimSpace(strings.TrimPrefix(comment, oninv006FixtureAuthPrefix))
		}
	}
	return ""
}

// ─── Tests ───────────────────────────────────────────────────────────────────

// TestONINV006SpecTextBinding verifies that ON-INV-006 and its key obligation
// phrases are present in specs/operator-nfr.md.  Eroding the spec text would
// silently remove the rule the remaining subtests enforce.
func TestONINV006SpecTextBinding(t *testing.T) {
	t.Parallel()

	root := oninv006FixtureRepoRoot(t)
	specPath := filepath.Join(root, "specs", "operator-nfr.md")
	lines := oninv006FixtureLoadLines(t, specPath)

	body, headingLineNo := oninv006FixtureONINV006Body(t, lines)
	t.Logf("ON-INV-006 heading found at specs/operator-nfr.md line %d; body window = %d lines",
		headingLineNo, len(body))

	type check struct {
		id     string
		needle string
		detail string
	}

	checks := []check{
		{
			id:     "1",
			needle: "MAY introduce",
			detail: "ON-INV-006 body must contain 'MAY introduce' — the normative prohibition " +
				"on adding new control surfaces that bypass the between-task invariant",
		},
		{
			id:     "2",
			needle: "stop --immediate",
			detail: "ON-INV-006 body must name 'stop --immediate' as the authorised abort path " +
				"per §4.3.ON-009; its absence means the spec no longer defines the only legal " +
				"abort control surface",
		},
		{
			id:     "3",
			needle: "drain-gated",
			detail: "ON-INV-006 body must name the 'drain-gated' pause/upgrade path per §4.3.ON-008 " +
				"as the second authorised control surface; absence removes the structural guard",
		},
		{
			id:     "4",
			needle: "escape-hatches",
			detail: "ON-INV-006 body must prohibit local 'escape-hatches' (e.g. kill-run, " +
				"abandon-run, skip-reconciliation); absence means the rule no longer covers " +
				"local workarounds",
		},
		{
			id:     "5",
			needle: "Tags: mechanism",
			detail: "ON-INV-006 must carry Tags: mechanism; absence indicates the body was " +
				"truncated or the Tags line removed",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s", c.id), func(t *testing.T) {
			t.Parallel()
			if !oninv006FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"ON-INV-006 spec-text check(%s) FAILED\n"+
						"  spec:    specs/operator-nfr.md ~line %d (ON-INV-006 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}
}

func TestONINV006LocalAuthorization(t *testing.T) {
	t.Parallel()

	lines := []string{
		"func run() int {",
		"\t// harmonik frobnicate -- read-only state viewer.",
		"\t// ON-INV-006-AUTH: operator-nfr.md §4.9 ON-055; read-only, no run impact",
		"\tif len(os.Args) >= 2 && os.Args[1] == \"frobnicate\" {",
		"\t\treturn runFrobnicate(os.Args[2:])",
		"\t}",
		"}",
	}

	if !oninv006FixtureHasAuthorization(map[string]string{}, "frobnicate", lines, 3) {
		t.Fatal("expected local ON-INV-006-AUTH comment to authorize unlisted control surface")
	}
	if got := oninv006FixtureLocalAuthorization(lines, 3); !strings.Contains(got, "ON-055") {
		t.Fatalf("expected local authorization text to be returned, got %q", got)
	}
}

func TestONINV006LocalAuthorizationRequiresAdjacentCommentBlock(t *testing.T) {
	t.Parallel()

	lines := []string{
		"func run() int {",
		"\t// ON-INV-006-AUTH: operator-nfr.md §4.9 ON-055; stale comment",
		"\t_ = computeSomething()",
		"\tif len(os.Args) >= 2 && os.Args[1] == \"frobnicate\" {",
		"\t\treturn runFrobnicate(os.Args[2:])",
		"\t}",
		"}",
	}

	if oninv006FixtureHasAuthorization(map[string]string{}, "frobnicate", lines, 3) {
		t.Fatal("did not expect non-adjacent ON-INV-006-AUTH comment to authorize dispatch site")
	}
}

// TestONINV006CLISubcommands scans cmd/harmonik/main.go for top-level
// subcommand dispatch sites (os.Args[1] == "<verb>") and asserts that every
// verb is either present in oninv006FixtureCLIAllowlist or has a local
// ON-INV-006-AUTH comment. An unlisted/unannotated verb is a candidate
// ON-INV-006 violation: it may introduce a run-abort path that does not route
// through the state machine of §7.1.
func TestONINV006CLISubcommands(t *testing.T) {
	t.Parallel()

	root := oninv006FixtureRepoRoot(t)
	mainPath := filepath.Join(root, "cmd", "harmonik", "main.go")
	lines := oninv006FixtureLoadLines(t, mainPath)

	var violations []string
	for i, line := range lines {
		ms := oninv006FixtureCLIVerbLine.FindStringSubmatch(line)
		if ms == nil {
			continue
		}
		verb := ms[1]
		if !oninv006FixtureHasAuthorization(oninv006FixtureCLIAllowlist, verb, lines, i) {
			violations = append(violations, fmt.Sprintf(
				"cmd/harmonik/main.go line %d: unlisted CLI subcommand %q — "+
					"add an immediately-preceding `// ON-INV-006-AUTH: ...` comment "+
					"or add to oninv006FixtureCLIAllowlist with a requirement citation "+
					"(ON-008, ON-009, system-state SS-..., or new ON-NNN) confirming it cannot abort an in-flight run",
				i+1, verb,
			))
		}
	}

	for _, v := range violations {
		t.Errorf("ON-INV-006 cli-unlisted-verb: %s", v)
	}
	if len(violations) == 0 {
		t.Logf("ON-INV-006 CLI scan PASS — all %d CLI verbs in cmd/harmonik/main.go are allowlisted",
			oninv006FixtureCountCLIVerbs(lines))
	}
}

// oninv006FixtureCountCLIVerbs counts the number of CLI verb dispatch sites
// found in the provided lines (used for logging only).
func oninv006FixtureCountCLIVerbs(lines []string) int {
	n := 0
	for _, line := range lines {
		if oninv006FixtureCLIVerbLine.MatchString(line) {
			n++
		}
	}
	return n
}

// TestONINV006SocketOps scans internal/daemon/socket.go for case labels in the
// SocketRequest op switch and asserts that every op is either present in
// oninv006FixtureSocketOpAllowlist or has a local ON-INV-006-AUTH comment. An
// unlisted/unannotated op may expose a run-abort path bypassing the drain gate
// or state machine of §7.1.
func TestONINV006SocketOps(t *testing.T) {
	t.Parallel()

	root := oninv006FixtureRepoRoot(t)
	socketPath := filepath.Join(root, "internal", "daemon", "socket.go")
	lines := oninv006FixtureLoadLines(t, socketPath)

	// Only scan lines after the req.Op switch preamble to avoid false positives
	// from string literals in comments or other switch statements.  We detect
	// the switch by looking for the "switch req.Op" line and scan from there.
	switchIdx := -1
	for i, line := range lines {
		if strings.Contains(line, "switch req.Op") {
			switchIdx = i
			break
		}
	}
	if switchIdx < 0 {
		t.Fatal("ON-INV-006 socket scan: 'switch req.Op' not found in internal/daemon/socket.go — " +
			"the socket op dispatch site may have moved; update this sensor's scan anchor")
	}

	// Scan from the switch statement to the matching closing brace (a blank
	// line or the next top-level declaration).  We use a simple depth counter.
	depth := 0
	var violations []string
	inSwitch := false
	for i := switchIdx; i < len(lines); i++ {
		line := lines[i]
		depth += strings.Count(line, "{") - strings.Count(line, "}")
		if !inSwitch && strings.Contains(line, "{") {
			inSwitch = true
		}
		if inSwitch && depth <= 0 {
			break
		}
		ms := oninv006FixtureSocketOpLine.FindStringSubmatch(line)
		if ms == nil {
			continue
		}
		op := ms[1]
		if !oninv006FixtureHasAuthorization(oninv006FixtureSocketOpAllowlist, op, lines, i) {
			violations = append(violations, fmt.Sprintf(
				"internal/daemon/socket.go line %d: unlisted socket op %q — "+
					"add an immediately-preceding `// ON-INV-006-AUTH: ...` comment "+
					"or add to oninv006FixtureSocketOpAllowlist with a requirement citation "+
					"(ON-008, ON-009, system-state SS-..., or new ON-NNN) confirming it cannot abort an in-flight run",
				i+1, op,
			))
		}
	}

	for _, v := range violations {
		t.Errorf("ON-INV-006 socket-unlisted-op: %s", v)
	}
	if len(violations) == 0 {
		t.Logf("ON-INV-006 socket op scan PASS — all socket ops in internal/daemon/socket.go are allowlisted")
	}
}

// TestONINV006SignalHandlers scans cmd/harmonik/main.go for signals registered
// via signal.NotifyContext and asserts that every syscall.SIG* token is either
// present in oninv006FixtureSignalAllowlist or has a local ON-INV-006-AUTH
// comment. An unlisted/unannotated signal may introduce a daemon-termination
// path that skips the drain gate (ON-027) and bypasses the between-task
// invariant (ON-INV-006).
func TestONINV006SignalHandlers(t *testing.T) {
	t.Parallel()

	root := oninv006FixtureRepoRoot(t)
	mainPath := filepath.Join(root, "cmd", "harmonik", "main.go")
	lines := oninv006FixtureLoadLines(t, mainPath)

	var violations []string
	for i, line := range lines {
		if !oninv006FixtureSignalNotifyLine.MatchString(line) {
			continue
		}
		// Extract all syscall.SIG* tokens from the NotifyContext call line.
		// Multi-line call sites are uncommon; the existing usage fits one line.
		tokens := oninv006FixtureSyscallSigToken.FindAllStringSubmatch(line, -1)
		for _, tok := range tokens {
			sigKey := "syscall." + tok[1]
			if !oninv006FixtureHasAuthorization(oninv006FixtureSignalAllowlist, sigKey, lines, i) {
				violations = append(violations, fmt.Sprintf(
					"cmd/harmonik/main.go line %d: unlisted signal %s in signal.NotifyContext — "+
						"add an immediately-preceding `// ON-INV-006-AUTH: ...` comment "+
						"or add to oninv006FixtureSignalAllowlist with a requirement citation "+
						"confirming it routes through ON-027 drain, ON-009 stop-immediate, or cannot abort an in-flight run",
					i+1, sigKey,
				))
			}
		}
	}

	for _, v := range violations {
		t.Errorf("ON-INV-006 signal-unlisted: %s", v)
	}
	if len(violations) == 0 {
		t.Logf("ON-INV-006 signal handler scan PASS — all signal.NotifyContext registrations in cmd/harmonik/main.go are allowlisted")
	}
}
