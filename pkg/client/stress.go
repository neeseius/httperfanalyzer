package client

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptrace"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

var (
	client         *http.Client
	requestBody    string
	requestHeaders map[string]string
	Method         string
	URL            string
	delay          time.Duration
	stats          *Stats
)

type Stats struct {
	Lock              *sync.Mutex
	Cancel            context.CancelFunc
	StartTime         time.Time
	RequestsSent      int
	RequestsTaken     int
	RequestsSentLast  int
	RequestsToSend    int
	RequestsPerSecond int
	LatLow            int64
	LatHigh           int64
	LatTotal          int64
	RcCounts          map[int]int
	Complete          bool
}

func (s *Stats) GetStatsLine(carriage bool) string {
	et := time.Since(s.StartTime)
	s.Lock.Lock()

	latencyAvg := int64(0)
	reqSentThisSecond := s.RequestsSent - s.RequestsSentLast
	if reqSentThisSecond != 0 {
		latencyAvg = s.LatTotal / int64(reqSentThisSecond)
	}

	statsLine := fmt.Sprintf("[time: %d] [lat LoAvgHi: %d %d %d ms] [sent: %d %.2f%%] [tps: %d] [tps avg: %d] ",
		int(et.Seconds()), s.LatLow, latencyAvg, s.LatHigh, s.RequestsSent,
		float32(s.RequestsSent)/float32(s.RequestsToSend)*100,
		reqSentThisSecond, int(float64(s.RequestsSent)/et.Seconds()))

	for rc, rcCount := range s.RcCounts {
		rcPercent := float32(rcCount) / float32(s.RequestsSent) * 100
		statsLine += fmt.Sprintf("[%ds: %.2f%%] ", rc, rcPercent)
	}

	s.LatLow = 100000
	s.LatHigh = 0
	s.LatTotal = 0
	s.RequestsSentLast = s.RequestsSent
	s.Lock.Unlock()

	if carriage {
		statsLine += "\r"
	} else {
		statsLine += "\n"
	}

	return statsLine
}

func (s *Stats) PrintStats(ctx context.Context, wg *sync.WaitGroup) {
	wg.Add(1)
	defer wg.Done()

	s.StartTime = time.Now()

	for {
		select {
		case <-ctx.Done():
			fmt.Print(s.GetStatsLine(false))
			return

		case <-time.After(1 * time.Second):
			fmt.Print(s.GetStatsLine(true))
		}
	}
}

func (s *Stats) TakeRequest() bool {
	s.Lock.Lock()
	defer s.Lock.Unlock()

	if s.Complete {
		return false
	} else if s.RequestsTaken == s.RequestsToSend {
		s.Complete = true
		return false
	}

	s.RequestsTaken++
	return true
}

func (s *Stats) UpdateRcCount(rc int, latency int64) {
	s.Lock.Lock()
	defer s.Lock.Unlock()

	s.RequestsSent++
	if _, ok := s.RcCounts[rc]; !ok {
		s.RcCounts[rc] = 0
	}
	s.RcCounts[rc]++

	if latency < s.LatLow {
		s.LatLow = latency
	} else if latency > s.LatHigh {
		s.LatHigh = latency
	}

	s.LatTotal += latency

	if s.RequestsSent == s.RequestsToSend {
		s.Cancel()
	}
}

func getBody(param string) string {
	if param[0] != '@' {
		return param
	}

	data, err := ioutil.ReadFile(param[1:])
	if err != nil {
		panic(err.Error())
	}
	return string(data)
}

func getRequestHeaders(param string) map[string]string {
	headers := make(map[string]string)
	for _, paramValue := range strings.Split(param, ",") {
		kvPair := strings.Split(paramValue, "=")
		if len(kvPair) < 2 {
			fmt.Printf("Invalid header argument: '%s'\n", paramValue)
			os.Exit(1)
		}
		key := kvPair[0]
		value := kvPair[1]
		headers[key] = value
	}

	return headers
}

func requestWorker(n int, ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	for stats.TakeRequest() {
		reqBody := new(strings.Reader)
		if requestBody != "" {
			reqBody = strings.NewReader(requestBody)
		}

		request, err := http.NewRequest(Method, URL, reqBody)
		if err != nil {
			fmt.Printf("Worker %d error forming request: %s\n", n, err.Error())
			continue
		}

		for header, value := range requestHeaders {
			request.Header.Add(header, value)
		}

		var start time.Time
		var reqDuration time.Duration
		trace := &httptrace.ClientTrace{
			GotConn: func(_ httptrace.GotConnInfo) {
				start = time.Now()
			},
			GotFirstResponseByte: func() {
				reqDuration = time.Since(start)
			},
		}

		request = request.WithContext(httptrace.WithClientTrace(request.Context(), trace))
		resp, err := client.Do(request)
		if err != nil {
			fmt.Printf("Worker %d error making request: %s\n", n, err.Error())
			stats.UpdateRcCount(0, reqDuration.Milliseconds())
			continue
		}

		io.Copy(ioutil.Discard, resp.Body)
		resp.Body.Close()

		stats.UpdateRcCount(resp.StatusCode, reqDuration.Milliseconds())

		if delay != 0 {
			time.Sleep(delay)
		}
	}
}

func Stress(url, method, reqBody, headers string, count, maxConnections, timeout int, keepAlive bool, delayMS int) {
	if url == "" {
		fmt.Println("-url must be specified")
		os.Exit(1)
	}

	URL = url
	Method = method

	if len(reqBody) > 0 {
		requestBody = getBody(reqBody)
	}

	if len(headers) > 0 {
		requestHeaders = getRequestHeaders(headers)
	}

	delay = time.Millisecond * time.Duration(delayMS)

	transport := *http.DefaultTransport.(*http.Transport)
	transport.MaxIdleConns = maxConnections
	transport.MaxIdleConnsPerHost = maxConnections
	transport.MaxConnsPerHost = maxConnections
	transport.DisableKeepAlives = !keepAlive

	client = &http.Client{
		Transport: &transport,
		Timeout:   time.Duration(timeout) * time.Second}

	stats = &Stats{
		Lock:              &sync.Mutex{},
		RequestsSent:      0,
		RequestsPerSecond: 0,
		RequestsToSend:    count,
		RcCounts:          make(map[int]int),
		Complete:          false,
		LatLow:            100000,
	}

	sigs := make(chan os.Signal, 1)
	done := make(chan struct{}, 1)

	ctx, cancel := context.WithCancel(context.Background())
	stats.Cancel = cancel
	signal.Notify(sigs, syscall.SIGINT)
	signal.Notify(sigs, syscall.SIGTERM)

	go func(cancel context.CancelFunc) {
		<-sigs
		stats.Complete = true
		cancel()
	}(cancel)

	wg := &sync.WaitGroup{}
	fmt.Printf("Stressing %s with %d Connections\n",
		URL, maxConnections)
	go stats.PrintStats(ctx, wg)

	for i := 0; i < maxConnections; i++ {
		wg.Add(1)
		go requestWorker(i, ctx, wg)
	}

	wg.Wait()
	close(done)
	client.CloseIdleConnections()
}
