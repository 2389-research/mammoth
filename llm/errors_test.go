// ABOUTME: Tests for the error hierarchy in the unified LLM client SDK.
// ABOUTME: Validates error types, retryability, unwrapping, and HTTP status code mapping.

package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

func TestSDKError(t *testing.T) {
	t.Run("message only", func(t *testing.T) {
		err := &SDKError{Message: "something went wrong"}
		if err.Error() != "something went wrong" {
			t.Errorf("got %q, want %q", err.Error(), "something went wrong")
		}
		if err.IsRetryable() {
			t.Error("SDKError should not be retryable by default")
		}
		if err.Unwrap() != nil {
			t.Error("expected nil cause")
		}
	})

	t.Run("with cause", func(t *testing.T) {
		cause := fmt.Errorf("underlying issue")
		err := &SDKError{Message: "wrapper", Cause: cause}
		if err.Error() != "wrapper: underlying issue" {
			t.Errorf("got %q, want %q", err.Error(), "wrapper: underlying issue")
		}
		if !errors.Is(err, cause) {
			t.Error("errors.Is should find the cause")
		}
	})
}

func TestProviderError(t *testing.T) {
	raw := json.RawMessage(`{"error":"bad request"}`)
	retryAfter := 5.0
	err := &ProviderError{
		SDKError:   SDKError{Message: "provider failed"},
		Provider:   "openai",
		StatusCode: 400,
		ErrorCode:  "invalid_request",
		Retryable:  false,
		RetryAfter: &retryAfter,
		Raw:        raw,
	}

	if err.Provider != "openai" {
		t.Errorf("got provider %q, want %q", err.Provider, "openai")
	}
	if err.StatusCode != 400 {
		t.Errorf("got status %d, want %d", err.StatusCode, 400)
	}
	if err.ErrorCode != "invalid_request" {
		t.Errorf("got error code %q, want %q", err.ErrorCode, "invalid_request")
	}
	if err.IsRetryable() {
		t.Error("should not be retryable")
	}
	if err.RetryAfter == nil || *err.RetryAfter != 5.0 {
		t.Errorf("RetryAfter = %v, want 5.0", err.RetryAfter)
	}
	if string(err.Raw) != `{"error":"bad request"}` {
		t.Errorf("Raw = %s", err.Raw)
	}
}

func TestAuthenticationError(t *testing.T) {
	err := &AuthenticationError{
		ProviderError: ProviderError{
			SDKError:   SDKError{Message: "invalid API key"},
			Provider:   "anthropic",
			StatusCode: 401,
		},
	}

	if err.IsRetryable() {
		t.Error("AuthenticationError should not be retryable")
	}
	if err.StatusCode != 401 {
		t.Errorf("got status %d, want 401", err.StatusCode)
	}

	// errors.As should work
	var provErr *ProviderError
	if !errors.As(err, &provErr) {
		t.Error("errors.As should match ProviderError")
	}
	var sdkErr *SDKError
	if !errors.As(err, &sdkErr) {
		t.Error("errors.As should match SDKError")
	}
}

func TestAccessDeniedError(t *testing.T) {
	err := &AccessDeniedError{
		ProviderError: ProviderError{
			SDKError:   SDKError{Message: "access denied"},
			Provider:   "openai",
			StatusCode: 403,
		},
	}
	if err.IsRetryable() {
		t.Error("AccessDeniedError should not be retryable")
	}
	if err.StatusCode != 403 {
		t.Errorf("got status %d, want 403", err.StatusCode)
	}
}

func TestNotFoundError(t *testing.T) {
	err := &NotFoundError{
		ProviderError: ProviderError{
			SDKError:   SDKError{Message: "model not found"},
			Provider:   "openai",
			StatusCode: 404,
		},
	}
	if err.IsRetryable() {
		t.Error("NotFoundError should not be retryable")
	}
}

func TestInvalidRequestError(t *testing.T) {
	t.Run("status 400", func(t *testing.T) {
		err := &InvalidRequestError{
			ProviderError: ProviderError{
				SDKError:   SDKError{Message: "bad request"},
				StatusCode: 400,
			},
		}
		if err.IsRetryable() {
			t.Error("InvalidRequestError should not be retryable")
		}
	})

	t.Run("status 422", func(t *testing.T) {
		err := &InvalidRequestError{
			ProviderError: ProviderError{
				SDKError:   SDKError{Message: "unprocessable"},
				StatusCode: 422,
			},
		}
		if err.IsRetryable() {
			t.Error("InvalidRequestError should not be retryable")
		}
	})
}

