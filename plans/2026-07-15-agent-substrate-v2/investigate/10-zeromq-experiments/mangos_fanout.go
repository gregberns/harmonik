//go:build ignore

package main

import (
	"fmt"
	"time"

	"go.nanomsg.org/mangos/v3"
	"go.nanomsg.org/mangos/v3/protocol/pull"
	"go.nanomsg.org/mangos/v3/protocol/push"
	_ "go.nanomsg.org/mangos/v3/transport/all"
)

func main() {
	ph, _ := push.NewSocket()
	ph.Listen("tcp://127.0.0.1:40920")
	counts := make([]int, 2)
	done := make(chan int, 200)
	for i := 0; i < 2; i++ {
		p, _ := pull.NewSocket()
		p.Dial("tcp://127.0.0.1:40920")
		idx := i
		go func() {
			p.SetOption(mangos.OptionRecvDeadline, 3*time.Second)
			for {
				if _, err := p.Recv(); err != nil {
					return
				}
				done <- idx
			}
		}()
	}
	time.Sleep(500 * time.Millisecond)
	const N = 20
	for i := 0; i < N; i++ {
		ph.Send([]byte(fmt.Sprintf("job-%d", i)))
	}
	to := time.After(3 * time.Second)
	got := 0
	for got < N {
		select {
		case idx := <-done:
			counts[idx]++
			got++
		case <-to:
			fmt.Printf("mangos PUSH: received %d/%d distribution=%v (TIMEOUT)\n", got, N, counts)
			return
		}
	}
	fmt.Printf("mangos PUSH: received %d/%d distribution=%v\n", got, N, counts)
}
