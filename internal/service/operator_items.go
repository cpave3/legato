package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/cpave3/legato/internal/engine/store"
)

// The kinds of operator item an agent can post.
const (
	OperatorItemAction   = "action"
	OperatorItemQuestion = "question"
	OperatorItemDone     = "done"
	OperatorItemOther    = "other"
)

// The kinds of a how: a shell command, a slash command for the agent tool,
// or an address to open.
const (
	OperatorHowCommand = "command"
	OperatorHowSlash   = "slash"
	OperatorHowLink    = "link"
)

// The limits of an operator item.
const (
	OperatorItemMaxTitle   = 120
	OperatorItemMaxWhy     = 200
	OperatorItemMaxHow     = 2000
	OperatorItemMaxOptions = 4
	OperatorItemMaxOption  = 60
)

// The Attention signals of operator items, one per kind.
const (
	AttentionSignalOperatorAction   = "operator_action"
	AttentionSignalOperatorQuestion = "operator_question"
	AttentionSignalOperatorDone     = "operator_done"
	AttentionSignalOperatorOther    = "operator_other"
)

// OperatorItemsAttentionProducer names the Attention producer of operator
// items.
const OperatorItemsAttentionProducer = "operator_items"

var operatorItemSignals = map[string]string{
	OperatorItemAction:   AttentionSignalOperatorAction,
	OperatorItemQuestion: AttentionSignalOperatorQuestion,
	OperatorItemDone:     AttentionSignalOperatorDone,
	OperatorItemOther:    AttentionSignalOperatorOther,
}

// OperatorHow is what the operator runs or opens for an item.
type OperatorHow struct {
	Kind string `json:"kind" jsonschema:"command (a shell command for the operator to run), slash (a slash command for the operator to type to you) or link (an http(s) address to open)"`
	Text string `json:"text" jsonschema:"the command, the slash command or the address"`
}

// OperatorItemInput is an item an agent posts.
type OperatorItemInput struct {
	// TaskID and ExecutionID are the posting session's; AttributionTaskID
	// is the task the operator sees the item on, TaskID when empty.
	TaskID            string
	AttributionTaskID string
	ExecutionID       string
	Kind              string
	Title             string
	Why               string
	How               *OperatorHow
	Options           []string
}

// PostOperatorItem checks an item and keeps it. An open item of the same
// execution with the same title is updated instead, with a new revision.
func PostOperatorItem(ctx context.Context, db *store.Store, in OperatorItemInput) (store.OperatorItem, error) {
	item, options, err := checkOperatorItem(in)
	if err != nil {
		return store.OperatorItem{}, err
	}
	item.ID = newOperatorItemID()
	return db.PostOperatorItem(ctx, item, options)
}

// ClearOperatorItems closes one open item of an execution (id), or all of
// them (id empty), as cleared by the agent, and says how many it closed.
func ClearOperatorItems(ctx context.Context, db *store.Store, executionID, id string) (int, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return db.CloseExecutionOperatorItems(ctx, executionID, store.OperatorItemCleared)
	}
	err := db.CloseOperatorItem(ctx, id, executionID, store.OperatorItemCleared)
	if errors.Is(err, store.ErrNotFound) {
		return 0, fmt.Errorf("you have no open item %s for the operator", id)
	}
	if err != nil {
		return 0, err
	}
	return 1, nil
}

