package workflow

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// SchemaID names the JSON Schema of a schema_version 1 file, which stays
// published; SchemaID2 names version 2's (LEG-484).
const (
	SchemaID  = "https://cpave3.github.io/legato/schemas/workflow-v1.schema.json"
	SchemaID2 = "https://cpave3.github.io/legato/schemas/workflow-v2.schema.json"
)

// SchemaIDFor is the $id of a schema version's JSON Schema.
func SchemaIDFor(version int) string {
	if version == SchemaVersion2 {
		return SchemaID2
	}
	return SchemaID
}

// expressionPatternJSON matches a value that holds ${{ }}, as a JSON
// Schema pattern.
const expressionPatternJSON = `\$\{\{[^}]*\}\}`

// JSONSchema is the JSON Schema of a schema_version 1 file (JSONSchemaFor).
func JSONSchema() []byte { return JSONSchemaFor(SchemaVersion) }

// JSONSchemaFor is the JSON Schema (draft-07) of a workflow file of a
// schema version, 1 or 2, made from the definition's Go types (their yaml
// tags, a field tagged legato:"v1" or legato:"v2" in its version's only)
// and the executor catalog, with x-legato extensions for the editor and
// the builder. Step types and their with come from the catalog in both, so
// a step type added there needs no edit here. The same Go always gives the
// same bytes.
func JSONSchemaFor(version int) []byte {
	if version != SchemaVersion2 {
		version = SchemaVersion
	}
	data, err := json.MarshalIndent(buildSchema(version), "", "  ")
	if err != nil {
		panic("workflow schema: " + err.Error())
	}
	return append(data, '\n')
}

type schemaObject = map[string]any

func buildSchema(version int) schemaObject {
	definitions := schemaObject{}
	reflector := schemaReflector{definitions: definitions, version: version}
	root := reflector.object(reflect.TypeOf(Definition{}))
	root["$schema"] = "http://json-schema.org/draft-07/schema#"
	root["$id"] = SchemaIDFor(version)
	root["title"] = "Legato workflow"
	root["description"] = fmt.Sprintf("A Legato workflow definition, schema version %d.", version)
	root["properties"].(schemaObject)["schema_version"].(schemaObject)["const"] = version

	var types []string
	var cases []any
	var executors []any
	for _, spec := range catalog {
		types = append(types, spec.Type)
		name := "with." + spec.Type
		definitions[name] = withSchema(spec)
		then := schemaObject{"properties": schemaObject{"with": schemaObject{"$ref": "#/definitions/" + name}}}
		if needsWith(spec) {
			// The file does not load without with when its type requires a
			// field or checks a group.
			then["required"] = []string{"with"}
		}
		cases = append(cases, schemaObject{
			"if":   schemaObject{"properties": schemaObject{"type": schemaObject{"const": spec.Type}}, "required": []string{"type"}},
			"then": then,
		})
		executor := schemaObject{"type": spec.Type, "title": spec.Title, "description": spec.Description}
		if len(spec.Summary) > 0 {
			executor["summary"] = spec.Summary
		}
		if spec.Group != "" {
			executor["group"] = spec.Group
		}
		if len(spec.Outputs) > 0 {
			// The outputs a later step may read without the step declaring
			// them, for the editor's expression checks.
			var outputs []any
			for _, output := range spec.Outputs {
				outputs = append(outputs, schemaObject{"name": output.Name, "type": output.Type})
			}
			executor["outputs"] = outputs
		}
		executors = append(executors, executor)
	}
	step := definitions["step"].(schemaObject)
	properties := step["properties"].(schemaObject)
	properties["type"].(schemaObject)["enum"] = types
	step["allOf"] = cases
	if version == SchemaVersion2 {
		// A step is an executor step (type and with) or a loop (loop and
		// steps), never both.
		step["allOf"] = append(append([]any(nil), cases...),
			schemaObject{"anyOf": []any{schemaObject{"required": []string{"type"}}, schemaObject{"required": []string{"loop"}}}},
			schemaObject{"not": schemaObject{"required": []string{"loop", "type"}}},
			schemaObject{"not": schemaObject{"required": []string{"loop", "with"}}},
			schemaObject{"dependencies": schemaObject{"loop": []string{"steps"}, "steps": []string{"loop"}}},
		)
		query := definitions["loop_query"].(schemaObject)
		query["properties"].(schemaObject)["type"].(schemaObject)["enum"] = types
		query["allOf"] = cases
		loop := definitions["loop"].(schemaObject)
		loop["allOf"] = []any{schemaObject{"oneOf": []any{
			schemaObject{"required": []string{"over"}}, schemaObject{"required": []string{"next"}}, schemaObject{"required": []string{"repeat"}},
		}}}
		loop["dependencies"] = schemaObject{"next": []string{"key"}}
	}

	contexts := schemaObject{}
	for _, context := range ExpressionContexts() {
		contexts[context.Location] = context
	}
	root["definitions"] = definitions
	root["x-legato"] = schemaObject{
		"schema_version": version,
		"step_groups":    StepGroups,
		"executors":      executors,
		"expressions":    contexts,
		"task_fields":    TaskFields,
		// failure_outputs are what a step with on_failure: continue may be
		// read for after it fails.
		"failure_outputs":   FailureOutputs,
		"repository_fields": RepositoryFields,
		// condition_fields are what a condition's field may be, besides
		// task.metadata.<key>.
		"condition_fields": ConditionFields,
	}
	return root
}

