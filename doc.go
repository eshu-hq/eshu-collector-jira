// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package collector is the Eshu Jira work-item collector, extracted from the
// Eshu core repository (eshu-hq/eshu#8858).
//
// It runs through the Eshu component-extension host with the process runner:
// the host passes one bounded collector-sdk/v1alpha1 request on stdin and the
// collector returns one result on stdout. A claim names a Jira site scope; the
// collector reads changed issues, changelogs, remote links, and project,
// issue-type, status, workflow, and field metadata through bounded Jira Cloud
// REST calls, and emits the nine core work_item.* fact kinds. It is
// pre-release and holds no producer authority until its reviewed cutover.
//
// Boundary: this package imports only the published SDK modules
// (sdk/go/collector, sdk/go/factschema), the standard library, and its own
// internal helpers. It never imports go/internal, another collector
// implementation, or any core Postgres, graph, queue, or lease handle; the
// import-boundary test enforces this. The host, not this package, builds the
// core fact envelope, validates the result, and owns claims and commits.
//
// Evidence is redacted before emission: summaries, display names, account
// IDs, URLs, and titles are replaced by presence flags and fingerprints.
// Credentials are read from environment variables named in the activation
// config and never enter facts, source URIs, logs, or statuses.
package collector
