// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

const testToken = "tok-SECRET-123"

func testEnv() func(string) string {
	env := map[string]string{
		"JIRA_EMAIL": "bot@fixture.invalid",
		"JIRA_TOKEN": testToken,
		"JIRA_JQL":   "project = ENV",
		"EMPTY":      "   ",
	}
	return func(key string) string { return env[key] }
}

func validConfig() map[string]any {
	return map[string]any{
		"site_id":   "example.atlassian.net",
		"token_env": "JIRA_TOKEN",
		"email_env": "JIRA_EMAIL",
		"jql":       "project = ESHU",
	}
}

func TestParseSettingsAppliesInTreeDefaults(t *testing.T) {
	t.Parallel()

	settings, err := ParseSettings(validConfig(), "jira:site:fixture", testEnv())
	if err != nil {
		t.Fatalf("ParseSettings() error = %v", err)
	}
	target := settings.Target
	if target.Provider != ProviderJiraCloud || target.ScopeID != "jira:site:fixture" {
		t.Fatalf("provider/scope = %q/%q", target.Provider, target.ScopeID)
	}
	if target.BaseURL != "https://example.atlassian.net" {
		t.Fatalf("BaseURL = %q, want default from site_id", target.BaseURL)
	}
	if target.UpdatedLookback != 24*time.Hour {
		t.Fatalf("UpdatedLookback = %s, want 24h default", target.UpdatedLookback)
	}
	if target.Email != "bot@fixture.invalid" || target.Token != testToken || target.JQL != "project = ESHU" {
		t.Fatalf("resolved credentials/jql = %q/%q/%q", target.Email, target.Token, target.JQL)
	}
	if settings.Limits != DefaultResourceUse() {
		t.Fatalf("Limits = %+v, want defaults %+v", settings.Limits, DefaultResourceUse())
	}
}

func TestParseSettingsReadsEveryTargetKey(t *testing.T) {
	t.Parallel()

	config := map[string]any{
		"provider":          "jira_cloud",
		"scope_id":          "jira:site:fixture",
		"site_id":           "example.atlassian.net",
		"base_url":          "https://jira.example.invalid/",
		"token_env":         "JIRA_TOKEN",
		"jql_env":           "JIRA_JQL",
		"issue_limit":       7,
		"updated_lookback":  "36h",
		"changelog_limit":   8,
		"remote_link_limit": 9,
		"metadata_limit":    10,
		"limits": map[string]any{
			"maxRecordsPerClaim": 11, "maxPayloadBytes": 12, "maxResultBytes": 13, "claimTimeoutSeconds": 14,
		},
	}
	settings, err := ParseSettings(config, "jira:site:fixture", testEnv())
	if err != nil {
		t.Fatalf("ParseSettings() error = %v", err)
	}
	target := settings.Target
	if target.BaseURL != "https://jira.example.invalid" || target.JQL != "project = ENV" || target.Email != "" {
		t.Fatalf("target = %q/%q/%q", target.BaseURL, target.JQL, target.Email)
	}
	if target.IssueLimit != 7 || target.ChangelogLimit != 8 || target.RemoteLinkLimit != 9 || target.MetadataLimit != 10 {
		t.Fatalf("limits = %d/%d/%d/%d", target.IssueLimit, target.ChangelogLimit, target.RemoteLinkLimit, target.MetadataLimit)
	}
	if target.UpdatedLookback != 36*time.Hour {
		t.Fatalf("UpdatedLookback = %s", target.UpdatedLookback)
	}
	want := ResourceUse{MaxRecordsPerClaim: 11, MaxPayloadBytes: 12, MaxResultBytes: 13, ClaimTimeoutSeconds: 14}
	if settings.Limits != want {
		t.Fatalf("Limits = %+v, want %+v", settings.Limits, want)
	}
}

func TestParseSettingsZeroLookbackSelectsDefault(t *testing.T) {
	t.Parallel()

	config := validConfig()
	config["updated_lookback"] = "0s"
	settings, err := ParseSettings(config, "scope", testEnv())
	if err != nil {
		t.Fatalf("ParseSettings() error = %v", err)
	}
	if settings.Target.UpdatedLookback != 24*time.Hour {
		t.Fatalf("UpdatedLookback = %s, want in-tree 24h default for zero", settings.Target.UpdatedLookback)
	}
}

func TestParseSettingsRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		mutate func(map[string]any)
		scope  string
	}{
		{"missing site", func(c map[string]any) { delete(c, "site_id") }, "scope"},
		{"unresolved token env", func(c map[string]any) { c["token_env"] = "MISSING" }, "scope"},
		{"blank token value", func(c map[string]any) { c["token_env"] = "EMPTY" }, "scope"},
		{"no token env", func(c map[string]any) { delete(c, "token_env") }, "scope"},
		{"no jql", func(c map[string]any) { delete(c, "jql") }, "scope"},
		{"both jql and jql_env", func(c map[string]any) { c["jql_env"] = "JIRA_JQL" }, "scope"},
		{"unresolved jql env", func(c map[string]any) { delete(c, "jql"); c["jql_env"] = "MISSING" }, "scope"},
		{"unsupported provider", func(c map[string]any) { c["provider"] = "jira_server" }, "scope"},
		{"scope mismatch", func(c map[string]any) { c["scope_id"] = "other-scope" }, "scope"},
		{"blank claim scope", func(c map[string]any) {}, "  "},
		{"negative lookback", func(c map[string]any) { c["updated_lookback"] = "-1h" }, "scope"},
		{"bad lookback", func(c map[string]any) { c["updated_lookback"] = "tomorrow" }, "scope"},
		{"unknown key", func(c map[string]any) { c["issue_limt"] = 5 }, "scope"},
		{"wrong type", func(c map[string]any) { c["issue_limit"] = "many" }, "scope"},
		{"empty trusted deletion", func(c map[string]any) { c["allow_empty_successor"] = true }, "scope"},
		{"zero limit", func(c map[string]any) { c["limits"] = map[string]any{"maxResultBytes": 0} }, "scope"},
		{"negative limit", func(c map[string]any) { c["limits"] = map[string]any{"maxRecordsPerClaim": -1} }, "scope"},
		{"limit over ceiling", func(c map[string]any) { c["limits"] = map[string]any{"claimTimeoutSeconds": 999999} }, "scope"},
		{"unknown limit", func(c map[string]any) { c["limits"] = map[string]any{"maxBytes": 1} }, "scope"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			config := validConfig()
			tc.mutate(config)
			_, err := ParseSettings(config, tc.scope, testEnv())
			if err == nil {
				t.Fatal("ParseSettings() error = nil, want rejection")
			}
			var configErr ConfigError
			if !errors.As(err, &configErr) {
				t.Fatalf("error %T is not a ConfigError", err)
			}
			for _, secret := range []string{testToken, "bot@fixture.invalid"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("error text leaks %q: %v", secret, err)
				}
			}
		})
	}
}

func TestTargetConfigNeverPrintsCredential(t *testing.T) {
	t.Parallel()

	settings, err := ParseSettings(validConfig(), "scope", testEnv())
	if err != nil {
		t.Fatalf("ParseSettings() error = %v", err)
	}
	for _, rendered := range []string{
		fmt.Sprint(settings.Target), fmt.Sprintf("%v", settings.Target), fmt.Sprintf("%+v", settings.Target),
		fmt.Sprintf("%#v", settings.Target), settings.Target.String(),
	} {
		if strings.Contains(rendered, testToken) || strings.Contains(rendered, "bot@fixture.invalid") {
			t.Fatalf("formatted target leaks a credential: %s", rendered)
		}
	}
}

func TestResourceUseDefaultsFillOnlyTheGaps(t *testing.T) {
	t.Parallel()

	got := ResourceUse{MaxRecordsPerClaim: 5}.withDefaults()
	defaults := DefaultResourceUse()
	if got.MaxRecordsPerClaim != 5 || got.MaxPayloadBytes != defaults.MaxPayloadBytes ||
		got.MaxResultBytes != defaults.MaxResultBytes || got.ClaimTimeoutSeconds != defaults.ClaimTimeoutSeconds {
		t.Fatalf("withDefaults() = %+v", got)
	}
	if (ResourceUse{}).withDefaults() != defaults {
		t.Fatal("zero ResourceUse must resolve to defaults, never to unbounded")
	}
}

func TestAllowEmptySuccessorFailsClosedWithItsOwnClass(t *testing.T) {
	t.Parallel()

	config := validConfig()
	config["allow_empty_successor"] = true
	_, err := ParseSettings(config, "scope", testEnv())
	var configErr ConfigError
	if !errors.As(err, &configErr) || configErr.FailureClass() != FailureUnsupportedOption {
		t.Fatalf("error = %v, want a ConfigError with class %s", err, FailureUnsupportedOption)
	}
	config["allow_empty_successor"] = false
	if _, err := ParseSettings(config, "scope", testEnv()); err != nil {
		t.Fatalf("explicit false error = %v, want accepted (the default)", err)
	}
	delete(config, "allow_empty_successor")
	if _, err := ParseSettings(config, "scope", testEnv()); err != nil {
		t.Fatalf("absent option error = %v", err)
	}
	if (ConfigError{}).FailureClass() != FailureInvalidConfig {
		t.Fatal("an ordinary config error must keep the invalid_config class")
	}
}
