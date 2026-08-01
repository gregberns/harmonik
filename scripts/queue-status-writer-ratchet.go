// queue-status-writer-ratchet.go measures the queue status source surface.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	bravoBaseline        = 16
	daemonBaseline       = 14
	constructionBaseline = 18
)

type measurement struct {
	ownerAssignments   int
	daemonAssignments  int
	construction       int
	constructionByFile map[string]int
	misplaced          []string
}

func main() {
	root := os.Getenv("QUEUE_STATUS_RATCHET_ROOT")
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			fail("cannot read working directory: %v", err)
		}
	}

	m, err := measure(root)
	if err != nil {
		fail("measure source surface: %v", err)
	}

	fmt.Printf("queue-status-writer-ratchet: baseline direct assignments bravo=%d daemon=%d\n", bravoBaseline, daemonBaseline)
	fmt.Printf("queue-status-writer-ratchet: current direct assignments owner=%d daemon=%d\n", m.ownerAssignments, m.daemonAssignments)
	fmt.Printf("queue-status-writer-ratchet: construction-path writes=%d (17 literals + 1 pre-persist adjustment)\n", m.construction)
	for _, source := range sortedKeys(m.constructionByFile) {
		fmt.Printf("queue-status-writer-ratchet: construction source %s=%d\n", source, m.constructionByFile[source])
	}

	failed := false
	if m.ownerAssignments > bravoBaseline {
		fmt.Fprintf(os.Stderr, "queue-status-writer-ratchet: FAIL transition owner grew from %d to %d\n", bravoBaseline, m.ownerAssignments)
		failed = true
	}
	if m.daemonAssignments > daemonBaseline {
		fmt.Fprintf(os.Stderr, "queue-status-writer-ratchet: FAIL daemon baseline grew from %d to %d\n", daemonBaseline, m.daemonAssignments)
		failed = true
	}
	if m.construction > constructionBaseline {
		fmt.Fprintf(os.Stderr, "queue-status-writer-ratchet: FAIL construction surface grew from %d to %d\n", constructionBaseline, m.construction)
		failed = true
	}
	if len(m.misplaced) > 0 {
		fmt.Fprintln(os.Stderr, "queue-status-writer-ratchet: FAIL direct queue status write outside the transition owner:")
		for _, hit := range m.misplaced {
			fmt.Fprintln(os.Stderr, hit)
		}
		failed = true
	}
	if failed {
		os.Exit(1)
	}
	fmt.Println("queue-status-writer-ratchet: OK")
}

func measure(root string) (measurement, error) {
	result := measurement{constructionByFile: make(map[string]int)}
	for _, sourceRoot := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, sourceRoot), func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
			if err != nil {
				return fmt.Errorf("parse %s: %w", rel, err)
			}
			hasQueueImport := strings.HasPrefix(rel, "internal/queue/") || importsQueue(file)
			ast.Inspect(file, func(node ast.Node) bool {
				switch value := node.(type) {
				case *ast.AssignStmt:
					if hasQueueImport && assignsStatus(value) {
						switch {
						case rel == "internal/queue/status_transition.go":
							result.ownerAssignments++
						case strings.HasPrefix(rel, "internal/daemon/"):
							result.daemonAssignments++
						default:
							result.misplaced = append(result.misplaced, rel)
						}
					}
				case *ast.CompositeLit:
					if hasQueueImport && !strings.HasPrefix(rel, "internal/daemon/") && isQueueConstruction(rel, value) {
						count := statusFieldsDeep(value)
						result.construction += count
						if count > 0 {
							result.constructionByFile[rel] += count
						}
						return false
					}
				case *ast.CallExpr:
					if rel == "internal/queue/rpc.go" && callsDeferredConstruction(value) {
						result.construction++
						result.constructionByFile[rel]++
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			return measurement{}, err
		}
	}
	sort.Strings(result.misplaced)
	return result, nil
}

func sortedKeys(values map[string]int) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func importsQueue(file *ast.File) bool {
	for _, imp := range file.Imports {
		if strings.HasSuffix(strings.Trim(imp.Path.Value, "\""), "/internal/queue") {
			return true
		}
	}
	return false
}

func assignsStatus(stmt *ast.AssignStmt) bool {
	for _, lhs := range stmt.Lhs {
		if selector, ok := lhs.(*ast.SelectorExpr); ok && selector.Sel.Name == "Status" {
			return true
		}
	}
	return false
}

func isQueueConstruction(rel string, literal *ast.CompositeLit) bool {
	name := typeName(literal.Type)
	if name == "Queue" || name == "Group" || name == "Item" {
		return true
	}
	if rel == "internal/queue/cli/helpers.go" {
		return name == "queueDoc" || name == "groupDoc" || name == "itemDoc"
	}
	return rel == "cmd/harmonik/run_via_daemon.go" && name == "submitEnvelope"
}

func typeName(expr ast.Expr) string {
	switch value := expr.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.SelectorExpr:
		return value.Sel.Name
	case *ast.ArrayType:
		return typeName(value.Elt)
	default:
		return ""
	}
}

func statusFieldsDeep(literal *ast.CompositeLit) int {
	count := 0
	ast.Inspect(literal, func(node ast.Node) bool {
		field, ok := node.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		key, ok := field.Key.(*ast.Ident)
		if ok && key.Name == "Status" {
			count++
		}
		return true
	})
	return count
}

func callsDeferredConstruction(call *ast.CallExpr) bool {
	name, ok := call.Fun.(*ast.Ident)
	return ok && name.Name == "DeferItemForLedgerDependency"
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "queue-status-writer-ratchet: ERROR "+format+"\n", args...)
	os.Exit(2)
}
