package service

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/cpave3/legato/internal/engine/store"
)

// Argus's cases in Attention: Argus pushes one item per case, and the
// "argus" producer shows the open ones on the task that owns the case's PR.
// See openspec/changes/argus-attention.

const (
	AttentionSignalArgusCase     = "argus_case"
	AttentionSignalArgusNeedsYou = "argus_needs_you"

	// caseAttentionProducer is both the producer and the one integration
	// that may push items.
	caseAttentionProducer = "argus"

	caseTitleMax  = 200 // characters
	caseReasonMax = 300 // characters
	caseDetailMax = 4096
	// MaxCaseSyncKeys is the most keys a sync may list.
	MaxCaseSyncKeys = 10000
)

var caseKeyPattern = regexp.MustCompile(`^case-[A-Za-z0-9-]{1,96}$`)

// CaseTarget is the work a case is about. Repo is required with any of the
// others.
type CaseTarget struct {
	Repo   string `json:"repo,omitempty"`
	PR     int    `json:"pr,omitempty"`
	Branch string `json:"branch,omitempty"`
	SHA    string `json:"sha,omitempty"`
}

// CasePush is a case as Argus pushes it.
type CasePush struct {
	Revision string     `json:"revision"`
	Signal   string     `json:"signal"`
	Title    string     `json:"title"`
	Reason   string     `json:"reason"`
	Detail   string     `json:"detail"`
	Urgency  string     `json:"urgency"`
	Target   CaseTarget `json:"target"`
	Link     string     `json:"link"`
}

// CaseAttentionResult is what an upsert answers. ItemID is left out until
// the Attention item exists.
type CaseAttentionResult struct {
	ItemID string `json:"itemId,omitempty"`
	TaskID string `json:"taskId,omitempty"`
	Status string `json:"status"`
}

// CaseOwnerFinder finds the tasks that own a PR, branch or commit.
type CaseOwnerFinder interface {
	Lookup(ctx context.Context, q OwnerQuery) ([]OwnerMatch, error)
}

type CaseAttentionOptions struct {
	// Origin is integrations.argus.url; empty when Argus is not configured,
	// which refuses pushes and shows no items.
	Origin    string
	Attention *AttentionService
	Owners    CaseOwnerFinder
}

// CaseAttentionService takes Argus's cases and keeps them in Attention.
type CaseAttentionService struct {
	db   *store.Store
	opts CaseAttentionOptions
}

func NewCaseAttentionService(db *store.Store, opts CaseAttentionOptions) *CaseAttentionService {
	return &CaseAttentionService{db: db, opts: opts}
}

// Producer is the "argus" Attention producer. Register it whether or not
// Argus is configured: without it, its items are resolved.
func (s *CaseAttentionService) Producer() AttentionProducer { return caseAttentionAdapter{s} }

func (s *CaseAttentionService) check(integration string) error {
	if s.opts.Origin == "" {
		return delegationErr("unavailable", "the Argus integration is not configured (integrations.argus.url)")
	}
	if integration != caseAttentionProducer {
		return delegationErr("forbidden", "only the argus integration can push Attention items; this token is %q", integration)
	}
	return nil
}

// Upsert adds or updates a case's item. An upsert at the revision the case
// was resolved at changes nothing.
func (s *CaseAttentionService) Upsert(ctx context.Context, integration, key string, push CasePush) (CaseAttentionResult, error) {
	if err := s.check(integration); err != nil {
		return CaseAttentionResult{}, err
	}
	if err := s.validate(key, &push); err != nil {
		return CaseAttentionResult{}, err
	}
	target, _ := json.Marshal(push.Target)
	row, err := s.db.UpsertIntegrationAttention(ctx, store.IntegrationAttention{
		Integration: integration, Key: key, Revision: push.Revision, Signal: push.Signal,
		Title: push.Title, Reason: push.Reason, Detail: push.Detail, Urgency: push.Urgency,
		Target: string(target), Link: push.Link,
	})
	if err != nil {
		return CaseAttentionResult{}, err
	}
	if err := s.reconcile(ctx); err != nil {
		return CaseAttentionResult{}, err
	}
	res := CaseAttentionResult{Status: row.State}
	if item, err := s.db.GetAttentionItemBySource(ctx, caseAttentionProducer, key); err == nil {
		res.ItemID, res.TaskID, res.Status = item.ID, item.TaskID, item.Status
	} else if !errors.Is(err, store.ErrNotFound) {
		return CaseAttentionResult{}, err
	}
	return res, nil
}

// Resolve closes a case's item at a revision.
func (s *CaseAttentionService) Resolve(ctx context.Context, integration, key, revision, reason string) error {
	if err := s.check(integration); err != nil {
		return err
	}
	if !caseKeyPattern.MatchString(key) {
		return delegationErr("bad_request", "the key must be case- followed by up to 96 letters, digits and dashes")
	}
	if err := s.db.ResolveIntegrationAttention(ctx, integration, key, strings.TrimSpace(revision), reason); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return delegationErr("not_found", "Legato has no item for %s", key)
		}
		return err
	}
	return s.reconcile(ctx)
}

// Sync closes every open item whose key is not in keys, and says how many.
func (s *CaseAttentionService) Sync(ctx context.Context, integration string, keys []string) (int, error) {
	if err := s.check(integration); err != nil {
		return 0, err
	}
	if len(keys) > MaxCaseSyncKeys {
		return 0, delegationErr("bad_request", "a sync lists at most %d keys", MaxCaseSyncKeys)
	}
	for _, key := range keys {
		if !caseKeyPattern.MatchString(key) {
			return 0, delegationErr("bad_request", "%q is not a case key: case- followed by up to 96 letters, digits and dashes", key)
		}
	}
	n, err := s.db.ResolveIntegrationAttentionExcept(ctx, integration, keys, "sync")
	if err != nil {
		return 0, err
	}
	return n, s.reconcile(ctx)
}

