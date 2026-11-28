// Package managedhooks reads a folder of managed hooks: one folder per hook,
// each holding a HOOK.yaml manifest that names the scripts the hook runs on
// which events, beside those scripts. It checks every manifest in full and
// knows nothing of Legato or of how an agent tool runs a hook. See
// docs/ai/managed-hooks.md.
package managedhooks

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/cpave3/legato/internal/leaf/subfolders"
	"gopkg.in/yaml.v3"
)

// FileName is the manifest that makes a folder a managed hook.
const FileName = "HOOK.yaml"

// Event is a moment of an agent's session a hook runs at.
type Event string

// The events a manifest may name, in the order a session meets them.
const (
	SessionStart Event = "session_start"
	PreTool      Event = "pre_tool"
	PostTool     Event = "post_tool"
	Stop         Event = "stop"
)

// Events is every event, in the order a session meets them.
func Events() []Event { return []Event{SessionStart, PreTool, PostTool, Stop} }

// TimeLimit is an event's time limit in seconds: the default a manifest
// gets when it names none, and the most it may name. ok is false for an
// event that is not one of Events.
func TimeLimit(e Event) (def, max int, ok bool) {
	switch e {
	case SessionStart, PreTool:
		return 10, 30, true
	case PostTool:
		return 30, 120, true
	case Stop:
		return 120, 600, true
	}
	return 0, 0, false
}

// The tool matchers of pre_tool and post_tool: kinds of tool call, the same
// whichever agent tool makes it. MCPPrefix is followed by an MCP server's
// name.
const (
	MatchShell    = "shell"
	MatchFileEdit = "file_edit"
	MatchFileRead = "file_read"
	MatchSearch   = "search"
	MatchWeb      = "web"
	MCPPrefix     = "mcp:"
)

// AgentTools is every agent tool a manifest's agent_tools may name. Whether
// a tool can be given hooks is not this package's question.
func AgentTools() []string { return []string{"claude-code", "chimera", "codex"} }

// Hook is one folder of a managed-hooks folder.
type Hook struct {
	// Name is the folder's name, which the manifest's name must equal.
	Name string
	// Description says what the hook does, for the operator.
	Description string
	// Path is the folder as listed: a link to a hook stays a link.
	Path string
	// AgentTools are the agent tools the hook is for; empty is every one.
	AgentTools []string
	// ReadOnly gives the hook to read-only agents too.
	ReadOnly bool
	// Resources are the named resources the hook's scripts need.
	Resources []Resource
	// YieldsTo says when an agent's own hook already does this one's work.
	YieldsTo YieldsTo
	// Events are what the hook runs, by event.
	Events map[Event]Handler
	// Err says why the hook cannot be used; empty when it can. A hook with
	// an error has only its Name and Path, and its Description when read.
	Err string
}

// OK reports whether the hook can be given to an agent.
func (h Hook) OK() bool { return h.Err == "" }

// EventNames are the events the hook runs at, in the order a session meets
// them.
func (h Hook) EventNames() []Event {
	var names []Event
	for _, e := range Events() {
		if _, ok := h.Events[e]; ok {
			names = append(names, e)
		}
	}
	return names
}

// Handler is what a hook runs at one event.
type Handler struct {
	// Command is run from the hook's folder.
	Command string
	// Timeout is in seconds: the manifest's, else the event's default.
	Timeout int
	// Match are the tool matchers of pre_tool and post_tool; empty matches
	// every tool call.
	Match []string
}

// Resource is a named resource a hook's scripts need.
type Resource struct {
	// Name is the resource's name.
	Name string
	// As is the suffix of the variable that gives the resource's path: the
	// manifest's as, else Name upper-cased.
	As string
}

// YieldsTo says when an agent's own hook already does a managed hook's work.
type YieldsTo struct {
	// ClaudeCodeCommand matches the commands of Claude Code hooks; nil
	// when the manifest gives none.
	ClaudeCodeCommand *regexp.Regexp
	// ChimeraHook is a glob of Chimera hook file names; empty when the
	// manifest gives none.
	ChimeraHook string
}

// MatchesChimeraHook reports whether a Chimera hook file's name matches
// ChimeraHook.
func (y YieldsTo) MatchesChimeraHook(name string) bool {
	if y.ChimeraHook == "" {
		return false
	}
	ok, _ := filepath.Match(y.ChimeraHook, name)
	return ok
}

// namePattern is the skills' rule for a folder's name: lowercase letters,
// digits and hyphens, at most 64 characters.
var namePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

