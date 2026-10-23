// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewHTTPClientRejectsBadConfig(t *testing.T) {
	t.Parallel()

	for name, config := range map[string]HTTPClientConfig{
		"no token":         {BaseURL: "https://example.invalid", Token: ""},
		"blank token":      {BaseURL: "https://example.invalid", Token: "  "},
		"no scheme":        {BaseURL: "example.invalid", Token: "t"},
		"file scheme":      {BaseURL: "file:///etc/passwd", Token: "t"},
		"embedded creds":   {BaseURL: "https://user:pw@example.invalid", Token: "t"},
		"no host":          {BaseURL: "https://", Token: "t"},
		"unparseable host": {BaseURL: "https://exa mple.invalid:port", Token: "t"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := NewHTTPClient(config)
			if err == nil {
				t.Fatal("NewHTTPClient() error = nil, want rejection")
			}
			if strings.Contains(err.Error(), "pw") && strings.Contains(config.BaseURL, "pw") {
				t.Fatalf("error leaks the URL credential: %v", err)
			}
		})
	}
}

func TestClientSendsBasicAuthWithEmailAndBearerWithout(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		email string
		want  string
	}{
		{"basic", "bot@fixture.invalid", "Basic " + base64.StdEncoding.EncodeToString([]byte("bot@fixture.invalid:"+testToken))},
		{"bearer", "", "Bearer " + testToken},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stub := newJiraStub(t, func(w http.ResponseWriter, _ stubRequest) { writeJSON(w, 200, `{"issues":[],"isLast":true}`) })
			target := TargetConfig{JQL: "project = X", IssueLimit: 1}
			client := newTestClient(t, stub.URL, tc.email)
			var stats CollectionStats
			if _, _, err := client.searchIssues(t.Context(), target, testWindow(), &stats); err != nil {
				t.Fatalf("searchIssues() error = %v", err)
			}
			if got := stub.recorded()[0].Auth; got != tc.want {
				t.Fatalf("Authorization = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBoundedJQLClampsTheUpdatedWindow(t *testing.T) {
	t.Parallel()

	window := CollectionWindow{
		Since: time.Date(2026, 9, 29, 12, 5, 59, 0, time.UTC),
		Until: time.Date(2026, 10, 1, 12, 0, 30, 0, time.UTC),
	}
	clause := `updated >= "2026-09-29 12:05" AND updated <= "2026-10-01 12:00"`
	cases := []struct{ name, jql, want string }{
		{"blank", "", clause + " ORDER BY updated ASC"},
		{"plain", "project = ESHU", "(project = ESHU) AND " + clause + " ORDER BY updated ASC"},
		{"explicit order", "project = ESHU ORDER BY created DESC", "(project = ESHU) AND " + clause + " ORDER BY created DESC"},
		{"order only", "order by key", clause + " order by key"},
		{"padded", "  project = ESHU  ", "(project = ESHU) AND " + clause + " ORDER BY updated ASC"},
	}
	for _, tc := range cases {
		if got := boundedJQL(tc.jql, window); got != tc.want {
			t.Errorf("%s: boundedJQL(%q) = %q, want %q", tc.name, tc.jql, got, tc.want)
		}
	}
}

func TestSearchSendsWindowedJQLAndFields(t *testing.T) {
	t.Parallel()

	stub := newJiraStub(t, func(w http.ResponseWriter, _ stubRequest) { writeJSON(w, 200, `{"issues":[],"isLast":true}`) })
	client := newTestClient(t, stub.URL, "")
	var stats CollectionStats
	if _, _, err := client.searchIssues(t.Context(), TargetConfig{JQL: "project = ESHU", IssueLimit: 30}, testWindow(), &stats); err != nil {
		t.Fatalf("searchIssues() error = %v", err)
	}
	request := stub.recorded()[0]
	if request.Method != http.MethodPost || request.Path != jiraSearchEndpoint {
		t.Fatalf("request = %s %s", request.Method, request.Path)
	}
	var body searchRequest
	if err := json.Unmarshal([]byte(request.Body), &body); err != nil {
		t.Fatal(err)
	}
	if body.MaxResults != 30 || !strings.Contains(body.JQL, `updated >= "2026-09-29 12:00"`) || !strings.HasSuffix(body.JQL, "ORDER BY updated ASC") {
		t.Fatalf("search body = %+v", body)
	}
	if strings.Join(body.Fields, ",") != "summary,created,updated,resolutiondate,issuetype,status,project,assignee,reporter" {
		t.Fatalf("fields = %v", body.Fields)
	}
}

func TestSearchPaginatesByTokenAndReportsTruncation(t *testing.T) {
	t.Parallel()

	page := func(ids ...int) string {
		parts := make([]string, 0, len(ids))
		for _, id := range ids {
			parts = append(parts, fmt.Sprintf(`{"id":"%d","key":"K-%d","fields":{}}`, id, id))
		}
		return strings.Join(parts, ",")
	}
	cases := []struct {
		name          string
		limit         int
		pages         map[string]string
		wantIDs       []string
		wantTruncated bool
		wantPages     int
	}{
		{"walks every page", 10, map[string]string{
			"":   `{"issues":[` + page(1, 2) + `],"nextPageToken":"a","isLast":false}`,
			"a":  `{"issues":[` + page(3) + `],"isLast":true}`,
			"zz": `{}`,
		}, []string{"1", "2", "3"}, false, 2},
		{"cap ends the walk with more available", 2, map[string]string{
			"": `{"issues":[` + page(1, 2) + `],"nextPageToken":"a","isLast":false}`,
		}, []string{"1", "2"}, true, 1},
		{"cap met exactly on the last page", 2, map[string]string{
			"": `{"issues":[` + page(1, 2) + `],"nextPageToken":"a","isLast":true}`,
		}, []string{"1", "2"}, false, 1},
		{"page larger than cap is cut", 2, map[string]string{
			"": `{"issues":[` + page(1, 2, 3) + `],"isLast":true}`,
		}, []string{"1", "2"}, false, 1},
		{"empty page stops the walk", 5, map[string]string{
			"": `{"issues":[],"nextPageToken":"a","isLast":false}`,
		}, nil, false, 1},
		{"missing token stops the walk", 5, map[string]string{
			"": `{"issues":[` + page(1) + `],"isLast":false}`,
		}, []string{"1"}, false, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stub := newJiraStub(t, func(w http.ResponseWriter, r stubRequest) {
				var body searchRequest
				_ = json.Unmarshal([]byte(r.Body), &body)
				response, ok := tc.pages[body.NextPageToken]
				if !ok {
					writeJSON(w, 404, `{"errorMessages":["unexpected page"]}`)
					return
				}
				writeJSON(w, 200, response)
			})
			client := newTestClient(t, stub.URL, "")
			var stats CollectionStats
			issues, truncated, err := client.searchIssues(t.Context(), TargetConfig{IssueLimit: tc.limit}, testWindow(), &stats)
			if err != nil {
				t.Fatalf("searchIssues() error = %v", err)
			}
			var ids []string
			for _, issue := range issues {
				ids = append(ids, issue.ID)
			}
			if strings.Join(ids, ",") != strings.Join(tc.wantIDs, ",") || truncated != tc.wantTruncated || stats.SearchPages != tc.wantPages {
				t.Fatalf("ids=%v truncated=%v pages=%d, want %v/%v/%d", ids, truncated, stats.SearchPages, tc.wantIDs, tc.wantTruncated, tc.wantPages)
			}
		})
	}
}

func TestSearchClampsIssueLimit(t *testing.T) {
	t.Parallel()

	var asked atomic.Int64
	stub := newJiraStub(t, func(w http.ResponseWriter, r stubRequest) {
		var body searchRequest
		_ = json.Unmarshal([]byte(r.Body), &body)
		asked.Store(int64(body.MaxResults))
		writeJSON(w, 200, `{"issues":[],"isLast":true}`)
	})
	client := newTestClient(t, stub.URL, "")
	for limit, want := range map[int]int64{0: 50, -4: 50, 7: 7, 100: 100, 5000: 100} {
		var stats CollectionStats
		if _, _, err := client.searchIssues(t.Context(), TargetConfig{IssueLimit: limit}, testWindow(), &stats); err != nil {
			t.Fatal(err)
		}
		if asked.Load() != want {
			t.Errorf("issue_limit %d requested maxResults %d, want %d", limit, asked.Load(), want)
		}
	}
}

func TestChangelogPaginatesWithinTheLimit(t *testing.T) {
	t.Parallel()

	stub := newJiraStub(t, func(w http.ResponseWriter, r stubRequest) {
		switch r.Query {
		case "maxResults=3&startAt=0":
			writeJSON(w, 200, `{"startAt":0,"maxResults":2,"total":5,"values":[
				{"id":"1","created":"2026-09-20T09:00:00.000+0000","items":[{"field":"status","fromString":"A","toString":"B"}]},
				{"id":"2","created":"2026-09-20T09:00:00.000+0000","items":[{"field":"status","fromString":"B","toString":"C"}]}]}`)
		case "maxResults=1&startAt=2":
			writeJSON(w, 200, `{"startAt":2,"maxResults":2,"total":5,"values":[
				{"id":"3","items":[{"field":"status","fromString":"C","toString":"D"},{"field":"status","fromString":"D","toString":"E"}]}]}`)
		default:
			writeJSON(w, 404, `{"errorMessages":["unexpected query `+r.Query+`"]}`)
		}
	})
	client := newTestClient(t, stub.URL, "")
	var stats CollectionStats
	transitions, err := client.issueChangelog(t.Context(), Issue{ID: "10", Key: "K-10"}, 3, &stats)
	if err != nil {
		t.Fatalf("issueChangelog() error = %v", err)
	}
	if len(transitions) != 3 || stats.ChangelogPages != 2 {
		t.Fatalf("transitions=%d pages=%d, want 3/2", len(transitions), stats.ChangelogPages)
	}
	if transitions[2].To != "D" {
		t.Fatalf("limit must cut inside the last entry, got %+v", transitions[2])
	}
}

func TestChangelogWithoutPaginationMetadataIsOnePage(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	stub := newJiraStub(t, func(w http.ResponseWriter, _ stubRequest) {
		calls.Add(1)
		writeJSON(w, 200, `{"values":[{"id":"1","items":[{"field":"status","fromString":"A","toString":"B"}]}]}`)
	})
	client := newTestClient(t, stub.URL, "")
	var stats CollectionStats
	if _, err := client.issueChangelog(t.Context(), Issue{ID: "10", Key: "K-10"}, 50, &stats); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1 (no pagination metadata means a single page)", calls.Load())
	}
}

