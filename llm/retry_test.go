// ABOUTME: Tests for the retry logic in the unified LLM client SDK.
// ABOUTME: Validates retry policy defaults, delay calculation, shouldRetry logic, and the Retry wrapper.

package llm

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestDefaultRetryPolicy(t *testing.T) {
	p := DefaultRetryPolicy()

	if p.MaxRetries != 2 {
		t.Errorf("MaxRetries = %d, want 2", p.MaxRetries)
	}
	if p.BaseDelay != time.Second {
		t.Errorf("BaseDelay = %v, want 1s", p.BaseDelay)
	}
	if p.MaxDelay != 60*time.Second {
		t.Errorf("MaxDelay = %v, want 60s", p.MaxDelay)
	}
	if p.BackoffMultiplier != 2.0 {
		t.Errorf("BackoffMultiplier = %f, want 2.0", p.BackoffMultiplier)
	}
	if !p.Jitter {
		t.Error("Jitter should be true by default")
	}
	if p.OnRetry != nil {
		t.Error("OnRetry should be nil by default")
	}
}

func TestCalculateDelay(t *testing.T) {
	t.Run("exponential backoff without jitter", func(t *testing.T) {
		p := RetryPolicy{
			BaseDelay:         time.Second,
			MaxDelay:          60 * time.Second,
			BackoffMultiplier: 2.0,
			Jitter:            false,
		}

		tests := []struct {
			attempt   int
			wantDelay time.Duration
		}{
			{0, 1 * time.Second},
			{1, 2 * time.Second},
			{2, 4 * time.Second},
			{3, 8 * time.Second},
			{4, 16 * time.Second},
		}

		for _, tt := range tests {
			delay := p.CalculateDelay(tt.attempt)
			if delay != tt.wantDelay {
				t.Errorf("attempt %d: got %v, want %v", tt.attempt, delay, tt.wantDelay)
			}
		}
	})

	t.Run("respects max delay", func(t *testing.T) {
		p := RetryPolicy{
			BaseDelay:         10 * time.Second,
			MaxDelay:          30 * time.Second,
			BackoffMultiplier: 3.0,
			Jitter:            false,
		}

		// attempt 0: 10s, attempt 1: 30s (3x10), attempt 2: would be 90s but capped at 30s
		delay := p.CalculateDelay(2)
		if delay != 30*time.Second {
			t.Errorf("got %v, want 30s (capped at MaxDelay)", delay)
		}
	})

	t.Run("with jitter delay is bounded", func(t *testing.T) {
		p := RetryPolicy{
			BaseDelay:         time.Second,
			MaxDelay:          60 * time.Second,
			BackoffMultiplier: 2.0,
			Jitter:            true,
		}

		// Run multiple times to check jitter bounds
		for i := 0; i < 100; i++ {
			delay := p.CalculateDelay(0)
			// With jitter, delay should be in [0, baseDelay]
			if delay < 0 || delay > time.Second {
				t.Errorf("attempt 0 with jitter: got %v, want [0, 1s]", delay)
			}
		}

		for i := 0; i < 100; i++ {
			delay := p.CalculateDelay(2)
			// attempt 2: base delay is 4s, jitter should be in [0, 4s]
			if delay < 0 || delay > 4*time.Second {
				t.Errorf("attempt 2 with jitter: got %v, want [0, 4s]", delay)
			}
		}
	})

	t.Run("jitter produces variation", func(t *testing.T) {
		p := RetryPolicy{
			BaseDelay:         time.Second,
			MaxDelay:          60 * time.Second,
			BackoffMultiplier: 2.0,
			Jitter:            true,
		}

		// Collect multiple delays and verify they're not all identical
		seen := make(map[time.Duration]bool)
		for i := 0; i < 50; i++ {
			delay := p.CalculateDelay(1)
			seen[delay] = true
		}
		// With 50 samples and nanosecond precision, we should see variation
		if len(seen) < 2 {
			t.Error("jitter should produce varying delays")
		}
	})
}

