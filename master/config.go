package master

import (
	sjson "encoding/json"
	"errors"
	"fmt"
	"github.com/json-iterator/go"
	"strings"
	"time"
)

var json = jsoniter.Config{
	EscapeHTML:    true,
	CaseSensitive: true,
}.Froze()

var rewriter = strings.NewReplacer(
	"=>", ":",
	`"`, `\"`,
	"'", `"`,
	" undef", " null",
)

type tcpPingConfig struct {
	Timeout *float64 `json:"timeout,string"`
	Pings   *uint64  `json:"pings,string"`
}

type pingConfig struct {
	Timeout     *float64 `json:"timeout,string"`
	MinInterval *float64 `json:"MinInterval,string"`
	PacketSize  *uint64  `json:"packetsize,string"`
	Pings       *uint64  `json:"pings,string"`
}

type probeConfig struct {
	TCPPing  tcpPingConfig `json:"TCPPing"`
	TCPPing6 tcpPingConfig `json:"TCPPing6"`
	Ping     pingConfig    `json:"FPing"`
	Ping6    pingConfig    `json:"FPing6"`
}

type stringList []string

func (s *stringList) UnmarshalJSON(d []byte) error {
	var c string
	if err := json.Unmarshal(d, &c); err != nil {
		return err
	}

	*s = strings.Split(c, " ")
	return nil
}

func (s *stringList) Has(v string) bool {
	for _, ss := range *s {
		if ss == v {
			return true
		}
	}

	return false
}

type targetDescriptor struct {
	Probe  *string     `json:"probe"`
	Host   *string     `json:"host"`
	Port   *uint16     `json:"port,string"`
	Slaves *stringList `json:"slaves"`
}

type targetOrList struct {
	targetDescriptor
	SubTargets map[string]targetOrList
}

func (t *targetOrList) UnmarshalJSON(d []byte) error {
	if err := json.Unmarshal(d, &t.targetDescriptor); err != nil {
		return err
	}

	var raw map[string]sjson.RawMessage
	if err := json.Unmarshal(d, &raw); err != nil {
		return err
	}

	t.SubTargets = make(map[string]targetOrList)
	for name, message := range raw {
		if message == nil || message[0] != '{' {
			continue
		}

		var sub targetOrList
		if err := json.Unmarshal(message, &sub); err != nil {
			continue
		}

		t.SubTargets[name] = sub
	}

	return nil
}

type globalConfig struct {
	Pings *uint64 `json:"pings,string"`
	Step  *uint64 `json:"step,string"`
}

type rawConfig struct {
	Probes  probeConfig  `json:"Probes"`
	Targets targetOrList `json:"Targets"`
	Global  globalConfig `json:"Database"`
	Last    uint64       `json:"__last"`
}

func parseRawConfig(resp []byte) (c *rawConfig, err error) {
	msg := string(resp)
	msg = strings.TrimSuffix(strings.TrimPrefix(msg, "$VAR1 = "), ";\n")
	msg = rewriter.Replace(msg)

	err = json.UnmarshalFromString(msg, &c)
	return
}

type Target struct {
	Identifier string
	Host       string
	Port       uint16
}

type Targets struct {
	TCP    []Target
	TCPv6  []Target
	ICMP   []Target
	ICMPv6 []Target
}

