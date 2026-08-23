package main

import "encoding/json"

func subscribeRefused(ok *bool) bool { return ok != nil && !*ok }

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
