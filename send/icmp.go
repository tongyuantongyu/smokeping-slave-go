package send

import (
	"log"
	"net"
	"smokeping-slave-go/master"
	"smokeping-slave-go/send/icmp"
	"time"
)

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
		result := <-m.Issue(addr, 100, c.Timeout, int(c.PacketSize))
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
