package main

// schedule.go — `harmonik schedule` CLI subcommand block (codename:schedule, hk-0es).
//
// The generic recurring-job surface. All verbs mutate (or read)
// .harmonik/schedules.json directly via internal/schedule.Store, so they work
// whether or not the daemon is running: a running daemon reloads the file on its
// next tick (mtime-change detection) and picks up the change within one poll
// interval; when no daemon is up the change takes effect on next boot.
//
// Verbs:
//
//	add     --id <id> --schedule "daily@HH:MM <tz>" --action <command|spawn-crew> ...
//	list    [--json]
//	remove  <id>
//	enable  <id>
//	disable <id>
//	run-now <id>     (sets a one-shot force-fire flag; honours overlap policy)
//
// Exit codes (mirrors the queue/crew CLI taxonomy):
//
//	0   Success
//	1   Argument error / job not found / persistence failure
//	2   Unrecognised verb
//
// No daemon connection is required for any verb, so there is no exit-17 path
// here (unlike queue submit/crew start): the file IS the coordination surface.
//
// Spec ref: hk-0es brief (operator-locked D1 daily-only, D2 catch-up coalesce).

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/schedule"
)

// runScheduleSubcommand routes `harmonik schedule <verb> [args]`.
// subArgs is os.Args[2:].
func runScheduleSubcommand(subArgs []string) int {
	verb := ""
	if len(subArgs) > 0 {
		verb = subArgs[0]
	}
	rest := []string{}
	if len(subArgs) > 1 {
		rest = subArgs[1:]
	}

	switch verb {
	case "--help", "-h", "":
		scheduleUsage()
		return 0
	case "add":
		return runScheduleAdd(rest)
	case "list":
		return runScheduleList(rest)
	case "remove":
		return runScheduleRemove(rest)
	case "enable":
		return runScheduleEnableDisable(rest, true)
	case "disable":
		return runScheduleEnableDisable(rest, false)
	case "run-now":
		return runScheduleRunNow(rest)
	default:
		fmt.Fprintf(os.Stderr, "harmonik schedule: unrecognised verb %q; verbs are: add, list, remove, enable, disable, run-now\n", verb)
		return 2
	}
}

// resolveScheduleStore resolves projectDir (--project or cwd), constructs a
// Store, and loads the existing file. Returns nil + a non-zero exit code on error.
func resolveScheduleStore(projectFlag string) (*schedule.Store, int) {
	if projectFlag == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik schedule: cannot determine cwd: %v\n", err)
			return nil, 1
		}
		projectFlag = wd
	}
	abs, err := filepath.Abs(projectFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik schedule: cannot resolve project path %q: %v\n", projectFlag, err)
		return nil, 1
	}
	store := schedule.NewStore(abs)
	if loadErr := store.Load(); loadErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik schedule: load: %v\n", loadErr)
		return nil, 1
	}
	return store, 0
}

// parseScheduleSpec parses a "daily@HH:MM <tz>" spec string into a Schedule.
// The tz token is optional and defaults to "local".
//
// Accepted forms:
//
//	"daily@09:30"
//	"daily@09:30 local"
//	"daily@09:30 America/New_York"
func parseScheduleSpec(spec string) (schedule.Schedule, error) {
	spec = strings.TrimSpace(spec)
	// Split kind@time from the optional tz token.
	fields := strings.Fields(spec)
	if len(fields) == 0 {
		return schedule.Schedule{}, fmt.Errorf("empty --schedule")
	}
	head := fields[0]
	tz := schedule.TZLocal
	if len(fields) >= 2 {
		tz = fields[1]
	}
	at := strings.SplitN(head, "@", 2)
	if len(at) != 2 {
		return schedule.Schedule{}, fmt.Errorf("invalid --schedule %q: want \"daily@HH:MM [tz]\"", spec)
	}
	kind := at[0]
	if kind != schedule.ScheduleKindDaily {
		return schedule.Schedule{}, fmt.Errorf("unsupported schedule kind %q (v1 supports %q only)", kind, schedule.ScheduleKindDaily)
	}
	s := schedule.Schedule{Kind: kind, At: at[1], TZ: tz}
	// Validate via NextFire so a bad HH:MM / tz fails at add-time, not at fire-time.
	if _, err := schedule.NextFire(s, time.Now()); err != nil {
		return schedule.Schedule{}, err
	}
	return s, nil
}

