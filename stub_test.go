// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// jiraStub is a small real HTTP server for protocol-level behavior (headers,
// bodies, hangs). Replayed site fixtures use replayTransport instead.
type jiraStub struct {
	*httptest.Server
	mu       sync.Mutex
	requests []stubRequest
}

type stubRequest struct {
	Method string
	Path   string
	Query  string
	Auth   string
	Body   string
}

func newJiraStub(t *testing.T, handler func(http.ResponseWriter, stubRequest)) *jiraStub {
	t.Helper()
	stub := &jiraStub{}
	stub.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		request := stubRequest{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Auth: r.Header.Get("Authorization"), Body: string(body)}
		stub.mu.Lock()
		stub.requests = append(stub.requests, request)
		stub.mu.Unlock()
		handler(w, request)
	}))
	t.Cleanup(stub.Close)
	return stub
}

func (s *jiraStub) recorded() []stubRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]stubRequest(nil), s.requests...)
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func newTestClient(t *testing.T, baseURL string, email string) HTTPClient {
	t.Helper()
	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: baseURL, Email: email, Token: testToken})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v", err)
	}
	return client
}

func testWindow() CollectionWindow {
	return CollectionWindow{Since: testObservedAt().Add(-48 * time.Hour), Until: testObservedAt()}
}
