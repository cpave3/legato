package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cpave3/legato/internal/engine/burnlite"
	"github.com/cpave3/legato/internal/engine/jira"
)

// TicketUpdater changes a ticket's assignee and status, for the ticket.update
// step and ticket.create's assignee (LEG-656). Both writers are one.
type TicketUpdater interface {
	TicketWriter
	// Ticket reads a ticket's status and assignee: an error that is
	// errTicketNotFound when there is no such ticket.
	Ticket(ctx context.Context, key string) (TicketState, error)
	// ResolveAssignee turns me, an email or an account ID into a person. A
	// person no one matches is a *ticketRefusal.
	ResolveAssignee(ctx context.Context, value string) (TicketPerson, error)
	// Assign assigns the ticket to a person, or unassigns it for the zero
	// person.
	Assign(ctx context.Context, ticket TicketState, who TicketPerson) error
	// MoveTo moves a ticket to the status of that name, ignoring case, and
	// returns the status it is in after, with whether it moved. A status
	// it cannot reach is a *ticketRefusal naming those it can.
	MoveTo(ctx context.Context, ticket TicketState, status string) (string, bool, error)
}

// TicketState is a ticket as ticket.update finds it.
type TicketState struct {
	Key      string
	URL      string
	Status   string
	Assignee TicketPerson
	// Closed and CloseReason are Burnlite's.
	Closed      bool
	CloseReason string
}

// TicketPerson is whom a ticket is assigned to: Jira's account ID and
// display name, or Burnlite's claim holder as both. The zero person is no
// one.
type TicketPerson struct {
	ID   string
	Name string
}

// ticketRefusal is a change a provider cannot make to this ticket, said
// before trying: optional makes it a warning.
type ticketRefusal struct{ msg string }

func (r *ticketRefusal) Error() string { return r.msg }

func refuse(format string, args ...any) error {
	return &ticketRefusal{msg: fmt.Sprintf(format, args...)}
}

var errTicketNotFound = errors.New("not found")

// isMe and isNone are assignee's keywords, in any case.
func isMe(value string) bool   { return strings.EqualFold(value, "me") }
func isNone(value string) bool { return strings.EqualFold(value, "none") }

func (w JiraTicketWriter) Ticket(ctx context.Context, key string) (TicketState, error) {
	client := w.client()
	issue, err := client.GetIssue(ctx, key)
	if err != nil {
		var apiErr *jira.APIError
		if errors.As(err, &apiErr) && apiErr.Status == 404 {
			return TicketState{}, fmt.Errorf("%s %w in Jira (or Legato's Jira user may not see it)", key, errTicketNotFound)
		}
		return TicketState{}, err
	}
	state := TicketState{Key: key, URL: client.IssueURL(key), Status: issue.Fields.Status.Name}
	if a := issue.Fields.Assignee; a != nil {
		state.Assignee = TicketPerson{ID: a.AccountID, Name: a.DisplayName}
	}
	return state, nil
}

// ResolveAssignee: me is the user of jira.email's token; a value with an @
// is an email, which must match exactly one user (Jira hides the email of
// a user who keeps it private, who then matches nothing); anything else is
// an account ID.
func (w JiraTicketWriter) ResolveAssignee(ctx context.Context, value string) (TicketPerson, error) {
	client := w.client()
	switch {
	case isMe(value):
		me, err := client.Myself(ctx)
		if err != nil {
			return TicketPerson{}, err
		}
		return TicketPerson{ID: me.AccountID, Name: me.DisplayName}, nil
	case strings.Contains(value, "@"):
		users, err := client.FindUsers(ctx, value)
		if err != nil {
			return TicketPerson{}, err
		}
		var found []jira.UserField
		for _, u := range users {
			if strings.EqualFold(u.EmailAddress, value) {
				found = append(found, u)
			}
		}
		switch len(found) {
		case 1:
			return TicketPerson{ID: found[0].AccountID, Name: found[0].DisplayName}, nil
		case 0:
			return TicketPerson{}, refuse("no Jira user has the email %s (Jira hides the email of a user who keeps it private: give their account ID instead)", value)
		}
		names := make([]string, len(found))
		for i, u := range found {
			names[i] = fmt.Sprintf("%s (%s)", u.DisplayName, u.AccountID)
		}
		return TicketPerson{}, refuse("%d Jira users have the email %s: %s; give the account ID of one", len(found), value, strings.Join(names, ", "))
	}
	return TicketPerson{ID: value}, nil
}

func (w JiraTicketWriter) Assign(ctx context.Context, ticket TicketState, who TicketPerson) error {
	return w.client().Assign(ctx, ticket.Key, who.ID)
}

