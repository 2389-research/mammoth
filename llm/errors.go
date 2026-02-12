// ABOUTME: Error hierarchy for the unified LLM client SDK.
// ABOUTME: Defines structured error types for provider errors, network errors, and SDK-level errors with retryability.

package llm

import (
	"encoding/json"
)

// SDKError is the base error type for all errors in the LLM SDK.
// All other error types embed SDKError either directly or transitively.
type SDKError struct {
	Message string
	Cause   error
}

func (e *SDKError) Error() string {
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}

func (e *SDKError) Unwrap() error {
	return e.Cause
}

// IsRetryable returns false for the base SDKError. Subtypes override this.
func (e *SDKError) IsRetryable() bool {
	return false
}

// ProviderError represents an error returned by an LLM provider's API.
// It carries provider-specific metadata including status code, error code, and raw response.
type ProviderError struct {
	SDKError
	Provider   string
	StatusCode int
	ErrorCode  string
	Retryable  bool
	RetryAfter *float64
	Raw        json.RawMessage
}

func (e *ProviderError) Error() string {
	return e.SDKError.Error()
}

func (e *ProviderError) Unwrap() error {
	return e.SDKError.Unwrap()
}

// IsRetryable returns the Retryable flag set on the provider error.
func (e *ProviderError) IsRetryable() bool {
	return e.Retryable
}

// As enables errors.As to match ProviderError and SDKError from a ProviderError.
func (e *ProviderError) As(target any) bool {
	switch t := target.(type) {
	case **SDKError:
		*t = &e.SDKError
		return true
	default:
		return false
	}
}

// AuthenticationError represents a 401 Unauthorized response. Not retryable.
type AuthenticationError struct {
	ProviderError
}

func (e *AuthenticationError) Error() string     { return e.ProviderError.Error() }
func (e *AuthenticationError) Unwrap() error     { return e.ProviderError.Unwrap() }
func (e *AuthenticationError) IsRetryable() bool { return false }

func (e *AuthenticationError) As(target any) bool {
	switch t := target.(type) {
	case **ProviderError:
		*t = &e.ProviderError
		return true
	case **SDKError:
		*t = &e.SDKError
		return true
	default:
		return false
	}
}

// AccessDeniedError represents a 403 Forbidden response. Not retryable.
type AccessDeniedError struct {
	ProviderError
}

func (e *AccessDeniedError) Error() string     { return e.ProviderError.Error() }
func (e *AccessDeniedError) Unwrap() error     { return e.ProviderError.Unwrap() }
func (e *AccessDeniedError) IsRetryable() bool { return false }

func (e *AccessDeniedError) As(target any) bool {
	switch t := target.(type) {
	case **ProviderError:
		*t = &e.ProviderError
		return true
	case **SDKError:
		*t = &e.SDKError
		return true
	default:
		return false
	}
}

// NotFoundError represents a 404 Not Found response. Not retryable.
type NotFoundError struct {
	ProviderError
}

func (e *NotFoundError) Error() string     { return e.ProviderError.Error() }
func (e *NotFoundError) Unwrap() error     { return e.ProviderError.Unwrap() }
func (e *NotFoundError) IsRetryable() bool { return false }

func (e *NotFoundError) As(target any) bool {
	switch t := target.(type) {
	case **ProviderError:
		*t = &e.ProviderError
		return true
	case **SDKError:
		*t = &e.SDKError
		return true
	default:
		return false
	}
}

// InvalidRequestError represents a 400 or 422 response. Not retryable.
type InvalidRequestError struct {
	ProviderError
}

func (e *InvalidRequestError) Error() string     { return e.ProviderError.Error() }
func (e *InvalidRequestError) Unwrap() error     { return e.ProviderError.Unwrap() }
func (e *InvalidRequestError) IsRetryable() bool { return false }

func (e *InvalidRequestError) As(target any) bool {
	switch t := target.(type) {
	case **ProviderError:
		*t = &e.ProviderError
		return true
	case **SDKError:
		*t = &e.SDKError
		return true
	default:
		return false
	}
}

// RateLimitError represents a 429 Too Many Requests response. Retryable.
type RateLimitError struct {
	ProviderError
}