func TestShouldRetry(t *testing.T) {
	p := RetryPolicy{
		MaxRetries:        2,
		BaseDelay:         time.Second,
		MaxDelay:          60 * time.Second,
		BackoffMultiplier: 2.0,
	}

	t.Run("retryable error within attempts", func(t *testing.T) {
		err := &RateLimitError{
			ProviderError: ProviderError{
				SDKError: SDKError{Message: "rate limited"},
			},
		}
		if !p.ShouldRetry(err, 0) {
			t.Error("should retry on attempt 0")
		}
		if !p.ShouldRetry(err, 1) {
			t.Error("should retry on attempt 1")
		}
	})

	t.Run("retryable error exceeds max retries", func(t *testing.T) {
		err := &RateLimitError{
			ProviderError: ProviderError{
				SDKError: SDKError{Message: "rate limited"},
			},
		}
		if p.ShouldRetry(err, 2) {
			t.Error("should not retry on attempt 2 (max retries = 2)")
		}
		if p.ShouldRetry(err, 3) {
			t.Error("should not retry on attempt 3")
		}
	})

	t.Run("non-retryable error", func(t *testing.T) {
		err := &AuthenticationError{
			ProviderError: ProviderError{
				SDKError: SDKError{Message: "invalid key"},
			},
		}
		if p.ShouldRetry(err, 0) {
			t.Error("should not retry non-retryable error")
		}
	})

	t.Run("nil error", func(t *testing.T) {
		if p.ShouldRetry(nil, 0) {
			t.Error("should not retry nil error")
		}
	})

	t.Run("non-SDK error is not retryable", func(t *testing.T) {
		err := fmt.Errorf("random error")
		if p.ShouldRetry(err, 0) {
			t.Error("should not retry non-SDK error")
		}
	})

	t.Run("various retryable types", func(t *testing.T) {
		retryableErrors := []error{
			&NetworkError{SDKError: SDKError{Message: "net"}},
			&StreamError{SDKError: SDKError{Message: "stream"}},
			&RequestTimeoutError{SDKError: SDKError{Message: "timeout"}},
			&ServerError{ProviderError: ProviderError{SDKError: SDKError{Message: "500"}}},
		}
		for _, err := range retryableErrors {
			if !p.ShouldRetry(err, 0) {
				t.Errorf("should retry %T", err)
			}
		}
	})

	t.Run("various non-retryable types", func(t *testing.T) {
		nonRetryableErrors := []error{
			&AbortError{SDKError: SDKError{Message: "abort"}},
			&ConfigurationError{SDKError: SDKError{Message: "config"}},
			&InvalidToolCallError{SDKError: SDKError{Message: "invalid tool"}},
			&ContentFilterError{ProviderError: ProviderError{SDKError: SDKError{Message: "filter"}}},
		}
		for _, err := range nonRetryableErrors {
			if p.ShouldRetry(err, 0) {
				t.Errorf("should not retry %T", err)
			}
		}
	})
}

