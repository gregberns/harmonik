package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gregberns/harmonik/internal/core"
)

type decisionListItem struct {
	DecisionID     string   `json:"decision_id"`
	Question       string   `json:"question"`
	Options        []string `json:"options"`
	BlockedAgent   string   `json:"blocked_agent,omitempty"`
	ContextLink    string   `json:"context_link,omitempty"`
	ValueRequested bool     `json:"value_requested,omitempty"`
	Topic          string   `json:"topic,omitempty"`
	Urgency        string   `json:"urgency,omitempty"`
}

type decisionListResult struct {
	Decisions []decisionListItem `json:"decisions"`
}

type decisionAnswerResult struct {
	EventID string `json:"event_id,omitempty"`
	NoOp    bool   `json:"noop,omitempty"`
}

type decisionListRow struct {
	decisionListItem
	OrphanedPending bool `json:"orphaned_pending"`
}

func runDecisionsListSubcommand(subArgs []string) int {
	return runDecisionsListOrShow(subArgs, "", "list")
}

func runMailboxSubcommand(subArgs []string) int {
	jsonFlag := false
	socketFlag := ""
	projectFlag := ""

	for i := 0; i < len(subArgs); i++ {
		arg := subArgs[i]
		switch {
		case arg == "--help" || arg == "-h":
			mailboxUsage()
			return 0
		case arg == "--json":
			jsonFlag = true
		case arg == "--socket" && i+1 < len(subArgs):
			i++
			socketFlag = subArgs[i]
		case strings.HasPrefix(arg, "--socket="):
			socketFlag = strings.TrimPrefix(arg, "--socket=")
		case arg == "--project" && i+1 < len(subArgs):
			i++
			projectFlag = subArgs[i]
		case strings.HasPrefix(arg, "--project="):
			projectFlag = strings.TrimPrefix(arg, "--project=")
		case strings.HasPrefix(arg, "-"):
			fmt.Fprintf(os.Stderr, "harmonik mailbox: unknown flag %q\n", arg)
			return 1
		default:
			fmt.Fprintf(os.Stderr, "harmonik mailbox: unexpected argument %q\n", arg)
			return 1
		}
	}
	return runDecisionsListOrShowParsed("", core.DecisionTopicOperatorMailbox, jsonFlag, socketFlag, projectFlag, "mailbox")
}

func mailboxUsage() {
	fmt.Print(`harmonik mailbox — the operator mailbox (thin alias)

USAGE
  harmonik mailbox [--json] [--socket PATH] [--project DIR]

Equivalent to:
  harmonik decisions list --topic operator-mailbox

Renders every OPEN decision raised on the "operator-mailbox" topic — the
open-item set plus its count (the unread count). Reuses the SAME
durable/ordered/fsync'd/ack'd/async-answer hitl-decisions lifecycle as
"decisions list"; this is not a second bus.

EXIT CODES
  0   Success
  1   Argument error or daemon rejected the op
  17  Daemon not running (socket missing or ECONNREFUSED)
`)
}

func runDecisionsShowSubcommand(subArgs []string) int {
	jsonFlag := false
	socketFlag := ""
	projectFlag := ""
	var positional []string

	for i := 0; i < len(subArgs); i++ {
		arg := subArgs[i]
		switch {
		case arg == "--help" || arg == "-h":
			decisionsShowUsage()
			return 0
		case arg == "--json":
			jsonFlag = true
		case arg == "--socket" && i+1 < len(subArgs):
			i++
			socketFlag = subArgs[i]
		case strings.HasPrefix(arg, "--socket="):
			socketFlag = strings.TrimPrefix(arg, "--socket=")
		case arg == "--project" && i+1 < len(subArgs):
			i++
			projectFlag = subArgs[i]
		case strings.HasPrefix(arg, "--project="):
			projectFlag = strings.TrimPrefix(arg, "--project=")
		case strings.HasPrefix(arg, "-"):
			fmt.Fprintf(os.Stderr, "harmonik decisions show: unknown flag %q\n", arg)
			return 1
		default:
			positional = append(positional, arg)
		}
	}

	if len(positional) != 1 {
		fmt.Fprintf(os.Stderr, "harmonik decisions show: exactly one <decision_id> argument is required\n")
		return 1
	}
	return runDecisionsListOrShowParsed(positional[0], "", jsonFlag, socketFlag, projectFlag, "show")
}

