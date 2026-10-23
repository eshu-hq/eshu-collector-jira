// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu-collector-jira/internal/stableid"
	sdk "github.com/eshu-hq/eshu/sdk/go/collector"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	workitemv1 "github.com/eshu-hq/eshu/sdk/go/factschema/workitem/v1"
)

// buildRecordFact converts one Jira issue into a source fact.
func buildRecordFact(ctx FactContext, issue Issue) (sdk.Fact, error) {
	if err := validateFactContext(ctx); err != nil {
		return sdk.Fact{}, err
	}
	issueID := strings.TrimSpace(issue.ID)
	issueKey := strings.TrimSpace(issue.Key)
	if issueID == "" || issueKey == "" {
		return sdk.Fact{}, fmt.Errorf("jira issue id and key are required")
	}
	stableFactKey := stableid.StableID(FactKindRecord, map[string]any{
		"provider":  ProviderJiraCloud,
		"scope_id":  ctx.ScopeID,
		"issue_id":  issueID,
		"issue_key": issueKey,
	})
	selfURL := sanitizeURL(issue.Self)
	sourceURL := sanitizeURL(firstNonBlank(issue.BrowseURL, issue.Self, ctx.SourceURI))
	payload := map[string]any{
		"collector_instance_id":    ctx.CollectorInstanceID,
		"provider":                 ProviderJiraCloud,
		"redaction_policy_version": redactionPolicyVersion,
		"provider_work_item_id":    issueID,
		"work_item_key":            issueKey,
		"summary":                  "",
		"summary_present":          strings.TrimSpace(issue.Summary) != "",
		"issue_type_id":            strings.TrimSpace(issue.IssueType.ID),
		"issue_type_name":          strings.TrimSpace(issue.IssueType.Name),
		"status_id":                strings.TrimSpace(issue.Status.ID),
		"status_name":              strings.TrimSpace(issue.Status.Name),
		"project_id":               strings.TrimSpace(issue.Project.ID),
		"project_key":              strings.TrimSpace(issue.Project.Key),
		"project_name":             "",
		"project_name_present":     strings.TrimSpace(issue.Project.Name) != "",
		"assignee_account_id":      "",
		"assignee_display_name":    "",
		"assignee_present":         referencePresent(issue.Assignee),
		"reporter_account_id":      "",
		"reporter_display_name":    "",
		"reporter_present":         referencePresent(issue.Reporter),
		"created_at":               formatTime(issue.CreatedAt),
		"updated_at":               formatTime(issue.UpdatedAt),
		"resolved_at":              formatTime(issue.ResolvedAt),
		"self_url":                 "",
		"self_url_fingerprint":     urlFingerprint(selfURL),
		"source_url":               "",
		"source_url_fingerprint":   urlFingerprint(sourceURL),
	}
	if err := mergeContractPayload(payload, func() (map[string]any, error) {
		return factschema.EncodeWorkItemRecord(workitemv1.WorkItemRecord{
			Provider:               ProviderJiraCloud,
			ProviderWorkItemID:     issueID,
			WorkItemKey:            issueKey,
			RedactionPolicyVersion: stringPtr(redactionPolicyVersion),
			Summary:                stringPtr(""),
			SummaryPresent:         boolPtr(strings.TrimSpace(issue.Summary) != ""),
			IssueTypeID:            stringPtr(strings.TrimSpace(issue.IssueType.ID)),
			IssueTypeName:          stringPtr(strings.TrimSpace(issue.IssueType.Name)),
			StatusID:               stringPtr(strings.TrimSpace(issue.Status.ID)),
			StatusName:             stringPtr(strings.TrimSpace(issue.Status.Name)),
			ProjectID:              stringPtr(strings.TrimSpace(issue.Project.ID)),
			ProjectKey:             stringPtr(strings.TrimSpace(issue.Project.Key)),
			ProjectName:            stringPtr(""),
			ProjectNamePresent:     boolPtr(strings.TrimSpace(issue.Project.Name) != ""),
			AssigneeAccountID:      stringPtr(""),
			AssigneeDisplayName:    stringPtr(""),
			AssigneePresent:        boolPtr(referencePresent(issue.Assignee)),
			ReporterAccountID:      stringPtr(""),
			ReporterDisplayName:    stringPtr(""),
			ReporterPresent:        boolPtr(referencePresent(issue.Reporter)),
			CreatedAt:              stringPtr(formatTime(issue.CreatedAt)),
			UpdatedAt:              stringPtr(formatTime(issue.UpdatedAt)),
			ResolvedAt:             stringPtr(formatTime(issue.ResolvedAt)),
			SelfURL:                stringPtr(""),
			SelfURLFingerprint:     stringPtr(urlFingerprint(selfURL)),
			SourceURL:              stringPtr(""),
			SourceURLFingerprint:   stringPtr(urlFingerprint(sourceURL)),
			CollectorInstanceID:    stringPtr(ctx.CollectorInstanceID),
		})
	}); err != nil {
		return sdk.Fact{}, err
	}
	return workItemFact(ctx, FactKindRecord, stableFactKey, payload, issueID, ctx.SourceURI), nil
}

