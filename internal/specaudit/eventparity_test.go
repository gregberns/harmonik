package specaudit_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var nonProductionPathFragments = []string{
	"_test.go",
	"/internal/testhelpers/",
	"/internal/keepertest/",
	"/internal/keepertwin/",
	"/internal/runexectest/",
	"/internal/codextest/",
	"/internal/codexdigitaltwin/",
	"/internal/daemon/scenariotest/",
	"/internal/twinparity/",
	"/cmd/harmonik-twin-",
	"/test/",
	"/twins/",
	"/tools/",
}

var emitterFuncRe = regexp.MustCompile(`(?i)(^|\.)((emit|publish)[A-Za-z0-9_]*|[A-Za-z0-9_]*(appendevent|writeevent|newevent)[A-Za-z0-9_]*)$`)

var actEmitKindRe = regexp.MustCompile(`^ActEmit`)

var reactorActionTypes = map[string]bool{
	"Action": true,
}

var substringMatchFuncs = map[string]bool{
	"strings.Contains":   true,
	"strings.HasPrefix":  true,
	"strings.HasSuffix":  true,
	"strings.Index":      true,
	"strings.EqualFold":  true,
	"strings.Count":      true,
	"strings.SplitAfter": true,
}

type dynamicProducerSurface struct {
	Name        string // what the surface is, for the failure message
	AllowlistIn string // repo-relative file holding the allowlist map
	Allowlist   string // the map variable whose KEYS are the emittable types
	ConstantsIn string // repo-relative file declaring the key constants
	RelayFunc   string // the function that performs the runtime-typed emission
}

var dynamicProducerSurfaces = []dynamicProducerSurface{
	{
		Name:        "handler-contract watcher progress-stream relay",
		AllowlistIn: "internal/handlercontract/watcher_hc011.go",
		Allowlist:   "knownProgressMsgTypes",
		ConstantsIn: "internal/handlercontract/progressstream_hc007.go",
		RelayFunc:   "publishOrDeadLetter",
	},
}

type finding struct {
	Type string // the event type
	At   string // the consuming or asserting site, as this test names it
	Note string // what silently does not happen because of it
}

var orphanConsumers = []finding{
	{
		Type: "checkpoint_written",
		At:   "cmd/harmonik/eval_cmd.go evalReadEvents",
		Note: "`harmonik eval` reads commit_hash off this event to record which commit each eval " +
			"run produced. Nothing writes it, so the commit column is always empty.",
	},
	{
		Type: "metric",
		At:   "internal/watch/escalation.go Classify",
		Note: "The §8.8.1 metric channel has no producer anywhere. The watch classifies it as " +
			"routine churn, which is correct only because it never arrives.",
	},
	{
		Type: "session_keeper_operator_attached",
		At:   "internal/digest/resolver.go scanSuppressionEvents",
		Note: "The digest suppresses its own posts while a human is attached to the session. " +
			"internal/keeper stopped persisting this event, so the suppression never activates " +
			"and the daemon keeps posting digests over an attached operator.",
	},
}

var zeroCountAssertions = []finding{
	{
		Type: "session_keeper_operator_attached",
		At:   "internal/keeper/cycle_operator_attached_test.go TestCycler_OperatorAttached_SuppressesInjection",
		Note: "Keeper operator-attached suppression: the whole family asserts an event the keeper deliberately stopped emitting.",
	},
	{
		Type: "session_keeper_operator_attached",
		At:   "internal/keeper/cycle_operator_attached_test.go TestCycler_OperatorDetachThenResume",
		Note: "Same family.",
	},
	{
		Type: "session_keeper_operator_attached",
		At:   "internal/keeper/cycle_operator_attached_test.go TestCycler_OperatorDetached_Proceeds",
		Note: "Same family.",
	},
	{
		Type: "session_keeper_operator_attached",
		At:   "internal/keeper/cycle_operator_attached_test.go TestCycler_Precompact_OperatorAttached_Suppresses",
		Note: "Same family.",
	},
	{
		Type: "session_keeper_operator_attached",
		At:   "internal/keeper/cycle_twin_e2e_integration_test.go TestIntegration_TwinE2E_OperatorRealEnv",
		Note: "Same family, in the twin end-to-end path.",
	},
	{
		Type: "workspace_discarded",
		At:   "internal/workspace/lifecycleevents_wm015_test.go TestWM015_FullLifecycleMergedPath",
		Note: "Same package, same reason.",
	},
	{
		Type: "workspace_leased",
		At:   "internal/workspace/lifecycleevents_wm015_test.go TestWM015_CreatedEmittedOnEntryToCreated",
		Note: "internal/workspace contains no event emission at all, so its lifecycle-event suite asserts against a bus it never touches.",
	},
}

