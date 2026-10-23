// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	collector "github.com/eshu-hq/eshu-collector-jira"
	sdk "github.com/eshu-hq/eshu/sdk/go/collector"
)

const testToken = "tok-SECRET-123"

func claim() sdk.Claim {
	now := time.Now().UTC()
	return sdk.Claim{
		ComponentID: collector.ComponentID, InstanceID: "jira-qa", CollectorKind: "jira", SourceSystem: "jira",
		Scope: sdk.Scope{ID: "jira:site:fixture", Kind: "jira_site"}, SourceRunID: "run-1", GenerationID: "generation-1",
		WorkItemID: "work-1", FencingToken: "7", Attempt: 1, Deadline: now.Add(time.Hour), ConfigHandle: "component-config:jira",
	}
}

// fakeJira serves a one-issue site over real HTTP.
func fakeJira(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/rest/api/3/search/jql":
			_, _ = io.WriteString(w, `{"issues":[{"id":"10001","key":"ESHU-1","fields":{"summary":"private","status":{"id":"3","name":"In Progress"},"project":{"id":"10000","key":"ESHU"}}}],"isLast":true}`)
		case strings.HasSuffix(r.URL.Path, "/changelog"):
			_, _ = io.WriteString(w, `{"values":[{"id":"1","items":[{"field":"status","fromString":"To Do","toString":"In Progress"}]}]}`)
		case strings.HasSuffix(r.URL.Path, "/remotelink"):
			_, _ = io.WriteString(w, `[]`)
		case r.URL.Path == "/rest/api/3/issuetype/project":
			_, _ = io.WriteString(w, `[]`)
		default:
			_, _ = io.WriteString(w, `{"values":[],"isLast":true}`)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func requestFor(t *testing.T, baseURL string) string {
	t.Helper()
	raw, err := json.Marshal(collector.Request{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1, Claim: claim(), Contract: collector.Contract(),
		Config: map[string]any{
			"site_id": "fixture.invalid", "base_url": baseURL, "token_env": "JIRA_TOKEN", "email_env": "JIRA_EMAIL", "jql": "project = ESHU",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func getenv(key string) string {
	return map[string]string{"JIRA_TOKEN": testToken, "JIRA_EMAIL": "bot@fixture.invalid"}[key]
}

func TestRunClaimWritesOneValidResultToStdout(t *testing.T) {
	t.Parallel()

	server := fakeJira(t)
	for _, args := range [][]string{nil, {"--sdk-stdio"}} {
		var stdout, stderr bytes.Buffer
		err := run(context.Background(), args, strings.NewReader(requestFor(t, server.URL)), &stdout, &stderr, getenv)
		if err != nil {
			t.Fatalf("run(%v) error = %v", args, err)
		}
		decoder := json.NewDecoder(&stdout)
		decoder.DisallowUnknownFields()
		var result sdk.Result
		if err := decoder.Decode(&result); err != nil {
			t.Fatalf("stdout is not one SDK result: %v", err)
		}
		if decoder.More() {
			t.Fatal("stdout carries more than one JSON value")
		}
		if result.State != sdk.ResultComplete || len(result.Facts) != 2 {
			t.Fatalf("State=%q facts=%d, want complete with 2 (one record, one transition)", result.State, len(result.Facts))
		}
		if _, err := sdk.NewValidator(collector.Contract()).ValidateResult(result); err != nil {
			t.Fatalf("ValidateResult() error = %v", err)
		}
		for _, text := range []string{stdout.String(), stderr.String()} {
			if strings.Contains(text, testToken) || strings.Contains(text, "bot@fixture.invalid") {
				t.Fatalf("output leaks a credential: %s", text)
			}
		}
		if lines := strings.Count(strings.TrimSpace(stderr.String()), "\n"); lines != 0 {
			t.Fatalf("stderr = %q, want exactly one log line", stderr.String())
		}
	}
}

func TestRunClaimReportsConfigFailureAsTerminalResultNotCrash(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	err := run(context.Background(), nil, strings.NewReader(requestFor(t, "http://127.0.0.1:1")), &stdout, &stderr, func(string) string { return "" })
	if err != nil {
		t.Fatalf("run() error = %v; a config failure is a bounded terminal result", err)
	}
	var result sdk.Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.State != sdk.ResultTerminal || result.Statuses[0].FailureClass != collector.FailureInvalidConfig {
		t.Fatalf("result = %+v", result)
	}
}

func TestRunClaimRejectsBadInvocation(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		args  []string
		stdin string
	}{
		"empty stdin":      {nil, ""},
		"garbage stdin":    {nil, "{nope"},
		"extra argument":   {[]string{"serve"}, "{}"},
		"unknown flag":     {[]string{"--nope"}, "{}"},
		"wrong protocol":   {nil, `{"protocol_version":"x"}`},
		"unknown seed arg": {[]string{"seed", "extra"}, ""},
		"seed no dest":     {[]string{"seed"}, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			if err := run(context.Background(), tc.args, strings.NewReader(tc.stdin), &stdout, &stderr, getenv); err == nil {
				t.Fatal("run() error = nil, want failure")
			}
			if stdout.Len() != 0 {
				t.Fatalf("a rejected invocation wrote %q to stdout", stdout.String())
			}
		})
	}
}