func TestChangelogWalkStopsWhenAProviderAdvertisesPagesForever(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	stub := newJiraStub(t, func(w http.ResponseWriter, _ stubRequest) {
		calls.Add(1)
		// Entries with no items never fill the limit, and the page claims
		// there is always more.
		writeJSON(w, 200, `{"startAt":0,"maxResults":1,"total":1000000,"isLast":false,"values":[{"id":"1","items":[]}]}`)
	})
	client := newTestClient(t, stub.URL, "")
	var stats CollectionStats
	transitions, err := client.issueChangelog(t.Context(), Issue{ID: "10", Key: "K-10"}, 50, &stats)
	if err != nil {
		t.Fatalf("issueChangelog() error = %v", err)
	}
	if len(transitions) != 0 || calls.Load() != maxChangelogPages {
		t.Fatalf("transitions=%d calls=%d, want 0 and exactly %d", len(transitions), calls.Load(), maxChangelogPages)
	}
}

func TestChangelogRedactsSensitiveFieldValues(t *testing.T) {
	t.Parallel()

	for field, redacted := range map[string]bool{
		"assignee": true, "Reporter": true, "creator": true, "comment": true, "description": true,
		"attachment": true, "customfield_10010": true, "status": false, "labels": false,
	} {
		from, to, got := changelogValues(changelogItem{Field: field, From: "secret-from", To: "secret-to", FieldID: field})
		if got != redacted {
			t.Errorf("field %q redacted = %v, want %v", field, got, redacted)
		}
		if redacted && (from != "" || to != "") {
			t.Errorf("field %q leaked values %q/%q", field, from, to)
		}
		if !redacted && (from != "secret-from" || to != "secret-to") {
			t.Errorf("field %q lost values %q/%q", field, from, to)
		}
	}
	// A custom field is redacted by its field id even when the display name
	// looks harmless.
	if _, _, redacted := changelogValues(changelogItem{Field: "Story Points", FieldID: "customfield_10010"}); !redacted {
		t.Fatal("customfield_* ids must be redacted regardless of display name")
	}
}

