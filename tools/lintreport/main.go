// Command lintreport judges golangci-lint JSON against an explicit list of
// findings the tree is allowed to retain.
package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type issue struct {
	FromLinter string `json:"FromLinter"`
	Text       string `json:"Text"`
	Pos        struct {
		Filename string `json:"Filename"`
		Line     int    `json:"Line"`
	} `json:"Pos"`
}

type report struct {
	Issues []issue `json:"Issues"`
}

// key identifies a finding by its content. It intentionally contains no path
// or package: moving unchanged code across a package boundary must not mint a
// new exemption.
type key struct {
	digest string
	linter string
}

type finding struct {
	message  string
	location string
}

func main() {
	allowPath := flag.String("allow", "", "path to the allow list")
	write := flag.Bool("write", false, "rewrite the allow list from the current findings")
	flag.Parse()
	if flag.NArg() != 1 || *allowPath == "" {
		fmt.Fprintln(os.Stderr, "usage: lintreport -allow <file> [-write] <golangci-lint.json>")
		os.Exit(2)
	}
	findings, err := readFindings(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "lintreport:", err)
		os.Exit(2)
	}
	if *write {
		if err := writeAllow(*allowPath, findings); err != nil {
			fmt.Fprintln(os.Stderr, "lintreport:", err)
			os.Exit(2)
		}
		fmt.Printf("wrote %d allowed findings to %s\n", len(findings), *allowPath)
		return
	}
	allow, err := readAllow(*allowPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "lintreport:", err)
		os.Exit(2)
	}
	verdict, err := judge(os.Stdout, findings, allow)
	if err != nil {
		fmt.Fprintln(os.Stderr, "lintreport:", err)
		os.Exit(2)
	}
	os.Exit(verdict)
}

func readFindings(path string) (map[key][]finding, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- the CLI argument is the report to inspect.
	if err != nil {
		return nil, err
	}
	var r report
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	out := map[key][]finding{}
	parsed := map[string]*sourceFile{}
	for _, iss := range r.Issues {
		name := filepath.Clean(iss.Pos.Filename)
		sf, ok := parsed[name]
		if !ok {
			sf, err = parseSource(name)
			if err != nil {
				return nil, fmt.Errorf("resolve %s:%d: %w", name, iss.Pos.Line, err)
			}
			parsed[name] = sf
		}
		body, symbol, err := sf.enclosing(iss.Pos.Line)
		if err != nil {
			return nil, fmt.Errorf("normalize %s:%d: %w", name, iss.Pos.Line, err)
		}
		digest := findingDigest(iss.FromLinter, iss.Text, body)
		k := key{digest: digest, linter: iss.FromLinter}
		loc := fmt.Sprintf("%s:%d %s", filepath.ToSlash(name), iss.Pos.Line, symbol)
		out[k] = append(out[k], finding{message: fmt.Sprintf("%s: %s", loc, iss.Text), location: loc})
	}
	return out, nil
}

type sourceFile struct {
	fset *token.FileSet
	file *ast.File
	raw  []byte
}

func parseSource(path string) (*sourceFile, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- the linter supplies the source path.
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, raw, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	return &sourceFile{fset: fset, file: f, raw: raw}, nil
}

func (s *sourceFile) enclosing(line int) (body []byte, symbol string, err error) {
	var best ast.Node
	ast.Inspect(s.file, func(n ast.Node) bool {
		if n == nil {
			return true
		}
		start, end := s.fset.Position(n.Pos()).Line, s.fset.Position(n.End()).Line
		if start <= line && line <= end && identityNode(n) {
			if best == nil || n.End()-n.Pos() < best.End()-best.Pos() {
				best = n
			}
		}
		return true
	})
	if best == nil { // Package comments and malformed positions use the whole file sans package name.
		var b bytes.Buffer
		for _, d := range s.file.Decls {
			if err := format.Node(&b, s.fset, d); err != nil {
				return nil, "", err
			}
		}
		return b.Bytes(), "file scope", nil
	}
	var b bytes.Buffer
	if err := format.Node(&b, s.fset, best); err != nil {
		return nil, "", err
	}
	return b.Bytes(), nodeName(best), nil
}

func identityNode(n ast.Node) bool {
	switch n.(type) {
	case *ast.FuncDecl, *ast.GenDecl, *ast.TypeSpec, *ast.ValueSpec, *ast.ImportSpec:
		return true
	default:
		return false
	}
}

