package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

func boardCards(t *testing.T, svc *mockBoardService) []CardResponse {
	t.Helper()
	w := httptest.NewRecorder()
	New(svc, nil, nil, ":0").Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/board", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var resp BoardResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp.Columns[0].Cards
}

func TestBoardCardsCarryTheTicketKeyAndAddress(t *testing.T) {
	svc := &mockBoardService{
		columns: []service.Column{{Name: "Doing"}},
		cards: map[string][]service.Card{"Doing": {
			{ID: "k3x9q2ma", Title: "Forms", Status: "Doing", Provider: "burnlite", RemoteID: "LEG-386", URL: "https://burnlite.dev/LEG-386"},
			{ID: "local1", Title: "Local", Status: "Doing"},
		}},
	}
	cards := boardCards(t, svc)
	if cards[0].RemoteID != "LEG-386" || cards[0].SourceURL != "https://burnlite.dev/LEG-386" {
		t.Fatalf("card = %+v", cards[0])
	}
	raw, _ := json.Marshal(cards[1])
	var fields map[string]any
	_ = json.Unmarshal(raw, &fields)
	if _, ok := fields["remote_id"]; ok {
		t.Fatalf("local card has remote_id: %s", raw)
	}
	if _, ok := fields["source_url"]; ok {
		t.Fatalf("local card has source_url: %s", raw)
	}
}
