package specaudit_test

// eventparity_test.go — guards the event bus against a defect this project has
// shipped at least four times: an event type that something CONSUMES and
// nothing PRODUCES.
//
// The shape is always the same. A type is added to the registry in
// internal/core. A consumer is written for it — a switch case, a subscription
// filter, a counter, a suppression source. The producer is deferred, or is
// deleted later, or was only ever a twin. Nothing fails. The consumer takes its
// never-fires branch for the life of the program, and any test that asserts
// "this event fired zero times" passes unconditionally and can never fail. That
// second half is the sharper one: such a test is not a weak test, it is a test
// with no failing input, and it sells coverage that does not exist.
//
// Three tests here, and they fail for different reasons:
//
//   - TestEventParity_EveryConsumedTypeHasAProducer — a consumer whose event
//     nothing emits. The consequence is a protection or an observation that
//     silently does not happen.
//   - TestEventParity_NoZeroCountAssertionOnAnUnproducibleType — a test
//     asserting a count of zero for a type nothing can emit. The consequence is
//     a green assertion that no change to the product could ever turn red.
//   - TestEventParity_NoRecordOutlivesItsDefect — a row in the recorded-findings
//     tables that no longer describes anything. Without it those tables would be
//     a place to park defects instead of a list of them.
//
// The tree already carried 5 orphan consumers and 10 unfailable assertions on
// the day this landed. They are written down in orphanConsumers and
// zeroCountAssertions with the consequence of each, so this gate is green on
// today's tree and red on anything new. A permanently red gate would be the
// same defect this file is about, one level up: enforcement that cannot fail is
// enforcement that is not happening.
//
// # Why this is static, and what that costs
//
// Emission cannot be discovered by running the code. The whole defect class is
// code paths that never run, so a runtime emitter registry would report exactly
// the emissions that already happen and stay silent about the ones that do not.
// A whole-program call-graph tool (x/tools/go/callgraph) does not rescue it
// either, and adding that dependency would buy only the easy half: the hardest
// producer in this tree is
//
//	w.publishOrDeadLetter(ctx, core.EventType(typeOnly.Type), line, ...)
//
// in internal/handlercontract ((*Watcher).readLoop), where the event type
// is a string field decoded from a subprocess's NDJSON. That value is not in the
// program at all. No call graph recovers it. What bounds it is the static
// allowlist the code gates on — knownProgressMsgTypes — so this test reads that
// allowlist out of the source and credits its members as produced. See
// dynamicProducerSurfaces below: it is the one place this test can be lied to,
// so each entry names the gate it trusts and the test re-checks the gate exists.
//
// So: go/parser over the tree, plus intra-procedural flow for the
// assign-then-emit shape, plus the reactor/shell descriptor idiom, plus a
// declared table of dynamic surfaces. §"Known limits" at the bottom of this file
// states what still gets past it.

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

// ---------------------------------------------------------------------------
// What counts as production, an emitter, and a consumer
// ---------------------------------------------------------------------------

// nonProductionPathFragments name the files and packages that exist to serve
// tests or to stand in for a real component. An emission from one of these is
// not evidence that the live system emits anything: a twin that writes
// agent_failed proves only that the twin can be scripted to write it.
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

// emitterFuncRe matches the callee of every function in this tree that puts an
// event on the bus or appends one to events.jsonl. It is deliberately wide.
//
// Widening it can only ADD producers, which can only remove findings. Narrowing
// it is what makes this test bite. So the risk runs one way: a real emission
// path whose function name this regex does not match shows up as a false
// "consumed but never produced" report. Add the name here when that happens —
// do not delete the finding.
var emitterFuncRe = regexp.MustCompile(`(?i)(^|\.)((emit|publish)[A-Za-z0-9_]*|[A-Za-z0-9_]*(appendevent|writeevent|newevent)[A-Za-z0-9_]*)$`)

// actEmitKindRe matches the action-kind constants that mean "the shell emits
// this event". Both pure reactors in this tree — internal/keeper and
// internal/runexec — name theirs this way. See Pass B2.
var actEmitKindRe = regexp.MustCompile(`^ActEmit`)

