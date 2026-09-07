package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

type handoffHTTPService struct {
	ReviewService
	view             *service.HandoffView
	err              error
	tourID, packetID string
	note             service.HandoffNote
	exportInput      service.HandoffExportInput
	export           *service.HandoffExport
	calls            int
}

func (s *handoffHTTPService) Handoff(_ context.Context, tourID string) (*service.HandoffView, error) {
	s.calls++
	s.tourID = tourID
	return s.view, s.err
}
func (s *handoffHTTPService) SaveHandoffNote(_ context.Context, tourID, packetID string, note service.HandoffNote) (*service.HandoffNote, error) {
	s.calls++
	s.tourID, s.packetID, s.note = tourID, packetID, note
	note.Revision++
	return &note, s.err
}
func (s *handoffHTTPService) ExportHandoff(_ context.Context, tourID string, input service.HandoffExportInput) (*service.HandoffExport, error) {
	s.calls++
	s.tourID, s.exportInput = tourID, input
	return s.export, s.err
}

func handoffRequest(s *Server, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/review/tours/tour-1/handoff"+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	res := httptest.NewRecorder()
	s.Handler().ServeHTTP(res, req)
	return res
}

func TestHandoffGETThroughReviewServiceCapability(t *testing.T) {
	want := &service.HandoffView{TourID: "tour-1", CheckedAt: "2026-09-07", Packets: []service.HandoffPacket{{ID: "packet-2", Revision: 2, Input: service.HandoffInput{Overview: service.HandoffOverview{Why: "Durable context"}}}, {ID: "packet-1", Revision: 1}}}
	svc := &handoffHTTPService{view: want}
	srv := New(nil, nil, nil, "")
	srv.SetAuthToken("secret")
	srv.SetReviewService(svc)
	res := handoffRequest(srv, http.MethodGet, "", "")
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	var got service.HandoffView
	if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(&got, want) || svc.tourID != "tour-1" || svc.calls != 1 {
		t.Fatalf("view=%+v calls=%d tour=%s", got, svc.calls, svc.tourID)
	}
}

func TestHandoffNoteSavesPacketAndBroadcasts(t *testing.T) {
	svc := &handoffHTTPService{}
	srv := New(nil, nil, nil, "")
	srv.SetReviewService(svc)
	messages := make(chan WSMessage, 4)
	client := &wsClient{sendFn: func(msg WSMessage) { messages <- msg }}
	srv.hub.add(client)
	defer srv.hub.remove(client)
	body := `{"revision":3,"readiness":"need_help","body":"Check rollback","answers":{"scenario-1":"Retry"},"skipped":["scenario-2"]}`
	res := handoffRequest(srv, http.MethodPost, "/packet-2/note", body)
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	var got service.HandoffNote
	if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	var want service.HandoffNote
	if err := json.Unmarshal([]byte(body), &want); err != nil {
		t.Fatal(err)
	}
	if svc.tourID != "tour-1" || svc.packetID != "packet-2" || !reflect.DeepEqual(svc.note, want) {
		t.Fatalf("saved=%+v tour=%s packet=%s", svc.note, svc.tourID, svc.packetID)
	}
	want.Revision++
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("note=%+v want=%+v", got, want)
	}
	select {
	case msg := <-messages:
		if msg.Type != MsgReviewChanged || msg.TourID != "tour-1" || msg.Kind != "handoff_note" {
			t.Fatalf("event=%+v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("no review_changed event")
	}
}

func TestHandoffExportReturnsServicePreview(t *testing.T) {
	for _, body := range []string{
		`{"packet_id":"packet-1","format":"github"}`,
		`{"packet_id":"packet-2","format":"maintenance","concept_ids":["concept-2","concept-1"],"include_sources":true,"include_notes":true}`,
	} {
		t.Run(body, func(t *testing.T) {
			want := &service.HandoffExport{Markdown: "# AI-authored brief\nPinned revision", Filename: "handoff.md", Warnings: []string{"Preview before sharing"}}
			svc := &handoffHTTPService{export: want}
			srv := New(nil, nil, nil, "")
			srv.SetReviewService(svc)
			res := handoffRequest(srv, http.MethodPost, "/export", body)
			if res.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
			}
			var got service.HandoffExport
			if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			var input service.HandoffExportInput
			if err := json.Unmarshal([]byte(body), &input); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(&got, want) || !reflect.DeepEqual(svc.exportInput, input) || svc.tourID != "tour-1" || svc.calls != 1 {
				t.Fatalf("export=%+v input=%+v calls=%d", got, svc.exportInput, svc.calls)
			}
			if res.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("content type=%s", res.Header().Get("Content-Type"))
			}
		})
	}
}

