package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	flag "github.com/spf13/pflag"
	"io"
	"log"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"smokeping-slave-go/calc"
	"smokeping-slave-go/master"
	"smokeping-slave-go/send"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var node = flag.StringP("node", "n", "test", "Node name")
var key = flag.StringP("key", "k", "", "Node key")
var server = flag.StringP("server", "s", "http://localhost", "Server address")
var retry = flag.DurationP("retry", "r", 10*time.Second, "Retry interval when request failed")
var logTo = flag.StringSliceP("log", "l", []string{"-"}, "Log target")
var help = flag.BoolP("help", "h", false, "Print help")

const url = "/smokeping.fcgi"

var cli *http.Client
var sendResult = make(chan bool)
var working uint32

var fullVersion string
var buildDate string

func init() {
	if fullVersion != "" && buildDate != "" {
		fmt.Printf("Go Smokeping worker %s build at %s\n", fullVersion, buildDate)
	} else {
		log.Println("Go Smokeping worker non-release build. Not for production.")
	}

	flag.Parse()

	if *help {
		flag.PrintDefaults()
		os.Exit(0)
	}

	cli = &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   *retry / 2,
				KeepAlive: 10 * time.Second,
			}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          3,
			IdleConnTimeout:       time.Hour,
			TLSHandshakeTimeout:   *retry / 2,
			ExpectContinueTimeout: 1 * time.Second,
			DisableCompression:    true,
		},
	}

	var w []io.Writer
	for _, target := range *logTo {
		if target == "-" {
			w = append(w, os.Stderr)
		} else {
			f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_APPEND, os.ModePerm)
			if err != nil {
				log.Fatalf("Can't open log target %s: %s\n", target, err)
			}
			w = append(w, f)
		}
	}

	if len(w) == 0 {
		log.Fatalln("No log target specified.")
	}

	if len(w) == 1 {
		log.SetOutput(w[0])
	} else {
		log.SetOutput(io.MultiWriter(w...))
	}
}

var config = &master.Config{Last: 0}
var configLock sync.RWMutex

func getConfig() *master.Config {
	configLock.RLock()
	defer configLock.RUnlock()
	return config
}

func Send(data string) (err error) {
	defer func() {
		if err == nil {
			sendResult <- true
			return
		}

		log.Printf("Failed during communication: %s.\n", err)
		sendResult <- false
	}()

	hash := hmac.New(md5.New, []byte(*key))
	hash.Write([]byte(data))
	sign := hex.EncodeToString(hash.Sum(nil))
	buf := bytes.Buffer{}
	body := multipart.NewWriter(&buf)
	err = body.WriteField("slave", *node)
	if err != nil {
		return err
	}

	err = body.WriteField("key", sign)
	if err != nil {
		return err
	}

	err = body.WriteField("protocol", "2")
	if err != nil {
		return err
	}

	err = body.WriteField("config_time", strconv.FormatUint(getConfig().Last, 10))
	if err != nil {
		return err
	}

	err = body.WriteField("data", data)
	if err != nil {
		return err
	}

	err = body.Close()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), *retry/2)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", *server+url, &buf)
	if err != nil {
		return err
	}

	req.Header.Set("User-Agent", "smokeping-slave/1.0")
	req.Header.Set("Content-Type", body.FormDataContentType())

	resp, err := cli.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad http response: %s", resp.Status)
	}

	buf = bytes.Buffer{}
	_, err = io.Copy(&buf, resp.Body)
	if err != nil {
		return err
	}

	switch resp.Header.Get("Content-Type") {
	case "text/plain":
		if buf.String() != "OK\n" {
			return fmt.Errorf("bad response when sending result: %s", buf.String())
		}

		return nil

	case "application/smokeping-config":

	default:
		return fmt.Errorf("bad Content-Type when receiving config: %s", resp.Header.Get("Content-Type"))
	}

	configLock.Lock()
	defer configLock.Unlock()

	hash = hmac.New(md5.New, []byte(*key))
	hash.Write(buf.Bytes())
	sign = hex.EncodeToString(hash.Sum(nil))
	if sign != resp.Header.Get("Key") {
		return errors.New("bad signature in server response")
	}

	newConfig, err := master.ParseConfig(buf.Bytes(), *node)
	if err != nil {
		return err
	}

	if config.Last < newConfig.Last {
		config = newConfig
		log.Printf("Loaded new config with stamp %d.\n", config.Last)
		log.Printf("%d ICMP targets, %d ICMPv6 targets, %d TCP targets, %d TCPv6 targets.\n",
			len(config.T.ICMP), len(config.T.ICMPv6), len(config.T.TCP), len(config.T.TCPv6))
	} else {
		log.Printf("Not loading new config with same or older stamp %d (current is %d).\n", newConfig.Last, config.Last)
	}

	return nil
}

