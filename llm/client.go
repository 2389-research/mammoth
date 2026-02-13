// ABOUTME: Client infrastructure for the unified LLM client SDK with provider routing and middleware.
// ABOUTME: Provides NewClient with functional options, middleware chain execution, and module-level default client.

package llm

import (
	"context"
	"fmt"
	"os"
	"sync"
)

// Middleware is a function that wraps an LLM call, enabling request/response
// transformation, logging, caching, and other cross-cutting concerns.
// Middleware executes in registration order for requests and reverse order
// for responses (onion/chain-of-responsibility pattern).
type Middleware func(ctx context.Context, req Request, next NextFunc) (*Response, error)

// NextFunc is the function signature passed to middleware to continue the chain.
type NextFunc func(ctx context.Context, req Request) (*Response, error)

// Client is the primary entry point for making LLM API calls. It manages
// provider adapters, routes requests to the correct provider, and applies
// the middleware chain.
type Client struct {
	providers       map[string]ProviderAdapter
	defaultProvider string
	middleware      []Middleware
}

// ClientOption is a functional option for configuring a Client.
type ClientOption func(*Client)

// WithProvider registers a ProviderAdapter under the given name. If this is
// the first provider registered and no default has been set, it becomes the
// default provider.
func WithProvider(name string, adapter ProviderAdapter) ClientOption {
	return func(c *Client) {
		c.providers[name] = adapter
		if c.defaultProvider == "" {
			c.defaultProvider = name
		}
	}
}

// WithDefaultProvider sets the name of the provider used when a Request does
// not specify a Provider field.
func WithDefaultProvider(name string) ClientOption {
	return func(c *Client) {
		c.defaultProvider = name
	}
}

// WithMiddleware appends one or more middleware functions to the client's
// middleware chain. Middleware is executed in registration order for the
// request phase and reverse order for the response phase.
func WithMiddleware(mw ...Middleware) ClientOption {
	return func(c *Client) {
		c.middleware = append(c.middleware, mw...)
	}
}

