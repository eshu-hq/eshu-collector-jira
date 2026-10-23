// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu-collector-jira/internal/stableid"
	sdk "github.com/eshu-hq/eshu/sdk/go/collector"
)

func testFactContext() FactContext {
	return FactContext{
		ScopeID: "jira:site:fixture", GenerationID: "generation-1", CollectorInstanceID: "jira-qa",
		SourceSystem: "jira", ObservedAt: testObservedAt(), SourceURI: "https://example.atlassian.net",
	}
}

func TestStableKeysMatchTheInTreeIdentityShape(t *testing.T) {
	t.Parallel()

	ctx := testFactContext()
	record, err := buildRecordFact(ctx, Issue{ID: "10001", Key: "ESHU-1"})
	if err != nil {
		t.Fatal(err)
	}
	want := stableid.StableID("work_item.record", map[string]any{
		"provider": "jira_cloud", "scope_id": "jira:site:fixture", "issue_id": "10001", "issue_key": "ESHU-1",
	})
	if record.StableKey != want {
		t.Fatalf("record key = %s, want %s", record.StableKey, want)
	}
	transition, err := buildTransitionFact(ctx, Transition{ID: "555", IssueID: "10001", Field: "status"})
	if err != nil {
		t.Fatal(err)
	}
	wantTransition := stableid.StableID("work_item.transition", map[string]any{
		"provider": "jira_cloud", "scope_id": "jira:site:fixture", "issue_id": "10001", "changelog_id": "555", "field": "status",
	})
	if transition.StableKey != wantTransition {
		t.Fatalf("transition key = %s, want %s", transition.StableKey, wantTransition)
	}
}

func TestBuildersRejectUnidentifiableEvidence(t *testing.T) {
	t.Parallel()

	ctx := testFactContext()
	checks := map[string]func() error{
		"blank issue id":         func() error { _, err := buildRecordFact(ctx, Issue{Key: "K-1"}); return err },
		"blank issue key":        func() error { _, err := buildRecordFact(ctx, Issue{ID: "1"}); return err },
		"blank changelog id":     func() error { _, err := buildTransitionFact(ctx, Transition{IssueID: "1"}); return err },
		"anonymous remote link":  func() error { _, err := buildExternalLinkFact(ctx, ExternalLink{IssueID: "1"}); return err },
		"project without ids":    func() error { _, err := buildProjectMetadataFact(ctx, ProjectMetadata{Name: "x"}); return err },
		"blank issue type id":    func() error { _, err := buildIssueTypeMetadataFact(ctx, IssueTypeMetadata{Name: "x"}); return err },
		"blank status id":        func() error { _, err := buildStatusMetadataFact(ctx, StatusMetadata{Name: "x"}); return err },
		"blank workflow id":      func() error { _, err := buildWorkflowMetadataFact(ctx, WorkflowMetadata{Name: "x"}); return err },
		"blank field id":         func() error { _, err := buildFieldMetadataFact(ctx, FieldMetadata{Name: "x"}); return err },
		"warning without type":   func() error { _, err := buildMetadataWarningFact(ctx, MetadataWarning{Reason: "r"}); return err },
		"warning without reason": func() error { _, err := buildMetadataWarningFact(ctx, MetadataWarning{MetadataType: "t"}); return err },
	}
	for name, check := range checks {
		if err := check(); err == nil {
			t.Errorf("%s: error = nil, want rejection", name)
		}
	}
	for name, mutate := range map[string]func(*FactContext){
		"blank scope":      func(c *FactContext) { c.ScopeID = " " },
		"blank generation": func(c *FactContext) { c.GenerationID = "" },
		"blank instance":   func(c *FactContext) { c.CollectorInstanceID = "" },
		"blank source":     func(c *FactContext) { c.SourceSystem = "" },
	} {
		bad := ctx
		mutate(&bad)
		if _, err := buildRecordFact(bad, Issue{ID: "1", Key: "K-1"}); err == nil {
			t.Errorf("%s: error = nil, want rejection", name)
		}
	}
}

