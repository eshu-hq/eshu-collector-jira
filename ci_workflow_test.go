// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// reusableWorkflow is the pinned upstream lane body this repo's ci.yml
// calls. The tag is advisory; the SHA is what CI executes.
const reusableWorkflow = "eshu-hq/.github/.github/workflows/go-collector-ci.yml@4d01ab90baf5dd77c21cc1ee6bdf701332613449"

// TestCIWorkflowPinsActionsByCommitSHA keeps a moved tag from changing what
// pull-request CI executes. CI holds (almost) no permissions, but it still
// runs third-party action code on every pull request.
func TestCIWorkflowPinsActionsByCommitSHA(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t)
	for name, job := range workflow.Jobs {
		if job.Uses != "" && !pinnedAction.MatchString(job.Uses) {
			t.Errorf("job %s uses %q, want a full commit SHA pin", name, job.Uses)
		}
		for _, step := range job.Steps {
			if step.Uses != "" && !pinnedAction.MatchString(step.Uses) {
				t.Errorf("job %s uses %q, want a full commit SHA pin", name, step.Uses)
			}
		}
	}
	call, ok := workflow.Jobs["go-ci"]
	if !ok {
		t.Fatal("ci.yml has no go-ci reusable call; lane bodies must live upstream at a pinned SHA")
	}
	if call.Uses != reusableWorkflow {
		t.Errorf("go-ci uses %q, want the pinned reusable %q", call.Uses, reusableWorkflow)
	}
}

// TestGoCICallSurvivesSkippedChanges pins the schedule contract: the
// changes job runs only on pull_request and push, so the go-ci call must
// carry if: always(). Otherwise schedule runs skip every lane and the
// weekly scans silently stop.
func TestGoCICallSurvivesSkippedChanges(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t)
	call, ok := workflow.Jobs["go-ci"]
	if !ok {
		t.Fatal("ci.yml has no go-ci reusable call")
	}
	changes, ok := workflow.Jobs["changes"]
	if !ok {
		t.Fatal("ci.yml has no changes job")
	}
	if !strings.Contains(call.If, "always()") && strings.Contains(changes.If, "pull_request") {
		t.Errorf("go-ci if %q does not survive a skipped changes job (changes if %q); schedule runs would skip every lane", call.If, changes.If)
	}
	if call.Permissions["contents"] != "read" {
		t.Errorf("go-ci contents permission = %q, want read so the call can fetch the upstream reusable definition; top-level stays empty", call.Permissions["contents"])
	}
}

// TestLaneBodiesLiveUpstream keeps the epic's delivery shape: no lane body
// may drift back into the caller. The caller holds path selection, the
// go-ci call, and the two caller-side jobs the reusable cannot own (weekly
// findings issue, Security-tab upload). Everything else runs upstream.
func TestLaneBodiesLiveUpstream(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t)
	for _, name := range []string{"lint", "test", "race", "shell", "image", "boundary", "govulncheck", "gosec", "actionlint", "ci-complete"} {
		if _, ok := workflow.Jobs[name]; ok {
			t.Errorf("ci.yml has a local %q job, want lane bodies to live in the pinned reusable", name)
		}
	}
	for _, name := range []string{"changes", "go-ci", "scheduled-findings", "gosec-sarif"} {
		if _, ok := workflow.Jobs[name]; !ok {
			t.Errorf("ci.yml has no %q job, want path selection, the reusable call, and its two caller-side jobs", name)
		}
	}
}

// TestCIWorkflowSetsJobTimeouts keeps a hung runner from holding a PR open
// forever: every caller-owned job with steps declares a timeout. The go-ci
// call itself accepts no timeout (a reusable-call job takes only
// name/uses/with/secrets/needs/if/permissions); its ceiling lives in the
// upstream inner-lane timeouts.
func TestCIWorkflowSetsJobTimeouts(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t)
	for name, job := range workflow.Jobs {
		if name == "go-ci" {
			continue
		}
		if len(job.Steps) == 0 {
			continue
		}
		if job.Timeout <= 0 {
			t.Errorf("job %s sets no timeout-minutes", name)
		}
	}
}

// TestCIWorkflowCancelsSupersededRuns keeps stale pushes from consuming
// runners: one concurrency group per PR (or ref) with cancellation.
func TestCIWorkflowCancelsSupersededRuns(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t)
	if group := fmt.Sprint(workflow.Concurrency["group"]); !strings.Contains(group, "github.event.pull_request.number") {
		t.Errorf("concurrency group = %q, want one group per PR", group)
	}
	if cancel := fmt.Sprint(workflow.Concurrency["cancel-in-progress"]); cancel != "true" {
		t.Errorf("cancel-in-progress = %q, want true", cancel)
	}
}

