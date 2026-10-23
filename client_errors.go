// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"errors"

	"github.com/eshu-hq/eshu-collector-jira/internal/httpx"
)

// FailureClass is a bounded retry class for Jira provider failures.
type FailureClass = string

const (
	// FailurePermissionHidden marks Jira permission or issue-security denials.
	FailurePermissionHidden FailureClass = string(httpx.FailurePermissionHidden)
	// FailureDeleted marks missing or deleted Jira issues and sites.
	FailureDeleted FailureClass = string(httpx.FailureDeleted)
	// FailureArchived marks archived Jira projects or issues.
	FailureArchived FailureClass = string(httpx.FailureArchived)
	// FailureRateLimited marks Jira rate limiting as retryable.
	FailureRateLimited FailureClass = string(httpx.FailureRateLimited)
	// FailureRetryable marks transient transport or provider failures.
	FailureRetryable FailureClass = string(httpx.FailureRetryable)
	// FailureTerminal marks malformed or otherwise non-retryable failures.
	FailureTerminal FailureClass = string(httpx.FailureTerminal)
)

// JiraError is a bounded provider error. It deliberately omits tokens, URLs,
// and raw response bodies from Error.
type JiraError = httpx.HTTPError

// ProviderFailure is a bounded Jira provider failure.
type ProviderFailure = httpx.ProviderFailure

// ErrArchivedIssue marks Jira archived issue or project responses when the
// provider exposes the state without a distinct status code.
var ErrArchivedIssue = errors.New("jira issue is archived")

// jiraStatusPolicy maps HTTP statuses to the Jira failure vocabulary: auth
// denials are permission-hidden, missing resources are deleted, and 410 is
// archived.
var jiraStatusPolicy = httpx.StatusPolicy{
	AuthDeniedClass: httpx.FailurePermissionHidden,
	NotFoundClass:   httpx.FailureDeleted,
	GoneClass:       httpx.FailureArchived,
}

// InvalidEvidenceError marks provider data that cannot become a fact, such as
// an issue with no id or key. Retrying returns the same data, so the claim
// fails terminal instead of looping.
type InvalidEvidenceError struct {
	Reason string
}

// Error returns the bounded reason without echoing provider content.
func (e InvalidEvidenceError) Error() string {
	return "jira invalid evidence: " + e.Reason
}
