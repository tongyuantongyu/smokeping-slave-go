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
	"math/rand"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"runtime"
	"smokeping-slave-go/bind"
	"smokeping-slave-go/calc"
	"smokeping-slave-go/master"
	"smokeping-slave-go/priority"
	"smokeping-slave-go/send"
	"smokeping-slave-go/send/icmp"
	"strconv"
	"sync"
	"time"
)

var node = flag.StringP("node", "n", "test", "Node name")
var key = flag.StringP("key", "k", "", "Node key")
var server = flag.StringP("server", "s", "http://localhost", "Server address")
var timeout = flag.DurationP("timeout", "t", 30*time.Second, "Timeout when communicating with server")
var logTo = flag.StringSliceP("log", "l", []string{"-"}, "Log target")
var buffer = flag.IntP("buffer", "b", 1440, "Metric buffer size count")
var help = flag.BoolP("help", "h", false, "Print help")
var debug = flag.Bool("debug", false, "Enable debug message")
var scramble = flag.Bool("scramble", false, "Scramble ICMP id and seq")
var iface = flag.StringSlice("interface", nil, "Interface or ip(s) bind to")
var version = flag.Bool("version", false, "Print version")

const url = "/smokeping.fcgi"

var cli *http.Client

var fullVersion string
var buildDate string

