package scenario

import "fmt"

// GitSeedOpKind is the operation discriminator for GitSeedOp.
//
// Spec ref: specs/scenario-harness.md §6.1 — RECORD GitSeedOp, §6.3 — per-op
// args interpretation table.
type GitSeedOpKind string

// GitSeedOpKind values per specs/scenario-harness.md §6.1 ENUM declaration.
// Values are lowercase per the spec's convention for this type.
const (
	GitSeedOpCommit   GitSeedOpKind = "commit"
	GitSeedOpBranch   GitSeedOpKind = "branch"
	GitSeedOpTag      GitSeedOpKind = "tag"
	GitSeedOpCheckout GitSeedOpKind = "checkout"
)

// gitSeedOpRequiredKeys maps each GitSeedOpKind to the args keys that MUST be
// present for a GitSeedOp to be structurally well-formed. Optional keys
// (parent, ref, from, target) are not listed; their absence does not
// invalidate the op.
//
// Source of truth: specs/scenario-harness.md §6.3 — GitSeedOp.args
// interpretation table.
var gitSeedOpRequiredKeys = map[GitSeedOpKind][]string{
	GitSeedOpCommit:   {"message"},
	GitSeedOpBranch:   {"name"},
	GitSeedOpTag:      {"name"},
	GitSeedOpCheckout: {"ref"},
}

// Valid reports whether k is one of the four declared GitSeedOpKind constants.
func (k GitSeedOpKind) Valid() bool {
	switch k {
	case GitSeedOpCommit, GitSeedOpBranch, GitSeedOpTag, GitSeedOpCheckout:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so GitSeedOpKind serialises
// correctly in JSON and YAML scenario files.
func (k GitSeedOpKind) MarshalText() ([]byte, error) {
	if !k.Valid() {
		return nil, fmt.Errorf("gitseedopkind: unknown value %q", string(k))
	}
	return []byte(k), nil
}

// UnmarshalText implements encoding.TextUnmarshaler. It rejects any value that
// is not one of the four declared constants.
func (k *GitSeedOpKind) UnmarshalText(text []byte) error {
	v := GitSeedOpKind(text)
	if !v.Valid() {
		return fmt.Errorf("gitseedopkind: unknown value %q; must be one of commit, branch, tag, checkout", string(text))
	}
	*k = v
	return nil
}

// GitSeedOp is a single git operation applied to seed the workspace tree
// before scenario orchestration begins. Per-op interpretation of Args is
// declared in specs/scenario-harness.md §6.3.
//
// Spec ref: specs/scenario-harness.md §6.1 — RECORD GitSeedOp.
type GitSeedOp struct {
	// Op is the git operation to perform. Required.
	Op GitSeedOpKind `json:"op" yaml:"op"`

	// Args holds per-op key/value parameters. Required keys per op are
	// declared in specs/scenario-harness.md §6.3. Unknown keys are tolerated
	// (the table specifies required keys, not an exhaustive set).
	Args map[string]string `json:"args" yaml:"args"`
}

// Valid reports whether the GitSeedOp is structurally well-formed:
//   - Op is one of the four declared GitSeedOpKind constants.
//   - Args contains all required keys for the given op per §6.3.
//
// Optional keys (parent, ref, from, target) need not be present. Unknown
// extra keys are tolerated.
func (g GitSeedOp) Valid() bool {
	if !g.Op.Valid() {
		return false
	}
	for _, key := range gitSeedOpRequiredKeys[g.Op] {
		if _, ok := g.Args[key]; !ok {
			return false
		}
	}
	return true
}