// needsWith reports whether a step of the type must have with to load.
func needsWith(spec ExecutorSpec) bool {
	for _, field := range spec.With {
		if field.Required {
			return true
		}
	}
	for _, constraint := range spec.Constraints {
		if constraint.Checked && constraint.Kind != Excludes && constraint.Kind != Requires {
			return true
		}
	}
	return false
}

// withSchema is a step type's with: its fields, its checked constraints,
// and no other keys (an unknown one is a warning in the engine, LEG-461).
func withSchema(spec ExecutorSpec) schemaObject {
	properties := schemaObject{}
	var required []string
	for _, field := range spec.With {
		properties[field.Name] = fieldSchema(field)
		if field.Required {
			required = append(required, field.Name)
		}
	}
	out := schemaObject{
		"type": "object", "title": spec.Title, "description": spec.Description,
		"properties": properties, "additionalProperties": false,
	}
	if len(required) > 0 {
		out["required"] = required
	}
	var all []any
	var unchecked []any
	for _, constraint := range spec.Constraints {
		if !constraint.Checked {
			unchecked = append(unchecked, schemaObject{"kind": constraint.Kind, "fields": constraint.Fields})
			continue
		}
		var each []any
		for _, name := range constraint.Fields {
			each = append(each, schemaObject{"required": []string{name}})
		}
		switch constraint.Kind {
		case ExactlyOne:
			all = append(all, schemaObject{"oneOf": each})
		case AtLeastOne:
			all = append(all, schemaObject{"anyOf": each})
		case Excludes:
			all = append(all, schemaObject{"not": schemaObject{"required": constraint.Fields}})
		case Requires:
			all = append(all, schemaObject{"dependencies": schemaObject{constraint.Fields[0]: constraint.Fields[1:]}})
		}
	}
	if len(all) > 0 {
		out["allOf"] = all
	}
	outputs := []any{}
	for _, output := range spec.Outputs {
		outputs = append(outputs, schemaObject{"name": output.Name, "type": output.Type, "description": output.Description})
	}
	extension := schemaObject{"type": spec.Type, "outputs": outputs, "declared_outputs": spec.DeclaredOutputs}
	if len(unchecked) > 0 {
		extension["run_constraints"] = unchecked
	}
	if len(spec.Summary) > 0 {
		extension["summary"] = spec.Summary
	}
	if spec.Group != "" {
		extension["group"] = spec.Group
	}
	out["x-legato"] = extension
	return out
}

