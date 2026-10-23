// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

var (
	pinnedAction     = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._/-]+@[0-9a-f]{40}$`)
	secretUsePattern = regexp.MustCompile(`secrets\.([A-Za-z0-9_]+)`)
)

type workflowFile struct {
	On          map[string]any    `yaml:"on"`
	Permissions map[string]string `yaml:"permissions"`
	Jobs        map[string]struct {
		Permissions map[string]string `yaml:"permissions"`
		Steps       []struct {
			Name string `yaml:"name"`
			Uses string `yaml:"uses"`
			Run  string `yaml:"run"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

// releaseWorkflowProblems returns every way raw breaks the release workflow's
// safety properties: it must run only for pushed v* tags, hold only the
// permissions publishing and attesting an image need, use no secret but
// GITHUB_TOKEN, pin every action by full commit SHA, and deploy nothing.
func releaseWorkflowProblems(raw []byte) []string {
	var workflow workflowFile
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		return []string{fmt.Sprintf("release.yml does not parse: %v", err)}
	}
	var problems []string
	push, _ := workflow.On["push"].(map[string]any)
	tags, _ := push["tags"].([]any)
	if len(workflow.On) != 1 || len(push) != 1 || len(tags) != 1 || tags[0] != "v*" {
		problems = append(problems, fmt.Sprintf("triggers = %v, want only push of tags [v*]", workflow.On))
	}
	if len(workflow.Permissions) != 1 || workflow.Permissions["contents"] != "read" {
		problems = append(problems, fmt.Sprintf("workflow permissions = %v, want contents: read only", workflow.Permissions))
	}
	want := map[string]string{"contents": "read", "packages": "write", "id-token": "write", "attestations": "write"}
	for name, job := range workflow.Jobs {
		if len(job.Permissions) != len(want) {
			problems = append(problems, fmt.Sprintf("job %s permissions = %v, want %v", name, job.Permissions, want))
		}
		for key, value := range want {
			if job.Permissions[key] != value {
				problems = append(problems, fmt.Sprintf("job %s permission %s = %q, want %q", name, key, job.Permissions[key], value))
			}
		}
		for _, step := range job.Steps {
			if step.Uses != "" && !pinnedAction.MatchString(step.Uses) {
				problems = append(problems, fmt.Sprintf("job %s step %q uses %q, want a full commit SHA pin", name, step.Name, step.Uses))
			}
			for _, word := range []string{"kubectl", "helm ", "argocd", "terraform", "aws "} {
				if strings.Contains(step.Run, word) {
					problems = append(problems, fmt.Sprintf("job %s step %q runs %q: the release workflow must not deploy", name, step.Name, word))
				}
			}
		}
	}
	for _, match := range secretUsePattern.FindAllStringSubmatch(string(raw), -1) {
		if match[1] != "GITHUB_TOKEN" {
			problems = append(problems, "uses secret "+match[1]+", want only GITHUB_TOKEN")
		}
	}
	if strings.Contains(string(raw), "pull_request") {
		problems = append(problems, "mentions pull_request: nothing may publish on a PR")
	}
	return problems
}

func TestReleaseWorkflowShape(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(".github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	if problems := releaseWorkflowProblems(raw); len(problems) > 0 {
		t.Fatalf("release.yml:\n%s", strings.Join(problems, "\n"))
	}
}

// TestReleaseWorkflowGateRejectsViolations feeds the check synthetic
// workflows so it is proven to fail, not only to pass on the checked-in file.
func TestReleaseWorkflowGateRejectsViolations(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(".github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	good := string(raw)
	for name, mutate := range map[string]func(string) string{
		"pull request trigger": func(s string) string { return strings.Replace(s, "on:\n  push:", "on:\n  pull_request:\n  push:", 1) },
		"branch trigger": func(s string) string {
			return strings.Replace(s, "    tags:\n", "    branches:\n      - main\n    tags:\n", 1)
		},
		"tag pattern widened": func(s string) string { return strings.Replace(s, `- "v*"`, `- "*"`, 1) },
		"tag-pinned action": func(s string) string {
			return strings.Replace(s, "actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd", "actions/checkout@v5", 1)
		},
		"extra secret": func(s string) string { return s + "\n# ${{ secrets.DEPLOY_KEY }}\n" },
		"extra permission": func(s string) string {
			return strings.Replace(s, "      attestations: write\n", "      attestations: write\n      actions: write\n", 1)
		},
		"deploy step": func(s string) string {
			return strings.Replace(s, "cosign sign --yes", "kubectl apply -f x && cosign sign --yes", 1)
		},
	} {
		mutated := mutate(good)
		if mutated == good {
			t.Fatalf("%s: the mutation did not change the workflow", name)
		}
		if len(releaseWorkflowProblems([]byte(mutated))) == 0 {
			t.Errorf("%s: the gate accepted a violating workflow", name)
		}
	}
}
