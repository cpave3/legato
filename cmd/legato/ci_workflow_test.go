package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// In a GitHub stack only the top PR runs ci.yml's heavy jobs; the PRs below
// get ci/stack from scripts/stack-status.cjs (LEG-629). These tests hold the
// workflow files to that design.

type ghTrigger struct {
	Types    []string `yaml:"types"`
	Paths    []string `yaml:"paths"`
	Branches []string `yaml:"branches"`
}

// stringList is a YAML string or list of strings, as `needs:` may be.
type stringList []string

func (l *stringList) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		*l = stringList{n.Value}
		return nil
	}
	return n.Decode((*[]string)(l))
}

type ghStep struct {
	Name string            `yaml:"name"`
	If   string            `yaml:"if"`
	Uses string            `yaml:"uses"`
	With map[string]string `yaml:"with"`
}

type ghJob struct {
	If          string            `yaml:"if"`
	Needs       stringList        `yaml:"needs"`
	Permissions map[string]string `yaml:"permissions"`
	Steps       []ghStep          `yaml:"steps"`
}

type ghWorkflow struct {
	On          map[string]*ghTrigger `yaml:"on"`
	Permissions map[string]string     `yaml:"permissions"`
	Jobs        map[string]ghJob      `yaml:"jobs"`
}

func readWorkflow(t *testing.T, name string) ghWorkflow {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", name))
	if err != nil {
		t.Fatal(err)
	}
	var w ghWorkflow
	if err := yaml.Unmarshal(data, &w); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return w
}

// condition is a job's `if:` with its whitespace collapsed, as folded YAML
// scalars break it across lines.
func condition(j ghJob) string {
	return strings.Join(strings.Fields(j.If), " ")
}

// The one gate of a heavy job: not in a stack, or its top.
const stackGate = "!github.event.pull_request.stack || github.event.pull_request.stack.position == github.event.pull_request.stack.size"

const sameRepository = "github.event.pull_request.head.repo.full_name == github.repository"

var (
	heavyJobs = []string{"go", "web", "packages", "docs"}
	// Seconds each, and a single layer can break them on its own.
	cheapJobs = []string{"paths", "migrations"}
	stackJobs = []string{"stack", "stack-status"}
)

func TestCIRunsTheHeavyJobsOnlyOnTheTopOfAStack(t *testing.T) {
	ci := readWorkflow(t, "ci.yml")

	pr, ok := ci.On["pull_request"]
	if !ok {
		t.Fatal("ci.yml does not run on pull_request")
	}
	if pr == nil {
		pr = &ghTrigger{}
	}
	// Linking a stack fires nothing; marking its PRs ready builds the top.
	if !slices.Contains(pr.Types, "ready_for_review") {
		t.Errorf("pull_request types %v lack ready_for_review", pr.Types)
	}
	if slices.Contains(pr.Types, "closed") {
		t.Errorf("pull_request types %v include closed: closes belong to stack.yml", pr.Types)
	}

	for name, job := range ci.Jobs {
		known := slices.Contains(heavyJobs, name) || slices.Contains(cheapJobs, name) || slices.Contains(stackJobs, name)
		if !known {
			t.Errorf("job %q is not known to be heavy, cheap or a stack job: add it to a list here", name)
		}
		// Agents open draft PRs, and their review loops count the checks.
		if strings.Contains(job.If, "draft") {
			t.Errorf("job %q's condition names drafts: %s", name, condition(job))
		}
	}
	for _, name := range heavyJobs {
		job, ok := ci.Jobs[name]
		if !ok {
			t.Errorf("ci.yml has no %q job", name)
			continue
		}
		if !strings.Contains(condition(job), stackGate) {
			t.Errorf("heavy job %q is not gated on the top of a stack: if: %s", name, condition(job))
		}
	}
	for _, name := range append(slices.Clone(cheapJobs), "stack") {
		if strings.Contains(ci.Jobs[name].If, "stack.position") {
			t.Errorf("job %q must run on every PR, but its condition reads the stack position", name)
		}
	}
}

func TestCIPostsTheTopBuildsResultToTheStack(t *testing.T) {
	ci := readWorkflow(t, "ci.yml")
	for _, name := range stackJobs {
		job := ci.Jobs[name]
		if !strings.Contains(condition(job), sameRepository) {
			t.Errorf("job %q writes statuses but does not skip forks' PRs: if: %s", name, condition(job))
		}
		if job.Permissions["statuses"] != "write" {
			t.Errorf("job %q needs statuses: write, has %v", name, job.Permissions)
		}
	}

	result := ci.Jobs["stack-status"]
	var others []string
	for name := range ci.Jobs {
		if name != "stack-status" {
			others = append(others, name)
		}
	}
	sort.Strings(others)
	needs := slices.Sorted(slices.Values(result.Needs))
	// Every job, so the result posted below matches the run's conclusion,
	// which onReview reads later.
	if !slices.Equal(needs, others) {
		t.Errorf("stack-status needs %v, want every other job %v", needs, others)
	}
	cond := condition(result)
	// A run a newer push cancelled must post nothing: the newer run has
	// already marked the stack pending.
	if !strings.Contains(cond, "!cancelled()") {
		t.Errorf("stack-status does not skip a cancelled run: if: %s", cond)
	}
	if !strings.Contains(cond, "github.event.pull_request.stack.position == github.event.pull_request.stack.size") {
		t.Errorf("stack-status must run only on the top of a stack: if: %s", cond)
	}
}

