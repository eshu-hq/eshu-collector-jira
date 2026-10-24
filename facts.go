// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	sdk "github.com/eshu-hq/eshu/sdk/go/collector"
)

// buildFacts converts one collection into SDK facts in the same order the
// in-tree collector emitted envelopes: project, issue-type, status, workflow,
// and field metadata, metadata warnings, then each issue followed by its
// transitions and external links.
func buildFacts(ctx FactContext, result CollectionResult) ([]sdk.Fact, error) {
	facts := make([]sdk.Fact, 0,
		len(result.Issues)+len(result.Projects)+len(result.IssueTypes)+
			len(result.Statuses)+len(result.Workflows)+len(result.Fields)+len(result.MetadataWarnings))
	for _, project := range result.Projects {
		fact, err := buildProjectMetadataFact(ctx, project)
		if err != nil {
			return nil, err
		}
		facts = append(facts, fact)
	}
	for _, issueType := range result.IssueTypes {
		fact, err := buildIssueTypeMetadataFact(ctx, issueType)
		if err != nil {
			return nil, err
		}
		facts = append(facts, fact)
	}
	for _, status := range result.Statuses {
		fact, err := buildStatusMetadataFact(ctx, status)
		if err != nil {
			return nil, err
		}
		facts = append(facts, fact)
	}
	for _, workflow := range result.Workflows {
		fact, err := buildWorkflowMetadataFact(ctx, workflow)
		if err != nil {
			return nil, err
		}
		facts = append(facts, fact)
	}
	for _, field := range result.Fields {
		fact, err := buildFieldMetadataFact(ctx, field)
		if err != nil {
			return nil, err
		}
		facts = append(facts, fact)
	}
	for _, warning := range result.MetadataWarnings {
		fact, err := buildMetadataWarningFact(ctx, warning)
		if err != nil {
			return nil, err
		}
		facts = append(facts, fact)
	}
	for _, issue := range result.Issues {
		record, err := buildRecordFact(ctx, issue)
		if err != nil {
			return nil, err
		}
		facts = append(facts, record)
		for _, transition := range result.Transitions[issue.ID] {
			fact, err := buildTransitionFact(ctx, transition)
			if err != nil {
				return nil, err
			}
			facts = append(facts, fact)
		}
		for _, link := range result.ExternalLinks[issue.ID] {
			fact, err := buildExternalLinkFact(ctx, link)
			if err != nil {
				return nil, err
			}
			facts = append(facts, fact)
		}
	}
	return facts, nil
}

// dedupeFacts collapses facts that share a kind and stable key, keeping the
// last occurrence at the position of the first. The in-tree store resolves a
// repeated fact ID the same way (the last write wins at equal fencing
// tokens), and the host validator rejects a conflicting duplicate outright, so
// collapsing here keeps one changelog entry with two items for the same field
// from failing the whole claim.
func dedupeFacts(facts []sdk.Fact) []sdk.Fact {
	index := make(map[string]int, len(facts))
	out := make([]sdk.Fact, 0, len(facts))
	for _, fact := range facts {
		key := fact.Kind + "\x00" + fact.StableKey
		if at, ok := index[key]; ok {
			out[at] = fact
			continue
		}
		index[key] = len(out)
		out = append(out, fact)
	}
	return out
}
