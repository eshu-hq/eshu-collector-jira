// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultTimeout is the default per-request timeout for the HTTP client.
	DefaultTimeout = 30 * time.Second
	// MaxResponseBytes caps one decoded response body. A body past the cap
	// fails the request instead of being decoded.
	MaxResponseBytes int64 = 32 << 20
	// maxRetryAfter bounds provider retry guidance so a hostile header cannot
	// park a claim for an unbounded time.
	maxRetryAfter = 24 * time.Hour
)

// ErrResponseTooLarge marks a response body that exceeded MaxResponseBytes.
var ErrResponseTooLarge = errors.New("response body exceeds size cap")

// Doer is the subset of http.Client the request helper uses.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// HTTPError carries bounded HTTP provider failure details. Operation names the
// bounded provider endpoint token for the failed call (for example
// "search_issues"). It stays a short [a-z0-9_] identifier and never carries a
// URL path, query string, request-specific ID, or credential.
type HTTPError struct {
	Provider        string
	StatusCode      int
	Message         string
	RetryAfter      time.Duration
	RateLimitReason string
	Operation       string
	Cause           error
}

// Error returns a bounded HTTP failure string without provider response bodies.
func (e HTTPError) Error() string {
	provider := strings.TrimSpace(e.Provider)
	if provider == "" {
		provider = "collector"
	}
	if e.StatusCode != 0 {
		return fmt.Sprintf("%s request failed with status %d", provider, e.StatusCode)
	}
	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = "request failed"
	}
	return provider + " " + message
}

// Unwrap returns the underlying transport, encode, or decode cause.
func (e HTTPError) Unwrap() error {
	return e.Cause
}

// RetryAfterDelay returns provider retry guidance parsed from Retry-After.
func (e HTTPError) RetryAfterDelay() time.Duration {
	return e.RetryAfter
}

// JSONRequest configures one bounded JSON HTTP request. Operation is the
// bounded endpoint token stamped onto every HTTPError the call returns.
// Endpoint stays the raw URL path and is never used as the token.
type JSONRequest struct {
	Provider    string
	Method      string
	BaseURL     *url.URL
	Endpoint    string
	Operation   string
	Query       url.Values
	Body        any
	Out         any
	Client      Doer
	Headers     func(*http.Request)
	StatusError func(*http.Response) error
}

// ParseBaseURL validates a provider base URL without checking credentials.
func ParseBaseURL(provider string, rawURL string) (*url.URL, error) {
	name := strings.TrimSpace(provider)
	base, err := url.Parse(strings.TrimRight(strings.TrimSpace(rawURL), "/"))
	if err != nil {
		// url.Parse errors quote the input, which may carry a credential.
		return nil, fmt.Errorf("parse %s base_url: malformed URL", name)
	}
	if base.Scheme != "http" && base.Scheme != "https" {
		return nil, fmt.Errorf("%s base_url must use http or https", name)
	}
	if base.Host == "" {
		return nil, fmt.Errorf("%s base_url must include scheme and host", name)
	}
	if base.User != nil {
		return nil, fmt.Errorf("%s base_url must not include credentials", name)
	}
	return base, nil
}

// DefaultClient returns the default bounded collector HTTP client.
func DefaultClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &http.Client{Timeout: timeout}
}

// ParseRetryAfter parses a Retry-After seconds or HTTP-date header value. The
// result is clamped to a bounded maximum.
func ParseRetryAfter(raw string, now time.Time) time.Duration {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(trimmed); err == nil && seconds > 0 {
		return clampRetryAfter(time.Duration(seconds) * time.Second)
	}
	if now.IsZero() {
		now = time.Now()
	}
	if value, err := http.ParseTime(trimmed); err == nil {
		if delay := value.Sub(now); delay > 0 {
			return clampRetryAfter(delay)
		}
	}
	return 0
}

func clampRetryAfter(delay time.Duration) time.Duration {
	if delay > maxRetryAfter || delay < 0 {
		return maxRetryAfter
	}
	return delay
}