func TestRemoteLinksDedupeRejectSanitizeAndLimit(t *testing.T) {
	t.Parallel()

	stub := newJiraStub(t, func(w http.ResponseWriter, _ stubRequest) {
		writeJSON(w, 200, `[
			{"id":1,"globalId":"g1","application":{"name":"GitHub","type":"com.github"},"object":{"url":"https://github.com/o/r/pull/9?token=zzz#frag","title":"T"}},
			{"id":1,"object":{"url":"https://github.com/o/r/pull/9"}},
			{"application":{"name":"x"},"object":{}},
			{"id":"abc","object":{"url":"https://user:pw@example.invalid/x?api_key=k&keep=1"}},
			{"id":3,"object":{"url":"https://example.invalid/3"}}]`)
	})
	client := newTestClient(t, stub.URL, "")
	var stats CollectionStats
	links, err := client.issueRemoteLinks(t.Context(), Issue{ID: "10", Key: "K-10"}, 4, &stats)
	if err != nil {
		t.Fatalf("issueRemoteLinks() error = %v", err)
	}
	if len(links) != 2 || stats.RemoteLinksRejected != 1 || stats.UnsupportedProviderLinks != 1 {
		t.Fatalf("links=%d rejected=%d unsupported=%d", len(links), stats.RemoteLinksRejected, stats.UnsupportedProviderLinks)
	}
	if links[0].Object.URL != "https://github.com/o/r/pull/9" || links[0].ProviderSupportState != "supported_provider" {
		t.Fatalf("first link = %+v", links[0])
	}
	if got := links[1].Object.URL; strings.Contains(got, "pw") || strings.Contains(got, "api_key") || !strings.Contains(got, "keep=1") {
		t.Fatalf("second link URL = %q, want credentials and sensitive query removed", got)
	}
}

