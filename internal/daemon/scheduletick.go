package daemon

// scheduletick.go — work-loop integration for the generic recurring-job
// primitive (codename:schedule, hk-0es).
//
// The tick runs IN the supervise loop (runWorkLoop), after the dispatch-context
// check and before the capacity gate, so it reuses the existing 2s poll +
// wake-channel cadence and the claim-write serialisation already in place — it
// does NOT add a competing goroutine/ticker.
//
// Each pass, for every Enabled job whose next fire is due (pure schedule.Decide),
// the tick honours the job's overlap policy, fires the action (command via a
// detached process whose env is os.Environ()+handlerEnv with credential
// deny-list keys scrubbed — see fireCommandAction, NOT a pre-sanitised env — or
// spawn-crew via the SAME HandleCrewStart path `harmonik crew start` uses),
// records LastFire/LastPID, and persists.
//
// GENERIC: no project-codename literals here. spawn-crew billing guards apply by
// construction because the action reuses HandleCrewStart (which builds the launch
// spec with --remote-control and the no-credential-keys baseEnv).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/schedule"
)

// scrubCredentialEnv returns env with every credential deny-list key
// (ANTHROPIC_API_KEY, ANTHROPIC_AUTH_TOKEN, CLAUDE_CODE_OAUTH* per
// specs/credential-isolation.md §4 CI-002) removed. It reuses the single
// canonical deny-list predicate handler.IsCredentialDenyListKey so the schedule
// command-action path and the claude handler path strip the SAME keys — there is
// one deny-list, not two that can drift.
//
// This is defense-in-depth: by CI-001 the daemon's own environment already holds
// no credential key, so os.Environ() should be clean. But the 2026-05-30
// credit-burn incident (hk-f2nm1) was precisely a daemon mis-launched WITH
// ANTHROPIC_API_KEY in its env; under that failure mode a scheduled command would
// inherit it. The claude path applies a belt-and-braces strip at ClaudeEnvVars;
// this gives the command path the same guard. Unlike the claude path, command
// actions do not re-emit empty overrides (they spawn a detached child directly,
// not into an additive tmux server env), so a plain strip is sufficient.
func scrubCredentialEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		key := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			key = kv[:i]
		}
		if handler.IsCredentialDenyListKey(key) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// crewStarter is the minimal subset of CrewHandler the schedule tick needs to
// fire a spawn-crew action. CrewHandler (crewstart.go) satisfies it. Extracting
// the narrow interface keeps the tick testable with a lightweight double and
// avoids a hard dependency on the full handler in tests that don't spawn.
type crewStarter interface {
	HandleCrewStart(ctx context.Context, payload json.RawMessage) (json.RawMessage, error)
}

// commsWhoQuerier returns the set of presence-online agent names. Production
// shells out to `harmonik comms who --json`; tests inject a double. Returning a
// set keeps the spawn-crew overlap check a simple membership test.
type commsWhoQuerier func(ctx context.Context) (map[string]struct{}, error)

// runScheduleTick evaluates every job in the store once and fires those that are
// due and not blocked by their overlap policy. It is invoked once per work-loop
// poll iteration. Errors on individual jobs are logged and skipped — one bad job
// never stalls the others or the dispatch loop.
//
// deps.scheduleStore nil → no-op (legacy / unit-test daemons without the surface).
func runScheduleTick(ctx context.Context, deps workLoopDeps) {
	if deps.scheduleStore == nil {
		return
	}
	// Pick up out-of-process mutations (the CLI writes schedules.json directly
	// whether or not the daemon is running). Cheap stat-then-maybe-read.
	if _, err := deps.scheduleStore.ReloadIfChanged(); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: schedule: reload: %v\n", err)
		// Keep going with the in-memory state on a transient read error.
	}
	nowUTC := time.Now().UTC()
	for _, job := range deps.scheduleStore.List() {
		// A run-now request fires regardless of Enabled/due, honouring overlap.
		if job.ForceNext {
			if skipped, reason := overlapBlocks(ctx, deps, job); skipped {
				fmt.Fprintf(os.Stderr, "daemon: schedule: job %q: run-now skip-on-overlap (%s)\n", job.ID, reason)
			} else if err := doFireAction(ctx, deps, job, nowUTC); err != nil {
				fmt.Fprintf(os.Stderr, "daemon: schedule: job %q: run-now fire: %v\n", job.ID, err)
			}
			// Clear the flag whether or not the fire succeeded/was-skipped so it is a
			// genuine one-shot (a failed fire is logged; the operator can re-issue).
			if _, cErr := deps.scheduleStore.ClearForceNext(job.ID); cErr != nil {
				fmt.Fprintf(os.Stderr, "daemon: schedule: job %q: clear run-now flag: %v\n", job.ID, cErr)
			}
			continue
		}
		if !job.Enabled {
			continue
		}
		fireScheduledJobIfDue(ctx, deps, job, nowUTC)
	}
}