// TestCallerNeedsNoConfiguredSecrets keeps fork PRs working: top-level
// permissions stay empty, the only secret any caller step may touch is the
// automatic GITHUB_TOKEN (used solely by the weekly-findings issue step),
// and the go-ci call holds only contents: read.
func TestCallerNeedsNoConfiguredSecrets(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t)
	if len(workflow.Permissions) != 0 {
		t.Errorf("top-level permissions = %v, want empty (fork-safe); the go-ci call holds its own contents: read", workflow.Permissions)
	}
	for name, job := range workflow.Jobs {
		var texts []string
		for _, step := range job.Steps {
			texts = append(texts, step.Run)
			for key, value := range step.With {
				texts = append(texts, key+"="+fmt.Sprint(value))
			}
		}
		for _, text := range texts {
			for _, match := range secretUsePattern.FindAllStringSubmatch(text, -1) {
				if match[1] != "GITHUB_TOKEN" {
					t.Errorf("job %s uses configured secret %q, want no secrets but GITHUB_TOKEN", name, match[1])
				}
			}
		}
	}
}

// imageGoClauseProblems returns every way cond breaks the image-lane
// go-source contract: the image embeds Go sources, so the lane must run
// when the go change class is true. A bare substring check accepts an
// inverted comparison, which selects the lane exactly when Go sources did
// not change and skips it when they did.
func imageGoClauseProblems(cond string) []string {
	var problems []string
	if !strings.Contains(cond, "needs.changes.outputs.go == 'true'") {
		problems = append(problems, "run-image lacks \"needs.changes.outputs.go == 'true'\": Go sources ship in the image")
	}
	for _, inverted := range []string{
		"needs.changes.outputs.go != 'true'",
		"needs.changes.outputs.go == 'false'",
		"needs.changes.outputs.go != 'false'",
	} {
		if strings.Contains(cond, inverted) {
			problems = append(problems, "run-image contains inverted clause "+inverted)
		}
	}
	if !strings.Contains(cond, "needs.changes.outputs.image == 'true'") {
		problems = append(problems, "run-image lacks \"needs.changes.outputs.image == 'true'\": Dockerfile-only PRs skip the lane")
	}
	return problems
}

// TestImageLaneRunsOnGoSources keeps the shipped artifact validated on PRs
// that change what it embeds: the Dockerfile COPYs go.mod/go.sum, COPYs the
// whole context (which keeps .go sources), and go-builds the binary, so a
// Go-source or go.mod PR must select the image lane. A go.mod toolchain bump
// past the pinned base would otherwise pass every Go lane yet break docker
// build, surfacing only as post-merge red main. The live ci.yml text must
// pass; an inverted or removed go clause must fail. The image lane body
// lives upstream; the caller selects it with the go-ci run-image input,
// which the same clause rules judge.
func TestImageLaneRunsOnGoSources(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t)
	call, ok := workflow.Jobs["go-ci"]
	if !ok {
		t.Fatal("ci.yml has no go-ci reusable call")
	}
	cond, ok := call.With["run-image"].(string)
	if !ok {
		t.Fatal("go-ci run-image input is not a string")
	}
	if problems := imageGoClauseProblems(cond); len(problems) > 0 {
		t.Errorf("image lane breaks its go-source contract:\n%s", strings.Join(problems, "\n"))
	}

	inverted := strings.Replace(cond, "needs.changes.outputs.go == 'true'", "needs.changes.outputs.go != 'true'", 1)
	if len(imageGoClauseProblems(inverted)) == 0 {
		t.Errorf("inverted go clause: the gate accepted a lane that skips Go-source PRs")
	}
	removed := strings.ReplaceAll(cond, "needs.changes.outputs.go == 'true'", "")
	if len(imageGoClauseProblems(removed)) == 0 {
		t.Errorf("removed go clause: the gate accepted a lane with no go selection")
	}
}

