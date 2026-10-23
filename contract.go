// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	sdk "github.com/eshu-hq/eshu/sdk/go/collector"
)

const (
	// ComponentID is the manifest identity of this collector. Keep it in sync
	// with metadata.id in manifest.yaml.
	ComponentID = "dev.eshu.collector.jira"
	// CollectorKind is the durable collector family name for Jira facts.
	CollectorKind = "jira"
	// SourceSystem is the source system name facts carry when the claim does
	// not name one. Hosted claims carry the activation's source system.
	SourceSystem = "jira"
	// MetricsPrefix is the component-owned metric prefix the manifest declares.
	// Nothing in this repository emits a metric under it yet; see the README
	// section "Operator signals".
	MetricsPrefix = "eshu_dp_jira_collector_"
	// ProviderJiraCloud selects Jira Cloud REST API collection.
	ProviderJiraCloud = "jira_cloud"
	// SchemaVersion is the schema version of every emitted work_item fact.
	SchemaVersion = "1.0.0"
)

// Core work-item fact kinds. They are core-owned: the host requires a
// producer grant for each before accepting them from this component.
const (
	// FactKindRecord identifies one provider work item.
	FactKindRecord = "work_item.record"
	// FactKindTransition identifies one changelog transition.
	FactKindTransition = "work_item.transition"
	// FactKindExternalLink identifies one remote link attached to a work item.
	FactKindExternalLink = "work_item.external_link"
	// FactKindProjectMetadata identifies one project definition.
	FactKindProjectMetadata = "work_item.project_metadata"
	// FactKindIssueTypeMetadata identifies one issue-type definition.
	FactKindIssueTypeMetadata = "work_item.issue_type_metadata"
	// FactKindStatusMetadata identifies one status definition.
	FactKindStatusMetadata = "work_item.status_metadata"
	// FactKindWorkflowMetadata identifies one workflow definition.
	FactKindWorkflowMetadata = "work_item.workflow_metadata"
	// FactKindFieldMetadata identifies one field definition.
	FactKindFieldMetadata = "work_item.field_metadata"
	// FactKindMetadataWarning identifies one metadata collection warning.
	FactKindMetadataWarning = "work_item.metadata_warning"
)

// FactKinds returns every fact kind this collector may emit, in the order the
// manifest declares them.
func FactKinds() []string {
	return []string{
		FactKindRecord,
		FactKindTransition,
		FactKindExternalLink,
		FactKindProjectMetadata,
		FactKindIssueTypeMetadata,
		FactKindStatusMetadata,
		FactKindWorkflowMetadata,
		FactKindFieldMetadata,
		FactKindMetadataWarning,
	}
}

// Contract returns the SDK fact families this collector may emit.
func Contract() sdk.Contract {
	kinds := FactKinds()
	facts := make([]sdk.FactDeclaration, 0, len(kinds))
	for _, kind := range kinds {
		facts = append(facts, sdk.FactDeclaration{
			Kind:             kind,
			SchemaVersions:   []string{SchemaVersion},
			SourceConfidence: []sdk.SourceConfidence{sdk.SourceConfidenceReported},
		})
	}
	return sdk.Contract{ProtocolVersion: sdk.ProtocolVersionV1Alpha1, Facts: facts}
}
