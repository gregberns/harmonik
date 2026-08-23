package pi

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"time"
)

// ProbeBaseURL dials the host:port encoded in baseURL and returns a
// descriptive error if nothing answers within timeout. baseURL is the raw
// harnesses.pi.base_url config value (e.g. "http://127.0.0.1:8551/v1"); an
// empty baseURL is the caller's responsibility to skip (today's cloud-provider
// behavior — no base_url configured — has nothing to probe). ctx is the
// caller's context; ProbeBaseURL derives its own timeout from it and does not
// retain ctx beyond this call.
func ProbeBaseURL(ctx context.Context, baseURL string, timeout time.Duration) error {
	u, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("pi harness: harnesses.pi.base_url %q is not a valid URL: %w", baseURL, err)
	}
	host := u.Host
	if host == "" {
		return fmt.Errorf("pi harness: harnesses.pi.base_url %q has no host:port to probe", baseURL)
	}
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(dialCtx, "tcp", host)
	if err != nil {
		return fmt.Errorf(
			"pi harness: base_url %s is unreachable — nothing is listening on %s (%v); "+
				"every pi dispatch will fail until the tunnel to the model host is back up",
			baseURL, host, err)
	}
	return conn.Close()
}
