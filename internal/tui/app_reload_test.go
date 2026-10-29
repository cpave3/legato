package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/agents"
	"github.com/cpave3/legato/internal/tui/statusbar"
	"github.com/cpave3/legato/internal/tui/theme"
)

type restartingAgents struct {
	*fakeAgentService
	restarted []string
	err       error
}

func (r *restartingAgents) RestartAgent(_ context.Context, taskID string, _ []service.Resource) error {
	r.restarted = append(r.restarted, taskID)
	return r.err
}

func reloadApp(svc service.AgentService) App {
	return NewApp(&fakeBoardService{}, nil, svc, nil, &fakeReportService{}, theme.NewIcons("unicode"), nil, "", nil, nil, "", nil, nil)
}

func TestReloadingAnAgentRestartsItAndSaysSo(t *testing.T) {
	svc := &restartingAgents{fakeAgentService: &fakeAgentService{agents: twoAgents()}}
	app := reloadApp(svc)

	_, cmd := app.Update(agents.ReloadAgentMsg{TaskID: "task-1"})
	msgs := messagesOf(cmd)
	if len(svc.restarted) != 1 || svc.restarted[0] != "task-1" {
		t.Fatalf("restarted = %v", svc.restarted)
	}
	var said string
	for _, msg := range msgs {
		if info, ok := msg.(statusbar.InfoMsg); ok {
			said = info.Text
		}
	}
	if !strings.Contains(said, "task-1") {
		t.Fatalf("status = %q, msgs = %#v", said, msgs)
	}
}

func TestAnAgentThatCannotBeReloadedSaysWhy(t *testing.T) {
	svc := &restartingAgents{fakeAgentService: &fakeAgentService{agents: twoAgents()}, err: errors.New("a swarm runs this agent")}
	app := reloadApp(svc)

	_, cmd := app.Update(agents.ReloadAgentMsg{TaskID: "task-1"})
	var said string
	for _, msg := range messagesOf(cmd) {
		if e, ok := msg.(statusbar.ErrorMsg); ok {
			said = e.Text
		}
	}
	if !strings.Contains(said, "a swarm runs this agent") {
		t.Fatalf("error = %q", said)
	}
}
