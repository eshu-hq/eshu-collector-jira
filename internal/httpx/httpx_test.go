// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package httpx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var jiraLikePolicy = StatusPolicy{
	AuthDeniedClass: FailurePermissionHidden,
	NotFoundClass:   FailureDeleted,
	GoneClass:       FailureArchived,
}

func TestClassifyStatusFollowsThePolicy(t *testing.T) {
	t.Parallel()

	cases := []struct {
		status   int
		class    FailureClass
		terminal bool
	}{
		{401, FailurePermissionHidden, true}, {403, FailurePermissionHidden, true},
		{404, FailureDeleted, true}, {410, FailureArchived, true},
		{408, FailureRateLimited, false}, {409, FailureRateLimited, false}, {425, FailureRateLimited, false}, {429, FailureRateLimited, false},
		{500, FailureRetryable, false}, {503, FailureRetryable, false},
		{400, FailureTerminal, true}, {422, FailureTerminal, true},
		{302, FailureRetryable, false},
	}
	for _, tc := range cases {
		got := jiraLikePolicy.ClassifyStatus(tc.status)
		if got.Class != tc.class || got.Terminal != tc.terminal {
			t.Errorf("status %d = %+v, want %s terminal=%v", tc.status, got, tc.class, tc.terminal)
		}
	}
	generic := StatusPolicy{}
	if got := generic.ClassifyStatus(401); got.Class != FailureAuthDenied {
		t.Errorf("generic 401 = %+v", got)
	}
	if got := generic.ClassifyStatus(404); got.Class != FailureNotFound {
		t.Errorf("generic 404 = %+v", got)
	}
	if got := generic.ClassifyStatus(410); got.Class != FailureTerminal {
		t.Errorf("generic 410 = %+v", got)
	}
}

func TestClassifyProviderFailure(t *testing.T) {
	t.Parallel()

	rate := HTTPError{Provider: "jira", StatusCode: 429, RetryAfter: 9 * time.Second, Operation: "search_issues"}
	failure := ClassifyProviderFailure("jira", rate, jiraLikePolicy, FailureRetryable)
	if failure.FailureClass() != "rate_limited" || failure.TerminalFailure() || failure.RetryAfterDelay() != 9*time.Second || failure.FailureOperation() != "search_issues" {
		t.Fatalf("rate-limit failure = %+v", failure)
	}
	for name, err := range map[string]error{
		"canceled": context.Canceled, "deadline": fmt.Errorf("wrap: %w", context.DeadlineExceeded),
		"transport": HTTPError{Provider: "jira", Message: "request failed", Cause: errors.New("dial tcp")},
	} {
		got := ClassifyProviderFailure("jira", err, jiraLikePolicy, FailureRetryable)
		if got.FailureClass() != "retryable" || got.TerminalFailure() {
			t.Errorf("%s classified as %s terminal=%v, want retryable", name, got.FailureClass(), got.TerminalFailure())
		}
	}
	if got := ClassifyProviderFailure("jira", errors.New("other"), jiraLikePolicy, FailureTerminal); !got.TerminalFailure() {
		t.Error("a terminal fallback must be terminal")
	}
	already := NewProviderFailure("jira", FailureArchived, true, errors.New("x"))
	if got := ClassifyProviderFailure("jira", fmt.Errorf("wrapped: %w", already), jiraLikePolicy, FailureRetryable); got.FailureClass() != "archived" {
		t.Errorf("an existing ProviderFailure must pass through, got %s", got.FailureClass())
	}
}

func TestProviderFailureTextIsBounded(t *testing.T) {
	t.Parallel()

	cause := HTTPError{Provider: "jira", StatusCode: 401, Message: "bot@fixture.invalid token=abc"}
	failure := NewProviderFailure("jira", FailurePermissionHidden, true, cause)
	for _, text := range []string{failure.Error(), cause.Error(), fmt.Sprint(failure)} {
		if strings.Contains(text, "token") || strings.Contains(text, "bot@") {
			t.Fatalf("error text leaks provider content: %q", text)
		}
	}
	if (ProviderFailure{}).FailureClass() != "retryable" {
		t.Fatal("the zero failure must default to retryable")
	}
}

