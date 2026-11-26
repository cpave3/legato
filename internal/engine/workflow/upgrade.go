package workflow

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// Upgrading a schema_version 1 file to version 2 (spec LEG-482 §8,
// LEG-484). Upgrade rewrites the file's text in place, as the web builder's
// document model does (LEG-470): only what it changes is written, so every
// other byte, comments included, stays. It
//
//   - sets schema_version: 2;
//   - renames a step's trigger: to start: (an empty one goes; agent_ended
//     has no v2 start, see below);
//   - renames agent.run's keep_agent: to keep:;
//   - rewrites a step that starts when the agent ends (trigger:
//     agent_ended). The agent step before it (the run's own agent) waits for
//     complete (wait_for: [complete]) and, with keep_agent, keeps its agent;
//     an agent.stop of that agent then waits for it to end (wait_for:
//     [ended], agent: {from: <step>}). Any other such step gets that
//     agent.stop inserted before it;
//   - runs the step rewrites registered for a step type (stepRewrites),
//     which may replace or edit their step and say where its outputs went:
//     a later reference to an output a rewrite maps is rewritten, and one it
//     cannot map is flagged, as an error, at that reference's line.
//
// The step rewrites run on a version 2 file too, a loop's body's steps
// included, and are all an upgrade changes there: the inline review step's
// (LEG-599, review_passes_deprecation.go) rewrites it to call review-loop.
//
// The web editor's Upgrade to v2 and quick fix, and legato workflow
// upgrade, use it.

// StepRewrite rewrites a version 1 step of one type for version 2.
type StepRewrite func(step *UpgradeStep)

// stepRewrites are the step rewrites by step type: the inline review
// step's (LEG-599).
var stepRewrites = map[string]StepRewrite{}

// UpgradeStep is a version 1 step a StepRewrite rewrites.
type UpgradeStep struct {
	ID   string
	Type string
	// With is the step's with as the file gives it.
	With map[string]any
	// Line is the step's line in the file being upgraded.
	Line int

	replacement *string
	outputs     map[string]outputMapping
	newType     string
	wrap        *withWrap
	drop        []string
	notes       []string
}

// withWrap nests a step's with under a key beside one pair (workflow:
// review-loop, inputs: <the with>), keeping only the keys keep takes.
type withWrap struct {
	key, value, under string
	keep              func(key string) bool
}

type outputMapping struct{ to, reason string }

// Replace replaces the step with steps, a YAML list of version 2 steps
// written at the left margin ("- id: x\n  type: …\n"); the upgrade indents
// it as the step was.
func (s *UpgradeStep) Replace(steps string) {
	if !strings.HasSuffix(steps, "\n") {
		steps += "\n"
	}
	s.replacement = &steps
}

// SetType changes the step's type, in place.
func (s *UpgradeStep) SetType(stepType string) { s.newType = stepType }

// WrapWith nests the step's with under the key under, beside key: value,
// in place and with its comments; a with key keep refuses is left out.
func (s *UpgradeStep) WrapWith(key, value, under string, keep func(key string) bool) {
	s.wrap = &withWrap{key: key, value: value, under: under, keep: keep}
}

// Drop removes one of the step's keys, such as outputs.
func (s *UpgradeStep) Drop(key string) { s.drop = append(s.drop, key) }

// Note says what the upgrade could not do for the step, as a warning at
// its type in the upgraded file.
func (s *UpgradeStep) Note(message string) { s.notes = append(s.notes, message) }

// MapOutput says that a later reference to the step's output name
// (steps.<id>.outputs.<name>) reads path in version 2, such as
// steps.review.outputs.outcome.
func (s *UpgradeStep) MapOutput(name, path string) {
	s.output()[name] = outputMapping{to: path}
}

// Unmapped says that the step's output name has no version 2 equivalent:
// each later reference to it is flagged, at its line, with the reason.
func (s *UpgradeStep) Unmapped(name, reason string) {
	s.output()[name] = outputMapping{reason: reason}
}

func (s *UpgradeStep) output() map[string]outputMapping {
	if s.outputs == nil {
		s.outputs = map[string]outputMapping{}
	}
	return s.outputs
}

// UpgradeResult is an upgraded file: its text, whether it changed, and
// notes on what the upgrade could not do, each at its place in YAML: as
// warnings, but a reference to an output that has no equal, which is an
// error; with the upgraded file's own problems when it does not load (as
// errors).
type UpgradeResult struct {
	YAML    []byte       `json:"yaml"`
	Changed bool         `json:"changed"`
	Notes   []Diagnostic `json:"notes"`
}

