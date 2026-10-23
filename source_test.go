// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	sdk "github.com/eshu-hq/eshu/sdk/go/collector"
	"github.com/eshu-hq/eshu/sdk/go/collector/conformance"
)

// runScenario handles one scenario (optionally edited) and returns the result,
// the recorded HTTP traffic, and the operator log.
func runScenario(t *testing.T, name string, edit func(*replayScenario, *Request)) (sdk.Result, *replayTransport, string) {
	t.Helper()
	scenario := loadScenario(t, name)
	request := scenario.requestFor()
	if edit != nil {
		edit(&scenario, &request)
	}
	handler, transport, logs := scenario.handlerFor()
	result := handler.Handle(t.Context(), request)
	return result, transport, logs.String()
}

func mustValidate(t *testing.T, result sdk.Result) sdk.ValidationReport {
	t.Helper()
	report, err := sdk.NewValidator(Contract()).ValidateResult(result)
	if err != nil {
		t.Fatalf("ValidateResult() error = %v", err)
	}
	return report
}

func mustConform(t *testing.T, result sdk.Result) {
	t.Helper()
	if report := conformance.Run(conformanceRequest(loadManifest(t), result)); !report.OK() {
		t.Fatalf("conformance findings = %#v, want passed", report.Findings)
	}
}

func factCounts(result sdk.Result) map[string]int {
	counts := map[string]int{}
	for _, fact := range result.Facts {
		counts[fact.Kind]++
	}
	return counts
}

func TestHandleCompleteResultIsHostValid(t *testing.T) {
	t.Parallel()

	result, transport, _ := runScenario(t, "basic", nil)
	mustNotMiss(t, transport)
	if result.State != sdk.ResultComplete {
		t.Fatalf("State = %q, want complete", result.State)
	}
	if result.Claim != testClaim() {
		t.Fatalf("Claim = %+v, want the host claim echoed unchanged", result.Claim)
	}
	if result.Generation.ID != "generation-1" || !result.Generation.ObservedAt.Equal(testObservedAt()) || result.Generation.FreshnessHint == "" {
		t.Fatalf("Generation = %+v", result.Generation)
	}
	report := mustValidate(t, result)
	if report.FactCount != len(result.Facts) || report.DuplicateCount != 0 {
		t.Fatalf("validation report = %+v for %d facts", report, len(result.Facts))
	}
	mustConform(t, result)
	want := map[string]int{
		FactKindRecord: 3, FactKindTransition: 6, FactKindExternalLink: 4, FactKindProjectMetadata: 2,
		FactKindIssueTypeMetadata: 2, FactKindStatusMetadata: 2, FactKindWorkflowMetadata: 1, FactKindFieldMetadata: 2,
	}
	got := factCounts(result)
	for kind, count := range want {
		if got[kind] != count {
			t.Errorf("%s facts = %d, want %d (all: %v)", kind, got[kind], count, got)
		}
	}
	if len(result.Statuses) != 1 || result.Statuses[0].Class != sdk.StatusComplete || result.Statuses[0].FactCount != len(result.Facts) {
		t.Fatalf("Statuses = %+v", result.Statuses)
	}
	for _, fact := range result.Facts {
		if fact.SourceRef.ScopeID != "jira:site:fixture" || fact.SourceRef.SourceSystem != "jira" || fact.SchemaVersion != SchemaVersion ||
			fact.SourceConfidence != sdk.SourceConfidenceReported || !fact.ObservedAt.Equal(testObservedAt()) {
			t.Fatalf("fact %s has wrong provenance: %+v", fact.Kind, fact)
		}
	}
}

func TestHandleEmitsMetadataBeforeIssuesInInTreeOrder(t *testing.T) {
	t.Parallel()

	result, _, _ := runScenario(t, "basic", nil)
	var order []string
	for _, fact := range result.Facts {
		if len(order) == 0 || order[len(order)-1] != fact.Kind {
			order = append(order, fact.Kind)
		}
	}
	prefix := strings.Join(order[:5], ",")
	if prefix != "work_item.project_metadata,work_item.issue_type_metadata,work_item.status_metadata,work_item.workflow_metadata,work_item.field_metadata" {
		t.Fatalf("metadata order = %s", prefix)
	}
	if order[5] != FactKindRecord {
		t.Fatalf("first non-metadata kind = %s, want %s", order[5], FactKindRecord)
	}
}

