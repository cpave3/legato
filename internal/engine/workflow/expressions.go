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
	}
}
