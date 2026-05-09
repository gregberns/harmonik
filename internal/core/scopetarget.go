package core

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ScopeTargetKind is the discriminator for the four ScopeTarget syntactic shapes
// defined in specs/control-points.md §6.1.4.
type ScopeTargetKind string

const (
	// ScopeTargetKindWildcard represents the "*" shape: matches every target of
	// the declared scope.
	ScopeTargetKindWildcard ScopeTargetKind = "wildcard"

	// ScopeTargetKindPredicate represents the "node_type:<type>" shape: matches
	// all targets with the declared node_type attribute.
	ScopeTargetKindPredicate ScopeTargetKind = "predicate"

	// ScopeTargetKindList represents the ["<id-1>", "<id-2>", ...] shape: matches
	// the explicitly enumerated targets.
	ScopeTargetKindList ScopeTargetKind = "list"

	// ScopeTargetKindSingleton represents the "<single-id>" shape: matches a
	// single role name, run_id, or state_id per the enclosing BudgetScope.
	ScopeTargetKindSingleton ScopeTargetKind = "singleton"
)

// ScopeTarget is the constrained string-or-list primitive for the scope_target
// field of BudgetPayload (specs/control-points.md §6.1.4 RECORD ScopeTarget).
//
// The spec declares four mutually exclusive syntactic shapes:
//
//	"*"                              wildcard   — matches every target of the declared scope
//	"node_type:<type>"               predicate  — matches all targets with declared attribute
//	["<id-1>", "<id-2>", ...]        list       — matches the enumerated targets
//	"<single-id>"                    singleton  — role name | run_id | state_id per scope
//
// JSON serialises as the bare string or array that the spec shows — no wrapping
// object. The Kind field is the in-memory discriminator and is NOT written to
// the wire representation.
//
// Constructors: ScopeTargetWildcard, ScopeTargetPredicate, ScopeTargetList,
// ScopeTargetSingleton. Do not build the zero value directly; it is invalid.
type ScopeTarget struct {
	// Kind is the in-memory discriminator. Not serialised.
	Kind ScopeTargetKind

	// PredicateType holds the "<type>" portion from "node_type:<type>" when
	// Kind == ScopeTargetKindPredicate. Empty for all other kinds.
	PredicateType string

	// IDs holds the list elements when Kind == ScopeTargetKindList or the
	// single identifier when Kind == ScopeTargetKindSingleton. Nil/empty for
	// ScopeTargetKindWildcard and ScopeTargetKindPredicate.
	IDs []string
}

// ScopeTargetWildcard returns the wildcard ScopeTarget ("*").
func ScopeTargetWildcard() ScopeTarget {
	return ScopeTarget{Kind: ScopeTargetKindWildcard}
}

// ScopeTargetPredicate returns a predicate ScopeTarget ("node_type:<nodeType>").
// nodeType must be non-empty; it is the value after the "node_type:" prefix.
func ScopeTargetPredicate(nodeType string) (ScopeTarget, error) {
	if nodeType == "" {
		return ScopeTarget{}, fmt.Errorf("scopetarget: predicate node_type must not be empty")
	}
	return ScopeTarget{Kind: ScopeTargetKindPredicate, PredicateType: nodeType}, nil
}

// ScopeTargetList returns a list ScopeTarget from the given IDs.
// ids must contain at least one element; all elements must be non-empty.
func ScopeTargetList(ids []string) (ScopeTarget, error) {
	if len(ids) == 0 {
		return ScopeTarget{}, fmt.Errorf("scopetarget: list must contain at least one id")
	}
	for i, id := range ids {
		if id == "" {
			return ScopeTarget{}, fmt.Errorf("scopetarget: list element %d must not be empty", i)
		}
	}
	cp := make([]string, len(ids))
	copy(cp, ids)
	return ScopeTarget{Kind: ScopeTargetKindList, IDs: cp}, nil
}

// ScopeTargetSingleton returns a singleton ScopeTarget for the given id.
// id must be non-empty.
func ScopeTargetSingleton(id string) (ScopeTarget, error) {
	if id == "" {
		return ScopeTarget{}, fmt.Errorf("scopetarget: singleton id must not be empty")
	}
	return ScopeTarget{Kind: ScopeTargetKindSingleton, IDs: []string{id}}, nil
}

