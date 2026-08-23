package daemon

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"reflect"
	"text/tabwriter"
)

type wiringState struct {
	// field is the bootState field name (e.g. "handlerPauseCtrl").
	field string
	// typ is the field's declared Go type.
	typ string
	// constructed reports whether the field holds a non-nil value.
	constructed bool
}

func (bs *bootState) wiringAudit() []wiringState {
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
		out = append(out, wiringState{
			field:       t.Field(i).Name,
			typ:         t.Field(i).Type.String(),
			constructed: !f.IsNil(),
		})
	}
	return out
}

func (bs *bootState) logCompositionRoot(ctx context.Context, w io.Writer) {
	if os.Getenv("HARMONIK_DEBUG_WIRING") != "1" {
		return
	}
	if w == nil {
		w = os.Stderr
	}

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
