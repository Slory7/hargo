package hargo

import (
	"bufio"
	"compress/gzip"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"time"
)

// Run executes all entries in .har file
func Run(r *bufio.Reader, hc HarConfig, ignoreHarCookies bool, insecureSkipVerify bool, responseLines int) error {

	har, err := Decode(r)

	if err != nil {
		return err
	}

	check(err)

	jar, _ := cookiejar.New(nil)

	client := http.Client{
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			r.URL.Opaque = r.URL.Path
			return nil
		},
		Jar: jar,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: insecureSkipVerify},
		},
	}

	if len(har.Log.Entries) == 0 {
		return nil
	}

	first, _ := time.Parse("2006-01-02T15:04:05.000Z", har.Log.Entries[0].StartedDateTime)

	for _, entry := range har.Log.Entries {

		st, _ := time.Parse("2006-01-02T15:04:05.000Z", entry.StartedDateTime)
		diffst := st.Sub(first)
		if diffst > 0 {
			time.Sleep(diffst * time.Nanosecond)
		}
		first = st

		req, err := EntryToRequest(&entry, hc, ignoreHarCookies)

		if err != nil {
			return err
		}

		check(err)

		jar.SetCookies(req.URL, req.Cookies())

		resp, err := client.Do(req)

		check(err)

		fmt.Printf("[%s,%v] URL: %s\n", entry.Request.Method, resp.StatusCode, entry.Request.URL)

		if resp != nil {
			if responseLines > 0 || responseLines == -1 {
				fmt.Println("Reponse:")
				var reader io.ReadCloser
				switch resp.Header.Get("Content-Encoding") {
				case "gzip":
					reader, err = gzip.NewReader(resp.Body)
					defer reader.Close()
				default:
					reader = resp.Body
				}
				sc := bufio.NewScanner(reader)
				for i := 0; (responseLines == -1 || i < responseLines) && sc.Scan(); i++ {
					fmt.Println(sc.Text())
				}
			}
			resp.Body.Close()
		}

	}

	return nil
}