func runDecisionsListOrShow(subArgs []string, filterID, verb string) int {
	jsonFlag := false
	socketFlag := ""
	projectFlag := ""
	topicFlag := ""

	for i := 0; i < len(subArgs); i++ {
		arg := subArgs[i]
		switch {
		case arg == "--help" || arg == "-h":
			decisionsListUsage()
			return 0
		case arg == "--json":
			jsonFlag = true
		case arg == "--topic" && i+1 < len(subArgs):
			i++
			topicFlag = subArgs[i]
		case strings.HasPrefix(arg, "--topic="):
			topicFlag = strings.TrimPrefix(arg, "--topic=")
		case arg == "--socket" && i+1 < len(subArgs):
			i++
			socketFlag = subArgs[i]
		case strings.HasPrefix(arg, "--socket="):
			socketFlag = strings.TrimPrefix(arg, "--socket=")
		case arg == "--project" && i+1 < len(subArgs):
			i++
			projectFlag = subArgs[i]
		case strings.HasPrefix(arg, "--project="):
			projectFlag = strings.TrimPrefix(arg, "--project=")
		case strings.HasPrefix(arg, "-"):
			fmt.Fprintf(os.Stderr, "harmonik decisions list: unknown flag %q\n", arg)
			return 1
		default:
			fmt.Fprintf(os.Stderr, "harmonik decisions list: unexpected argument %q\n", arg)
			return 1
		}
	}
	return runDecisionsListOrShowParsed(filterID, topicFlag, jsonFlag, socketFlag, projectFlag, verb)
}

func runDecisionsListOrShowParsed(filterID, topicFilter string, jsonFlag bool, socketFlag, projectFlag, verb string) int {
	absProject, sockPath, rc := decisionsResolvePaths(projectFlag, socketFlag, verb)
	if rc != 0 {
		return rc
	}

	listPayload := map[string]any{}
	if filterID != "" {
		listPayload["decision_id"] = filterID
	}
	if topicFilter != "" {
		listPayload["topic"] = topicFilter
	}

	resultBytes, rc := decisionsDialOp(sockPath, "decisions-list", listPayload, verb)
	if rc != 0 {
		return rc
	}

	var result decisionListResult
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik decisions %s: decode result: %v\n", verb, err)
		return 1
	}

	items := result.Decisions
	if filterID != "" {
		filtered := items[:0:0]
		for _, it := range items {
			if it.DecisionID == filterID {
				filtered = append(filtered, it)
			}
		}
		items = filtered
		if len(items) == 0 {
			fmt.Fprintf(os.Stderr, "harmonik decisions %s: no open decision with id %q\n", verb, filterID)
			return 1
		}
	}
	if topicFilter != "" {
		filtered := items[:0:0]
		for _, it := range items {
			if it.Topic == topicFilter {
				filtered = append(filtered, it)
			}
		}
		items = filtered
	}

	eventsPath := filepath.Join(absProject, ".harmonik", "events", "events.jsonl")
	rows := flagOrphanedPending(items, eventsPath)

	sort.Slice(rows, func(i, j int) bool { return rows[i].DecisionID < rows[j].DecisionID })

	if jsonFlag {
		out, err := json.Marshal(rows)
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik decisions %s: marshal json: %v\n", verb, err)
			return 1
		}
		fmt.Println(string(out))
		return 0
	}

	renderDecisionRows(rows)
	return 0
}

func flagOrphanedPending(items []decisionListItem, eventsPath string) []decisionListRow {
	registry := ComputePresenceRegistry(eventsPath)
	rows := make([]decisionListRow, 0, len(items))
	for _, it := range items {
		orphaned := false
		if it.BlockedAgent != "" {
			if rec, ok := registry[it.BlockedAgent]; ok {
				orphaned = GetPresenceState(rec) == PresenceStateOffline
			}
		}
		rows = append(rows, decisionListRow{decisionListItem: it, OrphanedPending: orphaned})
	}
	return rows
}

func renderDecisionRows(rows []decisionListRow) {
	if len(rows) == 0 {
		fmt.Println("No open decisions.")
		return
	}
	for _, r := range rows {
		flag := ""
		if r.OrphanedPending {
			flag = "  [orphaned-pending]"
		}
		blocked := r.BlockedAgent
		if blocked == "" {
			blocked = "-"
		}
		ctx := r.ContextLink
		if ctx == "" {
			ctx = "-"
		}
		urgency := ""
		if r.Urgency != "" {
			urgency = fmt.Sprintf("  [%s]", r.Urgency)
		}
		fmt.Printf("%s · %s · %s · %s · %s%s%s\n",
			r.Question,
			strings.Join(r.Options, "|"),
			blocked,
			ctx,
			r.DecisionID,
			urgency,
			flag,
		)
	}
}

