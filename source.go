// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	sdk "github.com/eshu-hq/eshu/sdk/go/collector"
)

const (
	// maxDeadlineMargin caps how long before the claim deadline the collector
	// stops starting provider calls.
	maxDeadlineMargin = 2 * time.Second
	// deadlineMarginDivisor makes the margin 5% of the remaining claim time.
	deadlineMarginDivisor = 20
)

// errClaimDeadline is the cancellation cause of a context that ended because
// the claim lease deadline (less its margin) was reached. It tells that stop
// apart from a host cancel and from the configured claim timeout.
var errClaimDeadline = errors.New("claim lease deadline reached")

// ClientFactory builds one Jira evidence client for a validated target.
type ClientFactory func(TargetConfig) (EvidenceClient, error)

// Handler runs one claim. The zero value is usable: it reads the process
// environment, the wall clock, and the real Jira Cloud HTTP client.
type Handler struct {
	// Getenv resolves credential environment variables; nil selects os.Getenv
	// at the call site in cmd/collector.
	Getenv func(string) string
	// Now supplies the clock; nil selects time.Now.
	Now func() time.Time
	// NewClient builds the evidence client; nil selects the HTTP client.
	NewClient ClientFactory
	// Log receives one bounded JSON diagnostic line per claim; nil discards
	// it. The binary points it at stderr, which the extension host's process
	// runner does not surface (it keeps only a byte count), so the line helps
	// a local run and a test, not an operator in production.
	Log io.Writer
}

func (h Handler) now() time.Time {
	if h.Now != nil {
		return h.Now().UTC()
	}
	return time.Now().UTC()
}

func (h Handler) getenv(key string) string {
	if h.Getenv != nil {
		return h.Getenv(key)
	}
	return ""
}

func (h Handler) newClient(target TargetConfig) (EvidenceClient, error) {
	if h.NewClient != nil {
		return h.NewClient(target)
	}
	return NewHTTPClient(HTTPClientConfig{BaseURL: target.BaseURL, Email: target.Email, Token: target.Token})
}

// Handle collects the Jira site named by the claim and returns the SDK result
// the host validates and commits. It never returns facts together with a
// failure state, never lets a credential into a result, and echoes the claim
// unchanged so the host's identity check passes.
func (h Handler) Handle(ctx context.Context, request Request) sdk.Result {
	started := h.now()
	claim := request.Claim
	settings, err := ParseSettings(request.Config, claim.Scope.ID, h.getenv)
	if err != nil {
		// ConfigError text names config keys only and never quotes a
		// configured value, so it is safe for the diagnostic log line.
		h.logConfigFailure(claim, err)
		return budgetResult(claim, started, configFailureClass(err))
	}
	runCtx, cancel, ok := claimContext(ctx, claim, started, settings.Limits)
	defer cancel()
	if !ok {
		h.logFailure(claim, FailureClaimDeadline, "", 0, CollectionStats{})
		return failureResult(claim, started, Failure{Class: FailureClaimDeadline}, 0)
	}
	client, err := h.newClient(settings.Target)
	if err != nil {
		h.logConfigFailure(claim, err)
		return budgetResult(claim, started, FailureInvalidConfig)
	}
	window := CollectionWindow{Since: started.Add(-settings.Target.UpdatedLookback), Until: started}
	collected, err := client.CollectWorkItemEvidence(runCtx, settings.Target, window)
	if err != nil || stoppedAtClaimDeadline(runCtx) {
		failure := Failure{}
		if err != nil {
			failure = classifyFailure(err)
		}
		if stoppedAtClaimDeadline(runCtx) && !failure.Terminal {
			// The lease deadline ended the read. Whatever was collected so far
			// is incomplete, so it is dropped: the claim ends retryable with no
			// facts and the next attempt starts again. A deadline stop is
			// never partial. A terminal provider failure that landed just
			// before the stop keeps its own class.
			failure = Failure{
				Class: FailureClaimDeadline, Operation: failure.Operation,
				HTTPStatus: failure.HTTPStatus, Stage: failure.Stage, Stats: failure.Stats,
			}
		}
		latency := h.now().Sub(started)
		h.logFailure(claim, failure.Class, failure.Operation, failure.HTTPStatus, failure.Stats)
		return failureResult(claim, started, failure, latency)
	}
	collected.Stats.StaleWindows += windowStaleness(window, started, settings.Target.UpdatedLookback)
	return h.completeResult(claim, settings, window, collected, started)
}

