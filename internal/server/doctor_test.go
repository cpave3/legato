package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

type fakeDoctorReports struct {
	report   service.DoctorReport
	known    bool
	rechecks int
	after    *service.DoctorReport
}

func (f *fakeDoctorReports) Latest(context.Context) service.DoctorReport {
	f.known = true
	return f.report
}

func (f *fakeDoctorReports) Recheck(context.Context) service.DoctorReport {
	f.rechecks++
	if f.after != nil {
		f.report = *f.after
	}
	f.known = true
	return f.report
}

func (f *fakeDoctorReports) Known() (service.DoctorReport, bool) { return f.report, f.known }

var tmuxMissingReport = service.DoctorReport{Checks: []service.DoctorCheck{
	{ID: "tmux", Title: "tmux", Required: true, Result: service.DoctorMissing, Detail: "tmux not found on Legato's PATH", Fix: "Install tmux"},
	{ID: "yg", Title: "Yggdrasil (yg)", Result: service.DoctorOK, Detail: "/usr/bin/yg"},
}}

func doctorServer(reports *fakeDoctorReports) *Server {
	srv := New(&mockBoardService{}, nil, nil, ":0")
	srv.SetAuthToken("secret")
	srv.SetDoctor(reports)
	return srv
}

func doctorRequest(srv *Server, method, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestTheDoctorReportNeedsAToken(t *testing.T) {
	srv := doctorServer(&fakeDoctorReports{report: tmuxMissingReport})
	if rec := doctorRequest(srv, http.MethodGet, "/api/doctor", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("without a token: %d", rec.Code)
	}
	rec := doctorRequest(srv, http.MethodGet, "/api/doctor", "secret")
	var got struct {
		service.DoctorReport
		OK bool `json:"ok"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
		t.Fatalf("with a token: %d %s", rec.Code, rec.Body)
	}
	if got.OK || len(got.Checks) != 2 || got.Checks[0].Fix != "Install tmux" {
		t.Fatalf("report = %+v", got)
	}
}

func TestCheckAgainRunsTheChecks(t *testing.T) {
	fixed := service.DoctorReport{Checks: []service.DoctorCheck{{ID: "tmux", Title: "tmux", Required: true, Result: service.DoctorOK}}}
	reports := &fakeDoctorReports{report: tmuxMissingReport, after: &fixed}
	srv := doctorServer(reports)
	if rec := doctorRequest(srv, http.MethodPost, "/api/doctor/check", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("without a token: %d", rec.Code)
	}
	rec := doctorRequest(srv, http.MethodPost, "/api/doctor/check", "secret")
	if rec.Code != http.StatusOK || reports.rechecks != 1 || !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("check again: %d %s (rechecks %d)", rec.Code, rec.Body, reports.rechecks)
	}
}

func TestMetaSaysOnlyWhetherSetupIsIncomplete(t *testing.T) {
	srv := doctorServer(&fakeDoctorReports{report: tmuxMissingReport, known: true})
	rec := doctorRequest(srv, http.MethodGet, "/api/meta", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("meta: %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"setup_incomplete":true`) {
		t.Fatalf("meta = %s", body)
	}
	for _, leak := range []string{"tmux", "checks", "yg"} {
		if strings.Contains(body, leak) {
			t.Errorf("meta names %q: %s", leak, body)
		}
	}
	srv = doctorServer(&fakeDoctorReports{report: service.DoctorReport{}, known: true})
	if body := doctorRequest(srv, http.MethodGet, "/api/meta", "").Body.String(); !strings.Contains(body, `"setup_incomplete":false`) {
		t.Fatalf("meta with every check passed = %s", body)
	}
}

func TestSettingsSayWhenSetupIsIncomplete(t *testing.T) {
	srv := doctorServer(&fakeDoctorReports{report: tmuxMissingReport, known: true})
	rec := doctorRequest(srv, http.MethodGet, "/api/settings", "secret")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"setup_incomplete":true`) {
		t.Fatalf("settings: %d %s", rec.Code, rec.Body)
	}
}