func TestHandoffPOSTRejectsInvalidJSONBeforeService(t *testing.T) {
	for _, path := range []string{"/packet-1/note", "/export"} {
		for name, body := range map[string]string{
			"empty": "", "malformed": "{", "unknown": `{"surprise":true}`,
			"multiple": `{} {}`, "trailing": `{} garbage`, "null": `null`, "array": `[]`,
			"oversize field":      `{"body":"` + strings.Repeat("x", service.MaxHandoffBytes) + `"}`,
			"oversize whitespace": `{}` + strings.Repeat(" ", service.MaxHandoffBytes),
		} {
			t.Run(path+"/"+name, func(t *testing.T) {
				svc := &handoffHTTPService{}
				srv := New(nil, nil, nil, "")
				srv.SetReviewService(svc)
				res := handoffRequest(srv, http.MethodPost, path, body)
				if res.Code != http.StatusBadRequest || svc.calls != 0 {
					t.Fatalf("status=%d calls=%d body=%s", res.Code, svc.calls, res.Body.String())
				}
			})
		}
	}
}

func TestHandoffServiceErrors(t *testing.T) {
	for _, route := range []struct{ method, path string }{{http.MethodGet, ""}, {http.MethodPost, "/packet-1/note"}, {http.MethodPost, "/export"}} {
		for _, tc := range []struct {
			name   string
			err    error
			status int
		}{
			{"validation", &service.HandoffValidationError{Message: "invalid readiness"}, 400},
			{"missing", store.ErrNotFound, 404},
			{"handoff conflict", service.ErrHandoffConflict, 409},
			{"store conflict", store.ErrConflict, 409},
			{"internal", errors.New("storage unavailable"), 500},
		} {
			t.Run(route.path+"/"+tc.name, func(t *testing.T) {
				svc := &handoffHTTPService{err: fmt.Errorf("handoff: %w", tc.err)}
				srv := New(nil, nil, nil, "")
				srv.SetReviewService(svc)
				res := handoffRequest(srv, route.method, route.path, `{}`)
				if res.Code != tc.status || svc.calls != 1 {
					t.Fatalf("status=%d calls=%d body=%s", res.Code, svc.calls, res.Body.String())
				}
				var got struct {
					Error string `json:"error"`
				}
				if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				if got.Error == "" {
					t.Fatal("missing error message")
				}
				if tc.status == 400 || tc.status == 500 {
					if !strings.Contains(got.Error, tc.err.Error()) {
						t.Fatalf("error concealed: %q", got.Error)
					}
				}
			})
		}
	}
}

type reviewWithoutHandoff struct{ ReviewService }

func TestHandoffAuthMethodsAndOptionalCapability(t *testing.T) {
	for _, route := range []struct{ method, path string }{{http.MethodGet, ""}, {http.MethodPost, "/packet-1/note"}, {http.MethodPost, "/export"}} {
		t.Run(route.path, func(t *testing.T) {
			svc := &handoffHTTPService{}
			srv := New(nil, nil, nil, "")
			srv.SetAuthToken("secret")
			srv.SetReviewService(svc)
			for _, token := range []string{"", "Bearer wrong"} {
				req := httptest.NewRequest(route.method, "/api/review/tours/tour-1/handoff"+route.path, strings.NewReader(`{}`))
				req.Header.Set("Authorization", token)
				res := httptest.NewRecorder()
				srv.Handler().ServeHTTP(res, req)
				if res.Code != http.StatusUnauthorized {
					t.Fatalf("auth status=%d", res.Code)
				}
			}
			res := handoffRequest(srv, http.MethodDelete, route.path, `{}`)
			if res.Code != http.StatusMethodNotAllowed {
				t.Fatalf("method status=%d", res.Code)
			}
			for _, replacement := range []ReviewService{reviewWithoutHandoff{}, nil} {
				srv.SetReviewService(svc)
				srv.SetReviewService(replacement)
				res = handoffRequest(srv, route.method, route.path, `{}`)
				if res.Code != http.StatusServiceUnavailable {
					t.Fatalf("optional capability status=%d body=%s", res.Code, res.Body.String())
				}
			}
			if svc.calls != 0 {
				t.Fatalf("unexpected calls=%d", svc.calls)
			}
		})
	}
}

