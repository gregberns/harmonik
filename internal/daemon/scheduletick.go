package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/gregberns/harmonik/internal/crewrun"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/schedule"
)

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

type crewStarter interface {
	HandleCrewStart(ctx context.Context, payload json.RawMessage) (json.RawMessage, error)
}

type commsSendFunc func(ctx context.Context, to, from, body, topic string) error

type commsWhoQuerier func(ctx context.Context) (map[string]struct{}, error)

type schedulePort struct {
	store           *schedule.Store
	wakeC           <-chan struct{}
	crewHandler     crewStarter
	commsWhoQuerier commsWhoQuerier
	commsSend       commsSendFunc
	projectDir      string
	handlerEnv      []string
}

func newSchedulePort(daemonBinaryPath, projectDir string, handlerEnv []string, store *schedule.Store, crewHandler crewStarter) schedulePort {
	port := schedulePort{
		store:           store,
		crewHandler:     crewHandler,
		commsWhoQuerier: shellCommsWho(daemonBinaryPath, projectDir),
		commsSend:       shellCommsSend(daemonBinaryPath, projectDir),
		projectDir:      projectDir,
		handlerEnv:      handlerEnv,
	}
	if store != nil {
		port.wakeC = store.WakeCh()
	}
	return port
}

func runScheduleTick(ctx context.Context, port schedulePort) {
	if port.store == nil {
		return
	}
	if _, err := port.store.ReloadIfChanged(); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: schedule: reload: %v\n", err)
	}
	nowUTC := time.Now().UTC()
	for _, job := range port.store.List() {
		if job.ForceNext {
			if skipped, reason := overlapBlocks(ctx, port, job); skipped {
				fmt.Fprintf(os.Stderr, "daemon: schedule: job %q: run-now skip-on-overlap (%s)\n", job.ID, reason)
			} else if err := doFireAction(ctx, port, job, nowUTC); err != nil {
				fmt.Fprintf(os.Stderr, "daemon: schedule: job %q: run-now fire: %v\n", job.ID, err)
			}
			if _, cErr := port.store.ClearForceNext(job.ID); cErr != nil {
				fmt.Fprintf(os.Stderr, "daemon: schedule: job %q: clear run-now flag: %v\n", job.ID, cErr)
			}
			continue
		}
		if !job.Enabled {
			continue
		}
		fireScheduledJobIfDue(ctx, port, job, nowUTC)
	}
}

func fireScheduledJobIfDue(ctx context.Context, port schedulePort, job schedule.ScheduledJob, nowUTC time.Time) {
	decision, err := schedule.Decide(job, nowUTC)
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon: schedule: job %q: decide: %v\n", job.ID, err)
		return
	}
	if decision.MissedSkipped {
		fmt.Fprintf(os.Stderr, "daemon: schedule: job %q: skipping missed fire at %s (outside catch-up window)\n",
			job.ID, decision.FireInstant.UTC().Format(time.RFC3339))
		if _, mErr := port.store.MarkFired(job.ID, decision.FireInstant.UTC().Format(time.RFC3339), job.LastPID); mErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: schedule: job %q: mark missed: %v\n", job.ID, mErr)
		}
		return
	}
	if !decision.Fire {
		return
	}
	fireScheduledJob(ctx, port, job, nowUTC, decision.Catchup)
}

func fireScheduledJob(ctx context.Context, port schedulePort, job schedule.ScheduledJob, nowUTC time.Time, isCatchup bool) {
	if skipped, reason := overlapBlocks(ctx, port, job); skipped {
		fmt.Fprintf(os.Stderr, "daemon: schedule: job %q: skip-on-overlap (%s)\n", job.ID, reason)
		return
	}
	if isCatchup {
		fmt.Fprintf(os.Stderr, "daemon: schedule: job %q: firing coalesced catch-up\n", job.ID)
	}
	if err := doFireAction(ctx, port, job, nowUTC); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: schedule: job %q: fire: %v\n", job.ID, err)
	}
}

func overlapBlocks(ctx context.Context, port schedulePort, job schedule.ScheduledJob) (blocked bool, reason string) {
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
		if port.commsWhoQuerier == nil {
			return false, ""
		}
		online, err := port.commsWhoQuerier(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "daemon: schedule: job %q: comms who query failed (%v); not blocking\n", job.ID, err)
			return false, ""
		}
		if _, ok := online[job.Action.Crew]; ok {
			return true, fmt.Sprintf("crew %q presence-online", job.Action.Crew)
		}
		return false, ""
	case schedule.ActionKindCommsSend:
		return false, ""
	default:
		return false, ""
	}
}

