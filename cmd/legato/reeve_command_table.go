package main

// legatoCommandClasses classes every leaf subcommand of the legato CLI:
// what running it on the host needs when the Reeve asks for it (its legato
// tool). See reeve_commands.go.
var legatoCommandClasses = map[string]legatoCommandClass{
	// Tasks.
	"task list":              {"read", "List the tasks"},
	"task create":            {"change", "Create a task"},
	"task show":              {"read", "Show a task"},
	"task update":            {"change", "Update a task or move it"},
	"task description":       {"change", "Replace a task's description"},
	"task note":              {"change", "Add a note to a task"},
	"task notes":             {"read", "List a task's notes"},
	"task link":              {"change", "Link a repository to a task"},
	"task unlink":            {"change", "Unlink a repository from a task"},
	"task github context":    {"change", "Set a task's repository and branch"},
	"task github refresh":    {"change", "Refresh a task's pull request"},
	"task github rediscover": {"change", "Find a task's pull request again"},
	"task worktree set":      {"change", "Set a task's worktree"},
	"task worktree clear":    {"change", "Clear a task's worktree"},
	"task archive list":      {"read", "List the archived tasks"},
	"task archive restore":   {"change", "Restore an archived task"},

	// Workspaces and columns.
	"workspace list": {"read", "List the workspaces"},
	"columns sync":   {"change", "Sync the board's columns from the configuration"},

	// Clones.
	"clone setup": {"never", "Run a clone's setup"},

	// Resources.
	"resource list":   {"read", "List the resources"},
	"resource add":    {"change", "Save a resource"},
	"resource remove": {"risky", "Remove a saved resource"},

	// Secrets and credentials.
	"secret add":          {"risky", "Store a secret"},
	"secret list":         {"read", "List the stored secrets"},
	"secret remove":       {"risky", "Remove a stored secret"},
	"credential list":     {"read", "List the credentials and their rules"},
	"credential request":  {"never", "Ask for a credential for an agent session"},
	"credential get":      {"never", "Print a credential's value"},
	"credential exec":     {"never", "Run a command with a credential"},
	"credential git":      {"never", "Give git a credential"},
	"credential gh-token": {"never", "Print a GitHub token"},

	// Approval devices.
	"device list":        {"read", "List the approval devices"},
	"device reset":       {"never", "Remove every approval device"},
	"device attestation": {"read", "Print a device's attestation chain"},

	// Integrations.
	"integration token create": {"risky", "Make an integration token"},
	"integration token list":   {"read", "List the integration tokens"},
	"integration token revoke": {"risky", "Revoke an integration token"},
	"integration connect":      {"risky", "Connect Legato to an integration"},
	"integration status":       {"read", "Show the integrations' connections"},
	"integration disconnect":   {"risky", "Disconnect an integration"},

	// Audit log.
	"audit verify": {"read", "Check the audit log"},

	// Agents.
	"agent state":               {"never", "Report an agent's activity"},
	"agent permission-resolved": {"never", "Report a resolved permission request"},
	"agent stopped":             {"never", "Report that an agent stopped"},
	"agent allow-read":          {"never", "Allow an agent to read a pasted image"},
	"agent hook":                {"never", "Report an agent tool's hook"},
	"agent session-created":     {"never", "Record an agent session"},
	"agent peers":               {"read", "List the agents sharing a worktree"},
	"agent message":             {"change", "Send a message to a running agent"},
	"agent broadcast":           {"never", "Message every agent sharing a worktree"},
	"agent summary":             {"read", "Summarize the agent sessions"},
	"agent status":              {"read", "Show an agent's status"},
	"agent codex":               {"never", "Start Codex with its bridge"},
	"agent codex-bridge":        {"never", "Serve a Codex session"},
	"agent bridge":              {"never", "Serve a Claude Code session"},
	"agent link chimera":        {"never", "Connect a Chimera mod"},

	// Plans.
	"plan validate": {"read", "Validate a plan"},
	"plan submit":   {"change", "Submit a plan"},
	"plan show":     {"read", "Show a plan"},
	"plan feedback": {"read", "Show a plan's feedback"},
	"plan status":   {"read", "Show a plan's status"},
	"plan answer":   {"change", "Answer a question on a plan"},
	"plan complete": {"change", "Mark a plan complete"},
	"plan withdraw": {"change", "Withdraw a plan"},
	"plan claim":    {"change", "Claim a plan"},
	"plan inbox":    {"read", "List the plans waiting"},

	// Reviews.
	"review chapter show":      {"read", "Show a review chapter"},
	"review chapter edit":      {"change", "Edit a review chapter"},
	"review chapter remove":    {"change", "Remove a review chapter"},
	"review chapters":          {"read", "List a review's chapters"},
	"review annotate":          {"change", "Annotate a review"},
	"review annotation edit":   {"change", "Edit a review annotation"},
	"review annotation remove": {"change", "Remove a review annotation"},
	"review answer":            {"change", "Answer a review question"},
	"review ready":             {"change", "Mark a review ready"},
	"review show":              {"read", "Show a review"},
	"review sync":              {"change", "Sync a review with Git"},
	"review discard":           {"risky", "Discard a review"},
	"review restart":           {"change", "Restart a review"},
	"review claim":             {"change", "Claim a review"},
	"review inbox":             {"read", "List the reviews waiting"},
	"review handoff template":  {"read", "Print a handoff template"},
	"review handoff submit":    {"change", "Submit a review handoff"},
	"review handoff show":      {"read", "Show a review's handoffs"},
	"review handoff export":    {"read", "Preview a handoff as Markdown"},
	"review-pass feedback":     {"read", "Show a review pass's feedback"},
	"review-pass answer":       {"risky", "Answer review feedback on GitHub"},

	// Workflows.
	"workflow list":          {"read", "List the workflows"},
	"workflow validate":      {"read", "Validate a workflow"},
	"workflow run":           {"change", "Run a workflow"},
	"workflow runs":          {"read", "List the workflow runs"},
	"workflow show":          {"read", "Show a workflow run"},
	"workflow cancel":        {"risky", "Cancel a workflow run"},
	"workflow retry":         {"change", "Retry a workflow run"},
	"workflow trigger":       {"change", "Trigger a workflow step"},
	"workflow step complete": {"never", "Complete an agent's workflow step"},
	"workflow step fail":     {"never", "Fail an agent's workflow step"},

	// Swarms.
	"swarm validate-plan": {"read", "Validate a swarm plan"},
	"swarm create":        {"change", "Start a swarm"},
	"swarm propose-plan":  {"change", "Propose a swarm plan"},
	"swarm cancel":        {"risky", "Cancel a swarm"},
	"swarm extend-plan":   {"change", "Extend a swarm plan"},
	"swarm dispatch":      {"change", "Dispatch a swarm worker"},
	"swarm message":       {"change", "Message a swarm worker"},
	"swarm broadcast":     {"change", "Message every swarm worker"},
	"swarm close":         {"change", "Close a swarm worker"},
	"swarm finish":        {"risky", "Finish a swarm"},
	"swarm next-step":     {"change", "Advance a swarm to its next step"},
	"swarm progress":      {"change", "Report a swarm worker's progress"},
	"swarm question":      {"change", "Ask a swarm's conductor a question"},
	"swarm built":         {"change", "Report a swarm worker's work built"},
	"swarm status":        {"read", "Show a swarm's status"},
	"swarm inbox":         {"read", "Show a swarm's inbox"},

	// Attention.
	"attention list": {"read", "List what needs attention"},

	// Learning.
	"learn record":               {"change", "Record a learning"},
	"learn show":                 {"read", "Show a learning"},
	"learn list":                 {"read", "List the learnings"},
	"learn search":               {"read", "Search the learnings"},
	"learn context":              {"read", "Find learnings for a task"},
	"learn curate":               {"change", "Start curating a task's learnings"},
	"learn consolidate":          {"change", "Start consolidating the learnings"},
	"learn accept":               {"change", "Accept a learning"},
	"learn edit":                 {"change", "Edit a learning"},
	"learn reject":               {"change", "Reject a learning"},
	"learn enable":               {"change", "Enable a learning in agent prompts"},
	"learn disable":              {"change", "Disable a learning in agent prompts"},
	"learn incorporate":          {"change", "Propose where to incorporate a learning"},
	"learn incorporation submit": {"change", "Submit a learning's incorporation"},
	"learn verify":               {"change", "Verify a learning's incorporation"},
	"learn drift":                {"change", "Check incorporated learnings for drift"},
	"learn reflection submit":    {"never", "Submit an agent's reflection"},

	// Hooks.
	"hooks install":   {"change", "Install the integration hooks"},
	"hooks uninstall": {"risky", "Uninstall the integration hooks"},

	// Servers.
	"mcp serve": {"never", "Serve Legato's tools to an agent"},
	"serve":     {"never", "Run the web server"},

	// Web token.
	"auth token":      {"never", "Print the web token"},
	"auth regenerate": {"never", "Replace the web token"},

	// Settings.
	"config show":   {"read", "Show the settings"},
	"config export": {"read", "Export the settings as YAML"},
	"config import": {"change", "Propose importing settings"},

	// Legato itself.
	"restart": {"risky", "Restart Legato"},
	"version": {"read", "Show Legato's version"},
	"pair":    {"never", "Pair a device or a main instance"},
}
