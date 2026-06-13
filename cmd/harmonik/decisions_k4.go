package main

// decisions_k4.go — the OPERATOR-side `harmonik decisions` verbs (hitl-decisions
// component K4, bead hk-kba):
//
//   - list   → decisions-list daemon op: render the cross-agent "what-needs-me"
//              queue — every open decision as
//              `question · options · blocked_agent · context_link · decision_id`
//              (SPEC §2, S2). --json emits machine-readable output.
//   - show   → list filtered to one decision_id (client-side filter; SPEC §2).
//   - answer → decisions-answer daemon op: validate the decision is OPEN and the
//              option is one of its options (N7), emit decision_resolved; no-op
//              on an unknown/already-terminal id (N3 first-writer-wins).
//
// Orphaned-pending flag (N9, read-pure — NO EMIT). An open decision whose
// blocked_agent is OFFLINE (past the ~10-min presenceStaleCutoff, NOT merely
// Stale) is flagged "orphaned-pending" in the list/show output — DISPLAY ONLY.
// Why here and not in the daemon op: agent presence is computable ONLY in this
// (cmd/harmonik, package main) layer — ComputePresenceRegistry / GetPresenceState
// / the presenceStaleCutoff (10m) live in comms.go and there is no daemon-side
// presence projection. The daemon decisions-list op therefore returns the raw
// open set, and this CLI computes the Offline → orphaned-pending flag from the
// SAME events.jsonl, read-pure (no socket write, no event). That keeps the list
// op a pure projection (S6) and satisfies N9's read-side half (flag only; the
// keeper tick in K5 does the actual decision_withdrawn(orphaned) emission, and
// K5 reuses this SAME presence source — see the note at the bottom of this file).
//
// EXACT Offline determination used for orphaned-pending:
//   ComputePresenceRegistry(eventsPath) → PresenceRecord per agent;
//   GetPresenceState(rec) == PresenceStateOffline  ⟺  the blocked_agent emitted
//   an explicit leave beat OR its effective_last_seen is ≥ presenceStaleCutoff
//   (10 * time.Minute). A Stale agent (120s ≤ age < 10m) is NOT flagged — it is
//   presumed still-blocked (SPEC §5 / N9). An agent with NO presence record at
//   all (never seen) is treated as NOT-offline for flagging purposes: absence of
//   a record means we have no evidence the agent is gone, and flagging on bare
//   absence would over-flag freshly-raised decisions; the keeper tick (K5) is the
//   single source of truth for actual reaping and applies its own predicate.
//
// Spec ref: ~/.kerf/projects/gregberns-harmonik/hitl-decisions/SPEC.md §2, §3, §5, §6 (N3,N6,N7,N9).
// Bead ref: hk-kba (component K4).

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// decisionListItem mirrors the daemon's DecisionsListItem (the package boundary
// forbids importing internal/daemon, so we decode into a local shape).
type decisionListItem struct {
	DecisionID     string   `json:"decision_id"`
	Question       string   `json:"question"`
	Options        []string `json:"options"`
	BlockedAgent   string   `json:"blocked_agent,omitempty"`
	ContextLink    string   `json:"context_link,omitempty"`
	ValueRequested bool     `json:"value_requested,omitempty"`
}

// decisionListResult mirrors the daemon's DecisionsListResult.
type decisionListResult struct {
	Decisions []decisionListItem `json:"decisions"`
}

// decisionAnswerResult mirrors the daemon's DecisionsAnswerResult.
type decisionAnswerResult struct {
	EventID string `json:"event_id,omitempty"`
	NoOp    bool   `json:"noop,omitempty"`
}

// decisionListRow is one rendered row: a decision plus its computed
// orphaned-pending flag (display-only, N9 read-pure).
type decisionListRow struct {
	decisionListItem
	OrphanedPending bool `json:"orphaned_pending"`
}

// -----------------------------------------------------------------------------
// list / show
// -----------------------------------------------------------------------------

// runDecisionsListSubcommand implements `harmonik decisions list`.
// subArgs is os.Args[3:].
func runDecisionsListSubcommand(subArgs []string) int {
	return runDecisionsListOrShow(subArgs, "", "list")
}

