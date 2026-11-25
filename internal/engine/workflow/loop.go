package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strconv"
	"strings"
)

// Loops (spec LEG-482 §1 and §9.8, Ship's repeat; LEG-490). A loop step
// says loop: and steps:, and no type or with. Each pass runs its steps in
// their own scope, at paths <loop path>/<pass>/<id>; a next: loop's query
// runs first in each pass, at <loop path>/<pass>/next. The loop's outputs:
// are each pass's result, mapped with value expressions over the pass's
// steps; every result also has the reserved status and error.

// Loop is a loop step's loop: exactly one of Over, Next and Repeat.
type Loop struct {
	// Over is a list, read once when the loop starts: a ${{ }} of a json
	// value, or a list written out.
	Over any `yaml:"over,omitempty" json:"over,omitempty"`
	// Next is a query step run before each pass, whose items output the
	// pass takes its item from: the first whose key no pass has used.
	Next *LoopQuery `yaml:"next,omitempty" json:"next,omitempty"`
	// Repeat runs the body with no item, until Until or Max.
	Repeat bool `yaml:"repeat,omitempty" json:"repeat,omitempty"`
	// Key is what tells items apart (${{ loop.item.key }}): required with
	// next:, optional with over:, where an item whose key was used is
	// passed over.
	Key string `yaml:"key,omitempty" json:"key,omitempty"`
	// Max is the most passes: DefaultLoopMax when 0, at most MaxLoopMax.
	Max int `yaml:"max,omitempty" json:"max,omitempty"`
	// Until is checked after each pass, with loop.last the pass that just
	// ended: true ends the loop.
	Until *Condition `yaml:"until,omitempty" json:"until,omitempty"`
	// OnFailure is what a pass whose step fails does: OnFailurePause (the
	// default) holds the run at that step, with Retry, Skip and Stop;
	// OnFailureContinue ends the pass failed and starts the next;
	// OnFailureFail ends the pass failed and fails the loop step.
	OnFailure string `yaml:"on_failure,omitempty" json:"on_failure,omitempty"`
}

// LoopQuery is a next: loop's query step: an executor step with no ID.
type LoopQuery struct {
	Type string         `yaml:"type" json:"type"`
	With map[string]any `yaml:"with,omitempty" json:"with,omitempty"`
}

// DefaultLoopMax is a loop's max when it says none; MaxLoopMax is the most
// it may say.
const (
	DefaultLoopMax = 20
	MaxLoopMax     = 100
)

// PassResultLimit is the most a pass's result may hold, encoded as JSON.
const PassResultLimit = 16 << 10

// NextStepID is the ID of a next: loop's query step in each pass.
const NextStepID = "next"

// LoopStepType is the executor type of a loop step's attempts.
const LoopStepType = "loop"

// How a loop ended: its step's ended_by output.
const (
	LoopEndedItems     = "items"
	LoopEndedNextEmpty = "next_empty"
	LoopEndedUntil     = "until"
	LoopEndedMax       = "max"
	LoopEndedFailure   = "failure"
)

// A pass's status, its result's status.
const (
	PassSucceeded = "succeeded"
	PassFailed    = "failed"
	PassSkipped   = "skipped"
)

// PassReservedOutputs are the names every pass result has, which a loop's
// outputs may not take.
var PassReservedOutputs = []string{"status", "error"}

// LoopOutputs are a loop step's own outputs, with their types.
var LoopOutputs = []OutputSpec{
	{Name: "results", Type: "json", Description: "Every pass's result, in order"},
	{Name: "last", Type: "json", Description: "The last pass's result"},
	{Name: "count", Type: "integer", Description: "How many passes ran"},
	{Name: "succeeded", Type: "integer", Description: "How many passes succeeded"},
	{Name: "failed", Type: "integer", Description: "How many passes failed"},
	{Name: "ended_by", Type: "string", Description: "What ended the loop: items, next_empty, until, max or failure"},
}

// Limit is the loop's max, its default when it says none.
func (l Loop) Limit() int {
	if l.Max <= 0 {
		return DefaultLoopMax
	}
	return l.Max
}

// Failure is what a failed pass does, pause when the loop says nothing.
func (l Loop) Failure() string {
	if l.OnFailure == "" {
		return OnFailurePause
	}
	return l.OnFailure
}

