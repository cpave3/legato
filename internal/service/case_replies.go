package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/credential"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/oauthclient"
	"github.com/cpave3/legato/internal/engine/store"
)

// ArgusRefreshSecret is the name, in Legato's sealed store, of the refresh
// token Legato holds as Argus's OAuth client.
const ArgusRefreshSecret = "integration-argus-refresh"

// Reply outcomes an agent can send.
var caseReplyOutcomes = map[string]bool{"done": true, "finding": true, "question": true, "cannot": true}

const (
	maxCaseReplyText   = 16 << 10
	caseReplyFirstWait = 10 * time.Second
	caseReplyGiveUp    = 24 * time.Hour
	caseReplyMinDelay  = 30 * time.Second
	caseReplyMaxDelay  = time.Hour
	// caseReplyHold is when a reply held by a broken connection is looked at
	// again.
	caseReplyHold = time.Minute
)

// ErrIntegrationDisconnected: Legato has no working connection to the
// integration; the person has to run legato integration connect again.
var ErrIntegrationDisconnected = errors.New("Legato is not connected to Argus: run legato integration connect argus <url>")

// IntegrationSecrets reads and writes the integration's refresh token.
type IntegrationSecrets interface {
	Read(ctx context.Context, name string) (string, error)
	Write(ctx context.Context, name, value string) error
}

// SealedIntegrationSecrets keeps integration secrets in Legato's sealed
// store, as legato://name of type oauth-refresh.
type SealedIntegrationSecrets struct{ Store *SecretStoreService }

func (s SealedIntegrationSecrets) Read(ctx context.Context, name string) (string, error) {
	ref, err := parseLegatoRef(name)
	if err != nil {
		return "", err
	}
	secret, err := s.Store.Get(ctx, ref)
	if err != nil {
		return "", err
	}
	return string(secret.Value), nil
}

func (s SealedIntegrationSecrets) Write(ctx context.Context, name, value string) error {
	return s.Store.Add(ctx, name, "oauth-refresh", []byte(value))
}

// CaseReplyInput is what an agent's reply_case call says.
type CaseReplyInput struct {
	DelegationID string
	Outcome      string
	Text         string
}

// CaseReplyResult is what the agent is told: sent, or queued to retry.
type CaseReplyResult struct {
	ID    string `json:"id"`
	State string `json:"state"`
}

// CaseReplyOptions configure the reply service.
type CaseReplyOptions struct {
	// Integration names the connection (argus).
	Integration string
	// URL is the integration's address, where replies go.
	URL     string
	Secrets IntegrationSecrets
	HTTP    *http.Client
	Now     func() time.Time
	Bus     *events.Bus
	// Sending is false in a process that only records replies for the host
	// to send.
	NoSending bool
}

// CaseReplyService records agents' replies to delegations and sends them to
// the integration, as its OAuth client, retrying until they are taken.
type CaseReplyService struct {
	db   *store.Store
	opts CaseReplyOptions

	mu        sync.Mutex // one refresh at a time
	access    string
	expiresAt time.Time
	sendMu    sync.Mutex // one send of each entry at a time
}