// reactorActionTypes are the descriptor structs a shell unpacks and emits. A
// composite literal of any OTHER type is never credited as a producer, however
// it spells its fields — see Pass B2 for why that second condition is
// load-bearing rather than belt-and-braces.
var reactorActionTypes = map[string]bool{
	"Action": true,
}

// substringMatchFuncs are the string-search functions that let code consume an
// event by pattern-matching its type text instead of reading the typed field.
// A drain test in this repo counted run_completed exactly this way, matching
// payload text from a driver that no longer ran, so the count went silently to
// zero while the drain worked correctly. A search of this shape is invisible to
// a type-based audit unless it is named.
var substringMatchFuncs = map[string]bool{
	"strings.Contains":   true,
	"strings.HasPrefix":  true,
	"strings.HasSuffix":  true,
	"strings.Index":      true,
	"strings.EqualFold":  true,
	"strings.Count":      true,
	"strings.SplitAfter": true,
}

// dynamicProducerSurface describes production code that emits an event type
// read at run time from a bounded static set. The set — not the call site — is
// the producer, so the entry names the file that holds it, the constant table
// that resolves its members to strings, and the relay function whose continued
// existence makes the whole thing true.
//
// This is the only place the test takes a claim on trust. Each field is
// re-checked against the live source, so an entry cannot outlive the code it
// describes.
type dynamicProducerSurface struct {
	Name        string // what the surface is, for the failure message
	AllowlistIn string // repo-relative file holding the allowlist map
	Allowlist   string // the map variable whose KEYS are the emittable types
	ConstantsIn string // repo-relative file declaring the key constants
	RelayFunc   string // the function that performs the runtime-typed emission
}

// dynamicProducerSurfaces is the complete declared set. One entry today.
var dynamicProducerSurfaces = []dynamicProducerSurface{
	{
		Name:        "handler-contract watcher progress-stream relay",
		AllowlistIn: "internal/handlercontract/watcher_hc011.go",
		Allowlist:   "knownProgressMsgTypes",
		ConstantsIn: "internal/handlercontract/progressstream_hc007.go",
		RelayFunc:   "publishOrDeadLetter",
	},
}

// ---------------------------------------------------------------------------
// The recorded findings
// ---------------------------------------------------------------------------

// A finding is one defect this audit found on the tree the day it landed. The
// two tests below fail on anything NOT recorded here, so the gate is green on
// today's tree and red the moment a new orphan or a new unfailable assertion
// appears.
//
// A record is a debt, not a pardon. TestEventParity_NoRecordOutlivesItsDefect
// fails when an entry stops being a finding, so nobody can leave a stale row
// behind after a fix, and nobody can park a new defect here by copying the
// shape. The same discipline dynamicProducerSurfaces uses on its trust table,
// turned on the findings themselves.
//
// Clearing a row means one of two things: wire the producer, or delete the
// consumer and the assertions that watch for it. Silencing a row by widening
// emitterFuncRe is the one move that makes this file worse than nothing.
type finding struct {
	Type string // the event type
	At   string // the consuming or asserting site, as this test names it
	Note string // what silently does not happen because of it
}

// orphanConsumers: production code branches on a type nothing emits.
// Recorded 2026-08-04.
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
	// stall_detected was here. internal/daemon/stallfeed.go now calls
	// sentinel.DetectLayerA from the stale-watch scan and emits the event, so
	// the panel has a producer. Removed 2026-08-05.
}

// zeroCountAssertions: a test asserts an event fired zero times and nothing can
// make it fire. Each has no failing input. Recorded 2026-08-04.
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
		At:   "internal/keeper/cycle_operator_attached_throttle_test.go TestCycler_OperatorAttached_ReEmitsAfterInterval",
		Note: "The name says the keeper re-emits after an interval. The body asserts zero, and zero is all it can ever be.",
	},
	{
		Type: "session_keeper_operator_attached",
		At:   "internal/keeper/cycle_operator_attached_throttle_test.go TestCycler_OperatorAttached_ThrottledAcrossTicks",
		Note: "Claims to pin a throttle. Pins nothing — the throttled event does not exist.",
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
	{
		Type: "workspace_merge_status",
		At:   "internal/workspace/lifecycleevents_wm015_test.go TestWM015_DiscardedEmittedOnEntryToDiscarded closure",
		Note: "Same package, same reason.",
	},
}