// Upgrade upgrades a version 1 workflow file to version 2. A version 2 file
// comes back unchanged; a file that does not load is an error, as Parse
// gives it.
func Upgrade(source string, data []byte) (*UpgradeResult, error) {
	return upgrade(source, data, stepRewrites)
}

func upgrade(source string, data []byte, rewrites map[string]StepRewrite) (*UpgradeResult, error) {
	definition, err := Parse(source, data)
	if err != nil {
		return nil, err
	}
	versionOne := definition.SchemaVersion == SchemaVersion
	if !versionOne && !rewritesAny(definition.Steps, rewrites) {
		return &UpgradeResult{YAML: data, Notes: []Diagnostic{}}, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil || len(doc.Content) == 0 {
		return nil, fmt.Errorf("upgrade %s: %v", source, err)
	}
	u := &upgrader{text: newSourceText(data), notes: []Diagnostic{}}
	root := doc.Content[0]
	_, list := pairOf(root, "steps")
	if list == nil || list.Kind != yaml.SequenceNode || len(list.Content) != len(definition.Steps) {
		return nil, fmt.Errorf("upgrade %s: steps is not a list", source)
	}
	if versionOne {
		if _, version := pairOf(root, "schema_version"); version != nil {
			u.replaceScalar(version, "2")
		}
		u.flowList = list.Style&yaml.FlowStyle != 0
		u.steps = list.Content
		ids := map[string]bool{}
		for _, step := range definition.Steps {
			ids[step.ID] = true
		}
		for i := range definition.Steps {
			node := list.Content[i]
			if node.Kind != yaml.MappingNode {
				return nil, fmt.Errorf("upgrade %s: step %d is not a mapping", source, i+1)
			}
			u.upgradeStep(definition.Steps, i, node, ids)
		}
	}
	u.planned = map[*yaml.Node]*UpgradeStep{}
	if err := u.plan(definition.Steps, list, rewrites); err != nil {
		return nil, fmt.Errorf("upgrade %s: %v", source, err)
	}
	// References are read in their own scopes before any step is edited,
	// so a reference in a with the rewrite re-encodes is mapped in it.
	last := u.mapScope(definition.Steps, list, map[string]map[string]outputMapping{}, u.valueEnd(root, "steps"))
	for _, pair := range u.pairs(root, len(data)) {
		if pair.key == "outputs" {
			// The workflow's outputs read the last scope of its steps.
			u.mapText(pair.start, pair.end, last, nil)
		}
	}
	var replaced [][2]int
	if err := u.rewriteSteps(definition.Steps, list, &replaced); err != nil {
		return nil, fmt.Errorf("upgrade %s: %v", source, err)
	}
	u.commitReferences(replaced)
	out := u.apply()
	result := &UpgradeResult{YAML: out, Changed: !bytes.Equal(out, data), Notes: u.notes}
	if _, problems := Check(source, out); len(problems) > 0 {
		for _, problem := range problems {
			if problem.Severity == SeverityError {
				result.Notes = append(result.Notes, problem)
			}
		}
	}
	sort.SliceStable(result.Notes, func(i, j int) bool {
		a, b := result.Notes[i], result.Notes[j]
		return a.Line < b.Line || (a.Line == b.Line && a.Column < b.Column)
	})
	return result, nil
}

// rewritesAny says whether a step rewrite applies to any of steps, a loop's
// body's included.
func rewritesAny(steps []Step, rewrites map[string]StepRewrite) bool {
	found := false
	eachStep(steps, "steps", func(step Step, _ string, _ []Step, _ map[string]any) {
		_, ok := rewrites[step.Type]
		found = found || ok
	})
	return found
}

// rewriteSteps runs the step rewrites on a steps: list and the bodies of
// its loops.
// plan runs the step rewrites on a steps: list and its loops' bodies, and
// keeps what each asks for its step (u.planned), so references can be
// mapped before any edit.
func (u *upgrader) plan(steps []Step, list *yaml.Node, rewrites map[string]StepRewrite) error {
	if list == nil || list.Kind != yaml.SequenceNode || len(list.Content) != len(steps) {
		return fmt.Errorf("steps is not a list")
	}
	for i, step := range steps {
		node := list.Content[i]
		if node.Kind != yaml.MappingNode {
			return fmt.Errorf("step %d is not a mapping", i+1)
		}
		if step.Loop != nil {
			_, body := pairOf(node, "steps")
			if err := u.plan(step.Steps, body, rewrites); err != nil {
				return err
			}
			continue
		}
		if rewrite, ok := rewrites[step.Type]; ok {
			rewritten := &UpgradeStep{ID: step.ID, Type: step.Type, With: step.With, Line: node.Line}
			rewrite(rewritten)
			u.planned[node] = rewritten
		}
	}
	return nil
}

func (u *upgrader) rewriteSteps(steps []Step, list *yaml.Node, replaced *[][2]int) error {
	if list == nil || list.Kind != yaml.SequenceNode || len(list.Content) != len(steps) {
		return fmt.Errorf("steps is not a list")
	}
	flowList := u.flowList
	defer func() { u.flowList = flowList }()
	for i, step := range steps {
		node := list.Content[i]
		u.flowList = list.Style&yaml.FlowStyle != 0
		if node.Kind != yaml.MappingNode {
			return fmt.Errorf("step %d is not a mapping", i+1)
		}
		if step.Loop != nil {
			_, body := pairOf(node, "steps")
			if err := u.rewriteSteps(step.Steps, body, replaced); err != nil {
				return err
			}
			continue
		}
		rewritten := u.planned[node]
		if rewritten == nil {
			continue
		}
		if rewritten.replacement != nil {
			start, end, prefix, ok := u.itemRange(node)
			if !ok {
				u.note(node.Line, node.Column, fmt.Sprintf("step %q: write it as a block list item to upgrade it", step.ID))
				continue
			}
			// The step's own edits are dropped: the replacement is the step.
			u.dropWithin(start, end)
			u.edit(start, end, indentItem(*rewritten.replacement, prefix))
			*replaced = append(*replaced, [2]int{start, end})
			continue
		}
		u.editStep(node, rewritten)
	}
	return nil
}

// editStep makes a rewrite's changes to its step in place: its type, its
// with, the keys it drops, and its notes at its type.
func (u *upgrader) editStep(node *yaml.Node, step *UpgradeStep) {
	_, typeValue := pairOf(node, "type")
	if node.Style&yaml.FlowStyle != 0 && (step.newType != "" || step.wrap != nil || len(step.drop) > 0) {
		u.note(node.Line, node.Column, fmt.Sprintf("step %q: write it as a block mapping to upgrade it", step.ID))
		return
	}
	if step.newType != "" && typeValue != nil {
		u.replaceScalar(typeValue, step.newType)
	}
	if step.wrap != nil {
		u.wrapWith(node, step)
	}
	for _, key := range step.drop {
		if k, v := pairOf(node, key); k != nil {
			u.removePair(node, k, v)
		}
	}
	for _, message := range step.notes {
		if typeValue == nil {
			u.note(node.Line, node.Column, message)
			continue
		}
		start := u.text.offset(typeValue)
		end, ok := u.text.scalarEnd(typeValue, start)
		if !ok {
			end = start + 1
		}
		u.flags = append(u.flags, flag{start: start, end: end, message: message, severity: SeverityWarning})
	}
}

// wrapWith rewrites a step's with pair as with: {key: value, under: <the
// with, less what keep refuses>}, the with's own styles and comments kept.
func (u *upgrader) wrapWith(node *yaml.Node, step *UpgradeStep) {
	key, with := pairOf(node, "with")
	if key == nil || with == nil || with.Kind != yaml.MappingNode {
		u.note(node.Line, node.Column, fmt.Sprintf("step %q: write its with as a mapping to upgrade it", step.ID))
		return
	}
	start := u.text.offset(key)
	if !u.text.startsLine(start) {
		u.note(key.Line, key.Column, fmt.Sprintf("step %q: put with on a line of its own to upgrade it", step.ID))
		return
	}
	end := u.text.blockEnd(u.text.lineStart(start), key.Column-1)
	// Blank lines after the with belong between the steps.
	for end > start {
		previous := u.text.lineStart(end - 1)
		if !isBlank(string(u.text.data[previous:end])) || previous <= start {
			break
		}
		end = previous
	}
	inputs := *with
	inputs.Content = nil
	for i := 0; i+1 < len(with.Content); i += 2 {
		if step.wrap.keep(with.Content[i].Value) {
			inputs.Content = append(inputs.Content, with.Content[i], with.Content[i+1])
		}
	}
	inputs.Line, inputs.Column = 0, 0
	wrapped := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Value: step.wrap.key}, {Kind: yaml.ScalarNode, Value: step.wrap.value},
		{Kind: yaml.ScalarNode, Value: step.wrap.under}, &inputs,
	}}
	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(wrapped); err != nil {
		u.note(key.Line, key.Column, fmt.Sprintf("step %q: rewrite its with by hand: %v", step.ID, err))
		return
	}
	_ = encoder.Close()
	pad := strings.Repeat(" ", key.Column+1)
	var text strings.Builder
	text.WriteString("with:\n")
	for _, line := range strings.SplitAfter(out.String(), "\n") {
		if line == "" {
			continue
		}
		if isBlank(line) {
			text.WriteString(line)
			continue
		}
		text.WriteString(pad + line)
	}
	u.edit(start, end, text.String())
	u.wrapped = append(u.wrapped, [2]int{start, end})
}