const maxNameLength = 64

var (
	variableSuffix = regexp.MustCompile(`^[A-Z0-9_]+$`)
	mcpServer      = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
)

// Scan lists the hooks in dir, sorted by name. A folder that is not a usable
// hook is listed with its Err; files, and folders whose names start with a
// dot, are not listed. A dir that does not exist holds no hooks. Scan fails
// only when dir cannot be read.
func Scan(dir string) ([]Hook, error) {
	names, err := subfolders.List(dir)
	if err != nil {
		return nil, err
	}
	list := make([]Hook, 0, len(names))
	for _, name := range names {
		list = append(list, read(name, filepath.Join(dir, name)))
	}
	return list, nil
}

func read(name, path string) Hook {
	hook := Hook{Name: name, Path: path}
	if len(name) > maxNameLength || !namePattern.MatchString(name) {
		hook.Err = fmt.Sprintf("the folder's name must be lowercase letters, digits and hyphens, at most %d characters", maxNameLength)
		return hook
	}
	content, err := os.ReadFile(filepath.Join(path, FileName))
	if errors.Is(err, os.ErrNotExist) {
		hook.Err = "no " + FileName + " in the folder"
		return hook
	}
	if err != nil {
		hook.Err = fmt.Sprintf("reading %s: %v", FileName, err)
		return hook
	}
	parsed, err := parse(name, content)
	if err != nil {
		hook.Err = fmt.Sprintf("%s: %v", FileName, err)
		hook.Description = parsed.Description
		return hook
	}
	parsed.Name, parsed.Path = name, path
	return parsed
}

type manifest struct {
	Name        string                `yaml:"name"`
	Description string                `yaml:"description"`
	AgentTools  []string              `yaml:"agent_tools"`
	ReadOnly    bool                  `yaml:"read_only"`
	Resources   []yaml.Node           `yaml:"resources"`
	YieldsTo    rawYieldsTo           `yaml:"yields_to"`
	Events      map[string]rawHandler `yaml:"events"`
}

type rawYieldsTo struct {
	ClaudeCodeCommand string `yaml:"claude_code_command"`
	ChimeraHook       string `yaml:"chimera_hook"`
}

type rawHandler struct {
	Command string   `yaml:"command"`
	Timeout *int     `yaml:"timeout"`
	Match   []string `yaml:"match"`
}

// parse checks a manifest in full. On an error the Hook holds what could be
// read, for the operator.
func parse(folder string, content []byte) (Hook, error) {
	var m manifest
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	if err := decoder.Decode(&m); err != nil {
		return Hook{}, fmt.Errorf("not a valid manifest: %v", err)
	}
	hook := Hook{Description: strings.TrimSpace(m.Description), ReadOnly: m.ReadOnly}
	if err := checkHeader(folder, m, hook.Description); err != nil {
		return hook, err
	}
	tools, err := agentTools(m.AgentTools)
	if err != nil {
		return hook, err
	}
	hook.AgentTools = tools
	if hook.Resources, err = resources(m.Resources); err != nil {
		return hook, err
	}
	if hook.YieldsTo, err = yieldsTo(m.YieldsTo); err != nil {
		return hook, err
	}
	if hook.Events, err = events(m.Events); err != nil {
		return hook, err
	}
	return hook, nil
}

func checkHeader(folder string, m manifest, description string) error {
	switch {
	case m.Name == "":
		return errors.New("no name")
	case m.Name != folder:
		return fmt.Errorf("the name %q is not the folder's name", m.Name)
	case description == "":
		return errors.New("no description")
	}
	return nil
}

func agentTools(names []string) ([]string, error) {
	for _, name := range names {
		if !slices.Contains(AgentTools(), name) {
			return nil, fmt.Errorf("agent_tools: unknown agent tool %q (known: %s)", name, strings.Join(AgentTools(), ", "))
		}
	}
	return names, nil
}

func resources(nodes []yaml.Node) ([]Resource, error) {
	var list []Resource
	seen := map[string]bool{}
	for i := range nodes {
		resource, err := resource(&nodes[i])
		if err != nil {
			return nil, fmt.Errorf("resources[%d]: %v", i, err)
		}
		if seen[resource.As] {
			return nil, fmt.Errorf("resources[%d]: the variable suffix %s is used twice", i, resource.As)
		}
		seen[resource.As] = true
		list = append(list, resource)
	}
	return list, nil
}