func checkOperatorItem(in OperatorItemInput) (store.OperatorItem, []string, error) {
	kind := strings.TrimSpace(in.Kind)
	if _, ok := operatorItemSignals[kind]; !ok {
		return store.OperatorItem{}, nil, fmt.Errorf("kind must be action, question, done or other, not %q", in.Kind)
	}
	title := strings.TrimSpace(in.Title)
	switch {
	case title == "":
		return store.OperatorItem{}, nil, fmt.Errorf("title is required: one line saying what the operator needs to do or see")
	case strings.ContainsAny(title, "\r\n"):
		return store.OperatorItem{}, nil, fmt.Errorf("title must be one line")
	case utf8.RuneCountInString(title) > OperatorItemMaxTitle:
		return store.OperatorItem{}, nil, fmt.Errorf("title is %d characters; keep it to %d", utf8.RuneCountInString(title), OperatorItemMaxTitle)
	}
	why := strings.TrimSpace(in.Why)
	if n := utf8.RuneCountInString(why); n > OperatorItemMaxWhy {
		return store.OperatorItem{}, nil, fmt.Errorf("why is %d characters; keep it to %d", n, OperatorItemMaxWhy)
	}
	item := store.OperatorItem{
		TaskID: strings.TrimSpace(in.TaskID), AttributionTaskID: strings.TrimSpace(in.AttributionTaskID),
		ExecutionID: in.ExecutionID, Kind: kind, Title: title, Why: why,
	}
	if item.TaskID == "" {
		return store.OperatorItem{}, nil, fmt.Errorf("an operator item needs the posting task")
	}
	if item.AttributionTaskID == "" {
		item.AttributionTaskID = item.TaskID
	}
	if in.How != nil && (strings.TrimSpace(in.How.Kind) != "" || strings.TrimSpace(in.How.Text) != "") {
		how, text := strings.TrimSpace(in.How.Kind), strings.TrimSpace(in.How.Text)
		switch how {
		case OperatorHowCommand, OperatorHowSlash, OperatorHowLink:
		default:
			return store.OperatorItem{}, nil, fmt.Errorf("how.kind must be command, slash or link, not %q", in.How.Kind)
		}
		if text == "" {
			return store.OperatorItem{}, nil, fmt.Errorf("how.text is required: the %s itself", how)
		}
		if utf8.RuneCountInString(text) > OperatorItemMaxHow {
			return store.OperatorItem{}, nil, fmt.Errorf("how.text is longer than %d characters", OperatorItemMaxHow)
		}
		if how == OperatorHowLink {
			u, err := url.Parse(text)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return store.OperatorItem{}, nil, fmt.Errorf("a link must be an http(s) address, not %q", text)
			}
		}
		item.HowKind, item.HowText = how, text
	}
	var options []string
	if len(in.Options) > 0 {
		if kind != OperatorItemQuestion {
			return store.OperatorItem{}, nil, fmt.Errorf("options are for a question; a %s item has none", kind)
		}
		if len(in.Options) > OperatorItemMaxOptions {
			return store.OperatorItem{}, nil, fmt.Errorf("a question takes at most %d options, not %d", OperatorItemMaxOptions, len(in.Options))
		}
		seen := map[string]bool{}
		for _, option := range in.Options {
			option = strings.TrimSpace(option)
			switch {
			case option == "":
				return store.OperatorItem{}, nil, fmt.Errorf("an option cannot be empty")
			case strings.ContainsAny(option, "\r\n"):
				return store.OperatorItem{}, nil, fmt.Errorf("an option must be one line")
			case utf8.RuneCountInString(option) > OperatorItemMaxOption:
				return store.OperatorItem{}, nil, fmt.Errorf("option %q is longer than %d characters", option, OperatorItemMaxOption)
			case seen[option]:
				return store.OperatorItem{}, nil, fmt.Errorf("option %q is given twice", option)
			}
			seen[option] = true
			options = append(options, option)
		}
	}
	return item, options, nil
}

func newOperatorItemID() string {
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	return "op-" + hex.EncodeToString(b)
}

// OperatorItemsAttentionAdapter shows the open operator items in Attention,
// one item each, on the task that posted it.
type OperatorItemsAttentionAdapter struct {
	db       *store.Store
	messages OperatorItemMessenger
}

// OperatorItemMessenger sends an agent the operator's answer or Done: the
// agent service, through its queued message path.
type OperatorItemMessenger interface {
	QueueMessage(ctx context.Context, taskID, message string) error
}

// SetMessenger lets the adapter act on items (Done, an option, a reply).
// Without one it has no actions but Open and Copy.
func (a *OperatorItemsAttentionAdapter) SetMessenger(messages OperatorItemMessenger) {
	a.messages = messages
}

func NewOperatorItemsAttentionAdapter(db *store.Store) *OperatorItemsAttentionAdapter {
	return &OperatorItemsAttentionAdapter{db: db}
}

func (a *OperatorItemsAttentionAdapter) Producer() string { return OperatorItemsAttentionProducer }

func (a *OperatorItemsAttentionAdapter) Snapshot(ctx context.Context) ([]AttentionInput, error) {
	items, err := a.db.ListOpenOperatorItems(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]AttentionInput, 0, len(items))
	for _, item := range items {
		out = append(out, operatorAttentionInput(item, a.messages != nil))
	}
	return out, nil
}

// operatorDoneMessage is what the agent is told when the operator presses
// Done.
func operatorDoneMessage(title string) string {
	return "[legato operator] Done: " + title
}

// operatorAnswer is the operator's answer to a question item, as the agent
// gets it: their own words, untagged, after the question quoted.
func operatorAnswer(title, answer string) string {
	return "> " + title + "\n\n" + answer
}