// runScheduleAdd implements `schedule add`.
func runScheduleAdd(args []string) int {
	var (
		id, specStr, actionKind      string
		crew, queue, mission         string
		overlap, catchup, catchupWin string
		projectFlag                  string
		argv                         []string
		sawDashDash                  bool
	)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if sawDashDash {
			argv = append(argv, arg)
			continue
		}
		switch {
		case arg == "--":
			sawDashDash = true
		case arg == "--id" && i+1 < len(args):
			id = args[i+1]
			i++
		case arg == "--schedule" && i+1 < len(args):
			specStr = args[i+1]
			i++
		case arg == "--action" && i+1 < len(args):
			actionKind = args[i+1]
			i++
		case arg == "--crew" && i+1 < len(args):
			crew = args[i+1]
			i++
		case arg == "--queue" && i+1 < len(args):
			queue = args[i+1]
			i++
		case arg == "--mission" && i+1 < len(args):
			mission = args[i+1]
			i++
		case arg == "--overlap-policy" && i+1 < len(args):
			overlap = args[i+1]
			i++
		case arg == "--catchup" && i+1 < len(args):
			catchup = args[i+1]
			i++
		case arg == "--catchup-window" && i+1 < len(args):
			catchupWin = args[i+1]
			i++
		case arg == "--project" && i+1 < len(args):
			projectFlag = args[i+1]
			i++
		case strings.HasPrefix(arg, "--project="):
			projectFlag = strings.TrimPrefix(arg, "--project=")
		default:
			fmt.Fprintf(os.Stderr, "harmonik schedule add: unexpected argument %q\n", arg)
			return 1
		}
	}

	if id == "" {
		fmt.Fprintln(os.Stderr, "harmonik schedule add: --id is required")
		return 1
	}
	if specStr == "" {
		fmt.Fprintln(os.Stderr, "harmonik schedule add: --schedule is required")
		return 1
	}
	sched, err := parseScheduleSpec(specStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik schedule add: %v\n", err)
		return 1
	}

	action := schedule.Action{Kind: actionKind}
	switch actionKind {
	case schedule.ActionKindCommand:
		if len(argv) == 0 {
			fmt.Fprintln(os.Stderr, "harmonik schedule add: --action command requires `-- <argv...>`")
			return 1
		}
		action.Argv = argv
	case schedule.ActionKindSpawnCrew:
		if crew == "" || queue == "" {
			fmt.Fprintln(os.Stderr, "harmonik schedule add: --action spawn-crew requires --crew and --queue")
			return 1
		}
		action.Crew = crew
		action.Queue = queue
		action.Mission = mission
	default:
		fmt.Fprintf(os.Stderr, "harmonik schedule add: --action must be %q or %q (got %q)\n",
			schedule.ActionKindCommand, schedule.ActionKindSpawnCrew, actionKind)
		return 1
	}

	// Validate optional policy fields.
	switch overlap {
	case "", schedule.OverlapPolicySkip, schedule.OverlapPolicyAllow:
	default:
		fmt.Fprintf(os.Stderr, "harmonik schedule add: --overlap-policy must be %q or %q\n",
			schedule.OverlapPolicySkip, schedule.OverlapPolicyAllow)
		return 1
	}
	switch catchup {
	case "", schedule.CatchupCoalesceWithinWindow, schedule.CatchupOff:
	default:
		fmt.Fprintf(os.Stderr, "harmonik schedule add: --catchup must be %q or %q\n",
			schedule.CatchupCoalesceWithinWindow, schedule.CatchupOff)
		return 1
	}

	store, code := resolveScheduleStore(projectFlag)
	if code != 0 {
		return code
	}

	job := schedule.ScheduledJob{
		ID:            id,
		Schedule:      sched,
		Action:        action,
		Enabled:       true,
		OverlapPolicy: overlap,
		Catchup:       catchup,
		CatchupWindow: catchupWin,
	}
	if addErr := store.Add(job); addErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik schedule add: %v\n", addErr)
		return 1
	}
	fmt.Printf("added: %s (%s@%s %s, action=%s, enabled)\n", id, sched.Kind, sched.At, sched.TZ, action.Kind)
	return 0
}