// upgradeStep makes the version 1 step at i of steps a version 2 one.
func (u *upgrader) upgradeStep(steps []Step, i int, node *yaml.Node, ids map[string]bool) {
	step := steps[i]
	if step.Type == "agent.run" {
		if _, with := pairOf(node, "with"); with != nil {
			if key, _ := pairOf(with, "keep_agent"); key != nil {
				u.renameKey(key, "keep")
			}
		}
	}
	key, value := pairOf(node, "trigger")
	if key == nil {
		return
	}
	switch value.Value {
	case "":
		u.removePair(node, key, value)
		return
	case StartAgentEnded:
	default:
		u.renameKey(key, "start")
		return
	}
	// The step started when the run's agent ended: the agent the run's own
	// agent.run step before it started.
	source := -1
	for j := i - 1; j >= 0; j-- {
		if steps[j].Type == "agent.run" && !AgentOnTask(steps[j].With) && steps[j].With["task"] == nil {
			source = j
			break
		}
	}
	u.removePair(node, key, value)
	if source >= 0 && stepKeeps(steps[source].With) && len(steps[source].WaitFor) == 0 && !u.waits[source] {
		u.insertPair(u.list(source), "type", "wait_for: [complete]")
		if u.waits == nil {
			u.waits = map[int]bool{}
		}
		u.waits[source] = true
	}
	agent := "task: ${{ workflow.task }}"
	if source >= 0 {
		agent = "agent: {from: " + steps[source].ID + "}"
	}
	if step.Type == "agent.stop" && len(step.With) == 1 && isWorkflowTask(step.With["task"]) {
		u.insertPair(node, "type", "wait_for: [ended]")
		if source >= 0 {
			_, with := pairOf(node, "with")
			taskKey, taskValue := pairOf(with, "task")
			u.renameKey(taskKey, "agent")
			u.replaceScalar(taskValue, "{from: "+steps[source].ID+"}")
		}
		return
	}
	// Another step: an agent.stop before it waits for the agent to end.
	id := step.ID + "-agent-ended"
	for n := 2; ids[id]; n++ {
		id = fmt.Sprintf("%s-agent-ended-%d", step.ID, n)
	}
	ids[id] = true
	start, _, prefix, ok := u.itemRange(node)
	if !ok {
		u.note(node.Line, node.Column, fmt.Sprintf("step %q waited for the agent to end: add an agent.stop step before it with wait_for: [ended]", step.ID))
		return
	}
	u.edit(start, start, indentItem("- id: "+id+"\n  title: Wait for the agent to end\n  type: agent.stop\n  wait_for: [ended]\n  with:\n    "+agent+"\n", prefix))
}

