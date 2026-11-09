package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/auth"
	"github.com/cpave3/legato/internal/engine/credential"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/server"
	"github.com/cpave3/legato/internal/service"
)

// processChangeSets is this process's change set service: it proposes the
// Reeve's sets (its tools run here when this process serves the broker),
// applies decisions on them, and runs the approved ones while this
// process serves the broker.
var processChangeSets *service.ChangeSetService

// processSecrets asks the person for the Reeve's secrets.
var processSecrets *service.ReeveSecrets

// processStart is when this process started: a set that ended in a restart
// before it is reported as Legato being back.
var processStart = time.Now()

// startChangeSets makes the change set service of this process and runs
// approved sets in the background while it serves the broker.
func startChangeSets(ctx context.Context, cfg *config.Config, db *store.Store, approvals *service.ApprovalService, agents service.AgentService, owner func() bool) *service.ChangeSetService {
	reeve := newReeveService(db, agents, nil, approvals)
	changeSets := service.NewChangeSetService(service.ChangeSetOptions{
		Store: db, Approvals: approvals, Settings: settingsFor(db),
		Classify:       classifyCall,
		Run:            localAPI(cfg),
		ClassifyLegato: classifyLegatoCommand,
		RunLegato:      runLegatoCommand,
		HostExec:       runHostCommand,
		OutputDir:      filepath.Join(service.ReeveFolder(os.Getenv), "host-output"),
		Restart:        restartAll,
		Notify: func(ctx context.Context, taskID, text string) {
			notifyReeve(ctx, db, reeve, taskID, text)
		},
		RunningAgents: func(ctx context.Context) []string {
			var ids []string
			if agents == nil {
				return nil
			}
			sessions, _ := agents.ListAgents(ctx)
			for _, s := range sessions {
				if s.Status == "running" && !service.IsReeveTask(ctx, db, s.TaskID) {
					ids = append(ids, s.TaskID)
				}
			}
			return ids
		},
	})
	changeSets.Register()
	processSecrets = &service.ReeveSecrets{Approvals: approvals, Sink: legatoSecretSink{cfg: cfg, db: db}}
	processSecrets.Register()
	if owner != nil {
		go whileBrokerOwner(ctx, owner, 2*time.Second, func(ctx context.Context) { changeSets.RunQueued(ctx) })
	}
	processChangeSets = changeSets
	return changeSets
}

// classifyCall classes a call by the route class table. A call that is not
// a request at all is never made.
func classifyCall(method, path string) (string, string) {
	req, err := http.NewRequest(method, path, nil)
	if err != nil || strings.ContainsFunc(method+path, func(r rune) bool { return r <= ' ' || r == 0x7f }) {
		return "never", "Make a request that does not parse"
	}
	info := server.ClassifyRequest(req)
	return string(info.Class), info.Description
}

// localAPI runs a call against this instance's own web API with the web
// token: what the person approved, made as the person would make it.
func localAPI(cfg *config.Config) service.CallRunner {
	return func(ctx context.Context, method, path string, body []byte) (int, string, error) {
		token, err := auth.ReadToken(config.ResolveDataDir(cfg))
		if err != nil {
			return 0, "", fmt.Errorf("reading the web token: %w", err)
		}
		scheme := "http"
		if tlsEnabled(cfg) {
			scheme = "https"
		}
		req, err := http.NewRequestWithContext(ctx, method, scheme+"://127.0.0.1:"+cfg.Web.Port+path, bytes.NewReader(body))
		if err != nil {
			return 0, "", err
		}
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
		if len(body) > 0 {
			req.Header.Set("Content-Type", "application/json")
		}
		client := &http.Client{Timeout: 2 * time.Minute, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
			// A redirect is answered as it is, never followed: the call
			// fails rather than turning into another request.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		resp, err := client.Do(req)
		if err != nil {
			return 0, "", err
		}
		defer resp.Body.Close()
		out, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return resp.StatusCode, string(out), nil
	}
}

// notifyReeve tells a Reeve session something: into it when it is the
// running session, resuming it when it is suspended; an ended session is
// not told.
func notifyReeve(ctx context.Context, db *store.Store, reeve *service.ReeveService, taskID, text string) {
	if reeve == nil {
		return
	}
	current, err := db.CurrentReeveSession(ctx)
	if err != nil || current == nil || current.TaskID != taskID {
		return
	}
	_ = reeve.Message(ctx, text, "legato")
}

// legatoSecretSink keeps secrets the person pasted for the Reeve in
// Legato's own sealed store.
type legatoSecretSink struct {
	cfg *config.Config
	db  *store.Store
}

func (s legatoSecretSink) Put(ctx context.Context, name, value string) (string, error) {
	if err := configuredSecretStore(s.cfg, s.db).Add(ctx, name, credential.TypeValue, []byte(value)); err != nil {
		return "", err
	}
	return "legato://" + name, nil
}
