package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"encoding/json"
	"github.com/influxdata/influxdb/influxql"
	"time"
	"strings"
	"flag"
)

var (
	version         = "alpha"
	DefaultBackend  = "http://localhost:8086/query"
	DefaultBindAddr = ":8085"
	DefaultMaxAge   = "86400"
	c cli
)

type cli struct {
	Backend  string
	BindAddr string
	MaxAge string
}

// Needs to be changed to influxql.Result
type InfluxResponse struct {
	Results []struct {
		StatementId json.RawMessage `json:"statement_id"`
		Series []struct {
			Name string `json:"name"`
			Columns []string `json:"columns"`
			Values []json.RawMessage `json:"values"`
		} `json:"series"`
	} `json:"results"`
}


func main() {
	// Parse backend arguments
	fs := flag.NewFlagSet("InfluxDB cache, version " + version, flag.ExitOnError)
	fs.StringVar(&c.BindAddr, "bind", DefaultBindAddr, "Address where HTTP server listens to.")
	fs.StringVar(&c.Backend, "backend", DefaultBackend, "Backend where requests are being sent.")
	fs.StringVar(&c.MaxAge, "max-age", DefaultMaxAge, "TTL advertised to the cache server for cacheable queries.")
	fs.Parse(os.Args[1:])

	// Start server
	fmt.Println("Serving on "+c.BindAddr+"...")
	http.HandleFunc("/query", query)
	http.ListenAndServe(c.BindAddr, nil)
}

func query(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query()["q"] == nil {
		// Should return ? Or pipe to influxdb
		return
	}
	queryStr := r.URL.Query()["q"][0]

	queries, err := ChopQuery(queryStr)
	if err != nil {
		fmt.Println(err)
		os.Exit(0)
	}

	fmt.Println(">>", queryStr)
	for _, q := range(queries) {
		fmt.Println("\t", len(q), "\t", q[0])
	}

	// Prepare requests
	v := url.Values{}
	ForwardUrlQueryParameters(&v, r.URL)

	var finalResp InfluxResponse
	for _, q := range(queries) {
		var results InfluxResponse
		var err error
		if len(q) < 2 {
			v.Set("q", q[0])
			results, err = GetResponse(c.Backend, v, false)
			if err != nil {
				// Error 500 ?
				return
			}
		} else {
			for _, s := range(q) {
				v.Set("q", s)
				partial, err := GetResponse(c.Backend, v, true)
				if err != nil {
					// Error 500 ?
					return
				}
				Merge(&results, partial)
			}
		}
		finalResp.Results = append(finalResp.Results, results.Results...)
	}
	finalBytes, _ := json.Marshal(finalResp)

	// Send request
	w.Header().Set("Access-Control-Allow-Origin", strings.Join(r.Header["Origin"], ", "))
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, string(finalBytes) + "\n")
}

// Process the query and return the response
func GetResponse(u string, p url.Values, cacheable bool) (InfluxResponse, error) {
	u = u + "?" + p.Encode()

	client := &http.Client{}
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return InfluxResponse{}, err
	}

	if cacheable {
		req.Header.Add("X-Force-Cache-Control", "max-age="+c.MaxAge)
	} else {
		req.Header.Add("X-Force-Cache-Control", `no-cache`)
	}
	resp, err := client.Do(req)
	defer resp.Body.Close()

	if err != nil {
		return InfluxResponse{}, err
	}

	var respJSON InfluxResponse
	bodyBytes, _ := ioutil.ReadAll(resp.Body)
	json.Unmarshal(bodyBytes, &respJSON)

	return respJSON, nil
}

// Forward url query parmeters to another url
func ForwardUrlQueryParameters(dst *url.Values, src *url.URL) {
	for p, v := range(src.Query()) {
		if p != "q" {
			dst.Set(p, v[0])
		}
	}
}



//
// Chop part
//

const (
	// Size of a complete chunk
	ChunkSize  = 60

	// Minimum number of chunks to start splitting
	// Minimum 3 to have at least one complete chunk
	// Otherwise there is no reason for splitting the query
	MinChunkNb = 3
)

// Chop a query in multiple ordered cacheable queries
func ChopQuery(queryStr string) ([][]string, error) {
	query, err := influxql.ParseQuery(queryStr)
	if err != nil {
		return nil, err
	}

	var results [][]string

	for _, stmt := range(query.Statements) {
		switch s := stmt.(type) {
		case *influxql.SelectStatement:
			chopped, err := ChopStatement(s)
			if err != nil {
				return nil, err
			}
			results = append(results, chopped)

		default:
			results = append(results, []string{s.String()})
		}
	}

	return results, nil
}

