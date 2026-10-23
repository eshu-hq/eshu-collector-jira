// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package repoid derives the canonical repository identifier that Jira
// external links carry as linked_repository_id.
//
// COPIED CODE: NormalizeRemoteURL and CanonicalRepositoryID are copies of the
// Eshu core functions in go/internal/repositoryidentity (eshu-hq/eshu), which
// is not importable from a standalone collector. The identifier must match
// what core stores for every repository, so the golden table in
// golden_table_test.go pins this copy against values produced by the in-tree
// functions. Replace this package with a published eshu-sdk helper when one
// exists (request it on eshu-hq/eshu#8851, alongside the stable-id and HTTP
// helpers).
package repoid

import (
	"crypto/sha1" // #nosec G505 -- non-cryptographic repository identity digest, not a security primitive
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
)

// NormalizeRemoteURL normalizes SSH and HTTPS git remotes into canonical HTTPS.
func NormalizeRemoteURL(remoteURL string) string {
	candidate := strings.TrimSpace(remoteURL)
	if candidate == "" {
		return ""
	}

	host := ""
	path := ""
	switch {
	case strings.HasPrefix(candidate, "git@") && strings.Contains(candidate, ":"):
		remainder := strings.TrimPrefix(candidate, "git@")
		parts := strings.SplitN(remainder, ":", 2)
		if len(parts) == 2 {
			host = parts[0]
			path = parts[1]
		}
	case strings.HasPrefix(candidate, "ssh://"), strings.Contains(candidate, "://"):
		parsed, err := url.Parse(candidate)
		if err == nil {
			host = parsed.Hostname()
			path = parsed.Path
		}
	}

	if host == "" || path == "" {
		return strings.TrimRight(candidate, "/")
	}

	cleanPath := strings.Trim(path, "/")
	cleanPath = strings.TrimSuffix(cleanPath, ".git")
	cleanPath = strings.ToLower(strings.Join(strings.FieldsFunc(cleanPath, func(r rune) bool {
		return r == '/'
	}), "/"))
	if cleanPath == "" {
		return ""
	}

	return fmt.Sprintf("https://%s/%s", strings.ToLower(host), cleanPath)
}

// CanonicalRepositoryID returns the canonical repository identifier,
// "repository:r_<8-hex>": the first 32 bits of the SHA-1 over the normalized
// remote URL, or over the local path when no remote is available.
func CanonicalRepositoryID(remoteURL string, localPath string) (string, error) {
	identity := NormalizeRemoteURL(remoteURL)
	if identity == "" {
		if strings.TrimSpace(localPath) == "" {
			return "", fmt.Errorf("local path is required when remote url is not available")
		}
		identity = localPath
	}

	sum := sha1.Sum([]byte(identity)) // #nosec G401 -- non-cryptographic repository identity digest, not a security primitive
	return fmt.Sprintf("repository:r_%s", hex.EncodeToString(sum[:])[:8]), nil
}
