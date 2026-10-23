// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	sdk "github.com/eshu-hq/eshu/sdk/go/collector"
)

// TestClampedDeadlineTimeoutMargin pins the margin arithmetic: the collector
// stops min(2s, 5% of the time left) before the claim deadline. The host kills
// the process at the deadline, which is the initial lease expiry and is not
// extended by heartbeats.
func TestClampedDeadlineTimeoutMargin(t *testing.T) {
	t.Parallel()

	now := testObservedAt()
	for name, tc := range map[string]struct {
		remaining time.Duration
		want      time.Duration
	}{
		"60s lease takes the 2s cap": {60 * time.Second, 58 * time.Second},
		"10s takes 5 percent":        {10 * time.Second, 9500 * time.Millisecond},
		"1s takes 5 percent":         {time.Second, 950 * time.Millisecond},
		"40s sits at the cap":        {40 * time.Second, 38 * time.Second},
		"5m worker TTL":              {5 * time.Minute, 5*time.Minute - 2*time.Second},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, ok := clampedDeadlineTimeout(now, now.Add(tc.remaining))
			if !ok || got != tc.want {
				t.Fatalf("clampedDeadlineTimeout(%s) = %s, %t, want %s, true", tc.remaining, got, ok, tc.want)
			}
		})
	}
	if got, ok := clampedDeadlineTimeout(now, time.Time{}); ok {
		t.Fatalf("a missing deadline must not bound the claim, got %s", got)
	}
	for _, deadline := range []time.Time{now, now.Add(-time.Second)} {
		if got, ok := clampedDeadlineTimeout(now, deadline); ok {
			t.Fatalf("a passed deadline %s must report no timeout, got %s", deadline, got)
		}
	}
}

// deadlineProbe is an http.RoundTripper that records the deadline of each
// request context and, after passAfter requests, blocks until that context
// ends.
type deadlineProbe struct {
	inner     http.RoundTripper
	passAfter int
	mu        sync.Mutex
	calls     int
	deadlines []time.Time
}

func (p *deadlineProbe) RoundTrip(req *http.Request) (*http.Response, error) {
	p.mu.Lock()
	p.calls++
	call := p.calls
	if deadline, ok := req.Context().Deadline(); ok {
		p.deadlines = append(p.deadlines, deadline)
	}
	p.mu.Unlock()
	if call > p.passAfter {
		<-req.Context().Done()
		return nil, req.Context().Err()
	}
	return p.inner.RoundTrip(req)
}

// probedHandler builds a handler whose HTTP client goes through probe.
func probedHandler(t *testing.T, passAfter int) (Handler, *deadlineProbe, Request) {
	t.Helper()
	scenario := loadScenario(t, "basic")
	handler, transport, _ := scenario.handlerFor()
	probe := &deadlineProbe{inner: transport, passAfter: passAfter}
	handler.NewClient = func(target TargetConfig) (EvidenceClient, error) {
		return NewHTTPClient(HTTPClientConfig{
			BaseURL: target.BaseURL, Email: target.Email, Token: target.Token, Client: &http.Client{Transport: probe},
		})
	}
	return handler, probe, scenario.requestFor()
}

// TestProviderCallsCarryTheClampedDeadline proves the request context expires
// at remaining minus the margin, not at the claim deadline itself.
func TestProviderCallsCarryTheClampedDeadline(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		remaining time.Duration
		margin    time.Duration
	}{
		{"long claim takes the 2s cap", 60 * time.Second, 2 * time.Second},
		{"short claim takes 5 percent", 10 * time.Second, 500 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			handler, probe, request := probedHandler(t, 1<<20)
			request.Claim.Deadline = testObservedAt().Add(tc.remaining)
			before := time.Now()
			result := handler.Handle(t.Context(), request)
			if result.State != sdk.ResultComplete && result.State != sdk.ResultPartial {
				t.Fatalf("State = %q, want a successful collection", result.State)
			}
			if len(probe.deadlines) == 0 {
				t.Fatal("no request carried a context deadline")
			}
			want := before.Add(tc.remaining - tc.margin)
			got := probe.deadlines[0]
			if got.Before(want.Add(-250*time.Millisecond)) || got.After(want.Add(250*time.Millisecond)) {
				t.Fatalf("request deadline = %s from start, want about %s (remaining %s, margin %s)",
					got.Sub(before), want.Sub(before), tc.remaining, tc.margin)
			}
		})
	}
}

