package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cpave3/legato/internal/engine/mcpserver"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// MCPScope is Legato Scope's pool as the page_pin tools use it: the pins
// the operator sends from live pages and the origins agents subscribe to.
// service.ScopeService implements it. See docs/ai/scope.md.
type MCPScope interface {
	Subscribe(ctx context.Context, taskID, slot, origin string) (service.ScopeSubscribeResult, error)
	Unsubscribe(ctx context.Context, taskID, slot, origin string) error
	SubscriptionsOf(ctx context.Context, taskID, slot string) ([]string, error)
	ListForSlot(ctx context.Context, taskID, slot, origin, state string) ([]service.ScopePin, error)
	Get(ctx context.Context, pinID string) (service.ScopePinDetail, error)
	Screenshot(ctx context.Context, pinID string) ([]byte, string, error)
	Resolve(ctx context.Context, pinID, outcome, note, taskID string) (service.ScopePin, error)
}

func (m *mcpTools) scopeTools() []mcpserver.Tool {
	return []mcpserver.Tool{m.pagePinSubscribe(), m.pagePinUnsubscribe(), m.pagePinList(), m.pagePinGet(), m.pagePinResolve()}
}

// scopeSlot is the task slot of the calling session, which subscriptions
// belong to.
func (m *mcpTools) scopeSlot(ctx context.Context) (taskID, slot string, err error) {
	session, err := m.session(ctx)
	if err != nil {
		return "", "", err
	}
	if session.ParentTaskID != nil && *session.ParentTaskID != "" {
		return "", "", errors.New("a swarm worker cannot subscribe to a page's pins; the conductor or the task's own agent can")
	}
	return session.TaskID, session.Slot, nil
}

// scopeError says what went wrong in the agent's terms.
func scopeError(err error, pinID string) error {
	if errors.Is(err, store.ErrNotFound) && pinID != "" {
		return fmt.Errorf("there is no pin %s; page_pin_list lists the pins on your origins", pinID)
	}
	return err
}

type pagePinOriginInput struct {
	Origin string `json:"origin" jsonschema:"the page origin: scheme, host and port, such as http://localhost:5173"`
}

func (m *mcpTools) pagePinSubscribe() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name:  "page_pin_subscribe",
		Title: "Subscribe to a page's pins",
		Description: "Subscribes your task to a web page's origin, such as http://localhost:5173, so the comments the operator pins on its pages with Legato's browser extension come to you as one [legato scope] message per batch. " +
			"Use it without being asked when you start a dev server, or know of one the operator looks at. One agent holds an origin at a time: subscribing takes it from whoever had it, who is told. " +
			"The subscription stays with your task through restarts and ends when your agent ends. Pins already waiting on the origin are listed in the result.",
	}, func(ctx context.Context, in pagePinOriginInput) (mcpserver.Result, error) {
		taskID, slot, err := m.scopeSlot(ctx)
		if err != nil {
			return mcpserver.Result{}, err
		}
		result, err := m.opts.Scope.Subscribe(ctx, taskID, slot, in.Origin)
		if err != nil {
			return mcpserver.Result{}, err
		}
		return mcpserver.Result{Text: result.Report()}, nil
	})
}

func (m *mcpTools) pagePinUnsubscribe() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name:        "page_pin_unsubscribe",
		Title:       "Unsubscribe from a page's pins",
		Description: "Ends your subscription to a page origin, when you no longer serve it. Batches sent from it then wait in Legato's pool for the next agent to subscribe.",
	}, func(ctx context.Context, in pagePinOriginInput) (mcpserver.Result, error) {
		taskID, slot, err := m.scopeSlot(ctx)
		if err != nil {
			return mcpserver.Result{}, err
		}
		if err := m.opts.Scope.Unsubscribe(ctx, taskID, slot, in.Origin); err != nil {
			return mcpserver.Result{}, err
		}
		return mcpserver.Result{Text: "Unsubscribed from " + strings.TrimRight(strings.TrimSpace(in.Origin), "/") + "."}, nil
	})
}

type pagePinListInput struct {
	Origin string `json:"origin,omitempty" jsonschema:"a page origin to list; leave it out for the origins you subscribe to"`
	State  string `json:"state,omitempty" jsonschema:"only pins in this state: sent, delivered, fixed or wont_fix"`
}