func Once(c *master.Config) {
	type result struct {
		dt    []time.Duration
		count uint64
		id    string
	}

	agg := make(chan result)
	sendDone := sync.WaitGroup{}

	_sendDoneChan := make(chan struct{})

	handle := func(targets []master.Target, send send.Sender, pc *master.ProbeConfig) {
		ticker := time.NewTicker(c.Step / time.Duration(len(targets)))
		waiter := sync.WaitGroup{}
		waiter.Add(len(targets))
		for _, target := range targets {
			go func(t master.Target) {
				step := send(pc, &t)
				agg <- result{
					dt:    step,
					count: pc.Pings,
					id:    t.Identifier,
				}
				waiter.Done()
			}(target)

			<-ticker.C
		}
		ticker.Stop()

		waiter.Wait()
		sendDone.Done()
	}

	sendDone.Add(4)
	go handle(c.T.ICMP, send.Ping, &c.P.ICMP)
	go handle(c.T.ICMPv6, send.Ping6, &c.P.ICMPv6)
	go handle(c.T.TCP, send.TCPing, &c.P.TCP)
	go handle(c.T.TCPv6, send.TCPing6, &c.P.TCPv6)

	go func() {
		sendDone.Wait()
		close(_sendDoneChan)
	}()

	go func() {
		now := strconv.FormatInt(time.Now().Unix(), 10)
		log.Printf("Start a new round of probing at %s.\n", now)
		var b strings.Builder

	Collect:
		for {
			select {
			case r := <-agg:
				b.WriteString(r.id)
				b.WriteByte('\t')
				b.WriteString(now)
				b.WriteByte('\t')
				calc.Format(&b, r.dt, r.count)
				b.WriteByte('\n')
			case <-_sendDoneChan:
				break Collect
			}
		}

		if b.Len() == 0 {
			return
		}

		err := Send(b.String()[:b.Len()-1])
		defer func() {
			if err != nil {
				log.Printf("Send metric at %s to server failed: %s.\n", now, err)
			} else {
				log.Printf("Send metric at %s to server done.\n", now)
			}
		}()
		if err != nil {
			ticker := time.NewTicker(*retry)
			defer ticker.Stop()
			for i := 0; i < 3; i++ {
				<-ticker.C
				if err = Send(b.String()[:b.Len()-1]); err == nil {
					return
				}
			}
		}
	}()
}

func detect() {
	if atomic.LoadUint32(&working) == 1 {
		return
	}

	ticker := time.NewTicker(*retry)
	defer ticker.Stop()
	for ; true; <-ticker.C {
		if err := Send(""); err == nil {
			atomic.StoreUint32(&working, 1)
			return
		}
	}
}

func work() {
	c := getConfig()
	ticker := time.NewTicker(c.Step)
	stamp := c.Last
	defer ticker.Stop()

	for ; true; <-ticker.C {
		if atomic.LoadUint32(&working) == 0 {
			return
		}

		go Once(c)
		c = getConfig()
		if c.Last != stamp {
			ticker.Reset(c.Step)
			stamp = c.Last
		}
	}
}

func main() {
	go func() {
		count := 0
		for result := range sendResult {
			if result {
				count = 0
				atomic.StoreUint32(&working, 1)
			} else {
				count++
				if count > 5 {
					atomic.StoreUint32(&working, 0)
				}
			}
		}
	}()

	for {
		detect()
		work()
	}
}