// The keys of a scope's readable outputs that hold, inside a loop, the
// loop context's types and the pass result's: no step ID has a space.
const (
	loopRefsKey = " loop"
	passRefsKey = " pass"
)

// checkLoop checks a loop step and its body against the steps before it.
func (scope stepScope) checkLoop(step *Step, at string, seen map[string]bool) []*PathError {
	problem := func(key string, err error) []*PathError {
		return []*PathError{within(joinPath(at, key), fmt.Errorf("step %q: %w", step.ID, err))}
	}
	if err := checkID(*step, at, scope.version, seen); err != nil {
		return []*PathError{err}
	}
	if scope.version == SchemaVersion {
		return problem("loop", fmt.Errorf("loop and steps are schema_version 2 keys"))
	}
	if step.Loop == nil {
		return problem("steps", fmt.Errorf("steps needs loop: only a loop has steps"))
	}
	if step.Type != "" || len(step.With) > 0 {
		return problem("type", fmt.Errorf("a loop step takes no type or with"))
	}
	if err := normalizeStart(step, scope.version, scope.fromFile); err != nil {
		return []*PathError{within(joinPath(at, err.key), err.err)}
	}
	if len(step.WaitFor) > 0 || step.Timeout != "" {
		return problem("timeout", fmt.Errorf("a loop takes no wait_for or timeout"))
	}
	if step.Start == StartManual {
		return problem("start", fmt.Errorf("a loop starts by itself: put a gate step before it"))
	}
	if err := validateControl(*step, scope.version, scope.inputs, scope.outputs); err != nil {
		return []*PathError{within(joinPath(at, err.key), err.err)}
	}
	loop := step.Loop
	forms := 0
	for _, given := range []bool{loop.Over != nil, loop.Next != nil, loop.Repeat} {
		if given {
			forms++
		}
	}
	switch {
	case forms != 1:
		return problem("loop", fmt.Errorf("a loop takes one of over, next or repeat"))
	case loop.Max < 0 || loop.Max > MaxLoopMax:
		return problem("loop.max", fmt.Errorf("loop max must be between 1 and %d", MaxLoopMax))
	case loop.Next != nil && strings.TrimSpace(loop.Key) == "":
		return problem("loop.key", fmt.Errorf("loop key is required with next"))
	case loop.Repeat && loop.Key != "":
		return problem("loop.key", fmt.Errorf("a repeat loop takes no key"))
	}
	switch loop.OnFailure {
	case "", OnFailurePause, OnFailureContinue, OnFailureFail:
	default:
		return problem("loop.on_failure", fmt.Errorf("unsupported loop on_failure %q: pause, continue or fail", loop.OnFailure))
	}
	if len(step.Steps) == 0 {
		return problem("steps", fmt.Errorf("a loop needs steps"))
	}

	passTypes := map[string]string{"status": "string", "error": "string"}
	for key, output := range step.Outputs {
		passTypes[key] = output.Type
	}
	refs := func(fields ...string) map[string]map[string]string {
		types := map[string]string{"index": "integer", "results": "json", "previous": "json", "item": "json", "last": "json"}
		context := map[string]string{}
		for _, field := range fields {
			context[field] = types[field]
		}
		out := maps.Clone(scope.outputs)
		out[loopRefsKey], out[passRefsKey] = context, passTypes
		return out
	}
	inPass := refs("index", "item", "previous", "results")
	if loop.Over != nil {
		if err := checkOver(loop.Over, scope.inputs, scope.outputs); err != nil {
			return problem("loop.over", err)
		}
	}
	if loop.Next != nil {
		query := Step{ID: NextStepID, Type: loop.Next.Type, With: loop.Next.With}
		if _, known := catalogByType[query.Type]; !known {
			return problem("loop.next.type", fmt.Errorf("loop next has unsupported type %q", query.Type))
		}
		if err := validateStepContract(query, scope.inputs); err != nil {
			return problem("loop.next", err)
		}
		beforeItem := refs("index", "previous", "results")
		for _, field := range sortedKeys(query.With) {
			if err := validateReferences(query.With[field], scope.inputs, nil, beforeItem); err != nil {
				return problem("loop.next.with."+field, fmt.Errorf("loop next field %q: %w", field, err))
			}
		}
		for i := range step.Steps {
			if step.Steps[i].ID == NextStepID {
				return problem(fmt.Sprintf("steps[%d].id", i), fmt.Errorf("a next: loop's query is its pass's step next: its steps may not use that ID"))
			}
		}
	}
	if loop.Key != "" {
		if err := validateReferences(loop.Key, scope.inputs, nil, inPass); err != nil {
			return problem("loop.key", fmt.Errorf("loop key: %w", err))
		}
	}
	if loop.Until != nil {
		afterPass := refs("index", "item", "previous", "results", "last")
		err := loop.Until.validate(func(field string) error {
			path, err := ParsePath(field)
			if err != nil {
				return err
			}
			_, err = pathType(path, scope.inputs, nil, afterPass)
			return err
		})
		if err != nil {
			return problem("loop.until", fmt.Errorf("loop until: %w", err))
		}
	}

	body := scope
	body.outputs = inPass
	problems, bodyOutputs := body.check(step.Steps, joinPath(at, "steps"))
	for _, key := range sortedKeys(step.Outputs) {
		output := step.Outputs[key]
		where := "outputs." + key
		switch {
		case key == "status" || key == "error":
			problems = append(problems, problem(where, fmt.Errorf("loop output %q is reserved: every pass result has status and error", key))...)
		case !validOutputType(output.Type):
			problems = append(problems, problem(where+".type", fmt.Errorf("loop output %q has unsupported type %q: string, integer, boolean or json", key, output.Type))...)
		case output.Value == nil:
			problems = append(problems, problem(where, fmt.Errorf("loop output %q needs a value", key))...)
		default:
			if err := validateReferences(output.Value, scope.inputs, nil, bodyOutputs); err != nil {
				problems = append(problems, problem(where+".value", fmt.Errorf("loop output %q: %w", key, err))...)
			}
		}
	}
	return problems
}

