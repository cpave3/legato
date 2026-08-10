package server

import (
	"context"
	"errors"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

func TestHandleAgentPermissionForwardsTypedAction(t *testing.T) {
	agents := &promptAgentService{}
	server := New(nil, agents, nil, "")
	var messages []WSMessage
	client := &wsClient{ctx: context.Background(), sendFn: func(message WSMessage) {
		messages = append(messages, message)
	}}
	feedback := "  Use a read-only command  "

	server.handleWSMessage(client, WSMessage{
		Type: MsgAgentPermission, AgentID: "task-1", RequestID: "req-1",
		Decision: "deny", Feedback: &feedback,
	})

	if len(messages) != 0 {
		t.Fatalf("unexpected response: %#v", messages)
	}
	if agents.permissionTaskID != "task-1" || agents.permissionReqID != "req-1" {
		t.Fatalf("permission target = %q/%q", agents.permissionTaskID, agents.permissionReqID)
	}
	want := (service.PermissionAction{Decision: "deny", Feedback: "Use a read-only command"})
	if agents.permissionAction != want {
		t.Fatalf("action = %#v, want %#v", agents.permissionAction, want)
	}
}

func TestHandleAgentPermissionReturnsCorrelatedErrors(t *testing.T) {
	tests := []struct {
		name     string
		message  WSMessage
		agentErr error
		want     string
	}{
		{
			name:    "missing request",
			message: WSMessage{Type: MsgAgentPermission, AgentID: "task-1", Decision: "allow"},
			want:    "agent_id and request_id are required",
		},
		{
			name:    "invalid decision",
			message: WSMessage{Type: MsgAgentPermission, AgentID: "task-1", RequestID: "req-1", Decision: "later"},
			want:    "decision must be allow or deny",
		},
		{
			name: "blank feedback",
			message: func() WSMessage {
				feedback := "   "
				return WSMessage{Type: MsgAgentPermission, AgentID: "task-1", RequestID: "req-1", Decision: "deny", Feedback: &feedback}
			}(),
			want: "feedback must not be blank",
		},
		{
			name:     "downstream failure",
			message:  WSMessage{Type: MsgAgentPermission, AgentID: "task-1", RequestID: "req-1", Decision: "allow"},
			agentErr: errors.New("409 Conflict"),
			want:     "resolve permission failed: 409 Conflict",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agents := &promptAgentService{permissionErr: tt.agentErr}
			server := New(nil, agents, nil, "")
			var got WSMessage
			client := &wsClient{ctx: context.Background(), sendFn: func(message WSMessage) { got = message }}

			server.handleWSMessage(client, tt.message)

			if got.Type != MsgError || got.AgentID != tt.message.AgentID || got.RequestID != tt.message.RequestID || got.Error != tt.want {
				t.Fatalf("response = %#v", got)
			}
		})
	}
}
