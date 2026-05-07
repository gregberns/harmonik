//go:build crash

// Package crash is the placeholder package for harmonik's crash-recovery test
// suite. Tests in this package exercise daemon crash-and-restart invariants.
// This file exists so that `go test -tags=crash ./test/crash/` compiles and
// produces zero failures before any crash tests are authored.
package crash