var workflowTaskPattern = regexp.MustCompile(`^\$\{\{\s*workflow\.task\s*\}\}$`)

func isWorkflowTask(value any) bool {
	text, ok := value.(string)
	return ok && workflowTaskPattern.MatchString(text)
}

// list is the step node at i, set while the steps are upgraded.
func (u *upgrader) list(i int) *yaml.Node { return u.steps[i] }

// mapScope maps the references of a steps: list (from start to end of its
// text) to the outputs of rewritten steps, each read in its own scope as
// the engine reads it: the nearest step of its ID before it, in its own
// list, then in each list around it. visible is what the list's steps see
// before their own (an ID a later step reuses shadows it). It returns the
// list's last scope, which a loop's until, next and outputs and the
// workflow's outputs read.
func (u *upgrader) mapScope(steps []Step, list *yaml.Node, visible map[string]map[string]outputMapping, end int) map[string]map[string]outputMapping {
	scope := make(map[string]map[string]outputMapping, len(visible))
	for id, mapping := range visible {
		scope[id] = mapping
	}
	if list == nil || len(list.Content) != len(steps) {
		return scope
	}
	for i, step := range steps {
		node := list.Content[i]
		stepEnd := end
		if i+1 < len(list.Content) {
			stepEnd = u.text.offset(list.Content[i+1])
		}
		if step.Loop != nil {
			_, body := pairOf(node, "steps")
			_, loop := pairOf(node, "loop")
			inner := u.mapScope(step.Steps, body, scope, u.valueEnd(node, "steps", stepEnd))
			// The loop's if: and over read the scope before it; its
			// until, next and outputs read its pass's.
			for _, pair := range u.pairs(node, stepEnd) {
				switch pair.key {
				case "steps":
				case "outputs":
					u.mapText(pair.start, pair.end, inner, nil)
				case "loop":
					for _, sub := range u.pairs(loop, pair.end) {
						if sub.key == "over" {
							u.mapText(sub.start, sub.end, scope, nil)
						} else {
							u.mapText(sub.start, sub.end, inner, nil)
						}
					}
				default:
					u.mapText(pair.start, pair.end, scope, nil)
				}
			}
			scope[step.ID] = nil // a loop's outputs are its own
			continue
		}
		planned := u.planned[node]
		if planned == nil || planned.replacement == nil {
			var with *yaml.Node
			if planned != nil && planned.wrap != nil {
				_, with = pairOf(node, "with")
			}
			u.mapText(u.text.offset(node), stepEnd, scope, with)
		}
		if planned != nil && len(planned.outputs) > 0 {
			scope[step.ID] = planned.outputs
		} else {
			scope[step.ID] = nil
		}
	}
	return scope
}