// govulnWiringProblems returns every way cond breaks the govulncheck
// selection contract: dependency PRs must scan, pushes to main must record
// the post-merge result, and the weekly schedule must stay reachable so its
// failure-gated issue job can file findings.
func govulnWiringProblems(cond string) []string {
	var problems []string
	if !strings.Contains(cond, "needs.changes.outputs.deps == 'true'") {
		problems = append(problems, "run-govulncheck lacks \"needs.changes.outputs.deps == 'true'\": dependency PRs skip the scan")
	}
	if !strings.Contains(cond, "github.event_name == 'schedule'") {
		problems = append(problems, "run-govulncheck lacks the schedule event: weekly scans never run")
	}
	if !strings.Contains(cond, "github.event_name == 'push'") {
		problems = append(problems, "run-govulncheck lacks the push event: post-merge main goes unrecorded")
	}
	return problems
}

// TestGovulncheckRunsOnDepsScheduleAndPush pins the scan's reachability at
// the caller: the reusable scan fails loud, and this wiring is what puts a
// failing scan on schedule so the failure-gated findings job below it can
// file the weekly issue.
func TestGovulncheckRunsOnDepsScheduleAndPush(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t)
	call, ok := workflow.Jobs["go-ci"]
	if !ok {
		t.Fatal("ci.yml has no go-ci reusable call")
	}
	cond, ok := call.With["run-govulncheck"].(string)
	if !ok {
		t.Fatal("go-ci run-govulncheck input is not a string")
	}
	if problems := govulnWiringProblems(cond); len(problems) > 0 {
		t.Errorf("govulncheck wiring breaks its selection contract:\n%s", strings.Join(problems, "\n"))
	}

	quiet := strings.Replace(cond, "needs.changes.outputs.deps == 'true'", "", 1)
	if len(govulnWiringProblems(quiet)) == 0 {
		t.Errorf("removed deps clause: the gate accepted a scan that skips dependency PRs")
	}
	unreachable := strings.Replace(cond, "github.event_name == 'schedule'", "", 1)
	if len(govulnWiringProblems(unreachable)) == 0 {
		t.Errorf("removed schedule clause: the gate accepted a scan the weekly issue job can never observe")
	}
}

// TestScheduledFindingsFilesTheWeeklyIssue pins the caller-side half of the
// scanner notification contract (the reusable owns the loud scan and the
// advisory PR lanes): the findings job runs only when go-ci failed on a
// schedule event, and it files or updates an issue instead of failing
// silently.
func TestScheduledFindingsFilesTheWeeklyIssue(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t)
	job, ok := workflow.Jobs["scheduled-findings"]
	if !ok {
		t.Fatal("ci.yml has no scheduled-findings job")
	}
	for _, want := range []string{"failure()", "schedule"} {
		if !strings.Contains(job.If, want) {
			t.Errorf("scheduled-findings if %q lacks %s: findings outside a failed schedule run would file noise, or scheduled findings would never file", job.If, want)
		}
	}
	var runs []string
	for _, step := range job.Steps {
		runs = append(runs, step.Run)
	}
	joined := strings.Join(runs, "\n")
	for _, want := range []string{"gh issue create", "gh issue comment"} {
		if !strings.Contains(joined, want) {
			t.Errorf("scheduled-findings never runs %q: a repeat finding would spam a new issue every week", want)
		}
	}
}

// TestGosecSarifUploadStaysOffPRs keeps fork PRs on a read-only token: the
// Security-tab upload the reusable deliberately omits runs only on non-PR
// events, and it never turns a 403 into a silent green no-op.
func TestGosecSarifUploadStaysOffPRs(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t)
	job, ok := workflow.Jobs["gosec-sarif"]
	if !ok {
		t.Fatal("ci.yml has no gosec-sarif job")
	}
	if !strings.Contains(job.If, "github.event_name != 'pull_request'") {
		t.Errorf("gosec-sarif if %q is not gated off pull requests: fork PRs would need security-events write", job.If)
	}
	uploadIdx := stepIndex(job.Steps, func(s workflowStep) bool { return strings.Contains(s.Uses, "codeql-action/upload-sarif") })
	if uploadIdx < 0 {
		t.Fatal("gosec-sarif has no upload-sarif step")
	}
	if !job.Steps[uploadIdx].ContinueOnError {
		t.Errorf("gosec-sarif upload lacks continue-on-error: a 403 would fail the lane instead of staying advisory")
	}
	artifactIdx := stepIndex(job.Steps, func(s workflowStep) bool {
		return strings.Contains(s.Uses, "download-artifact") && strings.Contains(fmt.Sprint(s.With["name"]), "gosec-jira")
	})
	if artifactIdx < 0 {
		t.Fatal("gosec-sarif downloads no gosec-jira artifact: the SARIF it uploads comes from nowhere")
	}
}