func TestRetry(t *testing.T) {
	t.Run("succeeds on first attempt", func(t *testing.T) {
		p := DefaultRetryPolicy()
		var calls int32

		err := Retry(context.Background(), p, func() error {
			atomic.AddInt32(&calls, 1)
			return nil
		})

		if err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
		if atomic.LoadInt32(&calls) != 1 {
			t.Errorf("expected 1 call, got %d", atomic.LoadInt32(&calls))
		}
	})

	t.Run("retries and succeeds", func(t *testing.T) {
		p := RetryPolicy{
			MaxRetries:        3,
			BaseDelay:         time.Millisecond, // fast for testing
			MaxDelay:          10 * time.Millisecond,
			BackoffMultiplier: 2.0,
			Jitter:            false,
		}
		var calls int32

		err := Retry(context.Background(), p, func() error {
			n := atomic.AddInt32(&calls, 1)
			if n < 3 {
				return &NetworkError{SDKError: SDKError{Message: "fail"}}
			}
			return nil
		})

		if err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
		if atomic.LoadInt32(&calls) != 3 {
			t.Errorf("expected 3 calls, got %d", atomic.LoadInt32(&calls))
		}
	})

	t.Run("exhausts retries", func(t *testing.T) {
		p := RetryPolicy{
			MaxRetries:        2,
			BaseDelay:         time.Millisecond,
			MaxDelay:          10 * time.Millisecond,
			BackoffMultiplier: 2.0,
			Jitter:            false,
		}
		var calls int32

		err := Retry(context.Background(), p, func() error {
			atomic.AddInt32(&calls, 1)
			return &ServerError{
				ProviderError: ProviderError{
					SDKError: SDKError{Message: "always fails"},
				},
			}
		})

		if err == nil {
			t.Error("expected error after exhausting retries")
		}
		// Initial call + 2 retries = 3 total calls
		if atomic.LoadInt32(&calls) != 3 {
			t.Errorf("expected 3 calls (1 initial + 2 retries), got %d", atomic.LoadInt32(&calls))
		}

		var serverErr *ServerError
		if !errors.As(err, &serverErr) {
			t.Errorf("expected ServerError, got %T", err)
		}
	})

	t.Run("does not retry non-retryable error", func(t *testing.T) {
		p := RetryPolicy{
			MaxRetries:        3,
			BaseDelay:         time.Millisecond,
			MaxDelay:          10 * time.Millisecond,
			BackoffMultiplier: 2.0,
		}
		var calls int32

		err := Retry(context.Background(), p, func() error {
			atomic.AddInt32(&calls, 1)
			return &AuthenticationError{
				ProviderError: ProviderError{
					SDKError: SDKError{Message: "bad key"},
				},
			}
		})

		if err == nil {
			t.Error("expected error")
		}
		if atomic.LoadInt32(&calls) != 1 {
			t.Errorf("expected 1 call (no retries for non-retryable), got %d", atomic.LoadInt32(&calls))
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		p := RetryPolicy{
			MaxRetries:        10,
			BaseDelay:         100 * time.Millisecond,
			MaxDelay:          time.Second,
			BackoffMultiplier: 2.0,
			Jitter:            false,
		}

		ctx, cancel := context.WithCancel(context.Background())
		var calls int32

		// Cancel context after a short delay
		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		err := Retry(ctx, p, func() error {
			atomic.AddInt32(&calls, 1)
			return &NetworkError{SDKError: SDKError{Message: "net fail"}}
		})

		if err == nil {
			t.Error("expected error due to context cancellation")
		}
		// Should have been interrupted, not all 11 attempts
		if atomic.LoadInt32(&calls) > 5 {
			t.Errorf("expected fewer calls due to cancellation, got %d", atomic.LoadInt32(&calls))
		}
	})

	t.Run("calls OnRetry callback", func(t *testing.T) {
		var retryErrors []error
		var retryAttempts []int
		var retryDelays []time.Duration

		p := RetryPolicy{
			MaxRetries:        2,
			BaseDelay:         time.Millisecond,
			MaxDelay:          10 * time.Millisecond,
			BackoffMultiplier: 2.0,
			Jitter:            false,
			OnRetry: func(err error, attempt int, delay time.Duration) {
				retryErrors = append(retryErrors, err)
				retryAttempts = append(retryAttempts, attempt)
				retryDelays = append(retryDelays, delay)
			},
		}

		_ = Retry(context.Background(), p, func() error {
			return &NetworkError{SDKError: SDKError{Message: "fail"}}
		})

		if len(retryErrors) != 2 {
			t.Fatalf("expected 2 OnRetry calls, got %d", len(retryErrors))
		}
		if retryAttempts[0] != 0 || retryAttempts[1] != 1 {
			t.Errorf("attempts = %v, want [0, 1]", retryAttempts)
		}
		// Without jitter: attempt 0 -> 1ms, attempt 1 -> 2ms
		if retryDelays[0] != time.Millisecond {
			t.Errorf("delay[0] = %v, want 1ms", retryDelays[0])
		}
		if retryDelays[1] != 2*time.Millisecond {
			t.Errorf("delay[1] = %v, want 2ms", retryDelays[1])
		}
	})

	t.Run("respects RetryAfter on RateLimitError", func(t *testing.T) {
		retryAfterSec := 0.01 // 10ms
		p := RetryPolicy{
			MaxRetries:        1,
			BaseDelay:         time.Millisecond,
			MaxDelay:          time.Second,
			BackoffMultiplier: 2.0,
			Jitter:            false,
		}

		var calls int32
		start := time.Now()

		err := Retry(context.Background(), p, func() error {
			n := atomic.AddInt32(&calls, 1)
			if n == 1 {
				return &RateLimitError{
					ProviderError: ProviderError{
						SDKError:   SDKError{Message: "rate limited"},
						RetryAfter: &retryAfterSec,
					},
				}
			}
			return nil
		})

		elapsed := time.Since(start)
		if err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
		if atomic.LoadInt32(&calls) != 2 {
			t.Errorf("expected 2 calls, got %d", atomic.LoadInt32(&calls))
		}
		// Should have waited at least ~10ms for the RetryAfter
		if elapsed < 8*time.Millisecond {
			t.Errorf("expected at least ~10ms delay for RetryAfter, got %v", elapsed)
		}
	})

	t.Run("non-SDK errors are not retried", func(t *testing.T) {
		p := RetryPolicy{
			MaxRetries:        3,
			BaseDelay:         time.Millisecond,
			MaxDelay:          10 * time.Millisecond,
			BackoffMultiplier: 2.0,
		}
		var calls int32

		err := Retry(context.Background(), p, func() error {
			atomic.AddInt32(&calls, 1)
			return fmt.Errorf("plain error")
		})

		if err == nil {
			t.Error("expected error")
		}
		if atomic.LoadInt32(&calls) != 1 {
			t.Errorf("expected 1 call, got %d", atomic.LoadInt32(&calls))
		}
	})
}

func TestRetryPolicyZeroMaxRetries(t *testing.T) {
	p := RetryPolicy{
		MaxRetries:        0,
		BaseDelay:         time.Millisecond,
		MaxDelay:          10 * time.Millisecond,
		BackoffMultiplier: 2.0,
	}
	var calls int32

	err := Retry(context.Background(), p, func() error {
		atomic.AddInt32(&calls, 1)
		return &NetworkError{SDKError: SDKError{Message: "fail"}}
	})

	if err == nil {
		t.Error("expected error")
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("expected 1 call (no retries), got %d", atomic.LoadInt32(&calls))
	}
}
