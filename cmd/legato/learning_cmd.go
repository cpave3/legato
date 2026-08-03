package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

var learningServiceFactory = func(cfg *config.Config, db *store.Store) cli.LearningCLIService {
	return configuredLearningService(cfg, db, nil)
}

type projectIdentityResolverFunc func() (service.ProjectIdentity, error)

func learningSearchFilter(parsed parsedArgs, resolveProject projectIdentityResolverFunc) (service.LearningSearchFilter, error) {
	filter := service.LearningSearchFilter{LearningFilter: service.LearningFilter{
		Scope: parsed.Values["scope"], ProjectKey: strings.TrimSpace(parsed.Values["project"]),
		Category: parsed.Values["category"], Tags: splitLearningTags(parsed.Values["tags"]), Limit: 10,
	}, MinScore: 0.55}
	if parsed.Present["all-projects"] && parsed.Present["project"] {
		return filter, fmt.Errorf("--all-projects cannot be combined with --project")
	}
	if !parsed.Present["project"] && !parsed.Present["all-projects"] {
		identity, err := resolveProject()
		if err != nil {
			return filter, fmt.Errorf("resolve current repository (use --all-projects to search globally): %w", err)
		}
		filter.ProjectKey = identity.Key
	}
	status := strings.TrimSpace(parsed.Values["status"])
	switch {
	case status == "" && !parsed.Present["status"]:
		filter.Statuses = []string{"validated", "incorporated"}
	case status == "all":
	case status != "":
		filter.Statuses = []string{status}
	default:
		return filter, fmt.Errorf("--status requires a status or all")
	}
	if parsed.Present["limit"] {
		limit, err := strconv.Atoi(parsed.Values["limit"])
		if err != nil || limit <= 0 || limit > 200 {
			return filter, fmt.Errorf("--limit must be between 1 and 200")
		}
		filter.Limit = limit
	}
	if parsed.Present["min-score"] {
		score, err := strconv.ParseFloat(parsed.Values["min-score"], 64)
		if err != nil || score < 0 || score > 1 {
			return filter, fmt.Errorf("--min-score must be between 0 and 1")
		}
		filter.MinScore = score
	}
	return filter, nil
}

func learningCommandError(err error) *commandError {
	return &commandError{Code: "learning_error", Message: err.Error(), Exit: exitInternal}
}