// referenceSite is a reference to a rewritten step's output: the text it
// takes and what it becomes, or why it cannot be mapped.
type referenceSite struct {
	start, end int
	reference  string
	mapping    outputMapping
}

// mapText finds, between start and end, each reference to an output of a
// rewritten step scope sees, and keeps it to edit (commitReferences). With
// a with node (one the rewrite re-encodes), its scalars are mapped in place
// too, so the re-encoded inputs carry the mapped references.
func (u *upgrader) mapText(start, end int, scope map[string]map[string]outputMapping, with *yaml.Node) {
	if end > len(u.text.data) {
		end = len(u.text.data)
	}
	if start >= end {
		return
	}
	text := u.text.data
	for _, id := range sortedKeys(scope) {
		for _, name := range sortedKeys(scope[id]) {
			mapping := scope[id][name]
			reference := "steps." + id + ".outputs." + name
			for from := start; from < end; {
				at := bytes.Index(text[from:end], []byte(reference))
				if at < 0 {
					break
				}
				refStart := from + at
				refEnd := refStart + len(reference)
				from = refEnd
				if (refStart > 0 && isPathByte(text[refStart-1])) || (refEnd < len(text) && isPathByte(text[refEnd])) {
					continue
				}
				u.sites = append(u.sites, referenceSite{start: refStart, end: refEnd, reference: reference, mapping: mapping})
			}
			if with != nil && mapping.reason == "" {
				mapScalars(with, reference, mapping.to)
			}
		}
	}
}

// mapScalars replaces reference with to in every scalar under node.
func mapScalars(node *yaml.Node, reference, to string) {
	if node.Kind == yaml.ScalarNode {
		var out strings.Builder
		value := node.Value
		for {
			at := strings.Index(value, reference)
			if at < 0 {
				out.WriteString(value)
				break
			}
			end := at + len(reference)
			if (at > 0 && isPathByte(value[at-1])) || (end < len(value) && isPathByte(value[end])) {
				out.WriteString(value[:end])
				value = value[end:]
				continue
			}
			out.WriteString(value[:at] + to)
			value = value[end:]
		}
		node.Value = out.String()
		return
	}
	for _, child := range node.Content {
		mapScalars(child, reference, to)
	}
}