// clampedDeadlineTimeout returns how long the collector may work on a claim:
// the time left until deadline minus a safety margin of min(2s, 5% of the time
// left). The host sets the claim deadline to the initial lease expiry (the
// worker's default lease TTL is 60s, ESHU_COMPONENT_COLLECTOR_CLAIM_LEASE_TTL;
// heartbeats do not extend it) and kills the process there, so the collector
// stops starting provider calls a little earlier and ends with a retryable
// result of its own instead of being killed mid-request. It reports false when
// there is no deadline or none left; claimContext handles the latter.
func clampedDeadlineTimeout(now, deadline time.Time) (time.Duration, bool) {
	if deadline.IsZero() {
		return 0, false
	}
	remaining := deadline.Sub(now)
	if remaining <= 0 {
		return 0, false
	}
	margin := min(maxDeadlineMargin, remaining/deadlineMarginDivisor)
	return remaining - margin, true
}

// claimContext bounds the collection by the claim lease deadline (less its
// margin) and the configured claim timeout, whichever is shorter. It works in
// durations measured on the handler clock and applies them with
// context.WithTimeoutCause, so an injected clock never skews the real-time
// bound. A context that ends at the lease bound carries errClaimDeadline as
// its cause. ok is false when the lease is already spent.
func claimContext(
	ctx context.Context,
	claim sdk.Claim,
	now time.Time,
	limits ResourceUse,
) (context.Context, context.CancelFunc, bool) {
	budget := time.Duration(limits.ClaimTimeoutSeconds) * time.Second
	var cause error
	if !claim.Deadline.IsZero() {
		deadline := claim.Deadline.UTC()
		if !deadline.After(now) {
			return ctx, func() {}, false
		}
		if timeout, bounded := clampedDeadlineTimeout(now, deadline); bounded && timeout <= budget {
			budget, cause = timeout, errClaimDeadline
		}
	}
	runCtx, cancel := context.WithTimeoutCause(ctx, budget, cause)
	return runCtx, cancel, true
}

// stoppedAtClaimDeadline reports whether ctx ended because the claim lease
// deadline was reached, as opposed to a host cancel or the claim timeout.
func stoppedAtClaimDeadline(ctx context.Context) bool {
	return errors.Is(context.Cause(ctx), errClaimDeadline)
}

// windowStaleness reports one stale window when the collection window is
// unusable or ended long before the collector started. A collector reading a
// real clock always reports zero; the counter is part of the freshness
// identity so a stale window never skips as unchanged.
func windowStaleness(window CollectionWindow, startedAt time.Time, lookback time.Duration) int {
	if window.Since.IsZero() || window.Until.IsZero() || window.Until.Before(window.Since) {
		return 1
	}
	if lookback > 0 && window.Until.Before(startedAt.UTC().Add(-lookback)) {
		return 1
	}
	return 0
}

func (h Handler) completeResult(
	claim sdk.Claim,
	settings Settings,
	window CollectionWindow,
	collected CollectionResult,
	started time.Time,
) sdk.Result {
	observedAt := collected.ObservedAt.UTC()
	if observedAt.IsZero() {
		observedAt = started
	}
	factCtx := FactContext{
		ScopeID:             claim.Scope.ID,
		GenerationID:        claim.GenerationID,
		CollectorInstanceID: claim.InstanceID,
		SourceSystem:        claim.SourceSystem,
		ObservedAt:          observedAt,
		SourceURI:           settings.Target.BaseURL,
	}
	built, err := buildFacts(factCtx, collected)
	if err != nil {
		h.logFailure(claim, FailureInvalidEvidence, "", 0, collected.Stats)
		return budgetResult(claim, started, FailureInvalidEvidence)
	}
	hint := freshnessHint(window, collected, built)
	facts := dedupeFacts(built)
	if class := checkBudgets(facts, settings.Limits); class != "" {
		h.logFailure(claim, class, "", 0, collected.Stats)
		return budgetResult(claim, started, class)
	}
	result := sdk.Result{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		State:           sdk.ResultComplete,
		Claim:           claim,
		Generation:      sdk.Generation{ID: claim.GenerationID, ObservedAt: observedAt, FreshnessHint: hint},
		Facts:           facts,
		Statuses: []sdk.Status{{
			Class:           sdk.StatusComplete,
			FactCount:       len(facts),
			SourceLatencyMS: latencyMillis(h.now().Sub(started)),
		}},
	}
	if warnings := len(collected.MetadataWarnings); warnings > 0 {
		// Metadata the site hid or does not support is committed as warning
		// facts, so the generation stays complete; the partial state tells
		// the operator that some context is degraded.
		result.State = sdk.ResultPartial
		result.Statuses = append(result.Statuses, sdk.Status{
			Class: sdk.StatusWarning, Partial: true, WarningCount: warnings,
		})
	}
	if encoded, err := json.Marshal(result); err != nil || len(encoded) > settings.Limits.MaxResultBytes {
		h.logFailure(claim, FailureResultBudget, "", 0, collected.Stats)
		return budgetResult(claim, started, FailureResultBudget)
	}
	h.logSuccess(claim, result, collected.Stats)
	return result
}

