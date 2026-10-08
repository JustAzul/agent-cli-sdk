package prices

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"slices"
)

// ParseList reads a LiteLLM price list for the wanted models (provider →
// model names). namespaces maps a provider to the litellm_provider its
// entries carry; the models of a provider with no namespace are unpriced.
//
// A model is priced from the entry whose key equals its name and whose
// litellm_provider equals the provider's namespace. That entry needs its input
// and output costs as JSON numbers, else the model is unpriced. Every price is
// read from its JSON text as an exact rational and scaled to US dollars per
// million tokens, never through binary floating point. A cache-read cost the
// entry leaves out, and a cache-write cost it leaves out, nulls or sets to 0,
// resolve to the input price.
//
// Providers with no priced (or no unpriced) model are absent from the result
// maps, which are never nil. Unpriced lists are sorted.
func ParseList(body []byte, wanted map[string][]string, namespaces map[string]string) (models map[string]map[string]Price, unpriced map[string][]string, err error) {
	var list map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(&list); err != nil {
		return nil, nil, fmt.Errorf("price list: %w", err)
	}
	if list == nil {
		return nil, nil, errors.New("price list: not a JSON object")
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, nil, errors.New("price list: data after the JSON object")
	}

	models = map[string]map[string]Price{}
	unpriced = map[string][]string{}
	for provider, names := range wanted {
		namespace := namespaces[provider]
		for _, name := range names {
			var price Price
			priced := false
			if namespace != "" {
				price, priced = entryPrice(list[name], namespace)
			}
			if !priced {
				unpriced[provider] = append(unpriced[provider], name)
				continue
			}
			if models[provider] == nil {
				models[provider] = map[string]Price{}
			}
			models[provider][name] = price
		}
	}
	for provider, names := range unpriced {
		slices.Sort(names)
		unpriced[provider] = slices.Compact(names)
	}
	return models, unpriced, nil
}

// entryPrice prices one list entry of the given litellm_provider.
func entryPrice(raw json.RawMessage, namespace string) (Price, bool) {
	var entry map[string]json.RawMessage
	if json.Unmarshal(raw, &entry) != nil {
		return Price{}, false
	}
	var provider string
	if json.Unmarshal(entry["litellm_provider"], &provider) != nil || provider != namespace {
		return Price{}, false
	}
	input, ok := perMillion(entry["input_cost_per_token"])
	if !ok {
		return Price{}, false
	}
	output, ok := perMillion(entry["output_cost_per_token"])
	if !ok {
		return Price{}, false
	}
	price := Price{Input: input, CachedInput: input, CacheWrite: input, Output: output}
	if read, ok := perMillion(entry["cache_read_input_token_cost"]); ok {
		price.CachedInput = read
	}
	if write, ok := perMillion(entry["cache_creation_input_token_cost"]); ok && write.Sign() > 0 {
		price.CacheWrite = write
	}
	return price, true
}

// perMillion reads a JSON number, a cost per token, as US dollars per million
// tokens. Anything else, a negative number included, reports false.
func perMillion(raw json.RawMessage) (*big.Rat, bool) {
	text := bytes.TrimSpace(raw)
	if len(text) == 0 || text[0] != '-' && (text[0] < '0' || text[0] > '9') {
		return nil, false
	}
	r, ok := new(big.Rat).SetString(string(text))
	if !ok || r.Sign() < 0 {
		return nil, false
	}
	return r.Mul(r, million), true
}
