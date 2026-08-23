package main

import "bufio"

const largeScanBufferMax = 4 * 1024 * 1024

func setLargeScanBuffer(sc *bufio.Scanner) {
	sc.Buffer(make([]byte, 64*1024), largeScanBufferMax)
}