func fieldSchema(field Field) schemaObject {
	var value schemaObject
	switch field.Kind {
	case KindInteger:
		value = schemaObject{"type": "integer"}
	case KindBoolean:
		value = schemaObject{"type": "boolean"}
	case KindJSON:
		value = schemaObject{}
	case KindTask:
		value = schemaObject{"type": "string"}
		if field.Checked {
			// Only where the engine checks it at load is a task a whole
			// reference; elsewhere an earlier step's task output is one too.
			value["pattern"] = `^\$\{\{\s*workflow\.(task|inputs\.[^.}\s]+)\s*\}\}$`
		}
	case KindMacro:
		value = schemaObject{"type": "string", "minLength": 1}
	case KindChatButtons:
		text := func(max int) schemaObject {
			return schemaObject{"type": "string", "minLength": 1, "anyOf": []any{
				schemaObject{"maxLength": max}, schemaObject{"pattern": expressionPatternJSON}}}
		}
		value = schemaObject{"type": "array", "minItems": 1, "maxItems": MaxChatButtonEntries, "items": schemaObject{
			"type": "object", "additionalProperties": false, "required": []string{"label", "value"},
			"properties": schemaObject{
				"label": text(MaxChatButtonLabel),
				"value": text(MaxChatButtonValue),
				"style": schemaObject{"anyOf": []any{
					schemaObject{"enum": []string{ChatButtonStylePrimary, ChatButtonStyleDanger}},
					schemaObject{"type": "string", "pattern": expressionPatternJSON}}},
				"if": schemaObject{"$ref": "#/definitions/condition"},
			},
		}}
	default:
		value = schemaObject{"type": "string"}
	}
	if len(field.Enum) > 0 {
		value["enum"] = field.Enum
	}
	if field.Pattern != "" {
		value["pattern"] = field.Pattern
	}
	if len(field.Reserved) > 0 {
		value["not"] = schemaObject{"enum": field.Reserved}
	}
	takesExpressions := !field.Literal && field.Kind != KindJSON && field.Kind != KindTask && !(value["type"] == "string" && value["enum"] == nil)
	if takesExpressions {
		value = schemaObject{"anyOf": []any{value, schemaObject{"type": "string", "pattern": expressionPatternJSON}}}
	}
	value["description"] = field.Description
	if field.Default != nil {
		value["default"] = field.Default
	}
	extension := schemaObject{"kind": field.Kind, "expressions": !field.Literal}
	if field.RequiredAtRun {
		extension["required_at_run"] = true
	}
	if field.Essential {
		extension["essential"] = true
	}
	if field.Label != "" {
		extension["label"] = field.Label
	}
	if field.Order > 0 {
		extension["order"] = field.Order
	}
	if !field.Literal {
		extension["context"] = "with"
	}
	value["x-legato"] = extension
	return value
}

// schemaReflector makes object schemas from the definition's structs of a
// schema version, named in definitions by schemaNames.
type schemaReflector struct {
	definitions schemaObject
	version     int
}

var schemaNames = map[reflect.Type]string{
	reflect.TypeOf(Input{}):                    "input",
	reflect.TypeOf(Step{}):                     "step",
	reflect.TypeOf(Output{}):                   "output",
	reflect.TypeOf(Triggers{}):                 "triggers",
	reflect.TypeOf(TaskEnteredColumnTrigger{}): "trigger." + TriggerTaskEnteredColumn,
	reflect.TypeOf(ScheduleTrigger{}):          "trigger." + TriggerSchedule,
	reflect.TypeOf(PRTrigger{}):                "trigger." + TriggerPRChangesRequested,
	reflect.TypeOf(ChatMessageTrigger{}):       "trigger." + TriggerChatMessage,
	reflect.TypeOf(ChatMentionTrigger{}):       "trigger." + TriggerChatMention,
	reflect.TypeOf(ChatButtonTrigger{}):        "trigger." + TriggerChatButton,
	reflect.TypeOf(ChatTextMatch{}):            "chat_text_match",
	reflect.TypeOf(Condition{}):                "condition",
	reflect.TypeOf(Loop{}):                     "loop",
	reflect.TypeOf(LoopQuery{}):                "loop_query",
	reflect.TypeOf(WorkflowOutput{}):           "workflow_output",
}

