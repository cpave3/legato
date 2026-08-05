package main

import (
	"fmt"
	"strings"
)

type commandHandler func([]string) int

type argumentDef struct {
	Name        string
	Description string
	Optional    bool
	Repeatable  bool
}

type commandFlag struct {
	Name        string
	Value       string
	Description string
	Boolean     bool
	Repeatable  bool
}

type commandCapabilities struct {
	JSON        bool
	Mutates     bool
	TaskContext bool
	Integration bool
	LongRunning bool
}

type commandDef struct {
	Name         string
	Summary      string
	Description  string
	Arguments    []argumentDef
	Flags        []commandFlag
	Examples     []string
	Capabilities commandCapabilities
	Children     []*commandDef
	Run          commandHandler
}

func arg(name, description string) argumentDef {
	return argumentDef{Name: name, Description: description}
}

func optionalArg(name, description string) argumentDef {
	return argumentDef{Name: name, Description: description, Optional: true}
}

func valueFlag(name, value, description string) commandFlag {
	return commandFlag{Name: name, Value: value, Description: description}
}

func boolFlag(name, description string) commandFlag {
	return commandFlag{Name: name, Description: description, Boolean: true}
}

var jsonFlag = boolFlag("json", "Emit a stable JSON result or error envelope")

func leaf(name, summary string, run commandHandler, args []argumentDef, flags []commandFlag, examples ...string) *commandDef {
	return &commandDef{Name: name, Summary: summary, Run: run, Arguments: args, Flags: flags, Examples: examples}
}

func group(name, summary string, children ...*commandDef) *commandDef {
	return &commandDef{Name: name, Summary: summary, Children: children}
}