func TestRateLimitError(t *testing.T) {
	retryAfter := 30.0
	err := &RateLimitError{
		ProviderError: ProviderError{
			SDKError:   SDKError{Message: "rate limited"},
			Provider:   "openai",
			StatusCode: 429,
			RetryAfter: &retryAfter,
		},
	}
	if !err.IsRetryable() {
		t.Error("RateLimitError should be retryable")
	}
	if err.RetryAfter == nil || *err.RetryAfter != 30.0 {
		t.Errorf("RetryAfter = %v, want 30.0", err.RetryAfter)
	}
}

func TestServerError(t *testing.T) {
	tests := []struct {
		name   string
		status int
	}{
		{"500", 500},
		{"502", 502},
		{"503", 503},
		{"504", 504},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &ServerError{
				ProviderError: ProviderError{
					SDKError:   SDKError{Message: "server error"},
					StatusCode: tt.status,
				},
			}
			if !err.IsRetryable() {
				t.Error("ServerError should be retryable")
			}
		})
	}
}

func TestContentFilterError(t *testing.T) {
	err := &ContentFilterError{
		ProviderError: ProviderError{
			SDKError: SDKError{Message: "content filtered"},
		},
	}
	if err.IsRetryable() {
		t.Error("ContentFilterError should not be retryable")
	}
}

func TestContextLengthError(t *testing.T) {
	err := &ContextLengthError{
		ProviderError: ProviderError{
			SDKError:   SDKError{Message: "context too long"},
			StatusCode: 413,
		},
	}
	if err.IsRetryable() {
		t.Error("ContextLengthError should not be retryable")
	}
}

func TestQuotaExceededError(t *testing.T) {
	err := &QuotaExceededError{
		ProviderError: ProviderError{
			SDKError: SDKError{Message: "quota exceeded"},
		},
	}
	if err.IsRetryable() {
		t.Error("QuotaExceededError should not be retryable")
	}
}

func TestRequestTimeoutError(t *testing.T) {
	err := &RequestTimeoutError{
		SDKError: SDKError{Message: "request timed out"},
	}
	if !err.IsRetryable() {
		t.Error("RequestTimeoutError should be retryable")
	}

	var sdkErr *SDKError
	if !errors.As(err, &sdkErr) {
		t.Error("errors.As should match SDKError")
	}
}

func TestAbortError(t *testing.T) {
	err := &AbortError{
		SDKError: SDKError{Message: "operation aborted"},
	}
	if err.IsRetryable() {
		t.Error("AbortError should not be retryable")
	}
}

func TestNetworkError(t *testing.T) {
	cause := fmt.Errorf("connection refused")
	err := &NetworkError{
		SDKError: SDKError{Message: "network failure", Cause: cause},
	}
	if !err.IsRetryable() {
		t.Error("NetworkError should be retryable")
	}
	if !errors.Is(err, cause) {
		t.Error("errors.Is should find network cause")
	}
}

func TestStreamError(t *testing.T) {
	err := &StreamError{
		SDKError: SDKError{Message: "stream interrupted"},
	}
	if !err.IsRetryable() {
		t.Error("StreamError should be retryable")
	}
}

func TestInvalidToolCallError(t *testing.T) {
	err := &InvalidToolCallError{
		SDKError: SDKError{Message: "invalid tool call arguments"},
	}
	if err.IsRetryable() {
		t.Error("InvalidToolCallError should not be retryable")
	}
}

func TestNoObjectGeneratedError(t *testing.T) {
	err := &NoObjectGeneratedError{
		SDKError: SDKError{Message: "no object generated"},
	}
	if err.IsRetryable() {
		t.Error("NoObjectGeneratedError should not be retryable")
	}
}

func TestConfigurationError(t *testing.T) {
	err := &ConfigurationError{
		SDKError: SDKError{Message: "missing API key"},
	}
	if err.IsRetryable() {
		t.Error("ConfigurationError should not be retryable")
	}
}

func TestErrorsAsHierarchy(t *testing.T) {
	// A deeply nested error should be matchable at every level of the hierarchy
	authErr := &AuthenticationError{
		ProviderError: ProviderError{
			SDKError:   SDKError{Message: "invalid key"},
			Provider:   "anthropic",
			StatusCode: 401,
		},
	}

	// Should match AuthenticationError
	var auth *AuthenticationError
	if !errors.As(authErr, &auth) {
		t.Error("should match AuthenticationError")
	}

	// Should match ProviderError
	var prov *ProviderError
	if !errors.As(authErr, &prov) {
		t.Error("should match ProviderError")
	}

	// Should match SDKError
	var sdk *SDKError
	if !errors.As(authErr, &sdk) {
		t.Error("should match SDKError")
	}

	// Should NOT match unrelated types
	var netErr *NetworkError
	if errors.As(authErr, &netErr) {
		t.Error("should not match NetworkError")
	}
}