// fireScheduledJobIfDue evaluates one job at nowUTC and fires it when due.
// The ad-hoc run-now path is handled in runScheduleTick via the ForceNext flag
// (it bypasses the due check but still honours the overlap policy).
func fireScheduledJobIfDue(ctx context.Context, deps workLoopDeps, job schedule.ScheduledJob, nowUTC time.Time) {
	decision, err := schedule.Decide(job, nowUTC)
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon: schedule: job %q: decide: %v\n", job.ID, err)
		return
	}
	if decision.MissedSkipped {
		// A missed fire fell outside the catch-up window; advance LastFire past it
		// so we don't re-evaluate it forever, and log.
		fmt.Fprintf(os.Stderr, "daemon: schedule: job %q: skipping missed fire at %s (outside catch-up window)\n",
			job.ID, decision.FireInstant.UTC().Format(time.RFC3339))
		if _, mErr := deps.scheduleStore.MarkFired(job.ID, decision.FireInstant.UTC().Format(time.RFC3339), job.LastPID); mErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: schedule: job %q: mark missed: %v\n", job.ID, mErr)
		}
		return
	}
	if !decision.Fire {
		return
	}
	fireScheduledJob(ctx, deps, job, nowUTC, decision.Catchup)
}

// fireScheduledJob applies the overlap policy then fires; logs and returns.
func fireScheduledJob(ctx context.Context, deps workLoopDeps, job schedule.ScheduledJob, nowUTC time.Time, isCatchup bool) {
	if skipped, reason := overlapBlocks(ctx, deps, job); skipped {
		fmt.Fprintf(os.Stderr, "daemon: schedule: job %q: skip-on-overlap (%s)\n", job.ID, reason)
		return
	}
	if isCatchup {
		fmt.Fprintf(os.Stderr, "daemon: schedule: job %q: firing coalesced catch-up\n", job.ID)
	}
	if err := doFireAction(ctx, deps, job, nowUTC); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: schedule: job %q: fire: %v\n", job.ID, err)
	}
}

// overlapBlocks reports whether the job's overlap policy blocks a fire right now.
//
//   - OverlapPolicyAllow: never blocks.
//   - command action: blocks iff LastPID is still alive.
//   - spawn-crew action: blocks iff a crew named Action.Crew is presence-online.
func overlapBlocks(ctx context.Context, deps workLoopDeps, job schedule.ScheduledJob) (blocked bool, reason string) {
	if job.OverlapPolicy == schedule.OverlapPolicyAllow {
		return false, ""
	}
	switch job.Action.Kind {
	case schedule.ActionKindCommand:
		if job.LastPID > 0 && pidAlive(job.LastPID) {
			return true, fmt.Sprintf("prior command pid %d still alive", job.LastPID)
		}
		return false, ""
	case schedule.ActionKindSpawnCrew:
		if job.Action.Crew == "" {
			return false, ""
		}
		online, err := deps.commsWhoQuerier(ctx)
		if err != nil {
			// Fail-open on a query error: we'd rather risk a duplicate spawn than
			// silently never fire. HandleCrewStart's own collision check is the
			// backstop (it refuses a name+queue conflict).
			fmt.Fprintf(os.Stderr, "daemon: schedule: job %q: comms who query failed (%v); not blocking\n", job.ID, err)
			return false, ""
		}
		if _, ok := online[job.Action.Crew]; ok {
			return true, fmt.Sprintf("crew %q presence-online", job.Action.Crew)
		}
		return false, ""
	default:
		return false, ""
	}
}

// doFireAction performs the action and records the fire. On a command action it
// records the spawned pid; on spawn-crew it records pid 0. LastFire is set to
// nowUTC (RFC3339 UTC).
func doFireAction(ctx context.Context, deps workLoopDeps, job schedule.ScheduledJob, nowUTC time.Time) error {
	var firedPID int
	switch job.Action.Kind {
	case schedule.ActionKindCommand:
		pid, err := fireCommandAction(deps, job)
		if err != nil {
			return err
		}
		firedPID = pid
	case schedule.ActionKindSpawnCrew:
		if err := fireSpawnCrewAction(ctx, deps, job); err != nil {
			return err
		}
		firedPID = 0
	default:
		return fmt.Errorf("unknown action kind %q", job.Action.Kind)
	}
	if _, err := deps.scheduleStore.MarkFired(job.ID, nowUTC.Format(time.RFC3339), firedPID); err != nil {
		return fmt.Errorf("mark fired: %w", err)
	}
	return nil
}

