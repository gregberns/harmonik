package scenario

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"text/template"
)

// SyntheticMatrixName renders the per-cell synthetic name for a matrix-expanded
// scenario per SH-030: <baseName>[k1=v1,k2=v2,...] with keys in byte-lexicographic
// order. The cell map must be non-empty; passing an empty cell returns baseName
// unchanged (no bracket suffix).
func SyntheticMatrixName(baseName string, cell map[string]string) string {
	if len(cell) == 0 {
		return baseName
	}
	keys := make([]string, 0, len(cell))
	for k := range cell {
		keys = append(keys, k)
	}
	sort.Strings(keys) // byte-lexicographic order per SH-030

	var sb strings.Builder
	sb.WriteString(baseName)
	sb.WriteByte('[')
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(cell[k])
	}
	sb.WriteByte(']')
	return sb.String()
}

// ExpandMatrix expands s into one ScenarioFile per matrix cell per SH-030.
// If s.Matrix is nil or produces zero cells, a single-element slice containing
// s unmodified is returned.
//
// Each expanded cell receives:
//   - A synthetic name: <s.Name>[k1=v1,...] with keys in byte-lex order.
//   - All substitutable string fields processed via Go text/template using the
//     cell key-value pairs as the template data map.
//   - s.Matrix cleared to nil (the cell is fully materialized).
//
// Errors are prefixed with "scenario-load-failure:" to indicate the §8 class.
// Failure causes: template parse/execute errors (unresolvable template, unknown
// parameter key), or matrix-cell name collisions per SH-005.
func (s ScenarioFile) ExpandMatrix() ([]ScenarioFile, error) {
	if s.Matrix == nil || matrixCellCount(s.Matrix) == 0 {
		return []ScenarioFile{s}, nil
	}

	cells := matrixCartesianProduct(s.Matrix)

	results := make([]ScenarioFile, 0, len(cells))
	seen := make(map[string]bool, len(cells))

	for _, cell := range cells {
		expanded, err := applyMatrixParams(s, cell)
		if err != nil {
			return nil, fmt.Errorf("scenario-load-failure: matrix cell %v: %w", cell, err)
		}
		expanded.Name = SyntheticMatrixName(s.Name, cell)
		expanded.Matrix = nil

		if seen[expanded.Name] {
			return nil, fmt.Errorf("scenario-load-failure: matrix expansion produces duplicate name %q (SH-005, SH-030)", expanded.Name)
		}
		seen[expanded.Name] = true

		results = append(results, expanded)
	}

	// Sort by synthetic name (byte-lexicographic) per SH-007.
	sort.Slice(results, func(i, j int) bool {
		return results[i].Name < results[j].Name
	})

	return results, nil
}

