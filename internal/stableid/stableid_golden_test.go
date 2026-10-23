// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package stableid

import (
	"testing"
	"time"
)

// Golden values below were produced by the in-tree function
// facts.StableID in eshu-hq/eshu (origin/main ce050339cf) with a throwaway
// generator test. A mismatch means this copy no longer keys facts the way the
// in-tree collector does, which would re-key evidence at cutover.

var stableIDGolden = []struct {
	name     string
	factType string
	identity map[string]any
	want     string
}{
	{"record", "work_item.record", map[string]any{"provider": "jira_cloud", "scope_id": "jira-scope", "issue_id": "10001", "issue_key": "ESHU-1"}, "19bec387809f7309be9954f200f78fca1d0e5856c4adf794d449a20f8689a036"},
	{"transition", "work_item.transition", map[string]any{"provider": "jira_cloud", "scope_id": "jira-scope", "issue_id": "10001", "changelog_id": "555", "field": "status"}, "7ceaf0d4bcdd520700500799a549980b4f7379c28026e3e83b13e9fbfccb68b9"},
	{"html_unicode", "WorkItemFact", map[string]any{"a": "<b>&amp;</b>", "u": "café ☃ \U0001F600", "ctl": "line\nbreak\ttab"}, "c88892df272f6b291f957f64e437500a266d041ff6118fbf19775c6a8201cac1"},
	{"nested", "jira.collection.freshness", map[string]any{"envelopes": []string{"b", "a"}, "issue_limit": 50, "search_truncated": true, "since": "2026-10-01T00:00:00Z", "n": map[string]any{"k": []any{1, "x", map[string]any{"z": false}}}}, "d7fe222547234a7315b96f917d29bc4274ade81867d4522914c8063c79c254c5"},
	{"time_value", "T", map[string]any{"at": time.Date(2026, 10, 1, 12, 30, 45, 123456789, time.FixedZone("x", 3600*5)), "list": []any{time.Date(2026, 10, 1, 12, 30, 45, 123456789, time.FixedZone("x", 3600*5))}}, "71424113a073f8ab6adf9fc92f1864b97457ee6af84cfa2697b25ada4e2103f1"},
	{"empty", "", map[string]any{}, "c879d9fc6684c7042e1f18f40581da87611cdf955f8a7a288f5f36615eab68d5"},
	{"nil_identity", "N", nil, "0c9ef4afc44774d721e654731d87734dc698e5567172632a6712702cce28b10e"},
}

func TestStableIDMatchesInTreeGolden(t *testing.T) {
	t.Parallel()
	for _, tc := range stableIDGolden {
		if got := StableID(tc.factType, tc.identity); got != tc.want {
			t.Errorf("%s: StableID() = %s, want %s", tc.name, got, tc.want)
		}
	}
}

func TestStableIDIsIndependentOfMapOrderAndNormalizesTime(t *testing.T) {
	t.Parallel()
	loc := time.FixedZone("x", 5*3600)
	local := map[string]any{"at": time.Date(2026, 10, 1, 17, 0, 0, 0, loc), "k": "v"}
	utc := map[string]any{"k": "v", "at": time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
	if StableID("T", local) != StableID("T", utc) {
		t.Fatal("equal instants in different zones must share a stable id")
	}
	if StableID("T", utc) == StableID("U", utc) {
		t.Fatal("fact type must be part of the identity")
	}
}
