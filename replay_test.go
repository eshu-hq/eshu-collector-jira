// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// replayScenario is one recorded Jira site: the activation config, the
// credential environment, a fixed clock, and the HTTP responses keyed by
// "METHOD path?sortedQuery" (POST search adds "#<nextPageToken>"). The same
// files drive the in-tree collector when the parity goldens are regenerated
// (see testdata/README.md), so this harness keeps no collector-specific logic.
type replayScenario struct {
	Name      string                  `json:"name"`
	Now       time.Time               `json:"now"`
	Env       map[string]string       `json:"env"`
	Config    map[string]any          `json:"config"`
	Responses map[string]replayResult `json:"responses"`
}

type replayResult struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    json.RawMessage   `json:"body"`
}

// replayTransport serves a scenario without opening a socket and records what
// the collector asked for.
type replayTransport struct {
	scenario replayScenario
	mu       sync.Mutex
	calls    []string
	missing  []string
	auth     []string
	bodies   []string
}

func loadScenario(t testing.TB, name string) replayScenario {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "source", name+".json"))
	if err != nil {
		t.Fatalf("read scenario %s: %v", name, err)
	}
	var scenario replayScenario
	if err := json.Unmarshal(raw, &scenario); err != nil {
		t.Fatalf("decode scenario %s: %v", name, err)
	}
	return scenario
}

func (s replayScenario) getenv(key string) string { return s.Env[key] }

func newReplayTransport(scenario replayScenario) *replayTransport {
	return &replayTransport{scenario: scenario}
}

func (r *replayTransport) client() *http.Client {
	return &http.Client{Transport: r}
}

// RoundTrip implements http.RoundTripper.
func (r *replayTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	// A real transport stops on a canceled or expired context; so does this.
	if err := request.Context().Err(); err != nil {
		return nil, err
	}
	var body []byte
	if request.Body != nil {
		body, _ = io.ReadAll(request.Body)
		_ = request.Body.Close()
	}
	key := replayKey(request.Method, request.URL, body)
	r.mu.Lock()
	r.calls = append(r.calls, key)
	r.auth = append(r.auth, request.Header.Get("Authorization"))
	r.bodies = append(r.bodies, string(body))
	entry, ok := r.scenario.Responses[key]
	if !ok {
		r.missing = append(r.missing, key)
	}
	r.mu.Unlock()
	if !ok {
		entry = replayResult{Status: http.StatusNotFound, Body: json.RawMessage(`{"errorMessages":["no replay entry"]}`)}
	}
	header := http.Header{"Content-Type": []string{"application/json"}}
	for name, value := range entry.Headers {
		header.Set(name, value)
	}
	return &http.Response{
		Status:     fmt.Sprintf("%d %s", entry.Status, http.StatusText(entry.Status)),
		StatusCode: entry.Status,
		Header:     header,
		Body:       io.NopCloser(bytes.NewReader(entry.Body)),
		Request:    request,
	}, nil
}

func replayKey(method string, u *url.URL, body []byte) string {
	key := method + " " + u.Path
	if query := u.Query(); len(query) > 0 {
		key += "?" + query.Encode()
	}
	if method == http.MethodPost {
		var decoded struct {
			NextPageToken string `json:"nextPageToken"`
		}
		_ = json.Unmarshal(body, &decoded)
		key += "#" + decoded.NextPageToken
	}
	return key
}

func (r *replayTransport) sortedCalls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := append([]string(nil), r.calls...)
	sort.Strings(out)
	return out
}

func (r *replayTransport) missingKeys() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.missing...)
}

// handlerFor builds a Handler wired to a fresh replay transport and the
// scenario's clock and credentials.
func (s replayScenario) handlerFor() (Handler, *replayTransport, *bytes.Buffer) {
	transport := newReplayTransport(s)
	logs := &bytes.Buffer{}
	return Handler{
		Getenv: s.getenv,
		Now:    func() time.Time { return s.Now },
		NewClient: func(target TargetConfig) (EvidenceClient, error) {
			return NewHTTPClient(HTTPClientConfig{
				BaseURL: target.BaseURL, Email: target.Email, Token: target.Token, Client: transport.client(),
			})
		},
		Log: logs,
	}, transport, logs
}

// requestFor builds the host request for a scenario.
func (s replayScenario) requestFor() Request {
	return Request{
		ProtocolVersion: "collector-sdk/v1alpha1",
		Claim:           testClaim(),
		Contract:        Contract(),
		Config:          s.Config,
	}
}

func mustNotMiss(t testing.TB, transport *replayTransport) {
	t.Helper()
	if missing := transport.missingKeys(); len(missing) > 0 {
		t.Fatalf("scenario has no replay entry for: %s", strings.Join(missing, "; "))
	}
}