// matrixCartesianProduct generates all cells of the cartesian product for
// matrix. Keys within each cell are iterated in byte-lexicographic order so
// the per-cell maps are deterministically ordered. The slice of cells is NOT
// sorted; callers order by synthetic name for suite ordering (per ExpandMatrix).
func matrixCartesianProduct(matrix map[string][]string) []map[string]string {
	keys := make([]string, 0, len(matrix))
	for k := range matrix {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	result := []map[string]string{{}}
	for _, key := range keys {
		values := matrix[key]
		next := make([]map[string]string, 0, len(result)*len(values))
		for _, cell := range result {
			for _, val := range values {
				newCell := make(map[string]string, len(cell)+1)
				for k, v := range cell {
					newCell[k] = v
				}
				newCell[key] = val
				next = append(next, newCell)
			}
		}
		result = next
	}
	return result
}

// substituteString applies Go text/template substitution to s using params as
// the template data. The template data is a map[string]string; field access
// uses the standard {{.key}} syntax. missingkey=error is set so that any
// reference to an unknown parameter key returns an error per SH-030.
// Returns s unchanged when s contains no "{{" markers (fast path).
func substituteString(s string, params map[string]string) (string, error) {
	if !strings.Contains(s, "{{") {
		return s, nil
	}
	tmpl, err := template.New("").Option("missingkey=error").Parse(s)
	if err != nil {
		return "", fmt.Errorf("template parse: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, params); err != nil {
		return "", fmt.Errorf("template execute: %w", err)
	}
	return buf.String(), nil
}

// applyMatrixParams returns a deep-copy of sf with all substitutable string
// fields processed via substituteString using params. The Name and Matrix
// fields are NOT modified here; callers set the synthetic name and clear
// Matrix after calling this function (see ExpandMatrix).
func applyMatrixParams(sf ScenarioFile, params map[string]string) (ScenarioFile, error) {
	result := sf // start with a shallow copy; nested fields replaced below

	// Description
	desc, err := substituteString(sf.Description, params)
	if err != nil {
		return ScenarioFile{}, fmt.Errorf("description: %w", err)
	}
	result.Description = desc

	// WorkflowPath (*string)
	if sf.WorkflowPath != nil {
		p, err := substituteString(*sf.WorkflowPath, params)
		if err != nil {
			return ScenarioFile{}, fmt.Errorf("workflow_path: %w", err)
		}
		result.WorkflowPath = &p
	}

	// AgentOverrides
	if sf.AgentOverrides != nil {
		result.AgentOverrides = make(map[string]AgentOverride, len(sf.AgentOverrides))
		for role, ao := range sf.AgentOverrides {
			binary, err := substituteString(ao.Binary, params)
			if err != nil {
				return ScenarioFile{}, fmt.Errorf("agent_overrides[%q].binary: %w", role, err)
			}
			var args []string
			if ao.Args != nil {
				args = make([]string, len(ao.Args))
				for i, arg := range ao.Args {
					a, err := substituteString(arg, params)
					if err != nil {
						return ScenarioFile{}, fmt.Errorf("agent_overrides[%q].args[%d]: %w", role, i, err)
					}
					args[i] = a
				}
			}
			result.AgentOverrides[role] = AgentOverride{Binary: binary, Args: args}
		}
	}

	// FixtureSetup
	fs, err := applyMatrixParamsFixtureSetup(sf.FixtureSetup, params)
	if err != nil {
		return ScenarioFile{}, fmt.Errorf("fixture_setup: %w", err)
	}
	result.FixtureSetup = fs

	// ExpectedEvents
	if sf.ExpectedEvents != nil {
		result.ExpectedEvents = make([]EventExpectation, len(sf.ExpectedEvents))
		for i, ee := range sf.ExpectedEvents {
			newEE, err := applyMatrixParamsEventExpectation(ee, params, i)
			if err != nil {
				return ScenarioFile{}, err
			}
			result.ExpectedEvents[i] = newEE
		}
	}

	// ExpectedWorkspace
	if sf.ExpectedWorkspace != nil {
		result.ExpectedWorkspace = make([]WorkspacePredicate, len(sf.ExpectedWorkspace))
		for i, wp := range sf.ExpectedWorkspace {
			path, err := substituteString(wp.Path, params)
			if err != nil {
				return ScenarioFile{}, fmt.Errorf("expected_workspace[%d].path: %w", i, err)
			}
			desc, err := substituteString(wp.Description, params)
			if err != nil {
				return ScenarioFile{}, fmt.Errorf("expected_workspace[%d].description: %w", i, err)
			}
			newWP := wp
			newWP.Path = path
			newWP.Description = desc
			if wp.Expected != nil {
				exp, err := substituteString(*wp.Expected, params)
				if err != nil {
					return ScenarioFile{}, fmt.Errorf("expected_workspace[%d].expected: %w", i, err)
				}
				newWP.Expected = &exp
			}
			result.ExpectedWorkspace[i] = newWP
		}
	}

	// ExpectedOutcome
	if sf.ExpectedOutcome != nil {
		desc, err := substituteString(sf.ExpectedOutcome.Description, params)
		if err != nil {
			return ScenarioFile{}, fmt.Errorf("expected_outcome.description: %w", err)
		}
		newEO := *sf.ExpectedOutcome
		newEO.Description = desc
		result.ExpectedOutcome = &newEO
	}

	return result, nil
}

// applyMatrixParamsFixtureSetup returns a deep-copy of fs with substitutable
// string fields processed via substituteString.
func applyMatrixParamsFixtureSetup(fs FixtureSetup, params map[string]string) (FixtureSetup, error) {
	result := fs

	if fs.GitSeed != nil {
		result.GitSeed = make([]GitSeedOp, len(fs.GitSeed))
		for i, op := range fs.GitSeed {
			newOp := op
			if op.Args != nil {
				newOp.Args = make(map[string]string, len(op.Args))
				for k, v := range op.Args {
					v2, err := substituteString(v, params)
					if err != nil {
						return FixtureSetup{}, fmt.Errorf("git_seed[%d].args[%q]: %w", i, k, err)
					}
					newOp.Args[k] = v2
				}
			}
			result.GitSeed[i] = newOp
		}
	}

	if fs.Files != nil {
		result.Files = make(map[string]FileSeed, len(fs.Files))
		for path, seed := range fs.Files {
			newPath, err := substituteString(path, params)
			if err != nil {
				return FixtureSetup{}, fmt.Errorf("files[%q] key: %w", path, err)
			}
			newSeed := seed
			// Only substitute utf8 contents; base64 contents are binary data.
			if seed.Encoding != FileSeedEncodingBase64 {
				contents, err := substituteString(seed.Contents, params)
				if err != nil {
					return FixtureSetup{}, fmt.Errorf("files[%q].contents: %w", path, err)
				}
				newSeed.Contents = contents
			}
			result.Files[newPath] = newSeed
		}
	}

	if fs.SkillSearchPaths != nil {
		result.SkillSearchPaths = make([]string, len(fs.SkillSearchPaths))
		for i, p := range fs.SkillSearchPaths {
			p2, err := substituteString(p, params)
			if err != nil {
				return FixtureSetup{}, fmt.Errorf("skill_search_paths[%d]: %w", i, err)
			}
			result.SkillSearchPaths[i] = p2
		}
	}

	return result, nil
}

// applyMatrixParamsEventExpectation returns a deep-copy of ee with substitutable
// string fields processed. String values in PayloadMatch are substituted; non-string
// values are preserved as-is.
func applyMatrixParamsEventExpectation(ee EventExpectation, params map[string]string, idx int) (EventExpectation, error) {
	desc, err := substituteString(ee.Description, params)
	if err != nil {
		return EventExpectation{}, fmt.Errorf("expected_events[%d].description: %w", idx, err)
	}
	newEE := ee
	newEE.Description = desc

	if ee.PayloadMatch != nil {
		newEE.PayloadMatch = make(map[string]any, len(ee.PayloadMatch))
		for k, v := range ee.PayloadMatch {
			if sv, ok := v.(string); ok {
				sv2, err := substituteString(sv, params)
				if err != nil {
					return EventExpectation{}, fmt.Errorf("expected_events[%d].payload_match[%q]: %w", idx, k, err)
				}
				newEE.PayloadMatch[k] = sv2
			} else {
				newEE.PayloadMatch[k] = v
			}
		}
	}
	return newEE, nil
}
