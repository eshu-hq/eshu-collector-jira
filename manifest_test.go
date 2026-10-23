// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/sdk/go/factschema/fixturepack"
)

var digestPattern = regexp.MustCompile(`@sha256:[A-Fa-f0-9]{64}$`)

// placeholderDigestAllowed tolerates the all-zero artifact digest while the
// repository is pre-release. Set it to false in the change that records the
// first real image digest.
const placeholderDigestAllowed = true

// TestManifestReleaseProvenance proves releases are reproducible and the
// component manifest is valid: every artifact is digest-pinned, the manifest
// carries the supported core range and wire protocol, and every emitted fact
// family is versioned with declared confidence.
func TestManifestReleaseProvenance(t *testing.T) {
	t.Parallel()

	manifest := loadManifest(t)
	if strings.TrimSpace(manifest.APIVersion) == "" || strings.TrimSpace(manifest.Kind) == "" {
		t.Fatal("manifest missing apiVersion/kind")
	}
	if strings.TrimSpace(manifest.Metadata.ID) == "" || strings.TrimSpace(manifest.Metadata.Version) == "" {
		t.Fatal("manifest missing metadata id/version")
	}
	if manifest.Metadata.ID != ComponentID {
		t.Fatalf("manifest id = %q, want %q", manifest.Metadata.ID, ComponentID)
	}
	if len(manifest.Spec.Artifacts) == 0 {
		t.Fatal("manifest declares no artifacts")
	}
	for _, artifact := range manifest.Spec.Artifacts {
		if !digestPattern.MatchString(artifact.Image) {
			t.Fatalf("artifact image %q is not digest-pinned", artifact.Image)
		}
		if strings.Contains(artifact.Image, ":latest") {
			t.Fatalf("artifact image %q uses a mutable tag", artifact.Image)
		}
		// The all-zero digest matches the pin format but names no pullable
		// artifact. This repository is pre-release (eshu-hq/eshu#8858): no
		// image has been published yet, so the placeholder is tolerated only
		// while placeholderDigestAllowed is true. The release change that
		// records the real pushed digest via scripts/pin-digest.sh flips it
		// to false so the gate stays red on an unpinned manifest.
		if !placeholderDigestAllowed &&
			strings.HasSuffix(artifact.Image, "@sha256:"+strings.Repeat("0", 64)) {
			t.Fatalf("artifact image %q is the unpinned placeholder digest: run scripts/pin-digest.sh", artifact.Image)
		}
	}
	if len(manifest.Spec.EmittedFacts) == 0 {
		t.Fatal("manifest declares no emitted facts")
	}
	for _, family := range manifest.Spec.EmittedFacts {
		if strings.TrimSpace(family.Kind) == "" {
			t.Fatal("emitted fact kind empty")
		}
		if len(family.SchemaVersions) == 0 {
			t.Fatalf("fact kind %q has no schema versions", family.Kind)
		}
		if len(family.SourceConfidence) == 0 {
			t.Fatalf("fact kind %q has no source confidence", family.Kind)
		}
	}
	if len(manifest.Spec.ConsumerContracts.Reducer.Phases) == 0 {
		t.Fatal("manifest missing reducer consumer contract")
	}
	if strings.TrimSpace(manifest.Spec.Telemetry.MetricsPrefix) == "" {
		t.Fatal("manifest missing telemetry metrics prefix")
	}
}

// TestManifestDeclaresTheNineWorkItemKindsWithPublishedSchemas keeps the
// manifest, the contract the handler emits against, and the published payload
// schemas in step: every kind is declared once at schema 1.0.0 with reported
// confidence and a payloadSchemaRef that resolves in the pinned fixture pack.
func TestManifestDeclaresTheNineWorkItemKindsWithPublishedSchemas(t *testing.T) {
	t.Parallel()

	manifest := loadManifest(t)
	want := FactKinds()
	var got []string
	for _, family := range manifest.Spec.EmittedFacts {
		got = append(got, family.Kind)
		if strings.Join(family.SchemaVersions, ",") != SchemaVersion || strings.Join(family.SourceConfidence, ",") != "reported" {
			t.Errorf("%s: schemaVersions=%v sourceConfidence=%v", family.Kind, family.SchemaVersions, family.SourceConfidence)
		}
		if family.PayloadSchemaRef != family.Kind {
			t.Errorf("%s: payloadSchemaRef = %q, want the kind", family.Kind, family.PayloadSchemaRef)
		}
		if _, ok := fixturepack.SchemaFor(family.PayloadSchemaRef); !ok {
			t.Errorf("%s: pinned fixture pack has no schema %q", family.Kind, family.PayloadSchemaRef)
		}
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("manifest kinds %v, FactKinds() %v", got, want)
	}
	if len(manifest.Spec.CollectorKinds) != 1 || manifest.Spec.CollectorKinds[0] != CollectorKind {
		t.Fatalf("collectorKinds = %v, want [%s]", manifest.Spec.CollectorKinds, CollectorKind)
	}
	contract := manifest.Contract()
	if !reflect.DeepEqual(contract, Contract()) {
		t.Fatalf("manifest contract differs from Contract():\n%+v\n%+v", contract, Contract())
	}
}