// checkOver checks a loop's over: a list written out, or a whole ${{ }}
// of a json value.
func checkOver(over any, inputs map[string]Input, outputs map[string]map[string]string) error {
	if _, ok := over.([]any); ok {
		return validateReferences(over, inputs, nil, outputs)
	}
	text, _ := over.(string)
	inner, whole := wholeExpression(text)
	if !whole {
		return fmt.Errorf("loop over must be a list, or a ${{ }} of a json value")
	}
	if err := validateReferences(text, inputs, nil, outputs); err != nil {
		return fmt.Errorf("loop over: %w", err)
	}
	expression, err := ParseExpression(inner)
	if err != nil {
		return err
	}
	for _, operand := range expression.Operands {
		valueType := ""
		if operand.IsLiteral {
			valueType = literalType(operand.Literal)
		} else if valueType, err = pathType(operand.Path, inputs, nil, outputs); err != nil {
			return err
		}
		if valueType != "json" && valueType != "" {
			return fmt.Errorf("loop over must be a list: %s is %s, not json", inner, valueType)
		}
	}
	return nil
}

// loopPathType checks a loop.* path where the readable outputs say what
// the loop context has here.
func loopPathType(path []PathPart, previous map[string]map[string]string) (string, error) {
	context, inLoop := previous[loopRefsKey]
	if !inLoop {
		return "", fmt.Errorf("%s: loop is read only inside a loop", pathString(path))
	}
	if len(path) < 2 || path[1].IsIndex {
		return "", fmt.Errorf("%s: loop needs a field: index, item, previous or results", pathString(path))
	}
	field := path[1].Name
	fieldType, ok := context[field]
	switch {
	case !ok && field == "last":
		return "", fmt.Errorf("%s: loop.last is read only in a loop's until", pathString(path))
	case !ok && field == "item":
		return "", fmt.Errorf("%s: loop.item is read only once the pass has its item", pathString(path))
	case !ok:
		return "", fmt.Errorf("%s: loop has no field %q", pathString(path), field)
	}
	after := path[2:]
	switch field {
	case "index":
		if len(after) > 0 {
			return "", fmt.Errorf("%s: loop.index is a number", pathString(path))
		}
		return fieldType, nil
	case "previous", "last":
		if len(after) == 0 {
			return "json", nil
		}
		if after[0].IsIndex {
			return "", fmt.Errorf("%s: a pass result is read by name", pathString(path))
		}
		resultType, declared := previous[passRefsKey][after[0].Name]
		switch {
		case !declared:
			return "", fmt.Errorf("%s: the loop's pass result has no %q: declare it under the loop's outputs", pathString(path), after[0].Name)
		case len(after) == 1:
			return resultType, nil
		case resultType == "json":
			return "json", nil
		}
		return "", fmt.Errorf("%s: pass output %q has no field %q", pathString(path), after[0].Name, strings.TrimPrefix(pathString(after[1:]), "."))
	}
	return fieldType, nil
}

