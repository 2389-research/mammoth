// ABOUTME: Tests for the Client infrastructure, middleware chain, and provider routing.
// ABOUTME: Uses real test doubles (testAdapter) implementing ProviderAdapter to verify behavior.

package llm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
)

// testAdapter is a real ProviderAdapter implementation that returns pre-configured values.
// It records calls for verification and supports configurable Complete/Stream behavior.
type testAdapter struct {
	name          string
	completeResp  *Response
	completeErr   error
	streamEvents  []StreamEvent
	streamErr     error
	completeCalls []Request
	streamCalls   []Request
	closed        bool
	mu            sync.Mutex
}

func newTestAdapter(name string) *testAdapter {
	return &testAdapter{
		name: name,
		completeResp: &Response{
			ID:           "resp-" + name,
			Model:        "test-model",
			Provider:     name,
			Message:      AssistantMessage("hello from " + name),
			FinishReason: FinishReason{Reason: FinishStop},
		},
	}
}

func (a *testAdapter) Name() string { return a.name }

func (a *testAdapter) Complete(ctx context.Context, req Request) (*Response, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.completeCalls = append(a.completeCalls, req)
	if a.completeErr != nil {
		return nil, a.completeErr
	}
	return a.completeResp, nil
}

func (a *testAdapter) Stream(ctx context.Context, req Request) (<-chan StreamEvent, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.streamCalls = append(a.streamCalls, req)
	if a.streamErr != nil {
		return nil, a.streamErr
	}
	ch := make(chan StreamEvent, len(a.streamEvents))
	for _, evt := range a.streamEvents {
		ch <- evt
	}
	close(ch)
	return ch, nil
}

func (a *testAdapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closed = true
	return nil
}

func (a *testAdapter) getCompleteCalls() []Request {
	a.mu.Lock()
	defer a.mu.Unlock()
	result := make([]Request, len(a.completeCalls))
	copy(result, a.completeCalls)
	return result
}

func (a *testAdapter) getStreamCalls() []Request {
	a.mu.Lock()
	defer a.mu.Unlock()
	result := make([]Request, len(a.streamCalls))
	copy(result, a.streamCalls)
	return result
}

func (a *testAdapter) isClosed() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.closed
}

