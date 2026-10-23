// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repoid

import "testing"

// Golden values below were produced by repositoryidentity in eshu-hq/eshu
// (origin/main ce050339cf) with a throwaway generator test.

var normalizeRemoteGolden = []struct {
	in   string
	want string
}{
	{"https://github.com/Eshu-HQ/Eshu.git", "https://github.com/eshu-hq/eshu"},
	{"https://github.com/eshu-hq/eshu", "https://github.com/eshu-hq/eshu"},
	{"git@github.com:Eshu-HQ/eshu.git", "https://github.com/eshu-hq/eshu"},
	{"ssh://git@gitlab.example.com/Group/Sub/Repo.git", "https://gitlab.example.com/group/sub/repo"},
	{"https://gitlab.example.com/group/sub/repo/", "https://gitlab.example.com/group/sub/repo"},
	{"https://github.com:443/a/b", "https://github.com/a/b"},
	{"https://GitHub.com//a///b.git", "https://github.com/a/b"},
	{"not a url", "not a url"},
	{"", ""},
	{"   ", ""},
	{"https://host", "https://host"},
	{"git@host:", "git@host:"},
	{"https://user:pw@github.com/a/b.git", "https://github.com/a/b"},
}

var canonicalRepoGolden = []struct {
	remote  string
	local   string
	want    string
	wantErr bool
}{
	{"https://github.com/Eshu-HQ/Eshu.git", "", "repository:r_008329d6", false},
	{"https://github.com/Eshu-HQ/Eshu.git", "/tmp/x", "repository:r_008329d6", false},
	{"https://github.com/eshu-hq/eshu", "", "repository:r_008329d6", false},
	{"https://github.com/eshu-hq/eshu", "/tmp/x", "repository:r_008329d6", false},
	{"git@github.com:Eshu-HQ/eshu.git", "", "repository:r_008329d6", false},
	{"git@github.com:Eshu-HQ/eshu.git", "/tmp/x", "repository:r_008329d6", false},
	{"ssh://git@gitlab.example.com/Group/Sub/Repo.git", "", "repository:r_e926e744", false},
	{"ssh://git@gitlab.example.com/Group/Sub/Repo.git", "/tmp/x", "repository:r_e926e744", false},
	{"https://gitlab.example.com/group/sub/repo/", "", "repository:r_e926e744", false},
	{"https://gitlab.example.com/group/sub/repo/", "/tmp/x", "repository:r_e926e744", false},
	{"https://github.com:443/a/b", "", "repository:r_e58554e1", false},
	{"https://github.com:443/a/b", "/tmp/x", "repository:r_e58554e1", false},
	{"https://GitHub.com//a///b.git", "", "repository:r_e58554e1", false},
	{"https://GitHub.com//a///b.git", "/tmp/x", "repository:r_e58554e1", false},
	{"not a url", "", "repository:r_d7aad9a0", false},
	{"not a url", "/tmp/x", "repository:r_d7aad9a0", false},
	{"", "", "", true},
	{"", "/tmp/x", "repository:r_e7ad2368", false},
	{"   ", "", "", true},
	{"   ", "/tmp/x", "repository:r_e7ad2368", false},
	{"https://host", "", "repository:r_17e31be2", false},
	{"https://host", "/tmp/x", "repository:r_17e31be2", false},
	{"git@host:", "", "repository:r_60510f9d", false},
	{"git@host:", "/tmp/x", "repository:r_60510f9d", false},
	{"https://user:pw@github.com/a/b.git", "", "repository:r_e58554e1", false},
	{"https://user:pw@github.com/a/b.git", "/tmp/x", "repository:r_e58554e1", false},
}

func TestNormalizeRemoteURLMatchesInTreeGolden(t *testing.T) {
	t.Parallel()
	for _, tc := range normalizeRemoteGolden {
		if got := NormalizeRemoteURL(tc.in); got != tc.want {
			t.Errorf("NormalizeRemoteURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCanonicalRepositoryIDMatchesInTreeGolden(t *testing.T) {
	t.Parallel()
	for _, tc := range canonicalRepoGolden {
		got, err := CanonicalRepositoryID(tc.remote, tc.local)
		if (err != nil) != tc.wantErr {
			t.Errorf("CanonicalRepositoryID(%q,%q) error = %v, wantErr %v", tc.remote, tc.local, err, tc.wantErr)
			continue
		}
		if got != tc.want {
			t.Errorf("CanonicalRepositoryID(%q,%q) = %q, want %q", tc.remote, tc.local, got, tc.want)
		}
	}
}