// loopReadableOutputs are what later steps may read of a loop step.
func loopReadableOutputs(step Step) map[string]string {
	readable := make(map[string]string, len(LoopOutputs))
	for _, output := range LoopOutputs {
		readable[output.Name] = output.Type
	}
	if step.OnFailure == OnFailureContinue {
		for _, name := range FailureOutputs {
			if _, declared := readable[name]; !declared {
				readable[name] = "string"
			}
		}
	}
	return readable
}

// Scope is what a step's ${{ }} reads when it runs: the run's inputs and
// task, the outputs of the steps it may read (the nearest of each ID,
// succeeded or failed under continue; a skipped one's are missing), and
// inside a loop the loop context (LoopContext).
type Scope struct {
	Inputs map[string]any
	Task   map[string]any
	Steps  map[string]map[string]any
	Loop   map[string]any
}

func (s Scope) resolver() resolveScope {
	return resolveScope{inputs: s.Inputs, task: s.Task, outputs: s.Steps, loop: s.Loop}
}

// ScopeLevel is one scope a step instance is in, outermost first: the
// run's own steps, then each loop pass the path goes through.
type ScopeLevel struct {
	// Path is the scope's path: "" for the run's own steps,
	// <loop path>/<pass> for a pass.
	Path string
	// Steps are the scope's steps, of which the first Before come before
	// the path: their outputs may be read.
	Steps  []Step
	Before int
	// Loop and Pass are the loop and pass of a pass's scope; Loop is ""
	// for the run's own.
	Loop string
	Pass int
}

// pathLevel is a ScopeLevel and the step of the path in it: Index is the
// step's in Steps, -1 for a next: loop's query.
type pathLevel struct {
	ScopeLevel
	Index int
	Step  *Step
}

// walk follows a step instance's path through the definition. Version 1
// has no loops and allowed a slash in an ID: its paths are read whole.
func (d *Definition) walk(path string) ([]pathLevel, error) {
	segments := strings.Split(path, "/")
	if d.SchemaVersion == SchemaVersion {
		segments = []string{path}
	}
	steps, scope, loopPath, pass := d.Steps, "", "", 0
	var loopStep *Step
	var levels []pathLevel
	for i := 0; ; i += 2 {
		id := segments[i]
		level := pathLevel{ScopeLevel: ScopeLevel{Path: scope, Steps: steps, Loop: loopPath, Pass: pass}, Index: -1}
		if loopStep != nil && loopStep.Loop.Next != nil && id == NextStepID {
			if i != len(segments)-1 {
				return nil, fmt.Errorf("workflow step %q not found", path)
			}
			level.Step = &Step{ID: NextStepID, Title: "Next item", Type: loopStep.Loop.Next.Type, With: loopStep.Loop.Next.With, Start: StartAutomatic}
			return append(levels, level), nil
		}
		for j := range steps {
			if steps[j].ID == id {
				level.Index, level.Step, level.Before = j, &steps[j], j
				break
			}
		}
		if level.Step == nil {
			return nil, fmt.Errorf("workflow step %q not found", path)
		}
		levels = append(levels, level)
		if i == len(segments)-1 {
			return levels, nil
		}
		number, err := strconv.Atoi(segments[i+1])
		if level.Step.Loop == nil || i+2 >= len(segments) || err != nil || number < 0 {
			return nil, fmt.Errorf("workflow step %q not found", path)
		}
		loopStep, loopPath, pass = level.Step, StepPath(scope, id), number
		scope, steps = loopPath+"/"+segments[i+1], loopStep.Steps
	}
}