// commitReferences edits the references mapScope found, and flags each it
// cannot map: none inside a step a replacement took, and inside a with the
// rewrite re-encoded only the flags (its scalars were mapped in place),
// found again in the edited text.
func (u *upgrader) commitReferences(replaced [][2]int) {
	nth := map[string]int{}
	for _, site := range u.sites {
		if inside(site.start, replaced) {
			continue
		}
		if r, ok := rangeOf(site.start, u.wrapped); ok {
			if site.mapping.reason != "" {
				key := fmt.Sprintf("%d/%s", r[0], site.reference)
				u.flags = append(u.flags, flag{start: site.start, end: site.end, severity: SeverityError, from: r[0], find: site.reference, nth: nth[key],
					message: fmt.Sprintf("%s: %s; change this reference by hand", site.reference, site.mapping.reason)})
				nth[key]++
			}
			continue
		}
		if site.mapping.reason == "" {
			u.edit(site.start, site.end, site.mapping.to)
			continue
		}
		u.flags = append(u.flags, flag{start: site.start, end: site.end, message: fmt.Sprintf("%s: %s; change this reference by hand", site.reference, site.mapping.reason), severity: SeverityError})
	}
}

// keyRange is a mapping key's text: from its key to the next key, or end.
type keyRange struct {
	key        string
	start, end int
}

// pairs are a mapping's keys and their text ranges, the last ending at end.
func (u *upgrader) pairs(mapping *yaml.Node, end int) []keyRange {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	var out []keyRange
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		start := u.text.offset(mapping.Content[i])
		stop := end
		if i+2 < len(mapping.Content) {
			stop = u.text.offset(mapping.Content[i+2])
		}
		out = append(out, keyRange{key: mapping.Content[i].Value, start: start, end: stop})
	}
	return out
}

// valueEnd is where key's value in mapping ends: the next key's offset, or
// end (the whole text when none is given).
func (u *upgrader) valueEnd(mapping *yaml.Node, key string, end ...int) int {
	stop := len(u.text.data)
	if len(end) > 0 {
		stop = end[0]
	}
	for _, pair := range u.pairs(mapping, stop) {
		if pair.key == key {
			return pair.end
		}
	}
	return stop
}

func rangeOf(offset int, ranges [][2]int) ([2]int, bool) {
	for _, r := range ranges {
		if offset >= r[0] && offset < r[1] {
			return r, true
		}
	}
	return [2]int{}, false
}

