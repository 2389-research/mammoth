// ABOUTME: Retry logic with exponential backoff and jitter for the unified LLM client SDK.
// ABOUTME: Provides RetryPolicy configuration and a generic Retry wrapper that respects error retryability.

package llm

import (
	"context"
	"math"
	"math/rand/v2"
	"time"
)

// RetryPolicy configures how retry behavior works for LLM API calls.
type RetryPolicy struct {
	// MaxRetries is the maximum number of retry attempts (not counting the initial call).
	MaxRetries int

	// BaseDelay is the initial delay before the first retry.
	BaseDelay time.Duration

	// MaxDelay is the upper bound on the delay between retries.
	MaxDelay time.Duration

	// BackoffMultiplier controls exponential growth of the delay between retries.
	BackoffMultiplier float64

	// Jitter adds randomness to the delay to avoid thundering herd problems.
	Jitter bool

	// OnRetry is an optional callback invoked before each retry attempt.
	// It receives the error that triggered the retry, the attempt number (0-indexed),
	// and the delay that will be applied before the next attempt.
	OnRetry func(err error, attempt int, delay time.Duration)
}

// DefaultRetryPolicy returns a RetryPolicy with sensible defaults:
// 2 retries, 1s base delay, 60s max delay, 2x backoff, jitter enabled.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxRetries:        2,
		BaseDelay:         time.Second,
		MaxDelay:          60 * time.Second,
		BackoffMultiplier: 2.0,
		Jitter:            true,
	}
}

// CalculateDelay computes the delay for a given retry attempt using exponential backoff.
// When Jitter is enabled, the delay is randomized between 0 and the calculated backoff value.
// The result is always capped at MaxDelay.
func (p RetryPolicy) CalculateDelay(attempt int) time.Duration {
	// Exponential backoff: base * multiplier^attempt
	delayFloat := float64(p.BaseDelay) * math.Pow(p.BackoffMultiplier, float64(attempt))

	// Cap at MaxDelay
	if delayFloat > float64(p.MaxDelay) {
		delayFloat = float64(p.MaxDelay)
	}

	delay := time.Duration(delayFloat)

	if p.Jitter {
		// Randomize between 0 and the calculated delay (full jitter)
		delay = time.Duration(rand.Int64N(int64(delay) + 1))
	}

	return delay
}

// ShouldRetry determines whether the operation should be retried based on the error
// and the current attempt number. It returns false for nil errors, non-retryable errors,
// and when the attempt count has reached MaxRetries.
func (p RetryPolicy) ShouldRetry(err error, attempt int) bool {
	if err == nil {
		return false
	}
	if attempt >= p.MaxRetries {
		return false
	}

	// Check if the error implements the IsRetryable interface
	type retryable interface {
		IsRetryable() bool
	}
	if r, ok := err.(retryable); ok {
		return r.IsRetryable()
	}

	// Non-SDK errors are not retried
	return false
}

// Retry executes fn with the given retry policy. It retries on retryable errors up to
// MaxRetries times, using exponential backoff with optional jitter. If the error has a
// RetryAfter hint (e.g., from a RateLimitError), that value is used as the minimum delay.
// The context can be used to cancel retries early.
func Retry(ctx context.Context, policy RetryPolicy, fn func() error) error {
	var lastErr error

	for attempt := 0; ; attempt++ {
		lastErr = fn()
		if lastErr == nil {
			return nil
		}

		if !policy.ShouldRetry(lastErr, attempt) {
			return lastErr
		}

		delay := policy.CalculateDelay(attempt)

		// If the error carries a RetryAfter hint, use it as the minimum delay
		delay = applyRetryAfter(lastErr, delay)

		if policy.OnRetry != nil {
			policy.OnRetry(lastErr, attempt, delay)
		}

		select {
		case <-ctx.Done():
			return lastErr
		case <-time.After(delay):
			// Continue to next attempt
		}
	}
}

// applyRetryAfter checks if the error carries a RetryAfter value and returns the
// greater of the calculated delay and the RetryAfter duration.
func applyRetryAfter(err error, calculatedDelay time.Duration) time.Duration {
	// Check for ProviderError-based types that carry RetryAfter
	if pe, ok := extractProviderError(err); ok && pe.RetryAfter != nil {
		retryAfterDuration := time.Duration(*pe.RetryAfter * float64(time.Second))
		if retryAfterDuration > calculatedDelay {
			return retryAfterDuration
		}
	}

	return calculatedDelay
}

// extractProviderError attempts to extract a ProviderError from the given error.
// It handles both direct ProviderError types and subtypes that embed ProviderError.
func extractProviderError(err error) (*ProviderError, bool) {
	switch e := err.(type) {
	case *RateLimitError:
		return &e.ProviderError, true
	case *ServerError:
		return &e.ProviderError, true
	case *AuthenticationError:
		return &e.ProviderError, true
	case *AccessDeniedError:
		return &e.ProviderError, true
	case *NotFoundError:
		return &e.ProviderError, true
	case *InvalidRequestError:
		return &e.ProviderError, true
	case *ContentFilterError:
		return &e.ProviderError, true
	case *ContextLengthError:
		return &e.ProviderError, true
	case *QuotaExceededError:
		return &e.ProviderError, true
	case *ProviderError:
		return e, true
	default:
		return nil, false
	}
}