func TestHandleDegradedMetadataIsPartialWithWarnings(t *testing.T) {
	t.Parallel()

	result, _, _ := runScenario(t, "degraded", nil)
	if result.State != sdk.ResultPartial {
		t.Fatalf("State = %q, want partial", result.State)
	}
	if got := factCounts(result)[FactKindMetadataWarning]; got != 3 {
		t.Fatalf("metadata warnings = %d, want 3", got)
	}
	var warning *sdk.Status
	for i := range result.Statuses {
		if result.Statuses[i].Class == sdk.StatusWarning {
			warning = &result.Statuses[i]
		}
	}
	if warning == nil || !warning.Partial || warning.WarningCount != 3 {
		t.Fatalf("Statuses = %+v, want a partial warning status with 3 warnings", result.Statuses)
	}
	mustValidate(t, result)
	mustConform(t, result)
	// The degraded run still carries every issue: warnings do not drop evidence.
	if factCounts(result)[FactKindRecord] != 3 {
		t.Fatalf("records = %d, want 3", factCounts(result)[FactKindRecord])
	}
}

func TestHandleFailureStatesCarryNoFacts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		scenario   string
		state      sdk.ResultState
		class      string
		retryAfter int
	}{
		{"failure_unauthorized", sdk.ResultTerminal, "permission_hidden", 0},
		{"failure_forbidden", sdk.ResultTerminal, "permission_hidden", 0},
		{"failure_not_found", sdk.ResultTerminal, "deleted", 0},
		{"failure_gone", sdk.ResultTerminal, "archived", 0},
		{"failure_bad_request", sdk.ResultTerminal, "terminal", 0},
		{"failure_rate_limited", sdk.ResultRetryable, "rate_limited", 120},
		{"failure_server_error", sdk.ResultRetryable, "retryable", 30},
		{"failure_changelog_error", sdk.ResultRetryable, "retryable", 30},
		{"failure_metadata_server", sdk.ResultRetryable, "retryable", 30},
		{"failure_remote_link_gone", sdk.ResultTerminal, "archived", 0},
		{"failure_decode", sdk.ResultRetryable, "retryable", 30},
	}
	for _, tc := range cases {
		t.Run(tc.scenario, func(t *testing.T) {
			t.Parallel()
			result, _, logs := runScenario(t, tc.scenario, nil)
			if result.State != tc.state {
				t.Fatalf("State = %q, want %q", result.State, tc.state)
			}
			if len(result.Facts) != 0 {
				t.Fatalf("failure result carries %d facts; a failed claim must commit nothing", len(result.Facts))
			}
			if len(result.Statuses) != 1 || result.Statuses[0].Class != sdk.StatusFailure ||
				result.Statuses[0].FailureClass != tc.class || result.Statuses[0].RetryAfterSeconds != tc.retryAfter {
				t.Fatalf("Statuses = %+v, want failure %q retry_after=%d", result.Statuses, tc.class, tc.retryAfter)
			}
			mustValidate(t, result)
			if result.Claim != testClaim() || result.Generation.ID != "generation-1" {
				t.Fatalf("failure result must echo the claim: %+v", result.Claim)
			}
			for _, secret := range []string{testToken, "fixture error", "no replay entry", "http://jira.fixture.invalid"} {
				if strings.Contains(logs, secret) {
					t.Fatalf("operator log leaks %q: %s", secret, logs)
				}
			}
		})
	}
}

func TestHandleMetadataPermissionDenialIsAWarningNotAFailure(t *testing.T) {
	t.Parallel()

	result, _, _ := runScenario(t, "failure_metadata_forbidden_projects", nil)
	if result.State != sdk.ResultPartial || factCounts(result)[FactKindMetadataWarning] != 1 {
		t.Fatalf("State=%q warnings=%d, want partial with one warning", result.State, factCounts(result)[FactKindMetadataWarning])
	}
	mustValidate(t, result)
}

func TestHandleFailureOperationReachesTheLogNotTheStatus(t *testing.T) {
	t.Parallel()

	_, _, logs := runScenario(t, "failure_rate_limited", nil)
	var line logLine
	if err := json.Unmarshal([]byte(strings.TrimSpace(logs)), &line); err != nil {
		t.Fatalf("log is not one JSON line: %v\n%s", err, logs)
	}
	if line.Outcome != "failure" || line.FailureClass != "rate_limited" || line.Operation != "search_issues" || line.HTTPStatus != http.StatusTooManyRequests {
		t.Fatalf("log line = %+v", line)
	}
	if line.ScopeID != "jira:site:fixture" || line.GenerationID != "generation-1" || line.WorkItemID != "work-1" {
		t.Fatalf("log line lacks claim identity: %+v", line)
	}
}