func runLearnCmd(args []string) int {
	if len(args) == 0 {
		return renderCommandError("learn", usageError("missing_argument", "learn command is required"), false)
	}
	verb := args[0]
	path := []string{"learn", verb}
	if verb == "reflection" || verb == "incorporation" {
		group := verb
		if len(args) < 2 || args[1] != "submit" {
			return renderCommandError("learn."+group, usageError("unknown_command", "usage: legato learn "+group+" submit [flags]"), false)
		}
		path = append(path, "submit")
		args = append([]string{group + "-submit"}, args[2:]...)
		verb = group + "-submit"
	}
	parsed, parseErr := parseCommandArgs(args[1:], commandFlagSpecs(path...))
	if parseErr != nil {
		return renderCommandError(strings.Join(path, "."), parseErr, parsed.Present["json"])
	}
	jsonMode := parsed.Present["json"]
	fail := func(err error) int {
		return renderCommandError(strings.Join(path, "."), learningCommandError(err), jsonMode)
	}
	limit := 0
	if parsed.Present["limit"] {
		var err error
		limit, err = strconv.Atoi(parsed.Values["limit"])
		if err != nil || limit < 0 {
			return fail(fmt.Errorf("limit must be a non-negative integer"))
		}
	}
	revision := 0
	if parsed.Present["revision"] {
		var err error
		revision, err = strconv.Atoi(parsed.Values["revision"])
		if err != nil || revision <= 0 {
			return fail(fmt.Errorf("revision must be a positive integer; get the current revision with 'legato learn show <id>'"))
		}
	}

	cfg, err := config.Load()
	if err != nil {
		return fail(err)
	}
	db, err := openStore(cfg)
	if err != nil {
		return fail(err)
	}
	defer db.Close()
	learning := learningServiceFactory(cfg, db)
	ctx := context.Background()

	switch verb {
	case "curate":
		if len(parsed.Positionals) != 0 {
			return fail(fmt.Errorf("usage: legato learn curate --task <task-id>"))
		}
		taskID := firstNonEmpty(parsed.Values["task"], os.Getenv("LEGATO_TASK_ID"))
		if taskID == "" {
			return fail(fmt.Errorf("--task is required (or set LEGATO_TASK_ID)"))
		}
		projectKey, projectLabel, workingDir, err := learningTaskContext(ctx, db, taskID)
		if err != nil {
			return fail(err)
		}
		definition, err := service.BuiltInLearningWorkflow(learningWorkflowOptions(cfg))
		if err != nil {
			return fail(err)
		}
		workflows, err := workflowServiceFactory(db, cfg)
		if err != nil {
			return fail(err)
		}
		return runLearningOutput(func(w io.Writer) error {
			return cli.WorkflowRun(ctx, workflows, definition, map[string]string{"task": taskID, "project_key": projectKey, "project_label": projectLabel, "working_dir": workingDir}, w)
		}, fail)
	case "consolidate":
		if len(parsed.Positionals) != 0 {
			return fail(fmt.Errorf("usage: legato learn consolidate --scope user|project [--project <project-key>]"))
		}
		scope := strings.TrimSpace(parsed.Values["scope"])
		projectKey, projectLabel := strings.TrimSpace(parsed.Values["project"]), ""
		if scope == "project" && projectKey == "" {
			identity, resolveErr := service.ResolveProjectIdentity(nil, currentWorkingDirectory(), checkoutInspector())
			if resolveErr != nil {
				return fail(fmt.Errorf("--project is required outside a resolvable repository: %w", resolveErr))
			}
			projectKey, projectLabel = identity.Key, identity.Label
		}
		definition, err := service.BuiltInConsolidationWorkflow(scope, projectKey, projectLabel, learningWorkflowOptions(cfg))
		if err != nil {
			return fail(err)
		}
		workflows, err := workflowServiceFactory(db, cfg)
		if err != nil {
			return fail(err)
		}
		return runLearningOutput(func(w io.Writer) error { return cli.WorkflowRun(ctx, workflows, definition, nil, w) }, fail)
	case "record":
		if len(parsed.Positionals) > 1 {
			return fail(fmt.Errorf("usage: legato learn record [claim] --kind <kind> [flags]"))
		}
		claim := parsed.Values["claim"]
		if len(parsed.Positionals) == 1 {
			if claim != "" {
				return fail(fmt.Errorf("claim may be supplied either positionally or with --claim, not both"))
			}
			claim = parsed.Positionals[0]
		}
		input := service.LearningRecordInput{Scope: parsed.Values["scope"], ProjectKey: parsed.Values["project"], ProjectLabel: parsed.Values["project-label"], Kind: parsed.Values["kind"], Category: parsed.Values["category"], Tags: splitLearningTags(parsed.Values["tags"]), Claim: claim, Rationale: parsed.Values["rationale"], TaskID: parsed.Values["task"], SourceKind: parsed.Values["source-kind"], SourceRef: parsed.Values["source-ref"], Author: parsed.Values["author"]}
		if input.Scope == "" {
			input.Scope = "user"
		}
		if input.SourceKind == "" {
			input.SourceKind = "explicit"
		}
		if input.SourceRef == "" {
			input.SourceRef = "cli"
		}
		if input.Author == "" {
			input.Author = "user"
		}
		return runLearningOutput(func(w io.Writer) error { return cli.LearningRecord(ctx, learning, input, w) }, fail)
	case "show":
		if len(parsed.Positionals) != 1 {
			return fail(fmt.Errorf("usage: legato learn show <learning-id>"))
		}
		return runLearningOutput(func(w io.Writer) error { return cli.LearningShow(ctx, learning, parsed.Positionals[0], w) }, fail)
	case "list":
		svc, ok := learning.(cli.LearningListService)
		if !ok {
			return fail(fmt.Errorf("learning list is not available in this service build"))
		}
		return runLearningOutput(func(w io.Writer) error {
			return cli.LearningList(ctx, svc, parsed.Values["scope"], parsed.Values["status"], parsed.Values["project"], parsed.Values["category"], splitLearningTags(parsed.Values["tags"]), limit, w)
		}, fail)
	case "search":
		if len(parsed.Positionals) != 1 {
			return fail(fmt.Errorf("usage: legato learn search <query> [flags]"))
		}
		svc, ok := learning.(cli.LearningSearchService)
		if !ok {
			return fail(fmt.Errorf("learning search is not available in this service build"))
		}
		filter, err := learningSearchFilter(parsed, func() (service.ProjectIdentity, error) {
			return service.ResolveProjectIdentity(nil, currentWorkingDirectory(), checkoutInspector())
		})
		if err != nil {
			return fail(err)
		}
		return runLearningOutput(func(w io.Writer) error {
			return cli.LearningSearch(ctx, svc, parsed.Positionals[0], filter, w)
		}, fail)
	case "accept", "edit", "reject", "enable", "disable":
		if len(parsed.Positionals) != 1 {
			return fail(fmt.Errorf("usage: legato learn %s <learning-id> --revision <revision> [flags]", verb))
		}
		if !parsed.Present["revision"] {
			return fail(fmt.Errorf("--revision is required; get it with 'legato learn show %s'", parsed.Positionals[0]))
		}
		id := parsed.Positionals[0]
		if verb == "enable" || verb == "disable" {
			svc, ok := learning.(cli.LearningPromptActivationService)
			if !ok {
				return fail(fmt.Errorf("learning prompt activation is not available in this service build"))
			}
			return runLearningOutput(func(w io.Writer) error {
				return cli.LearningSetPromptEnabled(ctx, svc, id, revision, verb == "enable", w)
			}, fail)
		}
		if verb == "reject" {
			svc, ok := learning.(cli.LearningRejectService)
			if !ok {
				return fail(fmt.Errorf("learning rejection is not available in this service build"))
			}
			return runLearningOutput(func(w io.Writer) error { return cli.LearningReject(ctx, svc, id, revision, w) }, fail)
		}
		if verb == "edit" {
			svc, ok := learning.(cli.LearningEditService)
			if !ok {
				return fail(fmt.Errorf("learning editing is not available in this service build"))
			}
			return runLearningOutput(func(w io.Writer) error {
				return cli.LearningEdit(ctx, svc, id, revision, service.LearningEditInput{Claim: parsed.Values["claim"], Rationale: parsed.Values["rationale"], Scope: parsed.Values["scope"], ProjectKey: parsed.Values["project"], Category: parsed.Values["category"], Tags: splitLearningTags(parsed.Values["tags"])}, w)
			}, fail)
		}
		svc, ok := learning.(cli.LearningAcceptService)
		if !ok {
			return fail(fmt.Errorf("learning acceptance is not available in this service build"))
		}
		return runLearningOutput(func(w io.Writer) error {
			return cli.LearningAccept(ctx, svc, id, revision, parsed.Values["claim"], parsed.Values["scope"], parsed.Values["project"], w)
		}, fail)
	case "incorporate":
		if len(parsed.Positionals) != 1 {
			return fail(fmt.Errorf("usage: legato learn incorporate <learning-id> --revision <revision> --destination <destination> [flags]"))
		}
		if !parsed.Present["revision"] {
			return fail(fmt.Errorf("--revision is required; get it with 'legato learn show %s'", parsed.Positionals[0]))
		}
		svc, ok := learning.(cli.LearningIncorporationService)
		if !ok {
			return fail(fmt.Errorf("learning incorporation is not available in this service build"))
		}
		return runLearningOutput(func(w io.Writer) error {
			return cli.LearningIncorporate(ctx, svc, parsed.Positionals[0], revision, parsed.Values["destination"], parsed.Values["target"], parsed.Values["action"], w)
		}, fail)
	case "incorporation-submit":
		if len(parsed.Positionals) != 1 {
			return fail(fmt.Errorf("usage: legato learn incorporation submit <incorporation-id> --revision <revision> [--file <path|->]"))
		}
		if !parsed.Present["revision"] {
			return fail(fmt.Errorf("--revision is required for incorporation %s", parsed.Positionals[0]))
		}
		data, err := readLearningDocument(parsed.Values["file"], parsed.Present["file"], os.Stdin)
		if err != nil {
			return fail(err)
		}
		svc, ok := learning.(cli.LearningIncorporationSubmitService)
		if !ok {
			return fail(fmt.Errorf("incorporation submission is not available in this service build"))
		}
		return runLearningOutput(func(w io.Writer) error {
			return cli.LearningIncorporationSubmit(ctx, svc, parsed.Positionals[0], revision, data, w)
		}, fail)
	case "verify":
		if len(parsed.Positionals) != 1 || !parsed.Present["revision"] || !parsed.Present["repo-root"] {
			return fail(fmt.Errorf("usage: legato learn verify <incorporation-id> --revision <revision> --repo-root <path> [--confirm]"))
		}
		svc, ok := learning.(cli.LearningTargetVerificationService)
		if !ok {
			return fail(fmt.Errorf("deterministic incorporation verification is not available in this service build"))
		}
		return runLearningOutput(func(w io.Writer) error {
			return cli.LearningVerifyTarget(ctx, svc, parsed.Positionals[0], revision, parsed.Values["repo-root"], parsed.Present["confirm"], w)
		}, fail)
	case "drift":
		if len(parsed.Positionals) != 0 || !parsed.Present["repo-root"] {
			return fail(fmt.Errorf("usage: legato learn drift --repo-root <path>"))
		}
		svc, ok := learning.(cli.LearningDriftService)
		if !ok {
			return fail(fmt.Errorf("incorporation drift checking is not available in this service build"))
		}
		return runLearningOutput(func(w io.Writer) error {
			return cli.LearningDrift(ctx, svc, parsed.Values["repo-root"], w)
		}, fail)
	case "context":
		if len(parsed.Positionals) != 0 {
			return fail(fmt.Errorf("usage: legato learn context --task <task-id> [--limit <count>]"))
		}
		taskID := firstNonEmpty(parsed.Values["task"], os.Getenv("LEGATO_TASK_ID"))
		svc, ok := learning.(cli.LearningContextService)
		if !ok {
			return fail(fmt.Errorf("learning context is not available in this service build"))
		}
		return runLearningOutput(func(w io.Writer) error { return cli.LearningContext(ctx, svc, taskID, limit, w) }, fail)
	case "reflection-submit":
		if len(parsed.Positionals) != 0 {
			return fail(fmt.Errorf("reflection JSON must be supplied with --file or stdin"))
		}
		requestID := firstNonEmpty(parsed.Values["request"], os.Getenv("LEGATO_LEARNING_REQUEST_ID"))
		data, err := readLearningDocument(parsed.Values["file"], parsed.Present["file"], os.Stdin)
		if err != nil {
			return fail(err)
		}
		reflection, ok := learning.(cli.ReflectionCLIService)
		if !ok {
			return fail(fmt.Errorf("reflection submission is not available in this service build"))
		}
		workflows, err := workflowServiceFactory(db, cfg)
		if err != nil {
			return fail(err)
		}
		return runLearningOutput(func(w io.Writer) error {
			return cli.LearningReflectionSubmit(ctx, reflection, workflows, requestID, data, w)
		}, fail)
	default:
		return fail(fmt.Errorf("unknown learn command %q", verb))
	}
}

