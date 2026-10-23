// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const defaultUpdatedLookback = 24 * time.Hour

// ConfigError reports an invalid activation config. Its text names config keys
// and environment variable names only; it never carries a resolved secret.
type ConfigError struct {
	msg   string
	class string
}

// Error returns the bounded configuration failure text.
func (e ConfigError) Error() string { return e.msg }

// FailureClass returns the bounded status class for this rejection:
// unsupported_option for a recognized option this build cannot honor,
// invalid_config for everything else.
func (e ConfigError) FailureClass() string {
	if e.class != "" {
		return e.class
	}
	return FailureInvalidConfig
}

func configErrorf(format string, args ...any) error {
	return ConfigError{msg: fmt.Sprintf(format, args...)}
}

// TargetConfig describes one Jira Cloud site target.
type TargetConfig struct {
	Provider        string
	ScopeID         string
	SiteID          string
	BaseURL         string
	Email           string
	Token           string
	JQL             string
	IssueLimit      int
	UpdatedLookback time.Duration
	ChangelogLimit  int
	RemoteLinkLimit int
	MetadataLimit   int
}

// String returns a redacted summary so a stray %v or %s never prints the
// credential.
func (t TargetConfig) String() string {
	return fmt.Sprintf("jira target scope=%s site=%s", t.ScopeID, t.SiteID)
}

// GoString keeps %#v from printing the credential too.
func (t TargetConfig) GoString() string { return t.String() }

// Settings is the validated per-claim configuration.
type Settings struct {
	Target TargetConfig
	Limits ResourceUse
}

// activationConfig is the activation `config` map for one Jira target. The
// field names mirror the in-tree collector's per-target JSON.
type activationConfig struct {
	Provider        string `json:"provider"`
	ScopeID         string `json:"scope_id"`
	SiteID          string `json:"site_id"`
	BaseURL         string `json:"base_url"`
	EmailEnv        string `json:"email_env"`
	TokenEnv        string `json:"token_env"`
	JQL             string `json:"jql"`
	JQLEnv          string `json:"jql_env"`
	IssueLimit      int    `json:"issue_limit"`
	UpdatedLookback string `json:"updated_lookback"`
	ChangelogLimit  int    `json:"changelog_limit"`
	RemoteLinkLimit int    `json:"remote_link_limit"`
	MetadataLimit   int    `json:"metadata_limit"`
	// AllowEmptySuccessor is accepted only as false: the published SDK
	// result type cannot carry the trusted-deletion attestation yet.
	AllowEmptySuccessor bool          `json:"allow_empty_successor"`
	Limits              *limitsConfig `json:"limits"`
}

type limitsConfig struct {
	MaxRecordsPerClaim  *int `json:"maxRecordsPerClaim"`
	MaxPayloadBytes     *int `json:"maxPayloadBytes"`
	MaxResultBytes      *int `json:"maxResultBytes"`
	ClaimTimeoutSeconds *int `json:"claimTimeoutSeconds"`
}

// ParseSettings validates the activation config for the claimed scope. getenv
// resolves the credential environment variables the config names. Unknown
// keys fail closed so a misspelled limit cannot be silently ignored.
func ParseSettings(raw map[string]any, claimScopeID string, getenv func(string) string) (Settings, error) {
	var cfg activationConfig
	encoded, err := json.Marshal(raw)
	if err != nil {
		return Settings{}, configErrorf("jira collector config is not JSON serializable")
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return Settings{}, configErrorf("decode jira collector config: %s", decodeReason(err))
	}
	if cfg.AllowEmptySuccessor {
		// Fail closed. Sending the attestation needs Generation.TrustedDeletion,
		// which landed in eshu-hq/eshu after sdk/go/collector v0.2.0; this
		// repository does not hand-marshal unpublished wire fields. Remove
		// this branch when a collector SDK release carrying TrustedDeletion
		// is the pinned version.
		return Settings{}, ConfigError{
			msg:   "allow_empty_successor is not supported: the pinned collector SDK cannot carry the trusted-deletion attestation",
			class: FailureUnsupportedOption,
		}
	}
	target, err := buildTarget(cfg, claimScopeID, getenv)
	if err != nil {
		return Settings{}, err
	}
	limits, err := buildLimits(cfg.Limits)
	if err != nil {
		return Settings{}, err
	}
	return Settings{Target: target, Limits: limits}, nil
}

// decodeReason keeps a JSON decode failure to its reason without echoing
// decoded values.
func decodeReason(err error) string {
	var typeErr *json.UnmarshalTypeError
	switch {
	case errors.As(err, &typeErr):
		return "field " + typeErr.Field + " has the wrong type"
	case strings.HasPrefix(err.Error(), "json: unknown field"):
		return strings.TrimPrefix(err.Error(), "json: ")
	default:
		return "malformed JSON"
	}
}

