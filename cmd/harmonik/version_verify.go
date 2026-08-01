// Authoritative binary-provenance check for `harmonik version --binary`.
//
// # WHY THIS EXISTS
//
// "Does this binary contain fix X?" used to be answered one fix at a time, by
// probing for an artefact of that particular change: `strings <binary> | grep
// <literal>` or `go tool nm <binary> | grep <symbol>`. Both test an INCIDENTAL
// by-product of a fix rather than the fix:
//
//   - the literal or symbol can be renamed, inlined or dropped while the fix is
//     present, which reads as "missing";
//   - it can survive a later revert or refactor while the fix is gone, which
//     reads as "present";
//   - and a fix that introduces no new literal or symbol at all — a changed
//     comparison, a reordered call — leaves the technique with nothing to probe.
//
// So the probe has to be reinvented for every fix, and for some fixes it cannot
// be invented at all. It is not always wrong: for hk-9hvr0 it happened to be
// right, because that fix deleted a string constant — measured 2026-07-22,
// `strings -a … | grep -c harmonik-input` returned 1 on the pre-fix binary
// (revision eb2b4f1a) and 0 on the post-fix one. Being right about one fix is
// precisely what does not generalise.
//
// The Go toolchain already records the answer to the question actually being
// asked — WHICH SOURCE REVISION IS THIS BINARY — in every binary it builds:
// the `vcs.revision` / `vcs.modified` / `vcs.time` build settings, readable
// with `go version -m` or, here, `debug/buildinfo`. One probe, identical for
// every fix; containment then reduces to commit ancestry.
//
// Ancestry has its own limits and they are stated where operators see them
// (docs/daemon-redeploy.md §"Which fix is in this binary?"): it answers "was
// this commit ever merged into the binary's history", so a commit that was
// later REVERTED still reads as contained, and a fix that reached the binary as
// a CHERRY-PICK reads as missing under its original SHA. Both were reproduced —
// the cherry-pick one in this repository. When either applies, confirm by content
// (`git grep <symbol> <revision>`); the deploy gate is unaffected because there
// the target commit IS the revision the binary was built from.
//
// Refs: bead hk-9hvr0; docs/daemon-redeploy.md §"Which fix is in this binary?".
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

	"github.com/gregberns/harmonik/internal/gitprobe"
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
	// verifyStatusUsageError — the command could not be carried out at all:
	// bad flags, unreadable binary, unusable repo, or a --contains commit this
	// repository does not have. Reported only in --json mode, where every
	// outcome must be a JSON object on stdout; human mode prints prose to
	// stderr.
	verifyStatusUsageError = "usage-error"
)

