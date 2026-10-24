// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

// producerGrant is the shape of a core producer grant request. Core issues the
// grant; this repository stores only the request an operator submits.
type producerGrant struct {
	ProducerID     string    `json:"producer_id"`
	Version        string    `json:"version"`
	Kind           string    `json:"kind"`
	SchemaVersions []string  `json:"schema_versions"`
	Scope          string    `json:"scope"`
	ExpiresAt      time.Time `json:"expires_at"`
	Revoked        bool      `json:"revoked"`
}

// TestGrantExampleCoversExactlyTheManifestKinds proves the grant request an
// operator submits matches the manifest: one grant per declared core kind,
// bound to this producer and version, scoped to the manifest's collector kind
// (core matches a grant's scope against the declared collector kinds), and
// covering every declared schema version.
func TestGrantExampleCoversExactlyTheManifestKinds(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("grant.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var grants []producerGrant
	if err := json.Unmarshal(raw, &grants); err != nil {
		t.Fatalf("grant.example.json: %v", err)
	}
	manifest := loadManifest(t)
	declared := map[string][]string{}
	for _, family := range manifest.Spec.EmittedFacts {
		declared[family.Kind] = family.SchemaVersions
	}
	got := map[string][]string{}
	for _, grant := range grants {
		if grant.ProducerID != manifest.Metadata.ID || grant.Version != manifest.Metadata.Version {
			t.Fatalf("grant %+v is not bound to %s@%s", grant, manifest.Metadata.ID, manifest.Metadata.Version)
		}
		if grant.Scope != manifest.Spec.CollectorKinds[0] || grant.Revoked || grant.ExpiresAt.IsZero() {
			t.Fatalf("grant %+v must be live and scoped to the collector kind %q", grant, manifest.Spec.CollectorKinds[0])
		}
		if _, dup := got[grant.Kind]; dup {
			t.Fatalf("grant kind %s appears twice", grant.Kind)
		}
		got[grant.Kind] = grant.SchemaVersions
	}
	if !reflect.DeepEqual(got, declared) {
		t.Fatalf("grants cover %v, manifest declares %v", got, declared)
	}
	for kind := range declared {
		if !strings.HasPrefix(kind, "work_item.") {
			t.Fatalf("kind %q is not a core work_item kind", kind)
		}
	}
}

// TestRecordGrantsScriptListsEveryManifestKind keeps scripts/record-grants.sh
// from drifting away from the manifest.
func TestRecordGrantsScriptListsEveryManifestKind(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("scripts/record-grants.sh")
	if err != nil {
		t.Fatal(err)
	}
	kindPattern := regexp.MustCompile(`work_item\.[a-z_]+`)
	var inScript []string
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		inScript = append(inScript, kindPattern.FindAllString(line, -1)...)
	}
	var declared []string
	for _, family := range loadManifest(t).Spec.EmittedFacts {
		declared = append(declared, family.Kind)
	}
	sort.Strings(inScript)
	sort.Strings(declared)
	if !reflect.DeepEqual(inScript, declared) {
		t.Fatalf("record-grants.sh kinds %v, manifest kinds %v", inScript, declared)
	}
	if !strings.Contains(string(raw), "--scope jira") {
		t.Fatal("grants must be scoped to the collector kind")
	}
}
