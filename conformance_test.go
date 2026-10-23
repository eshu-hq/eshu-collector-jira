// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/eshu-hq/eshu/sdk/go/collector"
	"github.com/eshu-hq/eshu/sdk/go/collector/conformance"
)

var updateConformanceFixtures = flag.Bool("update-conformance-fixtures", false,
	"rewrite testdata/conformance from the replay scenarios")

// conformanceFixtures maps each checked-in SDK result fixture to the replay
// scenario that produces it. The fixtures are what `eshu component conform`
// reads, so the CLI and these tests judge the same bytes.
var conformanceFixtures = []struct {
	file     string
	scenario string
	state    sdk.ResultState
}{
	{"complete.json", "basic", sdk.ResultComplete},
	{"partial.json", "degraded", sdk.ResultPartial},
	{"retryable.json", "failure_rate_limited", sdk.ResultRetryable},
	{"terminal.json", "failure_unauthorized", sdk.ResultTerminal},
}

func encodeFixture(t *testing.T, result sdk.Result) []byte {
	t.Helper()
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(encoded, '\n')
}

// TestConformanceFixturesAreCurrent keeps testdata/conformance byte-equal to
// what the handler produces now. Run with -update-conformance-fixtures after a
// deliberate output change, then read the diff.
func TestConformanceFixturesAreCurrent(t *testing.T) {
	for _, fixture := range conformanceFixtures {
		result, _ := collectScenario(t, fixture.scenario)
		want := encodeFixture(t, result)
		path := filepath.Join("testdata", "conformance", fixture.file)
		if *updateConformanceFixtures {
			if err := os.WriteFile(path, want, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v (run with -update-conformance-fixtures)", path, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s is stale; rerun with -update-conformance-fixtures and review the diff", fixture.file)
		}
	}
}

// TestConformanceHarnessPassesTheCheckedInFixtures runs the public SDK
// conformance harness over the fixture files exactly as the CLI reads them,
// with the published payload schemas the manifest references.
func TestConformanceHarnessPassesTheCheckedInFixtures(t *testing.T) {
	t.Parallel()

	manifest := loadManifest(t)
	var results []sdk.Result
	for _, fixture := range conformanceFixtures {
		raw, err := os.ReadFile(filepath.Join("testdata", "conformance", fixture.file))
		if err != nil {
			t.Fatal(err)
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		var result sdk.Result
		if err := decoder.Decode(&result); err != nil {
			t.Fatalf("%s: %v", fixture.file, err)
		}
		if result.State != fixture.state {
			t.Fatalf("%s: state = %q, want %q", fixture.file, result.State, fixture.state)
		}
		results = append(results, result)
	}
	report := conformance.Run(conformanceRequest(manifest, results...))
	if !report.OK() {
		t.Fatalf("conformance findings = %#v, want passed", report.Findings)
	}
	if !report.Summary.IdempotentReemissionChecked {
		t.Fatal("IdempotentReemissionChecked = false, want true")
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"status":"passed"`, `"component_id":"` + ComponentID + `"`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("conformance report JSON missing %q: %s", want, raw)
		}
	}
}

// TestConformanceRejectsAPayloadThatBreaksThePublishedSchema proves the
// harness is wired to the payload schemas, not a rubber stamp: dropping a
// required field from a real fact must fail with payload_schema_invalid.
func TestConformanceRejectsAPayloadThatBreaksThePublishedSchema(t *testing.T) {
	t.Parallel()

	result, _ := collectScenario(t, "basic")
	for i := range result.Facts {
		if result.Facts[i].Kind == FactKindRecord {
			delete(result.Facts[i].Payload, "provider")
			break
		}
	}
	report := conformance.Run(conformanceRequest(loadManifest(t), result))
	if report.OK() || !hasFinding(report, conformance.FindingPayloadSchemaInvalid) {
		t.Fatalf("findings = %#v, want payload_schema_invalid", report.Findings)
	}
}
