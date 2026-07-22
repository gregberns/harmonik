//go:build ignore

package main

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/go-zeromq/zmq4"
)

func main() {
	fmt.Println("=== go-zeromq/zmq4 v0.17.0 : verifying reported open issues ===")
	ctx := context.Background()

	// --- Issue #97 "Socket can only Listen to one endpoint" -----------------
	s := zmq4.NewRep(ctx)
	defer s.Close()
	e1 := s.Listen("tcp://127.0.0.1:40902")
	e2 := s.Listen("tcp://127.0.0.1:40903")
	fmt.Printf("[#97] Listen returned: first=%v second=%v\n", e1, e2)
	// Does the SECOND endpoint actually accept a TCP connection?
	for _, p := range []string{"40902", "40903"} {
		c, err := net.DialTimeout("tcp", "127.0.0.1:"+p, 500*time.Millisecond)
		if err != nil {
			fmt.Printf("[#97]   port %s: NOT accepting (%v)  <-- endpoint is dead\n", p, err)
		} else {
			fmt.Printf("[#97]   port %s: accepting TCP\n", p)
			c.Close()
		}
	}

	// --- Issue #158 "Push to TCP not round robining" ------------------------
	push := zmq4.NewPush(ctx)
	defer push.Close()
	if err := push.Listen("tcp://127.0.0.1:40910"); err != nil {
		fmt.Println("[#158] push.Listen err:", err)
		return
	}
	counts := make([]int, 2)
	done := make(chan int, 100)
	for i := 0; i < 2; i++ {
		p := zmq4.NewPull(ctx)
		defer p.Close()
		if err := p.Dial("tcp://127.0.0.1:40910"); err != nil {
			fmt.Println("[#158] pull.Dial err:", err)
			return
		}
		idx := i
		go func() {
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
		if err := push.Send(zmq4.NewMsgString(fmt.Sprintf("job-%d", i))); err != nil {
			fmt.Println("[#158] push.Send err:", err)
			break
		}
	}
	timeout := time.After(3 * time.Second)
	got := 0
loop:
	for got < N {
		select {
		case idx := <-done:
			counts[idx]++
			got++
		case <-timeout:
			break loop
		}
	}
	fmt.Printf("[#158] sent %d jobs to 2 PULL workers -> received %d, distribution=%v\n", N, got, counts)
	if counts[0] == 0 || counts[1] == 0 {
		fmt.Println("[#158]   *** NOT round-robining: one worker got everything (fanout broken) ***")
	} else {
		fmt.Println("[#158]   round-robin appears to work")
	}
}