func recorded(list []finding, typ, at string) bool {
	for _, f := range list {
		if f.Type == typ && f.At == at {
			return true
		}
	}
	return false
}

// TestEventParity_EveryConsumedTypeHasAProducer fails when production code
// branches on an event type that no production code can emit — and that pairing
// is not already recorded in orphanConsumers.
func TestEventParity_EveryConsumedTypeHasAProducer(t *testing.T) {
	t.Parallel()
	idx := buildEventIndex(t)

	found := currentOrphanConsumers(idx)
	news := make([]string, 0, len(found))
	for _, f := range found {
		if !recorded(orphanConsumers, f.Type, f.At) {
			news = append(news, "    "+f.Type+"\n        consumed by: "+f.At)
		}
	}
	if len(news) > 0 {
		t.Errorf("%d NEW consumer(s) branch on an event type no production code can emit.\n"+
			"Each takes its never-fires branch for the life of the program.\n"+
			"Wire the producer, or delete the consumer. Recording it in orphanConsumers is the\n"+
			"third option and it is a debt, not a fix. Widening emitterFuncRe to make this go away\n"+
			"is never the answer unless you found a real emission path the regex fails to match.\n\n%s",
			len(news), strings.Join(news, "\n"))
	}
}

// TestEventParity_NoZeroCountAssertionOnAnUnproducibleType fails when a test
// asserts that an event fired zero times, nothing in production can make it
// fire, and the assertion is not already recorded. Such an assertion has no
// failing input: no change to the product can turn it red.
func TestEventParity_NoZeroCountAssertionOnAnUnproducibleType(t *testing.T) {
	t.Parallel()
	idx := buildEventIndex(t)

	found := currentZeroCountAssertions(idx)
	news := make([]string, 0, len(found))
	for _, f := range found {
		if !recorded(zeroCountAssertions, f.Type, f.At) {
			news = append(news, "    "+f.Type+"\n        asserted zero at: "+f.At)
		}
	}
	if len(news) > 0 {
		t.Errorf("%d NEW assertion(s) assert a count of zero on an event type nothing can emit.\n"+
			"These assertions cannot fail. They sell coverage that does not exist.\n"+
			"Delete them, or emit the event they are supposed to be watching for.\n\n%s",
			len(news), strings.Join(news, "\n"))
	}
}

// TestEventParity_NoRecordOutlivesItsDefect fails when a row in orphanConsumers
// or zeroCountAssertions is no longer a finding. This is what stops the two
// tables above from becoming a place to park defects: the moment a producer is
// wired, or a consumer or assertion is deleted, its row must go too. Without
// this, a stale row silently re-opens the hole it used to describe.
func TestEventParity_NoRecordOutlivesItsDefect(t *testing.T) {
	t.Parallel()
	idx := buildEventIndex(t)

	live := map[string]bool{}
	for _, f := range currentOrphanConsumers(idx) {
		live["orphan\x00"+f.Type+"\x00"+f.At] = true
	}
	for _, f := range currentZeroCountAssertions(idx) {
		live["zero\x00"+f.Type+"\x00"+f.At] = true
	}

	var stale []string
	for _, f := range orphanConsumers {
		if !live["orphan\x00"+f.Type+"\x00"+f.At] {
			stale = append(stale, "    orphanConsumers: "+f.Type+" at "+f.At)
		}
	}
	for _, f := range zeroCountAssertions {
		if !live["zero\x00"+f.Type+"\x00"+f.At] {
			stale = append(stale, "    zeroCountAssertions: "+f.Type+" at "+f.At)
		}
	}
	if len(stale) > 0 {
		t.Errorf("%d recorded finding(s) no longer describe anything in the tree.\n"+
			"Either the defect was fixed — delete the row — or the site moved and the row now\n"+
			"protects nothing while still reading as covered.\n\n%s",
			len(stale), strings.Join(stale, "\n"))
	}
}

