package workflow

// TaskFields are the fields a task reference has: ${{ workflow.task.<field> }}
// and ${{ workflow.inputs.<task input>.<field> }}. metadata goes on with a
// path (metadata.<key>...).
var TaskFields = []string{"id", "title", "description", "status", "priority", "provider", "remote_id", "working_dir", "metadata"}

// RepositoryFields are the fields of a repository input:
// ${{ workflow.inputs.<repository input>.<field> }}.
var RepositoryFields = []string{"name", "repo", "primary_dir", "base_branch"}

// ExpressionRoot is one form a ${{ }} expression may take somewhere. The
// angle-bracketed parts are filled from the definition: <input> (a declared
// input), <task input> and <repository input> (one of that type),
// <task field> and <repository field> (TaskFields, RepositoryFields),
// <earlier step> (a step before this one), <output> and <file output>
// (outputs it declares), <path> (any dotted path).
type ExpressionRoot struct {
	Pattern     string `json:"pattern"`
	Description string `json:"description"`
}

// ExpressionContext is what ${{ }} may read at one place in a definition.
type ExpressionContext struct {
	Location    string           `json:"location"`
	Description string           `json:"description"`
	Roots       []ExpressionRoot `json:"roots"`
}

var (
	inputRoots = []ExpressionRoot{
		{"workflow.inputs.<input>", "A workflow input"},
		{"workflow.inputs.<task input>.<task field>", "A field of a task input"},
		{"workflow.inputs.<task input>.metadata.<path>", "A metadata value of a task input"},
		{"workflow.inputs.<repository input>.<repository field>", "A field of a repository input"},
	}
	taskRoots = []ExpressionRoot{
		{"workflow.task", "The workflow task, whole"},
		{"workflow.task.<task field>", "A field of the workflow task"},
		{"workflow.task.metadata.<path>", "A metadata value of the workflow task"},
	}
	earlierRoots = []ExpressionRoot{
		{"steps.<earlier step>.outputs.<output>", "An output of an earlier step"},
		{"steps.<earlier step>.outputs.<output>.path", "Where an earlier step's file output is"},
	}
)

// loopRoots are what a loop's pass reads of its loop (LEG-490): <loop
// output> is one of the loop's outputs, or status or error.
var loopRoots = []ExpressionRoot{
	{"loop.index", "The pass's number, from 0"},
	{"loop.item", "The pass's item (none in a repeat loop)"},
	{"loop.item.<path>", "A field of the pass's item"},
	{"loop.previous.<loop output>", "A result of the newest pass that succeeded; missing in the first"},
	{"loop.results", "The results of the passes that have ended"},
	{"loop.results.<path>", "A pass's result, as loop.results[0].<output>"},
}

// chatEventRoots are what a chat trigger's inputs may read.
var chatEventRoots = []ExpressionRoot{
	{"event.bot", "The bot that received the message"},
	{"event.platform", "slack or discord"},
	{"event.channel", "The channel's ID"},
	{"event.user", "The ID of who sent the message"},
	{"event.text", "The message's text, with any message it forwards quoted after it"},
	{"event.message_id", "The message's ID"},
	{"event.thread_id", "What chat.message.send's thread_id takes to reply to the message, and chat.thread.read's to read its thread"},
	{"event.forwarded", "true when the message forwards or shares another"},
	{"event.forwarded_url", "The forwarded message's link; empty otherwise"},
	{"event.commands", "The workflows with chat_help: true that run on this bot, one per line"},
}

// chatButtonRoots are what a chat.button trigger's inputs may read.
var chatButtonRoots = []ExpressionRoot{
	{"event.bot", "The bot that posted the buttons"},
	{"event.platform", "slack or discord"},
	{"event.channel", "The channel's ID"},
	{"event.user", "The ID of who clicked"},
	{"event.value", "The clicked button's value"},
	{"event.label", "The clicked button's label"},
	{"event.match.<path>", "A named group of the value regex: event.match.task for ^fix:(?P<task>.+)$"},
	{"event.message_id", "The ID of the message with the buttons"},
	{"event.thread_id", "What chat.message.send's thread_id takes to reply to that message"},
}

// ExpressionContexts lists, for each place that takes ${{ }}, what it may
// read. They are the rules validateReferences and the trigger runtimes
// apply, for the editor's completion.
func ExpressionContexts() []ExpressionContext {
	with := append(append(append([]ExpressionRoot{}, inputRoots...), taskRoots...), earlierRoots...)
	with = append(with, ExpressionRoot{"outputs.<file output>.path", "Where this step's own file output goes"})
	outputPath := append(append(append([]ExpressionRoot{}, inputRoots...), taskRoots...), earlierRoots...)
	return []ExpressionContext{
		{Location: "run_title", Description: "The run's title: the inputs only, as the workflow task does not exist yet", Roots: append([]ExpressionRoot{}, inputRoots...)},
		{Location: "with", Description: "A step's with values", Roots: with},
		{Location: "output_path", Description: "A file output's path", Roots: outputPath},
		{Location: "if", Description: "A step's if: condition's field, a bare path (no ${{ }}); [n] and .field reach into JSON", Roots: append([]ExpressionRoot{}, outputPath...)},
		{Location: TasksListStepType + ".where", Description: "tasks.list's where: each condition's field, a bare path (no ${{ }}) over the task being checked; its values take the with roots", Roots: []ExpressionRoot{
			{"task.<task field>", "A field of the task being checked"},
			{"task.workspace", "The name of the task's workspace, or empty"},
			{"task.metadata.<path>", "A metadata value of the task being checked"},
		}},
		{Location: "pass", Description: "A loop's pass's steps (their with and if:) and the loop's outputs' values: as with, the earlier steps the nearest of each ID (the pass's own, then those before the loop), and the loop's context. A loop's until reads loop.last too: the result of the pass that just ended", Roots: append(append([]ExpressionRoot{}, with...), loopRoots...)},
		{Location: "trigger." + TriggerTaskEnteredColumn, Description: "The inputs of a column-entry trigger", Roots: []ExpressionRoot{
			{"event.task", "The ID of the task that entered the column"},
			{"task.<task field>", "A field of the task that entered the column"},
			{"task.metadata.<path>", "A metadata value of the task that entered the column"},
		}},
		{Location: "trigger." + TriggerSchedule, Description: "The inputs of a schedule trigger", Roots: []ExpressionRoot{
			{"schedule.time", "The run's due time"},
			{"schedule.previous_time", "The due time before it"},
		}},
		{Location: "trigger." + TriggerPRChangesRequested, Description: "The inputs of a changes-requested trigger", Roots: []ExpressionRoot{
			{"event.task", "The task that owns the pull request"},
			{"event.repo", "The pull request's repository (owner/name)"},
			{"event.pr_number", "The pull request's number"},
			{"event.pr_url", "The pull request's address"},
			{"event.reviewer", "Who requested the changes"},
		}},
		{Location: "trigger." + TriggerChatMessage, Description: "The inputs of a chat message trigger", Roots: chatEventRoots},
		{Location: "trigger." + TriggerChatMention, Description: "The inputs of a chat mention trigger", Roots: chatEventRoots},
		{Location: "trigger." + TriggerChatButton, Description: "The inputs of a chat button trigger", Roots: chatButtonRoots},
	}
}