func isPathByte(b byte) bool {
	return b == '_' || b == '-' || b == '.' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func inside(offset int, ranges [][2]int) bool {
	for _, r := range ranges {
		if offset >= r[0] && offset < r[1] {
			return true
		}
	}
	return false
}

// --- Text edits ------------------------------------------------------------

type textEdit struct {
	start, end int
	text       string
}

type flag struct {
	start, end int
	message    string
	severity   Severity
	// find, when set, places the flag at the nth find after from in the
	// edited text: a reference inside text an edit rewrote.
	find      string
	from, nth int
}

type upgrader struct {
	text     *sourceText
	edits    []textEdit
	notes    []Diagnostic
	flags    []flag
	flowList bool
	steps    []*yaml.Node
	waits    map[int]bool
	// planned are the step rewrites' changes, by step node; sites the
	// references mapScope found; wrapped the withs wrapWith rewrote.
	planned map[*yaml.Node]*UpgradeStep
	sites   []referenceSite
	wrapped [][2]int
}

func (u *upgrader) edit(start, end int, text string) {
	u.edits = append(u.edits, textEdit{start, end, text})
}

func (u *upgrader) note(line, column int, message string) {
	u.notes = append(u.notes, Diagnostic{Severity: SeverityWarning, Message: message, Line: line, Column: column, EndLine: line, EndColumn: column + 1})
}

// dropWithin drops the edits inside a range a replacement takes.
func (u *upgrader) dropWithin(start, end int) {
	kept := u.edits[:0]
	for _, edit := range u.edits {
		if edit.start >= start && edit.end <= end && !(edit.start == end && edit.end == end) {
			continue
		}
		kept = append(kept, edit)
	}
	u.edits = kept
}

// apply applies the edits, and places each flag in the edited text.
func (u *upgrader) apply() []byte {
	edits := append([]textEdit(nil), u.edits...)
	// Last first; at one place, a range before an insertion, so the range
	// never takes the inserted text.
	sort.SliceStable(edits, func(i, j int) bool {
		if edits[i].start != edits[j].start {
			return edits[i].start > edits[j].start
		}
		return edits[i].end > edits[j].end
	})
	out := append([]byte(nil), u.text.data...)
	for _, edit := range edits {
		out = append(out[:edit.start], append([]byte(edit.text), out[edit.end:]...)...)
	}
	shifted := func(offset int) int {
		out := offset
		for _, edit := range u.edits {
			if edit.end <= offset {
				out += len(edit.text) - (edit.end - edit.start)
			}
		}
		return out
	}
	edited := newSourceText(out)
	for _, f := range u.flags {
		start, end := shifted(f.start), shifted(f.end)
		if f.find != "" {
			at := shifted(f.from)
			for n := 0; n <= f.nth; n++ {
				i := bytes.Index(out[at:], []byte(f.find))
				if i < 0 {
					break
				}
				start, end = at+i, at+i+len(f.find)
				at = end
			}
		}
		line, column := edited.position(start)
		endLine, endColumn := edited.position(end)
		u.notes = append(u.notes, Diagnostic{Severity: f.severity, Message: f.message, Line: line, Column: column, EndLine: endLine, EndColumn: endColumn})
	}
	return out
}

func (u *upgrader) replaceScalar(node *yaml.Node, text string) {
	start := u.text.offset(node)
	end, ok := u.text.scalarEnd(node, start)
	if !ok {
		u.note(node.Line, node.Column, fmt.Sprintf("write %q on one line to upgrade it", node.Value))
		return
	}
	u.edit(start, end, text)
}

func (u *upgrader) renameKey(key *yaml.Node, name string) {
	start := u.text.offset(key)
	end, ok := u.text.scalarEnd(key, start)
	if !ok {
		return
	}
	u.edit(start, end, name)
}

// removePair removes a pair of a mapping: its lines in a block mapping,
// the pair and a comma in a flow one.
func (u *upgrader) removePair(mapping, key, value *yaml.Node) {
	start := u.text.offset(key)
	if mapping.Style&yaml.FlowStyle == 0 && strings.TrimLeft(string(u.text.data[u.text.lineStart(start):start]), " ") == "" {
		lineStart := u.text.lineStart(start)
		u.edit(lineStart, u.text.blockEnd(lineStart, key.Column-1), "")
		return
	}
	end, ok := u.text.scalarEnd(value, u.text.offset(value))
	if !ok {
		u.note(key.Line, key.Column, fmt.Sprintf("remove %s by hand", key.Value))
		return
	}
	// The comma after it, or else the one before.
	rest := u.text.data[end:]
	trimmed := bytes.TrimLeft(rest, " \t")
	if len(trimmed) > 0 && trimmed[0] == ',' {
		after := end + (len(rest) - len(trimmed)) + 1
		for after < len(u.text.data) && (u.text.data[after] == ' ' || u.text.data[after] == '\t') {
			after++
		}
		u.edit(start, after, "")
		return
	}
	before := start
	for before > 0 && (u.text.data[before-1] == ' ' || u.text.data[before-1] == '\t') {
		before--
	}
	if before > 0 && u.text.data[before-1] == ',' {
		before--
	}
	u.edit(before, end, "")
}

// insertPair inserts the pair text after the pair of the key after.
func (u *upgrader) insertPair(mapping *yaml.Node, after, pair string) {
	key, value := pairOf(mapping, after)
	if key == nil {
		u.note(mapping.Line, mapping.Column, "add "+pair+" by hand")
		return
	}
	end, ok := u.text.scalarEnd(value, u.text.offset(value))
	if !ok {
		u.note(key.Line, key.Column, "add "+pair+" by hand")
		return
	}
	if mapping.Style&yaml.FlowStyle != 0 || !u.text.startsLine(u.text.offset(key)) {
		u.edit(end, end, ", "+pair)
		return
	}
	next := u.text.lineEnd(end)
	u.edit(next, next, strings.Repeat(" ", key.Column-1)+pair+"\n")
}

// itemRange is the text of a block list item: from its line's start to the
// next line at or left of its dash, and the prefix up to its first key
// ("  - ").
func (u *upgrader) itemRange(node *yaml.Node) (start, end int, prefix string, ok bool) {
	if u.flowList {
		return 0, 0, "", false
	}
	at := u.text.offset(node)
	start = u.text.lineStart(at)
	prefix = string(u.text.data[start:at])
	dash := strings.LastIndex(prefix, "-")
	if dash < 0 || strings.TrimSpace(prefix) != "-" {
		return 0, 0, "", false
	}
	return start, u.text.blockEnd(start, dash), prefix, true
}

// indentItem indents a list item written at the left margin under prefix:
// its first line after prefix, the rest after as many spaces.
func indentItem(items, prefix string) string {
	pad := strings.Repeat(" ", len(prefix))
	dash := prefix[:strings.LastIndex(prefix, "-")]
	var out strings.Builder
	for _, line := range strings.SplitAfter(items, "\n") {
		switch {
		case line == "":
		case strings.HasPrefix(line, "- "):
			out.WriteString(dash + line)
		case strings.TrimSpace(line) == "":
			out.WriteString(line)
		default:
			out.WriteString(pad[:len(dash)] + line)
		}
	}
	return out.String()
}

// pairOf is a mapping's key and value nodes for key, or nils.
func pairOf(mapping *yaml.Node, key string) (*yaml.Node, *yaml.Node) {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil, nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i], mapping.Content[i+1]
		}
	}
	return nil, nil
}

