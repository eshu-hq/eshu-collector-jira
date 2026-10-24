// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"encoding/json"
	"strings"
	"testing"

	sdk "github.com/eshu-hq/eshu/sdk/go/collector"
)

// setSearch replaces the first search page of a scenario.
func setSearch(body string) func(*replayScenario, *Request) {
	return func(s *replayScenario, _ *Request) {
		s.Responses["POST /rest/api/3/search/jql#"] = replayResult{Status: 200, Body: json.RawMessage(body)}
	}
}

func jsonString(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func TestHugeProviderFieldsNeverReachTheResult(t *testing.T) {
	t.Parallel()

	huge := strings.Repeat("A", 4<<20)
	search := `{"issues":[{"id":"1","key":"K-1","self":"https://j.invalid/` + huge[:1<<16] + `","fields":{
		"summary":` + jsonString(t, huge) + `,"assignee":{"accountId":"a","displayName":` + jsonString(t, huge) + `},
		"project":{"id":"10000","key":"K","name":` + jsonString(t, huge) + `},"status":{"id":"1","name":"Open"}}}],"isLast":true}`
	result, _, _ := runScenario(t, "basic", func(s *replayScenario, r *Request) {
		setSearch(search)(s, r)
		for key := range s.Responses {
			if strings.Contains(key, "/changelog") || strings.Contains(key, "/remotelink") {
				delete(s.Responses, key)
			}
		}
		s.Responses["GET /rest/api/3/issue/K-1/changelog?maxResults=50&startAt=0"] = replayResult{Status: 200, Body: json.RawMessage(`{"values":[]}`)}
		s.Responses["GET /rest/api/3/issue/K-1/remotelink"] = replayResult{Status: 200, Body: json.RawMessage(`[]`)}
	})
	if result.State != sdk.ResultComplete {
		t.Fatalf("State = %q (%+v), want complete: oversize free text is replaced by presence flags", result.State, result.Statuses)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > 200<<10 {
		t.Fatalf("result is %d bytes; huge provider text leaked into facts", len(encoded))
	}
	mustValidate(t, result)
}

func TestHugeEmittedValueFailsTerminalAtThePayloadBound(t *testing.T) {
	t.Parallel()

	changelog := `{"startAt":0,"maxResults":50,"total":1,"isLast":true,"values":[{"id":"9","items":[{"field":"status","fromString":` +
		jsonString(t, strings.Repeat("B", 2<<20)) + `,"toString":"Done"}]}]}`
	result, _, _ := runScenario(t, "basic", func(s *replayScenario, _ *Request) {
		s.Responses["GET /rest/api/3/issue/ESHU-2/changelog?maxResults=50&startAt=0"] = replayResult{Status: 200, Body: json.RawMessage(changelog)}
	})
	if result.State != sdk.ResultTerminal || result.Statuses[0].FailureClass != FailurePayloadBudget || len(result.Facts) != 0 {
		t.Fatalf("State=%q statuses=%+v, want terminal %s", result.State, result.Statuses, FailurePayloadBudget)
	}
}

func TestControlCharactersStayJSONAndValidatorSafe(t *testing.T) {
	t.Parallel()

	search := `{"issues":[{"id":"1\u0000","key":"K-1\n\t\r","fields":{"summary":"line break\u0000nul","status":{"id":"1","name":"Op\u0007en\n"},
		"project":{"id":"10000","key":"K\u0001"}}}],"isLast":true}`
	changelog := `{"values":[{"id":"1\u0000","items":[{"field":"fi\neld","fromString":"a\u0000b","toString":"c\td"}]}]}`
	result, _, _ := runScenario(t, "basic", func(s *replayScenario, r *Request) {
		setSearch(search)(s, r)
		s.Responses["GET /rest/api/3/issue/K-1/changelog?maxResults=50&startAt=0"] = replayResult{Status: 200, Body: json.RawMessage(changelog)}
		s.Responses["GET /rest/api/3/issue/K-1/remotelink"] = replayResult{Status: 200, Body: json.RawMessage(`[]`)}
	})
	if result.State != sdk.ResultComplete {
		t.Fatalf("State = %q, want complete", result.State)
	}
	mustValidate(t, result)
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip sdk.Result
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatalf("result is not valid JSON after control characters: %v", err)
	}
	for _, fact := range result.Facts {
		if strings.ContainsAny(fact.StableKey, "\n\r\t\x00") {
			t.Fatalf("stable key carries control characters: %q", fact.StableKey)
		}
	}
}

func TestUnexpectedProviderShapesFailBoundedNeverPanic(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		edit  func(*replayScenario, *Request)
		state sdk.ResultState
		class string
	}{
		{"search is an array", setSearch(`[]`), sdk.ResultRetryable, FailureRetryable},
		{"search is a string", setSearch(`"x"`), sdk.ResultRetryable, FailureRetryable},
		{"search is null", setSearch(`null`), sdk.ResultComplete, ""},
		{"issues is an object", setSearch(`{"issues":{"a":1}}`), sdk.ResultRetryable, FailureRetryable},
		{"issue id is a number", setSearch(`{"issues":[{"id":1,"key":"K-1","fields":{}}],"isLast":true}`), sdk.ResultRetryable, FailureRetryable},
		{"assignee is a string", setSearch(`{"issues":[{"id":"1","key":"K-1","fields":{"assignee":"nobody"}}],"isLast":true}`), sdk.ResultRetryable, FailureRetryable},
		{"issue is null", setSearch(`{"issues":[null],"isLast":true}`), sdk.ResultTerminal, FailureInvalidEvidence},
		{"issue has no key", setSearch(`{"issues":[{"id":"1","fields":{}}],"isLast":true}`), sdk.ResultTerminal, FailureInvalidEvidence},
		{"empty 200 body", setSearch(``), sdk.ResultRetryable, FailureRetryable},
		{"html error page", func(s *replayScenario, _ *Request) {
			s.Responses["POST /rest/api/3/search/jql#"] = replayResult{Status: 200, Body: json.RawMessage(`<html>login</html>`)}
		}, sdk.ResultRetryable, FailureRetryable},
		{"deeply nested", setSearch(strings.Repeat("[", 200000)), sdk.ResultRetryable, FailureRetryable},
		{"remote links is an object", func(s *replayScenario, _ *Request) {
			s.Responses["GET /rest/api/3/issue/ESHU-1/remotelink"] = replayResult{Status: 200, Body: json.RawMessage(`{"links":[]}`)}
		}, sdk.ResultRetryable, FailureRetryable},
		{"changelog values is a string", func(s *replayScenario, _ *Request) {
			s.Responses["GET /rest/api/3/issue/ESHU-1/changelog?maxResults=50&startAt=0"] = replayResult{Status: 200, Body: json.RawMessage(`{"values":"x"}`)}
		}, sdk.ResultRetryable, FailureRetryable},
		{"metadata page is a list", func(s *replayScenario, _ *Request) {
			s.Responses["GET /rest/api/3/field/search?maxResults=100&startAt=0"] = replayResult{Status: 200, Body: json.RawMessage(`[1,2]`)}
		}, sdk.ResultRetryable, FailureRetryable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result, _, logs := runScenario(t, "basic", tc.edit)
			if result.State != tc.state {
				t.Fatalf("State = %q (%+v), want %q", result.State, result.Statuses, tc.state)
			}
			if tc.class != "" && (len(result.Statuses) == 0 || result.Statuses[0].FailureClass != tc.class) {
				t.Fatalf("Statuses = %+v, want class %q", result.Statuses, tc.class)
			}
			if tc.state != sdk.ResultComplete && len(result.Facts) != 0 {
				t.Fatalf("failure carries %d facts", len(result.Facts))
			}
			mustValidate(t, result)
			if len(logs) > 2048 {
				t.Fatalf("stderr log line is %d bytes, want one bounded line", len(logs))
			}
		})
	}
}

