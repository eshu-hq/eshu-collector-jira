#!/usr/bin/env bash
# Records the nine core producer grants this collector needs, one per
# work_item.* kind, in a component home. The grant scope is the manifest's
# collector kind ("jira"), not a site: core binds a grant to the kinds the
# manifest declares. Core issues grants; run this only against a home you are
# authorized to grant in, after the grant request is approved.
#
#   COMPONENT_HOME=/path/to/home ESHU=eshu scripts/record-grants.sh [version] [lifetime]
set -euo pipefail
: "${COMPONENT_HOME:?COMPONENT_HOME is not set: no grants recorded}"
eshu="${ESHU:-eshu}"
version="${1:-0.1.0}"
lifetime="${2:-720h}"
for kind in \
  work_item.record work_item.transition work_item.external_link \
  work_item.project_metadata work_item.issue_type_metadata work_item.status_metadata \
  work_item.workflow_metadata work_item.field_metadata work_item.metadata_warning; do
  "$eshu" component grant dev.eshu.collector.jira \
    --kind "$kind" --schema-version 1.0.0 --scope jira \
    --version "$version" --expires-in "$lifetime" \
    --component-home "$COMPONENT_HOME"
done