func currentOrphanConsumers(idx *eventIndex) []finding {
	out := make([]finding, 0, len(idx.consumers))
	for _, typ := range idx.sortedTypes() {
		if len(idx.consumers[typ]) == 0 || len(idx.producers[typ]) > 0 {
			continue
		}
		for _, at := range dedupe(idx.consumers[typ]) {
			out = append(out, finding{Type: typ, At: at})
		}
	}
	return out
}

func currentZeroCountAssertions(idx *eventIndex) []finding {
	out := make([]finding, 0, len(idx.zeroAsserts))
	for _, typ := range idx.sortedTypes() {
		if len(idx.zeroAsserts[typ]) == 0 || len(idx.producers[typ]) > 0 {
			continue
		}
		for _, at := range dedupe(idx.zeroAsserts[typ]) {
			out = append(out, finding{Type: typ, At: at})
		}
	}
	return out
}

type eventIndex struct {
	// value -> description of each site.
	producers   map[string][]string
	consumers   map[string][]string
	zeroAsserts map[string][]string
	// production files that unpack an action descriptor and emit its Type.
	shellEmitSites []string
	// registered event type string -> constant name.
	known map[string]string
}

func (idx *eventIndex) sortedTypes() []string {
	out := make([]string, 0, len(idx.known))
	for v := range idx.known {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func buildEventIndex(t *testing.T) *eventIndex {
	t.Helper()
	root := repoRootForEventParity(t)
	fset := token.NewFileSet()

	files := goFilesUnder(t, root)
	idx := &eventIndex{
		producers:   map[string][]string{},
		consumers:   map[string][]string{},
		zeroAsserts: map[string][]string{},
		known:       map[string]string{},
	}

	for _, p := range files {
		if !strings.Contains(p, "/internal/core/") {
			continue
		}
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", p, err)
		}
		for name, value := range typedStringConstants(f, "EventType") {
			idx.known[value] = name
		}
	}
	if len(idx.known) < 100 {
		t.Fatalf("found only %d event types in internal/core; the registry surface moved and this audit is now blind", len(idx.known))
	}

	for _, p := range files {
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", p, err)
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			rel = p
		}
		if isProductionPath(p) && shellEmitsActionType(f) {
			idx.shellEmitSites = append(idx.shellEmitSites, rel)
		}
		scanFile(f, rel, isProductionPath(p), idx)
	}

	if len(idx.shellEmitSites) == 0 {
		t.Fatal("no production code passes an action descriptor's .Type to an emitter; " +
			"the reactor/shell emission idiom is gone and Pass B2 now credits producers that do not exist")
	}

	for _, s := range dynamicProducerSurfaces {
		for _, v := range resolveDynamicSurface(t, fset, root, s) {
			if _, registered := idx.known[v]; registered {
				idx.producers[v] = append(idx.producers[v], s.Name)
			}
		}
	}

	return idx
}

func shellEmitsActionType(f *ast.File) bool {
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !emitterFuncRe.MatchString(exprString(call.Fun)) {
			return true
		}
		for _, arg := range call.Args {
			if sel, ok := arg.(*ast.SelectorExpr); ok && sel.Sel.Name == "Type" {
				found = true
			}
		}
		return true
	})
	return found
}

