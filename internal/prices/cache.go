package prices

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"slices"
	"time"
)

const (
	cacheVersion = 1
	cacheUnit    = "usd_per_1m_tokens"
)

// ErrUnusable marks a price cache file a reader must treat as absent: it is
// not parseable, carries another version or unit, lacks a field or holds a
// price that is not a plain decimal.
var ErrUnusable = errors.New("price cache is unusable")

// Cache is the local copy of the prices of the wanted models.
type Cache struct {
	Source    string                      // URL the prices were fetched from
	ETag      string                      // the source's entity tag; "" when it gave none
	FetchedAt time.Time                   // when a body was last downloaded
	CheckedAt time.Time                   // when the source last answered, a 304 included
	Models    map[string]map[string]Price // provider → model → price
	Unpriced  map[string][]string         // provider → wanted models the source gave no usable price
}

// Lookup returns the cached price of a model.
func (c *Cache) Lookup(provider, model string) (Price, bool) {
	p, ok := c.Models[provider][model]
	return p, ok
}

// Knows reports whether the cache has a verdict on a model: priced or unpriced.
func (c *Cache) Knows(provider, model string) bool {
	if _, ok := c.Models[provider][model]; ok {
		return true
	}
	return slices.Contains(c.Unpriced[provider], model)
}

// Covers reports whether every wanted model is known.
func (c *Cache) Covers(wanted map[string][]string) bool {
	for provider, models := range wanted {
		for _, model := range models {
			if !c.Knows(provider, model) {
				return false
			}
		}
	}
	return true
}

type priceFile struct {
	Input       string `json:"input"`
	CachedInput string `json:"cached_input"`
	CacheWrite  string `json:"cache_write"`
	Output      string `json:"output"`
}

// cacheFile is the persisted form; the field order is the file's.
type cacheFile struct {
	V         int                             `json:"v"`
	Source    string                          `json:"source"`
	ETag      *string                         `json:"etag"`
	FetchedAt string                          `json:"fetched_at"`
	CheckedAt string                          `json:"checked_at"`
	Unit      string                          `json:"unit"`
	Models    map[string]map[string]priceFile `json:"models"`
	Unpriced  map[string][]string             `json:"unpriced"`
}

// Encode renders the cache as its file: keys sorted, times in UTC, prices as
// shortest exact decimals, one trailing newline. It fails on a price that has
// no exact decimal form.
func (c *Cache) Encode() ([]byte, error) {
	f := cacheFile{
		V:         cacheVersion,
		Source:    c.Source,
		FetchedAt: c.FetchedAt.UTC().Format(time.RFC3339),
		CheckedAt: c.CheckedAt.UTC().Format(time.RFC3339),
		Unit:      cacheUnit,
		Models:    map[string]map[string]priceFile{},
		Unpriced:  map[string][]string{},
	}
	if c.ETag != "" {
		f.ETag = &c.ETag
	}
	for provider, models := range c.Models {
		f.Models[provider] = map[string]priceFile{}
		for name, p := range models {
			pf, err := encodePrice(p)
			if err != nil {
				return nil, fmt.Errorf("%s/%s: %w", provider, name, err)
			}
			f.Models[provider][name] = pf
		}
	}
	for provider, names := range c.Unpriced {
		f.Unpriced[provider] = slices.Sorted(slices.Values(names))
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func encodePrice(p Price) (priceFile, error) {
	var pf priceFile
	for _, field := range []struct {
		out *string
		in  *big.Rat
	}{{&pf.Input, p.Input}, {&pf.CachedInput, p.CachedInput}, {&pf.CacheWrite, p.CacheWrite}, {&pf.Output, p.Output}} {
		if field.in == nil {
			return priceFile{}, errors.New("price field is missing")
		}
		s, err := FormatDecimal(field.in)
		if err != nil {
			return priceFile{}, err
		}
		*field.out = s
	}
	return pf, nil
}

// Decode reads a cache file. Every failure wraps ErrUnusable.
func Decode(data []byte) (*Cache, error) {
	c, err := decode(data)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnusable, err)
	}
	return c, nil
}

var plainDecimal = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`)

func decode(data []byte) (*Cache, error) {
	// Pointers tell a missing field from a zero one.
	var f struct {
		V         *int                             `json:"v"`
		Source    *string                          `json:"source"`
		ETag      *string                          `json:"etag"`
		FetchedAt *string                          `json:"fetched_at"`
		CheckedAt *string                          `json:"checked_at"`
		Unit      *string                          `json:"unit"`
		Models    map[string]map[string]*priceFile `json:"models"`
		Unpriced  map[string][]string              `json:"unpriced"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&f); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("data after the JSON object")
	}
	switch {
	case f.V == nil || *f.V != cacheVersion:
		return nil, errors.New("unsupported version")
	case f.Unit == nil || *f.Unit != cacheUnit:
		return nil, errors.New("unsupported unit")
	case f.Source == nil, f.FetchedAt == nil, f.CheckedAt == nil, f.Models == nil, f.Unpriced == nil:
		return nil, errors.New("a field is missing")
	}
	c := &Cache{
		Source:   *f.Source,
		Models:   map[string]map[string]Price{},
		Unpriced: f.Unpriced,
	}
	if f.ETag != nil {
		c.ETag = *f.ETag
	}
	var err error
	if c.FetchedAt, err = parseTime(*f.FetchedAt); err != nil {
		return nil, fmt.Errorf("fetched_at: %w", err)
	}
	if c.CheckedAt, err = parseTime(*f.CheckedAt); err != nil {
		return nil, fmt.Errorf("checked_at: %w", err)
	}
	for provider, models := range f.Models {
		c.Models[provider] = map[string]Price{}
		for name, pf := range models {
			if pf == nil {
				return nil, fmt.Errorf("%s/%s: not an object", provider, name)
			}
			p, err := decodePrice(*pf)
			if err != nil {
				return nil, fmt.Errorf("%s/%s: %w", provider, name, err)
			}
			c.Models[provider][name] = p
		}
	}
	return c, nil
}

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	return t.UTC(), err
}

func decodePrice(pf priceFile) (Price, error) {
	var p Price
	for _, field := range []struct {
		name string
		in   string
		out  **big.Rat
	}{{"input", pf.Input, &p.Input}, {"cached_input", pf.CachedInput, &p.CachedInput}, {"cache_write", pf.CacheWrite, &p.CacheWrite}, {"output", pf.Output, &p.Output}} {
		if !plainDecimal.MatchString(field.in) {
			return Price{}, fmt.Errorf("%s: %q is not a plain decimal", field.name, field.in)
		}
		r, ok := new(big.Rat).SetString(field.in)
		if !ok {
			return Price{}, fmt.Errorf("%s: %q is not a plain decimal", field.name, field.in)
		}
		*field.out = r
	}
	return p, nil
}