// StepAt is the step at a step instance's path: one of the run's own, a
// step of a loop's pass (each/2/build), or a next: loop's query
// (each/2/next), which takes its type and with from the loop.
func (d *Definition) StepAt(path string) (*Step, error) {
	levels, err := d.walk(path)
	if err != nil {
		return nil, err
	}
	return levels[len(levels)-1].Step, nil
}

// Nearest finds the step an ID names from the step at path ({from: id}):
// the nearest step of that ID, in path's own scope before it, then in each
// enclosing scope. It returns the step's path.
func (d *Definition) Nearest(path, id string) (string, *Step, error) {
	levels, err := d.walk(path)
	if err != nil {
		return "", nil, err
	}
	for i := len(levels) - 1; i >= 0; i-- {
		level := levels[i]
		for j := range level.Steps[:level.Before] {
			if level.Steps[j].ID == id {
				return StepPath(level.Path, id), &level.Steps[j], nil
			}
		}
	}
	return "", nil, fmt.Errorf("step %s is not before %s in the workflow", id, path)
}

// Scopes are the scopes the step at path is in, outermost first.
func (d *Definition) Scopes(path string) ([]ScopeLevel, error) {
	levels, err := d.walk(path)
	if err != nil {
		return nil, err
	}
	out := make([]ScopeLevel, len(levels))
	for i, level := range levels {
		out[i] = level.ScopeLevel
	}
	return out, nil
}

// PassScopes are the scopes a loop's pass has once it has ended: the
// loop's own, then the pass with all its steps.
func (d *Definition) PassScopes(loopPath string, pass int) ([]ScopeLevel, error) {
	levels, err := d.walk(loopPath)
	if err != nil {
		return nil, err
	}
	loop := levels[len(levels)-1].Step
	if loop.Loop == nil {
		return nil, fmt.Errorf("workflow step %q is not a loop", loopPath)
	}
	out := make([]ScopeLevel, 0, len(levels)+1)
	for _, level := range levels {
		out = append(out, level.ScopeLevel)
	}
	return append(out, ScopeLevel{Path: PassPath(loopPath, pass), Steps: loop.Steps, Before: len(loop.Steps), Loop: loopPath, Pass: pass}), nil
}

// PassPath is the path of a loop's pass's scope: <loop path>/<pass>.
func PassPath(loopPath string, pass int) string {
	return loopPath + "/" + strconv.Itoa(pass)
}

// FirstPath is the path of the first step a loop's pass runs: its body's
// first step, or a next: loop's query.
func (d *Definition) FirstPath(loopPath string, pass int, query bool) (string, error) {
	loop, err := d.StepAt(loopPath)
	if err != nil {
		return "", err
	}
	if loop.Loop == nil || len(loop.Steps) == 0 {
		return "", fmt.Errorf("workflow step %q is not a loop", loopPath)
	}
	if query {
		return StepPath(PassPath(loopPath, pass), NextStepID), nil
	}
	return StepPath(PassPath(loopPath, pass), loop.Steps[0].ID), nil
}

// NextPath is where a run goes after the step at path: the next step of
// its scope; after a pass's last step (or a next: loop's query), the loop,
// which decides what comes next; after the run's last step, "" (the end).
func (d *Definition) NextPath(path string) (string, error) {
	levels, err := d.walk(path)
	if err != nil {
		return "", err
	}
	last := levels[len(levels)-1]
	if last.Index >= 0 && last.Index+1 < len(last.Steps) {
		return StepPath(last.Path, last.Steps[last.Index+1].ID), nil
	}
	return last.Loop, nil
}

// PassSteps are the steps a loop's pass is made of, in order: the body's
// steps, or only the query of a next: loop before it has its item.
func (d *Definition) PassSteps(loopPath string, query bool) ([]Step, error) {
	loop, err := d.StepAt(loopPath)
	if err != nil {
		return nil, err
	}
	if loop.Loop == nil {
		return nil, fmt.Errorf("workflow step %q is not a loop", loopPath)
	}
	if query {
		return []Step{{ID: NextStepID, Title: "Next item", Type: loop.Loop.Next.Type, With: loop.Loop.Next.With, Start: StartAutomatic}}, nil
	}
	return loop.Steps, nil
}

