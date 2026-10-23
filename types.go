// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"time"
)

// Reference is a bounded Jira identity/name object used in issue and user
// fields.
type Reference struct {
	ID          string `json:"id"`
	Key         string `json:"key"`
	Name        string `json:"name"`
	AccountID   string `json:"accountId"`
	DisplayName string `json:"displayName"`
}

// Issue is the Jira issue subset normalized by Eshu.
type Issue struct {
	ID         string
	Key        string
	Summary    string
	IssueType  Reference
	Status     Reference
	Project    Reference
	Assignee   Reference
	Reporter   Reference
	CreatedAt  time.Time
	UpdatedAt  time.Time
	ResolvedAt time.Time
	Self       string
	BrowseURL  string
}

// Transition is one Jira changelog item normalized as lifecycle evidence.
type Transition struct {
	ID             string
	IssueID        string
	IssueKey       string
	Field          string
	From           string
	To             string
	Author         Reference
	CreatedAt      time.Time
	ValueRedacted  bool
	AuthorRedacted bool
}

// LinkApplication identifies the external system attached to a Jira issue.
type LinkApplication struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// LinkObject identifies the remote object attached to a Jira issue.
type LinkObject struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
}

// ExternalLink is one Jira remote issue link normalized as cross-system
// evidence.
type ExternalLink struct {
	ID                   string
	IssueID              string
	IssueKey             string
	GlobalID             string
	Application          LinkApplication
	Relationship         string
	Object               LinkObject
	URLFingerprint       string
	URLRedacted          bool
	ProviderSupportState string
}

// MetadataScope identifies whether Jira metadata is global or scoped to a
// project.
type MetadataScope struct {
	Type      string
	ProjectID string
}

// ProjectMetadata is one Jira project definition normalized for source
// evidence without private project names or URLs.
type ProjectMetadata struct {
	ID              string
	Key             string
	Name            string
	TypeKey         string
	CategoryID      string
	CategoryName    string
	Style           string
	Archived        bool
	Deleted         bool
	Self            string
	LastIssueUpdate time.Time
	IssueCount      int
}

// IssueTypeMetadata is one Jira issue-type definition normalized for work-item
// context.
type IssueTypeMetadata struct {
	ID             string
	Name           string
	Description    string
	EntityID       string
	ProjectID      string
	Scope          MetadataScope
	HierarchyLevel int
	Subtask        bool
	Self           string
}

// StatusMetadata is one Jira workflow status definition normalized for
// work-item context.
type StatusMetadata struct {
	ID                string
	Name              string
	Description       string
	StatusCategory    string
	StatusCategoryKey string
	ProjectID         string
	Scope             MetadataScope
	Self              string
}

// WorkflowVersion identifies one provider workflow version.
type WorkflowVersion struct {
	ID     string
	Number int
}

// WorkflowStatusMetadata identifies one status reference inside a workflow
// definition.
type WorkflowStatusMetadata struct {
	StatusReference string
	StatusID        string
	Deprecated      bool
}

// WorkflowTransitionMetadata identifies one sanitized transition shape inside
// a workflow definition.
type WorkflowTransitionMetadata struct {
	ID                   string
	Name                 string
	Type                 string
	FromStatusReferences []string
	ToStatusReference    string
	HasValidators        bool
	HasTriggers          bool
	HasActions           bool
}

// WorkflowMetadata is one Jira workflow definition normalized for transition
// context without raw workflow or transition names.
type WorkflowMetadata struct {
	ID          string
	Name        string
	Description string
	Scope       MetadataScope
	Version     WorkflowVersion
	Statuses    []WorkflowStatusMetadata
	Transitions []WorkflowTransitionMetadata
}

// FieldSchema is the safe subset of a Jira field schema definition. CustomID
// records provider input presence only and is not emitted as a raw payload
// value.
type FieldSchema struct {
	Type     string
	Items    string
	System   string
	Custom   string
	CustomID string
}

// FieldMetadata is one Jira field definition normalized for work-item context.
type FieldMetadata struct {
	ID          string
	Name        string
	Description string
	Schema      FieldSchema
	Self        string
}

// MetadataWarning records a metadata collection state that must remain visible
// to readers instead of being confused with empty metadata.
type MetadataWarning struct {
	MetadataType string
	Reason       string
	FailureClass string
	ProviderID   string
}

// CollectionWindow bounds one Jira updated-window collection.
type CollectionWindow struct {
	Since time.Time
	Until time.Time
}

// CollectionResult is one bounded Jira work-item evidence fetch.
type CollectionResult struct {
	Issues           []Issue
	Transitions      map[string][]Transition
	ExternalLinks    map[string][]ExternalLink
	Projects         []ProjectMetadata
	IssueTypes       []IssueTypeMetadata
	Statuses         []StatusMetadata
	Workflows        []WorkflowMetadata
	Fields           []FieldMetadata
	MetadataWarnings []MetadataWarning
	ObservedAt       time.Time
	Stats            CollectionStats
	// IssueLimit is the effective issue cap the search applied after
	// normalization. It is part of the freshness identity: the same issues
	// under a wider limit are a different observation.
	IssueLimit int
	// SearchTruncated reports that more provider pages existed past
	// IssueLimit, so the issue set is capped rather than complete. It is
	// part of the freshness identity: a capped set must not skip as
	// unchanged against a complete one.
	SearchTruncated bool
}

// CollectionStats carries bounded collection counters for logs and tests.
type CollectionStats struct {
	SearchPages              int
	ChangelogPages           int
	RemoteLinkPages          int
	MetadataPages            int
	IssuesEmitted            int
	ChangelogEventsEmitted   int
	RemoteLinksEmitted       int
	RemoteLinksRejected      int
	MetadataObjectsScanned   int
	MetadataObjectsEmitted   int
	UnsupportedMetadata      int
	PermissionHiddenMetadata int
	StaleMetadata            int
	MetadataRedactions       int
	PartialFailures          int
	RateLimits               int
	RetryAfterSeconds        int
	StaleWindows             int
	UnsupportedProviderLinks int
}

// EvidenceClient fetches Jira work-item evidence for one target and time
// window.
type EvidenceClient interface {
	CollectWorkItemEvidence(context.Context, TargetConfig, CollectionWindow) (CollectionResult, error)
}

// PartialCollectionError preserves bounded collection counters when a Jira
// fetch fails after accepting part of a page set.
type PartialCollectionError struct {
	Stage string
	Stats CollectionStats
	Cause error
}

// Error returns a bounded failure string safe for logs and status.
func (e PartialCollectionError) Error() string {
	stage := e.Stage
	if stage == "" {
		stage = "collection"
	}
	return "jira partial collection failure: " + stage
}

// Unwrap returns the underlying provider or transport error.
func (e PartialCollectionError) Unwrap() error {
	return e.Cause
}