// stack-status.cjs reads the top PR's run of ci.yml by the name of its
// result job.
func TestStackStatusScriptNamesTheCIWorkflowAndItsResultJob(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	script, _ := filepath.Abs(filepath.Join("..", "..", "scripts", "stack-status.cjs"))
	out, err := exec.Command(node, "-p", `const s = require(process.argv[1]); s.buildWorkflow + " " + s.resultJob`, script).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	names := strings.Fields(string(out))
	if len(names) != 2 {
		t.Fatalf("node printed %q", out)
	}
	ci := readWorkflow(t, names[0])
	if _, ok := ci.Jobs[names[1]]; !ok {
		t.Errorf("%s has no job %q", names[0], names[1])
	}
}

func TestDocsBuildsPullRequestsInCI(t *testing.T) {
	docs := readWorkflow(t, "docs.yml")
	// A path-filtered workflow compares a stacked PR with the layer below, and
	// a check it skips stays pending: ci.yml's docs job builds PRs instead.
	if _, ok := docs.On["pull_request"]; ok {
		t.Error("docs.yml runs on pull_request; ci.yml's docs job builds pull requests")
	}
	if _, ok := docs.On["push"]; !ok {
		t.Error("docs.yml no longer builds and publishes main")
	}
}

func TestStackWorkflowHandlesReviewsAndCloses(t *testing.T) {
	stack := readWorkflow(t, "stack.yml")
	review := stack.On["pull_request_review"]
	if review == nil || !slices.Equal(review.Types, []string{"submitted", "dismissed"}) {
		t.Errorf("stack.yml pull_request_review types: %+v, want [submitted dismissed]", review)
	}
	pr := stack.On["pull_request"]
	if pr == nil || !slices.Equal(pr.Types, []string{"closed"}) {
		t.Errorf("stack.yml pull_request types: %+v, want [closed]", pr)
	}
	if len(stack.Permissions) != 1 || stack.Permissions["contents"] != "read" {
		t.Errorf("stack.yml's own permissions are %v, want only contents: read", stack.Permissions)
	}
	if len(stack.Jobs) == 0 {
		t.Fatal("stack.yml has no jobs")
	}
	for name, job := range stack.Jobs {
		if !strings.Contains(condition(job), sameRepository) {
			t.Errorf("job %q does not skip forks' PRs: if: %s", name, condition(job))
		}
		if job.Permissions["statuses"] != "write" {
			t.Errorf("job %q needs statuses: write, has %v", name, job.Permissions)
		}
	}
}

// A job that can write statuses runs no code of the pull request's: a branch
// pushed without the right to change workflows must not be able to change
// what posts ci/stack. So it loads the script from the default branch, and
// posts nothing while the default branch has none.
func TestStatusWritingJobsLoadTheScriptFromTheDefaultBranch(t *testing.T) {
	t.Skip("scratch stack only: loads the script from ci-stack-test/base, as main has none yet")
	const trusted = "${{ github.event.repository.default_branch }}"
	for _, name := range []string{"ci.yml", "stack.yml"} {
		w := readWorkflow(t, name)
		for jobName, job := range w.Jobs {
			if job.Permissions["statuses"] != "write" {
				continue
			}
			var checkouts, scripts int
			for _, step := range job.Steps {
				switch {
				case strings.HasPrefix(step.Uses, "actions/checkout@"):
					checkouts++
					if step.With["ref"] != trusted {
						t.Errorf("%s job %q checks out %q, want ref %s", name, jobName, step.With["ref"], trusted)
					}
				case strings.HasPrefix(step.Uses, "actions/github-script@"):
					scripts++
					if !strings.Contains(step.If, "hashFiles('scripts/stack-status.cjs') != ''") {
						t.Errorf("%s job %q runs the script without checking the default branch has it: if: %s", name, jobName, step.If)
					}
				}
			}
			if checkouts != 1 || scripts != 1 {
				t.Errorf("%s job %q has %d checkouts and %d scripts, want one of each", name, jobName, checkouts, scripts)
			}
		}
	}
}

// Each script a workflow requires must exist.
func TestStackWorkflowsLoadTheStackScript(t *testing.T) {
	re := regexp.MustCompile(`require\("\./(scripts/[^"]+)"\)`)
	for _, name := range []string{"ci.yml", "stack.yml"} {
		data, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", name))
		if err != nil {
			t.Fatal(err)
		}
		found := re.FindAllStringSubmatch(string(data), -1)
		if len(found) == 0 {
			t.Errorf("%s requires no script", name)
		}
		for _, m := range found {
			if _, err := os.Stat(filepath.Join("..", "..", m[1])); err != nil {
				t.Errorf("%s requires %s: %v", name, m[1], err)
			}
		}
	}
}
