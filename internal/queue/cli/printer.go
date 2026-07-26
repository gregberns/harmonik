package cli

import (
	"fmt"
	"io"
)

// printer wraps an io.Writer and retains the FIRST write error so a failed
// write is carried to a single decision point instead of being discarded at
// every call site. Once err is set, later writes are no-ops, so the caller
// checks once at the end rather than after every fmt.Fprintf.
//
// Why this exists: the queue CLI renders multi-line reports to stdout. Before
// this type every write was `_, _ = fmt.Fprintf(...)`, so a truncated report
// (EPIPE from a closed pager, ENOSPC, a broken redirect) was reported to the
// shell as exit 0 — the caller could not tell a complete report from half of
// one. Stdout renderers now turn a retained error into exitTransportError.
//
// For a printer wrapping errOut the retained error is deliberately terminal:
// every diagnostic write is already on a non-zero-exit path, and there is no
// second channel on which to report that stderr itself failed. The error is
// retained rather than discarded so it stops all subsequent writes and stays
// inspectable, but it cannot change an exit code that is already non-zero.
type printer struct {
	w   io.Writer
	err error
}

// newPrinter returns a printer writing to w.
func newPrinter(w io.Writer) *printer { return &printer{w: w} }

// printf writes a formatted string, retaining the first write error.
func (p *printer) printf(format string, args ...any) {
	if p.err != nil {
		return
	}
	_, p.err = fmt.Fprintf(p.w, format, args...)
}

// println writes the operands with spaces between them and a trailing newline,
// retaining the first write error.
func (p *printer) println(args ...any) {
	if p.err != nil {
		return
	}
	_, p.err = fmt.Fprintln(p.w, args...)
}

// failed reports whether any write through this printer failed.
func (p *printer) failed() bool { return p.err != nil }