// Exit codes. With --contains, 0 is the ONLY code that means "this binary
// definitively carries the commit"; every other code is a distinguishable
// refusal, never a bare false.
const (
	// verifyExitOK is the success code, shared by three outcomes: status
	// `contains` (the ship-safe one), status `revision` (informational, no
	// --contains was asked) and --help. A caller that needs the ship-safe
	// meaning must check the status token as well as the exit code — which is
	// exactly what the swap gate in docs/daemon-redeploy.md does.
	verifyExitOK            = 0
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
		res.ExitCode = verifyExitOK
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
	// An absent --contains target and an absent binary revision are treated
	// asymmetrically ON PURPOSE. --contains is OPERATOR INPUT: a typo or a
	// commit this repo has never seen is a usage error the operator can fix by
	// re-running the command (exit 2). The binary's revision is DATA READ FROM
	// THE ARTEFACT: if this repo cannot resolve it, the operator's command was
	// well formed and the tool simply cannot decide, which is
	// `unknown-revision` (exit 4). Collapsing the two would either invite a
	// typo'd SHA to be reported as an unanswerable provenance question, or
	// blame the operator for a shallow clone.
	targetOK, err := gitObjectExists(ctx, repoDir, commitSpec(target))
	if err != nil {
		return res, err
	}
	if !targetOK {
		return res, fmt.Errorf("commit %s does not exist in %s (a --contains target is operator input: check the SHA, or pass --repo DIR)", target, repoDir)
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
	isAncestor, err := gitprobe.IsAncestor(ctx, repoDir, target, stamp.Revision)
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
		res.ExitCode = verifyExitOK
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
  --json            Emit every outcome — including usage errors — as a single
                    JSON object on stdout

STATUS TOKENS / EXIT CODES
  contains         0  target commit is an ancestor; tree was clean  -> SAFE TO SWAP
  revision         0  no --contains asked; revision reported
  missing          1  target commit is NOT an ancestor; binary predates the fix
  usage-error      2  bad flags, unreadable binary, unusable repo, or a
                      --contains commit this repository does not have
  contains-dirty   3  ancestor, but vcs.modified=true — necessary, not sufficient
  no-build-info    4  file carries no Go build info (not a Go binary / stripped)
  no-vcs-stamp     4  build info present but no vcs.revision (-buildvcs=false)
  unknown-revision 4  binary's revision is not in this repo (shallow / rebased away)

  Exit 0 alone is NOT the ship-safe answer: --help and status ` + "`revision`" + ` also
  exit 0. Gate on the status token as well — see docs/daemon-redeploy.md.

WHY NOT strings | grep OR go tool nm | grep
  Both probe an INCIDENTAL artefact of one fix — a string literal or a symbol.
  It can be renamed, inlined or dropped while the fix is present, it can survive
  a revert while the fix is gone, and a fix that adds no new literal or symbol
  leaves nothing to probe at all. So the trick must be reinvented per fix, and
  for some fixes it cannot be invented. The vcs.revision stamp read here answers
  the question actually being asked — which source revision is this binary —
  identically for every fix.

  Containment is commit ancestry, so it answers "was this commit ever merged
  into the binary's history": a later-REVERTED commit still reads as contained,
  and a fix that arrived by CHERRY-PICK reads as missing under its original SHA.
  Confirm by content when either applies.

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

// versionArgsRouteToInspect reports whether argv (os.Args, program name at
// index 0) selects the binary-provenance check rather than the version line.
//
// Only the POSITIONAL `version` subcommand routes here. `--version` and
// `-version` never do, whatever follows them: specs/release-pipeline.md §2.3
// makes their output format normative ("harmonik v0.y.z (commit: <sha>)") and
// declares any other format a spec violation, so `harmonik --version --json`
// must keep printing the version line.
func versionArgsRouteToInspect(argv []string) bool {
	return len(argv) > 2 && argv[1] == "version"
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
	// Flag parsing can fail before it reaches --json, so the JSON decision is
	// made independently of the parser: a caller that asked for JSON gets JSON
	// on EVERY outcome, including the failures below.
	f.asJSON = f.asJSON || argsRequestJSON(args)
	if err != nil {
		return versionInspectFailure(f, fmt.Sprintf("harmonik version: %v\n\n%s", err, versionVerifyUsage), err.Error())
	}
	if f.help {
		return versionInspectOutcome{text: versionVerifyUsage, code: verifyExitOK}
	}
	if err := f.applyDefaults(); err != nil {
		return versionInspectFailure(f, fmt.Sprintf("harmonik version: %v\n", err), err.Error())
	}
	res, err := inspectBinary(ctx, f)
	if err != nil {
		return versionInspectFailure(f, fmt.Sprintf("harmonik version: %v\n", err), err.Error())
	}
	text, err := renderVerifyResult(res, f.asJSON)
	if err != nil {
		return versionInspectFailure(f, fmt.Sprintf("harmonik version: %v\n", err), err.Error())
	}
	return versionInspectOutcome{text: text, code: res.ExitCode}
}

// argsRequestJSON reports whether --json appears anywhere in args, regardless of
// whether the argument list as a whole parses.
func argsRequestJSON(args []string) bool {
	for _, arg := range args {
		if arg == "--json" {
			return true
		}
	}
	return false
}

// versionInspectFailure builds the exit-2 outcome for an operator error.
//
// Human mode writes prose to stderr. --json mode writes one JSON object with
// status `usage-error` to STDOUT, so a machine consumer parses stdout for every
// outcome rather than only for the ones that reached a verdict.
func versionInspectFailure(f versionVerifyFlags, humanText, detail string) versionInspectOutcome {
	if f.asJSON {
		res := verifyResult{
			Binary:   f.binary,
			Contains: f.contains,
			Status:   verifyStatusUsageError,
			Detail:   detail,
			ExitCode: verifyExitUsage,
		}
		if encoded, err := json.MarshalIndent(res, "", "  "); err == nil {
			return versionInspectOutcome{text: string(encoded) + "\n", code: verifyExitUsage}
		}
	}
	return versionInspectOutcome{text: humanText, toStderr: true, code: verifyExitUsage}
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