func (m *mcpTools) pagePinList() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name:        "page_pin_list",
		Title:       "List page pins",
		Description: "Lists the pins on the page origins you subscribe to, or on a named origin: each pin's ID, state, page, the operator's comment, where it is and the note it was resolved with. Use it when told pins are waiting.",
		ReadOnly:    true,
	}, func(ctx context.Context, in pagePinListInput) (mcpserver.Result, error) {
		taskID, slot, err := m.scopeSlot(ctx)
		if err != nil {
			return mcpserver.Result{}, err
		}
		pins, err := m.opts.Scope.ListForSlot(ctx, taskID, slot, in.Origin, in.State)
		if err != nil {
			return mcpserver.Result{}, err
		}
		if len(pins) == 0 {
			if in.Origin == "" {
				origins, _ := m.opts.Scope.SubscriptionsOf(ctx, taskID, slot)
				if len(origins) == 0 {
					return mcpserver.Result{Text: "You subscribe to no page origin, so there are no pins to list. Subscribe with page_pin_subscribe, or name an origin."}, nil
				}
			}
			return mcpserver.Result{Text: "No pins."}, nil
		}
		var b strings.Builder
		origin := ""
		for _, pin := range pins {
			if pin.Origin != origin {
				origin = pin.Origin
				fmt.Fprintf(&b, "%s:\n", origin)
			}
			fmt.Fprintf(&b, "- %s [%s] %s %q", pin.ID, strings.ReplaceAll(pin.State, "_", " "), pin.Path, pin.Comment)
			if pin.Selector != "" {
				fmt.Fprintf(&b, " at %s", pin.Selector)
			}
			if pin.Location != "" {
				fmt.Fprintf(&b, " (%s)", pin.Location)
			}
			if pin.Note != "" {
				fmt.Fprintf(&b, " — %s", pin.Note)
			}
			b.WriteString("\n")
		}
		b.WriteString("Read a pin with page_pin_get; mark one you handled with page_pin_resolve.")
		return mcpserver.Result{Text: b.String()}, nil
	})
}

type pagePinIDInput struct {
	PinID string `json:"pin_id" jsonschema:"the pin's ID, such as sp-0123456789"`
}

func (m *mcpTools) pagePinGet() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name:  "page_pin_get",
		Title: "Read a page pin",
		Description: "Returns one pin in full: the operator's comment, the page (address, title, viewport, scroll), the element (selectors, most stable first, its text and box), the styles that differ from the browser's defaults, " +
			"the component that rendered it when known (file:line), and a screenshot of the page around it, the element outlined.",
		ReadOnly: true,
	}, func(ctx context.Context, in pagePinIDInput) (mcpserver.Result, error) {
		if _, _, err := m.scopeSlot(ctx); err != nil {
			return mcpserver.Result{}, err
		}
		pinID := strings.TrimSpace(in.PinID)
		detail, err := m.opts.Scope.Get(ctx, pinID)
		if err != nil {
			return mcpserver.Result{}, scopeError(err, pinID)
		}
		result := mcpserver.Result{Text: detail.Text()}
		if detail.HasScreenshot {
			if data, mediaType, err := m.opts.Scope.Screenshot(ctx, pinID); err == nil {
				result.Images = []mcpserver.Image{{MIMEType: mediaType, Data: data}}
			} else {
				result.Text += "\nThe screenshot could not be read: " + err.Error() + "\n"
			}
		}
		return result, nil
	})
}

type pagePinResolveInput struct {
	PinID   string `json:"pin_id" jsonschema:"the pin's ID"`
	Outcome string `json:"outcome" jsonschema:"fixed or wont_fix"`
	Note    string `json:"note" jsonschema:"one line for the operator, shown on the page: what you changed, or why you will not"`
}

func (m *mcpTools) pagePinResolve() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name:  "page_pin_resolve",
		Title: "Resolve a page pin",
		Description: "Marks a pin fixed or won't fix, with a one-line note the operator reads on the page beside it. Resolve each pin you handled, without being asked. " +
			"Pins have no threads: ask the operator a question in the conversation instead. The operator can reopen a pin with a new comment, which reaches you as a new [legato scope] message.",
	}, func(ctx context.Context, in pagePinResolveInput) (mcpserver.Result, error) {
		taskID, _, err := m.scopeSlot(ctx)
		if err != nil {
			return mcpserver.Result{}, err
		}
		pinID := strings.TrimSpace(in.PinID)
		pin, err := m.opts.Scope.Resolve(ctx, pinID, strings.TrimSpace(in.Outcome), in.Note, taskID)
		if err != nil {
			return mcpserver.Result{}, scopeError(err, pinID)
		}
		return mcpserver.Result{Text: fmt.Sprintf("Pin %s is marked %s; the operator sees it on the page.", pin.ID, strings.ReplaceAll(pin.State, "_", " "))}, nil
	})
}
