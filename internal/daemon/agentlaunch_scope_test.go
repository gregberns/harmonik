package daemon

// agentlaunch_scope_test.go — pins the ONE divergence the launch-path collapse
// deliberately kept.
//
// runAgentLaunch (agentlaunch.go) exists because three hand-written launch paths
// drifted from each other seventeen times. Collapsing them makes that class of
// drift impossible — except for agentLaunchSandboxScope, which is a real
// three-way behavioural difference that was preserved on purpose because
// normalizing it in either direction is a live production change:
//
//	cognition gate  → sandboxScopeNone         (never srt-sandboxed)
//	DOT agentic node → sandboxScopeCapturedOnly (exec branch only)
//	single-mode      → sandboxScopeAll          (both branches + the go-cache redirect)
//
// A preserved divergence with nothing watching it is exactly as silently
// driftable as the seventeen that were deleted. This test is what stops that: it
// reads the scope each call site actually passes and pins it. If someone
// "tidies" the three to agree, this fails and says which one moved — which is
// the notification that a decision is being made, not a cleanup.
//
// It is a source-level assertion rather than a behavioural one because reaching
// the branch behaviourally needs a live srt backend, a configured sandbox
// harness list, and (for two of the three) a full DOT graph run. The scope
// VALUES are enforced behaviourally elsewhere (sandboxgate_test.go covers
// sandboxSpawnForRun's own gate); what is pinned here is only which site gets
// which.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

func TestAgentLaunchSandboxScope_PerSiteDivergenceIsPinned(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		file string
		want string
	}{
		{
			name: "cognition gate is never srt-sandboxed",
			file: "dot_gate.go",
			want: "sandboxScopeNone",
		},
		{
			name: "DOT agentic node sandboxes only the captured-session-id exec branch",
			file: "dot_cascade_core.go",
			want: "sandboxScopeCapturedOnly",
		},
		{
			name: "single-mode sandboxes both branches",
			file: "workloop.go",
			want: "sandboxScopeAll",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(repoRootForConformance(), "internal", "daemon", tc.file)
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parse %s: %v", tc.file, err)
			}

			got := agentLaunchScopeArgs(file)
			if len(got) != 1 {
				t.Fatalf("%s: want exactly one agentLaunchInput literal with a SandboxScope field, got %d (%v) — a second launch site in this file means the collapse regressed",
					tc.file, len(got), got)
			}
			if got[0] != tc.want {
				t.Errorf("%s passes SandboxScope: %s, want %s.\nThis is a PRESERVED divergence, not an oversight: see agentLaunchSandboxScope in agentlaunch.go. Changing it is a production behaviour change (it starts or stops sandboxing real runs), so if the change is intended, make the decision explicitly and update this pin with it.",
					tc.file, got[0], tc.want)
			}
		})
	}
}

// agentLaunchScopeArgs returns the identifier passed as SandboxScope in every
// agentLaunchInput composite literal in file. A non-identifier value (a call, a
// conditional, a variable computed elsewhere) is reported verbatim as "<dynamic>"
// so it fails loudly rather than being silently skipped — routing the scope
// through a variable would hide the divergence from every reader, which is the
// thing this pin exists to prevent.
func agentLaunchScopeArgs(file *ast.File) []string {
	var scopes []string
	ast.Inspect(file, func(node ast.Node) bool {
		lit, ok := node.(*ast.CompositeLit)
		if !ok || !isIdent(lit.Type, "agentLaunchInput") {
			return true
		}
		for _, elt := range lit.Elts {
			kv, kvOK := elt.(*ast.KeyValueExpr)
			if !kvOK || !isIdent(kv.Key, "SandboxScope") {
				continue
			}
			if ident, identOK := unparenExpr(kv.Value).(*ast.Ident); identOK {
				scopes = append(scopes, ident.Name)
			} else {
				scopes = append(scopes, "<dynamic>")
			}
		}
		return true
	})
	return scopes
}
