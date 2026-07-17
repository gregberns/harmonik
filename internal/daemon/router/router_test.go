package socketrouter_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	socketrouter "github.com/gregberns/harmonik/internal/daemon/router"
)

// T1 — route-hit: each registered op invokes exactly its fn, and the Result it
// returns is passed back untouched.
func TestDispatch_RouteHit(t *testing.T) {
	r := socketrouter.New()
	want := socketrouter.Result{OK: true, Payload: json.RawMessage(`{"a":1}`)}
	var gotRaw json.RawMessage
	r.Register("alpha", func(_ context.Context, raw json.RawMessage) socketrouter.Result {
		gotRaw = raw
		return want
	})
	r.Register("beta", func(_ context.Context, _ json.RawMessage) socketrouter.Result {
		t.Fatal("beta must not be invoked for op alpha")
		return socketrouter.Result{}
	})

	got := r.Dispatch(context.Background(), "alpha", json.RawMessage(`{"op":"alpha"}`))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Dispatch returned %+v, want %+v", got, want)
	}
	if string(gotRaw) != `{"op":"alpha"}` {
		t.Fatalf("handler got raw %q, want %q", gotRaw, `{"op":"alpha"}`)
	}
}

// T2 — unknown op returns the neutral Result{Unknown:true}; NO daemon: wire
// string is built here (that exact-byte assertion lives daemon-side in T5).
func TestDispatch_UnknownOp(t *testing.T) {
	r := socketrouter.New()
	r.Register("alpha", func(_ context.Context, _ json.RawMessage) socketrouter.Result {
		return socketrouter.Result{OK: true}
	})
	got := r.Dispatch(context.Background(), "bogus", nil)
	if !reflect.DeepEqual(got, socketrouter.Result{Unknown: true}) {
		t.Fatalf("unknown op returned %+v, want Result{Unknown:true}", got)
	}
}

// T3 — Classify truth-table, incl. the len>2 edge cases (wire-F5).
func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want socketrouter.Kind
	}{
		{"type outcome_emitted", `{"type":"outcome_emitted"}`, socketrouter.KindHookRelay},
		{"type null (len 4)", `{"type":null}`, socketrouter.KindHookRelay},
		{"type x (len 3)", `{"type":"x"}`, socketrouter.KindHookRelay},
		{"type 123 (len 3)", `{"type":123}`, socketrouter.KindHookRelay},
		{"type empty (len 2)", `{"type":""}`, socketrouter.KindOp},
		{"op field", `{"op":"claim-next"}`, socketrouter.KindOp},
		{"empty object", `{}`, socketrouter.KindOp},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var raw map[string]json.RawMessage
			if err := json.Unmarshal([]byte(tc.raw), &raw); err != nil {
				t.Fatalf("unmarshal %q: %v", tc.raw, err)
			}
			if got := socketrouter.Classify(raw); got != tc.want {
				t.Fatalf("Classify(%s) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

// Ops() returns a sorted, complete list.
func TestOps_Sorted(t *testing.T) {
	r := socketrouter.New()
	for _, op := range []string{"gamma", "alpha", "beta"} {
		r.Register(op, func(_ context.Context, _ json.RawMessage) socketrouter.Result {
			return socketrouter.Result{}
		})
	}
	got := r.Ops()
	want := []string{"alpha", "beta", "gamma"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Ops() = %v, want %v", got, want)
	}
}

// Register panics on a duplicate op (init-time wiring bug).
func TestRegister_DuplicatePanics(t *testing.T) {
	r := socketrouter.New()
	r.Register("alpha", func(_ context.Context, _ json.RawMessage) socketrouter.Result {
		return socketrouter.Result{}
	})
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate Register")
		}
	}()
	r.Register("alpha", func(_ context.Context, _ json.RawMessage) socketrouter.Result {
		return socketrouter.Result{}
	})
}