// buildTransitionFact converts one Jira changelog item into a source
// fact.
func buildTransitionFact(ctx FactContext, transition Transition) (sdk.Fact, error) {
	if err := validateFactContext(ctx); err != nil {
		return sdk.Fact{}, err
	}
	changelogID := strings.TrimSpace(transition.ID)
	if changelogID == "" {
		return sdk.Fact{}, fmt.Errorf("jira changelog id is required")
	}
	stableFactKey := stableid.StableID(FactKindTransition, map[string]any{
		"provider":     ProviderJiraCloud,
		"scope_id":     ctx.ScopeID,
		"issue_id":     strings.TrimSpace(transition.IssueID),
		"changelog_id": changelogID,
		"field":        strings.TrimSpace(transition.Field),
	})
	from, to := transitionValues(transition)
	payload := map[string]any{
		"collector_instance_id":    ctx.CollectorInstanceID,
		"provider":                 ProviderJiraCloud,
		"redaction_policy_version": redactionPolicyVersion,
		"provider_changelog_id":    changelogID,
		"provider_work_item_id":    strings.TrimSpace(transition.IssueID),
		"work_item_key":            strings.TrimSpace(transition.IssueKey),
		"field":                    strings.TrimSpace(transition.Field),
		"from":                     from,
		"to":                       to,
		"value_redacted":           transition.ValueRedacted,
		"author_account_id":        "",
		"author_display_name":      "",
		"author_present":           referencePresent(transition.Author),
		"author_redacted":          transition.AuthorRedacted || referencePresent(transition.Author),
		"created_at":               formatTime(transition.CreatedAt),
	}
	if err := mergeContractPayload(payload, func() (map[string]any, error) {
		return factschema.EncodeWorkItemTransition(workitemv1.WorkItemTransition{
			Provider:               ProviderJiraCloud,
			ProviderChangelogID:    changelogID,
			RedactionPolicyVersion: stringPtr(redactionPolicyVersion),
			ProviderWorkItemID:     stringPtr(strings.TrimSpace(transition.IssueID)),
			WorkItemKey:            stringPtr(strings.TrimSpace(transition.IssueKey)),
			Field:                  stringPtr(strings.TrimSpace(transition.Field)),
			From:                   stringPtr(from),
			To:                     stringPtr(to),
			ValueRedacted:          boolPtr(transition.ValueRedacted),
			AuthorAccountID:        stringPtr(""),
			AuthorDisplayName:      stringPtr(""),
			AuthorPresent:          boolPtr(referencePresent(transition.Author)),
			AuthorRedacted:         boolPtr(transition.AuthorRedacted || referencePresent(transition.Author)),
			CreatedAt:              stringPtr(formatTime(transition.CreatedAt)),
			CollectorInstanceID:    stringPtr(ctx.CollectorInstanceID),
		})
	}); err != nil {
		return sdk.Fact{}, err
	}
	return workItemFact(ctx, FactKindTransition, stableFactKey, payload, changelogID, ctx.SourceURI), nil
}