// resource reads an entry of resources: a name, or {name, as}.
func resource(node *yaml.Node) (Resource, error) {
	r, err := resourceEntry(node)
	if err != nil {
		return r, err
	}
	r.Name = strings.TrimSpace(r.Name)
	if r.Name == "" {
		return r, errors.New("no name: an entry is a name or {name, as}")
	}
	if r.As != "" {
		if !variableSuffix.MatchString(r.As) {
			return r, fmt.Errorf("as %q must be capital letters, digits and underscores", r.As)
		}
		return r, nil
	}
	r.As = strings.ToUpper(r.Name)
	if !variableSuffix.MatchString(r.As) {
		return r, fmt.Errorf("%q is not a variable suffix: give it an as of capital letters, digits and underscores", r.Name)
	}
	return r, nil
}

// resourceEntry decodes an entry's shape, checking a mapping's keys, since a
// node's own Decode would ignore unknown ones.
func resourceEntry(node *yaml.Node) (Resource, error) {
	switch node.Kind {
	case yaml.ScalarNode:
		return Resource{Name: node.Value}, nil
	case yaml.MappingNode:
		fields := map[string]string{}
		for i := 0; i+1 < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			if key.Value != "name" && key.Value != "as" {
				return Resource{}, fmt.Errorf("unknown field %q: an entry is a name or {name, as}", key.Value)
			}
			if value.Kind != yaml.ScalarNode {
				return Resource{}, fmt.Errorf("%s must be a string", key.Value)
			}
			fields[key.Value] = value.Value
		}
		return Resource{Name: fields["name"], As: fields["as"]}, nil
	}
	return Resource{}, errors.New("an entry is a name or {name, as}")
}

func yieldsTo(raw rawYieldsTo) (YieldsTo, error) {
	var y YieldsTo
	if raw.ClaudeCodeCommand != "" {
		re, err := regexp.Compile(raw.ClaudeCodeCommand)
		if err != nil {
			return y, fmt.Errorf("yields_to.claude_code_command: %v", err)
		}
		y.ClaudeCodeCommand = re
	}
	if raw.ChimeraHook != "" {
		if _, err := filepath.Match(raw.ChimeraHook, ""); err != nil {
			return y, fmt.Errorf("yields_to.chimera_hook: %q is not a glob: %v", raw.ChimeraHook, err)
		}
		y.ChimeraHook = raw.ChimeraHook
	}
	return y, nil
}

func events(raw map[string]rawHandler) (map[Event]Handler, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("no events: name at least one of %s", eventList())
	}
	names := make([]string, 0, len(raw))
	for name := range raw {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make(map[Event]Handler, len(raw))
	for _, name := range names {
		handler, err := eventHandler(Event(name), raw[name])
		if err != nil {
			return nil, err
		}
		out[Event(name)] = handler
	}
	return out, nil
}

func eventHandler(event Event, raw rawHandler) (Handler, error) {
	def, max, ok := TimeLimit(event)
	if !ok {
		return Handler{}, fmt.Errorf("events: unknown event %q (known: %s)", event, eventList())
	}
	h := Handler{Command: strings.TrimSpace(raw.Command), Timeout: def, Match: raw.Match}
	if h.Command == "" {
		return h, fmt.Errorf("events.%s: no command", event)
	}
	if raw.Timeout != nil {
		if *raw.Timeout <= 0 || *raw.Timeout > max {
			return h, fmt.Errorf("events.%s: timeout must be from 1 to at most %d seconds", event, max)
		}
		h.Timeout = *raw.Timeout
	}
	return h, checkMatch(event, raw.Match)
}

// checkMatch checks an event's tool matchers, which only pre_tool and
// post_tool take.
func checkMatch(event Event, match []string) error {
	if len(match) > 0 && event != PreTool && event != PostTool {
		return fmt.Errorf("events.%s: match is for pre_tool and post_tool only", event)
	}
	for _, m := range match {
		if !knownMatcher(m) {
			return fmt.Errorf("events.%s: unknown matcher %q (known: shell, file_edit, file_read, search, web, mcp:<server>)", event, m)
		}
	}
	return nil
}

func knownMatcher(m string) bool {
	switch m {
	case MatchShell, MatchFileEdit, MatchFileRead, MatchSearch, MatchWeb:
		return true
	}
	server, ok := strings.CutPrefix(m, MCPPrefix)
	return ok && mcpServer.MatchString(server)
}

func eventList() string {
	names := make([]string, 0, 4)
	for _, e := range Events() {
		names = append(names, string(e))
	}
	return strings.Join(names, ", ")
}