// MoveTo runs the transition whose destination is the status, in Jira's
// order; a transition that needs fields on its screen is Jira's refusal.
func (w JiraTicketWriter) MoveTo(ctx context.Context, ticket TicketState, status string) (string, bool, error) {
	if strings.EqualFold(ticket.Status, status) {
		return ticket.Status, false, nil
	}
	client := w.client()
	transitions, err := client.GetTransitions(ctx, ticket.Key)
	if err != nil {
		return ticket.Status, false, err
	}
	var reachable []string
	for _, t := range transitions {
		if strings.EqualFold(t.To.Name, status) {
			if err := client.DoTransition(ctx, ticket.Key, t.ID); err != nil {
				return ticket.Status, false, err
			}
			return t.To.Name, true, nil
		}
		if !containsFold(reachable, t.To.Name) {
			reachable = append(reachable, t.To.Name)
		}
	}
	if len(reachable) == 0 {
		return ticket.Status, false, refuse("%s cannot move to %q from %q: Jira offers no transition from it to Legato's Jira user", ticket.Key, status, ticket.Status)
	}
	return ticket.Status, false, refuse("%s cannot move to %q from %q: it can move to %s", ticket.Key, status, ticket.Status, strings.Join(reachable, ", "))
}

func (w BurnliteTicketWriter) Ticket(ctx context.Context, key string) (TicketState, error) {
	client, base, err := w.open(ctx)
	if err != nil {
		return TicketState{}, err
	}
	defer client.Close()
	task, err := client.GetTask(ctx, key)
	if err != nil {
		if errors.Is(err, burnlite.ErrNotFound) {
			return TicketState{}, fmt.Errorf("%s %w in Burnlite", key, errTicketNotFound)
		}
		return TicketState{}, err
	}
	state := TicketState{Key: key, URL: base + "/" + key, Status: task.Stage, Closed: task.State == "closed", CloseReason: task.CloseReason,
		Assignee: TicketPerson{ID: task.ClaimHolder, Name: task.ClaimHolder}}
	if state.Status == "" && state.Closed {
		state.Status = burnlite.StageDone
		if task.CloseReason == burnlite.CloseNotPlanned {
			state.Status = burnlite.StageDropped
		}
	}
	return state, nil
}

// ResolveAssignee: Burnlite has no users, only claims by holder name, so
// me claims as legato, the holder Legato writes every ticket as. Anything
// else is refused: a name would let a workflow claim as any agent.
func (w BurnliteTicketWriter) ResolveAssignee(_ context.Context, value string) (TicketPerson, error) {
	if !isMe(value) {
		return TicketPerson{}, fmt.Errorf("Burnlite assigns only by a claim: assignee must be me or none, not %q", value)
	}
	return TicketPerson{ID: burnliteHolder, Name: burnliteHolder}, nil
}

// Assign claims the Task as the person, or releases the claim of whoever
// holds it for no one.
func (w BurnliteTicketWriter) Assign(ctx context.Context, ticket TicketState, who TicketPerson) error {
	client, _, err := w.open(ctx)
	if err != nil {
		return err
	}
	defer client.Close()
	if who.ID == "" {
		if ticket.Assignee.ID == "" {
			return nil
		}
		return client.ReleaseClaim(ctx, ticket.Key, ticket.Assignee.ID)
	}
	return client.ClaimTask(ctx, ticket.Key, who.ID)
}

var burnliteStages = []string{burnlite.StageIdea, burnlite.StageExploring, burnlite.StageReady, burnlite.StageBuilding, burnlite.StageDone, burnlite.StageDropped}

// MoveTo sets an open stage, or closes the Task for done (or closed) and
// dropped. A closed Task is never reopened.
func (w BurnliteTicketWriter) MoveTo(ctx context.Context, ticket TicketState, status string) (string, bool, error) {
	stage := strings.ToLower(strings.TrimSpace(status))
	if stage == "closed" {
		stage = burnlite.StageDone
	}
	if !containsFold(burnliteStages, stage) {
		return ticket.Status, false, refuse("Burnlite has no status %q: give one of %s", status, strings.Join(burnliteStages, ", "))
	}
	if ticket.Status == stage {
		return ticket.Status, false, nil
	}
	if ticket.Closed {
		return ticket.Status, false, refuse("%s is closed (%s), and Legato does not reopen a Burnlite task to move it to %s", ticket.Key, ticket.Status, stage)
	}
	client, _, err := w.open(ctx)
	if err != nil {
		return ticket.Status, false, err
	}
	defer client.Close()
	switch stage {
	case burnlite.StageDone:
		err = client.CloseTask(ctx, ticket.Key, burnlite.CloseCompleted, "")
	case burnlite.StageDropped:
		err = client.CloseTask(ctx, ticket.Key, burnlite.CloseNotPlanned, "")
	default:
		err = client.SetStage(ctx, ticket.Key, stage)
	}
	if err != nil {
		return ticket.Status, false, err
	}
	return stage, true, nil
}