// buildExternalLinkFact converts one Jira remote link into a source
// fact.
func buildExternalLinkFact(ctx FactContext, link ExternalLink) (sdk.Fact, error) {
	if err := validateFactContext(ctx); err != nil {
		return sdk.Fact{}, err
	}
	sanitizedURL := sanitizeURL(link.Object.URL)
	fingerprint := firstNonBlank(link.URLFingerprint, urlFingerprint(sanitizedURL))
	recordID := firstNonBlank(link.ID, link.GlobalID, fingerprint)
	if recordID == "" {
		return sdk.Fact{}, fmt.Errorf("jira remote link id, global_id, or url is required")
	}
	stableFactKey := stableid.StableID(FactKindExternalLink, map[string]any{
		"provider":       ProviderJiraCloud,
		"scope_id":       ctx.ScopeID,
		"issue_id":       strings.TrimSpace(link.IssueID),
		"remote_link_id": recordID,
	})
	payload := map[string]any{
		"collector_instance_id":    ctx.CollectorInstanceID,
		"provider":                 ProviderJiraCloud,
		"redaction_policy_version": redactionPolicyVersion,
		"provider_remote_link_id":  strings.TrimSpace(link.ID),
		"provider_work_item_id":    strings.TrimSpace(link.IssueID),
		"work_item_key":            strings.TrimSpace(link.IssueKey),
		"global_id":                strings.TrimSpace(link.GlobalID),
		"application_name":         strings.TrimSpace(link.Application.Name),
		"application_type":         strings.TrimSpace(link.Application.Type),
		"relationship":             strings.TrimSpace(link.Relationship),
		"url":                      "",
		"url_present":              strings.TrimSpace(link.Object.URL) != "",
		"url_fingerprint":          fingerprint,
		"url_redacted":             link.URLRedacted || strings.TrimSpace(link.Object.URL) != "",
		"title":                    "",
		"title_present":            strings.TrimSpace(link.Object.Title) != "",
		"summary":                  "",
		"summary_present":          strings.TrimSpace(link.Object.Summary) != "",
		"correlation_anchor_class": externalLinkAnchorClass(link),
		"provider_support_state":   externalLinkSupportState(link),
	}
	// Resolve the typed PR/MR URL to a canonical repository id BEFORE the raw
	// URL is dropped above. Persist the durable id only on a confident
	// canonicalization; ambiguous or non-repository links omit the field
	// entirely rather than store a guessed id. The raw URL stays redacted.
	if repoID := linkedRepositoryID(link); repoID != "" {
		payload["linked_repository_id"] = repoID
	}
	if err := mergeContractPayload(payload, func() (map[string]any, error) {
		return factschema.EncodeWorkItemExternalLink(workitemv1.WorkItemExternalLink{
			Provider:               ProviderJiraCloud,
			RedactionPolicyVersion: stringPtr(redactionPolicyVersion),
			ProviderRemoteLinkID:   stringPtr(strings.TrimSpace(link.ID)),
			ProviderWorkItemID:     stringPtr(strings.TrimSpace(link.IssueID)),
			WorkItemKey:            stringPtr(strings.TrimSpace(link.IssueKey)),
			GlobalID:               stringPtr(strings.TrimSpace(link.GlobalID)),
			ApplicationName:        stringPtr(strings.TrimSpace(link.Application.Name)),
			ApplicationType:        stringPtr(strings.TrimSpace(link.Application.Type)),
			Relationship:           stringPtr(strings.TrimSpace(link.Relationship)),
			URL:                    stringPtr(""),
			URLPresent:             boolPtr(strings.TrimSpace(link.Object.URL) != ""),
			URLFingerprint:         stringPtr(fingerprint),
			URLRedacted:            boolPtr(link.URLRedacted || strings.TrimSpace(link.Object.URL) != ""),
			Title:                  stringPtr(""),
			TitlePresent:           boolPtr(strings.TrimSpace(link.Object.Title) != ""),
			Summary:                stringPtr(""),
			SummaryPresent:         boolPtr(strings.TrimSpace(link.Object.Summary) != ""),
			AnchorClass:            stringPtr(externalLinkAnchorClass(link)),
			ProviderSupportState:   stringPtr(externalLinkSupportState(link)),
			LinkedRepositoryID:     optionalStringPtrFromPayload(payload, "linked_repository_id"),
			CollectorInstanceID:    stringPtr(ctx.CollectorInstanceID),
		})
	}); err != nil {
		return sdk.Fact{}, err
	}
	return workItemFact(ctx, FactKindExternalLink, stableFactKey, payload, recordID, ctx.SourceURI), nil
}