func TestHandoffReadsExportsAndFailedNotesDoNotBroadcast(t *testing.T) {
	srv := New(nil, nil, nil, "")
	svc := &handoffHTTPService{view: &service.HandoffView{TourID: "tour-1", Packets: []service.HandoffPacket{}}, export: &service.HandoffExport{Markdown: "Preview"}}
	srv.SetReviewService(svc)
	messages := make(chan WSMessage, 4)
	client := &wsClient{sendFn: func(msg WSMessage) { messages <- msg }}
	srv.hub.add(client)
	defer srv.hub.remove(client)
	res := handoffRequest(srv, http.MethodGet, "", "")
	if res.Code != 200 || !strings.Contains(res.Body.String(), `"packets":[]`) {
		t.Fatalf("empty handoff status=%d body=%s", res.Code, res.Body.String())
	}
	res = handoffRequest(srv, http.MethodPost, "/export", `{"packet_id":"packet-1","format":"github"}`)
	if res.Code != 200 {
		t.Fatalf("export status=%d", res.Code)
	}
	svc.err = service.ErrHandoffConflict
	res = handoffRequest(srv, http.MethodPost, "/packet-1/note", `{"revision":1}`)
	if res.Code != 409 {
		t.Fatalf("note status=%d", res.Code)
	}
	res = handoffRequest(srv, http.MethodPost, "/packet-1/note", `{"unknown":true}`)
	if res.Code != 400 {
		t.Fatalf("invalid note status=%d", res.Code)
	}
	select {
	case msg := <-messages:
		t.Fatalf("unexpected broadcast=%+v", msg)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestHandoffPOSTSizeBoundaryWithoutContentLength(t *testing.T) {
	for _, route := range []struct{ path, body string }{
		{"/packet-1/note", `{"body":"A note"}`},
		{"/export", `{"packet_id":"packet-1","format":"github"}`},
	} {
		for _, extra := range []int{0, 1} {
			t.Run(fmt.Sprintf("%s/%d", route.path, extra), func(t *testing.T) {
				srv := New(nil, nil, nil, "")
				svc := &handoffHTTPService{export: &service.HandoffExport{Markdown: "Preview"}}
				srv.SetReviewService(svc)
				body := route.body + strings.Repeat(" ", service.MaxHandoffBytes-len(route.body)+extra)
				req := httptest.NewRequest(http.MethodPost, "/api/review/tours/tour-1/handoff"+route.path, strings.NewReader(body))
				req.ContentLength = -1
				res := httptest.NewRecorder()
				srv.Handler().ServeHTTP(res, req)
				status, calls := http.StatusOK, 1
				if extra != 0 {
					status, calls = http.StatusBadRequest, 0
				}
				if res.Code != status || svc.calls != calls {
					t.Fatalf("status=%d calls=%d", res.Code, svc.calls)
				}
			})
		}
	}
}

func TestHandoffHTTPUsesPersistedServiceAndNoteConflicts(t *testing.T) {
	f := newReviewServerFixture(t)
	ctx := context.Background()
	review, err := f.svc.Tour(ctx, f.tourID)
	if err != nil {
		t.Fatal(err)
	}
	input := service.HandoffInput{SchemaVersion: 1, PassID: review.Passes[len(review.Passes)-1].Pass.ID, BaseSHA: review.Tour.BaseSHA, HeadSHA: review.Tour.HeadSHA,
		Overview: service.HandoffOverview{Before: "No API package", After: "API package available", Why: "Expose API behavior", NonGoals: "No network request", Decision: "Use an API package", Alternative: "Keep behavior inline", Risk: "No implementation yet", VerificationGaps: "No checks reported", ReviewQuestion: "Is the API contract correct?", Rollback: "Revert the addition"},
		Concepts: []service.HandoffConcept{{ID: "api", Title: "API entry point", Claim: "The API package exists", Flow: []string{"API declaration"}, Invariant: "Package boundary", FailureCase: "Invalid declaration", ChangeGuide: "Start in api.go", SourceIDs: []string{"source"}}}, Sources: []service.HandoffSourceInput{{ID: "source", Path: "api.go", Side: "head", LineStart: 1, LineEnd: 1}}}
	packet, err := f.svc.SubmitHandoff(ctx, f.tourID, input)
	if err != nil {
		t.Fatal(err)
	}
	f.server.SetAuthToken("test-token")
	request := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/review/tours/"+f.tourID+"/handoff"+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer test-token")
		res := httptest.NewRecorder()
		f.server.Handler().ServeHTTP(res, req)
		return res
	}
	res := request(http.MethodGet, "", "")
	if res.Code != 200 || !strings.Contains(res.Body.String(), "package api") {
		t.Fatalf("GET=%d %s", res.Code, res.Body.String())
	}
	path := "/" + packet.ID + "/note"
	body := `{"revision":0,"readiness":"need_help","body":"Human question","answers":{},"skipped":[]}`
	res = request(http.MethodPost, path, body)
	if res.Code != 200 || !strings.Contains(res.Body.String(), `"revision":1`) {
		t.Fatalf("save=%d %s", res.Code, res.Body.String())
	}
	res = request(http.MethodPost, path, body)
	if res.Code != 409 {
		t.Fatalf("conflict=%d %s", res.Code, res.Body.String())
	}
	res = request(http.MethodPost, "/export", `{"packet_id":"`+packet.ID+`","format":"github"}`)
	if res.Code != 200 || !strings.Contains(res.Body.String(), "API entry point") || strings.Contains(res.Body.String(), "Human question") {
		t.Fatalf("export=%d %s", res.Code, res.Body.String())
	}
	res = request(http.MethodPost, "/export", `{"packet_id":"`+packet.ID+`","format":"maintenance","include_notes":true}`)
	if res.Code != 200 || !strings.Contains(res.Body.String(), "Human question") {
		t.Fatalf("selected export=%d %s", res.Code, res.Body.String())
	}
	after, err := f.svc.Tour(ctx, f.tourID)
	if err != nil || after.Tour.Status != "ready" {
		t.Fatalf("note changed review=%+v %v", after, err)
	}
}

// Real ReviewService must activate the optional API without startup wiring.
var _ HandoffService = (*service.ReviewService)(nil)