// runDecisionsShowSubcommand implements `harmonik decisions show <decision_id>`
// = `list` filtered to one decision_id (client-side filter). subArgs is os.Args[3:].
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
	return runDecisionsListOrShowParsed(positional[0], jsonFlag, socketFlag, projectFlag, "show")
}

// runDecisionsListOrShow parses the list-verb flags then renders. filterID is the
// optional single-decision filter ("" = all). verb is "list" or "show".
func runDecisionsListOrShow(subArgs []string, filterID, verb string) int {
	jsonFlag := false
	socketFlag := ""
	projectFlag := ""

	for i := 0; i < len(subArgs); i++ {
		arg := subArgs[i]
		switch {
		case arg == "--help" || arg == "-h":
			decisionsListUsage()
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
			fmt.Fprintf(os.Stderr, "harmonik decisions list: unknown flag %q\n", arg)
			return 1
		default:
			fmt.Fprintf(os.Stderr, "harmonik decisions list: unexpected argument %q\n", arg)
			return 1
		}
	}
	return runDecisionsListOrShowParsed(filterID, jsonFlag, socketFlag, projectFlag, verb)
}

// runDecisionsListOrShowParsed dials decisions-list, computes the orphaned-pending
// flag client-side from agent presence, and renders (text or JSON).
func runDecisionsListOrShowParsed(filterID string, jsonFlag bool, socketFlag, projectFlag, verb string) int {
	absProject, sockPath, rc := decisionsResolvePaths(projectFlag, socketFlag, verb)
	if rc != 0 {
		return rc
	}

	// The daemon list op returns the raw open set; pass the filter id through so
	// `show` narrows server-side too (the CLI also filters, so either is fine).
	listPayload := map[string]any{}
	if filterID != "" {
		listPayload["decision_id"] = filterID
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

	// Client-side filter for `show` (belt-and-suspenders if the daemon returns all).
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

	// Compute orphaned-pending (N9, read-pure): flag any open decision whose
	// blocked_agent is OFFLINE per the SAME events.jsonl presence projection.
	// NO socket write, NO event — display only.
	eventsPath := filepath.Join(absProject, ".harmonik", "events", "events.jsonl")
	rows := flagOrphanedPending(items, eventsPath)

	// Deterministic order (by decision_id) for stable output + testability.
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

// flagOrphanedPending returns the items as rows, flagging each whose blocked_agent
// is OFFLINE (GetPresenceState == PresenceStateOffline) per the presence registry
// over eventsPath. This is the N9 read-pure flag: presence is computed from the
// durable log, NO event is emitted. A blocked_agent that is empty, has no presence
// record, or is Online/Stale is NOT flagged (only a confirmed Offline is).
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

// renderDecisionRows prints the what-needs-me queue in human-readable form.
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
		fmt.Printf("%s · %s · %s · %s · %s%s\n",
			r.Question,
			strings.Join(r.Options, "|"),
			blocked,
			ctx,
			r.DecisionID,
			flag,
		)
	}
}

// -----------------------------------------------------------------------------
// answer
// -----------------------------------------------------------------------------

// runDecisionsAnswerSubcommand implements
// `harmonik decisions answer <decision_id> <option> [--value <text>] [--resolver <name>]`.
// subArgs is os.Args[3:].
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

	// Default resolver to "operator" when unspecified (this is the human-answerer
	// surface; SPEC §9 single-human-answerer).
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

	// N3 first-writer-wins: an unknown/already-terminal decision_id is a no-op —
	// no error (exit 0), a clear note to the operator.
	if result.NoOp {
		fmt.Printf("no-op: decision %s is unknown or already answered (no change)\n", decisionID)
		return 0
	}
	fmt.Println(result.EventID)
	return 0
}

// -----------------------------------------------------------------------------
// usage
// -----------------------------------------------------------------------------

func decisionsListUsage() {
	fmt.Print(`harmonik decisions list — the cross-agent "what-needs-me" queue

USAGE
  harmonik decisions list [--json] [--socket PATH] [--project DIR]

Renders every OPEN decision across all agents/works as
  question · options · blocked_agent · context_link · decision_id

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