func scanFile(f *ast.File, rel string, production bool, idx *eventIndex) {
	inCore := strings.Contains(rel, "internal/core/")

	type scope struct {
		node ast.Node
		name string
	}
	var scopes []scope
	harvestLiterals := func(root ast.Node, name string) {
		ast.Inspect(root, func(n ast.Node) bool {
			if fl, ok := n.(*ast.FuncLit); ok && fl.Body != nil {
				scopes = append(scopes, scope{fl.Body, name + " closure"})
			}
			return true
		})
	}
	for _, d := range f.Decls {
		switch x := d.(type) {
		case *ast.FuncDecl:
			if x.Body == nil {
				continue
			}
			scopes = append(scopes, scope{x.Body, x.Name.Name})
			harvestLiterals(x.Body, x.Name.Name)
		case *ast.GenDecl:
			if x.Tok != token.CONST {
				scopes = append(scopes, scope{x, "package scope"})
				harvestLiterals(x, "package scope")
			}
		}
	}

	for _, s := range scopes {
		scanScope(s.node, s.name, rel, production, inCore, idx)
	}
}

func scanScope(scope ast.Node, scopeName, rel string, production, inCore bool, idx *eventIndex) {
	at := func(ast.Node) string { return rel + " " + scopeName }

	inspectScope(scope, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !emitterFuncRe.MatchString(exprString(call.Fun)) {
			return true
		}
		for _, arg := range call.Args {
			for _, v := range eventValuesIn(arg, inCore, idx.known) {
				if production {
					idx.producers[v] = append(idx.producers[v], at(call))
				}
			}
		}
		return true
	})

	if production {
		emitted := map[string]bool{}
		inspectScope(scope, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || !emitterFuncRe.MatchString(exprString(call.Fun)) {
				return true
			}
			for _, arg := range call.Args {
				for _, id := range identsIn(arg) {
					emitted[id] = true
				}
			}
			return true
		})
		inspectScope(scope, func(n ast.Node) bool {
			var lhs []ast.Expr
			var rhs []ast.Expr
			switch x := n.(type) {
			case *ast.AssignStmt:
				lhs, rhs = x.Lhs, x.Rhs
			case *ast.ValueSpec:
				for _, nm := range x.Names {
					lhs = append(lhs, nm)
				}
				rhs = x.Values
			default:
				return true
			}
			for i, r := range rhs {
				if i >= len(lhs) {
					break
				}
				target, ok := lhs[i].(*ast.Ident)
				if !ok || !emitted[target.Name] {
					continue
				}
				for _, v := range eventValuesIn(r, inCore, idx.known) {
					idx.producers[v] = append(idx.producers[v], at(n))
				}
			}
			return true
		})
	}

	if production && emitterFuncRe.MatchString(scopeName) {
		enveloped := map[string]bool{}
		var site ast.Node
		forEachCompositeLit(scope, func(cl *ast.CompositeLit, _ string) {
			if lit := exprString(cl.Type); lit != "core.Event" && (!inCore || lit != "Event") {
				return
			}
			for _, elt := range cl.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if key, isIdent := kv.Key.(*ast.Ident); !isIdent || key.Name != "Type" {
					continue
				}
				site = cl
				for _, v := range eventValuesIn(kv.Value, inCore, idx.known) {
					idx.producers[v] = append(idx.producers[v], at(cl))
				}
				for _, id := range identsIn(kv.Value) {
					enveloped[id] = true
				}
			}
		})
		if len(enveloped) > 0 {
			inspectScope(scope, func(n ast.Node) bool {
				var lhs []ast.Expr
				var rhs []ast.Expr
				switch x := n.(type) {
				case *ast.AssignStmt:
					lhs, rhs = x.Lhs, x.Rhs
				case *ast.ValueSpec:
					for _, nm := range x.Names {
						lhs = append(lhs, nm)
					}
					rhs = x.Values
				default:
					return true
				}
				for i, r := range rhs {
					if i >= len(lhs) {
						break
					}
					target, ok := lhs[i].(*ast.Ident)
					if !ok || !enveloped[target.Name] {
						continue
					}
					for _, v := range eventValuesIn(r, inCore, idx.known) {
						idx.producers[v] = append(idx.producers[v], at(site))
					}
				}
				return true
			})
		}
	}

	if production {
		forEachCompositeLit(scope, func(cl *ast.CompositeLit, typeName string) {
			if !reactorActionTypes[typeName] {
				return
			}
			emitIntent := false
			var typeField ast.Expr
			for _, elt := range cl.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, isIdent := kv.Key.(*ast.Ident)
				if !isIdent {
					continue
				}
				switch key.Name {
				case "Type":
					typeField = kv.Value
				case "Kind":
					emitIntent = actEmitKindRe.MatchString(lastIdent(kv.Value))
				}
			}
			if !emitIntent || typeField == nil {
				return
			}
			for _, v := range eventValuesIn(typeField, inCore, idx.known) {
				idx.producers[v] = append(idx.producers[v], at(cl))
			}
		})
	}

	inspectScope(scope, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SwitchStmt:
			tagLooksLikeAType := isEventTypeExpr(x.Tag, inCore, idx.known)
			for _, stmt := range x.Body.List {
				cc, ok := stmt.(*ast.CaseClause)
				if !ok {
					continue
				}
				for _, e := range cc.List {
					vals := constantEventValuesIn(e, inCore, idx.known)
					if tagLooksLikeAType {
						vals = eventValuesIn(e, inCore, idx.known)
					}
					for _, v := range vals {
						if production {
							idx.consumers[v] = append(idx.consumers[v], at(cc))
						}
					}
				}
			}
		case *ast.BinaryExpr:
			if x.Op != token.EQL && x.Op != token.NEQ {
				return true
			}
			if !isEventTypeExpr(x.X, inCore, idx.known) && !isEventTypeExpr(x.Y, inCore, idx.known) {
				return true
			}
			for _, side := range []ast.Expr{x.X, x.Y} {
				for _, v := range eventValuesIn(side, inCore, idx.known) {
					if production {
						idx.consumers[v] = append(idx.consumers[v], at(x))
					}
				}
			}
		case *ast.CallExpr:
			if !substringMatchFuncs[exprString(x.Fun)] {
				return true
			}
			for _, arg := range x.Args {
				for _, v := range constantEventValuesIn(arg, inCore, idx.known) {
					if production {
						idx.consumers[v] = append(idx.consumers[v], at(x))
					}
				}
			}
		}
		return true
	})

	if !production {
		inspectScope(scope, func(n ast.Node) bool {
			ifs, ok := n.(*ast.IfStmt)
			if !ok {
				return true
			}
			for _, v := range dedupe(zeroCountTypes(ifs, inCore, idx.known)) {
				idx.zeroAsserts[v] = append(idx.zeroAsserts[v], at(ifs))
			}
			return true
		})
	}
}