// checkBudgets returns the failure class of the first exhausted bound, or "".
func checkBudgets(facts []sdk.Fact, limits ResourceUse) string {
	if len(facts) > limits.MaxRecordsPerClaim {
		return FailureRecordBudget
	}
	for _, fact := range facts {
		encoded, err := json.Marshal(fact.Payload)
		if err != nil || len(encoded) > limits.MaxPayloadBytes {
			return FailurePayloadBudget
		}
	}
	return ""
}

// logLine is the bounded diagnostic record written to stderr. The host drops
// it (see Handler.Log), and the worker records only the failure class from the
// status row until eshu-hq/eshu#8908 lands, so today it reaches a local run
// and a test, not an operator. Every field is a counter or a fixed-vocabulary
// token; none carries a URL, a provider body, or a secret.
type logLine struct {
	Event        string          `json:"event"`
	Outcome      string          `json:"outcome"`
	ScopeID      string          `json:"scope_id"`
	GenerationID string          `json:"generation_id"`
	WorkItemID   string          `json:"work_item_id"`
	Attempt      int             `json:"attempt"`
	FailureClass string          `json:"failure_class,omitempty"`
	Detail       string          `json:"detail,omitempty"`
	Operation    string          `json:"operation,omitempty"`
	HTTPStatus   int             `json:"http_status,omitempty"`
	Facts        int             `json:"facts,omitempty"`
	Stats        CollectionStats `json:"stats"`
}

func (h Handler) logFailure(claim sdk.Claim, class string, operation string, status int, stats CollectionStats) {
	h.emit(logLine{
		Event: "jira.claim", Outcome: "failure", ScopeID: claim.Scope.ID, GenerationID: claim.GenerationID,
		WorkItemID: claim.WorkItemID, Attempt: claim.Attempt, FailureClass: class, Operation: operation,
		HTTPStatus: status, Stats: stats,
	})
}

// configFailureClass returns the bounded status class for a rejected config.
func configFailureClass(err error) string {
	var configErr ConfigError
	if errors.As(err, &configErr) {
		return configErr.FailureClass()
	}
	return FailureInvalidConfig
}

// logConfigFailure records a rejected configuration with its bounded reason.
// Only a ConfigError or a client-construction error that never quotes its
// input carries a detail; anything else logs the class alone.
func (h Handler) logConfigFailure(claim sdk.Claim, err error) {
	line := logLine{
		Event: "jira.claim", Outcome: "failure", ScopeID: claim.Scope.ID, GenerationID: claim.GenerationID,
		WorkItemID: claim.WorkItemID, Attempt: claim.Attempt, FailureClass: FailureInvalidConfig,
	}
	var configErr ConfigError
	if errors.As(err, &configErr) {
		line.Detail = configErr.Error()
		line.FailureClass = configErr.FailureClass()
	}
	h.emit(line)
}

func (h Handler) logSuccess(claim sdk.Claim, result sdk.Result, stats CollectionStats) {
	h.emit(logLine{
		Event: "jira.claim", Outcome: string(result.State), ScopeID: claim.Scope.ID, GenerationID: claim.GenerationID,
		WorkItemID: claim.WorkItemID, Attempt: claim.Attempt, Facts: len(result.Facts), Stats: stats,
	})
}

func (h Handler) emit(line logLine) {
	if h.Log == nil {
		return
	}
	encoded, err := json.Marshal(line)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(h.Log, "%s\n", strings.TrimSpace(string(encoded)))
}
