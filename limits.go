// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

// ResourceUse reports and enforces the bounded cost of one claim. Every
// non-positive field selects its DefaultResourceUse value, so a partially
// customized value keeps the caller's settings and only the gaps fall back to
// defaults.
type ResourceUse struct {
	// MaxRecordsPerClaim bounds one claim's fact emission.
	MaxRecordsPerClaim int `json:"max_records_per_claim"`
	// MaxPayloadBytes bounds one emitted fact payload.
	MaxPayloadBytes int `json:"max_payload_bytes"`
	// MaxResultBytes bounds the encoded result written to stdout. It must
	// stay below the host's process stdout limit (4 MiB unless the activation
	// sets process.stdout_limit_bytes): the collector fails terminal with a
	// named class instead of letting the host drop an oversized result.
	MaxResultBytes int `json:"max_result_bytes"`
	// ClaimTimeoutSeconds bounds one claim's wall time.
	ClaimTimeoutSeconds int `json:"claim_timeout_seconds"`
}

const (
	maxRecordsCeiling = 1_000_000
	maxPayloadCeiling = 16 << 20
	maxResultCeiling  = 256 << 20
	maxTimeoutCeiling = 3600
)

// DefaultResourceUse is the starting bound. The result bound sits under the
// host's default 4 MiB stdout limit; a site with larger evidence raises both
// limits.maxResultBytes and the activation's process.stdout_limit_bytes.
func DefaultResourceUse() ResourceUse {
	return ResourceUse{
		MaxRecordsPerClaim:  50_000,
		MaxPayloadBytes:     1 << 20,
		MaxResultBytes:      3 << 20,
		ClaimTimeoutSeconds: 300,
	}
}

// withDefaults fills every non-positive field from DefaultResourceUse.
func (r ResourceUse) withDefaults() ResourceUse {
	defaults := DefaultResourceUse()
	if r.MaxRecordsPerClaim <= 0 {
		r.MaxRecordsPerClaim = defaults.MaxRecordsPerClaim
	}
	if r.MaxPayloadBytes <= 0 {
		r.MaxPayloadBytes = defaults.MaxPayloadBytes
	}
	if r.MaxResultBytes <= 0 {
		r.MaxResultBytes = defaults.MaxResultBytes
	}
	if r.ClaimTimeoutSeconds <= 0 {
		r.ClaimTimeoutSeconds = defaults.ClaimTimeoutSeconds
	}
	return r
}