func zeroCountTypes(ifs *ast.IfStmt, inCore bool, known map[string]string) []string {
	bindings := map[string]ast.Expr{}
	if ifs.Init != nil {
		ast.Inspect(ifs.Init, func(m ast.Node) bool {
			as, ok := m.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for i, l := range as.Lhs {
				if i >= len(as.Rhs) {
					break
				}
				if id, ok := l.(*ast.Ident); ok {
					bindings[id.Name] = as.Rhs[i]
				}
			}
			return true
		})
	}

	var types []string
	ast.Inspect(ifs.Cond, func(m ast.Node) bool {
		be, ok := m.(*ast.BinaryExpr)
		if !ok {
			return true
		}
		var other ast.Expr
		switch {
		case be.Op == token.NEQ && isZeroLiteral(be.Y):
			other = be.X // count != 0
		case be.Op == token.NEQ && isZeroLiteral(be.X):
			other = be.Y // 0 != count
		case be.Op == token.GTR && isZeroLiteral(be.Y):
			other = be.X // count > 0
		case be.Op == token.LSS && isZeroLiteral(be.X):
			other = be.Y // 0 < count
		default:
			return true
		}
		counted := resolveCount(other, bindings, 0)
		if counted == nil {
			return true
		}
		types = append(types, eventValuesIn(counted, inCore, known)...)
		return true
	})
	return types
}

var countCallRe = regexp.MustCompile(`(?i)count`)