func init() {
	if *version {
		fmt.Println(fullVersion)
		os.Exit(0)
	}

	if fullVersion != "" && buildDate != "" {
		log.Printf("Go Smokeping worker %s build at %s\n", fullVersion, buildDate)
	} else {
		log.Println("Go Smokeping worker non-release build. Not for production.")
	}

	flag.Parse()

	if *help {
		flag.PrintDefaults()
		os.Exit(0)
	}

	if *debug {
		icmp.Debug = true
		send.TCPDebug = true
	}

	if *scramble {
		send.Scramble = true
	}

	if err := bind.Parse(*iface); err != nil {
		log.Fatalf("Can not parse interface: %s.", err)
	}

	cli = &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   *timeout,
				KeepAlive: 10 * time.Second,
			}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          3,
			IdleConnTimeout:       time.Hour,
			TLSHandshakeTimeout:   *timeout,
			ExpectContinueTimeout: 1 * time.Second,
			DisableCompression:    true,
		},
	}

	var w []io.Writer
	for _, target := range *logTo {
		if target == "-" {
			w = append(w, os.Stdout)
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

var bodyBuffer bytes.Buffer

func sendOnce(data []byte) (err error) {
	bodyBuffer.Reset()

	// if len(data) != 0 {
	//	time.Sleep(10 * time.Second)
	//	if rand.Intn(25) != 0 {
	//		return errors.New("rand")
	//	}
	//
	//	return
	// }

	defer func() {
		if err != nil {
			log.Printf("Failed during communication: %s.\n", err)
		}
	}()

	if *debug && len(data) > 0 {
		log.Printf("Sending data:\n%s\n---------------------------------\n", string(data))
	}

	hash := hmac.New(md5.New, []byte(*key))
	hash.Write(data)
	sign := hex.EncodeToString(hash.Sum(nil))
	body := multipart.NewWriter(&bodyBuffer)
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

	p, err := body.CreateFormField("data")
	if err != nil {
		return err
	}
	_, err = p.Write(data)
	if err != nil {
		return err
	}

	err = body.Close()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", *server+url, &bodyBuffer)
	if err != nil {
		return err
	}

	req.Header.Set("User-Agent", "smokeping-slave/1.0")
	req.Header.Set("Content-Type", body.FormDataContentType())

	resp, err := cli.Do(req)
	if err != nil {
		return err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad http response: %s", resp.Status)
	}

	bodyBuffer.Reset()
	_, err = io.Copy(&bodyBuffer, resp.Body)
	if err != nil {
		return err
	}

	switch resp.Header.Get("Content-Type") {
	case "text/plain":
		if bodyBuffer.String() != "OK\n" {
			return fmt.Errorf("bad response when sending result: %s", bodyBuffer.String())
		}

		return nil

	case "application/smokeping-config":

	default:
		return fmt.Errorf("bad Content-Type when receiving config: %s", resp.Header.Get("Content-Type"))
	}

	configLock.Lock()
	defer configLock.Unlock()

	hash = hmac.New(md5.New, []byte(*key))
	hash.Write(bodyBuffer.Bytes())
	sign = hex.EncodeToString(hash.Sum(nil))
	if sign != resp.Header.Get("Key") {
		return errors.New("bad signature in server response")
	}

	newConfig, err := master.ParseConfig(bodyBuffer.Bytes(), *node)
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

type result struct {
	dt    []time.Duration
	count uint64
	id    string
}

type sendData struct {
	at   string
	data []result
}

type pendingData struct {
	at   string
	data []byte
}

func formatResult(r *sendData, b *bytes.Buffer) []byte {
	b.Reset()
	for _, entry := range r.data {
		b.WriteString(entry.id)
		b.WriteByte('\t')
		b.WriteString(r.at)
		b.WriteByte('\t')
		calc.Format(b, entry.dt, entry.count)
		b.WriteByte('\n')
	}

	return b.Bytes()[:b.Len()-1]
}

var dataCache = sync.Pool{
	New: func() any {
		return []result(nil)
	},
}

func backoutSleep(scaler int) {
	time.Sleep(time.Duration(scaler+rand.Intn(scaler)) * time.Millisecond)
}

func sender(data chan *sendData) {
	var b bytes.Buffer
	for v := range data {
		payload := formatResult(v, &b)
		dataCache.Put(v.data[:0])
		err := sendOnce(payload)
		if err != nil {
			for {
				backoutSleep(5000)
				if err = sendOnce(payload); err == nil {
					break
				}
			}
		}
		log.Printf("Metric at %s sent to server.\n", v.at)
	}
}

func drainSend[T any](c chan T, v T) {
	if cap(c) == 0 {
		panic("Can't drain send on unbuffered channel")
	}
	for {
		select {
		case c <- v:
			return
		default:
			select {
			case <-c:
			default:
			}
		}
	}
}

func senderInf(data chan *sendData) {
	var b bytes.Buffer
	back := make(chan *pendingData)
	drain := make(chan *pendingData)
	state := make(chan bool, 1)
	go backoutBuffer(back, drain)
	go backoutSender(drain, back, state)

	for v := range data {
		payload := formatResult(v, &b)
		dataCache.Put(v.data[:0])
		var err error
		for i := 0; i < 3; i++ {
			if err = sendOnce(payload); err == nil {
				log.Printf("Metric at %s sent to server.\n", v.at)
				drainSend(state, true)
				break
			}
			backoutSleep(2500)
		}
		if err != nil {
			drainSend(state, false)
			log.Printf("Failed sending metric at %s to server: %s. Put into retry queue.\n", v.at, err)
			back <- &pendingData{at: v.at, data: payload}
		}
	}
}

func backoutBuffer(data, drain chan *pendingData) {
	var buffer []*pendingData
	var current *pendingData
	var stored bool
	for {
		if len(buffer) == 0 {
			current = <-data
			stored = false
		} else {
			current = buffer[len(buffer)-1]
			stored = true
		}

		select {
		case v := <-data:
			if !stored {
				buffer = append(buffer, current)
			}

			current = v
		case drain <- current:
			if stored {
				if len(buffer) == 1 {
					// GC the buffer, as all stuffs are gone
					buffer = nil
				} else {
					buffer = buffer[:len(buffer)-1]
				}
			}
		}
	}
}

func backoutSender(data, resched chan *pendingData, state chan bool) {
	current := false
	for {
		if !current {
			current = <-state
			continue
		}

		select {
		case current = <-state:
			continue
		case v := <-data:
			if err := sendOnce(v.data); err == nil {
				log.Printf("Metric at %s sent to server.\n", v.at)
				backoutSleep(2500)
			} else {
				log.Printf("Failed sending metric at %s to server: %s. Put into retry queue (rescheduled).\n", v.at, err)
				current = false
				resched <- v
			}
		}
	}
}

func Once(c *master.Config, deliver chan *sendData) {
	agg := make(chan result)
	sendDone := sync.WaitGroup{}

	handle := func(targets []master.Target, send send.Sender, pc *master.ProbeConfig) {
		ticker := time.NewTicker(c.Step / time.Duration(len(targets)))
		waiter := sync.WaitGroup{}
		waiter.Add(len(targets))
		for _, idx := range rand.Perm(len(targets)) {
			go func(t master.Target) {
				step := send(pc, &t)
				agg <- result{
					dt:    step,
					count: pc.Pings,
					id:    t.Identifier,
				}
				waiter.Done()
			}(targets[idx])

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
		close(agg)
	}()

	now := strconv.FormatInt(time.Now().Add(c.Step).Unix(), 10)
	log.Printf("Start probing of round %s.\n", now)

	data := dataCache.Get().([]result)
	if cap(data) < c.T.Count {
		data = make([]result, 0, c.T.Count)
	}

	for r := range agg {
		data = append(data, r)
	}

	log.Printf("Finished probing of round %s.\n", now)

	if len(data) == 0 {
		return
	}
	if *buffer <= 0 {
		deliver <- &sendData{
			data: data,
			at:   now,
		}
		return
	}

	for i := 0; i < 10; i++ {
		select {
		case deliver <- &sendData{
			data: data,
			at:   now,
		}:
			return

		default:
			select {
			case old := <-deliver:
				log.Printf("Buffer full. Dropping old data at %s in favor of new data\n", old.at)
			default:
			}
		}
	}

}

func bootstrap() {
	for {
		if err := sendOnce(nil); err == nil {
			return
		}
		backoutSleep(5000)
	}
}

func work(deliver chan *sendData) {
	c := getConfig()
	ticker := time.NewTicker(c.Step)
	stamp := c.Last
	defer ticker.Stop()

	for ; true; <-ticker.C {
		go Once(c, deliver)
		c = getConfig()
		if c.Last != stamp {
			ticker.Reset(c.Step)
			stamp = c.Last
		}
	}
}

func main() {
	if err := priority.Elevate(); err != nil {
		log.Printf("Failed to improve process priority: %s.", err)
	}

	bootstrap()
	runtime.GC()

	if *buffer > 0 {
		data := make(chan *sendData, *buffer)
		go sender(data)
		work(data)
	} else {
		data := make(chan *sendData)
		go senderInf(data)
		work(data)
	}

}
