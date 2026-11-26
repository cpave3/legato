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
	mappings := map[string]map[string]outputMapping{}
	var replaced [][2]int
	if err := u.rewriteSteps(definition.Steps, list, rewrites, mappings, &replaced); err != nil {
		return nil, fmt.Errorf("upgrade %s: %v", source, err)
	}
	u.mapReferences(mappings, replaced)
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
func (u *upgrader) rewriteSteps(steps []Step, list *yaml.Node, rewrites map[string]StepRewrite, mappings map[string]map[string]outputMapping, replaced *[][2]int) error {
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
			if err := u.rewriteSteps(step.Steps, body, rewrites, mappings, replaced); err != nil {
				return err
			}
			continue
		}
		rewrite, ok := rewrites[step.Type]
		if !ok {
			continue
		}
		rewritten := &UpgradeStep{ID: step.ID, Type: step.Type, With: step.With, Line: node.Line}
		rewrite(rewritten)
		if len(rewritten.outputs) > 0 {
			mappings[step.ID] = rewritten.outputs
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

// mapReferences rewrites each reference to a mapped output and flags each
// one to an unmapped output, outside the steps that were replaced.
func (u *upgrader) mapReferences(mappings map[string]map[string]outputMapping, replaced [][2]int) {
	ids := sortedKeys(mappings)
	for _, id := range ids {
		for _, name := range sortedKeys(mappings[id]) {
			mapping := mappings[id][name]
			reference := "steps." + id + ".outputs." + name
			text := u.text.data
			for from := 0; ; {
				at := bytes.Index(text[from:], []byte(reference))
				if at < 0 {
					break
				}
				start := from + at
				end := start + len(reference)
				from = end
				if (start > 0 && isPathByte(text[start-1])) || (end < len(text) && isPathByte(text[end])) {
					continue
				}
				if inside(start, replaced) {
					continue
				}
				if mapping.reason == "" {
					u.edit(start, end, mapping.to)
					continue
				}
				u.flags = append(u.flags, flag{start: start, end: end, message: fmt.Sprintf("%s: %s; change this reference by hand", reference, mapping.reason), severity: SeverityError})
			}
		}
	}
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
}

type upgrader struct {
	text     *sourceText
	edits    []textEdit
	notes    []Diagnostic
	flags    []flag
	flowList bool
	steps    []*yaml.Node
	waits    map[int]bool
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
		line, column := edited.position(shifted(f.start))
		endLine, endColumn := edited.position(shifted(f.end))
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