func commandTree() *commandDef {
	task := group("task", "Create, inspect, update, and connect tasks",
		leaf("list", "List active tasks in board order", runTaskListContract, nil,
			[]commandFlag{valueFlag("status", "status", "Only tasks in this board column"), valueFlag("workspace", "name|unassigned", "Only tasks in this workspace"), valueFlag("limit", "count", "Maximum tasks to return"), jsonFlag},
			"legato task list", "legato task list --workspace Personal --status Backlog --json"),
		leaf("create", "Create a local task", runTaskCreateContract,
			[]argumentDef{arg("title", "Task title")},
			[]commandFlag{valueFlag("description", "text", "Markdown task details"), valueFlag("description-file", "path|-", "Read Markdown details from a file or stdin"), valueFlag("status", "status", "Initial board column"), valueFlag("priority", "priority", "Task priority"), valueFlag("workspace", "name|unassigned", "Workspace name; none or unassigned overrides the configured default"), jsonFlag},
			`legato task create "Implement login" --workspace Personal`),
		leaf("show", "Show task details", runTaskShowContract,
			[]argumentDef{optionalArg("task-id", "Defaults to LEGATO_TASK_ID")},
			[]commandFlag{valueFlag("format", "description|full|json", "Output detail level"), jsonFlag},
			"legato task show --format full"),
		leaf("update", "Update task fields or move its status", runTaskUpdateContract,
			[]argumentDef{optionalArg("task-id", "Defaults to LEGATO_TASK_ID")},
			[]commandFlag{valueFlag("status", "status", "Move to a configured column"), valueFlag("title", "title", "Replace a local task title"), valueFlag("description", "text", "Replace Legato-owned Markdown details"), valueFlag("description-file", "path|-", "Read replacement details from a file or stdin"), valueFlag("workspace", "name", `Assign by workspace name; "none" clears it`), jsonFlag},
			"legato task update --status Doing", `legato task update <task-id> --title "Clarify failure"`),
		leaf("description", "Replace a task's Legato-owned description", runTaskDescriptionContract,
			[]argumentDef{optionalArg("task-id", "Defaults to LEGATO_TASK_ID"), optionalArg("text", "Replacement Markdown")},
			[]commandFlag{valueFlag("description", "text", "Replacement Markdown"), valueFlag("description-file", "path|-", "Read Markdown from a file or stdin"), jsonFlag},
			`legato task description <task-id> --description-file details.md`),
		leaf("note", "Append a timestamped task note", runTaskNoteContract,
			[]argumentDef{optionalArg("task-id", "Defaults to LEGATO_TASK_ID"), arg("message", "Note text")},
			[]commandFlag{jsonFlag}, `legato task note <task-id> "Implementation complete"`),
		leaf("notes", "List durable task notes", runTaskNotesContract,
			[]argumentDef{optionalArg("task-id", "Defaults to LEGATO_TASK_ID")}, []commandFlag{jsonFlag},
			`legato task notes <task-id>`, `legato task notes <task-id> --json`),
		leaf("link", "Attach repository metadata to a task", runTaskLinkContract,
			[]argumentDef{optionalArg("task-id", "Defaults to LEGATO_TASK_ID")},
			[]commandFlag{valueFlag("branch", "branch", "Git branch"), valueFlag("repo", "owner/repo", "Repository"), valueFlag("sha", "commit", "Commit SHA"), jsonFlag},
			"legato task link <task-id> --branch feature/login"),
		leaf("unlink", "Remove repository metadata from a task", func(a []string) int { return runTaskUnlinkContract(a) },
			[]argumentDef{optionalArg("task-id", "Defaults to LEGATO_TASK_ID")}, []commandFlag{jsonFlag},
			"legato task unlink <task-id>"),
		group("github", "Manage task GitHub context and synchronization",
			leaf("context", "Set repository and branch context", func(a []string) int { return runTaskGitHubContract(append([]string{"context"}, a...)) },
				[]argumentDef{optionalArg("task-id", "Defaults to LEGATO_TASK_ID")},
				[]commandFlag{valueFlag("repo", "owner/repo", "GitHub repository"), valueFlag("branch", "branch", "Head branch"), valueFlag("sha", "commit", "Head commit SHA"), valueFlag("base-branch", "branch", "Default/base branch"), jsonFlag},
				"legato task github context <task-id> --repo owner/repo --branch feature/task"),
			leaf("refresh", "Refresh the currently linked PR", func(a []string) int { return runTaskGitHubContract(append([]string{"refresh"}, a...)) },
				[]argumentDef{optionalArg("task-id", "Defaults to LEGATO_TASK_ID")}, []commandFlag{jsonFlag}, "legato task github refresh <task-id>"),
			leaf("rediscover", "Rediscover a PR while retaining context", func(a []string) int { return runTaskGitHubContract(append([]string{"rediscover"}, a...)) },
				[]argumentDef{optionalArg("task-id", "Defaults to LEGATO_TASK_ID")}, []commandFlag{jsonFlag}, "legato task github rediscover <task-id>"),
		),
		group("worktree", "Manage task worktree metadata",
			leaf("set", "Set task worktree metadata", func(a []string) int { return runTaskWorktreeContract(append([]string{"set"}, a...)) },
				[]argumentDef{optionalArg("task-id", "Defaults to LEGATO_TASK_ID")},
				[]commandFlag{valueFlag("path", "path", "Worktree path"), valueFlag("primary-dir", "path", "Primary checkout path"), valueFlag("branch", "branch", "Worktree branch"), valueFlag("base-branch", "branch", "Base branch"), jsonFlag},
				"legato task worktree set <task-id> --path /work/task"),
			leaf("clear", "Clear task worktree metadata", func(a []string) int { return runTaskWorktreeContract(append([]string{"clear"}, a...)) },
				[]argumentDef{optionalArg("task-id", "Defaults to LEGATO_TASK_ID")}, []commandFlag{jsonFlag},
				"legato task worktree clear <task-id>"),
		),
		group("archive", "Browse and restore archived tasks",
			leaf("list", "List archived tasks", runTaskArchiveListContract, nil, []commandFlag{jsonFlag}, "legato task archive list"),
			leaf("restore", "Restore an archived task to the first configured column", runTaskArchiveRestoreContract,
				[]argumentDef{arg("task-id", "Archived task ID")}, []commandFlag{jsonFlag}, "legato task archive restore <task-id>"),
		),
	)

	workspace := group("workspace", "Discover task workspaces",
		leaf("list", "List configured workspaces", func(a []string) int { return runWorkspaceContract(append([]string{"list"}, a...)) },
			nil, []commandFlag{valueFlag("format", "text|json", "Output format"), jsonFlag}, "legato workspace list"),
	)
	columns := group("columns", "Manage config-driven board columns",
		leaf("sync", "Sync board columns from config", runColumnsSyncContract,
			nil, []commandFlag{jsonFlag}, "legato columns sync", "legato columns sync --json"),
	)
	agent := group("agent", "Report and inspect AI-agent activity",
		leaf("state", "Report agent activity for a task", runAgentStateContract,
			[]argumentDef{optionalArg("task-id", "Defaults to LEGATO_TASK_ID")},
			[]commandFlag{valueFlag("activity", "working|waiting|error|empty", "Current activity"), valueFlag("execution-id", "id", "Owning Legato execution ID"), valueFlag("working-dir", "path", "Agent working directory"), valueFlag("session-id", "id", "Reporting integration session ID"), valueFlag("event", "name", "Reporting integration lifecycle event"), jsonFlag},
			"legato agent state <task-id> --activity working"),
		leaf("session-created", "Record an integration-created agent session", runAgentSessionCreatedContract,
			[]argumentDef{arg("task-id", "Task ID"), arg("session-id", "External session ID")}, []commandFlag{jsonFlag},
			"legato agent session-created <task-id> <session-id>"),
		leaf("summary", "Summarize active agent sessions", runAgentSummaryContract, nil,
			[]commandFlag{valueFlag("exclude", "task-id", "Exclude one task"), jsonFlag}, "legato agent summary"),
		leaf("status", "Render agent status for an integration", runAgentStatusContract,
			[]argumentDef{optionalArg("task-id", "Defaults to LEGATO_TASK_ID")},
			[]commandFlag{valueFlag("format", "tmux", "Integration output format"), jsonFlag},
			"legato agent status <task-id> --format tmux"),
	)
	agent.Children[1].Capabilities.Integration = true

	hooks := group("hooks", "Manage AI-tool integration hooks",
		leaf("install", "Install integration hooks", func(a []string) int { return runHooksContract(append([]string{"install"}, a...)) }, nil,
			[]commandFlag{valueFlag("tool", "tool[,tool...]", "Required; comma-separated claude-code, staccato, chimera, codex, or yggdrasil"), jsonFlag},
			"legato hooks install --tool claude-code,codex,chimera"),
		leaf("uninstall", "Uninstall integration hooks", func(a []string) int { return runHooksContract(append([]string{"uninstall"}, a...)) }, nil,
			[]commandFlag{valueFlag("tool", "tool[,tool...]", "Required; comma-separated claude-code, staccato, chimera, codex, or yggdrasil"), jsonFlag},
			"legato hooks uninstall --tool codex"),
	)
	authGroup := group("auth", "Manage the web UI authentication token",
		leaf("token", "Print the current authentication token", func(a []string) int { return runAuthContract(append([]string{"token"}, a...)) }, nil, []commandFlag{jsonFlag}, "legato auth token"),
		leaf("regenerate", "Replace the authentication token", func(a []string) int { return runAuthContract(append([]string{"regenerate"}, a...)) }, nil, []commandFlag{jsonFlag}, "legato auth regenerate"),
	)

	reviewFlags := reviewCommandFlags()
	plan := legacyVerbGroup("plan", "Manage collaborative implementation plans", runPlanCmd,
		[]string{"submit", "show", "feedback", "status", "answer", "complete", "withdraw", "claim", "inbox"},
		reviewFlags)
	review := legacyVerbGroup("review", "Create and manage review tours", runReviewCmd,
		[]string{"chapter", "chapters", "annotate", "annotation", "answer", "ready", "show", "sync", "discard", "restart", "claim", "inbox"},
		reviewFlags)
	swarm := legacyVerbGroup("swarm", "Coordinate multi-agent swarms", runSwarmCmd,
		[]string{"validate-plan", "create", "propose-plan", "cancel", "extend-plan", "dispatch", "message", "broadcast", "close", "finish", "next-step", "progress", "question", "built", "status", "inbox"},
		[]commandFlag{jsonFlag})
	workflow := legacyVerbGroup("workflow", "Run and inspect reusable workflows", runWorkflowCmd,
		[]string{"list", "validate", "run", "runs", "show", "cancel", "retry", "trigger", "step"},
		[]commandFlag{valueFlag("input", "key=value", "Workflow input (repeatable)"), valueFlag("output", "key=value", "Step output (repeatable)"), valueFlag("artifact", "key=path", "Step artifact (repeatable)"), valueFlag("run", "run-id", "Workflow run identity"), valueFlag("step", "step-id", "Workflow step identity"), valueFlag("attempt", "attempt-id", "Workflow attempt identity"), valueFlag("status", "status", "Filter runs"), valueFlag("limit", "count", "Maximum runs"), jsonFlag})
	configureSwarmCommands(swarm)

	attention := legacyVerbGroup("attention", "Inspect the unified attention inbox", runAttentionCmd,
		[]string{"list"},
		[]commandFlag{valueFlag("producer", "name", "Filter by producer"), valueFlag("category", "category", "Filter by category"), valueFlag("urgency", "urgency", "Filter by urgency"), jsonFlag})

	learnFilters := []commandFlag{valueFlag("scope", "user|project", "Filter by learning scope"), valueFlag("status", "status", "Filter by lifecycle status"), valueFlag("project", "project-key", "Filter by stable project key"), valueFlag("category", "category", "Filter by KB category"), valueFlag("tags", "tag,...", "Require comma-separated tags"), valueFlag("limit", "count", "Maximum results"), jsonFlag}
	learnSearchFilters := []commandFlag{valueFlag("scope", "user|project", "Filter by learning scope"), valueFlag("status", "status|all", "Lifecycle status; defaults to validated and incorporated"), valueFlag("project", "project-key", "Repository key; defaults to the current repository"), boolFlag("all-projects", "Search user and project learnings across repositories"), valueFlag("category", "category", "Filter by KB category"), valueFlag("tags", "tag,...", "Require all comma-separated tags"), valueFlag("min-score", "0..1", "Minimum semantic cosine score; default 0.55"), valueFlag("limit", "count", "Maximum results; default 10"), jsonFlag}
	learn := group("learn", "Record and retrieve durable learnings",
		leaf("record", "Record an explicit candidate learning", func(a []string) int { return runLearnCmd(append([]string{"record"}, a...)) }, []argumentDef{optionalArg("claim", "Atomic learning claim")},
			[]commandFlag{valueFlag("claim", "text", "Learning claim"), valueFlag("kind", "kind", "Learning kind"), valueFlag("category", "category", "KB category"), valueFlag("tags", "tag,...", "Comma-separated KB tags"), valueFlag("scope", "user|project", "Learning scope"), valueFlag("project", "project-key", "Stable project key"), valueFlag("project-label", "label", "Human-readable project label"), valueFlag("rationale", "text", "Supporting rationale"), valueFlag("task", "task-id", "Source task"), valueFlag("source-kind", "kind", "Evidence source kind"), valueFlag("source-ref", "reference", "Evidence source reference"), valueFlag("author", "author", "Evidence author"), jsonFlag}, `legato learn record "Prefer table-driven tests" --kind preference`),
		leaf("show", "Show a learning and its evidence", func(a []string) int { return runLearnCmd(append([]string{"show"}, a...)) }, []argumentDef{arg("learning-id", "Learning ID")}, []commandFlag{jsonFlag}, "legato learn show <learning-id>"),
		leaf("list", "List scoped learnings", func(a []string) int { return runLearnCmd(append([]string{"list"}, a...)) }, nil, learnFilters, "legato learn list --status candidate"),
		leaf("search", "Search the durable knowledge base", func(a []string) int { return runLearnCmd(append([]string{"search"}, a...)) }, []argumentDef{arg("query", "Lexical and semantic search query")}, learnSearchFilters, `legato learn search "bounded retries"`, `legato learn search "date handling" --project repo:github.com/acme/widget --status validated`),
		leaf("context", "Retrieve learnings relevant to a task", func(a []string) int { return runLearnCmd(append([]string{"context"}, a...)) }, nil, []commandFlag{valueFlag("task", "task-id", "Task ID; defaults to LEGATO_TASK_ID"), valueFlag("limit", "count", "Maximum results"), jsonFlag}, "legato learn context --task <task-id>"),
		leaf("curate", "Launch the built-in learning workflow for a task", func(a []string) int { return runLearnCmd(append([]string{"curate"}, a...)) }, nil, []commandFlag{valueFlag("task", "task-id", "Task ID to reflect on and curate"), jsonFlag}, "legato learn curate --task <task-id>"),
		leaf("consolidate", "Launch the built-in learning consolidator", func(a []string) int { return runLearnCmd(append([]string{"consolidate"}, a...)) }, nil, []commandFlag{valueFlag("scope", "user|project", "Required consolidation scope"), valueFlag("project", "project-key", "Required for project scope"), jsonFlag}, "legato learn consolidate --scope user", "legato learn consolidate --scope project --project repo:github.com/acme/widget"),
		leaf("accept", "Accept a candidate, optionally editing its wording or scope", func(a []string) int { return runLearnCmd(append([]string{"accept"}, a...)) }, []argumentDef{arg("learning-id", "Learning ID")}, []commandFlag{valueFlag("revision", "revision", "Current learning revision"), valueFlag("claim", "text", "Replacement claim"), valueFlag("scope", "user|project", "Replacement scope"), valueFlag("project", "project-key", "Replacement project key"), jsonFlag}, "legato learn accept <learning-id> --revision 1"),
		leaf("edit", "Edit durable KB content and classification", func(a []string) int { return runLearnCmd(append([]string{"edit"}, a...)) }, []argumentDef{arg("learning-id", "Learning ID")}, []commandFlag{valueFlag("revision", "revision", "Current learning revision"), valueFlag("claim", "text", "Replacement claim"), valueFlag("rationale", "text", "Replacement rationale"), valueFlag("scope", "user|project", "Replacement scope"), valueFlag("project", "project-key", "Replacement project key"), valueFlag("category", "category", "Replacement KB category"), valueFlag("tags", "tag,...", "Replacement comma-separated tags"), jsonFlag}, "legato learn edit <learning-id> --revision 1 --claim 'Updated claim'"),
		leaf("reject", "Reject a candidate learning", func(a []string) int { return runLearnCmd(append([]string{"reject"}, a...)) }, []argumentDef{arg("learning-id", "Learning ID")}, []commandFlag{valueFlag("revision", "revision", "Current learning revision"), jsonFlag}, "legato learn reject <learning-id> --revision 1"),
		leaf("enable", "Enable an accepted learning for future agent prompts", func(a []string) int { return runLearnCmd(append([]string{"enable"}, a...)) }, []argumentDef{arg("learning-id", "Learning ID")}, []commandFlag{valueFlag("revision", "revision", "Current learning revision"), jsonFlag}, "legato learn enable <learning-id> --revision 2"),
		leaf("disable", "Disable an accepted learning for future agent prompts", func(a []string) int { return runLearnCmd(append([]string{"disable"}, a...)) }, []argumentDef{arg("learning-id", "Learning ID")}, []commandFlag{valueFlag("revision", "revision", "Current learning revision"), jsonFlag}, "legato learn disable <learning-id> --revision 3"),
		leaf("incorporate", "Propose where an accepted learning should be incorporated", func(a []string) int { return runLearnCmd(append([]string{"incorporate"}, a...)) }, []argumentDef{arg("learning-id", "Learning ID")}, []commandFlag{valueFlag("revision", "revision", "Current learning revision"), valueFlag("destination", "destination", "Incorporation destination"), valueFlag("target", "target", "Target path or reference"), valueFlag("action", "action", "Requested action"), jsonFlag}, "legato learn incorporate <learning-id> --revision 2 --destination documentation --target docs/guide.md"),
		group("incorporation", "Submit incorporation results", leaf("submit", "Submit exact JSON references and verification evidence", func(a []string) int { return runLearnCmd(append([]string{"incorporation", "submit"}, a...)) }, []argumentDef{arg("incorporation-id", "Incorporation ID")}, []commandFlag{valueFlag("revision", "revision", "Current incorporation revision"), valueFlag("file", "path|-", "JSON file; defaults to stdin"), jsonFlag}, "legato learn incorporation submit <id> --revision 1 --file result.json")),
		leaf("verify", "Verify a pending incorporation from its deterministic target", func(a []string) int { return runLearnCmd(append([]string{"verify"}, a...)) }, []argumentDef{arg("incorporation-id", "Incorporation ID")}, []commandFlag{valueFlag("revision", "revision", "Current incorporation revision"), valueFlag("repo-root", "path", "Repository root for deterministic verification"), boolFlag("confirm", "Explicitly confirm a test/lint result"), jsonFlag}, "legato learn verify <id> --revision 2 --repo-root ."),
		leaf("drift", "Manually check verified incorporation targets for drift", func(a []string) int { return runLearnCmd(append([]string{"drift"}, a...)) }, nil, []commandFlag{valueFlag("repo-root", "path", "Repository root"), jsonFlag}, "legato learn drift --repo-root ."),
		group("reflection", "Submit typed session reflections", leaf("submit", "Submit a reflection and complete its workflow step", func(a []string) int { return runLearnCmd(append([]string{"reflection", "submit"}, a...)) }, nil, []commandFlag{valueFlag("request", "request-id", "Reflection request ID"), valueFlag("file", "path|-", "JSON file; defaults to stdin"), jsonFlag}, "legato learn reflection submit --request <id> --file reflection.json")),
	)

	root := group("", "Legato task management and AI-agent orchestration",
		task, workspace, columns, agent, plan, review, workflow, swarm, attention, learn, hooks,
		leaf("serve", "Run the Legato web server", runServeCmd, nil, []commandFlag{valueFlag("port", "port", "Listening port"), boolFlag("hot-reload", "Supervise this foreground server and restart after binary replacement")}, "legato serve --port 3080", "legato --hot-reload serve --port 3080"),
		authGroup,
		leaf("pair", "Pair a device with the Legato web UI", runPairContract, nil, []commandFlag{valueFlag("host", "host|ip", "Address this device uses to reach this specific Legato instance"), valueFlag("port", "port", "Server port"), jsonFlag}, "legato pair --host 192.168.1.20"),
	)
	root.Flags = []commandFlag{boolFlag("hot-reload", "Supervise the default TUI or serve process and restart after binary replacement")}
	root.Examples = []string{"legato --hot-reload", "legato --hot-reload serve --port 3080"}
	markCapabilities(root)
	if update, ok := findCommand(root, []string{"task", "update"}); ok {
		update.Description = "Status and Legato-owned descriptions can be changed for any task. Provider-backed titles and provider descriptions remain read-only."
	}
	return root
}