func TestHandleConfigErrorsAreTerminalAndSecretFree(t *testing.T) {
	t.Parallel()

	for name, edit := range map[string]func(*replayScenario, *Request){
		"unknown key":      func(_ *replayScenario, r *Request) { r.Config["issue_limt"] = 1 },
		"unresolved token": func(s *replayScenario, _ *Request) { delete(s.Env, "JIRA_TOKEN") },
		"scope mismatch":   func(_ *replayScenario, r *Request) { r.Config["scope_id"] = "someone-else" },
		"credentialed host": func(_ *replayScenario, r *Request) {
			r.Config["base_url"] = "http://user:" + testToken + "@jira.fixture.invalid"
		},
		"trusted deletion": func(_ *replayScenario, r *Request) { r.Config["allow_empty_successor"] = true },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result, transport, logs := runScenario(t, "basic", edit)
			if result.State != sdk.ResultTerminal || len(result.Facts) != 0 ||
				result.Statuses[0].FailureClass != FailureInvalidConfig {
				t.Fatalf("result = %+v, want terminal %s", result, FailureInvalidConfig)
			}
			if len(transport.sortedCalls()) != 0 {
				t.Fatalf("a rejected config must not reach Jira: %v", transport.sortedCalls())
			}
			encoded, _ := json.Marshal(result)
			for _, text := range []string{string(encoded), logs} {
				if strings.Contains(text, testToken) {
					t.Fatalf("output leaks the credential: %s", text)
				}
			}
		})
	}
}

func TestHandleIsIdempotentForTheSameClaim(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"basic", "degraded", "paged", "failure_rate_limited"} {
		first, _, _ := runScenario(t, name, nil)
		second, _, _ := runScenario(t, name, nil)
		a, err := json.Marshal(first)
		if err != nil {
			t.Fatal(err)
		}
		b, err := json.Marshal(second)
		if err != nil {
			t.Fatal(err)
		}
		if string(a) != string(b) {
			t.Fatalf("%s: duplicate delivery produced different bytes", name)
		}
	}
}

func TestHandleDeadlineAndCancellation(t *testing.T) {
	t.Parallel()

	t.Run("lease already spent", func(t *testing.T) {
		t.Parallel()
		for _, deadline := range []time.Time{testObservedAt().Add(-time.Minute), testObservedAt().Add(time.Second)} {
			result, transport, _ := runScenario(t, "basic", func(_ *replayScenario, r *Request) { r.Claim.Deadline = deadline })
			if result.State != sdk.ResultRetryable || result.Statuses[0].FailureClass != FailureClaimDeadline || result.Statuses[0].RetryAfterSeconds < 1 {
				t.Fatalf("deadline %s: result = %+v", deadline, result)
			}
			if len(transport.sortedCalls()) != 0 {
				t.Fatalf("an expired lease must not call Jira: %v", transport.sortedCalls())
			}
			mustValidate(t, result)
		}
	})

	t.Run("canceled before start", func(t *testing.T) {
		t.Parallel()
		scenario := loadScenario(t, "basic")
		handler, _, _ := scenario.handlerFor()
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		result := handler.Handle(ctx, scenario.requestFor())
		if result.State != sdk.ResultRetryable || len(result.Facts) != 0 {
			t.Fatalf("State = %q with %d facts, want retryable with none", result.State, len(result.Facts))
		}
		mustValidate(t, result)
	})

	t.Run("claim timeout bounds a hung site", func(t *testing.T) {
		t.Parallel()
		release := make(chan struct{})
		stub := newJiraStub(t, func(w http.ResponseWriter, _ stubRequest) {
			select {
			case <-release:
			case <-time.After(20 * time.Second):
			}
			writeJSON(w, 200, `{}`)
		})
		t.Cleanup(func() { close(release) })
		scenario := loadScenario(t, "basic")
		scenario.Config["base_url"] = stub.URL
		scenario.Config["limits"] = map[string]any{"claimTimeoutSeconds": 1}
		handler := Handler{Getenv: scenario.getenv, Now: time.Now}
		request := scenario.requestFor()
		request.Claim.Deadline = time.Now().Add(time.Hour)
		started := time.Now()
		result := handler.Handle(t.Context(), request)
		if elapsed := time.Since(started); elapsed > 10*time.Second {
			t.Fatalf("hung site held the claim for %s", elapsed)
		}
		if result.State != sdk.ResultRetryable || result.Statuses[0].FailureClass != FailureRetryable {
			t.Fatalf("result = %+v, want retryable", result)
		}
	})
}

func TestHandleBudgetsFailTerminalWithNamedClasses(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		limits map[string]any
		class  string
	}{
		"record count":  {map[string]any{"maxRecordsPerClaim": 5}, FailureRecordBudget},
		"payload bytes": {map[string]any{"maxPayloadBytes": 64}, FailurePayloadBudget},
		"result bytes":  {map[string]any{"maxResultBytes": 2000}, FailureResultBudget},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result, _, logs := runScenario(t, "basic", func(_ *replayScenario, r *Request) { r.Config["limits"] = tc.limits })
			if result.State != sdk.ResultTerminal || len(result.Facts) != 0 || result.Statuses[0].FailureClass != tc.class {
				t.Fatalf("result = %+v, want terminal %s", result, tc.class)
			}
			mustValidate(t, result)
			if !strings.Contains(logs, tc.class) {
				t.Fatalf("log lacks the budget class: %s", logs)
			}
		})
	}
}