// TestNewClientWithProviders verifies that a client can be created with providers
// using the functional options pattern and that provider registration works.
func TestNewClientWithProviders(t *testing.T) {
	adapter1 := newTestAdapter("openai")
	adapter2 := newTestAdapter("anthropic")

	client := NewClient(
		WithProvider("openai", adapter1),
		WithProvider("anthropic", adapter2),
		WithDefaultProvider("openai"),
	)

	if client == nil {
		t.Fatal("expected non-nil client")
	}

	// Verify the client can route to both providers
	ctx := context.Background()

	resp, err := client.Complete(ctx, Request{
		Provider: "openai",
		Messages: []Message{UserMessage("hi")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Provider != "openai" {
		t.Errorf("expected provider 'openai', got %q", resp.Provider)
	}

	resp, err = client.Complete(ctx, Request{
		Provider: "anthropic",
		Messages: []Message{UserMessage("hi")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Provider != "anthropic" {
		t.Errorf("expected provider 'anthropic', got %q", resp.Provider)
	}
}

// TestRoutingToCorrectProvider verifies that the client routes requests to the
// provider specified in the request, not just the default.
func TestRoutingToCorrectProvider(t *testing.T) {
	openai := newTestAdapter("openai")
	anthropic := newTestAdapter("anthropic")

	client := NewClient(
		WithProvider("openai", openai),
		WithProvider("anthropic", anthropic),
		WithDefaultProvider("openai"),
	)

	ctx := context.Background()

	// Explicitly request anthropic
	_, err := client.Complete(ctx, Request{
		Provider: "anthropic",
		Messages: []Message{UserMessage("hello")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// openai should have 0 calls, anthropic should have 1
	if len(openai.getCompleteCalls()) != 0 {
		t.Errorf("expected 0 calls to openai, got %d", len(openai.getCompleteCalls()))
	}
	if len(anthropic.getCompleteCalls()) != 1 {
		t.Errorf("expected 1 call to anthropic, got %d", len(anthropic.getCompleteCalls()))
	}
}

// TestDefaultProviderFallback verifies that when no Provider is specified in the
// request, the client routes to the default provider.
func TestDefaultProviderFallback(t *testing.T) {
	openai := newTestAdapter("openai")
	anthropic := newTestAdapter("anthropic")

	client := NewClient(
		WithProvider("openai", openai),
		WithProvider("anthropic", anthropic),
		WithDefaultProvider("anthropic"),
	)

	ctx := context.Background()

	// No provider specified -- should route to default (anthropic)
	resp, err := client.Complete(ctx, Request{
		Messages: []Message{UserMessage("hello")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Provider != "anthropic" {
		t.Errorf("expected provider 'anthropic', got %q", resp.Provider)
	}
	if len(anthropic.getCompleteCalls()) != 1 {
		t.Errorf("expected 1 call to anthropic, got %d", len(anthropic.getCompleteCalls()))
	}
	if len(openai.getCompleteCalls()) != 0 {
		t.Errorf("expected 0 calls to openai, got %d", len(openai.getCompleteCalls()))
	}
}

// TestDefaultProviderFallbackFirstRegistered verifies that when no default provider
// is explicitly set, the first registered provider becomes the default.
func TestDefaultProviderFallbackFirstRegistered(t *testing.T) {
	anthropic := newTestAdapter("anthropic")

	client := NewClient(
		WithProvider("anthropic", anthropic),
	)

	ctx := context.Background()
	resp, err := client.Complete(ctx, Request{
		Messages: []Message{UserMessage("hello")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Provider != "anthropic" {
		t.Errorf("expected provider 'anthropic', got %q", resp.Provider)
	}
}

// TestErrorWhenNoProviderFound verifies that a ConfigurationError is returned
// when no provider can handle the request.
func TestErrorWhenNoProviderFound(t *testing.T) {
	client := NewClient()

	ctx := context.Background()

	_, err := client.Complete(ctx, Request{
		Provider: "nonexistent",
		Messages: []Message{UserMessage("hello")},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var configErr *ConfigurationError
	if !errors.As(err, &configErr) {
		t.Errorf("expected ConfigurationError, got %T: %v", err, err)
	}
}

// TestErrorWhenNoProviderFoundStream verifies that Stream also returns a
// ConfigurationError when no provider is available.
func TestErrorWhenNoProviderFoundStream(t *testing.T) {
	client := NewClient()

	ctx := context.Background()

	_, err := client.Stream(ctx, Request{
		Provider: "nonexistent",
		Messages: []Message{UserMessage("hello")},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var configErr *ConfigurationError
	if !errors.As(err, &configErr) {
		t.Errorf("expected ConfigurationError, got %T: %v", err, err)
	}
}

// TestMiddlewareExecutionOrder verifies that middleware executes in registration
// order for requests and reverse order for responses (onion pattern).
func TestMiddlewareExecutionOrder(t *testing.T) {
	adapter := newTestAdapter("test")
	var order []string

	mw1 := func(ctx context.Context, req Request, next NextFunc) (*Response, error) {
		order = append(order, "mw1-before")
		resp, err := next(ctx, req)
		order = append(order, "mw1-after")
		return resp, err
	}

	mw2 := func(ctx context.Context, req Request, next NextFunc) (*Response, error) {
		order = append(order, "mw2-before")
		resp, err := next(ctx, req)
		order = append(order, "mw2-after")
		return resp, err
	}

	mw3 := func(ctx context.Context, req Request, next NextFunc) (*Response, error) {
		order = append(order, "mw3-before")
		resp, err := next(ctx, req)
		order = append(order, "mw3-after")
		return resp, err
	}

	client := NewClient(
		WithProvider("test", adapter),
		WithDefaultProvider("test"),
		WithMiddleware(mw1, mw2, mw3),
	)

	ctx := context.Background()
	_, err := client.Complete(ctx, Request{
		Messages: []Message{UserMessage("hello")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{
		"mw1-before", "mw2-before", "mw3-before",
		"mw3-after", "mw2-after", "mw1-after",
	}

	if len(order) != len(expected) {
		t.Fatalf("expected %d entries, got %d: %v", len(expected), len(order), order)
	}

	for i, v := range expected {
		if order[i] != v {
			t.Errorf("order[%d] = %q, want %q (full order: %v)", i, order[i], v, order)
		}
	}
}

// TestMiddlewareCanModifyRequest verifies that middleware can modify the request
// before it reaches the provider.
func TestMiddlewareCanModifyRequest(t *testing.T) {
	adapter := newTestAdapter("test")

	injectModel := func(ctx context.Context, req Request, next NextFunc) (*Response, error) {
		req.Model = "injected-model"
		return next(ctx, req)
	}

	client := NewClient(
		WithProvider("test", adapter),
		WithDefaultProvider("test"),
		WithMiddleware(injectModel),
	)

	ctx := context.Background()
	_, err := client.Complete(ctx, Request{
		Messages: []Message{UserMessage("hello")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := adapter.getCompleteCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Model != "injected-model" {
		t.Errorf("expected model 'injected-model', got %q", calls[0].Model)
	}
}

// TestMiddlewareCanModifyResponse verifies that middleware can modify the response
// on the way back out.
func TestMiddlewareCanModifyResponse(t *testing.T) {
	adapter := newTestAdapter("test")

	addWarning := func(ctx context.Context, req Request, next NextFunc) (*Response, error) {
		resp, err := next(ctx, req)
		if err != nil {
			return nil, err
		}
		resp.Warnings = append(resp.Warnings, Warning{Message: "added-by-middleware"})
		return resp, err
	}

	client := NewClient(
		WithProvider("test", adapter),
		WithDefaultProvider("test"),
		WithMiddleware(addWarning),
	)

	ctx := context.Background()
	resp, err := client.Complete(ctx, Request{
		Messages: []Message{UserMessage("hello")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(resp.Warnings))
	}
	if resp.Warnings[0].Message != "added-by-middleware" {
		t.Errorf("expected warning message 'added-by-middleware', got %q", resp.Warnings[0].Message)
	}
}

// TestMiddlewareCanShortCircuit verifies that middleware can return early
// without calling next, short-circuiting the chain.
func TestMiddlewareCanShortCircuit(t *testing.T) {
	adapter := newTestAdapter("test")

	blocker := func(ctx context.Context, req Request, next NextFunc) (*Response, error) {
		return &Response{
			ID:           "blocked",
			Provider:     "middleware",
			Message:      AssistantMessage("blocked by middleware"),
			FinishReason: FinishReason{Reason: FinishStop},
		}, nil
	}

	client := NewClient(
		WithProvider("test", adapter),
		WithDefaultProvider("test"),
		WithMiddleware(blocker),
	)

	ctx := context.Background()
	resp, err := client.Complete(ctx, Request{
		Messages: []Message{UserMessage("hello")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.ID != "blocked" {
		t.Errorf("expected response ID 'blocked', got %q", resp.ID)
	}

	// The adapter should never have been called
	if len(adapter.getCompleteCalls()) != 0 {
		t.Errorf("expected 0 adapter calls, got %d", len(adapter.getCompleteCalls()))
	}
}

// TestMultipleMiddlewareChaining verifies that several middleware functions
// are chained correctly, each seeing the result of the previous.
func TestMultipleMiddlewareChaining(t *testing.T) {
	adapter := newTestAdapter("test")

	// Each middleware appends a metadata key
	makeMeta := func(key, value string) Middleware {
		return func(ctx context.Context, req Request, next NextFunc) (*Response, error) {
			if req.Metadata == nil {
				req.Metadata = make(map[string]string)
			}
			req.Metadata[key] = value
			return next(ctx, req)
		}
	}

	client := NewClient(
		WithProvider("test", adapter),
		WithDefaultProvider("test"),
		WithMiddleware(makeMeta("a", "1"), makeMeta("b", "2"), makeMeta("c", "3")),
	)

	ctx := context.Background()
	_, err := client.Complete(ctx, Request{
		Messages: []Message{UserMessage("hello")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := adapter.getCompleteCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}

	meta := calls[0].Metadata
	if meta["a"] != "1" || meta["b"] != "2" || meta["c"] != "3" {
		t.Errorf("expected metadata {a:1 b:2 c:3}, got %v", meta)
	}
}

// TestRegisterProvider verifies that RegisterProvider adds or replaces providers
// on an existing client.
func TestRegisterProvider(t *testing.T) {
	client := NewClient()

	adapter := newTestAdapter("gemini")
	client.RegisterProvider("gemini", adapter)

	ctx := context.Background()
	resp, err := client.Complete(ctx, Request{
		Provider: "gemini",
		Messages: []Message{UserMessage("hello")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Provider != "gemini" {
		t.Errorf("expected provider 'gemini', got %q", resp.Provider)
	}
}

// TestRegisterProviderReplace verifies that RegisterProvider replaces an existing
// provider with the same name.
func TestRegisterProviderReplace(t *testing.T) {
	original := newTestAdapter("openai")
	original.completeResp.ID = "original"

	replacement := newTestAdapter("openai")
	replacement.completeResp.ID = "replacement"

	client := NewClient(
		WithProvider("openai", original),
		WithDefaultProvider("openai"),
	)

	client.RegisterProvider("openai", replacement)

	ctx := context.Background()
	resp, err := client.Complete(ctx, Request{
		Messages: []Message{UserMessage("hello")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "replacement" {
		t.Errorf("expected response ID 'replacement', got %q", resp.ID)
	}
}

// TestRegisterProviderSetsDefaultIfNone verifies that RegisterProvider sets the
// default provider when there was none before.
func TestRegisterProviderSetsDefaultIfNone(t *testing.T) {
	client := NewClient()
	adapter := newTestAdapter("anthropic")
	client.RegisterProvider("anthropic", adapter)

	ctx := context.Background()
	resp, err := client.Complete(ctx, Request{
		Messages: []Message{UserMessage("hi")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Provider != "anthropic" {
		t.Errorf("expected default to be 'anthropic', got %q", resp.Provider)
	}
}

// TestClientClose verifies that Close closes all registered adapters.
func TestClientClose(t *testing.T) {
	a1 := newTestAdapter("openai")
	a2 := newTestAdapter("anthropic")
	a3 := newTestAdapter("gemini")

	client := NewClient(
		WithProvider("openai", a1),
		WithProvider("anthropic", a2),
		WithProvider("gemini", a3),
	)

	err := client.Close()
	if err != nil {
		t.Fatalf("unexpected error on Close: %v", err)
	}

	if !a1.isClosed() {
		t.Error("expected openai adapter to be closed")
	}
	if !a2.isClosed() {
		t.Error("expected anthropic adapter to be closed")
	}
	if !a3.isClosed() {
		t.Error("expected gemini adapter to be closed")
	}
}

// TestStreamRoutesToCorrectProvider verifies that Stream routes requests properly.
func TestStreamRoutesToCorrectProvider(t *testing.T) {
	adapter := newTestAdapter("anthropic")
	adapter.streamEvents = []StreamEvent{
		{Type: StreamStart},
		{Type: StreamTextDelta, Delta: "hello"},
		{Type: StreamFinish, FinishReason: &FinishReason{Reason: FinishStop}},
	}

	client := NewClient(
		WithProvider("anthropic", adapter),
		WithDefaultProvider("anthropic"),
	)

	ctx := context.Background()
	ch, err := client.Stream(ctx, Request{
		Messages: []Message{UserMessage("hello")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var events []StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}

	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	if events[0].Type != StreamStart {
		t.Errorf("expected StreamStart, got %q", events[0].Type)
	}
	if events[1].Delta != "hello" {
		t.Errorf("expected delta 'hello', got %q", events[1].Delta)
	}
}

// TestStreamErrorFromAdapter verifies that adapter-level errors propagate from Stream.
func TestStreamErrorFromAdapter(t *testing.T) {
	adapter := newTestAdapter("test")
	adapter.streamErr = fmt.Errorf("stream connection failed")

	client := NewClient(
		WithProvider("test", adapter),
		WithDefaultProvider("test"),
	)

	ctx := context.Background()
	_, err := client.Stream(ctx, Request{
		Messages: []Message{UserMessage("hello")},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "stream connection failed" {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestCompleteErrorFromAdapter verifies that adapter-level errors propagate from Complete.
func TestCompleteErrorFromAdapter(t *testing.T) {
	adapter := newTestAdapter("test")
	adapter.completeErr = fmt.Errorf("completion failed")

	client := NewClient(
		WithProvider("test", adapter),
		WithDefaultProvider("test"),
	)

	ctx := context.Background()
	_, err := client.Complete(ctx, Request{
		Messages: []Message{UserMessage("hello")},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "completion failed" {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestSetDefaultClientAndGetDefaultClient verifies the module-level default client
// functionality including set and get.
func TestSetDefaultClientAndGetDefaultClient(t *testing.T) {
	// Reset to known state
	SetDefaultClient(nil)

	adapter := newTestAdapter("test")
	client := NewClient(
		WithProvider("test", adapter),
		WithDefaultProvider("test"),
	)

	SetDefaultClient(client)

	got := GetDefaultClient()
	if got != client {
		t.Error("expected GetDefaultClient to return the client set by SetDefaultClient")
	}

	// Clean up
	SetDefaultClient(nil)
}

// TestGetDefaultClientLazyInit verifies that GetDefaultClient attempts lazy
// initialization from environment when no client is set. Without any API keys
// in the env, it returns nil (since FromEnv would fail).
func TestGetDefaultClientLazyInit(t *testing.T) {
	// Reset state
	SetDefaultClient(nil)

	// Clear all relevant env vars to ensure FromEnv fails
	origOpenAI := os.Getenv("OPENAI_API_KEY")
	origAnthropic := os.Getenv("ANTHROPIC_API_KEY")
	origGemini := os.Getenv("GEMINI_API_KEY")
	os.Unsetenv("OPENAI_API_KEY")
	os.Unsetenv("ANTHROPIC_API_KEY")
	os.Unsetenv("GEMINI_API_KEY")
	defer func() {
		if origOpenAI != "" {
			os.Setenv("OPENAI_API_KEY", origOpenAI)
		}
		if origAnthropic != "" {
			os.Setenv("ANTHROPIC_API_KEY", origAnthropic)
		}
		if origGemini != "" {
			os.Setenv("GEMINI_API_KEY", origGemini)
		}
		SetDefaultClient(nil)
	}()

	got := GetDefaultClient()
	if got != nil {
		t.Error("expected nil when no API keys are set in environment")
	}
}

// TestFromEnvNoKeys verifies that FromEnv returns a ConfigurationError
// when no API keys are present in the environment.
func TestFromEnvNoKeys(t *testing.T) {
	origOpenAI := os.Getenv("OPENAI_API_KEY")
	origAnthropic := os.Getenv("ANTHROPIC_API_KEY")
	origGemini := os.Getenv("GEMINI_API_KEY")
	os.Unsetenv("OPENAI_API_KEY")
	os.Unsetenv("ANTHROPIC_API_KEY")
	os.Unsetenv("GEMINI_API_KEY")
	defer func() {
		if origOpenAI != "" {
			os.Setenv("OPENAI_API_KEY", origOpenAI)
		}
		if origAnthropic != "" {
			os.Setenv("ANTHROPIC_API_KEY", origAnthropic)
		}
		if origGemini != "" {
			os.Setenv("GEMINI_API_KEY", origGemini)
		}
	}()

	_, err := FromEnv()
	if err == nil {
		t.Fatal("expected error from FromEnv with no keys")
	}

	var configErr *ConfigurationError
	if !errors.As(err, &configErr) {
		t.Errorf("expected ConfigurationError, got %T: %v", err, err)
	}
}

// TestFromEnvWithKeys verifies that FromEnv detects API keys and creates a client.
// Since real adapters aren't implemented yet, it uses placeholder adapters.
func TestFromEnvWithKeys(t *testing.T) {
	origOpenAI := os.Getenv("OPENAI_API_KEY")
	origAnthropic := os.Getenv("ANTHROPIC_API_KEY")
	origGemini := os.Getenv("GEMINI_API_KEY")
	os.Unsetenv("OPENAI_API_KEY")
	os.Unsetenv("ANTHROPIC_API_KEY")
	os.Unsetenv("GEMINI_API_KEY")
	defer func() {
		if origOpenAI != "" {
			os.Setenv("OPENAI_API_KEY", origOpenAI)
		} else {
			os.Unsetenv("OPENAI_API_KEY")
		}
		if origAnthropic != "" {
			os.Setenv("ANTHROPIC_API_KEY", origAnthropic)
		} else {
			os.Unsetenv("ANTHROPIC_API_KEY")
		}
		if origGemini != "" {
			os.Setenv("GEMINI_API_KEY", origGemini)
		} else {
			os.Unsetenv("GEMINI_API_KEY")
		}
	}()

	os.Setenv("ANTHROPIC_API_KEY", "test-key-anthropic")
	os.Setenv("OPENAI_API_KEY", "test-key-openai")

	client, err := FromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

// TestNewClientNoOptions verifies that creating a client with no options works
// and produces a valid empty client.
func TestNewClientNoOptions(t *testing.T) {
	client := NewClient()
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

// TestMiddlewareWithNoProviders verifies the error path when middleware is set
// but no provider exists.
func TestMiddlewareWithNoProviders(t *testing.T) {
	called := false
	mw := func(ctx context.Context, req Request, next NextFunc) (*Response, error) {
		called = true
		return next(ctx, req)
	}

	client := NewClient(WithMiddleware(mw))

	ctx := context.Background()
	_, err := client.Complete(ctx, Request{
		Messages: []Message{UserMessage("hello")},
	})
	if err == nil {
		t.Fatal("expected error")
	}

	// Middleware should still have been invoked before the routing failure
	if !called {
		t.Error("expected middleware to be called even when routing fails")
	}
}

// TestContextCancellation verifies that the client respects context cancellation.
func TestContextCancellation(t *testing.T) {
	adapter := newTestAdapter("test")
	adapter.completeErr = nil
	// Override Complete to block until context is done
	blockingAdapter := &blockingTestAdapter{testAdapter: adapter}

	client := NewClient(
		WithProvider("test", blockingAdapter),
		WithDefaultProvider("test"),
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := client.Complete(ctx, Request{
		Messages: []Message{UserMessage("hello")},
	})
	// The adapter sees a cancelled context; the exact error depends on adapter behavior
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

// blockingTestAdapter is a test adapter that checks context cancellation.
type blockingTestAdapter struct {
	*testAdapter
}

func (a *blockingTestAdapter) Complete(ctx context.Context, req Request) (*Response, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		return a.testAdapter.Complete(ctx, req)
	}
}

// TestMiddlewareErrorPropagation verifies that errors from middleware propagate
// correctly without calling further middleware or the adapter.
func TestMiddlewareErrorPropagation(t *testing.T) {
	adapter := newTestAdapter("test")
	innerCalled := false

	errorMw := func(ctx context.Context, req Request, next NextFunc) (*Response, error) {
		return nil, fmt.Errorf("middleware error")
	}

	innerMw := func(ctx context.Context, req Request, next NextFunc) (*Response, error) {
		innerCalled = true
		return next(ctx, req)
	}

	client := NewClient(
		WithProvider("test", adapter),
		WithDefaultProvider("test"),
		WithMiddleware(errorMw, innerMw),
	)

	ctx := context.Background()
	_, err := client.Complete(ctx, Request{
		Messages: []Message{UserMessage("hello")},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "middleware error" {
		t.Errorf("unexpected error: %v", err)
	}
	if innerCalled {
		t.Error("inner middleware should not have been called after error middleware")
	}
	if len(adapter.getCompleteCalls()) != 0 {
		t.Error("adapter should not have been called after middleware error")
	}
}

// TestWithMiddlewareAppends verifies that calling WithMiddleware multiple times
// in the options accumulates middleware.
func TestWithMiddlewareAppends(t *testing.T) {
	adapter := newTestAdapter("test")
	var order []string

	mw1 := func(ctx context.Context, req Request, next NextFunc) (*Response, error) {
		order = append(order, "first")
		return next(ctx, req)
	}
	mw2 := func(ctx context.Context, req Request, next NextFunc) (*Response, error) {
		order = append(order, "second")
		return next(ctx, req)
	}

	client := NewClient(
		WithProvider("test", adapter),
		WithDefaultProvider("test"),
		WithMiddleware(mw1),
		WithMiddleware(mw2),
	)

	ctx := context.Background()
	_, err := client.Complete(ctx, Request{Messages: []Message{UserMessage("hi")}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(order) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(order))
	}
	if order[0] != "first" || order[1] != "second" {
		t.Errorf("expected [first second], got %v", order)
	}
}
