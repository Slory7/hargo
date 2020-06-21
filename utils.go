package hargo

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/net/http/httpguts"

	log "github.com/sirupsen/logrus"
)

// Decode reads from a reader and returns Har object
func Decode(r *bufio.Reader) (Har, error) {
	dec := json.NewDecoder(r)
	var har Har
	err := dec.Decode(&har)

	if err != nil {
		log.Error(err)
	}

	// Delete ws:// entries as they block execution
	for i, entry := range har.Log.Entries {
		if strings.HasPrefix(entry.Request.URL, "ws://") {
			har.Log.Entries[i] = har.Log.Entries[len(har.Log.Entries)-1]
			har.Log.Entries = har.Log.Entries[:len(har.Log.Entries)-1]
		}
	}

	// Sort the entries by StartedDateTime to ensure they will be processed
	// in the same order as they happened
	sort.Slice(har.Log.Entries, func(i, j int) bool {
		return har.Log.Entries[i].StartedDateTime < har.Log.Entries[j].StartedDateTime
	})

	return har, err
}

// EntryToRequest converts a HAR entry type to an http.Request
func EntryToRequest(entry *Entry, hc HarConfig, ignoreHarCookies bool) (*http.Request, error) {
	body := ""

	if len(entry.Request.PostData.Params) == 0 {
		body = hc.ReplaceVariables(entry.Request.PostData.Text)
	} else {
		form := url.Values{}
		for _, p := range entry.Request.PostData.Params {
			form.Add(p.Name, hc.ReplaceVariables(p.Value))
		}
		body = form.Encode()
	}

	req, _ := http.NewRequest(entry.Request.Method, entry.Request.URL, bytes.NewBuffer([]byte(body)))

	for _, h := range entry.Request.Headers {
		if httpguts.ValidHeaderFieldName(h.Name) && httpguts.ValidHeaderFieldValue(h.Value) && h.Name != "Cookie" {
			req.Header.Add(h.Name, hc.ReplaceVariables(h.Value))
		}
	}

	if !ignoreHarCookies {
		for _, c := range entry.Request.Cookies {
			cookie := &http.Cookie{Name: c.Name, Value: hc.ReplaceVariables(c.Value), HttpOnly: false, Domain: c.Domain}
			req.AddCookie(cookie)
		}
	}

	return req, nil
}

func check(err error) {
	if err != nil {
		log.Error(err)
	}
}

// NewReader returns a bufio.Reader that will skip over initial UTF-8 byte order marks.
// https://tools.ietf.org/html/rfc7159#section-8.1
func NewReader(r io.Reader) *bufio.Reader {

	buf := bufio.NewReader(r)
	b, err := buf.Peek(3)
	if err != nil {
		// not enough bytes
		return buf
	}
	if b[0] == 0xef && b[1] == 0xbb && b[2] == 0xbf {
		log.Warn("BOM detected. Skipping first 3 bytes of file. Consider removing the BOM from this file. " +
			"See https://tools.ietf.org/html/rfc7159#section-8.1 for details.")
		buf.Discard(3)
	}
	return buf
}

//GetHarConfig gets the har config
func GetHarConfig(harFile string) HarConfig {
	hc := HarConfig{}
	harConfigFile := strings.TrimRight(harFile, ".har") + ".json"
	if _, err := os.Stat(harConfigFile); err == nil {
		log.Info("with har config file: ", harConfigFile)
		bytes, _ := ioutil.ReadFile(harConfigFile)
		json.Unmarshal(bytes, &hc)
	}
	return hc
}

// ReplaceVariables is to replace config variables
func (h *HarConfig) ReplaceVariables(s string) string {
	if len(h.Variables) <= 0 {
		re := regexp.MustCompile(`\{(\w+)\}`)
		if re.MatchString(s) {
			for _, g := range re.FindAllStringSubmatch(s, -1) {
				if v, ok := h.Variables[g[1]]; ok {
					s = strings.Replace(s, g[0], v, -1)
				}
			}
		}
	}
	return s
}

// ReplaceAlias is to replace config alias
func (h *HarConfig) ReplaceAlias(s string) string {
	if len(h.Alias) > 0 {
		if val, ok := h.Alias[s]; ok {
			return val
		}
		for k, v := range h.Alias {
			//this is a regexp
			if k[0] == '(' {
				if re, err := regexp.Compile(k); err == nil && re.MatchString(s) {
					s2 := re.ReplaceAllString(s, v)
					h.Alias[s] = s2
					return s2
				}
			}
		}
	}
	return s
}
