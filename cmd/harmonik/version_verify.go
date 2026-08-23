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

const (
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

const (
	verifyExitOK            = 0
	verifyExitMissing       = 1 // revision does not contain the commit
	verifyExitUsage         = 2 // bad flags, unreadable file, unusable repo
	verifyExitDirty         = 3 // contains, but built from a dirty tree
	verifyExitIndeterminate = 4 // provenance unreadable: cannot answer
)

var errNoBuildInfo = errors.New("no Go build information in binary")

type binaryStamp struct {
	Revision string
	Modified bool
	Time     string
}

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

func commitSpec(rev string) string { return rev + "^{commit}" }

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

func gitIsRepo(ctx context.Context, repoDir string) bool {
	cmd := exec.CommandContext(ctx, "git", "-C", repoDir, "rev-parse", "--git-dir")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run() == nil
}

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

func classifyContainment(ctx context.Context, stamp binaryStamp, repoDir, target string, res verifyResult) (verifyResult, error) {
	if !gitIsRepo(ctx, repoDir) {
		return res, fmt.Errorf("%s is not a git repository; pass --repo DIR", repoDir)
	}
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

func stampCleanlinessDetail(stamp binaryStamp) string {
	if stamp.Modified {
		return "built from a DIRTY tree (vcs.modified=true): the revision alone does not describe this binary"
	}
	return "built from a clean tree (vcs.modified=false)"
}

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

type versionVerifyFlags struct {
	binary   string
	contains string
	repo     string
	asJSON   bool
	help     bool
}

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

type versionInspectOutcome struct {
	text     string
	toStderr bool
	code     int
}

func versionArgsRouteToInspect(argv []string) bool {
	return len(argv) > 2 && argv[1] == "version"
}

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

func computeVersionInspect(ctx context.Context, args []string) versionInspectOutcome {
	f, err := parseVersionVerifyFlags(args)
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

func argsRequestJSON(args []string) bool {
	for _, arg := range args {
		if arg == "--json" {
			return true
		}
	}
	return false
}

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
