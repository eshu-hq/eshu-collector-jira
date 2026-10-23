// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"fmt"
	"sort"
	"strings"
	"time"

	sdk "github.com/eshu-hq/eshu/sdk/go/collector"
)

// FactContext carries the claim boundary fields for one Jira observation.
// The host, not this collector, derives the core fact envelope (fact ID,
// fencing token, collector kind) from the claim, so none of those appear here.
type FactContext struct {
	ScopeID             string
	GenerationID        string
	CollectorInstanceID string
	SourceSystem        string
	ObservedAt          time.Time
	SourceURI           string
}

func validateFactContext(ctx FactContext) error {
	if strings.TrimSpace(ctx.ScopeID) == "" {
		return fmt.Errorf("jira fact scope_id must not be blank")
	}
	if strings.TrimSpace(ctx.GenerationID) == "" {
		return fmt.Errorf("jira fact generation_id must not be blank")
	}
	if strings.TrimSpace(ctx.CollectorInstanceID) == "" {
		return fmt.Errorf("jira fact collector_instance_id must not be blank")
	}
	if strings.TrimSpace(ctx.SourceSystem) == "" {
		return fmt.Errorf("jira fact source_system must not be blank")
	}
	return nil
}

// workItemFact wraps a built payload in the public SDK fact envelope. The
// source URI is sanitized again here so no builder can leak credentials or
// sensitive query keys through source_ref.uri.
func workItemFact(
	ctx FactContext,
	factKind string,
	stableFactKey string,
	payload map[string]any,
	sourceRecordID string,
	sourceURI string,
) sdk.Fact {
	return sdk.Fact{
		Kind:             factKind,
		SchemaVersion:    SchemaVersion,
		StableKey:        stableFactKey,
		SourceConfidence: sdk.SourceConfidenceReported,
		ObservedAt:       ctx.ObservedAt.UTC(),
		SourceRef: sdk.SourceRef{
			SourceSystem: ctx.SourceSystem,
			ScopeID:      ctx.ScopeID,
			GenerationID: ctx.GenerationID,
			FactKey:      stableFactKey,
			URI:          sanitizeURL(sourceURI),
			RecordID:     strings.TrimSpace(sourceRecordID),
		},
		Payload:    payload,
		Redactions: redactionsFor(payload),
	}
}

// redactedBy maps a blanked payload field to the presence flag that proves the
// source carried a value for it.
var redactedBy = map[string]string{
	"assignee_account_id":   "assignee_present",
	"assignee_display_name": "assignee_present",
	"reporter_account_id":   "reporter_present",
	"reporter_display_name": "reporter_present",
	"author_account_id":     "author_present",
	"author_display_name":   "author_present",
	"from":                  "value_redacted",
	"to":                    "value_redacted",
}

// redactionsFor records every payload field the collector intentionally
// blanked while the source carried a value, so a reader can tell a redacted
// field from an absent one. The list is sorted for byte-stable output.
func redactionsFor(payload map[string]any) []sdk.Redaction {
	fields := make([]string, 0, 4)
	for key, value := range payload {
		text, ok := value.(string)
		if !ok || text != "" {
			continue
		}
		if fieldWasRedacted(payload, key) {
			fields = append(fields, key)
		}
	}
	if len(fields) == 0 {
		return nil
	}
	sort.Strings(fields)
	redactions := make([]sdk.Redaction, 0, len(fields))
	for _, field := range fields {
		redactions = append(redactions, sdk.Redaction{Field: field, Reason: redactionPolicyVersion})
	}
	return redactions
}

func fieldWasRedacted(payload map[string]any, key string) bool {
	if flag, ok := redactedBy[key]; ok {
		return payload[flag] == true
	}
	if payload[key+"_present"] == true {
		return true
	}
	fingerprint, _ := payload[key+"_fingerprint"].(string)
	return fingerprint != ""
}

// mergeContractPayload adds the keys the typed factschema encoder produces
// that the hand-built payload does not already carry, so the emitted payload
// validates against the published schema without changing existing keys.
func mergeContractPayload(payload map[string]any, encode func() (map[string]any, error)) error {
	encoded, err := encode()
	if err != nil {
		return err
	}
	for key, value := range encoded {
		if _, exists := payload[key]; !exists {
			payload[key] = value
		}
	}
	return nil
}

func stringPtr(value string) *string {
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}

func optionalStringPtrFromPayload(payload map[string]any, key string) *string {
	value, ok := payload[key].(string)
	if !ok {
		return nil
	}
	return &value
}
