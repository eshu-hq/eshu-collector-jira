// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

// Wire shapes for the Jira Cloud REST responses the client decodes.

type searchRequest struct {
	JQL           string   `json:"jql"`
	MaxResults    int      `json:"maxResults"`
	Fields        []string `json:"fields,omitempty"`
	Expand        string   `json:"expand,omitempty"`
	NextPageToken string   `json:"nextPageToken,omitempty"`
}

type searchResponse struct {
	Issues        []searchIssue `json:"issues"`
	NextPageToken string        `json:"nextPageToken"`
	IsLast        bool          `json:"isLast"`
}

type searchIssue struct {
	ID     string      `json:"id"`
	Key    string      `json:"key"`
	Self   string      `json:"self"`
	Fields issueFields `json:"fields"`
}

type issueFields struct {
	Summary        string    `json:"summary"`
	Created        string    `json:"created"`
	Updated        string    `json:"updated"`
	ResolutionDate string    `json:"resolutiondate"`
	IssueType      Reference `json:"issuetype"`
	Status         Reference `json:"status"`
	Project        Reference `json:"project"`
	Assignee       Reference `json:"assignee"`
	Reporter       Reference `json:"reporter"`
}

type changelogResponse struct {
	StartAt    int              `json:"startAt"`
	MaxResults int              `json:"maxResults"`
	Total      int              `json:"total"`
	IsLast     bool             `json:"isLast"`
	Values     []changelogEntry `json:"values"`
}

type changelogEntry struct {
	ID        string          `json:"id"`
	Author    Reference       `json:"author"`
	CreatedAt string          `json:"created"`
	Items     []changelogItem `json:"items"`
}

type changelogItem struct {
	Field     string `json:"field"`
	From      string `json:"fromString"`
	To        string `json:"toString"`
	FromID    string `json:"from"`
	ToID      string `json:"to"`
	FieldType string `json:"fieldtype"`
	FieldID   string `json:"fieldId"`
}

type remoteLinkResponse struct {
	ID           any             `json:"id"`
	GlobalID     string          `json:"globalId"`
	Application  LinkApplication `json:"application"`
	Relationship string          `json:"relationship"`
	Object       LinkObject      `json:"object"`
}

type jiraErrorResponse struct {
	ErrorMessages []string `json:"errorMessages"`
}
