package workflow

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// SchemaID names the workflow JSON Schema of this schema version.
const SchemaID = "https://cpave3.github.io/legato/schemas/workflow-v1.schema.json"

// expressionPatternJSON matches a value that holds ${{ }}, as a JSON
// Schema pattern.
const expressionPatternJSON = `\$\{\{[^}]*\}\}`

// JSONSchema is the JSON Schema (draft-07) of a workflow file of this
// schema version, made from the definition's Go types (their yaml tags)
// and the executor catalog, with x-legato extensions for the editor and
// the builder. The same Go always gives the same bytes.
func JSONSchema() []byte {
	data, err := json.MarshalIndent(buildSchema(), "", "  ")
	if err != nil {
		panic("workflow schema: " + err.Error())
	}
	return append(data, '\n')
}

type schemaObject = map[string]any

func buildSchema() schemaObject {
	definitions := schemaObject{}
	reflector := schemaReflector{definitions: definitions}
	root := reflector.object(reflect.TypeOf(Definition{}))
	root["$schema"] = "http://json-schema.org/draft-07/schema#"
	root["$id"] = SchemaID
	root["title"] = "Legato workflow"
	root["description"] = "A Legato workflow definition, schema version 1."

	var types, internal []string
	var cases []any
	var executors []any
	for _, spec := range catalog {
		types = append(types, spec.Type)
		if spec.Internal {
			internal = append(internal, spec.Type)
		}
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
		executor := schemaObject{"type": spec.Type, "title": spec.Title, "description": spec.Description, "internal": spec.Internal}
		if len(spec.Summary) > 0 {
			executor["summary"] = spec.Summary
		}
		executors = append(executors, executor)
	}
	step := definitions["step"].(schemaObject)
	properties := step["properties"].(schemaObject)
	properties["type"].(schemaObject)["enum"] = types
	step["allOf"] = cases

	contexts := schemaObject{}
	for _, context := range ExpressionContexts() {
		contexts[context.Location] = context
	}
	root["definitions"] = definitions
	root["x-legato"] = schemaObject{
		"schema_version":    SchemaVersion,
		"executors":         executors,
		"internal":          internal,
		"expressions":       contexts,
		"task_fields":       TaskFields,
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
		if constraint.Checked && constraint.Kind != Excludes {
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
		}
	}
	if len(all) > 0 {
		out["allOf"] = all
	}
	outputs := []any{}
	for _, output := range spec.Outputs {
		outputs = append(outputs, schemaObject{"name": output.Name, "type": output.Type, "description": output.Description})
	}
	extension := schemaObject{"type": spec.Type, "internal": spec.Internal, "outputs": outputs, "declared_outputs": spec.DeclaredOutputs}
	if len(unchecked) > 0 {
		extension["run_constraints"] = unchecked
	}
	if len(spec.Summary) > 0 {
		extension["summary"] = spec.Summary
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
	default:
		value = schemaObject{"type": "string"}
	}
	if len(field.Enum) > 0 {
		value["enum"] = field.Enum
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

// schemaReflector makes object schemas from the definition's structs,
// named in definitions by schemaNames.
type schemaReflector struct{ definitions schemaObject }

var schemaNames = map[reflect.Type]string{
	reflect.TypeOf(Input{}):                    "input",
	reflect.TypeOf(Step{}):                     "step",
	reflect.TypeOf(Output{}):                   "output",
	reflect.TypeOf(Triggers{}):                 "triggers",
	reflect.TypeOf(TaskEnteredColumnTrigger{}): "trigger." + TriggerTaskEnteredColumn,
	reflect.TypeOf(ScheduleTrigger{}):          "trigger." + TriggerSchedule,
	reflect.TypeOf(PRTrigger{}):                "trigger." + TriggerPRChangesRequested,
	reflect.TypeOf(Condition{}):                "condition",
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
	"Definition.steps":          {"description": "The steps, run in order", "required": true, "minItems": 1},

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

	"Output.type":        {"description": "What the output holds", "required": true, "enum": []string{"string", "integer", "boolean", "json", "file"}},
	"Output.description": {"description": "What the output is"},
	"Output.path":        {"description": "For a file output: where the file goes, relative to the run's folder", "context": "output_path"},
	"Output.media_type":  {"description": "For a file output: its media type"},

	"Triggers.task.entered_column":  {"description": "Start a run when a card enters a column"},
	"Triggers.schedule":             {"description": "Start runs at due times"},
	"Triggers.pr.changes_requested": {"description": "Start a run when a review requests changes on a pull request a task owns"},

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

	"Condition.all":        {"description": "Every one of these matches"},
	"Condition.any":        {"description": "At least one of these matches"},
	"Condition.not":        {"description": "This does not match"},
	"Condition.field":      {"description": "The field to compare, such as task.priority, task.metadata.<key> or event.from_column"},
	"Condition.exists":     {"description": "Whether the field has a value"},
	"Condition.equals":     {"description": "The field equals this"},
	"Condition.not_equals": {"description": "The field does not equal this"},
	"Condition.in":         {"description": "The field is one of these"},
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
		if field.Tag.Get("legato") == "v2" {
			// A schema_version 2 key: the published schema is version 1's
			// until version 2 has its own (LEG-484).
			continue
		}
		key := t.Name() + "." + name
		extra, ok := fieldSchemas[key]
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