func doFireAction(ctx context.Context, port schedulePort, job schedule.ScheduledJob, nowUTC time.Time) error {
	var firedPID int
	var fireErr error
	switch job.Action.Kind {
	case schedule.ActionKindCommand:
		firedPID, fireErr = fireCommandAction(port, job)
	case schedule.ActionKindSpawnCrew:
		fireErr = fireSpawnCrewAction(ctx, port, job)
	case schedule.ActionKindCommsSend:
		fireErr = fireCommsSendAction(ctx, port, job)
	default:
		fireErr = fmt.Errorf("unknown action kind %q", job.Action.Kind)
	}
	if _, err := port.store.MarkFired(job.ID, nowUTC.Format(time.RFC3339), firedPID); err != nil {
		return errors.Join(fireErr, fmt.Errorf("mark fired: %w", err))
	}
	return fireErr
}

func fireCommandAction(port schedulePort, job schedule.ScheduledJob) (int, error) {
	if len(job.Action.Argv) == 0 {
		return 0, fmt.Errorf("command action has empty argv")
	}
	// exec.Command, NOT exec.CommandContext, and that is deliberate. A scheduled
	// command is handed to the operating system and then let go: the lines below
	// put it in its own process group so it survives a daemon restart. A context
	// on this command would kill the job the moment the daemon's context ended,
	// which is the opposite of what a scheduled job is for.
	//nolint:gosec // G204: argv is operator-authored schedule config, not untrusted input.
	cmd := exec.Command(job.Action.Argv[0], job.Action.Argv[1:]...)
	cmd.Dir = port.projectDir
	cmd.Env = scrubCredentialEnv(append(os.Environ(), port.handlerEnv...))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("start command %q: %w", strings.Join(job.Action.Argv, " "), err)
	}
	pid := cmd.Process.Pid
	go func() { _ = cmd.Wait() }() //nolint:errcheck // detached; exit status not actionable
	return pid, nil
}

func fireSpawnCrewAction(ctx context.Context, port schedulePort, job schedule.ScheduledJob) error {
	if port.crewHandler == nil {
		return fmt.Errorf("spawn-crew action but no crew handler wired")
	}
	if job.Action.Crew == "" || job.Action.Queue == "" {
		return fmt.Errorf("spawn-crew action requires crew and queue")
	}
	req := crewrun.CrewStartRequest{
		Name:        job.Action.Crew,
		Queue:       job.Action.Queue,
		MissionPath: job.Action.Mission,
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal crew-start request: %w", err)
	}
	if _, err := port.crewHandler.HandleCrewStart(ctx, payload); err != nil {
		return fmt.Errorf("crew-start: %w", err)
	}
	return nil
}

func fireCommsSendAction(ctx context.Context, port schedulePort, job schedule.ScheduledJob) error {
	if port.commsSend == nil {
		return fmt.Errorf("comms-send action but no commsSend func wired")
	}
	if job.Action.To == "" {
		return fmt.Errorf("comms-send action: To is required")
	}
	return port.commsSend(ctx, job.Action.To, job.Action.From, job.Action.Body, job.Action.Topic)
}

func commsSendArgv(to, from, body, topic, projectDir string) []string {
	effectiveFrom := from
	if effectiveFrom == "" {
		effectiveFrom = "daemon"
	}
	args := []string{"comms", "send", "--to", to, "--from", effectiveFrom, "--project", projectDir}
	if topic != "" {
		args = append(args, "--topic", topic)
	}
	args = append(args, "--", body)
	return args
}

func shellCommsSend(daemonBinaryPath, projectDir string) commsSendFunc {
	bin := daemonBinaryPath
	if bin == "" {
		bin = "harmonik"
	}
	return func(ctx context.Context, to, from, body, topic string) error {
		args := commsSendArgv(to, from, body, topic, projectDir)
		//nolint:gosec // G204: bin is the resolved daemon binary; to/body/topic are operator-authored schedule config.
		cmd := exec.CommandContext(ctx, bin, args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("comms send --to %q: %w (output: %s)", to, err, strings.TrimSpace(string(out)))
		}
		return nil
	}
}

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, syscall.Signal(0)) == nil
}

type commsWhoEntry struct {
	Agent  string `json:"agent"`
	Status string `json:"status"`
}

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