func resolveCount(e ast.Expr, bindings map[string]ast.Expr, depth int) ast.Expr {
	if depth > 4 {
		return nil
	}
	switch x := e.(type) {
	case *ast.CallExpr:
		name := exprString(x.Fun)
		if name == "len" && len(x.Args) == 1 {
			if id, ok := x.Args[0].(*ast.Ident); ok {
				if bound, found := bindings[id.Name]; found {
					return bound
				}
			}
			return x.Args[0]
		}
		if countCallRe.MatchString(name) {
			return x
		}
		return nil
	case *ast.Ident:
		if bound, found := bindings[x.Name]; found {
			return resolveCount(bound, bindings, depth+1)
		}
		return nil
	}
	return nil
}

func isZeroLiteral(e ast.Expr) bool {
	bl, ok := e.(*ast.BasicLit)
	return ok && bl.Kind == token.INT && bl.Value == "0"
}

var eventConstRe = regexp.MustCompile(`^EventType[A-Z][A-Za-z0-9_]*$`)

func typedStringConstants(f *ast.File, typeName string) map[string]string {
	out := map[string]string{}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			if id, ok := vs.Type.(*ast.Ident); !ok || id.Name != typeName {
				continue
			}
			for i, nm := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				bl, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || bl.Kind != token.STRING {
					continue
				}
				if v, err := strconv.Unquote(bl.Value); err == nil {
					out[nm.Name] = v
				}
			}
		}
	}
	return out
}

func eventValuesIn(e ast.Expr, inCore bool, known map[string]string) []string {
	var out []string
	ast.Inspect(e, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			if id, ok := x.X.(*ast.Ident); ok && id.Name == "core" && eventConstRe.MatchString(x.Sel.Name) {
				if v := valueOfConst(x.Sel.Name, known); v != "" {
					out = append(out, v)
				}
			}
		case *ast.Ident:
			if inCore && eventConstRe.MatchString(x.Name) {
				if v := valueOfConst(x.Name, known); v != "" {
					out = append(out, v)
				}
			}
		case *ast.BasicLit:
			if x.Kind != token.STRING {
				return true
			}
			s, err := strconv.Unquote(x.Value)
			if err != nil {
				return true
			}
			if _, ok := known[s]; ok {
				out = append(out, s)
			}
		}
		return true
	})
	return out
}

func constantEventValuesIn(e ast.Expr, inCore bool, known map[string]string) []string {
	var out []string
	ast.Inspect(e, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			if id, ok := x.X.(*ast.Ident); ok && id.Name == "core" && eventConstRe.MatchString(x.Sel.Name) {
				if v := valueOfConst(x.Sel.Name, known); v != "" {
					out = append(out, v)
				}
			}
		case *ast.Ident:
			if inCore && eventConstRe.MatchString(x.Name) {
				if v := valueOfConst(x.Name, known); v != "" {
					out = append(out, v)
				}
			}
		}
		return true
	})
	return out
}

func valueOfConst(name string, known map[string]string) string {
	for v, n := range known {
		if n == name {
			return v
		}
	}
	return ""
}

func isEventTypeExpr(e ast.Expr, inCore bool, known map[string]string) bool {
	if e == nil {
		return false
	}
	if len(constantEventValuesIn(e, inCore, known)) > 0 {
		return true
	}
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			if x.Sel.Name == "Type" || x.Sel.Name == "EventType" {
				found = true
			}
			if id, ok := x.X.(*ast.Ident); ok && id.Name == "core" && x.Sel.Name == "EventType" {
				found = true
			}
		case *ast.Ident:
			if x.Name == "eventType" || x.Name == "evType" || x.Name == "EventType" {
				found = true
			}
		}
		return true
	})
	return found
}

func identsIn(e ast.Expr) []string {
	var out []string
	ast.Inspect(e, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			out = append(out, id.Name)
		}
		return true
	})
	return out
}

func exprString(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return exprString(x.X) + "." + x.Sel.Name
	case *ast.CallExpr:
		return exprString(x.Fun)
	case *ast.IndexExpr:
		return exprString(x.X)
	}
	return ""
}

