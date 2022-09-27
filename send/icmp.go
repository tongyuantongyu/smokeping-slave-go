package send

import (
	"log"
	"math/rand"
	"net"
	"smokeping-slave-go/master"
	"smokeping-slave-go/send/icmp"
	"sync/atomic"
	"time"
)

var Scramble bool
var id uint32

func ping(c *master.ProbeConfig, t *master.Target, v6 bool) (r []time.Duration) {
	var addr *net.IPAddr
	var err error
	if v6 {
		addr, err = net.ResolveIPAddr("ip6", t.Host)
	} else {
		addr, err = net.ResolveIPAddr("ip4", t.Host)
	}

	if err != nil {
		log.Printf("Bad ICMP host %s: %s\n", t.Host, err)
		return nil
	}

	m := icmp.GetICMPManager()

	var ticker *time.Ticker
	if c.MinInterval > 0 {
		ticker = time.NewTicker(c.MinInterval / 2)
	}

	r = make([]time.Duration, 0, c.Pings)
	for i := uint64(0); i < c.Pings; i++ {
		payload := icmp.ICMPPayload{ID: -1, Seq: -1}
		if c.PacketSize > 8 {
			payload.Data = make([]byte, c.PacketSize-8)
			rand.Read(payload.Data)
		}

		if !Scramble {
			payload.ID = int(atomic.AddUint32(&id, 1) & 0xffff)
			payload.Seq = int(i)
		}
		result := <-m.Issue(addr, 100, c.Timeout, payload)
		if result.Code == 257 {
			r = append(r, result.Latency)
		} else {
			r = append(r, time.Duration(-1))
		}

		if ticker != nil {
			<-ticker.C
			<-ticker.C
		}
	}

	if ticker != nil {
		ticker.Stop()
	}

	return
}

func Ping(c *master.ProbeConfig, t *master.Target) []time.Duration {
	return ping(c, t, false)
}

func Ping6(c *master.ProbeConfig, t *master.Target) []time.Duration {
	return ping(c, t, true)
}
