//go:build ignore

package main

import (
	"fmt"

	zmq "github.com/pebbe/zmq4"
)

func main() {
	maj, min, pat := zmq.Version()
	fmt.Printf("libzmq %d.%d.%d\n", maj, min, pat)
}
