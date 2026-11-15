package main

// legatoCommandAlternatives says what the Reeve uses instead of a command
// it may never run, or of one it reached for that does not exist. A refusal
// of the legato tool names it. TestEveryNeverCommandHasAnAlternative keeps
// every never command here.
var legatoCommandAlternatives = map[string]string{
	// Commands that do not exist, which the Reeve has reached for.
	"credential show": "legato credential list (read) lists the credentials and their rules; GET /api/credentials gives the same, and GET /api/credentials/grants the grants",
	"audit":           "legato audit verify (read) checks the audit log; GET /api/credentials/audit reads its entries (credential uses, host actions, changes that ran without asking)",
	"audit log":       "GET /api/credentials/audit reads the audit log's entries; legato audit verify (read) checks it",
	"audit list":      "GET /api/credentials/audit reads the audit log's entries; legato audit verify (read) checks it",
	"audit show":      "GET /api/credentials/audit reads the audit log's entries; legato audit verify (read) checks it",

	// Secrets: the Reeve never holds one.
	"credential request":  "a credential is for an agent session's own commands, never the Reeve's: name a secret by its reference (legato://<name>) in a setting or a credential, or ask the operator for one with request_secret",
	"credential get":      "the Reeve never holds a secret: name it by its reference (legato://<name>) in a setting or a credential, or ask the operator for one with request_secret",
	"credential exec":     "the Reeve never holds a secret: propose the change that needs it, and Legato uses the credential itself",
	"credential git":      "git's helper is for an agent session's own pushes; the Reeve reads agents' work from the repository cache",
	"credential gh-token": "the Reeve never holds a token: GET /api/tasks/<id>?view=summary gives a task's pull requests",
	"auth token":          "the web token is the operator's and is never shown: your own token is already in legato_api",
	"auth regenerate":     "only the operator replaces the web token, at the host: ask them",
	"device reset":        "only the operator removes approval devices, at the host or in Settings › Devices: ask them",
	"pair":                "pairing is the operator's: ask them to pair from Settings, or legato pair at the host",

	// Servers, and an agent tool's own hooks and steps.
	"serve":                     "restart_legato restarts Legato; instance_status says how it runs",
	"mcp serve":                 "your own tools are this server already",
	"clone setup":               "a clone's setup runs when its agent starts; POST /api/tasks/<id>/agent starts one",
	"agent state":               "an agent tool's hooks report its state: read it with GET /api/agents?view=compact",
	"agent permission-resolved": "an agent tool's hooks report this: answer a permission in the operator's approval tray",
	"agent stopped":             "an agent tool's hooks report this: POST /api/agents/kill stops an agent",
	"agent allow-read":          "an agent tool's hooks ask this for a pasted image",
	"agent hook":                "an agent tool's hooks report its state: read it with GET /api/agents?view=compact",
	"agent session-created":     "an agent tool's hooks record this when a session starts",
	"agent broadcast":           "message each agent with legato agent message <task> or POST /api/tasks/<id>/agent {\"prompt\": …}",
	"agent codex":               "POST /api/tasks/<id>/agent {\"agent_kind\": \"codex\"} starts Codex on a task",
	"agent codex-bridge":        "Legato starts a Codex session's bridge itself",
	"agent bridge":              "Legato starts a Claude Code session's bridge itself",
	"agent link chimera":        "Legato's Chimera mod starts its link itself",
	"workflow step complete":    "a workflow's own agent completes its step: GET /api/workflow-runs/<id> reads a run, and POST …/steps/<step>/trigger or …/retry act on it",
	"workflow step fail":        "a workflow's own agent fails its step: POST /api/workflow-runs/<id>/cancel cancels a run",
	"learn reflection submit":   "an agent submits its own reflection: GET /api/learnings reads them",
}
