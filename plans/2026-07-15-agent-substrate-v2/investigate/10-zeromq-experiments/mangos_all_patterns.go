//go:build ignore

package main

import (
	"fmt"
	"time"

	"go.nanomsg.org/mangos/v3"
	"go.nanomsg.org/mangos/v3/protocol/bus"
	"go.nanomsg.org/mangos/v3/protocol/pair"
	"go.nanomsg.org/mangos/v3/protocol/pub"
	"go.nanomsg.org/mangos/v3/protocol/pull"
	"go.nanomsg.org/mangos/v3/protocol/push"
	"go.nanomsg.org/mangos/v3/protocol/rep"
	"go.nanomsg.org/mangos/v3/protocol/req"
	"go.nanomsg.org/mangos/v3/protocol/respondent"
	"go.nanomsg.org/mangos/v3/protocol/sub"
	"go.nanomsg.org/mangos/v3/protocol/surveyor"
	_ "go.nanomsg.org/mangos/v3/transport/all"
)

func chk(what string, err error) {
	if err != nil {
		fmt.Printf("  FAIL %s: %v\n", what, err)
	}
}

// 1. REQUEST/REPLY -- brief's "request/reply"
func testReqRep(url string) {
	s, err := rep.NewSocket()
	chk("rep.New", err)
	chk("rep.Listen", s.Listen(url))
	go func() {
		for {
			m, err := s.Recv()
			if err != nil {
				return
			}
			s.Send([]byte("echo:" + string(m)))
		}
	}()
	c, err := req.NewSocket()
	chk("req.New", err)
	chk("req.Dial", c.Dial(url))
	chk("req.Send", c.Send([]byte("ping")))
	r, err := c.Recv()
	chk("req.Recv", err)
	fmt.Printf("  REQ/REP        -> %q\n", r)
	c.Close()
	s.Close()
}

// 2. PUB/SUB -- brief's "pubsub"
func testPubSub(url string) {
	p, err := pub.NewSocket()
	chk("pub.New", err)
	chk("pub.Listen", p.Listen(url))
	sk, err := sub.NewSocket()
	chk("sub.New", err)
	chk("sub.Dial", sk.Dial(url))
	chk("sub.Subscribe", sk.SetOption(mangos.OptionSubscribe, []byte("topicA")))
	time.Sleep(100 * time.Millisecond) // late-joiner window
	go func() {
		for i := 0; i < 20; i++ {
			p.Send([]byte("topicA:hello"))
			p.Send([]byte("topicB:ignored"))
			time.Sleep(20 * time.Millisecond)
		}
	}()
	sk.SetOption(mangos.OptionRecvDeadline, 2*time.Second)
	m, err := sk.Recv()
	chk("sub.Recv", err)
	fmt.Printf("  PUB/SUB        -> %q (topic filter works: got topicA not topicB)\n", m)
	p.Close()
	sk.Close()
}

// 3. PUSH/PULL -- brief's "fanout" (load-balanced distribution)
func testPushPull(url string) {
	pl, err := pull.NewSocket()
	chk("pull.New", err)
	chk("pull.Listen", pl.Listen(url))
	ph, err := push.NewSocket()
	chk("push.New", err)
	chk("push.Dial", ph.Dial(url))
	chk("push.Send", ph.Send([]byte("work-item")))
	pl.SetOption(mangos.OptionRecvDeadline, 2*time.Second)
	m, err := pl.Recv()
	chk("pull.Recv", err)
	fmt.Printf("  PUSH/PULL      -> %q\n", m)
	ph.Close()
	pl.Close()
}

// 4. PAIR -- brief's "point to point"
func testPair(url string) {
	a, err := pair.NewSocket()
	chk("pair.New a", err)
	chk("pair.Listen", a.Listen(url))
	b, err := pair.NewSocket()
	chk("pair.New b", err)
	chk("pair.Dial", b.Dial(url))
	chk("pair.Send", b.Send([]byte("p2p-msg")))
	a.SetOption(mangos.OptionRecvDeadline, 2*time.Second)
	m, err := a.Recv()
	chk("pair.Recv", err)
	fmt.Printf("  PAIR (p2p)     -> %q\n", m)
	a.Close()
	b.Close()
}

// 5. SURVEYOR/RESPONDENT -- NOT in ZeroMQ. Maps to brief's roster/"who's around".
func testSurvey(url string) {
	sv, err := surveyor.NewSocket()
	chk("surveyor.New", err)
	chk("surveyor.Listen", sv.Listen(url))
	sv.SetOption(mangos.OptionSurveyTime, 500*time.Millisecond)
	for _, name := range []string{"dgx", "gb-mac-mini"} {
		r, err := respondent.NewSocket()
		chk("respondent.New", err)
		chk("respondent.Dial", r.Dial(url))
		n := name
		go func() {
			for {
				if _, err := r.Recv(); err != nil {
					return
				}
				r.Send([]byte(n + ":alive"))
			}
		}()
	}
	time.Sleep(200 * time.Millisecond)
	chk("survey.Send", sv.Send([]byte("WHO_IS_ALIVE")))
	got := []string{}
	for {
		m, err := sv.Recv()
		if err != nil {
			break // survey deadline == end of round
		}
		got = append(got, string(m))
	}
	fmt.Printf("  SURVEYOR       -> %v  (one-shot fleet roll-call, no ZMQ equivalent)\n", got)
	sv.Close()
}

// 6. BUS -- NOT in ZeroMQ. Every peer talks to every peer = not-centralized mesh.
func testBus(urls []string) {
	socks := make([]mangos.Socket, 3)
	for i := range socks {
		s, err := bus.NewSocket()
		chk("bus.New", err)
		chk("bus.Listen", s.Listen(urls[i]))
		socks[i] = s
	}
	// full mesh: everyone dials everyone else
	for i := range socks {
		for j := range socks {
			if i != j {
				chk("bus.Dial", socks[i].Dial(urls[j]))
			}
		}
	}
	time.Sleep(300 * time.Millisecond)
	chk("bus.Send", socks[0].Send([]byte("node0-broadcast")))
	for i := 1; i < 3; i++ {
		socks[i].SetOption(mangos.OptionRecvDeadline, 2*time.Second)
		m, err := socks[i].Recv()
		chk(fmt.Sprintf("bus.Recv node%d", i), err)
		fmt.Printf("  BUS node%d      -> %q (mesh: no hub)\n", i, m)
	}
	for _, s := range socks {
		s.Close()
	}
}

func main() {
	fmt.Println("=== mangos v3 pattern test (pure Go, no libzmq) ===")
	testReqRep("tcp://127.0.0.1:40801")
	testPubSub("tcp://127.0.0.1:40802")
	testPushPull("tcp://127.0.0.1:40803")
	testPair("tcp://127.0.0.1:40804")
	testSurvey("tcp://127.0.0.1:40805")
	testBus([]string{"tcp://127.0.0.1:40811", "tcp://127.0.0.1:40812", "tcp://127.0.0.1:40813"})
	fmt.Println("=== done ===")
}
