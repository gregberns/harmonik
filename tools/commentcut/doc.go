// Command commentcut removes comment volume from this repository's Go sources
// without changing one byte of code.
//
// The repository is 27% comment lines. The tool cuts that number down with a
// deterministic program, not a model rewrite, and it proves the cut is safe
// before it keeps any file.
//
// # Verbs
//
//	commentcut list      classify every comment block and print JSON
//	commentcut apply     delete the cuttable blocks from the original file bytes
//	commentcut truncate  shorten an over-long comment block to its first N lines
//	commentcut verify    check a modified tree against a pristine baseline
//
// # The safety contract
//
// Three checks gate every write. A file that fails any of them is restored to
// its original bytes and reported on stderr.
//
//  1. The go/scanner token stream with comments excluded is identical before
//     and after.
//  2. No code line changed. Both files are rendered with every comment blanked
//     to spaces; the sequence of non-whitespace lines must match exactly, and
//     so must the column each surviving trailing comment starts at.
//  3. The tests of every affected package pass.
//
// Check 2 is the one that matters in practice. A standalone comment inside an
// aligned container (a composite literal, a struct field list, a parenthesised
// const/var/type block) is a tabwriter alignment-group separator. Delete it and
// the groups above and below merge, so gofumpt re-pads the key column, and the
// code bytes change with an identical token stream. See [hazardReasons] for the
// syntactic rule that refuses those deletions up front.
//
// The comment COLUMN is part of check 2 for the same reason, and it is the one
// case the syntactic rule cannot see: two statements in a function body carry
// trailing comments, a floating comment separates them, and deleting it merges
// their alignment run. See [codeLines].
//
// # The abort story
//
// apply and truncate refuse to start when any file in scope is untracked or
// already modified. The only copy of the originals a run holds is process
// memory, so the undo has to be git's, and it is only git's when git already
// holds an identical copy of every file the run may touch:
//
//	git restore --source=HEAD --worktree -- '*.go'
//
// -allow-dirty lifts the precondition and gives up that guarantee. A run that
// hits an I/O error restores every file it has already rewritten before it
// returns. An interrupted run does NOT: it leaves the files it had written in
// place, and git restore above is the undo. Every write goes through a sibling
// temp file and a rename, so a file is always either its old bytes or its new
// ones, never empty. A killed run can leave <path>.commentcut-<pid>.tmp behind;
// those are untracked, are not .go, never enter scope, and git clean removes
// them.
//
// # What is never cut
//
// Compiler and linter directives, the whole comment group that contains one,
// package doc comments, doc comments on exported symbols, struct field doc and
// trailing comments, and any group carrying a marker that a test reads. See
// [classifyFile] and [markerReasons].
//
// # Scope
//
// internal/, cmd/, tools/ and test/. Never ./... — evaltasks/ holds graded Go
// fixtures and testdata/ holds lint fixtures, and both are hard-excluded.
//
// A root is refused when it resolves outside the repository, lexically or
// through a symlink. "-root .." used to walk every sibling checkout on disk and
// rewrite files in all of them, because the exclusion set tested directory
// names and never asked whether the walk was still inside the tree.
package main