// runScheduleList implements `schedule list [--json]`.
func runScheduleList(args []string) int {
	jsonOut := false
	projectFlag := ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--json":
			jsonOut = true
		case arg == "--project" && i+1 < len(args):
			projectFlag = args[i+1]
			i++
		case strings.HasPrefix(arg, "--project="):
			projectFlag = strings.TrimPrefix(arg, "--project=")
		default:
			fmt.Fprintf(os.Stderr, "harmonik schedule list: unexpected argument %q\n", arg)
			return 1
		}
	}
	store, code := resolveScheduleStore(projectFlag)
	if code != 0 {
		return code
	}
	jobs := store.List()

	if jsonOut {
		for _, j := range jobs {
			line, err := json.Marshal(j)
			if err != nil {
				fmt.Fprintf(os.Stderr, "harmonik schedule list: marshal: %v\n", err)
				return 1
			}
			fmt.Println(string(line))
		}
		return 0
	}

	if len(jobs) == 0 {
		fmt.Println("(no scheduled jobs)")
		return 0
	}
	now := time.Now()
	for _, j := range jobs {
		enabled := "disabled"
		if j.Enabled {
			enabled = "enabled"
		}
		nextStr := "-"
		if next, err := schedule.NextFire(j.Schedule, now); err == nil {
			// Display next-fire in local time per the brief.
			nextStr = next.Local().Format("2006-01-02 15:04 MST")
		}
		lastStr := "never"
		if j.LastFire != "" {
			if t, err := time.Parse(time.RFC3339, j.LastFire); err == nil {
				lastStr = t.Local().Format("2006-01-02 15:04 MST")
			} else {
				lastStr = j.LastFire
			}
		}
		fmt.Printf("%-20s  %-8s  next=%-22s  last=%-22s  %s\n",
			j.ID, enabled, nextStr, lastStr, scheduleActionSummary(j.Action))
	}
	return 0
}

// scheduleActionSummary renders a one-line summary of an action for `list`.
func scheduleActionSummary(a schedule.Action) string {
	switch a.Kind {
	case schedule.ActionKindCommand:
		return "command: " + strings.Join(a.Argv, " ")
	case schedule.ActionKindSpawnCrew:
		return fmt.Sprintf("spawn-crew: crew=%s queue=%s", a.Crew, a.Queue)
	default:
		return a.Kind
	}
}

// runScheduleRemove implements `schedule remove <id>`.
func runScheduleRemove(args []string) int {
	id, projectFlag, code := scheduleSingleIDArgs("remove", args)
	if code != 0 {
		return code
	}
	store, code := resolveScheduleStore(projectFlag)
	if code != 0 {
		return code
	}
	removed, err := store.Remove(id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik schedule remove: %v\n", err)
		return 1
	}
	if !removed {
		fmt.Fprintf(os.Stderr, "harmonik schedule remove: no such job %q\n", id)
		return 1
	}
	fmt.Printf("removed: %s\n", id)
	return 0
}

// runScheduleEnableDisable implements `schedule enable|disable <id>`.
func runScheduleEnableDisable(args []string, enable bool) int {
	verb := "disable"
	if enable {
		verb = "enable"
	}
	id, projectFlag, code := scheduleSingleIDArgs(verb, args)
	if code != 0 {
		return code
	}
	store, code := resolveScheduleStore(projectFlag)
	if code != 0 {
		return code
	}
	ok, err := store.SetEnabled(id, enable)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik schedule %s: %v\n", verb, err)
		return 1
	}
	if !ok {
		fmt.Fprintf(os.Stderr, "harmonik schedule %s: no such job %q\n", verb, id)
		return 1
	}
	fmt.Printf("%sd: %s\n", verb, id)
	return 0
}

