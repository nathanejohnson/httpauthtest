package main

import (
	"bufio"
	"flag"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

type message struct {
	user   string
	pass   string
	status bool
}

type messagesMeta struct {
	sync.Mutex
	stopchan  chan struct{}
	semaphore chan struct{}
	message   chan *message
	donechan  chan struct{}
	url       string
	client    *http.Client
	stopping  bool
	password  string
	user      string
	startTime time.Time
}

func main() {

	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	maxroutines := fs.Int("maxroutines", runtime.NumCPU()*20, "The maximum number of go routines to spawn")
	user := fs.String("user", "admin", "username to try")
	url := fs.String("url", "http://192.168.1.1:8080/", "URL")

	fs.Parse(os.Args[1:])

	mm := messagesMetaInit(*maxroutines, *url)
	in := bufio.NewScanner(os.Stdin)
	for in.Scan() {
		pass := in.Text()
		if strings.HasPrefix(pass, "#") {
			continue
		}
		if mm.isStopping() {
			fmt.Printf("password is: '%s'\n", mm.password)
			os.Exit(0)
		}
		mm.scheduleCheck(*user, pass)

	}
}

func messagesMetaInit(maxroutines int, url string) *messagesMeta {
	mm := &messagesMeta{
		stopchan:  make(chan struct{}),
		semaphore: make(chan struct{}, maxroutines),
		message:   make(chan *message),
		donechan:  make(chan struct{}),
		url:       url,
		client: &http.Client{
			Transport: &http.Transport{
				DisableKeepAlives: true,
			},
		},
		stopping: false,
	}
	go mm.gatekeeper()
	return mm
}

func (mm *messagesMeta) gatekeeper() {
	msgct := 0
	for {
		select {
		case m := <-mm.message:
			mm.user = m.user
			mm.password = m.pass
			mm.setStopping()
			return
		case <-mm.donechan:
			msgct++
			if msgct%20 == 0 {
				secs := time.Since(mm.startTime).Seconds()
				ps := float64(msgct) / secs
				fmt.Printf("Processed %d password attempts in %.02f seconds, %.02f req/sec\n", msgct, secs, ps)
			}
			<-mm.semaphore

		}
	}
}

func (mm *messagesMeta) scheduleCheck(user, pass string) {
	if mm.startTime.IsZero() {
		mm.startTime = time.Now()
	}
	mm.semaphore <- struct{}{}
	go mm.checkPass(user, pass)
}

func (mm *messagesMeta) checkPass(user, pass string) {
	defer func() {
		mm.donechan <- struct{}{}
	}()
	if mm.isStopping() {
		return
	}
	r, err := http.NewRequest("GET", mm.url, nil)
	if err != nil {
		fmt.Printf("Error return from http.NewRequest: %s\n", err.Error())
		mm.setStopping()
		return
	}
	r.SetBasicAuth(user, pass)
	resp, err := mm.client.Do(r)
	if err != nil {
		fmt.Printf("Error returned from client.Do(): %s, carrying on\n", err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 400 {
		m := &message{
			user: user,
			pass: pass,
		}
		mm.message <- m
	}

}

func (mm *messagesMeta) isStopping() bool {
	mm.Lock()
	defer mm.Unlock()
	return mm.stopping
}

func (mm *messagesMeta) setStopping() {
	mm.Lock()
	mm.stopping = true
	mm.Unlock()
}
