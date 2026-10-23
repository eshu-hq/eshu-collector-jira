// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const redactionPolicyVersion = "jira_work_item_v1"

func externalLinkAnchorClass(link ExternalLink) string {
	value := strings.ToLower(firstNonBlank(link.Application.Type, link.Application.Name, link.Object.URL))
	switch {
	case strings.Contains(value, "github") && strings.Contains(strings.ToLower(link.Object.URL), "/pull/"):
		return "github_pull_request"
	case strings.Contains(value, "gitlab") && strings.Contains(strings.ToLower(link.Object.URL), "/merge_requests/"):
		return "gitlab_merge_request"
	default:
		return "remote_link"
	}
}

func externalLinkSupportState(link ExternalLink) string {
	if strings.TrimSpace(link.ProviderSupportState) != "" {
		return strings.TrimSpace(link.ProviderSupportState)
	}
	switch externalLinkAnchorClass(link) {
	case "github_pull_request", "gitlab_merge_request":
		return "supported_provider"
	default:
		return "unsupported_provider"
	}
}

func transitionValues(transition Transition) (string, string) {
	if transition.ValueRedacted {
		return "", ""
	}
	return strings.TrimSpace(transition.From), strings.TrimSpace(transition.To)
}

func referencePresent(ref Reference) bool {
	return strings.TrimSpace(ref.ID) != "" ||
		strings.TrimSpace(ref.Key) != "" ||
		strings.TrimSpace(ref.Name) != "" ||
		strings.TrimSpace(ref.AccountID) != "" ||
		strings.TrimSpace(ref.DisplayName) != ""
}

// sanitizeURL drops credentials, sensitive query keys, and fragments. A value
// that is not an absolute URL sanitizes to the empty string.
func sanitizeURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	parsed.User = nil
	query := parsed.Query()
	for key := range query {
		if sensitiveQueryKey(key) {
			query.Del(key)
		}
	}
	parsed.RawQuery = query.Encode()
	parsed.Fragment = ""
	return parsed.String()
}

func sensitiveQueryKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "access_token", "api_key", "apikey", "auth", "authorization", "jwt",
		"key", "password", "passwd", "secret", "sig", "signature", "token":
		return true
	default:
		return false
	}
}

func urlFingerprint(sanitized string) string {
	trimmed := strings.TrimSpace(sanitized)
	if trimmed == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(trimmed))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func textFingerprint(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(trimmed))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func anyString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	default:
		return ""
	}
}