func TestErrorsIsWithWrappedCause(t *testing.T) {
	sentinel := fmt.Errorf("sentinel error")
	err := &NetworkError{
		SDKError: SDKError{Message: "network issue", Cause: sentinel},
	}

	if !errors.Is(err, sentinel) {
		t.Error("errors.Is should find sentinel through the chain")
	}
}

func TestErrorFromStatusCode(t *testing.T) {
	raw := json.RawMessage(`{"detail":"test"}`)
	retryAfter := 10.0

	tests := []struct {
		name       string
		statusCode int
		wantType   string
		retryable  bool
		retryAfter *float64
	}{
		{"400 -> InvalidRequestError", 400, "InvalidRequestError", false, nil},
		{"401 -> AuthenticationError", 401, "AuthenticationError", false, nil},
		{"403 -> AccessDeniedError", 403, "AccessDeniedError", false, nil},
		{"404 -> NotFoundError", 404, "NotFoundError", false, nil},
		{"408 -> RequestTimeoutError", 408, "RequestTimeoutError", true, nil},
		{"413 -> ContextLengthError", 413, "ContextLengthError", false, nil},
		{"422 -> InvalidRequestError", 422, "InvalidRequestError", false, nil},
		{"429 -> RateLimitError", 429, "RateLimitError", true, &retryAfter},
		{"500 -> ServerError", 500, "ServerError", true, nil},
		{"502 -> ServerError", 502, "ServerError", true, nil},
		{"503 -> ServerError", 503, "ServerError", true, nil},
		{"504 -> ServerError", 504, "ServerError", true, nil},
		{"599 -> ServerError", 599, "ServerError", true, nil},
		{"418 -> ProviderError (unknown)", 418, "ProviderError", true, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ErrorFromStatusCode(tt.statusCode, "test error", "testprovider", "test_code", raw, tt.retryAfter)

			// Verify it's not nil
			if err == nil {
				t.Fatal("expected non-nil error")
			}

			// Verify retryability via the Retryable interface
			type retryable interface {
				IsRetryable() bool
			}
			if r, ok := err.(retryable); ok {
				if r.IsRetryable() != tt.retryable {
					t.Errorf("IsRetryable() = %v, want %v", r.IsRetryable(), tt.retryable)
				}
			} else {
				t.Error("error should implement IsRetryable()")
			}

			// Verify the correct type is returned
			switch tt.wantType {
			case "InvalidRequestError":
				var target *InvalidRequestError
				if !errors.As(err, &target) {
					t.Errorf("expected InvalidRequestError, got %T", err)
				}
			case "AuthenticationError":
				var target *AuthenticationError
				if !errors.As(err, &target) {
					t.Errorf("expected AuthenticationError, got %T", err)
				}
			case "AccessDeniedError":
				var target *AccessDeniedError
				if !errors.As(err, &target) {
					t.Errorf("expected AccessDeniedError, got %T", err)
				}
			case "NotFoundError":
				var target *NotFoundError
				if !errors.As(err, &target) {
					t.Errorf("expected NotFoundError, got %T", err)
				}
			case "RequestTimeoutError":
				var target *RequestTimeoutError
				if !errors.As(err, &target) {
					t.Errorf("expected RequestTimeoutError, got %T", err)
				}
			case "ContextLengthError":
				var target *ContextLengthError
				if !errors.As(err, &target) {
					t.Errorf("expected ContextLengthError, got %T", err)
				}
			case "RateLimitError":
				var target *RateLimitError
				if !errors.As(err, &target) {
					t.Errorf("expected RateLimitError, got %T", err)
				}
			case "ServerError":
				var target *ServerError
				if !errors.As(err, &target) {
					t.Errorf("expected ServerError, got %T", err)
				}
			case "ProviderError":
				var target *ProviderError
				if !errors.As(err, &target) {
					t.Errorf("expected ProviderError, got %T", err)
				}
			}
		})
	}
}

