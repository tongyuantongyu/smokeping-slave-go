package main

import (
	"fmt"
	flag "github.com/spf13/pflag"
	"net"
	"os"
	"smokeping-slave-go/priority"
	"smokeping-slave-go/send/icmp"
	"time"
)

var count = flag.Uint64P("number", "n", 4, "Ping count")
var size = flag.Uint64P("size", "s", 64, "Packet size")
var v4 = flag.BoolP("ipv4", "4", false, "Force IPv4")
var v6 = flag.BoolP("ipv6", "6", false, "Force IPv6")
var interval = flag.DurationP("interval", "i", time.Second, "Ping interval")
var timeout = flag.DurationP("timeout", "w", 3*time.Second, "Ping timeout")
var ttl = flag.Uint8P("ttl", "t", 100, "TTL")
var help = flag.BoolP("help", "h", false, "Print help")
var prio = flag.BoolP("priority", "p", false, "Use highest priority")

func main() {
	flag.Parse()

	if *help {
		flag.PrintDefaults()
		os.Exit(0)
	}

	var target string
	if flag.NArg() == 0 {
		fmt.Println("No target specified.")
		os.Exit(1)
	}

	if *prio {
		if err := priority.Elevate(); err != nil {
			fmt.Printf("Failed to improve process priority: %s", err)
		}
	}

	target = flag.Arg(0)

	var addr *net.IPAddr
	var err error

	switch {
	case *v6:
		addr, err = net.ResolveIPAddr("ip6", target)
	case *v4:
		addr, err = net.ResolveIPAddr("ip4", target)
	default:
		addr, err = net.ResolveIPAddr("ip", target)
	}

	if err != nil {
		fmt.Printf("Failed parsing target: %s", err)
	}

	m := icmp.GetICMPManager()
	time.Sleep(300 * time.Millisecond)

	for i := uint64(0); i < *count; i++ {
		result := <-m.Issue(addr, int(*ttl), *timeout, int(*size))
		latency := float64(result.Latency) / float64(time.Millisecond)
		switch result.Code {
		case 256:
			fmt.Println("Timeout")
		case 257:
			fmt.Printf("EchoReply from %s, in %9.4fms\n", result.AddrIP, latency)
		case 258:
			fmt.Printf("TimeExceed from %s, in %9.4fms\n", result.AddrIP, latency)
		default:
			fmt.Printf("DstUnreach(%3d) from %s, in %9.4fms\n", result.Code, result.AddrIP, latency)
		}

		time.Sleep(*interval)
	}
}