// recorded reports whether (typ, at) is already on list.
func recorded(list []finding, typ, at string) bool {
	for _, f := range list {
		if f.Type == typ && f.At == at {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// The three tests
// ---------------------------------------------------------------------------

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

// currentOrphanConsumers returns every (type, consuming site) pair where the
// type has a production consumer and no production producer.
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

// currentZeroCountAssertions returns every (type, asserting site) pair where a
// test asserts a count of zero and no production code can emit the type.
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

// ---------------------------------------------------------------------------
// The index
// ---------------------------------------------------------------------------

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

	// Step 1: the registry surface. Every `Name EventType = "value"` constant in
	// internal/core is a declared event type. A registered type is not an
	// emitted type — that is the whole point — so this is the population under
	// audit, not a set of facts about behaviour.
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

	// Step 2: every reference site, classified.
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

	// Pass B2 credits a reactor descriptor as a producer only because a shell
	// somewhere unpacks it and emits. If no shell does that any more, the credit
	// is a lie and every type that depends on it must be re-audited.
	if len(idx.shellEmitSites) == 0 {
		t.Fatal("no production code passes an action descriptor's .Type to an emitter; " +
			"the reactor/shell emission idiom is gone and Pass B2 now credits producers that do not exist")
	}

	// Step 3: the declared dynamic surfaces.
	for _, s := range dynamicProducerSurfaces {
		for _, v := range resolveDynamicSurface(t, fset, root, s) {
			if _, registered := idx.known[v]; registered {
				idx.producers[v] = append(idx.producers[v], s.Name)
			}
		}
	}

	return idx
}

// shellEmitsActionType reports whether f contains a shell that hands an action
// descriptor's Type field to an emitter — the second half of the reactor/shell
// idiom Pass B2 relies on.
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

// scanFile records every event-type reference in one file. Production files
// contribute producers and consumers. Test files contribute zero-count
// assertions.
func scanFile(f *ast.File, rel string, production bool, idx *eventIndex) {
	inCore := strings.Contains(rel, "internal/core/")

	// Walk each function body separately so the assign-then-emit flow below is
	// scoped to one set of locals. The enclosing name travels with the scope so
	// findings cite a symbol, not a line number.
	type scope struct {
		node ast.Node
		name string
	}
	var scopes []scope
	// harvestLiterals registers every function literal under root as its own
	// scope. inspectScope refuses to descend into a literal, so a literal that
	// is not registered here is walked by nobody. That loses CONSUMERS, and a
	// lost consumer is silent, so this runs for package-level declarations as
	// well as function bodies.
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

	// Pass A — direct emission. An event type reaching an emitter as a constant
	// or as its own string literal.
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

	// Pass B — assign-then-emit. `et := core.EventTypeRunCompleted; if failed {
	// et = core.EventTypeRunFailed }; bus.EmitWithRunID(ctx, id, et, payload)`
	// is how the daemon writes both run terminals, so a classifier that only
	// looks at the emitter's argument list reports both as never produced. This
	// resolves it inside one scope, which is where every instance in this tree
	// lives.
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

	// Pass B2 — the reactor/shell emission idiom. A pure reactor never touches
	// the bus. It returns a descriptor and a shell performs the effect:
	//
	//     internal/keeper/step.go   Action{Kind: ActEmit, Type: core.EventTypeSessionKeeperClearSent, ...}
	//     internal/keeper/shell.go  c.emitter.EmitWithRunID(ctx, core.RunID{}, a.Type, a.Payload)
	//
	// The constant never appears at an emitter call site, so Pass A and Pass B
	// both miss it and the whole §8.20 keeper family reads as never produced.
	//
	// Two conditions must BOTH hold before a literal is credited, because either
	// one alone is trivially forgeable. The literal's own type must be a known
	// reactor descriptor (reactorActionTypes), and it must state emit intent in
	// its Kind field. Without the type check, a dead
	//
	//     var _ = anything{Kind: ActEmitNothing, Type: core.EventTypeStallDetected}
	//
	// dropped in an unrelated package silences a real finding, which is the one
	// failure mode that would make this whole audit worse than nothing.
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

	// Pass C — consumption. A switch case on an event type, an equality test
	// against one, or a substring search for one.
	inspectScope(scope, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SwitchStmt:
			// A case that NAMES a registered constant is a consumer however the
			// tag is spelled — `switch typ { case core.EventTypeLaunchInitiated:`
			// in internal/runloop is real and the tag says nothing. The tag gate
			// applies only to bare string literals, where it is the only thing
			// separating a consumer from a coincidence.
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
			// Only a CONSTANT reference counts here. A bare "run_completed"
			// string handed to strings.Contains is far more often prose than a
			// consumer, and crediting it would hide real orphans.
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

	// Pass D — zero-count assertions in tests. The shape is an `if` that fails
	// when a COUNT of some event type is not zero.
	//
	// The count part is load-bearing. `if pos := rec.positionOf("x"); pos != 0`
	// also compares against zero, but it claims the event came FIRST, not that
	// it never came — reading it as a zero-count assertion would report a
	// perfectly good ordering check as unfailable. So the compared expression
	// must resolve to a count: a len() call, or a call that says count in its
	// name. Anything else is left alone.
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

// zeroCountTypes returns the event types that ifs asserts fired zero times.
func zeroCountTypes(ifs *ast.IfStmt, inCore bool, known map[string]string) []string {
	// Bindings introduced by the if-statement's own init clause, so
	// `if evts := em.EventsOfType(T); len(evts) != 0` resolves.
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

// countCallRe matches a call that returns a count of something.
var countCallRe = regexp.MustCompile(`(?i)count`)

// resolveCount returns the expression whose count e compares, or nil when e is
// not a count at all. It follows one level of if-init binding at a time and
// stops after a few hops so a self-referential binding cannot spin.
func resolveCount(e ast.Expr, bindings map[string]ast.Expr, depth int) ast.Expr {
	if depth > 4 {
		return nil
	}
	switch x := e.(type) {
	case *ast.CallExpr:
		name := exprString(x.Fun)
		if name == "len" && len(x.Args) == 1 {
			// len() over anything is a count. Substitute the argument's
			// binding when there is one, so the event type is visible.
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

// ---------------------------------------------------------------------------
// Expression helpers
// ---------------------------------------------------------------------------

var eventConstRe = regexp.MustCompile(`^EventType[A-Z][A-Za-z0-9_]*$`)

// typedStringConstants returns every `Name typeName = "value"` constant in f,
// keyed by constant name.
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

// eventValuesIn returns every registered event-type value that e names, whether
// as a core.EventTypeX constant or as its own string literal.
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

// constantEventValuesIn is eventValuesIn restricted to named constants.
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

// isEventTypeExpr reports whether e is plausibly an event type: a registered
// constant, a core.EventType conversion, or a field or variable named Type.
// The last case is what admits `switch ev.Type { case "decision_required": }`
// without admitting every switch over every string in the tree.
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

// inspectScope walks scope like ast.Inspect but never descends into a nested
// function literal. Each literal is registered as a scope of its own, so
// descending would visit everything inside it twice — once under the enclosing
// function's name and once under the literal's — and every count this audit
// reports would be inflated for exactly the code that is hardest to read.
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

// forEachCompositeLit visits every composite literal in scope and reports the
// name of its type. Go lets the element type be elided inside a slice literal,
// so `[]Action{{Kind: ActEmit, ...}}` gives the inner literal a nil Type. This
// carries the outer element type down so Pass B2's type gate does not read an
// elided descriptor as an unknown struct.
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
		// Element type for any literal nested directly inside this one.
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

// lastIdent returns the rightmost identifier of e, so ActEmit and
// runexec.ActEmit both read as "ActEmit".
func lastIdent(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return x.Sel.Name
	}
	return ""
}

// ---------------------------------------------------------------------------
// Dynamic producer surfaces
// ---------------------------------------------------------------------------

// resolveDynamicSurface reads one declared surface out of the live source and
// returns the event-type values it can emit. It fails the test rather than
// returning an empty set when the surface has moved, because an entry that
// quietly resolves to nothing turns this whole audit into a rubber stamp.
func resolveDynamicSurface(t *testing.T, fset *token.FileSet, root string, s dynamicProducerSurface) []string {
	t.Helper()

	constFile, err := parser.ParseFile(fset, filepath.Join(root, s.ConstantsIn), nil, 0)
	if err != nil {
		t.Fatalf("dynamic surface %q: parse %s: %v", s.Name, s.ConstantsIn, err)
	}
	// The constant type is the map's key type. Take every typed string constant
	// in the file and index it by name.
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

// ---------------------------------------------------------------------------
// Tree walking
// ---------------------------------------------------------------------------

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
			// Nested agent worktrees and scratch checkouts live UNDER the repo root
			// here. They hold their own copy of the tree, so walking them both
			// double-counts every producer and parses whatever placeholder content a
			// tool left behind. A stray unparseable file under .claire/worktrees/
			// turned all three tests red on a clean tree, which is a false red in the
			// merge decision — the exact failure this file argues against.
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

// ---------------------------------------------------------------------------
// Known limits — what still gets past this
// ---------------------------------------------------------------------------
//
//  1. A type read at run time from a set this test has not been told about.
//     Only dynamicProducerSurfaces closes that, and only for surfaces someone
//     adds. A NEW runtime-typed emit shows up as a false orphan, which is the
//     safe direction: it demands attention rather than granting silence.
//
//  2. A constant that reaches an emitter through a return value or another
//     package. buildWatcherFailedPayload returns core.EventType(...) and its
//     caller emits it. Only the declared surface covers that instance. The flow
//     analysis here is intra-procedural on purpose — inter-procedural flow needs
//     type information this test does not load, and would still stop at case 1.
//
//  3. A consumer that pattern-matches a type string it builds by concatenation,
//     reads from config, or spells differently from the registry. The digest
//     sentinel weights are keyed by strings from a config file. This test cannot
//     see those keys.
//
//  4. A zero-count assertion whose comparison against 0 lives inside a helper
//     (assertNoEvents(t, typ)). The literal 0 is not in the caller's condition,
//     so Pass D does not see it. Writing the comparison at the assertion site
//     keeps it visible. A helper hides it.
//
//  5. Absence asserted by something other than a count — a golden-file diff, a
//     snapshot, a map that simply lacks a key. Those have no zero to find.
//
//  6. This test says nothing about an event that is emitted and consumed but
//     carries a payload no consumer reads. That is a different defect.
//
//  7. Pass B2's type gate is a name check, not a type check. A struct named
//     Action with a Kind field and a Type field, anywhere in the tree, is
//     credited. That is deliberate — resolving real types needs go/types and a
//     full load of every package — and it is a much narrower opening than the
//     field-name-only rule it replaced, but it is an opening.
//
//  8. shellEmitsActionType asks whether ANY production file still unpacks an
//     action descriptor and emits its Type. Two do today. If one of them lost
//     that call, the other keeps the guard quiet while the orphaned reactor's
//     descriptors stay credited.
//
//  9. The audited population is the `Name EventType = "value"` constants in
//     internal/core. Five types are registered by string alone through
//     mustRegister and have no constant — agent_input_acked, agent_input_stale,
//     agent_message, agent_presence, session_keeper_config_rejected. A consumer
//     of one of those is invisible to this audit.
//
//  10. internal/codexinput spells the reactor/shell idiom differently: the
//     discriminator is a Type field and the event name is in an Emit field, so
//     Pass B2 does not model it. Harmless today only because its event names
//     fall outside the population in limit 9.
//
//  11. Reachability is not checked. An emit call in a production function that
//     nothing calls still counts as a producer. If someone adds a dead emitter
//     for one of the recorded types, the row goes stale and
//     TestEventParity_NoRecordOutlivesItsDefect will say so, but it will say the
//     defect was fixed when it was only papered over. The stall_detected row
//     used to sit here and rested on exactly that argument, made by a human
//     rather than by the audit.