// fieldSchemas says what reflection cannot: each field's description, and
// its enum, default, required and the expressions it takes. Every field
// of the definition's structs is here; a test fails when one is missing.
var fieldSchemas = map[string]schemaObject{
	"Definition.schema_version": {"description": "The workflow schema version", "const": SchemaVersion, "required": true},
	"Definition.name":           {"description": "The workflow's name: unique, and how runs, switches and the CLI know it", "required": true, "pattern": `\S`},
	"Definition.title":          {"description": "The workflow's title", "required": true, "pattern": `\S`},
	"Definition.run_title":      {"description": "Each run's title; may read the inputs", "context": "run_title"},
	"Definition.description":    {"description": "What the workflow does"},
	"Definition.after_start":    {"description": "What the web UI opens once a run starts: the run (workflow), its agent (agent, with one required task input), or nothing (stay)", "enum": []string{"workflow", "agent", "stay"}},
	"Definition.inputs":         {"description": "The inputs a run takes, by name"},
	"Definition.on":             {"description": "What starts runs by itself, besides starting one by hand: one trigger of each kind"},
	"Definition.chat_help":      {"description": "List this workflow in ${{ event.commands }} of the chat triggers on the bots its own chat triggers run on"},
	"Definition.steps":          {"description": "The steps, run in order", "required": true, "minItems": 1},
	"Definition.outputs":        {"description": "The run's own outputs, by name: computed when it succeeds, and returned by a workflow.run step that calls it"},

	"WorkflowOutput.type":        {"description": "What the output holds", "required": true, "enum": WorkflowOutputTypes},
	"WorkflowOutput.value":       {"description": "The output's value: an expression over the inputs and the steps' outputs", "required": true},
	"WorkflowOutput.description": {"description": "What the output is"},

	"Input.type":        {"description": "What the input holds", "required": true, "enum": []string{"string", "integer", "boolean", "json", "task", "repository"}},
	"Input.required":    {"description": "A run cannot start without it"},
	"Input.default":     {"description": "The value when none is given"},
	"Input.description": {"description": "What the input is, shown beside it"},
	"Input.choices":     {"description": "The values the input may take"},
	"Input.secret":      {"description": "Hide the value where runs are shown"},

	"Step.id":      {"description": "The step's ID: unique, and how later steps read its outputs", "required": true, "pattern": `\S`},
	"Step.title":   {"description": "The step's title"},
	"Step.type":    {"description": "The executor that runs the step", "required": true},
	"Step.trigger": {"description": "How the step starts: by itself (automatic), when the operator starts it (manual), or when the workflow task's agent ends (agent_ended)", "enum": []string{"", "automatic", "manual", TriggerAgentEnded}, "default": "automatic"},
	"Step.with":    {"description": "The executor's settings"},
	"Step.outputs": {"description": "The outputs the step gives later steps, by name"},

	"Step.start":      {"description": "How the step starts: by itself (automatic), or when the operator starts it (manual)", "enum": []string{StartAutomatic, StartManual}, "default": StartAutomatic},
	"Step.if":         {"description": "Run the step only when this matches; otherwise it is skipped and its outputs are missing"},
	"Step.on_failure": {"description": "What a failure does: fail the run (fail), make the failure the step's outputs and go on (continue), or hold the run with Retry, Skip and Stop (pause)", "enum": []string{OnFailureFail, OnFailureContinue, OnFailurePause}},
	"Step.wait_for":   {"description": "Agent steps: the signals that end the step, the first to come: complete, finished, ended or none", "minItems": 1, "uniqueItems": true, "items": schemaObject{"type": "string", "enum": []string{SignalComplete, SignalFinished, SignalEnded, SignalNone}}},
	"Step.timeout":    {"description": "How long the step may take once it starts, such as 30m or 12h", "pattern": `^([0-9]+(\.[0-9]+)?(ns|us|µs|ms|s|m|h))+$`},
	"Step.loop":       {"description": "Make the step a loop: it runs its steps pass after pass, over a list, a query's next item, or until a condition"},
	"Step.steps":      {"description": "A loop's steps, run in order in each pass", "minItems": 1},

	"Loop.over":       {"description": "A list, read once when the loop starts: a ${{ }} of a json value, or a list written out"},
	"Loop.next":       {"description": "A query step run before each pass, whose items output the pass takes its item from: the first whose key no pass has used"},
	"Loop.repeat":     {"description": "Run the steps with no item, until until or max"},
	"Loop.key":        {"description": "What tells items apart, such as ${{ loop.item.key }}: required with next"},
	"Loop.max":        {"description": "The most passes: 20 when 0 or absent, at most 100", "minimum": 0, "maximum": MaxLoopMax},
	"Loop.until":      {"description": "Checked after each pass, with loop.last the pass that just ended: when it matches the loop ends"},
	"Loop.on_failure": {"description": "What a pass whose step fails does: hold the run there (pause, the default), end the pass failed and go on (continue), or fail the loop (fail)", "enum": []string{OnFailurePause, OnFailureContinue, OnFailureFail}},

	"LoopQuery.type": {"description": "The executor of the query, such as tickets.list", "required": true},
	"LoopQuery.with": {"description": "The query's settings"},

	"Output.type":        {"description": "What the output holds", "required": true, "enum": []string{"string", "integer", "boolean", "json", "file"}},
	"Output.description": {"description": "What the output is"},
	"Output.path":        {"description": "For a file output: where the file goes, relative to the run's folder", "context": "output_path"},
	"Output.media_type":  {"description": "For a file output: its media type"},
	"Output.value":       {"description": "On a loop's output: what each pass's result holds under this name, an expression over the pass's steps"},

	"Triggers.task.entered_column":  {"description": "Start a run when a card enters a column"},
	"Triggers.schedule":             {"description": "Start runs at due times"},
	"Triggers.pr.changes_requested": {"description": "Start a run when a review requests changes on a pull request a task owns"},
	"Triggers.chat.message":         {"description": "Start a run when a chat bot set up in Settings › Integrations receives a message"},
	"Triggers.chat.mention":         {"description": "Start a run when a message mentions a chat bot set up in Settings › Integrations, or is sent to it directly"},
	"Triggers.chat.button":          {"description": "Start a run when someone clicks a button a chat bot posted with chat.message.send's buttons"},

	"ChatButtonTrigger.bot":              {"description": "The bot, by name: slack or discord", "required": true, "pattern": ChatBotNamePattern},
	"ChatButtonTrigger.channel":          {"description": "Only clicks in this channel, by the platform's ID"},
	"ChatButtonTrigger.user":             {"description": "Only clicks by this user, by the platform's ID; anyone in the channel when empty. A click by someone else starts nothing"},
	"ChatButtonTrigger.value":            {"description": "Only clicks on a button whose value matches; a regex's named groups are ${{ event.match.<name> }}"},
	"ChatButtonTrigger.enabled":          {"description": "false makes the trigger start paused, until a Legato switches it on"},
	"ChatButtonTrigger.confirm":          {"description": "Ask the operator before each run"},
	"ChatButtonTrigger.max_runs_per_day": {"description": "Most runs for one message's buttons in a day before the loop stop asks; 0 for 3", "minimum": 0},
	"ChatButtonTrigger.inputs":           {"description": "The run's inputs, by name: literals, or ${{ event.* }}", "context": "trigger." + TriggerChatButton},

	"TaskEnteredColumnTrigger.column":           {"description": "The column, by name", "required": true, "pattern": `\S`},
	"TaskEnteredColumnTrigger.enabled":          {"description": "false makes the trigger start paused, until a Legato switches it on"},
	"TaskEnteredColumnTrigger.confirm":          {"description": "Ask the operator before each run"},
	"TaskEnteredColumnTrigger.max_runs_per_day": {"description": "Most runs for one task in a day before the loop stop asks; 0 for 3", "minimum": 0},
	"TaskEnteredColumnTrigger.handed_off":       {"description": "Also run on cards a main handed off to this Legato"},
	"TaskEnteredColumnTrigger.inputs":           {"description": "The run's inputs, by name: literals, or ${{ }} of the task and event", "context": "trigger." + TriggerTaskEnteredColumn},
	"TaskEnteredColumnTrigger.conditions":       {"description": "Run only when the task matches"},

	"ScheduleTrigger.every":            {"description": "daily, weekdays or weekly, with at (and on for weekly); or give cron", "enum": []string{"daily", "weekdays", "weekly"}},
	"ScheduleTrigger.at":               {"description": "The time of day, HH:MM", "pattern": `^\s*([01]?[0-9]|2[0-3]):[0-5][0-9]\s*$`},
	"ScheduleTrigger.on":               {"description": "For weekly: the weekday, in any case", "examples": []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday", "mon", "tue", "wed", "thu", "fri", "sat", "sun"}},
	"ScheduleTrigger.cron":             {"description": "A five-field cron expression, in place of every"},
	"ScheduleTrigger.timezone":         {"description": "An IANA time zone; the host's when empty"},
	"ScheduleTrigger.catch_up":         {"description": "What a start does about due times missed while no Legato ran: once (one late run) or skip", "enum": []string{CatchUpOnce, CatchUpSkip}},
	"ScheduleTrigger.enabled":          {"description": "false makes the trigger start paused, until a Legato switches it on"},
	"ScheduleTrigger.confirm":          {"description": "Ask the operator before each run"},
	"ScheduleTrigger.max_runs_per_day": {"description": "Most runs in a day before the loop stop asks; 0 for 3", "minimum": 0},
	"ScheduleTrigger.inputs":           {"description": "The run's inputs, by name: literals, or ${{ schedule.* }}", "context": "trigger." + TriggerSchedule},

	"PRTrigger.enabled":          {"description": "false makes the trigger start paused, until a Legato switches it on"},
	"PRTrigger.confirm":          {"description": "Ask the operator before each run"},
	"PRTrigger.max_runs_per_day": {"description": "Most runs for one task in a day before the loop stop asks; 0 for 3", "minimum": 0},
	"PRTrigger.inputs":           {"description": "The run's inputs, by name: literals, or ${{ event.* }}", "context": "trigger." + TriggerPRChangesRequested},

	"ChatTextMatch.contains":     {"description": "The text contains this, ignoring case"},
	"ChatTextMatch.regex":        {"description": "The text matches this Go regular expression (RE2); (?i) ignores case", "maxLength": MaxChatRegexLength},
	"ChatTextMatch.not_contains": {"description": "The text does not contain this, ignoring case"},
	"ChatTextMatch.not_regex":    {"description": "The text does not match this Go regular expression (RE2); (?i) ignores case", "maxLength": MaxChatRegexLength},

	"Condition.all":        {"description": "Every one of these matches"},
	"Condition.any":        {"description": "At least one of these matches"},
	"Condition.not":        {"description": "This does not match"},
	"Condition.field":      {"description": "The field to compare, such as task.priority, task.metadata.<key> or event.from_column"},
	"Condition.exists":     {"description": "Whether the field has a value"},
	"Condition.equals":     {"description": "The field equals this; numbers compare by value"},
	"Condition.not_equals": {"description": "The field does not equal this"},
	"Condition.in":         {"description": "The field is one of these"},
	"Condition.gt":         {"description": "The field is greater than this: numbers by value, text as text; false when the field is missing"},
	"Condition.lt":         {"description": "The field is less than this: numbers by value, text as text; false when the field is missing"},
	"Condition.empty":      {"description": "Whether the field is empty: missing, null, \"\", [] or {}"},
}

// fieldSchemasV2 are the entries version 2 says otherwise.
var fieldSchemasV2 = map[string]schemaObject{
	// A loop step has no type.
	"Step.type":    {"description": "The executor that runs the step; a loop has none"},
	"Step.outputs": {"description": "The outputs the step gives later steps, by name; on a loop, each pass's result, mapped from its steps with value"},
	"Step.id":      {"description": "The step's ID: unique in its steps list, and how later steps read its outputs; a path names a loop's (each/build)", "required": true, "pattern": `^[^/]*\S[^/]*$`},
}

func (r schemaReflector) object(t reflect.Type) schemaObject {
	properties := schemaObject{}
	var required []string
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		name, _, _ := strings.Cut(field.Tag.Get("yaml"), ",")
		if name == "" || name == "-" || !field.IsExported() {
			continue
		}
		if only := field.Tag.Get("legato"); (only == "v2" && r.version != SchemaVersion2) || (only == "v1" && r.version != SchemaVersion) {
			// A key of the other schema version only.
			continue
		}
		key := t.Name() + "." + name
		extra, ok := fieldSchemas[key]
		if override, found := fieldSchemasV2[key]; found && r.version == SchemaVersion2 {
			extra = override
		}
		if !ok {
			panic(fmt.Sprintf("workflow schema: %s has no entry in fieldSchemas", key))
		}
		value := r.value(field.Type)
		for k, v := range extra {
			switch k {
			case "required":
				required = append(required, name)
			case "context":
				value["x-legato"] = schemaObject{"context": v}
			default:
				value[k] = v
			}
		}
		if _, isRef := value["$ref"]; isRef {
			// draft-07 ignores a $ref's siblings.
			value = schemaObject{"allOf": []any{schemaObject{"$ref": value["$ref"]}}, "description": value["description"]}
		}
		properties[name] = value
	}
	sort.Strings(required)
	out := schemaObject{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

func (r schemaReflector) value(t reflect.Type) schemaObject {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if name, ok := schemaNames[t]; ok {
		if _, done := r.definitions[name]; !done {
			r.definitions[name] = schemaObject{} // a recursive type finds its own name
			r.definitions[name] = r.object(t)
		}
		return schemaObject{"$ref": "#/definitions/" + name}
	}
	switch t.Kind() {
	case reflect.String:
		return schemaObject{"type": "string"}
	case reflect.Bool:
		return schemaObject{"type": "boolean"}
	case reflect.Int, reflect.Int64:
		return schemaObject{"type": "integer"}
	case reflect.Slice:
		items := r.value(t.Elem())
		if len(items) == 0 {
			return schemaObject{"type": "array"}
		}
		return schemaObject{"type": "array", "items": items}
	case reflect.Map:
		values := r.value(t.Elem())
		if len(values) == 0 {
			return schemaObject{"type": "object"}
		}
		return schemaObject{"type": "object", "additionalProperties": values}
	case reflect.Interface:
		return schemaObject{}
	case reflect.Struct:
		return r.object(t)
	}
	panic("workflow schema: no schema for " + t.String())
}

// The two chat trigger types say the same, each under its own name.
func init() {
	for typeName, kind := range map[string]string{"ChatMessageTrigger": TriggerChatMessage, "ChatMentionTrigger": TriggerChatMention} {
		for field, schema := range map[string]schemaObject{
			"bot":              {"description": "The bot, by name: slack or discord", "required": true, "pattern": ChatBotNamePattern},
			"channel":          {"description": "Only messages in this channel, by the platform's ID"},
			"user":             {"description": "Only messages from this user, by the platform's ID"},
			"text":             {"description": "Only messages whose text matches"},
			"include_bots":     {"description": "Also run on messages other bots post; the bot's own never start runs"},
			"fallback":         {"description": "Fire only when no other enabled chat trigger on the same bot, of either kind, matched the message; its own filters still apply"},
			"loop_item":        {"description": "What the loop stop counts runs by: the channel, or each thread", "enum": []string{ChatLoopChannel, ChatLoopThread}, "default": ChatLoopChannel},
			"enabled":          {"description": "false makes the trigger start paused, until a Legato switches it on"},
			"confirm":          {"description": "Ask the operator before each run"},
			"max_runs_per_day": {"description": "Most runs for one channel (or thread) in a day before the loop stop asks; 0 for 3", "minimum": 0},
			"inputs":           {"description": "The run's inputs, by name: literals, or ${{ event.* }}", "context": "trigger." + kind},
		} {
			fieldSchemas[typeName+"."+field] = schema
		}
	}
}
