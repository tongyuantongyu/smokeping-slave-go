package send

import (
	"context"
	"fmt"
	"log"
	"net"
	"smokeping-slave-go/bind"
	"smokeping-slave-go/fasttime"
	"smokeping-slave-go/master"
	"strings"
	"syscall"
	"time"
)

var TCPDebug bool

func tcping(c *master.ProbeConfig, t *master.Target, v6 bool) (r []time.Duration) {
	var ntwk string
	var v6wrap bool

	if strings.Contains(t.Host, ":") {
		if !v6 {
			v6 = true
			log.Printf("Auto corrected %s to use tcpping6.\n", t.Host)
		}

		if !strings.HasPrefix(t.Host, "[") {
			v6wrap = true
		}
	}

	if v6 {
		ntwk = "tcp6"
	} else {
		ntwk = "tcp4"
	}

	var host string
	if v6wrap {
		host = fmt.Sprintf("[%s]:%d", t.Host, t.Port)
	} else {
		host = fmt.Sprintf("%s:%d", t.Host, t.Port)
	}

	var start fasttime.Time
	var ctx context.Context
	var cancel context.CancelFunc
	var timer *time.Timer
	d := net.Dialer{
		Control: func(string, string, syscall.RawConn) error {
			start = fasttime.Now()
			timer = time.AfterFunc(c.Timeout, cancel)
			return nil
		},
		FallbackDelay: -1,
	}

	if ntwk == "tcp6" {
		d.LocalAddr = bind.LAddr6().AsTCP()
	} else if ntwk == "tcp4" {
		d.LocalAddr = bind.LAddr4().AsTCP()
	}

	var ticker *time.Ticker
	if c.MinInterval > 0 {
		ticker = time.NewTicker(c.MinInterval)
		defer ticker.Stop()
	}

	r = make([]time.Duration, 0, c.Pings)
	for i := uint64(0); i < c.Pings; i++ {
		ctx, cancel = context.WithCancel(context.Background())
		conn, err := d.DialContext(ctx, ntwk, host)
		if err != nil {
			r = append(r, time.Duration(-1))
			if !strings.Contains(err.Error(), "operation was canceled") {
				if tErr, ok := err.(interface{ Timeout() bool }); !ok || !tErr.Timeout() {
					log.Printf("Non-timeout TCPPing error for host %s: %s\n", host, err)
					cancel()
					r = nil
					return
				}
			}
		} else {
			now := fasttime.Now()
			r = append(r, now.Since(start))
			if TCPDebug {
				log.Printf("[DEBUG] In %9.4fms, TSYN->%39s@%d, <-@%d\n", float64(now.Since(start))/float64(time.Millisecond),
					host, start, now)
			}
		}
		if timer != nil {
			timer.Stop()
		}
		if conn != nil {
			_ = conn.Close()
		}

		cancel()
		if ticker != nil {
			<-ticker.C
		}
	}

	return
}

func TCPing(c *master.ProbeConfig, t *master.Target) []time.Duration {
	return tcping(c, t, false)
}

func TCPing6(c *master.ProbeConfig, t *master.Target) []time.Duration {
	return tcping(c, t, true)
}
