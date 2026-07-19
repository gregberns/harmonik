package main

// scanbuf.go — shared bufio.Scanner buffer sizing for event/JSONL streams.
//
// bufio.Scanner's default max token size is 64KB. Daemon event lines
// (reviewer_verdict notes, long operator directives, large bead descriptions)
// can legitimately exceed that, and a too-small buffer makes the scanner stop
// with bufio.ErrTooLong — silently truncating the stream. Every scanner that
// reads an NDJSON/JSONL stream must go through setLargeScanBuffer.

import "bufio"

// largeScanBufferMax is the maximum token (line) size accepted by scanners
// over event/JSONL streams: 4MB.
const largeScanBufferMax = 4 * 1024 * 1024

// setLargeScanBuffer raises sc's maximum token size to largeScanBufferMax so
// large-but-valid event lines do not abort the scan with bufio.ErrTooLong.
func setLargeScanBuffer(sc *bufio.Scanner) {
	sc.Buffer(make([]byte, 64*1024), largeScanBufferMax)
}
