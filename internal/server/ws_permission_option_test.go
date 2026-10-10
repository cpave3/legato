package server

import (
	"context"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

func TestHandleAgentPermissionForwardsTheWayItWasAllowed(t *testing.T) {
	agents := &promptAgentService{}
	server := New(nil, agents, nil, "")
	var messages []WSMessage
	client := &wsClient{ctx: context.Background(), sendFn: func(message WSMessage) {
		messages = append(messages, message)
	}}

	server.handleWSMessage(client, WSMessage{
		Type: MsgAgentPermission, AgentID: "task-1", RequestID: "req-1", Decision: "allow", Option: "always",
	})

	if len(messages) != 0 {
		t.Fatalf("unexpected response: %#v", messages)
	}
	if want := (service.PermissionAction{Decision: "allow", Option: "always"}); agents.permissionAction != want {
		t.Fatalf("action = %#v, want %#v", agents.permissionAction, want)
	}
}

func TestHandleAgentPermissionRefusesAnOptionOnADenial(t *testing.T) {
	agents := &promptAgentService{}
	server := New(nil, agents, nil, "")
	var got WSMessage
	client := &wsClient{ctx: context.Background(), sendFn: func(message WSMessage) { got = message }}

	server.handleWSMessage(client, WSMessage{
		Type: MsgAgentPermission, AgentID: "task-1", RequestID: "req-1", Decision: "deny", Option: "always",
	})

	if got.Type != MsgError || got.RequestID != "req-1" || got.Error != "an option is only valid when allowing permission" {
		t.Fatalf("response = %#v", got)
	}
	if agents.permissionReqID != "" {
		t.Fatalf("the denial was forwarded: %#v", agents.permissionAction)
	}
}