func TestSanitizeOperationKeepsOnlyABoundedToken(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("a", 200)
	cases := map[string]string{
		"":                              "",
		"  ":                            "",
		"search_issues":                 "search_issues",
		"Read Changelog":                "read_changelog",
		"https://x.invalid/a?token=abc": "https___x_invalid_a_token_abc",
		"///":                           "unknown_operation",
		long:                            strings.Repeat("a", 64),
	}
	for in, want := range cases {
		if got := NewProviderFailure("p", FailureRetryable, false, nil).WithOperation(in).FailureOperation(); got != want {
			t.Errorf("WithOperation(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseRetryAfterIsBounded(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	cases := map[string]time.Duration{
		"":          0,
		"0":         0,
		"-3":        0,
		"abc":       0,
		"15":        15 * time.Second,
		"999999999": 24 * time.Hour,
		now.Add(90 * time.Second).Format(http.TimeFormat): 90 * time.Second,
		now.Add(-time.Hour).Format(http.TimeFormat):       0,
		now.Add(1000 * time.Hour).Format(http.TimeFormat): 24 * time.Hour,
	}
	for in, want := range cases {
		if got := ParseRetryAfter(in, now); got != want {
			t.Errorf("ParseRetryAfter(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestParseBaseURLNeverEchoesTheInput(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"https://user:secret-pw@example.invalid", "ftp://example.invalid", "example.invalid", "https://", "http://[::1"} {
		_, err := ParseBaseURL("jira", raw)
		if err == nil {
			t.Errorf("ParseBaseURL(%q) error = nil", raw)
			continue
		}
		if strings.Contains(err.Error(), "secret-pw") || strings.Contains(err.Error(), raw) {
			t.Errorf("ParseBaseURL(%q) error echoes the input: %v", raw, err)
		}
	}
	if base, err := ParseBaseURL("jira", " https://example.invalid/sub/ "); err != nil || base.String() != "https://example.invalid/sub" {
		t.Errorf("ParseBaseURL() = %v, %v", base, err)
	}
}

func TestDoJSONNeverRetriesAndStampsTheOperation(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "3")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, "secret body")
	}))
	t.Cleanup(server.Close)
	base, _ := url.Parse(server.URL)
	var out map[string]any
	err := DoJSON(t.Context(), JSONRequest{Provider: "jira", BaseURL: base, Endpoint: "/rest/x", Operation: "list_x", Out: &out})
	var httpErr HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("error %T is not an HTTPError", err)
	}
	if calls.Load() != 1 || httpErr.StatusCode != 503 || httpErr.Operation != "list_x" || httpErr.RetryAfter != 3*time.Second {
		t.Fatalf("calls=%d err=%+v", calls.Load(), httpErr)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("error leaks the response body: %v", err)
	}
}

func TestDoJSONCapsTheResponseBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"pad":"`)
		chunk := []byte(strings.Repeat("x", 1<<20))
		for written := int64(0); written < MaxResponseBytes+(2<<20); written += int64(len(chunk)) {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
		_, _ = io.WriteString(w, `"}`)
	}))
	t.Cleanup(server.Close)
	base, _ := url.Parse(server.URL)
	var out map[string]any
	err := DoJSON(t.Context(), JSONRequest{Provider: "jira", BaseURL: base, Endpoint: "/rest/x", Operation: "list_x", Out: &out})
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("error = %v, want ErrResponseTooLarge", err)
	}
	if out != nil {
		t.Fatal("an oversized body must not be partially decoded into the target")
	}
}

func TestDoJSONDecodesWithinTheCap(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/json" || r.URL.Path != "/rest/x" || r.URL.RawQuery != "a=1&b=2" {
			http.Error(w, "bad request shape", http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(server.Close)
	base, _ := url.Parse(server.URL)
	var out struct{ OK bool }
	err := DoJSON(t.Context(), JSONRequest{
		Provider: "jira", BaseURL: base, Endpoint: "rest/x/", Query: url.Values{"b": {"2"}, "a": {"1"}}, Out: &out,
		Headers: func(r *http.Request) { r.Header.Set("X-Test", "1") },
	})
	if err != nil || !out.OK {
		t.Fatalf("DoJSON() error=%v out=%+v", err, out)
	}
}

func TestDoJSONHonorsContext(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { <-release }))
	t.Cleanup(func() { close(release); server.Close() })
	base, _ := url.Parse(server.URL)
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	err := DoJSON(ctx, JSONRequest{Provider: "jira", BaseURL: base, Endpoint: "/rest/x", Operation: "list_x"})
	var httpErr HTTPError
	if !errors.As(err, &httpErr) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want a deadline HTTPError", err)
	}
}
