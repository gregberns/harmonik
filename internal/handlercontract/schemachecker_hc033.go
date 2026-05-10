package handlercontract

import (
	"fmt"
	"reflect"
	"strings"
)

// schemaChecker — per-bead helper prefix for test helpers in
// schemachecker_hc033_test.go (implementer-protocol.md §Helper-prefix
// discipline; bead hk-8i31.40).

// CheckPayloadSchema inspects every exported field of the payload struct type
// rooted at prototype and returns an error if any field's effective JSON name
// matches the HC-031 common-prefix redaction regex.
//
// # Purpose
//
// HC-033 requires the event-schema registry to verify at startup that no
// registered event type's payload schema declares a field whose name matches the
// HC-031 regex.  Any such field MUST be a startup-time error, not a runtime
// warning.  This prevents schema drift that would silently ship unredacted
// secrets: if a payload struct names a field "secret" or "token", the watcher's
// common-prefix redaction middleware (hk-8i31.37) would never see the value
// because the JSON key name was never in the redaction path.
//
// # What counts as a "field name"
//
// For each exported struct field:
//
//  1. If the field has a `json:"-"` tag the field is skipped (it is omitted from
//     JSON encoding and is not observable).
//  2. If the field has a `json:"<name>[,...]"` tag the effective name is the
//     first comma-separated token.
//  3. If the field has no `json` tag the effective name is the Go field name.
//
// The check is case-insensitive (the HC-031 regex uses the (?i) flag).
//
// # Recursion
//
// CheckPayloadSchema recurses into embedded and named struct fields (both
// pointer and value forms), using a visited-type set to prevent infinite
// recursion on self-referential schemas.
//
// # prototype
//
// prototype MUST be a non-nil value of the struct type to check. Passing a
// pointer is fine (the function dereferences one level). Passing a non-struct
// value returns an error. Passing nil returns an error.
//
// # Return value
//
// Returns nil when no field violates HC-031.
// Returns a non-nil error describing the first violation found when any field
// name matches the HC-031 regex.
//
// Callers at daemon startup MUST treat a non-nil return as a fatal error and
// MUST NOT proceed with daemon initialisation.
//
// Spec: specs/handler-contract.md §4.7.HC-033.
func CheckPayloadSchema(prototype interface{}) error {
	if prototype == nil {
		return fmt.Errorf("handlercontract: CheckPayloadSchema: prototype is nil")
	}
	t := reflect.TypeOf(prototype)
	// Dereference one level of pointer.
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return fmt.Errorf(
			"handlercontract: CheckPayloadSchema: prototype must be a struct (or pointer to struct), got %s",
			t.Kind(),
		)
	}
	visited := make(map[reflect.Type]bool)
	return checkStructFields(t, visited, "")
}

// checkStructFields recursively checks all exported fields of structType.
// visited prevents infinite recursion on self-referential types.
// path is the dotted path of field names for error messages (empty at the top level).
func checkStructFields(structType reflect.Type, visited map[reflect.Type]bool, path string) error {
	if visited[structType] {
		return nil
	}
	visited[structType] = true

	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		if !field.IsExported() {
			continue
		}

		// Determine the effective JSON name for this field.
		jsonName := effectiveJSONName(field)
		if jsonName == "" {
			// json:"-" tag: field is omitted from JSON; not visible to consumers.
			continue
		}

		// Build the path for error messages.
		fieldPath := jsonName
		if path != "" {
			fieldPath = path + "." + jsonName
		}

		// Check the effective JSON name against the HC-031 regex.
		if redactionCommonPrefixRe.MatchString(jsonName) {
			return fmt.Errorf(
				"handlercontract: CheckPayloadSchema: field %q has effective JSON name %q "+
					"which matches the HC-031 common-prefix redaction regex "+
					"(secret|token|password|api[_-]?key|auth); "+
					"registering an event type with this payload schema MUST be a startup-time error "+
					"per specs/handler-contract.md §4.7.HC-033",
				fieldPath, jsonName,
			)
		}

		// Recurse into embedded and named struct fields.
		ft := field.Type
		if ft.Kind() == reflect.Ptr {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Struct {
			if err := checkStructFields(ft, visited, fieldPath); err != nil {
				return err
			}
		}
	}
	return nil
}

// effectiveJSONName returns the effective JSON field name for field.
//
// Rules (mirroring encoding/json behavior):
//   - json:"-"              → "" (field is omitted; caller should skip)
//   - json:"name[,options]" → "name"
//   - no json tag           → field.Name
func effectiveJSONName(field reflect.StructField) string {
	tag := field.Tag.Get("json")
	if tag == "" {
		// No json tag: effective name is the Go field name.
		return field.Name
	}
	// Split on comma to isolate the name portion.
	name, _, _ := strings.Cut(tag, ",")
	if name == "-" {
		// json:"-": field is explicitly omitted.
		return ""
	}
	if name == "" {
		// json:",omitempty" or similar with no explicit name → Go field name.
		return field.Name
	}
	return name
}
