package hookrelay

// ExportedIsRetryableDialErr exposes isRetryableDialErr for cross-platform unit
// testing of the CHB-017 non-socket disambiguation (hk-rupvi). The real dial
// returns a platform-dependent errno for "regular file dialed as unix socket"
// (ENOTSOCK on darwin, ECONNREFUSED on linux), so the stat-based fatal decision
// must be provable by feeding a SYNTHETIC ECONNREFUSED regardless of host OS.
var ExportedIsRetryableDialErr = isRetryableDialErr
