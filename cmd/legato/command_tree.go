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

const reflectionSubmitDescription = `The input must be a JSON document with this shape:

{
  "schema_version": 1,
  "observations": [
    {
      "kind": "technical_fact",
      "content": "The learning claim",
      "rationale": "Why it matters",
      "metadata": {}
    }
  ]
}

Each observation requires kind and content. Rationale and metadata are optional.
Recommended kind values: invariant, convention, pitfall, preference,
process_improvement, module_rule, technical_fact.
An empty observations array is valid when nothing durable was learned.`

func leaf(name, summary string, run commandHandler, args []argumentDef, flags []commandFlag, examples ...string) *commandDef {
	return &commandDef{Name: name, Summary: summary, Run: run, Arguments: args, Flags: flags, Examples: examples}
}

func group(name, summary string, examples []string, children ...*commandDef) *commandDef {
	return &commandDef{Name: name, Summary: summary, Examples: examples, Children: children}
}

func commandGroup(name, summary string, run commandHandler, args []argumentDef, flags []commandFlag, examples []string, children ...*commandDef) *commandDef {
	return &commandDef{Name: name, Summary: summary, Run: run, Arguments: args, Flags: flags, Examples: examples, Children: children}
}

func commandTree() *commandDef {
	task := group("task", "Create, inspect, update, and connect tasks", []string{"legato task list", `legato task create "Implement login" --workspace Personal`},
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
			[]argumentDef{optionalArg("task-id", "Defaults to LEGATO_TASK_ID")},
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
		group("github", "Manage task GitHub context and synchronization", []string{"legato task github refresh <task-id>"},
			leaf("context", "Set repository and branch context", func(a []string) int { return runTaskGitHubContract(append([]string{"context"}, a...)) },
				[]argumentDef{optionalArg("task-id", "Defaults to LEGATO_TASK_ID")},
				[]commandFlag{valueFlag("repo", "owner/repo", "GitHub repository"), valueFlag("branch", "branch", "Head branch"), valueFlag("sha", "commit", "Head commit SHA"), valueFlag("base-branch", "branch", "Default/base branch"), jsonFlag},
				"legato task github context <task-id> --repo owner/repo --branch feature/task"),
			leaf("refresh", "Refresh the currently linked PR", func(a []string) int { return runTaskGitHubContract(append([]string{"refresh"}, a...)) },
				[]argumentDef{optionalArg("task-id", "Defaults to LEGATO_TASK_ID")}, []commandFlag{jsonFlag}, "legato task github refresh <task-id>"),
			leaf("rediscover", "Rediscover a PR while retaining context", func(a []string) int { return runTaskGitHubContract(append([]string{"rediscover"}, a...)) },
				[]argumentDef{optionalArg("task-id", "Defaults to LEGATO_TASK_ID")}, []commandFlag{jsonFlag}, "legato task github rediscover <task-id>"),
		),
		group("worktree", "Manage task worktree metadata", []string{"legato task worktree set <task-id> --path /work/task"},
			leaf("set", "Set task worktree metadata", func(a []string) int { return runTaskWorktreeContract(append([]string{"set"}, a...)) },
				[]argumentDef{optionalArg("task-id", "Defaults to LEGATO_TASK_ID")},
				[]commandFlag{valueFlag("path", "path", "Worktree path"), valueFlag("primary-dir", "path", "Primary checkout path"), valueFlag("branch", "branch", "Worktree branch"), valueFlag("base-branch", "branch", "Base branch"), jsonFlag},
				"legato task worktree set <task-id> --path /work/task"),
			leaf("clear", "Clear task worktree metadata", func(a []string) int { return runTaskWorktreeContract(append([]string{"clear"}, a...)) },
				[]argumentDef{optionalArg("task-id", "Defaults to LEGATO_TASK_ID")}, []commandFlag{jsonFlag},
				"legato task worktree clear <task-id>"),
		),
		group("archive", "Browse and restore archived tasks", []string{"legato task archive list"},
			leaf("list", "List archived tasks", runTaskArchiveListContract, nil, []commandFlag{jsonFlag}, "legato task archive list"),
			leaf("restore", "Restore an archived task to the first configured column", runTaskArchiveRestoreContract,
				[]argumentDef{arg("task-id", "Archived task ID")}, []commandFlag{jsonFlag}, "legato task archive restore <task-id>"),
		),
	)

	workspace := group("workspace", "Discover task workspaces", []string{"legato workspace list"},
		leaf("list", "List configured workspaces", func(a []string) int { return runWorkspaceContract(append([]string{"list"}, a...)) },
			nil, []commandFlag{valueFlag("format", "text|json", "Output format"), jsonFlag}, "legato workspace list"),
	)
	resource := group("resource", "Keep the files and folders agents can be given", []string{"legato resource list", `legato resource add "Obsidian vault" ~/Documents/Vault --write`},
		leaf("list", "List resources, from the configuration and saved", func(a []string) int { return runResourceContract("list", a) },
			nil, []commandFlag{jsonFlag}, "legato resource list", "legato resource list --json"),
		leaf("add", "Save a resource, or add paths to a saved one", func(a []string) int { return runResourceContract("add", a) },
			[]argumentDef{arg("name", "Resource name"), arg("path...", "Absolute paths, or paths starting with ~/")},
			[]commandFlag{boolFlag("write", "Let agents change the paths, not only read them"), jsonFlag},
			`legato resource add "Obsidian vault" ~/Documents/Vault --write`, `legato resource add Specs /srv/specs /srv/rfcs`),
		leaf("remove", "Remove a saved resource", func(a []string) int { return runResourceContract("remove", a) },
			[]argumentDef{arg("name", "Resource name")}, []commandFlag{jsonFlag}, `legato resource remove "Obsidian vault"`),
	)
	secret := group("secret", "Keep secrets in Legato's own encrypted store, for credentials to name as legato://name", []string{"legato secret add elevenlabs-key < key.txt", "legato secret list"},
		leaf("add", "Store a secret, or replace one of the same name; reads the value from stdin", func(a []string) int { return runSecretContract("add", a) },
			[]argumentDef{arg("name", "Secret name")},
			[]commandFlag{valueFlag("type", "type", "value (default), userpass, keypair, oauth-client or oauth-refresh"), valueFlag("from", "path", "Read the value from a file instead of stdin"), jsonFlag},
			"legato secret add elevenlabs-key < key.txt", "legato secret add homelab-ssh --type keypair --from ~/.ssh/homelab"),
		leaf("list", "List the stored secrets, never their values", func(a []string) int { return runSecretContract("list", a) },
			nil, []commandFlag{jsonFlag}, "legato secret list", "legato secret list --json"),
		leaf("remove", "Remove a stored secret", func(a []string) int { return runSecretContract("remove", a) },
			[]argumentDef{arg("name", "Secret name")}, []commandFlag{jsonFlag}, "legato secret remove elevenlabs-key"),
	)
	credentialResource := commandFlag{Name: "resource", Value: "resource", Description: "What the use is for, as narrowly as the task allows", Repeatable: true}
	credentialGroup := group("credential", "Ask for and use the credentials Legato holds for agents", []string{"legato credential list", "legato credential exec elevenlabs --env ELEVENLABS_API_KEY -- libretto render story.yaml"},
		leaf("list", "List the configured credentials and their rules, never their values", func(a []string) int { return runCredentialContract("list", a) },
			nil, []commandFlag{jsonFlag}, "legato credential list", "legato credential list --json"),
		leaf("request", "Ask for a scope of a credential for this agent session, waiting up to a minute for the person", func(a []string) int { return runCredentialContract("request", a) },
			[]argumentDef{arg("name", "Credential name")},
			[]commandFlag{{Name: "operation", Value: "operation", Description: "What you will do with it", Repeatable: true}, credentialResource, valueFlag("reason", "text", "What you need it for, for the person"), boolFlag("blocking", "You cannot go on without it"), jsonFlag},
			`legato credential request elevenlabs --operation read --reason "Narrate the release video"`),
		leaf("get", "Print a credential's value, when this session may use it", func(a []string) int { return runCredentialContract("get", a) },
			[]argumentDef{arg("name", "Credential name")},
			[]commandFlag{valueFlag("operation", "operation", "What the use does (default read)"), credentialResource, jsonFlag},
			"legato credential get elevenlabs"),
		leaf("exec", "Run one command with a credential's value in one environment variable", func(a []string) int { return runCredentialContract("exec", a) },
			[]argumentDef{arg("name", "Credential name"), {Name: "-- command", Optional: false, Repeatable: true, Description: "The command to run, after --"}},
			[]commandFlag{valueFlag("env", "variable", "The variable that holds the value"), valueFlag("operation", "operation", "What the use does (default read)"), credentialResource, jsonFlag},
			"legato credential exec elevenlabs --env ELEVENLABS_API_KEY -- libretto render story.yaml"),
	)
	audit := commandGroup("audit", "Read the credential audit log, newest first", func(a []string) int { return runAuditContract("list", a) },
		nil, []commandFlag{valueFlag("credential", "name", "Only this credential"), valueFlag("task", "task-id", "Only this task"), valueFlag("execution", "execution-id", "Only this agent session"), valueFlag("limit", "count", "How many rows (default 50)"), jsonFlag},
		[]string{"legato audit --credential elevenlabs", "legato audit --task f41p6uf0 --json"},
		leaf("verify", "Check that no row of the audit log was changed or removed", func(a []string) int { return runAuditContract("verify", a) },
			nil, []commandFlag{jsonFlag}, "legato audit verify"),
	)
	columns := group("columns", "Manage config-driven board columns", []string{"legato columns sync"},
		leaf("sync", "Sync board columns from config", runColumnsSyncContract,
			nil, []commandFlag{jsonFlag}, "legato columns sync", "legato columns sync --json"),
	)
	agent := group("agent", "Report and inspect AI-agent activity", []string{"legato agent peers --json", "legato agent state <task-id> --activity working"},
		leaf("state", "Report agent activity for a task", runAgentStateContract,
			[]argumentDef{optionalArg("task-id", "Defaults to LEGATO_TASK_ID")},
			[]commandFlag{valueFlag("activity", "working|waiting|background|error|empty", "Current activity"), valueFlag("execution-id", "id", "Owning Legato execution ID"), valueFlag("working-dir", "path", "Agent working directory"), valueFlag("session-id", "id", "Reporting integration session ID"), valueFlag("event", "name", "Reporting integration lifecycle event"), jsonFlag},
			"legato agent state <task-id> --activity working"),
		leaf("permission-resolved", "Map a Chimera permission resolution payload to agent activity", runAgentPermissionResolvedContract,
			[]argumentDef{optionalArg("task-id", "Defaults to LEGATO_TASK_ID")},
			[]commandFlag{valueFlag("execution-id", "id", "Owning Legato execution ID"), valueFlag("working-dir", "path", "Agent working directory"), valueFlag("session-id", "id", "Reporting Chimera session ID"), jsonFlag},
			"legato agent permission-resolved <task-id> --execution-id <id>"),
		leaf("stopped", "Map a Claude Code Stop hook payload to agent activity", runAgentStoppedContract,
			[]argumentDef{optionalArg("task-id", "Defaults to LEGATO_TASK_ID")},
			[]commandFlag{valueFlag("execution-id", "id", "Owning Legato execution ID"), valueFlag("working-dir", "path", "Agent working directory"), jsonFlag},
			"legato agent stopped <task-id> --execution-id <id> < stop-payload.json"),
		leaf("allow-read", "Answer a Claude Code hook: allow reading an image pasted for the session", runAgentAllowReadContract,
			[]argumentDef{optionalArg("task-id", "Defaults to LEGATO_TASK_ID")}, nil,
			"legato agent allow-read < pre-tool-use-payload.json"),
		leaf("session-created", "Record an integration-created agent session", runAgentSessionCreatedContract,
			[]argumentDef{arg("task-id", "Task ID"), arg("session-id", "External session ID")}, []commandFlag{boolFlag("hook-payload", "Validate a Chimera SessionCreated JSON payload from stdin"), jsonFlag},
			"legato agent session-created <task-id> <session-id>"),
		leaf("peers", "List agents sharing this Git worktree", runAgentPeersContract, nil,
			[]commandFlag{valueFlag("task", "task-id", "Caller task; defaults to LEGATO_TASK_ID"), valueFlag("execution-id", "id", "Caller execution; defaults to LEGATO_EXECUTION_ID"), valueFlag("directory", "path", "Resolve peers for this directory"), jsonFlag},
			"legato agent peers --json"),
		leaf("message", "Queue a message to a running agent", runAgentMessageContract,
			[]argumentDef{arg("agent-id", "Recipient task ID or execution ID"), arg("text", "Message text")}, []commandFlag{jsonFlag},
			"legato agent message <task-or-execution-id> \"Please coordinate before editing agent.go\""),
		leaf("broadcast", "Message every agent sharing this Git worktree", runAgentBroadcastContract,
			[]argumentDef{arg("text", "Message text")},
			[]commandFlag{valueFlag("task", "task-id", "Sender task; defaults to LEGATO_TASK_ID"), valueFlag("execution-id", "id", "Sender execution; defaults to LEGATO_EXECUTION_ID"), valueFlag("directory", "path", "Resolve peers for this directory"), jsonFlag},
			"legato agent broadcast \"Working on cmd/legato/agent_contract.go\""),
		leaf("summary", "Summarize active agent sessions", runAgentSummaryContract, nil,
			[]commandFlag{valueFlag("exclude", "task-id", "Exclude one task"), jsonFlag}, "legato agent summary"),
		leaf("status", "Render agent status for an integration", runAgentStatusContract,
			[]argumentDef{optionalArg("task-id", "Defaults to LEGATO_TASK_ID")},
			[]commandFlag{valueFlag("format", "tmux", "Integration output format"), jsonFlag},
			"legato agent status <task-id> --format tmux"),
		leaf("codex", "Start Codex for an agent session with its native bridge", runAgentCodexContract,
			[]argumentDef{optionalArg("task-id", "Defaults to LEGATO_TASK_ID")},
			[]commandFlag{valueFlag("execution-id", "id", "Owning Legato execution ID; defaults to LEGATO_EXECUTION_ID")},
			"legato agent codex -- --model gpt-6-sol"),
		leaf("codex-bridge", "Serve a Codex session to Legato's native panel", runAgentCodexBridgeContract,
			[]argumentDef{optionalArg("task-id", "Defaults to LEGATO_TASK_ID")},
			[]commandFlag{valueFlag("execution-id", "id", "Owning Legato execution ID; defaults to LEGATO_EXECUTION_ID")},
			"legato agent codex-bridge <task-id> --execution-id <id>"),
		leaf("bridge", "Serve a Claude Code session to Legato's native panel", runAgentBridgeContract,
			[]argumentDef{optionalArg("task-id", "Defaults to LEGATO_TASK_ID")},
			[]commandFlag{valueFlag("execution-id", "id", "Owning Legato execution ID; defaults to LEGATO_EXECUTION_ID")},
			"legato agent bridge <task-id> --execution-id <id>"),
	)
	agent.Children[1].Capabilities.Integration = true
	agent.Children[len(agent.Children)-1].Capabilities = commandCapabilities{Integration: true, LongRunning: true}
	for _, child := range agent.Children {
		if child.Name == "codex" || child.Name == "codex-bridge" {
			child.Capabilities = commandCapabilities{Integration: true, LongRunning: true}
		}
	}

	hooks := group("hooks", "Manage AI-tool integration hooks", []string{"legato hooks install --tool chimera"},
		leaf("install", "Install integration hooks", func(a []string) int { return runHooksContract(append([]string{"install"}, a...)) }, nil,
			[]commandFlag{valueFlag("tool", "tool[,tool...]", "Required; comma-separated claude-code, staccato, chimera, codex, or yggdrasil"), jsonFlag},
			"legato hooks install --tool claude-code,codex,chimera"),
		leaf("uninstall", "Uninstall integration hooks", func(a []string) int { return runHooksContract(append([]string{"uninstall"}, a...)) }, nil,
			[]commandFlag{valueFlag("tool", "tool[,tool...]", "Required; comma-separated claude-code, staccato, chimera, codex, or yggdrasil"), jsonFlag},
			"legato hooks uninstall --tool codex"),
	)
	authGroup := group("auth", "Manage the web UI authentication token", []string{"legato auth token"},
		leaf("token", "Print the current authentication token", func(a []string) int { return runAuthContract(append([]string{"token"}, a...)) }, nil, []commandFlag{jsonFlag}, "legato auth token"),
		leaf("regenerate", "Replace the authentication token", func(a []string) int { return runAuthContract(append([]string{"regenerate"}, a...)) }, nil, []commandFlag{jsonFlag}, "legato auth regenerate"),
	)

	reviewFlags := reviewCommandFlags()
	plan := legacyVerbGroup("plan", "Manage collaborative implementation plans", runPlanCmd,
		[]string{"validate", "submit", "show", "feedback", "status", "answer", "complete", "withdraw", "claim", "inbox"},
		reviewFlags)
	review := legacyVerbGroup("review", "Create and manage review tours", runReviewCmd,
		[]string{"chapter", "chapters", "annotate", "annotation", "answer", "ready", "show", "sync", "discard", "restart", "claim", "inbox"},
		reviewFlags)
	configureReviewCommands(review)
	configurePlanCommands(plan)
	swarm := legacyVerbGroup("swarm", "Coordinate multi-agent swarms", runSwarmCmd,
		[]string{"validate-plan", "create", "propose-plan", "cancel", "extend-plan", "dispatch", "message", "broadcast", "close", "finish", "next-step", "progress", "question", "built", "status", "inbox"},
		[]commandFlag{jsonFlag})
	workflow := legacyVerbGroup("workflow", "Run and inspect reusable workflows", runWorkflowCmd,
		[]string{"list", "validate", "run", "runs", "show", "cancel", "retry", "trigger", "step"},
		[]commandFlag{valueFlag("input", "key=value", "Workflow input (repeatable)"), valueFlag("output", "key=value", "Step output (repeatable)"), valueFlag("artifact", "key=path", "Step artifact (repeatable)"), valueFlag("run", "run-id", "Workflow run identity"), valueFlag("step", "step-id", "Workflow step identity"), valueFlag("attempt", "attempt-id", "Workflow attempt identity"), valueFlag("status", "status", "Filter runs"), valueFlag("limit", "count", "Maximum runs"), jsonFlag})
	configureSwarmCommands(swarm)
	configureWorkflowCommands(workflow)

	attention := legacyVerbGroup("attention", "Inspect the unified attention inbox", runAttentionCmd,
		[]string{"list"},
		[]commandFlag{valueFlag("producer", "name", "Filter by producer"), valueFlag("category", "category", "Filter by category"), valueFlag("urgency", "urgency", "Filter by urgency"), jsonFlag})
	attention.Examples = []string{"legato attention list", "legato attention list --urgency high --json"}
	attention.Children[0].Examples = []string{"legato attention list --producer plan --json"}

	learnFilters := []commandFlag{valueFlag("scope", "user|project", "Filter by learning scope"), valueFlag("status", "status", "Filter by lifecycle status"), valueFlag("project", "project-key", "Filter by stable project key"), valueFlag("category", "category", "Filter by KB category"), valueFlag("tags", "tag,...", "Require comma-separated tags"), valueFlag("limit", "count", "Maximum results"), jsonFlag}
	learnSearchFilters := []commandFlag{valueFlag("scope", "user|project", "Filter by learning scope"), valueFlag("status", "status|all", "Lifecycle status; defaults to validated and incorporated"), valueFlag("project", "project-key", "Repository key; defaults to the current repository"), boolFlag("all-projects", "Search user and project learnings across repositories"), valueFlag("category", "category", "Filter by KB category"), valueFlag("tags", "tag,...", "Require all comma-separated tags"), valueFlag("min-score", "0..1", "Minimum semantic cosine score; default 0.55"), valueFlag("limit", "count", "Maximum results; default 10"), jsonFlag}
	learn := group("learn", "Record and retrieve durable learnings", []string{`legato learn search "bounded retries"`, `legato learn record "Prefer table-driven tests" --kind preference`},
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
		group("incorporation", "Submit incorporation results", []string{"legato learn incorporation submit <id> --revision 1 --file result.json"}, leaf("submit", "Submit exact JSON references and verification evidence", func(a []string) int { return runLearnCmd(append([]string{"incorporation", "submit"}, a...)) }, []argumentDef{arg("incorporation-id", "Incorporation ID")}, []commandFlag{valueFlag("revision", "revision", "Current incorporation revision"), valueFlag("file", "path|-", "JSON file; defaults to stdin"), jsonFlag}, "legato learn incorporation submit <id> --revision 1 --file result.json")),
		leaf("verify", "Verify a pending incorporation from its deterministic target", func(a []string) int { return runLearnCmd(append([]string{"verify"}, a...)) }, []argumentDef{arg("incorporation-id", "Incorporation ID")}, []commandFlag{valueFlag("revision", "revision", "Current incorporation revision"), valueFlag("repo-root", "path", "Repository root for deterministic verification"), boolFlag("confirm", "Explicitly confirm a test/lint result"), jsonFlag}, "legato learn verify <id> --revision 2 --repo-root ."),
		leaf("drift", "Manually check verified incorporation targets for drift", func(a []string) int { return runLearnCmd(append([]string{"drift"}, a...)) }, nil, []commandFlag{valueFlag("repo-root", "path", "Repository root"), jsonFlag}, "legato learn drift --repo-root ."),
		group("reflection", "Submit typed session reflections", []string{"legato learn reflection submit --request <id> --file reflection.json"}, leaf("submit", "Submit a reflection and complete its workflow step", func(a []string) int { return runLearnCmd(append([]string{"reflection", "submit"}, a...)) }, nil, []commandFlag{valueFlag("request", "request-id", "Reflection request ID"), valueFlag("file", "path|-", "JSON file; defaults to stdin"), jsonFlag}, "legato learn reflection submit --request <id> --file reflection.json")),
	)

	mcp := group("mcp", "Give an agent Legato's tools over the Model Context Protocol", []string{"legato mcp serve"},
		leaf("serve", "Serve Legato's tools to the agent of this session over stdio", runMCPServeContract, nil,
			[]commandFlag{valueFlag("task", "task-id", "Session task; defaults to LEGATO_TASK_ID"), valueFlag("execution-id", "id", "Owning Legato execution ID; defaults to LEGATO_EXECUTION_ID")},
			"legato mcp serve", "legato mcp serve --task <task-id> --execution-id <id>"),
	)
	mcp.Children[0].Capabilities = commandCapabilities{Integration: true, LongRunning: true}
	mcp.Children[0].Description = "Legato adds this server to every agent session it launches, so it is not run by hand. " +
		"It speaks MCP on stdin and stdout and acts on the session it was started in: showing artifacts and cards in the web UI, reading the task, recording notes and messaging peer agents."

	root := group("", "Legato task management and AI-agent orchestration", []string{"legato task list", "legato workflow list --json"},
		task, workspace, resource, secret, credentialGroup, audit, columns, agent, plan, review, workflow, swarm, attention, learn, hooks, mcp,
		leaf("serve", "Run the Legato web server", runServeCmd, nil, []commandFlag{valueFlag("port", "port", "Listening port"), boolFlag("hot-reload", "Supervise this foreground server and restart after binary replacement")}, "legato serve --port 3080", "legato --hot-reload serve --port 3080"),
		authGroup,
		leaf("pair", "Pair a device with the Legato web UI", runPairContract, nil, []commandFlag{valueFlag("host", "host|ip", "Address this device uses to reach this specific Legato instance"), valueFlag("port", "port", "Server port"), jsonFlag}, "legato pair --host 192.168.1.20"),
	)
	root.Flags = []commandFlag{boolFlag("hot-reload", "Supervise the default TUI or serve process and restart after binary replacement")}
	root.Examples = []string{"legato task list", "legato workflow list --json", "legato --hot-reload serve --port 3080"}
	markCapabilities(root)
	if update, ok := findCommand(root, []string{"task", "update"}); ok {
		update.Description = "Status and Legato-owned descriptions can be changed for any task. Provider-backed titles and provider descriptions remain read-only."
	}
	if reflectionSubmit, ok := findCommand(root, []string{"learn", "reflection", "submit"}); ok {
		reflectionSubmit.Description = reflectionSubmitDescription
		reflectionSubmit.Examples = append(reflectionSubmit.Examples,
			`printf '%s\n' '{"schema_version":1,"observations":[]}' | legato learn reflection submit --request <id> --file -`)
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
	return group(name, summary, []string{fmt.Sprintf("legato %s %s", name, verbs[0])}, children...)
}

type commandShape struct {
	arguments []argumentDef
	flags     []commandFlag
	examples  []string
}

func applyCommandShapes(parent *commandDef, shapes map[string]commandShape) {
	for _, child := range parent.Children {
		shape, ok := shapes[child.Name]
		if !ok {
			continue
		}
		child.Arguments = shape.arguments
		child.Flags = shape.flags
		child.Examples = shape.examples
	}
}

func planSelectors() []commandFlag {
	return []commandFlag{
		valueFlag("task", "task-id", "Plan task; defaults to LEGATO_TASK_ID"),
		valueFlag("name", "name", "Named plan; defaults to LEGATO_PLAN_NAME"),
		jsonFlag,
	}
}

func configurePlanCommands(plan *commandDef) {
	selectors := planSelectors()
	applyCommandShapes(plan, map[string]commandShape{
		"validate": {
			arguments: []argumentDef{arg("markdown-file", "Plan Markdown file to validate")},
			examples:  []string{"legato plan validate plan.md", "legato plan validate .legato/plans/search/plan.md"},
		},
		"submit": {
			arguments: []argumentDef{arg("bundle-dir", "Directory containing plan.md and plan.json")},
			flags: append(append([]commandFlag{}, selectors...),
				valueFlag("review-pass", "review-pass-id", "Originating review pass"),
				commandFlag{Name: "finding", Value: "finding-id", Description: "Originating review finding", Repeatable: true}),
			examples: []string{"legato plan submit .legato/plans/search --name search"},
		},
		"show":     {flags: selectors, examples: []string{"legato plan show --json --name search"}},
		"feedback": {flags: selectors, examples: []string{"legato plan feedback --json --name search"}},
		"status":   {flags: selectors, examples: []string{"legato plan status --json --name search"}},
		"answer": {
			arguments: []argumentDef{arg("thread-id", "Question thread ID"), optionalArg("markdown", "Markdown answer; or use --file")},
			flags:     append(append([]commandFlag{}, selectors...), valueFlag("file", "path", "Markdown answer file, which can show image files beside it; use - for stdin")),
			examples:  []string{`legato plan answer <thread-id> "Use SQLite FTS" --name search`, `legato plan answer <thread-id> --file answer.md --name search`},
		},
		"complete": {flags: selectors, examples: []string{"legato plan complete --name search"}},
		"withdraw": {flags: selectors, examples: []string{"legato plan withdraw --name search"}},
		"claim":    {flags: selectors, examples: []string{"legato plan claim --task <plan-task-id> --name search"}},
		"inbox":    {flags: []commandFlag{jsonFlag}, examples: []string{"legato plan inbox --json"}},
	})
	plan.Examples = []string{"legato plan validate .legato/plans/search/plan.md", "legato plan status --json --name search", "legato plan submit .legato/plans/search --name search"}
}

func reviewCommandFlags() []commandFlag {
	names := []string{"task", "name", "review-pass", "risk", "order", "hunk", "lines", "feedback", "narration-file", "file", "include", "finding"}
	flags := make([]commandFlag, 0, len(names)+1)
	for _, name := range names {
		f := valueFlag(name, "value", humanizeCommand(name))
		f.Repeatable = name == "file" || name == "include" || name == "finding"
		flags = append(flags, f)
	}
	return append(flags, jsonFlag)
}

func configureReviewCommands(review *commandDef) {
	selectors := []commandFlag{valueFlag("task", "task-id", "Review task; defaults to LEGATO_TASK_ID"), valueFlag("name", "name", "Named review; defaults to LEGATO_REVIEW_NAME"), jsonFlag}
	applyCommandShapes(review, map[string]commandShape{
		"chapters": {flags: selectors, examples: []string{"legato review chapters --json"}},
		"annotate": {
			arguments: []argumentDef{optionalArg("sha", "Commit SHA; defaults to the latest commit"), arg("text", "Review context")},
			flags: append(append([]commandFlag{}, selectors...),
				valueFlag("risk", "risk", "low, medium, high, or unsure"),
				valueFlag("order", "number", "Reading order"),
				commandFlag{Name: "file", Value: "path", Description: "File anchor", Repeatable: true},
				valueFlag("hunk", "number", "One-based hunk anchor"),
				valueFlag("lines", "start-end", "One-based line range within the hunk")),
			examples: []string{`legato review annotate "Check error handling" --file cmd/legato/main.go --hunk 2 --risk high`},
		},
		"answer": {
			arguments: []argumentDef{arg("step-id", "Review question step ID")},
			flags:     append(append([]commandFlag{}, selectors...), valueFlag("file", "path", "Markdown answer file, which can show image files beside it; use - for stdin")),
			examples: []string{`legato review answer <step-id> --file answer.md`, `legato review answer <step-id> --file - <<'LEGATO_ANSWER'
The handler preserves the ` + "`" + `existing contract` + "`" + `.
LEGATO_ANSWER`},
		},
		"ready": {
			arguments: []argumentDef{optionalArg("summary", "One-line review summary")},
			flags:     selectors,
			examples:  []string{`legato review ready "Ready for review"`},
		},
		"show":    {flags: selectors, examples: []string{"legato review show --json"}},
		"sync":    {flags: selectors, examples: []string{"legato review sync"}},
		"discard": {flags: selectors, examples: []string{"legato review discard --name search"}},
		"restart": {
			flags:    append(append([]commandFlag{}, selectors...), valueFlag("feedback", "text", "Guidance for the replacement review")),
			examples: []string{`legato review restart --feedback "Reorder around the public API"`},
		},
		"claim": {flags: selectors, examples: []string{"legato review claim --task <review-task-id>"}},
		"inbox": {flags: []commandFlag{jsonFlag}, examples: []string{"legato review inbox --json"}},
	})
	review.Children = append(review.Children, handoffCommands(selectors))
	review.Examples = []string{"legato review show --json", `legato review ready "Ready for review"`}
	for i, child := range review.Children {
		switch child.Name {
		case "chapter":
			createFlags := append([]commandFlag{}, selectors...)
			createFlags = append(createFlags, valueFlag("narration-file", "path", "Markdown narration file, which can show image files beside it; use - for stdin"), valueFlag("risk", "risk", "low, medium, high, or unsure"), valueFlag("order", "number", "Reading order"), commandFlag{Name: "include", Value: "path:hunk", Description: "Chapter hunk membership", Repeatable: true})
			editFlags := append([]commandFlag{}, selectors...)
			editFlags = append(editFlags, valueFlag("title", "text", "Replacement title"), valueFlag("narration", "text", "Replacement narration"), valueFlag("narration-file", "path", "Replacement Markdown narration file, which can show image files beside it; use - for stdin"), valueFlag("risk", "risk", "Replacement risk"), valueFlag("order", "number", "Replacement order"), commandFlag{Name: "include", Value: "path:hunk", Description: "Replace all memberships", Repeatable: true}, boolFlag("clear-narration", "Clear narration"), boolFlag("clear-risk", "Clear risk"), boolFlag("clear-order", "Clear order"))
			review.Children[i] = commandGroup("chapter", "Create or manage authored review chapters", child.Run, []argumentDef{arg("title", "Chapter title"), optionalArg("narration", "Chapter narration, as Markdown; or use --narration-file")}, createFlags, []string{"legato review chapter 'Title' --include file.go:1", "legato review chapter 'Title' --narration-file narration.md --include file.go:1"},
				leaf("show", "Show a chapter and selected diff", func(a []string) int { return runReviewCmd(append([]string{"chapter", "show"}, a...)) }, []argumentDef{arg("chapter-id", "Chapter ID or prefix")}, selectors, "legato review chapter show <chapter-id> --json"),
				leaf("edit", "Patch chapter metadata or memberships", runReviewChapterEditContract, []argumentDef{arg("chapter-id", "Chapter ID or prefix"), optionalArg("title", "Compatibility replacement title"), optionalArg("narration", "Compatibility replacement narration")}, editFlags, "legato review chapter edit <chapter-id> --title 'Updated' --include file.go:1"),
				leaf("remove", "Remove one chapter and its attached review metadata", runReviewChapterRemoveContract, []argumentDef{arg("chapter-id", "Chapter ID or prefix")}, selectors, "legato review chapter remove <chapter-id>"))
			review.Children[i].Examples = []string{"legato review chapter 'Title' --include file.go:1"}
		case "annotation":
			editFlags := append([]commandFlag{}, selectors...)
			editFlags = append(editFlags, valueFlag("text", "text", "Replacement annotation text"), valueFlag("risk", "risk", "Replacement risk"), valueFlag("order", "number", "Replacement order"), valueFlag("file", "path", "Replacement file anchor"), valueFlag("hunk", "number", "Replacement hunk anchor"), valueFlag("lines", "start-end", "Replacement line range"), boolFlag("clear-risk", "Clear risk"), boolFlag("clear-order", "Clear order"), boolFlag("clear-lines", "Clear line range"))
			review.Children[i] = group("annotation", "Edit or remove identities returned by review annotate", []string{"legato review annotation edit <annotation-id> --text 'Updated'"},
				leaf("edit", "Patch annotation text, metadata, or anchor", runReviewAnnotationEditContract, []argumentDef{arg("annotation-id", "Annotation ID or prefix"), optionalArg("text", "Compatibility replacement text")}, editFlags, "legato review annotation edit <annotation-id> --text 'Updated' --risk high"),
				leaf("remove", "Remove an annotation or clear commit annotation metadata", runReviewAnnotationRemoveContract, []argumentDef{arg("annotation-id", "Annotation ID or prefix")}, selectors, "legato review annotation remove <annotation-id>"))
		}
	}
}

func configureSwarmCommands(swarm *commandDef) {
	withJSON := func(flags ...commandFlag) []commandFlag { return append(flags, jsonFlag) }
	applyCommandShapes(swarm, map[string]commandShape{
		"validate-plan": {arguments: []argumentDef{arg("plan-file", "Plan bundle to validate")}, flags: withJSON(), examples: []string{"legato swarm validate-plan swarm.yaml"}},
		"create":        {arguments: []argumentDef{arg("goal", "Ad-hoc swarm goal")}, flags: withJSON(valueFlag("working-dir", "path", "Working directory")), examples: []string{`legato swarm create "Audit CLI help" --working-dir .`}},
		"propose-plan":  {arguments: []argumentDef{arg("plan-file", "Plan bundle")}, flags: withJSON(boolFlag("auto-approve", "Approve without interaction"), valueFlag("timeout", "duration", "Approval timeout")), examples: []string{"legato swarm propose-plan swarm.yaml --timeout 5m"}},
		"extend-plan":   {arguments: []argumentDef{arg("plan-file", "Plan bundle")}, flags: withJSON(boolFlag("auto-approve", "Approve without interaction"), valueFlag("timeout", "duration", "Approval timeout")), examples: []string{"legato swarm extend-plan follow-up.yaml --timeout 5m"}},
		"cancel":        {arguments: []argumentDef{arg("parent-id", "Parent swarm ID")}, flags: withJSON(), examples: []string{"legato swarm cancel <parent-id>"}},
		"dispatch":      {arguments: []argumentDef{arg("subtask-id", "Subtask ID")}, flags: withJSON(), examples: []string{"legato swarm dispatch <subtask-id>"}},
		"message":       {arguments: []argumentDef{arg("subtask-id", "Subtask ID"), arg("text", "Message text")}, flags: withJSON(boolFlag("urgent", "Mark the message urgent")), examples: []string{`legato swarm message <subtask-id> "Please rebase before editing" --urgent`}},
		"broadcast":     {arguments: []argumentDef{arg("parent-id", "Parent swarm ID"), arg("text", "Message text")}, flags: withJSON(boolFlag("urgent", "Mark the message urgent")), examples: []string{`legato swarm broadcast <parent-id> "Pause work on command_tree.go"`}},
		"close":         {arguments: []argumentDef{arg("subtask-id", "Subtask ID")}, flags: withJSON(), examples: []string{"legato swarm close <subtask-id>"}},
		"finish":        {arguments: []argumentDef{arg("parent-id", "Parent swarm ID"), arg("summary", "Completion summary")}, flags: withJSON(), examples: []string{`legato swarm finish <parent-id> "All subtasks verified"`}},
		"next-step":     {arguments: []argumentDef{arg("parent-id", "Parent swarm ID")}, flags: withJSON(), examples: []string{"legato swarm next-step <parent-id>"}},
		"progress":      {arguments: []argumentDef{arg("subtask-id", "Subtask ID"), arg("text", "Progress update")}, flags: withJSON(), examples: []string{`legato swarm progress <subtask-id> "Tests are passing"`}},
		"question":      {arguments: []argumentDef{arg("subtask-id", "Subtask ID"), arg("text", "Question text")}, flags: withJSON(), examples: []string{`legato swarm question <subtask-id> "Should this include groups?"`}},
		"built":         {arguments: []argumentDef{arg("subtask-id", "Subtask ID")}, flags: withJSON(), examples: []string{"legato swarm built <subtask-id>"}},
		"status":        {arguments: []argumentDef{arg("parent-id", "Parent swarm ID")}, flags: withJSON(), examples: []string{"legato swarm status <parent-id>"}},
		"inbox":         {arguments: []argumentDef{arg("parent-id", "Parent swarm ID")}, flags: withJSON(), examples: []string{"legato swarm inbox <parent-id>"}},
	})
	swarm.Examples = []string{"legato swarm status <parent-id>", "legato swarm propose-plan swarm.yaml --timeout 5m"}
}

func configureWorkflowCommands(workflow *commandDef) {
	identityFlags := []commandFlag{
		valueFlag("run", "run-id", "Workflow run identity; defaults to LEGATO_WORKFLOW_RUN_ID"),
		valueFlag("step", "step-id", "Workflow step identity; defaults to LEGATO_WORKFLOW_STEP_ID"),
		valueFlag("attempt", "attempt-id", "Workflow attempt identity; defaults to LEGATO_WORKFLOW_ATTEMPT_ID"),
	}
	applyCommandShapes(workflow, map[string]commandShape{
		"list":     {flags: []commandFlag{jsonFlag}, examples: []string{"legato workflow list --json"}},
		"validate": {arguments: []argumentDef{arg("name-or-file", "Workflow name or definition file")}, flags: []commandFlag{jsonFlag}, examples: []string{"legato workflow validate deploy.yaml --json"}},
		"run":      {arguments: []argumentDef{arg("name", "Workflow name")}, flags: []commandFlag{{Name: "input", Value: "key=value", Description: "Workflow input", Repeatable: true}, jsonFlag}, examples: []string{"legato workflow run deploy --input environment=staging --json"}},
		"runs":     {flags: []commandFlag{valueFlag("status", "status", "Filter runs"), valueFlag("limit", "count", "Maximum runs"), jsonFlag}, examples: []string{"legato workflow runs --status running --limit 20 --json"}},
		"show":     {arguments: []argumentDef{arg("run-id", "Workflow run ID")}, flags: []commandFlag{jsonFlag}, examples: []string{"legato workflow show <run-id> --json"}},
		"cancel":   {arguments: []argumentDef{arg("run-id", "Workflow run ID")}, flags: []commandFlag{jsonFlag}, examples: []string{"legato workflow cancel <run-id> --json"}},
		"retry":    {arguments: []argumentDef{arg("run-id", "Workflow run ID"), arg("step-id", "Workflow step ID")}, flags: []commandFlag{boolFlag("force", "Retry even when normal state checks would reject it"), jsonFlag}, examples: []string{"legato workflow retry <run-id> <step-id> --json"}},
		"trigger":  {arguments: []argumentDef{arg("run-id", "Workflow run ID"), arg("step-id", "Workflow step ID")}, flags: []commandFlag{boolFlag("force", "Trigger even when normal state checks would reject it"), jsonFlag}, examples: []string{"legato workflow trigger <run-id> <step-id> --json"}},
	})
	for i, child := range workflow.Children {
		if child.Name != "step" {
			continue
		}
		completeFlags := append(append([]commandFlag{}, identityFlags...), commandFlag{Name: "output", Value: "key=value", Description: "Step output", Repeatable: true}, commandFlag{Name: "artifact", Value: "key=path", Description: "Step artifact", Repeatable: true}, jsonFlag)
		failFlags := append(append([]commandFlag{}, identityFlags...), jsonFlag)
		workflow.Children[i] = commandGroup("step", "Complete or fail an agent workflow step", child.Run,
			[]argumentDef{arg("action", "complete or fail"), optionalArg("reason", "Failure reason for fail")}, append(identityFlags, jsonFlag),
			[]string{"legato workflow step complete --output result=value --json", `legato workflow step fail "reason" --json`},
			leaf("complete", "Complete an agent workflow step", func(a []string) int { return runWorkflowCmd(append([]string{"step", "complete"}, a...)) }, nil, completeFlags, "legato workflow step complete --output result=value --artifact review=review.md --json"),
			leaf("fail", "Fail an agent workflow step", func(a []string) int { return runWorkflowCmd(append([]string{"step", "fail"}, a...)) }, []argumentDef{arg("reason", "Failure reason")}, failFlags, `legato workflow step fail "reason" --json`))
	}
	workflow.Examples = []string{"legato workflow list --json", "legato workflow run deploy --input environment=staging --json"}
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
		specs[flag.Name] = flagSpec{Boolean: flag.Boolean, Repeatable: flag.Repeatable}
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
			if node.Run != nil {
				break
			}
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
