package send

import (
	"context"
	"fmt"
	"log"
	"net"
	"smokeping-slave-go/master"
	"strings"
	"syscall"
	"time"
)

func tcping(c *master.ProbeConfig, t *master.Target, v6 bool) (r []time.Duration) {
	var ntwk string
	if v6 {
		ntwk = "tcp6"
	} else {
		ntwk = "tcp4"
	}

	var host string
	if strings.Contains(t.Host, ":") && t.Host[0] != '[' {
		ntwk = "tcp6"
		host = fmt.Sprintf("[%s]:%d", t.Host, t.Port)
		log.Printf("Auto corrected %s to use tcpping6.", host)
	} else {
		host = fmt.Sprintf("%s:%d", t.Host, t.Port)
	}

	var start time.Time
	var ctx context.Context
	var cancel context.CancelFunc
	var timer *time.Timer
	d := net.Dialer{
		Control: func(string, string, syscall.RawConn) error {
			start = time.Now()
			timer = time.AfterFunc(c.Timeout, cancel)
			return nil
		},
		FallbackDelay: -1,
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
			r = append(r, time.Since(start))
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
