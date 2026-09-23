// Package openzoo adapts the harness to the OpenZoo local proxy (`npx openzoo`).
//
// The proxy is an OpenAI-compatible server on localhost that pays the zoo per
// request over x402 from a local burner wallet. There is no account and no API
// key: the bearer is a placeholder the proxy ignores, so this client accepts an
// empty key and substitutes one. Every request is a Responses API call streamed
// through unbuffered, exactly like the OpenAI client, so the rest of the harness
// is unchanged.
package openzoo

import (
	"encoding/json/jsontext"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/unreallabsai/unreal-agent/harness/llm"
	"github.com/unreallabsai/unreal-agent/harness/llm/responsesapi"
	"github.com/unreallabsai/unreal-agent/harness/primitives"
)

const (
	// BaseURL is where `openzoo` listens by default (OPENZOO_PORT overrides).
	BaseURL = "http://127.0.0.1:8402/v1"
	// DefaultModel lets the zoo pick the cheapest model that can do the job.
	DefaultModel = "openzoo/auto"
	// PlaceholderAPIKey is the documented dummy bearer; the zoo takes payment, not keys.
	PlaceholderAPIKey = "sk-openzoo"
	// DefaultMaxAttempts is sized so a wallet or gateway funding gap is waited
	// out rather than fatal: 60 attempts at up to 30s apart is ~30 minutes.
	DefaultMaxAttempts = 60
)

// fundingRetryPolicy treats "cannot pay right now" as transient. The proxy
// answers 402 while its own wallet is empty and the gateway answers 503 while
// its upstream payer is underfunded; both clear as soon as USDC lands, and a
// task that has run for twenty turns should not be lost to a one-minute gap.
var fundingRetryPolicy = primitives.RemoteRetryPolicy{
	InitialBackoff: 5 * time.Second,
	MaxBackoff:     30 * time.Second,
	RetryableStatusCodes: []int{
		http.StatusPaymentRequired,
		http.StatusRequestTimeout,
		http.StatusTooEarly,
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
		520, 521, 522, 523, 524, 529,
	},
}

type Config struct {
	APIKey      string // Optional. Defaults to PlaceholderAPIKey.
	BaseURL     string
	MaxAttempts *int
	Trace       func(Exchange)
}

type Exchange = responsesapi.Exchange

type Client struct {
	llm.Adapter
	remote *primitives.RemoteClient
}

var _ llm.Adapter = (*Client)(nil)

func NewClient(config Config) (*Client, error) {
	apiKey := strings.TrimSpace(config.APIKey)
	if apiKey == "" {
		apiKey = PlaceholderAPIKey
	}
	baseURL := strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	if baseURL == "" {
		return nil, errors.New("OpenZoo base URL must be set")
	}

	maxAttempts := config.MaxAttempts
	if maxAttempts == nil {
		attempts := DefaultMaxAttempts
		maxAttempts = &attempts
	}
	retryPolicy := fundingRetryPolicy
	remote := primitives.NewRemoteClient()
	adapter, err := responsesapi.NewAdapter(remote, responsesapi.Config{
		Endpoint: baseURL + "/responses",
		Headers: map[string][]string{
			"Authorization": {"Bearer " + apiKey},
			"Content-Type":  {"application/json"},
		},
		Trace:       config.Trace,
		MaxAttempts: maxAttempts,
		RetryPolicy: &retryPolicy,
		// The proxy forwards caller headers to the zoo gateway, which fronts the
		// same OpenRouter-style upstreams as the openrouter client. Pinning the
		// session to one warm upstream and opting into a one-hour cache breakpoint
		// means each turn pays for the new suffix, not a full-prefix rewrite —
		// and with x402 that discount lands directly on the wallet, per call.
		CacheKeyPlacement: responsesapi.CacheKeyPlacement{Header: "x-session-id"},
		Extensions: map[string]jsontext.Value{
			"cache_control": jsontext.Value(`{"type":"ephemeral","ttl":"1h"}`),
		},
	})
	if err != nil {
		_ = remote.Close()
		return nil, err
	}
	return &Client{Adapter: adapter, remote: remote}, nil
}

func (client *Client) Close() error {
	return client.remote.Close()
}