// fireCommandAction spawns Argv as a fresh detached process. Its environment is
// os.Environ() plus deps.handlerEnv (HARMONIK_PROJECT_HASH prepended), then run
// through scrubCredentialEnv so no credential deny-list key (ANTHROPIC_API_KEY
// etc.) reaches the child — the same defense-in-depth the claude path applies at
// ClaudeEnvVars. By CI-001 os.Environ() should already be credential-free; the
// scrub is the belt-and-braces guard against the 2026-05-30 mis-launch failure
// mode (hk-f2nm1). It does NOT block the loop on the process. Returns the spawned
// pid.
func fireCommandAction(deps workLoopDeps, job schedule.ScheduledJob) (int, error) {
	if len(job.Action.Argv) == 0 {
		return 0, fmt.Errorf("command action has empty argv")
	}
	//nolint:gosec // G204: argv is operator-authored schedule config, not untrusted input.
	cmd := exec.Command(job.Action.Argv[0], job.Action.Argv[1:]...)
	cmd.Dir = deps.projectDir
	// os.Environ()+handlerEnv made credential-safe by CI-001 (the daemon env holds
	// no key) AND the scrub below (belt-and-braces strip of the deny-list keys,
	// matching the claude path). handlerEnv is the same base env passed to handler
	// subprocesses; appended last so HARMONIK_PROJECT_HASH wins.
	cmd.Env = scrubCredentialEnv(append(os.Environ(), deps.handlerEnv...))
	// Detach into its own process group so it survives a daemon restart and is not
	// signalled by the daemon's own process-group teardown.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("start command %q: %w", strings.Join(job.Action.Argv, " "), err)
	}
	pid := cmd.Process.Pid
	// Reap asynchronously so the detached process does not become a zombie; do
	// NOT block the work loop.
	go func() { _ = cmd.Wait() }() //nolint:errcheck // detached; exit status not actionable
	return pid, nil
}

// fireSpawnCrewAction drives the daemon's crew-start path with the job's
// {crew,queue,mission}. Reusing HandleCrewStart is what enforces the
// subscription-billing baseline (--remote-control, no credential keys) by
// construction — the schedule tick never execs claude directly.
func fireSpawnCrewAction(ctx context.Context, deps workLoopDeps, job schedule.ScheduledJob) error {
	if deps.crewHandler == nil {
		return fmt.Errorf("spawn-crew action but no crew handler wired")
	}
	if job.Action.Crew == "" || job.Action.Queue == "" {
		return fmt.Errorf("spawn-crew action requires crew and queue")
	}
	req := CrewStartRequest{
		Name:        job.Action.Crew,
		Queue:       job.Action.Queue,
		MissionPath: job.Action.Mission,
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal crew-start request: %w", err)
	}
	if _, err := deps.crewHandler.HandleCrewStart(ctx, payload); err != nil {
		return fmt.Errorf("crew-start: %w", err)
	}
	return nil
}

// pidAlive reports whether pid refers to a live process (signal 0 probe).
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, syscall.Signal(0)) == nil
}

// commsWhoEntry is one `comms who --json` row: the agent name and its presence
// status. Only status=="online" agents block an overlapping spawn-crew fire.
type commsWhoEntry struct {
	Agent  string `json:"agent"`
	Status string `json:"status"`
}

// parseCommsWho parses `harmonik comms who --json` output into the set of
// presence-online agent names. The verified production shape is NDJSON (one JSON
// object per line), so the line-by-line parse is the primary path. To avoid
// silently failing OPEN if the output shape ever changes to a single JSON array
// (which would drop every entry and let a duplicate crew spawn through), the
// parse is tolerant: if NO line parses as an object, it retries by unmarshalling
// the whole output as a JSON array. A parse failure on BOTH shapes returns an
// error so the caller fails LOUD rather than silently treating nobody as online.
func parseCommsWho(out []byte) (map[string]struct{}, error) {
	online := make(map[string]struct{})
	anyLineParsed := false
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry commsWhoEntry
		if jErr := json.Unmarshal([]byte(line), &entry); jErr != nil {
			continue // not an object line; may be array brackets / pretty-printed JSON
		}
		anyLineParsed = true
		if entry.Status == "online" {
			online[entry.Agent] = struct{}{}
		}
	}
	if anyLineParsed {
		return online, nil
	}
	// No NDJSON object line parsed. Either the output was empty (nobody online → an
	// empty set is correct), or the shape changed to a JSON array. Try the array
	// form so a future shape change fails LOUD on a genuine parse error instead of
	// fail-OPEN with an empty set.
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return online, nil // genuinely empty: nobody online
	}
	var arr []commsWhoEntry
	if jErr := json.Unmarshal([]byte(trimmed), &arr); jErr != nil {
		return nil, fmt.Errorf("comms who: parse output (neither NDJSON nor JSON array): %w", jErr)
	}
	for _, entry := range arr {
		if entry.Status == "online" {
			online[entry.Agent] = struct{}{}
		}
	}
	return online, nil
}

// shellCommsWho is the production commsWhoQuerier: it shells out to
// `harmonik comms who --json` and returns the set of agents whose status is
// "online" (NOT stale/dead) via parseCommsWho. It uses the running daemon binary
// path so the correct project's presence registry is read.
//
// Returns an empty set (not an error) when the command exits 0 with no output —
// `comms who` exits 0 with no output when nobody is online — and an error when
// the command exits non-zero or emits output that parses as neither NDJSON nor a
// JSON array.
func shellCommsWho(daemonBinaryPath, projectDir string) commsWhoQuerier {
	bin := daemonBinaryPath
	if bin == "" {
		bin = "harmonik"
	}
	return func(ctx context.Context) (map[string]struct{}, error) {
		//nolint:gosec // G204: bin is the resolved daemon binary path; args are constant.
		cmd := exec.CommandContext(ctx, bin, "comms", "who", "--json", "--project", projectDir)
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("comms who: %w", err)
		}
		return parseCommsWho(out)
	}
}
