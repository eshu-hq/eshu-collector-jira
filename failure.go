// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"errors"
	"math"
	"time"

	"github.com/eshu-hq/eshu-collector-jira/internal/httpx"
	sdk "github.com/eshu-hq/eshu/sdk/go/collector"
)

const (
	// FailureInvalidConfig marks an activation config the collector cannot run.
	FailureInvalidConfig = "invalid_config"
	// FailureInvalidEvidence marks provider data that cannot become a fact.
	FailureInvalidEvidence = "invalid_evidence"
	// FailureClaimDeadline marks a claim whose lease deadline has passed.
	FailureClaimDeadline = "claim_deadline_exceeded"
	// FailureRecordBudget marks a claim past its fact-count bound.
	FailureRecordBudget = "record_budget_exceeded"
	// FailurePayloadBudget marks a fact past its payload-size bound.
	FailurePayloadBudget = "payload_budget_exceeded"
	// FailureResultBudget marks an encoded result past its size bound.
	FailureResultBudget = "result_budget_exceeded"

	defaultRetryAfterSeconds          = 30
	defaultRateLimitRetryAfterSeconds = 60
)

// Failure describes one bounded claim failure: the class and operation tokens
// are fixed vocabularies and never carry URLs, bodies, or credentials.
type Failure struct {
	Class      string
	Operation  string
	HTTPStatus int
	Terminal   bool
	RetryAfter time.Duration
	Stage      string
	Stats      CollectionStats
}

// classifyFailure maps a collection error to its bounded failure. The archived
// sentinel keeps the endpoint token a 410 carried, and a partial-collection
// wrapper keeps its stage and counters for the operator log.
func classifyFailure(err error) Failure {
	var invalid InvalidEvidenceError
	if errors.As(err, &invalid) {
		return Failure{Class: FailureInvalidEvidence, Terminal: true}
	}
	provider := classifiedProviderFailure(err)
	failure := Failure{
		Class:      provider.FailureClass(),
		Operation:  provider.FailureOperation(),
		Terminal:   provider.TerminalFailure(),
		RetryAfter: provider.RetryAfterDelay(),
	}
	var httpErr httpx.HTTPError
	if errors.As(provider, &httpErr) && httpErr.StatusCode >= 100 && httpErr.StatusCode <= 599 {
		failure.HTTPStatus = httpErr.StatusCode
	}
	var partial PartialCollectionError
	if errors.As(provider, &partial) {
		failure.Stage = partial.Stage
		failure.Stats = partial.Stats
	}
	return failure
}

func classifiedProviderFailure(err error) ProviderFailure {
	if errors.Is(err, ErrArchivedIssue) {
		failure := httpx.NewProviderFailure("jira", httpx.FailureArchived, true, err)
		// A 410 from a known endpoint wraps the archived sentinel in an
		// HTTPError carrying the endpoint token; keep it on the failure.
		var httpErr httpx.HTTPError
		if errors.As(err, &httpErr) && httpErr.Operation != "" {
			return failure.WithOperation(httpErr.Operation)
		}
		return failure
	}
	return httpx.ClassifyProviderFailure("jira", err, jiraStatusPolicy, httpx.FailureRetryable)
}

// failureResult builds the SDK result for a failed claim: terminal when
// retrying cannot help, retryable otherwise. Neither carries facts.
func failureResult(claim sdk.Claim, observedAt time.Time, failure Failure, latency time.Duration) sdk.Result {
	state := sdk.ResultRetryable
	status := sdk.Status{
		Class:           sdk.StatusFailure,
		FailureClass:    failure.Class,
		SourceLatencyMS: latencyMillis(latency),
	}
	if failure.Terminal {
		state = sdk.ResultTerminal
	} else {
		status.RetryAfterSeconds = retryAfterSeconds(failure)
	}
	return sdk.Result{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		State:           state,
		Claim:           claim,
		Generation:      sdk.Generation{ID: claim.GenerationID, ObservedAt: observedAt.UTC()},
		Statuses:        []sdk.Status{status},
	}
}

// budgetResult is the bounded terminal shape for exhausted budgets and bad
// configuration: an explicit terminal state with a named class.
func budgetResult(claim sdk.Claim, observedAt time.Time, class string) sdk.Result {
	return failureResult(claim, observedAt, Failure{Class: class, Terminal: true}, 0)
}

func retryAfterSeconds(failure Failure) int {
	if failure.RetryAfter > 0 {
		return int(math.Ceil(failure.RetryAfter.Seconds()))
	}
	if failure.Class == FailureRateLimited {
		return defaultRateLimitRetryAfterSeconds
	}
	return defaultRetryAfterSeconds
}

func latencyMillis(latency time.Duration) int {
	if latency <= 0 {
		return 0
	}
	return int(latency / time.Millisecond)
}