// NewClient creates a new Client with the given options applied.
func NewClient(opts ...ClientOption) *Client {
	c := &Client{
		providers: make(map[string]ProviderAdapter),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// envProviderAdapter is a placeholder adapter for providers detected via environment
// variables. It returns a ConfigurationError for all operations, indicating that real
// adapters have not yet been wired in. This placeholder enables FromEnv to register
// detected providers so routing logic works once real adapters are available.
type envProviderAdapter struct {
	providerName string
	apiKey       string
}

func (a *envProviderAdapter) Name() string { return a.providerName }

func (a *envProviderAdapter) Complete(ctx context.Context, req Request) (*Response, error) {
	return nil, &ConfigurationError{
		SDKError: SDKError{
			Message: fmt.Sprintf("provider %q detected from environment but real adapter not yet implemented", a.providerName),
		},
	}
}

func (a *envProviderAdapter) Stream(ctx context.Context, req Request) (<-chan StreamEvent, error) {
	return nil, &ConfigurationError{
		SDKError: SDKError{
			Message: fmt.Sprintf("provider %q detected from environment but real adapter not yet implemented", a.providerName),
		},
	}
}

func (a *envProviderAdapter) Close() error { return nil }

// FromEnv creates a Client by detecting API keys in the environment. It checks
// OPENAI_API_KEY, ANTHROPIC_API_KEY, and GEMINI_API_KEY. The first detected
// provider becomes the default. Returns a ConfigurationError if no keys are found.
func FromEnv() (*Client, error) {
	type envProvider struct {
		envVar string
		name   string
	}

	providers := []envProvider{
		{envVar: "OPENAI_API_KEY", name: "openai"},
		{envVar: "ANTHROPIC_API_KEY", name: "anthropic"},
		{envVar: "GEMINI_API_KEY", name: "gemini"},
	}

	var opts []ClientOption
	found := false

	for _, p := range providers {
		key := os.Getenv(p.envVar)
		if key != "" {
			adapter := &envProviderAdapter{
				providerName: p.name,
				apiKey:       key,
			}
			opts = append(opts, WithProvider(p.name, adapter))
			found = true
		}
	}

	if !found {
		return nil, &ConfigurationError{
			SDKError: SDKError{
				Message: "no API keys found in environment (checked OPENAI_API_KEY, ANTHROPIC_API_KEY, GEMINI_API_KEY)",
			},
		}
	}

	return NewClient(opts...), nil
}

// resolveProvider determines which ProviderAdapter should handle the request.
// It uses the request's Provider field if set, otherwise falls back to the
// client's default provider. Returns a ConfigurationError if no provider is found.
func (c *Client) resolveProvider(req Request) (ProviderAdapter, error) {
	name := req.Provider
	if name == "" {
		name = c.defaultProvider
	}
	if name == "" {
		return nil, &ConfigurationError{
			SDKError: SDKError{
				Message: "no provider specified and no default provider configured",
			},
		}
	}

	adapter, ok := c.providers[name]
	if !ok {
		return nil, &ConfigurationError{
			SDKError: SDKError{
				Message: fmt.Sprintf("provider %q not registered", name),
			},
		}
	}
	return adapter, nil
}

// Complete sends a completion request through the middleware chain and then to
// the appropriate provider adapter. It routes based on req.Provider or the
// default provider.
func (c *Client) Complete(ctx context.Context, req Request) (*Response, error) {
	// Build the innermost handler that resolves the provider and calls Complete
	handler := func(ctx context.Context, req Request) (*Response, error) {
		adapter, err := c.resolveProvider(req)
		if err != nil {
			return nil, err
		}
		return adapter.Complete(ctx, req)
	}

	// Wrap with middleware in reverse order so the first middleware registered
	// is the outermost layer (executed first on the way in, last on the way out).
	chain := handler
	for i := len(c.middleware) - 1; i >= 0; i-- {
		mw := c.middleware[i]
		next := chain
		chain = func(ctx context.Context, req Request) (*Response, error) {
			return mw(ctx, req, next)
		}
	}

	return chain(ctx, req)
}

// Stream sends a streaming request to the appropriate provider adapter.
// It routes based on req.Provider or the default provider.
func (c *Client) Stream(ctx context.Context, req Request) (<-chan StreamEvent, error) {
	adapter, err := c.resolveProvider(req)
	if err != nil {
		return nil, err
	}
	return adapter.Stream(ctx, req)
}

// Close shuts down all registered provider adapters. Errors from individual
// adapters are collected and returned as a combined error.
func (c *Client) Close() error {
	var errs []error
	for name, adapter := range c.providers {
		if err := adapter.Close(); err != nil {
			errs = append(errs, fmt.Errorf("closing provider %q: %w", name, err))
		}
	}
	if len(errs) > 0 {
		combined := errs[0]
		for _, e := range errs[1:] {
			combined = fmt.Errorf("%w; %v", combined, e)
		}
		return combined
	}
	return nil
}

// RegisterProvider adds or replaces a provider adapter on the client.
// If no default provider is set, the newly registered provider becomes the default.
func (c *Client) RegisterProvider(name string, adapter ProviderAdapter) {
	c.providers[name] = adapter
	if c.defaultProvider == "" {
		c.defaultProvider = name
	}
}

// Module-level default client for convenience functions.

var (
	defaultClient   *Client
	defaultClientMu sync.Mutex
)

// SetDefaultClient sets the module-level default client. Pass nil to clear it.
func SetDefaultClient(c *Client) {
	defaultClientMu.Lock()
	defer defaultClientMu.Unlock()
	defaultClient = c
}

// GetDefaultClient returns the module-level default client. If no client has
// been set, it attempts lazy initialization via FromEnv. Returns nil if
// FromEnv fails (no API keys configured).
func GetDefaultClient() *Client {
	defaultClientMu.Lock()
	defer defaultClientMu.Unlock()

	if defaultClient != nil {
		return defaultClient
	}

	// Attempt lazy init from environment
	c, err := FromEnv()
	if err != nil {
		return nil
	}
	defaultClient = c
	return defaultClient
}