func inspectScope(scope ast.Node, fn func(ast.Node) bool) {
	ast.Inspect(scope, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		if _, isLit := n.(*ast.FuncLit); isLit && n != scope {
			return false
		}
		return fn(n)
	})
}

func forEachCompositeLit(scope ast.Node, visit func(*ast.CompositeLit, string)) {
	var walk func(n ast.Node, elided string)
	walk = func(n ast.Node, elided string) {
		if n == nil {
			return
		}
		cl, isLit := n.(*ast.CompositeLit)
		if !isLit {
			ast.Inspect(n, func(m ast.Node) bool {
				if m == nil || m == n {
					return m == n
				}
				if _, isFn := m.(*ast.FuncLit); isFn {
					return false // its own scope; walked separately
				}
				if inner, ok := m.(*ast.CompositeLit); ok {
					walk(inner, "")
					return false
				}
				return true
			})
			return
		}
		name := lastIdent(cl.Type)
		if name == "" {
			name = elided
		}
		visit(cl, name)
		child := ""
		switch t := cl.Type.(type) {
		case *ast.ArrayType:
			child = lastIdent(t.Elt)
		case *ast.MapType:
			child = lastIdent(t.Value)
		case nil:
			child = elided
		}
		for _, elt := range cl.Elts {
			walk(elt, child)
		}
	}
	walk(scope, "")
}

func lastIdent(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return x.Sel.Name
	}
	return ""
}

func resolveDynamicSurface(t *testing.T, fset *token.FileSet, root string, s dynamicProducerSurface) []string {
	t.Helper()

	constFile, err := parser.ParseFile(fset, filepath.Join(root, s.ConstantsIn), nil, 0)
	if err != nil {
		t.Fatalf("dynamic surface %q: parse %s: %v", s.Name, s.ConstantsIn, err)
	}
	byName := map[string]string{}
	for _, decl := range constFile.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, nm := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				bl, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || bl.Kind != token.STRING {
					continue
				}
				if v, err := strconv.Unquote(bl.Value); err == nil {
					byName[nm.Name] = v
				}
			}
		}
	}

	listFile, err := parser.ParseFile(fset, filepath.Join(root, s.AllowlistIn), nil, 0)
	if err != nil {
		t.Fatalf("dynamic surface %q: parse %s: %v", s.Name, s.AllowlistIn, err)
	}

	var values []string
	relayFound := false
	ast.Inspect(listFile, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && strings.HasSuffix(exprString(call.Fun), s.RelayFunc) {
			relayFound = true
		}
		vs, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, nm := range vs.Names {
			if nm.Name != s.Allowlist || i >= len(vs.Values) {
				continue
			}
			cl, ok := vs.Values[i].(*ast.CompositeLit)
			if !ok {
				continue
			}
			for _, elt := range cl.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if id, ok := kv.Key.(*ast.Ident); ok {
					if v, known := byName[id.Name]; known {
						values = append(values, v)
					}
				}
			}
		}
		return true
	})

	if len(values) == 0 {
		t.Fatalf("dynamic surface %q: allowlist %s in %s resolved to no event types; "+
			"the surface moved and this entry now credits nothing while still claiming to",
			s.Name, s.Allowlist, s.AllowlistIn)
	}
	if !relayFound {
		t.Fatalf("dynamic surface %q: relay %s is gone from %s; the allowlist no longer emits anything "+
			"and every type it covers must be re-audited",
			s.Name, s.RelayFunc, s.AllowlistIn)
	}
	return values
}

func repoRootForEventParity(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("repoRootForEventParity: runtime.Caller(0) failed")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}

func goFilesUnder(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor", "node_modules", ".kerf", "runs", "refs":
				return filepath.SkipDir
			case ".claire", ".claude", "worktrees":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(p, ".go") {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Strings(out)
	return out
}

func isProductionPath(p string) bool {
	for _, frag := range nonProductionPathFragments {
		if strings.Contains(p, frag) {
			return false
		}
	}
	return true
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
