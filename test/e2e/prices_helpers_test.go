package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

func pricesPath(s *sandbox) string { return filepath.Join(s.home, "prices.json") }

func priceListFixture() string { return filepath.Join(repoRoot, "testdata", "prices", "list.json") }

func catalogFixture() string { return filepath.Join(repoRoot, "testdata", "prices", "catalog.json") }

// priceServer serves the fixture price list on 127.0.0.1 and records every
// request it receives.
type priceServer struct {
	*httptest.Server
	mu     sync.Mutex
	reqs   []*http.Request
	body   []byte
	etag   string        // "" serves no ETag and never answers 304
	status int           // non-zero answers this status instead of the list
	delay  time.Duration // waits this long before answering
}

func newPriceServer(t *testing.T) *priceServer {
	t.Helper()
	ps := &priceServer{body: []byte(readFile(t, priceListFixture())), etag: `"e1"`}
	ps.Server = httptest.NewServer(http.HandlerFunc(ps.serve))
	t.Cleanup(ps.Server.Close)
	return ps
}

func (ps *priceServer) serve(w http.ResponseWriter, r *http.Request) {
	ps.mu.Lock()
	ps.reqs = append(ps.reqs, r.Clone(context.Background()))
	body, etag, status, delay := ps.body, ps.etag, ps.status, ps.delay
	ps.mu.Unlock()
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-r.Context().Done():
			return
		}
	}
	if status != 0 {
		w.WriteHeader(status)
		return
	}
	if etag != "" {
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
	}
	w.Write(body)
}

func (ps *priceServer) requests() []*http.Request {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return append([]*http.Request(nil), ps.reqs...)
}

func (ps *priceServer) configure(f func(*priceServer)) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	f(ps)
}

// refreshing points the sandbox at a price server and a codex catalog, and
// seeds one telemetry run on m-zero. Tests that need another shape start from
// newSandbox themselves.
func refreshing(t *testing.T) (*sandbox, *priceServer) {
	t.Helper()
	ps := newPriceServer(t)
	s := newSandbox(t).set("AGENTCLI_PRICES_URL", ps.URL).set("FAKECODEX_MODELS", catalogFixture())
	seedModelRuns(t, s, `"model":"m-zero"`)
	return s, ps
}

// seedModelRuns writes one telemetry run per field list, in one month file.
func seedModelRuns(t *testing.T, s *sandbox, fields ...string) {
	t.Helper()
	ts := tsAgo(time.Hour)
	lines := make([]string, len(fields))
	for i, f := range fields {
		lines[i] = recLine("pr-"+strconv.Itoa(i), ts, f)
	}
	writeMonthFile(t, s.home, monthOf(ts), lines...)
}

func strs(t *testing.T, v any) []string {
	t.Helper()
	list, ok := v.([]any)
	if !ok {
		t.Fatalf("%#v is not a JSON list", v)
	}
	out := make([]string, len(list))
	for i, e := range list {
		if out[i], ok = e.(string); !ok {
			t.Fatalf("%#v is not a string", e)
		}
	}
	return out
}

// ageCache rewrites the times of the price cache so it looks fetched and
// checked that long ago.
func ageCache(t *testing.T, s *sandbox, fetchedAgo, checkedAgo time.Duration) {
	t.Helper()
	c := readJSONFile(t, pricesPath(s))
	c["fetched_at"], c["checked_at"] = tsAgo(fetchedAgo), tsAgo(checkedAgo)
	writeCache(t, s, c)
}

func writeCache(t *testing.T, s *sandbox, c map[string]any) {
	t.Helper()
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pricesPath(s), data, 0o600); err != nil {
		t.Fatal(err)
	}
}
