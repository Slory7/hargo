package hargo

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"time"

	log "github.com/sirupsen/logrus"
)

// LoadTest executes all HTTP requests in order concurrently
// for a given number of workers.
func LoadTest(harfile string, file *os.File, hc HarConfig, isSequence bool, workers int, timeout time.Duration, u url.URL, ignoreHarCookies bool, insecureSkipVerify bool) error {
	log.Infof("Starting load test with %d workers. Duration %v.", workers, timeout)

	results := make(chan TestResult)
	defer close(results)
	stop := make(chan bool)
	entries := make(chan Entry, workers)

	go ReadStream(file, entries, stop, isSequence)

	// if a InfluxDB URL is given the metrics will be written to that instance
	// if not the dummy consumer is initiated.
	if (url.URL{}) != u {
		go WritePoint(u, results)
	} else {
		go func(results chan TestResult) {
			for {
				<-results
			}
		}(results)
	}

	go wait(stop, timeout, workers)

	if isSequence {
		for i := 0; i < workers; i++ {
			go processEntries(harfile, hc, i, entries, results, ignoreHarCookies, insecureSkipVerify, stop)
		}
		<-stop
	} else {
	loop:
		for {
			select {
			case entry := <-entries:
				for i := 0; i < workers; i++ {
					ch := make(chan Entry)
					go func(en Entry) {
					loop:
						for {
							select {
							default:
								ch <- en
							case <-stop:
								break loop
							}
						}
					}(entry)
					go processEntries(harfile, hc, i, ch, results, ignoreHarCookies, insecureSkipVerify, stop)
				}
			case <-stop:
				break loop
			}
		}
	}
	fmt.Printf("\nTimeout of %.1fs elapsed. Terminating load test.\n", timeout.Seconds())
	return nil
}

// wait will close the stop chan when the timeout is hit.
func wait(stop chan bool, timeout time.Duration, workers int) {
	time.Sleep(timeout)
	close(stop)
}

func processEntries(harfile string, hc HarConfig, worker int, entries chan Entry, results chan TestResult, ignoreHarCookies bool, insecureSkipVerify bool, stop chan bool) {
	jar, _ := cookiejar.New(nil)

	httpClient := http.Client{
		Transport: &http.Transport{
			Dial: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).Dial,
			TLSClientConfig:       &tls.Config{InsecureSkipVerify: insecureSkipVerify},
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			r.URL.Opaque = r.URL.Path
			return nil
		},
		Jar: jar,
	}
	iter := 0
loop:
	for {
		select {
		case <-stop:
			break loop
		case entry := <-entries:
			msg := fmt.Sprintf("[%d,%d] %s", worker, iter, entry.Request.URL)

			req, err := EntryToRequest(&entry, hc, ignoreHarCookies)

			check(err)

			jar.SetCookies(req.URL, req.Cookies())

			startTime := time.Now()
			resp, err := httpClient.Do(req)
			endTime := time.Now()
			latency := int(endTime.Sub(startTime) / time.Millisecond)
			method := req.Method

			if err != nil {

				log.Error(err)
				log.Error(entry)
				tr := TestResult{
					URL:       req.URL.String(),
					URLShort:  hc.ReplaceAlias(req.URL.RequestURI()),
					Status:    0,
					StartTime: startTime,
					EndTime:   endTime,
					Latency:   latency,
					Method:    method,
					HarFile:   harfile}
				results <- tr
				continue
			}

			if resp != nil {
				resp.Body.Close()
			}

			msg += fmt.Sprintf(" %d %dms", resp.StatusCode, latency)

			log.Infoln(msg)

			tr := TestResult{
				URL:       req.URL.String(),
				URLShort:  hc.ReplaceAlias(req.URL.RequestURI()),
				Status:    resp.StatusCode,
				StartTime: startTime,
				EndTime:   endTime,
				Latency:   latency,
				Method:    method,
				Size:      int64(entry.Request.BodySize + entry.Response.BodySize),
				HarFile:   harfile,
			}

			results <- tr
		}
		iter++
	}
}
