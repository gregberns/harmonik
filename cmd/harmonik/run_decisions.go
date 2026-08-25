package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/lifecycle"
	"github.com/gregberns/harmonik/internal/queue"
)

type runBeadOptions struct {
	projectDir string
	// beads is the comma-separated value supplied by --beads.
	beads         string
	maxConcurrent int
	// context is inline text or an @file reference injected into each task.
	context string
	// notifyStream is "-" for stdout or a file path when explicitly enabled.
	notifyStream    string
	notifyStreamSet bool
	noNotifyStream  bool
	workflowMode    string
	workflowRef     string
	// templateParams contains repeatable --param KEY=VALUE substitutions.
	templateParams  map[string]string
	dryRun          bool
	targetBranch    string
	protectBranches []string
	// forbidUnprotectedDefault refuses an unprotected default main branch.
	forbidUnprotectedDefault bool
	wave                     bool
	help                     bool
	positional               []string
}

func parseRunBeadOptions(args []string) (runBeadOptions, error) {
	opts := runBeadOptions{maxConcurrent: 1, templateParams: map[string]string{}}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		name, value, hasValue := strings.Cut(arg, "=")
		next := func() (string, bool) {
			if hasValue {
				return value, true
			}
			if i+1 >= len(args) {
				return "", false
			}
			i++
			return args[i], true
		}
		switch name {
		case "--project":
			opts.projectDir, _ = next()
		case "--beads":
			opts.beads, _ = next()
		case "--max-concurrent":
			v, ok := next()
			n, err := strconv.Atoi(v)
			if !ok || err != nil || n < 1 {
				return opts, fmt.Errorf("--max-concurrent must be a positive integer, got %q", v)
			}
			opts.maxConcurrent = n
		case "--context":
			opts.context, _ = next()
		case "--review-loop":
			return opts, fmt.Errorf("--review-loop was retired along with the review-loop mode.\n  Drop the flag: dot is the default and is already reviewed.")
		case "--no-review-loop":
			return opts, fmt.Errorf("--no-review-loop was retired along with the review-loop mode.\n  It used to mean \"run single-node, unreviewed\". If that is what you want,\n  say it directly: --workflow-mode single.")
		case "--notify-stream":
			opts.notifyStreamSet = true
			opts.notifyStream = value
			if !hasValue || value == "" {
				opts.notifyStream = "-"
			}
		case "--workflow-mode":
			opts.workflowMode, _ = next()
		case "--workflow-ref":
			opts.workflowRef, _ = next()
		case "--no-notify-stream":
			opts.noNotifyStream = true
		case "--wave":
			opts.wave = true
		case "--param":
			v, _ := next()
			key, val, ok := strings.Cut(v, "=")
			if !ok || key == "" {
				return opts, fmt.Errorf("--param must be KEY=VALUE, got %q", v)
			}
			opts.templateParams[key] = val
		case "--target-branch":
			opts.targetBranch, _ = next()
		case "--protect-branch":
			v, _ := next()
			opts.protectBranches = append(opts.protectBranches, v)
		case "--forbid-default-main":
			opts.forbidUnprotectedDefault = true
		case "--dry-run", "--plan-only":
			opts.dryRun = true
		case "--help", "-h":
			opts.help = true
		default:
			if strings.HasPrefix(arg, "-") {
				return opts, fmt.Errorf("unknown flag %q", arg)
			}
			opts.positional = append(opts.positional, arg)
		}
	}
	if opts.help {
		return opts, nil
	}
	if _, err := selectRunBeads(opts.beads, opts.positional); err != nil {
		return opts, err
	}
	if _, err := selectRunWorkflow(opts.workflowMode, opts.workflowRef); err != nil {
		return opts, err
	}
	return opts, nil
}

func selectRunBeads(beads string, positional []string) ([]core.BeadID, error) {
	if beads != "" && len(positional) > 0 {
		return nil, fmt.Errorf("cannot mix positional <bead-id> with --beads; use one or the other")
	}
	if beads != "" {
		ids := make([]core.BeadID, 0)
		for _, raw := range strings.Split(beads, ",") {
			if id := strings.TrimSpace(raw); id != "" {
				ids = append(ids, core.BeadID(id))
			}
		}
		if len(ids) == 0 {
			return nil, fmt.Errorf("--beads requires at least one bead ID")
		}
		return ids, nil
	}
	switch len(positional) {
	case 0:
		return nil, fmt.Errorf("missing <bead-id> argument\nusage: harmonik run <bead-id> [--project DIR] [--context TEXT] [--workflow-mode MODE]\n       harmonik run --beads id1,id2,... [--max-concurrent N] [--project DIR] [--context TEXT] [--workflow-mode MODE]")
	case 1:
		return []core.BeadID{core.BeadID(positional[0])}, nil
	default:
		return nil, fmt.Errorf("too many positional arguments (got %d, expected 1); use --beads for multiple", len(positional))
	}
}

type runWorkflow struct{ mode, ref string }

func selectRunWorkflow(mode, ref string) (runWorkflow, error) {
	switch mode {
	case "", "builtin":
		if ref != "" {
			return runWorkflow{}, fmt.Errorf("--workflow-ref requires --workflow-mode dot")
		}
		return runWorkflow{}, nil
	case "single":
		if ref != "" {
			return runWorkflow{}, fmt.Errorf("--workflow-ref requires --workflow-mode dot")
		}
		return runWorkflow{mode: string(core.WorkflowModeSingle)}, nil
	case core.WorkflowModeRetiredReviewLoop:
		return runWorkflow{}, fmt.Errorf("--workflow-mode review-loop was retired; use --workflow-mode dot.\n  review-loop was a hand-written implementer→reviewer cycle; dot is the general\n  graph walker it was a special case of, and the embedded standard-bead.dot\n  default already runs an implementer→reviewer→close shape.")
	case "dot":
		if ref == "" {
			return runWorkflow{}, fmt.Errorf("--workflow-mode dot requires --workflow-ref <path>")
		}
		return runWorkflow{mode: string(core.WorkflowModeDot), ref: ref}, nil
	default:
		return runWorkflow{}, fmt.Errorf("unknown --workflow-mode %q (valid: builtin, single, dot)", mode)
	}
}

func selectNotifyStream(explicit, disabled bool, beadCount, maxConcurrent int, value string) (bool, string) {
	if explicit || disabled || (beadCount <= 1 && maxConcurrent <= 1) {
		return explicit, value
	}
	return true, "-"
}

type staleQueueDecision int

const (
	staleQueueContinue staleQueueDecision = iota
	staleQueueArchive
	staleQueueRefuse
)

func classifyStaleQueue(status queue.QueueStatus, pidStatus lifecycle.PidfileLockStatus, pidMissing bool) staleQueueDecision {
	if status == queue.QueueStatusCompleted {
		return staleQueueContinue
	}
	if status == queue.QueueStatusPausedByFailure || status == queue.QueueStatusCancelled || pidMissing || pidStatus == lifecycle.PidfileLockStatusStale {
		return staleQueueArchive
	}
	return staleQueueRefuse
}