func nodeName(n ast.Node) string {
	switch x := n.(type) {
	case *ast.FuncDecl:
		if x.Recv != nil && len(x.Recv.List) > 0 {
			return exprName(x.Recv.List[0].Type) + "." + x.Name.Name
		}
		return x.Name.Name
	case *ast.TypeSpec:
		return x.Name.Name
	case *ast.ValueSpec:
		if len(x.Names) > 0 {
			return x.Names[0].Name
		}
	case *ast.ImportSpec:
		return "import " + x.Path.Value
	case *ast.GenDecl:
		return x.Tok.String()
	}
	return "file scope"
}

func exprName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.StarExpr:
		return exprName(x.X)
	case *ast.IndexExpr:
		return exprName(x.X)
	case *ast.IndexListExpr:
		return exprName(x.X)
	default:
		return "receiver"
	}
}

func findingDigest(linter, text string, body []byte) string {
	normalizedText := strings.Join(strings.Fields(text), " ")
	h := sha256.New()
	_, _ = h.Write([]byte(linter + "\x00" + normalizedText + "\x00"))
	_, _ = h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

func readAllow(path string) (map[key]bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	allow := map[key]bool{}
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(strings.SplitN(sc.Text(), "#", 2)[0])
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 2 || len(parts[0]) != sha256.Size*2 {
			return nil, fmt.Errorf("%s line %d: want '<sha256> <linter>', got %q", path, n, line)
		}
		if _, err := hex.DecodeString(parts[0]); err != nil {
			return nil, fmt.Errorf("%s line %d: invalid digest", path, n)
		}
		allow[key{digest: parts[0], linter: parts[1]}] = true
	}
	return allow, sc.Err()
}

func writeAllow(path string, findings map[key][]finding) error {
	keys := sortedKeys(findings)
	var b strings.Builder
	b.WriteString("# Lint findings this tree is allowed to still have.\n")
	b.WriteString("# One line per content identity and linter. Location comments are only aids.\n")
	b.WriteString("# A finding NOT on this list fails the build.\n")
	b.WriteString("# Seeded from the tree as it stood when the gate was introduced.\n\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "%s\t%s\t# %s\n", k.digest, k.linter, findings[k][0].location)
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func judge(out *os.File, findings map[key][]finding, allow map[key]bool) (int, error) {
	newKeys := make([]key, 0, len(findings))
	stale := make([]key, 0, len(allow))
	tolerated := 0
	byLinter := map[string]int{}
	for _, k := range sortedKeys(findings) {
		if allow[k] {
			tolerated += len(findings[k])
			byLinter[k.linter] += len(findings[k])
			continue
		}
		newKeys = append(newKeys, k)
	}
	var b strings.Builder
	for _, k := range newKeys {
		b.WriteString(fmt.Sprintf("\n=== NOT ALLOWED  %s  [%s]\n", k.digest, k.linter))
		for _, f := range findings[k] {
			b.WriteString("  " + f.message + "\n")
		}
	}
	for k := range allow {
		if _, ok := findings[k]; !ok {
			stale = append(stale, k)
		}
	}
	sortKeys(stale)
	if len(stale) > 0 {
		b.WriteString(fmt.Sprintf("\n%d allow-list entries are now clean — delete these lines:\n", len(stale)))
		for _, k := range stale {
			b.WriteString("  " + k.digest + "\t" + k.linter + "\n")
		}
	}
	b.WriteString(fmt.Sprintf("\nIGNORED — findings the allow list tolerates today\n  %d findings across %d identities\n  by linter:\n", tolerated, len(allow)-len(stale)))
	for _, l := range sortedCounts(byLinter) {
		b.WriteString(fmt.Sprintf("    %5d  %s\n", byLinter[l], l))
	}
	verdict := 1
	if len(newKeys) == 0 {
		b.WriteString("\nno new lint findings\n")
		verdict = 0
	} else {
		b.WriteString(fmt.Sprintf("\nFAIL: %d finding identities are not on the allow list\n", len(newKeys)))
	}
	if _, err := out.WriteString(b.String()); err != nil {
		return 2, err
	}
	return verdict, nil
}

func sortedKeys(m map[key][]finding) []key {
	out := make([]key, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sortKeys(out)
	return out
}

func sortKeys(out []key) {
	sort.Slice(out, func(i, j int) bool {
		if out[i].digest != out[j].digest {
			return out[i].digest < out[j].digest
		}
		return out[i].linter < out[j].linter
	})
}

func sortedCounts(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if m[out[i]] != m[out[j]] {
			return m[out[i]] > m[out[j]]
		}
		return out[i] < out[j]
	})
	return out
}
