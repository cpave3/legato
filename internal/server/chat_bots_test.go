package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

const (
	testSlackApp = "xapp-1-A0-secret-app-token"
	testSlackBot = "xoxb-secret-bot-token"
)

// chatBotsServer serves real chat bots built from settings with tokens;
// none of them connects.
func chatBotsServer(t *testing.T) (http.Handler, *service.ChatBots) {
	t.Helper()
	bots := service.NewChatBots(service.ChatBotSpecs(service.ChatBotSettings{
		Slack:   service.SlackBotSettings{Enabled: true, AppToken: testSlackApp, BotToken: testSlackBot, DefaultChannel: "C1"},
		Discord: service.DiscordBotSettings{Token: "discord-secret-token"},
	}), nil)
	srv := New(&mockBoardService{}, nil, nil, ":0")
	srv.SetChatBots(bots)
	return srv.Handler(), bots
}

func TestChatBotStatusNamesNoToken(t *testing.T) {
	h, _ := chatBotsServer(t)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/settings/integrations/chat-bots", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body)
	}
	for _, secret := range []string{testSlackApp, testSlackBot, "discord-secret-token"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatalf("body holds a token: %s", w.Body)
		}
	}
	var body struct {
		Bots []service.ChatBotStatus `json:"bots"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Bots) != 2 || body.Bots[0].Name != "discord" || body.Bots[0].State != service.ChatBotDisabled || !body.Bots[0].Configured {
		t.Fatalf("bots = %+v", body.Bots)
	}
	if slack := body.Bots[1]; slack.Name != "slack" || slack.State != service.ChatBotConnecting || slack.DefaultChannel != "C1" {
		t.Fatalf("slack = %+v", slack)
	}
}

func TestReconnectingAChatBot(t *testing.T) {
	h, _ := chatBotsServer(t)
	for path, want := range map[string]int{
		"/api/settings/integrations/chat-bots/slack/reconnect":   http.StatusOK,
		"/api/settings/integrations/chat-bots/discord/reconnect": http.StatusConflict, // switched off
		"/api/settings/integrations/chat-bots/teams/reconnect":   http.StatusNotFound,
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", path, nil))
		if w.Code != want {
			t.Errorf("%s = %d, want %d: %s", path, w.Code, want, w.Body)
		}
	}
}

func TestChatBotRoutesWithoutChatBots(t *testing.T) {
	h := New(&mockBoardService{}, nil, nil, ":0").Handler()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/settings/integrations/chat-bots", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestTheWorkflowCheckNamesBotsThatAreNotSetUp(t *testing.T) {
	h, _ := chatBotsServer(t)
	yaml := "schema_version: 1\nname: ops\ntitle: Ops\non:\n  chat.mention:\n    bot: discord\nsteps:\n  - {id: a, title: A, type: chat.message.send, with: {bot: teams, text: hi}}\n"
	payload, _ := json.Marshal(map[string]string{"yaml": yaml})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/workflow-files/check", strings.NewReader(string(payload))))
	if !strings.Contains(w.Body.String(), "bot discord is switched off") || !strings.Contains(w.Body.String(), `no bot named \"teams\"`) {
		t.Fatalf("check = %d: %s", w.Code, w.Body)
	}
}
