// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"encoding/json"
	"strings"
	"testing"
)

func requestJSON(t *testing.T, mutate func(map[string]any)) string {
	t.Helper()
	raw, err := json.Marshal(Request{ProtocolVersion: "collector-sdk/v1alpha1", Claim: testClaim(), Contract: Contract(), Config: validConfig()})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if mutate != nil {
		mutate(doc)
	}
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestParseRequestAcceptsHostRequest(t *testing.T) {
	t.Parallel()

	request, err := ParseRequest(strings.NewReader(requestJSON(t, nil)))
	if err != nil {
		t.Fatalf("ParseRequest() error = %v", err)
	}
	if request.Claim.Scope.ID != testClaim().Scope.ID || request.Config["site_id"] != "example.atlassian.net" {
		t.Fatalf("request = %+v", request)
	}
}

func TestParseRequestRejectsBadInput(t *testing.T) {
	t.Parallel()

	claimMutation := func(field, value string) func(map[string]any) {
		return func(doc map[string]any) {
			claim := doc["claim"].(map[string]any)
			if field == "scope.id" {
				claim["scope"].(map[string]any)["id"] = value
				return
			}
			claim[field] = value
		}
	}
	cases := map[string]string{
		"empty":            "",
		"not json":         "{not json",
		"trailing value":   requestJSON(t, nil) + `{"x":1}`,
		"unknown field":    requestJSON(t, func(d map[string]any) { d["extra"] = true }),
		"wrong protocol":   requestJSON(t, func(d map[string]any) { d["protocol_version"] = "collector-sdk/v9" }),
		"missing scope id": requestJSON(t, claimMutation("scope.id", "")),
		"blank generation": requestJSON(t, claimMutation("generation_id", " ")),
		"blank instance":   requestJSON(t, claimMutation("instance_id", "")),
		"other collector":  requestJSON(t, claimMutation("collector_kind", "confluence")),
		"oversized":        `{"protocol_version":"` + strings.Repeat("x", MaxRequestBytes) + `"}`,
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseRequest(strings.NewReader(input))
			if err == nil {
				t.Fatal("ParseRequest() error = nil, want rejection")
			}
			if len(err.Error()) > 200 {
				t.Fatalf("error text is not bounded: %d bytes", len(err.Error()))
			}
		})
	}
}