func TestProviderErrorTextIsNeverEchoed(t *testing.T) {
	t.Parallel()

	for _, status := range []int{400, 401, 403, 404, 410, 429, 500, 503} {
		leak := `{"errorMessages":["` + testToken + `","bot@fixture.invalid","https://user:pw@jira.fixture.invalid/x?token=abc"]}`
		result, _, logs := runScenario(t, "basic", func(s *replayScenario, _ *Request) {
			s.Responses["POST /rest/api/3/search/jql#"] = replayResult{Status: status, Body: json.RawMessage(leak), Headers: map[string]string{"Retry-After": "3"}}
		})
		encoded, _ := json.Marshal(result)
		for _, text := range []string{string(encoded), logs} {
			for _, secret := range []string{testToken, "bot@fixture.invalid", "user:pw", "token=abc"} {
				if strings.Contains(text, secret) {
					t.Fatalf("status %d: output leaks %q", status, secret)
				}
			}
		}
		mustValidate(t, result)
	}
}

func TestRetryAfterIsBoundedAndNeverZeroOnRetryable(t *testing.T) {
	t.Parallel()

	for header, want := range map[string]int{"": 60, "0": 60, "-5": 60, "junk": 60, "7": 7, "99999999999": 86400} {
		result, _, _ := runScenario(t, "failure_rate_limited", func(s *replayScenario, _ *Request) {
			entry := s.Responses["POST /rest/api/3/search/jql#"]
			entry.Headers = map[string]string{"Retry-After": header}
			s.Responses["POST /rest/api/3/search/jql#"] = entry
		})
		if result.State != sdk.ResultRetryable || result.Statuses[0].RetryAfterSeconds != want {
			t.Errorf("Retry-After %q: state=%q retry_after=%d, want %d", header, result.State, result.Statuses[0].RetryAfterSeconds, want)
		}
		mustValidate(t, result)
	}
}
