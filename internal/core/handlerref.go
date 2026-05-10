package core

import "fmt"

// HandlerRef is the typed string identifier for a handler class registered under
// the handler-contract conformance-class taxonomy (handler-contract.md §6.1).
//
// A HandlerRef value names a handler class that can be resolved to a registered
// Handler implementation per [handler-contract.md §4.1 HC-001]. It is carried in:
//
//   - Node.HandlerRef for agentic workflow nodes (execution-model.md §6.1) — the
//     handler class the daemon dispatches when the node executes.
//   - Workspace.ImplementerHandlerRef (workspace-model.md §6.1) — the handler
//     class of the most-recent agentic session sidecar, used for merge-conflict
//     re-dispatch per §4.6.WM-022.
//
// The handler-contract spec (handler-contract.md §6.1) and workspace-model spec
// (workspace-model.md §6.1, §4.6.WM-022) declare HandlerRef as a non-empty
// String with no closed enum at MVH; validation requires only non-empty. The
// identifier format follows the lowercase-hyphenated agent_type convention per
// [architecture.md §6.1] (e.g., "agentic-claude", "non-agentic").
//
// Mechanical / non-agentic handler classes (non-agentic, generator, merge-node)
// are valid HandlerRef values but do NOT supply implementer_handler_ref for
// conflict resolution per WM-022; only agentic classes do. The agentic vs.
// non-agentic distinction is owned by the agent-type taxonomy (architecture.md
// §6.1); HandlerRef itself does not re-declare it.
//
// HandlerRef is a named type (not a Go alias) so it is not interchangeable with
// raw string at the call site. Callers obtaining handler_ref values from DOT
// node attributes MUST convert through this type so that the type system enforces
// that every handler reference has been validated.
type HandlerRef string

// Valid reports whether h is a non-empty HandlerRef string.
// Empty values are rejected; all non-empty strings are accepted.
func (h HandlerRef) Valid() bool {
	return h != ""
}

// MarshalText implements encoding.TextMarshaler so HandlerRef serialises
// correctly in JSON and YAML.
// It rejects empty values.
func (h HandlerRef) MarshalText() ([]byte, error) {
	if !h.Valid() {
		return nil, fmt.Errorf("handlerref: value must not be empty")
	}
	return []byte(h), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects empty strings; all non-empty strings are accepted.
func (h *HandlerRef) UnmarshalText(text []byte) error {
	v := HandlerRef(text)
	if !v.Valid() {
		return fmt.Errorf("handlerref: value must not be empty")
	}
	*h = v
	return nil
}
