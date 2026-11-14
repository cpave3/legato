package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/cpave3/legato/internal/engine/broker"
	"github.com/cpave3/legato/internal/engine/chimera"
	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/store"
)

// ChimeraLinkOptions describes the main session whose mod owns stdin.
type ChimeraLinkOptions struct {
	TaskID, ExecutionID string
	Stdin               io.Reader
	Host                BridgeHost
}

type chimeraLinkHost interface {
	BridgeHost
	bindChimera(context.Context, string, string) error
	unbindChimera(context.Context, string, string) error
	reportChimera(context.Context, []byte, string) error
}

// RunChimeraLink consumes the mod's ordered NDJSON stream until it ends.
// The host owns the binding; no Chimera server is polled during startup.
func RunChimeraLink(ctx context.Context, s *store.Store, opts ChimeraLinkOptions) (err error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	host := opts.Host
	if host == nil {
		host = StoreBridgeHost(s)
	}
	h, ok := host.(chimeraLinkHost)
	if !ok {
		return fmt.Errorf("host cannot link Chimera")
	}
	session, err := h.Session(ctx, opts.TaskID, opts.ExecutionID)
	if err != nil {
		return err
	}
	var endpoint, id string
	var cwd string
	questions := make(map[string]bool)
	permissions := make(map[string]bool)
	// Interceptions precede automatic arbitration. Only pendingPermissions in
	// the snapshot proves that Chimera has actually asked the operator.
	candidates := make(map[string]bool)
	defer func() {
		if endpoint != "" {
			cleanupErr := h.unbindChimera(context.WithoutCancel(ctx), id, endpoint)
			if err == nil {
				err = cleanupErr
			}
		}
	}()
	records := readChimeraRecords(ctx, opts.Stdin)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	var applied uint64
	probeFailed := false
	httpClient := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	for {
		var raw []byte
		select {
		case <-ctx.Done():
			return ctx.Err()
		case record := <-records:
			if record.err != nil {
				if errors.Is(record.err, io.EOF) {
					return nil
				}
				return record.err
			}
			raw = record.raw
		case <-ticker.C:
			if len(candidates) == 0 {
				continue
			}
			probeCtx, stop := context.WithTimeout(ctx, 2*time.Second)
			snapshot, probeErr := chimera.NewClientWithHTTPClient(endpoint, httpClient).GetSession(probeCtx, id)
			stop()
			if probeErr != nil {
				// Client errors can contain the secret URL; do not log them.
				if !probeFailed {
					log.Print("Chimera link: cannot confirm pending permissions; retrying")
				}
				probeFailed = true
				continue
			}
			probeFailed = false
			if snapshot.PendingPermissions == nil {
				continue
			}
			previous := len(permissions)
			clear(permissions)
			for _, p := range *snapshot.PendingPermissions {
				if candidates[p.RequestID] {
					permissions[p.RequestID] = true
				}
			}
			if len(permissions) == previous {
				continue
			}
			raw, _ = json.Marshal(map[string]any{"type": "permission.pending", "pendingRequests": len(questions) + len(permissions)})
			if err := retryChimeraReport(ctx, func() error { return h.reportChimera(ctx, raw, cwd) }); err != nil {
				return err
			}
			continue
		}
		var e struct {
			Seq        uint64 `json:"seq"`
			Type       string `json:"type"`
			SessionID  string `json:"sessionId"`
			ServerURL  string `json:"serverUrl"`
			Cwd        string `json:"cwd"`
			Reason     string `json:"reason"`
			IsSubagent bool   `json:"isSubagent"`
			APIVersion int    `json:"apiVersion"`
			RequestID  string `json:"requestId"`
		}
		if err := json.Unmarshal(raw, &e); err != nil {
			return fmt.Errorf("reading Chimera event: %w", err)
		}
		if e.IsSubagent {
			continue
		}
		if e.Seq == 0 {
			return fmt.Errorf("Chimera event sequence is required")
		}
		if e.Seq <= applied {
			continue
		}
		switch e.Type {
		case "session.start":
			if e.APIVersion != 1 {
				return fmt.Errorf("unsupported Chimera mod API %d", e.APIVersion)
			}
			if e.SessionID == "" {
				return fmt.Errorf("Chimera session ID is required")
			}
			if err := validateChimeraLinkURL(e.ServerURL); err != nil {
				return err
			}
			// Claim authority before saving identity, so a hook already in
			// flight cannot replace the mod's selector after it is written.
			if err := retryChimeraReport(ctx, func() error { return h.reportChimera(ctx, raw, e.Cwd) }); err != nil {
				return err
			}
			if session.RecordsIdentity {
				if err := retryChimeraReport(ctx, func() error { return h.SaveIdentity(ctx, "chimera", e.SessionID, e.Cwd, "") }); err != nil {
					return err
				}
			}
			if err := retryChimeraReport(ctx, func() error { return h.bindChimera(ctx, e.SessionID, e.ServerURL) }); err != nil {
				return err
			}
			endpoint, id = e.ServerURL, e.SessionID
			cwd = e.Cwd
			clear(questions)
			clear(permissions)
			clear(candidates)
			applied = e.Seq
			continue
		case "session.end":
			if e.SessionID != id {
				continue
			}
			if e.Reason == "switch" {
				continue
			}
			if err := retryChimeraReport(ctx, func() error { return h.reportChimera(ctx, raw, cwd) }); err != nil {
				return err
			}
			return nil
		default:
			if id == "" || e.SessionID != id {
				continue
			}
		}
		switch e.Type {
		case "turn.start", "turn.complete":
			clear(questions)
			clear(permissions)
			clear(candidates)
		case "permission.request":
			candidates[e.RequestID] = true
		case "permission.resolved":
			delete(candidates, e.RequestID)
			delete(permissions, e.RequestID)
		case "question.request":
			questions[e.RequestID] = true
		case "question.resolved":
			delete(questions, e.RequestID)
		}
		var record map[string]any
		if err := json.Unmarshal(raw, &record); err != nil {
			return err
		}
		record["pendingRequests"] = len(questions) + len(permissions)
		raw, err = json.Marshal(record)
		if err != nil {
			return err
		}
		if err := retryChimeraReport(ctx, func() error { return h.reportChimera(ctx, raw, cwd) }); err != nil {
			return err
		}
		applied = e.Seq
	}
}

