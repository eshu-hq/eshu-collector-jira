// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu-collector-jira/internal/httpx"
	sdk "github.com/eshu-hq/eshu/sdk/go/collector"
)

// TestOversizeProviderResponseEndsTerminal serves a body past the response cap
// over real HTTP and checks the claim ends terminal with no facts, instead of
// being retried (and re-downloaded) forever.
func TestOversizeProviderResponseEndsTerminal(t *testing.T) {
	t.Parallel()

	stub := newJiraStub(t, func(w http.ResponseWriter, _ stubRequest) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"pad":"`)
		chunk := strings.Repeat("x", 1<<20)
		for written := int64(0); written < httpx.MaxResponseBytes+(2<<20); written += int64(len(chunk)) {
			if _, err := io.WriteString(w, chunk); err != nil {
				return
			}
		}
		_, _ = io.WriteString(w, `"}`)
	})
	scenario := loadScenario(t, "basic")
	scenario.Config["base_url"] = stub.URL
	handler := Handler{Getenv: scenario.getenv, Now: time.Now}
	request := scenario.requestFor()
	request.Claim.Deadline = time.Now().Add(time.Hour)
	result := handler.Handle(t.Context(), request)
	if result.State != sdk.ResultTerminal || len(result.Facts) != 0 {
		t.Fatalf("State = %q with %d facts, want terminal with none", result.State, len(result.Facts))
	}
	if class := result.Statuses[0].FailureClass; class != string(httpx.FailureTerminal) {
		t.Fatalf("class = %q, want %q", class, httpx.FailureTerminal)
	}
	mustValidate(t, result)
}