func TestMetadataWarningsReplaceHiddenMetadata(t *testing.T) {
	t.Parallel()

	cases := map[int]struct{ reason, class string }{
		http.StatusUnauthorized: {"permission_hidden", FailurePermissionHidden},
		http.StatusForbidden:    {"permission_hidden", FailurePermissionHidden},
		http.StatusNotFound:     {"unsupported", FailureDeleted},
		http.StatusGone:         {"archived", FailureArchived},
	}
	for status, want := range cases {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			t.Parallel()
			stub := newJiraStub(t, func(w http.ResponseWriter, r stubRequest) {
				if strings.HasPrefix(r.Path, "/rest/api/3/workflows") {
					writeJSON(w, status, `{"errorMessages":["`+testToken+` must not travel"]}`)
					return
				}
				writeJSON(w, 200, `{"values":[],"isLast":true}`)
			})
			client := newTestClient(t, stub.URL, "")
			result := CollectionResult{}
			if err := client.collectWorkflows(t.Context(), 10, &result); err != nil {
				t.Fatalf("collectWorkflows() error = %v", err)
			}
			if len(result.MetadataWarnings) != 1 {
				t.Fatalf("warnings = %+v", result.MetadataWarnings)
			}
			warning := result.MetadataWarnings[0]
			if warning.MetadataType != "workflow" || warning.Reason != want.reason || warning.FailureClass != want.class {
				t.Fatalf("warning = %+v, want %+v", warning, want)
			}
		})
	}
}