// reconcile brings Attention up to date: Argus's items, and the PR items
// that leave out what an Argus case already says.
func (s *CaseAttentionService) reconcile(ctx context.Context) error {
	if s.opts.Attention == nil {
		return nil
	}
	if err := s.opts.Attention.Reconcile(ctx, caseAttentionProducer); err != nil {
		return err
	}
	if s.opts.Attention.HasProducer(prAttentionProducer) {
		return s.opts.Attention.Reconcile(ctx, prAttentionProducer)
	}
	return nil
}

func (s *CaseAttentionService) validate(key string, p *CasePush) error {
	if !caseKeyPattern.MatchString(key) {
		return delegationErr("bad_request", "the key must be case- followed by up to 96 letters, digits and dashes")
	}
	p.Revision, p.Title = strings.TrimSpace(p.Revision), strings.TrimSpace(p.Title)
	p.Reason = strings.TrimSpace(firstLine(p.Reason))
	switch {
	case p.Revision == "":
		return delegationErr("bad_request", "revision is required")
	case p.Title == "":
		return delegationErr("bad_request", "title is required")
	case p.Reason == "":
		return delegationErr("bad_request", "reason is required")
	case p.Signal != AttentionSignalArgusCase && p.Signal != AttentionSignalArgusNeedsYou:
		return delegationErr("bad_request", "signal must be argus_case or argus_needs_you")
	case p.Urgency != "low" && p.Urgency != "normal" && p.Urgency != "high":
		return delegationErr("bad_request", "urgency must be low, normal or high")
	case p.Target.PR < 0:
		return delegationErr("bad_request", "target.pr must be a PR number")
	}
	p.Target.Repo, p.Target.Branch, p.Target.SHA = strings.TrimSpace(p.Target.Repo), strings.TrimSpace(p.Target.Branch), strings.TrimSpace(p.Target.SHA)
	if p.Target.Repo == "" && (p.Target.PR != 0 || p.Target.Branch != "" || p.Target.SHA != "") {
		return delegationErr("bad_request", "target.repo is required with a PR, branch or SHA")
	}
	if !sameOrigin(p.Link, s.opts.Origin) {
		return delegationErr("invalid_link", "the link must be an https address on Argus's origin, %s", s.opts.Origin)
	}
	p.Title = cutRunes(p.Title, caseTitleMax)
	p.Reason = cutRunes(p.Reason, caseReasonMax)
	p.Detail = cutBytes(p.Detail, caseDetailMax)
	return nil
}

// sameOrigin says whether link is an https address (or http on loopback) on
// the origin of base.
func sameOrigin(link, base string) bool {
	l, err := url.Parse(link)
	if err != nil || l.Host == "" {
		return false
	}
	b, err := url.Parse(base)
	if err != nil || b.Host == "" {
		return false
	}
	if l.Scheme != "https" && !(l.Scheme == "http" && isLoopbackHost(l.Hostname())) {
		return false
	}
	return strings.EqualFold(l.Scheme, b.Scheme) && strings.EqualFold(hostWithPort(l), hostWithPort(b))
}

func hostWithPort(u *url.URL) string {
	port := u.Port()
	if port == "" {
		port = map[string]string{"https": "443", "http": "80"}[strings.ToLower(u.Scheme)]
	}
	return net.JoinHostPort(u.Hostname(), port)
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func firstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return s[:i]
	}
	return s
}

// cutRunes cuts s to at most max characters, ending in an ellipsis when cut.
func cutRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max-1]) + "…"
}

// cutBytes cuts s to at most max bytes at a character boundary, ending in
// an ellipsis when cut.
func cutBytes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	end := max - len("…")
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end] + "…"
}

// caseAttentionAdapter is the "argus" producer: the open cases, each on the
// task that owns its work now.
type caseAttentionAdapter struct{ s *CaseAttentionService }

func (a caseAttentionAdapter) Producer() string { return caseAttentionProducer }

func (a caseAttentionAdapter) Snapshot(ctx context.Context) ([]AttentionInput, error) {
	if a.s.opts.Origin == "" {
		return nil, nil
	}
	rows, err := a.s.db.ListOpenIntegrationAttention(ctx, caseAttentionProducer)
	if err != nil {
		return nil, err
	}
	items := make([]AttentionInput, 0, len(rows))
	for _, row := range rows {
		item := AttentionInput{
			SignalKind: row.Signal, SourceKey: row.Key, SourceRevision: row.Revision,
			Title: row.Title, Reason: row.Reason, Detail: row.Detail,
			Category: caseAttentionProducer, Urgency: row.Urgency,
			Navigation: AttentionNavigation{Kind: "external", Target: row.Link},
		}
		var target CaseTarget
		_ = json.Unmarshal([]byte(row.Target), &target)
		if task := a.owner(ctx, target); task != nil {
			item.TaskID, item.WorkspaceID = task.ID, task.WorkspaceID
		}
		items = append(items, item)
	}
	return items, nil
}

// owner is the task that owns a case's work, or nil.
func (a caseAttentionAdapter) owner(ctx context.Context, target CaseTarget) *store.Task {
	if a.s.opts.Owners == nil || target.Repo == "" {
		return nil
	}
	matches, err := a.s.opts.Owners.Lookup(ctx, OwnerQuery(target))
	if err != nil || len(matches) == 0 {
		return nil
	}
	task, err := a.s.db.GetTask(ctx, matches[0].Task.ID)
	if err != nil {
		return nil
	}
	return task
}