// ChopStatement splits a statement accross it's time range
// If the statement must not be split, it is returned untouched
func ChopStatement(stmt *influxql.SelectStatement) ([]string, error) {
	var intact = []string{stmt.String()}
	var chopped []string

	// Return the statement intact if it is time independent
	interval, err := RecursiveGroupByInterval(stmt)
	if interval == 0 || err != nil {
		return intact, err
	}

	// Determine the time range of the query
	// Convert "now()" to current time. (Necessary !!)
	stmt = stmt.Reduce(&influxql.NowValuer{Now: time.Now().UTC()})
	min, max, _ := RecursiveTimeRange(stmt)
	if min.IsZero() {
		return intact, nil
	}
	if max.IsZero() {
		max = time.Now().UTC()
	}

	// Check if the interval is big enough
	ChunkDuration := time.Duration(ChunkSize) * interval
	if max.Sub(min) < ChunkDuration * time.Duration(MinChunkNb) {
		return intact, nil
	}

	// Determine the different time ranges
	times := []time.Time{min, min.Truncate(ChunkDuration).Add(ChunkDuration)}
	for i := 1; times[i].Add(ChunkDuration).Before(max); i++ {
		times = append(times, times[i].Add(ChunkDuration))
	}
	times = append(times, max)

	// Create the list of statements
	for i := 0; i < len(times) - 1; i++ {
		new := stmt.Clone()
		SetTimeRangeRecursively(new, times[i], times[i + 1])
		chopped = append(chopped, new.String())
	}

	return chopped, nil
}

// RecursiveTimeRange return the smallest time range
func RecursiveTimeRange(stmt influxql.Statement) (min, max time.Time, err error) {
	var v timeRangeVisitor
	influxql.Walk(&v, stmt)
	return v.Start, v.End, nil
}

type timeRangeVisitor struct {
	Start time.Time
	End   time.Time
}

func (v *timeRangeVisitor) Visit(n influxql.Node) influxql.Visitor {
	switch node := n.(type) {
	case *influxql.SelectStatement:
		start, end, _ := influxql.TimeRange(node.Condition)
		if v.Start.Before(start) {
			v.Start = start
		}
		if !end.IsZero() {
			if v.End.IsZero() ||  v.End.After(end) {
				v.End = end
			}
		}
	}
	return v
}

// Rewrite the statement with the defined time range
func SetTimeRangeRecursively(stmt *influxql.SelectStatement, start, end time.Time) error {
	if end.IsZero() {
		end = time.Now().UTC()
	}
	if influxql.HasTimeExpr(stmt.Condition) {
		err := stmt.SetTimeRange(start, end)
		if err != nil {
			return err
		}
	}
	for _, s := range(stmt.Sources) {
		if subq, ok := s.(*influxql.SubQuery); ok {
			err := SetTimeRangeRecursively(subq.Statement, start, end)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// Return the least common multiple interval that can be used to split the statement
// Return a 0s interval if the statement can be split anywhere
// Return an error if the statement cannot be split (disabled for the moment)
func RecursiveGroupByInterval(stmt *influxql.SelectStatement) (interval time.Duration, err error) {
	interval, err = stmt.GroupByInterval()
	if err != nil {
		return time.Duration(0), err
	}

	// Stop the whole process if the statement cannot be sliced
	if !stmt.IsRawQuery && interval == 0 {
		return time.Duration(0), nil
		// return time.Duration(0), errors.New("statement is time independent")
	}

	// Otherwise find the LCM recursively in sub queries
	for _, s := range(stmt.Sources) {
		if subq, ok := s.(*influxql.SubQuery); ok {
			i, err := RecursiveGroupByInterval(subq.Statement)
			if err != nil {
				return time.Duration(0), err
			}

			// Only keep the 
			if interval == 0 {
				interval = i
			} else if i != 0 {
				interval = LCM(interval, i)
			}
		}
	}
	return
}

// Greatest common divisor for time.Duration
func GCD(a, b time.Duration) time.Duration {
	var tmp time.Duration
	for b != 0 {
		tmp = a % b
		a, b = b, tmp
	}
	return a
}

// Least common multiple for time.Duration
func LCM(a, b time.Duration) time.Duration {
	c := GCD(a, b)
	if c == 0 {
		return 0
	}
	// Here the order is important to avoid overflow
	return a / c * b
}



//
// Merge part
//

// influxql.TagSet
// influxql.Message
// influxql.Result
func Merge(dst *InfluxResponse, src InfluxResponse) error {
	// fmt.Println("Merging...")
	for i, srcResult := range(src.Results) {
		// Each result
		if i + 1 > len(dst.Results) {
			// Append results if there is not enough
			// fmt.Println("Append new result")
			dst.Results = append(dst.Results, srcResult)
			continue
		}
		for _, srcSeries := range(srcResult.Series) {
			// Each series
			for k, dstSeries := range(dst.Results[i].Series) {
				if dstSeries.Name == srcSeries.Name {
					// Append values
					// fmt.Println("Append", len(srcSeries.Values), "values to", dstSeries.Name)
					dst.Results[i].Series[k].Values = append(dst.Results[i].Series[k].Values, srcSeries.Values...)
					break
				}
				// Otherwise append the Series to the destination
				// fmt.Println("Append new series", dstSeries.Name)
				dst.Results[i].Series = append(dst.Results[i].Series, srcSeries)
			}
		}
	}
	return nil
}
