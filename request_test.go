// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"encoding/json"
	"errors"
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

// TestParseRequestEnforcesTheStdinCapOnAValidRequest uses an otherwise valid
// request padded to the cap, so it is rejected for size alone: without the
// read limit a padded request is accepted, and a request that fails some other
// check proves nothing about the cap.
func TestParseRequestEnforcesTheStdinCapOnAValidRequest(t *testing.T) {
	t.Parallel()

	padded := func(padding int) string {
		return requestJSON(t, func(doc map[string]any) {
			doc["config"].(map[string]any)["pad"] = strings.Repeat("x", padding)
		})
	}
	base := len(padded(0))
	atCap := padded(MaxRequestBytes - base)
	if len(atCap) != MaxRequestBytes {
		t.Fatalf("test request is %d bytes, want exactly %d", len(atCap), MaxRequestBytes)
	}
	if _, err := ParseRequest(strings.NewReader(atCap)); err != nil {
		t.Fatalf("a valid request of exactly %d bytes error = %v, want accepted", MaxRequestBytes, err)
	}
	overCap := padded(MaxRequestBytes - base + 1)
	_, err := ParseRequest(strings.NewReader(overCap))
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("a valid request of %d bytes error = %v, want a size rejection", len(overCap), err)
	}
	if strings.Contains(err.Error(), "xxxx") {
		t.Fatal("the size error must not quote the input")
	}
}

// countingReader serves limit bytes of 'x', then fails, and counts what was
// read, so a parser that does not bound its read is caught without hanging.
type countingReader struct {
	read  int
	limit int
}

func (c *countingReader) Read(p []byte) (int, error) {
	if c.read >= c.limit {
		return 0, errors.New("test reader exhausted: the parser read past its bound")
	}
	n := min(len(p), c.limit-c.read)
	for i := range n {
		p[i] = 'x'
	}
	c.read += n
	return n, nil
}

func TestParseRequestStopsReadingAtTheCap(t *testing.T) {
	t.Parallel()

	reader := &countingReader{limit: 8 * MaxRequestBytes}
	_, err := ParseRequest(reader)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("error = %v, want a size rejection", err)
	}
	if reader.read > MaxRequestBytes+1 {
		t.Fatalf("read %d bytes, want at most %d (the cap plus one)", reader.read, MaxRequestBytes+1)
	}
}