// ParseRetryAfterHeader parses a Retry-After header against the current time.
func ParseRetryAfterHeader(raw string) time.Duration {
	return ParseRetryAfter(raw, time.Now())
}

// DoJSON executes one bounded JSON request. It never retries: the extension
// host owns retry by releasing the claim.
func DoJSON(ctx context.Context, request JSONRequest) error {
	method := strings.TrimSpace(request.Method)
	if method == "" {
		method = http.MethodGet
	}
	client := request.Client
	if client == nil {
		client = DefaultClient(DefaultTimeout)
	}
	httpRequest, err := buildRequest(ctx, request, method)
	if err != nil {
		return HTTPError{Provider: request.Provider, Message: "build request", Operation: request.Operation, Cause: err}
	}
	response, err := client.Do(httpRequest)
	if err != nil {
		return HTTPError{Provider: request.Provider, Message: "request failed", Operation: request.Operation, Cause: err}
	}
	defer closeBody(response.Body)
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return withRequestOperation(statusError(request, response), request.Operation)
	}
	if request.Out == nil {
		return nil
	}
	limited := &io.LimitedReader{R: response.Body, N: MaxResponseBytes + 1}
	if err := json.NewDecoder(limited).Decode(request.Out); err != nil {
		if limited.N <= 0 {
			err = ErrResponseTooLarge
		}
		return HTTPError{Provider: request.Provider, Message: "decode response", Operation: request.Operation, Cause: err}
	}
	if limited.N <= 0 {
		return HTTPError{Provider: request.Provider, Message: "decode response", Operation: request.Operation, Cause: ErrResponseTooLarge}
	}
	return nil
}

func buildRequest(ctx context.Context, request JSONRequest, method string) (*http.Request, error) {
	if request.BaseURL == nil {
		return nil, fmt.Errorf("base_url is required")
	}
	var body io.Reader
	if request.Body != nil {
		encoded, err := json.Marshal(request.Body)
		if err != nil {
			return nil, fmt.Errorf("encode request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	reqURL := *request.BaseURL
	reqURL.Path = joinURLPath(reqURL.Path, request.Endpoint)
	reqURL.RawQuery = request.Query.Encode()
	httpRequest, err := http.NewRequestWithContext(ctx, method, reqURL.String(), body)
	if err != nil {
		return nil, err
	}
	httpRequest.Header.Set("Accept", "application/json")
	if request.Body != nil {
		httpRequest.Header.Set("Content-Type", "application/json")
	}
	if request.Headers != nil {
		request.Headers(httpRequest)
	}
	return httpRequest, nil
}

func joinURLPath(parts ...string) string {
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.Trim(part, "/"); trimmed != "" {
			clean = append(clean, trimmed)
		}
	}
	if len(clean) == 0 {
		return ""
	}
	return "/" + path.Join(clean...)
}

func statusError(request JSONRequest, response *http.Response) error {
	if request.StatusError != nil {
		if err := request.StatusError(response); err != nil {
			return err
		}
	}
	return HTTPError{
		Provider:        request.Provider,
		StatusCode:      response.StatusCode,
		Message:         http.StatusText(response.StatusCode),
		RetryAfter:      ParseRetryAfterHeader(response.Header.Get("Retry-After")),
		RateLimitReason: strings.TrimSpace(response.Header.Get("RateLimit-Reason")),
		Operation:       request.Operation,
	}
}

// withRequestOperation stamps the request endpoint token onto a status-hook
// error that left Operation empty. A hook-set token wins over the request
// token; wrapped chains pass through untouched.
func withRequestOperation(err error, operation string) error {
	if operation == "" || err == nil {
		return err
	}
	if httpErr, ok := err.(HTTPError); ok && strings.TrimSpace(httpErr.Operation) == "" {
		httpErr.Operation = operation
		return httpErr
	}
	return err
}

func closeBody(body io.Closer) {
	if body != nil {
		_ = body.Close()
	}
}
