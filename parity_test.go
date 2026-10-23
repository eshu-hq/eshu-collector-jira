// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	sdk "github.com/eshu-hq/eshu/sdk/go/collector"
)

// goldenRun is the in-tree collector's output for one replay scenario,
// recorded with testdata/golden/intree_golden_generator.go.txt (see
// testdata/README.md for the reproduction command).
type goldenRun struct {
	Scenario string `json:"scenario"`
	Outcome  string `json:"outcome"`
	Scope    struct {
		ScopeID       string `json:"scope_id"`
		ScopeKind     string `json:"scope_kind"`
		SourceSystem  string `json:"source_system"`
		CollectorKind string `json:"collector_kind"`
	} `json:"scope"`
	Generation struct {
		GenerationID    string `json:"generation_id"`
		ObservedAt      string `json:"observed_at"`
		FreshnessHint   string `json:"freshness_hint"`
		TrustedDeletion bool   `json:"trusted_deletion"`
	} `json:"generation"`
	Facts   []goldenFact `json:"facts"`
	Failure *struct {
		Class          string `json:"class"`
		Terminal       bool   `json:"terminal"`
		Operation      string `json:"operation"`
		RetryAfterSecs int    `json:"retry_after_seconds"`
	} `json:"failure"`
}

type goldenFact struct {
	Kind             string         `json:"kind"`
	StableKey        string         `json:"stable_key"`
	SchemaVersion    string         `json:"schema_version"`
	SourceConfidence string         `json:"source_confidence"`
	CollectorKind    string         `json:"collector_kind"`
	ObservedAt       string         `json:"observed_at"`
	SourceSystem     string         `json:"source_system"`
	SourceScopeID    string         `json:"source_scope_id"`
	SourceURI        string         `json:"source_uri"`
	SourceRecordID   string         `json:"source_record_id"`
	IsTombstone      bool           `json:"is_tombstone"`
	Payload          map[string]any `json:"payload"`
}

func loadGolden(t *testing.T, scenario string) goldenRun {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "golden", scenario+".golden.json"))
	if err != nil {
		t.Fatalf("read golden %s: %v", scenario, err)
	}
	var run goldenRun
	if err := json.Unmarshal(raw, &run); err != nil {
		t.Fatalf("decode golden %s: %v", scenario, err)
	}
	return run
}

func scenarioNames(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("testdata", "source", "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no replay scenarios found: %v", err)
	}
	names := make([]string, 0, len(files))
	for _, file := range files {
		names = append(names, strings.TrimSuffix(filepath.Base(file), ".json"))
	}
	sort.Strings(names)
	return names
}

