package daemon

// wiringlog.go — daemon-startup composition-root wiring log (hk-4mupj).
//
// Operators enable the log by setting HARMONIK_DEBUG_WIRING=1 in the daemon's
// environment before launch.  Output goes to cfg.LogWriter, or os.Stderr when
// none is attached, so it appears alongside other daemon diagnostic output.
//
// The audit is DERIVED from the live *bootState at the moment it is emitted —
// it reports which composition-root SINGLETONS are actually constructed and
// which are nil.  It is deliberately not a hand-maintained table: the previous
// version was a 37-entry []wiringEntry constant that printed the same text no
// matter what had been wired, so it could not detect the silent drop it existed
// to catch, and every one of its `daemon.go:NNN` call sites had rotted (the
// wiring moved to bootstate.go / bootsocket.go / bootworkloop.go during the
// partition work).  Deriving from bootState means a dropped singleton shows up
// as ABSENT without anyone remembering to update this file.
//
// Scope, stated plainly so the log is not read as more than it is: this covers
// the singletons bootState holds.  A dropped bus.Subscribe, a dropped
// StartWatcher call, and a dropped workLoopDeps field all remain invisible
// here — those are wiring ACTIONS rather than constructed values, and nothing
// in this file detects them.
//
// Output is deterministic — bootState field order — so operators can diff two
// daemon versions to catch silent drops.
//
// Bead ref: hk-4mupj.

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"reflect"
	"text/tabwriter"
)

// wiringState is one derived composition-root observation: a bootState field,
// its declared type, and whether it holds a constructed value at audit time.
type wiringState struct {
	// field is the bootState field name (e.g. "handlerPauseCtrl").
	field string
	// typ is the field's declared Go type.
	typ string
	// constructed reports whether the field holds a non-nil value.
	constructed bool
}

// wiringAudit reads the live bootState and reports one entry per wireable
// singleton — every field whose type can be nil (pointer, interface, map,
// slice, func, chan).  Plain scalars (cfg, hooks, clockRegressionDetected) are
// configuration rather than wiring and are skipped.
//
// Reflection is what makes this a real drop detector: adding or removing a
// bootState singleton changes the audit with no edit here.
//
// Bead ref: hk-4mupj.
func (bs *bootState) wiringAudit() []wiringState {
	// Elem() rather than ValueOf(*bs): bootState holds Config by value, and
	// copying it just to read field names would be a needless struct copy.
	v := reflect.ValueOf(bs).Elem()
	t := v.Type()
	out := make([]wiringState, 0, t.NumField())
	for i := range t.NumField() {
		f := v.Field(i)
		switch f.Kind() {
		case reflect.Pointer, reflect.Interface, reflect.Map,
			reflect.Slice, reflect.Func, reflect.Chan:
		default:
			continue
		}
		// IsNil reads an unexported field, which reflect permits: only
		// Interface and Set are gated on exportedness.
		out = append(out, wiringState{
			field:       t.Field(i).Name,
			typ:         t.Field(i).Type.String(),
			constructed: !f.IsNil(),
		})
	}
	return out
}

// logCompositionRoot writes one line per composition-root singleton to w when
// HARMONIK_DEBUG_WIRING=1 is set in the process environment.  When w is nil it
// falls back to os.Stderr.
//
// Format: tab-separated columns — Singleton | Type | State.
//
// Bead ref: hk-4mupj.
func (bs *bootState) logCompositionRoot(ctx context.Context, w io.Writer) {
	if os.Getenv("HARMONIK_DEBUG_WIRING") != "1" {
		return
	}
	if w == nil {
		w = os.Stderr
	}

	// The tabwriter buffers every row until Flush, so a failure anywhere means
	// the operator asked for the audit and saw nothing at all. There is nothing
	// to recover at boot — one diagnostic line is the whole remedy — so the
	// rows share a single error rather than each being checked in place.
	var werr error
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	row := func(format string, args ...any) {
		if werr == nil {
			_, werr = fmt.Fprintf(tw, format, args...)
		}
	}

	entries := bs.wiringAudit()
	row("daemon: composition-root wiring audit (%d singletons)\n", len(entries))
	row("  #\tSingleton\tType\tState\n")
	row("  -\t---------\t----\t-----\n")
	for i, e := range entries {
		state := "ABSENT (nil)"
		if e.constructed {
			state = "constructed"
		}
		row("  %d\t%s\t%s\t%s\n", i+1, e.field, e.typ, state)
	}
	if werr == nil {
		werr = tw.Flush()
	}
	if werr != nil {
		slog.WarnContext(ctx, "composition_root_wiring_audit_not_written", "err", werr)
	}
}