func TestMetadataHardFailureStopsTheClaim(t *testing.T) {
	t.Parallel()

	stub := newJiraStub(t, func(w http.ResponseWriter, r stubRequest) {
		switch r.Path {
		case jiraSearchEndpoint:
			writeJSON(w, 200, `{"issues":[],"isLast":true}`)
		case jiraProjectSearchEndpoint:
			writeJSON(w, 200, `{"values":[],"isLast":true}`)
		default:
			writeJSON(w, 500, `{"errorMessages":["boom"]}`)
		}
	})
	client := newTestClient(t, stub.URL, "")
	_, err := client.CollectWorkItemEvidence(t.Context(), TargetConfig{JQL: "project = X"}, testWindow())
	var partial PartialCollectionError
	if !errors.As(err, &partial) || partial.Stage != "metadata" || partial.Stats.PartialFailures != 1 {
		t.Fatalf("error = %v, want a metadata PartialCollectionError", err)
	}
	if strings.Contains(err.Error(), "boom") {
		t.Fatalf("error text echoes the provider body: %v", err)
	}
}

func TestClientDoesNotRetryAndKeepsRetryAfter(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	stub := newJiraStub(t, func(w http.ResponseWriter, _ stubRequest) {
		calls.Add(1)
		w.Header().Set("Retry-After", "45")
		w.Header().Set("RateLimit-Reason", "jira-quota")
		writeJSON(w, 429, `{"errorMessages":["slow down"]}`)
	})
	client := newTestClient(t, stub.URL, "")
	var stats CollectionStats
	_, _, err := client.searchIssues(t.Context(), TargetConfig{}, testWindow(), &stats)
	var jiraErr JiraError
	if !errors.As(err, &jiraErr) {
		t.Fatalf("error %T is not a JiraError: %v", err, err)
	}
	if calls.Load() != 1 || jiraErr.StatusCode != 429 || jiraErr.RetryAfter != 45*time.Second || jiraErr.RateLimitReason != "jira-quota" {
		t.Fatalf("calls=%d err=%+v", calls.Load(), jiraErr)
	}
	if jiraErr.Operation != "search_issues" {
		t.Fatalf("Operation = %q, want search_issues", jiraErr.Operation)
	}
}

func TestClientHonorsContextCancellation(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	stub := newJiraStub(t, func(w http.ResponseWriter, _ stubRequest) {
		select {
		case <-release:
		case <-time.After(10 * time.Second):
		}
		writeJSON(w, 200, `{}`)
	})
	t.Cleanup(func() { close(release) })
	client := newTestClient(t, stub.URL, "")
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	var stats CollectionStats
	_, _, err := client.searchIssues(ctx, TargetConfig{}, testWindow(), &stats)
	if err == nil {
		t.Fatal("searchIssues() error = nil, want cancellation")
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("cancellation took %s", elapsed)
	}
	if failure := classifyFailure(err); failure.Terminal || failure.Class != FailureRetryable {
		t.Fatalf("cancellation classified as %+v, want retryable", failure)
	}
}

func TestIssueEndpointOperationTokens(t *testing.T) {
	t.Parallel()

	for endpoint, want := range map[string]string{
		jiraSearchEndpoint:                    "search_issues",
		jiraProjectSearchEndpoint:             "list_projects",
		jiraIssueTypesEndpoint:                "list_issue_types",
		jiraStatusesEndpoint:                  "list_statuses",
		jiraFieldsEndpoint:                    "list_fields",
		jiraWorkflowsEndpoint:                 "list_workflows",
		"/rest/api/3/issue/ESHU-1/changelog":  "read_changelog",
		"/rest/api/3/issue/ESHU-1/remotelink": "read_remote_links",
		"/rest/api/3/issue/ESHU-1":            "",
		"/somewhere/else":                     "",
	} {
		if got := jiraOperationForEndpoint(endpoint); got != want {
			t.Errorf("jiraOperationForEndpoint(%q) = %q, want %q", endpoint, got, want)
		}
	}
}