func (e *RateLimitError) Error() string     { return e.ProviderError.Error() }
func (e *RateLimitError) Unwrap() error     { return e.ProviderError.Unwrap() }
func (e *RateLimitError) IsRetryable() bool { return true }

func (e *RateLimitError) As(target any) bool {
	switch t := target.(type) {
	case **ProviderError:
		*t = &e.ProviderError
		return true
	case **SDKError:
		*t = &e.SDKError
		return true
	default:
		return false
	}
}

// ServerError represents a 5xx server error response. Retryable.
type ServerError struct {
	ProviderError
}

func (e *ServerError) Error() string     { return e.ProviderError.Error() }
func (e *ServerError) Unwrap() error     { return e.ProviderError.Unwrap() }
func (e *ServerError) IsRetryable() bool { return true }

func (e *ServerError) As(target any) bool {
	switch t := target.(type) {
	case **ProviderError:
		*t = &e.ProviderError
		return true
	case **SDKError:
		*t = &e.SDKError
		return true
	default:
		return false
	}
}

// ContentFilterError represents a content moderation/filter rejection. Not retryable.
type ContentFilterError struct {
	ProviderError
}

func (e *ContentFilterError) Error() string     { return e.ProviderError.Error() }
func (e *ContentFilterError) Unwrap() error     { return e.ProviderError.Unwrap() }
func (e *ContentFilterError) IsRetryable() bool { return false }

func (e *ContentFilterError) As(target any) bool {
	switch t := target.(type) {
	case **ProviderError:
		*t = &e.ProviderError
		return true
	case **SDKError:
		*t = &e.SDKError
		return true
	default:
		return false
	}
}

// ContextLengthError represents a 413 payload/context too large response. Not retryable.
type ContextLengthError struct {
	ProviderError
}

func (e *ContextLengthError) Error() string     { return e.ProviderError.Error() }
func (e *ContextLengthError) Unwrap() error     { return e.ProviderError.Unwrap() }
func (e *ContextLengthError) IsRetryable() bool { return false }

func (e *ContextLengthError) As(target any) bool {
	switch t := target.(type) {
	case **ProviderError:
		*t = &e.ProviderError
		return true
	case **SDKError:
		*t = &e.SDKError
		return true
	default:
		return false
	}
}

// QuotaExceededError represents a quota exhaustion error. Not retryable.
type QuotaExceededError struct {
	ProviderError
}

func (e *QuotaExceededError) Error() string     { return e.ProviderError.Error() }
func (e *QuotaExceededError) Unwrap() error     { return e.ProviderError.Unwrap() }
func (e *QuotaExceededError) IsRetryable() bool { return false }

func (e *QuotaExceededError) As(target any) bool {
	switch t := target.(type) {
	case **ProviderError:
		*t = &e.ProviderError
		return true
	case **SDKError:
		*t = &e.SDKError
		return true
	default:
		return false
	}
}

// RequestTimeoutError represents a request timeout (408 or client-side). Retryable.
type RequestTimeoutError struct {
	SDKError
}

func (e *RequestTimeoutError) Error() string     { return e.SDKError.Error() }
func (e *RequestTimeoutError) Unwrap() error     { return e.SDKError.Unwrap() }
func (e *RequestTimeoutError) IsRetryable() bool { return true }

func (e *RequestTimeoutError) As(target any) bool {
	switch t := target.(type) {
	case **SDKError:
		*t = &e.SDKError
		return true
	default:
		return false
	}
}

// AbortError represents an intentionally aborted operation. Not retryable.
type AbortError struct {
	SDKError
}

func (e *AbortError) Error() string     { return e.SDKError.Error() }
func (e *AbortError) Unwrap() error     { return e.SDKError.Unwrap() }
func (e *AbortError) IsRetryable() bool { return false }

func (e *AbortError) As(target any) bool {
	switch t := target.(type) {
	case **SDKError:
		*t = &e.SDKError
		return true
	default:
		return false
	}
}

// NetworkError represents a network-level failure (DNS, connection refused, etc.). Retryable.
type NetworkError struct {
	SDKError
}