// LoopState is what a loop keeps on its step row (loop_state_json): the
// over: list it read when it started, the keys its passes used, and its
// passes.
type LoopState struct {
	// Started is set when the loop first starts: an over: loop's list is
	// read then, and a Retry of the loop goes on from its passes.
	Started bool `json:"started,omitempty"`
	// Items is an over: loop's list; Cursor the next of them to take.
	Items  []any `json:"items,omitempty"`
	Cursor int   `json:"cursor,omitempty"`
	// Seen are the keys of the items passes have taken.
	Seen []string `json:"seen,omitempty"`
	// Passes are the passes so far; the last is Open while it runs.
	Passes []LoopPass `json:"passes,omitempty"`
	// EndedBy says what ended the loop, once it has.
	EndedBy string `json:"ended_by,omitempty"`
}

// LoopPass is one pass of a loop.
type LoopPass struct {
	Index int `json:"index"`
	// Item is the pass's item: missing for a repeat loop, and for a next:
	// loop's until its query has run (Body false).
	Item any    `json:"item,omitempty"`
	Key  string `json:"key,omitempty"`
	// Body is true once the pass's steps are made: at once, except for a
	// next: loop, whose query runs first.
	Body bool `json:"body,omitempty"`
	// Open is true while the pass runs; Result is set when it ends.
	Open   bool           `json:"open,omitempty"`
	Result map[string]any `json:"result,omitempty"`
}

// ParseLoopState reads a loop's state from its step row; empty is a loop
// not started.
func ParseLoopState(text string) (LoopState, error) {
	var state LoopState
	if strings.TrimSpace(text) == "" {
		return state, nil
	}
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	if err := decoder.Decode(&state); err != nil {
		return state, fmt.Errorf("loop state: %w", err)
	}
	return state, nil
}

// Encode is the state as the step row keeps it.
func (s LoopState) Encode() string {
	encoded, _ := json.Marshal(s)
	return string(encoded)
}

// Open is the pass that runs, or nil.
func (s *LoopState) Open() *LoopPass {
	if n := len(s.Passes); n > 0 && s.Passes[n-1].Open {
		return &s.Passes[n-1]
	}
	return nil
}

// HasSeen reports whether a pass has taken an item of that key.
func (s LoopState) HasSeen(key string) bool {
	for _, seen := range s.Seen {
		if seen == key {
			return true
		}
	}
	return false
}

// results are the results of the passes that have ended, in order.
func (s LoopState) results() []any {
	results := make([]any, 0, len(s.Passes))
	for _, pass := range s.Passes {
		if !pass.Open && pass.Result != nil {
			results = append(results, pass.Result)
		}
	}
	return results
}

// previous is the result of the newest pass that ended and succeeded
// before pass index, or nil.
func (s LoopState) previous(index int) map[string]any {
	for i := len(s.Passes) - 1; i >= 0; i-- {
		pass := s.Passes[i]
		if pass.Index < index && !pass.Open && pass.Result != nil && pass.Result["status"] == PassSucceeded {
			return pass.Result
		}
	}
	return nil
}

// LoopContext is what loop.* reads in pass index: loop.index, loop.item
// (missing with no item), loop.previous (the newest pass that succeeded,
// or missing) and loop.results (every pass that has ended).
func LoopContext(state LoopState, index int) map[string]any {
	context := map[string]any{"index": index, "results": state.results()}
	for _, pass := range state.Passes {
		if pass.Index == index && pass.Item != nil {
			context["item"] = pass.Item
		}
	}
	if previous := state.previous(index); previous != nil {
		context["previous"] = previous
	}
	return context
}

// UntilContext is what a loop's until reads after a pass: the pass's own
// context, and loop.last, its result.
func UntilContext(state LoopState) map[string]any {
	if len(state.Passes) == 0 {
		return map[string]any{"results": []any{}}
	}
	last := state.Passes[len(state.Passes)-1]
	context := LoopContext(state, last.Index)
	if last.Result != nil {
		context["last"] = last.Result
		if last.Result["status"] == PassSucceeded {
			context["previous"] = last.Result
		}
	}
	return context
}