type chimeraRecord struct {
	raw []byte
	err error
}

// The caller owns stdin and closes it when the link ends. A separate reader
// lets the link confirm prompts while a long tool call emits no events.
func readChimeraRecords(ctx context.Context, input io.Reader) <-chan chimeraRecord {
	records := make(chan chimeraRecord)
	go func() {
		scanner := bufio.NewScanner(input)
		scanner.Buffer(make([]byte, 4096), 1024*1024)
		for scanner.Scan() {
			select {
			case records <- chimeraRecord{raw: append([]byte(nil), scanner.Bytes()...)}:
			case <-ctx.Done():
				return
			}
		}
		err := scanner.Err()
		if err == nil {
			err = io.EOF
		}
		select {
		case records <- chimeraRecord{err: err}:
		case <-ctx.Done():
		}
	}()
	return records
}

// A short broker outage pauses the same stream; refusals still reach stderr.
func retryChimeraReport(ctx context.Context, apply func() error) error {
	for {
		err := apply()
		if !errors.Is(err, broker.ErrUnreachable) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func validateChimeraLinkURL(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Port() == "" {
		return fmt.Errorf("Chimera server URL must be loopback HTTP")
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("Chimera server URL must be loopback HTTP")
	}
	return nil
}

func (h *storeBridgeHost) bindChimera(ctx context.Context, id, endpoint string) error {
	if err := h.s.UpdateChimeraDiscoveryForSession(ctx, h.session.ID, id, endpoint); err != nil {
		return err
	}
	if err := h.s.RemoveChimeraLaunchSecretFile(ctx, h.session.ID); err != nil {
		return fmt.Errorf("removing Chimera bootstrap secret: %w", err)
	}
	ipc.Broadcast(ipc.Message{Type: "agents_changed", TaskID: h.taskID})
	return nil
}
func (h *storeBridgeHost) unbindChimera(ctx context.Context, id, endpoint string) error {
	cleared, err := h.s.ClearChimeraDiscoveryForSession(ctx, h.session.ID, id, endpoint)
	if cleared {
		ipc.Broadcast(ipc.Message{Type: "agents_changed", TaskID: h.taskID})
	}
	return err
}
func (h *storeBridgeHost) reportChimera(ctx context.Context, raw []byte, cwd string) error {
	hook, err := ParseChimeraHook(raw)
	if err != nil {
		return err
	}
	return ApplyChimeraHook(h.s, ChimeraHookReport{TaskID: h.taskID, ExecutionID: h.session.ExecutionID, WorkingDir: cwd, Hook: hook}, nil, nil)
}
func (h *brokerBridgeHost) bindChimera(ctx context.Context, id, endpoint string) error {
	return h.call(ctx, "endpoint.bind", map[string]string{"endpoint": endpoint, "session_id": id}, nil)
}
func (h *brokerBridgeHost) unbindChimera(ctx context.Context, id, endpoint string) error {
	return h.call(ctx, "endpoint.unbind", map[string]string{"endpoint": endpoint, "session_id": id}, nil)
}
func (h *brokerBridgeHost) reportChimera(ctx context.Context, raw []byte, cwd string) error {
	hook, err := ParseChimeraHook(raw)
	if err != nil {
		return err
	}
	return h.call(ctx, "agent.hook", map[string]any{"kind": "chimera", "hook": hook, "working_dir": cwd}, nil)
}