func (e *NetworkError) Error() string     { return e.SDKError.Error() }
func (e *NetworkError) Unwrap() error     { return e.SDKError.Unwrap() }
func (e *NetworkError) IsRetryable() bool { return true }

func (e *NetworkError) As(target any) bool {
	switch t := target.(type) {
	case **SDKError:
		*t = &e.SDKError
		return true
	default:
		return false
	}
}

// StreamError represents an error during response streaming. Retryable.
type StreamError struct {
	SDKError
}

func (e *StreamError) Error() string     { return e.SDKError.Error() }
func (e *StreamError) Unwrap() error     { return e.SDKError.Unwrap() }
func (e *StreamError) IsRetryable() bool { return true }

func (e *StreamError) As(target any) bool {
	switch t := target.(type) {
	case **SDKError:
		*t = &e.SDKError
		return true
	default:
		return false
	}
}

// InvalidToolCallError represents a malformed or invalid tool call from the model. Not retryable.
type InvalidToolCallError struct {
	SDKError
}

func (e *InvalidToolCallError) Error() string     { return e.SDKError.Error() }
func (e *InvalidToolCallError) Unwrap() error     { return e.SDKError.Unwrap() }
func (e *InvalidToolCallError) IsRetryable() bool { return false }

func (e *InvalidToolCallError) As(target any) bool {
	switch t := target.(type) {
	case **SDKError:
		*t = &e.SDKError
		return true
	default:
		return false
	}
}

// NoObjectGeneratedError represents a failure to generate the expected structured output. Not retryable.
type NoObjectGeneratedError struct {
	SDKError
}

func (e *NoObjectGeneratedError) Error() string     { return e.SDKError.Error() }
func (e *NoObjectGeneratedError) Unwrap() error     { return e.SDKError.Unwrap() }
func (e *NoObjectGeneratedError) IsRetryable() bool { return false }

func (e *NoObjectGeneratedError) As(target any) bool {
	switch t := target.(type) {
	case **SDKError:
		*t = &e.SDKError
		return true
	default:
		return false
	}
}

// ConfigurationError represents an SDK configuration problem (missing API key, etc.). Not retryable.
type ConfigurationError struct {
	SDKError
}

func (e *ConfigurationError) Error() string     { return e.SDKError.Error() }
func (e *ConfigurationError) Unwrap() error     { return e.SDKError.Unwrap() }
func (e *ConfigurationError) IsRetryable() bool { return false }

func (e *ConfigurationError) As(target any) bool {
	switch t := target.(type) {
	case **SDKError:
		*t = &e.SDKError
		return true
	default:
		return false
	}
}

// ErrorFromStatusCode maps an HTTP status code to the appropriate error type.
// For unknown status codes, it returns a ProviderError with Retryable=true as a
// conservative default (unknown errors are assumed transient).
func ErrorFromStatusCode(statusCode int, message, provider, errorCode string, raw json.RawMessage, retryAfter *float64) error {
	base := ProviderError{
		SDKError:   SDKError{Message: message},
		Provider:   provider,
		StatusCode: statusCode,
		ErrorCode:  errorCode,
		Raw:        raw,
		RetryAfter: retryAfter,
	}

	switch {
	case statusCode == 400:
		base.Retryable = false
		return &InvalidRequestError{ProviderError: base}
	case statusCode == 401:
		base.Retryable = false
		return &AuthenticationError{ProviderError: base}
	case statusCode == 403:
		base.Retryable = false
		return &AccessDeniedError{ProviderError: base}
	case statusCode == 404:
		base.Retryable = false
		return &NotFoundError{ProviderError: base}
	case statusCode == 408:
		return &RequestTimeoutError{SDKError: SDKError{Message: message}}
	case statusCode == 413:
		base.Retryable = false
		return &ContextLengthError{ProviderError: base}
	case statusCode == 422:
		base.Retryable = false
		return &InvalidRequestError{ProviderError: base}
	case statusCode == 429:
		base.Retryable = true
		return &RateLimitError{ProviderError: base}
	case statusCode >= 500 && statusCode <= 599:
		base.Retryable = true
		return &ServerError{ProviderError: base}
	default:
		// Unknown status codes are treated as retryable (conservative default)
		base.Retryable = true
		return &base
	}
}