func TestHandleNeverLeaksCredentialsOrProviderText(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"basic", "degraded", "paged", "failure_unauthorized", "failure_rate_limited", "failure_decode"} {
		result, _, logs := runScenario(t, name, nil)
		encoded, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{testToken, "bot@fixture.invalid", "Fix deploy", "secret summary", "Person 1", "acct-1", "token=leak", "token=abc", "api_key"} {
			if strings.Contains(string(encoded), secret) || strings.Contains(logs, secret) {
				t.Fatalf("%s: output leaks %q", name, secret)
			}
		}
	}
}

func TestHandleUsesTheClaimSourceSystem(t *testing.T) {
	t.Parallel()

	result, _, _ := runScenario(t, "basic", func(_ *replayScenario, r *Request) { r.Claim.SourceSystem = "jira-qa-site" })
	mustValidate(t, result)
	for _, fact := range result.Facts {
		if fact.SourceRef.SourceSystem != "jira-qa-site" {
			t.Fatalf("fact %s source_system = %q, want the claim's", fact.Kind, fact.SourceRef.SourceSystem)
		}
	}
}

func TestHandleCollapsesDuplicateStableKeysLastWins(t *testing.T) {
	t.Parallel()

	result, _, _ := runScenario(t, "basic", nil)
	var labels []sdk.Fact
	for _, fact := range result.Facts {
		if fact.Kind == FactKindTransition && fact.Payload["field"] == "labels" {
			labels = append(labels, fact)
		}
	}
	if len(labels) != 1 || labels[0].Payload["from"] != "b" || labels[0].Payload["to"] != "c" {
		t.Fatalf("labels transitions = %+v, want one fact carrying the last item", labels)
	}
	if report := mustValidate(t, result); report.DuplicateCount != 0 {
		t.Fatalf("DuplicateCount = %d; the handler must collapse conflicting duplicates itself", report.DuplicateCount)
	}
}

func TestFreshnessHintTracksContentAndWindowButNotTheRetry(t *testing.T) {
	t.Parallel()

	base, _, _ := runScenario(t, "basic", nil)
	again, _, _ := runScenario(t, "basic", nil)
	if base.Generation.FreshnessHint != again.Generation.FreshnessHint {
		t.Fatal("a byte-identical retry must share the freshness hint so the store can skip it")
	}
	retried, _, _ := runScenario(t, "basic", func(_ *replayScenario, r *Request) {
		r.Claim.Attempt = 2
		r.Claim.FencingToken = "8"
		r.Claim.GenerationID = "generation-2"
	})
	if retried.Generation.FreshnessHint != base.Generation.FreshnessHint {
		t.Fatal("claim attempt, fencing token, and generation id must stay out of the freshness hint")
	}

	flipped, _, _ := runScenario(t, "basic", func(s *replayScenario, _ *Request) {
		entry := s.Responses["POST /rest/api/3/search/jql#tok2"]
		entry.Body = json.RawMessage(strings.Replace(string(entry.Body), `"To Do"`, `"Done"`, 1))
		s.Responses["POST /rest/api/3/search/jql#tok2"] = entry
	})
	if flipped.Generation.FreshnessHint == base.Generation.FreshnessHint {
		t.Fatal("a status flip on an existing issue must change the freshness hint")
	}
	shifted, _, _ := runScenario(t, "basic", func(s *replayScenario, _ *Request) { s.Now = s.Now.Add(time.Hour) })
	if shifted.Generation.FreshnessHint == base.Generation.FreshnessHint {
		t.Fatal("a later collection window is a different observation and must change the hint")
	}
	truncated, _, _ := runScenario(t, "basic", func(_ *replayScenario, r *Request) { r.Config["issue_limit"] = 2 })
	if truncated.Generation.FreshnessHint == base.Generation.FreshnessHint {
		t.Fatal("a capped issue set must not share a hint with a complete one")
	}
}

func TestHandleNeverSkipsAsUnchangedItself(t *testing.T) {
	t.Parallel()

	// The host sends no previous digest, so unchanged detection belongs to the
	// freshness hint the store compares. The handler therefore never returns
	// the unchanged state, whatever the scenario.
	for _, name := range []string{"basic", "degraded", "paged"} {
		result, _, _ := runScenario(t, name, nil)
		if result.State == sdk.ResultUnchanged {
			t.Fatalf("%s: handler returned unchanged without a previous digest", name)
		}
	}
}
