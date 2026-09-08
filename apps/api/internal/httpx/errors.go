package httpx

import (
	"fmt"
	"net/http"
)

// APIError is the single error type crossing the handler boundary. `cause` is
// deliberately unexported so it is never serialised to the client.
type APIError struct {
	Status  int            `json:"-"`
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`

	cause error
}

func (e *APIError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *APIError) Unwrap() error { return e.cause }

func (e *APIError) internal() string {
	if e.cause != nil {
		return e.cause.Error()
	}
	return e.Message
}

// WithDetails attaches field-level context, typically validation failures.
func (e *APIError) WithDetails(details map[string]any) *APIError {
	e.Details = details
	return e
}

// WithCause records the underlying error for logs without exposing it.
func (e *APIError) WithCause(err error) *APIError {
	e.cause = err
	return e
}

func BadRequest(message string) *APIError {
	return &APIError{Status: http.StatusBadRequest, Code: "bad_request", Message: message}
}

func Unauthorized(message string) *APIError {
	if message == "" {
		message = "authentication is required"
	}
	return &APIError{Status: http.StatusUnauthorized, Code: "unauthorized", Message: message}
}

func Forbidden(message string) *APIError {
	if message == "" {
		message = "you do not have access to this resource"
	}
	return &APIError{Status: http.StatusForbidden, Code: "forbidden", Message: message}
}

func NotFound(resource string) *APIError {
	return &APIError{
		Status:  http.StatusNotFound,
		Code:    "not_found",
		Message: fmt.Sprintf("%s was not found", resource),
	}
}

func Conflict(message string) *APIError {
	return &APIError{Status: http.StatusConflict, Code: "conflict", Message: message}
}

func UnprocessableEntity(message string, details map[string]any) *APIError {
	return &APIError{
		Status:  http.StatusUnprocessableEntity,
		Code:    "unprocessable_entity",
		Message: message,
		Details: details,
	}
}

func TooManyRequests(retryAfterSeconds int) *APIError {
	return &APIError{
		Status:  http.StatusTooManyRequests,
		Code:    "rate_limited",
		Message: "too many requests",
		Details: map[string]any{"retryAfterSeconds": retryAfterSeconds},
	}
}

// ServiceUnavailable marks a dependency (R2, Agora, the indexer) as missing or
// down, which is distinct from a bug in this service.
func ServiceUnavailable(dependency string) *APIError {
	return &APIError{
		Status:  http.StatusServiceUnavailable,
		Code:    "service_unavailable",
		Message: fmt.Sprintf("%s is not available", dependency),
	}
}

// Internal wraps an unexpected error with a client-safe message.
func Internal(err error) *APIError {
	return &APIError{
		Status:  http.StatusInternalServerError,
		Code:    "internal_error",
		Message: "an unexpected error occurred",
		cause:   err,
	}
}
