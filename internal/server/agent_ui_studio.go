package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/cpave3/legato/internal/engine/libretto"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// AgentStudioService finds the libretto studio behind a studio item, starting
// it if it is not running.
type AgentStudioService interface {
	Address(ctx context.Context, itemID string) (string, error)
}

// SetAgentStudioService enables the studio route.
func (s *Server) SetAgentStudioService(studios AgentStudioService) {
	s.agentStudio = studios
}

// AgentStudioNotifier leaves a message for a task's agent, to be read when it
// next can: at once when it is idle, between steps when it is working.
type AgentStudioNotifier interface {
	QueueMessage(ctx context.Context, taskID, message string) error
}

// SetAgentStudioNotifier names who tells an agent what the person did in a
// studio. Without it the agents service does, when there is one.
func (s *Server) SetAgentStudioNotifier(notifier AgentStudioNotifier) {
	s.agentStudioNotifier = notifier
}

// studioNotifyHeader is how a studio panel asks for the item's agent to be
// told of what the request does: a take to review, a comment to act on. The
// person can turn that off in the panel, which then leaves the header out.
const studioNotifyHeader = "X-Legato-Studio-Notify"

// tellAgent tells the agent of a studio item's task what a request to the
// studio did, when that is the agent's to act on. answer is what the studio
// answered.
func (s *Server) tellAgent(itemID, method, rest string, answer []byte) {
	notifier := s.agentStudioNotifier
	if notifier == nil {
		// An agents service that can hold a message until the agent is
		// between steps is who tells it.
		notifier, _ = s.agents.(service.AgentMessageQueuer)
	}
	if notifier == nil || s.agentUI == nil {
		return
	}
	item, err := s.agentUI.Get(context.Background(), itemID, 0)
	if err != nil {
		return
	}
	storyboard, ok := service.AgentUIStoryboard(item)
	if !ok {
		return
	}
	note, ok := service.StudioNote(storyboard, method, rest, answer)
	if !ok {
		return
	}
	// The panel is not kept waiting for an agent to take the message.
	go func() {
		if err := notifier.QueueMessage(context.Background(), item.TaskID, note); err != nil {
			log.Printf("studio: could not tell the agent of task %s: %v", item.TaskID, err)
		}
	}()
}

// studioError is what the studio route answers when there is no studio to
// pass a request to. Code lets the panel tell a libretto that is missing or
// too old, which the person can do something about, from anything else.
type studioError struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

// agentStudioHandler passes a studio panel's requests on to the libretto
// studio of its item. The studio is a process of libretto's that listens on
// this machine alone; the panel may be on another, and reaches it only
// through here, behind Legato's own token.
func (s *Server) agentStudioHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fail := func(status int, code, message string) {
			s.writeJSON(w, status, studioError{Error: message, Code: code})
		}
		if s.agentStudio == nil {
			fail(http.StatusServiceUnavailable, "studio_unavailable", "this Legato has no studio")
			return
		}
		address, err := s.agentStudio.Address(r.Context(), r.PathValue("id"))
		var old *libretto.TooOldError
		switch {
		case err == nil:
		case errors.Is(err, libretto.ErrNotInstalled):
			fail(http.StatusServiceUnavailable, "libretto_missing", libretto.Explain(err))
			return
		case errors.As(err, &old):
			fail(http.StatusServiceUnavailable, "libretto_old", libretto.Explain(err))
			return
		case errors.Is(err, service.ErrAgentUINotAStudio), errors.Is(err, store.ErrNotFound):
			fail(http.StatusNotFound, "not_a_studio", "the item is not a studio")
			return
		default:
			fail(http.StatusBadGateway, "studio_failed", err.Error())
			return
		}
		target, err := url.Parse(address)
		if err != nil {
			fail(http.StatusBadGateway, "studio_failed", "the studio gave an address that cannot be read")
			return
		}
		proxy := &httputil.ReverseProxy{
			Rewrite: func(out *httputil.ProxyRequest) {
				out.SetURL(target)
				// The studio sees the request as its own page would send it:
				// under its own address, with the header that page sends.
				// It turns away anything else.
				out.Out.URL.Path, out.Out.URL.RawPath = "/"+r.PathValue("rest"), ""
				out.Out.Host = target.Host
				out.Out.Header.Set("X-Libretto-Studio", "1")
				// The token is Legato's, and no business of the studio's.
				out.Out.Header.Del("Authorization")
				out.Out.Header.Del("Cookie")
				out.Out.Header.Del(studioNotifyHeader)
			},
			ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
				s.writeJSON(w, http.StatusBadGateway, studioError{Error: "the studio did not answer: " + err.Error(), Code: "studio_failed"})
			},
		}
		if r.Method != http.MethodGet && r.Header.Get(studioNotifyHeader) != "" {
			itemID, method, rest := r.PathValue("id"), r.Method, r.PathValue("rest")
			proxy.ModifyResponse = func(res *http.Response) error {
				if res.StatusCode != http.StatusOK {
					return nil
				}
				// The answer is the storyboard as it now is: small, and read
				// here to learn which take the scene was given.
				answer, err := io.ReadAll(res.Body)
				_ = res.Body.Close()
				if err != nil {
					return err
				}
				res.Body = io.NopCloser(bytes.NewReader(answer))
				s.tellAgent(itemID, method, rest, answer)
				return nil
			}
		}
		proxy.ServeHTTP(w, r)
	}
}