func aggregateTargets(targets *targetOrList, name string) (t *Targets) {
	t = new(Targets)

	type descriptor struct {
		Probe  string
		Host   string
		Port   uint16
		Slaves stringList
	}

	updateDesc := func(curr *descriptor, update *targetDescriptor) {
		if update.Probe != nil {
			curr.Probe = *update.Probe
		}

		if update.Host != nil {
			curr.Host = *update.Host
		}

		if update.Port != nil {
			curr.Port = *update.Port
		}

		if update.Slaves != nil {
			curr.Slaves = *update.Slaves
		}
	}

	addTarget := func(id string, curr descriptor) {
		if curr.Host == "" || !curr.Slaves.Has(name) {
			return
		}

		switch curr.Probe {
		case "FPing":
			t.ICMP = append(t.ICMP, Target{
				Identifier: id,
				Host:       curr.Host,
			})

		case "FPing6":
			t.ICMPv6 = append(t.ICMPv6, Target{
				Identifier: id,
				Host:       curr.Host,
			})

		case "TCPPing":
			if curr.Port != 0 {
				t.TCP = append(t.TCP, Target{
					Identifier: id,
					Host:       curr.Host,
					Port:       curr.Port,
				})
			}

		case "TCPPing6":
			if curr.Port != 0 {
				t.TCPv6 = append(t.TCPv6, Target{
					Identifier: id,
					Host:       curr.Host,
					Port:       curr.Port,
				})
			}
		}
	}

	desc := descriptor{}

	var process func(tol *targetOrList, curr descriptor, id string)
	process = func(tol *targetOrList, curr descriptor, id string) {
		updateDesc(&curr, &tol.targetDescriptor)

		if len(tol.SubTargets) == 0 {
			addTarget(id, curr)
		} else {
			for group, stol := range tol.SubTargets {
				process(&stol, curr, id+"/"+group)
			}
		}
	}

	process(targets, desc, "")
	return
}

type ProbeConfig struct {
	Timeout     time.Duration
	MinInterval time.Duration
	PacketSize  uint64
	Pings       uint64
}

type Parameter struct {
	TCP    ProbeConfig
	TCPv6  ProbeConfig
	ICMP   ProbeConfig
	ICMPv6 ProbeConfig
}

type Config struct {
	P    Parameter
	T    Targets
	Last uint64
	Step time.Duration
}

func copyPingConfig(src *pingConfig, dst *ProbeConfig) error {
	if src.Pings != nil {
		dst.Pings = *src.Pings
		if dst.Pings == 0 {
			return errors.New("ping count is 0")
		}
	}

	if src.MinInterval != nil {
		dst.MinInterval = time.Duration(*src.MinInterval * float64(time.Second))
	}

	if src.PacketSize != nil {
		dst.PacketSize = *src.PacketSize
	}

	if src.Timeout != nil {
		dst.Timeout = time.Duration(*src.Timeout * float64(time.Second))
		if dst.Timeout < 100*time.Microsecond {
			return fmt.Errorf("unreasonable timeout: %dus", dst.Timeout)
		}
	}

	return nil
}

func copyTCPingConfig(src *tcpPingConfig, dst *ProbeConfig) error {
	if src.Pings != nil {
		dst.Pings = *src.Pings
		if dst.Pings == 0 {
			return errors.New("ping count is 0")
		}
	}

	if src.Timeout != nil {
		dst.Timeout = time.Duration(*src.Timeout * float64(time.Second))
		if dst.Timeout < 100*time.Microsecond {
			return fmt.Errorf("unreasonable timeout: %dus", dst.Timeout)
		}
	}

	return nil
}

func ParseConfig(resp []byte, name string) (c *Config, err error) {
	rc, err := parseRawConfig(resp)
	if err != nil {
		return
	}

	c = new(Config)
	c.Step = time.Minute

	if rc.Global.Pings != nil {
		c.P.ICMP.Pings = *rc.Global.Pings
		c.P.ICMPv6.Pings = *rc.Global.Pings
		c.P.TCP.Pings = *rc.Global.Pings
	}

	if err = copyPingConfig(&rc.Probes.Ping, &c.P.ICMP); err != nil {
		return
	}
	if err = copyPingConfig(&rc.Probes.Ping6, &c.P.ICMPv6); err != nil {
		return
	}
	if err = copyTCPingConfig(&rc.Probes.TCPPing, &c.P.TCP); err != nil {
		return
	}
	if err = copyTCPingConfig(&rc.Probes.TCPPing6, &c.P.TCPv6); err != nil {
		return
	}

	if rc.Global.Step != nil {
		c.Step = time.Second * time.Duration(*rc.Global.Step)
	}

	if rc.Last == 0 {
		return nil, errors.New("bad config timestamp")
	}
	c.Last = rc.Last

	c.T = *aggregateTargets(&rc.Targets, name)

	return
}
