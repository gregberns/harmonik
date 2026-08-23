package main

import (
	"strings"
	"testing"
)

func TestCheckFileAcceptsCommentOnlyChanges(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		before     string
		after      string
		wantOK     bool
		wantReason string
	}{
		{
			name:   "a deleted comment line is accepted",
			before: "package p\n\nfunc f() {\n\t// gone\n\tg()\n}\n\nfunc g() {}\n",
			after:  "package p\n\nfunc f() {\n\tg()\n}\n\nfunc g() {}\n",
			wantOK: true,
		},
		{
			name:   "a deleted trailing comment is accepted",
			before: "package p\n\nvar x = 1 // gone\n",
			after:  "package p\n\nvar x = 1\n",
			wantOK: true,
		},
		{
			name:   "extra blank lines alone are accepted",
			before: "package p\n\nvar x = 1\n",
			after:  "package p\n\n\n\nvar x = 1\n\n",
			wantOK: true,
		},
		{
			name:       "a changed identifier is rejected",
			before:     "package p\n\nvar x = 1\n",
			after:      "package p\n\nvar y = 1\n",
			wantReason: "token stream changed",
		},
		{
			name:       "re-padded alignment is rejected",
			before:     "package p\n\ntype S struct {\n\tA int\n\t// c\n\tLongerName string\n}\n",
			after:      "package p\n\ntype S struct {\n\tA          int\n\tLongerName string\n}\n",
			wantReason: "code line",
		},
		{
			name:       "a removed statement is rejected",
			before:     "package p\n\nfunc f() {\n\tg()\n\tg()\n}\n\nfunc g() {}\n",
			after:      "package p\n\nfunc f() {\n\tg()\n}\n\nfunc g() {}\n",
			wantReason: "token stream changed",
		},
		{
			name:       "a file that no longer parses is rejected",
			before:     "package p\n\n/* c */\nvar x = 1\n",
			after:      "package p\n\n/* c\nvar x = 1\n",
			wantReason: "result does not scan",
		},
		{
			name:   "a comment inside a string literal is not a comment",
			before: "package p\n\nconst s = `// PLANTED`\n",
			after:  "package p\n\nconst s = `// PLANTED`\n",
			wantOK: true,
		},
		{
			name:       "editing a comment inside a string literal is rejected",
			before:     "package p\n\nconst s = `// PLANTED`\n",
			after:      "package p\n\nconst s = ``\n",
			wantReason: "token stream changed",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res := checkFile([]byte(tc.before), []byte(tc.after))
			if res.OK != tc.wantOK {
				t.Fatalf("OK = %v, want %v (reason %q)", res.OK, tc.wantOK, res.Reason)
			}
			if tc.wantReason != "" && !strings.Contains(res.Reason, tc.wantReason) {
				t.Fatalf("reason = %q, want it to contain %q", res.Reason, tc.wantReason)
			}
		})
	}
}

// TestCommentsInsideStringLiteralsAreNeverBlocks is the P0 mandate. A
// line-oriented tool corrupts the Go fixtures this repo embeds in backtick
// strings; an AST tool cannot see them at all.
func TestCommentsInsideStringLiteralsAreNeverBlocks(t *testing.T) {
	t.Parallel()
	const src = "package p\n\n" +
		"const fixture = `//go:build scenario\n\n" +
		"package daemon\n\n" +
		"// PLANTED marker the sensor counts\n`\n\n" +
		"// realComment is the only comment in this file.\nfunc f() {}\n"
	fb, err := classifyFile("x.go", []byte(src))
	if err != nil {
		t.Fatalf("classifyFile: %v", err)
	}
	if len(fb.Blocks) != 1 {
		for _, b := range fb.Blocks {
			t.Logf("block: %q", b.Text)
		}
		t.Fatalf("found %d comment blocks, want 1", len(fb.Blocks))
	}
	if !strings.Contains(fb.Blocks[0].Text, "realComment") {
		t.Fatalf("wrong block found: %q", fb.Blocks[0].Text)
	}
}

func TestCodeLinesDropsCommentOnlyLines(t *testing.T) {
	t.Parallel()
	got, err := codeLines([]byte("package p\n\n// a\n// b\nvar x = 1 // trailing\n"))
	if err != nil {
		t.Fatalf("codeLines: %v", err)
	}
	want := []codeLine{{text: "package p", col: -1}, {text: "var x = 1", col: 11}}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("line %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestTrailingCommentRepaddingIsACodeChange is the defect an adversarial pass
// found and neither static check could see: gofmt merges two tabwriter
// alignment runs when the comment between them goes, and re-pads a code line
// that no token moved on. Right-trimming the blanked line hid it, so the tool
// reported "0 files failed the static checks" on a tree it had re-padded.
func TestTrailingCommentRepaddingIsACodeChange(t *testing.T) {
	t.Parallel()
	const before = "package p\n\nfunc f() {\n" +
		"\tx := 1 // short trailing\n" +
		"\t// FLOATING comment\n" +
		"\tyyyyyyyyyyyyyyyyyyyyyyyyyyyy := 2 // long trailing\n" +
		"\t_, _ = x, yyyyyyyyyyyyyyyyyyyyyyyyyyyy\n}\n"
	const after = "package p\n\nfunc f() {\n" +
		"\tx := 1                            // short trailing\n" +
		"\tyyyyyyyyyyyyyyyyyyyyyyyyyyyy := 2 // long trailing\n" +
		"\t_, _ = x, yyyyyyyyyyyyyyyyyyyyyyyyyyyy\n}\n"
	res := checkFile([]byte(before), []byte(after))
	if res.OK {
		t.Fatal("checkFile accepted a re-padded code line")
	}
	if !strings.Contains(res.Reason, "re-padded") {
		t.Fatalf("reason = %q, want it to name the re-padding", res.Reason)
	}
}
