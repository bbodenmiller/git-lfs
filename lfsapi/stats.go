package lfsapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"time"
)

type httpTransferStats struct {
	HeaderSize int
	BodySize   int64
	Start      time.Time
	Stop       time.Time
}

type httpTransfer struct {
	Key           string
	requestStats  *httpTransferStats
	responseStats *httpTransferStats
}

type statsContextKey string

const httpStatsKey = statsContextKey("http")

func (c *Client) LogHTTPStats(w io.WriteCloser) {
	fmt.Fprintf(w, "concurrent=%d time=%d version=%s\n", c.ConcurrentTransfers, time.Now().Unix(), UserAgent)
	c.httpLogger = w
}

// LogStats is intended to be called after all HTTP operations for the
// commmand have finished. It dumps k/v logs, one line per httpTransfer into
// a log file with the current timestamp.
//
// DEPRECATED: Call LogHTTPStats() before the first HTTP request.
func (c *Client) LogStats(out io.Writer) {}

// LogRequest tells the client to log the request's stats to the http log
// after the response body has been read.
func (c *Client) LogRequest(r *http.Request, reqKey string) *http.Request {
	ctx := context.WithValue(r.Context(), httpStatsKey, reqKey)
	return r.WithContext(ctx)
}

// LogResponse sends the current response stats to the http log.
//
// DEPRECATED: Use LogRequest() instead.
func (c *Client) LogResponse(key string, res *http.Response) {}

func (c *Client) startResponseStats(res *http.Response, start time.Time) {
	if c.httpLogger == nil {
		return
	}

	reqHeaderSize := 0
	resHeaderSize := 0

	if dump, err := httputil.DumpRequest(res.Request, false); err == nil {
		reqHeaderSize = len(dump)
	}

	if dump, err := httputil.DumpResponse(res, false); err == nil {
		resHeaderSize = len(dump)
	}

	reqstats := &httpTransferStats{HeaderSize: reqHeaderSize, BodySize: res.Request.ContentLength}

	// Response body size cannot be figured until it is read. Do not rely on a Content-Length
	// header because it may not exist or be -1 in the case of chunked responses.
	resstats := &httpTransferStats{HeaderSize: resHeaderSize, Start: start}
	t := &httpTransfer{requestStats: reqstats, responseStats: resstats}
	if v := res.Request.Context().Value(httpStatsKey); v != nil {
		t.Key = v.(string)
	} else {
		t.Key = "none"
	}

	c.transferMu.Lock()
	if c.transfers == nil {
		c.transfers = make(map[*http.Response]*httpTransfer)
	}
	c.transfers[res] = t
	c.transferMu.Unlock()
}

func (c *Client) finishResponseStats(res *http.Response, bodySize int64) {
	if res == nil || c.httpLogger == nil {
		return
	}

	c.transferMu.Lock()
	defer c.transferMu.Unlock()

	if c.transfers == nil {
		return
	}

	if transfer, ok := c.transfers[res]; ok {
		transfer.responseStats.BodySize = bodySize
		transfer.responseStats.Stop = time.Now()
		if c.httpLogger != nil {
			writeHTTPStats(c.httpLogger, res, transfer)
		}
		delete(c.transfers, res)
	}
}

func writeHTTPStats(w io.Writer, res *http.Response, t *httpTransfer) {
	fmt.Fprintf(w, "key=%s reqheader=%d reqbody=%d resheader=%d resbody=%d restime=%d status=%d url=%s\n",
		t.Key,
		t.requestStats.HeaderSize,
		t.requestStats.BodySize,
		t.responseStats.HeaderSize,
		t.responseStats.BodySize,
		t.responseStats.Stop.Sub(t.responseStats.Start).Nanoseconds(),
		res.StatusCode,
		res.Request.URL,
	)
}
