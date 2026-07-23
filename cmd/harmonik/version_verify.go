// Authoritative binary-provenance check for `harmonik version --binary`.
//
// # WHY THIS EXISTS
//
// "Does this binary contain fix X?" used to be answered by folklore:
// `strings <binary> | grep <token>` (false-positives, because the Go linker
// packs unrelated strings adjacent in the string blob) or
// `go tool nm <binary> | grep <symbol>` (a per-fix trick that silently stops
// working when a symbol is renamed or inlined). Both infer provenance from
// artefacts of compilation. Neither answers the actual question.
//
// The Go toolchain already stamps the answer into every binary it builds:
// the `vcs.revision` / `vcs.modified` / `vcs.time` build settings, readable
// with `go version -m` or, here, `debug/buildinfo`. That is the single
// authoritative source, and it generalises: containment of ANY commit is
// "is that commit an ancestor of the binary's revision", with no per-fix
// symbol trick.
//
// Cite: bead hk-9hvr0 (false close caused by `strings | grep harmonik-input`);
// docs/daemon-redeploy.md §"Which fix is in this binary?".
package main

import (
	"context"
	"debug/buildinfo"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"strings"
)

// Result statuses reported by `harmonik version --binary`. They are stable
// tokens: printed as `status:` in human output and as `"status"` in --json.
const (
	// verifyStatusRevision — no --contains was requested; the binary's
	// revision and dirty flag were read successfully.
	verifyStatusRevision = "revision"
	// verifyStatusContains — the target commit is an ancestor of the
	// binary's revision and the tree it was built from was clean.
	verifyStatusContains = "contains"
	// verifyStatusMissing — the target commit is NOT an ancestor of the
	// binary's revision. The binary predates the fix.
	verifyStatusMissing = "missing"
	// verifyStatusContainsDirty — the target commit is an ancestor, but
	// vcs.modified=true: the revision is necessary, not sufficient, because
	// uncommitted edits were present at build time.
	verifyStatusContainsDirty = "contains-dirty"
	// verifyStatusNoBuildInfo — the file carries no Go build information
	// (not a Go binary, or stripped of it).
	verifyStatusNoBuildInfo = "no-build-info"
	// verifyStatusNoVCSStamp — build info is present but has no
	// vcs.revision (built with -buildvcs=false, or outside a worktree).
	verifyStatusNoVCSStamp = "no-vcs-stamp"
	// verifyStatusUnknownRevision — the binary names a revision that the
	// local repository does not have (shallow clone, or a rev rebased away).
	verifyStatusUnknownRevision = "unknown-revision"
)

// Exit codes. 0 is the ONLY code that means "this binary definitively carries
// the fix"; every other code is a distinguishable refusal, never a bare false.
const (
	verifyExitContains      = 0 // contains, built from a clean tree
	verifyExitMissing       = 1 // revision does not contain the commit
	verifyExitUsage         = 2 // bad flags, unreadable file, unusable repo
	verifyExitDirty         = 3 // contains, but built from a dirty tree
	verifyExitIndeterminate = 4 // provenance unreadable: cannot answer
)

// errNoBuildInfo marks a file that carries no Go build information.
var errNoBuildInfo = errors.New("no Go build information in binary")

// binaryStamp is the VCS provenance the Go toolchain embeds in a binary.
// A zero Revision means the build carried no vcs.revision setting.
type binaryStamp struct {
	Revision string
	Modified bool
	Time     string
}

// verifyResult is the full answer for one binary. It is rendered either as
// key/value lines or, with --json, as this struct.
type verifyResult struct {
	Binary    string `json:"binary"`
	Revision  string `json:"revision,omitempty"`
	Modified  bool   `json:"modified"`
	BuildTime string `json:"build_time,omitempty"`
	Contains  string `json:"contains,omitempty"`
	Status    string `json:"status"`
	Detail    string `json:"detail"`
	ExitCode  int    `json:"exit_code"`
}

// readBinaryStamp extracts the VCS build settings from the binary at path.
//
// A file that exists but carries no Go build information yields
// errNoBuildInfo. A missing/unreadable file yields the underlying fs error so
// the caller can report it as an operator mistake rather than a verdict.
func readBinaryStamp(path string) (binaryStamp, error) {
	info, err := buildinfo.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
			return binaryStamp{}, err
		}
		return binaryStamp{}, fmt.Errorf("%w: %w", errNoBuildInfo, err)
	}
	stamp := binaryStamp{}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			stamp.Revision = s.Value
		case "vcs.modified":
			stamp.Modified = s.Value == "true"
		case "vcs.time":
			stamp.Time = s.Value
		}
	}
	return stamp, nil
}