func runDecisionsAnswerSubcommand(subArgs []string) int {
	valueFlag := ""
	resolverFlag := ""
	socketFlag := ""
	projectFlag := ""
	var positional []string

	for i := 0; i < len(subArgs); i++ {
		arg := subArgs[i]
		switch {
		case arg == "--help" || arg == "-h":
			decisionsAnswerUsage()
			return 0
		case arg == "--value" && i+1 < len(subArgs):
			i++
			valueFlag = subArgs[i]
		case strings.HasPrefix(arg, "--value="):
			valueFlag = strings.TrimPrefix(arg, "--value=")
		case arg == "--resolver" && i+1 < len(subArgs):
			i++
			resolverFlag = subArgs[i]
		case strings.HasPrefix(arg, "--resolver="):
			resolverFlag = strings.TrimPrefix(arg, "--resolver=")
		case arg == "--socket" && i+1 < len(subArgs):
			i++
			socketFlag = subArgs[i]
		case strings.HasPrefix(arg, "--socket="):
			socketFlag = strings.TrimPrefix(arg, "--socket=")
		case arg == "--project" && i+1 < len(subArgs):
			i++
			projectFlag = subArgs[i]
		case strings.HasPrefix(arg, "--project="):
			projectFlag = strings.TrimPrefix(arg, "--project=")
		case strings.HasPrefix(arg, "-"):
			fmt.Fprintf(os.Stderr, "harmonik decisions answer: unknown flag %q\n", arg)
			return 1
		default:
			positional = append(positional, arg)
		}
	}

	if len(positional) != 2 {
		fmt.Fprintf(os.Stderr, "harmonik decisions answer: exactly two arguments are required: <decision_id> <option>\n")
		return 1
	}
	decisionID := positional[0]
	chosenOption := positional[1]

	resolver := resolverFlag
	if resolver == "" {
		resolver = "operator"
	}

	_, sockPath, rc := decisionsResolvePaths(projectFlag, socketFlag, "answer")
	if rc != 0 {
		return rc
	}

	answerPayload := map[string]any{
		"decision_id":   decisionID,
		"chosen_option": chosenOption,
		"resolver":      resolver,
	}
	if valueFlag != "" {
		answerPayload["value"] = valueFlag
	}

	resultBytes, rc := decisionsDialOp(sockPath, "decisions-answer", answerPayload, "answer")
	if rc != 0 {
		return rc
	}

	var result decisionAnswerResult
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik decisions answer: decode result: %v\n", err)
		return 1
	}

	if result.NoOp {
		fmt.Printf("no-op: decision %s is unknown or already answered (no change)\n", decisionID)
		return 0
	}
	fmt.Println(result.EventID)
	return 0
}

func decisionsListUsage() {
	fmt.Print(`harmonik decisions list — the cross-agent "what-needs-me" queue

USAGE
  harmonik decisions list [--json] [--topic TOPIC] [--socket PATH] [--project DIR]

Renders every OPEN decision across all agents/works as
  question · options · blocked_agent · context_link · decision_id [urgency]

--topic TOPIC narrows the result to decisions raised with that exact topic
(e.g. --topic operator-mailbox — equivalent to "harmonik mailbox").

An open decision whose blocked_agent is Offline (past the ~10-min presence
cutoff, not merely Stale) is flagged "orphaned-pending" (display only — no event
is emitted; the keeper tick reaps it). --json emits a machine-readable array.

This is a PURE read of the open-decision projection — it renders with no
aggregator process running.

EXIT CODES
  0   Success
  1   Argument error or daemon rejected the op
  17  Daemon not running (socket missing or ECONNREFUSED)
`)
}

func decisionsShowUsage() {
	fmt.Print(`harmonik decisions show — show one open decision by id

USAGE
  harmonik decisions show <decision_id> [--json] [--socket PATH] [--project DIR]

Equivalent to "decisions list" filtered to a single decision_id, with the same
orphaned-pending flag. Exit 1 if no open decision has that id.

EXIT CODES
  0   Success
  1   Argument error, unknown id, or daemon rejected the op
  17  Daemon not running (socket missing or ECONNREFUSED)
`)
}

func decisionsAnswerUsage() {
	fmt.Print(`harmonik decisions answer — resolve an open decision

USAGE
  harmonik decisions answer <decision_id> <option> [--value <text>]
                            [--resolver <name>] [--socket PATH] [--project DIR]

Emits decision_resolved for <decision_id> with the chosen <option>, which MUST
be one of that decision's options (rejected otherwise). Resolving an unknown or
already-answered decision_id is a no-op (exit 0, no event) — first-writer-wins.

FLAGS
  --value TEXT      Optional free-text answer (v1.1 hook; ignored in v1 parsing).
  --resolver NAME   Who answered (default: operator).
  --socket PATH     Override socket path (default: <project>/.harmonik/daemon.sock).
  --project DIR     Project directory (default: cwd).

EXIT CODES
  0   Success (event_id printed) or no-op on unknown/answered id
  1   Argument error, bad option (not in the decision's options), or op rejected
  17  Daemon not running (socket missing or ECONNREFUSED)
`)
}
