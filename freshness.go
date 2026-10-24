// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/eshu-hq/eshu-collector-jira/internal/stableid"
	sdk "github.com/eshu-hq/eshu/sdk/go/collector"
)

// freshnessHint returns the stable content digest for one Jira collection
// (#7926 in the in-tree collector): the collection window bounds plus the
// sorted digests of the facts the collection actually emitted, the applied
// issue cap, whether the issue search truncated past that cap, and the
// completeness counters.
//
// Each entry covers its fact kind, stable key, and full payload, so same-key
// content drift (a status flip, a new transition, a new remote link, drifted
// metadata) mints a new generation instead of skipping as unchanged. It never
// covers the raw provider body. Claim and transport metadata (generation ID,
// fencing token, observed_at) stay out, so a byte-identical retry shares the
// hint and the host's store skips the duplicate.
//
// The hint is computed over the facts before duplicate stable keys collapse,
// exactly as the in-tree collector computes it over its envelopes.
func freshnessHint(window CollectionWindow, result CollectionResult, facts []sdk.Fact) string {
	encoded := make([]string, 0, len(facts))
	for _, fact := range facts {
		entry := map[string]any{
			"fact_kind":  fact.Kind,
			"payload":    fact.Payload,
			"stable_key": fact.StableKey,
		}
		raw, err := json.Marshal(entry)
		if err != nil {
			// Fail open on purpose: a blank hint disables duplicate
			// detection, so the generation always stores instead of risking
			// a skipped write. That costs one missed dedup, never lost
			// evidence.
			return ""
		}
		encoded = append(encoded, string(raw))
	}
	sort.Strings(encoded)
	return stableid.StableID("jira.collection.freshness", map[string]any{
		"envelopes":         encoded,
		"issue_limit":       result.IssueLimit,
		"metadata_warnings": len(result.MetadataWarnings),
		"partial_failures":  result.Stats.PartialFailures,
		"search_truncated":  result.SearchTruncated,
		"since":             window.Since.UTC().Format(time.RFC3339Nano),
		"stale_windows":     result.Stats.StaleWindows,
		"until":             window.Until.UTC().Format(time.RFC3339Nano),
	})
}