// TestMiscCatchAllFailsClosed pins the epic's classification rule at the
// caller: anything the named classes do not cover still runs the Go lanes.
// A Makefile-only change must prove the lanes still work; only docs stay
// exempt.
func TestMiscCatchAllFailsClosed(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(".github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow workflowFile
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	changes, ok := workflow.Jobs["changes"]
	if !ok {
		t.Fatal("ci.yml has no changes job")
	}
	filterIdx := stepIndex(changes.Steps, func(s workflowStep) bool { return strings.Contains(s.Uses, "paths-filter") })
	if filterIdx < 0 {
		t.Fatal("changes job has no paths-filter step")
	}
	filters, ok := changes.Steps[filterIdx].With["filters"].(string)
	if !ok {
		t.Fatal("paths-filter step has no filters input")
	}
	if problems := miscFilterProblems(filters); len(problems) > 0 {
		t.Errorf("path filter breaks the fail-closed contract:\n%s", strings.Join(problems, "\n"))
	}
	without := strings.Replace(filters, "misc:", "not-misc:", 1)
	if len(miscFilterProblems(without)) == 0 {
		t.Errorf("removed misc class: the gate accepted a filter where unclassified files run no lane")
	}
}

// miscFilterProblems returns every way filters breaks the fail-closed
// contract: the misc class must exist so unclassified files select the Go
// lanes, and docs must stay the only exempt class.
func miscFilterProblems(filters string) []string {
	var problems []string
	class := false
	for _, line := range strings.Split(filters, "\n") {
		if strings.TrimSpace(line) == "misc:" {
			class = true
		}
	}
	if !class {
		problems = append(problems, "no misc class: unclassified files run no lane")
	}
	for _, exempt := range []string{"!README.md", "!LICENSE"} {
		if !strings.Contains(filters, exempt) {
			problems = append(problems, "misc class lacks "+exempt+": docs-only PRs would pay for lanes")
		}
	}
	return problems
}

// TestCoverageFloorNeverDrops locks the epic's finish line at the caller:
// the floor input starts at the measured 92.0%-clearing 90 and may only
// rise. A seeded drop must fail; deleting assertions to move the number is
// not a fix.
func TestCoverageFloorNeverDrops(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t)
	call, ok := workflow.Jobs["go-ci"]
	if !ok {
		t.Fatal("ci.yml has no go-ci reusable call")
	}
	raw, ok := call.With["coverage-floor"]
	if !ok {
		t.Fatal("go-ci has no coverage-floor input")
	}
	floor, err := strconv.ParseFloat(fmt.Sprint(raw), 64)
	if err != nil {
		t.Fatalf("coverage-floor %v does not parse as a number: %v", raw, err)
	}
	if floor < 90 {
		t.Errorf("coverage-floor = %v, want at least 90 (the measured 92.0%% baseline already clears it; the floor may only rise)", floor)
	}
}

// TestBoundaryCommandRunsTheImportBoundary keeps the standalone-repo gate
// wired after the move: the reusable boundary lane runs this repo's
// boundary test, which rejects Eshu core and cross-collector imports.
func TestBoundaryCommandRunsTheImportBoundary(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t)
	call, ok := workflow.Jobs["go-ci"]
	if !ok {
		t.Fatal("ci.yml has no go-ci reusable call")
	}
	command, ok := call.With["boundary-command"].(string)
	if !ok || !strings.Contains(command, "TestImportBoundaryRejectsCoreAndCrossCollectorImports") {
		t.Errorf("go-ci boundary-command = %q, want it to run TestImportBoundaryRejectsCoreAndCrossCollectorImports", fmt.Sprint(call.With["boundary-command"]))
	}
	runBoundary, ok := call.With["run-boundary"].(string)
	if !ok {
		t.Fatal("go-ci run-boundary input is not a string")
	}
	if !strings.Contains(runBoundary, "needs.changes.outputs.go == 'true'") {
		t.Errorf("run-boundary %q lacks Go-source selection: a new forbidden import would skip the gate", runBoundary)
	}
}

func readCIWorkflow(t *testing.T) workflowFile {
	t.Helper()

	raw, err := os.ReadFile(".github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow workflowFile
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	if len(workflow.Jobs) == 0 {
		t.Fatal("ci.yml defines no jobs")
	}
	return workflow
}