func splitLearningTags(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, part)
		}
	}
	return result
}

func learningWorkflowOptions(cfg *config.Config) service.LearningWorkflowOptions {
	adapter := firstNonEmpty(cfg.Learning.CuratorAdapter, cfg.Swarm.DefaultAgent)
	return service.LearningWorkflowOptions{
		Adapter: adapter, Tier: cfg.Learning.CuratorTier,
		WorkingDir:     cfg.Learning.CuratorWorkingDir,
		PacketMaxItems: cfg.Learning.PacketMaxItems, PacketMaxBytes: cfg.Learning.PacketMaxBytes,
	}
}

func currentWorkingDirectory() string {
	wd, _ := os.Getwd()
	return wd
}

func learningTaskContext(ctx context.Context, db *store.Store, taskID string) (string, string, string, error) {
	task, err := db.GetTask(ctx, taskID)
	if err != nil {
		return "", "", "", fmt.Errorf("task %q not found: %w", taskID, err)
	}
	codeContext, err := store.ParseCodeContext(task.CodeContext)
	if err != nil {
		return "", "", "", fmt.Errorf("read task project context: %w", err)
	}
	directory := ""
	if codeContext != nil {
		directory = codeContext.WorkingDir
	}
	if directory == "" && task.WorktreePath != nil {
		directory = *task.WorktreePath
	}
	if directory == "" {
		directory = currentWorkingDirectory()
	}
	identity, err := service.ResolveProjectIdentity(codeContext, directory, checkoutInspector())
	if err != nil {
		// Task curation remains useful for user-scoped evidence when no repository
		// identity is attached; project consolidation, by contrast, is strict.
		return "", "", directory, nil
	}
	return identity.Key, identity.Label, directory, nil
}

func readLearningDocument(path string, hasFile bool, stdin io.Reader) ([]byte, error) {
	if !hasFile || path == "-" {
		return io.ReadAll(stdin)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read learning JSON document %q: %w", path, err)
	}
	return data, nil
}

func runLearningOutput(run func(io.Writer) error, fail func(error) int) int {
	if err := run(os.Stdout); err != nil {
		return fail(err)
	}
	return 0
}