func NewCaseReplyService(db *store.Store, opts CaseReplyOptions) *CaseReplyService {
	if opts.HTTP == nil {
		opts.HTTP = &http.Client{Timeout: 30 * time.Second}
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Integration == "" {
		opts.Integration = "argus"
	}
	opts.URL = strings.TrimRight(opts.URL, "/")
	return &CaseReplyService{db: db, opts: opts}
}

// Reply records an agent's reply and, in the sending host, tries it at once.
// It refuses a reply to a delegation that was not delivered to the caller's
// task.
func (s *CaseReplyService) Reply(ctx context.Context, taskID, executionID string, in CaseReplyInput) (CaseReplyResult, error) {
	if !caseReplyOutcomes[in.Outcome] {
		return CaseReplyResult{}, fmt.Errorf("outcome must be done, finding, question or cannot, not %q", in.Outcome)
	}
	if len(in.Text) == 0 || len(in.Text) > maxCaseReplyText {
		return CaseReplyResult{}, errors.New("text must be 1 byte to 16 KiB")
	}
	d, err := s.db.GetDelegation(ctx, in.DelegationID)
	if errors.Is(err, store.ErrNotFound) {
		return CaseReplyResult{}, fmt.Errorf("no delegation %s", in.DelegationID)
	}
	if err != nil {
		return CaseReplyResult{}, err
	}
	if d.TaskID != taskID {
		return CaseReplyResult{}, fmt.Errorf("delegation %s was not sent to this task", in.DelegationID)
	}
	if d.State != store.DelegationDelivered {
		return CaseReplyResult{}, fmt.Errorf("delegation %s is %s, not delivered", in.DelegationID, d.State)
	}
	body, _ := json.Marshal(map[string]any{
		"delegationId": d.ID, "outcome": in.Outcome, "text": in.Text,
		"agent": map[string]string{"taskId": taskID, "agentId": executionID},
	})
	now := s.opts.Now()
	entry := store.CaseReply{ID: newDelegationID("cr-"), DelegationID: d.ID, CaseID: d.CaseID, TaskID: taskID, Body: string(body), CreatedAt: now, NextAttemptAt: now}
	if err := s.db.InsertCaseReply(ctx, entry); err != nil {
		return CaseReplyResult{}, err
	}
	s.changed(taskID)
	if s.opts.NoSending {
		return CaseReplyResult{ID: entry.ID, State: "queued"}, nil
	}
	attemptCtx, cancel := context.WithTimeout(ctx, caseReplyFirstWait)
	defer cancel()
	outcome := s.attempt(attemptCtx, entry)
	switch outcome.state {
	case store.CaseReplySent:
		return CaseReplyResult{ID: entry.ID, State: "sent"}, nil
	case store.CaseReplyFailed:
		return CaseReplyResult{ID: entry.ID, State: "failed"}, fmt.Errorf("Argus refused the reply: %s", outcome.reason)
	}
	return CaseReplyResult{ID: entry.ID, State: "queued"}, nil
}

// SendDue tries every reply whose next attempt is due.
func (s *CaseReplyService) SendDue(ctx context.Context) {
	due, err := s.db.DueCaseReplies(ctx, s.opts.Now())
	if err != nil {
		log.Printf("case replies: %v", err)
		return
	}
	for _, entry := range due {
		s.attempt(ctx, entry)
	}
}

// Run sends due replies until ctx ends.
func (s *CaseReplyService) Run(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		s.SendDue(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

type attemptOutcome struct {
	state  string
	reason string
}

// attempt sends one reply and records what became of it.
func (s *CaseReplyService) attempt(ctx context.Context, entry store.CaseReply) attemptOutcome {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	if current, err := s.db.GetCaseReply(ctx, entry.ID); err != nil || current.State != store.CaseReplyPending {
		if err == nil {
			return attemptOutcome{state: current.State, reason: current.LastError}
		}
		return attemptOutcome{state: store.CaseReplyPending}
	} else {
		entry = *current
	}
	now := s.opts.Now()
	if now.Sub(entry.CreatedAt) >= caseReplyGiveUp {
		return s.fail(ctx, entry, "not taken by Argus within 24 hours: "+entry.LastError)
	}
	status, problem, err := s.post(ctx, entry, false)
	switch {
	case errors.Is(err, ErrIntegrationDisconnected):
		_ = s.db.HoldCaseReply(ctx, entry.ID, now.Add(caseReplyHold), err.Error())
		return attemptOutcome{state: store.CaseReplyPending, reason: err.Error()}
	case err != nil:
		return s.retry(ctx, entry, err.Error())
	case status >= 200 && status < 300:
		if err := s.db.MarkCaseReplySent(ctx, entry.ID, now); err != nil {
			log.Printf("case reply %s: %v", entry.ID, err)
		}
		s.changed(entry.TaskID)
		return attemptOutcome{state: store.CaseReplySent}
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		// A fresh token was refused: the connection is broken, not the reply.
		s.disconnect(ctx, fmt.Sprintf("Argus answered %d to a fresh token", status))
		_ = s.db.HoldCaseReply(ctx, entry.ID, now.Add(caseReplyHold), ErrIntegrationDisconnected.Error())
		return attemptOutcome{state: store.CaseReplyPending, reason: ErrIntegrationDisconnected.Error()}
	case status >= 400 && status < 500:
		return s.fail(ctx, entry, fmt.Sprintf("%d %s", status, problem))
	default:
		return s.retry(ctx, entry, fmt.Sprintf("%d %s", status, problem))
	}
}

func (s *CaseReplyService) retry(ctx context.Context, entry store.CaseReply, reason string) attemptOutcome {
	delay := caseReplyMinDelay << entry.Attempts
	if delay <= 0 || delay > caseReplyMaxDelay {
		delay = caseReplyMaxDelay
	}
	_ = s.db.RescheduleCaseReply(ctx, entry.ID, s.opts.Now().Add(delay), reason)
	return attemptOutcome{state: store.CaseReplyPending, reason: reason}
}

func (s *CaseReplyService) fail(ctx context.Context, entry store.CaseReply, reason string) attemptOutcome {
	_ = s.db.MarkCaseReplyFailed(ctx, entry.ID, reason)
	s.changed(entry.TaskID)
	return attemptOutcome{state: store.CaseReplyFailed, reason: reason}
}

// post sends a reply with the access token; on a 401 it refreshes once and
// tries again. It returns Argus's status and its error text.
func (s *CaseReplyService) post(ctx context.Context, entry store.CaseReply, refreshed bool) (int, string, error) {
	token, err := s.accessToken(ctx, refreshed)
	if err != nil {
		return 0, "", err
	}
	address := s.opts.URL + "/api/cases/" + url.PathEscape(entry.CaseID) + "/replies"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewReader([]byte(entry.Body)))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", entry.ID)
	resp, err := s.opts.HTTP.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode == http.StatusUnauthorized && !refreshed {
		return s.post(ctx, entry, true)
	}
	return resp.StatusCode, problemText(raw), nil
}

// problemText is Argus's {error, message} as one line.
func problemText(raw []byte) string {
	var p struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &p) == nil && p.Error != "" {
		return strings.TrimSpace(p.Error + ": " + p.Message)
	}
	return ""
}

// accessToken is the access token in hand, or a new one. force refreshes
// even when the one in hand has not expired.
func (s *CaseReplyService) accessToken(ctx context.Context, force bool) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !force && s.access != "" && s.opts.Now().Add(time.Minute).Before(s.expiresAt) {
		return s.access, nil
	}
	in, err := s.db.GetIntegration(ctx, s.opts.Integration)
	if errors.Is(err, store.ErrNotFound) || (err == nil && in.State != store.IntegrationConnected) {
		return "", ErrIntegrationDisconnected
	}
	if err != nil {
		return "", err
	}
	md := &oauthclient.Metadata{Issuer: in.Issuer, TokenEndpoint: in.TokenEndpoint, Resource: in.Resource}
	refresh, err := s.opts.Secrets.Read(ctx, ArgusRefreshSecret)
	if err != nil {
		return "", fmt.Errorf("reading Argus's refresh token: %w", err)
	}
	token, err := oauthclient.Refresh(ctx, s.opts.HTTP, md, in.ClientID, refresh)
	if errors.Is(err, oauthclient.ErrInvalidGrant) {
		// Another process may have rotated it since it was read.
		if again, readErr := s.opts.Secrets.Read(ctx, ArgusRefreshSecret); readErr == nil && again != refresh {
			token, err = oauthclient.Refresh(ctx, s.opts.HTTP, md, in.ClientID, again)
		}
	}
	if errors.Is(err, oauthclient.ErrInvalidGrant) {
		s.disconnectLocked(ctx, "Argus refused Legato's refresh token")
		return "", ErrIntegrationDisconnected
	}
	if err != nil {
		return "", err
	}
	// The rotated token is kept before the new access token is used.
	if token.RefreshToken != "" && token.RefreshToken != refresh {
		if err := s.opts.Secrets.Write(ctx, ArgusRefreshSecret, token.RefreshToken); err != nil {
			return "", fmt.Errorf("keeping Argus's new refresh token: %w", err)
		}
	}
	// The token's lifetime, on this service's clock.
	lifetime := 5 * time.Minute
	if !token.ExpiresAt.IsZero() {
		lifetime = time.Until(token.ExpiresAt)
	}
	s.access, s.expiresAt = token.AccessToken, s.opts.Now().Add(lifetime)
	return s.access, nil
}

func (s *CaseReplyService) disconnect(ctx context.Context, why string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.disconnectLocked(ctx, why)
}

func (s *CaseReplyService) disconnectLocked(ctx context.Context, why string) {
	s.access = ""
	if err := s.db.SetIntegrationState(ctx, s.opts.Integration, store.IntegrationDisconnected); err != nil {
		log.Printf("integration %s: %v", s.opts.Integration, err)
	}
	log.Printf("integration %s disconnected: %s", s.opts.Integration, why)
	s.changed("")
}

func (s *CaseReplyService) changed(taskID string) {
	if s.opts.Bus != nil {
		s.opts.Bus.Publish(events.Event{Type: events.EventDelegationChanged, At: time.Now(), Payload: taskID})
	}
}

func parseLegatoRef(name string) (credential.Ref, error) {
	return credential.ParseRef("legato://" + name)
}

// Remove deletes a secret.
func (s SealedIntegrationSecrets) Remove(ctx context.Context, name string) error {
	return s.Store.Remove(ctx, name)
}
