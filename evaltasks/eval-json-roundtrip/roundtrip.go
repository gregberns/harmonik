// Package evaljsonroundtrip provides a Config with a Duration-as-string field
// for eval grading. This is the pre-committed reference; the model-under-test overwrites it.
package evaljsonroundtrip

import (
	"encoding/json"
	"time"
)

// Config holds configuration with a Timeout expressed as a Go duration string on the wire.
type Config struct {
	Timeout time.Duration
}

type wireConfig struct {
	Timeout string `json:"timeout"`
}

// MarshalJSON serialises Timeout as a Go duration string (e.g. "1m30s").
func (c Config) MarshalJSON() ([]byte, error) {
	return json.Marshal(wireConfig{Timeout: c.Timeout.String()})
}

// UnmarshalJSON deserialises Timeout from a Go duration string.
func (c *Config) UnmarshalJSON(b []byte) error {
	var w wireConfig
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	d, err := time.ParseDuration(w.Timeout)
	if err != nil {
		return err
	}
	c.Timeout = d
	return nil
}