// runScheduleRunNow implements `schedule run-now <id>`.
func runScheduleRunNow(args []string) int {
	id, projectFlag, code := scheduleSingleIDArgs("run-now", args)
	if code != 0 {
		return code
	}
	store, code := resolveScheduleStore(projectFlag)
	if code != 0 {
		return code
	}
	ok, err := store.RequestRunNow(id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik schedule run-now: %v\n", err)
		return 1
	}
	if !ok {
		fmt.Fprintf(os.Stderr, "harmonik schedule run-now: no such job %q\n", id)
		return 1
	}
	fmt.Printf("run-now requested: %s (fires on the daemon's next tick, honouring overlap policy)\n", id)
	return 0
}

// scheduleSingleIDArgs parses the common `<id> [--project DIR]` argument shape.
func scheduleSingleIDArgs(verb string, args []string) (id, projectFlag string, code int) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--project" && i+1 < len(args):
			projectFlag = args[i+1]
			i++
		case strings.HasPrefix(arg, "--project="):
			projectFlag = strings.TrimPrefix(arg, "--project=")
		case strings.HasPrefix(arg, "-"):
			fmt.Fprintf(os.Stderr, "harmonik schedule %s: unknown flag %q\n", verb, arg)
			return "", "", 1
		default:
			if id != "" {
				fmt.Fprintf(os.Stderr, "harmonik schedule %s: unexpected argument %q\n", verb, arg)
				return "", "", 1
			}
			id = arg
		}
	}
	if id == "" {
		fmt.Fprintf(os.Stderr, "harmonik schedule %s: <id> is required\n", verb)
		return "", "", 1
	}
	return id, projectFlag, 0
}

func scheduleUsage() {
	fmt.Print(`harmonik schedule — generic recurring-job primitive

USAGE
  harmonik schedule <verb> [flags]

VERBS
  add       Add a scheduled job
  list      List scheduled jobs (id, enabled, next-fire, last-fire, action)
  remove    Remove a job by id
  enable    Enable a job by id
  disable   Disable a job by id
  run-now   Fire a job on the daemon's next tick (honours overlap policy)

ADD FLAGS
  --id <id>                    Unique job id (required)
  --schedule "daily@HH:MM [tz]"  Daily fire time; tz is "local" or an IANA zone (default local)
  --action <command|spawn-crew>  Action kind (required)
  --crew <c>                   Crew name        (spawn-crew)
  --queue <q>                  Named queue      (spawn-crew)
  --mission <p>                Mission handoff path (spawn-crew, optional)
  -- <argv...>                 Command + args   (command; must be last)
  --overlap-policy <skip|allow>  Default skip
  --catchup <coalesce-within-window|off>  Default coalesce-within-window
  --catchup-window <dur>       Catch-up window (e.g. 24h); default = schedule interval
  --project DIR                Project directory (default: cwd)

NOTES
  All verbs mutate .harmonik/schedules.json directly and work whether or not the
  daemon is running. A running daemon picks up changes on its next poll tick.

EXIT CODES
  0   Success
  1   Argument error / job not found / persistence failure
  2   Unrecognised verb

EXAMPLES
  harmonik schedule add --id nightly-crew --schedule "daily@02:00 America/New_York" \
    --action spawn-crew --crew nightowl --queue night --mission /tmp/mission.md
  harmonik schedule add --id rotate-logs --schedule "daily@00:30" \
    --action command -- /usr/bin/logrotate /etc/logrotate.conf
  harmonik schedule list
  harmonik schedule disable nightly-crew
  harmonik schedule run-now rotate-logs
  harmonik schedule remove rotate-logs
`)
}