// normalizeJSON round-trips a payload through JSON so both sides compare as a
// JSON consumer reads them (numbers as float64, slices as []any).
func normalizeJSON(t *testing.T, value any) any {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(encoded, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// dedupeGolden applies the store's last-write-wins resolution of a repeated
// fact id, which the extracted handler applies itself before emission.
func dedupeGolden(facts []goldenFact) []goldenFact {
	index := map[string]int{}
	out := make([]goldenFact, 0, len(facts))
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

// TestParityWithInTreeCollector replays every recorded Jira site through the
// extracted handler and compares it with what the in-tree collector produced
// for the same HTTP responses: kind, stable key, payload, scope, generation,
// and freshness hint for successes; class, terminality, and retry guidance for
// failures. FactID, fencing token, and the partition key are host-owned and
// differ by design, so they are not compared.
func TestParityWithInTreeCollector(t *testing.T) {
	t.Parallel()

	for _, name := range scenarioNames(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			golden := loadGolden(t, name)
			result, transport := collectScenario(t, name)
			if golden.Outcome == "failure" {
				compareFailure(t, golden, result)
				return
			}
			mustNotMiss(t, transport)
			compareGeneration(t, golden, result)
		})
	}
}

func compareFailure(t *testing.T, golden goldenRun, result sdk.Result) {
	t.Helper()
	if golden.Failure == nil {
		t.Fatal("golden failure has no failure block")
	}
	wantState := sdk.ResultRetryable
	if golden.Failure.Terminal {
		wantState = sdk.ResultTerminal
	}
	if result.State != wantState || len(result.Facts) != 0 {
		t.Fatalf("State=%q facts=%d, want %q with none (in-tree terminal=%v)", result.State, len(result.Facts), wantState, golden.Failure.Terminal)
	}
	status := result.Statuses[0]
	if status.FailureClass != golden.Failure.Class {
		t.Fatalf("failure class = %q, in-tree %q", status.FailureClass, golden.Failure.Class)
	}
	if golden.Failure.RetryAfterSecs > 0 && status.RetryAfterSeconds != golden.Failure.RetryAfterSecs {
		t.Fatalf("retry_after = %d, in-tree provider guidance %d", status.RetryAfterSeconds, golden.Failure.RetryAfterSecs)
	}
	if !golden.Failure.Terminal && status.RetryAfterSeconds < 1 {
		t.Fatalf("a retryable result needs retry_after_seconds, got %d", status.RetryAfterSeconds)
	}
}

func compareGeneration(t *testing.T, golden goldenRun, result sdk.Result) {
	t.Helper()
	claim := testClaim()
	if golden.Scope.ScopeID != claim.Scope.ID || golden.Scope.ScopeKind != claim.Scope.Kind ||
		golden.Scope.SourceSystem != claim.SourceSystem || golden.Scope.CollectorKind != claim.CollectorKind {
		t.Fatalf("scope differs: in-tree %+v, claim %+v", golden.Scope, claim)
	}
	if result.Claim != claim || result.Generation.ID != golden.Generation.GenerationID {
		t.Fatalf("generation id = %q, in-tree %q", result.Generation.ID, golden.Generation.GenerationID)
	}
	if got := result.Generation.ObservedAt.UTC().Format(time.RFC3339Nano); got != golden.Generation.ObservedAt {
		t.Fatalf("generation observed_at = %s, in-tree %s", got, golden.Generation.ObservedAt)
	}
	if golden.Generation.TrustedDeletion {
		t.Fatal("golden attests trusted deletion; the extracted collector cannot")
	}
	if result.Generation.FreshnessHint != golden.Generation.FreshnessHint {
		t.Fatalf("freshness hint = %s, in-tree %s", result.Generation.FreshnessHint, golden.Generation.FreshnessHint)
	}
	want := dedupeGolden(golden.Facts)
	if len(result.Facts) != len(want) {
		t.Fatalf("fact count = %d, in-tree %d (after duplicate-key collapse)", len(result.Facts), len(want))
	}
	hasWarning := false
	for i, fact := range result.Facts {
		expected := want[i]
		label := fact.Kind + " " + expected.SourceRecordID
		if fact.Kind != expected.Kind || fact.StableKey != expected.StableKey {
			t.Fatalf("fact %d = %s %s, in-tree %s %s", i, fact.Kind, fact.StableKey, expected.Kind, expected.StableKey)
		}
		if fact.SchemaVersion != expected.SchemaVersion || string(fact.SourceConfidence) != expected.SourceConfidence ||
			claim.CollectorKind != expected.CollectorKind || fact.Tombstone != expected.IsTombstone {
			t.Fatalf("%s: schema/confidence/collector/tombstone differ from in-tree %+v", label, expected)
		}
		if got := fact.ObservedAt.UTC().Format(time.RFC3339Nano); got != expected.ObservedAt {
			t.Fatalf("%s: observed_at = %s, in-tree %s", label, got, expected.ObservedAt)
		}
		ref := fact.SourceRef
		if ref.SourceSystem != expected.SourceSystem || ref.ScopeID != expected.SourceScopeID || ref.URI != expected.SourceURI ||
			ref.RecordID != expected.SourceRecordID || ref.FactKey != expected.StableKey || ref.GenerationID != golden.Generation.GenerationID {
			t.Fatalf("%s: source_ref %+v differs from in-tree %+v", label, ref, expected)
		}
		if got := normalizeJSON(t, fact.Payload); !reflect.DeepEqual(got, normalizeJSON(t, expected.Payload)) {
			t.Fatalf("%s: payload differs from in-tree\n got: %v\nwant: %v", label, got, expected.Payload)
		}
		if fact.Kind == FactKindMetadataWarning {
			hasWarning = true
		}
	}
	wantState := sdk.ResultComplete
	if hasWarning {
		wantState = sdk.ResultPartial
	}
	if result.State != wantState {
		t.Fatalf("State = %q, want %q", result.State, wantState)
	}
}

// TestParityCoversEveryFactKind keeps the parity corpus honest: the replay
// scenarios together must exercise all nine kinds and the failure classes the
// collector can return, so a kind or class cannot ship unproven.
func TestParityCoversEveryFactKind(t *testing.T) {
	t.Parallel()

	compared := map[string]int{}
	classes := map[string]bool{}
	for _, name := range scenarioNames(t) {
		golden := loadGolden(t, name)
		for _, fact := range dedupeGolden(golden.Facts) {
			compared[fact.Kind]++
		}
		if golden.Failure != nil {
			classes[golden.Failure.Class] = true
		}
	}
	for _, kind := range FactKinds() {
		if compared[kind] == 0 {
			t.Errorf("no parity scenario produces %s", kind)
		}
	}
	for _, class := range []string{"permission_hidden", "deleted", "archived", "rate_limited", "retryable", "terminal"} {
		if !classes[class] {
			t.Errorf("no parity scenario produces failure class %s", class)
		}
	}
	t.Logf("compared facts per kind across scenarios: %v", compared)
}

// TestRecordPayloadKeysMatchTheDemoCassette compares the payload key set of an
// emitted work_item.record with the key set in the repository's recorded demo
// cassette (a facts replay, not an HTTP recording, so it cannot drive the
// collector; it can still pin the payload shape downstream demos expect).
func TestRecordPayloadKeysMatchTheDemoCassette(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join("testdata", "cassettes", "supply-chain-demo.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cassette struct {
		Scopes []struct {
			Facts []struct {
				Kind    string         `json:"fact_kind"`
				Payload map[string]any `json:"payload"`
			} `json:"facts"`
		} `json:"scopes"`
	}
	if err := json.Unmarshal(raw, &cassette); err != nil {
		t.Fatal(err)
	}
	var demo map[string]any
	for _, scope := range cassette.Scopes {
		for _, fact := range scope.Facts {
			if fact.Kind == FactKindRecord {
				demo = fact.Payload
			}
		}
	}
	if demo == nil {
		t.Fatal("cassette holds no work_item.record")
	}
	result, _ := collectScenario(t, "basic")
	var emitted map[string]any
	for _, fact := range result.Facts {
		if fact.Kind == FactKindRecord {
			emitted = fact.Payload
			break
		}
	}
	keys := func(m map[string]any) []string {
		out := make([]string, 0, len(m))
		for key := range m {
			out = append(out, key)
		}
		sort.Strings(out)
		return out
	}
	if !reflect.DeepEqual(keys(emitted), keys(demo)) {
		t.Fatalf("record payload keys differ from the demo cassette\n got: %v\nwant: %v", keys(emitted), keys(demo))
	}
}

// TestFixtureClockStaysInsideTheInTreeStaleWindow fails with a pointer to the
// fix before the goldens go stale. The in-tree collector decides whether a
// collection window is stale against the real clock, so the recorded freshness
// hints only match while every scenario's fixed clock is newer than
// now-minus-lookback. The scenarios use a ten-year lookback; regenerate the
// fixtures and goldens with a newer fixed clock before that window closes.
func TestFixtureClockStaysInsideTheInTreeStaleWindow(t *testing.T) {
	t.Parallel()

	for _, name := range scenarioNames(t) {
		scenario := loadScenario(t, name)
		lookback, err := time.ParseDuration(scenario.Config["updated_lookback"].(string))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !scenario.Now.After(time.Now().Add(-lookback)) {
			t.Fatalf("%s: fixture clock %s is older than the lookback window; regenerate testdata/source and testdata/golden (see testdata/README.md)", name, scenario.Now)
		}
	}
}