// --- The file's text ----------------------------------------------------------

type sourceText struct {
	data  []byte
	lines []int // each line's start
}

func newSourceText(data []byte) *sourceText {
	lines := []int{0}
	for i, b := range data {
		if b == '\n' {
			lines = append(lines, i+1)
		}
	}
	return &sourceText{data: data, lines: lines}
}

// offset is where a node starts: yaml's line, and its column in characters.
func (s *sourceText) offset(node *yaml.Node) int {
	if node.Line < 1 || node.Line > len(s.lines) {
		return len(s.data)
	}
	at := s.lines[node.Line-1]
	for column := 1; column < node.Column && at < len(s.data); column++ {
		_, size := utf8.DecodeRune(s.data[at:])
		at += size
	}
	return at
}

// position is an offset's line and column (in characters), from 1.
func (s *sourceText) position(offset int) (int, int) {
	line := sort.Search(len(s.lines), func(i int) bool { return s.lines[i] > offset })
	start := s.lines[line-1]
	return line, utf8.RuneCount(s.data[start:offset]) + 1
}

func (s *sourceText) lineStart(offset int) int {
	return bytes.LastIndexByte(s.data[:offset], '\n') + 1
}

// lineEnd is the start of the line after offset's.
func (s *sourceText) lineEnd(offset int) int {
	if i := bytes.IndexByte(s.data[offset:], '\n'); i >= 0 {
		return offset + i + 1
	}
	return len(s.data)
}

func (s *sourceText) startsLine(offset int) bool {
	return strings.TrimLeft(string(s.data[s.lineStart(offset):offset]), " ") == "" || strings.TrimSpace(string(s.data[s.lineStart(offset):offset])) == "-"
}

// blockEnd is the start of the first line after the one at start that has
// content at or left of column (0-based): where a block that starts there
// ends. Blank lines and comments deeper than column stay in the block.
func (s *sourceText) blockEnd(start, column int) int {
	at := s.lineEnd(start)
	for at < len(s.data) {
		next := s.lineEnd(at)
		line := strings.TrimRight(string(s.data[at:next]), "\r\n")
		trimmed := strings.TrimLeft(line, " ")
		indent := len(line) - len(trimmed)
		if trimmed != "" && indent <= column {
			return at
		}
		at = next
	}
	return len(s.data)
}

// scalarEnd is where a scalar written on one line ends.
func (s *sourceText) scalarEnd(node *yaml.Node, start int) (int, bool) {
	if node == nil || node.Kind != yaml.ScalarNode {
		return 0, false
	}
	data := s.data
	switch {
	case node.Style&yaml.DoubleQuotedStyle != 0:
		for i := start + 1; i < len(data) && data[i] != '\n'; i++ {
			if data[i] == '\\' {
				i++
				continue
			}
			if data[i] == '"' {
				return i + 1, true
			}
		}
	case node.Style&yaml.SingleQuotedStyle != 0:
		for i := start + 1; i < len(data) && data[i] != '\n'; i++ {
			if data[i] == '\'' {
				if i+1 < len(data) && data[i+1] == '\'' {
					i++
					continue
				}
				return i + 1, true
			}
		}
	case node.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0:
	default:
		if strings.Contains(node.Value, "\n") || !bytes.HasPrefix(data[start:], []byte(node.Value)) {
			return 0, false
		}
		return start + len(node.Value), true
	}
	return 0, false
}