// commitSpec peels a revision to its commit object, so that an existence
// check rejects a name that resolves to a tree/blob or does not resolve at
// all. `git rev-parse --verify` is NOT a substitute: it accepts any well-formed
// 40-hex name whether or not the object is present.
func commitSpec(rev string) string { return rev + "^{commit}" }

// gitObjectExists reports whether repoDir's object database holds the object
// named by spec (a git revision expression, e.g. the output of commitSpec).
//
// An error means git itself could not be run — that is an environment
// problem, distinct from "the object is absent", which is (false, nil).
func gitObjectExists(ctx context.Context, repoDir, spec string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoDir, "cat-file", "-e", spec)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return false, nil
		}
		return false, fmt.Errorf("git cat-file in %s: %w", repoDir, err)
	}
	return true, nil
}

// gitIsAncestor reports whether ancestor is reachable from descendant.
// `git merge-base --is-ancestor` exits 0 for yes and 1 for no; any other exit
// status is a real failure and is returned as an error.
func gitIsAncestor(ctx context.Context, repoDir, ancestor, descendant string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoDir, "merge-base", "--is-ancestor", ancestor, descendant)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("git merge-base --is-ancestor %s %s: %w", ancestor, descendant, err)
}

// gitIsRepo reports whether repoDir is inside a git working tree.
func gitIsRepo(ctx context.Context, repoDir string) bool {
	cmd := exec.CommandContext(ctx, "git", "-C", repoDir, "rev-parse", "--git-dir")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run() == nil
}

// classifyStamp turns a binary's stamp into a verdict.
//
// When target is empty the verdict is purely informational (which revision,
// dirty or not). When target is non-empty the verdict answers containment,
// and every honest failure mode gets its own status token:
//
//	no-vcs-stamp      — the build did not record a revision at all
//	unknown-revision  — the revision is not in this repository
//	contains-dirty    — ancestor, but built from a modified tree
//
// A returned error means the environment (git, the repo) was unusable, not
// that a verdict was reached.
func classifyStamp(ctx context.Context, stamp binaryStamp, repoDir, target string) (verifyResult, error) {
	res := verifyResult{
		Revision:  stamp.Revision,
		Modified:  stamp.Modified,
		BuildTime: stamp.Time,
		Contains:  target,
	}
	if stamp.Revision == "" {
		res.Status = verifyStatusNoVCSStamp
		res.Detail = "binary has Go build info but no vcs.revision (built with -buildvcs=false or outside a worktree); provenance cannot be established"
		res.ExitCode = verifyExitIndeterminate
		return res, nil
	}
	if target == "" {
		res.Status = verifyStatusRevision
		res.Detail = stampCleanlinessDetail(stamp)
		res.ExitCode = verifyExitContains
		return res, nil
	}
	return classifyContainment(ctx, stamp, repoDir, target, res)
}

// classifyContainment answers "is target an ancestor of the binary's
// revision" for a stamp already known to carry a revision.
func classifyContainment(ctx context.Context, stamp binaryStamp, repoDir, target string, res verifyResult) (verifyResult, error) {
	if !gitIsRepo(ctx, repoDir) {
		return res, fmt.Errorf("%s is not a git repository; pass --repo DIR", repoDir)
	}
	targetOK, err := gitObjectExists(ctx, repoDir, commitSpec(target))
	if err != nil {
		return res, err
	}
	if !targetOK {
		return res, fmt.Errorf("commit %s does not exist in %s", target, repoDir)
	}
	revOK, err := gitObjectExists(ctx, repoDir, commitSpec(stamp.Revision))
	if err != nil {
		return res, err
	}
	if !revOK {
		res.Status = verifyStatusUnknownRevision
		res.Detail = fmt.Sprintf("revision %s is not in %s (shallow clone, or the rev was rebased away); containment cannot be decided", stamp.Revision, repoDir)
		res.ExitCode = verifyExitIndeterminate
		return res, nil
	}
	isAncestor, err := gitIsAncestor(ctx, repoDir, target, stamp.Revision)
	if err != nil {
		return res, err
	}
	return containmentVerdict(stamp, target, isAncestor, res), nil
}