func legacyVerbGroup(name, summary string, runner commandHandler, verbs []string, flags []commandFlag) *commandDef {
	children := make([]*commandDef, 0, len(verbs))
	for _, verb := range verbs {
		verb := verb
		children = append(children, leaf(verb, humanizeCommand(verb), func(a []string) int {
			return runner(append([]string{verb}, a...))
		}, []argumentDef{{Name: "arguments", Optional: true, Repeatable: true, Description: "Command-specific arguments"}}, flags,
			fmt.Sprintf("legato %s %s --help", name, verb)))
	}
	return group(name, summary, children...)
}

func reviewCommandFlags() []commandFlag {
	names := []string{"task", "name", "review-pass", "risk", "order", "hunk", "lines", "feedback", "file", "include", "finding"}
	flags := make([]commandFlag, 0, len(names)+1)
	for _, name := range names {
		f := valueFlag(name, "value", humanizeCommand(name))
		f.Repeatable = name == "file" || name == "include" || name == "finding"
		flags = append(flags, f)
	}
	return append(flags, jsonFlag)
}

func configureSwarmCommands(swarm *commandDef) {
	type shape struct {
		args  []argumentDef
		flags []commandFlag
	}
	shapes := map[string]shape{
		"validate-plan": {args: []argumentDef{arg("plan-file", "Plan bundle to validate")}},
		"create":        {args: []argumentDef{arg("goal", "Ad-hoc swarm goal")}, flags: []commandFlag{valueFlag("working-dir", "path", "Working directory")}},
		"propose-plan":  {args: []argumentDef{arg("plan-file", "Plan bundle")}, flags: []commandFlag{boolFlag("auto-approve", "Approve without interaction"), valueFlag("timeout", "duration", "Approval timeout")}},
		"extend-plan":   {args: []argumentDef{arg("plan-file", "Plan bundle")}, flags: []commandFlag{boolFlag("auto-approve", "Approve without interaction"), valueFlag("timeout", "duration", "Approval timeout")}},
		"cancel":        {args: []argumentDef{arg("parent-id", "Parent swarm ID")}},
		"dispatch":      {args: []argumentDef{arg("subtask-id", "Subtask ID")}},
		"message":       {args: []argumentDef{arg("subtask-id", "Subtask ID"), arg("text", "Message text")}, flags: []commandFlag{boolFlag("urgent", "Mark the message urgent")}},
		"broadcast":     {args: []argumentDef{arg("parent-id", "Parent swarm ID"), arg("text", "Message text")}, flags: []commandFlag{boolFlag("urgent", "Mark the message urgent")}},
		"close":         {args: []argumentDef{arg("subtask-id", "Subtask ID")}},
		"finish":        {args: []argumentDef{arg("parent-id", "Parent swarm ID"), arg("summary", "Completion summary")}},
		"next-step":     {args: []argumentDef{arg("parent-id", "Parent swarm ID")}},
		"progress":      {args: []argumentDef{arg("subtask-id", "Subtask ID"), arg("text", "Progress update")}},
		"question":      {args: []argumentDef{arg("subtask-id", "Subtask ID"), arg("text", "Question text")}},
		"built":         {args: []argumentDef{arg("subtask-id", "Subtask ID")}},
		"status":        {args: []argumentDef{arg("parent-id", "Parent swarm ID")}},
		"inbox":         {args: []argumentDef{arg("parent-id", "Parent swarm ID")}},
	}
	for _, child := range swarm.Children {
		if shape, ok := shapes[child.Name]; ok {
			child.Arguments = shape.args
			child.Flags = append(shape.flags, jsonFlag)
			child.Examples = []string{fmt.Sprintf("legato swarm %s --help", child.Name)}
		}
	}
}

