// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	sdk "github.com/eshu-hq/eshu/sdk/go/collector"
	"github.com/eshu-hq/eshu/sdk/go/collector/conformance"
	"github.com/eshu-hq/eshu/sdk/go/factschema/fixturepack"
)

func testObservedAt() time.Time {
	return time.Date(2026, time.October, 1, 12, 0, 0, 0, time.UTC)
}

// testClaim is a host-shaped claim for the fixture Jira site. Its scope id and
// instance id are the values the parity goldens were recorded with.
func testClaim() sdk.Claim {
	return sdk.Claim{
		ComponentID:   ComponentID,
		InstanceID:    "jira-qa",
		CollectorKind: CollectorKind,
		SourceSystem:  SourceSystem,
		Scope:         sdk.Scope{ID: "jira:site:fixture", Kind: "jira_site"},
		SourceRunID:   "run-1",
		GenerationID:  "generation-1",
		WorkItemID:    "work-1",
		FencingToken:  "7",
		Attempt:       1,
		Deadline:      testObservedAt().Add(time.Hour),
		ConfigHandle:  "component-config:jira",
	}
}

func loadManifest(t testing.TB) conformance.Manifest {
	t.Helper()
	raw, err := os.ReadFile("manifest.yaml")
	if err != nil {
		t.Fatalf("os.ReadFile(manifest.yaml) error = %v", err)
	}
	var manifest conformance.Manifest
	if err := yaml.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("yaml.Unmarshal(manifest.yaml) error = %v", err)
	}
	return manifest
}

func hasFinding(report conformance.Report, code conformance.FindingCode) bool {
	for _, finding := range report.Findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}

// collectScenario runs one replay scenario through the handler.
func collectScenario(t testing.TB, name string) (sdk.Result, *replayTransport) {
	t.Helper()
	scenario := loadScenario(t, name)
	handler, transport, _ := scenario.handlerFor()
	return handler.Handle(t.Context(), scenario.requestFor()), transport
}

// conformanceRequest builds the public fixture-mode conformance request for a
// manifest, supplying the published payload schema each declared
// payloadSchemaRef names so payload shape is checked against the schemas the
// core host validates with.
func conformanceRequest(manifest conformance.Manifest, results ...sdk.Result) conformance.Request {
	schemas := map[string]json.RawMessage{}
	for _, family := range manifest.Spec.EmittedFacts {
		if family.PayloadSchemaRef == "" {
			continue
		}
		if raw, ok := fixturepack.SchemaFor(family.PayloadSchemaRef); ok {
			schemas[family.PayloadSchemaRef] = raw
		}
	}
	return conformance.Request{Manifest: manifest, Fixtures: results, Mode: conformance.ModeFixture, PayloadSchemas: schemas}
}