// containmentVerdict picks the status/exit pair once ancestry is known.
func containmentVerdict(stamp binaryStamp, target string, isAncestor bool, res verifyResult) verifyResult {
	switch {
	case !isAncestor:
		res.Status = verifyStatusMissing
		res.Detail = fmt.Sprintf("%s is NOT an ancestor of %s: this binary predates the commit", target, stamp.Revision)
		if stamp.Modified {
			res.Detail += " (tree was dirty at build time, so unrecorded local edits cannot be ruled in or out)"
		}
		res.ExitCode = verifyExitMissing
	case stamp.Modified:
		res.Status = verifyStatusContainsDirty
		res.Detail = fmt.Sprintf("%s is an ancestor of %s, but vcs.modified=true: the revision is necessary, not sufficient — uncommitted edits were present at build time", target, stamp.Revision)
		res.ExitCode = verifyExitDirty
	default:
		res.Status = verifyStatusContains
		res.Detail = fmt.Sprintf("%s is an ancestor of %s, built from a clean tree", target, stamp.Revision)
		res.ExitCode = verifyExitContains
	}
	return res
}

// stampCleanlinessDetail phrases the dirty flag for informational output.
func stampCleanlinessDetail(stamp binaryStamp) string {
	if stamp.Modified {
		return "built from a DIRTY tree (vcs.modified=true): the revision alone does not describe this binary"
	}
	return "built from a clean tree (vcs.modified=false)"
}

// versionVerifyUsage is the help text for `harmonik version --binary`.
const versionVerifyUsage = `harmonik version --binary — read a binary's build provenance and test commit containment

USAGE
  harmonik version
  harmonik version --binary PATH [--contains COMMIT] [--repo DIR] [--json]

FLAGS
  --binary PATH     Binary to inspect (default: the running harmonik executable)
  --contains COMMIT Test whether COMMIT is an ancestor of the binary's revision
  --repo DIR        Repository whose commit graph decides ancestry (default: cwd)
  --json            Emit the full result as a JSON object

STATUS TOKENS / EXIT CODES
  contains         0  target commit is an ancestor; tree was clean  -> SAFE TO SWAP
  revision         0  no --contains asked; revision reported
  missing          1  target commit is NOT an ancestor; binary predates the fix
  (usage error)    2  bad flags, unreadable binary, unusable repo
  contains-dirty   3  ancestor, but vcs.modified=true — necessary, not sufficient
  no-build-info    4  file carries no Go build info (not a Go binary / stripped)
  no-vcs-stamp     4  build info present but no vcs.revision (-buildvcs=false)
  unknown-revision 4  binary's revision is not in this repo (shallow / rebased away)

WHY NOT strings | grep
  ` + "`strings <binary> | grep <token>`" + ` false-positives: the Go linker packs
  unrelated strings adjacent in the string blob, so a substring can appear in a
  binary that does not contain the fix. ` + "`go tool nm | grep <symbol>`" + ` works but
  must be reinvented per fix and breaks silently on rename or inlining. The
  vcs.revision stamp read here is the authoritative source.

EXAMPLES
  harmonik version --binary /Users/me/go/bin/harmonik
  harmonik version --binary /Users/me/go/bin/harmonik --contains 2b921fdef && echo SAFE
`

// versionVerifyFlags is the parsed form of the --binary flag set.
type versionVerifyFlags struct {
	binary   string
	contains string
	repo     string
	asJSON   bool
	help     bool
}

// parseVersionVerifyFlags parses the arguments following `harmonik version`.
func parseVersionVerifyFlags(args []string) (versionVerifyFlags, error) {
	f := versionVerifyFlags{}
	valued := map[string]*string{
		"--binary":   &f.binary,
		"--contains": &f.contains,
		"--repo":     &f.repo,
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--help" || arg == "-h" {
			f.help = true
			return f, nil
		}
		if arg == "--json" {
			f.asJSON = true
			continue
		}
		name, value, hasValue := strings.Cut(arg, "=")
		dst, known := valued[name]
		if !known {
			return f, fmt.Errorf("unknown flag %q", arg)
		}
		if hasValue {
			*dst = value
			continue
		}
		if i+1 >= len(args) {
			return f, fmt.Errorf("flag %s requires a value", name)
		}
		i++
		*dst = args[i]
	}
	return f, nil
}