func humanizeCommand(name string) string {
	return strings.ToUpper(name[:1]) + strings.ReplaceAll(name[1:], "-", " ")
}

func markCapabilities(node *commandDef) {
	if node.Run != nil {
		for _, flag := range node.Flags {
			if flag.Name == "json" {
				node.Capabilities.JSON = true
			}
		}
	}
	for _, child := range node.Children {
		markCapabilities(child)
	}
}

func findCommand(root *commandDef, path []string) (*commandDef, bool) {
	node := root
	for _, part := range path {
		found := false
		for _, child := range node.Children {
			if child.Name == part {
				node, found = child, true
				break
			}
		}
		if !found {
			return node, false
		}
	}
	return node, true
}

func commandFlagSpecs(path ...string) map[string]flagSpec {
	node, ok := findCommand(commandTree(), path)
	if !ok {
		panic("command definition missing for " + strings.Join(path, " "))
	}
	specs := make(map[string]flagSpec, len(node.Flags))
	for _, flag := range node.Flags {
		specs[flag.Name] = flagSpec{Boolean: flag.Boolean}
	}
	return specs
}

func resolveCommand(root *commandDef, args []string) (*commandDef, []string, []string, *commandError) {
	node, path, index := root, []string{}, 0
	for index < len(args) && len(node.Children) > 0 {
		var next *commandDef
		for _, child := range node.Children {
			if child.Name == args[index] {
				next = child
				break
			}
		}
		if next == nil {
			return node, path, args[index:], usageError("unknown_command", fmt.Sprintf("unknown command %q", strings.Join(append(path, args[index]), " ")))
		}
		node = next
		path = append(path, args[index])
		index++
	}
	if node.Run == nil {
		return node, path, args[index:], usageError("missing_argument", fmt.Sprintf("%s command is required", strings.Join(append([]string{"legato"}, path...), " ")))
	}
	return node, path, args[index:], nil
}