func buildTarget(cfg activationConfig, claimScopeID string, getenv func(string) string) (TargetConfig, error) {
	provider := strings.TrimSpace(cfg.Provider)
	if provider == "" {
		provider = ProviderJiraCloud
	}
	if provider != ProviderJiraCloud {
		return TargetConfig{}, configErrorf("unsupported jira provider %q", provider)
	}
	scopeID := strings.TrimSpace(claimScopeID)
	if scopeID == "" {
		return TargetConfig{}, configErrorf("claim scope id is required")
	}
	if configured := strings.TrimSpace(cfg.ScopeID); configured != "" && configured != scopeID {
		return TargetConfig{}, configErrorf("jira target scope_id is not configured for this claim scope")
	}
	siteID := strings.TrimSpace(cfg.SiteID)
	if siteID == "" {
		return TargetConfig{}, configErrorf("site_id is required")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://" + siteID
	}
	tokenEnv := strings.TrimSpace(cfg.TokenEnv)
	token := ""
	if tokenEnv != "" {
		token = strings.TrimSpace(getenv(tokenEnv))
	}
	if token == "" {
		return TargetConfig{}, configErrorf("token_env %s did not resolve a credential", tokenEnv)
	}
	email := ""
	if emailEnv := strings.TrimSpace(cfg.EmailEnv); emailEnv != "" {
		email = strings.TrimSpace(getenv(emailEnv))
	}
	lookback, err := parseLookback(cfg.UpdatedLookback)
	if err != nil {
		return TargetConfig{}, err
	}
	jql, err := resolveJQL(cfg, getenv)
	if err != nil {
		return TargetConfig{}, err
	}
	return TargetConfig{
		Provider:        provider,
		ScopeID:         scopeID,
		SiteID:          siteID,
		BaseURL:         baseURL,
		Email:           email,
		Token:           token,
		JQL:             jql,
		IssueLimit:      cfg.IssueLimit,
		UpdatedLookback: lookback,
		ChangelogLimit:  cfg.ChangelogLimit,
		RemoteLinkLimit: cfg.RemoteLinkLimit,
		MetadataLimit:   cfg.MetadataLimit,
	}, nil
}

func resolveJQL(cfg activationConfig, getenv func(string) string) (string, error) {
	direct := strings.TrimSpace(cfg.JQL)
	jqlEnv := strings.TrimSpace(cfg.JQLEnv)
	switch {
	case direct != "" && jqlEnv != "":
		return "", configErrorf("only one of jql or jql_env may be set")
	case direct != "":
		return direct, nil
	case jqlEnv != "":
		jql := strings.TrimSpace(getenv(jqlEnv))
		if jql == "" {
			return "", configErrorf("jql_env %s did not resolve a JQL query", jqlEnv)
		}
		return jql, nil
	default:
		return "", configErrorf("jql or jql_env is required")
	}
}

func parseLookback(raw string) (time.Duration, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return defaultUpdatedLookback, nil
	}
	value, err := time.ParseDuration(trimmed)
	if err != nil {
		return 0, configErrorf("parse updated_lookback: not a valid duration")
	}
	if value < 0 {
		return 0, configErrorf("updated lookback must be positive")
	}
	if value == 0 {
		return defaultUpdatedLookback, nil
	}
	return value, nil
}

func buildLimits(cfg *limitsConfig) (ResourceUse, error) {
	limits := DefaultResourceUse()
	if cfg == nil {
		return limits, nil
	}
	for _, field := range []struct {
		name    string
		value   *int
		ceiling int
		target  *int
	}{
		{"maxRecordsPerClaim", cfg.MaxRecordsPerClaim, maxRecordsCeiling, &limits.MaxRecordsPerClaim},
		{"maxPayloadBytes", cfg.MaxPayloadBytes, maxPayloadCeiling, &limits.MaxPayloadBytes},
		{"maxResultBytes", cfg.MaxResultBytes, maxResultCeiling, &limits.MaxResultBytes},
		{"claimTimeoutSeconds", cfg.ClaimTimeoutSeconds, maxTimeoutCeiling, &limits.ClaimTimeoutSeconds},
	} {
		if field.value == nil {
			continue
		}
		if *field.value <= 0 || *field.value > field.ceiling {
			return ResourceUse{}, configErrorf("limits.%s must be a positive integer no greater than %d", field.name, field.ceiling)
		}
		*field.target = *field.value
	}
	return limits, nil
}
