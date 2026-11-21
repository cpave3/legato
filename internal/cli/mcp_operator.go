package cli

import (
	"context"
	"fmt"

	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/mcpserver"
	"github.com/cpave3/legato/internal/service"
)

// operatorPostedLine ends what for_operator returns.
const operatorPostedLine = "It is in the operator's rail. If you can't go on without it, end your turn."

type forOperatorInput struct {
	Kind    string               `json:"kind" jsonschema:"action (something only the operator can do: run a command, test in a browser, merge, restart Legato), question (something you asked in plain words), done (finished work for the operator to look at) or other (anything else the operator must see)"`
	Title   string               `json:"title" jsonschema:"one line, at most 120 characters, saying what the operator needs to do or see, such as \"Run /to-tickets on the LEG-374 spec\""`
	Why     string               `json:"why,omitempty" jsonschema:"what it blocks or what it means, as a phrase of at most 200 characters, such as \"Blocks the 7 LEG-365 build tickets\""`
	How     *service.OperatorHow `json:"how,omitempty" jsonschema:"what the operator runs or opens to do it, shown with a Copy or Open button"`
	Options []string             `json:"options,omitempty" jsonschema:"for a question only: up to four short answers the operator can pick with one tap"`
}

type clearForOperatorInput struct {
	ID string `json:"id,omitempty" jsonschema:"the ID for_operator returned; leave it out to clear every open item of yours"`
}

func (m *mcpTools) operatorTools() []mcpserver.Tool {
	return []mcpserver.Tool{m.forOperator(), m.clearForOperator()}
}

func (m *mcpTools) forOperator() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name:  "for_operator",
		Title: "Put something in the operator's rail",
		Description: "Puts an item in the operator's needs-you rail, which is in view on every page of Legato, and shows you as needing them while it is open. " +
			"Use it only for something the operator must see or act on: a command only they can run, a slash command they must type to you, a test in a browser, a pull request to approve or merge, a question you asked in plain words, finished work they should look at. " +
			"Never use it for progress, for a summary of what you are doing, or for anything that does not matter to them. " +
			"When your agent tool has its own question tool and you are mid-turn, ask with that instead. " +
			"Say in why what it blocks or what it means. Posting again with the same title updates that item rather than adding one. " +
			"The item closes when the operator next types to you, when you end, or when they press Done; clear it yourself with clear_for_operator as soon as it no longer applies. " +
			"It returns the item's ID.",
	}, func(ctx context.Context, in forOperatorInput) (mcpserver.Result, error) {
		session, err := m.session(ctx)
		if err != nil {
			return mcpserver.Result{}, err
		}
		attribution, err := m.workTask(ctx)
		if err != nil {
			return mcpserver.Result{}, err
		}
		item, err := service.PostOperatorItem(ctx, m.opts.Store, service.OperatorItemInput{
			TaskID: session.TaskID, AttributionTaskID: attribution, ExecutionID: session.ExecutionID,
			Kind: in.Kind, Title: in.Title, Why: in.Why, How: in.How, Options: in.Options,
		})
		if err != nil {
			return mcpserver.Result{}, err
		}
		ipc.Broadcast(ipc.Message{Type: "operator_items_changed", TaskID: session.TaskID, ItemID: item.ID})
		// Finished work for the operator to look at is the agent saying it
		// finished.
		if item.Kind == "done" && !service.IsReeveTask(ctx, m.opts.Store, session.TaskID) {
			if _, err := service.ReportWorkStage(ctx, m.opts.Store, *session, "finished", item.Title); err != nil {
				return mcpserver.Result{}, err
			}
		}
		verb := "Posted"
		if item.Revision > 1 {
			verb = "Updated"
		}
		return mcpserver.Result{
			Text: fmt.Sprintf("%s %s for the operator. %s", verb, item.ID, operatorPostedLine),
			Data: map[string]any{"id": item.ID, "revision": item.Revision},
		}, nil
	})
}

func (m *mcpTools) clearForOperator() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name:  "clear_for_operator",
		Title: "Clear what you put in the operator's rail",
		Description: "Takes an item you posted with for_operator out of the operator's rail, once it no longer applies: you did it yourself, it was answered another way, or the question went away. " +
			"Give the ID for_operator returned, or no ID to clear every open item of yours.",
	}, func(ctx context.Context, in clearForOperatorInput) (mcpserver.Result, error) {
		session, err := m.session(ctx)
		if err != nil {
			return mcpserver.Result{}, err
		}
		cleared, err := service.ClearOperatorItems(ctx, m.opts.Store, session.ExecutionID, in.ID)
		if err != nil {
			return mcpserver.Result{}, err
		}
		if cleared > 0 {
			ipc.Broadcast(ipc.Message{Type: "operator_items_changed", TaskID: session.TaskID, ItemID: in.ID})
		}
		text := fmt.Sprintf("Cleared %d item(s) from the operator's rail.", cleared)
		if cleared == 0 {
			text = "You had no open items in the operator's rail."
		}
		return mcpserver.Result{Text: text, Data: map[string]any{"cleared": cleared}}, nil
	})
}