// Valid reports whether the ScopeTarget is structurally well-formed:
//   - Kind must be one of the four declared constants.
//   - Wildcard: IDs must be nil/empty and PredicateType must be empty.
//   - Predicate: PredicateType must be non-empty; IDs must be nil/empty.
//   - List: IDs must have at least one non-empty element; PredicateType must be empty.
//   - Singleton: IDs must contain exactly one non-empty element; PredicateType must be empty.
func (s ScopeTarget) Valid() bool {
	switch s.Kind {
	case ScopeTargetKindWildcard:
		return s.PredicateType == "" && len(s.IDs) == 0
	case ScopeTargetKindPredicate:
		return s.PredicateType != "" && len(s.IDs) == 0
	case ScopeTargetKindList:
		if s.PredicateType != "" || len(s.IDs) == 0 {
			return false
		}
		for _, id := range s.IDs {
			if id == "" {
				return false
			}
		}
		return true
	case ScopeTargetKindSingleton:
		return s.PredicateType == "" && len(s.IDs) == 1 && s.IDs[0] != ""
	default:
		return false
	}
}

const predicatePrefix = "node_type:"

// MarshalJSON implements json.Marshaler.
//
// The wire representation follows the spec literally:
//   - Wildcard   → "\"*\""
//   - Predicate  → "\"node_type:<type>\""
//   - List       → "[\"<id-1>\", \"<id-2>\", ...]"
//   - Singleton  → "\"<id>\""
//
// Rejects invalid ScopeTarget values.
func (s ScopeTarget) MarshalJSON() ([]byte, error) {
	if !s.Valid() {
		return nil, fmt.Errorf("scopetarget: cannot marshal invalid value (kind=%q)", string(s.Kind))
	}
	switch s.Kind {
	case ScopeTargetKindWildcard:
		return json.Marshal("*")
	case ScopeTargetKindPredicate:
		return json.Marshal(predicatePrefix + s.PredicateType)
	case ScopeTargetKindList:
		return json.Marshal(s.IDs)
	case ScopeTargetKindSingleton:
		return json.Marshal(s.IDs[0])
	default:
		return nil, fmt.Errorf("scopetarget: unknown kind %q", string(s.Kind))
	}
}

// UnmarshalJSON implements json.Unmarshaler.
//
// It accepts the four wire shapes the spec defines:
//   - JSON string "\"*\""                        → ScopeTargetKindWildcard
//   - JSON string "\"node_type:<type>\""          → ScopeTargetKindPredicate
//   - JSON array  "[\"<id-1>\", ...]"             → ScopeTargetKindList
//   - JSON string "\"<single-id>\""               → ScopeTargetKindSingleton
//
// All other inputs are rejected.
func (s *ScopeTarget) UnmarshalJSON(data []byte) error {
	// Try array first.
	if len(data) > 0 && data[0] == '[' {
		var ids []string
		if err := json.Unmarshal(data, &ids); err != nil {
			return fmt.Errorf("scopetarget: cannot parse list: %w", err)
		}
		v, err := ScopeTargetList(ids)
		if err != nil {
			return fmt.Errorf("scopetarget: invalid list: %w", err)
		}
		*s = v
		return nil
	}

	// Try string shapes.
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("scopetarget: expected string or array, got %s", string(data))
	}

	switch {
	case raw == "*":
		*s = ScopeTargetWildcard()
	case strings.HasPrefix(raw, predicatePrefix):
		nodeType := strings.TrimPrefix(raw, predicatePrefix)
		v, err := ScopeTargetPredicate(nodeType)
		if err != nil {
			return fmt.Errorf("scopetarget: invalid predicate: %w", err)
		}
		*s = v
	case raw != "":
		v, err := ScopeTargetSingleton(raw)
		if err != nil {
			return fmt.Errorf("scopetarget: invalid singleton: %w", err)
		}
		*s = v
	default:
		return fmt.Errorf("scopetarget: empty string is not a valid shape")
	}
	return nil
}