func TestRecordPayloadCarriesPresenceFlagsNotContent(t *testing.T) {
	t.Parallel()

	issue := Issue{
		ID: "10001", Key: "ESHU-1", Summary: "Private launch plan",
		Project:  Reference{ID: "10000", Key: "ESHU", Name: "Secret Project"},
		Assignee: Reference{AccountID: "acct-9", DisplayName: "Pat Q"}, Reporter: Reference{},
		Self: "https://example.atlassian.net/rest/api/3/issue/10001?token=zzz", BrowseURL: "https://example.atlassian.net/browse/ESHU-1",
		CreatedAt: time.Date(2026, 9, 1, 10, 0, 0, 0, time.FixedZone("x", 3600)),
	}
	fact, err := buildRecordFact(testFactContext(), issue)
	if err != nil {
		t.Fatal(err)
	}
	payload := fact.Payload
	for _, key := range []string{"summary", "project_name", "assignee_account_id", "assignee_display_name", "reporter_account_id", "self_url", "source_url"} {
		if payload[key] != "" {
			t.Errorf("payload[%s] = %v, want blanked", key, payload[key])
		}
	}
	if payload["summary_present"] != true || payload["project_name_present"] != true || payload["assignee_present"] != true || payload["reporter_present"] != false {
		t.Fatalf("presence flags = %v/%v/%v/%v", payload["summary_present"], payload["project_name_present"], payload["assignee_present"], payload["reporter_present"])
	}
	if payload["created_at"] != "2026-09-01T09:00:00Z" {
		t.Fatalf("created_at = %v, want UTC RFC3339", payload["created_at"])
	}
	if fp, _ := payload["self_url_fingerprint"].(string); !strings.HasPrefix(fp, "sha256:") {
		t.Fatalf("self_url_fingerprint = %v", payload["self_url_fingerprint"])
	}
	// The fingerprint is over the sanitized URL: the secret query is gone.
	if payload["self_url_fingerprint"] != urlFingerprint("https://example.atlassian.net/rest/api/3/issue/10001") {
		t.Fatal("fingerprint must be taken over the sanitized URL")
	}
}

