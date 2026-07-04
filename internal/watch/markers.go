package watch

// markers.go — T12: watch marker-check on the event stream.
//
// Watch checks each type's declared markers.never_emits against the event stream
// (not transcripts). On a violation it returns a friendly reminder string at
// PULL-DIGEST severity — callers pass it to appendDigestFlag, not SendEscalation.
//
// Spec: .kerf/works/agent-manifest/SPEC.md §2 (markers field) + 07-tasks.md T12.
// Declarative-only wiring: manifest.yaml is the truth; no transcript grepping.

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/gregberns/harmonik/internal/agentmanifest"
	"github.com/gregberns/harmonik/internal/core"
)

// MarkerChecker checks events against declared markers.never_emits constraints.
//
// Rules are loaded once from the agents directory at construction time and are
// read-only after that — safe to share across goroutines.
type MarkerChecker struct {
	// rules maps typeName -> set of marker patterns in that type's never_emits.
	// Each pattern is either a bare event type ("crew_start") or a qualified
	// type:qualifier pair ("queue_submit:main").
	rules map[string]map[string]struct{}

	// typeNames is the set of known type folder names.
	// Used to distinguish singleton agents (captain, watch, admiral — named by type)
	// from crew instances (named by instance; resolve to "crew").
	typeNames map[string]struct{}
}

// NewMarkerChecker loads all agent type manifests from agentsDir and builds the
// never_emits lookup table.
//
// agentsDir is the absolute path to the .harmonik/agents/ directory.
// A missing directory is not an error — an empty checker is returned (no
// violations possible until manifests are authored).
// Invalid or unreadable manifests are skipped defensively.
func NewMarkerChecker(agentsDir string) (*MarkerChecker, error) {
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return &MarkerChecker{
				rules:     make(map[string]map[string]struct{}),
				typeNames: make(map[string]struct{}),
			}, nil
		}
		return nil, fmt.Errorf("markers: read agents dir %q: %w", agentsDir, err)
	}

	rules := make(map[string]map[string]struct{})
	typeNames := make(map[string]struct{})

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, "_") {
			continue // reserved (_skills, etc.)
		}
		tf, loadErr := agentmanifest.Load(agentsDir, name)
		if loadErr != nil {
			continue // skip unreadable/invalid manifests
		}
		typeNames[name] = struct{}{}
		if len(tf.Manifest.Markers.NeverEmits) == 0 {
			continue
		}
		markerSet := make(map[string]struct{}, len(tf.Manifest.Markers.NeverEmits))
		for _, marker := range tf.Manifest.Markers.NeverEmits {
			markerSet[marker] = struct{}{}
		}
		rules[name] = markerSet
	}

	return &MarkerChecker{rules: rules, typeNames: typeNames}, nil
}

// Check examines ev against the loaded never_emits rules.
//
// If the event's emitter type has declared the event type (or a matching
// qualified pattern) as never_emits, it returns a friendly reminder string.
// Returns "" if no violation is detected or the emitter type cannot be resolved.
//
// The returned string is PULL-DIGEST severity: callers should pass it to
// EscalationEngine.appendDigestFlag rather than SendEscalation.
func (mc *MarkerChecker) Check(ev core.Event) string {
	emitterName, emitterType := mc.resolveEmitter(ev)
	if emitterType == "" {
		return ""
	}
	markerSet, ok := mc.rules[emitterType]
	if !ok {
		return "" // type has no never_emits constraints
	}

	// Build the qualified key first (e.g. "queue_submit:main").
	qualifiedKey := mc.qualifiedKey(ev)
	violated := ""
	if qualifiedKey != "" {
		if _, hit := markerSet[qualifiedKey]; hit {
			violated = qualifiedKey
		}
	}
	// Fall through to bare event type if qualified key didn't match.
	if violated == "" {
		if _, hit := markerSet[ev.Type]; hit {
			violated = ev.Type
		}
	}
	if violated == "" {
		return ""
	}

	return fmt.Sprintf(
		"Marker reminder: agent %q (type %s) emitted event %q, which its manifest declares as never_emits.",
		emitterName, emitterType, violated,
	)
}

// resolveEmitter extracts the emitter name and type from the event payload.
//
// Resolution:
//  1. Extract the 'from' (or synonymous) field from the payload.
//  2. If the name is a known type folder name, the type equals the name (singleton agents).
//  3. If the name is unrecognised, assume it is a crew instance (type = "crew").
//  4. If no name can be found, return ("", "") — the event is unskippable.
func (mc *MarkerChecker) resolveEmitter(ev core.Event) (name, typeName string) {
	name = mc.emitterName(ev)
	if name == "" {
		return "", ""
	}
	if _, known := mc.typeNames[name]; known {
		return name, name
	}
	// Unknown name → crew instance (named by instance, not by type).
	return name, "crew"
}

// emitterName extracts the first non-empty string value from a set of known
// payload fields that identify the emitting agent.
func (mc *MarkerChecker) emitterName(ev core.Event) string {
	if len(ev.Payload) == 0 {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		return ""
	}
	for _, key := range []string{"from", "agent", "crew", "assignee"} {
		if v, ok := payload[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// qualifiedKey builds the type:qualifier key for ev.
//
// For example, if ev.Type == "queue_submit" and the payload contains
// queue == "main", qualifiedKey returns "queue_submit:main".
// Returns "" when no qualifier can be derived from the payload.
func (mc *MarkerChecker) qualifiedKey(ev core.Event) string {
	if len(ev.Payload) == 0 {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		return ""
	}
	for _, key := range []string{"queue", "queue_name", "target", "lane"} {
		if v, ok := payload[key].(string); ok && v != "" {
			return ev.Type + ":" + v
		}
	}
	return ""
}
