package hargo

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	client "github.com/influxdata/influxdb1-client/v2"
	log "github.com/sirupsen/logrus"
)

var db string

// newInfluxDBClient returns a new InfluxDB client
func newInfluxDBClient(u url.URL) (client.Client, error) {

	addr := fmt.Sprintf("%s://%s:%s", u.Scheme, u.Hostname(), u.Port())
	log.Print("Connecting to InfluxDB: ", addr)

	c, err := client.NewHTTPClient(client.HTTPConfig{
		Addr: addr,
	})

	if err != nil {
		log.Fatal("Error: ", err)
		return c, err
	}

	retry := 1

	for retry < 3 {
		_, resp, e := c.Ping(2 * time.Second)
		if e != nil {
			retry++
			time.Sleep(10 * time.Second)
		} else if len(resp) > 0 {
			log.Println("Version InfluxDB: " + resp)
			break
		}
	}

	db = strings.Replace(u.Path, "/", "", -1)

	log.Info("DB: ", db)

	cmd := fmt.Sprintf("CREATE DATABASE %s", db)

	log.Debug("Query: ", cmd)

	_, err = queryDB(c, cmd)
	if err != nil {
		log.Warn("Could not connect to InfluxDB: ", err)
		return nil, err
	}
	return c, nil
}

// WritePoint inserts data to InfluxDB
func WritePoint(u url.URL, results chan TestResult) {
	c, err := newInfluxDBClient(u)

	if err != nil {
		log.Warn("No test results will be recorded to InfluxDB")
	} else {
		log.Info("Recording results to InfluxDB: ", u.String())
	}
	for {
		result := <-results

		bp, err := client.NewBatchPoints(client.BatchPointsConfig{
			Database:  db,
			Precision: "ms",
		})

		tags := map[string]string{
			"URLShort": result.URLShort,
		}
		fields := map[string]interface{}{
			"URL":       result.URL,
			"Status":    result.Status,
			"StartTime": result.StartTime,
			"EndTime":   result.EndTime,
			"Latency":   result.Latency,
			"Method":    result.Method,
			"Size":      result.Size,
			"HarFile":   result.HarFile,
		}

		pt, err := client.NewPoint("test_result", tags, fields, time.Now())

		if err != nil {
			log.Fatalln("Error: ", err)
		}

		bp.AddPoint(pt)

		err = c.Write(bp)

		if err != nil {
			log.Fatalln("Error: ", err)
		}
	}
}

// queryDB convenience function to query the database
func queryDB(clnt client.Client, cmd string) (res []client.Result, _ error) {
	q := client.Query{
		Command:  cmd,
		Database: db,
	}
	if response, err := clnt.Query(q); err == nil {
		if response.Error() != nil {
			return res, response.Error()
		}
		res = response.Results
	} else {
		return res, err
	}
	return res, nil
}
