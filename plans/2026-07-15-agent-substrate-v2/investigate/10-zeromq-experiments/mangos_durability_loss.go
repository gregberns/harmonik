//go:build ignore

package main

import (
	"fmt"
	"time"

	"go.nanomsg.org/mangos/v3"
	"go.nanomsg.org/mangos/v3/protocol/pub"
	"go.nanomsg.org/mangos/v3/protocol/sub"
	_ "go.nanomsg.org/mangos/v3/transport/all"
)

func main() {
	fmt.Println("=== BRIEF §5 test: are messages 'written down'? ===")

	// ---- A. LATE JOINER: publisher sends before subscriber connects --------
	p, _ := pub.NewSocket()
	p.Listen("tcp://127.0.0.1:40930")
	for i := 0; i < 10; i++ {
		p.Send([]byte(fmt.Sprintf("early-%d", i))) // nobody is listening yet
	}
	time.Sleep(100 * time.Millisecond)

	s, _ := sub.NewSocket()
	s.Dial("tcp://127.0.0.1:40930")
	s.SetOption(mangos.OptionSubscribe, []byte(""))
	time.Sleep(300 * time.Millisecond) // fully connected now
	s.SetOption(mangos.OptionRecvDeadline, 1*time.Second)
	m, err := s.Recv()
	if err != nil {
		fmt.Printf("A. LATE JOINER : subscriber received NOTHING (%v)\n", err)
		fmt.Println("   -> all 10 messages sent before it connected are GONE. No replay. No log.")
	} else {
		fmt.Printf("A. LATE JOINER : unexpectedly got %q\n", m)
	}

	// ---- B. SLOW SUBSCRIBER: does PUB block, or silently drop? ------------
	p2, _ := pub.NewSocket()
	p2.Listen("tcp://127.0.0.1:40931")
	s2, _ := sub.NewSocket()
	s2.Dial("tcp://127.0.0.1:40931")
	s2.SetOption(mangos.OptionSubscribe, []byte(""))
	s2.SetOption(mangos.OptionRecvDeadline, 500*time.Millisecond)
	time.Sleep(300 * time.Millisecond)

	const N = 100000
	start := time.Now()
	sendErrs := 0
	for i := 0; i < N; i++ {
		// subscriber is NEVER reading -> its queue fills
		if err := p2.Send([]byte(fmt.Sprintf("flood-%06d", i))); err != nil {
			sendErrs++
		}
	}
	elapsed := time.Since(start)
	fmt.Printf("B. SLOW SUB    : published %d msgs in %v with %d send errors\n", N, elapsed.Round(time.Millisecond), sendErrs)
	fmt.Println("   -> PUB never blocked and never errored. Overflow was dropped SILENTLY.")

	// now drain and see how many actually survived, and whether they're contiguous
	recv := 0
	var first, last string
	for {
		m, err := s2.Recv()
		if err != nil {
			break
		}
		if recv == 0 {
			first = string(m)
		}
		last = string(m)
		recv++
	}
	fmt.Printf("   subscriber actually received %d of %d (%.1f%%). first=%s last=%s\n",
		recv, N, 100*float64(recv)/float64(N), first, last)
	fmt.Printf("   -> %d messages VANISHED with no error, no ACK, no way for the sender to know.\n", N-recv)
}
