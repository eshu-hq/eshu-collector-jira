// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package httpx

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// FailureClass is a bounded retry class safe for status rows, telemetry
// dimensions, and logs.
type FailureClass string

const (
	// FailureAuthDenied marks missing, invalid, or unauthorized credentials.
	FailureAuthDenied FailureClass = "auth_denied"
	// FailurePermissionHidden marks source data hidden by provider permissions.
	FailurePermissionHidden FailureClass = "permission_hidden"
	// FailureNotFound marks missing provider resources.
	FailureNotFound FailureClass = "not_found"
	// FailureDeleted marks provider resources that were removed or hidden by absence.
	FailureDeleted FailureClass = "deleted"
	// FailureArchived marks provider resources no longer active but still known.
	FailureArchived FailureClass = "archived"
	// FailureRateLimited marks provider throttling as retryable.
	FailureRateLimited FailureClass = "rate_limited"
	// FailureRetryable marks transient transport or provider failures.
	FailureRetryable FailureClass = "retryable"
	// FailureTerminal marks malformed input or non-retryable provider failures.
	FailureTerminal FailureClass = "terminal"
)

// Classification is the bounded failure decision for a provider status.
type Classification struct {
	Class    FailureClass
	Terminal bool
}

// StatusPolicy maps common HTTP statuses to collector-specific failure
// classes. A blank field selects the generic class.
type StatusPolicy struct {
	AuthDeniedClass FailureClass
	NotFoundClass   FailureClass
	GoneClass       FailureClass
}

// ClassifyStatus returns the bounded failure decision for an HTTP status code.
func (p StatusPolicy) ClassifyStatus(status int) Classification {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return Classification{Class: orDefault(p.AuthDeniedClass, FailureAuthDenied), Terminal: true}
	case http.StatusNotFound:
		return Classification{Class: orDefault(p.NotFoundClass, FailureNotFound), Terminal: true}
	case http.StatusGone:
		return Classification{Class: orDefault(p.GoneClass, FailureTerminal), Terminal: true}
	case http.StatusRequestTimeout, http.StatusConflict, http.StatusTooEarly, http.StatusTooManyRequests:
		return Classification{Class: FailureRateLimited}
	default:
		if status >= http.StatusInternalServerError {
			return Classification{Class: FailureRetryable}
		}
		if status >= http.StatusBadRequest {
			return Classification{Class: FailureTerminal, Terminal: true}
		}
		return Classification{Class: FailureRetryable}
	}
}

func orDefault(class FailureClass, fallback FailureClass) FailureClass {
	if class != "" {
		return class
	}
	return fallback
}

// maxOperationLength bounds the persisted operation token so a caller cannot
// smuggle high-cardinality or unbounded values into status rows.
const maxOperationLength = 64

// ProviderFailure is a bounded source-provider failure. It deliberately omits
// provider response bodies, raw URLs, and credential-bearing request details
// from Error. The optional operation token names the bounded provider step
// that failed (for example "search_issues"); it stays a short identifier and
// never carries a URL, body fragment, or credential.
type ProviderFailure struct {
	provider     string
	operation    string
	failureClass FailureClass
	terminal     bool
	cause        error
}

// NewProviderFailure builds a bounded provider failure.
func NewProviderFailure(provider string, class FailureClass, terminal bool, cause error) ProviderFailure {
	return ProviderFailure{
		provider:     strings.TrimSpace(provider),
		failureClass: class,
		terminal:     terminal,
		cause:        cause,
	}
}

// WithOperation returns a copy carrying a bounded operation token sanitized to
// [a-z0-9_]. Blank input stays blank; non-blank input with no surviving token
// becomes "unknown_operation".
func (f ProviderFailure) WithOperation(operation string) ProviderFailure {
	f.operation = sanitizeOperation(operation)
	return f
}

// Error returns a bounded failure string safe for logs and status rows.
func (f ProviderFailure) Error() string {
	provider := strings.TrimSpace(f.provider)
	if provider == "" {
		provider = "collector"
	}
	return fmt.Sprintf("%s provider failure: %s", provider, f.FailureClass())
}

// Unwrap returns the underlying provider or transport error.
func (f ProviderFailure) Unwrap() error {
	return f.cause
}

// FailureClass returns the bounded provider failure class.
func (f ProviderFailure) FailureClass() string {
	if f.failureClass == "" {
		return string(FailureRetryable)
	}
	return string(f.failureClass)
}

// FailureOperation returns the bounded operation token, or "" when none.
func (f ProviderFailure) FailureOperation() string {
	return f.operation
}

// TerminalFailure reports whether retrying this claim cannot succeed.
func (f ProviderFailure) TerminalFailure() bool {
	return f.terminal
}

// RetryAfterDelay returns provider retry guidance preserved from the cause.
func (f ProviderFailure) RetryAfterDelay() time.Duration {
	var retryAfter interface{ RetryAfterDelay() time.Duration }
	if errors.As(f.cause, &retryAfter) {
		return retryAfter.RetryAfterDelay()
	}
	return 0
}

func sanitizeOperation(operation string) string {
	trimmed := strings.TrimSpace(operation)
	if trimmed == "" {
		return ""
	}
	var builder strings.Builder
	builder.Grow(len(trimmed))
	for _, r := range strings.ToLower(trimmed) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' {
			builder.WriteRune(r)
		} else {
			builder.WriteByte('_')
		}
	}
	token := strings.Trim(builder.String(), "_")
	if len(token) > maxOperationLength {
		token = strings.Trim(token[:maxOperationLength], "_")
	}
	if token == "" {
		return "unknown_operation"
	}
	return token
}

// ClassifyProviderFailure maps HTTP and context failures to ProviderFailure.
// An endpoint token carried by the cause (HTTPError.Operation) is preserved
// onto the returned failure so claim handling can name the failing step.
func ClassifyProviderFailure(provider string, err error, policy StatusPolicy, fallback FailureClass) ProviderFailure {
	var failure ProviderFailure
	if errors.As(err, &failure) {
		return failure
	}
	// A body past MaxResponseBytes is a property of the request, not a
	// transient fault: retrying downloads up to the cap again and fails the
	// same way. It is terminal.
	if errors.Is(err, ErrResponseTooLarge) {
		return withCarriedOperation(NewProviderFailure(provider, FailureTerminal, true, err), err)
	}
	var httpErr HTTPError
	if errors.As(err, &httpErr) {
		classified := policy.ClassifyStatus(httpErr.StatusCode)
		return withCarriedOperation(NewProviderFailure(provider, classified.Class, classified.Terminal, err), err)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return withCarriedOperation(NewProviderFailure(provider, FailureRetryable, false, err), err)
	}
	if fallback == "" {
		fallback = FailureRetryable
	}
	return withCarriedOperation(NewProviderFailure(provider, fallback, fallback == FailureTerminal, err), err)
}

func withCarriedOperation(failure ProviderFailure, err error) ProviderFailure {
	var httpErr HTTPError
	if errors.As(err, &httpErr) && strings.TrimSpace(httpErr.Operation) != "" {
		return failure.WithOperation(httpErr.Operation)
	}
	return failure
}
