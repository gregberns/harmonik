package main

// subscriberefusal.go — the one definition of "the daemon refused this
// subscribe request", shared by every subscribe-op client in this package.
//
// The daemon answers a subscribe op it cannot serve with a single SocketResponse
// object and then closes the connection (internal/daemon/socket.go
// handleSubscribe):
//
//	{"ok":false,"error":"daemon: SubscribeHandler not registered"}
//
// No trailing newline, and no "type" field. That last part is what makes this
// dangerous: a client decoding stream lines as EVENTS finds no type it
// recognises, skips the line, reads EOF, and reports a clean, empty finish. The
// refusal is indistinguishable from "the daemon had nothing to send" unless the
// client looks for it on purpose.
//
// A refusal is permanent for this connection, not a transient drop — the daemon
// is up and has declined this subscription. A client must report it, never
// retry it in a tight reconnect loop, and never let it read as success.
//
// Bead ref: hk-1dwk2.

import "encoding/json"

// subscribeRefused reports whether a decoded subscribe-stream line is the
// daemon's refusal. ok is the line's "ok" field: nil when absent, which is the
// case for every event, and false only in a refusal envelope. A pointer is
// load-bearing here — a plain bool cannot tell "absent" from "false", so every
// event would read as a refusal.
//
// Use this from clients that decode the stream with a json.Decoder into a
// struct carrying the ok/error fields; use subscribeRefusalReason from clients
// that hold the raw line bytes.
func subscribeRefused(ok *bool) bool { return ok != nil && !*ok }

// subscribeRefusalReason reports whether one raw line from a subscribe stream is
// the daemon's refusal rather than an event, and if so returns the daemon's
// stated reason. A line that is not valid JSON is not a refusal — malformed
// input is the caller's own business.
func subscribeRefusalReason(line []byte) (reason string, refused bool) {
	var env struct {
		Ok    *bool  `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(line, &env); err != nil {
		return "", false
	}
	if !subscribeRefused(env.Ok) {
		return "", false
	}
	if env.Error == "" {
		return "daemon refused the subscription without a reason", true
	}
	return env.Error, true
}