func TestErrorFromStatusCodePreservesFields(t *testing.T) {
	raw := json.RawMessage(`{"info":"detail"}`)
	retryAfter := 15.5

	err := ErrorFromStatusCode(429, "rate limited", "openai", "rate_limit_exceeded", raw, &retryAfter)

	var rateErr *RateLimitError
	if !errors.As(err, &rateErr) {
		t.Fatal("expected RateLimitError")
	}
	if rateErr.Provider != "openai" {
		t.Errorf("Provider = %q, want %q", rateErr.Provider, "openai")
	}
	if rateErr.ErrorCode != "rate_limit_exceeded" {
		t.Errorf("ErrorCode = %q, want %q", rateErr.ErrorCode, "rate_limit_exceeded")
	}
	if string(rateErr.Raw) != `{"info":"detail"}` {
		t.Errorf("Raw = %s", rateErr.Raw)
	}
	if rateErr.RetryAfter == nil || *rateErr.RetryAfter != 15.5 {
		t.Errorf("RetryAfter = %v, want 15.5", rateErr.RetryAfter)
	}
	if rateErr.Error() != "rate limited" {
		t.Errorf("Error() = %q, want %q", rateErr.Error(), "rate limited")
	}
}

func TestErrorMessageFormatting(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantMsg string
	}{
		{
			"SDKError without cause",
			&SDKError{Message: "simple error"},
			"simple error",
		},
		{
			"SDKError with cause",
			&SDKError{Message: "outer", Cause: fmt.Errorf("inner")},
			"outer: inner",
		},
		{
			"ProviderError inherits SDKError message",
			&ProviderError{SDKError: SDKError{Message: "provider issue"}},
			"provider issue",
		},
		{
			"AuthenticationError inherits chain",
			&AuthenticationError{ProviderError: ProviderError{SDKError: SDKError{Message: "auth failed"}}},
			"auth failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Error() != tt.wantMsg {
				t.Errorf("Error() = %q, want %q", tt.err.Error(), tt.wantMsg)
			}
		})
	}
}

func TestAllErrorsImplementErrorInterface(t *testing.T) {
	// Compile-time check that all types implement error
	var _ error = (*SDKError)(nil)
	var _ error = (*ProviderError)(nil)
	var _ error = (*AuthenticationError)(nil)
	var _ error = (*AccessDeniedError)(nil)
	var _ error = (*NotFoundError)(nil)
	var _ error = (*InvalidRequestError)(nil)
	var _ error = (*RateLimitError)(nil)
	var _ error = (*ServerError)(nil)
	var _ error = (*ContentFilterError)(nil)
	var _ error = (*ContextLengthError)(nil)
	var _ error = (*QuotaExceededError)(nil)
	var _ error = (*RequestTimeoutError)(nil)
	var _ error = (*AbortError)(nil)
	var _ error = (*NetworkError)(nil)
	var _ error = (*StreamError)(nil)
	var _ error = (*InvalidToolCallError)(nil)
	var _ error = (*NoObjectGeneratedError)(nil)
	var _ error = (*ConfigurationError)(nil)
}

func TestRetryableInterface(t *testing.T) {
	// Verify all error types have IsRetryable and return the correct value
	tests := []struct {
		name      string
		err       interface{ IsRetryable() bool }
		retryable bool
	}{
		{"SDKError", &SDKError{Message: "test"}, false},
		{"ProviderError retryable=true", &ProviderError{SDKError: SDKError{Message: "test"}, Retryable: true}, true},
		{"ProviderError retryable=false", &ProviderError{SDKError: SDKError{Message: "test"}, Retryable: false}, false},
		{"AuthenticationError", &AuthenticationError{}, false},
		{"AccessDeniedError", &AccessDeniedError{}, false},
		{"NotFoundError", &NotFoundError{}, false},
		{"InvalidRequestError", &InvalidRequestError{}, false},
		{"RateLimitError", &RateLimitError{}, true},
		{"ServerError", &ServerError{}, true},
		{"ContentFilterError", &ContentFilterError{}, false},
		{"ContextLengthError", &ContextLengthError{}, false},
		{"QuotaExceededError", &QuotaExceededError{}, false},
		{"RequestTimeoutError", &RequestTimeoutError{}, true},
		{"AbortError", &AbortError{}, false},
		{"NetworkError", &NetworkError{}, true},
		{"StreamError", &StreamError{}, true},
		{"InvalidToolCallError", &InvalidToolCallError{}, false},
		{"NoObjectGeneratedError", &NoObjectGeneratedError{}, false},
		{"ConfigurationError", &ConfigurationError{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.IsRetryable() != tt.retryable {
				t.Errorf("IsRetryable() = %v, want %v", tt.err.IsRetryable(), tt.retryable)
			}
		})
	}
}
