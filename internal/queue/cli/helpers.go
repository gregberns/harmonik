package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/gregberns/harmonik/internal/queue"
)

func parseQueueFlags(subArgs []string, errOut io.Writer) (projectDir string, positional []string, outputJSON, ok bool) {
	return parseQueueFlagsExtra(subArgs, errOut, nil)
}

func parseQueueFlagsExtra(
	subArgs []string,
	errOut io.Writer,
	extraFlagFn func(args []string, i int) (nextI int, consumed bool),
) (projectDir string, positional []string, outputJSON, ok bool) {
	diag := newPrinter(errOut)
	for i := 0; i < len(subArgs); {
		arg := subArgs[i]
		switch {
		case arg == "--project" && i+1 < len(subArgs):
			projectDir = subArgs[i+1]
			i += 2
		case strings.HasPrefix(arg, "--project="):
			projectDir = strings.TrimPrefix(arg, "--project=")
			i++
		case arg == "--json":
			outputJSON = true
			i++
		case arg == "--format" && i+1 < len(subArgs):
			outputJSON = subArgs[i+1] == "json"
			i += 2
		case strings.HasPrefix(arg, "--format="):
			outputJSON = strings.TrimPrefix(arg, "--format=") == "json"
			i++
		default:
			if extraFlagFn != nil {
				nextI, consumed := extraFlagFn(subArgs, i)
				if consumed {
					i = nextI
					continue
				}
			}
			if len(arg) > 1 && arg[0] == '-' {
				diag.printf("harmonik queue: unrecognized flag %q\n", arg)
				return "", nil, false, false
			}
			positional = append(positional, arg)
			i++
		}
	}

	if projectDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			diag.printf("harmonik queue: cannot determine working directory: %v\n", err)
			return "", nil, false, false
		}
		projectDir = wd
	}
	abs, err := filepath.Abs(projectDir)
	if err != nil {
		diag.printf("harmonik queue: cannot resolve project path %q: %v\n", projectDir, err)
		return "", nil, false, false
	}
	return abs, positional, outputJSON, true
}

func harmonikDirFromProject(projectDir string, errOut io.Writer) string {
	diag := newPrinter(errOut)
	if _, err := os.Stat(projectDir); err != nil {
		diag.printf("harmonik queue: project directory %q not accessible: %v\n", projectDir, err)
		return ""
	}
	return filepath.Join(projectDir, ".harmonik")
}

func buildEnvelope(op string, fields map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	opBytes, err := json.Marshal(op)
	if err != nil {
		return nil, fmt.Errorf("encode op %q: %w", op, err)
	}
	out := make(map[string]json.RawMessage, len(fields)+1)
	out["op"] = opBytes
	for k, v := range fields {
		out[k] = v
	}
	return out, nil
}

func encodeEnvelope(op string, doc map[string]json.RawMessage) ([]byte, error) {
	envelope, err := buildEnvelope(op, doc)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("marshal %s envelope: %w", op, err)
	}
	return payload, nil
}

func loadQueueDocFromFile(verb, queueFile, queueName string, diag *printer) (doc map[string]json.RawMessage, ok bool) {
	//nolint:gosec // G304: path comes from operator CLI argument
	data, readErr := os.ReadFile(queueFile)
	if readErr != nil {
		diag.printf("harmonik queue %s: cannot read %q: %v\n", verb, queueFile, readErr)
		return nil, false
	}
	if jsonErr := json.Unmarshal(data, &doc); jsonErr != nil {
		diag.printf("harmonik queue %s: invalid JSON in %q: %v\n", verb, queueFile, jsonErr)
		return nil, false
	}
	if normErr := normalizeQueueDocGroups(doc, diag); normErr != nil {
		diag.printf("harmonik queue %s: cannot normalize group kinds: %v\n", verb, normErr)
		return nil, false
	}
	if queueName == "" {
		return doc, true
	}
	nameBytes, nameErr := json.Marshal(queueName)
	if nameErr != nil {
		diag.printf("harmonik queue %s: cannot encode --queue name %q: %v\n", verb, queueName, nameErr)
		return nil, false
	}
	doc["name"] = nameBytes
	return doc, true
}

func marshalJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}

func normalizeQueueDocGroups(doc map[string]json.RawMessage, diag *printer) error {
	groupsRaw, ok := doc["groups"]
	if !ok {
		return nil
	}
	var groups []map[string]json.RawMessage
	if err := json.Unmarshal(groupsRaw, &groups); err != nil {
		return err
	}
	waved := false
	for i, g := range groups {
		kindRaw, hasKind := g["kind"]
		kindStr := strings.Trim(string(kindRaw), `"`)
		if !hasKind || kindStr == "" || string(kindRaw) == "null" {
			g["kind"] = json.RawMessage(`"stream"`)
			groups[i] = g
		} else if kindStr == "wave" {
			waved = true
		}
	}
	if waved {
		diag.println("harmonik queue submit: warning: wave group(s) detected — waves are immutable " +
			"and trigger single-active lockout (QM-027) on a shared daemon; use kind:stream for the daily loop")
	}
	normalized, err := json.Marshal(groups)
	if err != nil {
		return err
	}
	doc["groups"] = normalized
	return nil
}

func beadsToQueueDoc(beadIDs []string, queueName, workflowMode string) (map[string]json.RawMessage, error) {
	type itemDoc struct {
		BeadID       string `json:"bead_id"`
		Status       string `json:"status"`
		WorkflowMode string `json:"workflow_mode,omitempty"`
	}
	type groupDoc struct {
		GroupIndex int       `json:"group_index"`
		Kind       string    `json:"kind"`
		Status     string    `json:"status"`
		Items      []itemDoc `json:"items"`
	}
	type queueDoc struct {
		SchemaVersion int        `json:"schema_version"`
		Name          string     `json:"name,omitempty"`
		Groups        []groupDoc `json:"groups"`
	}

	items := make([]itemDoc, len(beadIDs))
	pendingItem := queue.NewPendingItem(queue.Item{})
	for i, id := range beadIDs {
		items[i] = itemDoc{BeadID: id, Status: string(pendingItem.Status), WorkflowMode: workflowMode}
	}
	pendingGroup := queue.NewPendingGroup(queue.Group{})
	doc := queueDoc{
		SchemaVersion: 1,
		Name:          queueName,
		Groups: []groupDoc{
			{GroupIndex: 0, Kind: "stream", Status: string(pendingGroup.Status), Items: items},
		},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func parseBeadsFlag(raw string) []string {
	var ids []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			ids = append(ids, part)
		}
	}
	return ids
}
