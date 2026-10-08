package prices

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	// DefaultFetchTimeout bounds a whole fetch: connecting, waiting and reading.
	DefaultFetchTimeout = 20 * time.Second
	// DefaultFetchMaxBytes bounds the decoded body.
	DefaultFetchMaxBytes int64 = 32 << 20
)

// FetchOptions bounds a fetch; a zero field takes its default.
type FetchOptions struct {
	Timeout  time.Duration // for the whole exchange, body included
	MaxBytes int64         // largest decoded body accepted
}

// Fetched is what the source answered.
type Fetched struct {
	NotModified bool   // the source answered 304 to the conditional request
	Body        []byte // the decoded body of a 200
	ETag        string // the entity tag of a 200, "" when it sent none
}

// Fetch GETs url accepting gzip. A non-empty etag makes the request
// conditional. Only 200 and 304 succeed (a 304 only to a conditional
// request); a transport error, a timeout, another status or a decoded body
// over the limit is an error. The request carries no credentials and no query.
func Fetch(url, etag string, opts FetchOptions) (Fetched, error) {
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultFetchTimeout
	}
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = DefaultFetchMaxBytes
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return Fetched{}, fmt.Errorf("price list request: %w", err)
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	// The transport adds Accept-Encoding: gzip and decodes the answer itself.
	client := &http.Client{Timeout: opts.Timeout}
	resp, err := client.Do(req)
	if err != nil {
		return Fetched{}, fmt.Errorf("price list: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotModified:
		if etag == "" {
			return Fetched{}, errors.New("price list: the source answered 304 to an unconditional request")
		}
		return Fetched{NotModified: true}, nil
	case http.StatusOK:
	default:
		return Fetched{}, fmt.Errorf("price list: the source answered %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, opts.MaxBytes+1))
	if err != nil {
		return Fetched{}, fmt.Errorf("price list: reading the body: %w", err)
	}
	if int64(len(body)) > opts.MaxBytes {
		return Fetched{}, fmt.Errorf("price list: the body is over %d bytes", opts.MaxBytes)
	}
	return Fetched{Body: body, ETag: resp.Header.Get("ETag")}, nil
}
