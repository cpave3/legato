package server

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

func TestHandleAgentQuestionForwardsTheAnswers(t *testing.T) {
	agents := &promptAgentService{}
	server := New(nil, agents, nil, "")
	var messages []WSMessage
	client := &wsClient{ctx: context.Background(), sendFn: func(message WSMessage) {
		messages = append(messages, message)
	}}

	server.handleWSMessage(client, WSMessage{
		Type: MsgAgentQuestion, AgentID: "task-1", RequestID: "req-1",
		Answers: map[string][]string{"q1": {"Blue"}, "q2": {"Small", "Medium"}},
	})

	if len(messages) != 0 {
		t.Fatalf("unexpected response: %#v", messages)
	}
	if agents.questionTaskID != "task-1" || agents.questionReqID != "req-1" {
		t.Fatalf("question target = %q/%q", agents.questionTaskID, agents.questionReqID)
	}
	want := service.QuestionAnswer{Answers: map[string][]string{"q1": {"Blue"}, "q2": {"Small", "Medium"}}}
	if !reflect.DeepEqual(agents.questionAnswer, want) {
		t.Fatalf("answer = %#v, want %#v", agents.questionAnswer, want)
	}
}

func TestHandleAgentQuestionForwardsADismissal(t *testing.T) {
	agents := &promptAgentService{}
	server := New(nil, agents, nil, "")
	client := &wsClient{ctx: context.Background(), sendFn: func(WSMessage) {}}

	server.handleWSMessage(client, WSMessage{Type: MsgAgentQuestion, AgentID: "task-1", RequestID: "req-1", Dismissed: true})

	if want := (service.QuestionAnswer{Dismissed: true}); !reflect.DeepEqual(agents.questionAnswer, want) {
		t.Fatalf("answer = %#v, want %#v", agents.questionAnswer, want)
	}
}

func TestHandleAgentQuestionReturnsCorrelatedErrors(t *testing.T) {
	tests := []struct {
		name     string
		message  WSMessage
		agentErr error
		want     string
	}{
		{
			name:    "missing request",
			message: WSMessage{Type: MsgAgentQuestion, AgentID: "task-1", Dismissed: true},
			want:    "agent_id and request_id are required",
		},
		{
			name:    "neither answered nor dismissed",
			message: WSMessage{Type: MsgAgentQuestion, AgentID: "task-1", RequestID: "req-1"},
			want:    "answers are required unless the question is dismissed",
		},
		{
			name: "answered and dismissed",
			message: WSMessage{
				Type: MsgAgentQuestion, AgentID: "task-1", RequestID: "req-1",
				Answers: map[string][]string{"q1": {"Blue"}}, Dismissed: true,
			},
			want: "a dismissed question takes no answers",
		},
		{
			name: "downstream failure",
			message: WSMessage{
				Type: MsgAgentQuestion, AgentID: "task-1", RequestID: "req-1",
				Answers: map[string][]string{"q1": {"Blue"}},
			},
			agentErr: errors.New("400 Bad Request"),
			want:     "answer question failed: 400 Bad Request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agents := &promptAgentService{questionErr: tt.agentErr}
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