func TestRedactionsNameOnlyFieldsTheSourceHeld(t *testing.T) {
	t.Parallel()

	withAssignee, err := buildRecordFact(testFactContext(), Issue{ID: "1", Key: "K-1", Summary: "s", Assignee: Reference{AccountID: "a"}})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, redaction := range withAssignee.Redactions {
		if redaction.Reason != redactionPolicyVersion {
			t.Fatalf("redaction %+v lacks the policy reason", redaction)
		}
		got = append(got, redaction.Field)
	}
	// source_url is redacted too: with no browse URL on the issue it falls back to the site URL.
	want := []string{"assignee_account_id", "assignee_display_name", "source_url", "summary"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("redacted fields = %v, want %v (sorted, only what the source held)", got, want)
	}
	bare, err := buildRecordFact(testFactContext(), Issue{ID: "1", Key: "K-1"})
	if err != nil {
		t.Fatal(err)
	}
	// With no browse or self URL the source URL falls back to the site URL,
	// which is blanked like any other URL; nothing else was held.
	if len(bare.Redactions) != 1 || bare.Redactions[0].Field != "source_url" {
		t.Fatalf("an issue with no private fields redacted %+v, want only source_url", bare.Redactions)
	}
	transition, err := buildTransitionFact(testFactContext(), Transition{ID: "1", IssueID: "1", Field: "assignee", From: "a", To: "b", ValueRedacted: true, Author: Reference{AccountID: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if transition.Payload["from"] != "" || transition.Payload["to"] != "" || transition.Payload["author_redacted"] != true {
		t.Fatalf("redacted transition payload = %v", transition.Payload)
	}
}

func TestSanitizeURL(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"":               "",
		"  ":             "",
		"not a url":      "",
		"/relative/path": "",
		"https://example.invalid/a?x=1&token=t#frag":     "https://example.invalid/a?x=1",
		"https://u:p@example.invalid/a":                  "https://example.invalid/a",
		"https://example.invalid/a?API_KEY=1&Key=2&ok=3": "https://example.invalid/a?ok=3",
		"https://example.invalid/a?signature=s&sig=s2":   "https://example.invalid/a",
		"http://example.invalid:8080/a b":                "http://example.invalid:8080/a%20b",
	}
	for input, want := range cases {
		if got := sanitizeURL(input); got != want {
			t.Errorf("sanitizeURL(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestLinkedRepositoryID(t *testing.T) {
	t.Parallel()

	link := func(app, url string) ExternalLink {
		return ExternalLink{Application: LinkApplication{Type: app}, Object: LinkObject{URL: url}}
	}
	same := linkedRepositoryID(link("com.github", "https://github.com/Eshu-HQ/Eshu/pull/42"))
	if same == "" || same != linkedRepositoryID(link("com.github", "https://github.com/eshu-hq/eshu/pull/7?x=1")) {
		t.Fatalf("one repository must resolve to one id regardless of case, PR number, or query: %q", same)
	}
	nested := linkedRepositoryID(link("gitlab", "https://gitlab.example.invalid/group/sub/repo/-/merge_requests/3"))
	if nested == "" || nested == same {
		t.Fatalf("nested gitlab id = %q", nested)
	}
	for name, l := range map[string]ExternalLink{
		"issue not pull request": link("com.github", "https://github.com/o/r/issues/4"),
		"deep github path":       link("com.github", "https://github.com/o/r/extra/pull/4"),
		"shallow github path":    link("com.github", "https://github.com/o/pull/4"),
		"flat gitlab path":       link("gitlab", "https://gitlab.example.invalid/repo/-/merge_requests/3"),
		"confluence":             link("com.atlassian.confluence", "https://wiki.example.invalid/pages/1"),
		"malformed":              link("com.github", "%%%/pull/1"),
		"no host":                link("com.github", "/o/r/pull/1"),
	} {
		if id := linkedRepositoryID(l); id != "" {
			t.Errorf("%s: linkedRepositoryID = %q, want none (never persist a guess)", name, id)
		}
	}
}

func TestExternalLinkAnchorAndSupportState(t *testing.T) {
	t.Parallel()

	cases := []struct {
		link   ExternalLink
		anchor string
		state  string
	}{
		{ExternalLink{Application: LinkApplication{Type: "com.github"}, Object: LinkObject{URL: "https://github.com/o/r/pull/1"}}, "github_pull_request", "supported_provider"},
		{ExternalLink{Application: LinkApplication{Name: "GitLab"}, Object: LinkObject{URL: "https://g.invalid/a/b/-/merge_requests/2"}}, "gitlab_merge_request", "supported_provider"},
		{ExternalLink{Application: LinkApplication{Type: "com.github"}, Object: LinkObject{URL: "https://github.com/o/r/issues/1"}}, "remote_link", "unsupported_provider"},
		{ExternalLink{Application: LinkApplication{Type: "x"}, ProviderSupportState: " custom "}, "remote_link", "custom"},
	}
	for _, tc := range cases {
		if got := externalLinkAnchorClass(tc.link); got != tc.anchor {
			t.Errorf("anchor(%+v) = %q, want %q", tc.link, got, tc.anchor)
		}
		if got := externalLinkSupportState(tc.link); got != tc.state {
			t.Errorf("state(%+v) = %q, want %q", tc.link, got, tc.state)
		}
	}
}

func TestDedupeFactsKeepsFirstPositionAndLastContent(t *testing.T) {
	t.Parallel()

	facts := []sdk.Fact{
		{Kind: "a", StableKey: "1", Payload: map[string]any{"v": "first"}},
		{Kind: "b", StableKey: "1", Payload: map[string]any{"v": "other-kind"}},
		{Kind: "a", StableKey: "2", Payload: map[string]any{"v": "second"}},
		{Kind: "a", StableKey: "1", Payload: map[string]any{"v": "last"}},
	}
	got := dedupeFacts(facts)
	if len(got) != 3 || got[0].Payload["v"] != "last" || got[1].Kind != "b" || got[2].StableKey != "2" {
		t.Fatalf("dedupeFacts() = %+v", got)
	}
}