// Outputs are the loop step's outputs: results, last (missing with no
// pass), count, succeeded, failed and ended_by.
func (s LoopState) Outputs() map[string]any {
	results := s.results()
	succeeded, failed := 0, 0
	for _, result := range results {
		switch result.(map[string]any)["status"] {
		case PassSucceeded:
			succeeded++
		case PassFailed:
			failed++
		}
	}
	outputs := map[string]any{"results": results, "count": len(results), "succeeded": succeeded, "failed": failed, "ended_by": s.EndedBy}
	if len(results) > 0 {
		outputs["last"] = results[len(results)-1]
	}
	return outputs
}

// loopAt is the loop step at path.
func (d *Definition) loopAt(path string) (*Step, error) {
	step, err := d.StepAt(path)
	if err != nil {
		return nil, err
	}
	if step.Loop == nil {
		return nil, fmt.Errorf("workflow step %q is not a loop", path)
	}
	return step, nil
}

// LoopItems reads an over: loop's list, when the loop starts, in the
// scope the loop step is in. A list that is missing has no items.
func (d *Definition) LoopItems(loopPath string, scope Scope) ([]any, error) {
	loop, err := d.loopAt(loopPath)
	if err != nil {
		return nil, err
	}
	value, err := scope.resolver().value(loop.Loop.Over)
	if errors.Is(err, errMissing) || (err == nil && value == nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("loop over: %w", err)
	}
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("loop over is not a list")
	}
	return items, nil
}

// LoopKey is an item's key, with the item in scope.Loop: the key's text,
// or the item's JSON when the loop says no key.
func (d *Definition) LoopKey(loopPath string, scope Scope) (string, error) {
	loop, err := d.loopAt(loopPath)
	if err != nil {
		return "", err
	}
	var value any = scope.Loop["item"]
	if loop.Loop.Key != "" {
		value, err = scope.resolver().value(loop.Loop.Key)
		if errors.Is(err, errMissing) {
			return "", fmt.Errorf("loop key %s is missing for an item", loop.Loop.Key)
		}
		if err != nil {
			return "", fmt.Errorf("loop key: %w", err)
		}
	}
	if text, ok := value.(string); ok {
		return text, nil
	}
	encoded, _ := json.Marshal(value)
	return string(encoded), nil
}

// LoopUntil checks a loop's until after a pass, with loop.last in
// scope.Loop (UntilContext); false when the loop has no until.
func (d *Definition) LoopUntil(loopPath string, scope Scope) (bool, error) {
	loop, err := d.loopAt(loopPath)
	if err != nil {
		return false, err
	}
	if loop.Loop.Until == nil {
		return false, nil
	}
	holds, err := conditionHolds(*loop.Loop.Until, scope.resolver())
	if err != nil {
		return false, fmt.Errorf("loop %q until: %w", loop.ID, err)
	}
	return holds, nil
}

// PassResult is a pass's result once it has ended: for a pass that
// succeeded, the loop's outputs resolved in the pass's scope (each that
// is missing or not of its type left out), and always status and error.
// A result over PassResultLimit is a failed pass's, with no outputs.
func (d *Definition) PassResult(loopPath string, scope Scope, status, errorText string) map[string]any {
	result := map[string]any{}
	if loop, err := d.loopAt(loopPath); err == nil && status == PassSucceeded {
		declared := make(map[string]WorkflowOutput, len(loop.Outputs))
		for key, output := range loop.Outputs {
			declared[key] = WorkflowOutput{Type: output.Type, Value: output.Value}
		}
		resolved, _ := resolveOutputMap(declared, scope.resolver())
		maps.Copy(result, resolved)
	}
	result["status"], result["error"] = status, errorText
	if encoded, _ := json.Marshal(result); len(encoded) > PassResultLimit {
		return map[string]any{"status": PassFailed, "error": fmt.Sprintf("the pass's result is %d bytes, over the %d KiB limit", len(encoded), PassResultLimit>>10)}
	}
	return result
}

// conditionHolds evaluates a condition whose fields are paths a scope
// reads; a missing value is one the condition's rules say how to take.
func conditionHolds(condition Condition, scope resolveScope) (bool, error) {
	return condition.evaluate(func(field string) (any, bool, error) {
		path, err := ParsePath(field)
		if err != nil {
			return nil, false, err
		}
		value, err := scope.path(path)
		if errors.Is(err, errMissing) {
			return nil, false, nil
		}
		return value, err == nil, err
	})
}