// TestADeadlineStopAfterPartialProgressIsRetryableNotPartial proves a stop at
// the claim deadline after some provider calls succeeded ends retryable with
// no facts. It must not degrade to a partial generation that commits the
// incomplete read.
func TestADeadlineStopAfterPartialProgressIsRetryableNotPartial(t *testing.T) {
	t.Parallel()

	handler, probe, request := probedHandler(t, 2)
	request.Claim.Deadline = testObservedAt().Add(600 * time.Millisecond)
	result := handler.Handle(t.Context(), request)
	if probe.calls <= probe.passAfter {
		t.Fatalf("calls = %d, want the probe to block after %d", probe.calls, probe.passAfter)
	}
	if result.State != sdk.ResultRetryable || len(result.Facts) != 0 {
		t.Fatalf("State = %q with %d facts, want retryable with none", result.State, len(result.Facts))
	}
	if len(result.Statuses) != 1 || result.Statuses[0].FailureClass != FailureClaimDeadline ||
		result.Statuses[0].Partial || result.Statuses[0].RetryAfterSeconds < 1 {
		t.Fatalf("Statuses = %+v, want one non-partial %s with retry guidance", result.Statuses, FailureClaimDeadline)
	}
	mustValidate(t, result)
}

// staleSuccessClient returns a full collection after the context ended, as a
// client that swallowed the cancellation would.
type staleSuccessClient struct{ inner EvidenceClient }

func (c staleSuccessClient) CollectWorkItemEvidence(ctx context.Context, target TargetConfig, window CollectionWindow) (CollectionResult, error) {
	result, err := c.inner.CollectWorkItemEvidence(context.WithoutCancel(ctx), target, window)
	<-ctx.Done()
	return result, err
}

// TestAResultFinishedAfterTheDeadlineStopIsDiscarded proves the handler never
// commits a collection that ran into the lease bound as complete, even when
// the client returned no error.
func TestAResultFinishedAfterTheDeadlineStopIsDiscarded(t *testing.T) {
	t.Parallel()

	handler, _, request := probedHandler(t, 1<<20)
	inner := handler.NewClient
	handler.NewClient = func(target TargetConfig) (EvidenceClient, error) {
		client, err := inner(target)
		return staleSuccessClient{inner: client}, err
	}
	request.Claim.Deadline = testObservedAt().Add(400 * time.Millisecond)
	result := handler.Handle(t.Context(), request)
	if result.State != sdk.ResultRetryable || len(result.Facts) != 0 ||
		result.Statuses[0].FailureClass != FailureClaimDeadline {
		t.Fatalf("State = %q with %d facts, want retryable %s with none", result.State, len(result.Facts), FailureClaimDeadline)
	}
}

// TestTheClaimTimeoutIsNotTheLeaseDeadline keeps the two bounds apart: a claim
// timeout shorter than the lease ends as an ordinary retryable failure, not as
// claim_deadline_exceeded.
func TestTheClaimTimeoutIsNotTheLeaseDeadline(t *testing.T) {
	t.Parallel()

	handler, _, request := probedHandler(t, 1)
	request.Config["limits"] = map[string]any{"claimTimeoutSeconds": 1}
	request.Claim.Deadline = testObservedAt().Add(time.Hour)
	result := handler.Handle(t.Context(), request)
	if result.State != sdk.ResultRetryable || result.Statuses[0].FailureClass != FailureRetryable {
		t.Fatalf("State = %q class = %q, want retryable %s", result.State, result.Statuses[0].FailureClass, FailureRetryable)
	}
}