// applyDefaults fills --binary from the running executable and --repo from the
// working directory.
func (f *versionVerifyFlags) applyDefaults() error {
	if f.binary == "" {
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("cannot resolve the running executable: %w", err)
		}
		f.binary = exe
	}
	if f.repo == "" {
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("cannot determine working directory: %w", err)
		}
		f.repo = wd
	}
	return nil
}

// versionInspectOutcome is a fully rendered answer plus where it goes and what
// the process should exit with. Keeping rendering separate from writing gives
// the command exactly one write site, whose error is checked.
type versionInspectOutcome struct {
	text     string
	toStderr bool
	code     int
}

// runVersionInspect implements `harmonik version <flags>`.
//
// It is the single authoritative answer to "which commit is this binary built
// from, and does it contain commit X" — see the package-level rationale above.
func runVersionInspect(args []string, stdout, stderr io.Writer) int {
	out := computeVersionInspect(context.Background(), args)
	w := stdout
	if out.toStderr {
		w = stderr
	}
	if _, err := io.WriteString(w, out.text); err != nil {
		return verifyExitUsage
	}
	return out.code
}

// computeVersionInspect resolves flags, reads provenance and renders the
// answer. It writes nothing.
func computeVersionInspect(ctx context.Context, args []string) versionInspectOutcome {
	f, err := parseVersionVerifyFlags(args)
	if err != nil {
		return versionInspectFailure(fmt.Sprintf("harmonik version: %v\n\n%s", err, versionVerifyUsage))
	}
	if f.help {
		return versionInspectOutcome{text: versionVerifyUsage, code: verifyExitContains}
	}
	if err := f.applyDefaults(); err != nil {
		return versionInspectFailure(fmt.Sprintf("harmonik version: %v\n", err))
	}
	res, err := inspectBinary(ctx, f)
	if err != nil {
		return versionInspectFailure(fmt.Sprintf("harmonik version: %v\n", err))
	}
	text, err := renderVerifyResult(res, f.asJSON)
	if err != nil {
		return versionInspectFailure(fmt.Sprintf("harmonik version: %v\n", err))
	}
	return versionInspectOutcome{text: text, code: res.ExitCode}
}

// versionInspectFailure builds the stderr/exit-2 outcome for an operator error.
func versionInspectFailure(text string) versionInspectOutcome {
	return versionInspectOutcome{text: text, toStderr: true, code: verifyExitUsage}
}

// inspectBinary reads and classifies one binary. A returned error is an
// environment/operator failure (unreadable file, unusable repo); a file with
// no Go build info is a verdict, not an error.
func inspectBinary(ctx context.Context, f versionVerifyFlags) (verifyResult, error) {
	stamp, err := readBinaryStamp(f.binary)
	if err != nil {
		if errors.Is(err, errNoBuildInfo) {
			return verifyResult{
				Binary:   f.binary,
				Contains: f.contains,
				Status:   verifyStatusNoBuildInfo,
				Detail:   "file carries no Go build information (not a Go binary, or the build info was stripped); provenance cannot be established",
				ExitCode: verifyExitIndeterminate,
			}, nil
		}
		return verifyResult{}, fmt.Errorf("cannot read %s: %w", f.binary, err)
	}
	res, err := classifyStamp(ctx, stamp, f.repo, f.contains)
	if err != nil {
		return verifyResult{}, err
	}
	res.Binary = f.binary
	return res, nil
}

// renderVerifyResult renders a verdict as JSON or as key/value lines.
func renderVerifyResult(res verifyResult, asJSON bool) (string, error) {
	if asJSON {
		encoded, err := json.MarshalIndent(res, "", "  ")
		if err != nil {
			return "", fmt.Errorf("cannot encode result: %w", err)
		}
		return string(encoded) + "\n", nil
	}
	out := fmt.Sprintf("binary:   %s\n", res.Binary)
	if res.Revision != "" {
		out += fmt.Sprintf("revision: %s (vcs.modified=%t)\n", res.Revision, res.Modified)
	}
	if res.BuildTime != "" {
		out += fmt.Sprintf("built:    %s\n", res.BuildTime)
	}
	if res.Contains != "" {
		out += fmt.Sprintf("contains: %s\n", res.Contains)
	}
	out += fmt.Sprintf("status:   %s\n", res.Status)
	out += fmt.Sprintf("detail:   %s\n", res.Detail)
	return out, nil
}
