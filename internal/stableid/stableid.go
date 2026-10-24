// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package stableid derives the deterministic identifiers that name Jira
// facts.
//
// COPIED CODE: this is a byte-for-byte copy of the unexported-to-this-module
// Eshu core function facts.StableID (go/internal/facts/encode/stable.go in
// eshu-hq/eshu). Stable keys must match the in-tree collector exactly so a
// cutover does not re-key evidence. Replace this package with the published
// eshu-sdk helper when eshu-hq/eshu#8851 lands. Until then,
// stableid_golden_test.go pins the output against values produced by the
// in-tree function.
package stableid

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// StableID returns a deterministic identifier for one fact-adjacent payload.
// It hashes the fact type together with the normalized identity map, so the
// same identity yields the same id on every collector run, host, and replay.
func StableID(factType string, identity map[string]any) string {
	record := map[string]any{
		"fact_type": factType,
		"identity":  normalizeStableValue(identity),
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		panic(fmt.Sprintf("marshal stable id payload: %v", err))
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func normalizeStableValue(value any) any {
	switch typed := value.(type) {
	case time.Time:
		return typed.UTC().Format(time.RFC3339Nano)
	case map[string]any:
		cloned := make(map[string]any, len(typed))
		for key, nested := range typed {
			cloned[key] = normalizeStableValue(nested)
		}
		return cloned
	case []any:
		cloned := make([]any, len(typed))
		for i := range typed {
			cloned[i] = normalizeStableValue(typed[i])
		}
		return cloned
	case []string:
		cloned := make([]any, len(typed))
		for i := range typed {
			cloned[i] = typed[i]
		}
		return cloned
	default:
		return typed
	}
}