// Execute runs Done, an option or a reply on an item.
func (a *OperatorItemsAttentionAdapter) Execute(ctx context.Context, request AttentionActionRequest) error {
	if a.messages == nil {
		return fmt.Errorf("this Legato cannot message agents")
	}
	item, err := a.db.GetOperatorItem(ctx, request.SourceKey)
	if err != nil {
		return err
	}
	if item.Status != store.OperatorItemOpen {
		return ErrAttentionConflict
	}
	session, err := a.db.GetRunningAgentSession(ctx, item.TaskID, item.ExecutionID)
	if err != nil || session.ExecutionID != item.ExecutionID {
		return fmt.Errorf("the agent that posted this has ended")
	}
	ctx = WithAgentSlot(ctx, session.Slot)
	var answer string
	switch {
	case request.ActionID == "done":
		if err := a.messages.QueueMessage(ctx, item.TaskID, operatorDoneMessage(item.Title)); err != nil {
			return err
		}
		if err := a.db.CloseOperatorItem(ctx, item.ID, item.ExecutionID, store.OperatorItemDone); err != nil {
			return err
		}
		NotifyOperatorItemsChanged(item.TaskID)
		return nil
	case request.ActionID == "reply":
		answer = strings.TrimSpace(request.Text)
	case strings.HasPrefix(request.ActionID, "option_"):
		options := item.Options()
		n, err := strconv.Atoi(strings.TrimPrefix(request.ActionID, "option_"))
		if err != nil || n < 1 || n > len(options) {
			return fmt.Errorf("unknown action %q", request.ActionID)
		}
		answer = options[n-1]
	default:
		return fmt.Errorf("unknown action %q", request.ActionID)
	}
	if answer == "" {
		return fmt.Errorf("the answer is empty")
	}
	if err := a.messages.QueueMessage(ctx, item.TaskID, operatorAnswer(item.Title, answer)); err != nil {
		return err
	}
	if err := a.db.CloseOperatorItem(ctx, item.ID, item.ExecutionID, store.OperatorItemAnswered); err != nil {
		return err
	}
	// An answer is the operator's turn: the agent's other items close too.
	if _, err := a.db.CloseExecutionOperatorItems(ctx, item.ExecutionID, store.OperatorItemOperatorTurn); err != nil {
		return err
	}
	NotifyOperatorItemsChanged(item.TaskID)
	return nil
}

func operatorAttentionInput(item store.OperatorItem, acts bool) AttentionInput {
	reason := item.Why
	if reason == "" {
		reason = operatorItemReason(item.Kind)
	}
	var detail strings.Builder
	if item.HowText != "" {
		switch item.HowKind {
		case OperatorHowLink:
			fmt.Fprintf(&detail, "Open: %s", item.HowText)
		case OperatorHowSlash:
			fmt.Fprintf(&detail, "Type to the agent: `%s`", item.HowText)
		default:
			fmt.Fprintf(&detail, "Run: `%s`", item.HowText)
		}
	}
	if options := item.Options(); len(options) > 0 {
		if detail.Len() > 0 {
			detail.WriteString("\n\n")
		}
		detail.WriteString("Options: " + strings.Join(options, " · "))
	}
	return AttentionInput{
		SignalKind: operatorItemSignals[item.Kind],
		SourceKey:  item.ID,
		// The revision rises on every post of the item, so a dismissal of
		// an older version no longer holds. updated_at has milliseconds but
		// two posts may still share one.
		SourceRevision: strconv.Itoa(item.Revision),
		TaskID:         item.AttributionTaskID,
		Title:          item.Title,
		Reason:         reason,
		Detail:         detail.String(),
		Category:       "agent",
		Urgency:        "normal",
		Navigation:     AttentionNavigation{Kind: "agent", Target: item.TaskID},
		Actions:        operatorItemActions(item, acts),
	}
}

// operatorItemActions are Open, then Copy or Open link for a how, then the
// options and a reply for a question, and Done.
func operatorItemActions(item store.OperatorItem, acts bool) []AttentionAction {
	actions := []AttentionAction{{ID: "open", Label: "Open agent", Kind: "navigate", Intent: "primary"}}
	switch item.HowKind {
	case OperatorHowCommand, OperatorHowSlash:
		actions = append(actions, AttentionAction{ID: "copy", Label: "Copy", Kind: "copy", Text: item.HowText})
	case OperatorHowLink:
		actions = append(actions, AttentionAction{ID: "open_link", Label: "Open", Kind: "navigate", Href: item.HowText})
	}
	if !acts {
		return actions
	}
	if item.Kind == OperatorItemQuestion {
		for i, option := range item.Options() {
			actions = append(actions, AttentionAction{ID: fmt.Sprintf("option_%d", i+1), Label: option, Kind: "execute"})
		}
		actions = append(actions, AttentionAction{ID: "reply", Label: "Reply", Kind: "execute", RequiresText: true})
	}
	return append(actions, AttentionAction{ID: "done", Label: "Done", Kind: "execute"})
}

func operatorItemReason(kind string) string {
	switch kind {
	case OperatorItemAction:
		return "The agent needs you to do this"
	case OperatorItemQuestion:
		return "The agent asks you this"
	case OperatorItemDone:
		return "The agent finished this for you to look at"
	default:
		return "The agent wants you to see this"
	}
}
