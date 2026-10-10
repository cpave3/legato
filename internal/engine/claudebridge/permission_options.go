package claudebridge

import (
	"encoding/json"
	"fmt"
	"strings"
)

// permissionOption is one way a request can be allowed. A request that lists
// options is allowed by naming one; a request that lists none takes a plain
// allow.
type permissionOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// updates are the Claude Code permission changes allowing this way
	// applies. They go back to the engine exactly as it wrote them.
	updates []json.RawMessage
}

const exitPlanModeTool = "ExitPlanMode"

const (
	optionAllowOnce   = "once"
	optionAllowAlways = "always"
	optionAcceptEdits = "accept_edits"
	optionManualEdits = "manual"
)

// planOf reads the plan an ExitPlanMode call asks the person to approve.
func planOf(tool string, input map[string]any) string {
	if tool != exitPlanModeTool {
		return ""
	}
	return stringArg(input, "plan")
}

// allowOptions lists the ways a request can be allowed, as Claude Code's own
// dialog offers them: a plan is approved into one of two edit modes, and a
// tool call can be allowed along with the rules the engine suggests so it
// does not ask again.
func allowOptions(tool string, suggestions []json.RawMessage) []permissionOption {
	if tool == exitPlanModeTool {
		return []permissionOption{
			{ID: optionAcceptEdits, Label: "Approve, and accept edits without asking", updates: []json.RawMessage{setMode("acceptEdits")}},
			{ID: optionManualEdits, Label: "Approve, and ask before each edit", updates: []json.RawMessage{setMode("default")}},
		}
	}
	if len(suggestions) == 0 {
		return nil
	}
	return []permissionOption{
		{ID: optionAllowOnce, Label: "Allow once"},
		{ID: optionAllowAlways, Label: "Allow, and " + describeUpdates(suggestions), updates: suggestions},
	}
}

func setMode(mode string) json.RawMessage {
	return json.RawMessage(`{"type":"setMode","mode":"` + mode + `","destination":"session"}`)
}

// chosenOption finds the option an allow names. An allow that names none
// takes the one that changes least: once for a tool call, manual edits for a
// plan.
func (p permission) chosenOption(id string) (permissionOption, error) {
	if id == "" {
		id = optionAllowOnce
		if p.plan != "" {
			id = optionManualEdits
		}
		if len(p.options) == 0 {
			return permissionOption{}, nil
		}
	}
	for _, option := range p.options {
		if option.ID == id {
			return option, nil
		}
	}
	return permissionOption{}, fmt.Errorf("%w: %s is not one of this request's options", errInvalidAnswer, id)
}

// permissionUpdate is one change Claude Code suggests to its permissions.
type permissionUpdate struct {
	Type  string `json:"type"`
	Rules []struct {
		ToolName    string `json:"toolName"`
		RuleContent string `json:"ruleContent"`
	} `json:"rules"`
	Mode        string   `json:"mode"`
	Directories []string `json:"directories"`
	Destination string   `json:"destination"`
}

// describeUpdates says in a few words what a set of suggested permission
// changes will allow, and for how long.
func describeUpdates(updates []json.RawMessage) string {
	var parts []string
	reach := 0
	for _, raw := range updates {
		var update permissionUpdate
		if json.Unmarshal(raw, &update) != nil {
			continue
		}
		reach = max(reach, destinationReach[update.Destination])
		switch update.Type {
		case "addRules":
			names := make([]string, 0, len(update.Rules))
			for _, rule := range update.Rules {
				name := rule.ToolName
				if rule.RuleContent != "" {
					name += "(" + rule.RuleContent + ")"
				}
				names = append(names, name)
			}
			if len(names) > 0 {
				parts = append(parts, "don't ask again for "+strings.Join(names, ", "))
			}
		case "addDirectories":
			if len(update.Directories) > 0 {
				parts = append(parts, "always allow access to "+strings.Join(update.Directories, ", "))
			}
		case "setMode":
			if update.Mode == "acceptEdits" {
				parts = append(parts, "accept edits")
			} else if update.Mode != "" {
				parts = append(parts, "switch to "+update.Mode+" mode")
			}
		}
	}
	if len(parts) == 0 {
		parts = []string{"don't ask again"}
	}
	said := parts[0]
	if len(parts) > 1 {
		said = strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
	}
	return said + reachSuffix[reach]
}

// destinationReach orders where a permission change is saved by how far it
// carries; a label names the furthest.
var destinationReach = map[string]int{"session": 1, "localSettings": 2, "projectSettings": 2, "userSettings": 3}

var reachSuffix = map[int]string{1: " for this session", 2: " in this project", 3: " in every project"}

// denialMessage is what the model reads when the person denies a request in
// Legato. It follows the wording of a denial at Claude Code's own dialog: a
// bare reason reads to the model as a tool's error, not as the person's word.
func denialMessage(feedback string) string {
	message := "The user doesn't want to proceed with this tool use: they rejected it from Legato, where they are following this session."
	if feedback = strings.TrimSpace(feedback); feedback != "" {
		message += " To tell you how to proceed, the user said:\n" + feedback
	}
	return message
}
